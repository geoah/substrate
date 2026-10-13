package api

import (
	"encoding/json"
	"errors"
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
		`"unit":"min","maxGap":"PT5M","budget":{"maxSamples":5000000}}`
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
		a.Unit != "min" || a.MaxGap != "PT5M" || a.Budget.MaxSamples != 5000000 || a.Sources.Policy != "priority" ||
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
	}, "downsample="+url.QueryEscape(`{"points":800,"method":"m4"}`)), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if d := ds.lastAggregate.Downsample; d == nil || d.Points != 800 || d.Method != "m4" {
		t.Fatalf("downsample = %+v", d)
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
	wantMessage(t, rec, "name metric or property, not both")

	ds.errs["Aggregate"] = &substrate.ValidationError{Problems: []string{"aggregate.budget.maxBuckets: 2880 buckets, over 1440"}}
	rec = env.do(t, http.MethodGet, aggregatePath(t, a), tok, nil)
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, "maxBuckets")
	ds.errs["Aggregate"] = errors.New("boom")
	rec = env.do(t, http.MethodGet, aggregatePath(t, a), tok, nil)
	wantStatus(t, rec, http.StatusInternalServerError)
}

// A metric read's filter takes a summary's statistic beside the stream keys,
// and the route hands it to the dataset as written.
func TestRecordsAggregateTakesTheStatisticFilter(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	a := substrate.Aggregate{
		Metric:  &substrate.AggregateMetric{Ref: "ada.example.com/observations/metric/health/hrv_rmssd"},
		From:    substrate.AggregateFromSummaries,
		GroupBy: []string{substrate.GroupStatistic},
	}
	f, _ := json.Marshal(substrate.Filter{Properties: map[string]substrate.Cond{"statistic": {In: []any{"avg", "max"}}}})
	rec := env.do(t, http.MethodGet, aggregatePath(t, a, "filter="+url.QueryEscape(string(f))), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	q := ds.lastAggregate
	if c, ok := q.Filter.Properties["statistic"]; !ok || len(c.In) != 2 || q.Aggregate.GroupBy[0] != "statistic" {
		t.Fatalf("the dataset saw filter %+v groupBy %v", q.Filter, q.Aggregate.GroupBy)
	}
}

// The time options of a metric read decode strictly and reach the dataset as
// written: the recorded-local calendar's fallback zone.
func TestRecordsAggregateCarriesTheTimeOptions(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	body := `{"metric":{"ref":"ada.example.com/observations/metric/health/body_mass"},` +
		`"range":{"gte":"2026-10-01T00:00:00+03:00","lt":"2026-10-04T00:00:00+03:00"},` +
		`"bucket":{"calendar":"day","fallbackTimezone":"Europe/Athens"},"reducers":["count"]}`
	rec := env.do(t, http.MethodGet, recordsPath+"?aggregate="+url.QueryEscape(body), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	b := ds.lastAggregate.Aggregate.Bucket
	if b == nil || b.FallbackTimezone != "Europe/Athens" {
		t.Fatalf("bucket = %+v", b)
	}
}
