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
func (d *fakeDataset) PlanVocabularyApply(_ context.Context, _ substrate.Actor, docs []map[string]any) (substrate.VocabularyPlan, error) {
	if err := d.fail("PlanVocabularyApply"); err != nil {
		return substrate.VocabularyPlan{}, err
	}
	if len(docs) == 0 {
		return substrate.VocabularyPlan{}, fmt.Errorf("%w: no documents", substrate.ErrValidation)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastVocabularyDocs = docs
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
	d.mu.Unlock()
	return d.ApplyVocabularyDocuments(ctx, actor, docs)
}

// The preview verb: the dataset's plan for the documents, and nothing written.
// Its body is the documents alone, so a confirmation there is the unknown key
// the strict decoder refuses.
func TestSchemaPlanEndpoint(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
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
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
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

// The batch schema verb: one POST, every document admitted or none, the
// caller's bearer context supplying dataset and actor.
func TestSchemaApplyEndpoint(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

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
