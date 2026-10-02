package estimate

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeReader struct {
	segment string
	since   time.Time
	result  uint64
	err     error
	calls   int
}

func (r *fakeReader) CountUsers(_ context.Context, segment string, since time.Time) (uint64, error) {
	r.calls++
	r.segment, r.since = segment, since
	return r.result, r.err
}

func newTestService(r *fakeReader, now time.Time) *Service {
	s := NewService(r)
	s.now = func() time.Time { return now }
	return s
}

func TestEstimateUsesFourteenDayWindow(t *testing.T) {
	r := &fakeReader{result: 42}
	now := time.Date(2026, 9, 16, 23, 59, 0, 0, time.UTC)

	got, err := newTestService(r, now).Estimate(context.Background(), "sports")
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if got != 42 {
		t.Fatalf("Estimate = %d, want 42", got)
	}
	// Sept 3 .. Sept 16 inclusive is 14 days; Sept 2 is out.
	if want := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC); !r.since.Equal(want) {
		t.Fatalf("since = %v, want %v", r.since, want)
	}
	if r.segment != "sports" {
		t.Fatalf("segment = %q", r.segment)
	}
}

func TestEstimateWindowIsComputedInUTC(t *testing.T) {
	r := &fakeReader{}
	// 01:00 on Sept 17 in Tehran is still Sept 16 in UTC.
	now := time.Date(2026, 9, 17, 1, 0, 0, 0, time.FixedZone("IRST", 3*3600+1800))

	if _, err := newTestService(r, now).Estimate(context.Background(), "sports"); err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if want := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC); !r.since.Equal(want) {
		t.Fatalf("since = %v, want %v", r.since, want)
	}
}

func TestEstimateRejectsEmptySegment(t *testing.T) {
	r := &fakeReader{}
	for _, seg := range []string{"", "   "} {
		if _, err := newTestService(r, time.Now()).Estimate(context.Background(), seg); !errors.Is(err, ErrInvalidSegment) {
			t.Fatalf("Estimate(%q) error = %v, want ErrInvalidSegment", seg, err)
		}
	}
	if r.calls != 0 {
		t.Fatal("store must not be queried for invalid input")
	}
}

func TestEstimatePropagatesStoreError(t *testing.T) {
	storeErr := errors.New("clickhouse down")
	_, err := newTestService(&fakeReader{err: storeErr}, time.Now()).Estimate(context.Background(), "sports")
	if !errors.Is(err, storeErr) {
		t.Fatalf("error = %v, want %v", err, storeErr)
	}
}
