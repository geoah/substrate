package observed

import (
	"math"
	"math/big"
	"sort"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// Stats is one rollup row: the statistics of one group's points in one
// bucket of one stream, the columns of `observed_rollups`.
type Stats struct {
	// N counts the available points, Bounded those written with a
	// comparator, Values those with a number.
	N, Bounded, Values int64
	// Sum, Min, Max, Mean and M2 are over the numbers; M2 is the sum of
	// squared deviations from the mean, so the variance is M2 over Values.
	Sum, Min, Max, Mean, M2 float64
	// First and Last are the group's first and last available points.
	First, Last *Point
	// CoveredMs is the time the row's points cover: from the first point to
	// the last for a gauge; the union of the spans for an amount, unclipped,
	// since an amount belongs whole to the bucket its span starts in; the
	// time the row's steps span for a counter, its step in included.
	CoveredMs int64
	// HeldMs and TWASum are a gauge's held time inside the bucket under the
	// value's own hold, and the sum of value times held milliseconds, built
	// from every point of the bucket (Held) and kept for the points of this
	// group. A value held past the bucket's end is the read's to carry
	// (HeldFromRows).
	HeldMs int64
	TWASum float64
	// LastHoldMs is how long after Last the gauge's value may hold as far as
	// the bucket knows: the earlier of its own hold and the next point in
	// the bucket. Nil means it holds until the stream's next point.
	LastHoldMs *int64
	// MaxGapMs is the largest gap between consecutive points of the stream
	// in the bucket, any group and unavailable points included, counting the
	// gap from the bucket's start to its first point and from its last point
	// to the bucket's end. StartGapMs is the first of those: where a value
	// held from an earlier bucket stops. Both are the bucket's, the same on
	// every row of it.
	MaxGapMs, StartGapMs int64
	// Increase and Resets are a counter's: the steps whose later point is in
	// the row, the step in from the stream's previous available point
	// included (Increase).
	Increase *big.Rat
	Resets   int64
	// SinSum and CosSum add the sines and cosines of the circular points'
	// angles; Angles counts them.
	SinSum, CosSum float64
	Angles         int64
}

// Bucket computes one bucket [start, end) of one stream: points is every
// point of the stream inside the bucket, in time order, unavailable points
// included, and prev the stream's last available point before start, read
// by a counter for its step in (nil for none, and ignored by the other
// forms). It answers one row per group a point of the bucket belongs to, so
// a bucket holding only unavailable points still has a row, with N zero: the
// read needs to see where a hold stops. A summary has no rollup row.
func Bucket(form string, start, end time.Time, prev *Point, points []Point) map[string]*Stats {
	if form == vocabulary.FormSummary || len(points) == 0 {
		return nil
	}
	out := map[string]*Stats{}
	row := func(g string) *Stats {
		s := out[g]
		if s == nil {
			s = &Stats{}
			out[g] = s
		}
		return s
	}
	values := map[string]*moments{}
	last := map[string]int{}
	for i := range points {
		p := &points[i]
		s := row(p.Group)
		if p.Unavailable {
			continue
		}
		s.N++
		if p.Bounded {
			s.Bounded++
		}
		if s.First == nil {
			s.First = p
		}
		s.Last = p
		last[p.Group] = i
		if p.HasValue {
			s.Values++
			s.Sum += p.Value
			if s.Values == 1 {
				s.Min, s.Max = p.Value, p.Value
			} else {
				s.Min, s.Max = math.Min(s.Min, p.Value), math.Max(s.Max, p.Value)
			}
			m := values[p.Group]
			if m == nil {
				m = &moments{}
				values[p.Group] = m
			}
			m.add(p.Value)
			s.Mean, s.M2 = m.mean, m.m2
		}
		if p.Circular {
			s.SinSum += math.Sin(p.Angle)
			s.CosSum += math.Cos(p.Angle)
			s.Angles++
		}
	}
	maxGap, startGap := gaps(points, start, end)
	for _, s := range out {
		s.MaxGapMs, s.StartGapMs = maxGap, startGap
	}
	switch form {
	case vocabulary.FormGauge:
		for _, iv := range Held(points, start, end, 0) {
			p := points[iv.Point]
			s := out[p.Group]
			d := iv.End.Sub(iv.Start).Milliseconds()
			s.HeldMs += d
			if p.HasValue {
				s.TWASum += p.Value * float64(d)
			}
		}
		for g, i := range last {
			s, p := out[g], points[i]
			s.CoveredMs = s.Last.At.Sub(s.First.At).Milliseconds()
			var bound *time.Duration
			if p.Hold != nil {
				h := *p.Hold
				bound = &h
			}
			if i+1 < len(points) {
				if d := points[i+1].At.Sub(p.At); bound == nil || d < *bound {
					bound = &d
				}
			}
			if bound != nil {
				ms := bound.Milliseconds()
				s.LastHoldMs = &ms
			}
		}
	case vocabulary.FormAmount:
		spans := map[string][][2]time.Time{}
		for _, p := range points {
			if !p.Unavailable {
				spans[p.Group] = append(spans[p.Group], [2]time.Time{p.At, p.End})
			}
		}
		for g, list := range spans {
			out[g].CoveredMs = union(list).Milliseconds()
		}
	case vocabulary.FormCounter:
		for _, s := range out {
			s.Increase = new(big.Rat)
		}
		before := prev
		for i := range points {
			p := &points[i]
			if p.Unavailable {
				continue
			}
			if before != nil {
				s := out[p.Group]
				d, reset := Step(*before, *p)
				s.Increase.Add(s.Increase, d)
				if reset {
					s.Resets++
				}
				s.CoveredMs += p.At.Sub(before.At).Milliseconds()
			}
			before = p
		}
	}
	return out
}

// gaps is a bucket's largest gap between consecutive points (from the
// bucket's start to its first point and from its last point to its end
// included) and the first of them.
func gaps(points []Point, start, end time.Time) (maxGap, startGap int64) {
	startGap = points[0].At.Sub(start).Milliseconds()
	maxGap = startGap
	for i := 1; i < len(points); i++ {
		maxGap = max(maxGap, points[i].At.Sub(points[i-1].At).Milliseconds())
	}
	return max(maxGap, end.Sub(points[len(points)-1].At).Milliseconds()), startGap
}

// union is the length of the union of spans sorted by their start.
func union(spans [][2]time.Time) time.Duration {
	var total time.Duration
	var cur [2]time.Time
	open := false
	for _, s := range spans {
		switch {
		case !open:
			cur, open = s, true
		case !s[0].After(cur[1]):
			if s[1].After(cur[1]) {
				cur[1] = s[1]
			}
		default:
			total += cur[1].Sub(cur[0])
			cur = s
		}
	}
	if open {
		total += cur[1].Sub(cur[0])
	}
	return total
}

// Merge folds b into a: two rows of one group of one stream, b's bucket
// after a's. Every statistic a reducer reads from rows merges without loss:
// counts and sums add, the extremes compare, the first is a's and the last
// is b's, the mean and m2 combine by Chan's formula, a gauge's covered time
// runs from the merged first point to the merged last. The per-bucket
// columns (LastHoldMs, MaxGapMs, StartGapMs) do not merge: the read carries
// a hold across rows in order (HeldFromRows) and judges eligibility per
// bucket (Eligible), so Merge leaves them zero.
func Merge(form string, a, b Stats) Stats {
	out := Stats{
		N: a.N + b.N, Bounded: a.Bounded + b.Bounded, Values: a.Values + b.Values,
		Sum: a.Sum + b.Sum, HeldMs: a.HeldMs + b.HeldMs, TWASum: a.TWASum + b.TWASum,
		Resets: a.Resets + b.Resets, SinSum: a.SinSum + b.SinSum, CosSum: a.CosSum + b.CosSum,
		Angles: a.Angles + b.Angles, First: a.First, Last: b.Last,
	}
	if out.First == nil {
		out.First = b.First
	}
	if out.Last == nil {
		out.Last = a.Last
	}
	switch {
	case a.Values == 0:
		out.Min, out.Max, out.Mean, out.M2 = b.Min, b.Max, b.Mean, b.M2
	case b.Values == 0:
		out.Min, out.Max, out.Mean, out.M2 = a.Min, a.Max, a.Mean, a.M2
	default:
		out.Min, out.Max = math.Min(a.Min, b.Min), math.Max(a.Max, b.Max)
		na, nb := float64(a.Values), float64(b.Values)
		n := na + nb
		d := b.Mean - a.Mean
		out.Mean = a.Mean + d*nb/n
		out.M2 = a.M2 + b.M2 + d*d*na*nb/n
	}
	switch form {
	case vocabulary.FormGauge:
		if out.First != nil {
			out.CoveredMs = out.Last.At.Sub(out.First.At).Milliseconds()
		}
	default:
		out.CoveredMs = a.CoveredMs + b.CoveredMs
	}
	if a.Increase != nil || b.Increase != nil {
		out.Increase = new(big.Rat)
		for _, inc := range []*big.Rat{a.Increase, b.Increase} {
			if inc != nil {
				out.Increase.Add(out.Increase, inc)
			}
		}
	}
	return out
}

// Eligible reports whether a bucket whose largest gap is maxGapMs may answer
// `twa` and `timeInState` from its stored held time for a request with
// maxGap (zero for none): when no gap inside it is longer than maxGap, no
// hold inside it is cut by maxGap, so the held time stored under the
// values' own holds is the request's. Otherwise the read takes the bucket's
// raw points.
func Eligible(maxGap time.Duration, maxGapMs int64) bool {
	return maxGap <= 0 || maxGap.Milliseconds() >= maxGapMs
}

// CircularMeanOf is the circular mean of merged sums (Stats.SinSum,
// Stats.CosSum, Stats.Angles), under the same threshold as CircularMean.
func CircularMeanOf(s Stats) (float64, bool) { return circularMean(s.SinSum, s.CosSum, s.Angles) }

// Row is one rollup row as a read finds it: the bucket it starts and the
// group it holds.
type Row struct {
	Start time.Time
	Group string
	Stats
}

// HeldSum is one group's held time and value-time sum over a window.
type HeldSum struct {
	Ms      int64
	ValueMs float64
}

// HeldFromRows answers, per group, what Held answers from raw points over
// [from, to), from a gauge stream's rollup rows instead: rows are every row
// of the stream (every group) whose bucket starts in [from, to), from and to
// are bucket starts, and carry is the point current at from, the read's one
// lookup into the raw points (nil for none). Each bucket adds its stored
// held time; the value its last point holds past the bucket's end is carried
// to the next bucket's first point (StartGapMs), its own hold, maxGap or to,
// whichever comes first, across buckets with no row. It answers false when a
// row is not Eligible under maxGap, and the read takes raw points instead.
func HeldFromRows(rows []Row, carry *Point, from, to time.Time, maxGap time.Duration) (map[string]HeldSum, bool) {
	out, _, ok := HeldFromRowsCarry(rows, carry, from, to, maxGap)
	return out, ok
}

// HeldFromRowsCarry is HeldFromRows that also answers the value held at to:
// the point whose hold runs past to, its Hold cut to what the rows allow,
// nil where nothing holds. A read walking consecutive buckets hands it to
// the next bucket as that bucket's carry, so one lookup into the raw points
// starts the walk and the rows carry it on.
func HeldFromRowsCarry(rows []Row, carry *Point, from, to time.Time, maxGap time.Duration) (map[string]HeldSum, *Point, bool) {
	out := map[string]HeldSum{}
	add := func(g string, ms int64, valueMs float64) {
		h := out[g]
		h.Ms += ms
		h.ValueMs += valueMs
		out[g] = h
	}
	type held struct {
		group    string
		value    float64
		hasValue bool
		at       time.Time
		limit    *time.Time
	}
	var state *held
	if carry != nil && !carry.Unavailable {
		state = &held{group: carry.Group, value: carry.Value, hasValue: carry.HasValue, at: carry.At}
		if carry.Hold != nil {
			e := carry.At.Add(*carry.Hold)
			state.limit = &e
		}
	}
	// carryTo accounts the held state from cursor to stop.
	cursor := from
	carryTo := func(stop time.Time) {
		if state == nil {
			return
		}
		end := stop
		if state.limit != nil && state.limit.Before(end) {
			end = *state.limit
		}
		if maxGap > 0 {
			if e := state.at.Add(maxGap); e.Before(end) {
				end = e
			}
		}
		if end.After(cursor) {
			ms := end.Sub(cursor).Milliseconds()
			v := 0.0
			if state.hasValue {
				v = state.value * float64(ms)
			}
			add(state.group, ms, v)
		}
	}
	sorted := append([]Row(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	for i := 0; i < len(sorted); {
		start := sorted[i].Start
		j := i
		for j < len(sorted) && sorted[j].Start.Equal(start) {
			j++
		}
		bucket := sorted[i:j]
		i = j
		if start.Before(from) || !start.Before(to) {
			continue
		}
		for _, r := range bucket {
			if !Eligible(maxGap, r.MaxGapMs) {
				return nil, nil, false
			}
		}
		carryTo(start.Add(time.Duration(bucket[0].StartGapMs) * time.Millisecond))
		end := start.Add(BucketWidth)
		state = nil
		for _, r := range bucket {
			add(r.Group, r.HeldMs, r.TWASum)
			if r.N == 0 || r.Last == nil {
				continue
			}
			var limit *time.Time
			if r.LastHoldMs != nil {
				e := r.Last.At.Add(time.Duration(*r.LastHoldMs) * time.Millisecond)
				limit = &e
			}
			if limit == nil || limit.After(end) {
				state = &held{group: r.Group, value: r.Last.Value, hasValue: r.Last.HasValue, at: r.Last.At, limit: limit}
			}
		}
		cursor = end
	}
	carryTo(to)
	if state == nil {
		return out, nil, true
	}
	next := &Point{At: state.at, End: state.at, Value: state.value, HasValue: state.hasValue, Group: state.group}
	if state.limit != nil {
		h := state.limit.Sub(state.at)
		next.Hold = &h
	}
	return out, next, true
}
