package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// The aggregate read answers the engine's reduction over the observed index:
// a day of heart rate reduced into its day bucket, the same day cut into a
// chart's columns by a downsample, and each metric's newest value on the
// latest read. A read naming two selectors is the engine's 422. The grammar
// the route hands the engine is TestRecordsAggregateCarriesTheGrammar.
func TestRecordsAggregateAnswersTheEnginesRead(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	if err := enginetest.InstallCatalogues(context.Background(), env.ds, "health"); err != nil {
		t.Fatalf("install the health catalogue: %v", err)
	}
	const heartRate = "substrate.reamde.dev/observations/metric/health/heart_rate"
	for i := range 3 {
		env.put(t, "substrate.reamde.dev/observations/observation", fmt.Sprintf("hr%d", i), map[string]any{
			"metric": map[string]any{"ref": heartRate},
			"form":   "gauge", "value": map[string]any{"value": "62", "unit": "/min"},
			"at": fmt.Sprintf("2026-09-01T0%d:00:00Z", 6+i), "utcOffset": 0, "revision": 1,
		})
	}
	day := &substrate.AggregateRange{Gte: "2026-09-01T00:00:00Z", Lt: "2026-09-02T00:00:00Z"}

	rec := env.do(t, http.MethodGet, aggregatePath(t, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: heartRate},
		Range:    day,
		Bucket:   &substrate.AggregateBucket{Calendar: "day", Timezone: "UTC"},
		Reducers: []substrate.AggregateReducer{{Name: substrate.ReducerAvg}},
	}), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	res := decodeJSON[substrate.AggregateResult](t, rec)
	if res.Metric != heartRate || len(res.Rows) != 1 || res.Rows[0].Count != 3 || len(res.Rows[0].Values) != 1 {
		t.Fatalf("result = %+v, want one day bucket over the three readings", res)
	}
	if v := res.Rows[0].Values[0]; v.Reducer.Name != substrate.ReducerAvg || v.Value == nil || *v.Value != "62" {
		t.Fatalf("value = %+v, want the average, 62", v)
	}

	rec = env.do(t, http.MethodGet, aggregatePath(t, substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: heartRate}, Range: day,
	}, "downsample="+url.QueryEscape(`{"points":800}`)), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	res = decodeJSON[substrate.AggregateResult](t, rec)
	readings := 0
	for _, row := range res.Rows {
		readings += int(row.Count)
	}
	if res.Bucket == nil || res.Bucket.Every == "" || len(res.Rows) < 3 || readings != 3 {
		t.Fatalf("downsample = bucket %+v over %d rows holding %d readings, want fixed columns holding the three", res.Bucket, len(res.Rows), readings)
	}

	rec = env.do(t, http.MethodGet, recordsPath+"?aggregate="+url.QueryEscape(`{"latest":true,"groupBy":["metric"]}`), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	res = decodeJSON[substrate.AggregateResult](t, rec)
	if len(res.Rows) != 1 || res.Rows[0].Group == nil || res.Rows[0].Group.Metric != heartRate || len(res.Rows[0].Values) != 1 ||
		res.Rows[0].Values[0].Value == nil || *res.Rows[0].Values[0].Value != "62" {
		t.Fatalf("the latest read = %+v, want heart rate's newest value", res.Rows)
	}

	rec = env.do(t, http.MethodGet, aggregatePath(t, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: heartRate},
		Property: &substrate.AggregateProperty{Name: "price"},
	}), env.token, nil)
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, "name one of metric, metrics and property")
}
