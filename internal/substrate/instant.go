package substrate

import (
	"fmt"
	"time"
)

// instantLayouts are the forms an instant value may take on the wire: RFC 3339
// with or without fractional seconds, a zone-less date-time and a bare date.
// The last two read as UTC.
var instantLayouts = []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"}

// ParseInstant reads an instant value the way every filter and write does,
// and holds it to the range CheckInstantRange admits. The plain list's `at`
// filter and the window read's bounds both parse through it, so a bound one
// accepts the other accepts too.
func ParseInstant(s string) (time.Time, error) {
	for _, layout := range instantLayouts {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts, CheckInstantRange(ts)
		}
	}
	return time.Time{}, fmt.Errorf("expected an RFC 3339 instant")
}

// pgMinYear and pgMaxYear bound the instants Postgres stores, 4713 BC to
// 294276 AD, in Go's astronomical numbering (year 0 is 1 BC, so 4713 BC is
// -4712).
const (
	pgMinYear = -4712
	pgMaxYear = 294276
)

// CheckInstantRange holds a parsed instant to what `timestamptz` accepts. Go's
// parsers take instants Postgres cannot store, and the value persists as
// written because the columns are jsonb. Both read paths CAST it
// (condJSON's range filters, orderExpr's ordering), so a single stored instant
// outside the range fails the whole collection listing rather than its own row.
// Year 0 is the reachable case: `time.Parse` takes "0000-01-01T00:00:00Z", the
// numeric bound admits it, and Postgres has no year zero at all.
func CheckInstantRange(ts time.Time) error {
	switch y := ts.Year(); {
	case y == 0:
		return fmt.Errorf("year 0000 is not an instant Postgres stores: there is no year zero, and the range is 4713 BC to 294276 AD")
	case y < pgMinYear || y > pgMaxYear:
		return fmt.Errorf("year %d is outside what Postgres stores (4713 BC to 294276 AD)", y)
	}
	return nil
}
