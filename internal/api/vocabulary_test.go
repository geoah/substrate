package api

import (
	"context"
	"fmt"
	"net/http"
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
}
