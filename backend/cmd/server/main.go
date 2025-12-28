package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/redis/go-redis/v9"

	"turbodriver/internal/api"
	"turbodriver/internal/auth"
	"turbodriver/internal/dispatch"
	"turbodriver/internal/geo"
	"turbodriver/internal/storage"
)

func main() {
	addr := envOrDefault("HTTP_ADDR", ":8080")

	store, authStore, identityDB, authTTL, eventLogger, rideLister := initStore()
	hub := dispatch.NewHub()
	go hub.Run()

	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	api.AttachRoutes(r, store, hub, authStore, identityDB, authTTL, eventLogger, rideLister)

	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("TurboDriver API listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func initStore() (*dispatch.Store, *auth.InMemoryStore, *storage.IdentityStore, time.Duration, storage.EventLogger, dispatch.RideLister) {
	dbURL := os.Getenv("DATABASE_URL")
	redisURL := envOrDefault("REDIS_URL", "redis://redis:6379")
	authEnabled := envOrDefault("AUTH_MODE", "memory")
	authTTL := parseDuration(envOrDefault("AUTH_TTL", "720h")) // default 30 days

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var (
		persist dispatch.Persistence
		geoLoc  dispatch.GeoLocator = geo.NewInMemoryGeo()
		authMem *auth.InMemoryStore
		idDB    *storage.IdentityStore
		events  storage.EventLogger
		rideLst dispatch.RideLister
	)

	if dbURL != "" {
		pool, err := storage.DefaultPool(ctx, dbURL)
		if err != nil {
			log.Printf("database connection failed, falling back to in-memory: %v", err)
		} else if err := storage.EnsureSchema(ctx, pool); err != nil {
			log.Printf("schema init failed, falling back to in-memory: %v", err)
		} else {
			log.Printf("using PostgreSQL persistence")
			pg := storage.NewPostgres(pool)
			persist = pg
			events = pg
			rideLst = pg
			idDB = storage.NewIdentityStore(pool)
			if err := idDB.EnsureSchema(ctx); err != nil {
				log.Printf("identity schema init failed: %v", err)
				idDB = nil
			}
		}
	}

	if redisURL != "" {
		opt, err := redis.ParseURL(redisURL)
		if err == nil {
			client := redis.NewClient(opt)
			if err := client.Ping(ctx).Err(); err != nil {
				log.Printf("redis unreachable, geo fallback to in-memory: %v", err)
			} else {
				log.Printf("using Redis geo index")
				geoLoc = redisGeoLocator{idx: geo.NewIndex(client)}
			}
		} else {
			log.Printf("redis URL parse error, geo fallback to in-memory: %v", err)
		}
	}

	if authEnabled == "memory" && authMem == nil {
		authMem = auth.NewInMemoryStore()
		log.Printf("auth: in-memory token issuance enabled")
		if idDB != nil {
			seedIdentities(ctx, idDB, authMem)
		}
	}

	return dispatch.NewStoreWithDeps(persist, geoLoc), authMem, idDB, authTTL, events, rideLst
}

func parseDuration(val string) time.Duration {
	d, err := time.ParseDuration(val)
	if err != nil {
		return 0
	}
	return d
}

func seedIdentities(ctx context.Context, db *storage.IdentityStore, mem *auth.InMemoryStore) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	all, err := db.All(ctx)
	if err != nil {
		log.Printf("failed to preload identities: %v", err)
		return
	}
	for _, ident := range all {
		mem.Seed(ident)
	}
}

// adapter structs to avoid package import cycle
type redisGeoLocator struct{ idx *geo.Index }

func (r redisGeoLocator) Nearby(lat, lon, radiusKM float64) (string, float64, error) {
	return r.idx.Nearby(context.Background(), lat, lon, radiusKM)
}
func (r redisGeoLocator) Add(driverID string, lat, lon float64) error {
	return r.idx.AddDriver(context.Background(), driverID, lat, lon)
}
func (r redisGeoLocator) Remove(driverID string) error {
	return r.idx.RemoveDriver(context.Background(), driverID)
}
