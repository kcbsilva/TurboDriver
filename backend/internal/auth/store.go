package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"turbodriver/internal/dispatch"
)

// InMemoryStore keeps issued tokens mapped to identities.
type InMemoryStore struct {
    mu    sync.RWMutex
    users map[string]dispatch.Identity
}

func NewInMemoryStore() *InMemoryStore {
    return &InMemoryStore{
        users: make(map[string]dispatch.Identity),
    }
}

// Register creates an identity with the given role and returns the token.
func (s *InMemoryStore) Register(role dispatch.IdentityRole, ttl time.Duration) (dispatch.Identity, error) {
    if role != dispatch.RoleDriver && role != dispatch.RolePassenger && role != dispatch.RoleAdmin {
        return dispatch.Identity{}, errors.New("invalid role")
    }
    id := fmt.Sprintf("%s_%s", role, randomID())
    token := randomID()

    identity := dispatch.Identity{
        ID:    id,
        Role:  role,
        Token: token,
    }
    if ttl > 0 {
        expiry := time.Now().Add(ttl)
        identity.ExpiresAt = &expiry
    }

	s.mu.Lock()
	s.users[token] = identity
	s.mu.Unlock()
	return identity, nil
}

func (s *InMemoryStore) Lookup(token string) (dispatch.Identity, bool) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    u, ok := s.users[token]
    if !ok {
        return dispatch.Identity{}, false
    }
    if u.ExpiresAt != nil && time.Now().After(*u.ExpiresAt) {
        return dispatch.Identity{}, false
    }
    return u, ok
}

// Seed allows hydrating identities from persistent storage.
func (s *InMemoryStore) Seed(identity dispatch.Identity) {
    if identity.Token == "" {
        return
    }
    if identity.ExpiresAt != nil && time.Now().After(*identity.ExpiresAt) {
        return
    }
    s.mu.Lock()
    s.users[identity.Token] = identity
    s.mu.Unlock()
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
