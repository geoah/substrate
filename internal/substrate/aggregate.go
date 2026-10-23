package substrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/strictjson"
)

// The aggregate read (decision 0161): a mode of the records route that
// reduces the observed index (decision 0160) or a declared number of the
// filtered kinds into buckets, checked against forms, units and budgets
// before it runs. The request is the `aggregate` parameter (Aggregate), with
// `downsample` (Downsample) beside it for a chart's columns; the answer is
// one shape, AggregateResult, pinned by the wire golden and mirrored in the
// console's types.ts. docs/api.md has the grammar.

// The reducers, by name. A reducer that takes an argument is an object with
// the name as its one key: {"percentile": 0.05}, {"durationIn": [95, 114]},
// {"timeInState": ["open"]}.
const (
	ReducerCount        = "count"
	ReducerSum          = "sum"
	ReducerAvg          = "avg"
	ReducerMin          = "min"
	ReducerMax          = "max"
	ReducerFirst        = "first"
	ReducerLast         = "last"
	ReducerStdDev       = "stddev"
	ReducerPercentile   = "percentile"
	ReducerDurationIn   = "durationIn"
	ReducerTimeInState  = "timeInState"
	ReducerIncrease     = "increase"
	ReducerRate         = "rate"
	ReducerCircularMean = "circularMean"
)

// AggregateReducers lists every reducer name, in the order docs/api.md gives
// them.
var AggregateReducers = []string{
	ReducerCount, ReducerSum, ReducerAvg, ReducerMin, ReducerMax, ReducerFirst, ReducerLast,
	ReducerStdDev, ReducerPercentile, ReducerDurationIn, ReducerTimeInState, ReducerIncrease, ReducerRate,
	ReducerCircularMean,
}

// FormReducers is the reducers each form admits, by form: `gauge`, `amount`,
// `counter` and `summary`, and `coded gauge` and `coded amount` for a metric
// with a code set. The aggregate read refuses any other for the form, and
// the metric catalogue's `reducers` are held to this table.
var FormReducers = map[string][]string{
	"gauge": {
		ReducerCount, ReducerAvg, ReducerMin, ReducerMax,
		ReducerFirst, ReducerLast, ReducerStdDev, ReducerPercentile, ReducerDurationIn,
		ReducerCircularMean,
	},
	"coded gauge": {ReducerCount, ReducerFirst, ReducerLast, ReducerTimeInState},
	"amount": {
		ReducerCount, ReducerSum, ReducerAvg, ReducerMin, ReducerMax,
		ReducerFirst, ReducerLast, ReducerStdDev, ReducerPercentile,
	},
	"coded amount": {ReducerCount, ReducerSum, ReducerMin, ReducerMax},
	"counter": {
		ReducerCount, ReducerFirst, ReducerLast, ReducerMin, ReducerMax,
		ReducerIncrease, ReducerRate,
	},
	"summary": {
		ReducerCount, ReducerFirst, ReducerLast, ReducerSum, ReducerMin,
		ReducerMax, ReducerAvg,
	},
}

// AggregateAdditiveStatistics are the summary statistics `sum` adds: a total
// of totals or of counts. A day's average, extreme or vendor score does not
// add, so `sum` over such summaries is refused.
var AggregateAdditiveStatistics = []string{ReducerSum, ReducerCount}

// The `from` values: which points a read takes.
const (
	AggregateFromRaw       = "raw"
	AggregateFromSummaries = "summaries"
	AggregateFromPreferRaw = "prefer-raw"
)

// The source policies.
const (
	SourcesSeparate = "separate"
	SourcesPriority = "priority"
)

// FillZero is the one fill: a bucket a series holds no row in answers zero
// where a number answers.
const FillZero = "zero"

// The budgets: each default applies where the request names none, and a
// request naming more than the cap is refused naming the cap.
const (
	// AggregateMaxBuckets bounds groups times buckets.
	AggregateMaxBuckets    = 1440
	AggregateMaxBucketsCap = 10000
	// AggregateMaxSamples bounds the work one read materializes: the raw
	// points and rollup rows it takes, the records the records time mode
	// matches and weighs, each extra window a point is placed in, and the
	// values a rolling window reads. The cap is what one process holds in
	// memory for one read (docs/api.md, "Budgets").
	AggregateMaxSamples    = 1000000
	AggregateMaxSamplesCap = 2000000
	// AggregateMaxGroups bounds the groups a groupBy (or the separate
	// policy's split by source) may produce.
	AggregateMaxGroups    = 200
	AggregateMaxGroupsCap = 2000
	// AggregateTimeout bounds the whole read, its statements and the work
	// after them, inside any earlier deadline the caller set.
	AggregateTimeout = 30 * time.Second
	// AggregateMaxMetrics bounds the metrics one read names in `metrics`.
	// Each metric runs its own statements (its streams, its forms, its rows
	// and points) in the read's one snapshot, so the count bounds the
	// statements a read runs; a day's or a vitals screen names fewer.
	AggregateMaxMetrics = 16
)

// AggregateQuery is one aggregate read: the records filter, the aggregate
// body, and the downsample beside it when a chart asks for columns. It is
// also the body of a function's host.records.aggregate; the agent query tool
// takes its three parts as its filter, aggregate and downsample arguments.
type AggregateQuery struct {
	Filter     Filter      `json:"filter"`
	Aggregate  *Aggregate  `json:"aggregate,omitempty"`
	Downsample *Downsample `json:"downsample,omitempty"`
}

// Aggregate is the `aggregate` parameter: what to reduce, over which range,
// in which buckets, with which reducers.
type Aggregate struct {
	// Metric selects the observed index by a metric record's reference.
	Metric *AggregateMetric `json:"metric,omitempty"`
	// Metrics selects several metrics read over one range, bucket, groupBy
	// and sources policy, each with its own unit, reducers, from and
	// statistic: one set of rows, grouped by metric.
	Metrics []AggregateMetricRead `json:"metrics,omitempty"`
	// Property selects a declared number of the filtered kinds instead.
	Property *AggregateProperty `json:"property,omitempty"`
	// Latest asks for each group's newest point instead of buckets: no
	// range, bucket or reducers; the metric, the filter, groupBy and unit
	// alone.
	Latest bool `json:"latest,omitempty"`
	// From is raw (the default), summaries or prefer-raw.
	From     string             `json:"from,omitempty"`
	Range    *AggregateRange    `json:"range,omitempty"`
	Bucket   *AggregateBucket   `json:"bucket,omitempty"`
	GroupBy  []string           `json:"groupBy,omitempty"`
	Reducers []AggregateReducer `json:"reducers,omitempty"`
	Sources  *AggregateSources  `json:"sources,omitempty"`
	// Unit is the unit the values answer in: a unit of the metric's unit's
	// linear component, or on a property read of the property's.
	Unit string `json:"unit,omitempty"`
	// DurationUnit is the unit durationIn and timeInState answer their
	// times in (s by default).
	DurationUnit string `json:"durationUnit,omitempty"`
	// MaxGap caps a gauge's hold: no value stands longer than this after its
	// point. An ISO 8601 duration.
	MaxGap string `json:"maxGap,omitempty"`
	// Fill is zero or absent: zero answers a bucket a series holds no row in.
	Fill    string            `json:"fill,omitempty"`
	Rolling *AggregateRolling `json:"rolling,omitempty"`
	Budget  *AggregateBudget  `json:"budget,omitempty"`
}

// RetiredKeys are the keys the aggregate body took before v1 and refuses
// now, each with what replaced it (strictjson.Retiring): a decode of the
// body alone checks them before UnmarshalJSON runs.
func (Aggregate) RetiredKeys() map[string]string { return retiredAggregateKeys }

// UnmarshalJSON decodes the body strictly, refusing a key it took before v1
// by name, also where the body sits inside an AggregateQuery.
func (a *Aggregate) UnmarshalJSON(raw []byte) error {
	type plain Aggregate
	if err := refuseRetired(raw, "", retiredAggregateKeys); err != nil {
		return err
	}
	return strictjson.DecodeBytes(raw, (*plain)(a), false)
}

// RetiredKeys are the keys a downsample took before v1 and refuses now.
func (Downsample) RetiredKeys() map[string]string { return retiredDownsampleKeys }

// UnmarshalJSON decodes a downsample strictly, refusing a key it took before
// v1 by name.
func (d *Downsample) UnmarshalJSON(raw []byte) error {
	type plain Downsample
	if err := refuseRetired(raw, "", retiredDownsampleKeys); err != nil {
		return err
	}
	return strictjson.DecodeBytes(raw, (*plain)(d), false)
}

// retiredBucketKeys and retiredRecordsKeys are the keys the bucket and its
// records mode took before v1 and refuse now.
var (
	retiredAggregateKeys = map[string]string{
		"minCoverage": "prefer-raw takes a bucket's raw points where they cover 0.9 of it, a fixed rule",
	}
	retiredDownsampleKeys = map[string]string{"method": "every downsample is M4: first, last, min and max per column"}
	retiredBucketKeys     = map[string]string{
		"offset": "a calendar bucket starts where the calendar does; read in a timezone, or bucket by every with an origin",
	}
	retiredRecordsKeys = map[string]string{
		"around":     "a window is its record's own span; write the span the read should take on the record",
		"maxRecords": "the records a window read matches count against budget.maxSamples",
	}
)

// refuseRetired refuses a key of the JSON object raw that retired names,
// written at the path at.
func refuseRetired(raw []byte, at string, retired map[string]string) error {
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil {
		return nil
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		if why, ok := retired[k]; ok {
			return fmt.Errorf("%s%s was removed: %s", at, k, why)
		}
	}
	return nil
}

// AggregateMetric names a metric record by its full record path.
type AggregateMetric struct {
	Ref string `json:"ref"`
}

// AggregateMetricRead is one metric of a several-metric read: its record
// path, the unit its values answer in (the metric record's by default) and
// its durations' unit (the read's by default), its reducers and from (the
// read's by default), and the one summary statistic its summaries are read
// at.
type AggregateMetricRead struct {
	Ref          string             `json:"ref"`
	Unit         string             `json:"unit,omitempty"`
	DurationUnit string             `json:"durationUnit,omitempty"`
	Reducers     []AggregateReducer `json:"reducers,omitempty"`
	From         string             `json:"from,omitempty"`
	Statistic    string             `json:"statistic,omitempty"`
}

// AggregateProperty names a declared numeric, quantity, money or duration
// property of the filtered kinds, and the datetime property that places a
// record in time (`at` by default).
type AggregateProperty struct {
	Name   string `json:"name"`
	Anchor string `json:"anchor,omitempty"`
	// UTCOffset names the declared int property holding each record's UTC
	// offset in minutes at its anchor (utcOffsetEnd beside endsAt), which
	// the recorded-local calendar places the record by.
	UTCOffset string `json:"utcOffset,omitempty"`
	// GroupBy names declared string, enum or reference properties of the
	// records (sport, source), one group per combination of their values;
	// aggregate.groupBy groups nothing on a property read.
	GroupBy []string `json:"groupBy,omitempty"`
	// Sources chooses among records of several sources before any is
	// reduced, as the records mode's sources choose among its windows.
	Sources *AggregateRecordSources `json:"sources,omitempty"`
}

// The records' source policies: every record (the default), or the
// highest-ranked source's where two sources' records overlap.
const (
	RecordSourcesSeparate = "separate"
	RecordSourcesPriority = "priority"
)

// AggregateRecordSources chooses among records several sources wrote of one
// thing (a night, a run): under priority a record whose [at, endsAt)
// overlaps a record of a higher-ranked source, the Match properties equal,
// is dropped. Property names the records' declared property that holds
// their source (a reference, a string or an enum), and Order ranks its
// values, first highest; a value Order does not name ranks last.
type AggregateRecordSources struct {
	Policy   string   `json:"policy,omitempty"`
	Order    []string `json:"order,omitempty"`
	Property string   `json:"property,omitempty"`
	Match    []string `json:"match,omitempty"`
}

// AggregateRange is the half-open range [gte, lt), two RFC 3339 instants.
// With RecordedLocal it is whole recorded-local dates instead: gte and lt
// name dates (`2026-10-01`, or a midnight instant, whose date is read as
// written), and a day's row is in range where the date its own UTC offset
// names lies in [gte, lt). Only a recorded-local day read (calendar day, no
// timezone) takes it.
type AggregateRange struct {
	Gte           string `json:"gte"`
	Lt            string `json:"lt"`
	RecordedLocal bool   `json:"recordedLocal,omitempty"`
}

// AggregateBucket is one of the four time modes: `every` (with `origin`) on
// UTC instants; `calendar` with a `timezone`; `calendar` alone, the
// recorded-local day; `records`, each matched record's [at, endsAt). In the
// response it is the request's bucket as the read applied it, its place
// the metric's bucketPlace where the request named none.
type AggregateBucket struct {
	// Every is an ISO 8601 duration; Origin the instant buckets count from
	// (the Unix epoch by default).
	Every  string `json:"every,omitempty"`
	Origin string `json:"origin,omitempty"`
	// Calendar is hour, day, week or month.
	Calendar string `json:"calendar,omitempty"`
	// Timezone is an IANA zone; absent with Calendar means the recorded-local
	// calendar, each point placed by its own UTC offset.
	Timezone string `json:"timezone,omitempty"`
	// FallbackTimezone is the IANA zone the recorded-local calendar places a
	// value written without a UTC offset by; without it such a value is
	// refused, never placed on its UTC date.
	FallbackTimezone string `json:"fallbackTimezone,omitempty"`
	// WeekStart is the first day of a week, monday by default.
	WeekStart string `json:"weekStart,omitempty"`
	// Place is where a value with a span lands: "start", the bucket its start
	// lies in, or "end", the bucket its last instant lies in, so a night
	// lands on the day it ends. Absent, it is the metric's bucketPlace, else
	// start. A gauge's and a counter's points are instants and are refused
	// under "end".
	Place   string            `json:"place,omitempty"`
	Records *AggregateRecords `json:"records,omitempty"`
}

// UnmarshalJSON decodes a bucket strictly, refusing a key it took before v1
// by name.
func (b *AggregateBucket) UnmarshalJSON(raw []byte) error {
	type plain AggregateBucket
	if err := refuseRetired(raw, "bucket.", retiredBucketKeys); err != nil {
		return err
	}
	return strictjson.DecodeBytes(raw, (*plain)(b), false)
}

// The places of a value with a span.
const (
	AggregatePlaceStart = "start"
	AggregatePlaceEnd   = "end"
)

// AggregateRecords is the records time mode: the records the filter matches
// inside the range are the buckets, each its [at, endsAt). Sources keeps one
// source's record where several sources' records overlap, and UTCOffsets
// names the properties a window's start and end are written at.
type AggregateRecords struct {
	Filter     Filter                  `json:"filter"`
	Sources    *AggregateRecordSources `json:"sources,omitempty"`
	UTCOffsets *AggregateRecordOffsets `json:"utcOffsets,omitempty"`
}

// UnmarshalJSON decodes the records mode strictly, refusing a key it took
// before v1 by name.
func (r *AggregateRecords) UnmarshalJSON(raw []byte) error {
	type plain AggregateRecords
	if err := refuseRetired(raw, "bucket.records.", retiredRecordsKeys); err != nil {
		return err
	}
	return strictjson.DecodeBytes(raw, (*plain)(r), false)
}

// AggregateRecordOffsets names the records' declared int properties holding
// their UTC offset in minutes where they start and where they end
// (utcOffsetStart, utcOffsetEnd); End is Start's where absent. A row's start
// and end are written at its record's offsets, the range's where the record
// holds none.
type AggregateRecordOffsets struct {
	Start string `json:"start"`
	End   string `json:"end,omitempty"`
}

// AggregateSources is the source policy: separate (the default, one row per
// source) or priority (an ordered list resolved per stored interval before
// bucketing). Order names sources by record path or by id.
type AggregateSources struct {
	Policy string   `json:"policy,omitempty"`
	Order  []string `json:"order,omitempty"`
}

// The rolling windows' ends.
const (
	RollingThroughCurrent  = "current"
	RollingThroughPrevious = "previous"
)

// AggregateRolling adds, per row, the given reducers over a window of Buckets
// buckets, filled buckets included: this bucket and the Buckets-1 before it,
// or with Through "previous" the Buckets before it, so a baseline leaves the
// bucket itself out. A window reaches before the range: the read reduces the
// buckets it needs there and answers none of them.
type AggregateRolling struct {
	Buckets  int                `json:"buckets"`
	Reducers []AggregateReducer `json:"reducers,omitempty"`
	Through  string             `json:"through,omitempty"`
}

// AggregateBudget bounds a read before it runs. Zero is the default.
type AggregateBudget struct {
	MaxBuckets int   `json:"maxBuckets,omitempty"`
	MaxSamples int64 `json:"maxSamples,omitempty"`
	MaxGroups  int   `json:"maxGroups,omitempty"`
}

// The budget paths a read's refusal names.
const (
	AggregateMaxBucketsPath = "aggregate.budget.maxBuckets"
	AggregateMaxGroupsPath  = "aggregate.budget.maxGroups"
)

// NamedRecord is a record path a request names beyond its filters' kinds,
// and where it names it.
type NamedRecord struct {
	Path string
	At   string
}

// RecordsNamed are the record paths an aggregate read names beyond its
// filters' kinds: the targets of either filter's referencing, and every
// metric it names, whose record the read looks up for its unit. A caller holding a reads allowlist holds each path's kind to it, as
// it holds the kinds; a referencing that names no target is an error.
func (q AggregateQuery) RecordsNamed() ([]NamedRecord, error) {
	out, err := q.Filter.Referencing.Named("filter.referencing")
	if err != nil {
		return nil, err
	}
	a := q.Aggregate
	if a == nil {
		return out, nil
	}
	if b := a.Bucket; b != nil && b.Records != nil {
		more, err := b.Records.Filter.Referencing.Named("aggregate.bucket.records.filter.referencing")
		if err != nil {
			return nil, err
		}
		out = append(out, more...)
	}
	if a.Metric != nil {
		out = append(out, NamedRecord{Path: a.Metric.Ref, At: "aggregate.metric.ref"})
	}
	for i, m := range a.Metrics {
		out = append(out, NamedRecord{Path: m.Ref, At: fmt.Sprintf("aggregate.metrics[%d].ref", i)})
	}
	return out, nil
}

// LimitRows lowers the read's own budget so it answers at most rows rows,
// for a caller holding a row budget of its own (a function's, an agent's).
// budget.maxBuckets bounds a bucketed read's series times the buckets it
// answers (a rolling window's lookback and the records a records-mode source
// policy drops are not counted) and budget.maxGroups the latest read's
// groups, and the read checks each before it takes a point, so a read past
// the caller's budget is refused before it runs. A budget the request names outside 0 to its cap is left for the read
// to refuse. It answers the budget paths it lowered, which
// AggregateRowsRefused matches a refusal against; q's aggregate is copied,
// never written through.
func (q *AggregateQuery) LimitRows(rows int) []string {
	if q.Aggregate == nil || rows <= 0 {
		return nil
	}
	agg := *q.Aggregate
	budget := AggregateBudget{}
	if agg.Budget != nil {
		budget = *agg.Budget
	}
	var lowered []string
	lower := func(n *int, def, limit int, path string) {
		cur := *n
		if cur < 0 || cur > limit {
			return
		}
		if cur == 0 {
			cur = def
		}
		if cur > rows {
			*n = rows
			lowered = append(lowered, path)
		}
	}
	lower(&budget.MaxBuckets, AggregateMaxBuckets, AggregateMaxBucketsCap, AggregateMaxBucketsPath)
	if agg.Latest {
		// The latest read's rows are its groups.
		lower(&budget.MaxGroups, AggregateMaxGroups, AggregateMaxGroupsCap, AggregateMaxGroupsPath)
	}
	if lowered != nil {
		agg.Budget = &budget
		q.Aggregate = &agg
	}
	return lowered
}

// AggregateRowsRefused reports whether err is a read's refusal on one of the
// budget paths LimitRows lowered: the caller's row budget refused it, not
// the request's own.
func AggregateRowsRefused(err error, lowered []string) bool {
	var ve *ValidationError
	if len(lowered) == 0 || !errors.As(err, &ve) {
		return false
	}
	for _, d := range ve.Details() {
		if slices.Contains(lowered, d.Path) {
			return true
		}
	}
	return false
}

// Downsample is the `downsample` parameter: Points columns over the range,
// each answering first, last, min and max (M4).
type Downsample struct {
	Points int `json:"points"`
}

// AggregateReducer is one reducer: a bare name, or an object naming one
// reducer with its argument. On the wire it is spelled exactly as the request
// spelled it, so a value's reducer reads back as the reducer that asked.
type AggregateReducer struct {
	Name string
	// Percentile is percentile's q, 0 to 1.
	Percentile *float64
	// Between is durationIn's [lo, hi), in the values' unit.
	Between []float64
	// States is timeInState's codes; nil (the bare word) and empty both mean
	// every code the stream holds.
	States []string
}

// MarshalJSON writes the bare name, or the one-key object of a reducer that
// carries its argument.
func (r AggregateReducer) MarshalJSON() ([]byte, error) {
	switch {
	case r.Name == ReducerPercentile && r.Percentile != nil:
		return json.Marshal(map[string]float64{ReducerPercentile: *r.Percentile})
	case r.Name == ReducerDurationIn && r.Between != nil:
		return json.Marshal(map[string][]float64{ReducerDurationIn: r.Between})
	case r.Name == ReducerTimeInState && r.States != nil:
		return json.Marshal(map[string][]string{ReducerTimeInState: r.States})
	}
	return json.Marshal(r.Name)
}

// UnmarshalJSON reads a bare name or a one-key object. Which names exist and
// what each takes is the read's to judge, so an unknown name decodes and is
// refused there by name; only the shape is held here.
func (r *AggregateReducer) UnmarshalJSON(b []byte) error {
	*r = AggregateReducer{}
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &r.Name)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("a reducer is a name or an object such as {\"percentile\": 0.05}")
	}
	if len(obj) != 1 {
		return fmt.Errorf("a reducer object names one reducer, such as {\"durationIn\": [95, 114]}")
	}
	for name, raw := range obj {
		r.Name = name
		var err error
		switch name {
		case ReducerPercentile:
			err = json.Unmarshal(raw, &r.Percentile)
		case ReducerDurationIn:
			err = json.Unmarshal(raw, &r.Between)
		case ReducerTimeInState:
			r.States = []string{}
			err = json.Unmarshal(raw, &r.States)
			if r.States == nil {
				r.States = []string{}
			}
		default:
			return fmt.Errorf("%q takes no argument: write it as the bare name; percentile, durationIn and timeInState take one", name)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// String is the reducer as a refusal names it: the bare name, or the
// one-key object of a reducer with its argument (`{"percentile":0.05}`).
func (r AggregateReducer) String() string {
	b, err := r.MarshalJSON()
	if err != nil || (len(b) > 0 && b[0] == '"') {
		return r.Name
	}
	return string(b)
}

// AggregateResult is the one answer of an aggregate read.
type AggregateResult struct {
	// Metric is the metric record's path, or Property the property, as read.
	Metric   string `json:"metric,omitempty"`
	Property string `json:"property,omitempty"`
	// Unit is the unit the values are in; absent on a several-metric read,
	// whose Units names each metric's.
	Unit string `json:"unit,omitempty"`
	// Units is, on a several-metric read, each metric's unit by its record
	// path: its values' unit, or a coded metric's durations unit.
	Units  map[string]string `json:"units,omitempty"`
	Bucket *AggregateBucket  `json:"bucket,omitempty"`
	// From is the request's from; absent on a several-metric read whose
	// metrics read from different points.
	From string `json:"from,omitempty"`
	// Exact is false when any value came from a float64 reduction or a unit
	// conversion with no finite decimal expansion. It says nothing about
	// whether the index is current: a read over a kind whose observed index
	// is being re-derived is refused with ErrUnavailable instead.
	Exact bool           `json:"exact"`
	Rows  []AggregateRow `json:"rows"`
	// Sources lists every source that fed a row, by record path; a stream
	// that names no source adds none.
	Sources []string      `json:"sources,omitempty"`
	Head    AggregateHead `json:"head"`
}

// UnmarshalJSON decodes an answer leniently. The bucket it echoes is a
// request type whose own decode is strict, which is right for a request and
// wrong for a client reading an answer from a newer server: a key this build
// does not know is dropped here, never a failed read.
func (r *AggregateResult) UnmarshalJSON(raw []byte) error {
	type plain AggregateResult
	var shadow struct {
		*plain
		Bucket json.RawMessage `json:"bucket,omitempty"`
	}
	shadow.plain = (*plain)(r)
	if err := json.Unmarshal(raw, &shadow); err != nil {
		return err
	}
	r.Bucket = nil
	if len(shadow.Bucket) == 0 || string(shadow.Bucket) == "null" {
		return nil
	}
	type plainBucket AggregateBucket
	var bucket struct {
		*plainBucket
		Records json.RawMessage `json:"records,omitempty"`
	}
	bucket.plainBucket = &plainBucket{}
	if err := json.Unmarshal(shadow.Bucket, &bucket); err != nil {
		return fmt.Errorf("bucket: %w", err)
	}
	r.Bucket = (*AggregateBucket)(bucket.plainBucket)
	r.Bucket.Records = nil
	if len(bucket.Records) > 0 && string(bucket.Records) != "null" {
		type plainRecords AggregateRecords
		records := &plainRecords{}
		if err := json.Unmarshal(bucket.Records, records); err != nil {
			return fmt.Errorf("bucket.records: %w", err)
		}
		r.Bucket.Records = (*AggregateRecords)(records)
	}
	return nil
}

// AggregateHead is the changelog position and history generation the read
// was served at, as a list page reports them.
type AggregateHead struct {
	Changelog  int64  `json:"changelog"`
	Generation string `json:"generation"`
}

// AggregateRow is one bucket of one group.
type AggregateRow struct {
	// Start and End bound the bucket, each with its own UTC offset.
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	// Form is the form of the points that fed the row; a row never mixes
	// forms. Absent on a property read.
	Form  string          `json:"form,omitempty"`
	Group *AggregateGroup `json:"group,omitempty"`
	// Record is the matched record in the records time mode.
	Record *AggregateRecord `json:"record,omitempty"`
	// From says whether raw points or summaries served the row.
	From string `json:"from,omitempty"`
	// Sources lists the sources that fed the row, by record path; a stream
	// that names no source adds none.
	Sources []string `json:"sources,omitempty"`
	// Coverage is the time the row's points cover, an ISO 8601 duration:
	// held time for a gauge, the union of spans for an amount or a summary,
	// the time a counter's steps span.
	Coverage string           `json:"coverage,omitempty"`
	Values   []AggregateValue `json:"values"`
	// Count is the points that fed the row (a stream's point count on the
	// latest read, the records on a property read).
	Count int64 `json:"count"`
	// First is, on the latest read, the instant of the group's earliest
	// available point, at that point's own record's offset: where a
	// timeline reading "all" of a metric starts.
	First *time.Time `json:"first,omitempty"`
	// Offsetless counts the points among Count written without a UTC offset,
	// which the recorded-local calendar placed by bucket.fallbackTimezone.
	Offsetless int64 `json:"offsetless,omitempty"`
	// Prorated counts the amounts among Count a records-mode window cut:
	// each counts by the part of its span inside the window, its number
	// taken in proportion.
	Prorated int64 `json:"prorated,omitempty"`
	// Bounded counts the values among Count written with a comparator (a
	// lab's `<0.01`), whose true number is unknown: every reducer whose
	// answer depends on it (sum, avg, stddev, percentile, durationIn,
	// circularMean, increase, rate) answers null for the row, count counts
	// them, first and last answer them with their comparator, and min and
	// max answer with a comparator where the bounds decide it.
	Bounded int64 `json:"bounded,omitempty"`
	// Synthetic marks a row `fill` created.
	Synthetic bool `json:"synthetic,omitempty"`
	// NonAdditive marks a row whose window may overlap another's (the records
	// time mode), so its sums do not add up to a total.
	NonAdditive bool `json:"nonAdditive,omitempty"`
}

// AggregateGroup is a row's group: one key per groupBy word, a record path
// for source, about and metric, the code for code, a summary's statistic
// for statistic; the currency of a money property; on a property read, each
// property.groupBy property's value (a reference's record path), absent
// where the records hold none.
type AggregateGroup struct {
	Source     string            `json:"source,omitempty"`
	About      string            `json:"about,omitempty"`
	Code       string            `json:"code,omitempty"`
	Metric     string            `json:"metric,omitempty"`
	Channel    string            `json:"channel,omitempty"`
	Statistic  string            `json:"statistic,omitempty"`
	Currency   string            `json:"currency,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// AggregateRecord is the record a records-mode row's window came from, at the
// version the read saw.
type AggregateRecord struct {
	Ref     string `json:"ref"`
	Version int64  `json:"version"`
}

// AggregateValue is one reducer's answer for a row.
type AggregateValue struct {
	Reducer AggregateReducer `json:"reducer"`
	// Value is a decimal string, or null: no answer for the row (a reducer
	// over a censored value among Bounded included), or a coded first or
	// last, whose answer is Code.
	Value *string `json:"value"`
	// Comparator says the true number lies beyond Value (`<`, `<=`, `>=`,
	// `>`): a first, last or latest value written with one, or a min or max
	// a censored value decides.
	Comparator string `json:"comparator,omitempty"`
	// Unit is the value's unit; absent for count.
	Unit string `json:"unit,omitempty"`
	// Code is a coded first or last, or the state a timeInState value is for.
	Code string `json:"code,omitempty"`
	// At is, on a downsample, the instant the column's first, last, min or
	// max was taken at, so a chart draws each where it lies in the column;
	// of equal extremes, the earliest. A min or max served from rollup rows
	// leaves it out where finding it would pass maxSamples.
	At *time.Time `json:"at,omitempty"`
	// Rolling and Of mark a rolling value: Of is the reducer it rolls over
	// Rolling buckets. N is the buckets in the window with a value of Of,
	// fewer than Rolling where the window holds a gap.
	Rolling int               `json:"rolling,omitempty"`
	Of      *AggregateReducer `json:"of,omitempty"`
	N       *int              `json:"n,omitempty"`
}

// The stream keys a groupBy and the latest read name. A summary's statistic
// is a key too: a source may keep a night's average and maximum apart.
const (
	GroupSource    = "source"
	GroupAbout     = "about"
	GroupCode      = "code"
	GroupMetric    = "metric"
	GroupChannel   = "channel"
	GroupStatistic = "statistic"
)

// AggregateGroupBy lists the groupBy words.
var AggregateGroupBy = []string{GroupSource, GroupAbout, GroupCode, GroupMetric, GroupChannel, GroupStatistic}

// AggregateFilterProperties are the properties a metric read's filter may
// narrow: the stream keys, the code and a summary's statistic.
var AggregateFilterProperties = []string{"source", "about", "metric", "channel", "code", "statistic"}

// AggregateFilterArm names the first filter arm a metric read does not take,
// or "": it narrows by `kinds`, `implements` and the stream properties alone,
// because the index rows carry nothing else to test. The records route
// refuses the arm by name, as the ranked read refuses its own.
func AggregateFilterArm(f Filter) string {
	switch {
	case f.Search != "":
		return "search"
	case len(f.Purposes) > 0:
		return "purposes"
	case len(f.IDs) > 0:
		return "ids"
	case len(f.Labels) > 0:
		return "labels"
	case f.Deleted != nil:
		return "deleted"
	case f.Orphaned != nil:
		return "orphaned"
	case f.Ambiguous != nil:
		return "ambiguous"
	case f.Referencing != nil:
		return "referencing"
	}
	names := make([]string, 0, len(f.Properties))
	for name := range f.Properties {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !slices.Contains(AggregateFilterProperties, name) {
			return "properties." + name
		}
	}
	return ""
}

// AggregateFilterArmMessage is the refusal of an arm AggregateFilterArm names.
func AggregateFilterArmMessage(arm string) string {
	return "filter." + arm + " is not supported with aggregate: a metric read narrows by filter.kinds, filter.implements and filter.properties." +
		strings.Join(AggregateFilterProperties, ", filter.properties.")
}
