package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// IdempotencyStore persists idempotency keys with TTL.
type IdempotencyStore struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

func NewIdempotencyStore(pool *pgxpool.Pool, ttl time.Duration) *IdempotencyStore {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &IdempotencyStore{pool: pool, ttl: ttl}
}

func (s *IdempotencyStore) TTL() time.Duration {
	return s.ttl
}

func (s *IdempotencyStore) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS idempotency_keys (
	key TEXT PRIMARY KEY,
	ride_id TEXT NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idempotency_keys_expires_idx ON idempotency_keys(expires_at);
`)
	return err
}

func (s *IdempotencyStore) Remember(ctx context.Context, key, rideID string) error {
	if key == "" || rideID == "" {
		return nil
	}
	exp := time.Now().Add(s.ttl)
	_, err := s.pool.Exec(ctx, `
INSERT INTO idempotency_keys (key, ride_id, expires_at)
VALUES ($1,$2,$3)
ON CONFLICT (key) DO UPDATE SET ride_id=EXCLUDED.ride_id, expires_at=EXCLUDED.expires_at
`, key, rideID, exp)
	return err
}

func (s *IdempotencyStore) Lookup(ctx context.Context, key string) (string, bool, error) {
	if key == "" {
		return "", false, nil
	}
	var rideID string
	var expires time.Time
	err := s.pool.QueryRow(ctx, `
SELECT ride_id, expires_at FROM idempotency_keys WHERE key = $1
`, key).Scan(&rideID, &expires)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return "", false, nil
		}
		return "", false, err
	}
	if time.Now().After(expires) {
		return "", false, nil
	}
	return rideID, true, nil
}
