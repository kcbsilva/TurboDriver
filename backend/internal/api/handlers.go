package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"turbodriver/internal/dispatch"
)

func requireRole(w http.ResponseWriter, r *http.Request, enforce bool, allowed ...dispatch.IdentityRole) bool {
	if !enforce {
		return true
	}
	id, ok := identityFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	for _, role := range allowed {
		if id.Role == role {
			return true
		}
	}
	respondError(w, http.StatusForbidden, "forbidden")
	return false
}

func matchIdentity(w http.ResponseWriter, r *http.Request, enforce bool, targetID string) bool {
	if !enforce {
		return true
	}
	id, ok := identityFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	if id.Role == dispatch.RoleAdmin {
		return true
	}
	if id.ID != targetID {
		respondError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func canAccessRide(r *http.Request, enforce bool, ride dispatch.Ride) bool {
	if !enforce {
		return true
	}
	id, ok := identityFromContext(r.Context())
	if !ok {
		return false
	}
	return canAccessRideWithIdentity(id, ride)
}

func canAccessRideWithIdentity(id dispatch.Identity, ride dispatch.Ride) bool {
	if id.Role == dispatch.RoleAdmin {
		return true
	}
	if id.Role == dispatch.RolePassenger && ride.PassengerID == id.ID {
		return true
	}
	if id.Role == dispatch.RoleDriver && ride.DriverID == id.ID {
		return true
	}
	return false
}

// ApplicationStore captures persistence for driver applications/profiles.
type ApplicationStore interface {
	UpsertDriverApplication(ctx context.Context, app dispatch.DriverApplication) (int64, error)
	GetDriverApplication(ctx context.Context, driverID string) (dispatch.DriverApplication, bool, error)
	UpdateApplicationStatus(ctx context.Context, driverID string, status dispatch.DriverApplicationStatus) error
	UpsertDriverLicense(ctx context.Context, lic dispatch.DriverLicense) (int64, error)
	UpsertDriverVehicle(ctx context.Context, veh dispatch.DriverVehicle) (int64, error)
	ReplaceVehiclePhotos(ctx context.Context, vehicleID int64, photos []dispatch.VehiclePhoto) error
	UpsertLiveness(ctx context.Context, liv dispatch.DriverLiveness) (int64, error)
	LoadApplicationDetails(ctx context.Context, driverID string) (dispatch.DriverApplication, bool, error)
	UpsertPassengerProfile(ctx context.Context, prof dispatch.PassengerProfile) (int64, error)
	GetPassengerProfile(ctx context.Context, passengerID string) (dispatch.PassengerProfile, bool, error)
	UpsertRating(ctx context.Context, r dispatch.Rating) error
	GetRatingsForRide(ctx context.Context, rideID string) ([]dispatch.Rating, error)
	GetRatingsForProfile(ctx context.Context, profileID string) ([]dispatch.Rating, error)
}

type Handler struct {
	store  *dispatch.Store
	hub    *dispatch.Hub
	auth   authConfig
	events dispatch.EventLogger
	db     dispatch.RideLister
	apps   ApplicationStore

	eventsLogged    int64
	rideStarts      int64
	rideAccepts     int64
	rideCancels     int64
	rideCompletes   int64
	acceptTimeouts  int64
	startTime       time.Time
	reqCount        int64
	reqErrors       int64
	reqLatencyNS    int64
	staleTTL        time.Duration
	matchLatencyNS  int64
	acceptLatencyNS int64
	matchBuckets    bucketCounter
	acceptBuckets   bucketCounter
	matchCount      int64
	matchSumNS      int64
	acceptCount     int64
	acceptSumNS     int64
}

type driverLocationPayload struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Accuracy  float64 `json:"accuracy,omitempty"`
	Timestamp int64   `json:"timestamp,omitempty"`
}

func (h *Handler) UpdateDriverLocation(w http.ResponseWriter, r *http.Request) {
	enforce := h.auth.store != nil
	if !requireRole(w, r, enforce, dispatch.RoleDriver, dispatch.RoleAdmin) {
		return
	}
	driverID := chi.URLParam(r, "driverID")
	if !matchIdentity(w, r, enforce, driverID) {
		return
	}
	var payload driverLocationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	ts := time.Now()
	if payload.Timestamp > 0 {
		ts = time.UnixMilli(payload.Timestamp)
	}
	loc := dispatch.Coordinate{
		Latitude:  payload.Latitude,
		Longitude: payload.Longitude,
		Accuracy:  payload.Accuracy,
		At:        ts,
	}

	state, err := h.store.UpdateDriverLocation(driverID, loc)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "failed to persist driver location")
		return
	}
	h.hub.PublishDriverUpdate(driverID, state)
	respondJSON(w, http.StatusOK, state)
}

type rideRequestPayload struct {
	PassengerID string  `json:"passengerId"`
	PickupLat   float64 `json:"pickupLat"`
	PickupLong  float64 `json:"pickupLong"`
	Idempotency string  `json:"idempotencyKey,omitempty"`
}

func (h *Handler) RequestRide(w http.ResponseWriter, r *http.Request) {
	enforce := h.auth.store != nil
	if !requireRole(w, r, enforce, dispatch.RolePassenger, dispatch.RoleAdmin) {
		return
	}
	identity, _ := identityFromContext(r.Context())
	var payload rideRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	// Idempotency: reuse existing ride when key matches
	if payload.Idempotency != "" {
		if ride, ok := h.store.LookupIdempotent(payload.Idempotency); ok {
			respondJSON(w, http.StatusOK, ride)
			return
		}
	}

	passengerID := payload.PassengerID
	if identity.Role == dispatch.RolePassenger {
		passengerID = identity.ID
	}

	ride, err := h.store.CreateRide(passengerID, dispatch.Coordinate{
		Latitude:  payload.PickupLat,
		Longitude: payload.PickupLong,
		At:        time.Now(),
	}, payload.Idempotency)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	h.hub.PublishRideUpdate(ride)
	h.logRideEvent(r.Context(), ride, "ride_requested", map[string]any{
		"passengerId": ride.PassengerID,
		"driverId":    ride.DriverID,
		"statusTo":    ride.Status,
	})
	h.rideStarts++
	if ride.CreatedAt.After(time.Time{}) {
		latency := time.Since(ride.CreatedAt)
		if ride.Status == dispatch.RideAssigned {
			atomic.AddInt64(&h.matchLatencyNS, latency.Nanoseconds())
			h.matchBuckets.observe(latency)
			atomic.AddInt64(&h.matchCount, 1)
			atomic.AddInt64(&h.matchSumNS, latency.Nanoseconds())
		}
	}
	go h.awaitAcceptance(ride.ID, ride.DriverID)
	respondJSON(w, http.StatusAccepted, ride)
}

func (h *Handler) GetRide(w http.ResponseWriter, r *http.Request) {
	rideID := chi.URLParam(r, "rideID")
	ride, ok := h.store.GetRide(rideID)
	if !ok {
		respondError(w, http.StatusNotFound, "ride not found")
		return
	}
	respondJSON(w, http.StatusOK, ride)
}

type acceptRidePayload struct {
	DriverID string `json:"driverId"`
}

func (h *Handler) AcceptRide(w http.ResponseWriter, r *http.Request) {
	enforce := h.auth.store != nil
	if !requireRole(w, r, enforce, dispatch.RoleDriver, dispatch.RoleAdmin) {
		return
	}
	rideID := chi.URLParam(r, "rideID")
	var payload acceptRidePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if !matchIdentity(w, r, enforce, payload.DriverID) {
		return
	}
	if enforce && !h.store.DriverIsFresh(payload.DriverID, h.staleTTL) {
		respondError(w, http.StatusBadRequest, "driver heartbeat too old")
		return
	}
	ride, prevStatus, err := h.store.AcceptRide(rideID, payload.DriverID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logRideEvent(r.Context(), ride, "ride_accepted", map[string]any{
		"driverId":   payload.DriverID,
		"statusFrom": prevStatus,
		"statusTo":   ride.Status,
	})
	h.rideAccepts++
	// acceptance latency: from assigned to accepted
	if ride.CreatedAt.After(time.Time{}) {
		latency := time.Since(ride.CreatedAt)
		atomic.AddInt64(&h.acceptLatencyNS, latency.Nanoseconds())
		h.acceptBuckets.observe(latency)
		atomic.AddInt64(&h.acceptCount, 1)
		atomic.AddInt64(&h.acceptSumNS, latency.Nanoseconds())
	}
	h.hub.PublishRideUpdate(ride)
	respondJSON(w, http.StatusOK, ride)
}

func (h *Handler) CancelRide(w http.ResponseWriter, r *http.Request) {
	enforce := h.auth.store != nil
	if !requireRole(w, r, enforce, dispatch.RolePassenger, dispatch.RoleDriver, dispatch.RoleAdmin) {
		return
	}
	rideID := chi.URLParam(r, "rideID")
	ride, prevStatus, err := h.store.CancelRide(rideID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !canAccessRide(r, enforce, ride) {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	h.logRideEvent(r.Context(), ride, "ride_cancelled", map[string]any{
		"statusFrom": prevStatus,
		"statusTo":   ride.Status,
	})
	h.rideCancels++
	h.hub.PublishRideUpdate(ride)
	respondJSON(w, http.StatusOK, ride)
}

func (h *Handler) CompleteRide(w http.ResponseWriter, r *http.Request) {
	enforce := h.auth.store != nil
	if !requireRole(w, r, enforce, dispatch.RoleDriver, dispatch.RoleAdmin) {
		return
	}
	rideID := chi.URLParam(r, "rideID")
	ride, prevStatus, err := h.store.CompleteRide(rideID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !matchIdentity(w, r, enforce, ride.DriverID) {
		return
	}
	h.logRideEvent(r.Context(), ride, "ride_completed", map[string]any{
		"driverId":   ride.DriverID,
		"statusFrom": prevStatus,
		"statusTo":   ride.Status,
	})
	h.rideCompletes++
	h.hub.PublishRideUpdate(ride)
	respondJSON(w, http.StatusOK, ride)
}

func (h *Handler) awaitAcceptance(rideID, driverID string) {
	const window = 15 * time.Second
	time.Sleep(window)

	ride, changed, err := h.store.ReassignIfUnaccepted(rideID, driverID)
	if err != nil || !changed {
		if err == nil && !changed {
			h.acceptTimeouts++
		}
		return
	}
	h.logRideEvent(context.Background(), ride, "ride_reassigned", map[string]any{
		"previousDriver": driverID,
		"newDriver":      ride.DriverID,
		"statusTo":       ride.Status,
	})
	h.hub.PublishRideUpdate(ride)
}

func (h *Handler) RideWebsocket(w http.ResponseWriter, r *http.Request) {
	rideID := chi.URLParam(r, "rideID")
	ride, ok := h.store.GetRide(rideID)
	if !ok {
		respondError(w, http.StatusNotFound, "ride not found")
		return
	}
	if id, ok := h.auth.authorized(r); !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	} else if h.auth.store != nil {
		if !canAccessRideWithIdentity(id, ride) {
			respondError(w, http.StatusForbidden, "forbidden")
			return
		}
	}
	h.hub.ServeRide(w, r, ride.ID)
}

func (h *Handler) RegisterIdentity(w http.ResponseWriter, r *http.Request) {
	if h.auth.store == nil {
		respondError(w, http.StatusServiceUnavailable, "auth not configured")
		return
	}
	if !requireRole(w, r, true, dispatch.RoleAdmin) {
		return
	}
	var payload struct {
		Role string `json:"role"`
		TTL  string `json:"ttl,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	ttl := h.auth.ttl
	if payload.TTL != "" {
		if parsed, err := time.ParseDuration(payload.TTL); err == nil {
			ttl = parsed
		}
	}
	if h.auth.signupSecret != "" {
		secret := r.Header.Get("X-Signup-Secret")
		if secret == "" {
			respondError(w, http.StatusUnauthorized, "missing signup secret")
			return
		}
		if subtle.ConstantTimeCompare([]byte(secret), []byte(h.auth.signupSecret)) != 1 {
			respondError(w, http.StatusForbidden, "invalid signup secret")
			return
		}
	}
	identity, err := h.auth.store.Register(dispatch.IdentityRole(payload.Role), ttl)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.auth.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		h.auth.db.Save(ctx, identity, ttl)
	}
	respondJSON(w, http.StatusOK, identity)
}

// SignupIdentity issues a token without admin (pilot convenience).
func (h *Handler) SignupIdentity(w http.ResponseWriter, r *http.Request) {
	if h.auth.store == nil {
		respondError(w, http.StatusServiceUnavailable, "auth not configured")
		return
	}
	var payload struct {
		Role string `json:"role"`
		TTL  string `json:"ttl,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	ttl := h.auth.ttl
	if payload.TTL != "" {
		if parsed, err := time.ParseDuration(payload.TTL); err == nil {
			ttl = parsed
		}
	}
	identity, err := h.auth.store.Register(dispatch.IdentityRole(payload.Role), ttl)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.auth.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		h.auth.db.Save(ctx, identity, ttl)
	}
	respondJSON(w, http.StatusOK, identity)
}

func (h *Handler) ListRideEvents(w http.ResponseWriter, r *http.Request) {
	if h.events == nil {
		respondError(w, http.StatusServiceUnavailable, "event log unavailable")
		return
	}
	if !requireRole(w, r, true, dispatch.RoleAdmin) {
		return
	}
	rideID := chi.URLParam(r, "rideID")
	limit := parseLimit(r.URL.Query().Get("limit"), 100)
	offset := parseOffset(r.URL.Query().Get("offset"))
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	events, err := h.events.ListRideEvents(ctx, rideID, limit, offset)
	total, _ := h.events.CountRideEvents(ctx, rideID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to fetch events")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data":   events,
		"limit":  limit,
		"offset": offset,
		"total":  total,
	})
}

func (h *Handler) ListPassengerRides(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		respondError(w, http.StatusServiceUnavailable, "ride history unavailable")
		return
	}
	identity, ok := identityFromContext(r.Context())
	if !ok || identity.Role != dispatch.RolePassenger {
		respondError(w, http.StatusForbidden, "passenger only")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 100)
	offset := parseOffset(r.URL.Query().Get("offset"))
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	rides, err := h.db.ListRidesByPassenger(ctx, identity.ID, limit, offset)
	total, _ := h.db.CountRidesByPassenger(ctx, identity.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to fetch rides")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data":   rides,
		"limit":  limit,
		"offset": offset,
		"total":  total,
	})
}

func (h *Handler) ListDriverRides(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		respondError(w, http.StatusServiceUnavailable, "ride history unavailable")
		return
	}
	identity, ok := identityFromContext(r.Context())
	if !ok || identity.Role != dispatch.RoleDriver {
		respondError(w, http.StatusForbidden, "driver only")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 100)
	offset := parseOffset(r.URL.Query().Get("offset"))
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	rides, err := h.db.ListRidesByDriver(ctx, identity.ID, limit, offset)
	total, _ := h.db.CountRidesByDriver(ctx, identity.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to fetch rides")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data":   rides,
		"limit":  limit,
		"offset": offset,
		"total":  total,
	})
}

func parseLimit(raw string, def int) int {
	if raw == "" {
		return def
	}
	if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 1000 {
		return v
	}
	return def
}

func parseOffset(raw string) int {
	if raw == "" {
		return 0
	}
	if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
		return v
	}
	return 0
}

// Driver application submission

type applicationPayload struct {
	LocationCode   string   `json:"locationCode"`
	RulesVersionID *int64   `json:"rulesVersionId,omitempty"`
	License        licBody  `json:"license"`
	Vehicle        vehBody  `json:"vehicle"`
	Photos         []photo  `json:"photos"`
	Liveness       liveBody `json:"liveness"`
}

type licBody struct {
	Number      string `json:"number"`
	Country     string `json:"country,omitempty"`
	Region      string `json:"region,omitempty"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
	Remunerated bool   `json:"remunerated"`
	DocumentURL string `json:"documentUrl,omitempty"`
}

type vehBody struct {
	Type            string `json:"type"`
	PlateNumber     string `json:"plateNumber,omitempty"`
	DocumentNumber  string `json:"documentNumber,omitempty"`
	DocumentURL     string `json:"documentUrl,omitempty"`
	DocumentExpires string `json:"documentExpiresAt,omitempty"`
	Ownership       string `json:"ownership"`
	ContractURL     string `json:"contractUrl,omitempty"`
	ContractExpires string `json:"contractExpiresAt,omitempty"`
}

type photo struct {
	Angle    string `json:"angle"`
	PhotoURL string `json:"photoUrl"`
}

type liveBody struct {
	ChallengeSequence []string          `json:"challengeSequence"`
	Captures          map[string]string `json:"captures"`
}

func (h *Handler) SubmitDriverApplication(w http.ResponseWriter, r *http.Request) {
	if h.apps == nil {
		respondError(w, http.StatusServiceUnavailable, "application store unavailable")
		return
	}
	enforce := h.auth.store != nil
	if !requireRole(w, r, enforce, dispatch.RoleDriver, dispatch.RoleAdmin) {
		return
	}
	driverID := chi.URLParam(r, "driverID")
	if !matchIdentity(w, r, enforce, driverID) {
		return
	}

	var payload applicationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if payload.LocationCode == "" || payload.License.Number == "" {
		respondError(w, http.StatusBadRequest, "locationCode and license.number are required")
		return
	}
	if err := validateVehicle(payload.Vehicle); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validatePhotos(payload.Photos); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateLiveness(payload.Liveness); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// License
	lic := dispatch.DriverLicense{
		DriverID:    driverID,
		Number:      payload.License.Number,
		Country:     payload.License.Country,
		Region:      payload.License.Region,
		Remunerated: payload.License.Remunerated,
		DocumentURL: payload.License.DocumentURL,
	}
	if t := parseOptionalTime(payload.License.ExpiresAt); t != nil {
		lic.ExpiresAt = t
	}
	if _, err := h.apps.UpsertDriverLicense(ctx, lic); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save license")
		return
	}

	// Vehicle
	veh := dispatch.DriverVehicle{
		DriverID:       driverID,
		Type:           strings.ToLower(payload.Vehicle.Type),
		PlateNumber:    payload.Vehicle.PlateNumber,
		DocumentNumber: payload.Vehicle.DocumentNumber,
		DocumentURL:    payload.Vehicle.DocumentURL,
		Ownership:      strings.ToLower(payload.Vehicle.Ownership),
		ContractURL:    payload.Vehicle.ContractURL,
	}
	if t := parseOptionalTime(payload.Vehicle.DocumentExpires); t != nil {
		veh.DocumentExpires = t
	}
	if t := parseOptionalTime(payload.Vehicle.ContractExpires); t != nil {
		veh.ContractExpires = t
	}
	vehID, err := h.apps.UpsertDriverVehicle(ctx, veh)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save vehicle")
		return
	}

	// Photos
	var photos []dispatch.VehiclePhoto
	for _, p := range payload.Photos {
		photos = append(photos, dispatch.VehiclePhoto{
			VehicleID: vehID,
			Angle:     strings.ToLower(p.Angle),
			PhotoURL:  p.PhotoURL,
		})
	}
	if len(photos) > 0 {
		if err := h.apps.ReplaceVehiclePhotos(ctx, vehID, photos); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save photos")
			return
		}
	}

	// Liveness
	capturesJSON, _ := json.Marshal(payload.Liveness.Captures)
	liv := dispatch.DriverLiveness{
		DriverID:          driverID,
		ChallengeSequence: payload.Liveness.ChallengeSequence,
		Captures:          capturesJSON,
		Verified:          false,
	}
	if _, err := h.apps.UpsertLiveness(ctx, liv); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save liveness")
		return
	}

	// Application record
	app := dispatch.DriverApplication{
		DriverID:     driverID,
		LocationCode: payload.LocationCode,
		RulesVersion: payload.RulesVersionID,
		Status:       dispatch.ApplicationPending,
	}
	if _, err := h.apps.UpsertDriverApplication(ctx, app); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save application")
		return
	}

	full, ok, err := h.apps.LoadApplicationDetails(ctx, driverID)
	if err != nil || !ok {
		respondError(w, http.StatusInternalServerError, "failed to load application")
		return
	}
	respondJSON(w, http.StatusOK, full)
}

func (h *Handler) GetDriverApplication(w http.ResponseWriter, r *http.Request) {
	if h.apps == nil {
		respondError(w, http.StatusServiceUnavailable, "application store unavailable")
		return
	}
	enforce := h.auth.store != nil
	if !requireRole(w, r, enforce, dispatch.RoleDriver, dispatch.RoleAdmin) {
		return
	}
	driverID := chi.URLParam(r, "driverID")
	if !matchIdentity(w, r, enforce, driverID) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	app, ok, err := h.apps.LoadApplicationDetails(ctx, driverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load application")
		return
	}
	if !ok {
		respondError(w, http.StatusNotFound, "application not found")
		return
	}
	respondJSON(w, http.StatusOK, app)
}

func (h *Handler) UpdateApplicationStatus(w http.ResponseWriter, r *http.Request) {
	if h.apps == nil {
		respondError(w, http.StatusServiceUnavailable, "application store unavailable")
		return
	}
	if !requireRole(w, r, true, dispatch.RoleAdmin) {
		return
	}
	driverID := chi.URLParam(r, "driverID")
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	newStatus := dispatch.DriverApplicationStatus(strings.ToLower(body.Status))
	switch newStatus {
	case dispatch.ApplicationPending, dispatch.ApplicationApproved, dispatch.ApplicationRejected, dispatch.ApplicationNeedsReview:
	default:
		respondError(w, http.StatusBadRequest, "invalid status")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := h.apps.UpdateApplicationStatus(ctx, driverID, newStatus); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update status")
		return
	}
	app, ok, err := h.apps.LoadApplicationDetails(ctx, driverID)
	if err != nil || !ok {
		respondJSON(w, http.StatusOK, map[string]string{"status": string(newStatus)})
		return
	}
	app.Status = newStatus
	respondJSON(w, http.StatusOK, app)
}

// Passenger profile

func (h *Handler) UpsertPassengerProfile(w http.ResponseWriter, r *http.Request) {
	if h.apps == nil {
		respondError(w, http.StatusServiceUnavailable, "profile store unavailable")
		return
	}
	enforce := h.auth.store != nil
	if !requireRole(w, r, enforce, dispatch.RolePassenger, dispatch.RoleAdmin) {
		return
	}
	pid := chi.URLParam(r, "passengerID")
	if !matchIdentity(w, r, enforce, pid) {
		return
	}
	var body struct {
		FullName     string `json:"fullName"`
		Address      string `json:"address,omitempty"`
		GovernmentID string `json:"governmentId,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if body.FullName == "" {
		respondError(w, http.StatusBadRequest, "fullName required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	prof := dispatch.PassengerProfile{
		PassengerID:  pid,
		FullName:     body.FullName,
		Address:      body.Address,
		GovernmentID: body.GovernmentID,
	}
	if _, err := h.apps.UpsertPassengerProfile(ctx, prof); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save profile")
		return
	}
	saved, ok, err := h.apps.GetPassengerProfile(ctx, pid)
	if err != nil || !ok {
		respondError(w, http.StatusInternalServerError, "failed to read profile")
		return
	}
	respondJSON(w, http.StatusOK, saved)
}

func (h *Handler) GetPassengerProfile(w http.ResponseWriter, r *http.Request) {
	if h.apps == nil {
		respondError(w, http.StatusServiceUnavailable, "profile store unavailable")
		return
	}
	enforce := h.auth.store != nil
	if !requireRole(w, r, enforce, dispatch.RolePassenger, dispatch.RoleAdmin) {
		return
	}
	pid := chi.URLParam(r, "passengerID")
	if !matchIdentity(w, r, enforce, pid) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	prof, ok, err := h.apps.GetPassengerProfile(ctx, pid)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to read profile")
		return
	}
	if !ok {
		respondError(w, http.StatusNotFound, "profile not found")
		return
	}
	respondJSON(w, http.StatusOK, prof)
}

// RateRide allows passenger and driver to rate each other (1-5 stars).
func (h *Handler) RateRide(w http.ResponseWriter, r *http.Request) {
	if h.apps == nil {
		respondError(w, http.StatusServiceUnavailable, "rating store unavailable")
		return
	}
	enforce := h.auth.store != nil
	if !requireRole(w, r, enforce, dispatch.RolePassenger, dispatch.RoleDriver, dispatch.RoleAdmin) {
		return
	}
	rideID := chi.URLParam(r, "rideID")
	ride, ok := h.store.GetRide(rideID)
	if !ok {
		respondError(w, http.StatusNotFound, "ride not found")
		return
	}
	id, _ := identityFromContext(r.Context())
	var body struct {
		Stars   int    `json:"stars"`
		Comment string `json:"comment,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if body.Stars < 1 || body.Stars > 5 {
		respondError(w, http.StatusBadRequest, "stars must be 1-5")
		return
	}
	if body.Stars <= 3 && strings.TrimSpace(body.Comment) == "" {
		respondError(w, http.StatusBadRequest, "comment required for 3 stars or less")
		return
	}
	var rating dispatch.Rating
	rating.RideID = rideID
	rating.Stars = body.Stars
	rating.Comment = body.Comment
	rating.RequiresAttention = body.Stars <= 3

	switch id.Role {
	case dispatch.RolePassenger:
		if ride.PassengerID != id.ID {
			respondError(w, http.StatusForbidden, "forbidden")
			return
		}
		if ride.DriverID == "" {
			respondError(w, http.StatusBadRequest, "ride missing driver")
			return
		}
		rating.RaterRole = dispatch.RolePassenger
		rating.RaterID = id.ID
		rating.RateeID = ride.DriverID
	case dispatch.RoleDriver:
		if ride.DriverID != id.ID {
			respondError(w, http.StatusForbidden, "forbidden")
			return
		}
		rating.RaterRole = dispatch.RoleDriver
		rating.RaterID = id.ID
		rating.RateeID = ride.PassengerID
	case dispatch.RoleAdmin:
		respondError(w, http.StatusForbidden, "admin cannot rate")
		return
	default:
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.apps.UpsertRating(ctx, rating); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save rating")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"stars": rating.Stars})
}

func (h *Handler) GetRatingsForDriver(w http.ResponseWriter, r *http.Request) {
	h.getRatingsForProfile(w, r, dispatch.RoleDriver)
}

func (h *Handler) GetRatingsForPassenger(w http.ResponseWriter, r *http.Request) {
	h.getRatingsForProfile(w, r, dispatch.RolePassenger)
}

func (h *Handler) getRatingsForProfile(w http.ResponseWriter, r *http.Request, role dispatch.IdentityRole) {
	if h.apps == nil {
		respondError(w, http.StatusServiceUnavailable, "rating store unavailable")
		return
	}
	enforce := h.auth.store != nil
	if !requireRole(w, r, enforce, role, dispatch.RoleAdmin) {
		return
	}
	var id string
	if role == dispatch.RoleDriver {
		id = chi.URLParam(r, "driverID")
	} else {
		id = chi.URLParam(r, "passengerID")
	}
	if !matchIdentity(w, r, enforce, id) && !(enforce && isAdmin(r)) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	ratings, err := h.apps.GetRatingsForProfile(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to fetch ratings")
		return
	}
	var sum int
	for _, rt := range ratings {
		sum += rt.Stars
	}
	avg := 0.0
	if len(ratings) > 0 {
		avg = float64(sum) / float64(len(ratings))
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"average": avg,
		"count":   len(ratings),
		"data":    ratings,
	})
}

func isAdmin(r *http.Request) bool {
	id, ok := identityFromContext(r.Context())
	return ok && id.Role == dispatch.RoleAdmin
}

// Summaries: profile + ride counts + ratings.
func (h *Handler) GetPassengerSummary(w http.ResponseWriter, r *http.Request) {
	h.getSummary(w, r, dispatch.RolePassenger)
}

func (h *Handler) GetDriverSummary(w http.ResponseWriter, r *http.Request) {
	h.getSummary(w, r, dispatch.RoleDriver)
}

func (h *Handler) getSummary(w http.ResponseWriter, r *http.Request, role dispatch.IdentityRole) {
	if h.apps == nil || h.db == nil {
		respondError(w, http.StatusServiceUnavailable, "summary unavailable")
		return
	}
	enforce := h.auth.store != nil
	if !requireRole(w, r, enforce, role, dispatch.RoleAdmin) {
		return
	}
	var id string
	if role == dispatch.RoleDriver {
		id = chi.URLParam(r, "driverID")
	} else {
		id = chi.URLParam(r, "passengerID")
	}
	if !matchIdentity(w, r, enforce, id) && !(enforce && isAdmin(r)) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var rideCount int
	var err error
	if role == dispatch.RoleDriver {
		rideCount, err = h.db.CountRidesByDriver(ctx, id)
	} else {
		rideCount, err = h.db.CountRidesByPassenger(ctx, id)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to count rides")
		return
	}

	profile := map[string]any{}
	if role == dispatch.RolePassenger {
		if prof, ok, err := h.apps.GetPassengerProfile(ctx, id); err == nil && ok {
			profile = profToMap(prof)
		}
	}

	ratings, err := h.apps.GetRatingsForProfile(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to fetch ratings")
		return
	}
	avg := 0.0
	if len(ratings) > 0 {
		var sum int
		for _, rt := range ratings {
			sum += rt.Stars
		}
		avg = float64(sum) / float64(len(ratings))
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"profile":       profile,
		"rideCount":     rideCount,
		"ratingAverage": avg,
		"ratingCount":   len(ratings),
		"ratings":       ratings,
	})
}

func profToMap(p dispatch.PassengerProfile) map[string]any {
	return map[string]any{
		"id":           p.ID,
		"passengerId":  p.PassengerID,
		"fullName":     p.FullName,
		"address":      p.Address,
		"governmentId": p.GovernmentID,
		"createdAt":    p.CreatedAt,
		"updatedAt":    p.UpdatedAt,
	}
}

func validateVehicle(v vehBody) error {
	vt := strings.ToLower(v.Type)
	if vt != "car" && vt != "motorcycle" && vt != "bus" {
		return fmt.Errorf("vehicle.type must be car, motorcycle, or bus")
	}
	own := strings.ToLower(v.Ownership)
	if own != "owns" && own != "renting" && own != "lent" {
		return fmt.Errorf("vehicle.ownership must be owns, renting, or lent")
	}
	if (own == "renting" || own == "lent") && v.ContractURL == "" {
		return fmt.Errorf("vehicle.contractUrl required when ownership is renting or lent")
	}
	return nil
}

func validatePhotos(ph []photo) error {
	required := map[string]bool{"front": false, "back": false, "left": false, "right": false}
	for _, p := range ph {
		angle := strings.ToLower(p.Angle)
		if _, ok := required[angle]; ok {
			required[angle] = true
		}
	}
	for angle, ok := range required {
		if !ok {
			return fmt.Errorf("missing vehicle photo angle: %s", angle)
		}
	}
	return nil
}

func validateLiveness(l liveBody) error {
	if len(l.ChallengeSequence) == 0 {
		return fmt.Errorf("liveness.challengeSequence required")
	}
	required := map[string]bool{"up": false, "down": false, "left": false, "right": false}
	for _, dir := range l.ChallengeSequence {
		if _, ok := required[strings.ToLower(dir)]; ok {
			required[strings.ToLower(dir)] = true
		}
	}
	for dir := range required {
		if _, ok := l.Captures[dir]; !ok {
			return fmt.Errorf("liveness.captures missing direction: %s", dir)
		}
	}
	return nil
}

func parseOptionalTime(val string) *time.Time {
	if val == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return nil
	}
	return &t
}

// Metrics exposes a minimal Prometheus text endpoint.
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "turbodriver_events_logged %d\n", h.eventsLogged)
	fmt.Fprintf(w, "turbodriver_ride_starts %d\n", h.rideStarts)
	fmt.Fprintf(w, "turbodriver_ride_accepts %d\n", h.rideAccepts)
	fmt.Fprintf(w, "turbodriver_ride_cancels %d\n", h.rideCancels)
	fmt.Fprintf(w, "turbodriver_ride_completes %d\n", h.rideCompletes)
	fmt.Fprintf(w, "turbodriver_ride_accept_timeouts %d\n", h.acceptTimeouts)
	uptime := time.Since(h.startTime).Seconds()
	fmt.Fprintf(w, "turbodriver_prunes %d\n", h.store.PruneCount())
	total, available, stale := h.store.SnapshotDrivers(h.staleTTL)
	fmt.Fprintf(w, "turbodriver_drivers_available %d\n", available)
	fmt.Fprintf(w, "turbodriver_drivers_stale_current %d\n", stale)
	zeroAvail := 0
	if available == 0 {
		zeroAvail = 1
	}
	fmt.Fprintf(w, "turbodriver_drivers_zero_available %d\n", zeroAvail)
	stalePct := 0.0
	if total > 0 {
		stalePct = float64(stale) / float64(total)
	}
	fmt.Fprintf(w, "turbodriver_drivers_stale_ratio %.4f\n", stalePct)
	fmt.Fprintf(w, "turbodriver_match_latency_seconds_total %.6f\n", float64(atomic.LoadInt64(&h.matchLatencyNS))/1e9)
	fmt.Fprintf(w, "turbodriver_accept_latency_seconds_total %.6f\n", float64(atomic.LoadInt64(&h.acceptLatencyNS))/1e9)
	fmt.Fprintf(w, "turbodriver_match_latency_seconds_sum %.6f\n", float64(atomic.LoadInt64(&h.matchSumNS))/1e9)
	fmt.Fprintf(w, "turbodriver_match_latency_seconds_count %d\n", atomic.LoadInt64(&h.matchCount))
	fmt.Fprintf(w, "turbodriver_accept_latency_seconds_sum %.6f\n", float64(atomic.LoadInt64(&h.acceptSumNS))/1e9)
	fmt.Fprintf(w, "turbodriver_accept_latency_seconds_count %d\n", atomic.LoadInt64(&h.acceptCount))
	for le, count := range h.matchBuckets.snapshot() {
		fmt.Fprintf(w, "turbodriver_match_latency_seconds_bucket{le=\"%.0f\"} %d\n", le, count)
	}
	for le, count := range h.acceptBuckets.snapshot() {
		fmt.Fprintf(w, "turbodriver_accept_latency_seconds_bucket{le=\"%.0f\"} %d\n", le, count)
	}
	fmt.Fprintf(w, "turbodriver_uptime_seconds %.0f\n", uptime)
	fmt.Fprintf(w, "turbodriver_goroutines %d\n", runtime.NumGoroutine())
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Fprintf(w, "turbodriver_mem_alloc_bytes %d\n", m.Alloc)
	fmt.Fprintf(w, "turbodriver_heap_objects %d\n", m.HeapObjects)
	fmt.Fprintf(w, "turbodriver_requests_total %d\n", atomic.LoadInt64(&h.reqCount))
	fmt.Fprintf(w, "turbodriver_request_errors_total %d\n", atomic.LoadInt64(&h.reqErrors))
	latencySec := float64(atomic.LoadInt64(&h.reqLatencyNS)) / 1e9
	fmt.Fprintf(w, "turbodriver_request_latency_seconds_total %.6f\n", latencySec)
}

// metricsMiddleware captures basic request metrics.
func (h *Handler) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		atomic.AddInt64(&h.reqCount, 1)
		if rec.status >= 400 {
			atomic.AddInt64(&h.reqErrors, 1)
		}
		atomic.AddInt64(&h.reqLatencyNS, time.Since(start).Nanoseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (h *Handler) observeBucket(buckets map[float64]int64, d time.Duration) {
	secs := d.Seconds()
	for le := range buckets {
		if secs <= le {
			v := buckets[le] + 1
			buckets[le] = v
		}
	}
}

func (h *Handler) logRideEvent(ctx context.Context, ride dispatch.Ride, evtType string, payload map[string]any) {
	if h.events == nil {
		return
	}
	body, _ := json.Marshal(payload)
	var actorID, actorRole string
	if id, ok := identityFromContext(ctx); ok {
		actorID = id.ID
		actorRole = string(id.Role)
	}
	_ = h.events.AppendRideEvent(ctx, dispatch.RideEvent{
		RideID:    ride.ID,
		Type:      evtType,
		Payload:   body,
		ActorID:   actorID,
		ActorRole: actorRole,
		CreatedAt: time.Now(),
	})
	h.eventsLogged++
}
