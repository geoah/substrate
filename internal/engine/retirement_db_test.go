package engine_test

// Retired names (decision 0055) on the doors that admit a declaration at run
// time: the apply verb and InstallBundleClosure. A retirement is written by the
// author, stored on the declaration row, carried into every later document of
// its package or kind, survives a rebuild from the changelog, and refuses a
// declaration of the name on each door. The boot door is
// upgrade_guard_db_test.go's (TestBootUpgradeRefusesARetiredName).

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const retPackage = "retire.example.com/shop"

// packageDocRetiring renders a package header that retires the given kind
// names; none renders the plain header.
func packageDocRetiring(pkg string, kinds ...string) map[string]any {
	doc := vocabulary.PackageManifest(pkg, 0)
	if len(kinds) == 0 {
		return doc
	}
	names := make([]any, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, k)
	}
	doc["data"].(map[string]any)["retired"] = map[string]any{"kinds": names}
	return doc
}

// retWidgetProps is the widget before anything retires: a string, an enum with
// two values and a machine with three states.
func retWidgetProps() map[string]any {
	return map[string]any{
		"size":  map[string]any{"type": "string"},
		"level": map[string]any{"type": "enum", "values": []any{"low", "high"}},
		"phase": map[string]any{
			"type": "state", "states": []any{"open", "done", "archived"}, "initial": "open",
			"transitions": []any{
				map[string]any{"from": "open", "to": "done"},
				map[string]any{"from": "done", "to": "archived"},
			},
		},
	}
}

// retWidgetNarrowed is the widget with `size`, `low` and `archived` removed:
// the shape the retirement below spends them from.
func retWidgetNarrowed() map[string]any {
	return map[string]any{
		"level": map[string]any{"type": "enum", "values": []any{"high"}},
		"phase": map[string]any{
			"type": "state", "states": []any{"open", "done"}, "initial": "open",
			"transitions": []any{map[string]any{"from": "open", "to": "done"}},
		},
	}
}

// retWidgetRetired is the block that spends what retWidgetNarrowed removed.
func retWidgetRetired() map[string]any {
	return map[string]any{
		"properties": []any{"size"},
		"values":     map[string]any{"level": []any{"low"}},
		"states":     map[string]any{"phase": []any{"archived"}},
	}
}

func retKindDoc(pkg, singular string, props, retired map[string]any) map[string]any {
	data := map[string]any{"properties": props}
	if retired != nil {
		data["retired"] = retired
	}
	return vocabulary.KindManifest(pkg, map[string]any{"singular": singular}, data)
}

// wantRetiredRefusal asserts a refused declaration that names the retirement.
func wantRetiredRefusal(t *testing.T, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("declaring a retired name must refuse")
	}
	for _, f := range fragments {
		if !strings.Contains(err.Error(), f) {
			t.Fatalf("the refusal must name %q, got: %v", f, err)
		}
	}
}

func TestRetirementRefusesOnEveryDoor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	apply := func(docs ...map[string]any) error {
		_, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs)
		return err
	}
	// The base closure's header carries an EMPTY list, which is no retirement:
	// the row stores no block, and re-applying the same header is a no-op that
	// moves no version.
	emptyList := packageDocRetiring(retPackage)
	emptyList["data"].(map[string]any)["retired"] = map[string]any{"kinds": []any{}}
	if err := apply(emptyList,
		retKindDoc(retPackage, "widget", retWidgetProps(), nil),
		retKindDoc(retPackage, "gadget", map[string]any{"label": map[string]any{"type": "string"}}, nil),
		retKindDoc(retPackage, "trinket", map[string]any{"label": map[string]any{"type": "string"}}, nil),
	); err != nil {
		t.Fatalf("install the base closure: %v", err)
	}
	header := mustGet(t, ds, "substrate.reamde.dev/core/package", retPackage)
	if _, stored := header.Properties["retired"]; stored {
		t.Fatalf("an empty list stored a block: %v", header.Properties["retired"])
	}
	if err := apply(emptyList); err != nil {
		t.Fatalf("re-apply the header with an empty list: %v", err)
	}
	if again := mustGet(t, ds, "substrate.reamde.dev/core/package", retPackage); again.Properties["version"] != header.Properties["version"] {
		t.Fatalf("re-applying an empty list moved the package version %v -> %v", header.Properties["version"], again.Properties["version"])
	}

	// Retiring a name the package still declares is one document contradicting
	// itself, refused at load; so is a kind retiring what it declares.
	wantRetiredRefusal(t, apply(packageDocRetiring(retPackage, "gadget")),
		"kind retire.example.com/shop/gadget is retired and declared", "a retired name is never declared again")
	wantRetiredRefusal(t, apply(retKindDoc(retPackage, "widget", retWidgetProps(), retWidgetRetired())),
		`property "size" is retired and declared`, `"low" is retired and declared`, `"archived" is retired and declared`)

	// The retire: the kind is dropped and its name spent in the same breath;
	// the widget loses a property, a value and a state and spends each.
	if _, err := ds.Delete(ctx, owner, "substrate.reamde.dev/core/kind", retPackage+"/gadget", substrate.DeleteInput{}); err != nil {
		t.Fatalf("drop gadget: %v", err)
	}
	if err := apply(packageDocRetiring(retPackage, "gadget"),
		retKindDoc(retPackage, "widget", retWidgetNarrowed(), retWidgetRetired())); err != nil {
		t.Fatalf("retire: %v", err)
	}

	assertStored := func(when string) {
		t.Helper()
		pkg := mustGet(t, ds, "substrate.reamde.dev/core/package", retPackage)
		if got := pkg.Properties["retired"]; !reflect.DeepEqual(got, map[string]any{"kinds": []any{"gadget"}}) {
			t.Fatalf("%s: package retired = %v", when, got)
		}
		widget := mustGet(t, ds, "substrate.reamde.dev/core/kind", retPackage+"/widget")
		if got := widget.Properties["retired"]; !reflect.DeepEqual(got, retWidgetRetired()) {
			t.Fatalf("%s: widget retired = %v", when, got)
		}
	}
	assertStored("after the retire")

	// A change elsewhere in the package travels without its header: deleting
	// another kind moves the package version from the STORED row, retirement
	// included, rather than from a bare header that would read as un-retiring.
	if _, err := ds.Delete(ctx, owner, "substrate.reamde.dev/core/kind", retPackage+"/trinket", substrate.DeleteInput{}); err != nil {
		t.Fatalf("deleting another kind of a package with retirements must admit: %v", err)
	}
	assertStored("after deleting another kind without the header")

	// The marker is a declaration key like any other: it rides the changelog,
	// so a rebuild from the segment files carries it.
	if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Username(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	assertStored("after the rebuild")

	// The apply door: each retired name is refused, and the stored declaration
	// does not move.
	refusedOnApply := func() {
		t.Helper()
		wantRetiredRefusal(t, apply(retKindDoc(retPackage, "gadget", map[string]any{"label": map[string]any{"type": "string"}}, nil)),
			"kind retire.example.com/shop/gadget is retired and declared")
		props := retWidgetNarrowed()
		props["size"] = map[string]any{"type": "int"}
		wantRetiredRefusal(t, apply(retKindDoc(retPackage, "widget", props, nil)), `property "size" is retired and declared`)
		props = retWidgetNarrowed()
		props["level"] = map[string]any{"type": "enum", "values": []any{"high", "low"}}
		wantRetiredRefusal(t, apply(retKindDoc(retPackage, "widget", props, nil)), `"low" is retired and declared`)
		props = retWidgetNarrowed()
		props["phase"] = retWidgetProps()["phase"]
		wantRetiredRefusal(t, apply(retKindDoc(retPackage, "widget", props, nil)), `"archived" is retired and declared`)
		if _, err := ds.KindByRef(ctx, retPackage+"/gadget"); err == nil {
			t.Fatal("a refused declaration landed the retired kind")
		}
	}
	refusedOnApply()

	// Omitting the block does not lift it: a plain re-apply of both documents
	// admits, the rows keep every entry, and the refusals still hold.
	if err := apply(packageDocRetiring(retPackage), retKindDoc(retPackage, "widget", retWidgetNarrowed(), nil)); err != nil {
		t.Fatalf("a re-apply without the block must admit: %v", err)
	}
	assertStored("after a re-apply that omitted the block")
	refusedOnApply()

	// A retired value or state spends the NAME, not the property's shape:
	// `level` may stop being an enum and `phase` a machine, the entries lie
	// dormant, and they bite again the moment either declares the name.
	reshaped := map[string]any{
		"level": map[string]any{"type": "string"},
		"phase": map[string]any{"type": "string"},
	}
	if err := apply(retKindDoc(retPackage, "widget", reshaped, nil)); err != nil {
		t.Fatalf("a retired value must not pin the property's datatype: %v", err)
	}
	if err := apply(retKindDoc(retPackage, "widget", retWidgetNarrowed(), nil)); err != nil {
		t.Fatalf("the property may become an enum and a machine again: %v", err)
	}
	assertStored("after the properties changed shape and back")
	refusedOnApply()

	// The install door: a published closure retires a kind, a later closure
	// that declares it again is refused, and one that omits the block keeps
	// it. The same admission the catalog's install and import ride.
	inst, ok := ds.(substrate.BundleInstaller)
	if !ok {
		t.Fatal("dataset does not implement the closure-install seam")
	}
	const pkg = "acme.example.com/mirror"
	actor := substrate.BundleActor(vocabulary.SplitPackageRef(pkg))
	closure := func(header map[string]any, members ...map[string]any) []map[string]any {
		installs := make([]any, 0, len(members))
		for _, m := range members {
			installs = append(installs, m["metadata"].(map[string]any)["id"])
		}
		return append([]map[string]any{
			header,
			vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
			vocabulary.BundleManifest(pkg, map[string]any{"description": "mirror kinds", "installs": installs}),
		}, members...)
	}
	install := func(docs []map[string]any) error {
		_, err := inst.InstallBundleClosure(ctx, actor, docs, nil, substrate.BundleInstall{Published: true})
		return err
	}
	label := map[string]any{"label": map[string]any{"type": "string"}}
	thing := retKindDoc(pkg, "thing", label, nil)
	other := retKindDoc(pkg, "other", label, nil)
	if err := install(closure(packageDocRetiring(pkg), thing, other)); err != nil {
		t.Fatalf("install the provider: %v", err)
	}
	if err := install(closure(packageDocRetiring(pkg, "other"), thing)); err != nil {
		t.Fatalf("an upgrade that drops and retires a kind must admit: %v", err)
	}
	wantRetiredRefusal(t, install(closure(packageDocRetiring(pkg, "other"), thing, other)),
		"kind acme.example.com/mirror/other is retired and declared")
	wantRetiredRefusal(t, install(closure(packageDocRetiring(pkg), thing, other)),
		"kind acme.example.com/mirror/other is retired and declared")
	// The upgrade preview reports the same line the install refuses on,
	// without writing anything.
	planner, ok := ds.(substrate.BundleUpgradePlanner)
	if !ok {
		t.Fatal("dataset does not plan bundle upgrades")
	}
	plan, err := planner.PlanBundleUpgrade(ctx, closure(packageDocRetiring(pkg), thing, other))
	if err != nil {
		t.Fatalf("plan the upgrade: %v", err)
	}
	if !strings.Contains(strings.Join(plan.Blockers, "; "), "kind acme.example.com/mirror/other is retired and declared") {
		t.Fatalf("the preview did not report the retirement: %+v", plan.Blockers)
	}
	if err := install(closure(packageDocRetiring(pkg), thing)); err != nil {
		t.Fatalf("an upgrade that omits the block must admit: %v", err)
	}
	mirror := mustGet(t, ds, "substrate.reamde.dev/core/package", pkg)
	if got := mirror.Properties["retired"]; !reflect.DeepEqual(got, map[string]any{"kinds": []any{"other"}}) {
		t.Fatalf("the install door lifted the retirement: %v", got)
	}
	if _, err := ds.KindByRef(ctx, pkg+"/other"); err == nil {
		t.Fatal("a refused install landed the retired kind")
	}
}
