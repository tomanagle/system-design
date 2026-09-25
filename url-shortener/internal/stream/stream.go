package stream

import "context"

type Message struct {
	Key   []byte
	Value []byte
}

type Producer interface {
	Produce(Message) bool
}

type Consumer interface {
	Run(context.Context) error
}

type EventFactory interface {
	NewEvent(code, userAgent, language string) Event
}

type EventStore interface {
	Insert(context.Context, []StoredEvent) error
}
