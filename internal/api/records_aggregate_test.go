package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// aggregatePath is the records route's aggregate mode with the parameters
// given.
func aggregatePath(t *testing.T, a any, extra ...string) string {
	t.Helper()
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	path := recordsPath + "?aggregate=" + url.QueryEscape(string(raw))
	for _, p := range extra {
		path += "&" + p
	}
	return path
}

// The aggregate mode carries its body, the downsample and the filter to the
// dataset as written, the reducers in both spellings included, and answers
// the AggregateResult.
func TestRecordsAggregateCarriesTheGrammar(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	body := `{"metric":{"ref":"ada.example.com/observations/metric/health/heart_rate"},` +
		`"range":{"gte":"2026-04-06T00:00:00+03:00","lt":"2026-10-05T00:00:00+03:00"},` +
		`"bucket":{"calendar":"week","weekStart":"monday","timezone":"Europe/Athens"},` +
		`"reducers":[{"durationIn":[95,114]},"avg",{"percentile":0.05},{"timeInState":[]}],` +
		`"sources":{"policy":"priority","order":["garmin-forerunner-265","apple-watch-s10"]},` +
		`"durationUnit":"min","maxGap":"PT5M","budget":{"maxSamples":1500000}}`
	filter, _ := json.Marshal(substrate.Filter{Kinds: []string{personKind}})
	path := recordsPath + "?aggregate=" + url.QueryEscape(body) + "&filter=" + url.QueryEscape(string(filter))
	rec := env.do(t, http.MethodGet, path, tok, nil)
	wantStatus(t, rec, http.StatusOK)

	q := ds.lastAggregate
	if q == nil || q.Aggregate == nil {
		t.Fatal("the dataset saw no aggregate read")
	}
	a := q.Aggregate
	if a.Metric.Ref != "ada.example.com/observations/metric/health/heart_rate" || a.Range.Gte != "2026-04-06T00:00:00+03:00" ||
		a.Bucket.Calendar != "week" || a.Bucket.WeekStart != "monday" || a.Bucket.Timezone != "Europe/Athens" ||
		a.DurationUnit != "min" || a.MaxGap != "PT5M" || a.Budget.MaxSamples != 1500000 || a.Sources.Policy != "priority" ||
		len(a.Sources.Order) != 2 {
		t.Fatalf("aggregate = %+v", a)
	}
	if len(a.Reducers) != 4 || a.Reducers[0].Name != "durationIn" || a.Reducers[0].Between[1] != 114 ||
		a.Reducers[1].Name != "avg" || *a.Reducers[2].Percentile != 0.05 || a.Reducers[3].States == nil {
		t.Fatalf("reducers = %+v", a.Reducers)
	}
	if len(q.Filter.Kinds) != 1 || q.Filter.Kinds[0] != personKind || q.Downsample != nil {
		t.Fatalf("filter %+v downsample %+v", q.Filter, q.Downsample)
	}
	res := decodeJSON[substrate.AggregateResult](t, rec)
	if len(res.Rows) != 1 || res.Rows[0].Values[0].Reducer.Name != "avg" || *res.Rows[0].Values[0].Value != "62" {
		t.Fatalf("result = %+v", res)
	}

	rec = env.do(t, http.MethodGet, aggregatePath(t, substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: "ada.example.com/observations/metric/health/heart_rate"},
		Range:  &substrate.AggregateRange{Gte: "2026-09-01T00:00:00Z", Lt: "2026-10-01T00:00:00Z"},
	}, "downsample="+url.QueryEscape(`{"points":800}`)), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if d := ds.lastAggregate.Downsample; d == nil || d.Points != 800 {
		t.Fatalf("downsample = %+v", d)
	}

	rec = env.do(t, http.MethodGet, recordsPath+"?aggregate="+url.QueryEscape(`{"latest":true,"groupBy":["metric"]}`), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if a := ds.lastAggregate.Aggregate; !a.Latest || len(a.GroupBy) != 1 {
		t.Fatalf("the latest read = %+v", a)
	}
}

// A key the aggregate read took before v1 is refused by name with what
// replaced it, at the top of the body, in the bucket and in a downsample,
// and the dataset never sees the read. A read over an index being
// re-derived is a 503 the client retries.
func TestAggregateRefusesARetiredKeyAndRetriesAnUnavailableRead(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	for _, x := range []struct{ query, want string }{
		{"aggregate=" + url.QueryEscape(`{"metric":{"ref":"x/y/z/w"},"minCoverage":0.5}`), "minCoverage was removed"},
		{"aggregate=" + url.QueryEscape(`{"bucket":{"calendar":"day","offset":"PT4H"}}`), "bucket.offset was removed"},
		{"aggregate=" + url.QueryEscape(`{"bucket":{"records":{"filter":{},"maxRecords":5}}}`), "bucket.records.maxRecords was removed"},
		{"aggregate=" + url.QueryEscape(`{"metric":{"ref":"x/y/z/w"}}`) + "&downsample=" + url.QueryEscape(`{"points":8,"method":"m4"}`), "method was removed"},
	} {
		ds.lastAggregate = nil
		rec := env.do(t, http.MethodGet, recordsPath+"?"+x.query, tok, nil)
		wantStatus(t, rec, http.StatusBadRequest)
		wantMessage(t, rec, x.want)
		if ds.lastAggregate != nil {
			t.Errorf("%s: the dataset saw the refused read", x.query)
		}
	}
	// The re-derivation's 503 names its reason; any other 503 names none.
	for _, x := range []struct {
		err    error
		reason string
	}{
		{fmt.Errorf("%w; retry the read once it finishes", substrate.ErrObservedRederiving), substrate.ReasonObservedRederiving},
		{fmt.Errorf("%w: every read slot is taken", substrate.ErrUnavailable), ""},
	} {
		ds.errs["Aggregate"] = x.err
		rec := env.do(t, http.MethodGet, aggregatePath(t, substrate.Aggregate{Latest: true}), tok, nil)
		wantErrorCode(t, rec, http.StatusServiceUnavailable, codeUnavailable)
		if rec.Header().Get("Retry-After") == "" {
			t.Errorf("%v: a 503 without Retry-After", x.err)
		}
		var body substrate.ErrorEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%v: decode the error body: %v", x.err, err)
		}
		if body.Error.Reason != x.reason {
			t.Errorf("%v: reason %q, want %q", x.err, body.Error.Reason, x.reason)
		}
	}
}

// The aggregate mode is refused beside every parameter of the other modes,
// naming the parameter; its own parameters decode strictly; a filter arm the
// index cannot test is refused by name; a downsample alone names what it
// needs; and the dataset's refusals are 422s.
func TestAggregateIsRefusedBesideListParameters(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	a := substrate.Aggregate{Metric: &substrate.AggregateMetric{Ref: "ada.example.com/observations/metric/x"}}

	for _, extra := range []string{"q=run", "watch=1", "count=1", "orderBy=at", "first=10", "after=c", "offset=5", "expand=source", "withAnnotations=1"} {
		name, _, _ := strings.Cut(extra, "=")
		rec := env.do(t, http.MethodGet, aggregatePath(t, a, extra), tok, nil)
		wantStatus(t, rec, http.StatusBadRequest)
		wantMessage(t, rec, `"`+name+`"`, "not supported with aggregate")
	}
	rec := env.do(t, http.MethodGet, recordsPath+"?aggregate="+url.QueryEscape(`{"metric":{"ref":"x/y/z/w"},"reducer":["avg"]}`), tok, nil)
	wantStatus(t, rec, http.StatusBadRequest)
	wantMessage(t, rec, "aggregate", "reducer")
	rec = env.do(t, http.MethodGet, recordsPath+"?aggregate="+url.QueryEscape(`{"reducers":[{"avg":1}]}`), tok, nil)
	wantStatus(t, rec, http.StatusBadRequest)
	wantMessage(t, rec, "takes no argument")
	rec = env.do(t, http.MethodGet, aggregatePath(t, a, "downsample="+url.QueryEscape(`{"points":800,"shade":"red"}`)), tok, nil)
	wantStatus(t, rec, http.StatusBadRequest)
	wantMessage(t, rec, "downsample", "shade")
	rec = env.do(t, http.MethodGet, recordsPath+"?downsample="+url.QueryEscape(`{"points":800}`), tok, nil)
	wantStatus(t, rec, http.StatusBadRequest)
	wantMessage(t, rec, "downsample needs aggregate")

	f, _ := json.Marshal(substrate.Filter{Search: "run"})
	rec = env.do(t, http.MethodGet, aggregatePath(t, a, "filter="+url.QueryEscape(string(f))), tok, nil)
	wantStatus(t, rec, http.StatusBadRequest)
	wantMessage(t, rec, "filter.search is not supported with aggregate")
	f, _ = json.Marshal(substrate.Filter{Properties: map[string]substrate.Cond{"value": {Gt: 3}}})
	rec = env.do(t, http.MethodGet, aggregatePath(t, a, "filter="+url.QueryEscape(string(f))), tok, nil)
	wantStatus(t, rec, http.StatusBadRequest)
	wantMessage(t, rec, "filter.properties.value")
	// A property read reduces records, so it takes every filter arm.
	rec = env.do(t, http.MethodGet, aggregatePath(t, substrate.Aggregate{Property: &substrate.AggregateProperty{Name: "price"}},
		"filter="+url.QueryEscape(string(f))), tok, nil)
	wantStatus(t, rec, http.StatusOK)

	two := a
	two.Property = &substrate.AggregateProperty{Name: "price"}
	rec = env.do(t, http.MethodGet, aggregatePath(t, two), tok, nil)
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, "name one of metric, metrics and property")

	ds.errs["Aggregate"] = &substrate.ValidationError{Problems: []string{"aggregate.budget.maxBuckets: 2880 buckets, over 1440"}}
	rec = env.do(t, http.MethodGet, aggregatePath(t, a), tok, nil)
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, "maxBuckets")
	ds.errs["Aggregate"] = errors.New("boom")
	rec = env.do(t, http.MethodGet, aggregatePath(t, a), tok, nil)
	wantStatus(t, rec, http.StatusInternalServerError)
}

// A metric read's filter takes a summary's statistic beside the stream keys,
// and exists or a null inside in on a stream key, and the route hands them
// to the dataset as written.
func TestRecordsAggregateTakesTheStatisticFilter(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	a := substrate.Aggregate{
		Metric:  &substrate.AggregateMetric{Ref: "ada.example.com/observations/metric/health/hrv_rmssd"},
		From:    substrate.AggregateFromSummaries,
		GroupBy: []string{substrate.GroupStatistic},
	}
	filter := `{"properties":{"statistic":{"in":["avg","max"]},"channel":{"in":[null,"workout"]},"about":{"exists":false}}}`
	rec := env.do(t, http.MethodGet, aggregatePath(t, a, "filter="+url.QueryEscape(filter)), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	q := ds.lastAggregate
	if c, ok := q.Filter.Properties["statistic"]; !ok || len(c.In) != 2 || q.Aggregate.GroupBy[0] != "statistic" {
		t.Fatalf("the dataset saw filter %+v groupBy %v", q.Filter, q.Aggregate.GroupBy)
	}
	if c := q.Filter.Properties["channel"]; len(c.In) != 2 || c.In[0] != nil || c.In[1] != "workout" {
		t.Fatalf("the dataset saw channel %+v", c)
	}
	if c := q.Filter.Properties["about"]; c.Exists == nil || *c.Exists {
		t.Fatalf("the dataset saw about %+v", c)
	}
}

// The time options of a metric read decode strictly and reach the dataset as
// written: the recorded-local calendar's fallback zone, the place of a value
// with a span, and a rolling window that ends at the previous bucket.
func TestRecordsAggregateCarriesTheTimeOptions(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	body := `{"metric":{"ref":"ada.example.com/observations/metric/health/body_mass"},` +
		`"range":{"gte":"2026-10-01T00:00:00+03:00","lt":"2026-10-04T00:00:00+03:00"},` +
		`"bucket":{"calendar":"day","fallbackTimezone":"Europe/Athens","place":"end"},"reducers":["count"],` +
		`"rolling":{"buckets":30,"reducers":["avg","stddev"],"through":"previous"}}`
	rec := env.do(t, http.MethodGet, recordsPath+"?aggregate="+url.QueryEscape(body), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	a := ds.lastAggregate.Aggregate
	if b := a.Bucket; b == nil || b.FallbackTimezone != "Europe/Athens" || b.Place != "end" {
		t.Fatalf("bucket = %+v", b)
	}
	if r := a.Rolling; r == nil || r.Buckets != 30 || r.Through != "previous" || len(r.Reducers) != 2 {
		t.Fatalf("rolling = %+v", r)
	}
}

// The options that read records decode strictly and reach the dataset as
// written: the records mode's and the property read's source policies, the
// records mode's offsets, and the property read's offset and groups.
func TestRecordsAggregateCarriesTheRecordOptions(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	sources := `{"policy":"priority","order":["whoop-4-0","garmin-forerunner-265"],"property":"source","match":["sport"]}`
	body := `{"metric":{"ref":"ada.example.com/observations/metric/health/sleep_stage"},` +
		`"range":{"gte":"2026-09-01T00:00:00+03:00","lt":"2026-10-01T00:00:00+03:00"},` +
		`"bucket":{"records":{"filter":{"kinds":["ada.example.com/health/sleep"]},"sources":` + sources + `,` +
		`"utcOffsets":{"start":"utcOffsetStart","end":"utcOffsetEnd"}}},` +
		`"reducers":["sum"]}`
	rec := env.do(t, http.MethodGet, recordsPath+"?aggregate="+url.QueryEscape(body), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	want := substrate.AggregateRecordSources{Policy: "priority", Order: []string{"whoop-4-0", "garmin-forerunner-265"}, Property: "source", Match: []string{"sport"}}
	sameSources := func(got *substrate.AggregateRecordSources) bool {
		return got != nil && got.Policy == want.Policy && strings.Join(got.Order, ",") == strings.Join(want.Order, ",") &&
			got.Property == want.Property && strings.Join(got.Match, ",") == strings.Join(want.Match, ",")
	}
	if b := ds.lastAggregate.Aggregate.Bucket; b == nil || b.Records == nil || !sameSources(b.Records.Sources) ||
		b.Records.UTCOffsets == nil || b.Records.UTCOffsets.Start != "utcOffsetStart" || b.Records.UTCOffsets.End != "utcOffsetEnd" {
		t.Fatalf("bucket = %+v", b)
	}

	body = `{"property":{"name":"timeAsleep","anchor":"endsAt","utcOffset":"utcOffsetEnd","groupBy":["sport","source"],"sources":` + sources + `},` +
		`"range":{"gte":"2026-09-01T00:00:00+03:00","lt":"2026-10-01T00:00:00+03:00"},"bucket":{"calendar":"week"},"reducers":["sum","stddev"]}`
	rec = env.do(t, http.MethodGet, recordsPath+"?aggregate="+url.QueryEscape(body), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if p := ds.lastAggregate.Aggregate.Property; p == nil || !sameSources(p.Sources) || p.UTCOffset != "utcOffsetEnd" ||
		strings.Join(p.GroupBy, ",") != "sport,source" {
		t.Fatalf("property = %+v", p)
	}
}

// A several-metric read's metrics decode strictly, each with its unit,
// reducers in both spellings, from and statistic, and reach the dataset as
// written; a key a metric does not take is a 400 naming it.
func TestRecordsAggregateCarriesSeveralMetrics(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	body := `{"metrics":[{"ref":"ada.example.com/observations/metric/health/heart_rate","unit":"/min","reducers":["avg",{"percentile":0.9}]},` +
		`{"ref":"ada.example.com/observations/metric/health/resting_heart_rate","from":"summaries","statistic":"vendor"}],` +
		`"range":{"gte":"2026-09-24T00:00:00+03:00","lt":"2026-09-25T00:00:00+03:00"},"bucket":{"calendar":"day"},"reducers":["last"]}`
	rec := env.do(t, http.MethodGet, recordsPath+"?aggregate="+url.QueryEscape(body), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	m := ds.lastAggregate.Aggregate.Metrics
	if len(m) != 2 || m[0].Ref != "ada.example.com/observations/metric/health/heart_rate" || m[0].Unit != "/min" ||
		len(m[0].Reducers) != 2 || *m[0].Reducers[1].Percentile != 0.9 ||
		m[1].From != "summaries" || m[1].Statistic != "vendor" || m[1].Reducers != nil {
		t.Fatalf("metrics = %+v", m)
	}
	rec = env.do(t, http.MethodGet, recordsPath+"?aggregate="+url.QueryEscape(`{"metrics":[{"ref":"x/y/z/w","fill":"zero"}]}`), tok, nil)
	wantStatus(t, rec, http.StatusBadRequest)
	wantMessage(t, rec, "aggregate", "fill")
}
