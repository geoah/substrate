package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/geoah/substrate/internal/strictjson"
	"github.com/geoah/substrate/internal/substrate"
)

// aggregateFlags are the flags that build one aggregate read. Each is applied
// only when the command line names it (pflag's Changed), so an unset flag
// sends nothing and the server applies its own default.
type aggregateFlags struct {
	output           string
	body             string
	filter           string
	kinds            []string
	statistic        string
	property         string
	anchor           string
	utcOffset        string
	propertyGroupBy  []string
	gte              string
	lt               string
	every            string
	origin           string
	calendar         string
	timezone         string
	fallbackTimezone string
	weekStart        string
	offset           string
	place            string
	records          []string
	recordsFilter    string
	aroundBefore     string
	aroundAfter      string
	maxRecords       int
	recordOffsets    []string
	recordPolicy     string
	recordOrder      []string
	recordSource     string
	recordMatch      []string
	reducers         []string
	groupBy          []string
	policy           string
	order            []string
	unit             string
	from             string
	minCoverage      float64
	maxGap           string
	fill             string
	rolling          int
	rollingReducers  []string
	rollingThrough   string
	maxBuckets       int
	maxSamples       int64
	maxGroups        int
	downsample       int
}

// aggregateCommand is the aggregate read (decision 0161): the records route
// with `aggregate`, answering one AggregateResult. The CLI builds the request
// and prints the answer; every check of forms, units and budgets is the
// server's.
func (a *app) aggregateCommand() *cobra.Command {
	var o aggregateFlags
	cmd := &cobra.Command{
		Use:   "aggregate [metric...]",
		Short: "Reduce metrics or a numeric property into time buckets",
		Long: `Reduce a metric's observed points, or a declared number of the filtered
kinds, into time buckets.

The argument is the metric record's full path,
<authority>/<package>/<kind>/<id>, such as
ada.example.com/observations/metric/health/heart_rate. Several arguments
read several metrics over one range and bucket (aggregate.metrics), each row
naming its metric. --property names a declared number of the filtered kinds
instead (--anchor the datetime property that places a record in time,
--utc-offset the int property holding its UTC offset for the recorded-local
calendar, --property-group-by the string, enum or reference properties to
group by). With none of them, the latest read across metrics is a filter on
properties.metric with --group-by metric,source --reducer last.

The range is --gte and --lt, two RFC 3339 instants. The bucket is one of
three time modes: --every (an ISO 8601 duration, counted from --origin);
--calendar (minute, hour, day, week, month or year) with --timezone,
--week-start and --offset, where a calendar with no timezone is the
recorded-local calendar and --fallback-timezone places a value written
without a UTC offset; or --records, the kinds whose records the read's
buckets are, with --records-filter, --around-before, --around-after,
--max-records and --records-utc-offsets START[,END]. --place end lands a
value with a span in the bucket it ends in.

--reducer is repeatable: count, sum, avg, twa, min, max, first, last,
stddev, increase, rate, circularMean, or one with its argument:
percentile:0.05, durationIn:95,114 (the [low, high) bounds in the values'
unit), timeInState:open,closed (bare timeInState is every state).
--statistic reads a summary at one statistic (filter.properties.statistic).
--policy and --order choose among the sources of points; --record-policy,
--record-order, --record-source and --record-match choose among the
sources of records (bucket.records.sources, or property.sources beside
--property).

A flag the command line does not name sends nothing, and the server applies
its default.

--body FILE (or -f, "-" for stdin) reads the request as JSON: either
{"filter": ..., "aggregate": ..., "downsample": ...} or the aggregate body
alone. A flag given beside it replaces that one field of the body, and a
list flag the whole list. Each metrics entry's own unit, reducers, from and
statistic have no flags and are written in a body.

The table prints one line per row: its start and end, then the group
(GROUP), the record of a records-mode window (RECORD), the sources that fed a
row whose group names none (SOURCES), the form (FORM), whether raw points or
summaries served it (FROM), the time its points cover (COVERAGE), the values
placed by --fallback-timezone (OFFSETLESS), the amounts a window cut
(PRORATED) and FLAGS (filled for a row --fill made, unavailable for a row
whose index points expired, overlaps for a records-mode window whose sums do
not add, bounded=N for values written with a comparator), each column where
a row carries it, then each value as reducer=value unit
("-" where a reducer has no answer; a coded first or last prints its code,
a timeInState value names its state, and a rolling value its window and the
n buckets in it with a value). -o json and -o yaml print the answer whole.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := outputFormat(o.output); err != nil {
				return err
			}
			ctx := cmd.Context()
			cl, err := a.client()
			if err != nil {
				return err
			}
			q, err := a.aggregateQuery(ctx, cmd.Flags(), &o, args)
			if err != nil {
				return err
			}
			res, err := cl.aggregate(ctx, q)
			if err != nil {
				return err
			}
			return printAs(a, o.output, res, func() error { return printAggregateTable(a.out, res) })
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.output, "output", "o", "", "output format: table|yaml|json")
	f.StringVarP(&o.body, "body", "f", "", `read the request from this JSON file ("-" reads stdin)`)
	f.StringVar(&o.filter, "filter", "", `records filter as JSON (substrate.Filter), e.g. '{"properties":{"source":{"eq":"..."}}}'`)
	f.StringSliceVar(&o.kinds, "kinds", nil, "only these kinds (comma-separated, each <authority>/<package>/<name>)")
	f.StringVar(&o.statistic, "statistic", "", "read summaries at this statistic alone (avg, max, sum, ...): filter.properties.statistic")
	f.StringVar(&o.property, "property", "", "read this declared number of the filtered kinds instead of a metric")
	f.StringVar(&o.anchor, "anchor", "", "with --property: the datetime property that places a record in time")
	f.StringVar(&o.utcOffset, "utc-offset", "", "with --property: the int property holding a record's UTC offset in minutes")
	f.StringSliceVar(&o.propertyGroupBy, "property-group-by", nil, "with --property: group by these string, enum or reference properties")
	f.StringVar(&o.gte, "gte", "", "range start, inclusive (RFC 3339)")
	f.StringVar(&o.lt, "lt", "", "range end, exclusive (RFC 3339)")
	f.StringVar(&o.every, "every", "", "fixed bucket width, an ISO 8601 duration (PT5M, P1D)")
	f.StringVar(&o.origin, "origin", "", "with --every: the instant buckets count from (RFC 3339)")
	f.StringVar(&o.calendar, "calendar", "", "calendar bucket: minute|hour|day|week|month|year")
	f.StringVar(&o.timezone, "timezone", "", "with --calendar: an IANA zone (absent: the recorded-local calendar)")
	f.StringVar(&o.fallbackTimezone, "fallback-timezone", "", "recorded-local calendar: the IANA zone a value written without a UTC offset is placed by")
	f.StringVar(&o.weekStart, "week-start", "", "with --calendar week: the first day of a week (monday, sunday)")
	f.StringVar(&o.offset, "offset", "", "with --calendar: shift each bucket's start, an ISO 8601 duration (PT4H)")
	f.StringVar(&o.place, "place", "", "where a value with a span lands: start (the default) or end, the bucket it ends in")
	f.StringSliceVar(&o.records, "records", nil, "records time mode: the kinds whose records are the buckets (comma-separated)")
	f.StringVar(&o.recordsFilter, "records-filter", "", "records time mode: the records' filter as JSON (substrate.Filter)")
	f.StringVar(&o.aroundBefore, "around-before", "", "records time mode: widen each window this far before its start (ISO 8601)")
	f.StringVar(&o.aroundAfter, "around-after", "", "records time mode: widen each window this far after its end (ISO 8601)")
	f.IntVar(&o.maxRecords, "max-records", 0, "records time mode: the most records the filter may match")
	f.StringSliceVar(&o.recordOffsets, "records-utc-offsets", nil, "records time mode: START[,END], the records' int UTC offset properties")
	f.StringVar(&o.recordPolicy, "record-policy", "", "sources of records: separate|priority (with --records or --property)")
	f.StringSliceVar(&o.recordOrder, "record-order", nil, "with --record-policy priority: the sources in order (repeatable)")
	f.StringVar(&o.recordSource, "record-source", "", "with --record-policy priority: the records' property holding their source")
	f.StringSliceVar(&o.recordMatch, "record-match", nil, "with --record-policy priority: properties two records must share to overlap")
	f.StringArrayVar(&o.reducers, "reducer", nil, "a reducer (repeatable): avg, percentile:0.05, durationIn:95,114, timeInState:open,closed")
	f.StringSliceVar(&o.groupBy, "group-by", nil, "group rows by source, about, code, metric, kind, channel or statistic (repeatable or comma-separated)")
	f.StringVar(&o.policy, "policy", "", "source policy: separate|priority|summaries")
	f.StringSliceVar(&o.order, "order", nil, "with --policy priority: the sources in order, by id or record path (repeatable)")
	f.StringVar(&o.unit, "unit", "", "convert values (a unit of the metric's measure) or durations (min, h) to this unit")
	f.StringVar(&o.from, "from", "", "which points to read: raw|summaries|prefer-raw")
	f.Float64Var(&o.minCoverage, "min-coverage", 0, "with --from prefer-raw: the fraction of a bucket raw points must cover")
	f.StringVar(&o.maxGap, "max-gap", "", "the longest a gauge value holds after its point, an ISO 8601 duration")
	f.StringVar(&o.fill, "fill", "", "make buckets dense: null|zero|locf")
	f.IntVar(&o.rolling, "rolling", 0, "add rolling values over this bucket and the N-1 before it")
	f.StringArrayVar(&o.rollingReducers, "rolling-reducer", nil, "with --rolling: a reducer the rolling values use (repeatable)")
	f.StringVar(&o.rollingThrough, "rolling-through", "", "with --rolling: current (the default) or previous, a window that leaves the bucket out")
	f.IntVar(&o.maxBuckets, "max-buckets", 0, "budget: the most groups times buckets the read may answer")
	f.Int64Var(&o.maxSamples, "max-samples", 0, "budget: the most raw index rows the read may take")
	f.IntVar(&o.maxGroups, "max-groups", 0, "budget: the most groups --group-by may produce")
	f.IntVar(&o.downsample, "downsample", 0, `answer N chart columns instead of buckets (sends downsample={"points":N,"method":"m4"})`)
	return cmd
}

// aggregateQuery builds the records route's parameters: the body, if any,
// then each flag the command line named laid over it.
func (a *app) aggregateQuery(ctx context.Context, fs *pflag.FlagSet, o *aggregateFlags, metrics []string) (url.Values, error) {
	var req substrate.AggregateQuery
	if fs.Changed("body") {
		raw, err := a.readAggregateBody(o.body)
		if err != nil {
			return nil, err
		}
		if req, err = parseAggregateBody(raw); err != nil {
			return nil, fmt.Errorf("parse --body %s: %w", o.body, err)
		}
	}
	if fs.Changed("filter") {
		var flt substrate.Filter
		if err := strictjson.DecodeBytes([]byte(o.filter), &flt, true); err != nil {
			return nil, fmt.Errorf("parse --filter as JSON: %w", err)
		}
		req.Filter = flt
	}
	if fs.Changed("kinds") {
		refs, err := a.kindRefs(ctx, o.kinds)
		if err != nil {
			return nil, err
		}
		req.Filter.Kinds = refs
	}
	if fs.Changed("statistic") {
		if req.Filter.Properties == nil {
			req.Filter.Properties = map[string]substrate.Cond{}
		}
		req.Filter.Properties["statistic"] = substrate.Cond{Eq: o.statistic}
	}
	if req.Aggregate == nil {
		req.Aggregate = &substrate.Aggregate{}
	}
	if err := o.apply(fs, req.Aggregate, metrics); err != nil {
		return nil, err
	}
	if err := a.applyRecords(ctx, fs, o, req.Aggregate); err != nil {
		return nil, err
	}
	if fs.Changed("downsample") {
		req.Downsample = &substrate.Downsample{Points: o.downsample, Method: "m4"}
	}
	q := url.Values{}
	filter, err := json.Marshal(req.Filter)
	if err != nil {
		return nil, fmt.Errorf("encode filter: %w", err)
	}
	if string(filter) != "{}" {
		q.Set("filter", string(filter))
	}
	agg, err := json.Marshal(req.Aggregate)
	if err != nil {
		return nil, fmt.Errorf("encode aggregate: %w", err)
	}
	q.Set("aggregate", string(agg))
	if req.Downsample != nil {
		ds, err := json.Marshal(req.Downsample)
		if err != nil {
			return nil, fmt.Errorf("encode downsample: %w", err)
		}
		q.Set("downsample", string(ds))
	}
	return q, nil
}

// kindRefs resolves each kind a flag names to its full reference.
func (a *app) kindRefs(ctx context.Context, kinds []string) ([]string, error) {
	refs := make([]string, 0, len(kinds))
	for _, k := range kinds {
		col, err := a.resolveCollection(ctx, k)
		if err != nil {
			return nil, err
		}
		refs = append(refs, col.pkg()+"/"+col.Name)
	}
	return refs, nil
}

// The flags of each time mode: a read takes one mode.
var (
	everyFlags    = []string{"every", "origin"}
	calendarFlags = []string{"calendar", "timezone", "fallback-timezone", "week-start", "offset"}
	recordsFlags  = []string{"records", "records-filter", "around-before", "around-after", "max-records", "records-utc-offsets"}
	// recordSourceFlags choose among the sources of records: the records
	// mode's windows, or a property read's records.
	recordSourceFlags = []string{"record-policy", "record-order", "record-source", "record-match"}
)

func anySet(set func(string) bool, names []string) bool {
	for _, n := range names {
		if set(n) {
			return true
		}
	}
	return false
}

// apply lays the named flags over agg, one field each. A list flag replaces
// the whole list.
func (o *aggregateFlags) apply(fs *pflag.FlagSet, agg *substrate.Aggregate, metrics []string) error {
	set := fs.Changed
	for _, m := range metrics {
		if strings.Count(m, "/") < 3 {
			return fmt.Errorf("a metric is named by its record path, <authority>/<package>/<kind>/<id>, got %q", m)
		}
	}
	switch {
	case len(metrics) > 0 && set("property"):
		return errors.New("name metrics or --property, not both")
	case len(metrics) == 1:
		agg.Metric, agg.Metrics, agg.Property = &substrate.AggregateMetric{Ref: metrics[0]}, nil, nil
	case len(metrics) > 1:
		agg.Metric, agg.Metrics, agg.Property = nil, make([]substrate.AggregateMetricRead, len(metrics)), nil
		for i, m := range metrics {
			agg.Metrics[i] = substrate.AggregateMetricRead{Ref: m}
		}
	case set("property"):
		agg.Metric, agg.Metrics, agg.Property = nil, nil, &substrate.AggregateProperty{Name: o.property}
	}
	for _, name := range []string{"anchor", "utc-offset", "property-group-by"} {
		if set(name) && agg.Property == nil {
			return fmt.Errorf("--%s is an option of the property read; give --property beside it", name)
		}
	}
	if p := agg.Property; p != nil {
		setString(set, "anchor", &p.Anchor, o.anchor)
		setString(set, "utc-offset", &p.UTCOffset, o.utcOffset)
		if set("property-group-by") {
			p.GroupBy = o.propertyGroupBy
		}
	}

	if set("gte") || set("lt") {
		if agg.Range == nil {
			agg.Range = &substrate.AggregateRange{}
		}
		setString(set, "gte", &agg.Range.Gte, o.gte)
		setString(set, "lt", &agg.Range.Lt, o.lt)
	}
	if r := agg.Range; r != nil {
		if r.Gte == "" || r.Lt == "" {
			if set("gte") || set("lt") {
				return errors.New("the range needs both --gte and --lt")
			}
			return errors.New("the range needs both gte and lt")
		}
		for _, bound := range []struct{ flag, field, v string }{{"gte", "range.gte", r.Gte}, {"lt", "range.lt", r.Lt}} {
			if _, err := time.Parse(time.RFC3339, bound.v); err != nil {
				name := bound.field
				if set(bound.flag) {
					name = "--" + bound.flag
				}
				return fmt.Errorf("%s %q is not an RFC 3339 instant, such as 2026-04-06T00:00:00+03:00", name, bound.v)
			}
		}
	}

	modes := 0
	for _, flags := range [][]string{everyFlags, calendarFlags, recordsFlags} {
		if anySet(set, flags) {
			modes++
		}
	}
	if modes > 1 {
		return errors.New("--every and --origin are one time mode; --calendar, --timezone, --fallback-timezone, --week-start and --offset another; --records and its flags a third; give one")
	}
	if modes > 0 || set("place") {
		if agg.Bucket == nil {
			agg.Bucket = &substrate.AggregateBucket{}
		}
		b := agg.Bucket
		setString(set, "every", &b.Every, o.every)
		setString(set, "origin", &b.Origin, o.origin)
		setString(set, "calendar", &b.Calendar, o.calendar)
		setString(set, "timezone", &b.Timezone, o.timezone)
		setString(set, "fallback-timezone", &b.FallbackTimezone, o.fallbackTimezone)
		setString(set, "week-start", &b.WeekStart, o.weekStart)
		setString(set, "offset", &b.Offset, o.offset)
		setString(set, "place", &b.Place, o.place)
	}

	if set("group-by") {
		agg.GroupBy = o.groupBy
	}
	if set("reducer") {
		rs, err := parseReducers("--reducer", o.reducers)
		if err != nil {
			return err
		}
		agg.Reducers = rs
	}
	if set("policy") || set("order") {
		if agg.Sources == nil {
			agg.Sources = &substrate.AggregateSources{}
		}
		setString(set, "policy", &agg.Sources.Policy, o.policy)
		if set("order") {
			agg.Sources.Order = o.order
		}
	}
	setString(set, "unit", &agg.Unit, o.unit)
	setString(set, "from", &agg.From, o.from)
	if set("min-coverage") {
		v := o.minCoverage
		agg.MinCoverage = &v
	}
	setString(set, "max-gap", &agg.MaxGap, o.maxGap)
	setString(set, "fill", &agg.Fill, o.fill)

	if set("rolling") {
		if agg.Rolling == nil {
			agg.Rolling = &substrate.AggregateRolling{}
		}
		agg.Rolling.Buckets = o.rolling
	}
	for _, name := range []string{"rolling-reducer", "rolling-through"} {
		if set(name) && agg.Rolling == nil {
			return fmt.Errorf("--%s needs --rolling N, the buckets it rolls over", name)
		}
	}
	if set("rolling-reducer") {
		rs, err := parseReducers("--rolling-reducer", o.rollingReducers)
		if err != nil {
			return err
		}
		agg.Rolling.Reducers = rs
	}
	if set("rolling-through") {
		agg.Rolling.Through = o.rollingThrough
	}

	if set("max-buckets") || set("max-samples") || set("max-groups") {
		if agg.Budget == nil {
			agg.Budget = &substrate.AggregateBudget{}
		}
		if set("max-buckets") {
			agg.Budget.MaxBuckets = o.maxBuckets
		}
		if set("max-samples") {
			agg.Budget.MaxSamples = o.maxSamples
		}
		if set("max-groups") {
			agg.Budget.MaxGroups = o.maxGroups
		}
	}
	return nil
}

// applyRecords lays the records time mode's flags over agg's bucket, and the
// record source flags over the property read's sources or the records mode's.
func (a *app) applyRecords(ctx context.Context, fs *pflag.FlagSet, o *aggregateFlags, agg *substrate.Aggregate) error {
	set := fs.Changed
	if anySet(set, recordsFlags) {
		b := agg.Bucket
		if b.Records == nil {
			b.Records = &substrate.AggregateRecords{}
		}
		r := b.Records
		if set("records-filter") {
			var flt substrate.Filter
			if err := strictjson.DecodeBytes([]byte(o.recordsFilter), &flt, true); err != nil {
				return fmt.Errorf("parse --records-filter as JSON: %w", err)
			}
			r.Filter = flt
		}
		if set("records") {
			refs, err := a.kindRefs(ctx, o.records)
			if err != nil {
				return err
			}
			r.Filter.Kinds = refs
		}
		if set("around-before") || set("around-after") {
			if r.Around == nil {
				r.Around = &substrate.AggregateAround{}
			}
			setString(set, "around-before", &r.Around.Before, o.aroundBefore)
			setString(set, "around-after", &r.Around.After, o.aroundAfter)
		}
		if set("max-records") {
			r.MaxRecords = o.maxRecords
		}
		if set("records-utc-offsets") {
			if n := len(o.recordOffsets); n < 1 || n > 2 {
				return errors.New("--records-utc-offsets names the records' start offset property, and their end's after a comma: utcOffsetStart,utcOffsetEnd")
			}
			r.UTCOffsets = &substrate.AggregateRecordOffsets{Start: o.recordOffsets[0]}
			if len(o.recordOffsets) == 2 {
				r.UTCOffsets.End = o.recordOffsets[1]
			}
		}
	}
	if !anySet(set, recordSourceFlags) {
		return nil
	}
	var src **substrate.AggregateRecordSources
	switch {
	case agg.Property != nil:
		src = &agg.Property.Sources
	case agg.Bucket != nil && agg.Bucket.Records != nil:
		src = &agg.Bucket.Records.Sources
	default:
		return errors.New("--record-policy, --record-order, --record-source and --record-match choose among records: give --property or the records time mode (--records) beside them")
	}
	if *src == nil {
		*src = &substrate.AggregateRecordSources{}
	}
	rs := *src
	setString(set, "record-policy", &rs.Policy, o.recordPolicy)
	if set("record-order") {
		rs.Order = o.recordOrder
	}
	setString(set, "record-source", &rs.Property, o.recordSource)
	if set("record-match") {
		rs.Match = o.recordMatch
	}
	return nil
}

func setString(set func(string) bool, name string, field *string, v string) {
	if set(name) {
		*field = v
	}
}

func parseReducers(flag string, specs []string) ([]substrate.AggregateReducer, error) {
	out := make([]substrate.AggregateReducer, 0, len(specs))
	for _, s := range specs {
		r, err := parseReducer(s)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", flag, s, err)
		}
		out = append(out, r)
	}
	return out, nil
}

// parseReducer reads the flag spelling of one reducer: a bare name, or a
// name and its argument after a colon. Which names exist is the server's to
// judge; only an argument the wire has no place for is refused here.
func parseReducer(s string) (substrate.AggregateReducer, error) {
	name, arg, hasArg := strings.Cut(strings.TrimSpace(s), ":")
	r := substrate.AggregateReducer{Name: name}
	if name == "" {
		return r, errors.New("a reducer has a name, such as avg or percentile:0.05")
	}
	switch name {
	case substrate.ReducerPercentile:
		if !hasArg {
			return r, errors.New("percentile takes its q, from 0 to 1, as percentile:0.05")
		}
		q, err := strconv.ParseFloat(strings.TrimSpace(arg), 64)
		if err != nil {
			return r, errors.New("percentile takes a number from 0 to 1, as percentile:0.05")
		}
		r.Percentile = &q
	case substrate.ReducerDurationIn:
		parts := strings.Split(arg, ",")
		if !hasArg || len(parts) != 2 {
			return r, errors.New("durationIn takes two bounds, low and high, as durationIn:95,114")
		}
		for _, p := range parts {
			v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
			if err != nil {
				return r, fmt.Errorf("durationIn bound %q is not a number", p)
			}
			r.Between = append(r.Between, v)
		}
	case substrate.ReducerTimeInState:
		if hasArg {
			r.States = []string{}
			for _, p := range strings.Split(arg, ",") {
				if p = strings.TrimSpace(p); p != "" {
					r.States = append(r.States, p)
				}
			}
		}
	default:
		if hasArg {
			return r, fmt.Errorf("%s takes no argument; percentile, durationIn and timeInState take one", name)
		}
	}
	return r, nil
}

// readAggregateBody reads --body, "-" being stdin.
func (a *app) readAggregateBody(name string) ([]byte, error) {
	if name == "-" {
		b, err := io.ReadAll(a.in)
		if err != nil {
			return nil, fmt.Errorf("read --body from stdin: %w", err)
		}
		return b, nil
	}
	b, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read --body: %w", err)
	}
	return b, nil
}

// parseAggregateBody reads a request document: an AggregateQuery when it
// carries any of `filter`, `aggregate` and `downsample`, else the aggregate
// body alone. The decode is the server's, so a misspelled key is refused
// here instead of dropped.
func parseAggregateBody(raw []byte) (substrate.AggregateQuery, error) {
	var req substrate.AggregateQuery
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return req, fmt.Errorf("the body is a JSON object: %w", err)
	}
	_, hasFilter := keys["filter"]
	_, hasAggregate := keys["aggregate"]
	_, hasDownsample := keys["downsample"]
	// The server's own strict decode, keeping every number's digits (a
	// filter's operands are `any`).
	if hasFilter || hasAggregate || hasDownsample {
		return req, strictjson.DecodeBytes(raw, &req, true)
	}
	var agg substrate.Aggregate
	if err := strictjson.DecodeBytes(raw, &agg, true); err != nil {
		return req, err
	}
	req.Aggregate = &agg
	return req, nil
}

func printAggregateTable(w io.Writer, res *substrate.AggregateResult) error {
	var grouped, recorded, sourced, formed, served, covered, offsetless, prorated, flagged bool
	for _, row := range res.Rows {
		grouped = grouped || row.Group != nil
		recorded = recorded || row.Record != nil
		// The separate policy splits rows by source without a group: the
		// sources are what tell two rows of one bucket apart.
		sourced = sourced || (len(row.Sources) > 0 && (row.Group == nil || row.Group.Source == ""))
		formed = formed || row.Form != ""
		served = served || row.From != ""
		covered = covered || row.Coverage != ""
		offsetless = offsetless || row.Offsetless > 0
		prorated = prorated || row.Prorated > 0
		flagged = flagged || rowFlags(row) != ""
	}
	tw := newTable(w)
	header := []string{"START", "END"}
	if grouped {
		header = append(header, "GROUP")
	}
	if recorded {
		header = append(header, "RECORD")
	}
	if sourced {
		header = append(header, "SOURCES")
	}
	if formed {
		header = append(header, "FORM")
	}
	if served {
		header = append(header, "FROM")
	}
	if covered {
		header = append(header, "COVERAGE")
	}
	if offsetless {
		header = append(header, "OFFSETLESS")
	}
	if prorated {
		header = append(header, "PRORATED")
	}
	if flagged {
		header = append(header, "FLAGS")
	}
	header = append(header, "VALUES")
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, row := range res.Rows {
		cells := []string{row.Start.Format(time.RFC3339), row.End.Format(time.RFC3339)}
		if grouped {
			cells = append(cells, dash(groupLabel(row.Group)))
		}
		if recorded {
			ref := ""
			if row.Record != nil {
				ref = row.Record.Ref
			}
			cells = append(cells, dash(ref))
		}
		if sourced {
			cells = append(cells, dash(strings.Join(row.Sources, ",")))
		}
		for _, col := range []struct {
			shown bool
			value string
		}{{formed, row.Form}, {served, row.From}, {covered, row.Coverage}} {
			if col.shown {
				cells = append(cells, dash(col.value))
			}
		}
		if offsetless {
			cells = append(cells, strconv.FormatInt(row.Offsetless, 10))
		}
		if prorated {
			cells = append(cells, strconv.FormatInt(row.Prorated, 10))
		}
		if flagged {
			cells = append(cells, dash(rowFlags(row)))
		}
		for _, v := range row.Values {
			cells = append(cells, valueLabel(v))
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

// rowFlags names what makes a row other than plain data: filled (a row fill
// made), unavailable (expired index points its reducers need), overlaps (a
// records-mode window that may overlap another, whose sums do not add) and
// bounded=N (values written with a comparator).
func rowFlags(row substrate.AggregateRow) string {
	var flags []string
	if row.Synthetic {
		flags = append(flags, "filled")
	}
	if row.Unavailable {
		flags = append(flags, "unavailable")
	}
	if row.NonAdditive {
		flags = append(flags, "overlaps")
	}
	if row.Bounded > 0 {
		flags = append(flags, "bounded="+strconv.FormatInt(row.Bounded, 10))
	}
	return strings.Join(flags, ",")
}

// groupLabel is a row's group as key=value pairs, in the order the wire
// declares the keys, then a property read's group properties by name.
func groupLabel(g *substrate.AggregateGroup) string {
	if g == nil {
		return ""
	}
	var parts []string
	for _, kv := range [][2]string{
		{substrate.GroupSource, g.Source},
		{substrate.GroupAbout, g.About},
		{substrate.GroupCode, g.Code},
		{substrate.GroupMetric, g.Metric},
		{substrate.GroupKind, g.Kind},
		{substrate.GroupChannel, g.Channel},
		{substrate.GroupStatistic, g.Statistic},
		{"currency", g.Currency},
	} {
		if kv[1] != "" {
			parts = append(parts, kv[0]+"="+kv[1])
		}
	}
	names := make([]string, 0, len(g.Properties))
	for name := range g.Properties {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		parts = append(parts, name+"="+g.Properties[name])
	}
	return strings.Join(parts, ",")
}

// valueLabel is one value as reducer=value unit. A coded first or last
// answers its code as the value; any other value's code is the state it is
// for, written on the reducer that owns it: the value's own, or for a rolling
// value the reducer it rolls over. A rolling value names its window and n,
// the buckets in it with a value.
func valueLabel(v substrate.AggregateValue) string {
	label := reducerLabel(v.Reducer)
	coded := v.Code != "" && v.Rolling == 0 && v.Value == nil &&
		(v.Reducer.Name == substrate.ReducerFirst || v.Reducer.Name == substrate.ReducerLast)
	if v.Code != "" && !coded && v.Rolling == 0 {
		label = v.Reducer.Name + ":" + v.Code
	}
	if v.Rolling > 0 {
		window := strconv.Itoa(v.Rolling)
		if v.N != nil {
			window += ",n=" + strconv.Itoa(*v.N)
		}
		label = "rolling(" + window + "," + label
		if v.Of != nil {
			of := reducerLabel(*v.Of)
			if v.Code != "" {
				of = v.Of.Name + ":" + v.Code
			}
			label += " of " + of
		}
		label += ")"
	}
	switch {
	case v.Value != nil && v.Unit != "":
		return label + "=" + *v.Value + " " + v.Unit
	case v.Value != nil:
		return label + "=" + *v.Value
	case coded:
		return label + "=" + v.Code
	}
	return label + "=-"
}

// reducerLabel spells a reducer the way --reducer takes it.
func reducerLabel(r substrate.AggregateReducer) string {
	switch {
	case r.Percentile != nil:
		return r.Name + ":" + strconv.FormatFloat(*r.Percentile, 'f', -1, 64)
	case len(r.Between) > 0:
		bounds := make([]string, len(r.Between))
		for i, b := range r.Between {
			bounds[i] = strconv.FormatFloat(b, 'f', -1, 64)
		}
		return r.Name + ":" + strings.Join(bounds, ",")
	case len(r.States) > 0:
		return r.Name + ":" + strings.Join(r.States, ",")
	}
	return r.Name
}
