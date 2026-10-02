// Package ingest moves (user_id, segment) pairs from RabbitMQ into the store.
//
// Delivery semantics: at-least-once delivery + idempotent writes, which gives
// "effectively-once" results. A message is acknowledged only after the batch
// containing it has been durably inserted, so a crash at any point leads to a
// redelivery, never to a loss. Redelivered rows are harmless because the store
// is idempotent and estimates count distinct users.
package ingest

import (
	"context"
	"errors"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/KhashayarKhm/go-challenge/internal/store"
	"github.com/KhashayarKhm/go-challenge/pkg/segmentation"
)

// ErrDeliveriesClosed is returned by Run when the broker closes the delivery
// channel (connection or channel failure). Unacknowledged messages are
// requeued by RabbitMQ, so the process can simply restart.
var ErrDeliveriesClosed = errors.New("ingest: delivery channel closed")

// Config tunes batching.
type Config struct {
	// BatchSize is the maximum number of rows per insert. It must equal the
	// consumer prefetch: RabbitMQ then never hands out more unacked messages
	// than one batch, which bounds memory without a separate limit goroutine.
	BatchSize int
	// FlushInterval bounds how long a partially filled batch waits, i.e. the
	// maximum delay before a tagging becomes visible to estimates.
	FlushInterval time.Duration
	// RetryBackoff is how long to pause after a failed insert, so a store
	// outage does not turn into a hot redelivery loop.
	RetryBackoff time.Duration
	// ShutdownTimeout bounds the final flush when Run is canceled.
	ShutdownTimeout time.Duration
}

// DefaultConfig returns production defaults: ClickHouse prefers inserts of
// 10k+ rows at most about once per second.
func DefaultConfig() Config {
	return Config{
		BatchSize:       10_000,
		FlushInterval:   10 * time.Second,
		RetryBackoff:    2 * time.Second,
		ShutdownTimeout: 5 * time.Second,
	}
}

// Batcher accumulates deliveries in memory and flushes them to a store.Writer
// when the batch is full or the flush interval elapses, whichever comes first.
// All state is owned by the single Run goroutine, so no locking is needed.
type Batcher struct {
	writer store.Writer
	cfg    Config
	log    *slog.Logger
	now    func() time.Time

	// tick overrides the ticker in tests.
	tick <-chan time.Time

	rows []store.Membership
	// last is the most recent buffered delivery; acking it with multiple=true
	// acknowledges the whole batch in one round trip.
	last amqp.Delivery
}

// NewBatcher creates a Batcher. A nil logger discards logs.
func NewBatcher(w store.Writer, cfg Config, log *slog.Logger) *Batcher {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Batcher{
		writer: w,
		cfg:    cfg,
		log:    log,
		now:    time.Now,
		rows:   make([]store.Membership, 0, cfg.BatchSize),
	}
}

// Run consumes deliveries until ctx is canceled (it then flushes what it has
// and returns nil) or the delivery channel is closed (ErrDeliveriesClosed).
func (b *Batcher) Run(ctx context.Context, deliveries <-chan amqp.Delivery) error {
	tick := b.tick
	if tick == nil {
		t := time.NewTicker(b.cfg.FlushInterval)
		defer t.Stop()
		tick = t.C
	}

	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), b.cfg.ShutdownTimeout)
			defer cancel()
			b.flush(flushCtx)
			return nil

		case d, ok := <-deliveries:
			if !ok {
				// The channel is gone, so acks are impossible; the broker
				// requeues everything unacked. Drop the buffer.
				return ErrDeliveriesClosed
			}
			b.add(d)
			if len(b.rows) >= b.cfg.BatchSize {
				b.flush(ctx)
			}

		case <-tick:
			b.flush(ctx)
		}
	}
}

// add decodes one delivery into the buffer. Messages that can never be valid
// are rejected without requeue, which routes them to the dead-letter queue
// instead of blocking the queue forever.
func (b *Batcher) add(d amqp.Delivery) {
	msg, err := segmentation.DecodeMessage(d.Body)
	if err != nil {
		b.log.Warn("rejecting invalid message", "delivery_tag", d.DeliveryTag, "err", err)
		if err := d.Reject(false); err != nil {
			b.log.Error("reject failed", "delivery_tag", d.DeliveryTag, "err", err)
		}
		return
	}

	b.rows = append(b.rows, store.Membership{
		UserID:  msg.UserID,
		Segment: msg.Segment,
		Day:     store.DayOf(b.eventTime(d)),
	})
	b.last = d
}

// eventTime prefers the publish timestamp set by the segmentation client, so
// a backlog or replay does not move users to the day ES consumed them.
func (b *Batcher) eventTime(d amqp.Delivery) time.Time {
	if !d.Timestamp.IsZero() {
		return d.Timestamp
	}
	return b.now()
}

// flush inserts the buffer, then acks it; on failure it nacks with requeue so
// RabbitMQ redelivers the batch later. The buffer is cleared either way: the
// broker, not the process, is the source of truth for unprocessed messages.
func (b *Batcher) flush(ctx context.Context) {
	if len(b.rows) == 0 {
		return
	}
	defer b.reset()

	if err := b.writer.Insert(ctx, b.rows); err != nil {
		b.log.Error("insert failed; requeueing batch", "rows", len(b.rows), "err", err)
		if err := b.last.Nack(true, true); err != nil {
			b.log.Error("nack failed", "err", err)
		}
		b.sleep(ctx, b.cfg.RetryBackoff)
		return
	}

	if err := b.last.Ack(true); err != nil {
		// The rows are stored but will be redelivered: harmless, writes are idempotent.
		b.log.Error("ack failed", "err", err)
		return
	}
	b.log.Debug("batch flushed", "rows", len(b.rows))
}

func (b *Batcher) reset() {
	b.rows = b.rows[:0]
	b.last = amqp.Delivery{}
}

func (b *Batcher) sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
