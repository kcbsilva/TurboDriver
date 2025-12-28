package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
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

type Handler struct {
	store  *dispatch.Store
	hub    *dispatch.Hub
	auth   authConfig
	events dispatch.EventLogger
	db     dispatch.RideLister
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

	passengerID := payload.PassengerID
	if identity.Role == dispatch.RolePassenger {
		passengerID = identity.ID
	}

	ride, err := h.store.CreateRide(passengerID, dispatch.Coordinate{
		Latitude:  payload.PickupLat,
		Longitude: payload.PickupLong,
		At:        time.Now(),
	})
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
	h.hub.PublishRideUpdate(ride)
	respondJSON(w, http.StatusOK, ride)
}

func (h *Handler) awaitAcceptance(rideID, driverID string) {
	const window = 15 * time.Second
	time.Sleep(window)

	ride, changed, err := h.store.ReassignIfUnaccepted(rideID, driverID)
	if err != nil || !changed {
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
}
