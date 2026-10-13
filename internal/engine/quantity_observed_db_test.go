package engine_test

// A quantity filter and order over the observed trait's `value`, which reads
// a temperature as a difference where the row says `delta: true`, as the
// observed index and the aggregate read do.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestQuantityFilterAndOrderReadADeltaAsADifference(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	ctx := context.Background()
	reading := func(metric, value, unit string, delta bool) map[string]any {
		p := map[string]any{
			"metric": r.metricRef(metric), "form": "gauge", "value": quantity(value, unit),
			"at": "2026-10-07T06:41:12+03:00", "utcOffset": 180, "revision": 1,
		}
		if delta {
			p["delta"] = true
		}
		return p
	}
	for id, props := range map[string]map[string]any{
		"warmer":  reading("health/skin_temperature_deviation", "0.5", "K", true),
		"warm":    reading("health/skin_temperature_deviation", "0.2", "Cel", true),
		"cooler":  reading("health/skin_temperature_deviation", "-0.1", "[degF]", true),
		"surface": reading("health/skin_temperature", "33.5", "Cel", false),
	} {
		if _, err := r.put(t, r.observation, id, props); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	ids := func(q substrate.Query) []string {
		t.Helper()
		q.Filter.Kinds = []string{r.observation}
		q.First = 50
		page, err := r.ds.List(ctx, q)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		var out []string
		for _, rec := range page.Records {
			out = append(out, rec.ID)
		}
		return out
	}
	byID := []substrate.Order{{Property: "id"}}
	for _, tc := range []struct {
		name string
		cond substrate.Cond
		want string
	}{
		{"a difference above 0.3 Cel, and the absolute reading", substrate.Cond{Gt: quantity("0.3", "Cel")}, "surface,warmer"},
		{"a difference below 0.3 K", substrate.Cond{Lt: quantity("0.3", "K")}, "cooler,warm"},
		{"a difference equal to 0.36 [degF]", substrate.Cond{Eq: quantity("0.36", "[degF]")}, "warm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(substrate.Query{OrderBy: byID, Filter: substrate.Filter{
				Properties: map[string]substrate.Cond{"value": tc.cond},
			}})
			if strings.Join(got, ",") != tc.want {
				t.Fatalf("filtered to %v, want %s", got, tc.want)
			}
		})
	}
	if got := ids(substrate.Query{OrderBy: []substrate.Order{{Property: "value"}}}); strings.Join(got, ",") != "cooler,warm,warmer,surface" {
		t.Fatalf("ordered by value: %v, want the differences by their size, then the absolute reading", got)
	}
}
