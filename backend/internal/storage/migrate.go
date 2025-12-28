package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ApplySchema applies schema.sql once, recording hash in migrations table.
func ApplySchema(ctx context.Context, pool *pgxpool.Pool) error {
	if err := ensureMigrationTable(ctx, pool); err != nil {
		return err
	}
	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		return err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(schema))
	applied, err := isHashApplied(ctx, pool, hash)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO migrations (name, hash) VALUES ($1,$2)`, "schema.sql", hash)
	return err
}

func ensureMigrationTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS migrations (
	id SERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	hash TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS migrations_name_hash_idx ON migrations(name, hash);
`)
	return err
}

func isHashApplied(ctx context.Context, pool *pgxpool.Pool, hash string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM migrations WHERE name=$1 AND hash=$2)`, "schema.sql", hash).Scan(&exists)
	return exists, err
}
