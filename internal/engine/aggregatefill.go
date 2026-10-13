package engine

// The aggregate read's last two steps (aggregate.go): `fill` makes each
// group's buckets dense and marks what it made synthetic, then `rolling`
// reduces each value over a window of buckets, filled ones included.

import (
	"context"
	"math"
	"slices"
	"sort"
	"strconv"

	"github.com/geoah/substrate/internal/substrate"
)

// fillAndRoll applies fill and rolling per series (a group and a code) and
// orders the rows by bucket, then group, then code. A fill makes every series
// dense, so the series times the buckets are checked against maxBuckets
// before a row is made, and the rows made never exceed it, the lookback
// buckets' included. The lookback's rows feed the rolling windows and are
// dropped after them; a series is filled from its rows in the range, and a
// lookback row carries nothing into the range, so the rows answered are the
// ones the read answers without rolling. Both loops check ctx as they go.
func (p *aggPlan) fillAndRoll(ctx context.Context, rows []aggOut, buckets []*aggBucket, u *aggUnits, meta *aggMeta) ([]substrate.AggregateRow, error) {
	var steps everyThousand
	type series struct {
		key  string
		rows map[int]*aggOut
	}
	byKey := map[string]*series{}
	var keys []string
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
	}
	sort.Strings(keys)
	if n := len(keys) * len(buckets); p.fill != "" && n > p.maxBuckets {
		return nil, refuse("aggregate.budget.maxBuckets", "fill makes %d series times %d buckets, %d rows, over %d; narrow the filter, widen the bucket or raise maxBuckets (the cap is %d)",
			len(keys), len(buckets), n, p.maxBuckets, substrate.AggregateMaxBucketsCap)
	}
	var out []aggOut
	for _, k := range keys {
		s := byKey[k]
		var template, prev *aggOut
		for bi, b := range buckets {
			if o := s.rows[bi]; o != nil && template == nil && !b.lookback {
				template = o
			}
		}
		for bi, b := range buckets {
			if err := steps.check(ctx); err != nil {
				return nil, err
			}
			if bi > 0 && buckets[bi-1].lookback && !b.lookback {
				prev = nil
			}
			o := s.rows[bi]
			if o == nil && p.fill != "" && template != nil {
				rangeFrom, rangeTo := p.rangeOf(b)
				start, end := b.shown(&b.ends, rangeFrom, rangeTo)
				made := aggOut{bucket: bi, group: template.group, code: template.code, row: substrate.AggregateRow{
					Start: start, End: end, Form: template.row.Form, Group: template.row.Group, Record: b.record,
					Synthetic: true, NonAdditive: template.row.NonAdditive,
				}}
				made.row.Values = p.filled(template, prev)
				o = &made
			}
			if o == nil {
				continue
			}
			out = append(out, *o)
			prev = &out[len(out)-1]
		}
	}
	if len(out) > p.maxBuckets {
		return nil, refuse("aggregate.budget.maxBuckets", "the read makes %d rows, over %d; narrow the filter, widen the bucket or raise maxBuckets (the cap is %d)",
			len(out), p.maxBuckets, substrate.AggregateMaxBucketsCap)
	}
	if p.rolling != nil {
		if err := p.roll(ctx, out, buckets, meta); err != nil {
			return nil, err
		}
	}
	out = slices.DeleteFunc(out, func(o aggOut) bool { return buckets[o.bucket].lookback })
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

// filled is a made row's values. locf carries the previous row's values
// whole, each with its own reducer, unit and code, so a coded last carries
// the previous code and a timeInState the previous row's states, whatever the
// first row of the series held. Otherwise (and before the series' first row)
// the values follow the first row's: a timeInState keeps its state, which
// names the value, while a coded first or last, whose code is its answer,
// answers nothing; zero is "0" where a number answers.
func (p *aggPlan) filled(template, prev *aggOut) []substrate.AggregateValue {
	if p.fill == substrate.FillLOCF && prev != nil {
		return append([]substrate.AggregateValue(nil), prev.row.Values...)
	}
	out := make([]substrate.AggregateValue, 0, len(template.row.Values))
	for _, v := range template.row.Values {
		fv := substrate.AggregateValue{Reducer: v.Reducer, Unit: v.Unit}
		coded := v.Code != "" && (v.Reducer.Name == substrate.ReducerFirst || v.Reducer.Name == substrate.ReducerLast)
		if v.Reducer.Name == substrate.ReducerTimeInState {
			fv.Code = v.Code
		}
		if p.fill == substrate.FillZero && !coded {
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
// is checked before each row.
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
		// them.
		at    map[int]map[key]float64
		whole map[int]bool
	}
	bySeries := map[string]*series{}
	for i, o := range rows {
		k := o.group.sortKey + "\x1e" + o.code
		s := bySeries[k]
		if s == nil {
			s = &series{at: map[int]map[key]float64{}, whole: map[int]bool{}}
			bySeries[k] = s
		}
		s.rows = append(s.rows, i)
		place, lookback := pos[o.bucket], buckets[o.bucket].lookback
		if s.whole[place] && !lookback {
			continue
		}
		values := map[key]float64{}
		for _, v := range o.row.Values {
			if v.Value == nil {
				continue
			}
			if f, err := strconv.ParseFloat(*v.Value, 64); err == nil {
				values[key{v.Reducer.String(), v.Code}] = f
			}
		}
		s.at[place], s.whole[place] = values, lookback
	}
	for _, s := range bySeries {
		for _, i := range s.rows {
			if buckets[rows[i].bucket].lookback {
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
				for _, rr := range p.rolling.Reducers {
					var acc []float64
					for place := here - last - n + 1; place <= here-last; place++ {
						if f, ok := s.at[place][key{of.String(), v.Code}]; ok {
							acc = append(acc, f)
						}
					}
					held := len(acc)
					rv := substrate.AggregateValue{Reducer: rr, Of: &of, Rolling: n, Unit: v.Unit, Code: v.Code, N: &held}
					if r, ok := rollOf(rr.Name, acc); ok {
						str := formatFloat(r)
						rv.Value = &str
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

func rollOf(name string, vs []float64) (float64, bool) {
	if len(vs) == 0 {
		return 0, false
	}
	switch name {
	case substrate.ReducerSum, substrate.ReducerAvg:
		var s float64
		for _, v := range vs {
			s += v
		}
		if name == substrate.ReducerAvg {
			s /= float64(len(vs))
		}
		return s, true
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
		return math.Sqrt(m2 / float64(len(vs))), true
	case substrate.ReducerMin:
		m := math.Inf(1)
		for _, v := range vs {
			m = math.Min(m, v)
		}
		return m, true
	default:
		m := math.Inf(-1)
		for _, v := range vs {
			m = math.Max(m, v)
		}
		return m, true
	}
}
