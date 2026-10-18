package engine

// The aggregate read's last two steps (aggregate.go): `fill` makes each
// group's buckets dense and marks what it made synthetic, then `rolling`
// reduces each value over a window of buckets, filled ones included.

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
)

// fillAndRoll applies fill and rolling per series (a group and a code) and
// orders the rows by bucket, then group, then code. A fill makes every series
// dense, so the series times the buckets answered are checked against
// maxBuckets before a row is made, and the rows answered never exceed it; the
// lookback buckets' rows are held to the cap (checkLookback). The lookback's
// rows feed the rolling windows and are
// dropped after them; a series is filled from its rows in the range, and a
// lookback row carries nothing into the range, so the rows answered are the
// ones the read answers without rolling. Both loops check ctx as they go.
// spent is the samples the read counted before it (aggRun.charge): the
// values a rolling window reads count against maxSamples beside them, before
// any is rolled.
func (p *aggPlan) fillAndRoll(ctx context.Context, rows []aggOut, buckets []*aggBucket, meta *aggMeta, spent int64) ([]substrate.AggregateRow, error) {
	var steps everyThousand
	type series struct {
		key  string
		rows map[int]*aggOut
	}
	byKey := map[string]*series{}
	var keys []string
	// A series answering no row (its values lie only in a lookback or on a
	// day around a range of recorded-local dates) is never filled.
	answering := map[string]bool{}
	groups := map[*aggGroup]bool{}
	for i := range rows {
		o := &rows[i]
		k := o.group.sortKey + "\x1e" + o.code
		s := byKey[k]
		if s == nil {
			s = &series{key: k, rows: map[int]*aggOut{}}
			byKey[k] = s
			keys = append(keys, k)
		}
		s.rows[o.bucket] = o
		if buckets[o.bucket].answers() {
			answering[k], groups[o.group] = true, true
		}
	}
	sort.Strings(keys)
	if err := p.checkAnswered(len(groups), len(answering), answered(buckets)); err != nil {
		return nil, err
	}
	if n := len(answering) * answered(buckets); p.fill != "" && n > p.maxBuckets {
		return nil, refuse("aggregate.budget.maxBuckets", "fill makes %d series times %d buckets, %d rows, over %d; narrow the filter, widen the bucket or raise maxBuckets (the cap is %d)",
			len(answering), answered(buckets), n, p.maxBuckets, substrate.AggregateMaxBucketsCap)
	}
	if p.fill != "" {
		if err := p.checkLookback(len(answering), buckets); err != nil {
			return nil, err
		}
	}
	var out []aggOut
	for _, k := range keys {
		s := byKey[k]
		var template *aggOut
		for bi, b := range buckets {
			if o := s.rows[bi]; o != nil && template == nil && b.answers() {
				template = o
			}
		}
		for bi, b := range buckets {
			if err := steps.check(ctx); err != nil {
				return nil, err
			}
			o := s.rows[bi]
			if o == nil && p.fill != "" && template != nil {
				rangeFrom, rangeTo := p.rangeOf(b)
				start, end := b.shown(&b.ends, rangeFrom, rangeTo)
				made := aggOut{bucket: bi, group: template.group, code: template.code, row: substrate.AggregateRow{
					Start: start, End: end, Form: template.row.Form, Group: template.row.Group, Record: b.record,
					Synthetic: true, NonAdditive: template.row.NonAdditive,
				}}
				made.row.Values = p.filled(template)
				o = &made
			}
			if o == nil {
				continue
			}
			out = append(out, *o)
		}
	}
	made := 0
	for _, o := range out {
		if buckets[o.bucket].answers() {
			made++
		}
	}
	if made > p.maxBuckets {
		return nil, refuse("aggregate.budget.maxBuckets", "the read makes %d rows, over %d; narrow the filter, widen the bucket or raise maxBuckets (the cap is %d)",
			made, p.maxBuckets, substrate.AggregateMaxBucketsCap)
	}
	if p.rolling != nil {
		var work int64
		for _, o := range out {
			if buckets[o.bucket].answers() {
				work += int64(len(o.row.Values) * len(p.rolling.Reducers) * p.rolling.Buckets)
			}
		}
		if spent+work > p.maxSamples {
			return nil, refuse("aggregate.budget.maxSamples", "the rolling windows read %d values (%d rows' values times %d reducers times %d buckets) beside the %d samples the read took, over %d; roll over fewer buckets or reducers, narrow the range or raise maxSamples (the cap is %d)",
				work, made, len(p.rolling.Reducers), p.rolling.Buckets, spent, p.maxSamples, substrate.AggregateMaxSamplesCap)
		}
		if err := p.roll(ctx, out, buckets, meta); err != nil {
			return nil, err
		}
	}
	out = slices.DeleteFunc(out, func(o aggOut) bool { return !buckets[o.bucket].answers() })
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].bucket != out[j].bucket {
			return out[i].bucket < out[j].bucket
		}
		if out[i].group.sortKey != out[j].group.sortKey {
			return out[i].group.sortKey < out[j].group.sortKey
		}
		return out[i].code < out[j].code
	})
	res := make([]substrate.AggregateRow, len(out))
	for i, o := range out {
		res[i] = o.row
	}
	return res, nil
}

// positions are the buckets' places in the sequence a rolling window counts:
// one per start, so a recorded-local bucket the range reaches before its
// first, a lookback bucket and a bucket of the range, is one place.
func positions(buckets []*aggBucket) []int {
	out := make([]int, len(buckets))
	for i, b := range buckets {
		switch {
		case i == 0:
		case b.start.Equal(buckets[i-1].start):
			out[i] = out[i-1]
		default:
			out[i] = out[i-1] + 1
		}
	}
	return out
}

// filled is a made row's values, which follow the series' first row's: a
// timeInState keeps its state, which names the value, while a coded first or
// last, whose code is its answer, answers nothing; every other value is "0".
func (p *aggPlan) filled(template *aggOut) []substrate.AggregateValue {
	out := make([]substrate.AggregateValue, 0, len(template.row.Values))
	for _, v := range template.row.Values {
		fv := substrate.AggregateValue{Reducer: v.Reducer, Unit: v.Unit}
		coded := v.Code != "" && (v.Reducer.Name == substrate.ReducerFirst || v.Reducer.Name == substrate.ReducerLast)
		if v.Reducer.Name == substrate.ReducerTimeInState {
			fv.Code = v.Code
		}
		if !coded {
			zero := "0"
			fv.Value = &zero
		}
		out = append(out, fv)
	}
	return out
}

// roll appends, to each row of the range, every rolling reducer over each of
// its values across a window of places (positions) of the same series: the
// row's own and the Buckets-1 before it, or the Buckets before it where the
// window ends at the previous bucket. A place holds one row's values: where
// a lookback bucket and a bucket of the range share it, the lookback one,
// which holds the bucket's values from both sides of the range's start
// (localBuckets), so the bucket weighs once. A window value is matched by
// its reducer and code, and a null one is skipped; n says how many places
// held one. A row's window is up to the cap's buckets long, so the context
// is checked before each row. Where a group's sources hold different
// statistics, a window whose values come from rows of two is refused, as a
// row taking two is (rowStatistic).
func (p *aggPlan) roll(ctx context.Context, rows []aggOut, buckets []*aggBucket, meta *aggMeta) error {
	pos := positions(buckets)
	n := p.rolling.Buckets
	last := 0
	if p.rolling.Through == substrate.RollingThroughPrevious {
		last = 1
	}
	type key struct{ reducer, code string }
	type series struct {
		rows []int
		// at is each place's values, and whole whether a lookback row set
		// them; stat and sources are the statistic and the sources of the
		// row that set them, kept where the group mixes statistics.
		at      map[int]map[key]rollIn
		whole   map[int]bool
		stat    map[int]string
		sources map[int][]string
	}
	split := ""
	if p.source == substrate.AggregateFromSummaries {
		split = ", or split the series with groupBy statistic"
	}
	// mixed refuses a window whose values of k, from place first to last,
	// come from rows of two statistics, naming each one's sources.
	mixed := func(o aggOut, s *series, k key, first, last int) error {
		if !o.group.mixedStatistics {
			return nil
		}
		by := map[string][]string{}
		for place := first; place <= last; place++ {
			st := s.stat[place]
			if _, ok := s.at[place][k]; !ok || st == "" {
				continue
			}
			if _, ok := by[st]; !ok {
				by[st] = []string{}
			}
			for _, src := range s.sources[place] {
				if !slices.Contains(by[st], src) {
					by[st] = append(by[st], src)
				}
			}
		}
		if len(by) < 2 {
			return nil
		}
		var parts []string
		for _, st := range sortedKeys(by) {
			parts = append(parts, fmt.Sprintf("%s from %s", orNoStatistic(st), strings.Join(by[st], ", ")))
		}
		return refuse("aggregate.rolling", "the window of the row from %s to %s takes the summaries %s, and a rolling value reads one statistic; name one with %s%s",
			o.row.Start.Format(time.RFC3339), o.row.End.Format(time.RFC3339), andList(parts), o.group.statisticAt, split)
	}
	bySeries := map[string]*series{}
	for i, o := range rows {
		k := o.group.sortKey + "\x1e" + o.code
		s := bySeries[k]
		if s == nil {
			s = &series{at: map[int]map[key]rollIn{}, whole: map[int]bool{}, stat: map[int]string{}, sources: map[int][]string{}}
			bySeries[k] = s
		}
		s.rows = append(s.rows, i)
		place, lookback := pos[o.bucket], buckets[o.bucket].lookback
		if s.whole[place] && !lookback {
			continue
		}
		values := map[key]rollIn{}
		for _, v := range o.row.Values {
			if v.Value == nil {
				continue
			}
			if f, err := strconv.ParseFloat(*v.Value, 64); err == nil {
				values[key{v.Reducer.String(), v.Code}] = rollIn{f: f, digits: *v.Value, comparator: v.Comparator}
			}
		}
		s.at[place], s.whole[place] = values, lookback
		if o.group.mixedStatistics {
			s.stat[place], s.sources[place] = o.stat, o.row.Sources
		}
	}
	for _, s := range bySeries {
		for _, i := range s.rows {
			if !buckets[rows[i].bucket].answers() {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			// The window is the places (here-last-n, here-last].
			here := pos[rows[i].bucket]
			base := append([]substrate.AggregateValue(nil), rows[i].row.Values...)
			for _, v := range base {
				of := v.Reducer
				if err := mixed(rows[i], s, key{of.String(), v.Code}, here-last-n+1, here-last); err != nil {
					return err
				}
				for _, rr := range p.rolling.Reducers {
					var acc []rollIn
					for place := here - last - n + 1; place <= here-last; place++ {
						if in, ok := s.at[place][key{of.String(), v.Code}]; ok {
							acc = append(acc, in)
						}
					}
					held := len(acc)
					rv := substrate.AggregateValue{Reducer: rr, Of: &of, Rolling: n, Unit: v.Unit, Code: v.Code, N: &held}
					str, comparator, ok, err := rollOf(ctx, rr.Name, acc)
					if err != nil {
						return err
					}
					if ok {
						rv.Value, rv.Comparator = &str, comparator
						meta.inexact = true
					}
					if of.Name == substrate.ReducerCount {
						rv.Unit = ""
					}
					rows[i].row.Values = append(rows[i].row.Values, rv)
				}
			}
		}
	}
	return nil
}

// rollIn is one value a rolling window reads: its number, its digits and
// the comparator a censored value was answered with.
type rollIn struct {
	f          float64
	digits     string
	comparator string
}

// rollOf reduces a window's values. A window holding a censored value
// answers null for sum, avg and stddev, which need its unknown number, and
// a min or max with a comparator where the bounds decide it and null where
// they do not (observed.CensoredMinPoint), in the winner's digits.
func rollOf(ctx context.Context, name string, in []rollIn) (value, comparator string, ok bool, err error) {
	if len(in) == 0 {
		return "", "", false, nil
	}
	if slices.ContainsFunc(in, func(v rollIn) bool { return v.comparator != "" }) {
		if name != substrate.ReducerMin && name != substrate.ReducerMax {
			return "", "", false, nil
		}
		pts := make([]observed.Point, len(in))
		for i, v := range in {
			pts[i] = observed.Point{HasValue: true, Value: v.f, Exact: v.digits, Comparator: v.comparator}
		}
		best := observed.CensoredMinPointContext
		if name == substrate.ReducerMax {
			best = observed.CensoredMaxPointContext
		}
		p, comparator, ok, err := best(ctx, pts, everything)
		if err != nil || !ok {
			return "", "", false, err
		}
		return p.Exact, comparator, true, nil
	}
	vs := make([]float64, len(in))
	for i, v := range in {
		vs[i] = v.f
	}
	return formatFloat(rollFloat(name, vs)), "", true, nil
}

// rollFloat reduces a window's numbers, none of them censored.
func rollFloat(name string, vs []float64) float64 {
	switch name {
	case substrate.ReducerSum, substrate.ReducerAvg:
		var s float64
		for _, v := range vs {
			s += v
		}
		if name == substrate.ReducerAvg {
			s /= float64(len(vs))
		}
		return s
	case substrate.ReducerStdDev:
		// The population deviation, as the stddev reducer answers it.
		var mean float64
		for _, v := range vs {
			mean += v
		}
		mean /= float64(len(vs))
		var m2 float64
		for _, v := range vs {
			m2 += (v - mean) * (v - mean)
		}
		return math.Sqrt(m2 / float64(len(vs)))
	case substrate.ReducerMin:
		m := math.Inf(1)
		for _, v := range vs {
			m = math.Min(m, v)
		}
		return m
	default:
		m := math.Inf(-1)
		for _, v := range vs {
			m = math.Max(m, v)
		}
		return m
	}
}
