package engine

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/geoah/substrate/internal/metrics"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A committed write is counted under its repository, kind, op and hand, and
// the inventory reads the record and the package that declares its kind.
func TestUsageMetricsCountAWriteAndReadTheInventory(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	// A series names its repository by its id, which a rename does not move.
	id := ds.info.ID
	changes := metrics.Changes.WithLabelValues(metrics.Repository(id), metrics.Name(id, raceWidget), "put", "person")
	before := counterValue(t, changes)

	racePut(t, ds, map[string]any{"name": "one"})
	racePut(t, ds, map[string]any{"name": "two"})

	if got := counterValue(t, changes) - before; got != 2 {
		t.Fatalf("the changes counter moved by %v, want 2", got)
	}

	inv, err := ds.readInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inv.Records[raceWidget] != 2 {
		t.Errorf("records[%s] = %d, want 2", raceWidget, inv.Records[raceWidget])
	}
	if inv.Since.IsZero() || time.Since(inv.Since) > time.Hour {
		t.Errorf("first change at %v", inv.Since)
	}
	origins := map[string]metrics.PackageInventory{}
	for _, p := range inv.Packages {
		origins[p.Package] = p
	}
	if p := origins[racePackage]; p.Kinds != 1 {
		t.Errorf("the race package = %+v, want one kind", p)
	}
	if p := origins[vocabulary.PackageCore]; p.Origin != "builtin" || p.Kinds == 0 {
		t.Errorf("core = %+v, want builtin with its kinds", p)
	}
	// The tasks sample is imported: rehomed, and named by where it came from.
	var sample bool
	for _, p := range inv.Packages {
		if p.Origin == "sample" && p.From != "" {
			sample = true
		}
	}
	if !sample {
		t.Errorf("no imported sample among %+v", inv.Packages)
	}
}

// counterValue reads one counter's current value.
func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetCounter().GetValue()
}
