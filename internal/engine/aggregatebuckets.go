package engine

// The aggregate read's buckets (aggregate.go): the four time modes and the
// bucket a request with none reads, the whole range.
//
//   - every: windows of one fixed length counted from an origin, on UTC
//     instants.
//   - calendar with a timezone: calendar units in that zone, so a day across
//     a clock change is 23 or 25 hours.
//   - calendar alone: the recorded-local calendar. Each point is placed by its
//     own UTC offset, so a bucket is a span of wall clock and its UTC extent
//     depends on the offsets of the points in it.
//   - records: each matched record's [at, endsAt), widened by `around`.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// bucketWidth is a rollup row's width.
const bucketWidth = observed.BucketWidth

// The bucket modes.
const (
	bucketRange    = "range"
	bucketEvery    = "every"
	bucketCalendar = "calendar"
	bucketLocal    = "local"
	bucketRecords  = "records"
)

var calendarUnits = []string{"minute", "hour", "day", "week", "month", "year"}

// bucketSpec is a request's bucket, parsed.
type bucketSpec struct {
	mode      string
	every     time.Duration
	origin    time.Time
	calendar  string
	loc       *time.Location
	weekStart time.Weekday
	offset    time.Duration
	// records mode
	records       *substrate.AggregateRecords
	before, after time.Duration
	maxRecords    int
	// echo is the bucket as the response states it.
	echo *substrate.AggregateBucket
}

// aggBucket is one output bucket.
type aggBucket struct {
	// start and end bound the bucket's points: UTC instants, clipped to the
	// range; on a recorded-local bucket, the wall-clock bounds written as UTC.
	start, end time.Time
	local      bool
	// zone is where start and end are written.
	zone *time.Location
	// record is the records mode's matched record.
	record *substrate.AggregateRecord
	// first and last are a recorded-local bucket's first and last points,
	// whose offsets its start and end are written in (mark).
	first, last *edge
}

// edge is one end of a recorded-local bucket: the place of the point that
// opens or closes it, in the order every reader takes points in, and the
// point's offset in minutes.
type edge struct {
	at     time.Time
	rid    string
	ord    int64
	stream int
	off    int64
}

func (e *edge) point() observed.Point { return observed.Point{At: e.at, RecordID: e.rid, Ord: e.ord} }

// mark takes one point of a recorded-local bucket, a raw point or a rollup
// row's first or last, and keeps the bucket's first and last by pointBefore,
// so neither the order points and rows arrive in nor the quarter hour a row
// starts decides which offset the bucket's start and end are written in.
func (b *aggBucket) mark(p observed.Point, stream int, off int64) {
	e := &edge{at: p.At, rid: p.RecordID, ord: p.Ord, stream: stream, off: off}
	if b.first == nil || pointBefore(p, stream, b.first.point(), b.first.stream) {
		b.first = e
	}
	if b.last == nil || pointBefore(b.last.point(), b.last.stream, p, stream) {
		b.last = e
	}
}

// pointBefore is observed.Before with the stream last: two streams of one
// group may hold one record id at one instant (two kinds).
func pointBefore(a observed.Point, sa int, b observed.Point, sb int) bool {
	if observed.Before(a, b) {
		return true
	}
	if observed.Before(b, a) {
		return false
	}
	return sa < sb
}

// length is the time the row reports, the denominator of a coverage
// fraction: on a recorded-local bucket from its first point's offset to its
// last's, which is 31 hours on a day flown west, not 24.
func (b *aggBucket) length(rangeFrom, rangeTo time.Time) time.Duration {
	start, end := b.shown(rangeFrom, rangeTo)
	return end.Sub(start)
}

// aligned reports whether the bucket is a run of whole rollup rows.
func (b *aggBucket) aligned() bool {
	if b.local {
		return true
	}
	return b.start.Truncate(bucketWidth).Equal(b.start) && b.end.Truncate(bucketWidth).Equal(b.end)
}

// shown is the row's start and end as the response writes them.
func (b *aggBucket) shown(rangeFrom, rangeTo time.Time) (time.Time, time.Time) {
	if !b.local {
		return b.start.In(b.zone), b.end.In(b.zone)
	}
	startOff, endOff := offsetMinutes(rangeFrom), offsetMinutes(rangeTo)
	if b.first != nil {
		startOff = b.first.off
	}
	if b.last != nil {
		endOff = b.last.off
	}
	return wallAt(b.start, startOff), wallAt(b.end, endOff)
}

// wallAt writes a wall-clock time (encoded as UTC) at an offset.
func wallAt(wall time.Time, offset int64) time.Time {
	zone := time.FixedZone("", int(offset)*60)
	return time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), zone)
}

// wallOf is the wall clock of an instant at an offset, encoded as UTC.
func wallOf(t time.Time, offset int64) time.Time {
	return t.UTC().Add(time.Duration(offset) * time.Minute)
}

func offsetMinutes(t time.Time) int64 {
	_, off := t.Zone()
	return int64(off / 60)
}

// parseBucket reads the request's bucket into p.bucket.
func (p *aggPlan) parseBucket() error {
	b := p.a.Bucket
	spec := bucketSpec{weekStart: time.Monday, loc: p.from.Location()}
	if b == nil {
		spec.mode = bucketRange
		p.bucket = spec
		return nil
	}
	echo := *b
	spec.echo = &echo
	modes := 0
	for _, set := range []bool{b.Every != "", b.Calendar != "", b.Records != nil} {
		if set {
			modes++
		}
	}
	if modes != 1 {
		return refuse("aggregate.bucket", "name one time mode: every, calendar or records")
	}
	switch {
	case b.Every != "":
		spec.mode = bucketEvery
		d, err := vocabulary.ParseISODuration(b.Every)
		if err != nil {
			return refuse("aggregate.bucket.every", "%v", err)
		}
		if d <= 0 {
			return refuse("aggregate.bucket.every", "a bucket is longer than zero")
		}
		spec.every = d
		spec.origin = time.Unix(0, 0).UTC()
		if b.Origin != "" {
			o, err := time.Parse(time.RFC3339Nano, b.Origin)
			if err != nil {
				return refuse("aggregate.bucket.origin", "%q is not an RFC 3339 instant", b.Origin)
			}
			spec.origin = o
		}
		if b.Timezone != "" || b.WeekStart != "" || b.Offset != "" {
			return refuse("aggregate.bucket", "timezone, weekStart and offset are the calendar mode's; every counts fixed lengths from its origin")
		}
	case b.Calendar != "":
		if !slices.Contains(calendarUnits, b.Calendar) {
			return refuse("aggregate.bucket.calendar", "%q is not a calendar unit; it is one of %s", b.Calendar, strings.Join(calendarUnits, ", "))
		}
		spec.calendar = b.Calendar
		spec.mode = bucketLocal
		spec.loc = time.UTC
		if b.Timezone != "" {
			loc, err := time.LoadLocation(b.Timezone)
			if err != nil {
				return refuse("aggregate.bucket.timezone", "%q is not a time zone the server knows", b.Timezone)
			}
			spec.mode, spec.loc = bucketCalendar, loc
		}
		if b.Origin != "" {
			return refuse("aggregate.bucket.origin", "an origin is the every mode's; a calendar bucket starts where the calendar does")
		}
		if b.WeekStart != "" {
			wd, ok := weekdays[strings.ToLower(b.WeekStart)]
			if !ok {
				return refuse("aggregate.bucket.weekStart", "%q is not a day of the week", b.WeekStart)
			}
			spec.weekStart = wd
			echo.WeekStart = strings.ToLower(b.WeekStart)
		} else if b.Calendar == "week" {
			echo.WeekStart = "monday"
		}
		if b.Offset != "" {
			d, err := vocabulary.ParseISODuration(b.Offset)
			if err != nil {
				return refuse("aggregate.bucket.offset", "%v", err)
			}
			if d < 0 || d >= 24*time.Hour {
				return refuse("aggregate.bucket.offset", "an offset shifts a bucket's start by less than a day")
			}
			if d%time.Minute != 0 {
				return refuse("aggregate.bucket.offset", "an offset is whole minutes")
			}
			if b.Calendar == "minute" || b.Calendar == "hour" {
				return refuse("aggregate.bucket.offset", "an offset shifts a day's start; a %s bucket has none to shift", b.Calendar)
			}
			spec.offset = d
		}
	default:
		spec.mode = bucketRecords
		spec.records = b.Records
		if b.Timezone != "" || b.WeekStart != "" || b.Offset != "" || b.Origin != "" {
			return refuse("aggregate.bucket", "the records mode's windows are the records'; timezone, weekStart, offset and origin are not its")
		}
		if a := b.Records.Around; a != nil {
			for _, x := range []struct {
				raw  string
				path string
				dst  *time.Duration
			}{{a.Before, "before", &spec.before}, {a.After, "after", &spec.after}} {
				if x.raw == "" {
					continue
				}
				d, err := vocabulary.ParseISODuration(x.raw)
				if err != nil || d < 0 {
					return refuse("aggregate.bucket.records.around."+x.path, "a widening is a duration of zero or more, such as PT30M")
				}
				*x.dst = d
			}
		}
		spec.maxRecords = substrate.AggregateMaxRecords
		if n := b.Records.MaxRecords; n != 0 {
			if n < 0 || n > substrate.AggregateMaxRecordsCap {
				return refuse("aggregate.bucket.records.maxRecords", "%d is over the cap of %d", n, substrate.AggregateMaxRecordsCap)
			}
			spec.maxRecords = n
		}
	}
	spec.echo = &echo
	p.bucket = spec
	return nil
}

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
}

// buckets enumerates the output buckets, refusing past maxBuckets before it
// builds them. The records mode reads its records here.
func (ds *dataset) buckets(ctx context.Context, tx *sql.Tx, p *aggPlan) ([]*aggBucket, error) {
	s := p.bucket
	limit := p.maxBuckets
	tooMany := func(n int) error {
		return refuse("aggregate.budget.maxBuckets", "the range holds more than %d buckets (at least %d); widen the bucket, narrow the range or raise maxBuckets (the cap is %d)",
			limit, n, substrate.AggregateMaxBucketsCap)
	}
	switch s.mode {
	case bucketRange:
		return []*aggBucket{{start: p.from, end: p.to, zone: p.from.Location()}}, nil
	case bucketEvery:
		k0 := floorDiv(p.from.Sub(s.origin), s.every)
		k1 := floorDiv(p.to.Sub(s.origin)-1, s.every)
		if n := k1 - k0 + 1; n > int64(limit) {
			return nil, tooMany(int(min(n, 1<<30)))
		}
		var out []*aggBucket
		for k := k0; k <= k1; k++ {
			start := s.origin.Add(time.Duration(k) * s.every)
			out = append(out, clipBucket(start, start.Add(s.every), p, p.from.Location()))
		}
		return out, nil
	case bucketCalendar:
		var out []*aggBucket
		for start := calStart(p.from, s); start.Before(p.to); start = calNext(start, s) {
			if len(out) >= limit {
				return nil, tooMany(len(out) + 1)
			}
			out = append(out, clipBucket(start, calNext(start, s), p, s.loc))
		}
		return out, nil
	case bucketLocal:
		// The wall clock the range's own offsets name: a range written at
		// +03:00 is the local days it says.
		from, to := wallOf(p.from, offsetMinutes(p.from)), wallOf(p.to, offsetMinutes(p.to))
		var out []*aggBucket
		for start := calStart(from, s); start.Before(to); start = calNext(start, s) {
			if len(out) >= limit {
				return nil, tooMany(len(out) + 1)
			}
			out = append(out, &aggBucket{start: start, end: calNext(start, s), local: true, zone: time.UTC})
		}
		return out, nil
	case bucketRecords:
		return ds.recordBuckets(ctx, tx, p)
	}
	return nil, fmt.Errorf("substrate/engine: unknown bucket mode %q", s.mode)
}

func clipBucket(start, end time.Time, p *aggPlan, zone *time.Location) *aggBucket {
	if start.Before(p.from) {
		start = p.from
	}
	if end.After(p.to) {
		end = p.to
	}
	return &aggBucket{start: start.UTC(), end: end.UTC(), zone: zone}
}

func floorDiv(d, w time.Duration) int64 {
	q := int64(d / w)
	if d%w != 0 && d < 0 {
		q--
	}
	return q
}

// localBucketAt is the recorded-local bucket holding a wall-clock time,
// created where the range's own enumeration did not reach (a point whose
// offset puts it on a day the range's offsets do not name).
func localBucketAt(buckets []*aggBucket, wall time.Time, s bucketSpec) ([]*aggBucket, *aggBucket) {
	start := calStart(wall, s)
	i, found := slices.BinarySearchFunc(buckets, start, func(b *aggBucket, t time.Time) int { return b.start.Compare(t) })
	if found {
		return buckets, buckets[i]
	}
	b := &aggBucket{start: start, end: calNext(start, s), local: true, zone: time.UTC}
	return slices.Insert(buckets, i, b), b
}

// calStart is the start of the calendar bucket holding t, in the spec's zone
// (UTC for a wall clock), with the spec's week start and day offset.
func calStart(t time.Time, s bucketSpec) time.Time {
	w := t.In(s.loc)
	off := int(s.offset / time.Second)
	var start time.Time
	switch s.calendar {
	case "minute", "hour":
		// The wall clock's minute or hour; across a repeated hour time.Date
		// names the first, so step on to the one holding t.
		step := time.Minute
		start = time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), 0, 0, s.loc)
		if s.calendar == "hour" {
			step = time.Hour
			start = time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), 0, 0, 0, s.loc)
		}
		for !start.Add(step).After(t) {
			start = start.Add(step)
		}
		return start
	case "day":
		start = time.Date(w.Year(), w.Month(), w.Day(), 0, 0, off, 0, s.loc)
	case "week":
		back := (int(w.Weekday()) - int(s.weekStart) + 7) % 7
		start = time.Date(w.Year(), w.Month(), w.Day()-back, 0, 0, off, 0, s.loc)
	case "month":
		start = time.Date(w.Year(), w.Month(), 1, 0, 0, off, 0, s.loc)
	case "year":
		start = time.Date(w.Year(), 1, 1, 0, 0, off, 0, s.loc)
	}
	if start.After(t) {
		start = calPrev(start, s)
	}
	return start
}

// calBase is a bucket start's calendar date, its day offset removed.
func calBase(start time.Time, s bucketSpec) time.Time {
	w := start.In(s.loc)
	return time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second()-int(s.offset/time.Second), 0, s.loc)
}

func calStep(start time.Time, s bucketSpec, n int) time.Time {
	switch s.calendar {
	case "minute":
		return start.Add(time.Duration(n) * time.Minute)
	case "hour":
		return start.Add(time.Duration(n) * time.Hour)
	}
	b := calBase(start, s)
	off := int(s.offset / time.Second)
	switch s.calendar {
	case "day":
		return time.Date(b.Year(), b.Month(), b.Day()+n, 0, 0, off, 0, s.loc)
	case "week":
		return time.Date(b.Year(), b.Month(), b.Day()+7*n, 0, 0, off, 0, s.loc)
	case "month":
		return time.Date(b.Year(), b.Month()+time.Month(n), 1, 0, 0, off, 0, s.loc)
	default:
		return time.Date(b.Year()+n, 1, 1, 0, 0, off, 0, s.loc)
	}
}

func calNext(start time.Time, s bucketSpec) time.Time { return calStep(start, s, 1) }
func calPrev(start time.Time, s bucketSpec) time.Time { return calStep(start, s, -1) }

// recordBuckets reads the records time mode's records: those the filter
// matches whose `at` lies in the range, each one bucket.
func (ds *dataset) recordBuckets(ctx context.Context, tx *sql.Tx, p *aggPlan) ([]*aggBucket, error) {
	s := p.bucket
	b := &builder{overRecords: true}
	if _, err := ds.buildFilter(ctx, tx, b, s.records.Filter); err != nil {
		return nil, prefixValidation("aggregate.bucket.records.filter", err)
	}
	if err := planWithValues(ctx, tx, b); err != nil {
		return nil, err
	}
	b.add(`at >= ` + b.arg(p.from) + ` AND at < ` + b.arg(p.to))
	limit := min(s.maxRecords, p.maxBuckets)
	rows, err := tx.QueryContext(ctx, `SELECT kind, id, version, at, ends_at FROM records WHERE `+strings.Join(b.where, " AND ")+
		` ORDER BY at, kind, id LIMIT `+b.arg(limit+1), b.args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*aggBucket
	// The limit counts the records the filter matches, the ones whose window
	// is empty (no endsAt, no widening) included: counting only the windows
	// kept let empty ones use up the rows the LIMIT read and end the read
	// short without a word.
	matched := 0
	for rows.Next() {
		var kind, id string
		var version int64
		var at time.Time
		var endsAt sql.NullTime
		if err := rows.Scan(&kind, &id, &version, &at, &endsAt); err != nil {
			return nil, err
		}
		if matched++; matched > limit {
			if limit == s.maxRecords {
				return nil, refuse("aggregate.bucket.records.maxRecords", "the filter matches more than %d records in the range; narrow it or raise maxRecords (the cap is %d)",
					limit, substrate.AggregateMaxRecordsCap)
			}
			return nil, refuse("aggregate.budget.maxBuckets", "the filter matches more than %d records in the range, one bucket each; narrow it or raise maxBuckets", limit)
		}
		end := at
		if endsAt.Valid {
			end = endsAt.Time
		}
		start, stop := at.Add(-s.before), end.Add(s.after)
		if !stop.After(start) {
			continue
		}
		out = append(out, &aggBucket{
			start: start.UTC(), end: stop.UTC(), zone: p.from.Location(),
			record: &substrate.AggregateRecord{Ref: kind + "/" + id, Version: version},
		})
	}
	return out, rows.Err()
}

// prefixValidation names where a validation refusal from a nested filter came
// from.
func prefixValidation(path string, err error) error {
	var ve *substrate.ValidationError
	if errors.As(err, &ve) {
		out := make([]string, len(ve.Problems))
		for i, pr := range ve.Problems {
			out[i] = path + ": " + pr
		}
		return &substrate.ValidationError{Problems: out}
	}
	if errors.Is(err, substrate.ErrValidation) {
		return fmt.Errorf("%w: %s: %s", substrate.ErrValidation, path, strings.TrimPrefix(err.Error(), substrate.ErrValidation.Error()+": "))
	}
	return err
}
