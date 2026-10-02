package ingest

import (
	"testing"
	"time"
)

func TestDefaultConsumerTag(t *testing.T) {
	tehran := time.FixedZone("IRST", 3*3600+1800)
	// 01:10 in Tehran is 21:40 the previous day in UTC.
	got := DefaultConsumerTag(time.Date(2026, 10, 3, 1, 10, 59, 0, tehran))
	if want := "estimation-service-2026-10-02 21:40"; got != want {
		t.Fatalf("DefaultConsumerTag = %q, want %q", got, want)
	}
}
