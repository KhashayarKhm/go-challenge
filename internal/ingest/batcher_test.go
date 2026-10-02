package ingest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/KhashayarKhm/go-challenge/internal/store"
)

// fakeAcker records broker acknowledgements (implements amqp.Acknowledger).
type fakeAcker struct {
	mu      sync.Mutex
	acks    []ackCall
	nacks   []ackCall
	rejects []uint64
}

type ackCall struct {
	tag      uint64
	multiple bool
	requeue  bool
}

func (a *fakeAcker) Ack(tag uint64, multiple bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.acks = append(a.acks, ackCall{tag: tag, multiple: multiple})
	return nil
}

func (a *fakeAcker) Nack(tag uint64, multiple, requeue bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nacks = append(a.nacks, ackCall{tag: tag, multiple: multiple, requeue: requeue})
	return nil
}

func (a *fakeAcker) Reject(tag uint64, requeue bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rejects = append(a.rejects, tag)
	return nil
}

func (a *fakeAcker) snapshot() (acks, nacks []ackCall, rejects []uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]ackCall(nil), a.acks...), append([]ackCall(nil), a.nacks...), append([]uint64(nil), a.rejects...)
}

// fakeWriter records inserted batches and can fail on demand.
type fakeWriter struct {
	mu      sync.Mutex
	batches [][]store.Membership
	err     error
}

func (w *fakeWriter) Insert(_ context.Context, ms []store.Membership) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return w.err
	}
	w.batches = append(w.batches, append([]store.Membership(nil), ms...))
	return nil
}

func (w *fakeWriter) snapshot() [][]store.Membership {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([][]store.Membership(nil), w.batches...)
}

var publishedAt = time.Date(2026, 9, 5, 23, 30, 0, 0, time.UTC)

type harness struct {
	t          *testing.T
	batcher    *Batcher
	writer     *fakeWriter
	acker      *fakeAcker
	deliveries chan amqp.Delivery
	tick       chan time.Time
	cancel     context.CancelFunc
	done       chan error
	nextTag    uint64
}

func newHarness(t *testing.T, batchSize int, writerErr error) *harness {
	t.Helper()
	h := &harness{
		t:          t,
		writer:     &fakeWriter{err: writerErr},
		acker:      &fakeAcker{},
		deliveries: make(chan amqp.Delivery),
		tick:       make(chan time.Time),
		done:       make(chan error, 1),
	}
	h.batcher = NewBatcher(h.writer, Config{BatchSize: batchSize, ShutdownTimeout: time.Second}, nil)
	h.batcher.tick = h.tick
	h.batcher.now = func() time.Time { return time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC) }

	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() { h.done <- h.batcher.Run(ctx, h.deliveries) }()
	t.Cleanup(func() { h.stop() })
	return h
}

// send hands one delivery to the batcher. The channel is unbuffered, so when
// send returns the previous delivery has been fully processed.
func (h *harness) send(body string, ts time.Time) uint64 {
	h.nextTag++
	h.deliveries <- amqp.Delivery{
		Acknowledger: h.acker,
		DeliveryTag:  h.nextTag,
		Timestamp:    ts,
		Body:         []byte(body),
	}
	return h.nextTag
}

// tickAndWait triggers a flush and waits until it has finished: the second
// send on the unbuffered tick channel blocks until the first flush returned.
func (h *harness) tickAndWait() {
	h.tick <- time.Time{}
	h.tick <- time.Time{}
}

func (h *harness) stop() error {
	if h.cancel == nil {
		return nil
	}
	h.cancel()
	h.cancel = nil
	select {
	case err := <-h.done:
		return err
	case <-time.After(2 * time.Second):
		h.t.Fatal("batcher did not stop")
		return nil
	}
}

const sports = `{"user_id":"u1","segment":"sports"}`

func TestFlushWhenBatchIsFull(t *testing.T) {
	h := newHarness(t, 2, nil)
	h.send(sports, publishedAt)
	last := h.send(`{"user_id":"u2","segment":"sports"}`, publishedAt)
	h.send(`{"user_id":"u3","segment":"news"}`, publishedAt) // processed only after the full flush returned

	batches := h.writer.snapshot()
	if len(batches) != 1 || len(batches[0]) != 2 {
		t.Fatalf("batches = %+v, want one batch of 2", batches)
	}
	acks, _, _ := h.acker.snapshot()
	if len(acks) != 1 || acks[0] != (ackCall{tag: last, multiple: true}) {
		t.Fatalf("acks = %+v, want single multiple-ack of tag %d", acks, last)
	}
}

func TestFlushOnTick(t *testing.T) {
	h := newHarness(t, 100, nil)
	tag := h.send(sports, publishedAt)
	h.tickAndWait()

	batches := h.writer.snapshot()
	if len(batches) != 1 || len(batches[0]) != 1 {
		t.Fatalf("batches = %+v, want one batch of 1", batches)
	}
	acks, _, _ := h.acker.snapshot()
	if len(acks) != 1 || acks[0].tag != tag || !acks[0].multiple {
		t.Fatalf("acks = %+v", acks)
	}
}

func TestEmptyTickDoesNotInsert(t *testing.T) {
	h := newHarness(t, 100, nil)
	h.tickAndWait()
	if got := h.writer.snapshot(); len(got) != 0 {
		t.Fatalf("expected no insert, got %+v", got)
	}
}

func TestInsertFailureNacksWithRequeue(t *testing.T) {
	h := newHarness(t, 100, errors.New("clickhouse down"))
	tag := h.send(sports, publishedAt)
	h.tickAndWait()

	acks, nacks, _ := h.acker.snapshot()
	if len(acks) != 0 {
		t.Fatalf("must not ack a failed batch, got %+v", acks)
	}
	if len(nacks) != 1 || nacks[0] != (ackCall{tag: tag, multiple: true, requeue: true}) {
		t.Fatalf("nacks = %+v, want requeue of tag %d", nacks, tag)
	}
}

func TestInvalidMessageIsRejectedWithoutRequeue(t *testing.T) {
	h := newHarness(t, 100, nil)
	bad := h.send(`not json`, publishedAt)
	empty := h.send(`{"user_id":"","segment":"sports"}`, publishedAt)
	h.tickAndWait()

	_, _, rejects := h.acker.snapshot()
	if len(rejects) != 2 || rejects[0] != bad || rejects[1] != empty {
		t.Fatalf("rejects = %v, want [%d %d]", rejects, bad, empty)
	}
	if got := h.writer.snapshot(); len(got) != 0 {
		t.Fatalf("invalid messages must not be inserted, got %+v", got)
	}
}

func TestDayComesFromPublishTimestamp(t *testing.T) {
	h := newHarness(t, 100, nil)
	h.send(sports, publishedAt) // published late on Sept 5 (UTC)
	h.send(sports, time.Time{}) // no timestamp: falls back to now (Sept 7)
	h.tickAndWait()

	batch := h.writer.snapshot()[0]
	if want := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC); !batch[0].Day.Equal(want) {
		t.Fatalf("day = %v, want %v", batch[0].Day, want)
	}
	if want := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC); !batch[1].Day.Equal(want) {
		t.Fatalf("fallback day = %v, want %v", batch[1].Day, want)
	}
	if batch[0].UserID != "u1" || batch[0].Segment != "sports" {
		t.Fatalf("unexpected row %+v", batch[0])
	}
}

func TestShutdownFlushesBuffer(t *testing.T) {
	h := newHarness(t, 100, nil)
	tag := h.send(sports, publishedAt)
	if err := h.stop(); err != nil {
		t.Fatalf("Run returned %v, want nil on cancel", err)
	}
	if got := h.writer.snapshot(); len(got) != 1 {
		t.Fatalf("expected final flush, got %+v", got)
	}
	acks, _, _ := h.acker.snapshot()
	if len(acks) != 1 || acks[0].tag != tag {
		t.Fatalf("acks = %+v", acks)
	}
}

func TestClosedDeliveriesReturnsError(t *testing.T) {
	h := newHarness(t, 100, nil)
	h.send(sports, publishedAt)
	close(h.deliveries)

	select {
	case err := <-h.done:
		if !errors.Is(err, ErrDeliveriesClosed) {
			t.Fatalf("Run = %v, want ErrDeliveriesClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return")
	}
	h.cancel = nil // already stopped
	// No ack is possible on a dead channel; the broker requeues instead.
	if acks, _, _ := h.acker.snapshot(); len(acks) != 0 {
		t.Fatalf("unexpected acks %+v", acks)
	}
}
