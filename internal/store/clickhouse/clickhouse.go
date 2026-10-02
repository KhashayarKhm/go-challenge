// Package clickhouse is the ClickHouse adapter of store.Writer and store.Reader.
//
// ClickHouse is chosen because "how many distinct users in a segment over a
// time range" is an analytical (OLAP) query over a large, append-only dataset:
// a column store reads only the needed columns, sorted by (segment, day).
package clickhouse

import (
	"context"
	"fmt"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/KhashayarKhm/go-challenge/internal/store"
)

const (
	insertQuery = "INSERT INTO segment_users (segment, day, user_id)"
	// uniq is an adaptive, fixed-memory distinct count: exact for small sets
	// and within ~1-2% for multi-million-user segments. Counting distinct users
	// is also what makes duplicates (redeliveries, unmerged parts, multiple days)
	// harmless.
	countQuery = "SELECT uniq(user_id) FROM segment_users WHERE segment = ? AND day >= ?"
)

// Store implements store.Writer and store.Reader on a ClickHouse connection.
type Store struct {
	conn driver.Conn
}

var (
	_ store.Writer = (*Store)(nil)
	_ store.Reader = (*Store)(nil)
)

// New wraps an existing connection.
func New(conn driver.Conn) *Store {
	return &Store{conn: conn}
}

// Open connects using a DSN such as clickhouse://user:pass@host:9000/db and
// verifies the connection.
func Open(ctx context.Context, dsn string) (*Store, error) {
	opts, err := ch.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: parse dsn: %w", err)
	}
	conn, err := ch.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: open: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("clickhouse: ping: %w", err)
	}
	return New(conn), nil
}

// Insert writes all memberships as a single batch. One large insert per flush
// keeps the number of parts low; ClickHouse degrades with many small inserts.
func (s *Store) Insert(ctx context.Context, memberships []store.Membership) error {
	if len(memberships) == 0 {
		return nil
	}
	batch, err := s.conn.PrepareBatch(ctx, insertQuery)
	if err != nil {
		return fmt.Errorf("clickhouse: prepare batch: %w", err)
	}
	for _, m := range memberships {
		if err := batch.Append(m.Segment, m.Day, m.UserID); err != nil {
			_ = batch.Abort()
			return fmt.Errorf("clickhouse: append: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("clickhouse: send batch: %w", err)
	}
	return nil
}

// CountUsers counts distinct users of segment tagged on any day >= since.
func (s *Store) CountUsers(ctx context.Context, segment string, since time.Time) (uint64, error) {
	var n uint64
	if err := s.conn.QueryRow(ctx, countQuery, segment, since).Scan(&n); err != nil {
		return 0, fmt.Errorf("clickhouse: count users: %w", err)
	}
	return n, nil
}

// Close closes the connection.
func (s *Store) Close() error {
	return s.conn.Close()
}
