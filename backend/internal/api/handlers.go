package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"turbodriver/internal/dispatch"
)

type Handler struct {
	store *dispatch.Store
	hub   *dispatch.Hub
}

type driverLocationPayload struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Accuracy  float64 `json:"accuracy,omitempty"`
	Timestamp int64   `json:"timestamp,omitempty"`
}

func (h *Handler) UpdateDriverLocation(w http.ResponseWriter, r *http.Request) {
	driverID := chi.URLParam(r, "driverID")
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
	var payload rideRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	ride, err := h.store.CreateRide(payload.PassengerID, dispatch.Coordinate{
		Latitude:  payload.PickupLat,
		Longitude: payload.PickupLong,
		At:        time.Now(),
	})
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	h.hub.PublishRideUpdate(ride)
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
	rideID := chi.URLParam(r, "rideID")
	var payload acceptRidePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	ride, err := h.store.AcceptRide(rideID, payload.DriverID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.hub.PublishRideUpdate(ride)
	respondJSON(w, http.StatusOK, ride)
}

func (h *Handler) CancelRide(w http.ResponseWriter, r *http.Request) {
	rideID := chi.URLParam(r, "rideID")
	ride, err := h.store.CancelRide(rideID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.hub.PublishRideUpdate(ride)
	respondJSON(w, http.StatusOK, ride)
}

func (h *Handler) CompleteRide(w http.ResponseWriter, r *http.Request) {
	rideID := chi.URLParam(r, "rideID")
	ride, err := h.store.CompleteRide(rideID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.hub.PublishRideUpdate(ride)
	respondJSON(w, http.StatusOK, ride)
}

func (h *Handler) RideWebsocket(w http.ResponseWriter, r *http.Request) {
	rideID := chi.URLParam(r, "rideID")
	ride, ok := h.store.GetRide(rideID)
	if !ok {
		respondError(w, http.StatusNotFound, "ride not found")
		return
	}
	h.hub.ServeRide(w, r, ride.ID)
}
