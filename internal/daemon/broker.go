package daemon

import (
	"context"
	"sync"
)

// Broker fan-outs events to subscribers without blocking publishers.
type Broker[T any] struct {
	mu        sync.RWMutex
	subs      map[chan T]struct{}
	done      chan struct{}
	bufferCap int
}

// NewBroker constructs a broker with sensible defaults.
func NewBroker[T any]() *Broker[T] {
	return &Broker[T]{
		subs:      make(map[chan T]struct{}),
		done:      make(chan struct{}),
		bufferCap: 64,
	}
}

// Shutdown closes the broker and all subscriber channels.
func (b *Broker[T]) Shutdown() {
	select {
	case <-b.done:
		return
	default:
		close(b.done)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.subs {
		close(ch)
	}
	clear(b.subs)
}

// Subscribe registers for future events. The returned channel closes when the
// provided context is done or the broker shuts down.
func (b *Broker[T]) Subscribe(ctx context.Context) <-chan T {
	b.mu.Lock()
	defer b.mu.Unlock()

	select {
	case <-b.done:
		ch := make(chan T)
		close(ch)
		return ch
	default:
	}

	ch := make(chan T, b.bufferCap)
	b.subs[ch] = struct{}{}

	go func() {
		<-ctx.Done()

		b.mu.Lock()
		defer b.mu.Unlock()

		if _, ok := b.subs[ch]; !ok {
			return
		}
		delete(b.subs, ch)
		close(ch)
	}()

	return ch
}

// Publish sends payload to all subscribers using best-effort delivery.
func (b *Broker[T]) Publish(payload T) {
	b.mu.RLock()
	select {
	case <-b.done:
		b.mu.RUnlock()
		return
	default:
	}

	subs := make([]chan T, 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- payload:
		default:
			// Slow subscriber; skip to avoid blocking the publisher.
		}
	}
}

func (b *Broker[T]) GetSubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
