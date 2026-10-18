package engine

import (
	"regexp"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The UTC offsets a recorded date may be lived at: a day at +14:00 starts
// 14 hours before UTC midnight, and one at -12:00 ends 12 hours after it.
const (
	earliestOffset = 14 * time.Hour
	latestOffset   = 12 * time.Hour
)

// localDate is a range bound's date: a bare date or a midnight instant, its
// date as written.
var localDate = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})(?:T00:00(?::00(?:\.0+)?)?(?:Z|[+-]\d{2}:\d{2}))?$`)

// localDates is a range of whole recorded-local dates, [from, to), each
// `YYYY-MM-DD`.
type localDates struct{ from, to string }

// excludes reports whether a recorded-local day starting at the wall-clock
// time start lies outside the dates; false on a read with none.
func (d *localDates) excludes(start time.Time) bool {
	if d == nil {
		return false
	}
	day := start.Format(time.DateOnly)
	return day < d.from || day >= d.to
}

// keep answers the rows whose own start names a date in the range. A
// recorded-local row's start carries the offset it was lived at, so its
// date is the one at that offset, never the instant's in UTC.
func (d *localDates) keep(rows []substrate.AggregateRow) []substrate.AggregateRow {
	out := rows[:0]
	for _, r := range rows {
		if day := r.Start.Format(time.DateOnly); day >= d.from && day < d.to {
			out = append(out, r)
		}
	}
	return out
}

// recordedLocalDates reads a range of whole recorded-local dates
// (range.recordedLocal) as the instants that hold every such day, from the
// first date's start at the earliest offset to the last date's end at the
// latest, and answers the dates the rows are then held to. A day lived at
// another offset than the reader's starts and ends at other instants, so
// no one pair of instants is the reader's dates. The days those instants
// reach outside the dates are reduced and never answered or counted
// against a budget (aggBucket.outside), and neither are the groups and
// series whose values lie only on those days (aggPlan.padded). A range of instants comes back as it
// is, with no dates.
func recordedLocalDates(q substrate.AggregateQuery) (substrate.AggregateQuery, *localDates, error) {
	a := q.Aggregate
	if a == nil || a.Range == nil || !a.Range.RecordedLocal {
		return q, nil, nil
	}
	b := a.Bucket
	if b == nil || b.Calendar != "day" || b.Timezone != "" || b.Every != "" || b.Records != nil || a.Latest || q.Downsample != nil {
		return q, nil, refuse("aggregate.range.recordedLocal", "a range of recorded-local dates is a recorded-local day read's: bucket calendar day with no timezone")
	}
	bound := func(at, text string) (string, time.Time, error) {
		m := localDate.FindStringSubmatch(text)
		if m == nil {
			return "", time.Time{}, refuse(at, "%q is not a date: a recorded-local range names dates, 2026-10-01, or midnights", text)
		}
		day, err := time.Parse(time.DateOnly, m[1])
		if err != nil {
			return "", time.Time{}, refuse(at, "%q is not a date: %v", text, err)
		}
		return m[1], day, nil
	}
	from, fromDay, err := bound("aggregate.range.gte", a.Range.Gte)
	if err != nil {
		return q, nil, err
	}
	to, toDay, err := bound("aggregate.range.lt", a.Range.Lt)
	if err != nil {
		return q, nil, err
	}
	if !fromDay.Before(toDay) {
		return q, nil, refuse("aggregate.range", "gte %s is not before lt %s", from, to)
	}
	widened := *a
	widened.Range = &substrate.AggregateRange{
		Gte: fromDay.Add(-earliestOffset).Format(time.RFC3339),
		Lt:  toDay.Add(latestOffset).Format(time.RFC3339),
	}
	q.Aggregate = &widened
	return q, &localDates{from: from, to: to}, nil
}
