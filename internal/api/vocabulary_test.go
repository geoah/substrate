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

// The batch schema verb: one POST, every document admitted or none, the
// caller's bearer context supplying dataset and actor.
func TestSchemaApplyEndpoint(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	rec := env.do(t, http.MethodPost, "/api/v1/"+coreAuthority+"/vocabulary/apply", tok, map[string]any{
		"documents": []map[string]any{
			{
				"kind":     coreAuthority + "/authority",
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
	rec = env.do(t, http.MethodPost, "/api/v1/"+coreAuthority+"/vocabulary/apply", tok, map[string]any{
		"documents": []map[string]any{},
	})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
}
