package api

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"turbodriver/internal/auth"
	"turbodriver/internal/dispatch"
	"turbodriver/internal/storage"
)

// AttachRoutes wires HTTP routes to handlers.
func AttachRoutes(r chi.Router, store *dispatch.Store, hub *dispatch.Hub, authStore *auth.InMemoryStore, identityDB *storage.IdentityStore, defaultTTL time.Duration, eventLogger dispatch.EventLogger, rideLister dispatch.RideLister) {
	authCfg := newAuthConfig(authStore, identityDB, defaultTTL)
	handler := &Handler{
		store:     store,
		hub:       hub,
		auth:      authCfg,
		events:    eventLogger,
		db:        rideLister,
		startTime: time.Now(),
		staleTTL:  parseDurationEnv("DRIVER_TTL", "5m"),
	}

	r.Use(handler.metricsMiddleware)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	r.Group(func(pr chi.Router) {
		pr.Use(authCfg.middleware)
		pr.Post("/api/drivers/{driverID}/location", handler.UpdateDriverLocation)
		pr.Post("/api/rides", handler.RequestRide)
		pr.Get("/api/rides/{rideID}", handler.GetRide)
		pr.Get("/api/history/passenger", handler.ListPassengerRides)
		pr.Get("/api/history/driver", handler.ListDriverRides)
		pr.Post("/api/rides/{rideID}/accept", handler.AcceptRide)
		pr.Post("/api/rides/{rideID}/cancel", handler.CancelRide)
		pr.Post("/api/rides/{rideID}/complete", handler.CompleteRide)
	})

	r.Group(func(pr chi.Router) {
		pr.Use(authCfg.middleware)
		pr.Post("/api/auth/register", handler.RegisterIdentity)
		pr.Get("/api/admin/rides/{rideID}/events", handler.ListRideEvents)
	})

	r.Get("/metrics", handler.Metrics)

	r.Group(func(pr chi.Router) {
		pr.Use(authCfg.middleware)
		fileServer := http.StripPrefix("/admin", http.FileServer(http.Dir("./static/admin")))
		pr.Get("/admin/*", func(w http.ResponseWriter, r *http.Request) {
			fileServer.ServeHTTP(w, r)
		})
	})

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

func parseDurationEnv(key, def string) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		val = def
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		return 0
	}
	return d
}
