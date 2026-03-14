package auth

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

type cacheEntry struct {
	err       error
	timestamp time.Time
}

type AuthCache struct {
	ttl     time.Duration
	entries sync.Map
}

func NewCache(ttl time.Duration) *AuthCache {
	return &AuthCache{ttl: ttl}
}

func cacheKey(token, node string) string {
	h := sha256.Sum256([]byte(token + node))
	return fmt.Sprintf("%x", h)
}

func (c *AuthCache) Get(token, node string) (error, bool) {
	key := cacheKey(token, node)
	val, ok := c.entries.Load(key)
	if !ok {
		return nil, false
	}
	entry := val.(*cacheEntry)
	if time.Since(entry.timestamp) > c.ttl {
		c.entries.Delete(key)
		return nil, false
	}
	return entry.err, true
}

func (c *AuthCache) Set(token, node string, err error) {
	key := cacheKey(token, node)
	c.entries.Store(key, &cacheEntry{
		err:       err,
		timestamp: time.Now(),
	})
}
