package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
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

type Handler struct {
	store  *dispatch.Store
	hub    *dispatch.Hub
	auth   authConfig
	events dispatch.EventLogger
	db     dispatch.RideLister

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
	matchBuckets    map[float64]int64
	acceptBuckets   map[float64]int64
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
			h.observeBucket(h.matchBuckets, latency)
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
		h.observeBucket(h.acceptBuckets, latency)
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
	for le := range h.matchBuckets {
		fmt.Fprintf(w, "turbodriver_match_latency_seconds_bucket{le=\"%.0f\"} %d\n", le, h.matchBuckets[le])
	}
	for le := range h.acceptBuckets {
		fmt.Fprintf(w, "turbodriver_accept_latency_seconds_bucket{le=\"%.0f\"} %d\n", le, h.acceptBuckets[le])
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
