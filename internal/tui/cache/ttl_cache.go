package cache

import (
	"sync"
	"time"
)

// CacheEntry represents a cached value with expiration time
type CacheEntry[T any] struct {
	Value      T
	ExpiresAt  time.Time
}

// TTLCache is a thread-safe cache with time-to-live support
type TTLCache[K comparable, V any] struct {
	mu      sync.RWMutex
	entries map[K]CacheEntry[V]
	ttl     time.Duration
}

// NewTTLCache creates a new TTL cache with the specified time-to-live
func NewTTLCache[K comparable, V any](ttl time.Duration) *TTLCache[K, V] {
	return &TTLCache[K, V]{
		entries: make(map[K]CacheEntry[V]),
		ttl:     ttl,
	}
}

// Get retrieves a value from the cache if it exists and hasn't expired
func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[key]
	if !exists {
		var zero V
		return zero, false
	}

	// Check if entry has expired
	if time.Now().After(entry.ExpiresAt) {
		var zero V
		return zero, false
	}

	return entry.Value, true
}

// Set stores a value in the cache with TTL
func (c *TTLCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = CacheEntry[V]{
		Value:     value,
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

// Invalidate removes a specific key from the cache
func (c *TTLCache[K, V]) Invalidate(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
}

// InvalidateAll clears the entire cache
func (c *TTLCache[K, V]) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[K]CacheEntry[V])
}

// Cleanup removes expired entries from the cache
func (c *TTLCache[K, V]) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, key)
		}
	}
}

// Size returns the number of entries in the cache (including expired ones)
func (c *TTLCache[K, V]) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}
