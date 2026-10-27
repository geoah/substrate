package engine_test

// The seeded observations package (decision record 0192): a new repository
// holds the five base kinds under the substrate's own name, with no import,
// and a repository created before the binary seeded them receives them at its
// next open like any shipped upgrade. Its records are the owner's to write;
// its declarations are not.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// observationsKinds are the five kinds the seeded package declares.
var observationsKinds = []string{kindMetric, kindObservation, kindTrace, kindSource, kindSourcelink}

// requireObservationsSeeded fails unless ds declares every observations kind
// as the seed's, keeps each kind's own purpose, and takes an owner's metric
// and observation through them while refusing the token a declaration.
func requireObservationsSeeded(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	for _, kind := range observationsKinds {
		info, err := ds.KindByRef(ctx, kind)
		if err != nil {
			t.Fatalf("%s is not declared: %v", kind, err)
		}
		if info.Source != vocabulary.SourceBuiltin {
			t.Errorf("%s source = %q, want %q", kind, info.Source, vocabulary.SourceBuiltin)
		}
	}
	// Unlike core's, these kinds are the owner's data and keep their own
	// purpose rather than reading as the substrate's machinery.
	decl := mustGet(t, ds, "substrate.reamde.dev/core/kind", kindObservation)
	if got := decl.Properties["purpose"]; got == vocabulary.PurposeInternal {
		t.Errorf("%s declares purpose %v, want the owner's data", kindObservation, got)
	}
	// The first-party catalogues are installed, never seeded.
	for _, kind := range []string{kindSleep, kindLabresult} {
		if _, err := ds.KindByRef(ctx, kind); !errors.Is(err, substrate.ErrNotFound) {
			t.Errorf("%s is declared without an install: %v", kind, err)
		}
	}

	putShippedUnits(t, ds, "1/min")
	mustPut(t, ds, owner, substrate.PutInput{Kind: kindMetric, ID: "mine/pulse", Properties: map[string]any{
		"name": "Pulse", "unit": unitRef("1/min"), "form": "gauge",
	}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: kindObservation, ID: "pulse/1", Properties: map[string]any{
		"metric": map[string]any{"ref": kindMetric + "/mine/pulse"}, "form": "gauge", "hold": "PT0S",
		"value": quantity("61", "/min"), "at": "2026-10-07T06:41:12Z",
	}})

	_, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{{
		"kind":     "substrate.reamde.dev/core/kind",
		"metadata": map[string]any{"id": kindObservation},
		"data": map[string]any{
			"authority": vocabulary.AuthorityPublisher, "package": "observations",
			"names":      map[string]any{"singular": "observation"},
			"properties": map[string]any{"note": map[string]any{"type": "string"}},
		},
	}})
	if !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a token rewrote the seeded observations package: %v", err)
	}
}

func TestCreationSeedsTheObservationsPackage(t *testing.T) {
	t.Parallel()
	_, ds := newCoreDataset(t)
	requireObservationsSeeded(t, ds)
}

func TestBootUpgradeSeedsTheObservationsPackageIntoAnOlderRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)

	// --- binary N: the seed without the observations package.
	before := shippedTree(t)
	if err := os.RemoveAll(filepath.Join(before, vocabulary.PackageObservations)); err != nil {
		t.Fatal(err)
	}
	svc1 := openTree(t, dsn, before)
	const name = "older.example.com"
	if _, err := svc1.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds1, err := svc1.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds1.KindByRef(ctx, kindObservation); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("binary N already declares %s: %v", kindObservation, err)
	}
	head := maxSeq(t, ds1)
	_ = svc1.Close()

	// --- binary N+1: the shipped seed, observations included.
	svc2 := openTree(t, dsn, shippedTree(t))
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, name)
	if err != nil {
		t.Fatalf("open on binary N+1: %v", err)
	}
	sawPackage := false
	for _, ch := range changesSince(t, ds2, head) {
		if ch.Actor != substrate.ActorSystem {
			t.Fatalf("upgrade entry %d is attributed to %q", ch.Seq, ch.Actor)
		}
		if ch.RecordID == vocabulary.PackageObservations {
			sawPackage = true
		}
	}
	if !sawPackage {
		t.Fatal("the upgrade entries do not include the observations package row")
	}
	if v := packageRowVersion(t, ds2, vocabulary.PackageObservations); v < 1 {
		t.Fatalf("the upgraded observations package row is at version %d", v)
	}
	requireObservationsSeeded(t, ds2)
}
