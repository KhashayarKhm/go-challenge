package segmentation

import (
	"context"
	"sync"
)

// InMemoryPublisher is a Publisher fake for USS unit tests. It records every
// valid published message and can be told to fail via Err.
type InMemoryPublisher struct {
	mu       sync.Mutex
	messages []Message
	// Err, when non-nil, is returned by every Publish call (to simulate an
	// unavailable broker).
	Err error
}

var _ Publisher = (*InMemoryPublisher)(nil)

// Publish validates the pair like the real adapter does and records it.
func (p *InMemoryPublisher) Publish(ctx context.Context, userID, segment string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m := Message{UserID: userID, Segment: segment}
	if err := m.Validate(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Err != nil {
		return p.Err
	}
	p.messages = append(p.messages, m)
	return nil
}

// Messages returns a copy of everything published so far, in order.
func (p *InMemoryPublisher) Messages() []Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Message(nil), p.messages...)
}

// Close is a no-op.
func (p *InMemoryPublisher) Close() error { return nil }
