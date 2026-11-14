package engine_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The fixture's record ids.
var (
	flourID = vocabulary.DerivedMintedID("publish", "flour")
	breadID = vocabulary.DerivedMintedID("publish", "bread")
	noteID  = vocabulary.DerivedMintedID("publish", "note")
	cakeID  = vocabulary.DerivedMintedID("publish", "cake")
	hookID  = vocabulary.DerivedMintedID("publish", "trigger")
)

// recipesDeclarations are ~/recipes: an ingredient, a recipe that
// references one, and a function that writes a recipe.
func recipesDeclarations() []map[string]any {
	const recipes = "~/recipes"
	return []map[string]any{
		vocabulary.PackageManifest(recipes, 1),
		vocabulary.KindManifest(recipes, map[string]any{"singular": "ingredient"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
		vocabulary.KindManifest(recipes, map[string]any{"singular": "recipe"}, map[string]any{
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"main": map[string]any{"type": "reference", "kind": recipes + "/ingredient"},
			},
		}),
		vocabulary.FunctionManifest(recipes, "add", map[string]any{
			"description": "writes one recipe",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{recipes + "/recipe"}},
			"source": `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "~/recipes/recipe", "id": input["args"]["id"],
                         "properties": {"name": input["args"]["name"]}}]}
`,
		}),
	}
}

// publishFixture declares ~/recipes (a recipe that references its
// ingredients, and a function that writes a recipe) and ~/notes (a note that
// references a recipe), with records of each.
func publishFixture(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	const recipes, notes = "~/recipes", "~/notes"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, append(recipesDeclarations(),
		vocabulary.PackageManifest(notes, 1),
		vocabulary.KindManifest(notes, map[string]any{"singular": "note"}, map[string]any{
			"properties": map[string]any{
				"text":   map[string]any{"type": "string"},
				"recipe": map[string]any{"type": "reference", "kind": recipes + "/recipe"},
			},
		}),
	)); err != nil {
		t.Fatalf("declare the packages: %v", err)
	}
	for _, in := range []substrate.PutInput{
		{Kind: recipes + "/ingredient", ID: flourID, Properties: map[string]any{"name": "flour"}},
		{Kind: recipes + "/recipe", ID: breadID, Properties: map[string]any{
			"name": "bread", "main": map[string]any{"ref": recipes + "/ingredient/" + flourID},
		}},
		{Kind: notes + "/note", ID: noteID, Properties: map[string]any{
			"text": "bake on sunday", "recipe": map[string]any{"ref": recipes + "/recipe/" + breadID},
		}},
	} {
		if _, err := ds.Put(ctx, substrate.ActorPerson, in); err != nil {
			t.Fatalf("put %s/%s: %v", in.Kind, in.ID, err)
		}
	}
	// A trigger that watches one of the package's kinds and calls its
	// function.
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: vocabulary.TriggerKind, ID: hookID, Properties: map[string]any{
		"enabled":  true,
		"source":   map[string]any{"record": map[string]any{"kinds": []any{recipes + "/ingredient"}}},
		"callable": map[string]any{"ref": vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocFunction), recipes+"/add")},
	}}); err != nil {
		t.Fatalf("put the trigger: %v", err)
	}
}

// Publishing moves a package of the repository's own to the authority it
// is published under (decision record 0289): its declarations, every record
// of its kinds, every reference at one (from its own records and from
// another package's), the other package's pin at it, and the kinds its
// function writes, in one transaction with its first release. Nothing of it
// stays in `~`, and the function writes the published kinds.
func TestPublishingMovesAPackageOutOfTheLocalNamespace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	name := testdb.Repository(t)
	if _, err := svc.Register(ctx, substrate.RegisterInput{Identity: name, Repository: name, Password: testPassword, SkipTOTP: true}); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	publishFixture(t, ds)
	const pub = "pub.example.com"
	// A record view of the package's recipe, and a change policy over its
	// ingredients and the other package's notes, both naming the package
	// before the publish.
	const recordView, policy = "substrate.reamde.dev/console/recordview", "recipe-changes"
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: recordView, ID: "recipe", Properties: map[string]any{
		"appliesTo": map[string]any{"ref": vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocKind), "~/recipes/recipe")},
		"name":      "Recipe",
		"sections":  []any{map[string]any{"properties": map[string]any{"list": []any{map[string]any{"property": "name", "label": "Name"}}}}},
	}}); err != nil {
		t.Fatalf("put the record view: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: vocabulary.KindRecordPatchPolicy, ID: policy, Properties: map[string]any{
		"selector": map[string]any{"kinds": []any{"~/recipes/ingredient", "~/notes/note"}},
		"action":   "gate",
	}}); err != nil {
		t.Fatalf("put the change policy: %v", err)
	}
	app, _, err := ds.MintToken(ctx, substrate.MintTokenInput{
		Label: "recipes app", ClientID: "https://app.example/client.json",
		Scopes: []string{"read:~/recipes/*", "read:implements(~/recipes/searchable)", "write:~/notes/note"},
	})
	if err != nil {
		t.Fatalf("mint an app's token: %v", err)
	}

	plan, err := ds.PlanPublish(ctx, substrate.ActorPerson, substrate.PublishInput{Package: "~/recipes", Authority: pub})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Published != pub+"/recipes" || plan.Records != 2 || plan.References != 1 || len(plan.Blockers) != 0 ||
		len(plan.Packages) != 1 || plan.Packages[0] != "~/notes" {
		t.Fatalf("the plan: %+v; want two records carried, one note repointed, ~/notes following, nothing blocking", plan)
	}

	done, err := ds.Publish(ctx, substrate.ActorPerson, substrate.PublishInput{Package: "~/recipes", Authority: pub})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if done.Release == nil || done.Release.Version != 1 || done.Release.Package != pub+"/recipes" || len(done.Release.SHA256) != 64 {
		t.Fatalf("the publish answered %+v, want release 1 of %s/recipes", done, pub)
	}

	bread, err := ds.Get(ctx, pub+"/recipes/recipe", breadID)
	if err != nil {
		t.Fatalf("the recipe under the published name: %v", err)
	}
	if ref, _ := bread.Properties["main"].(map[string]any); ref["ref"] != pub+"/recipes/ingredient/"+flourID {
		t.Errorf("the recipe's reference: %v, want it at the published ingredient", bread.Properties["main"])
	}
	note, err := ds.Get(ctx, "~/notes/note", noteID)
	if err != nil {
		t.Fatal(err)
	}
	if ref, _ := note.Properties["recipe"].(map[string]any); ref["ref"] != pub+"/recipes/recipe/"+breadID {
		t.Errorf("the note's reference: %v, want it at the published recipe", note.Properties["recipe"])
	}
	if _, err := ds.Get(ctx, "~/recipes/recipe", breadID); !errors.Is(err, substrate.ErrNotFound) && !errors.Is(err, substrate.ErrValidation) {
		t.Errorf("~/recipes/recipe still answers: %v", err)
	}
	if _, _, err := ds.CallFunction(ctx, substrate.ActorPerson, pub+"/recipes/add", map[string]any{"id": cakeID, "name": "cake"}); err != nil {
		t.Fatalf("call the published function: %v", err)
	}
	if _, err := ds.Get(ctx, pub+"/recipes/recipe", cakeID); err != nil {
		t.Errorf("the published function did not write the published kind: %v", err)
	}
	hook, err := ds.Get(ctx, vocabulary.TriggerKind, hookID)
	if err != nil {
		t.Fatal(err)
	}
	if got := hook.Properties["callable"].(map[string]any)["ref"]; got != vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocFunction), pub+"/recipes/add") {
		t.Errorf("the trigger calls %v", got)
	}
	if got := hook.Properties["source"].(map[string]any)["record"].(map[string]any)["kinds"]; !reflect.DeepEqual(got, []any{pub + "/recipes/ingredient"}) {
		t.Errorf("the trigger watches %v", got)
	}
	token, err := ds.Get(ctx, "substrate.reamde.dev/core/token", app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := token.Properties["scopes"]; !reflect.DeepEqual(got, []any{"read:" + pub + "/recipes/*", "read:implements(" + pub + "/recipes/searchable)", "write:~/notes/note"}) {
		t.Errorf("the app's scopes after the publish are %v", got)
	}
	view, err := ds.Get(ctx, recordView, "recipe")
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Properties["appliesTo"].(map[string]any)["ref"]; got != vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocKind), pub+"/recipes/recipe") {
		t.Errorf("the record view applies to %v, want the published recipe", got)
	}
	gate, err := ds.Get(ctx, vocabulary.KindRecordPatchPolicy, policy)
	if err != nil {
		t.Fatal(err)
	}
	if got := gate.Properties["selector"].(map[string]any)["kinds"]; !reflect.DeepEqual(got, []any{pub + "/recipes/ingredient", "~/notes/note"}) {
		t.Errorf("the change policy selects %v, want the published ingredient and ~/notes/note", got)
	}
	// Over every live record, the record view and the change policy among
	// them.
	assertNothingLocalOf(t, dsn, ds.Repository().ID, "~/recipes/")

	releases, err := ds.Releases(ctx, pub)
	if err != nil || len(releases) != 1 || releases[0] != *done.Release {
		t.Fatalf("the releases: %+v (%v), want the first", releases, err)
	}
	closure, err := ds.ReleaseClosure(ctx, pub+"/recipes", 1)
	if err != nil || len(closure) == 0 {
		t.Fatalf("the closure: %v", err)
	}
}

// assertNothingLocalOf fails on any live record of a kind under prefix, any
// declaration naming it, and any reference at it. An actor record the
// publish disabled, the hand the package had in `~`, still names it: it is
// the record of that hand, so the old writes still link something (decision
// 0270).
func assertNothingLocalOf(t *testing.T, dsn, repository, prefix string) {
	t.Helper()
	db, err := engine.OpenScopedDB(dsn, repository, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for what, q := range map[string]string{
		"a live record of its kinds": `SELECT count(*) FROM records WHERE deleted_at IS NULL AND starts_with(kind, $1)`,
		"a live declaration of it":   `SELECT count(*) FROM records WHERE deleted_at IS NULL AND starts_with(id, $1)`,
		"a reference at it":          `SELECT count(*) FROM refs WHERE starts_with(dst_kind, $1)`,
		"a live record spelling it": `SELECT count(*) FROM records WHERE deleted_at IS NULL AND strpos(props::text, $1) > 0
			AND NOT (kind = 'substrate.reamde.dev/core/actor' AND props->>'disabled' = 'true')`,
	} {
		var n int
		if err := db.QueryRowContext(context.Background(), q, prefix).Scan(&n); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if n != 0 {
			t.Errorf("%d rows hold %s, %s", n, what, prefix)
		}
	}
}

// newPublishingRepository registers a repository under name and declares the
// publish fixture in it.
func newPublishingRepository(t *testing.T, name string) (substrate.Dataset, string) {
	t.Helper()
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.Register(ctx, substrate.RegisterInput{Identity: name, Repository: name, Password: testPassword, SkipTOTP: true}); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	publishFixture(t, ds)
	return ds, dsn
}

// A package that is a bundle, with a trigger that calls its function, is not
// blocked by the trigger: the publish repoints the trigger before its guard
// counts, and the preview says so rather than refusing what the publish does.
func TestPublishingABundledPackageWithATriggerIsNotBlocked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := newPublishingRepository(t, testdb.Repository(t))
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, append(recipesDeclarations(),
		vocabulary.BundleManifest("~/recipes", map[string]any{
			"description": "recipes and what they need",
			"installs":    []any{"~/recipes/ingredient", "~/recipes/recipe", "~/recipes/add"},
		}),
	)); err != nil {
		t.Fatalf("make the package a bundle: %v", err)
	}
	in := substrate.PublishInput{Package: "~/recipes", Authority: "pub.example.com"}
	plan, err := ds.PlanPublish(ctx, substrate.ActorPerson, in)
	if err != nil || len(plan.Blockers) != 0 {
		t.Fatalf("the plan: %+v (%v), want nothing blocking", plan.Blockers, err)
	}
	if _, err := ds.Publish(ctx, substrate.ActorPerson, in); err != nil {
		t.Fatalf("publish: %v", err)
	}
	hook, err := ds.Get(ctx, vocabulary.TriggerKind, hookID)
	if err != nil {
		t.Fatal(err)
	}
	if got := hook.Properties["callable"].(map[string]any)["ref"]; got != vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocFunction), "pub.example.com/recipes/add") {
		t.Errorf("the trigger calls %v", got)
	}
}

// Two packages published in turn under the repository's own name: once the
// first is published there, the second's reference at it names a published
// package, not another local one, so the second publishes too and its
// reference stays at the first.
func TestTwoPackagesPublishedInTurnUnderTheRepositorysOwnName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	name := testdb.Repository(t)
	ds, dsn := newPublishingRepository(t, name)
	for _, pkg := range []string{"~/recipes", "~/notes"} {
		if _, err := ds.Publish(ctx, substrate.ActorPerson, substrate.PublishInput{Package: pkg, Authority: name}); err != nil {
			t.Fatalf("publish %s under %s: %v", pkg, name, err)
		}
	}
	note, err := ds.Get(ctx, name+"/notes/note", noteID)
	if err != nil {
		t.Fatalf("the note under the published name: %v", err)
	}
	if got := note.Properties["recipe"].(map[string]any)["ref"]; got != name+"/recipes/recipe/"+breadID {
		t.Errorf("the note's reference reads %v", got)
	}
	assertNothingLocalOf(t, dsn, ds.Repository().ID, "~/notes/")
	assertNothingLocalOf(t, dsn, ds.Repository().ID, "~/recipes/")
}
