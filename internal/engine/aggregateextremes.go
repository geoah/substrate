package engine

// Where a downsample's extremes were taken (aggregateread.go). A column
// served from raw points answers its min's and max's instant from the point
// itself; a rollup row keeps its extremes' digits and not their instants, so
// a column served from rows keeps, for each extreme, the rows holding it and
// the cut ends' points that do, and each row's records are derived to find
// its point holding the extreme. Of equal extremes the earliest point answers, as the
// raw path's does. The instants are an addition to the values, so the
// look-up runs only where the budget has room for it and never refuses a
// read the values alone fit.

import (
	"fmt"
	"math/big"
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
// bucket, group and stored digits, which sets point once found.
type extremeCand struct {
	point  *observed.Point
	stream int
	row    *extremeRow
}

// extremeRow is a rollup row holding an extreme, and its point count, which
// the look-up is charged.
type extremeRow struct {
	s          *aggStream
	start, end time.Time
	group      obsGroup
	stored     string
	n          int64
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
// its earliest cut end point, and its rows of the earliest bucket that
// holds the number (its rows arrive in time order), so a column of a
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
// candidates' points, nil where a candidate row's point is not known (the
// look-up did not run, or found none), since an equal extreme earlier than
// every known one may lie in it.
func (x *aggExtreme) at() *time.Time {
	if x == nil {
		return nil
	}
	var best *extremeCand
	for _, c := range x.cands {
		if c.point == nil {
			return nil
		}
		if best == nil || pointBefore(*c.point, c.stream, *best.point, best.stream) {
			best = c
		}
	}
	if best == nil {
		return nil
	}
	t := best.point.At
	return &t
}

// extremeInstants looks up the point each candidate row of the groups'
// rollup-served cells holds its extreme at, deriving the records of the
// row's bucket. The look-up is charged the rows' points against maxSamples;
// where they would pass what the read has left of it, it does not run and
// those extremes answer no instant.
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
	if len(rows) == 0 || r.samples+n > r.p.maxSamples {
		return nil
	}
	r.samples += n
	for _, c := range rows {
		if err := r.canceled(); err != nil {
			return err
		}
		x := c.row
		pts, err := r.store.points(x.s.info(), x.s.form, x.start, x.end)
		if err != nil {
			return fmt.Errorf("substrate/engine: aggregate extremes: %w", err)
		}
		want, ok := new(big.Rat).SetString(x.stored)
		if !ok {
			continue
		}
		key := x.group.key()
		for i := range pts {
			p := pts[i]
			if p.Group != key || !p.HasValue || p.Unavailable {
				continue
			}
			if v, ok := new(big.Rat).SetString(p.Exact); ok && v.Cmp(want) == 0 {
				c.point = &observed.Point{At: p.At, RecordID: p.RecordID, Ord: p.Ord}
				break
			}
		}
	}
	return nil
}

// rowCand is a candidate rollup row of stream s holding an extreme, its
// stored digits taken from the row before conversion.
func rowCand(s *aggStream, stream int, row observed.Row, g obsGroup) func(isMax bool) *extremeCand {
	return func(isMax bool) *extremeCand {
		stored := row.MinExact
		if isMax {
			stored = row.MaxExact
		}
		return &extremeCand{stream: stream, row: &extremeRow{s: s, start: row.Start, end: row.End, group: g, stored: stored, n: row.N}}
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
