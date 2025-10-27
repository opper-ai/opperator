package pubsub

import (
	"context"
	"sync"
)

const defaultBufferSize = 64

// Broker fan-outs events to subscribers without blocking publishers.
type Broker[T any] struct {
	mu        sync.RWMutex
	subs      map[chan Event[T]]struct{}
	done      chan struct{}
	subCount  int
	bufferCap int
}

// NewBroker constructs a broker with sensible defaults.
func NewBroker[T any]() *Broker[T] {
	return NewBrokerWithOptions[T](defaultBufferSize)
}

// NewBrokerWithOptions builds a broker using the provided channel buffer size.
func NewBrokerWithOptions[T any](buffer int) *Broker[T] {
	if buffer <= 0 {
		buffer = defaultBufferSize
	}
	return &Broker[T]{
		subs:      make(map[chan Event[T]]struct{}),
		done:      make(chan struct{}),
		bufferCap: buffer,
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
	b.subCount = 0
}

// Subscribe registers for future events. The returned channel closes when the
// provided context is done or the broker shuts down.
func (b *Broker[T]) Subscribe(ctx context.Context) <-chan Event[T] {
	b.mu.Lock()
	defer b.mu.Unlock()

	select {
	case <-b.done:
		ch := make(chan Event[T])
		close(ch)
		return ch
	default:
	}

	ch := make(chan Event[T], b.bufferCap)
	b.subs[ch] = struct{}{}
	b.subCount++

	go func() {
		<-ctx.Done()

		b.mu.Lock()
		defer b.mu.Unlock()

		if _, ok := b.subs[ch]; !ok {
			return
		}
		delete(b.subs, ch)
		close(ch)
		b.subCount--
	}()

	return ch
}

// Publish sends payload to all subscribers using best-effort delivery.
func (b *Broker[T]) Publish(t EventType, payload T) {
	b.mu.RLock()
	select {
	case <-b.done:
		b.mu.RUnlock()
		return
	default:
	}

	subs := make([]chan Event[T], 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.RUnlock()

	evt := Event[T]{Type: t, Payload: payload}
	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
			// Slow subscriber; skip to avoid blocking the publisher.
		}
	}
}

func (b *Broker[T]) GetSubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.subCount
}
