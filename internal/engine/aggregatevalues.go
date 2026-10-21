package engine

// The aggregate read's rows and values (aggregateread.go): each reducer
// answered over one row's evidence, in the read's unit.

import (
	"context"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// row assembles one output row from its evidence, and answers the one
// statistic a summary row reads (rowStatistic).
func (r *aggRun) row(b *aggBucket, g *aggGroup, code string, ev *aggEvidence, from string) (substrate.AggregateRow, string, error) {
	rangeFrom, rangeTo := r.p.rangeOf(b)
	ends := &ev.ends
	if r.p.sharedEnds {
		ends = &b.ends
	}
	start, end := b.shown(ends, rangeFrom, rangeTo)
	row := substrate.AggregateRow{
		Start: start, End: end, Form: ev.form, Record: b.record, From: from,
		Count: ev.count, Bounded: ev.bounded, Offsetless: ev.offsetless, Prorated: ev.prorated,
		Coverage:    vocabulary.FormatISODuration(time.Duration(ev.coverage) * time.Millisecond),
		NonAdditive: r.p.bucket.mode == bucketRecords,
	}
	if len(r.p.groupBy) > 0 {
		key := g.key
		if r.p.grouped[substrate.GroupCode] {
			key.Code = code
		}
		row.Group = &key
	}
	// A stream that names no source adds no path, as on the latest read.
	for s := range ev.sources {
		if s == "" {
			continue
		}
		row.Sources = append(row.Sources, s)
		if b.answers() {
			r.meta.sources[s] = true
		}
	}
	sort.Strings(row.Sources)
	stat, err := r.rowStatistic(g, ev, b)
	if err != nil {
		return row, "", err
	}
	if row.Values, err = r.values(ev, g, code, stat, start.Location()); err != nil {
		return row, "", err
	}
	if tr := traceOf(r.ctx); tr != nil {
		tr.rows++
	}
	return row, stat, nil
}

// values answers every reducer over one row's evidence, in request order. A
// reducer over a row is as long as the row's points or spans, so the copies
// it reads (taken once per row) and the reducer itself check the context as
// they go (observed's Context forms). A row holding a censored value (a
// comparator, ev.bounded) answers null for every reducer whose answer
// depends on that value's unknown number (censoredNull), and a row a
// censored value holds time in (ev.heldBounded) for those of them that read
// held time (readsHeld); such a row is always served from raw points
// (planGroup, planCarries), since a rollup row keeps the count of its
// censored values and not which they are.
func (r *aggRun) values(ev *aggEvidence, g *aggGroup, code, stat string, zone *time.Location) ([]substrate.AggregateValue, error) {
	v := &rowValues{r: r, ev: ev, g: g, code: code, stat: stat, zone: zone}
	tr := traceOf(r.ctx)
	for _, red := range r.p.reducers {
		if err := r.ctx.Err(); err != nil {
			return nil, err
		}
		if tr != nil && tr.beforeReducer != nil {
			tr.beforeReducer()
		}
		if v.mixes(red) {
			v.out = append(v.out, substrate.AggregateValue{Reducer: red, Unit: r.u.value})
			continue
		}
		if censoredNull[red.Name] && (ev.bounded > 0 || ev.heldBounded > 0 && readsHeld(ev.form, red.Name)) {
			v.out = append(v.out, substrate.AggregateValue{Reducer: red, Unit: v.unitOf(red)})
			continue
		}
		var err error
		switch red.Name {
		case substrate.ReducerCount:
			n := strconv.FormatInt(ev.count, 10)
			v.out = append(v.out, substrate.AggregateValue{Reducer: red, Value: &n})
		case substrate.ReducerAvg:
			if ev.form == vocabulary.FormGauge {
				err = v.timeWeighted(red)
			} else {
				err = v.number(red)
			}
		case substrate.ReducerSum, substrate.ReducerStdDev, substrate.ReducerPercentile, substrate.ReducerCircularMean:
			err = v.number(red)
		case substrate.ReducerMin, substrate.ReducerMax:
			err = v.extreme(red)
		case substrate.ReducerFirst, substrate.ReducerLast:
			err = v.point(red)
		case substrate.ReducerDurationIn, substrate.ReducerTimeInState:
			err = v.held(red)
		case substrate.ReducerIncrease, substrate.ReducerRate:
			v.counter(red)
		}
		if err != nil {
			return nil, err
		}
	}
	return v.out, nil
}

// readsHeld reports whether a reducer reads a row's held time, where a
// value carried in from before the row weighs beside its members: a gauge's
// time-weighted avg, and durationIn.
func readsHeld(form, reducer string) bool {
	return reducer == substrate.ReducerDurationIn || form == vocabulary.FormGauge && reducer == substrate.ReducerAvg
}

// censoredNull are the reducers whose answer depends on the number of every
// value they reduce, so a row holding a censored value (`<0.01`, whose true
// number is unknown) has none: they answer null there. count counts it,
// first and last answer it with its comparator, and min and max answer
// where the bounds decide (observed.CensoredMinPoint); timeInState reads codes.
var censoredNull = map[string]bool{
	substrate.ReducerSum: true, substrate.ReducerAvg: true, substrate.ReducerStdDev: true,
	substrate.ReducerPercentile: true, substrate.ReducerCircularMean: true, substrate.ReducerDurationIn: true,
	substrate.ReducerIncrease: true, substrate.ReducerRate: true,
}

// unitOf is the unit a reducer's value answers in.
func (v *rowValues) unitOf(red substrate.AggregateReducer) string {
	switch red.Name {
	case substrate.ReducerDurationIn:
		return v.r.u.duration
	case substrate.ReducerRate:
		return v.r.u.value + "/s"
	case substrate.ReducerCircularMean:
		if v.r.u.graph != nil {
			return v.r.u.value
		}
		return "deg"
	}
	return v.r.u.value
}

// preferRawStatistic is, for each reducer, the summary statistic that is the
// same quantity as the reducer over raw points.
var preferRawStatistic = map[string]string{
	substrate.ReducerSum: "sum", substrate.ReducerAvg: "avg", substrate.ReducerMin: "min",
	substrate.ReducerMax: "max", substrate.ReducerLast: "last",
}

// mixes reports whether a prefer-raw bucket that fell to summaries holds a
// statistic other than the one the reducer reads over raw points
// (preferRawStatistic), so that one series never answers a day's average as
// its maximum: such a reducer answers null there, and first always does.
// count counts the summaries whatever they hold. A read from summaries
// reduces whatever statistic it selects (checkStatistics).
func (v *rowValues) mixes(red substrate.AggregateReducer) bool {
	if v.r.p.source != substrate.AggregateFromPreferRaw || v.ev.form != vocabulary.FormSummary || red.Name == substrate.ReducerCount {
		return false
	}
	return preferRawStatistic[red.Name] != v.stat
}

// rowValues is one row's reducers being answered: its evidence, the values
// answered so far, and the copy of its points the raw reducers read, taken
// once. A summary row's points are one statistic's (rowStatistic), so
// every reducer reads all of them.
type rowValues struct {
	r    *aggRun
	ev   *aggEvidence
	g    *aggGroup
	code string
	// stat is a summary row's one statistic (rowStatistic).
	stat   string
	points []observed.Point
	copied bool
	out    []substrate.AggregateValue
	// zone is where a downsample's instants are written: the row's start's.
	zone *time.Location
}

// stamp writes, on a downsample, the instant a value was taken at, in the
// row's zone.
func (v *rowValues) stamp(value *substrate.AggregateValue, at *time.Time) {
	if v.r.p.kind != aggDownsample || at == nil {
		return
	}
	t := at.In(v.zone)
	value.At = &t
}

// everything is the window a row's reducers read its points over: the
// points are the row's already.
var everything = observed.Window{From: time.Unix(-1<<40, 0), To: time.Unix(1<<40, 0)}

// pts are the row's raw points.
func (v *rowValues) pts() ([]observed.Point, error) {
	if v.copied {
		return v.points, nil
	}
	list := make([]observed.Point, 0, len(v.ev.members))
	for _, m := range v.ev.members {
		if err := v.r.canceled(); err != nil {
			return nil, err
		}
		list = append(list, m.Point)
	}
	v.points, v.copied = list, true
	return list, nil
}

// float answers a float64 reduction, which makes the read inexact.
func (v *rowValues) float(red substrate.AggregateReducer, f float64, ok bool, unit string) {
	v.r.meta.inexact = true
	v.out = append(v.out, v.r.value(red, f, ok, unit))
}

// number answers sum, avg (on every form but a gauge's), stddev, percentile
// and circularMean: over the raw points, a summary row's included, or from
// the merged rollup rows.
func (v *rowValues) number(red substrate.AggregateReducer) error {
	r, ev := v.r, v.ev
	unit := r.u.value
	var f func(context.Context, []observed.Point, observed.Window) (float64, bool, error)
	var merged float64
	switch red.Name {
	case substrate.ReducerSum:
		f, merged = observed.SumContext, ev.stats.Sum
	case substrate.ReducerAvg:
		f, merged = observed.AvgContext, ev.stats.Mean
	case substrate.ReducerStdDev:
		f, merged = observed.StdDevContext, math.Sqrt(ev.stats.M2/float64(max(ev.stats.Values, 1)))
	case substrate.ReducerPercentile:
		q := *red.Percentile
		f = func(ctx context.Context, list []observed.Point, w observed.Window) (float64, bool, error) {
			return observed.PercentileContext(ctx, list, w, q)
		}
	case substrate.ReducerCircularMean:
		unit, f = "deg", observed.CircularMeanContext
		if r.u.graph != nil {
			// Under the unit graph the mean answers in the read's unit,
			// whose turn the read checked (checkForms).
			unit = r.u.value
		}
	}
	// A percentile never merges from rollup rows (rollupable), so a row
	// asking one is always raw.
	if !ev.raw && red.Name != substrate.ReducerPercentile {
		if red.Name == substrate.ReducerCircularMean {
			m, ok := observed.CircularMeanOf(ev.stats)
			v.float(red, r.fromDegrees(m), ok, unit)
			return nil
		}
		if err := r.checkFinite(red, merged, ev.stats.Values > 0); err != nil {
			return err
		}
		v.float(red, merged, ev.stats.Values > 0, unit)
		return nil
	}
	list, err := v.pts()
	if err != nil {
		return err
	}
	n, ok, err := f(r.ctx, list, everything)
	if err != nil {
		return err
	}
	if red.Name == substrate.ReducerCircularMean {
		n = r.fromDegrees(n)
	}
	if err := r.checkFinite(red, n, ok); err != nil {
		return err
	}
	v.float(red, n, ok, unit)
	return nil
}

// fromDegrees is a circular mean in degrees in the read's unit under the
// unit graph, one full circle being the unit's turn; before it, degrees.
func (r *aggRun) fromDegrees(deg float64) float64 {
	if r.u.graph == nil {
		return deg
	}
	turn := r.u.graph.Turn(r.u.value)
	if turn == nil {
		return deg
	}
	t, _ := turn.Float64()
	return deg * t / 360
}

// checkFinite refuses, under the unit graph, an answer the conversion into
// the read's unit carried past the range of a float, rather than answering
// it as null: the repository's conversions decide the factor, and a factor
// that large is one a reader needs named.
func (r *aggRun) checkFinite(red substrate.AggregateReducer, v float64, ok bool) error {
	if r.u.graph == nil || !ok || !math.IsInf(v, 0) {
		return nil
	}
	return refuse(r.p.path("unit"), "%s of %s converts past the range of a float in %s; read it in a unit nearer the stored ones",
		red.String(), aggMetricName(r.p), r.u.value)
}

// extreme answers a min or a max: compared exactly and answered in the
// winner's digits (converted exactly where the unit's factor allows), not a
// float reduction.
func (v *rowValues) extreme(red substrate.AggregateReducer) error {
	r, ev := v.r, v.ev
	var digits string
	var ok bool
	var at *time.Time
	var comparator string
	switch {
	case ev.raw:
		best := observed.CensoredMinPointContext
		if red.Name == substrate.ReducerMax {
			best = observed.CensoredMaxPointContext
		}
		list, err := r.extremaPoints(ev, v.g)
		if err != nil {
			return err
		}
		var p observed.Point
		if p, comparator, ok, err = best(r.ctx, list, everything); err != nil {
			return err
		}
		digits, at = p.Exact, &p.At
	case ev.bounded > 0:
		// A rollup-served row whose cut end holds a censored value: the
		// merged statistics keep its bound and not its comparator.
	case red.Name == substrate.ReducerMin:
		digits, ok, at = ev.stats.MinExact, ev.stats.Values > 0, ev.extremes[0].at()
	default:
		digits, ok, at = ev.stats.MaxExact, ev.stats.Values > 0, ev.extremes[1].at()
	}
	value := substrate.AggregateValue{Reducer: red, Unit: r.u.value}
	if ok {
		value.Value, value.Comparator = &digits, comparator
		v.stamp(&value, at)
	}
	v.out = append(v.out, value)
	return nil
}

// point answers a first or a last: its code, or its digits in the read's
// unit.
func (v *rowValues) point(red substrate.AggregateReducer) error {
	r, ev := v.r, v.ev
	var p observed.Point
	var ok bool
	switch {
	case ev.raw:
		list, err := v.pts()
		if err != nil {
			return err
		}
		if red.Name == substrate.ReducerFirst {
			p, ok, err = observed.FirstContext(r.ctx, list, everything)
		} else {
			p, ok, err = observed.LastContext(r.ctx, list, everything)
		}
		if err != nil {
			return err
		}
	case red.Name == substrate.ReducerFirst && ev.stats.First != nil:
		p, ok = *ev.stats.First, true
	case red.Name == substrate.ReducerLast && ev.stats.Last != nil:
		p, ok = *ev.stats.Last, true
	}
	value, err := r.pointValue(red, p, ok)
	if err != nil {
		return err
	}
	if ok {
		v.stamp(&value, &p.At)
	}
	v.out = append(v.out, value)
	return nil
}

// timeWeighted answers a gauge's avg, its time-weighted mean (decision
// 0158): each value weighed by the time it holds in the row, the value
// carried in from before the row included, from the raw spans or the held
// sums walked across the rollup rows. A row whose values hold for no time
// (each written with hold PT0S, a blood pressure) answers the mean of its
// values, each weighing one, which is the time-weighted mean of equal holds.
func (v *rowValues) timeWeighted(red substrate.AggregateReducer) error {
	r, ev := v.r, v.ev
	var f float64
	var ok bool
	if ev.raw {
		var err error
		if f, ok, err = observed.TWAOfContext(r.ctx, ev.spans); err != nil {
			return err
		}
	} else {
		var h observed.HeldSum
		for _, x := range ev.held {
			h.Ms += x.Ms
			h.ValueMs += x.ValueMs
		}
		f, ok = h.ValueMs/float64(max(h.Ms, 1)), h.Ms > 0
	}
	if ok {
		v.float(red, f, true, r.u.value)
		return nil
	}
	return v.number(red)
}

// held answers durationIn and timeInState over the row's held time: its raw
// spans, or the held sums walked across its rollup rows.
func (v *rowValues) held(red substrate.AggregateReducer) error {
	r, ev := v.r, v.ev
	switch red.Name {
	case substrate.ReducerDurationIn:
		d, err := observed.DurationInOfContext(r.ctx, ev.spans, red.Between[0], red.Between[1])
		if err != nil {
			return err
		}
		v.out = append(v.out, r.duration(red, d.Milliseconds(), ""))
	default:
		per := map[string]int64{}
		if ev.raw {
			states, err := observed.TimeInStateOfContext(r.ctx, ev.spans)
			if err != nil {
				return err
			}
			for c, d := range states {
				per[c] = d.Milliseconds()
			}
		} else {
			for c, h := range ev.held {
				if c != "" {
					per[c] += h.Ms
				}
			}
		}
		states := red.States
		if len(states) == 0 {
			states = sortedKeys(per)
		}
		for _, st := range states {
			if r.p.grouped[substrate.GroupCode] && st != v.code {
				continue
			}
			one := substrate.AggregateReducer{Name: red.Name, States: []string{st}}
			v.out = append(v.out, r.duration(one, per[st], st))
		}
	}
	return nil
}

// counter answers a counter's increase, exactly, and its rate over the time
// its steps span; both null where the row holds no step.
func (v *rowValues) counter(red substrate.AggregateReducer) {
	r, steps := v.r, v.ev.steps
	if red.Name == substrate.ReducerRate {
		if !steps.any || steps.covered <= 0 {
			v.out = append(v.out, substrate.AggregateValue{Reducer: red, Unit: r.u.value + "/s"})
			return
		}
		f, _ := steps.inc.Float64()
		v.float(red, f/(float64(steps.covered)/1000), true, r.u.value+"/s")
		return
	}
	if !steps.any {
		v.out = append(v.out, substrate.AggregateValue{Reducer: red, Unit: r.u.value})
		return
	}
	digits, exact := vocabulary.RatDecimal(steps.inc, vocabulary.ConvertedDigits)
	if !exact {
		r.meta.inexact = true
	}
	v.out = append(v.out, substrate.AggregateValue{Reducer: red, Value: &digits, Unit: r.u.value})
}

// extremaPoints are the members a raw min or max compares, in the read's
// unit. A counter's members keep their stored digits, because its steps are
// taken on them (convertPoint), so they are converted here, each from its
// own unit, as a rollup row's extremes are (convertStats).
func (r *aggRun) extremaPoints(ev *aggEvidence, g *aggGroup) ([]observed.Point, error) {
	var list []observed.Point
	for _, m := range ev.members {
		if err := r.canceled(); err != nil {
			return nil, err
		}
		p := m.Point
		if ev.form == vocabulary.FormCounter && p.HasValue && p.Unit != "" {
			c, err := r.conversion(p.Unit, m.delta, g.sourceOf(m.stream, ev.form))
			if err != nil {
				return nil, err
			}
			r.convertWith(&p, c)
		}
		list = append(list, p)
	}
	return list, nil
}

// value writes a float answer, or null where the reducer had none.
func (r *aggRun) value(red substrate.AggregateReducer, v float64, ok bool, unit string) substrate.AggregateValue {
	out := substrate.AggregateValue{Reducer: red, Unit: unit}
	if ok && !math.IsNaN(v) && !math.IsInf(v, 0) {
		s := formatFloat(v)
		out.Value = &s
	}
	return out
}

// pointValue writes a first or last: its code, or its digits in the read's
// unit. A counter's raw point arrives in its stored unit (convertPoint).
func (r *aggRun) pointValue(red substrate.AggregateReducer, p observed.Point, ok bool) (substrate.AggregateValue, error) {
	out := substrate.AggregateValue{Reducer: red}
	if !ok {
		out.Unit = r.u.value
		return out, nil
	}
	if !p.HasValue {
		out.Code = p.Code
		return out, nil
	}
	if p.Unit != "" && p.Unit != r.u.value {
		c, err := r.conversion(p.Unit, false, "")
		if err != nil {
			return out, err
		}
		r.convertWith(&p, c)
	}
	digits := p.Exact
	if digits == "" {
		digits = formatFloat(p.Value)
		r.meta.inexact = true
	}
	out.Value, out.Unit, out.Comparator = &digits, r.u.value, p.Comparator
	if p.Code != "" {
		out.Code = p.Code
	}
	return out, nil
}

// duration writes a time in the read's duration unit.
func (r *aggRun) duration(red substrate.AggregateReducer, ms int64, code string) substrate.AggregateValue {
	v, exact := durationIn(ms, r.u.duration)
	if !exact {
		r.meta.inexact = true
	}
	return substrate.AggregateValue{Reducer: red, Value: &v, Unit: r.u.duration, Code: code}
}

// formatFloat writes a float64 answer to 15 significant digits, the digits it
// holds, so float noise (0.30000000000000004) is not reported as data.
func formatFloat(v float64) string {
	r, err := strconv.ParseFloat(strconv.FormatFloat(v, 'g', 15, 64), 64)
	if err != nil {
		r = v
	}
	if r == 0 {
		return "0"
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}
