package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The package an app declares under its `declare:` scope (decision 0205).
const (
	apPackage = swAuthority + "/training"
	apSet     = apPackage + "/set"
	apKindRef = "substrate.reamde.dev/core/kind"
)

var (
	apApp   = substrate.AppActor("workouts.example.org")
	apOther = substrate.AppActor("notes.example.org")
)

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

// installAppPackage has the app declare its package, as Substrate Workouts
// does at sign-in.
func installAppPackage(t *testing.T) substrate.Dataset {
	t.Helper()
	_, ds := newCoreDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), apApp, apDocs("reps")); err != nil {
		t.Fatalf("the app declares its package: %v", err)
	}
	if got := mustGet(t, ds, kindPackageRef, apPackage).Properties["declaredBy"]; got != string(apApp) {
		t.Fatalf("declaredBy = %v, want %s", got, apApp)
	}
	return ds
}

// wantManagedByApp holds a refusal to the 403 that names the owning app.
func wantManagedByApp(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, substrate.ErrForbidden) || !strings.Contains(err.Error(), string(apApp)) {
		t.Errorf("%s: %v, want a 403 naming %s", what, err, apApp)
	}
}

// AN APP'S PACKAGE CHANGES ONLY THROUGH THAT APP: every door the owner has,
// an agent, a function and another app are refused an apply, a put, a patch
// and a delete of its package, bundle and kind documents, and an uninstall;
// the app itself still upgrades it.
func TestAnAppsPackageChangesOnlyThroughThatApp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installAppPackage(t)

	hands := []substrate.Actor{
		substrate.ActorConsole, substrate.ActorCLI, substrate.ActorAPI,
		"agent:" + swAuthority + ":coach:planner",
		"function:" + swAuthority + ":coach:rollup",
		apOther,
	}
	for _, hand := range hands {
		_, err := ds.ApplyVocabularyDocuments(ctx, hand, apDocs("reps", "weight"))
		wantManagedByApp(t, string(hand)+" applies the package", err)
		_, err = ds.ApplyVocabularyDocuments(ctx, hand, apDocs("reps")[2:])
		wantManagedByApp(t, string(hand)+" applies one kind", err)
		_, err = ds.PlanVocabularyApply(ctx, hand, apDocs("reps", "weight"))
		wantManagedByApp(t, string(hand)+" previews an apply", err)
	}
	kind := mustGet(t, ds, apKindRef, apSet)
	props := map[string]any{}
	for k, v := range kind.Properties {
		props[k] = v
	}
	props["description"] = "edited by hand"
	_, err := ds.Put(ctx, substrate.ActorConsole, substrate.PutInput{Kind: apKindRef, ID: apSet, Properties: props})
	wantManagedByApp(t, "the console puts the kind", err)
	_, err = ds.Patch(ctx, substrate.ActorCLI, apKindRef, apSet, substrate.PatchInput{Properties: map[string]any{"description": "edited"}})
	wantManagedByApp(t, "substratectl patches the kind", err)
	_, err = ds.Patch(ctx, substrate.ActorConsole, kindPackageRef, apPackage, substrate.PatchInput{Properties: map[string]any{"description": "mine"}})
	wantManagedByApp(t, "the console patches the package", err)
	_, err = ds.Delete(ctx, substrate.ActorAPI, apKindRef, apSet, substrate.DeleteInput{})
	wantManagedByApp(t, "the owner deletes the kind", err)
	_, err = ds.Delete(ctx, substrate.ActorAPI, kindPackageRef, apPackage, substrate.DeleteInput{})
	wantManagedByApp(t, "the owner deletes the package", err)
	_, err = ds.Delete(ctx, substrate.ActorAPI, "substrate.reamde.dev/core/bundle", apPackage, substrate.DeleteInput{})
	wantManagedByApp(t, "the owner deletes the bundle", err)
	wantManagedByApp(t, "the owner uninstalls the bundle", ds.UninstallBundle(ctx, apPackage))

	if got := mustGet(t, ds, apKindRef, apSet); got.Version != kind.Version {
		t.Fatalf("a refused write moved the kind: version %d, was %d", got.Version, kind.Version)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, apApp, apDocs("reps", "weight")); err != nil {
		t.Fatalf("the app upgrades its own package: %v", err)
	}
	if got := mustGet(t, ds, kindPackageRef, apPackage).Properties["declaredBy"]; got != string(apApp) {
		t.Fatalf("declaredBy after the app's upgrade = %v", got)
	}
}

// THE RECORDS STAY THE OWNER'S: only the declarations are locked. The owner
// creates, edits and deletes records of the app's kinds as of any other.
func TestAnAppsKindsRecordsStayTheOwners(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installAppPackage(t)
	set := mustPut(t, ds, substrate.ActorConsole, substrate.PutInput{Kind: apSet, Properties: map[string]any{"reps": "8"}})
	if _, err := ds.Patch(ctx, substrate.ActorCLI, apSet, set.ID, substrate.PatchInput{Properties: map[string]any{"reps": "10"}}); err != nil {
		t.Fatalf("the owner edits a record of the app's kind: %v", err)
	}
	if got := mustGet(t, ds, apSet, set.ID).Properties["reps"]; got != "10" {
		t.Fatalf("reps = %v", got)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, apSet, set.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("the owner deletes a record of the app's kind: %v", err)
	}
}

// AN APP WRITES ONLY THE PACKAGE IT DECLARED: a package the owner declared is
// the owner's, whatever scope an app holds for it.
func TestAnAppDoesNotWriteTheOwnersPackage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorConsole, apDocs("reps")); err != nil {
		t.Fatalf("the owner declares the package: %v", err)
	}
	_, err := ds.ApplyVocabularyDocuments(ctx, apApp, apDocs("reps", "weight"))
	if !errors.Is(err, substrate.ErrForbidden) || !strings.Contains(err.Error(), "owner's package") {
		t.Fatalf("an app wrote the owner's package: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorCLI, apDocs("reps", "weight")); err != nil {
		t.Fatalf("the owner edits their own package: %v", err)
	}
}

// THE OWNER TAKES OVER: a deliberate, confirmed act by the owner's own hand
// makes the package theirs. It refuses a confirmation that does not repeat
// the package, every hand that is not the owner's, a package no app manages
// and one nobody holds; it moves nothing but the declarer, so the records
// and the declarations stay as they were; and afterwards the owner writes
// the package and the app no longer does.
func TestTheOwnerTakesOverAnAppsPackage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installAppPackage(t)
	set := mustPut(t, ds, substrate.ActorConsole, substrate.PutInput{Kind: apSet, Properties: map[string]any{"reps": "8"}})
	kind := mustGet(t, ds, apKindRef, apSet)

	if _, err := ds.TakeOverPackage(ctx, substrate.ActorConsole, apPackage, ""); !errors.Is(err, substrate.ErrValidation) {
		t.Errorf("a takeover without its confirmation: %v", err)
	}
	if _, err := ds.TakeOverPackage(ctx, substrate.ActorConsole, apPackage, swAuthority+"/other"); !errors.Is(err, substrate.ErrValidation) {
		t.Errorf("a takeover confirming another package: %v", err)
	}
	for _, hand := range []substrate.Actor{apApp, apOther, "agent:" + swAuthority + ":coach:planner", substrate.ActorSystem, "bundle:" + swAuthority + ":training", ""} {
		if _, err := ds.TakeOverPackage(ctx, hand, apPackage, apPackage); !errors.Is(err, substrate.ErrForbidden) {
			t.Errorf("%q took the package over: %v", hand, err)
		}
	}
	if _, err := ds.TakeOverPackage(ctx, substrate.ActorConsole, swAuthority+"/missing", swAuthority+"/missing"); !errors.Is(err, substrate.ErrNotFound) {
		t.Errorf("a takeover of a package nobody holds: %v", err)
	}
	if _, err := ds.TakeOverPackage(ctx, substrate.ActorConsole, "substrate.reamde.dev/core", "substrate.reamde.dev/core"); !errors.Is(err, substrate.ErrConflict) {
		t.Errorf("a takeover of a package no app manages: %v", err)
	}

	row, err := ds.TakeOverPackage(ctx, substrate.ActorConsole, apPackage, apPackage)
	if err != nil {
		t.Fatalf("the owner takes the package over: %v", err)
	}
	if row.Properties["declaredBy"] != string(substrate.ActorConsole) {
		t.Fatalf("declaredBy after the takeover = %v", row.Properties["declaredBy"])
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

	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorCLI, apDocs("reps", "weight")); err != nil {
		t.Fatalf("the owner edits the package after taking it over: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, apApp, apDocs("reps", "weight", "rpe")); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("the app wrote a package the owner took over: %v", err)
	}
}
