package pubsub

import "context"

// EventType identifies the lifecycle stage of an event payload.
type EventType string

const (
	// CreatedEvent signals that a new resource is available.
	CreatedEvent EventType = "created"
	// UpdatedEvent signals that an existing resource mutated.
	UpdatedEvent EventType = "updated"
	// DeletedEvent signals that a resource was removed.
	DeletedEvent EventType = "deleted"
)

// Event wraps a payload emitted by the broker.
type Event[T any] struct {
	Type    EventType
	Payload T
}

// Subscriber exposes the Subscribe API implemented by Broker.
type Subscriber[T any] interface {
	Subscribe(context.Context) <-chan Event[T]
}

// Publisher exposes the Publish API implemented by Broker.
type Publisher[T any] interface {
	Publish(EventType, T)
}
