package segmentation

import "context"

// Publisher is the port USS depends on. Code in USS should accept this
// interface rather than a concrete adapter, so it can be unit-tested with
// InMemoryPublisher and the transport can change without touching USS.
type Publisher interface {
	// Publish sends one (userID, segment) pair. A nil error means the pair is
	// durably accepted by the transport (for RabbitMQ: confirmed by the broker).
	Publish(ctx context.Context, userID, segment string) error
	// Close releases the underlying resources.
	Close() error
}
