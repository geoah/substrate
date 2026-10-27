package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A repository whose binary declared no unit kinds opens under one that
// does: the open's seed finds no kind to write, the shipped upgrade declares
// them, and the seed runs right after it, so the repository opens holding
// every shipped unit and conversion.
func TestTheUpgradeThatDeclaresTheUnitKindsSeedsThem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root := engine.MigratedDSN(t), t.TempDir()
	const core = "substrate.reamde.dev/core"

	older := shippedTree(t)
	if err := os.Remove(filepath.Join(older, core, "unit.yaml")); err != nil {
		t.Fatalf("drop the unit kinds from binary N: %v", err)
	}
	// Binary N seeded no observations package either (record 0192): its
	// metric kind names the unit kind.
	if err := os.RemoveAll(filepath.Join(older, vocabulary.PackageObservations)); err != nil {
		t.Fatal(err)
	}
	svc := openTree(t, dsn, older, engine.WithDataRoot(root), engine.WithUnitSeed(true))
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create on binary N: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.KindByRef(ctx, vocabulary.KindUnit); err == nil {
		t.Fatal("binary N declares the unit kind; the upgrade would prove nothing")
	}
	_ = svc.Close()

	newer := shippedTree(t)
	svc = openTree(t, dsn, newer, engine.WithDataRoot(root), engine.WithUnitSeed(true))
	defer func() { _ = svc.Close() }()
	ds, err = svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open on binary N+1: %v", err)
	}
	if _, err := ds.KindByRef(ctx, vocabulary.KindUnit); err != nil {
		t.Fatalf("the upgrade did not declare the unit kind: %v", err)
	}
	units, convs := shippedCounts()
	tab := unitsOf(t, ds)
	if len(tab.Units) != units || len(tab.Conversions) != convs || len(tab.Unseeded) != 0 {
		t.Fatalf("the upgrade seeded %d units and %d conversions, skipped %v; want %d and %d",
			len(tab.Units), len(tab.Conversions), tab.Unseeded, units, convs)
	}
}
