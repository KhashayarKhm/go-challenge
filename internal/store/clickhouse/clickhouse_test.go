package clickhouse

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/KhashayarKhm/go-challenge/internal/store"
)

// Integration test against a real ClickHouse (see docker-compose.yml).
// Run with: CLICKHOUSE_DSN=clickhouse://default:clickhouse@localhost:9000/default go test ./internal/store/clickhouse
// The schema from migrations/001_init.sql must already be applied.
func TestStoreIntegration(t *testing.T) {
	dsn := os.Getenv("CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("CLICKHOUSE_DSN not set; skipping ClickHouse integration test")
	}
	ctx := context.Background()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// A unique segment isolates this run from previous data.
	segment := "it-" + time.Now().Format("20060102150405.000000000")
	today := store.DayOf(time.Now())
	old := today.AddDate(0, 0, -20)

	rows := []store.Membership{
		{UserID: "u1", Segment: segment, Day: today},
		{UserID: "u1", Segment: segment, Day: today},                   // same-day duplicate
		{UserID: "u1", Segment: segment, Day: today.AddDate(0, 0, -3)}, // same user, other day
		{UserID: "u2", Segment: segment, Day: today.AddDate(0, 0, -13)},
		{UserID: "u3", Segment: segment, Day: old}, // outside the window
		{UserID: "u4", Segment: "other-" + segment, Day: today},
	}
	if err := s.Insert(ctx, rows); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// Re-inserting the same batch (a redelivery) must not change the answer.
	if err := s.Insert(ctx, rows); err != nil {
		t.Fatalf("Insert (redelivery): %v", err)
	}

	got, err := s.CountUsers(ctx, segment, today.AddDate(0, 0, -13))
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if got != 2 {
		t.Fatalf("CountUsers = %d, want 2 (u1, u2)", got)
	}
}
