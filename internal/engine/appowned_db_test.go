package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
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
	// apClient is the app's full client id, which ownership is compared on,
	// and apCtx a request the app's own token made.
	apClient = "https://workouts.example.org/client.json"
	apCtx    = substrate.WithClientID(context.Background(), apClient)
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
	if _, err := ds.ApplyVocabularyDocuments(apCtx, apApp, apDocs("reps")); err != nil {
		t.Fatalf("the app declares its package: %v", err)
	}
	row := mustGet(t, ds, kindPackageRef, apPackage)
	if got := row.Properties["declaredBy"]; got != string(apApp) {
		t.Fatalf("declaredBy = %v, want %s", got, apApp)
	}
	if got := row.Properties["declaredByClient"]; got != apClient {
		t.Fatalf("declaredByClient = %v, want %s", got, apClient)
	}
	return ds
}

// wantManagedByApp holds a refusal to the 403 that names the owning app.
func wantManagedByApp(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, substrate.ErrForbidden) || !strings.Contains(err.Error(), apClient) {
		t.Errorf("%s: %v, want a 403 naming %s", what, err, apClient)
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
	if _, err := ds.ApplyVocabularyDocuments(apCtx, apApp, apDocs("reps", "weight")); err != nil {
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
	_, err := ds.ApplyVocabularyDocuments(apCtx, apApp, apDocs("reps", "weight"))
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
	// The answer says who it was taken from, and the app's client id leaves
	// the row with its ownership.
	if row.Properties["takenOverFrom"] != apClient {
		t.Fatalf("takenOverFrom = %v, want %s", row.Properties["takenOverFrom"], apClient)
	}
	if _, ok := row.Properties["declaredByClient"]; ok {
		t.Fatalf("declaredByClient survived the takeover: %v", row.Properties["declaredByClient"])
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
	if _, err := ds.ApplyVocabularyDocuments(apCtx, apApp, apDocs("reps", "weight", "rpe")); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("the app wrote a package the owner took over: %v", err)
	}
}

// OWNERSHIP IS THE CLIENT ID, NOT THE HOST: a second app served from the same
// host writes as the same `app:<host>` actor, and is still not the owner. So
// is a second loopback build. The app itself, under any token for its client
// id (a renewal), still upgrades its package.
func TestAnotherAppOnTheSameHostIsNotTheOwner(t *testing.T) {
	t.Parallel()
	ds := installAppPackage(t)
	for _, client := range []string{
		"https://workouts.example.org/notes/client.json",
		"https://workouts.example.org/client.json?build=local",
	} {
		_, err := ds.ApplyVocabularyDocuments(substrate.WithClientID(context.Background(), client), apApp, apDocs("reps", "weight"))
		if !errors.Is(err, substrate.ErrForbidden) || !strings.Contains(err.Error(), "another app, "+apClient) || !strings.Contains(err.Error(), client) {
			t.Errorf("%s, on the owner's host, wrote its package: %v", client, err)
		}
	}
	// An app actor with no client id at all is no request's, and is not the
	// owner of a package whose client id is stamped either.
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), apApp, apDocs("reps", "weight")); !errors.Is(err, substrate.ErrForbidden) {
		t.Errorf("an app actor without a client id wrote a stamped package: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(apCtx, apApp, apDocs("reps", "weight")); err != nil {
		t.Fatalf("the owning app, under a renewed token, upgrades its package: %v", err)
	}

	// Two loopback builds share `app:127.0.0.1:<port>` and are two owners.
	_, local := newCoreDataset(t)
	loopback := substrate.AppActor("127.0.0.1:8765")
	debug := substrate.WithClientID(context.Background(), "http://127.0.0.1:8765/workouts-debug/client.json")
	other := substrate.WithClientID(context.Background(), "http://127.0.0.1:8765/notes/client.json")
	if _, err := local.ApplyVocabularyDocuments(debug, loopback, apDocs("reps")); err != nil {
		t.Fatalf("a local build declares the package: %v", err)
	}
	if _, err := local.ApplyVocabularyDocuments(other, loopback, apDocs("reps", "weight")); !errors.Is(err, substrate.ErrForbidden) {
		t.Errorf("a second loopback app wrote the local build's package: %v", err)
	}
}

// A HOST-ONLY STAMP IS CLAIMED ONCE: a package an app declared before the
// client id was stamped carries only `app:<host>`. The first apply from a
// client id on that host is admitted and stamps its client id; from then on
// only that client id is the owner, and another app on the host is refused.
func TestAHostOnlyStampIsClaimedByTheFirstClientOnThatHost(t *testing.T) {
	t.Parallel()
	_, ds := newCoreDataset(t)
	// An engine caller with no client id writes as an older binary's app did:
	// the row carries the host alone.
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), apApp, apDocs("reps")); err != nil {
		t.Fatalf("the app declares its package, host only: %v", err)
	}
	row := mustGet(t, ds, kindPackageRef, apPackage)
	if _, ok := row.Properties["declaredByClient"]; ok || row.Properties["declaredBy"] != string(apApp) {
		t.Fatalf("a host-only stamp: %v", row.Properties)
	}
	// Another host is refused before and after the claim.
	if _, err := ds.ApplyVocabularyDocuments(substrate.WithClientID(context.Background(), "https://notes.example.org/client.json"), apOther, apDocs("reps", "weight")); !errors.Is(err, substrate.ErrForbidden) {
		t.Errorf("another host wrote a host-only package: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(apCtx, apApp, apDocs("reps", "weight")); err != nil {
		t.Fatalf("the first client on the host claims the package: %v", err)
	}
	if got := mustGet(t, ds, kindPackageRef, apPackage).Properties["declaredByClient"]; got != apClient {
		t.Fatalf("declaredByClient after the claim = %v, want %s", got, apClient)
	}
	_, err := ds.ApplyVocabularyDocuments(substrate.WithClientID(context.Background(), "https://workouts.example.org/notes/client.json"), apApp, apDocs("reps", "weight", "rpe"))
	if !errors.Is(err, substrate.ErrForbidden) || !strings.Contains(err.Error(), apClient) {
		t.Errorf("a second client on the host wrote a claimed package: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(apCtx, apApp, apDocs("reps", "weight", "rpe")); err != nil {
		t.Fatalf("the claiming client upgrades its package: %v", err)
	}
}

// AN APP'S PACKAGE KEEPS WHAT IT REQUIRES: the owner's uninstall of a package
// an app's bundle requires is refused, naming the app's package and the app,
// since neither the app nor the owner could mend the app's package after it.
// The refusal lands nothing, so a reopen admits the app's package as it was
// rather than quarantining it. Disabling the required bundle is a runtime
// switch and changes no declaration, so it is admitted. Once the owner takes
// the app's package over, the uninstall is theirs to make, and so is the
// quarantine that follows it.
func TestAnAppsPackageKeepsThePackageItRequires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	const dep = swAuthority + "/health"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorConsole, []map[string]any{
		vocabulary.PackageManifest(dep, 0),
		vocabulary.BundleManifest(dep, map[string]any{"installs": []any{dep + "/workout"}}),
		vocabulary.KindManifest(dep, map[string]any{"singular": "workout"}, map[string]any{}),
	}); err != nil {
		t.Fatalf("the owner installs the required package: %v", err)
	}
	docs := apDocs("reps")
	docs[1] = vocabulary.BundleManifest(apPackage, map[string]any{"installs": []any{apSet}, "requires": []any{dep}})
	if _, err := ds.ApplyVocabularyDocuments(apCtx, apApp, docs); err != nil {
		t.Fatalf("the app declares a package requiring %s: %v", dep, err)
	}

	err = ds.UninstallBundle(ctx, dep)
	if !errors.Is(err, substrate.ErrGuard) || !strings.Contains(err.Error(), apPackage+" requires "+dep) || !strings.Contains(err.Error(), apClient) {
		t.Fatalf("the owner uninstalled what the app's package requires: %v", err)
	}
	if err := ds.DisableBundle(ctx, dep); err != nil {
		t.Fatalf("the owner disables the required bundle: %v", err)
	}
	if err := ds.EnableBundle(ctx, dep); err != nil {
		t.Fatalf("the owner enables the required bundle again: %v", err)
	}
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	svc2 := reopenWith(t, dsn, root)
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if q := mustGet(t, ds2, kindPackageRef, apPackage).Properties["quarantined"]; q == true {
		t.Fatalf("the app's package is quarantined after a refused uninstall")
	}
	mustPut(t, ds2, substrate.ActorConsole, substrate.PutInput{Kind: apSet, Properties: map[string]any{"reps": "5"}})

	if _, err := ds2.TakeOverPackage(ctx, substrate.ActorConsole, apPackage, apPackage); err != nil {
		t.Fatalf("take the app's package over: %v", err)
	}
	// What the guard holds back: the owner's uninstall lands on a package
	// they keep, and the package requiring it is quarantined at the next
	// open, theirs to mend. An app could not have mended it.
	if err := ds2.UninstallBundle(ctx, dep); err != nil {
		t.Fatalf("the owner uninstalls what their own package requires: %v", err)
	}
	_ = svc2.Close()
	svc3 := reopenWith(t, dsn, root)
	ds3, err := svc3.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if row := mustGet(t, ds3, kindPackageRef, apPackage); row.Properties["quarantined"] != true {
		t.Fatalf("the package requiring an uninstalled one opened unquarantined: %v", row.Properties)
	}
}

// THE BUNDLE'S RUNTIME SWITCH IS THE OWNER'S, ITS DATA IS NOT: the owner
// disables and enables an app's bundle (the off switch a coach agent, or a
// person, relies on), and is refused a purge, which deletes every record of
// the app's kinds, until they take the package over.
func TestTheOwnerSwitchesAnAppsBundleButDoesNotPurgeIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installAppPackage(t)
	set := mustPut(t, ds, substrate.ActorConsole, substrate.PutInput{Kind: apSet, Properties: map[string]any{"reps": "8"}})

	if err := ds.DisableBundle(ctx, apPackage); err != nil {
		t.Fatalf("the owner disables the app's bundle: %v", err)
	}
	if err := ds.EnableBundle(ctx, apPackage); err != nil {
		t.Fatalf("the owner enables the app's bundle: %v", err)
	}
	if err := ds.DisableBundle(ctx, apPackage); err != nil {
		t.Fatalf("the owner disables the app's bundle again: %v", err)
	}
	_, err := ds.PurgeBundle(ctx, apPackage)
	wantManagedByApp(t, "the owner purges the app's bundle", err)
	if got := mustGet(t, ds, apSet, set.ID); got.DeletedAt != nil {
		t.Fatalf("a refused purge deleted a record")
	}
	// The disabled bundle is still the app's to upgrade.
	if _, err := ds.ApplyVocabularyDocuments(apCtx, apApp, apDocs("reps", "weight")); err != nil {
		t.Fatalf("the app upgrades its disabled bundle: %v", err)
	}

	if _, err := ds.TakeOverPackage(ctx, substrate.ActorConsole, apPackage, apPackage); err != nil {
		t.Fatalf("take the package over: %v", err)
	}
	if n, err := ds.PurgeBundle(ctx, apPackage); err != nil || n == 0 {
		t.Fatalf("the owner purges a package they took over: %d, %v", n, err)
	}
}
