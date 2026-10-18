package engine

// The aggregate read's plan (aggregateread.go): where each group's buckets are
// served from, the spans the read takes, and the budget each read is charged
// to before it runs.

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// plan decides where each group's buckets are served from, chooses the
// rollup rows of the groups they serve (planSlots), and counts the rows and
// the raw points the rest need against maxSamples before reading them.
func (r *aggRun) plan(groups []*aggGroup) error {
	r.slots, r.rowGroups = map[*aggStream][]aggSlot{}, map[string]obsGroup{}
	r.frags = map[*aggStream]map[*aggBucket][][2]time.Time{}
	r.carries, r.series = map[*aggStream][]*observed.Point{}, map[*aggStream]*aggSeries{}
	r.bounds = r.bucketBounds()
	for _, g := range groups {
		if g.rollups = r.rollupable(g); !g.rollups {
			continue
		}
		if err := r.planGroup(g); err != nil {
			return err
		}
		if g.rollups && g.form == vocabulary.FormGauge {
			if err := r.planCarries(g); err != nil {
				return err
			}
		}
		if !g.rollups {
			// Served from raw points after all: the rows it chose are freed
			// and their charge returned, and its points are charged below.
			for _, s := range g.raw {
				for _, sl := range r.slots[s] {
					r.samples -= int64(len(sl.rows))
				}
				delete(r.slots, s)
				delete(r.frags, s)
			}
		}
	}
	want := map[*aggStream][][2]time.Time{}
	for _, g := range groups {
		// A priority group of amounts or summaries reads its points, and
		// its context streams', over the windows priorityContext widened.
		wide := r.windows
		if r.contextWindows != nil && g.form != vocabulary.FormGauge && g.form != vocabulary.FormCounter {
			wide = r.contextWindows
		}
		for _, s := range g.raw {
			if g.rollups {
				var spans [][2]time.Time
				for _, f := range r.frags[s] {
					spans = append(spans, f...)
				}
				c, err := r.cover(spans)
				if err != nil {
					return err
				}
				want[s] = c
				continue
			}
			want[s] = wide
		}
		if r.p.source != substrate.AggregateFromRaw {
			for _, s := range g.sums {
				want[s] = r.contextWindowsOr()
			}
		}
		for _, s := range g.context {
			want[s] = r.contextWindows
		}
	}
	var total int64
	for s, spans := range want {
		if len(spans) == 0 {
			continue
		}
		n, err := r.countPoints(s, spans)
		if err != nil {
			return err
		}
		total += n
	}
	if err := r.charge(total, "raw points to reduce"); err != nil {
		return err
	}
	for s, spans := range want {
		if len(spans) == 0 {
			r.series[s] = &aggSeries{}
			continue
		}
		ser, err := r.readSeries(s, spans)
		if err != nil {
			return err
		}
		r.series[s] = ser
		if tr := traceOf(r.ctx); tr != nil {
			for _, seg := range ser.segs {
				tr.rawPoints += int64(len(seg.points))
			}
		}
	}
	return nil
}

// planGroup chooses each of a rollup group's streams' rows (planSlots), and
// sends the group to raw points where the rows cannot answer: a row holding
// a censored value keeps how many, not which, so the raw points answer it
// with its comparator (values); and a gauge's bucket or a recorded-local
// read that no tier covers whole is read from raw points whole, since only
// an amount's or a counter's cut ends merge from raw points into rows.
func (r *aggRun) planGroup(g *aggGroup) error {
	local := r.p.bucket.mode == bucketLocal
	for _, s := range g.raw {
		slots, rest, err := r.planSlots(s, g.form)
		if err != nil {
			return err
		}
		r.slots[s] = slots
		for _, sl := range slots {
			for _, row := range sl.rows {
				if row.Bounded > 0 {
					g.rollups = false
					return nil
				}
			}
		}
		if len(rest) == 0 {
			continue
		}
		if local || g.form == vocabulary.FormGauge {
			g.rollups = false
			return nil
		}
		frags := map[*aggBucket][][2]time.Time{}
		for _, b := range r.buckets {
			for _, sp := range rest {
				if lo, hi := later(sp[0], b.start), earlier(sp[1], b.end); lo.Before(hi) {
					frags[b] = append(frags[b], [2]time.Time{lo, hi})
				}
			}
		}
		r.frags[s] = frags
	}
	return nil
}

// aggSlot is one bucket of one tier a stream's rows are taken from: every
// row of the stream's form in it, of every group.
type aggSlot struct {
	start, end time.Time
	rows       []observed.Row
}

// bucketBounds are the buckets' starts and ends, sorted and once each.
func (r *aggRun) bucketBounds() []time.Time {
	var out []time.Time
	for _, b := range r.buckets {
		out = append(out, b.start, b.end)
	}
	slices.SortFunc(out, time.Time.Compare)
	return slices.CompactFunc(out, time.Time.Equal)
}

// slotFits reports whether a bucket [a, b) of width w may stand for the
// points in it on this read. Outside the recorded-local calendar it may not
// cross a bucket's start or end, so it lies whole in every bucket it meets.
// A recorded-local row lands whole in the local bucket its start names at
// its group's offset, which a row of at most a day at its own offset does
// for a day, a week and a month, and of at most an hour for an hour; and it
// may not cross the range's start, where a lookback day is split.
func (r *aggRun) slotFits(a, b time.Time, w time.Duration) bool {
	if r.p.bucket.mode == bucketLocal {
		if r.p.bucket.calendar == "hour" && w > time.Hour {
			return false
		}
		return !a.Before(r.p.from) || !b.After(r.p.from)
	}
	i := sort.Search(len(r.bounds), func(i int) bool { return r.bounds[i].After(a) })
	return i == len(r.bounds) || !r.bounds[i].Before(b)
}

// planSlots covers the read's windows with one stream's rollup rows, the
// coarsest of its metric's tiers first. A bucket of a tier is taken where it
// lies inside what is not yet covered, fits the read (slotFits), no row of
// another alignment overlaps it (a +02:00 day beside a +01:00 one, or a
// +05:30 hour beside a whole one), and, for a gauge, every row of it is
// Eligible under the read's maxGap; then its rows stand for every point in
// it. A stretch no row of a tier meets holds no point. What no tier covers
// is answered as rest, for raw points. The rows of each tier are counted
// against maxSamples before they are read, and the charge of those not taken
// is returned.
func (r *aggRun) planSlots(s *aggStream, form string) (slots []aggSlot, rest [][2]time.Time, err error) {
	open, err := r.cover(slices.Clone(r.windows))
	if err != nil {
		return nil, nil, err
	}
	for ti := len(s.tiers) - 1; ti >= 0 && len(open) > 0; ti-- {
		w := s.tiers[ti]
		spans := make([][2]time.Time, len(open))
		for i, o := range open {
			spans[i] = [2]time.Time{o[0].Add(-w).Add(time.Millisecond), o[1]}
		}
		n, err := r.countRollups(s, w, spans)
		if err != nil {
			return nil, nil, err
		}
		if err := r.charge(n, "rollup rows"); err != nil {
			return nil, nil, err
		}
		rows, err := r.readRollups(s, w, spans)
		if err != nil {
			return nil, nil, err
		}
		var starts []time.Time
		byStart := map[time.Time][]observed.Row{}
		for _, row := range rows {
			byStart[row.Start] = append(byStart[row.Start], row)
			if len(byStart[row.Start]) == 1 {
				starts = append(starts, row.Start)
			}
		}
		slices.SortFunc(starts, time.Time.Compare)
		var taken, occupied [][2]time.Time
		held := 0
		for i, start := range starts {
			if err := r.canceled(); err != nil {
				return nil, nil, err
			}
			end := start.Add(w)
			occupied = append(occupied, [2]time.Time{start, end})
			if (i > 0 && starts[i-1].Add(w).After(start)) || (i+1 < len(starts) && starts[i+1].Before(end)) {
				continue
			}
			if !open.within(start, end) || !r.slotFits(start, end, w) {
				continue
			}
			if form == vocabulary.FormGauge && slices.ContainsFunc(byStart[start], func(row observed.Row) bool {
				return !observed.Eligible(r.p.maxGap, row.MaxGapMs)
			}) {
				continue
			}
			slots = append(slots, aggSlot{start: start, end: end, rows: byStart[start]})
			taken = append(taken, [2]time.Time{start, end})
			held += len(byStart[start])
		}
		r.samples -= int64(len(rows) - held)
		if tr := traceOf(r.ctx); tr != nil {
			tr.rollupRows += int64(held)
		}
		// What no row of this tier meets holds no point, and what a taken
		// slot covers is done.
		occ, err := r.cover(occupied)
		if err != nil {
			return nil, nil, err
		}
		open = subtractSpans(intersectSpans(open, occ), taken)
	}
	slices.SortFunc(slots, func(a, b aggSlot) int { return a.start.Compare(b.start) })
	return slots, open, nil
}

// countRollups counts the rows of one stream's form and tier whose bucket
// starts in the spans.
func (r *aggRun) countRollups(s *aggStream, w time.Duration, spans [][2]time.Time) (int64, error) {
	from, to := spanArrays(spans)
	var n int64
	if err := r.tx.QueryRowContext(r.ctx, `
		SELECT coalesce(sum(x.c), 0)::bigint FROM unnest($4::timestamptz[], $5::timestamptz[]) AS w(a, b)
		CROSS JOIN LATERAL (
			SELECT count(*) AS c FROM observed_rollups r
			WHERE r.stream = $1 AND r.form = $2 AND r.tier = $3 AND r.bucket >= w.a AND r.bucket < w.b) x`,
		s.id, s.form, tierSeconds(w), from, to).Scan(&n); err != nil {
		return 0, fmt.Errorf("substrate/engine: aggregate rollup count: %w", err)
	}
	return n, nil
}

// within reports whether [a, b) lies inside one span of the cover.
func (c cover) within(a, b time.Time) bool {
	i := sort.Search(len(c), func(i int) bool { return c[i][1].After(a) })
	return i < len(c) && !c[i][0].After(a) && !c[i][1].Before(b)
}

// intersectSpans is the parts of a that b covers; both sorted and joined.
func intersectSpans(a, b [][2]time.Time) [][2]time.Time {
	var out [][2]time.Time
	for i, j := 0, 0; i < len(a) && j < len(b); {
		lo, hi := later(a[i][0], b[j][0]), earlier(a[i][1], b[j][1])
		if lo.Before(hi) {
			out = append(out, [2]time.Time{lo, hi})
		}
		if a[i][1].Before(b[j][1]) {
			i++
		} else {
			j++
		}
	}
	return out
}

// subtractSpans is the parts of a that no span of cut covers; a sorted and
// joined, cut sorted and disjoint.
func subtractSpans(a, cut [][2]time.Time) [][2]time.Time {
	var out [][2]time.Time
	for _, sp := range a {
		lo := sp[0]
		for _, c := range cut {
			if !c[1].After(lo) || !c[0].Before(sp[1]) {
				continue
			}
			if c[0].After(lo) {
				out = append(out, [2]time.Time{lo, c[0]})
			}
			lo = later(lo, c[1])
		}
		if lo.Before(sp[1]) {
			out = append(out, [2]time.Time{lo, sp[1]})
		}
	}
	return out
}

// contextWindows is the windows a priority read's summaries are read over:
// the widened ones where priorityContext found any, else the read's own.
func (r *aggRun) contextWindowsOr() [][2]time.Time {
	if r.contextWindows != nil {
		return r.contextWindows
	}
	return r.windows
}

// planCarries reads the value current at each window's start for a gauge
// group served from rollups, before the raw points are counted. A
// recorded-local read places the time such a value holds into the window's
// first slot by the value's own offset (walkHeld). A censored value that
// still holds into a window sends the group to raw points, as a rollup row
// holding one does (planGroup): the held sums would weigh its bound as its
// number.
func (r *aggRun) planCarries(g *aggGroup) error {
	for _, s := range g.raw {
		c, err := r.readCarries(s, r.windows)
		if err != nil {
			return err
		}
		r.carries[s] = c
		for i, p := range c {
			if p != nil && p.Comparator != "" && !p.Unavailable && !carryEnded(p, r.windows[i][0], r.p.maxGap) {
				g.rollups = false
				return nil
			}
		}
	}
	return nil
}

// charge counts n more units of materialized work against maxSamples before
// the read takes them, and refuses the read once those counted so far (the
// records a records-mode read matched, the forms' discovery, the rollup
// rows, the reduction's points, the coverage's, the copies of a point in
// more windows) pass it. what says what the n are.
func (r *aggRun) charge(n int64, what string) error {
	r.samples += n
	if r.samples <= r.p.maxSamples {
		return nil
	}
	return refuse("aggregate.budget.maxSamples", "the read needs %d samples (%d of them %s), over %d: a sample is a raw point, rollup row, record or window copy the read holds; narrow the range, use reducers rollups serve (not percentile or durationIn, and no maxGap tighter than the data), or raise maxSamples (the cap is %d)",
		r.samples, n, what, r.p.maxSamples, substrate.AggregateMaxSamplesCap)
}

// chargeCopies charges the copies past the first of a point or an interval
// placed in n records-mode windows: each is held until its row is reduced.
func (r *aggRun) chargeCopies(n int) error {
	if n < 2 {
		return nil
	}
	return r.charge(int64(n-1), "copies of a point in the further records-mode windows it lies in")
}

// rollupable reports whether every reducer of the read merges exactly from a
// group's rollup rows: whether its rows may serve it at all, before
// planGroup finds which rows cover what.
func (r *aggRun) rollupable(g *aggGroup) bool {
	p := r.p
	if p.forceRaw || len(g.raw) == 0 {
		return false
	}
	for _, s := range g.raw {
		if len(s.tiers) == 0 {
			return false
		}
	}
	if p.policy == substrate.SourcesPriority && len(g.raw)+len(g.contextOf(g.form)) > 1 {
		return false
	}
	// A row holds the points that start in its bucket, and place end
	// places them by their end; a records-mode window takes an amount by
	// the part of its span inside, which may start in a row before it.
	if p.bucket.placeEnd || r.overlaps(g.form) {
		return false
	}
	for _, red := range p.reducers {
		switch red.Name {
		case substrate.ReducerPercentile, substrate.ReducerDurationIn:
			return false
		}
	}
	if p.bucket.mode == bucketLocal {
		// A value written without an offset is placed by the fallback zone's
		// clock at its own instant, which a row does not keep.
		for _, s := range g.raw {
			if s.offsetless {
				return false
			}
		}
	}
	return true
}

// joinSpans sorts intervals by start, in place, and joins those that overlap
// or touch. The sort checks ctx and the join calls check once per interval:
// a cover can be as long as a stream's points.
func joinSpans(ctx context.Context, spans [][2]time.Time, check func() error) (cover, error) {
	if err := observed.SortStableFunc(ctx, spans, func(a, b [2]time.Time) int { return a[0].Compare(b[0]) }); err != nil {
		return nil, err
	}
	var out cover
	for _, s := range spans {
		if err := check(); err != nil {
			return nil, err
		}
		if n := len(out); n > 0 && !s[0].After(out[n-1][1]) {
			if s[1].After(out[n-1][1]) {
				out[n-1][1] = s[1]
			}
			continue
		}
		out = append(out, s)
	}
	return out, nil
}
