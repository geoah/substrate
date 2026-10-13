package engine

// THE AGGREGATE READ (decision 0161, substrate/aggregate.go).
//
// One request reduces the observed index (decision 0160) or a declared number
// of the filtered kinds into buckets. Everything the request can be refused
// for is checked first: the shape of the body here (planAggregate), then the
// metric reference and the selected streams' forms and units
// (aggregatemetric.go), then the budgets, each before the work it bounds:
// every statement that reads raw points or rollup rows counts them against
// maxSamples before it runs (aggRun.charge). A value in a unit that does not
// convert is refused where the read takes it. The read runs on one
// read-only repeatable-read snapshot under a statement timeout inside the
// caller's deadline, and answers the changelog head it was served at.
//
// The evaluation order is fixed: select streams, build held intervals from
// every transition, resolve the source policy, clip to the range and the
// buckets, filter intervals by code, reduce, fill, roll. internal/observed
// holds the semantics of every step (the held intervals, the reducers, a
// rollup row and its merge, the priority resolution), so a bucket served
// from observed_rollups and one served from raw points answer the same.

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// aggKind is which read a request is.
type aggKind int

const (
	aggMetric aggKind = iota
	aggLatest
	aggProperty
	aggDownsample
)

// aggDownsampleCap bounds a downsample's columns.
const aggDownsampleCap = 10000

// aggRollingCap bounds a rolling window's buckets.
const aggRollingCap = 1000

// aggPlan is a request as the read runs it: every field parsed, every
// default applied.
type aggPlan struct {
	q    substrate.AggregateQuery
	a    substrate.Aggregate
	kind aggKind
	// metric is the metric's record path; empty on a latest read that
	// selects by filter alone.
	metric   string
	from, to time.Time
	// readFrom is where the read takes points from: from, or the start of
	// the buckets before the range a rolling window reads (lookback of them),
	// which the read reduces and does not answer.
	readFrom time.Time
	lookback int
	hasRange bool
	bucket   bucketSpec
	reducers []substrate.AggregateReducer
	groupBy  []string
	grouped  map[string]bool
	policy   string
	order    []string
	source   string
	// minCoverage is prefer-raw's floor.
	minCoverage float64
	maxGap      time.Duration
	fill        string
	rolling     *substrate.AggregateRolling
	maxBuckets  int
	maxGroups   int
	maxSamples  int64
	points      int
	unit        string
	// codes is filter.properties.code: the codes intervals are filtered to
	// after they are built.
	codes map[string]bool
	// forceRaw reads raw points where rollups could serve (the tests'
	// lever; aggregateForceRaw).
	forceRaw bool
	// propBySource says property.sources' priority policy chooses among the
	// property read's records.
	propBySource bool
	// metrics are a several-metric read's metrics, each a plan of its own
	// over the read's range, bucket, groupBy and sources (forMetric); nil on
	// any other read.
	metrics []*aggPlan
	// at is where the plan's metric options are named in a refusal
	// (aggregate, or aggregate.metrics[i] on a several-metric read), refAt
	// its metric's reference and reducersAt its reducers.
	at, refAt, reducersAt string
	// statistic is the one summary statistic a several-metric read's metric
	// reads its summaries at; "" reads every statistic the filter admits.
	statistic string
	// sharedEnds writes a recorded-local row at its bucket's bounds, which
	// every metric of a several-metric read marks with the values it takes
	// (markEnds), so the metrics' rows of one day carry one start and one
	// end.
	sharedEnds bool
}

// path names one of the plan's metric options in a refusal.
func (p *aggPlan) path(option string) string { return p.at + "." + option }

// isEntry reports whether the plan is one metric's of a several-metric read
// (forMetric).
func (p *aggPlan) isEntry() bool { return p.at != "aggregate" }

// refuse refuses what the plan's metric does not admit. On a several-metric
// read a refusal whose path is a read-wide option or budget opens with the
// metric's entry and reference, since the path alone does not say which
// metric fails it.
func (p *aggPlan) refuse(path, format string, args ...any) error {
	if p.isEntry() && path != p.at && !strings.HasPrefix(path, p.at+".") {
		format, args = "in %s (%s), "+format, append([]any{p.at, p.metric}, args...)
	}
	return refuse(path, format, args...)
}

// statisticPath is where a read names the one summary statistic a metric's
// summaries are read at: the entry's statistic, or the filter on a read of
// one metric.
func (p *aggPlan) statisticPath() string {
	if p.isEntry() {
		return p.path("statistic")
	}
	return "filter.properties.statistic"
}

// reducerPath names the plan's i-th reducer in a refusal.
func (p *aggPlan) reducerPath(i int) string { return fmt.Sprintf("%s[%d]", p.reducersAt, i) }

// refuse is a validation refusal naming what it refuses.
func refuse(path, format string, args ...any) error {
	return &substrate.ValidationError{Problems: []string{path + ": " + fmt.Sprintf(format, args...)}}
}

// aggregateRawKey is the context key aggregateForceRaw sets.
type aggregateRawKey struct{}

// aggregateTraceKey is the context key a test's aggTrace rides under.
type aggregateTraceKey struct{}

// aggTrace counts what a read took from each table, for the tests to tell a
// rollup-served answer from a raw one.
type aggTrace struct {
	rollupRows, rawPoints int64
	// mergedRows is the rollup rows a reduction merged.
	mergedRows int64
	// afterPlan runs once a metric read's plan has read every row and point
	// it reduces, before the reduction: a test cancels the read there.
	afterPlan func()
	// beforeReducer runs as each reducer of a row begins, after the read's
	// last check of its context before it: a test cancels the read there, so
	// only the reducer's own checks can see it.
	beforeReducer func()
	// rows is the rows a metric read assembled, every reducer answered.
	rows int64
}

// everyThousand counts a Go-side loop's steps and answers the context's
// error on the first step and on every thousandth after. The statement
// timeout bounds the SQL, not the work after it, so a long loop stops when
// its caller does.
type everyThousand int

func (n *everyThousand) check(ctx context.Context) error {
	i := *n
	*n++
	if i&(1<<10-1) != 0 {
		return nil
	}
	return ctx.Err()
}

func traceOf(ctx context.Context) *aggTrace {
	t, _ := ctx.Value(aggregateTraceKey{}).(*aggTrace)
	return t
}

// aggregateForceRaw reports whether the caller asked every bucket be read from
// raw points. Only the engine's tests set it (export_test.go), to hold a
// rollup answer against the raw one.
func aggregateForceRaw(ctx context.Context) bool {
	v, _ := ctx.Value(aggregateRawKey{}).(bool)
	return v
}

func (ds *dataset) Aggregate(ctx context.Context, q substrate.AggregateQuery) (*substrate.AggregateResult, error) {
	p, err := planAggregate(q)
	if err != nil {
		return nil, err
	}
	p.forceRaw = aggregateForceRaw(ctx)
	for _, m := range p.metrics {
		m.forceRaw = p.forceRaw
	}
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := aggregateTimeout(ctx, tx); err != nil {
		return nil, err
	}
	var head sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT max(seq) FROM changelog`).Scan(&head); err != nil {
		return nil, err
	}
	for _, m := range append([]*aggPlan{p}, p.metrics...) {
		if m.metric == "" {
			continue
		}
		if err := ds.checkMetric(ctx, tx, m.refAt, m.metric); err != nil {
			return nil, err
		}
	}
	var res *substrate.AggregateResult
	switch p.kind {
	case aggLatest:
		res, err = ds.aggregateLatest(ctx, tx, p)
	case aggProperty:
		res, err = ds.aggregateProperty(ctx, tx, p)
	default:
		res, err = ds.aggregateMetric(ctx, tx, p)
	}
	if err != nil {
		return nil, err
	}
	if p.kind != aggProperty {
		pending, err := ds.observedPending(ctx, tx, p.q.Filter)
		if err != nil {
			return nil, err
		}
		if pending {
			res.Exact = false
		}
	}
	res.Head = substrate.AggregateHead{Changelog: head.Int64, Generation: ds.historyGeneration()}
	if res.Rows == nil {
		res.Rows = []substrate.AggregateRow{}
	}
	return res, nil
}

// observedPending reports whether the background pass still owes the
// observed index of a kind the filter admits (reprojection.go): until it
// reaches a record, the index holds the points the record's previous
// declaration derived, so the answer is not the one the records give, and
// the read says so with exact false.
func (ds *dataset) observedPending(ctx context.Context, tx *sql.Tx, f substrate.Filter) (bool, error) {
	kinds, err := ds.aggregateKinds(f)
	if err != nil {
		return false, err
	}
	var pending bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM index_reprojections
		WHERE observed AND ($1::text[] IS NULL OR kind = ANY($1::text[])))`, kinds).Scan(&pending)
	if err != nil {
		return false, fmt.Errorf("substrate/engine: aggregate: read the observed re-derivations owed: %w", err)
	}
	return pending, nil
}

// aggregateTimeout bounds every statement of the read inside the caller's
// remaining deadline, and inside AggregateTimeout where the caller set none
// or a later one.
func aggregateTimeout(ctx context.Context, tx *sql.Tx) error {
	limit := substrate.AggregateTimeout
	if dl, ok := ctx.Deadline(); ok {
		if rest := time.Until(dl); rest < limit {
			limit = rest
		}
	}
	ms := max(limit.Milliseconds(), 1)
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`SET LOCAL statement_timeout = %d`, ms))
	return err
}

// planAggregate checks the body's shape and parses it. Nothing here reads the
// repository: what the selected streams admit is checked once they are
// selected.
func planAggregate(q substrate.AggregateQuery) (*aggPlan, error) {
	if q.Aggregate == nil {
		return nil, refuse("aggregate", "the read needs an aggregate body: a metric or a property, a range and reducers")
	}
	a := *q.Aggregate
	p := &aggPlan{q: q, a: a, grouped: map[string]bool{}, at: "aggregate", refAt: "aggregate.metric.ref", reducersAt: "aggregate.reducers"}
	selectors := 0
	for _, set := range []bool{a.Metric != nil, a.Metrics != nil, a.Property != nil} {
		if set {
			selectors++
		}
	}
	if selectors > 1 {
		return nil, refuse("aggregate", "name one of metric, metrics and property")
	}
	if a.Range != nil {
		from, err := time.Parse(time.RFC3339Nano, a.Range.Gte)
		if err != nil {
			return nil, refuse("aggregate.range.gte", "%q is not an RFC 3339 instant", a.Range.Gte)
		}
		to, err := time.Parse(time.RFC3339Nano, a.Range.Lt)
		if err != nil {
			return nil, refuse("aggregate.range.lt", "%q is not an RFC 3339 instant", a.Range.Lt)
		}
		if !from.Before(to) {
			return nil, refuse("aggregate.range", "gte %s is not before lt %s", a.Range.Gte, a.Range.Lt)
		}
		p.from, p.to, p.hasRange = from, to, true
	}
	if a.Metric != nil {
		if _, _, ok := vocabulary.SplitRecordPath(a.Metric.Ref); !ok {
			return nil, refuse("aggregate.metric.ref", "%q is not a record path, <authority>/<package>/<kind>/<id>", a.Metric.Ref)
		}
		p.metric = a.Metric.Ref
	}
	if err := p.parseReducers(); err != nil {
		return nil, err
	}
	if err := p.parseCommon(); err != nil {
		return nil, err
	}
	switch {
	case q.Downsample != nil:
		p.kind = aggDownsample
		if err := p.parseDownsample(); err != nil {
			return nil, err
		}
	case a.Property != nil:
		p.kind = aggProperty
		if err := p.parseProperty(); err != nil {
			return nil, err
		}
	case a.Metrics == nil && a.Bucket == nil && !p.hasRange && len(p.reducers) == 1 && p.reducers[0].Name == substrate.ReducerLast:
		p.kind = aggLatest
		if err := p.parseLatest(); err != nil {
			return nil, err
		}
		return p, nil
	default:
		p.kind = aggMetric
		if p.metric == "" && a.Metrics == nil {
			return nil, refuse("aggregate.metric", `name the metric, {"ref": "<authority>/observations/metric/<id>"}, several in metrics, or a property; only the latest read (no bucket, no range, the one reducer last) selects by filter alone`)
		}
	}
	if !p.hasRange {
		if a.Metrics != nil {
			return nil, refuse("aggregate.range", `a several-metric read is bucketed and needs a range, {"gte": "<instant>", "lt": "<instant>"}; the latest read selects several metrics with filter.properties.metric`)
		}
		return nil, refuse("aggregate.range", `a bucketed read needs a range, {"gte": "<instant>", "lt": "<instant>"}; only the latest read takes none`)
	}
	if len(p.reducers) == 0 && p.kind != aggDownsample && a.Metrics == nil {
		return nil, refuse("aggregate.reducers", "name at least one reducer, such as avg")
	}
	if err := p.parseBucket(); err != nil {
		return nil, err
	}
	if p.kind == aggMetric || p.kind == aggDownsample {
		if err := p.parseCodes(); err != nil {
			return nil, err
		}
		if a.Metrics != nil {
			if err := p.parseMetrics(); err != nil {
				return nil, err
			}
		} else if err := p.parseMetricOptions(); err != nil {
			return nil, err
		}
	}
	if err := p.planLookback(); err != nil {
		return nil, err
	}
	for _, m := range p.metrics {
		m.readFrom, m.lookback = p.readFrom, p.lookback
	}
	return p, nil
}

// parseMetrics makes a several-metric read's plans: one per metric, over the
// read's range, bucket, groupBy and sources, each with its own unit,
// reducers, from and statistic and grouped by metric, so every row names
// its metric. Each metric's options are checked here on their own, and its
// forms and units once its streams are selected, before any point is read.
func (p *aggPlan) parseMetrics() error {
	a := p.a
	switch {
	case len(a.Metrics) == 0:
		return refuse("aggregate.metrics", `name at least one metric, {"ref": "<authority>/observations/metric/<id>"}`)
	case len(a.Metrics) > substrate.AggregateMaxMetrics:
		return refuse("aggregate.metrics", "a read names at most %d metrics, not %d; read the rest in another request", substrate.AggregateMaxMetrics, len(a.Metrics))
	case p.unit != "":
		return refuse("aggregate.unit", "a unit converts one metric's values; name it in each aggregate.metrics entry")
	}
	preferRaw := false
	for i, m := range a.Metrics {
		at := fmt.Sprintf("aggregate.metrics[%d]", i)
		if _, _, ok := vocabulary.SplitRecordPath(m.Ref); !ok {
			return refuse(at+".ref", "%q is not a record path, <authority>/<package>/<kind>/<id>", m.Ref)
		}
		for j := range i {
			if a.Metrics[j].Ref == m.Ref {
				return refuse(at+".ref", "%s is named at aggregate.metrics[%d] too; read a metric once, and split its summaries' statistics with groupBy statistic", m.Ref, j)
			}
		}
		mp, err := p.forMetric(at, m)
		if err != nil {
			return err
		}
		preferRaw = preferRaw || mp.source == substrate.AggregateFromPreferRaw
		p.metrics = append(p.metrics, mp)
	}
	if a.MinCoverage != nil && !preferRaw {
		return refuse("aggregate.minCoverage", "minCoverage is prefer-raw's floor, and no metric of this read is from prefer-raw")
	}
	return nil
}

// forMetric is the plan of one metric of a several-metric read: the read's
// plan with the metric's own options, grouped by metric.
func (p *aggPlan) forMetric(at string, m substrate.AggregateMetricRead) (*aggPlan, error) {
	mp := *p
	mp.metrics, mp.sharedEnds = nil, true
	mp.metric, mp.at, mp.refAt, mp.statistic = m.Ref, at, at+".ref", m.Statistic
	mp.grouped = maps.Clone(p.grouped)
	mp.groupBy = slices.Clone(p.groupBy)
	if !mp.grouped[substrate.GroupMetric] {
		mp.grouped[substrate.GroupMetric] = true
		mp.groupBy = append(mp.groupBy, substrate.GroupMetric)
	}
	if m.Reducers != nil {
		mp.reducersAt, mp.reducers = at+".reducers", nil
		for i, r := range m.Reducers {
			if err := checkReducer(mp.reducerPath(i), r); err != nil {
				return nil, err
			}
			mp.reducers = append(mp.reducers, r)
		}
	}
	if len(mp.reducers) == 0 {
		return nil, refuse(at+".reducers", "name at least one reducer, such as avg, here or in aggregate.reducers for every metric")
	}
	if m.Unit != "" {
		code, err := vocabulary.CanonicalUnit(m.Unit)
		if err != nil {
			return nil, refuse(at+".unit", "%v", err)
		}
		mp.unit = code
	}
	from := m.From
	if from == "" {
		from = p.a.From
	}
	if err := mp.parseFrom(from); err != nil {
		return nil, err
	}
	if mp.statistic != "" && mp.source == substrate.AggregateFromRaw {
		return nil, refuse(at+".statistic", "a statistic narrows the metric's summaries, and from raw reads none; read it from summaries or prefer-raw, or leave statistic out")
	}
	if err := mp.parseMetricOptions(); err != nil {
		return nil, err
	}
	return &mp, nil
}

// planLookback sets where the read takes points from: a rolling window over
// the range's first buckets reads the buckets before it, buckets-1 of them,
// or buckets where the window ends at the previous bucket, so a bucket's
// rolling value does not depend on where the range starts.
func (p *aggPlan) planLookback() error {
	p.readFrom = p.from
	if p.rolling == nil {
		return nil
	}
	switch p.bucket.mode {
	case bucketRange:
		return refuse("aggregate.rolling", "a rolling window rolls over a sequence of buckets; name a bucket")
	case bucketRecords:
		return nil
	}
	p.lookback = p.rolling.Buckets - 1
	if p.rolling.Through == substrate.RollingThroughPrevious {
		p.lookback = p.rolling.Buckets
	}
	if p.lookback == 0 {
		return nil
	}
	s := p.bucket
	switch s.mode {
	case bucketEvery:
		p.readFrom = everyStart(p.from, s.origin, s.every).Add(-time.Duration(p.lookback) * s.every)
	case bucketCalendar:
		p.readFrom = calStep(calStart(p.from, s), s, -p.lookback)
	case bucketLocal:
		// The instant the range's own offset reads the lookback's first
		// wall-clock bucket start at.
		wall := wallOf(p.from, offsetMinutes(p.from))
		p.readFrom = p.from.Add(calStep(calStart(wall, s), s, -p.lookback).Sub(wall))
	}
	return nil
}

// parseReducers checks every reducer's name and argument.
func (p *aggPlan) parseReducers() error {
	for i, r := range p.a.Reducers {
		if err := checkReducer(p.reducerPath(i), r); err != nil {
			return err
		}
		p.reducers = append(p.reducers, r)
	}
	return nil
}

func checkReducer(path string, r substrate.AggregateReducer) error {
	if !slices.Contains(substrate.AggregateReducers, r.Name) {
		return refuse(path, "%q is not a reducer; the reducers are %s", r.Name, strings.Join(substrate.AggregateReducers, ", "))
	}
	switch r.Name {
	case substrate.ReducerPercentile:
		if r.Percentile == nil {
			return refuse(path, `percentile takes its q, {"percentile": 0.05}`)
		}
		if q := *r.Percentile; q < 0 || q > 1 || math.IsNaN(q) {
			return refuse(path, "percentile's q is between 0 and 1, not %v", q)
		}
	case substrate.ReducerDurationIn:
		if len(r.Between) != 2 {
			return refuse(path, `durationIn takes [lo, hi), two numbers in the values' unit: {"durationIn": [95, 114]}`)
		}
		if !(r.Between[0] < r.Between[1]) {
			return refuse(path, "durationIn's lo %v is not below its hi %v", r.Between[0], r.Between[1])
		}
	}
	return nil
}

// parseCommon reads the options every read shares: groupBy, sources, from,
// maxGap, fill, rolling, unit and the budgets.
func (p *aggPlan) parseCommon() error {
	a := p.a
	for i, g := range a.GroupBy {
		if !slices.Contains(substrate.AggregateGroupBy, g) {
			return refuse(fmt.Sprintf("aggregate.groupBy[%d]", i), "%q is not a group; group by %s", g, strings.Join(substrate.AggregateGroupBy, ", "))
		}
		if p.grouped[g] {
			return refuse(fmt.Sprintf("aggregate.groupBy[%d]", i), "%q is named twice", g)
		}
		p.grouped[g] = true
		p.groupBy = append(p.groupBy, g)
	}
	p.policy = substrate.SourcesSeparate
	if a.Sources != nil {
		switch a.Sources.Policy {
		case "", substrate.SourcesSeparate:
			if len(a.Sources.Order) > 0 {
				return refuse("aggregate.sources.order", "an order ranks sources for the priority and summaries policies; separate keeps every source apart")
			}
		case substrate.SourcesPriority, substrate.SourcesSummaries:
			p.policy = a.Sources.Policy
			p.order = a.Sources.Order
		default:
			return refuse("aggregate.sources.policy", "%q is not a policy; the policies are separate, priority and summaries", a.Sources.Policy)
		}
	}
	if err := p.parseFrom(a.From); err != nil {
		return err
	}
	p.minCoverage = 0.9
	if a.MinCoverage != nil {
		// A several-metric read takes it where any metric reads prefer-raw
		// (parseMetrics).
		if p.source != substrate.AggregateFromPreferRaw && a.Metrics == nil {
			return refuse("aggregate.minCoverage", "minCoverage is prefer-raw's floor; this read is from %s", p.source)
		}
		if c := *a.MinCoverage; c < 0 || c > 1 || math.IsNaN(c) {
			return refuse("aggregate.minCoverage", "a coverage is between 0 and 1, not %v", c)
		}
		p.minCoverage = *a.MinCoverage
	}
	if a.MaxGap != "" {
		d, err := vocabulary.ParseISODuration(a.MaxGap)
		if err != nil {
			return refuse("aggregate.maxGap", "%v", err)
		}
		if d <= 0 {
			return refuse("aggregate.maxGap", "a gap is longer than zero")
		}
		p.maxGap = d
	}
	switch a.Fill {
	case "", substrate.FillNull, substrate.FillZero, substrate.FillLOCF:
		p.fill = a.Fill
	default:
		return refuse("aggregate.fill", "%q is not a fill; fill is null, zero or locf", a.Fill)
	}
	if r := a.Rolling; r != nil {
		if r.Buckets < 1 || r.Buckets > aggRollingCap {
			return refuse("aggregate.rolling.buckets", "a rolling window is 1 to %d buckets, not %d", aggRollingCap, r.Buckets)
		}
		rolled := *r
		if len(rolled.Reducers) == 0 {
			rolled.Reducers = []substrate.AggregateReducer{{Name: substrate.ReducerAvg}}
		}
		for i, rr := range rolled.Reducers {
			switch rr.Name {
			case substrate.ReducerAvg, substrate.ReducerSum, substrate.ReducerMin, substrate.ReducerMax, substrate.ReducerStdDev:
			default:
				return refuse(fmt.Sprintf("aggregate.rolling.reducers[%d]", i), "a rolling window takes avg, sum, min, max or stddev, not %s", rr.String())
			}
		}
		switch rolled.Through {
		case "", substrate.RollingThroughCurrent:
			rolled.Through = substrate.RollingThroughCurrent
		case substrate.RollingThroughPrevious:
		default:
			return refuse("aggregate.rolling.through", "%q is not a window's end; a window runs through the current bucket or the previous one", rolled.Through)
		}
		p.rolling = &rolled
	}
	if a.Unit != "" {
		code, err := vocabulary.CanonicalUnit(a.Unit)
		if err != nil {
			return refuse(p.path("unit"), "%v", err)
		}
		p.unit = code
	}
	p.maxBuckets, p.maxGroups, p.maxSamples = substrate.AggregateMaxBuckets, substrate.AggregateMaxGroups, substrate.AggregateMaxSamples
	if b := a.Budget; b != nil {
		for _, x := range []struct {
			path   string
			n, cap int64
		}{
			{"aggregate.budget.maxBuckets", int64(b.MaxBuckets), substrate.AggregateMaxBucketsCap},
			{"aggregate.budget.maxSamples", b.MaxSamples, substrate.AggregateMaxSamplesCap},
			{"aggregate.budget.maxGroups", int64(b.MaxGroups), substrate.AggregateMaxGroupsCap},
		} {
			if err := checkBudget(x.path, x.n, x.cap); err != nil {
				return err
			}
		}
		if b.MaxBuckets > 0 {
			p.maxBuckets = b.MaxBuckets
		}
		if b.MaxSamples > 0 {
			p.maxSamples = b.MaxSamples
		}
		if b.MaxGroups > 0 {
			p.maxGroups = b.MaxGroups
		}
	}
	return nil
}

// parseFrom reads which points the plan takes: from, or under the summaries
// policy the summaries.
func (p *aggPlan) parseFrom(from string) error {
	p.source = substrate.AggregateFromRaw
	switch from {
	case "", substrate.AggregateFromRaw:
	case substrate.AggregateFromSummaries, substrate.AggregateFromPreferRaw:
		p.source = from
	default:
		return refuse(p.path("from"), "%q is not a source of points; from is raw, summaries or prefer-raw", from)
	}
	if p.policy == substrate.SourcesSummaries {
		if from != "" && from != substrate.AggregateFromSummaries {
			return refuse(p.path("from"), "the summaries policy reads summaries; from %s contradicts it", from)
		}
		p.source = substrate.AggregateFromSummaries
	}
	return nil
}

// checkBudget refuses a budget below zero or over its cap; zero is the
// default.
func checkBudget(path string, n, limit int64) error {
	switch {
	case n < 0:
		return refuse(path, "%d is negative; leave it out for the default, or name 1 to %d", n, limit)
	case n > limit:
		return refuse(path, "%d is over the cap of %d", n, limit)
	}
	return nil
}

// parseCodes reads filter.properties.code, the codes a metric read filters
// intervals to after building them from every transition.
func (p *aggPlan) parseCodes() error {
	code, ok := p.q.Filter.Properties["code"]
	if !ok {
		return nil
	}
	codes, err := condStrings("filter.properties.code", code)
	if err != nil {
		return err
	}
	p.codes = map[string]bool{}
	for _, c := range codes {
		p.codes[c] = true
	}
	return nil
}

// parseDownsample turns a downsample into the metric read it is: columns of
// one width over the range, each answering first, last, min and max.
func (p *aggPlan) parseDownsample() error {
	d := p.q.Downsample
	switch {
	case p.metric == "":
		return refuse("aggregate.metric", "a downsample reads one metric; name it")
	case !p.hasRange:
		return refuse("aggregate.range", "a downsample cuts a range into columns; name it")
	case len(p.a.Reducers) > 0:
		return refuse("downsample", "refused beside aggregate.reducers: a downsample answers first, last, min and max per column")
	case p.a.Rolling != nil:
		return refuse("downsample", "refused beside aggregate.rolling: a downsample's columns are never rolled")
	case p.a.Fill != "":
		return refuse("downsample", "refused beside aggregate.fill: a downsample's columns are never filled")
	case p.a.Bucket != nil:
		return refuse("downsample", "refused beside aggregate.bucket: the columns are the range cut into points")
	case d.Method != "" && d.Method != "m4":
		return refuse("downsample.method", "%q is not a method; the one method is m4", d.Method)
	case d.Points < 1 || d.Points > aggDownsampleCap:
		return refuse("downsample.points", "a downsample takes 1 to %d points, not %d", aggDownsampleCap, d.Points)
	}
	p.points = d.Points
	// A column at least a rollup row wide is a run of whole rows; a narrower
	// one reads raw points. Aligning moves the origin back to a quarter hour,
	// so the width is taken over the aligned extent: the columns from the
	// aligned origin to the range's end are never more than points.
	perColumn := func(span time.Duration) time.Duration {
		return (span + time.Duration(d.Points) - 1) / time.Duration(d.Points)
	}
	width := perColumn(p.to.Sub(p.from))
	origin := p.from
	if width >= bucketWidth {
		origin = p.from.UTC().Truncate(bucketWidth)
		width = perColumn(p.to.Sub(origin))
		width = ((width + bucketWidth - 1) / bucketWidth) * bucketWidth
	} else if width < time.Second {
		width = time.Second
	}
	p.a.Bucket = &substrate.AggregateBucket{Every: vocabulary.FormatISODuration(width), Origin: origin.UTC().Format(time.RFC3339Nano)}
	p.reducers = []substrate.AggregateReducer{
		{Name: substrate.ReducerFirst}, {Name: substrate.ReducerLast}, {Name: substrate.ReducerMin}, {Name: substrate.ReducerMax},
	}
	return nil
}

// parseProperty checks a property read's options: it reads records, so the
// index's options (from, sources, maxGap) mean nothing to it.
func (p *aggPlan) parseProperty() error {
	a := p.a
	if a.Property.Name == "" {
		return refuse("aggregate.property.name", "name the property to reduce")
	}
	for _, x := range []struct {
		set  bool
		path string
	}{
		{a.From != "", "aggregate.from"},
		{a.Sources != nil, "aggregate.sources"},
		{a.MaxGap != "", "aggregate.maxGap"},
		{a.MinCoverage != nil, "aggregate.minCoverage"},
	} {
		if x.set {
			hint := "drop it"
			if x.path == "aggregate.sources" {
				hint = "property.sources chooses among records"
			}
			return refuse(x.path, "a property read reduces records, not the observed index; %s", hint)
		}
	}
	var err error
	if p.propBySource, err = checkRecordSources("aggregate.property.sources", a.Property.Sources); err != nil {
		return err
	}
	for _, g := range p.groupBy {
		if g != substrate.GroupKind {
			return refuse("aggregate.groupBy", "a property read groups by kind here, and by the records' own properties in property.groupBy (money groups by currency itself), not %s", g)
		}
	}
	for i, g := range a.Property.GroupBy {
		if g == "" || slices.Contains(a.Property.GroupBy[:i], g) {
			return refuse(fmt.Sprintf("aggregate.property.groupBy[%d]", i), "names a declared property once, not %q", g)
		}
	}
	for i, r := range p.reducers {
		switch r.Name {
		case substrate.ReducerCount, substrate.ReducerSum, substrate.ReducerAvg, substrate.ReducerMin, substrate.ReducerMax, substrate.ReducerStdDev:
		default:
			return refuse(fmt.Sprintf("aggregate.reducers[%d]", i), "a property read takes count, sum, avg, min, max and stddev, not %s", r.String())
		}
	}
	return nil
}

// parseLatest checks the latest read: the newest point per group, straight
// from observed_latest.
func (p *aggPlan) parseLatest() error {
	a := p.a
	for _, g := range p.groupBy {
		if g == substrate.GroupCode {
			return refuse("aggregate.groupBy", "the latest read groups by the stream keys (metric, source, about, kind, channel) and a summary's statistic; a code is a value, not a stream")
		}
	}
	for _, x := range []struct {
		set  bool
		path string
	}{
		{a.Fill != "", "aggregate.fill"},
		{a.Rolling != nil, "aggregate.rolling"},
		{a.MaxGap != "", "aggregate.maxGap"},
		{a.MinCoverage != nil, "aggregate.minCoverage"},
		{a.From != "" && a.From != substrate.AggregateFromRaw, "aggregate.from"},
		{p.policy != substrate.SourcesSeparate, "aggregate.sources"},
	} {
		if x.set {
			return refuse(x.path, "the latest read answers each group's newest point; it takes groupBy, unit and the filter alone")
		}
	}
	if p.unit != "" && p.metric == "" {
		return refuse("aggregate.unit", "a unit converts one metric's values; name the metric beside it")
	}
	if _, ok := p.q.Filter.Properties["code"]; ok {
		return refuse("filter.properties.code", "the latest read answers each stream's newest point; a code filters the points of a bucketed read")
	}
	return nil
}

// parseMetricOptions checks the options only a bucketed metric read takes.
func (p *aggPlan) parseMetricOptions() error {
	if p.bucket.mode == bucketRecords && p.rolling != nil {
		return refuse("aggregate.rolling", "the records time mode's windows are not a sequence of buckets; a rolling window has nothing to roll over")
	}
	if p.grouped[substrate.GroupStatistic] && p.source != substrate.AggregateFromSummaries {
		if p.isEntry() {
			return p.refuse("aggregate.groupBy", "groupBy statistic splits summaries, and the metric is from %s; read it from summaries with %s, or leave groupBy statistic out and name each metric's one statistic in its entry's statistic",
				p.source, p.path("from"))
		}
		return refuse("aggregate.groupBy", "groupBy statistic splits summaries, and this read is from %s; read from summaries, or name one statistic with filter.properties.statistic", p.source)
	}
	return nil
}

// condStrings reads an eq or in condition as the strings it admits: a code, a
// channel or a statistic. Both on one condition admit what both admit, as the
// list's filter does, so `eq` A beside `in` [B] admits nothing.
func condStrings(path string, c substrate.Cond) ([]string, error) {
	return condValues(path, c, func(v any) (string, error) {
		if s := referencePathOf(v); s != "" {
			return s, nil
		}
		return "", refuse(path, "compares against a string or a reference, not %v", v)
	})
}

// condValues reads an eq or in condition as the strings str turns its values
// into.
func condValues(path string, c substrate.Cond, str func(v any) (string, error)) ([]string, error) {
	if c.Prefix != "" || c.Gt != nil || c.Gte != nil || c.Lt != nil || c.Lte != nil || c.Contains != nil || c.Exists != nil || c.Match != "" {
		return nil, refuse(path, "an aggregate read narrows it by eq or in alone")
	}
	if c.Eq == nil && c.In == nil {
		return nil, refuse(path, "name the value with eq, or several with in")
	}
	return condSet(c, str)
}

// condSet is the strings an eq or in condition admits, both beside each
// other admitting what both admit.
func condSet(c substrate.Cond, str func(v any) (string, error)) ([]string, error) {
	in := []string{}
	for _, v := range c.In {
		s, err := str(v)
		if err != nil {
			return nil, err
		}
		in = append(in, s)
	}
	if c.Eq == nil {
		return in, nil
	}
	eq, err := str(c.Eq)
	if err != nil {
		return nil, err
	}
	if c.In != nil && !slices.Contains(in, eq) {
		return []string{}, nil
	}
	return []string{eq}, nil
}

// streamKeyClause narrows a stream key a value may leave out (source, about,
// channel), stored as "" where it is absent: eq or in on the values str
// reads, a null inside in for the absent key, and exists for absent (false)
// or present (true). Every operator given narrows, as the list's filter's
// do, so `in: [null, "workout"]` is "no channel or workout" and `exists:
// false` beside `eq` admits nothing.
func streamKeyClause(b *builder, col, path string, c substrate.Cond, str func(v any) (string, error)) error {
	if c.Prefix != "" || c.Gt != nil || c.Gte != nil || c.Lt != nil || c.Lte != nil || c.Contains != nil || c.Match != "" {
		return refuse(path, "an aggregate read narrows a stream key by eq, in or exists")
	}
	if c.Eq == nil && c.In == nil && c.Exists == nil {
		return refuse(path, "name the value with eq, several with in, or exists: false for none")
	}
	absentOr := func(v any) (string, error) {
		if v == nil {
			return "", nil
		}
		if s, ok := v.(string); ok && s == "" {
			return "", refuse(path, `"" names no value; select the streams without one by exists: false, or by null inside in`)
		}
		return str(v)
	}
	if c.Eq != nil || c.In != nil {
		vals, err := condSet(c, absentOr)
		if err != nil {
			return err
		}
		b.add(col + ` = ANY(` + b.textArray(vals) + `)`)
	}
	if c.Exists != nil {
		op := `=`
		if *c.Exists {
			op = `<>`
		}
		b.add(col + ` ` + op + ` ''`)
	}
	return nil
}
