package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"turbodriver/internal/auth"
	"turbodriver/internal/dispatch"
	"turbodriver/internal/storage"
)

// Seed script: creates sample passenger/driver identities for local testing.
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dbURL := envOrDefault("DATABASE_URL", "postgres://turbodriver:turbodriver@localhost:5432/turbodriver?sslmode=disable")
	pool, err := storage.DefaultPool(ctx, dbURL)
	if err != nil {
		log.Fatalf("db connect failed: %v", err)
	}
	if err := storage.EnsureSchema(ctx, pool); err != nil {
		log.Fatalf("schema ensure failed: %v", err)
	}

	idStore := storage.NewIdentityStore(pool)
	if err := idStore.EnsureSchema(ctx); err != nil {
		log.Fatalf("identity schema failed: %v", err)
	}
	pg := storage.NewPostgres(pool)

	mem := auth.NewInMemoryStore()
	ttl := 24 * time.Hour

	passenger, _ := mem.Register(dispatch.RolePassenger, ttl)
	driver, _ := mem.Register(dispatch.RoleDriver, ttl)
	admin, _ := mem.Register(dispatch.RoleAdmin, ttl)

	mem.Seed(passenger)
	mem.Seed(driver)
	mem.Seed(admin)

	for _, ident := range []dispatch.Identity{passenger, driver, admin} {
		if _, err := idStore.Save(ctx, ident, ttl); err != nil {
			log.Fatalf("save identity failed: %v", err)
		}
		fmt.Printf("%s: id=%s token=%s expires=%v\n", ident.Role, ident.ID, ident.Token, ident.ExpiresAt)
	}

	// seed driver location (NYC) for quick testing
	_ = pg.SaveDriver(dispatch.DriverState{
		ID:        driver.ID,
		Available: true,
		Location: dispatch.Coordinate{
			Latitude:  40.758,
			Longitude: -73.9855,
			Accuracy:  5,
			At:        time.Now(),
		},
		UpdatedAt: time.Now(),
		Status:    "idle",
		RadiusKM:  3,
	})
}

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
