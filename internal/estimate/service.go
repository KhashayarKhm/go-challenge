// Package estimate implements `func estimate(segment) -> number of users`.
package estimate

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/KhashayarKhm/go-challenge/internal/store"
)

// WindowDays is how long a user stays in a segment after being tagged.
// A user tagged on day D is counted on days D .. D+13 (UTC), i.e. for 14
// calendar days, and re-tagging extends the membership.
const WindowDays = 14

// ErrInvalidSegment is returned for an empty segment name.
var ErrInvalidSegment = errors.New("estimate: segment must not be empty")

// Service owns the business rule (the 2-week window); the store only counts.
type Service struct {
	reader store.Reader
	now    func() time.Time
}

// NewService creates a Service backed by reader.
func NewService(reader store.Reader) *Service {
	return &Service{reader: reader, now: time.Now}
}

// Estimate returns the number of distinct users currently in segment.
//
// "Today" is computed here in UTC instead of with ClickHouse's today(), so the
// day boundary does not depend on the database server's time zone.
func (s *Service) Estimate(ctx context.Context, segment string) (uint64, error) {
	if strings.TrimSpace(segment) == "" {
		return 0, ErrInvalidSegment
	}
	since := store.DayOf(s.now()).AddDate(0, 0, -(WindowDays - 1))
	return s.reader.CountUsers(ctx, segment, since)
}
