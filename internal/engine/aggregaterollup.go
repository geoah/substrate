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
		var rows []observed.Row
		for _, sl := range r.slots[s] {
			rows = append(rows, sl.rows...)
		}
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
				// The row lands in the day its bucket names at its offset,
				// whose clock it is aligned to; the day's first and last
				// points are the rows' own.
				var err error
				if targets, err = r.bucketsOf(row.Start, og.offset); err != nil {
					return nil, err
				}
			} else {
				meeting, err := r.bucketsMeeting(row.Start, row.End)
				if err != nil {
					return nil, err
				}
				for _, b := range meeting {
					if !row.Start.Before(b.start) && !row.End.After(b.end) {
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
						ev.ends.mark(*pt, si, og.offset)
					}
				}
				r.mergeStats(ev, st, g.form, si)
				r.offerExtremes(ev, st, rowCand(s, si, row, og))
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
				if err := r.walkHeld(out, g, s, si, r.slots[s], w, r.carries[s][i]); err != nil {
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
	// Two products, not f*f: a factor whose square overflows makes an M2 of
	// 0 (one value) NaN, where the deviation is 0.
	s.M2 = s.M2 * f * f
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

// amountCoverage settles each cell's coverage, the union of its amounts'
// spans: from the rows' and the cut ends' extents (observed.UnionMs) where no
// row's extent overlaps another, and from the raw spans in the cells where
// one does, so a cell served from rows reports the coverage, and prefer-raw
// makes the choice, that the raw points would. A raw span counts in a cell
// where its instant lies in the cell, on a recorded-local read where its wall
// clock at its own offset lies in the cell's day, and of the cell's code where
// the read groups by code; spans that overlap or touch join, as joinSpans
// joins them, and each joined span counts in whole milliseconds.
func (r *aggRun) amountCoverage(g *aggGroup, cells aggCells) error {
	type pending struct {
		ev           *aggEvidence
		a, b, wa, wb time.Time
		code         string
	}
	var list []pending
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
			c := pending{ev: ev, a: bk.start, b: bk.end, wa: bk.start, wb: bk.end, code: code}
			if local {
				c.a, c.b = r.p.rangeOf(bk)
			}
			list = append(list, c)
		}
	}
	if len(list) == 0 {
		return nil
	}
	var n int64
	for _, c := range list {
		for _, s := range g.raw {
			k, err := r.countPoints(s, [][2]time.Time{{c.a, c.b}})
			if err != nil {
				return err
			}
			n += k
		}
	}
	if err := r.charge(n, "raw spans to join where amounts' rollup rows overlap"); err != nil {
		return err
	}
	byCode := r.p.grouped[substrate.GroupCode]
	for _, c := range list {
		var spans [][2]time.Time
		for _, s := range g.raw {
			pts, err := r.store.points(s.info(), vocabulary.FormAmount, c.a, c.b)
			if err != nil {
				return err
			}
			for _, p := range pts {
				if err := r.canceled(); err != nil {
					return err
				}
				if p.Unavailable || (byCode && p.Code != c.code) || !r.admits(p.Code) {
					continue
				}
				if local {
					wall := p.At.Add(time.Duration(r.store.groups[p.Group].offset) * time.Minute)
					if wall.Before(c.wa) || !wall.Before(c.wb) {
						continue
					}
				}
				spans = append(spans, [2]time.Time{p.At, p.End})
			}
		}
		sort.Slice(spans, func(i, j int) bool {
			if !spans[i][0].Equal(spans[j][0]) {
				return spans[i][0].Before(spans[j][0])
			}
			return spans[i][1].Before(spans[j][1])
		})
		var total int64
		var cur [2]time.Time
		for i, sp := range spans {
			switch {
			case i == 0:
				cur = sp
			case !sp[0].After(cur[1]):
				if sp[1].After(cur[1]) {
					cur[1] = sp[1]
				}
			default:
				total += cur[1].Sub(cur[0]).Milliseconds()
				cur = sp
			}
		}
		if len(spans) > 0 {
			total += cur[1].Sub(cur[0]).Milliseconds()
		}
		c.ev.coverage = total
	}
	return nil
}

// rollupFragments merges the raw points of an amount's or a counter's
// bucket parts no slot covers (planGroup) into the buckets, through
// observed.Bucket as a rollup row would, its groups in key order so the merge
// does not follow a map's. The segments and each segment's points are in
// time order, so a part's points are found by binary search: a bucket
// narrower than a rollup row is all one part, and a scan per bucket would
// take buckets times points.
func (r *aggRun) rollupFragments(out aggCells, g *aggGroup, s *aggStream, si int) error {
	segs := r.series[s].segs
	for _, b := range r.buckets {
		if err := r.canceled(); err != nil {
			return err
		}
		for _, e := range r.frags[s][b] {
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
				r.offerExtremes(ev, conv, pointCand(pts, key, *st, si))
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

// walkHeld walks a gauge stream's slots across one of the read's windows
// (the range, or a records-mode window, which may reach past the range),
// carrying the held value from slot to slot (observed.HeldFromRowsCarryIn)
// from the point current at the window's start, across the stretches no row
// holds as well, and adds each slot's held time to the buckets it lies in. A
// recorded-local bucket takes a group's time by the group's offset, and the
// time a value carried into a slot holds there by the carried value's own,
// which need not be the slot's rows'.
func (r *aggRun) walkHeld(out aggCells, g *aggGroup, s *aggStream, si int, slots []aggSlot, w [2]time.Time, carry *observed.Point) error {
	cursor := w[0]
	// across carries the held value over [cursor, to), where no row is,
	// cut at bucket boundaries so each bucket gets its share.
	across := func(to time.Time) error {
		for cursor.Before(to) {
			if err := r.canceled(); err != nil {
				return err
			}
			if carry == nil || carryEnded(carry, cursor, r.p.maxGap) {
				carry, cursor = nil, to
				return nil
			}
			end, err := r.chunkEnd(cursor, to, carry)
			if err != nil {
				return err
			}
			if err := r.holdSlot(out, g, s, si, aggSlot{start: cursor, end: end}, &carry); err != nil {
				return err
			}
			cursor = end
		}
		return nil
	}
	i := sort.Search(len(slots), func(i int) bool { return !slots[i].start.Before(w[0]) })
	for ; i < len(slots) && slots[i].start.Before(w[1]); i++ {
		if err := across(slots[i].start); err != nil {
			return err
		}
		if err := r.holdSlot(out, g, s, si, slots[i], &carry); err != nil {
			return err
		}
		cursor = slots[i].end
	}
	return across(w[1])
}

// holdSlot adds one slot's held time, its rows' and the carried value's,
// to the buckets it lies in, and moves carry on to the value held past it.
func (r *aggRun) holdSlot(out aggCells, g *aggGroup, s *aggStream, si int, sl aggSlot, carry **observed.Point) error {
	in := *carry
	sums, next, carried, ok := observed.HeldFromRowsCarryIn(sl.rows, in, sl.start, sl.end, r.p.maxGap)
	if !ok {
		return fmt.Errorf("substrate/engine: aggregate: a rollup row of %s is not eligible after the plan read it so", s.metric)
	}
	*carry = next
	// In key order: the sums add as floats, and bucketsOf may make a
	// recorded-local bucket, so a map's order must decide neither.
	for _, key := range sortedKeys(sums) {
		h := sums[key]
		og := r.rowGroups[key]
		if h.Ms == 0 || !r.admits(og.code) {
			continue
		}
		type part struct {
			at     time.Time
			to     time.Time
			ms     int64
			valued float64
		}
		parts := []part{{at: sl.start, to: sl.end, ms: h.Ms, valued: h.ValueMs}}
		if r.p.bucket.mode == bucketLocal && in != nil && key == in.Group && carried.Ms > 0 {
			// The carried value's time from the slot's start, placed by its
			// own offset, a local bucket at a time; the rest is the rows'.
			parts = parts[:0]
			if rest := h.Ms - carried.Ms; rest > 0 {
				parts = append(parts, part{at: sl.start, to: sl.end, ms: rest, valued: h.ValueMs - carried.ValueMs})
			}
			value := carried.ValueMs / float64(carried.Ms)
			from, until := sl.start, sl.start.Add(time.Duration(carried.Ms)*time.Millisecond)
			for from.Before(until) {
				end, err := r.chunkEnd(from, until, in)
				if err != nil {
					return err
				}
				ms := end.Sub(from).Milliseconds()
				parts = append(parts, part{at: from, to: end, ms: ms, valued: value * float64(ms)})
				from = end
			}
		}
		for _, pt := range parts {
			ms, valueMs := pt.ms, pt.valued
			if og.unit != "" {
				c, err := r.conversion(og.unit, og.delta, s.source)
				if err != nil {
					return err
				}
				valueMs = c.ff*valueMs + c.fo*float64(ms)
			}
			buckets, err := r.bucketsOf(pt.at, og.offset)
			if err != nil {
				return err
			}
			for _, b := range buckets {
				ev := cell(out, b, r.codeKey(og.code), g.form, false)
				if b.local {
					ev.ends.hold(pt.at, pt.to, si, og.offset)
				}
				hs := ev.held[og.code]
				hs.Ms += ms
				hs.ValueMs += valueMs
				ev.held[og.code] = hs
				ev.coverage += ms
				ev.sources[s.source] = true
			}
		}
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
func (r *aggRun) chunkEnd(slot, next time.Time, carry *observed.Point) (time.Time, error) {
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
		i := sort.Search(len(r.bounds), func(i int) bool { return r.bounds[i].After(slot) })
		if i < len(r.bounds) && r.bounds[i].Before(end) {
			end = r.bounds[i]
		}
	default:
		bs, err := r.bucketsOf(slot, 0)
		if err != nil {
			return end, err
		}
		if len(bs) > 0 && bs[0].end.Before(end) {
			end = bs[0].end
		}
	}
	// The range's start ends the lookback: held time on either side of it
	// lands in a bucket of its own side.
	if slot.Before(r.p.from) && r.p.from.Before(end) {
		end = r.p.from
	}
	if !end.After(slot) {
		end = slot.Add(bucketWidth)
	}
	return end, nil
}
