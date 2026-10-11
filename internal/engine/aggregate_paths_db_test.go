package engine_test

// The aggregate read's two paths held to each other (decision 0161): each
// case is a request the second review found answered differently from
// observed_rollups and from raw points, or wrongly on both, with the
// review's own numbers. Every case reads both paths and compares them row by
// row before it checks the answer.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// bothPaths runs one read from rollups where they serve and once from raw
// points alone, and fails unless the two answer the same rows.
func (f aggFix) bothPaths(filter substrate.Filter, a substrate.Aggregate) (rollups, raw *substrate.AggregateResult, rollupRows int64) {
	f.t.Helper()
	ctx, trace := engine.AggregateTraced(context.Background())
	rollups = f.agg(ctx, filter, a)
	raw = f.agg(engine.AggregateRaw(context.Background()), filter, a)
	sameRows(f.t, rollups, raw)
	return rollups, raw, trace.RollupRows()
}

// sameRows fails unless the two results hold the same rows, field by field.
func sameRows(t *testing.T, rollups, raw *substrate.AggregateResult) {
	t.Helper()
	a, _ := json.Marshal(rollups.Rows)
	b, _ := json.Marshal(raw.Rows)
	if string(a) != string(b) {
		t.Errorf("rollups and raw points answer differently:\nrollups %s\nraw     %s", a, b)
	}
}

func at(t time.Time) string { return t.Format(time.RFC3339) }

// A read takes the forms its range holds, never the form of the stream's
// newest point: an amount early in the hour is counted though a gauge of the
// same stream came after it, a range holding both is refused as mixed, and a
// summary written later leaves a read from raw as it was.
func TestAggregateFormsComeFromTheRange(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	steps := f.metricRef("health/steps")
	f.mustPut(t, f.observation, "phone/early", map[string]any{
		"metric": steps, "source": f.sourceRef("phone"), "form": "amount", "value": quantity("10", "{steps}"),
		"at": at(start), "endsAt": at(start.Add(10 * time.Minute)),
	})
	f.mustPut(t, f.observation, "phone/later", map[string]any{
		"metric": steps, "source": f.sourceRef("phone"), "form": "gauge", "value": quantity("20", "{steps}"),
		"at": at(start.Add(30 * time.Minute)),
	})
	quarter := substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
		Range:    rng(start, start.Add(15*time.Minute)),
		Reducers: reducers("count"),
	}
	check := func(stage string) {
		t.Helper()
		res, _, _ := f.bothPaths(substrate.Filter{}, quarter)
		if len(res.Rows) != 1 {
			t.Fatalf("%s: %d rows, want the amount's", stage, len(res.Rows))
		}
		if c, _ := val(res.Rows[0], "count"); c != "1" || res.Rows[0].Form != "amount" {
			t.Errorf("%s: count %s form %s, want 1 amount", stage, c, res.Rows[0].Form)
		}
	}
	check("a gauge after the amount")
	hour := quarter
	hour.Range = rng(start, start.Add(time.Hour))
	f.refusedAgg(substrate.AggregateQuery{Aggregate: &hour}, "a row never mixes forms")
	f.mustPut(t, f.observation, "phone/daily", map[string]any{
		"metric": steps, "source": f.sourceRef("phone"), "form": "summary", "statistic": "sum", "period": "day",
		"value": quantity("100", "{steps}"), "at": at(start), "endsAt": at(start.Add(24 * time.Hour)),
	})
	check("a summary after both")
}

// An amount's coverage is the union of its spans on both paths. Two traces of
// one source over [00:00, 00:35) and [00:15, 00:50) cover 50 minutes, not 70,
// so prefer-raw passes the hour's 100-step summary over under its 0.9 floor
// only where raw points cover 54 minutes; and two amounts in one quarter hour
// at two offsets (two rollup groups) cover their union, an hour, not 90
// minutes.
func TestAggregateAmountCoverageIsAUnion(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		id       string
		from, to time.Duration
	}{{"a", 0, 35 * time.Minute}, {"b", 15 * time.Minute, 50 * time.Minute}} {
		f.series("steps/phone/"+x.id, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount", "unit": "{steps}",
		}, start.Add(x.from), start.Add(x.to), []int64{0}, []any{"1"})
	}
	f.mustPut(t, f.observation, "phone/hour", map[string]any{
		"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "summary", "statistic": "sum",
		"period": "hour", "value": quantity("100", "{steps}"), "at": at(start), "endsAt": at(start.Add(time.Hour)),
	})
	hour := substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
		Range:    rng(start, start.Add(time.Hour)),
		Reducers: reducers("sum"),
		From:     "prefer-raw",
	}
	res, _, _ := f.bothPaths(substrate.Filter{}, hour)
	if len(res.Rows) != 1 || res.Rows[0].From != "summaries" {
		t.Fatalf("prefer-raw: %+v, want the summary's row", res.Rows)
	}
	if v, _ := val(res.Rows[0], "sum"); v != "100" {
		t.Errorf("prefer-raw sum = %s, want the summary's 100", v)
	}
	hour.From = ""
	res, _, rollupRows := f.bothPaths(substrate.Filter{}, hour)
	if rollupRows == 0 {
		t.Error("the raw read took no rollup row")
	}
	if len(res.Rows) != 1 || res.Rows[0].Coverage != "PT50M" {
		t.Fatalf("from raw: %+v, want 50 minutes covered", res.Rows)
	}
	if v, _ := val(res.Rows[0], "sum"); v != "2" {
		t.Errorf("from raw sum = %s, want the two traces' 2", v)
	}
	// Two rollup groups of one quarter hour: offsets +03:00 and 00:00.
	next := start.Add(2 * time.Hour)
	for _, x := range []struct {
		id       string
		from, to time.Duration
		offset   int
	}{{"long", 0, time.Hour, 180}, {"short", 5 * time.Minute, 35 * time.Minute, 0}} {
		f.mustPut(t, f.observation, "watch/"+x.id, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("watch"), "form": "amount",
			"value": quantity("3", "{steps}"), "at": at(next.Add(x.from)), "endsAt": at(next.Add(x.to)), "utcOffset": x.offset,
		})
	}
	hour.Range = rng(next, next.Add(time.Hour))
	res, _, _ = f.bothPaths(substrate.Filter{}, hour)
	if len(res.Rows) != 1 || res.Rows[0].Coverage != "PT1H" {
		t.Errorf("two groups in one quarter hour: %+v, want an hour covered", res.Rows)
	}
}

// The bucket budget counts the series a read answers, codes included, before
// a point is read: two coded amounts grouped by code over four quarter hours
// with fill are eight rows, refused under maxBuckets 4 and answered in full
// under 8.
func TestAggregateBucketBudgetCountsCodes(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, code := range []string{"light", "deep"} {
		from := start.Add(time.Duration(i) * 20 * time.Minute)
		f.mustPut(t, f.observation, "stage/"+code, map[string]any{
			"metric": f.metricRef("health/sleep_stage"), "source": f.sourceRef("ring"), "form": "amount",
			"value": quantity("5", "min"), "code": code, "at": at(from), "endsAt": at(from.Add(5 * time.Minute)),
		})
	}
	read := func(maxBuckets int) substrate.AggregateQuery {
		return substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
			Range:    rng(start, start.Add(time.Hour)),
			Bucket:   &substrate.AggregateBucket{Every: "PT15M"},
			GroupBy:  []string{"code"},
			Reducers: reducers("sum"),
			Fill:     "zero",
			Budget:   &substrate.AggregateBudget{MaxBuckets: maxBuckets, MaxGroups: 2},
		}}
	}
	ctx, trace := engine.AggregateTraced(context.Background())
	if _, err := f.ds.Aggregate(ctx, read(4)); !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "aggregate.budget.maxBuckets") {
		t.Fatalf("err = %v, want a refusal naming maxBuckets", err)
	}
	if trace.RawPoints() != 0 || trace.RollupRows() != 0 {
		t.Errorf("the refused read took %d raw points and %d rollup rows", trace.RawPoints(), trace.RollupRows())
	}
	q := read(8)
	res, _, _ := f.bothPaths(q.Filter, *q.Aggregate)
	if len(res.Rows) != 8 {
		t.Errorf("under maxBuckets 8: %d rows, want two codes in four buckets", len(res.Rows))
	}
}

// A counter read in a unit it was not stored in: its extremes are in the
// unit asked for on both paths while its steps take the stored digits, and a
// bucket whose one reading has none before it has no increase on either path
// (null), not a measured change of zero.
func TestAggregateCounterInAnotherUnit(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		id, value, unit string
		at              time.Duration
	}{{"first", "1", "kW.h", 0}, {"second", "2000", "W.h", 30 * time.Minute}} {
		f.mustPut(t, f.observation, "meter/"+x.id, map[string]any{
			"metric": f.metricRef("energy"), "source": f.sourceRef("meter"), "form": "counter", "resetRule": "monotonic",
			"value": quantity(x.value, x.unit), "at": at(start.Add(x.at)),
		})
	}
	read := func(to time.Duration) substrate.AggregateRow {
		t.Helper()
		res, _, rollupRows := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("energy")},
			Range:    rng(start, start.Add(to)),
			Reducers: reducers("min", "max", "increase"),
			Unit:     "W.h",
		})
		if rollupRows == 0 {
			t.Error("the read took no rollup row")
		}
		if len(res.Rows) != 1 {
			t.Fatalf("%d rows, want one", len(res.Rows))
		}
		return res.Rows[0]
	}
	lone := read(15 * time.Minute)
	if lo, _ := val(lone, "min"); lo != "1000" {
		t.Errorf("the lone kW.h reading's min = %s W.h, want 1000", lo)
	}
	if v, ok := val(lone, "increase"); ok {
		t.Errorf("a reading with none before it has increase %s, want none", v)
	}
	hour := read(time.Hour)
	if lo, _ := val(hour, "min"); lo != "1000" {
		t.Errorf("min = %s W.h, want 1000", lo)
	}
	if hi, _ := val(hour, "max"); hi != "2000" {
		t.Errorf("max = %s W.h, want 2000", hi)
	}
	if v, _ := val(hour, "increase"); v != "2000" {
		t.Errorf("increase = %s W.h, want 2000: a reading in another unit starts over", v)
	}
}

// A recorded-local day starts and ends at the offsets of its first and last
// points on both paths, however those fall in quarter hours: an amount and a
// gauge at 12:01Z (+03:00) and 12:10Z (-04:00), one rollup slot, make a day
// from midnight +03:00 to midnight -04:00, 31 hours, not 17. And prefer-raw
// measures a day's coverage over the 31 hours it reports: 25 held hours are
// not 0.9 of it, so the day's summary serves it.
func TestAggregateRecordedLocalDayFromRealPoints(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	noon := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		id, value string
		at        time.Duration
		offset    int
	}{{"a", "10", time.Minute, 180}, {"b", "20", 10 * time.Minute, -240}} {
		f.mustPut(t, f.observation, "travel/steps/"+x.id, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("travel"), "form": "amount",
			"value": quantity(x.value, "{steps}"), "at": at(noon.Add(x.at)), "endsAt": at(noon.Add(x.at + 5*time.Minute)),
			"utcOffset": x.offset,
		})
		f.mustPut(t, f.observation, "travel/hr/"+x.id, map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("travel"), "form": "gauge",
			"value": quantity(x.value, "/min"), "at": at(noon.Add(x.at)), "hold": "PT5M", "utcOffset": x.offset,
		})
	}
	athensDay := time.Date(2026, 9, 1, 0, 0, 0, 0, time.FixedZone("", 3*3600))
	travel := substrate.Filter{Properties: map[string]substrate.Cond{"source": {Eq: f.sourcePath("travel")}}}
	for _, x := range []struct{ metric, reducer string }{{"health/steps", "sum"}, {"health/heart_rate", "twa"}} {
		res, _, _ := f.bothPaths(travel, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath(x.metric)},
			Range:    rng(athensDay, athensDay.AddDate(0, 0, 2)),
			Bucket:   &substrate.AggregateBucket{Calendar: "day"},
			Reducers: reducers("count", x.reducer),
		})
		if len(res.Rows) != 1 {
			t.Fatalf("%s: %d rows, want the one day", x.metric, len(res.Rows))
		}
		row := res.Rows[0]
		if row.Start.Format(time.RFC3339) != "2026-09-01T00:00:00+03:00" || row.End.Format(time.RFC3339) != "2026-09-02T00:00:00-04:00" {
			t.Errorf("%s: the day runs %s to %s, want midnight +03:00 to midnight -04:00", x.metric,
				row.Start.Format(time.RFC3339), row.End.Format(time.RFC3339))
		}
	}
	// A gauge held 24 hours at +03:00 and then 1 hour at -04:00 of the same
	// local day, and the day's summary.
	trip := substrate.Filter{Properties: map[string]substrate.Cond{"source": {Eq: f.sourcePath("trip")}}}
	for _, x := range []struct {
		id, value string
		at        time.Time
		offset    int
	}{{"1", "60", athensDay, 180}, {"2", "80", time.Date(2026, 9, 2, 3, 0, 0, 0, time.UTC), -240}} {
		f.mustPut(t, f.observation, "trip/hr/"+x.id, map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("trip"), "form": "gauge",
			"value": quantity(x.value, "/min"), "at": at(x.at), "utcOffset": x.offset,
		})
	}
	f.mustPut(t, f.observation, "trip/hr/day", map[string]any{
		"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("trip"), "form": "summary",
		"statistic": "avg", "period": "day", "value": quantity("70", "/min"),
		"at": at(athensDay), "endsAt": at(athensDay.AddDate(0, 0, 1)), "utcOffset": 180,
	})
	res, _, _ := f.bothPaths(trip, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
		From:     "prefer-raw",
		Range:    rng(athensDay, athensDay.AddDate(0, 0, 2)),
		Bucket:   &substrate.AggregateBucket{Calendar: "day"},
		Reducers: reducers("avg"),
	})
	if len(res.Rows) == 0 || res.Rows[0].From != "summaries" {
		t.Fatalf("prefer-raw: %+v, want the first day from its summary", res.Rows)
	}
	if v, _ := val(res.Rows[0], "avg"); v != "70" {
		t.Errorf("the first day's avg = %s, want the summary's 70", v)
	}
}

// Points at one instant are first and last by record id on both paths:
// `a-reversed` holds 2000 W.h and `z-reversed` 1 kW.h at 00:05, two rollup
// groups by unit, and in W.h the first is 2000 and the last 1000.
func TestAggregateFirstAndLastBreakTiesByRecord(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct{ id, value, unit string }{{"a-reversed", "2000", "W.h"}, {"z-reversed", "1", "kW.h"}} {
		f.mustPut(t, f.observation, x.id, map[string]any{
			"metric": f.metricRef("energy"), "source": f.sourceRef("meter"), "form": "gauge",
			"value": quantity(x.value, x.unit), "at": at(start.Add(5 * time.Minute)),
		})
	}
	res, _, rollupRows := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("energy")},
		Range:    rng(start, start.Add(time.Hour)),
		Reducers: reducers("first", "last"),
		Unit:     "W.h",
	})
	if rollupRows == 0 {
		t.Error("the read took no rollup row")
	}
	if len(res.Rows) != 1 {
		t.Fatalf("%d rows", len(res.Rows))
	}
	first, _ := val(res.Rows[0], "first")
	last, _ := val(res.Rows[0], "last")
	if first != "2000" || last != "1000" {
		t.Errorf("first/last = %s/%s, want 2000/1000", first, last)
	}
}

// `eq` and `in` on one stream key admit what both admit, as the list's filter
// does: `eq` source A beside `in` [source B] reads no stream, where it once
// read both, and the same holds for a code.
func TestAggregateFilterConditionsIntersect(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, src := range []string{"a", "b"} {
		f.mustPut(t, f.observation, "door/"+src, map[string]any{
			"metric": f.metricRef("air_temperature"), "source": f.sourceRef(src), "form": "gauge",
			"code": []string{"open", "closed"}[i], "at": at(start.Add(time.Duration(i) * time.Minute)),
		})
	}
	read := func(name string, c substrate.Cond) []substrate.AggregateRow {
		t.Helper()
		filter := substrate.Filter{Kinds: []string{f.observation}, Properties: map[string]substrate.Cond{name: c}}
		res, _, _ := f.bothPaths(filter, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("air_temperature")},
			Range:    rng(start, start.Add(time.Hour)),
			Reducers: reducers("count"),
		})
		return res.Rows
	}
	both := substrate.Cond{Eq: f.sourcePath("a"), In: []any{f.sourcePath("b")}}
	page, err := f.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
		Kinds: []string{f.observation}, Properties: map[string]substrate.Cond{"source": both},
	}})
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("the list under eq A and in [B]: %d records (%v), want none", len(page.Records), err)
	}
	if rows := read("source", both); len(rows) != 0 {
		t.Errorf("eq A and in [B] on source: %d rows, want none", len(rows))
	}
	overlap := substrate.Cond{Eq: f.sourcePath("a"), In: []any{f.sourcePath("a"), f.sourcePath("b")}}
	if rows := read("source", overlap); len(rows) != 1 || rows[0].Sources[0] != f.sourcePath("a") {
		t.Errorf("eq A and in [A, B] on source: %+v, want A's row alone", rows)
	}
	if rows := read("code", substrate.Cond{Eq: "open", In: []any{"closed"}}); len(rows) != 0 {
		t.Errorf("eq open and in [closed] on code: %d rows, want none", len(rows))
	}
}

// locf carries a coded stream's previous row whole: `open` at 00:15 and
// `closed` at 01:15, each held a minute, leave the third hour `closed`, for
// a last and for the time in each state alike.
func TestAggregateLocfCarriesThePreviousCode(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, code := range []string{"open", "closed"} {
		f.mustPut(t, f.observation, "door/"+code, map[string]any{
			"metric": f.metricRef("air_temperature"), "source": f.sourceRef("contact"), "form": "gauge", "code": code,
			"hold": "PT1M", "at": at(start.Add(time.Duration(i)*time.Hour + 15*time.Minute)),
		})
	}
	read := func(red substrate.AggregateReducer, unit string) substrate.AggregateRow {
		t.Helper()
		res, _, _ := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("air_temperature")},
			Range:    rng(start, start.Add(3*time.Hour)),
			Bucket:   &substrate.AggregateBucket{Every: "PT1H"},
			Reducers: []substrate.AggregateReducer{red},
			Fill:     "locf",
			Unit:     unit,
		})
		if len(res.Rows) != 3 || !res.Rows[2].Synthetic {
			t.Fatalf("%s: %+v, want three hours, the third filled", red.String(), res.Rows)
		}
		return res.Rows[2]
	}
	if last := read(substrate.AggregateReducer{Name: "last"}, ""); len(last.Values) != 1 || last.Values[0].Code != "closed" {
		t.Errorf("the filled hour's last = %+v, want closed", last.Values)
	}
	states := read(substrate.AggregateReducer{Name: "timeInState"}, "min")
	if len(states.Values) != 1 || states.Values[0].Code != "closed" || states.Values[0].Value == nil || *states.Values[0].Value != "1" {
		t.Errorf("the filled hour's time in state = %+v, want closed for 1 min", states.Values)
	}
}

// A property read counts its groups before it reads a value: a EUR and a JPY
// spend are two groups, refused under maxGroups 1, and two groups in two
// months are four cells, refused under maxBuckets 3.
func TestAggregatePropertyBudgetsBeforeTheScan(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	ctx := context.Background()
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{vocabularyPackage(moneyPackage), spendManifest()}); err != nil {
		t.Fatalf("install the spend kind: %v", err)
	}
	for i, x := range []struct {
		amount   int64
		currency string
	}{{1005, "EUR"}, {2, "JPY"}} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: moneyPackage + "/spend", ID: strconv.Itoa(i), Properties: map[string]any{
			"price": moneyValue(x.amount, x.currency), "paidAt": "2026-09-03T10:00:00Z",
		}})
	}
	read := func(budget substrate.AggregateBudget) error {
		_, err := ds.Aggregate(ctx, substrate.AggregateQuery{
			Filter: substrate.Filter{Kinds: []string{moneyPackage + "/spend"}},
			Aggregate: &substrate.Aggregate{
				Property: &substrate.AggregateProperty{Name: "price", Anchor: "paidAt"},
				Range:    rng(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)),
				Bucket:   &substrate.AggregateBucket{Calendar: "month", Timezone: "UTC"},
				Reducers: reducers("sum"),
				Budget:   &budget,
			},
		})
		return err
	}
	for _, x := range []struct {
		budget substrate.AggregateBudget
		want   string
	}{
		{substrate.AggregateBudget{MaxGroups: 1}, "aggregate.budget.maxGroups"},
		{substrate.AggregateBudget{MaxBuckets: 3}, "aggregate.budget.maxBuckets"},
	} {
		if err := read(x.budget); !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), x.want) {
			t.Errorf("budget %+v: err = %v, want a refusal naming %s", x.budget, err, x.want)
		}
	}
	if err := read(substrate.AggregateBudget{MaxGroups: 2, MaxBuckets: 4}); err != nil {
		t.Errorf("within the budget: %v", err)
	}
}

// A min or a max answers the stored digits, compared exactly, on both paths
// and in a downsample's columns, and the read says it is exact:
// 9007199254740993 and 9007199254740992 are one float64, which once answered
// 9007199254740990 with exact true.
func TestAggregateExtremesKeepTheirDigits(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, v := range []string{"9007199254740993", "9007199254740992"} {
		from := start.Add(time.Duration(1+20*i) * time.Minute)
		f.mustPut(t, f.observation, "big/"+strconv.Itoa(i), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("counter"), "form": "amount",
			"value": quantity(v, "{steps}"), "at": at(from), "endsAt": at(from.Add(time.Minute)),
		})
	}
	a := substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
		Range:    rng(start, start.Add(time.Hour)),
		Reducers: reducers("min", "max"),
	}
	rollups, raw, _ := f.bothPaths(substrate.Filter{}, a)
	for _, res := range []*substrate.AggregateResult{rollups, raw} {
		if len(res.Rows) != 1 {
			t.Fatalf("%d rows", len(res.Rows))
		}
		lo, _ := val(res.Rows[0], "min")
		hi, _ := val(res.Rows[0], "max")
		if lo != "9007199254740992" || hi != "9007199254740993" || !res.Exact {
			t.Errorf("min/max = %s/%s, exact %v; want the stored digits, exact", lo, hi, res.Exact)
		}
	}
	a.Reducers = nil
	cols, err := f.ds.Aggregate(context.Background(), substrate.AggregateQuery{Aggregate: &a, Downsample: &substrate.Downsample{Points: 1}})
	if err != nil {
		t.Fatalf("downsample: %v", err)
	}
	if len(cols.Rows) != 1 || !cols.Exact {
		t.Fatalf("downsample: %+v, exact %v", cols.Rows, cols.Exact)
	}
	if lo, _ := val(cols.Rows[0], "min"); lo != "9007199254740992" {
		t.Errorf("the column's min = %s, want 9007199254740992", lo)
	}
	if hi, _ := val(cols.Rows[0], "max"); hi != "9007199254740993" {
		t.Errorf("the column's max = %s, want 9007199254740993", hi)
	}
}

// A downsample answers at most `points` columns when its origin moves back to
// a quarter hour: [00:01, 01:01) in one column is one column, not two.
func TestAggregateDownsampleKeepsItsColumnCount(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 1, 0, 0, time.UTC)
	for i, minute := range []time.Duration{0, 59} {
		from := start.Add(minute * time.Minute)
		f.mustPut(t, f.observation, "col/"+strconv.Itoa(i), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount",
			"value": quantity("5", "{steps}"), "at": at(from), "endsAt": at(from.Add(time.Minute)),
		})
	}
	for _, points := range []int{1, 2, 3} {
		res, err := f.ds.Aggregate(context.Background(), substrate.AggregateQuery{
			Aggregate: &substrate.Aggregate{
				Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
				Range:  rng(start, start.Add(time.Hour)),
			},
			Downsample: &substrate.Downsample{Points: points},
		})
		if err != nil {
			t.Fatalf("%d points: %v", points, err)
		}
		if len(res.Rows) == 0 || len(res.Rows) > points {
			t.Errorf("%d points: %d columns (%s wide)", points, len(res.Rows), res.Bucket.Every)
		}
	}
}

// A records-mode window is not clipped to the range, on either path: a sleep
// record over [00:00, 02:00) read with a range of [00:00, 01:00) takes an
// amount at 01:30 and a gauge's value held after 01:00, and an `around`
// widening reaches past the range at both ends.
func TestAggregateRecordWindowsReachPastTheRange(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	sleep := strings.TrimSuffix(f.observation, "/observations/observation") + "/health/sleep"
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	f.mustPut(t, sleep, "night", map[string]any{"at": at(start), "endsAt": at(start.Add(2 * time.Hour))})
	f.mustPut(t, sleep, "nap", map[string]any{"at": at(start.Add(30 * time.Minute)), "endsAt": at(start.Add(45 * time.Minute))})
	for _, x := range []struct {
		id, value string
		from, to  time.Duration
	}{{"before", "5", -5 * time.Minute, 0}, {"after", "10", 90 * time.Minute, 100 * time.Minute}} {
		f.mustPut(t, f.observation, "steps/"+x.id, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount",
			"value": quantity(x.value, "{steps}"), "at": at(start.Add(x.from)), "endsAt": at(start.Add(x.to)),
		})
	}
	for _, x := range []struct {
		id, value string
		at        time.Duration
	}{{"evening", "60", -time.Hour}, {"night", "80", 90 * time.Minute}} {
		f.mustPut(t, f.observation, "hr/"+x.id, map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("phone"), "form": "gauge",
			"value": quantity(x.value, "/min"), "at": at(start.Add(x.at)),
		})
	}
	read := func(metric, id string, around *substrate.AggregateAround, reds ...string) substrate.AggregateRow {
		t.Helper()
		res, _, rollupRows := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath(metric)},
			Range:  rng(start, start.Add(time.Hour)),
			Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
				Filter: substrate.Filter{Kinds: []string{sleep}, IDs: []string{id}}, Around: around,
			}},
			Reducers: reducers(reds...),
		})
		if rollupRows == 0 {
			t.Errorf("%s over %s read no rollup row; the windows are whole rows", metric, id)
		}
		if len(res.Rows) != 1 {
			t.Fatalf("%s over %s: %d rows, want one", metric, id, len(res.Rows))
		}
		return res.Rows[0]
	}
	if v, _ := val(read("health/steps", "night", nil, "sum"), "sum"); v != "10" {
		t.Errorf("the night's steps = %s, want the 10 at 01:30", v)
	}
	widened := &substrate.AggregateAround{Before: "PT1H", After: "PT1H"}
	if v, _ := val(read("health/steps", "nap", widened, "sum"), "sum"); v != "15" {
		t.Errorf("the widened nap's steps = %s, want 5 before the range and 10 after it", v)
	}
	night := read("health/heart_rate", "night", nil, "twa", "count")
	if v, _ := val(night, "twa"); v != "65" {
		t.Errorf("the night's time-weighted heart rate = %s, want 65 (60 held 90 minutes, 80 for 30)", v)
	}
	if c, _ := val(night, "count"); c != "1" {
		t.Errorf("the night's count = %s, want the point at 01:30", c)
	}
	nap := read("health/heart_rate", "nap", widened, "twa")
	if v, _ := val(nap, "twa"); !close9(num(t, v), (60*120+80*15)/135.0) {
		t.Errorf("the widened nap's time-weighted heart rate = %s, want %v", v, (60*120+80*15)/135.0)
	}
}
