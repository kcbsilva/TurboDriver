package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"turbodriver/internal/dispatch"
)

type IdentityStore struct {
	pool *pgxpool.Pool
}

func NewIdentityStore(pool *pgxpool.Pool) *IdentityStore {
	return &IdentityStore{pool: pool}
}

func (s *IdentityStore) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS identities (
	id TEXT PRIMARY KEY,
	role TEXT NOT NULL,
	token TEXT UNIQUE NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	expires_at TIMESTAMPTZ
);
`)
	return err
}

func (s *IdentityStore) Save(ctx context.Context, ident dispatch.Identity, ttl time.Duration) (dispatch.Identity, error) {
	var expires *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expires = &t
	}
	_, err := s.pool.Exec(ctx, `
INSERT INTO identities (id, role, token, expires_at)
VALUES ($1,$2,$3,$4)
ON CONFLICT (id) DO UPDATE SET role = EXCLUDED.role, token = EXCLUDED.token, expires_at = EXCLUDED.expires_at
`, ident.ID, ident.Role, ident.Token, expires)
	if err != nil {
		return dispatch.Identity{}, err
	}
	ident.ExpiresAt = expires
	return ident, nil
}

func (s *IdentityStore) Lookup(ctx context.Context, token string) (dispatch.Identity, bool, error) {
	var ident dispatch.Identity
	var expires *time.Time
	err := s.pool.QueryRow(ctx, `
SELECT id, role, token, expires_at FROM identities WHERE token = $1
`, token).Scan(&ident.ID, &ident.Role, &ident.Token, &expires)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return dispatch.Identity{}, false, err
		}
		if err.Error() == "no rows in result set" {
			return dispatch.Identity{}, false, nil
		}
		return dispatch.Identity{}, false, err
	}
	if expires != nil && expires.Before(time.Now()) {
		return dispatch.Identity{}, false, nil
	}
	return ident, true, nil
}

func (s *IdentityStore) All(ctx context.Context) ([]dispatch.Identity, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, role, token FROM identities`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dispatch.Identity
	for rows.Next() {
		var ident dispatch.Identity
		if err := rows.Scan(&ident.ID, &ident.Role, &ident.Token); err != nil {
			return nil, err
		}
		out = append(out, ident)
	}
	return out, rows.Err()
}
