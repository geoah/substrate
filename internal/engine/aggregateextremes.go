package engine

// Where a downsample's extremes were taken (aggregateread.go). A column
// served from raw points answers its min's and max's instant from the point
// itself; a rollup row keeps its extremes' digits and not their instants, so
// a column served from rows keeps, for each extreme, the rows holding it and
// the cut ends' points that do, and one statement finds each row's point
// holding the extreme. Of equal extremes the earliest point answers, as the
// raw path's does.

import (
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/observed"
)

// aggExtreme is a rollup-served column's min or max: the number in the
// read's unit, and the candidates holding it.
type aggExtreme struct {
	value  float64
	digits string
	cands  []*extremeCand
}

// extremeCand is a place holding a column's extreme: a cut end's raw point
// (point set), or a rollup row the read looks the point up in by its stream,
// quarter hour, group and stored digits, which sets point once found.
type extremeCand struct {
	point  *observed.Point
	stream int
	row    *extremeRow
}

// extremeRow is a rollup row holding an extreme, and its point count, which
// the look-up is charged.
type extremeRow struct {
	s      *aggStream
	start  time.Time
	group  obsGroup
	stored string
	n      int64
}

// beats reports whether a is a better extreme than b: smaller for a min,
// larger for a max, compared exactly (observed.NumberLess).
func beats(isMax bool, a float64, ad string, b float64, bd string) bool {
	if isMax {
		return observed.NumberLess(b, bd, a, ad)
	}
	return observed.NumberLess(a, ad, b, bd)
}

// offerExtremes offers a downsample cell the extremes of statistics s, taken
// in the read's unit, held by candidates the place makes for each (0 the
// min, 1 the max).
func (r *aggRun) offerExtremes(ev *aggEvidence, s observed.Stats, place func(isMax bool) *extremeCand) {
	if r.p.kind != aggDownsample || s.Values == 0 {
		return
	}
	for i, isMax := range []bool{false, true} {
		value, digits := s.Min, s.MinExact
		if isMax {
			value, digits = s.Max, s.MaxExact
		}
		if c := place(isMax); c != nil {
			ev.extremes[i] = ev.extremes[i].offer(isMax, value, digits, c)
		}
	}
}

// offer takes a candidate for the extreme: a better number replaces every
// candidate, an equal one joins them, a worse one is dropped. A stream keeps
// its earliest cut end point, and its rows of the earliest quarter hour
// that holds the number (its rows arrive in time order), so a column of a
// constant stream looks up one row per stream, not every row.
func (x *aggExtreme) offer(isMax bool, value float64, digits string, c *extremeCand) *aggExtreme {
	switch {
	case x == nil || beats(isMax, value, digits, x.value, x.digits):
		return &aggExtreme{value: value, digits: digits, cands: []*extremeCand{c}}
	case beats(isMax, x.value, x.digits, value, digits):
		return x
	}
	for i, have := range x.cands {
		if have.stream != c.stream || (have.row == nil) != (c.row == nil) {
			continue
		}
		if c.row == nil {
			if observed.Before(*c.point, *have.point) {
				x.cands[i] = c
			}
			return x
		}
		if have.row.start.Before(c.row.start) {
			return x
		}
	}
	x.cands = append(x.cands, c)
	return x
}

// at is the instant the extreme was taken at: the earliest of its
// candidates' points, nil where none was found.
func (x *aggExtreme) at() *time.Time {
	if x == nil {
		return nil
	}
	var best *extremeCand
	for _, c := range x.cands {
		if c.point != nil && (best == nil || pointBefore(*c.point, c.stream, *best.point, best.stream)) {
			best = c
		}
	}
	if best == nil {
		return nil
	}
	t := best.point.At
	return &t
}

// extremeAtSQL finds, for each row ($1 to $13, numbered i), the first
// available point of its stream, form, quarter hour and group whose stored
// digits are the row's extreme, in the order every reader takes points in.
const extremeAtSQL = `
	SELECT c.i, p.ts, p.record_id, p.ord
	FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::timestamptz[],
	            $8::text[], $9::text[], $10::boolean[], $11::bigint[], $12::boolean[], $13::text[])
	     WITH ORDINALITY AS c(metric, kind, source, about, channel, form, a, code, unit, delta, off, known, digits, i)
	CROSS JOIN LATERAL (
		SELECT p.ts, p.record_id, p.ord FROM observed_points p
		WHERE p.metric = c.metric AND p.kind = c.kind AND p.source = c.source AND p.about = c.about AND p.channel = c.channel
		  AND p.form = c.form AND p.ts >= c.a AND p.ts < c.a + interval '15 minutes'
		  AND p.code = c.code AND p.unit = c.unit AND p.delta = c.delta AND p.utc_offset_min = c.off AND p.offset_known = c.known
		  AND p.value_exact = c.digits::numeric
		ORDER BY p.ts, p.record_id, p.ord LIMIT 1) p`

// extremeInstants looks up, in one statement, the point each candidate row
// of the groups' rollup-served cells holds its extreme at. The rows' points
// count against maxSamples first: the statement reads at most them.
func (r *aggRun) extremeInstants(groups []*aggGroup, raws []aggCells) error {
	var rows []*extremeCand
	var n int64
	for gi, g := range groups {
		if !g.rollups {
			continue
		}
		for _, b := range r.buckets {
			for _, code := range sortedKeys(raws[gi][b]) {
				for _, x := range raws[gi][b][code].extremes {
					if x == nil {
						continue
					}
					for _, c := range x.cands {
						if err := r.canceled(); err != nil {
							return err
						}
						if c.row != nil {
							rows, n = append(rows, c), n+c.row.n
						}
					}
				}
			}
		}
	}
	if len(rows) == 0 {
		return nil
	}
	if err := r.charge(n, "raw points of the rollup rows holding the columns' extremes"); err != nil {
		return err
	}
	var metric, kind, source, about, channel, form, code, unit, digits []string
	var starts []time.Time
	var delta, known []bool
	var off []int64
	for _, c := range rows {
		x := c.row
		metric, kind, source = append(metric, x.s.metric), append(kind, x.s.kind), append(source, x.s.source)
		about, channel, form = append(about, x.s.about), append(channel, x.s.channel), append(form, x.s.form)
		starts, code, unit = append(starts, x.start), append(code, x.group.code), append(unit, x.group.unit)
		delta, off, known = append(delta, x.group.delta), append(off, x.group.offset), append(known, !x.group.offsetUnknown)
		digits = append(digits, x.stored)
	}
	args := []any{metric, kind, source, about, channel, form, starts, code, unit, delta, off, known, digits}
	res, err := r.tx.QueryContext(r.ctx, extremeAtSQL, args...)
	if err != nil {
		return fmt.Errorf("substrate/engine: aggregate extremes: %w", err)
	}
	defer func() { _ = res.Close() }()
	for res.Next() {
		var i int
		p := &observed.Point{}
		if err := res.Scan(&i, &p.At, &p.RecordID, &p.Ord); err != nil {
			return err
		}
		rows[i-1].point = p
	}
	return res.Err()
}

// rowCand is a candidate rollup row of stream s holding an extreme, its
// stored digits taken from the row before conversion.
func rowCand(s *aggStream, stream int, row observed.Row, g obsGroup) func(isMax bool) *extremeCand {
	return func(isMax bool) *extremeCand {
		stored := row.MinExact
		if isMax {
			stored = row.MaxExact
		}
		return &extremeCand{stream: stream, row: &extremeRow{s: s, start: row.Start, group: g, stored: stored, n: row.N}}
	}
}

// pointCand is a candidate among a cut end's points of one group holding an
// extreme: the first whose digits are the group's statistics' (taken over
// those points, before conversion).
func pointCand(pts []observed.Point, key string, s observed.Stats, stream int) func(isMax bool) *extremeCand {
	return func(isMax bool) *extremeCand {
		digits := s.MinExact
		if isMax {
			digits = s.MaxExact
		}
		for i := range pts {
			if p := pts[i]; p.Group == key && p.HasValue && !p.Unavailable && p.Exact == digits {
				return &extremeCand{point: &p, stream: stream}
			}
		}
		return nil
	}
}
