package auth

import (
	"crypto/sha256"
	"sync"
	"time"
)

type cacheEntry struct {
	err       error
	timestamp time.Time
}
type AuthCache struct {
	ttl     time.Duration
	mu      sync.Mutex
	entries map[[32]byte]cacheEntry
}

func NewCache(ttl time.Duration) *AuthCache {
	return &AuthCache{ttl: ttl, entries: make(map[[32]byte]cacheEntry)}
}
func cacheKey(token, path string) [32]byte { return sha256.Sum256([]byte(token + "\x00" + path)) }
func (c *AuthCache) Get(token, path string) (error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := cacheKey(token, path)
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Since(entry.timestamp) > c.ttl {
		delete(c.entries, key)
		return nil, false
	}
	return entry.err, true
}
func (c *AuthCache) Set(token, path string, err error) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.entries {
		if time.Since(v.timestamp) > c.ttl {
			delete(c.entries, k)
		}
	}
	key := cacheKey(token, path)
	if _, exists := c.entries[key]; !exists && len(c.entries) >= 4096 {
		return
	}
	c.entries[key] = cacheEntry{err: err, timestamp: time.Now()}
}
