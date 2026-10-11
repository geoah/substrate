package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/geoah/substrate/internal/substrate"
)

// aggregateFlags are the flags that build one aggregate read. Each is applied
// only when the command line names it (pflag's Changed), so an unset flag
// sends nothing and the server applies its own default.
type aggregateFlags struct {
	output          string
	body            string
	filter          string
	kinds           []string
	property        string
	anchor          string
	gte             string
	lt              string
	every           string
	origin          string
	calendar        string
	timezone        string
	weekStart       string
	offset          string
	reducers        []string
	groupBy         []string
	policy          string
	order           []string
	unit            string
	from            string
	minCoverage     float64
	maxGap          string
	fill            string
	rolling         int
	rollingReducers []string
	maxBuckets      int
	maxSamples      int64
	maxGroups       int
	downsample      int
}

// aggregateCommand is the aggregate read (decision 0161): the records route
// with `aggregate`, answering one AggregateResult. The CLI builds the request
// and prints the answer; every check of forms, units and budgets is the
// server's.
func (a *app) aggregateCommand() *cobra.Command {
	var o aggregateFlags
	cmd := &cobra.Command{
		Use:   "aggregate [metric]",
		Short: "Reduce a metric or a numeric property into time buckets",
		Long: `Reduce a metric's observed points, or a declared number of the filtered
kinds, into time buckets.

The argument is the metric record's full path,
<authority>/<package>/<kind>/<id>, such as
ada.example.com/observations/metric/health/heart_rate. --property names a
declared number of the filtered kinds instead (--anchor the datetime property
that places a record in time). Without either, --filter or --body must say
what to read: the latest read across metrics is a filter on
properties.metric with --group-by metric,source --reducer last.

The range is --gte and --lt, two RFC 3339 instants. The bucket is --every
(an ISO 8601 duration, counted from --origin) or --calendar (minute, hour,
day, week, month or year) with --timezone, --week-start and --offset; a
calendar with no timezone is the recorded-local calendar.

--reducer is repeatable: count, sum, avg, twa, min, max, first, last,
stddev, increase, rate, circularMean, or one with its argument:
percentile:0.05, durationIn:95,114 (the [low, high) bounds in the values'
unit), timeInState:open,closed (bare timeInState is every state).

A flag the command line does not name sends nothing, and the server applies
its default.

--body FILE (or -f, "-" for stdin) reads the request as JSON: either
{"filter": ..., "aggregate": ..., "downsample": ...} or the aggregate body
alone. A flag given beside it replaces that one field of the body, and a
list flag the whole list. The records time mode (bucket.records) has no
flags and is written in a body.

The table prints one line per row: its start and end, the group, the
form, whether raw points or summaries served it, the time its points cover,
then each value as reducer=value unit ("-" where a reducer has no answer; a
coded first or last prints its code). -o json and -o yaml print the answer
whole.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := a.client()
			if err != nil {
				return err
			}
			metric := ""
			if len(args) == 1 {
				metric = args[0]
			}
			q, err := a.aggregateQuery(ctx, cmd.Flags(), &o, metric)
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
	f.StringVar(&o.property, "property", "", "read this declared number of the filtered kinds instead of a metric")
	f.StringVar(&o.anchor, "anchor", "", "with --property: the datetime property that places a record in time")
	f.StringVar(&o.gte, "gte", "", "range start, inclusive (RFC 3339)")
	f.StringVar(&o.lt, "lt", "", "range end, exclusive (RFC 3339)")
	f.StringVar(&o.every, "every", "", "fixed bucket width, an ISO 8601 duration (PT5M, P1D)")
	f.StringVar(&o.origin, "origin", "", "with --every: the instant buckets count from (RFC 3339)")
	f.StringVar(&o.calendar, "calendar", "", "calendar bucket: minute|hour|day|week|month|year")
	f.StringVar(&o.timezone, "timezone", "", "with --calendar: an IANA zone (absent: the recorded-local calendar)")
	f.StringVar(&o.weekStart, "week-start", "", "with --calendar week: the first day of a week (monday, sunday)")
	f.StringVar(&o.offset, "offset", "", "with --calendar: shift each bucket's start, an ISO 8601 duration (PT4H)")
	f.StringArrayVar(&o.reducers, "reducer", nil, "a reducer (repeatable): avg, percentile:0.05, durationIn:95,114, timeInState:open,closed")
	f.StringSliceVar(&o.groupBy, "group-by", nil, "group rows by source, about, code, metric, kind or channel (repeatable or comma-separated)")
	f.StringVar(&o.policy, "policy", "", "source policy: separate|priority|summaries")
	f.StringSliceVar(&o.order, "order", nil, "with --policy priority: the sources in order, by id or record path (repeatable)")
	f.StringVar(&o.unit, "unit", "", "convert values (a unit of the metric's measure) or durations (min, h) to this unit")
	f.StringVar(&o.from, "from", "", "which points to read: raw|summaries|prefer-raw")
	f.Float64Var(&o.minCoverage, "min-coverage", 0, "with --from prefer-raw: the fraction of a bucket raw points must cover")
	f.StringVar(&o.maxGap, "max-gap", "", "the longest a gauge value holds after its point, an ISO 8601 duration")
	f.StringVar(&o.fill, "fill", "", "make buckets dense: null|zero|locf")
	f.IntVar(&o.rolling, "rolling", 0, "add rolling values over this bucket and the N-1 before it")
	f.StringArrayVar(&o.rollingReducers, "rolling-reducer", nil, "with --rolling: a reducer the rolling values use (repeatable)")
	f.IntVar(&o.maxBuckets, "max-buckets", 0, "budget: the most groups times buckets the read may answer")
	f.Int64Var(&o.maxSamples, "max-samples", 0, "budget: the most raw index rows the read may take")
	f.IntVar(&o.maxGroups, "max-groups", 0, "budget: the most groups --group-by may produce")
	f.IntVar(&o.downsample, "downsample", 0, `answer N chart columns instead of buckets (sends downsample={"points":N,"method":"m4"})`)
	return cmd
}

// aggregateQuery builds the records route's parameters: the body, if any,
// then each flag the command line named laid over it.
func (a *app) aggregateQuery(ctx context.Context, fs *pflag.FlagSet, o *aggregateFlags, metric string) (url.Values, error) {
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
		if err := decodeStrict([]byte(o.filter), &flt); err != nil {
			return nil, fmt.Errorf("parse --filter as JSON: %w", err)
		}
		req.Filter = flt
	}
	if fs.Changed("kinds") {
		refs := make([]string, 0, len(o.kinds))
		for _, k := range o.kinds {
			col, err := a.resolveCollection(ctx, k)
			if err != nil {
				return nil, err
			}
			refs = append(refs, col.pkg()+"/"+col.Name)
		}
		req.Filter.Kinds = refs
	}
	if req.Aggregate == nil {
		req.Aggregate = &substrate.Aggregate{}
	}
	if err := o.apply(fs, req.Aggregate, metric); err != nil {
		return nil, err
	}
	filter, err := json.Marshal(req.Filter)
	if err != nil {
		return nil, fmt.Errorf("encode filter: %w", err)
	}
	if req.Aggregate.Metric == nil && req.Aggregate.Property == nil && string(filter) == "{}" {
		return nil, errors.New("name what to read: a metric record path as the argument, --property, --filter or --body")
	}
	if fs.Changed("downsample") {
		req.Downsample = &substrate.Downsample{Points: o.downsample, Method: "m4"}
	}
	q := url.Values{}
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

// apply lays the named flags over agg, one field each. A list flag replaces
// the whole list.
func (o *aggregateFlags) apply(fs *pflag.FlagSet, agg *substrate.Aggregate, metric string) error {
	set := fs.Changed
	switch {
	case metric != "" && set("property"):
		return errors.New("name a metric or --property, not both")
	case metric != "":
		if strings.Count(metric, "/") < 3 {
			return fmt.Errorf("a metric is named by its record path, <authority>/<package>/<kind>/<id>, got %q", metric)
		}
		agg.Metric, agg.Property = &substrate.AggregateMetric{Ref: metric}, nil
	case set("property"):
		agg.Metric, agg.Property = nil, &substrate.AggregateProperty{Name: o.property}
	}
	if set("anchor") {
		if agg.Property == nil {
			return errors.New("--anchor names the time property of --property; give --property beside it")
		}
		agg.Property.Anchor = o.anchor
	}

	if set("gte") || set("lt") {
		if agg.Range == nil {
			agg.Range = &substrate.AggregateRange{}
		}
		if set("gte") {
			agg.Range.Gte = o.gte
		}
		if set("lt") {
			agg.Range.Lt = o.lt
		}
	}
	if r := agg.Range; r != nil {
		if r.Gte == "" || r.Lt == "" {
			return errors.New("the range needs both --gte and --lt")
		}
		for _, bound := range [][2]string{{"--gte", r.Gte}, {"--lt", r.Lt}} {
			if _, err := time.Parse(time.RFC3339, bound[1]); err != nil {
				return fmt.Errorf("%s %q is not an RFC 3339 instant, such as 2026-04-06T00:00:00+03:00", bound[0], bound[1])
			}
		}
	}

	fixed := set("every") || set("origin")
	calendar := set("calendar") || set("timezone") || set("week-start") || set("offset")
	if fixed && calendar {
		return errors.New("--every and --origin are one time mode, --calendar, --timezone, --week-start and --offset another; give one")
	}
	if fixed || calendar {
		if agg.Bucket == nil {
			agg.Bucket = &substrate.AggregateBucket{}
		}
		b := agg.Bucket
		setString(set, "every", &b.Every, o.every)
		setString(set, "origin", &b.Origin, o.origin)
		setString(set, "calendar", &b.Calendar, o.calendar)
		setString(set, "timezone", &b.Timezone, o.timezone)
		setString(set, "week-start", &b.WeekStart, o.weekStart)
		setString(set, "offset", &b.Offset, o.offset)
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
	if set("rolling-reducer") {
		if agg.Rolling == nil {
			return errors.New("--rolling-reducer needs --rolling N, the buckets it rolls over")
		}
		rs, err := parseReducers("--rolling-reducer", o.rollingReducers)
		if err != nil {
			return err
		}
		agg.Rolling.Reducers = rs
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
// body alone. The decode is strict, as the server's is, so a misspelled key
// is refused here instead of dropped.
func parseAggregateBody(raw []byte) (substrate.AggregateQuery, error) {
	var req substrate.AggregateQuery
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return req, fmt.Errorf("the body is a JSON object: %w", err)
	}
	_, hasFilter := keys["filter"]
	_, hasAggregate := keys["aggregate"]
	_, hasDownsample := keys["downsample"]
	if hasFilter || hasAggregate || hasDownsample {
		return req, decodeStrict(raw, &req)
	}
	var agg substrate.Aggregate
	if err := decodeStrict(raw, &agg); err != nil {
		return req, err
	}
	req.Aggregate = &agg
	return req, nil
}

// decodeStrict decodes one JSON value, refusing an unknown key and keeping
// every number's digits (a filter's operands are `any`).
func decodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after the JSON value")
	}
	return nil
}

func printAggregateTable(w io.Writer, res *substrate.AggregateResult) error {
	grouped, recorded := false, false
	for _, row := range res.Rows {
		grouped = grouped || row.Group != nil
		recorded = recorded || row.Record != nil
	}
	tw := newTable(w)
	header := []string{"START", "END"}
	if grouped {
		header = append(header, "GROUP")
	}
	if recorded {
		header = append(header, "RECORD")
	}
	header = append(header, "FORM", "FROM", "COVERAGE", "VALUES")
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
		cells = append(cells, dash(row.Form), dash(row.From), dash(row.Coverage))
		for _, v := range row.Values {
			cells = append(cells, valueLabel(v))
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

// groupLabel is a row's group as key=value pairs, in the order the wire
// declares the keys.
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
		{"currency", g.Currency},
	} {
		if kv[1] != "" {
			parts = append(parts, kv[0]+"="+kv[1])
		}
	}
	return strings.Join(parts, ",")
}

// valueLabel is one value as reducer=value unit. A timeInState value names
// the state it is for, and a coded first or last prints its code.
func valueLabel(v substrate.AggregateValue) string {
	label := reducerLabel(v.Reducer)
	if v.Code != "" && v.Value != nil {
		label = v.Reducer.Name + ":" + v.Code
	}
	if v.Rolling > 0 {
		if v.Of != nil {
			label += " of " + reducerLabel(*v.Of)
		}
		label = fmt.Sprintf("rolling(%d,%s)", v.Rolling, label)
	}
	switch {
	case v.Value != nil && v.Unit != "":
		return label + "=" + *v.Value + " " + v.Unit
	case v.Value != nil:
		return label + "=" + *v.Value
	case v.Code != "":
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
