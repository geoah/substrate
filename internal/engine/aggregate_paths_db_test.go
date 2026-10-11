package engine_test

// The aggregate read's two paths held to each other (decision 0161): each
// case is a request the second review found answered differently from
// observed_rollups and from raw points, or wrongly on both, with the
// review's own numbers. Every case reads both paths and compares them row by
// row before it checks the answer.

import (
	"context"
	"encoding/json"
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
