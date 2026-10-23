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
// convert is refused where the read takes it. The read runs in one of a
// bounded number of slots (aggregateSlots), on one read-only repeatable-read
// snapshot, under one deadline for its statements and its work
// (substrate.AggregateTimeout, inside the caller's), and answers the
// changelog head it was served at. A read over an observed kind whose index
// is being re-derived is refused as unavailable (checkObservedReady).
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

// aggMinCoverage is prefer-raw's floor: the fraction of a bucket raw points
// must cover before its summary is passed over.
const aggMinCoverage = 0.9

// aggregateSlots bounds the aggregate reads one process runs at once, every
// door's (REST, functions, agents): each holds a snapshot and up to its
// maxSamples of points in memory. A raw point is about 1.3 KB at the
// reduction's peak (its segment row, the cell's member, the reducers' copy
// and, for a gauge, its held span, measured on a heart-rate stream), so a
// read at substrate.AggregateMaxSamplesCap holds about 2.6 GB and two slots
// bound the reads together to about 5 GB.
const aggregateSlots = 2

// aggregateSem is the process-wide aggregate admission semaphore, a token
// per slot.
var aggregateSem = make(chan struct{}, aggregateSlots)

// aggregateQueueWait is how long a read waits for a slot before it is refused
// as unavailable; a variable so a test can shorten it.
var aggregateQueueWait = 10 * time.Second

// aggregateDeadline bounds one read whole, its wait for a slot, its
// statements and its work: substrate.AggregateTimeout, a variable so a test
// can shorten it.
var aggregateDeadline = substrate.AggregateTimeout

// acquireAggregateSlot takes a slot, waiting up to aggregateQueueWait, and
// answers its release.
func acquireAggregateSlot(ctx context.Context) (func(), error) {
	release := func() { <-aggregateSem }
	select {
	case aggregateSem <- struct{}{}:
		return release, nil
	default:
	}
	wait := time.NewTimer(aggregateQueueWait)
	defer wait.Stop()
	select {
	case aggregateSem <- struct{}{}:
		return release, nil
	case <-wait.C:
		return nil, fmt.Errorf("%w: the server runs %d aggregate reads at once and none finished within %s; retry the read",
			substrate.ErrUnavailable, cap(aggregateSem), aggregateQueueWait)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

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
	readFrom   time.Time
	lookback   int
	hasRange   bool
	bucket     bucketSpec
	reducers   []substrate.AggregateReducer
	groupBy    []string
	grouped    map[string]bool
	policy     string
	order      []string
	source     string
	maxGap     time.Duration
	fill       string
	rolling    *substrate.AggregateRolling
	maxBuckets int
	maxGroups  int
	maxSamples int64
	points     int
	// unit is the values' unit and durationUnit the durations' (durationIn,
	// timeInState), each "" for the default.
	unit, durationUnit string
	// codes is filter.properties.code: the codes intervals are filtered to
	// after they are built.
	codes map[string]bool
	// forceRaw reads raw points where rollups could serve (the tests'
	// lever; aggregateForceRaw).
	forceRaw bool
	// propBySource says property.sources' priority policy chooses among the
	// property read's records.
	propBySource bool
	// links is the read's graph of linked sources under the priority policy
	// (sourceLinks), shared by every metric of a several-metric read; nil
	// where the policy is not priority or no kind binds `sourcelink`.
	links *sourceLinks
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
	// recordsRead is the records the records time mode read and weighed,
	// the first work charged to maxSamples (recordBuckets).
	recordsRead int64
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
	// placeSteps is the steps a property read counted placing its records
	// in buckets, the comparisons a bucket search makes included.
	placeSteps int64
	// discoverBatch, where set, replaces discoverBatch for the read, and
	// discoverPairs is the most pairs one discovery statement read.
	discoverBatch, discoverPairs int
	// linkRows is the refs rows the priority policy's link lookups read.
	linkRows int64
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
	q, dates, err := recordedLocalDates(q)
	if err != nil {
		return nil, err
	}
	res, err := ds.aggregateInstants(ctx, q, dates)
	if err != nil || dates == nil {
		return res, err
	}
	res.Rows = dates.keep(res.Rows)
	return res, nil
}

// aggregateInstants is the read over a range of instants, the days outside
// dates, where set, reduced and never answered.
func (ds *dataset) aggregateInstants(ctx context.Context, q substrate.AggregateQuery, dates *localDates) (*substrate.AggregateResult, error) {
	p, err := planAggregate(q)
	if err != nil {
		return nil, err
	}
	p.forceRaw = aggregateForceRaw(ctx)
	p.bucket.dates = dates
	for _, m := range p.metrics {
		m.forceRaw = p.forceRaw
		m.bucket.dates = dates
	}
	outer := ctx
	ctx, cancel := context.WithTimeout(ctx, aggregateDeadline)
	defer cancel()
	res, err := ds.aggregate(ctx, p)
	if err != nil && ctx.Err() != nil && outer.Err() == nil {
		// The read's own deadline ended it, not the caller's.
		return nil, refuse("aggregate", "the read ran past its %s limit; narrow the range, widen the bucket, read fewer metrics or use reducers rollups serve",
			aggregateDeadline)
	}
	return res, err
}

// aggregate runs a planned read in a slot, on one snapshot.
func (ds *dataset) aggregate(ctx context.Context, p *aggPlan) (*substrate.AggregateResult, error) {
	release, err := acquireAggregateSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := aggregateTimeout(ctx, tx); err != nil {
		return nil, err
	}
	if p.kind != aggProperty {
		if err := ds.checkObservedReady(ctx, tx, p.q.Filter, p.selectedMetrics()); err != nil {
			return nil, err
		}
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
	res.Head = substrate.AggregateHead{Changelog: head.Int64, Generation: ds.historyGeneration()}
	if res.Rows == nil {
		res.Rows = []substrate.AggregateRow{}
	}
	return res, nil
}

// selectedMetrics answers the metric record paths the read names, or nil
// where it selects by filter alone (a latest read without a metric), which
// may reach any metric.
func (p *aggPlan) selectedMetrics() []string {
	var out []string
	for _, m := range append([]*aggPlan{p}, p.metrics...) {
		if m.metric != "" {
			out = append(out, m.metric)
		}
	}
	if p.metric == "" && len(p.metrics) == 0 {
		return nil
	}
	return out
}

// checkObservedReady refuses, with ErrUnavailable, a read over an observed
// index that is not current, read in the read's own snapshot: where the
// background pass still owes the re-derivation of a kind the filter admits
// (index_reprojections, queued, part done or interrupted by a shutdown), or
// where the repository's index was derived under older rules than this
// binary's (observed_index) and the open has not yet asked for it. Until the
// pass reaches a record, the index holds the points the record's previous
// declaration or rules derived, so an answer would not be the records'.
// A request narrowed to some metrics (observed_metrics, queued by a change
// to their `rollups`) holds back only the reads naming one of them, so
// editing one metric's tiers leaves every other metric of the kind readable;
// metrics nil is a read that may reach any metric.
func (ds *dataset) checkObservedReady(ctx context.Context, tx *sql.Tx, f substrate.Filter, metrics []string) error {
	kinds, err := ds.aggregateKinds(f)
	if err != nil {
		return err
	}
	var pending bool
	var version sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT
			EXISTS (SELECT 1 FROM index_reprojections WHERE observed
				AND ($1::text[] IS NULL OR kind = ANY($1::text[]))
				AND ($2::text[] IS NULL OR observed_metrics IS NULL OR observed_metrics && $2::text[])),
			(SELECT version FROM observed_index)`, kinds, metrics).Scan(&pending, &version)
	if err != nil {
		return fmt.Errorf("substrate/engine: aggregate: read the observed re-derivations owed: %w", err)
	}
	if pending || (version.Valid && version.Int64 < observedIndexVersion) {
		return fmt.Errorf("%w; retry the read once it finishes", substrate.ErrObservedRederiving)
	}
	return nil
}

// aggregateTimeout bounds every statement of the read inside the read's
// deadline (Aggregate sets one), so a statement the context cannot reach
// mid-flight ends with it.
func aggregateTimeout(ctx context.Context, tx *sql.Tx) error {
	limit := aggregateDeadline
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
	case a.Latest:
		p.kind = aggLatest
		if err := p.parseLatest(); err != nil {
			return nil, err
		}
		return p, nil
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
	default:
		p.kind = aggMetric
		if p.metric == "" && a.Metrics == nil {
			return nil, refuse("aggregate.metric", `name the metric, {"ref": "<authority>/observations/metric/<id>"}, several in metrics, or a property; only the latest read ("latest": true) selects by filter alone`)
		}
	}
	if !p.hasRange {
		if a.Metrics != nil {
			return nil, refuse("aggregate.range", `a several-metric read is bucketed and needs a range, {"gte": "<instant>", "lt": "<instant>"}; the latest read ("latest": true) selects several metrics with filter.properties.metric`)
		}
		return nil, refuse("aggregate.range", `a bucketed read needs a range, {"gte": "<instant>", "lt": "<instant>"}; each group's newest point is the latest read, "latest": true`)
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
		p.metrics = append(p.metrics, mp)
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
	// A read's unit is a spelling the repository's graph resolves once the
	// read has its transaction (aggregateUnits).
	mp.unit = strings.TrimSpace(m.Unit)
	if m.DurationUnit != "" {
		code, err := durationUnit(at+".durationUnit", m.DurationUnit)
		if err != nil {
			return nil, err
		}
		mp.durationUnit = code
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
// maxGap, fill, rolling, the units and the budgets.
func (p *aggPlan) parseCommon() error {
	a := p.a
	for i, g := range a.GroupBy {
		if g == "kind" {
			return refuse(fmt.Sprintf("aggregate.groupBy[%d]", i), "groupBy kind was removed: narrow filter.kinds to one kind per read, or group a property read by its records' own properties in property.groupBy")
		}
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
				return refuse("aggregate.sources.order", "an order ranks sources for the priority policy; separate keeps every source apart")
			}
		case substrate.SourcesPriority:
			p.policy = a.Sources.Policy
			p.order = a.Sources.Order
		case "summaries":
			return refuse("aggregate.sources.policy", `the summaries policy was removed: read from summaries ("from": "summaries") under the priority policy, which keeps the highest-ranked source's summary where two overlap`)
		default:
			return refuse("aggregate.sources.policy", "%q is not a policy; the policies are separate and priority", a.Sources.Policy)
		}
	}
	if err := p.parseFrom(a.From); err != nil {
		return err
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
	case "", substrate.FillZero:
		p.fill = a.Fill
	case "null", "locf":
		return refuse("aggregate.fill", "fill %s was removed: zero is the one fill; leave fill out and a bucket with no row is absent", a.Fill)
	default:
		return refuse("aggregate.fill", "%q is not a fill; the one fill is zero", a.Fill)
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
	p.unit = strings.TrimSpace(a.Unit)
	if a.DurationUnit != "" {
		code, err := durationUnit("aggregate.durationUnit", a.DurationUnit)
		if err != nil {
			return err
		}
		p.durationUnit = code
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

// parseFrom reads which points the plan takes.
func (p *aggPlan) parseFrom(from string) error {
	p.source = substrate.AggregateFromRaw
	switch from {
	case "", substrate.AggregateFromRaw:
	case substrate.AggregateFromSummaries, substrate.AggregateFromPreferRaw:
		p.source = from
	default:
		return refuse(p.path("from"), "%q is not a source of points; from is raw, summaries or prefer-raw", from)
	}
	return nil
}

// durationUnit reads a durations' unit: one of the fixed list
// (timeunits.go).
func durationUnit(path, unit string) (string, error) {
	u := strings.TrimSpace(unit)
	if !isTimeUnit(u) {
		return "", refuse(path, "%s is not a unit of duration; durationIn and timeInState answer in %s", unit, timeUnitNames)
	}
	return u, nil
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
		{a.DurationUnit != "", "aggregate.durationUnit"},
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
	if len(p.groupBy) > 0 {
		return refuse("aggregate.groupBy", "a property read groups by the records' own properties in property.groupBy (money groups by currency itself), not by %s", p.groupBy[0])
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
			return refuse("aggregate.groupBy", "the latest read groups by the stream keys (metric, source, about, channel) and a summary's statistic; a code is a value, not a stream")
		}
	}
	for _, x := range []struct {
		set  bool
		path string
	}{
		{a.Metrics != nil, "aggregate.metrics"},
		{a.Property != nil, "aggregate.property"},
		{p.q.Downsample != nil, "downsample"},
		{a.Range != nil, "aggregate.range"},
		{a.Bucket != nil, "aggregate.bucket"},
		{len(a.Reducers) > 0, "aggregate.reducers"},
		{a.Fill != "", "aggregate.fill"},
		{a.Rolling != nil, "aggregate.rolling"},
		{a.MaxGap != "", "aggregate.maxGap"},
		{a.DurationUnit != "", "aggregate.durationUnit"},
		{a.From != "" && a.From != substrate.AggregateFromRaw, "aggregate.from"},
		{p.policy != substrate.SourcesSeparate, "aggregate.sources"},
	} {
		if x.set {
			return refuse(x.path, "the latest read answers each group's newest point; it takes the metric, the filter, groupBy and unit alone")
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
