package engine

// The aggregate read over the observed index (aggregate.go): the metric read
// and the downsample, which is a metric read in columns.
//
// A read selects streams from observed_latest, groups them, checks every
// reducer against each group's form and the unit against the measure, and
// decides per group where its buckets are served from:
//
//   - observed_rollups, when every reducer merges exactly from rollup rows
//     (internal/observed.Merge) and every gauge row is Eligible under the
//     request's maxGap. A bucket's whole rows are merged after converting each
//     from its own unit; an amount's or a counter's bucket edges that cut a
//     row (the records mode, a range that starts mid-row) are read from raw
//     points and merged in. A gauge's held time walks the rows with
//     HeldFromRowsCarry, so one lookup into the raw points per stream carries
//     a value into the range.
//   - raw observed_points otherwise (percentile, durationIn, a maxGap tighter
//     than the data, a priority over two or more streams, a gauge's bucket
//     that cuts a row), within maxSamples, counted before the read.

import (
	"context"
	"database/sql"
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

// aggStream is one selected stream: its observed_latest row.
type aggStream struct {
	obsStream
	form      string
	unit      string
	code      string
	delta     bool
	value     sql.NullString
	ts, tsEnd time.Time
	statistic sql.NullString
	bounded   bool
	offset    int64
	recordID  string
	revision  sql.NullInt64
	points    int64
	rank      int
}

func (s *aggStream) hold() obsHold { return obsHold{obsStream: s.obsStream, form: s.form} }

// coded reports whether the stream holds codes: a coded gauge holds a code
// instead of a number, a coded amount a code beside its number.
func (s *aggStream) coded() bool { return s.code != "" }

// aggGroup is one group of streams: what a row reduces.
type aggGroup struct {
	key     substrate.AggregateGroup
	sortKey string
	raw     []*aggStream
	sums    []*aggStream
	form    string
	coded   bool
	// rollups says the group's raw buckets are served from observed_rollups.
	rollups bool
}

// aggPoint is one point as the read holds it: the observed point (its value
// converted to the read's unit, except a counter's, whose steps read the
// stored digits) and what the index rows carry beside it.
type aggPoint struct {
	observed.Point
	off    int64
	stat   string
	delta  bool
	stream int
	rid    string
	ord    int
}

// aggSpan is one held interval, with the offset of the point that holds it.
type aggSpan struct {
	observed.Span
	off    int64
	stream int
}

// aggMeta is what a read learns beside its rows.
type aggMeta struct {
	sources map[string]bool
	inexact bool
}

func (ds *dataset) aggregateMetric(ctx context.Context, tx *sql.Tx, p *aggPlan) (*substrate.AggregateResult, error) {
	streams, err := ds.selectStreams(ctx, tx, p)
	if err != nil {
		return nil, err
	}
	units, err := ds.aggregateUnits(ctx, tx, p, streams)
	if err != nil {
		return nil, err
	}
	groups, err := p.groupStreams(streams)
	if err != nil {
		return nil, err
	}
	if err := p.checkForms(groups, units); err != nil {
		return nil, err
	}
	buckets, err := ds.buckets(ctx, tx, p)
	if err != nil {
		return nil, err
	}
	if n := len(buckets) * max(len(groups), 1); n > p.maxBuckets {
		return nil, refuse("aggregate.budget.maxBuckets", "%d groups times %d buckets is %d, over %d; narrow the filter, widen the bucket or raise maxBuckets (the cap is %d)",
			len(groups), len(buckets), n, p.maxBuckets, substrate.AggregateMaxBucketsCap)
	}
	res := &substrate.AggregateResult{
		Metric: p.metric, Unit: units.value, Bucket: p.bucket.echo, From: p.source, Exact: true,
	}
	if p.kind == aggDownsample {
		res.Bucket = p.a.Bucket
	}
	if res.Unit == "" {
		res.Unit = units.duration
	}
	r := &aggRun{ds: ds, ctx: ctx, tx: tx, p: p, u: units, buckets: buckets, meta: aggMeta{sources: map[string]bool{}}}
	if err := r.plan(groups); err != nil {
		return nil, err
	}
	rows, err := r.run(groups)
	if err != nil {
		return nil, err
	}
	res.Rows = p.fillAndRoll(rows, r.buckets, units, &r.meta)
	res.Exact = !r.meta.inexact
	for s := range r.meta.sources {
		res.Sources = append(res.Sources, s)
	}
	sort.Strings(res.Sources)
	return res, nil
}

// --- stream selection ---

// selectStreams reads the streams the metric and the filter select from
// observed_latest, and ranks them for the priority policies.
func (ds *dataset) selectStreams(ctx context.Context, tx *sql.Tx, p *aggPlan) ([]*aggStream, error) {
	if arm := substrate.AggregateFilterArm(p.q.Filter); arm != "" {
		return nil, fmt.Errorf("%w: %s", substrate.ErrValidation, substrate.AggregateFilterArmMessage(arm))
	}
	kinds, err := ds.aggregateKinds(p.q.Filter)
	if err != nil {
		return nil, err
	}
	b := &builder{}
	if kinds != nil {
		b.add(`kind = ANY(` + b.textArray(kinds) + `)`)
	}
	if p.metric != "" {
		b.add(`metric = ` + b.arg(p.metric))
	}
	for _, name := range []string{"metric", "source", "about", "channel"} {
		c, ok := p.q.Filter.Properties[name]
		if !ok {
			continue
		}
		vals, err := condStrings("filter.properties."+name, c)
		if err != nil {
			return nil, err
		}
		b.add(name + ` = ANY(` + b.textArray(vals) + `)`)
	}
	where := "TRUE"
	if len(b.where) > 0 {
		where = strings.Join(b.where, " AND ")
	}
	rows, err := tx.QueryContext(ctx, `SELECT metric, kind, source, about, channel, form, unit, code, delta, value::text,
		ts, ts_end, statistic, bounded, utc_offset_min, record_id, revision, points
		FROM observed_latest WHERE `+where+` ORDER BY metric, kind, source, about, channel`, b.args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate streams: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*aggStream
	for rows.Next() {
		s := &aggStream{}
		if err := rows.Scan(&s.metric, &s.kind, &s.source, &s.about, &s.channel, &s.form, &s.unit, &s.code, &s.delta, &s.value,
			&s.ts, &s.tsEnd, &s.statistic, &s.bounded, &s.offset, &s.recordID, &s.revision, &s.points); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, s := range out {
		s.rank = len(p.order)
		for i, o := range p.order {
			if o == s.source || strings.HasSuffix(s.source, "/"+o) {
				s.rank = i
				break
			}
		}
	}
	return out, nil
}

// aggregateKinds resolves filter.kinds and filter.implements to the observed
// kinds they admit; nil means every kind.
func (ds *dataset) aggregateKinds(f substrate.Filter) ([]string, error) {
	reg := ds.registry()
	var out []string
	if len(f.Kinds) > 0 {
		for _, name := range f.Kinds {
			t, err := reg.Resolve(name)
			if err != nil {
				return nil, fmt.Errorf("%w: filter.kinds: %w", substrate.ErrValidation, err)
			}
			if !bindsObserved(t) {
				return nil, refuse("filter.kinds", "%s does not bind %s, so the observed index holds none of its records", t.Identity, vocabulary.TraitObservedCore)
			}
			out = append(out, t.Identity)
		}
	}
	if f.Implements != "" {
		impl, err := reg.ImplementingStrict(f.Implements)
		if err != nil {
			return nil, fmt.Errorf("%w: filter.implements: %w", substrate.ErrValidation, err)
		}
		admitted := map[string]bool{}
		for _, t := range impl {
			if bindsObserved(t) {
				admitted[t.Identity] = true
			}
		}
		if out == nil {
			for id := range admitted {
				out = append(out, id)
			}
			sort.Strings(out)
			if out == nil {
				out = []string{}
			}
		} else {
			out = slices.DeleteFunc(out, func(id string) bool { return !admitted[id] })
		}
	}
	return out, nil
}

// groupStreams splits the streams into groups: one per value of the groupBy
// stream keys, and under the separate policy one per source as well.
func (p *aggPlan) groupStreams(streams []*aggStream) ([]*aggGroup, error) {
	byKey := map[string]*aggGroup{}
	var out []*aggGroup
	for _, s := range streams {
		summary := s.form == vocabulary.FormSummary
		switch p.source {
		case substrate.AggregateFromRaw:
			if summary {
				continue
			}
		case substrate.AggregateFromSummaries:
			if !summary {
				continue
			}
		}
		var key substrate.AggregateGroup
		var parts []string
		for _, g := range []struct {
			word, value string
			dst         *string
		}{
			{substrate.GroupMetric, s.metric, &key.Metric},
			{substrate.GroupKind, s.kind, &key.Kind},
			{substrate.GroupSource, s.source, &key.Source},
			{substrate.GroupAbout, s.about, &key.About},
			{substrate.GroupChannel, s.channel, &key.Channel},
		} {
			hidden := g.word == substrate.GroupSource && p.policy == substrate.SourcesSeparate
			if p.grouped[g.word] || hidden {
				parts = append(parts, g.value)
				if p.grouped[g.word] {
					*g.dst = g.value
				}
			} else {
				parts = append(parts, "")
			}
		}
		k := strings.Join(parts, "\x1f")
		g := byKey[k]
		if g == nil {
			g = &aggGroup{key: key, sortKey: k}
			byKey[k] = g
			out = append(out, g)
		}
		if summary {
			g.sums = append(g.sums, s)
		} else {
			g.raw = append(g.raw, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sortKey < out[j].sortKey })
	if len(out) > p.maxGroups {
		return nil, refuse("aggregate.budget.maxGroups", "the read makes %d groups, over %d; narrow the filter or group by fewer keys (the cap is %d)",
			len(out), p.maxGroups, substrate.AggregateMaxGroupsCap)
	}
	for _, g := range out {
		order := func(list []*aggStream) {
			sort.SliceStable(list, func(i, j int) bool {
				if list[i].rank != list[j].rank {
					return list[i].rank < list[j].rank
				}
				return streamLess(list[i].obsStream, list[j].obsStream)
			})
		}
		order(g.raw)
		order(g.sums)
		for _, s := range g.raw {
			if g.form == "" {
				g.form = s.form
			} else if s.form != g.form {
				return nil, refuse("aggregate", "the selected streams hold %s and %s values, and a row never mixes forms; group them apart (groupBy kind or source) or narrow the filter",
					g.form, s.form)
			}
			g.coded = g.coded || s.coded()
		}
	}
	return out, nil
}

// --- units ---

// aggUnits is the read's units: the measure and unit of the values, and the
// unit of the durations durationIn and timeInState answer.
type aggUnits struct {
	measure  string
	value    string
	duration string
	conv     map[convKey]*convFactor
}

type convKey struct {
	unit  string
	delta bool
}

// convFactor is the exact map from one stored unit to the read's:
// value = stored * f + o.
type convFactor struct {
	f, o     *big.Rat
	ff, fo   float64
	identity bool
}

// factor is the conversion of a stored unit into the values' unit.
func (u *aggUnits) factor(unit string, delta bool) (*convFactor, error) {
	k := convKey{unit, delta}
	if c, ok := u.conv[k]; ok {
		return c, nil
	}
	c := &convFactor{f: big.NewRat(1, 1), o: new(big.Rat), ff: 1, identity: true}
	if unit != "" && u.value != "" && unit != u.value {
		measure := u.measure
		if delta {
			measure = "temperature-difference"
		}
		zero, err := vocabulary.ConvertIn(measure, new(big.Rat), unit, u.value)
		if err != nil {
			return nil, err
		}
		one, err := vocabulary.ConvertIn(measure, big.NewRat(1, 1), unit, u.value)
		if err != nil {
			return nil, err
		}
		c.o, c.f = zero, new(big.Rat).Sub(one, zero)
		c.ff, _ = c.f.Float64()
		c.fo, _ = c.o.Float64()
		c.identity = false
	}
	if u.conv == nil {
		u.conv = map[convKey]*convFactor{}
	}
	u.conv[k] = c
	return c, nil
}

// digits converts stored digits exactly; exact is false where the result has
// no finite decimal expansion and was rounded.
func (c *convFactor) digits(s string) (string, bool) {
	if c.identity || s == "" {
		return s, true
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return s, false
	}
	r.Mul(r, c.f).Add(r, c.o)
	return vocabulary.RatDecimal(r, vocabulary.ConvertedDigits)
}

// aggregateUnits settles the read's units and refuses a unit outside the
// metric's measure, naming the measure.
func (ds *dataset) aggregateUnits(ctx context.Context, tx *sql.Tx, p *aggPlan, streams []*aggStream) (*aggUnits, error) {
	u := &aggUnits{duration: "s"}
	var metricUnit string
	if p.metric != "" {
		kind, id, _ := vocabulary.SplitRecordPath(p.metric)
		var measures, unit sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT props->>'measures', props->>'unit' FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
			kind, id).Scan(&measures, &unit)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		u.measure, metricUnit = measures.String, unit.String
	}
	// The first stream holding numbers names the measure where the metric
	// declares none; a raw stream before a summary.
	var first *aggStream
	for _, s := range streams {
		if s.unit == "" {
			continue
		}
		if first == nil || (first.form == vocabulary.FormSummary && s.form != vocabulary.FormSummary) {
			first = s
		}
	}
	if u.measure == "" && first != nil {
		m, err := vocabulary.MeasureOf(first.unit)
		if err == nil {
			u.measure = m.Name
			if first.delta && m.Name == "temperature" {
				u.measure = "temperature-difference"
			}
		}
	}
	durations := false
	for _, r := range p.reducers {
		if r.Name == substrate.ReducerDurationIn || r.Name == substrate.ReducerTimeInState {
			durations = true
		}
	}
	if p.unit != "" {
		inMeasure := u.measure != "" && resolves(p.unit, u.measure)
		inDuration := resolves(p.unit, "duration")
		switch {
		case inMeasure:
			u.value = p.unit
			if inDuration {
				u.duration = p.unit
			}
		case inDuration && durations:
			u.duration = p.unit
		case u.measure == "":
			return nil, refuse("aggregate.unit", "%s converts nothing here: the selected streams hold codes, and only a duration unit (beside timeInState) applies", p.unit)
		default:
			return nil, refuse("aggregate.unit", "%s is not a %s unit, the measure of %s; name one of its units, or a duration unit beside durationIn or timeInState",
				p.unit, u.measure, aggMetricName(p))
		}
	}
	if u.value == "" {
		switch {
		case metricUnit != "" && u.measure != "" && resolves(metricUnit, u.measure):
			u.value = metricUnit
		case first != nil:
			u.value = first.unit
		}
	}
	for _, s := range streams {
		if s.unit == "" {
			continue
		}
		if _, err := u.factor(s.unit, s.delta); err != nil {
			return nil, refuse("aggregate.unit", "the stream from %s stores %s, which does not convert to %s: %v", orOwner(s.source), s.unit, u.value, err)
		}
	}
	return u, nil
}

func resolves(unit, measure string) bool {
	_, err := vocabulary.ResolveMeasure(unit, measure)
	return err == nil
}

func aggMetricName(p *aggPlan) string {
	if p.metric == "" {
		return "the selected streams"
	}
	return p.metric
}

func orOwner(source string) string {
	if source == "" {
		return "no source"
	}
	return source
}

// --- what each form takes ---

// formReducers is what each form admits, by name.
var formReducers = map[string][]string{
	"gauge": {
		substrate.ReducerCount, substrate.ReducerAvg, substrate.ReducerTWA, substrate.ReducerMin, substrate.ReducerMax,
		substrate.ReducerFirst, substrate.ReducerLast, substrate.ReducerStdDev, substrate.ReducerPercentile, substrate.ReducerDurationIn,
		substrate.ReducerCircularMean,
	},
	"coded gauge": {substrate.ReducerCount, substrate.ReducerFirst, substrate.ReducerLast, substrate.ReducerTimeInState},
	"amount": {
		substrate.ReducerCount, substrate.ReducerSum, substrate.ReducerAvg, substrate.ReducerMin, substrate.ReducerMax,
		substrate.ReducerFirst, substrate.ReducerLast, substrate.ReducerStdDev, substrate.ReducerPercentile,
	},
	"coded amount": {substrate.ReducerCount, substrate.ReducerSum, substrate.ReducerMin, substrate.ReducerMax},
	"counter": {
		substrate.ReducerCount, substrate.ReducerFirst, substrate.ReducerLast, substrate.ReducerMin, substrate.ReducerMax,
		substrate.ReducerIncrease, substrate.ReducerRate,
	},
	"summary": {
		substrate.ReducerCount, substrate.ReducerFirst, substrate.ReducerLast, substrate.ReducerSum, substrate.ReducerMin,
		substrate.ReducerMax, substrate.ReducerAvg,
	},
}

// formWhy says why a reducer a form refuses does not apply.
var formWhy = map[string]string{
	"gauge":        "a gauge is a level, and levels do not add",
	"coded gauge":  "its values are codes, not numbers",
	"amount":       "an amount accrues over its span and holds nothing after it",
	"coded amount": "a coded amount is reduced by code",
	"counter":      "a counter is a running total, read by its increase",
	"summary":      "a summary is a statistic its source computed",
}

// checkForms refuses a reducer a group's form does not take, naming the form,
// and the circular mean of a value that is not an angle.
func (p *aggPlan) checkForms(groups []*aggGroup, u *aggUnits) error {
	for _, g := range groups {
		var forms []string
		if len(g.raw) > 0 {
			f := g.form
			if g.coded {
				f = "coded " + f
			}
			forms = append(forms, f)
		}
		if len(g.sums) > 0 && p.source != substrate.AggregateFromRaw {
			forms = append(forms, vocabulary.FormSummary)
		}
		if p.policy == substrate.SourcesPriority && g.form == vocabulary.FormCounter && len(g.raw) > 1 {
			return refuse("aggregate.sources.policy", "priority does not resolve two counters' totals per interval; read them separate (stream from %s and %s)",
				orOwner(g.raw[0].source), orOwner(g.raw[1].source))
		}
		for _, form := range forms {
			for i, r := range p.reducers {
				if !slices.Contains(formReducers[form], r.Name) {
					return refuse(fmt.Sprintf("aggregate.reducers[%d]", i), "%s does not apply to %s %s: %s; %s %s takes %s",
						r.String(), article(form), form, formWhy[form], article(form), form, strings.Join(formReducers[form], ", "))
				}
				if r.Name == substrate.ReducerCircularMean && u.measure != "angle" {
					return refuse(fmt.Sprintf("aggregate.reducers[%d]", i), "circularMean averages angles on the circle; %s is a %s",
						aggMetricName(p), orNothing(u.measure))
				}
			}
		}
	}
	return nil
}

func article(word string) string {
	if strings.IndexAny(word[:1], "aeiou") == 0 {
		return "an"
	}
	return "a"
}

func orNothing(measure string) string {
	if measure == "" {
		return "code"
	}
	return measure + " value"
}
