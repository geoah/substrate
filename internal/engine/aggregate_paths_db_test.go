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
