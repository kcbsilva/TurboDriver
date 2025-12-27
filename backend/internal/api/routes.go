package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"turbodriver/internal/dispatch"
)

// AttachRoutes wires HTTP routes to handlers.
func AttachRoutes(r chi.Router, store *dispatch.Store, hub *dispatch.Hub) {
	handler := &Handler{store: store, hub: hub}

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	r.Post("/api/drivers/{driverID}/location", handler.UpdateDriverLocation)
	r.Post("/api/rides", handler.RequestRide)
	r.Get("/api/rides/{rideID}", handler.GetRide)
	r.Post("/api/rides/{rideID}/accept", handler.AcceptRide)
	r.Post("/api/rides/{rideID}/cancel", handler.CancelRide)
	r.Post("/api/rides/{rideID}/complete", handler.CompleteRide)

	r.Get("/ws/rides/{rideID}", handler.RideWebsocket)
}

func respondJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body != nil {
		json.NewEncoder(w).Encode(body)
	}
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
