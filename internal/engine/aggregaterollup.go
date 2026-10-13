package engine

// The aggregate read's reduction from rollup rows (aggregateread.go): whole
// rows merged after converting each from its own unit, an amount's or a
// counter's cut bucket ends merged from raw points, and a gauge's held time
// walked across the rows from the value carried into each window.

import (
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// evalRollups reduces a group from its rollup rows: whole rows merged after
// converting each from its own unit, an amount's or a counter's cut bucket ends
// from raw points, a gauge's held time walked across rows with its carry.
func (r *aggRun) evalRollups(g *aggGroup) (aggCells, error) {
	out := aggCells{}
	local := r.p.bucket.mode == bucketLocal
	tr := traceOf(r.ctx)
	for si, s := range g.raw {
		rows := r.rows[s]
		for _, row := range rows {
			if err := r.canceled(); err != nil {
				return nil, err
			}
			if tr != nil {
				tr.mergedRows++
			}
			og := r.rowGroups[row.Group]
			// A row of nulls alone is kept for the held time it stops
			// (walkHeld) and merges into no bucket: the raw points make none.
			if row.N == 0 || !r.admits(og.code) {
				continue
			}
			var targets []*aggBucket
			if local {
				// The row lands in the day its quarter hour names at its
				// offset; the day's first and last points are the rows' own.
				targets = r.bucketsOf(row.Start, og.offset)
			} else {
				for _, b := range r.bucketsMeeting(row.Start, row.Start.Add(bucketWidth)) {
					whole, _ := split(b)
					if !row.Start.Before(whole[0]) && row.Start.Before(whole[1]) {
						targets = append(targets, b)
					}
				}
			}
			if len(targets) == 0 {
				continue
			}
			st, err := r.convertStats(row.Stats, og, s.source)
			if err != nil {
				return nil, err
			}
			for _, b := range targets {
				ev := cell(out, b, r.codeKey(og.code), g.form, false)
				if local && row.First != nil {
					for _, pt := range []*observed.Point{row.First, row.Last} {
						b.ends.mark(*pt, si, og.offset)
						ev.ends.mark(*pt, si, og.offset)
					}
				}
				r.mergeStats(ev, st, g.form, si)
				ev.sources[s.source] = true
				switch {
				case g.form == vocabulary.FormAmount && row.First != nil:
					ev.extents = append(ev.extents, rowExtent(row.Stats))
				case g.form == vocabulary.FormCounter:
					ev.coverage += row.CoveredMs
				}
				if st.Increase != nil {
					ev.steps.add(st.Increase, false, row.CoveredMs)
					ev.steps.resets += row.Resets
				}
			}
		}
		if g.form != vocabulary.FormGauge && !local {
			if err := r.rollupFragments(out, g, s, si); err != nil {
				return nil, err
			}
		}
		if g.form == vocabulary.FormGauge {
			for i, w := range r.windows {
				if err := r.walkHeld(out, g, s, si, rows, w, r.carries[s][i]); err != nil {
					return nil, err
				}
			}
		}
	}
	if g.form == vocabulary.FormAmount {
		if err := r.amountCoverage(g, out); err != nil {
			return nil, err
		}
	}
	for _, codes := range out {
		for _, ev := range codes {
			ev.count, ev.bounded = ev.stats.N, ev.stats.Bounded
		}
	}
	return out, nil
}

// convertStats converts one rollup row's statistics from its own unit into
// the read's: an affine map moves the sum by the count, the mean and the
// extremes by the offset, m2 by the factor squared; a counter's increase is a
// difference and takes the factor alone.
func (r *aggRun) convertStats(s observed.Stats, g obsGroup, source string) (observed.Stats, error) {
	if g.unit == "" {
		return s, nil
	}
	c, err := r.conversion(g.unit, g.delta, source)
	if err != nil {
		return s, err
	}
	if c.identity {
		return s, nil
	}
	f, o := c.ff, c.fo
	s.Sum = f*s.Sum + o*float64(s.Values)
	s.Min, s.Max, s.Mean = f*s.Min+o, f*s.Max+o, f*s.Mean+o
	s.M2 *= f * f
	s.TWASum = f*s.TWASum + o*float64(s.HeldMs)
	if s.Increase != nil {
		s.Increase = new(big.Rat).Mul(s.Increase, c.f)
	}
	conv := func(p *observed.Point) *observed.Point {
		if p == nil || !p.HasValue {
			return p
		}
		q := *p
		r.convertWith(&q, c)
		return &q
	}
	s.First, s.Last = conv(s.First), conv(s.Last)
	for _, d := range []*string{&s.MinExact, &s.MaxExact} {
		digits, exact := c.digits(*d)
		if !exact && r.wants(substrate.ReducerMin, substrate.ReducerMax) {
			r.meta.inexact = true
		}
		*d = digits
	}
	return s, nil
}

// wants reports whether the read asks for any of the reducers.
func (r *aggRun) wants(names ...string) bool {
	for _, red := range r.p.reducers {
		if slices.Contains(names, red.Name) {
			return true
		}
	}
	return false
}

// mergeStats merges converted statistics of the group's stream-th stream into
// a bucket's, keeping the first and last point in the order raw points are
// read in (pointBefore: the instant, the record id, the index, the stream),
// whatever order the rows arrive in: two groups of one bucket interleave, and
// two at one instant tie on time alone.
func (r *aggRun) mergeStats(ev *aggEvidence, s observed.Stats, form string, stream int) {
	first, last := ev.stats.First, ev.stats.Last
	ev.stats = observed.Merge(form, ev.stats, s)
	ev.stats.First, ev.stats.Last = first, last
	if s.First != nil && (first == nil || pointBefore(*s.First, stream, *first, ev.firstStream)) {
		ev.stats.First, ev.firstStream = s.First, stream
	}
	if s.Last != nil && (last == nil || pointBefore(*last, ev.lastStream, *s.Last, stream)) {
		ev.stats.Last, ev.lastStream = s.Last, stream
	}
}

// rowExtent is an amount row's coverage as UnionMs joins it.
func rowExtent(s observed.Stats) observed.Extent {
	return observed.Extent{Start: s.First.At, End: s.SpanEnd, Ms: s.CoveredMs}
}

// amountCoveragePoints are the raw spans of a group's amount streams ($1 to
// $5) in one cell c of amountCoverageCells: the points a raw read takes into
// the cell, those whose instant lies in [a, b), on a recorded-local read
// ($12) whose wall clock at their own offset lies in [wa, wb), of the cell's
// code where the read groups by code ($13), and of the admitted codes ($11)
// where the filter names some ($14).
const amountCoveragePoints = `
	SELECT p.ts, p.ts_end AS e
	FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[]) AS s(metric, kind, source, about, channel)
	CROSS JOIN LATERAL (
		SELECT p.ts, p.ts_end FROM observed_points p
		WHERE p.metric = s.metric AND p.kind = s.kind AND p.source = s.source AND p.about = s.about AND p.channel = s.channel
		  AND p.ts >= c.a AND p.ts < c.b AND p.form = 'amount' AND ` + availablePoint + `
		  AND (NOT $12::boolean OR (p.ts + p.utc_offset_min * interval '1 minute' >= c.wa
		                            AND p.ts + p.utc_offset_min * interval '1 minute' < c.wb))
		  AND (NOT $13::boolean OR p.code = c.code)
		  AND (NOT $14::boolean OR p.code = ANY($11::text[]))) p`

// amountCoverageCells are the cells ($6 to $10) whose coverage is taken from
// the raw spans.
const amountCoverageCells = `unnest($6::timestamptz[], $7::timestamptz[], $8::timestamptz[], $9::timestamptz[], $10::text[]) WITH ORDINALITY AS c(a, b, wa, wb, code, i)`

// amountCoverageCountSQL counts the spans amountCoverageSQL reduces, every
// cell's, before it runs: they count against maxSamples like any raw point
// a read takes.
const amountCoverageCountSQL = `
	SELECT coalesce(sum((SELECT count(*) FROM (` + amountCoveragePoints + `) q)), 0)::bigint FROM ` + amountCoverageCells

// amountCoverageSQL is the union of each cell's spans (amountCoveragePoints).
// Spans that overlap or touch join, as joinSpans joins them, and each joined
// span counts in whole milliseconds.
const amountCoverageSQL = `
	SELECT c.i, coalesce((
		SELECT sum(floor(extract(epoch FROM z.e - z.s) * 1000))::bigint FROM (
			SELECT min(y.ts) AS s, max(y.e) AS e FROM (
				SELECT x.ts, x.e, sum(x.brk) OVER (ORDER BY x.ts, x.e ROWS UNBOUNDED PRECEDING) AS grp FROM (
					SELECT q.ts, q.e, CASE WHEN q.ts <= max(q.e) OVER (ORDER BY q.ts, q.e ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING)
						THEN 0 ELSE 1 END AS brk
					FROM (` + amountCoveragePoints + `) q
				) x
			) y GROUP BY y.grp
		) z), 0)
	FROM ` + amountCoverageCells

// amountCoverage settles each cell's coverage, the union of its amounts'
// spans: from the rows' and the cut ends' extents (observed.UnionMs) where no
// row's extent overlaps another, and from the raw spans in SQL in the cells
// where one does, so a cell served from rows reports the coverage, and
// prefer-raw makes the choice, that the raw points would.
func (r *aggRun) amountCoverage(g *aggGroup, cells aggCells) error {
	var pending []*aggEvidence
	var a, b, wa, wb []time.Time
	var codes []string
	local := r.p.bucket.mode == bucketLocal
	for _, bk := range r.buckets {
		for _, code := range sortedKeys(cells[bk]) {
			if err := r.canceled(); err != nil {
				return err
			}
			ev := cells[bk][code]
			ms, ok, err := observed.UnionMsContext(r.ctx, ev.extents)
			if err != nil {
				return err
			}
			if ok {
				ev.coverage = ms
				continue
			}
			pending = append(pending, ev)
			if local {
				a, b, wa, wb = append(a, r.p.from), append(b, r.p.to), append(wa, bk.start), append(wb, bk.end)
			} else {
				a, b, wa, wb = append(a, bk.start), append(b, bk.end), append(wa, bk.start), append(wb, bk.end)
			}
			codes = append(codes, code)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	ids := streamKeys(g.raw, false)
	admitted := sortedKeys(r.p.codes)
	args := []any{ids[0], ids[1], ids[2], ids[3], ids[4], a, b, wa, wb, codes, admitted, local, r.p.grouped[substrate.GroupCode], r.p.codes != nil}
	var n int64
	if err := r.tx.QueryRowContext(r.ctx, amountCoverageCountSQL, args...).Scan(&n); err != nil {
		return fmt.Errorf("substrate/engine: aggregate coverage count: %w", err)
	}
	if err := r.charge(n, "raw spans to join where amounts' rollup rows overlap"); err != nil {
		return err
	}
	rows, err := r.tx.QueryContext(r.ctx, amountCoverageSQL, args...)
	if err != nil {
		return fmt.Errorf("substrate/engine: aggregate coverage: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var i int
		var ms int64
		if err := rows.Scan(&i, &ms); err != nil {
			return err
		}
		pending[i-1].coverage = ms
	}
	return rows.Err()
}

// rollupFragments merges the raw points of an amount's or a counter's cut bucket
// ends into the buckets, through observed.Bucket as a rollup row would, its
// groups in key order so the merge does not follow a map's. The segments and
// each segment's points are in time order, so a cut end's points are found
// by binary search: a bucket narrower than a rollup row is all cut end, and a
// scan per bucket would take buckets times points.
func (r *aggRun) rollupFragments(out aggCells, g *aggGroup, s *aggStream, si int) error {
	segs := r.series[s].segs
	for _, b := range r.buckets {
		if err := r.canceled(); err != nil {
			return err
		}
		_, frags := split(b)
		for _, e := range frags {
			k := sort.Search(len(segs), func(i int) bool { return !segs[i].to.Before(e[1]) })
			if k == len(segs) || segs[k].from.After(e[0]) {
				continue
			}
			seg := &segs[k]
			lo := sort.Search(len(seg.points), func(i int) bool { return !seg.points[i].At.Before(e[0]) })
			hi := sort.Search(len(seg.points), func(i int) bool { return !seg.points[i].At.Before(e[1]) })
			var pts []observed.Point
			groups := map[string]string{}
			for _, ap := range seg.points[lo:hi] {
				if err := r.canceled(); err != nil {
					return err
				}
				if !r.admits(ap.Code) {
					continue
				}
				q := ap.Point
				if err := r.convertPoint(&q, ap.delta, s); err != nil {
					return err
				}
				ck := r.codeKey(ap.Code)
				q.Group = ck + "\x1f" + q.Unit
				groups[q.Group] = ck
				pts = append(pts, q)
			}
			if len(pts) == 0 {
				continue
			}
			var prev *observed.Point
			for i := lo - 1; i >= 0; i-- {
				if err := r.canceled(); err != nil {
					return err
				}
				if ap := seg.points[i]; !ap.Unavailable {
					p := ap.Point
					prev = &p
					break
				}
			}
			if prev == nil && seg.prev != nil {
				p := seg.prev.Point
				prev = &p
			}
			stats := observed.Bucket(g.form, e[0], e[1], prev, pts)
			for _, key := range sortedKeys(stats) {
				st := stats[key]
				unit := strings.SplitN(key, "\x1f", 2)[1]
				conv, err := r.convertStats(*st, obsGroup{unit: unit}, s.source)
				if err != nil {
					return err
				}
				ev := cell(out, b, groups[key], g.form, false)
				r.mergeStats(ev, conv, g.form, si)
				if st.N > 0 {
					ev.sources[s.source] = true
				}
				if g.form == vocabulary.FormCounter {
					ev.coverage += st.CoveredMs
				}
				if conv.Increase != nil {
					ev.steps.add(conv.Increase, false, st.CoveredMs)
					ev.steps.resets += st.Resets
				}
			}
			if g.form == vocabulary.FormAmount {
				// A cut end's spans are known exactly, so they join the
				// rows' extents as raw spans.
				for _, p := range pts {
					if err := r.canceled(); err != nil {
						return err
					}
					if !p.Unavailable {
						ev := cell(out, b, r.codeKey(p.Code), g.form, false)
						ev.extents = append(ev.extents, observed.Extent{Start: p.At, End: p.End, Ms: p.End.Sub(p.At).Milliseconds(), Exact: true})
					}
				}
			}
		}
	}
	return nil
}

// walkHeld walks a gauge stream's 15-minute slots across one of the read's
// windows (the range, or a records-mode window, which may reach past the
// range), carrying the held value from row to row (observed.HeldFromRowsCarry)
// from the point current at the window's start, and adds each slot's held
// time to the buckets it lies in: a recorded-local bucket by the offset of the
// point that held.
func (r *aggRun) walkHeld(out aggCells, g *aggGroup, s *aggStream, si int, rows []observed.Row, w [2]time.Time, carry *observed.Point) error {
	from, to := floor15(w[0]), w[1]
	i := sort.Search(len(rows), func(i int) bool { return !rows[i].Start.Before(from) })
	for slot := from; slot.Before(to); {
		if err := r.canceled(); err != nil {
			return err
		}
		for i < len(rows) && rows[i].Start.Before(slot) {
			i++
		}
		j := i
		for j < len(rows) && rows[j].Start.Equal(slot) {
			j++
		}
		end := slot.Add(bucketWidth)
		if i == j {
			// No row here: jump to the next row, the carry holding (or not)
			// across the empty slots, cut at bucket boundaries so each bucket gets
			// its share.
			next := to
			if i < len(rows) && rows[i].Start.Before(to) {
				next = rows[i].Start
			}
			if carry == nil || carryEnded(carry, slot, r.p.maxGap) {
				carry = nil
				slot = next
				continue
			}
			end = r.chunkEnd(slot, next, carry)
		}
		sums, nextCarry, ok := observed.HeldFromRowsCarry(rows[i:j], carry, slot, end, r.p.maxGap)
		if !ok {
			return fmt.Errorf("substrate/engine: aggregate: a rollup row of %s is not eligible after the plan read it so", s.metric)
		}
		// In key order: the sums add as floats, and bucketsOf may make a
		// recorded-local bucket, so a map's order must decide neither.
		for _, key := range sortedKeys(sums) {
			h := sums[key]
			og := r.rowGroups[key]
			if h.Ms == 0 || !r.admits(og.code) {
				continue
			}
			ms, valueMs := h.Ms, h.ValueMs
			if og.unit != "" {
				c, err := r.conversion(og.unit, og.delta, s.source)
				if err != nil {
					return err
				}
				valueMs = c.ff*valueMs + c.fo*float64(ms)
			}
			for _, b := range r.bucketsOf(slot, og.offset) {
				ev := cell(out, b, r.codeKey(og.code), g.form, false)
				if b.local {
					b.ends.hold(slot, end, si, og.offset)
					ev.ends.hold(slot, end, si, og.offset)
				}
				hs := ev.held[og.code]
				hs.Ms += ms
				hs.ValueMs += valueMs
				ev.held[og.code] = hs
				ev.coverage += ms
				ev.sources[s.source] = true
			}
		}
		carry, slot = nextCarry, end
	}
	return nil
}

// carryEnded reports whether a carried value has stopped holding by t.
func carryEnded(c *observed.Point, t time.Time, maxGap time.Duration) bool {
	if c.Hold != nil && !c.At.Add(*c.Hold).After(t) {
		return true
	}
	return maxGap > 0 && !c.At.Add(maxGap).After(t)
}

// chunkEnd is where a carry across empty slots is cut: the next row, the end
// of the bucket the slot lies in (the next start or end of any records-mode
// window, which may overlap), or the window's end.
func (r *aggRun) chunkEnd(slot, next time.Time, carry *observed.Point) time.Time {
	end := next
	switch {
	case r.p.bucket.mode == bucketLocal:
		// The day's end is computed, not looked up: a bucket made here would
		// stand for held time that may never land in it.
		og := r.rowGroups[carry.Group]
		shift := time.Duration(og.offset) * time.Minute
		day := calNext(calStart(slot.UTC().Add(shift), r.p.bucket), r.p.bucket)
		if e := day.Add(-shift); e.Before(end) {
			end = e
		}
	case r.p.bucket.mode == bucketRecords:
		if r.bounds == nil {
			for _, b := range r.buckets {
				r.bounds = append(r.bounds, b.start, b.end)
			}
			slices.SortFunc(r.bounds, time.Time.Compare)
		}
		i := sort.Search(len(r.bounds), func(i int) bool { return r.bounds[i].After(slot) })
		if i < len(r.bounds) && r.bounds[i].Before(end) {
			end = r.bounds[i]
		}
	default:
		bs := r.bucketsOf(slot, 0)
		if len(bs) == 0 {
			break
		}
		if e := bs[0].end; e.Before(end) {
			end = e
		}
	}
	if !end.After(slot) {
		end = slot.Add(bucketWidth)
	}
	return end
}
