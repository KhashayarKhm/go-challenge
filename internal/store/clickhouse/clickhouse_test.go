//go:build integration

package clickhouse

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/KhashayarKhm/go-challenge/internal/store"
)

// Integration test against a real ClickHouse (see docker-compose.yml).
//
// Built only with -tags integration (`make test-integration`). The tester
// prepares the environment: CLICKHOUSE_DSN (from .env.test.local) must point at
// a database migrated with cmd/migrate, e.g. es_test (see README). The
// test empties segment_users when it finishes.
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
	t.Cleanup(func() {
		if err := s.conn.Exec(context.Background(), "TRUNCATE TABLE segment_users"); err != nil {
			t.Errorf("clean up segment_users: %v", err)
		}
		s.Close()
	})

	today := store.DayOf(time.Now())
	rows := []store.Membership{
		{UserID: "u1", Segment: "sports", Day: today},
		{UserID: "u1", Segment: "sports", Day: today},                   // same-day duplicate
		{UserID: "u1", Segment: "sports", Day: today.AddDate(0, 0, -3)}, // same user, other day
		{UserID: "u2", Segment: "sports", Day: today.AddDate(0, 0, -13)},
		{UserID: "u3", Segment: "sports", Day: today.AddDate(0, 0, -20)}, // outside the window
		{UserID: "u4", Segment: "news", Day: today},
	}
	if err := s.Insert(ctx, rows); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// Re-inserting the same batch (a redelivery) must not change the answer.
	if err := s.Insert(ctx, rows); err != nil {
		t.Fatalf("Insert (redelivery): %v", err)
	}

	got, err := s.CountUsers(ctx, "sports", today.AddDate(0, 0, -13))
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if got != 2 {
		t.Fatalf("CountUsers = %d, want 2 (u1, u2)", got)
	}
}
