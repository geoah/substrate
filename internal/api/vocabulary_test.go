package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// ApplyVocabularyDocuments is the fake's batch schema verb: it records the
// documents and answers one record per document, the way the engine does.
func (d *fakeDataset) ApplyVocabularyDocuments(_ context.Context, actor substrate.Actor, docs []map[string]any) ([]*substrate.Record, error) {
	if len(docs) == 0 {
		return nil, fmt.Errorf("%w: no documents", substrate.ErrValidation)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastVocabularyDocs = docs
	out := make([]*substrate.Record, 0, len(docs))
	for _, doc := range docs {
		meta, _ := doc["metadata"].(map[string]any)
		id, _ := meta["id"].(string)
		kind, _ := doc["kind"].(string)
		out = append(out, &substrate.Record{ID: id, Kind: kind, Version: 1})
	}
	return out, nil
}

// PlanVocabularyApply answers the plan the test seeded, so what is under test
// is the handler's pass-through and the codes it maps.
func (d *fakeDataset) PlanVocabularyApply(ctx context.Context, actor substrate.Actor, docs []map[string]any) (substrate.VocabularyPlan, error) {
	return d.PlanVocabularyApplyWith(ctx, actor, docs, substrate.VocabularyApply{})
}

// PlanVocabularyApplyWith records the origin the preview was asked under, so a
// test can see the body's `origin` arrive at the dataset.
func (d *fakeDataset) PlanVocabularyApplyWith(_ context.Context, _ substrate.Actor, docs []map[string]any, opts substrate.VocabularyApply) (substrate.VocabularyPlan, error) {
	if err := d.fail("PlanVocabularyApply"); err != nil {
		return substrate.VocabularyPlan{}, err
	}
	if len(docs) == 0 {
		return substrate.VocabularyPlan{}, fmt.Errorf("%w: no documents", substrate.ErrValidation)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastVocabularyDocs = docs
	d.lastOrigin = opts.Origin
	return d.plan, nil
}

// ApplyVocabularyDocumentsWith records the confirmation and applies as the
// bare verb does.
func (d *fakeDataset) ApplyVocabularyDocumentsWith(ctx context.Context, actor substrate.Actor, docs []map[string]any, opts substrate.VocabularyApply) ([]*substrate.Record, error) {
	if err := d.fail("ApplyVocabularyDocumentsWith"); err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.lastConfirm = opts.Confirm
	d.lastOrigin = opts.Origin
	d.mu.Unlock()
	return d.ApplyVocabularyDocuments(ctx, actor, docs)
}

// The preview verb: the dataset's plan for the documents, and nothing written.
// Its body is the documents alone, so a confirmation there is the unknown key
// the strict decoder refuses.
func TestSchemaPlanEndpoint(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	ds.plan = substrate.VocabularyPlan{
		ConversionPlan: substrate.ConversionPlan{
			Lossy: true, Work: 3, PlanHash: "cafe", ChangelogSeq: 41,
			Steps: []substrate.ConversionStep{{Step: substrate.StepNull, Kind: "geoah.example.com/shop/widget", Property: "color", Records: 3, Lossy: true}},
		},
		Blockers: []string{"a guard line"},
	}
	doc := map[string]any{
		"kind":     corePackage + "/authority",
		"metadata": map[string]any{"id": "widgets.example.substrate.reamde.dev"},
		"data":     map[string]any{"version": 1},
	}
	rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/plan", tok, map[string]any{"documents": []map[string]any{doc}})
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[substrate.VocabularyPlan](t, rec)
	if !out.Lossy || out.Work != 3 || out.PlanHash != "cafe" || out.ChangelogSeq != 41 || len(out.Steps) != 1 || out.Steps[0].Property != "color" || len(out.Blockers) != 1 {
		t.Fatalf("plan response = %+v", out)
	}
	if len(ds.lastVocabularyDocs) != 1 {
		t.Fatalf("dataset saw %d documents", len(ds.lastVocabularyDocs))
	}
	rec = env.do(t, http.MethodPost, "/api/v1/vocabulary/plan", tok, map[string]any{
		"documents": []map[string]any{doc},
		"confirm":   map[string]any{"planHash": "cafe", "changelogSeq": 41},
	})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

// The apply body's `confirm` reaches the dataset's confirmed verb; without it
// the bare verb runs; and a lossy refusal answers 403 under its own code, so a
// client knows to preview and confirm rather than migrate records.
func TestSchemaApplyCarriesTheConfirmation(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	doc := map[string]any{
		"kind":     corePackage + "/authority",
		"metadata": map[string]any{"id": "widgets.example.substrate.reamde.dev"},
		"data":     map[string]any{"version": 1},
	}
	rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", tok, map[string]any{
		"documents": []map[string]any{doc},
		"confirm":   map[string]any{"planHash": "cafe", "changelogSeq": 41},
	})
	wantStatus(t, rec, http.StatusOK)
	if ds.lastConfirm == nil || *ds.lastConfirm != (substrate.ConversionConfirm{PlanHash: "cafe", ChangelogSeq: 41}) {
		t.Fatalf("the dataset saw confirmation %+v", ds.lastConfirm)
	}
	ds.lastConfirm = nil
	rec = env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", tok, map[string]any{"documents": []map[string]any{doc}})
	wantStatus(t, rec, http.StatusOK)
	if ds.lastConfirm != nil {
		t.Fatalf("a bare apply handed the dataset a confirmation: %+v", ds.lastConfirm)
	}
	ds.errs["ApplyVocabularyDocumentsWith"] = fmt.Errorf("%w: %w: the change removes values", substrate.ErrGuard, substrate.ErrLossyConversion)
	rec = env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", tok, map[string]any{
		"documents": []map[string]any{doc},
		"confirm":   map[string]any{"planHash": "0000", "changelogSeq": 41},
	})
	wantErrorCode(t, rec, http.StatusForbidden, codeLossy)
}

// A hand-rehomed closure names the package it was authored as (`origin`), and
// both verbs hand the claim to the dataset (decision record 0070): the apply
// so the copy is stamped, the plan so its preview hashes as the apply will. A
// bare apply claims nothing.
func TestSchemaApplyAndPlanCarryTheOrigin(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	const origin = "samples.substrate.reamde.dev/tasks"
	doc := map[string]any{
		"kind":     corePackage + "/package",
		"metadata": map[string]any{"id": "geoah.example.com/tasks"},
		"data":     map[string]any{"authority": "geoah.example.com", "package": "tasks", "version": 7},
	}
	rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", tok, map[string]any{
		"documents": []map[string]any{doc},
		"origin":    origin,
	})
	wantStatus(t, rec, http.StatusOK)
	if ds.lastOrigin != origin {
		t.Fatalf("the apply handed the dataset origin %q, want %q", ds.lastOrigin, origin)
	}
	ds.lastOrigin = ""
	rec = env.do(t, http.MethodPost, "/api/v1/vocabulary/plan", tok, map[string]any{
		"documents": []map[string]any{doc},
		"origin":    origin,
	})
	wantStatus(t, rec, http.StatusOK)
	if ds.lastOrigin != origin {
		t.Fatalf("the plan handed the dataset origin %q, want %q", ds.lastOrigin, origin)
	}
	ds.lastOrigin = "unset"
	rec = env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", tok, map[string]any{"documents": []map[string]any{doc}})
	wantStatus(t, rec, http.StatusOK)
	if ds.lastOrigin != "unset" {
		t.Fatalf("a bare apply took the confirmed path and handed the dataset origin %q", ds.lastOrigin)
	}
}

// The batch schema verb: one POST, every document admitted or none, the
// caller's bearer context supplying dataset and actor.
func TestSchemaApplyEndpoint(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", tok, map[string]any{
		"documents": []map[string]any{
			{
				"kind":     corePackage + "/authority",
				"metadata": map[string]any{"id": "widgets.example.substrate.reamde.dev"},
				"data":     map[string]any{"version": 1},
			},
		},
	})
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[vocabularyApplyResponse](t, rec)
	if len(out.Records) != 1 || out.Records[0].ID != "widgets.example.substrate.reamde.dev" {
		t.Fatalf("apply response = %+v", out)
	}
	if len(ds.lastVocabularyDocs) != 1 {
		t.Fatalf("dataset saw %d documents", len(ds.lastVocabularyDocs))
	}

	// An empty batch is a validation refusal, mapped like every other.
	rec = env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", tok, map[string]any{
		"documents": []map[string]any{},
	})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
}

// `holdWaitingMappings` holds back a mapping whose source kind neither the
// repository nor the batch declares, prunes its `installs:` entry, commits the
// rest, and names what it held and the package it waits on (decision record
// 0106). A mapping whose source is present rides the batch. Without the flag
// the batch reaches the dataset whole, where the loader refuses it.
func TestSchemaApplyHoldsWaitingMappings(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	const owner = "geoah.example.com/crm"
	const slackUser = "providers.substrate.reamde.dev/slack/user"
	mapping := func(name, from string) map[string]any {
		return map[string]any{
			"kind":     corePackage + "/recordmapping",
			"metadata": map[string]any{"id": owner + "/" + name},
			"data": map[string]any{
				"authority": "geoah.example.com", "package": "crm",
				"from": from, "to": owner + "/contact", "property": "contact",
			},
		}
	}
	docs := []map[string]any{
		{
			"kind":     corePackage + "/bundle",
			"metadata": map[string]any{"id": owner},
			"data": map[string]any{
				"authority": "geoah.example.com", "package": "crm",
				"installs": []any{owner + "/contact", owner + "/slackcontact", owner + "/taskcontact"},
			},
		},
		{
			"kind":     corePackage + "/kind",
			"metadata": map[string]any{"id": owner + "/contact"},
			"data":     map[string]any{"authority": "geoah.example.com", "package": "crm"},
		},
		mapping("slackcontact", slackUser),
		mapping("taskcontact", "samples.substrate.reamde.dev/tasks/task"),
	}

	rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", tok, map[string]any{
		"documents": docs, "holdWaitingMappings": true,
	})
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[vocabularyApplyResponse](t, rec)
	want := substrate.SuggestedMapping{
		ID: owner + "/slackcontact", From: slackUser, To: owner + "/contact",
		Package: "providers.substrate.reamde.dev/slack", State: substrate.SuggestedMappingWaiting,
	}
	if len(out.HeldMappings) != 1 || out.HeldMappings[0].ID != want.ID || out.HeldMappings[0].Package != want.Package ||
		out.HeldMappings[0].From != want.From || out.HeldMappings[0].To != want.To || out.HeldMappings[0].State != want.State {
		t.Fatalf("heldMappings = %+v, want [%+v]", out.HeldMappings, want)
	}
	var ids []string
	for _, d := range ds.lastVocabularyDocs {
		meta, _ := d["metadata"].(map[string]any)
		ids = append(ids, fmt.Sprint(meta["id"]))
		if id := meta["id"]; id == owner {
			data, _ := d["data"].(map[string]any)
			if got := fmt.Sprint(data["installs"]); got != fmt.Sprint([]any{owner + "/contact", owner + "/taskcontact"}) {
				t.Fatalf("the bundle's installs were not pruned: %s", got)
			}
		}
	}
	if fmt.Sprint(ids) != fmt.Sprint([]string{owner, owner + "/contact", owner + "/taskcontact"}) {
		t.Fatalf("the dataset saw %v", ids)
	}

	// The plan takes the same flag, so its preview is of the same batch.
	rec = env.do(t, http.MethodPost, "/api/v1/vocabulary/plan", tok, map[string]any{
		"documents": docs, "holdWaitingMappings": true,
	})
	wantStatus(t, rec, http.StatusOK)
	if len(ds.lastVocabularyDocs) != 3 {
		t.Fatalf("the plan saw %d documents, want the 3 the apply commits", len(ds.lastVocabularyDocs))
	}

	// Without the flag the whole batch reaches the dataset, and nothing is
	// reported held.
	rec = env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", tok, map[string]any{"documents": docs})
	wantStatus(t, rec, http.StatusOK)
	if out := decodeJSON[vocabularyApplyResponse](t, rec); len(out.HeldMappings) != 0 || len(ds.lastVocabularyDocs) != 4 {
		t.Fatalf("an apply without the flag held %+v and passed %d documents", out.HeldMappings, len(ds.lastVocabularyDocs))
	}

	// A batch of nothing but waiting mappings commits nothing: the apply
	// answers what it held and never reaches the dataset, and the plan is the
	// empty one, so `--allow-data-loss` previews it without a refusal.
	ds.lastVocabularyDocs = nil
	onlyWaiting := []map[string]any{mapping("slackcontact", slackUser)}
	rec = env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", tok, map[string]any{
		"documents": onlyWaiting, "holdWaitingMappings": true,
	})
	wantStatus(t, rec, http.StatusOK)
	if out := decodeJSON[vocabularyApplyResponse](t, rec); len(out.HeldMappings) != 1 || len(out.Records) != 0 || ds.lastVocabularyDocs != nil {
		t.Fatalf("an all-waiting apply answered %+v and passed %d documents", out, len(ds.lastVocabularyDocs))
	}
	rec = env.do(t, http.MethodPost, "/api/v1/vocabulary/plan", tok, map[string]any{
		"documents": onlyWaiting, "holdWaitingMappings": true,
	})
	wantStatus(t, rec, http.StatusOK)
	if plan := decodeJSON[substrate.VocabularyPlan](t, rec); plan.Lossy || len(plan.Steps) != 0 || ds.lastVocabularyDocs != nil {
		t.Fatalf("an all-waiting plan answered %+v and passed %d documents", plan, len(ds.lastVocabularyDocs))
	}
}

// AN APP KEEPS ITS OWN PACKAGE'S DECLARATIONS AND NOTHING ELSE (decision
// 0203): under `declare:~/<package>` its token applies that package's
// package, bundle and kind documents, and is refused a function, another
// package, a confirmed lossy change and the preview, which stay the owner's.
func TestAnAppDeclaresOnlyThePackageItsScopeNames(t *testing.T) {
	e := newAppEnv(t)
	pkg := fakeRepository + "/training"
	app := e.grant("declare:~/training write:~/training/*", []string{"declare:" + pkg, "write:" + pkg + "/*"})
	doc := func(kind, id string) map[string]any {
		return map[string]any{"kind": corePackage + "/" + kind, "metadata": map[string]any{"id": id}, "data": map[string]any{}}
	}
	apply := func(body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		return e.do(http.MethodPost, "/api/v1/vocabulary/apply", app, raw, "application/json")
	}
	own := []map[string]any{doc("package", pkg), doc("bundle", pkg), doc("kind", pkg+"/set")}
	wantStatus(t, apply(map[string]any{"documents": own}), http.StatusOK)
	if got := len(e.svc.datasets[fakeRepository].lastVocabularyDocs); got != len(own) {
		t.Fatalf("the dataset saw %d documents, want %d", got, len(own))
	}
	for name, docs := range map[string][]map[string]any{
		"a function of the package":   {doc("function", pkg+"/rollup")},
		"a trigger":                   {doc("trigger", "training-on-set")},
		"another package of its own":  {doc("kind", fakeRepository+"/notes/note")},
		"a first-party package":       {doc("kind", "substrate.reamde.dev/training/set")},
		"its package beside another":  {doc("package", pkg), doc("package", fakeRepository+"/notes")},
		"a kind named without a home": {doc("kind", "set")},
	} {
		if rec := apply(map[string]any{"documents": docs}); rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d %s, want 403", name, rec.Code, rec.Body.String())
		}
	}
	confirmed := apply(map[string]any{"documents": own, "confirm": map[string]any{"planHash": "cafe", "changelogSeq": 1}})
	wantStatus(t, confirmed, http.StatusForbidden)
	raw, _ := json.Marshal(map[string]any{"documents": own})
	wantStatus(t, e.do(http.MethodPost, "/api/v1/vocabulary/plan", app, raw, "application/json"), http.StatusForbidden)
	// A token without the scope is refused the same batch.
	writer := e.grant("write:~/training/*", []string{"write:" + pkg + "/*"})
	if rec := e.do(http.MethodPost, "/api/v1/vocabulary/apply", writer, raw, "application/json"); rec.Code != http.StatusForbidden {
		t.Errorf("a write scope declared its package: %d %s", rec.Code, rec.Body.String())
	}
}

// A DECLARED BUNDLE ONLY LISTS ITS CLOSURE (decision 0203): `modules` is code
// every function of the package imports, `oauth2` makes the package a
// provider at endpoints the app would choose, `inputs` binds records the
// owner configures. A kind may not build indexes or embed values, which spend
// the owner's database and provider. Each is refused, naming the key.
func TestADeclaredBundleListsItsKindsAndNothingElse(t *testing.T) {
	e := newAppEnv(t)
	pkg := fakeRepository + "/training"
	app := e.grant("declare:~/training write:~/training/*", []string{"declare:" + pkg, "write:" + pkg + "/*"})
	apply := func(docs ...map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"documents": docs})
		return e.do(http.MethodPost, "/api/v1/vocabulary/apply", app, raw, "application/json")
	}
	bundle := func(data map[string]any) map[string]any {
		base := map[string]any{"authority": fakeRepository, "package": "training", "installs": []any{pkg + "/set"}}
		for k, v := range data {
			base[k] = v
		}
		return map[string]any{"kind": corePackage + "/bundle", "metadata": map[string]any{"id": pkg}, "data": base}
	}
	kind := func(data map[string]any) map[string]any {
		return map[string]any{"kind": corePackage + "/kind", "metadata": map[string]any{"id": pkg + "/set"}, "data": data}
	}
	wantStatus(t, apply(bundle(map[string]any{"requires": []any{"substrate.reamde.dev/health"}, "description": "sets"})), http.StatusOK)
	for name, doc := range map[string]map[string]any{
		"modules":       bundle(map[string]any{"modules": map[string]any{"helpers": "import os"}}),
		"oauth2":        bundle(map[string]any{"oauth2": map[string]any{"authorizationEndpoint": "https://evil.example.org/auth"}}),
		"inputs":        bundle(map[string]any{"inputs": map[string]any{"config": map[string]any{"kind": pkg + "/set"}}}),
		"indices":       kind(map[string]any{"indices": []any{map[string]any{"properties": []any{"reps"}}}}),
		"embed":         kind(map[string]any{"properties": map[string]any{"notes": map[string]any{"type": "text", "embed": true}}}),
		"embed (field)": kind(map[string]any{"properties": map[string]any{"target": map[string]any{"type": "object", "fields": map[string]any{"notes": map[string]any{"type": "text", "embed": true}}}}}),
	} {
		rec := apply(doc)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), strings.Fields(name)[0]) {
			t.Errorf("%s: %d %s, want 403 naming the key", name, rec.Code, rec.Body.String())
		}
	}
}

// AN APP'S APPLY ONLY ADDS (decision 0203): whatever the repository holds in
// the package, the owner's additions included, stays. A kind document that
// leaves out a stored property, field, enum value, state, transition or
// trait is refused with 409, and so is a bundle that would replace a package
// whose stored closure it does not list in full. The app reads its own kind
// rows, and no other package's.
func TestAnAppsApplyNeverRemovesWhatThePackageHolds(t *testing.T) {
	e := newAppEnv(t)
	pkg := fakeRepository + "/training"
	app := e.grant("declare:~/training write:~/training/*", []string{"declare:" + pkg, "write:" + pkg + "/*"})
	ds := e.svc.datasets[fakeRepository]
	stored := map[string]any{
		"traits": []any{"substrate.reamde.dev/core/revised"},
		"properties": map[string]any{
			"reps":   map[string]any{"type": "int"},
			"notes":  map[string]any{"type": "text"},
			"effort": map[string]any{"type": "enum", "values": []any{map[string]any{"value": "easy"}, map[string]any{"value": "hard"}}},
			"status": map[string]any{
				"type": "state", "states": []any{"active", "finished"},
				"transitions": []any{map[string]any{"from": "active", "to": "finished"}},
			},
			"target": map[string]any{"type": "object", "fields": map[string]any{"load": map[string]any{"type": "decimal"}}},
		},
	}
	for _, k := range []string{"package", "bundle", "kind"} {
		ds.types = append(ds.types, substrate.KindInfo{Identity: corePackage + "/" + k, Name: k, Authority: "substrate.reamde.dev", Package: "core", Source: "builtin"})
	}
	ds.types = append(ds.types,
		substrate.KindInfo{Identity: pkg + "/set", Name: "set", Authority: fakeRepository, Package: "training", Source: "installed", Version: 2, Definition: stored},
		substrate.KindInfo{Identity: pkg + "/bodyweight", Name: "bodyweight", Authority: fakeRepository, Package: "training", Source: "installed", Version: 1, Definition: map[string]any{}},
	)
	ds.records[pkg] = &substrate.Record{Kind: corePackage + "/bundle", ID: pkg, Properties: map[string]any{"installs": []any{pkg + "/set", pkg + "/bodyweight"}}}
	ds.records[pkg+"/set"] = &substrate.Record{Kind: corePackage + "/kind", ID: pkg + "/set", Properties: map[string]any{"version": 2}}

	apply := func(docs ...map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"documents": docs})
		return e.do(http.MethodPost, "/api/v1/vocabulary/apply", app, raw, "application/json")
	}
	without := func(edit func(def map[string]any)) map[string]any {
		raw, _ := json.Marshal(stored)
		var def map[string]any
		_ = json.Unmarshal(raw, &def)
		edit(def)
		return map[string]any{"kind": corePackage + "/kind", "metadata": map[string]any{"id": pkg + "/set"}, "data": def}
	}
	props := func(def map[string]any) map[string]any { return def["properties"].(map[string]any) }
	prop := func(def map[string]any, name string) map[string]any { return props(def)[name].(map[string]any) }

	// Everything kept, one property added: admitted.
	wantStatus(t, apply(without(func(def map[string]any) { props(def)["rpe"] = map[string]any{"type": "decimal"} })), http.StatusOK)
	for name, doc := range map[string]map[string]any{
		"a property":    without(func(def map[string]any) { delete(props(def), "notes") }),
		"a field":       without(func(def map[string]any) { prop(def, "target")["fields"] = map[string]any{} }),
		"an enum value": without(func(def map[string]any) { prop(def, "effort")["values"] = []any{map[string]any{"value": "easy"}} }),
		"a state":       without(func(def map[string]any) { prop(def, "status")["states"] = []any{"active"} }),
		"a transition":  without(func(def map[string]any) { prop(def, "status")["transitions"] = []any{} }),
		"a trait":       without(func(def map[string]any) { def["traits"] = []any{} }),
	} {
		if rec := apply(doc); rec.Code != http.StatusConflict {
			t.Errorf("dropping %s: %d %s, want 409", name, rec.Code, rec.Body.String())
		}
	}
	bundle := func(installs ...any) map[string]any {
		return map[string]any{
			"kind": corePackage + "/bundle", "metadata": map[string]any{"id": pkg},
			"data": map[string]any{"authority": fakeRepository, "package": "training", "installs": installs},
		}
	}
	rec := apply(bundle(pkg + "/set"))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), pkg+"/bodyweight") {
		t.Errorf("a bundle dropping the owner's kind: %d %s, want 409 naming it", rec.Code, rec.Body.String())
	}
	wantStatus(t, apply(bundle(pkg+"/set", pkg+"/bodyweight", pkg+"/session")), http.StatusOK)
	// A package holding kinds and no bundle: a bundle would replace it blind.
	delete(ds.records, pkg)
	wantStatus(t, apply(bundle(pkg+"/set", pkg+"/bodyweight")), http.StatusConflict)

	// Its own kind rows read; another package's do not.
	wantStatus(t, e.do(http.MethodGet, "/api/v1/"+corePackage+"/kind/"+url.PathEscape(pkg+"/set"), app, nil, ""), http.StatusOK)
	ds.records[fakeRepository+"/notes/note"] = &substrate.Record{Kind: corePackage + "/kind", ID: fakeRepository + "/notes/note"}
	if rec := e.do(http.MethodGet, "/api/v1/"+corePackage+"/kind/"+url.PathEscape(fakeRepository+"/notes/note"), app, nil, ""); rec.Code == http.StatusOK {
		t.Errorf("an app read another package's kind row: %s", rec.Body.String())
	}
}
