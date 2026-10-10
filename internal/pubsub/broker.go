package pubsub

import "sync"

// Broker fans out every published message to all current subscribers (alerts today, WebSocket feeds later).
// Publishing never blocks: a subscriber that falls behind has messages dropped instead of stalling the workers.
type Broker[T any] struct {
	mu         sync.RWMutex
	subs       map[chan T]struct{}
	bufferSize int
}

func NewBroker[T any](bufferSize int) *Broker[T] {
	return &Broker[T]{
		subs:       make(map[chan T]struct{}),
		bufferSize: bufferSize,
	}
}

// Subscribe returns a receive-only channel of messages and a function to stop listening
func (b *Broker[T]) Subscribe() (<-chan T, func()) {
	ch := make(chan T, b.bufferSize)

	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch) // Lets the subscriber's `for msg := range ch` loop end
		}
	}
	return ch, unsubscribe
}

// Publish delivers msg to every subscriber that has room in its buffer
func (b *Broker[T]) Publish(msg T) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- msg:
		default: // Slow subscriber, drop rather than block the caller
		}
	}
}
