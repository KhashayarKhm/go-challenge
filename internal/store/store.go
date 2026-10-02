// Package store defines the persistence ports of ES. Business code depends on
// these interfaces; internal/store/clickhouse is the production adapter.
package store

import (
	"context"
	"time"
)

// Membership is one fact: "UserID was tagged with Segment on Day".
//
// Day is a UTC calendar date (time part zeroed). Storing days instead of exact
// timestamps collapses the many daily taggings of a user into a single row,
// at the cost of up to 24h imprecision at the 2-week boundary.
type Membership struct {
	UserID  string
	Segment string
	Day     time.Time
}

// Writer persists memberships. Insert must be idempotent: the ingestion
// pipeline is at-least-once, so the same membership may be inserted again
// after a crash or a redelivery.
type Writer interface {
	Insert(ctx context.Context, memberships []Membership) error
}

// Reader answers counting queries.
type Reader interface {
	// CountUsers returns the number of distinct users tagged with segment on
	// any day >= since. The window policy (2 weeks) lives in the caller.
	CountUsers(ctx context.Context, segment string, since time.Time) (uint64, error)
}

// DayOf truncates t to its UTC calendar day. It is the single place that
// defines what "a day" means for both ingestion and estimation.
func DayOf(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
