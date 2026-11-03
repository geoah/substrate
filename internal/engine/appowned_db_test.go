package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A package an app declared, as Substrate Workouts did under decision 0203.
const (
	apPackage = swAuthority + "/training"
	apSet     = apPackage + "/set"
	apKindRef = "substrate.reamde.dev/core/kind"
)

var apApp = substrate.AppActor("workouts.example.org")

// apDocs is the app's package: its header, its bundle and one kind, with the
// properties named.
func apDocs(props ...string) []map[string]any {
	declared := map[string]any{}
	for _, p := range props {
		declared[p] = map[string]any{"type": "string"}
	}
	return []map[string]any{
		vocabulary.PackageManifest(apPackage, 0),
		vocabulary.BundleManifest(apPackage, map[string]any{"installs": []any{apSet}}),
		vocabulary.KindManifest(apPackage, map[string]any{"singular": "set"}, map[string]any{"properties": declared}),
	}
}

// installAppPackage has the app declare its package. The row names the app
// as its declarer and nothing more: no client id is stamped beside it any
// more (decision 0214).
func installAppPackage(t *testing.T) substrate.Dataset {
	t.Helper()
	_, ds := newCoreDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), apApp, apDocs("reps")); err != nil {
		t.Fatalf("the app declares its package: %v", err)
	}
	row := mustGet(t, ds, kindPackageRef, apPackage)
	if got := row.Properties["declaredBy"]; got != string(apApp) {
		t.Fatalf("declaredBy = %v, want %s", got, apApp)
	}
	if client, ok := row.Properties["declaredByClient"]; ok {
		t.Fatalf("declaredByClient was stamped: %v", client)
	}
	return ds
}

// AN APP'S PACKAGE IS NOT LOCKED TO IT (decision 0214, retiring 0205's
// lock): the owner's doors and the app itself write its declarations alike.
// THE OWNER STILL TAKES IT OVER: a deliberate, confirmed act by the owner's
// own hand rewrites the stamps so the row names them, and the app it was
// taken from. It refuses a confirmation that does not repeat the package,
// every hand that is not the owner's, a package no app declared and one
// nobody holds, and moves nothing but the stamps.
func TestTheOwnerTakesOverAnAppsPackage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installAppPackage(t)
	set := mustPut(t, ds, substrate.ActorConsole, substrate.PutInput{Kind: apSet, Properties: map[string]any{"reps": "8"}})
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorConsole, apDocs("reps", "weight")); err != nil {
		t.Fatalf("the owner edits a package an app declared: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, apApp, apDocs("reps", "weight", "rpe")); err != nil {
		t.Fatalf("the app edits the package it declared: %v", err)
	}
	kind := mustGet(t, ds, apKindRef, apSet)

	if _, err := ds.TakeOverPackage(ctx, substrate.ActorConsole, apPackage, ""); !errors.Is(err, substrate.ErrValidation) {
		t.Errorf("a takeover without its confirmation: %v", err)
	}
	if _, err := ds.TakeOverPackage(ctx, substrate.ActorConsole, apPackage, swAuthority+"/other"); !errors.Is(err, substrate.ErrValidation) {
		t.Errorf("a takeover confirming another package: %v", err)
	}
	for _, hand := range []substrate.Actor{apApp, substrate.AppActor("notes.example.org"), "agent:" + swAuthority + ":coach:planner", substrate.ActorSystem, "bundle:" + swAuthority + ":training", ""} {
		if _, err := ds.TakeOverPackage(ctx, hand, apPackage, apPackage); !errors.Is(err, substrate.ErrForbidden) {
			t.Errorf("%q took the package over: %v", hand, err)
		}
	}
	if _, err := ds.TakeOverPackage(ctx, substrate.ActorConsole, swAuthority+"/missing", swAuthority+"/missing"); !errors.Is(err, substrate.ErrNotFound) {
		t.Errorf("a takeover of a package nobody holds: %v", err)
	}
	if _, err := ds.TakeOverPackage(ctx, substrate.ActorConsole, "substrate.reamde.dev/core", "substrate.reamde.dev/core"); !errors.Is(err, substrate.ErrConflict) {
		t.Errorf("a takeover of a package no app declared: %v", err)
	}

	row, err := ds.TakeOverPackage(ctx, substrate.ActorConsole, apPackage, apPackage)
	if err != nil {
		t.Fatalf("the owner takes the package over: %v", err)
	}
	if row.Properties["declaredBy"] != string(substrate.ActorConsole) {
		t.Fatalf("declaredBy after the takeover = %v", row.Properties["declaredBy"])
	}
	// The answer says who it was taken from: the app's actor, since no
	// client id was stamped.
	if row.Properties["takenOverFrom"] != string(apApp) {
		t.Fatalf("takenOverFrom = %v, want %s", row.Properties["takenOverFrom"], apApp)
	}
	if _, ok := row.Properties["declaredByClient"]; ok {
		t.Fatalf("declaredByClient after the takeover: %v", row.Properties["declaredByClient"])
	}
	if got := mustGet(t, ds, apKindRef, apSet); got.Version != kind.Version {
		t.Fatalf("the takeover moved the kind: version %d, was %d", got.Version, kind.Version)
	}
	if got := mustGet(t, ds, apSet, set.ID); got.Properties["reps"] != "8" || got.DeletedAt != nil {
		t.Fatalf("the takeover moved a record: %+v", got)
	}
	if _, err := ds.TakeOverPackage(ctx, substrate.ActorConsole, apPackage, apPackage); !errors.Is(err, substrate.ErrConflict) {
		t.Errorf("a second takeover: %v", err)
	}
	// The package is written by every hand after the takeover as it was
	// before it.
	if _, err := ds.ApplyVocabularyDocuments(ctx, apApp, apDocs("reps", "weight", "rpe", "notes")); err != nil {
		t.Fatalf("the app edits the package after the takeover: %v", err)
	}
}
