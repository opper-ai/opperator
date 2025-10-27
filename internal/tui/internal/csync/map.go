package csync

import "sync"

// Map provides a minimal concurrent map implementation for simple use cases.
type Map[K comparable, V any] struct {
	mu    sync.RWMutex
	inner map[K]V
}

// NewMap allocates an empty Map.
func NewMap[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{inner: make(map[K]V)}
}

// NewMapFrom wraps an existing map with concurrency protection.
func NewMapFrom[K comparable, V any](data map[K]V) *Map[K, V] {
	if data == nil {
		data = make(map[K]V)
	}
	return &Map[K, V]{inner: data}
}

func (m *Map[K, V]) Set(key K, value V) {
	m.mu.Lock()
	m.inner[key] = value
	m.mu.Unlock()
}

func (m *Map[K, V]) GetOrSet(key K, fn func() V) V {
	if v, ok := m.Get(key); ok {
		return v
	}
	v := fn()
	m.Set(key, v)
	return v
}

func (m *Map[K, V]) Del(key K) {
	m.mu.Lock()
	delete(m.inner, key)
	m.mu.Unlock()
}

func (m *Map[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.inner[key]
	return v, ok
}

func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.inner)
}

func (m *Map[K, V]) Take(key K) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.inner[key]
	if ok {
		delete(m.inner, key)
	}
	return v, ok
}

func (m *Map[K, V]) Keys() []K {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]K, 0, len(m.inner))
	for k := range m.inner {
		keys = append(keys, k)
	}
	return keys
}

// point-in-time view of the entries.
func (m *Map[K, V]) Snapshot() map[K]V {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[K]V, len(m.inner))
	for k, v := range m.inner {
		out[k] = v
	}
	return out
}

// Range iterates over the entries in the map, calling fn for each key/value
// pair. Iteration stops early if fn returns false.
func (m *Map[K, V]) Range(fn func(K, V) bool) {
	snapshot := m.Snapshot()
	for k, v := range snapshot {
		if !fn(k, v) {
			return
		}
	}
}
