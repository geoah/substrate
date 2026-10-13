package engine

// The aggregate read's plan (aggregateread.go): where each group's buckets are
// served from, the spans the read takes, and the budget each read is charged
// to before it runs.

import (
	"context"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// plan decides where each group's buckets are served from, reads the rollup
// rows of the groups they serve, and counts the rows and the raw points the
// rest need against maxSamples before reading them.
func (r *aggRun) plan(groups []*aggGroup) error {
	r.rows, r.rowGroups = map[*aggStream][]observed.Row{}, map[string]obsGroup{}
	r.carries, r.series = map[*aggStream][]*observed.Point{}, map[*aggStream]*aggSeries{}
	for _, g := range groups {
		g.rollups = r.rollupable(g)
	}
	if err := r.planSpans(); err != nil {
		return err
	}
	if err := r.chargeRollups(groups); err != nil {
		return err
	}
	for _, g := range groups {
		if !g.rollups {
			continue
		}
		for _, s := range g.raw {
			rows, err := r.readRollups(s)
			if err != nil {
				return err
			}
			r.rows[s] = rows
			if tr := traceOf(r.ctx); tr != nil {
				tr.rollupRows += int64(len(rows))
			}
			for _, row := range rows {
				if err := r.canceled(); err != nil {
					return err
				}
				if g.form == vocabulary.FormGauge && !observed.Eligible(r.p.maxGap, row.MaxGapMs) {
					g.rollups = false
					break
				}
				// A recorded-local row lands whole in the day its quarter
				// hour names at its offset, which holds only where the
				// offset is whole quarter hours: at +00:01 a row from 23:45Z
				// holds points on both sides of local midnight.
				if og := r.rowGroups[row.Group]; r.p.bucket.mode == bucketLocal && (og.offsetUnknown || !wholeQuarterHours(og.offset)) {
					g.rollups = false
					break
				}
			}
		}
		if g.rollups && g.form == vocabulary.FormGauge {
			if err := r.planCarries(g); err != nil {
				return err
			}
		}
		if !g.rollups {
			// Served from raw points after all: the rows it read are freed
			// and their charge returned, and its points are charged below.
			for _, s := range g.raw {
				r.samples -= int64(len(r.rows[s]))
				delete(r.rows, s)
			}
		}
	}
	want := map[*aggStream][][2]time.Time{}
	for _, g := range groups {
		for _, s := range g.raw {
			if g.rollups {
				if g.form != vocabulary.FormGauge {
					want[s] = r.fragments
				}
				continue
			}
			want[s] = r.windows
		}
		if r.p.source != substrate.AggregateFromRaw {
			for _, s := range g.sums {
				want[s] = r.windows
			}
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

// planCarries reads the value current at each window's start for a gauge
// group served from rollups. On a recorded-local read, a value that still
// holds into its window at an offset that is not whole quarter hours sends
// the group to raw points, as such a row does: walkHeld puts a slot's carried
// time whole in the day the slot's start names, and `open` at +00:01 from
// 23:44Z, held into the slot from 23:45Z, crosses local midnight at 23:59Z.
// It runs before the raw points are counted, so they are budgeted.
func (r *aggRun) planCarries(g *aggGroup) error {
	windows := r.windows
	for _, s := range g.raw {
		c, err := r.readCarries(s, windows)
		if err != nil {
			return err
		}
		r.carries[s] = c
		if r.p.bucket.mode != bucketLocal {
			continue
		}
		for i, p := range c {
			if p == nil || p.Unavailable || carryEnded(p, floor15(windows[i][0]), r.p.maxGap) {
				continue
			}
			if og := r.rowGroups[p.Group]; og.offsetUnknown || !wholeQuarterHours(og.offset) {
				g.rollups = false
			}
		}
	}
	if !g.rollups {
		for _, s := range g.raw {
			delete(r.carries, s)
		}
	}
	return nil
}

// rollupCountSQL counts the rollup rows of the streams ($1 to $6) in the
// spans ($7 and $8) the reads of readRollups take.
const rollupCountSQL = `
	SELECT coalesce(sum(x.c), 0)::bigint
	FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[]) AS s(metric, kind, source, about, channel, form)
	CROSS JOIN unnest($7::timestamptz[], $8::timestamptz[]) AS w(a, b)
	CROSS JOIN LATERAL (
		SELECT count(*) AS c FROM observed_rollups r
		WHERE r.metric = s.metric AND r.kind = s.kind AND r.source = s.source AND r.about = s.about AND r.channel = s.channel
		  AND r.form = s.form AND r.bucket15 >= w.a AND r.bucket15 < w.b) x`

// chargeRollups counts the rollup rows the groups served from rollups would
// read against maxSamples, in one statement before any is read: the rows are
// held until the read ends, and a long range of quarter hours is as many rows
// as a dense stream is points.
func (r *aggRun) chargeRollups(groups []*aggGroup) error {
	var streams []*aggStream
	for _, g := range groups {
		if g.rollups {
			streams = append(streams, g.raw...)
		}
	}
	if len(streams) == 0 {
		return nil
	}
	ids := streamKeys(streams, true)
	from, to := spanArrays(r.rowSpans)
	var n int64
	if err := r.tx.QueryRowContext(r.ctx, rollupCountSQL, ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], from, to).Scan(&n); err != nil {
		return fmt.Errorf("substrate/engine: aggregate rollup count: %w", err)
	}
	return r.charge(n, "rollup rows")
}

// wholeQuarterHours reports whether a UTC offset in minutes puts local
// midnight on a rollup row's boundary.
func wholeQuarterHours(offset int64) bool { return offset%int64(bucketWidth/time.Minute) == 0 }

// charge counts n more raw points or rollup rows against maxSamples before
// a statement reads them, and refuses the read once those counted so far (the
// forms' discovery, the rollup rows, the reduction's points, the coverage's)
// pass it. what says what the n are.
func (r *aggRun) charge(n int64, what string) error {
	r.samples += n
	if r.samples <= r.p.maxSamples {
		return nil
	}
	return refuse("aggregate.budget.maxSamples", "the read needs %d raw points and rollup rows (%d of them %s), over %d; narrow the range, use reducers rollups serve (not percentile or durationIn, and no maxGap tighter than the data), or raise maxSamples (the cap is %d)",
		r.samples, n, what, r.p.maxSamples, substrate.AggregateMaxSamplesCap)
}

// rollupable reports whether every reducer of the read merges exactly from a
// group's rollup rows in every bucket.
func (r *aggRun) rollupable(g *aggGroup) bool {
	p := r.p
	if p.forceRaw || len(g.raw) == 0 {
		return false
	}
	if p.policy == substrate.SourcesPriority && len(g.raw) > 1 {
		return false
	}
	for _, red := range p.reducers {
		switch red.Name {
		case substrate.ReducerPercentile, substrate.ReducerDurationIn:
			return false
		}
	}
	if p.bucket.mode == bucketLocal {
		if p.bucket.calendar == "minute" || p.bucket.offset%bucketWidth != 0 {
			return false
		}
		// A value written without an offset is placed by the fallback zone's
		// clock at its own instant, which a row does not keep.
		for _, s := range g.raw {
			if s.offsetless {
				return false
			}
		}
		return aligned15(p.from) && aligned15(p.to)
	}
	if g.form == vocabulary.FormGauge {
		for _, b := range r.buckets {
			if !b.aligned() {
				return false
			}
		}
	}
	return true
}

func aligned15(t time.Time) bool { return t.Truncate(bucketWidth).Equal(t) }

func floor15(t time.Time) time.Time { return t.UTC().Truncate(bucketWidth) }

func ceil15(t time.Time) time.Time {
	f := floor15(t)
	if f.Equal(t) {
		return f
	}
	return f.Add(bucketWidth)
}

// split is a bucket's whole rollup rows and the ends that cut a row.
func split(b *aggBucket) (whole [2]time.Time, frags [][2]time.Time) {
	cs, fe := ceil15(b.start), floor15(b.end)
	if !cs.Before(fe) {
		return [2]time.Time{}, [][2]time.Time{{b.start, b.end}}
	}
	if b.start.Before(cs) {
		frags = append(frags, [2]time.Time{b.start, cs})
	}
	if fe.Before(b.end) {
		frags = append(frags, [2]time.Time{fe, b.end})
	}
	return [2]time.Time{cs, fe}, frags
}

// planSpans joins the rollup rows the read's windows meet, each window from
// the row holding its start to the row holding its end, and the bucket ends
// an amount's or a counter's rollup-served read takes from raw points. A
// recorded-local read has no cut end: its rows land whole in a day.
func (r *aggRun) planSpans() error {
	rows := make([][2]time.Time, 0, len(r.windows))
	for _, w := range r.windows {
		rows = append(rows, [2]time.Time{floor15(w[0]), ceil15(w[1])})
	}
	var err error
	if r.rowSpans, err = r.cover(rows); err != nil {
		return err
	}
	if r.p.bucket.mode == bucketLocal {
		return nil
	}
	var frags [][2]time.Time
	for _, b := range r.buckets {
		_, f := split(b)
		frags = append(frags, f...)
	}
	r.fragments, err = r.cover(frags)
	return err
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
