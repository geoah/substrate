package engine

// The aggregate read's last two steps (aggregate.go): `fill` makes each
// group's buckets dense and marks what it made synthetic, then `rolling`
// reduces each value over a window of buckets, filled ones included.

import (
	"context"
	"math"
	"sort"
	"strconv"

	"github.com/geoah/substrate/internal/substrate"
)

// fillAndRoll applies fill and rolling per series (a group and a code) and
// orders the rows by bucket, then group, then code. A fill makes every series
// dense, so the series times the buckets are checked against maxBuckets
// before a row is made, and the rows answered never exceed it. Both loops
// check ctx as they go.
func (p *aggPlan) fillAndRoll(ctx context.Context, rows []aggOut, buckets []*aggBucket, u *aggUnits, meta *aggMeta) ([]substrate.AggregateRow, error) {
	canceled := everyThousand(ctx)
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
		for bi := range buckets {
			if o := s.rows[bi]; o != nil && template == nil {
				template = o
			}
		}
		for bi, b := range buckets {
			if err := canceled(); err != nil {
				return nil, err
			}
			o := s.rows[bi]
			if o == nil && p.fill != "" && template != nil {
				start, end := b.shown(&b.ends, p.from, p.to)
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
		if err := p.roll(ctx, out, meta); err != nil {
			return nil, err
		}
	}
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

// roll appends, to each row, every rolling reducer over each of its values
// across this bucket and the Buckets-1 before it in the same series. A
// window value is matched by its reducer and code, and a null one is skipped.
// A row's window is up to the cap's buckets long, so the context is checked
// before each row.
func (p *aggPlan) roll(ctx context.Context, rows []aggOut, meta *aggMeta) error {
	n := p.rolling.Buckets
	bySeries := map[string][]int{}
	for i, o := range rows {
		k := o.group.sortKey + "\x1e" + o.code
		bySeries[k] = append(bySeries[k], i)
	}
	type key struct{ reducer, code string }
	for _, idx := range bySeries {
		sort.Slice(idx, func(a, b int) bool { return rows[idx[a]].bucket < rows[idx[b]].bucket })
		values := make([]map[key]float64, len(idx))
		for j, i := range idx {
			values[j] = map[key]float64{}
			for _, v := range rows[i].row.Values {
				if v.Value == nil {
					continue
				}
				if f, err := strconv.ParseFloat(*v.Value, 64); err == nil {
					values[j][key{v.Reducer.String(), v.Code}] = f
				}
			}
		}
		for j, i := range idx {
			if err := ctx.Err(); err != nil {
				return err
			}
			base := append([]substrate.AggregateValue(nil), rows[i].row.Values...)
			for _, v := range base {
				of := v.Reducer
				for _, rr := range p.rolling.Reducers {
					var acc []float64
					for w := j; w >= 0 && rows[idx[w]].bucket > rows[i].bucket-n; w-- {
						if f, ok := values[w][key{of.String(), v.Code}]; ok {
							acc = append(acc, f)
						}
					}
					rv := substrate.AggregateValue{Reducer: rr, Of: &of, Rolling: n, Unit: v.Unit, Code: v.Code}
					if r, ok := rollOf(rr.Name, acc); ok {
						s := formatFloat(r)
						rv.Value = &s
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
