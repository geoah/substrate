package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/samples"
)

// The query tool's aggregate argument reaches the aggregate read: it answers
// the rows, holds every kind it reads to the reads allowlist (a call naming
// none reads the allowlist's observed kinds), and counts its rows against the
// row budget.
func TestAgentQueryToolAggregates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, home := aggregateToolFixture(t)
	observation := home + "/observations/observation"
	aggregate := map[string]any{
		"metric":   map[string]any{"ref": home + "/observations/metric/health/body_mass"},
		"range":    map[string]any{"gte": "2026-09-01T00:00:00Z", "lt": "2026-09-04T00:00:00Z"},
		"bucket":   map[string]any{"calendar": "day", "timezone": "UTC"},
		"reducers": []any{"avg", "count"},
	}
	scope := queryScope{kinds: []string{home + "/observations/*"}, rows: 50}
	out, ok, rows := ds.runQueryTool(ctx, scope, map[string]any{"aggregate": aggregate})
	if !ok || rows != 3 {
		t.Fatalf("aggregate: ok %v rows %d: %s", ok, rows, out)
	}
	var res substrate.AggregateResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if len(res.Rows) != 3 || *res.Rows[1].Values[0].Value != "71.5" || res.Rows[1].Values[0].Unit != "kg" {
		t.Fatalf("result = %s", out)
	}
	// The argument as a JSON string decodes the same.
	raw, _ := json.Marshal(aggregate)
	if out, ok, _ := ds.runQueryTool(ctx, scope, map[string]any{"aggregate": string(raw)}); !ok {
		t.Fatalf("a string-encoded aggregate: %s", out)
	}
	for name, x := range map[string]struct {
		scope queryScope
		args  map[string]any
		want  string
	}{
		"no observed kind granted": {
			queryScope{kinds: []string{home + "/health/sleep"}, rows: 50},
			map[string]any{"aggregate": aggregate},
			"no kind in the reads allowlist binds",
		},
		"a kind outside the allowlist": {
			queryScope{kinds: []string{home + "/health/sleep"}, rows: 50},
			map[string]any{"aggregate": aggregate, "filter": map[string]any{"kinds": []any{observation}}},
			"not in the reads allowlist",
		},
		"over the row budget": {
			queryScope{kinds: []string{home + "/observations/*"}, rows: 2},
			map[string]any{"aggregate": aggregate},
			"the 2 left of the read budget",
		},
		"a misspelled key": {scope, map[string]any{"aggregate": map[string]any{"reducer": []any{"avg"}}}, "unknown field"},
		"a refusal of the read": {scope, map[string]any{"aggregate": map[string]any{
			"metric": aggregate["metric"], "range": aggregate["range"], "reducers": []any{"sum"},
		}}, "does not apply to a gauge"},
	} {
		out, ok, _ := ds.runQueryTool(ctx, x.scope, x.args)
		if ok || !strings.Contains(out, x.want) {
			t.Errorf("%s: ok %v, %s; want a tool error naming %q", name, ok, out, x.want)
		}
	}
}

// aggregateToolFixture is a repository with the observations and health
// samples and three daily body mass values from one scale.
func aggregateToolFixture(t *testing.T) (*dataset, string) {
	t.Helper()
	ctx := context.Background()
	ds := openInternalDataset(t)
	c, err := catalog.Load(catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the sample catalog: %v", err)
	}
	for _, id := range []string{samples.Authority + "/observations", samples.Authority + "/health"} {
		if _, _, err := c.Import(ctx, substrate.ActorAPI, id, ds); err != nil {
			t.Fatalf("import %s: %v", id, err)
		}
	}
	home := testdb.Repository(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: home + "/observations/source", ID: "scale",
		Properties: map[string]any{"name": "Scale"},
	}); err != nil {
		t.Fatalf("put the source: %v", err)
	}
	day := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	for d := range 3 {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: home + "/observations/observation", ID: fmt.Sprintf("w/%d", d), Properties: map[string]any{
			"metric": map[string]any{"ref": home + "/observations/metric/health/body_mass"}, "form": "gauge", "hold": "PT0S",
			"source": map[string]any{"ref": home + "/observations/source/scale"},
			"value":  map[string]any{"value": fmt.Sprintf("7%d.5", d), "unit": "kg"}, "at": day.AddDate(0, 0, d).Format(time.RFC3339),
		}}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	return ds, home
}

// Every argument of the query tool's aggregate arm is read as the records
// route reads it and held to the allowlist: a filter sent as a JSON string
// is decoded like the aggregate argument, a reverse read's targets and the
// metrics a read names are held to the allowlist, an argument the aggregate
// read takes no part of is refused, and a read past the row budget is
// refused before it takes a point.
func TestAgentQueryToolAggregateHoldsEveryArgument(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, home := aggregateToolFixture(t)
	observation := home + "/observations/observation"
	bodyMass := home + "/observations/metric/health/body_mass"
	scale := home + "/observations/source/scale"
	days := map[string]any{"gte": "2026-09-01T00:00:00Z", "lt": "2026-09-04T00:00:00Z"}
	daily := map[string]any{
		"metric": map[string]any{"ref": bodyMass}, "range": days,
		"bucket": map[string]any{"calendar": "day", "timezone": "UTC"}, "reducers": []any{"avg"},
	}
	wide := queryScope{kinds: []string{home + "/observations/*"}, rows: 50}
	// The observations and the metrics, not the sources.
	narrow := queryScope{kinds: []string{observation, home + "/observations/metric"}, rows: 50}
	result := func(t *testing.T, out string) substrate.AggregateResult {
		t.Helper()
		var res substrate.AggregateResult
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("decode %s: %v", out, err)
		}
		return res
	}

	// A filter sent as a JSON string narrows the read: heart rate has no
	// values, so the latest read answers none, never body mass's.
	latest := map[string]any{"reducers": []any{"last"}, "groupBy": []any{"metric", "source"}}
	heartRate := `{"properties":{"metric":{"eq":"` + home + `/observations/metric/health/heart_rate"}}}`
	out, ok, _ := ds.runQueryTool(ctx, wide, map[string]any{"aggregate": latest, "filter": heartRate})
	if !ok || len(result(t, out).Rows) != 0 {
		t.Fatalf("a string filter on heart rate: ok %v, %s; want no rows", ok, out)
	}
	// The list arm decodes it the same way.
	out, ok, _ = ds.runQueryTool(ctx, wide, map[string]any{"filter": `{"kinds":["` + observation + `"],"ids":["w/1"]}`})
	var page substrate.Page
	if err := json.Unmarshal([]byte(out), &page); !ok || err != nil || len(page.Records) != 1 || page.Records[0].ID != "w/1" {
		t.Fatalf("a string filter on the list arm: ok %v, %s; want w/1 alone", ok, out)
	}

	// The kind shorthand, several metrics and a downsample each reach the read.
	out, ok, rows := ds.runQueryTool(ctx, narrow, map[string]any{"kind": observation, "aggregate": daily})
	if !ok || rows != 3 {
		t.Fatalf("kind shorthand: ok %v rows %d: %s", ok, rows, out)
	}
	metrics := map[string]any{
		"metrics": []any{map[string]any{"ref": bodyMass, "reducers": []any{"max"}}}, "range": days,
		"bucket": map[string]any{"calendar": "day", "timezone": "UTC"},
	}
	out, ok, _ = ds.runQueryTool(ctx, narrow, map[string]any{"aggregate": metrics})
	if res := result(t, out); !ok || len(res.Rows) != 3 || res.Units[bodyMass] != "kg" || res.Rows[2].Group.Metric != bodyMass {
		t.Fatalf("several metrics: ok %v, %s", ok, out)
	}
	out, ok, _ = ds.runQueryTool(ctx, narrow, map[string]any{
		"aggregate":  map[string]any{"metric": map[string]any{"ref": bodyMass}, "range": days},
		"downsample": map[string]any{"points": 3},
	})
	if res := result(t, out); !ok || len(res.Rows) != 3 || len(res.Rows[0].Values) != 4 || *res.Rows[0].Values[0].Value != "70.5" {
		t.Fatalf("downsample: ok %v, %s", ok, out)
	}

	// A model sends "" or {} for an argument it does not mean: an empty
	// aggregate leaves a get a get, and an empty downsample beside a bucketed
	// read is no downsample.
	for _, filler := range []any{"", map[string]any{}} {
		out, ok, _ = ds.runQueryTool(ctx, wide, map[string]any{"id": "w/0", "kind": observation, "aggregate": filler})
		if !ok || !strings.Contains(out, `"record"`) {
			t.Fatalf("a get beside aggregate %#v: ok %v, %s; want the record", filler, ok, out)
		}
		out, ok, rows = ds.runQueryTool(ctx, wide, map[string]any{"aggregate": daily, "downsample": filler})
		if !ok || rows != 3 {
			t.Fatalf("a bucketed read beside downsample %#v: ok %v rows %d, %s; want the three days", filler, ok, rows, out)
		}
	}

	for name, x := range map[string]struct {
		scope queryScope
		args  map[string]any
		want  string
	}{
		"a filter neither an object nor a string":      {wide, map[string]any{"aggregate": daily, "filter": 5.0}, "filter is JSON"},
		"a list filter neither an object nor a string": {wide, map[string]any{"filter": true}, "filter is JSON"},
		"a reverse read of a kind outside the allowlist": {narrow, map[string]any{
			"filter":    map[string]any{"kinds": []any{observation}, "referencing": map[string]any{"ref": scale}},
			"aggregate": map[string]any{"property": map[string]any{"name": "value"}, "range": days, "reducers": []any{"count"}},
		}, "observations/source is not in the reads allowlist"},
		"a records bucket's reverse read outside the allowlist": {narrow, map[string]any{"aggregate": map[string]any{
			"metric": map[string]any{"ref": bodyMass}, "range": days, "reducers": []any{"avg"},
			"bucket": map[string]any{"records": map[string]any{"filter": map[string]any{
				"kinds": []any{observation}, "referencing": map[string]any{"ref": scale},
			}}},
		}}, "observations/source is not in the reads allowlist"},
		"a metric outside the allowlist": {
			queryScope{kinds: []string{observation}, rows: 50},
			map[string]any{"aggregate": daily},
			"observations/metric is not in the reads allowlist",
		},
		"one of several metrics outside the allowlist": {
			queryScope{kinds: []string{observation}, rows: 50},
			map[string]any{"aggregate": metrics},
			"observations/metric is not in the reads allowlist",
		},
		"q beside aggregate":       {wide, map[string]any{"q": "weight", "first": 1.0, "aggregate": daily}, "q is refused beside aggregate"},
		"a get beside aggregate":   {wide, map[string]any{"id": "w/0", "kind": observation, "aggregate": daily}, "id is refused beside aggregate"},
		"orderBy beside aggregate": {wide, map[string]any{"orderBy": []any{map[string]any{"property": "at"}}, "aggregate": daily}, "orderBy is refused beside aggregate"},
		"downsample alone":         {wide, map[string]any{"downsample": map[string]any{"points": 3}}, "downsample cuts an aggregate read"},
		"a property read naming no kinds": {wide, map[string]any{"aggregate": map[string]any{
			"property": map[string]any{"name": "value"}, "range": days, "reducers": []any{"count"},
		}}, "a property read names the kinds whose property it reduces"},
		"no observed kind granted": {
			queryScope{kinds: []string{home + "/health/sleep"}, rows: 50},
			map[string]any{"aggregate": daily},
			"no kind in the reads allowlist binds",
		},
	} {
		out, ok, _ := ds.runQueryTool(ctx, x.scope, x.args)
		if ok || !strings.Contains(out, x.want) {
			t.Errorf("%s: ok %v, %s; want a tool error naming %q", name, ok, out, x.want)
		}
	}

	// A read answering more rows than the budget holds is refused before it
	// takes a point.
	traced := context.WithValue(ctx, aggregateTraceKey{}, &aggTrace{})
	out, ok, rows = ds.runQueryTool(traced, queryScope{kinds: wide.kinds, rows: 2}, map[string]any{"aggregate": daily})
	if tr := traceOf(traced); ok || rows != 0 || tr.rawPoints != 0 || tr.rollupRows != 0 || !strings.Contains(out, "read budget") {
		t.Fatalf("over the row budget: ok %v rows %d, read %d points and %d rollup rows: %s", ok, rows, tr.rawPoints, tr.rollupRows, out)
	}
	// The read's own advice to raise maxBuckets cannot help, since the row
	// budget lowers it again, so a model that followed it would spend a call.
	if strings.Contains(out, "raise maxBuckets") || !strings.Contains(out, "does not help") {
		t.Fatalf("over the row budget: %s; want the read budget's remedy, not the read's", out)
	}
}
