package engine_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// publishingService registers a repository on a service of its own and
// declares the publish fixture in it.
func publishingService(t *testing.T, opts ...engine.Option) (substrate.Service, substrate.Dataset, string, string) {
	t.Helper()
	ctx := context.Background()
	svc, dsn := newService(t, opts...)
	name := testdb.Repository(t)
	if _, err := svc.Register(ctx, substrate.RegisterInput{Identity: name, Repository: name, Password: testPassword, SkipTOTP: true}); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	publishFixture(t, ds)
	return svc, ds, dsn, ds.Repository().ID
}

// scalar runs one counting query on the repository's own handle.
func scalar(t *testing.T, dsn, repository, q string, args ...any) int {
	t.Helper()
	db, err := engine.OpenScopedDB(dsn, repository, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// runsOf counts the runs of a trigger, and of those the ones about a record
// whose id is among ids when ids are given.
func runsOf(t *testing.T, dsn, repository, trigger string, ids ...string) int {
	t.Helper()
	q := `SELECT count(*) FROM records WHERE kind = 'substrate.reamde.dev/core/triggerrun' AND strpos((props->'trigger')::text, $1) > 0`
	args := []any{trigger}
	if len(ids) > 0 {
		q += ` AND EXISTS (SELECT 1 FROM unnest($2::text[]) AS i WHERE strpos(coalesce(props->>'record', ''), i) > 0)`
		args = append(args, ids)
	}
	return scalar(t, dsn, repository, q, args...)
}

const noopSource = "def main(input, host):\n    return {}\n"

// The lines a publish writes for a carried record are the same record to
// every trigger, a source of `*` included: the fixture's own trigger, over
// the package's ingredients, runs nothing for the records the publish
// carries, and once for one written after it. A carried record keeps the ids
// it was merged from, and a rebuild gives them back (decision record 0298).
func TestAPublishSetsOffNoTriggerForWhatItCarries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn, repository := publishingService(t)
	const pub = "pub.example.com"
	loser := vocabulary.DerivedMintedID("publish", "flour again")
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "~/recipes/ingredient", ID: loser, Properties: map[string]any{"name": "flour"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: "~/recipes/ingredient", Winner: flourID, Loser: loser}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// A trigger over every kind beside the fixture's, calling a function
	// that writes nothing.
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.FunctionManifest("~/notes", "noop", map[string]any{"description": "does nothing", "runtime": vocabulary.RuntimePython, "source": noopSource}),
	}); err != nil {
		t.Fatal(err)
	}
	everything := vocabulary.DerivedMintedID("publish", "everything")
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: vocabulary.TriggerKind, ID: everything, Properties: map[string]any{
		"enabled":  true,
		"source":   map[string]any{"record": map[string]any{"kinds": []any{"*"}}},
		"callable": map[string]any{"ref": vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocFunction), "~/notes/noop")},
	}}); err != nil {
		t.Fatalf("put the trigger over every kind: %v", err)
	}
	// Everything before the publish delivered, so what follows is its own.
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	hookRuns := runsOf(t, dsn, repository, hookID)
	if _, err := ds.Publish(ctx, substrate.ActorPerson, substrate.PublishInput{Package: "~/recipes", Authority: pub}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	if got := runsOf(t, dsn, repository, hookID); got != hookRuns {
		t.Errorf("the fixture's trigger ran %d times for the records the publish carried, want none", got-hookRuns)
	}
	if got := runsOf(t, dsn, repository, everything, flourID, breadID, noteID); got != 0 {
		t.Errorf("the trigger over every kind ran %d times for the carried and repointed records, want none", got)
	}
	sugar := vocabulary.DerivedMintedID("publish", "sugar")
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: pub + "/recipes/ingredient", ID: sugar, Properties: map[string]any{"name": "sugar"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	if got := runsOf(t, dsn, repository, hookID); got != hookRuns+1 {
		t.Errorf("the fixture's trigger ran %d times for an ingredient written after the publish, want once", got-hookRuns)
	}

	merged := func(when string) {
		t.Helper()
		got, err := ds.Get(ctx, pub+"/recipes/ingredient", loser)
		if err != nil || got.ID != flourID {
			t.Fatalf("%s, the merged-away id under the published kind answers %v (%v), want the ingredient it was merged into", when, got, err)
		}
	}
	merged("after the publish")
	if _, err := svc.(rebuilder).RebuildRepository(ctx, repository); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	ds, err := svc.Dataset(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	merged("after a rebuild")
}

// Every name a publish's table answers MOVES at moves: a change policy's
// agents and functions with its kinds, so no gate lifts; a pattern over
// every package of `~` with the published package's glob beside it, in a
// token's scopes, a trigger's source and a policy's selector; a console
// preference; a collection view's filter; and a change request's proposal,
// whose envelope is frozen, through the fold. The request is accepted after
// the publish and writes the published kind.
func TestAPublishMovesEveryNameItsTableAnswers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, dsn, repository := publishingService(t)
	const pub = "pub.example.com"
	put := func(in substrate.PutInput) {
		t.Helper()
		if _, err := ds.Put(ctx, substrate.ActorPerson, in); err != nil {
			t.Fatalf("put %s/%s: %v", in.Kind, in.ID, err)
		}
	}
	put(substrate.PutInput{Kind: vocabulary.KindRecordPatchPolicy, ID: "gate-add", Properties: map[string]any{
		"selector": map[string]any{"kinds": []any{"~/recipes/recipe"}, "functions": []any{"~/recipes/add"}},
		"action":   "gate",
	}})
	put(substrate.PutInput{Kind: vocabulary.KindRecordPatchPolicy, ID: "refuse-chef", Properties: map[string]any{
		"selector": map[string]any{"kinds": []any{"~/*"}, "agents": []any{"~/recipes/chef"}},
		"action":   "refuse",
	}})
	put(substrate.PutInput{Kind: vocabulary.TriggerKind, ID: vocabulary.DerivedMintedID("publish", "everything local"), Properties: map[string]any{
		"enabled":  false,
		"source":   map[string]any{"record": map[string]any{"kinds": []any{"~/*"}}},
		"callable": map[string]any{"ref": vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocFunction), "~/recipes/add")},
	}})
	put(substrate.PutInput{Kind: vocabulary.CoreKind("consolepreference"), ID: "console", Properties: map[string]any{
		"favorites": []any{"~/recipes/recipe"}, "collapsed": []any{"~/recipes"},
	}})
	put(substrate.PutInput{Kind: vocabulary.PackageConsole + "/collectionview", ID: "flour-recipes", Properties: map[string]any{
		"appliesTo": map[string]any{"ref": vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocKind), "~/recipes/recipe")},
		"name":      "With flour",
		"where":     map[string]any{"main": map[string]any{"eq": map[string]any{"ref": "~/recipes/ingredient/" + flourID}}},
	}})
	app, _, err := ds.MintToken(ctx, substrate.MintTokenInput{
		Label: "all of mine", ClientID: "https://app.example/client.json", Scopes: []string{"read:~/*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The policy gates the function's write into a change request: a frozen
	// envelope naming the function and the kind.
	if _, _, err := ds.CallFunction(ctx, substrate.ActorPerson, "~/recipes/add", map[string]any{"id": cakeID, "name": "cake"}); err != nil {
		t.Fatalf("call the gated function: %v", err)
	}
	request := ""
	for _, r := range listAll(t, ds, vocabulary.KindRecordPatchRequest) {
		if r.Properties["targetId"] == cakeID {
			request = r.ID
		}
	}
	if request == "" {
		t.Fatal("the gated write made no change request")
	}

	in := substrate.PublishInput{Package: "~/recipes", Authority: pub}
	plan, err := ds.PlanPublish(ctx, substrate.ActorPerson, in)
	if err != nil || len(plan.Blockers) != 0 {
		t.Fatalf("the plan: %+v (%v), want nothing blocking", plan.Blockers, err)
	}
	if _, err := ds.Publish(ctx, substrate.ActorPerson, in); err != nil {
		t.Fatalf("publish: %v", err)
	}

	props := func(kind, id string) map[string]any {
		t.Helper()
		r, err := ds.Get(ctx, kind, id)
		if err != nil {
			t.Fatalf("get %s/%s: %v", kind, id, err)
		}
		return r.Properties
	}
	for _, c := range []struct {
		what string
		got  any
		want any
	}{
		{"the gate's functions", props(vocabulary.KindRecordPatchPolicy, "gate-add")["selector"].(map[string]any)["functions"], []any{pub + "/recipes/add"}},
		{"the gate's kinds", props(vocabulary.KindRecordPatchPolicy, "gate-add")["selector"].(map[string]any)["kinds"], []any{pub + "/recipes/recipe"}},
		{"the refusal's agents", props(vocabulary.KindRecordPatchPolicy, "refuse-chef")["selector"].(map[string]any)["agents"], []any{pub + "/recipes/chef"}},
		{"the refusal's kinds", props(vocabulary.KindRecordPatchPolicy, "refuse-chef")["selector"].(map[string]any)["kinds"], []any{"~/*", pub + "/recipes/*"}},
		{"the trigger's kinds", props(vocabulary.TriggerKind, vocabulary.DerivedMintedID("publish", "everything local"))["source"].(map[string]any)["record"].(map[string]any)["kinds"], []any{"~/*", pub + "/recipes/*"}},
		{"the token's scopes", props("substrate.reamde.dev/core/token", app.ID)["scopes"], []any{"read:~/*", "read:" + pub + "/recipes/*"}},
		{"the favorites", props(vocabulary.CoreKind("consolepreference"), "console")["favorites"], []any{pub + "/recipes/recipe"}},
		{"the collapsed", props(vocabulary.CoreKind("consolepreference"), "console")["collapsed"], []any{pub + "/recipes"}},
		{"the view's filter", props(vocabulary.PackageConsole+"/collectionview", "flour-recipes")["where"], map[string]any{"main": map[string]any{"eq": map[string]any{"ref": pub + "/recipes/ingredient/" + flourID}}}},
		{"the request's kind", props(vocabulary.KindRecordPatchRequest, request)["targetKind"], pub + "/recipes/recipe"},
		{"the request's function", props(vocabulary.KindRecordPatchRequest, request)["function"], map[string]any{"ref": vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocFunction), pub+"/recipes/add")}},
	} {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s: %v, want %v", c.what, c.got, c.want)
		}
	}
	assertNothingLocalOf(t, dsn, repository, "~/recipes/")

	reviewed, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindRecordPatchRequest, request, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &reviewed.Version,
	}); err != nil {
		t.Fatalf("accept the request: %v", err)
	}
	if _, err := ds.Get(ctx, pub+"/recipes/recipe", cakeID); err != nil {
		t.Errorf("the accepted request did not write the published kind: %v", err)
	}
}

// listAll is every live record of kind.
func listAll(t *testing.T, ds substrate.Dataset, kind string) []*substrate.Record {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{kind}}})
	if err != nil {
		t.Fatalf("list %s: %v", kind, err)
	}
	return page.Records
}

// A setting of the package stops the publish: the preview lists it, the
// confirm is refused naming it, and nothing changes.
func TestASettingOfThePackageStopsThePublish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, dsn, repository := publishingService(t)
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: vocabulary.CoreKind("setting"), ID: "~/recipes/servings", Properties: map[string]any{
		"value": 4,
	}}); err != nil {
		t.Skipf("this build writes no setting by hand: %v", err)
	}
	in := substrate.PublishInput{Package: "~/recipes", Authority: "pub.example.com"}
	plan, err := ds.PlanPublish(ctx, substrate.ActorPerson, in)
	if err != nil || len(plan.Blockers) == 0 || !strings.Contains(strings.Join(plan.Blockers, "\n"), "~/recipes/servings") {
		t.Fatalf("the plan: %v (%v), want the setting named", plan.Blockers, err)
	}
	if _, err := ds.Publish(ctx, substrate.ActorPerson, in); !errors.Is(err, substrate.ErrGuard) || !strings.Contains(err.Error(), "~/recipes/servings") {
		t.Fatalf("publish = %v, want it refused naming the setting", err)
	}
	if n := scalar(t, dsn, repository, `SELECT count(*) FROM records WHERE kind = 'substrate.reamde.dev/core/release'`); n != 0 {
		t.Errorf("a refused publish wrote %d releases", n)
	}
}

// The preview is the publish rolled back: one over the work ceiling lists
// the ceiling and writes nothing, and the confirm is refused the same way.
func TestAPreviewIsThePublishRolledBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, dsn, repository := publishingService(t, engine.WithConversionCeiling(2))
	head := func() int {
		return scalar(t, dsn, repository, `SELECT coalesce(max(seq), 0) FROM changelog`)
	}
	before := head()
	in := substrate.PublishInput{Package: "~/recipes", Authority: "pub.example.com"}
	plan, err := ds.PlanPublish(ctx, substrate.ActorPerson, in)
	if err != nil || len(plan.Blockers) == 0 {
		t.Fatalf("the plan: %+v (%v), want the ceiling among its blockers", plan.Blockers, err)
	}
	if after := head(); after != before {
		t.Errorf("the preview moved the changelog from %d to %d", before, after)
	}
	if _, err := ds.Publish(ctx, substrate.ActorPerson, in); err == nil {
		t.Fatal("the confirm went through what its preview refused")
	} else if !strings.Contains(strings.Join(plan.Blockers, "\n"), firstLine(err.Error())) && !strings.Contains(err.Error(), plan.Blockers[0]) {
		t.Errorf("the confirm was refused for %q, the preview for %v", err, plan.Blockers)
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "; ")
	if _, rest, ok := strings.Cut(line, ": "); ok {
		return rest
	}
	return line
}

// A release is admitted by the installer's own check, on the closure it
// will serve: one that names another local package is refused at its
// preview and at its confirm, and so is a published agent that names a
// provider rather than the input it takes.
func TestAReleaseIsAdmittedAsAnotherServerInstallsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _, _ := publishingService(t)
	const pub = "pub.example.com"
	if _, err := ds.Publish(ctx, substrate.ActorPerson, substrate.PublishInput{Package: "~/recipes", Authority: pub}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// The package whole, as its owner declares it again under the published
	// name (a bundle's batch replaces the package), with one more
	// declaration each time.
	recipes := pub + "/recipes"
	apply := func(extra ...map[string]any) {
		t.Helper()
		docs := []map[string]any{
			vocabulary.PackageManifest(recipes, 0),
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
				"source":      noopSource,
			}),
		}
		docs = append(docs, extra...)
		var installs []any
		for _, d := range docs[1:] {
			installs = append(installs, d["metadata"].(map[string]any)["id"])
		}
		docs = append(docs, vocabulary.BundleManifest(recipes, map[string]any{"installs": installs}))
		if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, docs); err != nil {
			t.Fatalf("declare the package again: %v", err)
		}
	}
	refused := func(why string) {
		t.Helper()
		plan, err := ds.ReleasePackage(ctx, substrate.ActorPerson, substrate.ReleaseInput{Package: recipes})
		if err != nil || !strings.Contains(strings.Join(plan.Blockers, "\n"), why) {
			t.Fatalf("the release's plan: %+v (%v), want it refused naming %q", plan, err, why)
		}
		if _, err := ds.ReleasePackage(ctx, substrate.ActorPerson, substrate.ReleaseInput{Package: recipes, Confirm: true}); err == nil || !strings.Contains(err.Error(), why) {
			t.Fatalf("the release = %v, want it refused naming %q", err, why)
		}
	}
	// An agent that spends the owner's provider by name.
	apply(vocabulary.AgentManifest(recipes, "chef", map[string]any{
		"description": "cooks", "provider": "anthropic", "model": "claude-x", "prompt": "You cook.",
	}))
	refused("providerInput")
	// A kind that names one of the repository's own packages.
	apply(vocabulary.KindManifest(recipes, map[string]any{"singular": "menu"}, map[string]any{
		"properties": map[string]any{"note": map[string]any{"type": "reference", "kind": "~/notes/note"}},
	}))
	refused("~/notes/note")
	releases, err := ds.Releases(ctx, pub)
	if err != nil || len(releases) != 1 {
		t.Fatalf("the releases after a refused one: %v (%v), want the first alone", releases, err)
	}
}

// A run that started under the package's hand in `~` and writes after the
// publish is refused: its record stays disabled, so nothing writes as a hand
// whose code is gone.
func TestAHandWhoseCodeWasPublishedWritesNothingUnderItsOldName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, dsn, repository := publishingService(t)
	// The function writes once before the publish, so its hand has a record.
	if _, _, err := ds.CallFunction(ctx, substrate.ActorPerson, "~/recipes/add", map[string]any{"id": cakeID, "name": "cake"}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if _, err := ds.Publish(ctx, substrate.ActorPerson, substrate.PublishInput{Package: "~/recipes", Authority: "pub.example.com"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	hand := substrate.Actor("function:~:recipes:add")
	_, err := ds.Put(ctx, hand, substrate.PutInput{Kind: "~/notes/note", Properties: map[string]any{"text": "late"}})
	if !errors.Is(err, substrate.ErrForbidden) || !strings.Contains(err.Error(), "no longer declared") {
		t.Fatalf("a write by %s after the publish = %v, want it refused", hand, err)
	}
	if n := scalar(t, dsn, repository, `SELECT count(*) FROM records WHERE kind = 'substrate.reamde.dev/core/actor' AND props->>'name' = '~/recipes/add' AND props->>'disabled' = 'true'`); n != 1 {
		t.Errorf("the hand's record: %d disabled, want it to stay disabled", n)
	}
}

// smallPackage declares the local package pkg, one kind and one record of
// it, for a test that publishes many packages in turn.
func smallPackage(t *testing.T, ds substrate.Dataset, pkg string) {
	t.Helper()
	ctx := context.Background()
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest("~/"+pkg, 1),
		vocabulary.KindManifest("~/"+pkg, map[string]any{"singular": "thing"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatalf("declare ~/%s: %v", pkg, err)
	}
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "~/" + pkg + "/thing", ID: flourID, Properties: map[string]any{"name": pkg}}); err != nil {
		t.Fatal(err)
	}
}

// A rename to a name and a publish under the same name, started together,
// both land, in either order, and leave the name naming the published
// package: a publish under the repository's own name is decision 0289's
// case, a rename onto a name the repository publishes under is allowed for
// the same reason, and the fold reads a name the repository holds a
// published package under as that package's, whichever landed first. Each
// round then renames away from the name it published under, which keeps
// what it published.
func TestARenameRacingAPublishUnderItsDestinationLandsBoth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	label := testdb.RepositoryLabel(t)
	name := func(s string) string { return label + "-" + s + ".example.com" }
	ada := name("ada")
	if _, err := svc.Register(ctx, substrate.RegisterInput{Identity: ada, Repository: ada, Password: testPassword, SkipTOTP: true}); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	id := ds.Repository().ID
	current := ada
	for i := range 4 {
		to, pkg := name(fmt.Sprintf("race%d", i)), fmt.Sprintf("pkg%c", 'a'+i)
		smallPackage(t, ds, pkg)
		var wg sync.WaitGroup
		var renameErr, publishErr error
		begin := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-begin
			_, renameErr = svc.RenameRepository(ctx, substrate.LoginInput{Identity: ada, Repository: current, Password: testPassword}, to)
		}()
		go func() {
			defer wg.Done()
			<-begin
			_, publishErr = ds.Publish(ctx, substrate.ActorPerson, substrate.PublishInput{Package: "~/" + pkg, Authority: to})
		}()
		close(begin)
		wg.Wait()
		if renameErr != nil || publishErr != nil {
			t.Fatalf("round %d: the rename %v, the publish %v; want both", i, renameErr, publishErr)
		}
		current = to
		got, err := svc.Dataset(ctx, to)
		if err != nil || got.Repository().ID != id {
			t.Fatalf("round %d: %s resolves to %v (%v)", i, to, got, err)
		}
		if _, err := ds.Get(ctx, to+"/"+pkg+"/thing", flourID); err != nil {
			t.Fatalf("round %d: the published record: %v", i, err)
		}
		assertNothingLocalOf(t, dsn, id, "~/"+pkg+"/")
	}
	// Every package published in an earlier round is still there under its
	// name, though the repository answers to that name no more.
	for i := range 4 {
		if _, err := ds.Get(ctx, name(fmt.Sprintf("race%d", i))+"/"+fmt.Sprintf("pkg%c", 'a'+i)+"/thing", flourID); err != nil {
			t.Errorf("the package published in round %d: %v", i, err)
		}
	}
}

// A publish under a former name the repository still answers to is
// refused (the 90 days are for coming back to it, and a published name
// would stop it spelling `~` at once), and so is one under a name another
// repository here answers to; under a name the repository took back, it
// goes ahead.
func TestAPublishUnderAFormerOrAnotherRepositorysNameIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	label := testdb.RepositoryLabel(t)
	name := func(s string) string { return label + "-" + s + ".example.com" }
	ada, bob, later := name("ada"), name("bob"), name("later")
	for _, n := range []string{ada, bob} {
		if _, err := svc.Register(ctx, substrate.RegisterInput{Identity: n, Repository: n, Password: testPassword, SkipTOTP: true}); err != nil {
			t.Fatal(err)
		}
	}
	ds, err := svc.Dataset(ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	smallPackage(t, ds, "notes")
	if _, err := svc.RenameRepository(ctx, substrate.LoginInput{Identity: ada, Repository: ada, Password: testPassword}, later); err != nil {
		t.Fatalf("rename: %v", err)
	}
	for _, to := range []string{ada, bob} {
		for _, run := range []func(context.Context, substrate.Actor, substrate.PublishInput) (substrate.PublishPlan, error){ds.PlanPublish, ds.Publish} {
			plan, err := run(ctx, substrate.ActorPerson, substrate.PublishInput{Package: "~/notes", Authority: to})
			if err == nil && len(plan.Blockers) == 0 {
				t.Fatalf("a publish under %s went ahead", to)
			}
			if err != nil && !errors.Is(err, substrate.ErrConflict) {
				t.Fatalf("a publish under %s = %v, want a conflict", to, err)
			}
		}
	}
	if _, err := svc.RenameRepository(ctx, substrate.LoginInput{Identity: ada, Repository: later, Password: testPassword}, ada); err != nil {
		t.Fatalf("rename back: %v", err)
	}
	if _, err := ds.Publish(ctx, substrate.ActorPerson, substrate.PublishInput{Package: "~/notes", Authority: ada}); err != nil {
		t.Fatalf("a publish under the name the repository took back: %v", err)
	}
}

// planNames is whether a plan's blockers name every one of want.
func planNames(plan substrate.PublishPlan, want ...string) bool {
	joined := strings.Join(plan.Blockers, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			return false
		}
	}
	return len(plan.Blockers) > 0
}

// What a publish leaves as history but is read again while its record is
// open stops it: an approval still to run, whose arguments the function is
// handed, and a parked delivery of a line of the package's kinds, which a
// retry replays under the kind it names. Each is named in the preview and
// refuses the confirm; decided, or forgotten, the publish goes ahead. A
// trigger's arguments are data, left as written and listed in the plan.
func TestAnOpenCallOnThePackageStopsThePublish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _, _ := publishingService(t)
	const pub = "pub.example.com"
	in := substrate.PublishInput{Package: "~/recipes", Authority: pub}
	approval := vocabulary.DerivedMintedID("publish", "approval")
	if err := engine.LandApproval(ctx, ds, approval, map[string]any{
		"thread":    map[string]any{"ref": vocabulary.RecordPath(vocabulary.PackageLLM+"/thread", vocabulary.DerivedMintedID("publish", "thread"))},
		"agent":     map[string]any{"ref": vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocAgent), "substrate.reamde.dev/assistant/moss")},
		"function":  map[string]any{"ref": vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocFunction), "~/recipes/add")},
		"tool":      "add",
		"arguments": map[string]any{"kind": "~/recipes/recipe", "id": cakeID},
	}); err != nil {
		t.Fatalf("land the approval: %v", err)
	}
	// A function that fails, on the package's ingredients, and a trigger
	// whose arguments name the package.
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.FunctionManifest("~/notes", "boom", map[string]any{
			"description": "fails", "runtime": vocabulary.RuntimePython,
			"source": "def main(input, host):\n    raise RuntimeError('boom')\n",
		}),
		vocabulary.FunctionManifest("~/notes", "sweep", map[string]any{
			"description": "sweeps into a kind", "runtime": vocabulary.RuntimePython,
			"arguments": []any{map[string]any{"name": "into", "type": "string"}}, "source": noopSource,
		}),
	}); err != nil {
		t.Fatal(err)
	}
	boom := vocabulary.DerivedMintedID("publish", "boom")
	argued := vocabulary.DerivedMintedID("publish", "argued")
	for id, props := range map[string]map[string]any{
		boom: {
			"enabled": true, "source": map[string]any{"record": map[string]any{"kinds": []any{"~/recipes/ingredient"}}},
			"callable": map[string]any{"ref": vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocFunction), "~/notes/boom")},
		},
		argued: {
			"enabled": false, "source": map[string]any{"schedule": map[string]any{"recurrence": "FREQ=DAILY", "timezone": "UTC"}},
			"callable":  map[string]any{"ref": vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocFunction), "~/notes/sweep")},
			"arguments": map[string]any{"into": "~/recipes/recipe"},
		},
	} {
		if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: vocabulary.TriggerKind, ID: id, Properties: props}); err != nil {
			t.Fatalf("put trigger %s: %v", id, err)
		}
	}
	sugar := vocabulary.DerivedMintedID("publish", "sugar")
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "~/recipes/ingredient", ID: sugar, Properties: map[string]any{"name": "sugar"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	parked, err := ds.TriggerFailures(ctx, boom)
	if err != nil || len(parked) == 0 {
		t.Fatalf("the failing trigger parked %v (%v), want a delivery", parked, err)
	}

	plan, err := ds.PlanPublish(ctx, substrate.ActorPerson, in)
	if err != nil || !planNames(plan, vocabulary.KindLLMApproval+"/"+approval, "trigger "+boom+" holds a parked delivery") {
		t.Fatalf("the plan: %+v (%v), want the open approval and the parked delivery named", plan.Blockers, err)
	}
	if _, err := ds.Publish(ctx, substrate.ActorPerson, in); !errors.Is(err, substrate.ErrGuard) {
		t.Fatalf("publish = %v, want it refused", err)
	}

	// Decided and forgotten, the publish goes ahead; the trigger's arguments
	// stay as written, listed in the plan.
	if _, err := ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindLLMApproval, approval, substrate.PatchInput{
		Properties: map[string]any{"state": "denied"},
	}); err != nil {
		t.Fatalf("deny the approval: %v", err)
	}
	for _, f := range parked {
		if err := ds.ForgetTriggerFailure(ctx, boom, f.ID); err != nil {
			t.Fatalf("forget: %v", err)
		}
	}
	plan, err = ds.PlanPublish(ctx, substrate.ActorPerson, in)
	if err != nil || len(plan.Blockers) != 0 {
		t.Fatalf("the plan once decided: %v (%v), want nothing blocking", plan.Blockers, err)
	}
	if want := vocabulary.TriggerKind + "/" + argued; !slices.Contains(plan.Texts, want) {
		t.Errorf("the plan lists %v, want the trigger whose arguments name the package, %s", plan.Texts, want)
	}
	if _, err := ds.Publish(ctx, substrate.ActorPerson, in); err != nil {
		t.Fatalf("publish once decided: %v", err)
	}
	trig, err := ds.Get(ctx, vocabulary.TriggerKind, argued)
	if err != nil || !reflect.DeepEqual(trig.Properties["arguments"], map[string]any{"into": "~/recipes/recipe"}) {
		t.Errorf("the trigger's arguments: %v (%v), want them as written", trig.Properties["arguments"], err)
	}
}

// A check that runs only at the transaction's end refuses the preview as it
// refuses the confirm: the preview rolls back where the confirm commits.
func TestAnEndOfTransactionRefusalShowsInThePreview(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const pub = "pub.example.com"
	_, ds, _, _ := publishingService(t, engine.WithTestEndOfTransactionCheck(func(declares func(string) bool) error {
		if declares(pub + "/recipes") {
			return &substrate.ValidationError{Problems: []string{"refused at the transaction's end"}}
		}
		return nil
	}))
	in := substrate.PublishInput{Package: "~/recipes", Authority: pub}
	plan, err := ds.PlanPublish(ctx, substrate.ActorPerson, in)
	if err != nil || !planNames(plan, "refused at the transaction's end") {
		t.Fatalf("the plan: %+v (%v), want the end-of-transaction refusal among its blockers", plan.Blockers, err)
	}
	if _, err := ds.Publish(ctx, substrate.ActorPerson, in); err == nil || !strings.Contains(err.Error(), "refused at the transaction's end") {
		t.Fatalf("publish = %v, want the same refusal", err)
	}
}

// A release requires what an installing server receives: a package this
// repository publishes from its latest release, never from what is declared
// here since. A kind added to a published package and not released is
// unknown to an installer, so a package naming it is refused, and so is one
// whose quantity names a unit only this repository holds.
func TestAReleaseCompilesAgainstWhatAnInstallerReceives(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _, _ := publishingService(t)
	const pub, other = "pub.example.com", "other.example.com"
	if _, err := ds.Publish(ctx, substrate.ActorPerson, substrate.PublishInput{Package: "~/recipes", Authority: pub}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// A kind added to the published package, not released.
	recipes := pub + "/recipes"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.KindManifest(recipes, map[string]any{"singular": "extra"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Skipf("this build does not take one kind into a published bundle by itself: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.KindManifest("~/notes", map[string]any{"singular": "aside"}, map[string]any{
			"properties": map[string]any{"extra": map[string]any{"type": "reference", "kind": recipes + "/extra"}},
		}),
	}); err != nil {
		t.Fatal(err)
	}
	plan, err := ds.PlanPublish(ctx, substrate.ActorPerson, substrate.PublishInput{Package: "~/notes", Authority: other})
	if err != nil || !planNames(plan, recipes+"/extra") {
		t.Fatalf("the plan: %+v (%v), want the kind no release of %s holds refused", plan.Blockers, err, recipes)
	}
}

// A package of another publisher's that a release requires is read as the
// catalog serves it, never from this repository's installed copy, which may
// be older: with no catalog to ask the publish stops, naming it. And it is
// compiled as an installer applies it: a suggested mapping from a kind a
// fresh repository lacks is dropped, not refused.
func TestARequirementOfAnotherPublisherIsReadFromTheCatalog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _, _ := publishingService(t)
	const lib = "lib.example.com/spices"
	spice := vocabulary.KindManifest(lib, map[string]any{"singular": "spice"}, map[string]any{
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
	})
	installed := []map[string]any{
		vocabulary.PackageManifest(lib, 1),
		vocabulary.BundleManifest(lib, map[string]any{"description": "Spices", "installs": []any{lib + "/spice"}}),
		spice,
	}
	if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(vocabulary.SplitPackageRef(lib)), installed, nil, substrate.BundleInstall{Published: true}); err != nil {
		t.Fatalf("install %s: %v", lib, err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest("~/pantry", 1),
		vocabulary.KindManifest("~/pantry", map[string]any{"singular": "jar"}, map[string]any{
			"properties": map[string]any{"spice": map[string]any{"type": "reference", "kind": lib + "/spice"}},
		}),
	}); err != nil {
		t.Fatal(err)
	}
	in := substrate.PublishInput{Package: "~/pantry", Authority: "other.example.com"}
	plan, err := ds.PlanPublish(ctx, substrate.ActorPerson, in)
	if err != nil || !planNames(plan, lib) {
		t.Fatalf("the plan with no catalog: %+v (%v), want %s refused", plan.Blockers, err, lib)
	}
	served := []map[string]any{
		vocabulary.PackageManifest(lib, 1),
		vocabulary.BundleManifest(lib, map[string]any{"description": "Spices", "installs": []any{lib + "/spice", lib + "/fromnowhere"}}),
		spice,
		vocabulary.MappingManifest(lib, "fromnowhere", map[string]any{
			"from": "providers.example.com/nowhere/thing", "to": lib + "/spice",
			"map": map[string]any{"name": map[string]any{"path": "title"}},
		}),
	}
	asked := ""
	catalog := substrate.WithReleaseResolver(ctx, func(_ context.Context, id string, _ int64) ([]map[string]any, error) {
		asked = id
		return served, nil
	})
	plan, err = ds.PlanPublish(catalog, substrate.ActorPerson, in)
	if err != nil || len(plan.Blockers) > 0 || asked != lib {
		t.Fatalf("the plan with a catalog: %+v (%v), asked for %q, want nothing stopping it", plan.Blockers, err, asked)
	}
}

// A quantity the release declares in a unit only this repository holds is
// refused: an installer holds the seed's units alone.
func TestAReleaseNamesNoUnitOnlyThisRepositoryHolds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _, _ := publishingService(t)
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: vocabulary.KindUnit, ID: "pinch", Properties: map[string]any{"name": "pinch"}}); err != nil {
		t.Fatalf("make a unit: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, append(recipesDeclarations(),
		vocabulary.KindManifest("~/recipes", map[string]any{"singular": "spice"}, map[string]any{
			"properties": map[string]any{"amount": map[string]any{"type": "quantity", "unit": "pinch"}},
		}),
	)); err != nil {
		t.Fatalf("declare a quantity in the owner's unit: %v", err)
	}
	in := substrate.PublishInput{Package: "~/recipes", Authority: "pub.example.com"}
	plan, err := ds.PlanPublish(ctx, substrate.ActorPerson, in)
	if err != nil || !planNames(plan, "pinch") {
		t.Fatalf("the plan: %+v (%v), want the owner's unit refused", plan.Blockers, err)
	}
	if _, err := ds.Publish(ctx, substrate.ActorPerson, in); err == nil || !strings.Contains(err.Error(), "pinch") {
		t.Fatalf("publish = %v, want it refused naming the unit", err)
	}
}
