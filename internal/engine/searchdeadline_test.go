package engine

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// A statement's error counts as its deadline only when it is that deadline's
// own: the context's error, or the server's query_canceled, while the
// deadline is done and the context it derives from is not.
func TestDeadlineErrTakesOnlyTheDeadlinesOwnError(t *testing.T) {
	parent := context.Background()
	expired, cancel := context.WithDeadline(parent, time.Now().Add(-time.Second))
	defer cancel()
	live, cancelLive := context.WithTimeout(parent, time.Hour)
	defer cancelLive()
	gone, cancelGone := context.WithCancel(parent)
	cancelGone()
	underGone, cancelUnder := context.WithDeadline(gone, time.Now().Add(-time.Second))
	defer cancelUnder()

	canceled := &pgconn.PgError{Code: sqlstateQueryCanceled}
	for _, tc := range []struct {
		name     string
		err      error
		deadline context.Context
		parent   context.Context
		want     bool
	}{
		{"the deadline's own error", fmt.Errorf("read: %w", context.DeadlineExceeded), expired, parent, true},
		{"query_canceled past the deadline", fmt.Errorf("read: %w", canceled), expired, parent, true},
		{"a SQL error past the deadline", &pgconn.PgError{Code: "22012"}, expired, parent, false},
		{"a closed pool past the deadline", sql.ErrConnDone, expired, parent, false},
		{"query_canceled before the deadline", canceled, live, parent, false},
		{"the deadline's error with the caller gone", context.DeadlineExceeded, underGone, gone, false},
		{"no error", nil, expired, parent, false},
	} {
		if got := deadlineErr(tc.err, tc.deadline, tc.parent); got != tc.want {
			t.Errorf("%s: deadlineErr = %v, want %v", tc.name, got, tc.want)
		}
	}
}
