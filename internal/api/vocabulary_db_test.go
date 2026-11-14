package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// vocabularyWidgets is the batch declaring <repository>/shop/widget with the
// given properties, which lands in the local namespace as ~/shop/widget.
func vocabularyWidgets(env *dbEnv, props map[string]any) []map[string]any {
	pkg := env.repository + "/shop"
	return []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "widget"}, map[string]any{"properties": props}),
	}
}

// vocabularyPost posts body to a vocabulary verb as the owner.
func vocabularyPost(t *testing.T, env *dbEnv, verb string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return env.do(t, http.MethodPost, "/api/v1/vocabulary/"+verb, env.token, body)
}

// vocabularyHead is the repository's changelog head.
func vocabularyHead(t *testing.T, env *dbEnv) substrate.ChangelogHead {
	t.Helper()
	head, err := env.ds.Head(t.Context())
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	return head
}

// vocabularyColorWidgets declares the widget kind with a name and a color,
// writes n widgets carrying a color, and answers the batch that drops the
// color: lossy over those widgets.
func vocabularyColorWidgets(t *testing.T, env *dbEnv, n int) []map[string]any {
	t.Helper()
	if _, err := env.ds.ApplyVocabularyDocuments(t.Context(), substrate.ActorPerson, vocabularyWidgets(env, map[string]any{
		"name":  map[string]any{"type": "string"},
		"color": map[string]any{"type": "string"},
	})); err != nil {
		t.Fatalf("declare the widget: %v", err)
	}
	for i := range n {
		env.put(t, env.repository+"/shop/widget", fmt.Sprintf("w%d", i+1), map[string]any{"name": "widget", "color": "red"})
	}
	return vocabularyWidgets(env, map[string]any{"name": map[string]any{"type": "string"}})
}

// The preview verb: the engine's plan for the documents, and nothing written.
// Dropping a color three widgets carry is one lossy null step, and a required
// size they lack is a guard line.
func TestSchemaPlanEndpoint(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	vocabularyColorWidgets(t, env, 3)
	docs := vocabularyWidgets(env, map[string]any{
		"name": map[string]any{"type": "string"},
		"size": map[string]any{"type": "string", "required": true},
	})
	head := vocabularyHead(t, env)

	rec := vocabularyPost(t, env, "plan", map[string]any{"documents": docs})
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[substrate.VocabularyPlan](t, rec)
	widget := "~/shop/widget"
	if !out.Lossy || out.Work != 3 || out.PlanHash == "" || out.ChangelogSeq != head.Seq || len(out.Steps) != 1 || len(out.Blockers) == 0 {
		t.Fatalf("plan response = %+v (head %d)", out, head.Seq)
	}
	if s := out.Steps[0]; s.Step != substrate.StepNull || s.Kind != widget || s.Property != "color" || s.Records != 3 || !s.Lossy {
		t.Fatalf("step = %+v", s)
	}
	if !strings.Contains(strings.Join(out.Blockers, "; "), "size") {
		t.Fatalf("blockers = %q, want the required size named", out.Blockers)
	}
	if got := vocabularyHead(t, env); got != head {
		t.Fatalf("the preview wrote: head %+v, was %+v", got, head)
	}
	if got := env.get(t, widget, "w1"); got.Properties["color"] != "red" {
		t.Fatalf("the preview touched a widget: %+v", got)
	}
}

// The apply body's `confirm` reaches the engine's confirmed verb: the lossy
// batch it previewed lands with it. Without one the bare verb runs, and its
// lossy refusal answers 403 under its own code, so a client knows to preview
// and confirm rather than migrate records; a confirmation for another plan is
// the engine's conflict.
func TestSchemaApplyCarriesTheConfirmation(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	docs := vocabularyColorWidgets(t, env, 2)
	widget := env.repository + "/shop/widget"

	rec := vocabularyPost(t, env, "apply", map[string]any{"documents": docs})
	wantErrorCode(t, rec, http.StatusForbidden, codeLossy)
	if got := env.get(t, widget, "w1"); got.Properties["color"] != "red" {
		t.Fatalf("a refused apply dropped the color: %+v", got)
	}

	rec = vocabularyPost(t, env, "plan", map[string]any{"documents": docs})
	wantStatus(t, rec, http.StatusOK)
	plan := decodeJSON[substrate.VocabularyPlan](t, rec)
	if !plan.Lossy || plan.PlanHash == "" {
		t.Fatalf("plan = %+v", plan)
	}
	rec = vocabularyPost(t, env, "apply", map[string]any{
		"documents": docs,
		"confirm":   map[string]any{"planHash": "0000", "changelogSeq": plan.ChangelogSeq},
	})
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)

	rec = vocabularyPost(t, env, "apply", map[string]any{
		"documents": docs,
		"confirm":   map[string]any{"planHash": plan.PlanHash, "changelogSeq": plan.ChangelogSeq},
	})
	wantStatus(t, rec, http.StatusOK)
	for _, id := range []string{"w1", "w2"} {
		if got := env.get(t, widget, id); got.Properties["color"] != nil {
			t.Fatalf("the confirmed drop left %s its color: %+v", id, got)
		}
	}
}

// A hand-rehomed closure names the package it was authored as (`origin`), and
// both verbs hand the claim to the engine (decision record 0070): the apply
// so the copy is stamped, the plan so its preview hashes as the apply will.
// Once the copy is edited, the plan under the claim discards the edit and the
// apply under the claim takes that plan's confirmation and no less. A bare
// apply claims nothing.
func TestSchemaApplyAndPlanCarryTheOrigin(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	const origin = "samples.substrate.reamde.dev/tasks"
	pkg := env.repository + "/tasks"
	copyOf := func(props map[string]any) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(pkg, 7),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "item"}, map[string]any{"properties": props}),
		}
	}
	docs := copyOf(map[string]any{"name": map[string]any{"type": "string"}})
	rec := vocabularyPost(t, env, "apply", map[string]any{"documents": docs, "origin": origin})
	wantStatus(t, rec, http.StatusOK)
	row := env.get(t, corePackage+"/package", pkg)
	if row == nil || row.Properties["origin"] != origin || fmt.Sprint(row.Properties["originVersion"]) != "7" {
		t.Fatalf("the apply stamped %+v, want origin %s at 7", row, origin)
	}

	// The owner edits the copy; the stamp stays and no longer matches.
	rec = vocabularyPost(t, env, "apply", map[string]any{"documents": copyOf(map[string]any{
		"name": map[string]any{"type": "string"}, "note": map[string]any{"type": "string"},
	})})
	wantStatus(t, rec, http.StatusOK)

	rec = vocabularyPost(t, env, "plan", map[string]any{"documents": docs, "origin": origin})
	wantStatus(t, rec, http.StatusOK)
	plan := decodeJSON[substrate.VocabularyPlan](t, rec)
	if !plan.DiscardsEdits || plan.PlanHash == "" {
		t.Fatalf("the plan under the claim = %+v, want it to discard the edit", plan)
	}
	rec = vocabularyPost(t, env, "plan", map[string]any{"documents": docs})
	wantStatus(t, rec, http.StatusOK)
	if bare := decodeJSON[substrate.VocabularyPlan](t, rec); bare.DiscardsEdits {
		t.Fatalf("a plan claiming nothing discards edits: %+v", bare)
	}
	rec = vocabularyPost(t, env, "apply", map[string]any{"documents": docs, "origin": origin})
	wantErrorCode(t, rec, http.StatusForbidden, codeLossy)
	rec = vocabularyPost(t, env, "apply", map[string]any{
		"documents": docs, "origin": origin,
		"confirm": map[string]any{"planHash": plan.PlanHash, "changelogSeq": plan.ChangelogSeq},
	})
	wantStatus(t, rec, http.StatusOK)

	other := env.repository + "/notes"
	rec = vocabularyPost(t, env, "apply", map[string]any{"documents": []map[string]any{
		vocabulary.PackageManifest(other, 0),
		vocabulary.KindManifest(other, map[string]any{"singular": "note"}, map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
	}})
	wantStatus(t, rec, http.StatusOK)
	if row := env.get(t, corePackage+"/package", other); row == nil || row.Properties["origin"] != nil {
		t.Fatalf("a bare apply stamped an origin: %+v", row)
	}
}

// The batch schema verb: one POST, every document admitted or none, the
// caller's bearer context supplying dataset and actor.
func TestSchemaApplyEndpoint(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	docs := vocabularyWidgets(env, map[string]any{"name": map[string]any{"type": "string"}})
	rec := vocabularyPost(t, env, "apply", map[string]any{"documents": docs})
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[vocabularyApplyResponse](t, rec)
	var ids []string
	for _, r := range out.Records {
		ids = append(ids, r.Kind+" "+r.ID)
	}
	widget := "~/shop/widget"
	want := []string{corePackage + "/package ~/shop", corePackage + "/kind " + widget}
	if !slices.Equal(ids, want) {
		t.Fatalf("apply answered %v, want %v", ids, want)
	}
	if got := env.get(t, corePackage+"/kind", widget); got == nil {
		t.Fatal("the kind was not stored")
	}
	env.put(t, widget, "w1", map[string]any{"name": "declared"})

	// An empty batch is a validation refusal, mapped like every other.
	rec = vocabularyPost(t, env, "apply", map[string]any{"documents": []map[string]any{}})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
}

// `holdWaitingMappings` holds back a mapping whose source kind neither the
// repository nor the batch declares, prunes its `installs:` entry, commits the
// rest, and names what it held and the package it waits on (decision record
// 0106). A mapping whose source is present rides the batch. Without the flag
// the batch reaches the engine whole, where the loader refuses it.
func TestSchemaApplyHoldsWaitingMappings(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	// The batch spells the package under the repository's full name, and
	// every answer names it as the repository holds it, under `~`.
	owner, own := env.repository+"/crm", "~/crm"
	const slackUser = "providers.substrate.reamde.dev/slack/user"
	mapping := func(name, from string) map[string]any {
		return vocabulary.MappingManifest(owner, name, map[string]any{
			"from": from, "to": owner + "/contact", "property": "contact",
		})
	}
	docs := []map[string]any{
		vocabulary.PackageManifest(owner, 0),
		vocabulary.BundleManifest(owner, map[string]any{
			"installs": []any{owner + "/contact", owner + "/slackcontact", owner + "/taskcontact"},
		}),
		vocabulary.KindManifest(owner, map[string]any{"singular": "contact"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
		mapping("slackcontact", slackUser),
		mapping("taskcontact", taskKind),
	}

	// The plan takes the same flag, so its preview is of the batch the apply
	// commits; without it the waiting mapping refuses the preview too.
	rec := vocabularyPost(t, env, "plan", map[string]any{"documents": docs, "holdWaitingMappings": true})
	wantStatus(t, rec, http.StatusOK)
	if plan := decodeJSON[substrate.VocabularyPlan](t, rec); plan.Lossy || len(plan.Blockers) != 0 {
		t.Fatalf("the held plan = %+v", plan)
	}
	rec = vocabularyPost(t, env, "plan", map[string]any{"documents": docs})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, slackUser)

	rec = vocabularyPost(t, env, "apply", map[string]any{"documents": docs, "holdWaitingMappings": true})
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[vocabularyApplyResponse](t, rec)
	want := substrate.SuggestedMapping{
		ID: own + "/slackcontact", From: slackUser, To: own + "/contact",
		Package: "providers.substrate.reamde.dev/slack", State: substrate.SuggestedMappingWaiting,
	}
	if len(out.HeldMappings) != 1 || out.HeldMappings[0].ID != want.ID || out.HeldMappings[0].Package != want.Package ||
		out.HeldMappings[0].From != want.From || out.HeldMappings[0].To != want.To || out.HeldMappings[0].State != want.State {
		t.Fatalf("heldMappings = %+v, want [%+v]", out.HeldMappings, want)
	}
	var ids []string
	for _, r := range out.Records {
		ids = append(ids, r.ID)
	}
	if fmt.Sprint(ids) != fmt.Sprint([]string{own, own, own + "/contact", own + "/taskcontact"}) {
		t.Fatalf("the engine admitted %v", ids)
	}
	bundle := env.get(t, corePackage+"/bundle", owner)
	if bundle == nil || fmt.Sprint(bundle.Properties["installs"]) != fmt.Sprint([]any{own + "/contact", own + "/taskcontact"}) {
		t.Fatalf("the bundle's installs were not pruned: %+v", bundle)
	}
	if got := env.get(t, corePackage+"/recordmapping", owner+"/slackcontact"); got != nil {
		t.Fatalf("the held mapping was stored: %+v", got)
	}
	if got := env.get(t, corePackage+"/recordmapping", owner+"/taskcontact"); got == nil {
		t.Fatal("the mapping whose source is present did not ride the batch")
	}

	// Without the flag the whole batch reaches the engine, which refuses it,
	// and nothing is reported held.
	rec = vocabularyPost(t, env, "apply", map[string]any{"documents": docs})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	if strings.Contains(rec.Body.String(), "heldMappings") {
		t.Fatalf("an apply without the flag reported held mappings: %s", rec.Body)
	}

	// A batch of nothing but waiting mappings commits nothing: the apply
	// answers what it held and never reaches the engine, and the plan is the
	// empty one, so `--allow-data-loss` previews it without a refusal.
	head := vocabularyHead(t, env)
	onlyWaiting := []map[string]any{mapping("slackcontact", slackUser)}
	rec = vocabularyPost(t, env, "apply", map[string]any{"documents": onlyWaiting, "holdWaitingMappings": true})
	wantStatus(t, rec, http.StatusOK)
	if out := decodeJSON[vocabularyApplyResponse](t, rec); len(out.HeldMappings) != 1 || len(out.Records) != 0 {
		t.Fatalf("an all-waiting apply answered %+v", out)
	}
	rec = vocabularyPost(t, env, "plan", map[string]any{"documents": onlyWaiting, "holdWaitingMappings": true})
	wantStatus(t, rec, http.StatusOK)
	if plan := decodeJSON[substrate.VocabularyPlan](t, rec); plan.Lossy || len(plan.Steps) != 0 || plan.PlanHash != "" {
		t.Fatalf("an all-waiting plan answered %+v", plan)
	}
	if got := vocabularyHead(t, env); got != head {
		t.Fatalf("an all-waiting batch wrote: head %+v, was %+v", got, head)
	}
}

// vocabularyAppPackage has an app declare <repository>/training, as an app
// did under the retired `declare:` scope (decision 0203): its package row
// names the app as its declarer.
func vocabularyAppPackage(t *testing.T, env *dbEnv) (string, substrate.Actor) {
	t.Helper()
	pkg := env.repository + "/training"
	app := substrate.AppActor("training.example.org")
	if _, err := env.ds.ApplyVocabularyDocuments(t.Context(), app, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.BundleManifest(pkg, map[string]any{"installs": []any{pkg + "/set"}}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "set"}, map[string]any{
			"properties": map[string]any{"reps": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatalf("the app declares its package: %v", err)
	}
	if row := env.get(t, corePackage+"/package", pkg); row == nil || !linksActorRecord(row.Properties["declaredBy"], app) {
		t.Fatalf("the package row = %+v, want it declared by %s", row, app)
	}
	return pkg, app
}

// THE OWNER TAKES OVER AN APP'S PACKAGE (decision 0205): the route hands the
// engine the package, the confirmation and the person's hand, and
// answers the package row; the engine's refusals keep their codes. An app's
// token never reaches the route, its own package's or another's.
func TestTheOwnerTakesOverAnAppsPackage(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	pkg, app := vocabularyAppPackage(t, env)
	takeover := func(body map[string]any, bearer string) *httptest.ResponseRecorder {
		t.Helper()
		return env.do(t, http.MethodPost, "/api/v1/vocabulary/takeover", bearer, body)
	}

	appToken := env.mint(t, substrate.MintTokenInput{
		Label: "training", ClientID: "https://training.example.org/client.json", ClientName: "Training",
		Scopes: []string{"write:" + pkg + "/*"},
	})
	wantErrorCode(t, takeover(map[string]any{"package": pkg, "confirm": pkg}, appToken), http.StatusForbidden, codeForbidden)
	wantErrorCode(t, takeover(map[string]any{"package": pkg, "confirm": env.repository + "/other"}, env.token), http.StatusUnprocessableEntity, codeValidation)
	wantErrorCode(t, takeover(map[string]any{"package": env.repository + "/missing", "confirm": env.repository + "/missing"}, env.token), http.StatusNotFound, codeNotFound)
	wantErrorCode(t, takeover(map[string]any{"package": corePackage, "confirm": corePackage}, env.token), http.StatusConflict, codeConflict)
	if row := env.get(t, corePackage+"/package", pkg); !linksActorRecord(row.Properties["declaredBy"], app) {
		t.Fatalf("a refused takeover moved the stamps: %+v", row.Properties)
	}

	rec := takeover(map[string]any{"package": pkg, "confirm": pkg}, env.token)
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[substrate.Record](t, rec)
	if got.ID != "~/training" || !linksActorRecord(got.Properties["declaredBy"], substrate.ActorPerson) ||
		got.Properties["takenOverFrom"] != strings.TrimPrefix(string(app), substrate.AppActorPrefix) {
		t.Fatalf("takeover answered %+v", got)
	}
	// The package is no app's now, so a second takeover is the engine's
	// conflict.
	wantErrorCode(t, takeover(map[string]any{"package": pkg, "confirm": pkg}, env.token), http.StatusConflict, codeConflict)
}

// NO SCOPED TOKEN APPLIES VOCABULARY (decision 0214): whatever an app's
// token writes, its apply and its preview answer 403 before the engine, and
// so do the catalog's bundle doors; the bundle record is a kind it does not
// know. An app's own kinds arrive as a package its approval installs.
func TestAnAppsTokenNeverAppliesVocabulary(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	pkg := env.repository + "/training"
	app := env.mint(t, substrate.MintTokenInput{
		Label: "training", ClientID: "https://training.example.org/client.json", ClientName: "Training",
		Scopes: []string{"write:" + pkg + "/*", "read:*"},
	})
	head := vocabularyHead(t, env)
	own := []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "set"}, map[string]any{}),
	}
	rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", app, map[string]any{"documents": own})
	wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
	wantMessage(t, rec, "does not reach the vocabulary")
	wantErrorCode(t, env.do(t, http.MethodPost, "/api/v1/vocabulary/plan", app, map[string]any{"documents": own}), http.StatusForbidden, codeForbidden)
	if row := env.get(t, corePackage+"/package", pkg); row != nil {
		t.Fatalf("an app's apply declared its package: %+v", row)
	}
	for _, id := range []string{"samples.substrate.reamde.dev/people", "providers.substrate.reamde.dev/google"} {
		rec = env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(id)+"/add", app, nil)
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
	}
	// The bundle record's lifecycle PATCH is on the record path, where a kind
	// the token cannot read is unknown to it: core's bundle kind is internal,
	// which no scope reads (docs/apps.md).
	rec = env.do(t, http.MethodPatch, "/api/v1/"+corePackage+"/bundle/"+url.PathEscape(pkg), app,
		map[string]any{"properties": map[string]any{"uninstalled": true}})
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if got := vocabularyHead(t, env); got != head {
		t.Fatalf("an app's vocabulary requests wrote: head %+v, was %+v", got, head)
	}
}

// linksActorRecord reports whether a stored value links the actor record of a
// hand (decision 0270).
func linksActorRecord(v any, hand substrate.Actor) bool {
	ref, _ := v.(map[string]any)
	return ref[vocabulary.ReferenceValueKey] == vocabulary.ActorRef(vocabulary.ActorID(hand))["ref"]
}
