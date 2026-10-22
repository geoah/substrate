package engine

// The aggregate read over the observed index (aggregate.go): the metric read
// and the downsample, which is a metric read in columns.
//
// A read enumerates streams from observed_latest, finds the forms their
// points hold inside the read's windows (discoverForms, from the segments),
// groups them, checks every reducer against each group's form and the unit
// against the measure, and decides per group where its buckets are served
// from:
//
//   - observed_rollups, when every reducer merges exactly from rollup rows
//     (internal/observed.Merge). Each stream's rows are chosen tier by tier,
//     the coarsest first, where a tier's bucket fits the read's buckets
//     (planSlots), and a gauge's rows must be Eligible under the request's
//     maxGap. A bucket's rows are merged after converting each from its own
//     unit; the parts of an amount's or a counter's bucket no row covers
//     (the records mode, a range that starts mid-row) are read from raw
//     points and merged in. A gauge's held time walks the rows with
//     HeldFromRowsCarryIn, so one lookup into the raw points per window
//     carries a value into it.
//   - raw points otherwise (percentile, durationIn, a priority over two or
//     more streams, a censored value, a gauge's bucket or a recorded-local
//     read no tier covers), derived from the records whose segments meet
//     the windows, within maxSamples, counted before they are derived.

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// aggStream is one selected stream: its observed_latest row, and on a
// bucketed read one form its points hold inside the read's windows with the
// codes those points carry ("" for a point without one). A summary's
// statistic is part of the stream: a source that keeps a night's average and
// maximum has a stream of each, and every other form's stat is "".
type aggStream struct {
	obsStream
	stat string
	// id is the stream's private id (observed_streams), and maxExtentMs its
	// longest record's extent, which bounds every scan of its segments.
	id          int64
	maxExtentMs int64
	// tiers are its metric's rollup tiers, finest first.
	tiers     []time.Duration
	form      string
	unit      string
	code      string
	codes     []string
	delta     bool
	value     sql.NullString
	ts, tsEnd time.Time
	// comparator is the newest point's (observed_latest), which the latest
	// read answers it with.
	comparator string
	offset     int64
	recordID   string
	points     int64
	rank       int
	// offsetless says points the read takes from the stream were written
	// without a UTC offset (discoverForms).
	offsetless bool
}

func streamLess(a, b obsStream) bool {
	for _, c := range [][2]string{{a.metric, b.metric}, {a.kind, b.kind}, {a.source, b.source}, {a.about, b.about}, {a.channel, b.channel}} {
		if c[0] != c[1] {
			return c[0] < c[1]
		}
	}
	return false
}

// aggStreamLess orders streams by their keys.
func aggStreamLess(a, b *aggStream) bool {
	if a.obsStream != b.obsStream {
		return streamLess(a.obsStream, b.obsStream)
	}
	return a.stat < b.stat
}

// coded reports whether the stream's points in the read's windows hold
// codes: a coded gauge holds a code instead of a number, a coded amount a
// code beside its number.
func (s *aggStream) coded() bool {
	return slices.ContainsFunc(s.codes, func(c string) bool { return c != "" })
}

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
	// mixedStatistics says the group's sources hold different statistics,
	// one each, which the priority and summaries policies admit
	// (checkStatistics) and each row and rolling value holds to one
	// (rowStatistic, roll). statisticAt is where the read names one.
	mixedStatistics bool
	statisticAt     string
	// context are the streams of the group's key and form that hold points
	// only outside the read's windows, which the priority policy weighs and
	// never places (priorityContext).
	context []*aggStream
}

// contextOf is the group's context streams of a form.
func (g *aggGroup) contextOf(form string) []*aggStream {
	var out []*aggStream
	for _, s := range g.context {
		if s.form == form {
			out = append(out, s)
		}
	}
	return out
}

// sourceOf is the source of the stream-th raw stream of the group, or of its
// summaries for a summary row: what a refusal names.
func (g *aggGroup) sourceOf(stream int, form string) string {
	list := g.raw
	if form == vocabulary.FormSummary {
		list = g.sums
	}
	if stream < 0 || stream >= len(list) {
		return ""
	}
	return list[stream].source
}

// aggPoint is one point as the read holds it: the observed point (its value
// converted to the read's unit, except a counter's, whose steps read the
// stored digits) and what the index rows carry beside it.
type aggPoint struct {
	observed.Point
	off int64
	// fallback says off is the fallback zone's: the point recorded none.
	fallback bool
	delta    bool
	stream   int
	// frac is the part of an amount's span a records-mode window holds,
	// which At and End are clipped to and its number is taken by; nil is
	// the whole.
	frac *big.Rat
	// bucket is the bucket placeRaw puts the point in.
	bucket *aggBucket
}

// aggMeta is what a read learns beside its rows.
type aggMeta struct {
	sources map[string]bool
	inexact bool
}

// metricRun is one metric of a read as it runs: its plan's run, its groups
// and, once reduced, their cells.
type metricRun struct {
	r          *aggRun
	groups     []*aggGroup
	raws, sums []aggCells
}

// aggregateMetric answers a metric read, a downsample or a several-metric
// read. Every metric is settled first (its streams, units, forms and
// groups, each refusal naming the metric's own options), then each plans
// and is charged, then each is reduced, and only then are rows written: the
// metrics share the read's buckets, its budgets and, in the recorded-local
// calendar, each bucket's bounds.
func (ds *dataset) aggregateMetric(ctx context.Context, tx *sql.Tx, p *aggPlan) (*substrate.AggregateResult, error) {
	buckets, err := ds.buckets(ctx, tx, p)
	if err != nil {
		return nil, err
	}
	windows, err := p.windows(ctx, buckets)
	if err != nil {
		return nil, err
	}
	plans := p.metrics
	if plans == nil {
		plans = []*aggPlan{p}
	}
	runs := make([]*metricRun, len(plans))
	samples := p.recordsRead
	groups, series := 0, 0
	for i, mp := range plans {
		m, err := ds.settleMetric(ctx, tx, mp, buckets, windows, samples)
		if err != nil {
			return nil, err
		}
		runs[i], samples = m, m.r.samples
		groups, series = groups+len(m.groups), series+m.r.nSeries
	}
	per, limit := p.bucketLimit(buckets)
	if err := p.checkMetrics(len(plans), groups, series, per, limit); err != nil {
		return nil, err
	}
	if err := p.checkLookback(series, buckets); err != nil {
		return nil, err
	}
	for _, m := range runs {
		m.r.samples = samples
		if err := m.r.plan(m.groups); err != nil {
			return nil, err
		}
		samples = m.r.samples
	}
	if tr := traceOf(ctx); tr != nil && tr.afterPlan != nil {
		tr.afterPlan()
	}
	// A recorded-local read makes a bucket where a point names a day the
	// range's enumeration did not; each metric reduces into the buckets the
	// metrics before it made, so one day is one bucket for every metric.
	for _, m := range runs {
		m.r.buckets, m.r.samples = buckets, samples
		if m.raws, m.sums, err = m.r.eval(m.groups); err != nil {
			return nil, err
		}
		buckets, samples = m.r.buckets, m.r.samples
	}
	if per, limit := p.bucketLimit(buckets); per*max(series, 1) > limit {
		return nil, refuse("aggregate.budget.maxBuckets", "the points fall in %d recorded-local buckets, times %d series is %d, over %d; narrow the range or raise maxBuckets",
			per, series, per*max(series, 1), limit)
	}
	if err := p.checkLookback(series, buckets); err != nil {
		return nil, err
	}
	for _, m := range runs {
		m.r.buckets = buckets
		if err := m.r.markEnds(m.groups, m.raws, m.sums); err != nil {
			return nil, err
		}
	}
	meta := aggMeta{sources: map[string]bool{}}
	var rows []aggOut
	held := 0
	for _, m := range runs {
		out, n, err := m.r.assemble(m.groups, m.raws, m.sums)
		if err != nil {
			return nil, err
		}
		rows, held = append(rows, out...), held+n
		meta.inexact = meta.inexact || m.r.meta.inexact
		for s := range m.r.meta.sources {
			meta.sources[s] = true
		}
	}
	// A padded read's groups that answer a row are held in fillAndRoll
	// (checkAnswered).
	if held > p.maxGroups && !p.padded() {
		return nil, refuse("aggregate.budget.maxGroups", "grouping by code makes %d groups, over %d; filter the codes or raise maxGroups (the cap is %d)",
			held, p.maxGroups, substrate.AggregateMaxGroupsCap)
	}
	res := p.metricResult(runs)
	if res.Rows, err = p.fillAndRoll(ctx, rows, buckets, &meta, samples); err != nil {
		return nil, err
	}
	res.Exact = !meta.inexact
	for s := range meta.sources {
		res.Sources = append(res.Sources, s)
	}
	sort.Strings(res.Sources)
	return res, nil
}

// settleMetric selects one metric's streams and settles its units, forms
// and groups, refusing what the metric's options do not admit before any
// rollup row or point is read to reduce. spent is the raw points the read's
// statements counted before it, which the metric's discovery adds to.
func (ds *dataset) settleMetric(ctx context.Context, tx *sql.Tx, p *aggPlan, buckets []*aggBucket, windows [][2]time.Time, spent int64) (*metricRun, error) {
	selected, err := ds.selectStreams(ctx, tx, p)
	if err != nil {
		return nil, err
	}
	// The units are the selected streams' whatever the range holds, so a
	// range with no point still answers in the metric's unit.
	units, err := ds.aggregateUnits(ctx, tx, p, selected)
	if err != nil {
		return nil, err
	}
	if err := ds.settlePlace(ctx, tx, p); err != nil {
		return nil, err
	}
	if err := ds.streamTiers(ctx, tx, selected); err != nil {
		return nil, err
	}
	store := newObsStore(ctx, tx, ds.liveFoldView(), units.graph)
	streams, samples, err := ds.discoverForms(ctx, store, p, selected, windows, spent)
	if err != nil {
		return nil, err
	}
	if err := p.checkOffsets(streams); err != nil {
		return nil, err
	}
	groups, err := p.groupStreams(streams)
	if err != nil {
		return nil, err
	}
	if err := p.checkForms(groups, units); err != nil {
		return nil, err
	}
	series, err := p.seriesOf(groups)
	if err != nil {
		return nil, err
	}
	var contextWindows [][2]time.Time
	if p.policy == substrate.SourcesPriority {
		if contextWindows, samples, err = ds.priorityContext(ctx, store, p, selected, streams, groups, windows, samples); err != nil {
			return nil, err
		}
	}
	if per, limit := p.bucketLimit(buckets); per*max(series, 1) > limit {
		return nil, p.refuse("aggregate.budget.maxBuckets", "%d series (groups, and codes where grouped by code) times %d buckets is %d, over %d; narrow the filter, widen the bucket or raise maxBuckets (the cap is %d)",
			series, per, per*max(series, 1), limit, substrate.AggregateMaxBucketsCap)
	}
	if err := p.checkLookback(series, buckets); err != nil {
		return nil, err
	}
	r := &aggRun{
		ds: ds, ctx: ctx, tx: tx, p: p, u: units, buckets: buckets, windows: windows,
		meta: aggMeta{sources: map[string]bool{}}, nSeries: series, samples: samples, store: store,
		contextWindows: contextWindows,
	}
	store.charge = func(n int64) error {
		return r.charge(n, "raw points of the records holding the value before a window")
	}
	return &metricRun{r: r, groups: groups}, nil
}

// settlePlace places the metric's values by its record's bucketPlace where
// the request names no place: a night lands on the day it ends whether or
// not the client knows to ask, and the API and the console agree. It applies
// to the every and calendar modes; a downsample's columns and the records
// mode place values where they lie.
func (ds *dataset) settlePlace(ctx context.Context, tx *sql.Tx, p *aggPlan) error {
	s := &p.bucket
	if s.placeNamed || p.metric == "" || p.kind == aggDownsample || (s.mode != bucketEvery && s.mode != bucketCalendar && s.mode != bucketLocal) {
		return nil
	}
	c, _, err := metricContract(ctx, tx, ds.registry(), p.metric)
	if err != nil {
		return err
	}
	s.placeEnd = c.BucketPlace == substrate.AggregatePlaceEnd
	return nil
}

// checkMetrics holds a several-metric read's metrics to the read's budgets
// together: their groups (their series, where grouped by code) to
// maxGroups, and their series times the buckets to limit (bucketLimit; a
// padded read holds both to the caps here). Each metric was held to them
// alone as it settled.
func (p *aggPlan) checkMetrics(metrics, groups, series, buckets, limit int) error {
	if p.metrics == nil {
		return nil
	}
	n := groups
	if p.grouped[substrate.GroupCode] {
		n = series
	}
	if n > p.groupLimit() {
		return refuse("aggregate.budget.maxGroups", "the %d metrics make %d groups, over %d; name fewer metrics, narrow the filter or group by fewer keys (the cap is %d)",
			metrics, n, p.groupLimit(), substrate.AggregateMaxGroupsCap)
	}
	if n := buckets * max(series, 1); n > limit {
		return refuse("aggregate.budget.maxBuckets", "the %d metrics make %d series (groups, and codes where grouped by code), times %d buckets is %d, over %d; name fewer metrics, narrow the filter, widen the bucket or raise maxBuckets (the cap is %d)",
			metrics, series, buckets, n, limit, substrate.AggregateMaxBucketsCap)
	}
	return nil
}

// metricResult is the answer's head: the metric and its unit, or on a
// several-metric read each metric's unit in units, and from where every
// metric reads from the same points.
func (p *aggPlan) metricResult(runs []*metricRun) *substrate.AggregateResult {
	res := &substrate.AggregateResult{Bucket: p.bucket.echo, From: p.source, Exact: true}
	if p.kind == aggDownsample {
		res.Bucket = p.a.Bucket
	}
	unitOf := func(u *aggUnits) string {
		if u.value != "" {
			return u.value
		}
		return u.duration
	}
	if p.metrics == nil {
		res.Metric, res.Unit = p.metric, unitOf(runs[0].r.u)
		if b := res.Bucket; b != nil && b.Place == "" && p.bucket.placeEnd {
			// The place the metric's bucketPlace chose (settlePlace).
			echo := *b
			echo.Place = substrate.AggregatePlaceEnd
			res.Bucket = &echo
		}
		return res
	}
	res.Units, res.From = map[string]string{}, runs[0].r.p.source
	for _, m := range runs {
		res.Units[m.r.p.metric] = unitOf(m.r.u)
		if m.r.p.source != res.From {
			res.From = ""
		}
	}
	return res
}

// --- stream selection ---

// checkOffsets refuses a recorded-local read of a stream holding values
// written without a UTC offset, unless the bucket names a fallback zone to
// place them by: such a value has no local day, and its UTC date is a guess.
func (p *aggPlan) checkOffsets(streams []*aggStream) error {
	if p.bucket.mode != bucketLocal || p.bucket.fallback != nil {
		return nil
	}
	for _, s := range streams {
		if s.offsetless {
			return refuse("aggregate.bucket.fallbackTimezone", "the %s values of %s from %s include values written without a UTC offset, which the recorded-local calendar cannot place; name bucket.fallbackTimezone to place them by a zone's clock, or read in bucket.timezone",
				s.form, s.metric, orOwner(s.source))
		}
	}
	return nil
}

// checkMetric refuses a metric reference that names no metric, before
// anything is selected: a record of a kind no observed kind's `metric`
// pins, or a path no metric record has and no observed value names (a
// metric record deleted after its values were written still reads them). An
// unknown metric would otherwise answer no rows, as a metric with no values
// in the range does.
func (ds *dataset) checkMetric(ctx context.Context, tx *sql.Tx, path, ref string) error {
	kind, id, _ := vocabulary.SplitRecordPath(ref)
	if pins := ds.metricKinds(); pins != nil && !slices.Contains(pins, kind) {
		return refuse(path, "%s is a record of %s, not a metric; a metric is a record of %s", ref, kind, strings.Join(pins, " or "))
	}
	var known bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL)
		OR EXISTS (SELECT 1 FROM observed_streams s JOIN observed_latest l ON l.stream = s.id WHERE s.metric = $3)`,
		kind, id, ref).Scan(&known); err != nil {
		return fmt.Errorf("substrate/engine: aggregate metric: %w", err)
	}
	if !known {
		return refuse(path, "%s names no metric: no record has that path and no observed value names it", ref)
	}
	return nil
}

// metricKinds are the kinds the `metric` reference of every kind binding
// `observed` pins, sorted; nil where one pins none, so any kind may be a
// metric.
func (ds *dataset) metricKinds() []string {
	var out []string
	for _, t := range ds.registry().Kinds() {
		if !bindsObserved(t) {
			continue
		}
		prop, ok := t.Prop("metric")
		switch {
		case ok && prop.ToTrait != "":
			for _, k := range ds.registry().Kinds() {
				if k.Implements(prop.ToTrait) {
					out = append(out, k.Identity)
				}
			}
		case !ok || prop.To == "":
			return nil
		default:
			out = append(out, prop.To)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

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
	if c, ok := p.q.Filter.Properties["metric"]; ok {
		vals, err := condValues("filter.properties.metric", c, func(v any) (string, error) {
			return ds.streamRef(kinds, "metric", "filter.properties.metric", v)
		})
		if err != nil {
			return nil, err
		}
		b.add(`metric = ANY(` + b.textArray(vals) + `)`)
	}
	for _, name := range []string{"source", "about", "channel"} {
		c, ok := p.q.Filter.Properties[name]
		if !ok {
			continue
		}
		path := "filter.properties." + name
		str := func(v any) (string, error) { return ds.streamRef(kinds, name, path, v) }
		if name == "channel" {
			str = func(v any) (string, error) {
				if s, ok := v.(string); ok {
					return s, nil
				}
				return "", refuse(path, "compares against a string, not %v", v)
			}
		}
		if err := streamKeyClause(b, name, path, c, str); err != nil {
			return nil, err
		}
	}
	order := make([]string, len(p.order))
	for i, o := range p.order {
		if order[i], err = ds.streamRef(kinds, "source", fmt.Sprintf("aggregate.sources.order[%d]", i), o); err != nil {
			return nil, err
		}
	}
	// A statistic narrows the summaries; every other form's values carry
	// none and are kept. A several-metric read's metric may name its own,
	// which narrows beside the filter's.
	if c, ok := p.q.Filter.Properties[substrate.GroupStatistic]; ok {
		vals, err := condStrings("filter.properties.statistic", c)
		if err != nil {
			return nil, err
		}
		b.add(`(statistic = '' OR statistic = ANY(` + b.textArray(vals) + `))`)
	}
	if p.statistic != "" {
		b.add(`(statistic = '' OR statistic = ` + b.arg(p.statistic) + `)`)
	}
	where := "TRUE"
	if len(b.where) > 0 {
		where = strings.Join(b.where, " AND ")
	}
	// observed_latest keeps a row per stream with an available point, and
	// each row is a stream of the read.
	rows, err := tx.QueryContext(ctx, `SELECT s.id, s.max_extent_ms, s.metric, s.kind, s.source, s.about, s.channel, s.statistic,
		l.form, l.unit, l.code, l.delta, l.value::text, l.ts, l.ts_end, l.comparator, l.utc_offset_min, l.record_id, l.points
		FROM observed_streams s JOIN observed_latest l ON l.stream = s.id WHERE `+where+`
		ORDER BY s.metric, s.kind, s.source, s.about, s.channel, s.statistic`, b.args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate streams: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*aggStream
	for rows.Next() {
		s := &aggStream{}
		var extent int64
		if err := rows.Scan(&s.id, &extent, &s.metric, &s.kind, &s.source, &s.about, &s.channel, &s.stat, &s.form, &s.unit, &s.code, &s.delta, &s.value,
			&s.ts, &s.tsEnd, &s.comparator, &s.offset, &s.recordID, &s.points); err != nil {
			return nil, err
		}
		s.maxExtentMs = extent
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// A source the order does not name ranks after those it does, and a
	// stream naming no source after every named one.
	for _, s := range out {
		s.rank = len(order)
		switch i := slices.Index(order, s.source); {
		case i >= 0:
			s.rank = i
		case s.source == "":
			s.rank = len(order) + 1
		}
	}
	if len(order) == 0 && p.metric != "" && p.policy == substrate.SourcesPriority {
		if err := ds.rankByPriorities(ctx, tx, p.metric, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// rankByPriorities ranks the streams by their source records' priorities
// for the metric's record path, 1 first, the order the priority and summaries
// policies take where the request names none. Only a record whose kind binds
// core's `source` ranks (metric.go). A source the record ranks not, or with
// no such record, goes after the ranked ones, and a stream naming no source
// after every named one; the stream order breaks ties.
func (ds *dataset) rankByPriorities(ctx context.Context, tx *sql.Tx, metric string, streams []*aggStream) error {
	reg := ds.registry()
	var kinds, ids []string
	seen := map[string]bool{}
	for _, s := range streams {
		if kind, id, ok := vocabulary.SplitRecordPath(s.source); ok && !seen[s.source] && bindsSource(reg, kind) {
			seen[s.source] = true
			kinds, ids = append(kinds, kind), append(ids, id)
		}
	}
	ranks := map[string]int{}
	if len(ids) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT r.kind, r.id, r.props->'priorities'->>$3
			FROM unnest($1::text[], $2::text[]) AS s(kind, id)
			JOIN records r ON r.kind = s.kind AND r.id = s.id AND r.deleted_at IS NULL
			WHERE jsonb_exists(r.props->'priorities', $3)`, kinds, ids, metric)
		if err != nil {
			return fmt.Errorf("substrate/engine: aggregate source priorities: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var kind, id string
			var rank sql.NullString
			if err := rows.Scan(&kind, &id, &rank); err != nil {
				return err
			}
			if n, err := strconv.Atoi(rank.String); err == nil {
				ranks[vocabulary.RecordPath(kind, id)] = n
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	for _, s := range streams {
		s.rank = math.MaxInt - 1
		if n, ok := ranks[s.source]; ok {
			s.rank = n
		} else if s.source == "" {
			s.rank = math.MaxInt
		}
	}
	return nil
}

// rankingSourceKinds are the kinds whose records' priorities a read ranks its
// sources by (rankByPriorities): where a priority policy names no order on a
// metric read, the kind each selected observed kind's source
// reference pins, "" where one pins none and a record of any kind may rank.
// A caller holding a reads allowlist holds each to it, since the ranking
// reveals what those records hold.
func (ds *dataset) rankingSourceKinds(q substrate.AggregateQuery) []string {
	a := q.Aggregate
	if a == nil || a.Sources == nil || len(a.Sources.Order) > 0 || (a.Metric == nil && len(a.Metrics) == 0) ||
		a.Sources.Policy != substrate.SourcesPriority {
		return nil
	}
	var selected []string
	for _, k := range q.Filter.Kinds {
		if ty, err := ds.resolveType(k); err == nil {
			selected = append(selected, ty.Identity)
		}
	}
	var pins []string
	for _, t := range ds.registry().Kinds() {
		if !bindsObserved(t) || (len(q.Filter.Kinds) > 0 && !slices.Contains(selected, t.Identity)) {
			continue
		}
		if prop, ok := t.Prop("source"); ok && !slices.Contains(pins, prop.To) {
			pins = append(pins, prop.To)
		}
	}
	slices.Sort(pins)
	return pins
}

// streamRef is the record path a filter value or an order entry names for a
// stream's reference property (metric, source, about): a full path as
// written, as a string or {ref}, or a bare id completed with the kind the
// selected kinds' declaration of the property pins, as a list filter
// completes it (referenceFilterPath). A bare id is refused where those kinds
// pin no kind, any kind, or two. kinds nil is every kind binding observed.
func (ds *dataset) streamRef(kinds []string, name, path string, v any) (string, error) {
	s := referencePathOf(v)
	if s == "" {
		return "", refuse(path, "compares against a record path or a reference, not %v", v)
	}
	if _, _, ok := vocabulary.SplitRecordPath(s); ok {
		return s, nil
	}
	reg := ds.registry()
	var pins []string
	for _, t := range reg.Kinds() {
		if !bindsObserved(t) || (kinds != nil && !slices.Contains(kinds, t.Identity)) {
			continue
		}
		pin := ""
		if prop, ok := t.Prop(name); ok {
			pin = prop.To
		}
		if !slices.Contains(pins, pin) {
			pins = append(pins, pin)
		}
	}
	if len(pins) != 1 {
		slices.Sort(pins)
		for i, pin := range pins {
			if pin == "" {
				pins[i] = "no kind"
			}
		}
		return "", refuse(path, "%q is a bare id, and the selected kinds' %s references pin %s; name the full path, <kind>/<id>",
			s, name, andList(pins))
	}
	return referenceFilterPath(path, &vocabulary.Property{To: pins[0]}, s)
}

// windows are the spans a metric read takes points from: the range with the
// buckets a rolling window reads before it, or each records-mode window,
// overlapping ones joined.
func (p *aggPlan) windows(ctx context.Context, buckets []*aggBucket) ([][2]time.Time, error) {
	if p.bucket.mode != bucketRecords {
		return [][2]time.Time{{p.readFrom, p.to}}, nil
	}
	spans := make([][2]time.Time, 0, len(buckets))
	for _, b := range buckets {
		spans = append(spans, [2]time.Time{b.start, b.end})
	}
	var steps everyThousand
	return joinSpans(ctx, spans, func() error { return steps.check(ctx) })
}

// discoverBatch is how many pairs of a stream and a window one statement of
// form discovery reads segments for.
const discoverBatch = 1024

// discoveredSegment is the record ref, a segment of the stream numbered
// stream, crossing the start or end of the window numbered window.
type discoveredSegment struct {
	stream, window int
	ref            eref
}

// discoverSegmentsSQL lists, for each pair of a stream ($1, numbered $2)
// and a window ($4 and $5, numbered $6), the stream's segments of the forms
// $7 whose points may meet the window: starting at or after $3, the
// window's start less the stream's longest extent (spanSegmentsSQL says why
// the caller computes it), and before its end, and ending at or after its
// start. OFFSET 0 keeps the subquery a loop over the pairs, one range of the
// stream index each: flattened into a join, a small table plans as a scan of
// every segment of the repository.
const discoverSegmentsSQL = `
	SELECT x.n, x.i, ` + segmentColumns + `
	FROM unnest($1::bigint[], $2::int[], $3::timestamptz[], $4::timestamptz[], $5::timestamptz[], $6::int[])
	     AS x(id, n, lo, a, b, i)
	CROSS JOIN LATERAL (
		SELECT * FROM observed_segments g
		WHERE g.stream = x.id AND g.form = ANY($7::text[])
		  AND g.first_ts >= x.lo AND g.first_ts < x.b AND g.end_ts >= x.a OFFSET 0) g
	ORDER BY x.n, x.i, g.first_ts, g.record_id`

// places reports whether the read takes a point of the form into the window
// [a, b): by its start, by its last instant under place end (the end of a
// span, which is in (a, b]), or for an amount in the records mode wherever
// its span meets the window.
func (p *aggPlan) places(pt observed.Point, form string, a, b time.Time) bool {
	switch {
	case p.bucket.mode == bucketRecords && form == vocabulary.FormAmount:
		return pt.At.Before(b) && (!pt.At.Before(a) || pt.End.After(a))
	case p.bucket.placeEnd && pt.End.After(pt.At):
		return pt.End.After(a) && !pt.End.After(b)
	}
	return !pt.At.Before(a) && pt.At.Before(b)
}

// discoverForms turns each selected stream into one stream per form its
// points hold inside the windows, with the codes they carry. The latest row
// names a stream and says nothing about the points a range holds, so it
// never decides a form: every form found is checked, and a group holding two
// is refused, before any path is chosen. A form the read does not take (a
// summary on a read from raw) is not looked for.
//
// A segment wholly inside a window answers from what the index keeps of it
// (its form, its codes, whether it recorded its offset); one that crosses a
// window's start or end is derived, and its points that the read places in the
// window answer. Those derived points are counted first, a copy for each
// further window a record crosses, and refused past maxSamples; samples is that count beside spent, the start of the read's
// (aggRun.charge). A gauge also answers by the point current at a window's
// start where that value still holds into it under its own hold and the
// request's maxGap, because its held time is the window's; the record that
// lookup derives counts too, whether or not its value still holds.
func (ds *dataset) discoverForms(ctx context.Context, store *obsStore, p *aggPlan, streams []*aggStream, windows [][2]time.Time, spent int64, forms ...string) (out []*aggStream, samples int64, err error) {
	if len(streams) == 0 || len(windows) == 0 {
		return nil, spent, nil
	}
	switch {
	case len(forms) > 0:
	case p.source == substrate.AggregateFromRaw:
		forms = []string{vocabulary.FormGauge, vocabulary.FormAmount, vocabulary.FormCounter}
	case p.source == substrate.AggregateFromSummaries:
		forms = []string{vocabulary.FormSummary}
	default:
		forms = []string{vocabulary.FormGauge, vocabulary.FormAmount, vocabulary.FormCounter, vocabulary.FormSummary}
	}
	type key struct {
		n    int
		form string
	}
	type held struct {
		codes      map[string]bool
		offsetless bool
	}
	got := map[key]*held{}
	note := func(n int, form, code string, known bool) {
		k := key{n, form}
		h := got[k]
		if h == nil {
			h = &held{codes: map[string]bool{}}
			got[k] = h
		}
		h.codes[code] = true
		h.offsetless = h.offsetless || !known
	}
	// A segment wholly inside a window is noted as it is read and kept no
	// longer. A segment crossing a window's start or end is kept, once per
	// window it crosses, to be derived and its points placed in that window.
	// Its record's points count against maxSamples the first time it is met
	// and again, as copies, for every further window, as the reduction
	// charges a point it places in several windows (chargeCopies): a long
	// amount crossing many records-mode windows is held and walked once per
	// window, so the refusal comes before more is held.
	var found []discoveredSegment
	var crossing []obsSegment
	counted := map[eref]bool{}
	overBudget := func() error {
		beside := ""
		if spent > 0 {
			beside = fmt.Sprintf(" beside the %d raw points the read counted before it", spent)
		}
		return refuse("aggregate.budget.maxSamples", "finding the forms the range holds takes at least %d raw points from the records that cross a window's start or end, a copy for each further window a record crosses%s, over %d; narrow the range, or raise maxSamples (the cap is %d)",
			samples, beside, p.maxSamples, substrate.AggregateMaxSamplesCap)
	}
	// The pairs of a stream and a window are as many as the streams times
	// the windows, so they are sent a batch at a time (discoverBatch), each
	// built with the context checked as it goes.
	batch := discoverBatch
	tr := traceOf(ctx)
	if tr != nil && tr.discoverBatch > 0 {
		batch = tr.discoverBatch
	}
	ids := make([]int64, 0, discoverBatch)
	var ns, is []int
	var lo, from, to []time.Time
	flush := func() error {
		if len(ids) == 0 {
			return nil
		}
		if tr != nil {
			tr.discoverPairs = max(tr.discoverPairs, len(ids))
		}
		rows, err := store.q.QueryContext(ctx, discoverSegmentsSQL, ids, ns, lo, from, to, is, forms)
		if err != nil {
			return fmt.Errorf("substrate/engine: aggregate forms: %w", err)
		}
		for rows.Next() {
			var n, wi int
			seg, err := scanSegment(rows, &n, &wi)
			if err != nil {
				_ = rows.Close()
				return err
			}
			f := discoveredSegment{stream: n - 1, window: wi - 1, ref: seg.ref}
			if w := windows[f.window]; !seg.first.Before(w[0]) && seg.end.Before(w[1]) {
				if seg.available > 0 {
					for _, code := range seg.codes {
						note(f.stream, seg.form, code, seg.known)
					}
				}
				continue
			}
			if counted[seg.ref] {
				samples += int64(max(seg.points, 1))
			} else {
				counted[seg.ref] = true
				samples += int64(seg.points)
				crossing = append(crossing, seg)
			}
			if samples+spent > p.maxSamples {
				_ = rows.Close()
				return overBudget()
			}
			found = append(found, f)
		}
		if err := finishRows(rows); err != nil {
			return err
		}
		ids, ns, is = ids[:0], ns[:0], is[:0]
		lo, from, to = lo[:0], from[:0], to[:0]
		return nil
	}
	var steps everyThousand
	for n, s := range streams {
		for i, w := range windows {
			if err := steps.check(ctx); err != nil {
				return nil, 0, err
			}
			ids, ns, is = append(ids, s.id), append(ns, n+1), append(is, i+1)
			lo, from, to = append(lo, s.info().reachBack(w[0])), append(from, w[0]), append(to, w[1])
			if len(ids) >= batch {
				if err := flush(); err != nil {
					return nil, 0, err
				}
			}
		}
	}
	if err := flush(); err != nil {
		return nil, 0, err
	}
	samples += spent
	if err := store.derive(crossing); err != nil {
		return nil, 0, err
	}
	// The carry lookups below derive the record holding the value before
	// each window, and its points count as the crossing records' do.
	prev := store.charge
	defer func() { store.charge = prev }()
	store.charge = func(n int64) error {
		samples += n
		if samples <= p.maxSamples {
			return nil
		}
		return refuse("aggregate.budget.maxSamples", "finding the value current at each window's start derives %d raw points beside the %d the read counted before it, over %d; narrow the range, or raise maxSamples (the cap is %d)",
			n, samples-n, p.maxSamples, substrate.AggregateMaxSamplesCap)
	}
	for _, f := range found {
		w := windows[f.window]
		r := store.rows[f.ref]
		if r == nil {
			continue
		}
		for i, pt := range r.points {
			if pt.available() && p.places(r.observedPoint(i), r.form, w[0], w[1]) {
				note(f.stream, r.form, pt.code, pt.offsetKnown)
			}
		}
	}
	if slices.Contains(forms, vocabulary.FormGauge) {
		for n, s := range streams {
			if s.stat != "" {
				continue
			}
			for _, w := range windows {
				c, err := store.before(s.info(), vocabulary.FormGauge, w[0], false)
				if err != nil {
					return nil, 0, err
				}
				if c == nil || c.Unavailable || carryEnded(c, w[0], p.maxGap) {
					continue
				}
				note(n, vocabulary.FormGauge, c.Code, !store.groups[c.Group].offsetUnknown)
			}
		}
	}
	keys := make([]key, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].n != keys[j].n {
			return keys[i].n < keys[j].n
		}
		return keys[i].form < keys[j].form
	})
	for _, k := range keys {
		c := *streams[k.n]
		c.form, c.codes = k.form, sortedKeys(got[k].codes)
		c.offsetless = got[k].offsetless
		out = append(out, &c)
	}
	return out, samples, nil
}

// priorityContext finds what a priority read of amounts or summaries weighs
// beside the points it takes. The policy drops a lower source's point where
// it overlaps a higher source's kept point, and that point may lie outside
// the read's windows: an amount from 23:30 to 00:30 drops a lower source's
// amount at 00:00 whether or not the range starts the day before. So the
// read takes the group's points in the windows widened by the chain's reach
// (priorityReach), and finds the streams of the group's key and form whose
// only points lie there: those it weighs and never places. The widened
// windows are answered for plan to read every priority group's points over,
// and the points the discovery derives are counted against maxSamples beside
// spent.
func (ds *dataset) priorityContext(ctx context.Context, store *obsStore, p *aggPlan, selected, streams []*aggStream, groups []*aggGroup, windows [][2]time.Time, spent int64) ([][2]time.Time, int64, error) {
	var forms []string
	for _, g := range groups {
		if len(g.sums) > 0 && p.source != substrate.AggregateFromRaw && !slices.Contains(forms, vocabulary.FormSummary) {
			forms = append(forms, vocabulary.FormSummary)
		}
		if g.form == vocabulary.FormAmount && p.source != substrate.AggregateFromSummaries && !slices.Contains(forms, vocabulary.FormAmount) {
			forms = append(forms, vocabulary.FormAmount)
		}
	}
	// The policy orders a group's streams by rank and breaks a tie by the
	// stream order (groupStreams), so every stream of a group is a link of
	// its own, ranked or not: the longest chain is the most streams of one
	// group and form. A stream's form is its newest point's (observed_latest)
	// and says nothing of the points before it, so every stream counts as a
	// link of each form the read weighs: a gauge written after a source's
	// amounts leaves those amounts in the chain.
	type chain struct {
		key     string
		summary bool
	}
	links := 0
	per := map[chain]int{}
	for _, s := range selected {
		_, k := p.groupKey(s, false)
		for _, form := range forms {
			c := chain{k, form == vocabulary.FormSummary}
			per[c]++
			links = max(links, per[c])
		}
	}
	if len(forms) == 0 || len(windows) == 0 || links < 2 {
		return nil, spent, nil
	}
	ext := priorityReach(selected, links)
	if ext == 0 {
		return nil, spent, nil
	}
	wide := make([][2]time.Time, len(windows))
	for i, w := range windows {
		reach := &obsStreamInfo{maxExtentMs: ext}
		hi := addMillis(w[1], ext)
		if hi.After(obsLatest) {
			hi = obsLatest
		}
		wide[i] = [2]time.Time{reach.reachBack(w[0]), hi}
	}
	var steps everyThousand
	contextWindows, err := joinSpans(ctx, wide, func() error { return steps.check(ctx) })
	if err != nil {
		return nil, 0, err
	}
	found, samples, err := ds.discoverForms(ctx, store, p, selected, contextWindows, spent, forms...)
	if err != nil {
		return nil, 0, err
	}
	type streamID struct {
		obsStream
		stat, form string
	}
	have := map[streamID]bool{}
	for _, s := range streams {
		have[streamID{s.obsStream, s.stat, s.form}] = true
	}
	byKey := map[string]*aggGroup{}
	for _, g := range groups {
		byKey[g.sortKey] = g
	}
	for _, s := range found {
		if have[streamID{s.obsStream, s.stat, s.form}] {
			continue
		}
		_, k := p.groupKey(s, false)
		g := byKey[k]
		switch {
		case g == nil:
		case s.form == vocabulary.FormSummary && len(g.sums) > 0, s.form != vocabulary.FormSummary && len(g.raw) > 0 && g.form == s.form:
			g.context = append(g.context, s)
		}
	}
	return contextWindows, samples, nil
}

// priorityReach is how far beyond its windows a priority read weighs the
// amounts or summaries of its streams: the farthest a chain of overlapping
// values, each of a higher rank than the one it drops, can reach from a
// value the windows place. A value spans no longer than its stream's longest
// record (max_extent_ms), so each link reaches no farther than the longest of
// those among the streams, and a chain has a link per stream of its group.
// It is milliseconds, never a time.Duration, and stops at
// maxPriorityReachMs: an extent may pass the 292 years a Duration holds, and
// a chain of them would overflow any count.
func priorityReach(streams []*aggStream, links int) int64 {
	var longest int64
	for _, s := range streams {
		longest = max(longest, s.maxExtentMs)
	}
	if longest > 0 && int64(links) > maxPriorityReachMs/longest {
		return maxPriorityReachMs
	}
	return int64(links) * longest
}

// maxPriorityReachMs is a reach wider than any two instants a record can
// name (years 0 to 9999), so a capped reach still takes every value.
const maxPriorityReachMs = int64(10001 * 366 * 24 * 3600 * 1000)

// obsLatest is the highest bound a widened window passes Postgres: no
// record names an instant past year 9999, and an offset moves one less than
// a day.
var obsLatest = time.Date(10001, time.January, 1, 0, 0, 0, 0, time.UTC)

// streamTiers sets each stream's rollup tiers from its metric's record, the
// default where the path names no metric record.
func (ds *dataset) streamTiers(ctx context.Context, tx *sql.Tx, streams []*aggStream) error {
	tiers := map[string][]time.Duration{}
	for _, s := range streams {
		t, ok := tiers[s.metric]
		if !ok {
			c, found, err := metricContract(ctx, tx, ds.registry(), s.metric)
			if err != nil {
				return err
			}
			t = c.Rollups
			if !found {
				t = rowTiers(nil)
			}
			tiers[s.metric] = t
		}
		s.tiers = t
	}
	return nil
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
		key, k := p.groupKey(s, p.policy == substrate.SourcesSeparate)
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
	if len(out) > p.groupLimit() {
		who := "the read"
		if p.isEntry() {
			who = "the metric"
		}
		return nil, p.refuse("aggregate.budget.maxGroups", "%s makes %d groups, over %d; narrow the filter or group by fewer keys (the cap is %d)",
			who, len(out), p.groupLimit(), substrate.AggregateMaxGroupsCap)
	}
	for _, g := range out {
		order := func(list []*aggStream) {
			sort.SliceStable(list, func(i, j int) bool {
				if list[i].rank != list[j].rank {
					return list[i].rank < list[j].rank
				}
				return aggStreamLess(list[i], list[j])
			})
		}
		order(g.raw)
		order(g.sums)
		for _, s := range g.raw {
			if g.form == "" {
				g.form = s.form
			} else if s.form != g.form {
				return nil, refuse(p.at, "the selected streams hold %s and %s values, and a row never mixes forms; group them apart (groupBy source) or narrow filter.kinds",
					g.form, s.form)
			}
			g.coded = g.coded || s.coded()
		}
	}
	return out, nil
}

// groupKey is the group a stream falls in: its value of each groupBy stream
// key, and where hideSource, its source too, kept apart without naming it
// (the separate policy). The sort key holds a part for every stream key,
// empty where it does not group, so groups sort by the keys in their order.
func (p *aggPlan) groupKey(s *aggStream, hideSource bool) (substrate.AggregateGroup, string) {
	var key substrate.AggregateGroup
	parts := make([]string, 0, 6)
	for _, g := range []struct {
		word, value string
		dst         *string
	}{
		{substrate.GroupMetric, s.metric, &key.Metric},
		{substrate.GroupSource, s.source, &key.Source},
		{substrate.GroupAbout, s.about, &key.About},
		{substrate.GroupChannel, s.channel, &key.Channel},
		{substrate.GroupStatistic, s.stat, &key.Statistic},
	} {
		switch {
		case p.grouped[g.word]:
			*g.dst = g.value
			parts = append(parts, g.value)
		case hideSource && g.word == substrate.GroupSource:
			parts = append(parts, g.value)
		default:
			parts = append(parts, "")
		}
	}
	return key, strings.Join(parts, "\x1f")
}

// seriesOf counts the series a read can answer: one per group, or where it
// groups by code one per group and code its streams' points hold in the
// read's windows (discoverForms), the filter's codes alone. A row is one
// series in one bucket and fill makes every series dense, so the series
// times the buckets bound the rows; it is checked against maxGroups here,
// before a point is read.
func (p *aggPlan) seriesOf(groups []*aggGroup) (int, error) {
	if !p.grouped[substrate.GroupCode] {
		return len(groups), nil
	}
	n := 0
	for _, g := range groups {
		codes := map[string]bool{}
		for _, s := range append(append([]*aggStream(nil), g.raw...), g.sums...) {
			for _, c := range s.codes {
				if p.codes == nil || p.codes[c] {
					codes[c] = true
				}
			}
		}
		n += len(codes)
	}
	if n > p.groupLimit() {
		return 0, p.refuse("aggregate.budget.maxGroups", "grouping by code makes %d groups, over %d; filter the codes or raise maxGroups (the cap is %d)",
			n, p.groupLimit(), substrate.AggregateMaxGroupsCap)
	}
	return n, nil
}

// --- units ---

// aggUnits is the read's units: the measure and unit of the values, and the
// unit of the durations durationIn and timeInState answer.
type aggUnits struct {
	measure  string
	value    string
	duration string
	conv     map[convKey]*convFactor
	// graph is the repository's unit graph under the unit graph switch
	// (decision 0186), nil before it: value is then a unit id, and every
	// stored unit converts into it through graph's linear conversions.
	graph *vocabulary.UnitGraph
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

// factor is the conversion of a stored unit into the values' unit. The
// metric read refuses through aggRun.conversion, which names the value.
func (u *aggUnits) factor(unit string, delta bool) (*convFactor, error) {
	k := convKey{unit, delta}
	if c, ok := u.conv[k]; ok {
		return c, nil
	}
	c := &convFactor{f: big.NewRat(1, 1), o: new(big.Rat), ff: 1, identity: true}
	if u.graph != nil && unit != "" && u.value != "" && unit != u.value {
		id, ok := u.graph.Resolve(unit)
		if !ok {
			return nil, fmt.Errorf("%q names no unit of this repository", unit)
		}
		if id != u.value {
			f, o, err := u.graph.Linear(id, u.value, delta)
			if err != nil {
				return nil, err
			}
			c.f, c.o = f, o
			c.ff, _ = f.Float64()
			c.fo, _ = o.Float64()
			if math.IsInf(c.ff, 0) || math.IsInf(c.fo, 0) || (c.ff == 0 && f.Sign() != 0) {
				return nil, fmt.Errorf("the factor from %s to %s is past the range of a float, which the read reduces in", id, u.value)
			}
			c.identity = false
		}
	} else if u.graph == nil && unit != "" && u.value != "" && unit != u.value {
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
// metric's measure, naming the measure. A stored unit that does not convert
// is refused where the read takes a value in it (aggRun.conversion), not here:
// a stream's unit is its newest point's, and a point outside the range
// refuses nothing.
func (ds *dataset) aggregateUnits(ctx context.Context, tx *sql.Tx, p *aggPlan, streams []*aggStream) (*aggUnits, error) {
	if p.graphUnits {
		return ds.aggregateUnitsGraph(ctx, tx, p, streams)
	}
	u := &aggUnits{duration: "s"}
	var metricUnit string
	if p.metric != "" {
		c, _, err := metricContract(ctx, tx, ds.registry(), p.metric)
		if err != nil {
			return nil, err
		}
		u.measure, metricUnit = c.Measures, c.Unit
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
	if p.durationUnit != "" {
		if !durations && p.kind != aggLatest && !p.isEntry() {
			return nil, refuse("aggregate.durationUnit", "durationUnit is the unit durationIn and timeInState answer their times in, and this read asks neither")
		}
		u.duration = p.durationUnit
	}
	if p.unit != "" {
		inDuration := resolves(p.unit, "duration")
		switch {
		case u.measure != "" && resolves(p.unit, u.measure):
			u.value = p.unit
		case inDuration && durations:
			return nil, refuse(p.path("unit"), "%s is a unit of duration, and unit converts the values of %s; name it in durationUnit, the unit durationIn and timeInState answer their times in",
				p.unit, aggMetricName(p))
		case u.measure == "":
			return nil, refuse(p.path("unit"), "%s converts nothing here: the selected streams hold codes; a timeInState answers its times in durationUnit", p.unit)
		default:
			return nil, refuse(p.path("unit"), "%s is not a unit of %s, the measure of %s; name one of its units",
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
var formReducers = substrate.FormReducers

// formWhy says why a reducer a form refuses does not apply.
var formWhy = map[string]string{
	"gauge":        "a gauge is a level, and levels do not add",
	"coded gauge":  "its values are codes, not numbers",
	"amount":       "an amount accrues over its span and holds nothing after it",
	"coded amount": "a coded amount is reduced by code",
	"counter":      "a counter is a running total, read by its increase",
	"summary":      "a summary is a statistic its source computed",
}

// additive reports whether sum adds the group's summaries: every statistic
// they hold adds.
func (g *aggGroup) additive() bool {
	for _, s := range g.sums {
		if !slices.Contains(substrate.AggregateAdditiveStatistics, s.stat) {
			return false
		}
	}
	return true
}

// rowStatistic is the one statistic a summary row's members hold, or an
// error naming the two it would mix. Under the priority and summaries
// policies a group's sources may hold different statistics (each source
// one, checkStatistics): a bucket where the policy keeps one source's
// summaries reads its statistic, and one that keeps two sources' of
// different statistics, a week of nights each source filled alone, is
// refused rather than averaged across methods.
func (r *aggRun) rowStatistic(g *aggGroup, ev *aggEvidence, b *aggBucket) (string, error) {
	if ev.form != vocabulary.FormSummary || len(g.sums) == 0 {
		return "", nil
	}
	stat := g.sums[0].stat
	if !g.mixedStatistics {
		return stat, nil
	}
	by := map[string][]string{}
	for _, m := range ev.members {
		s := g.sums[m.stream]
		if !slices.Contains(by[s.stat], orOwner(s.source)) {
			by[s.stat] = append(by[s.stat], orOwner(s.source))
		}
		stat = s.stat
	}
	if len(by) < 2 {
		return stat, nil
	}
	var parts []string
	for _, st := range sortedKeys(by) {
		parts = append(parts, fmt.Sprintf("%s from %s", orNoStatistic(st), strings.Join(by[st], ", ")))
	}
	from, to := r.p.rangeOf(b)
	return "", refuse(r.p.at, "the row from %s to %s takes the summaries %s, and a row reads one statistic; read a bucket each source's summaries fill alone (a day, a night), or name one statistic with %s",
		from.Format(time.RFC3339), to.Format(time.RFC3339), andList(parts), r.p.statisticPath())
}

// checkStatistics refuses a group whose summaries hold two statistics, since
// a row reads one, and on a read from summaries sum over a statistic that
// does not add. Under the priority and summaries policies each bucket takes
// the summaries of the source the policy keeps there, so the rule holds per
// source: two sources may hold two statistics (WHOOP's nightly HRV is
// `vendor`, Garmin's `avg`), and a row that would take both is refused where
// it is answered (rowStatistic). It runs once the forms are found, before
// any point is reduced, so a read that would count a night's average and
// maximum as two nights is refused, not answered. prefer-raw reads a bucket's
// summaries only where its raw points fall short, so a reducer over a
// statistic that is not its own answers null in such a bucket
// (rowValues.mixes) instead of refusing the buckets raw points serve.
func (p *aggPlan) checkStatistics(groups []*aggGroup) error {
	if p.source == substrate.AggregateFromRaw {
		return nil
	}
	split := ", or split them with groupBy statistic"
	if p.source != substrate.AggregateFromSummaries {
		split = ""
	}
	perSource := p.policy == substrate.SourcesPriority
	for _, g := range groups {
		var stats, sources []string
		bySource := map[string][]string{}
		for _, s := range g.sums {
			if !slices.Contains(stats, s.stat) {
				stats = append(stats, s.stat)
			}
			if !slices.Contains(sources, orOwner(s.source)) {
				sources = append(sources, orOwner(s.source))
			}
			if !slices.Contains(bySource[orOwner(s.source)], s.stat) {
				bySource[orOwner(s.source)] = append(bySource[orOwner(s.source)], s.stat)
			}
		}
		slices.Sort(stats)
		slices.Sort(sources)
		switch {
		case len(stats) > 1 && perSource:
			for _, source := range sources {
				if own := bySource[source]; len(own) > 1 {
					slices.Sort(own)
					return refuse(p.at, "the summaries from %s hold the statistics %s, and a row reads one; name it with %s%s",
						source, andList(own), p.statisticPath(), split)
				}
			}
			g.mixedStatistics, g.statisticAt = true, p.statisticPath()
		case len(stats) > 1:
			return refuse(p.at, "the summaries from %s hold the statistics %s, and a row reads one; name it with %s%s",
				strings.Join(sources, ", "), andList(stats), p.statisticPath(), split)
		}
		if p.source != substrate.AggregateFromSummaries || g.additive() {
			continue
		}
		for i, r := range p.reducers {
			if r.Name == substrate.ReducerSum {
				var not []string
				for _, st := range stats {
					if !slices.Contains(substrate.AggregateAdditiveStatistics, st) {
						not = append(not, orNoStatistic(st))
					}
				}
				return p.refuse(p.reducerPath(i), "sum adds summaries whose statistic is sum or count; the summaries from %s are %s, so read them with count, first, last, avg, min or max",
					strings.Join(sources, ", "), andList(not))
			}
		}
	}
	return nil
}

func andList(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

func orNoStatistic(stat string) string {
	if stat == "" {
		return "of no statistic"
	}
	return stat
}

// checkForms refuses a reducer a group's form does not take, naming the form,
// and the circular mean of a value that is not an angle.
func (p *aggPlan) checkForms(groups []*aggGroup, u *aggUnits) error {
	if err := p.checkStatistics(groups); err != nil {
		return err
	}
	if p.bucket.placeEnd && !p.bucket.placeNamed {
		// The metric's bucketPlace, not the request, asked for the end: its
		// instants place the same by either, and by start rollups serve them.
		instants := true
		for _, g := range groups {
			if len(g.sums) > 0 || (len(g.raw) > 0 && g.form != vocabulary.FormGauge && g.form != vocabulary.FormCounter) {
				instants = false
			}
		}
		if instants {
			p.bucket.placeEnd = false
		}
	}
	for _, g := range groups {
		if p.bucket.placeEnd && p.bucket.placeNamed && len(g.raw) > 0 && (g.form == vocabulary.FormGauge || g.form == vocabulary.FormCounter) {
			return refuse("aggregate.bucket.place", "place end puts an amount or a summary in the bucket its span ends in; the %s values of %s are instants, read them by start",
				g.form, aggMetricName(p))
		}
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
			return p.refuse("aggregate.sources.policy", "priority does not resolve two counters' totals per interval; read them separate (stream from %s and %s)",
				orOwner(g.raw[0].source), orOwner(g.raw[1].source))
		}
		for _, form := range forms {
			for i, r := range p.reducers {
				if !slices.Contains(formReducers[form], r.Name) {
					return p.refuse(p.reducerPath(i), "%s does not apply to %s %s: %s; %s %s takes %s",
						r.String(), article(form), form, formWhy[form], article(form), form, strings.Join(formReducers[form], ", "))
				}
				if r.Name == substrate.ReducerCircularMean && u.graph != nil {
					if u.graph.Turn(u.value) == nil {
						return refuse(p.reducerPath(i), "circularMean averages angles on the circle; %s holds values in %s, which declares no turn",
							aggMetricName(p), orCodes(u.value))
					}
					continue
				}
				if r.Name == substrate.ReducerCircularMean && u.measure != "angle" {
					return refuse(p.reducerPath(i), "circularMean averages angles on the circle; %s holds %s",
						aggMetricName(p), orNothing(u.measure))
				}
			}
		}
	}
	if u.graph != nil {
		return p.checkLevelsIn(u.graph, u.value, aggMetricName(p))
	}
	return p.checkLevels(u.measure, aggMetricName(p))
}

// levelAdds are the reducers that add values or their changes, which a
// logarithmic measure's values never do: 61 dB and 62 dB are not 123 dB.
var levelAdds = []string{substrate.ReducerSum, substrate.ReducerIncrease, substrate.ReducerRate}

// energyLevels are the levels of a power against a stated reference. Their
// mean is the level of the mean power (60 and 80 dB[SPL], an hour each, hold
// 77.0 dB[SPL] over the two hours), so an arithmetic mean of the decibels,
// and the deviation around it, misstates them. A level against no reference
// (dB, such as a hearing threshold) and an acidity do average: the mean of
// pH values is the pH of the geometric mean activity.
var energyLevels = []string{"sound-pressure-level", "power-level"}

// levelMeans are the reducers that take an arithmetic mean of the values.
var levelMeans = []string{substrate.ReducerAvg, substrate.ReducerStdDev}

// levelRefusal says why a reducer does not apply to a measure's values, or
// "" where it does.
func levelRefusal(measure, reducer string) string {
	m, ok := vocabulary.LookupMeasure(measure)
	if !ok || m.Scale != vocabulary.ScaleLogarithmic {
		return ""
	}
	switch {
	case slices.Contains(levelAdds, reducer):
		return fmt.Sprintf("%s is logarithmic, and its values do not add (61 and 62 %s are not 123 %s)", measure, m.Base, m.Base)
	case slices.Contains(energyLevels, measure) && slices.Contains(levelMeans, reducer):
		return fmt.Sprintf("%s is the level of a power, whose mean is not the mean of its decibels (60 and 80 %s an hour each hold 77.0, not 70)", measure, m.Base)
	}
	return ""
}

// checkLevels refuses, on every form and every read, a reducer or a rolling
// reducer that adds or averages what a logarithmic measure's values do not
// (levelRefusal), what naming the metric or the property read. count, min,
// max, first, last, percentile and durationIn read a level as it is.
func (p *aggPlan) checkLevels(measure, what string) error {
	for i, r := range p.reducers {
		if why := levelRefusal(measure, r.Name); why != "" {
			return p.refuse(p.reducerPath(i), "%s does not apply to %s: %s; read count, min, max, first, last or percentile",
				r.String(), what, why)
		}
	}
	if p.rolling == nil {
		return nil
	}
	for i, r := range p.rolling.Reducers {
		if why := levelRefusal(measure, r.Name); why != "" {
			return p.refuse(fmt.Sprintf("aggregate.rolling.reducers[%d]", i), "a rolling %s does not apply to %s: %s; roll min or max",
				r.String(), what, why)
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
		return "codes"
	}
	return "values of " + measure
}
