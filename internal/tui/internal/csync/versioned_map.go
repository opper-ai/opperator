package csync

import (
	"sync"
	"sync/atomic"
)

// increasing version each time the contents change. It is primarily used to
// observe when diagnostic collections change without scanning the entire map.
type VersionedMap[K comparable, V any] struct {
	mu      sync.RWMutex
	inner   map[K]V
	version atomic.Uint64
}

// NewVersionedMap constructs an empty VersionedMap instance.
func NewVersionedMap[K comparable, V any]() *VersionedMap[K, V] {
	return &VersionedMap[K, V]{
		inner: make(map[K]V),
	}
}

// it was present.
func (m *VersionedMap[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.inner[key]
	return v, ok
}

func (m *VersionedMap[K, V]) Set(key K, value V) {
	m.mu.Lock()
	m.inner[key] = value
	m.mu.Unlock()
	m.version.Add(1)
}

func (m *VersionedMap[K, V]) Del(key K) {
	m.mu.Lock()
	delete(m.inner, key)
	m.mu.Unlock()
	m.version.Add(1)
}

func (m *VersionedMap[K, V]) Snapshot() map[K]V {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[K]V, len(m.inner))
	for k, v := range m.inner {
		out[k] = v
	}
	return out
}

// Len reports the current number of entries stored in the map.
func (m *VersionedMap[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.inner)
}

func (m *VersionedMap[K, V]) Version() uint64 {
	return m.version.Load()
}
