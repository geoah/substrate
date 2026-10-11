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
	observation := home + "/observations/observation"
	day := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	for d := range 3 {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: observation, ID: fmt.Sprintf("w/%d", d), Properties: map[string]any{
			"metric": map[string]any{"ref": home + "/observations/metric/health/body_mass"}, "form": "gauge", "hold": "PT0S",
			"value": map[string]any{"value": fmt.Sprintf("7%d.5", d), "unit": "kg"}, "at": day.AddDate(0, 0, d).Format(time.RFC3339),
		}}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
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
			"holds no observed kind",
		},
		"a kind outside the allowlist": {
			queryScope{kinds: []string{home + "/health/sleep"}, rows: 50},
			map[string]any{"aggregate": aggregate, "filter": map[string]any{"kinds": []any{observation}}},
			"not in the reads allowlist",
		},
		"over the row budget": {
			queryScope{kinds: []string{home + "/observations/*"}, rows: 2},
			map[string]any{"aggregate": aggregate},
			"budget of 2",
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
