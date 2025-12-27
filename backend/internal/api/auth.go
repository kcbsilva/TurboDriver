package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"turbodriver/internal/auth"
	"turbodriver/internal/dispatch"
)

type authConfig struct {
	store *auth.InMemoryStore
	db    IdentityDB
	ttl   time.Duration
}

type IdentityDB interface {
	Lookup(ctx context.Context, token string) (dispatch.Identity, bool, error)
	Save(ctx context.Context, ident dispatch.Identity, ttl time.Duration) (dispatch.Identity, error)
}

func newAuthConfig(store *auth.InMemoryStore, db IdentityDB, ttl time.Duration) authConfig {
	return authConfig{store: store, db: db, ttl: ttl}
}

func (a authConfig) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.store == nil && a.db == nil {
			next.ServeHTTP(w, r)
			return
		}
		token := parseToken(r)
		if token == "" {
			respondError(w, http.StatusUnauthorized, "missing token")
			return
		}
		identity, ok := a.lookup(r.Context(), token)
		if !ok {
			respondError(w, http.StatusForbidden, "invalid token")
			return
		}
		ctx := context.WithValue(r.Context(), identityCtxKey{}, identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authorized returns identity when present and valid.
func (a authConfig) authorized(r *http.Request) (dispatch.Identity, bool) {
	token := parseToken(r)
	if token == "" {
		return dispatch.Identity{}, false
	}
	return a.lookup(r.Context(), token)
}

type identityCtxKey struct{}

func identityFromContext(ctx context.Context) (dispatch.Identity, bool) {
	id, ok := ctx.Value(identityCtxKey{}).(dispatch.Identity)
	return id, ok
}

func (a authConfig) lookup(ctx context.Context, token string) (dispatch.Identity, bool) {
	if a.store != nil {
		if id, ok := a.store.Lookup(token); ok {
			return id, true
		}
	}
	if a.db != nil {
		id, ok, err := a.db.Lookup(ctx, token)
		if err == nil && ok {
			return id, true
		}
	}
	return dispatch.Identity{}, false
}

func parseToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	return ""
}
