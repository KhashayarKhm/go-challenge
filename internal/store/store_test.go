package store

import (
	"testing"
	"time"
)

func TestDayOf(t *testing.T) {
	tehran := time.FixedZone("IRST", 3*3600+1800)
	tests := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{"utc midday", time.Date(2026, 9, 5, 12, 30, 0, 0, time.UTC), time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)},
		{"utc midnight", time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)},
		// 02:00 in Tehran is still the previous day in UTC.
		{"converted to utc", time.Date(2026, 9, 5, 2, 0, 0, 0, tehran), time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DayOf(tt.in); !got.Equal(tt.want) || got.Location() != time.UTC {
				t.Fatalf("DayOf(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
