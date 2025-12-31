package dispatch

import (
	"sync"
	"time"
)

type idemEntry struct {
	rideID string
	expiry time.Time
}

type idemCache struct {
	mu     sync.Mutex
	byKey  map[string]idemEntry
	ttl    time.Duration
}

func newIdemCache() *idemCache {
	return &idemCache{
		byKey: make(map[string]idemEntry),
		ttl:   30 * time.Minute,
	}
}

// SetTTL overrides ttl used for cache entries.
func (c *idemCache) SetTTL(ttl time.Duration) {
	if ttl > 0 {
		c.ttl = ttl
	}
}

// Remember stores key->ride mapping.
func (c *idemCache) Remember(key, rideID string) {
	if key == "" || rideID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byKey[key] = idemEntry{rideID: rideID, expiry: time.Now().Add(c.ttl)}
}

// Lookup returns ride id if key exists and not expired.
func (c *idemCache) Lookup(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.byKey[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiry) {
		delete(c.byKey, key)
		return "", false
	}
	return entry.rideID, true
}
