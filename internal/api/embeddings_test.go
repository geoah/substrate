package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The re-embed verb is the console's door to what `substratectl --dsn …
// repository reembed` is the operator's: it queues, it does not buy, and it
// reports the pair every replacement vector will name.
func TestReembedEndpoint(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	path := "/api/v1/embeddings/reembed"

	rec := env.do(t, http.MethodPost, path, tok, map[string]any{})
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[substrate.ReembedReport](t, rec)
	if out.Provider != "vectors" || out.Model != "text-embedding-3-small" || out.Enqueued != 7 {
		t.Fatalf("reembed report = %+v", out)
	}
	if out.All {
		t.Fatalf("the default scan reported all=true")
	}

	// `all` reaches the dataset: the flag is what a gateway swapped behind an
	// unchanged row and model needs, and a verb that dropped it would look
	// like it worked.
	rec = env.do(t, http.MethodPost, path, tok, map[string]any{"all": true})
	wantStatus(t, rec, http.StatusOK)
	if out := decodeJSON[substrate.ReembedReport](t, rec); !out.All {
		t.Fatalf("all=true did not reach the dataset: %+v", out)
	}
	if len(ds.reembedCalls) != 2 || ds.reembedCalls[0] || !ds.reembedCalls[1] {
		t.Fatalf("dataset saw %v", ds.reembedCalls)
	}

	// A repository with no embeddings provider gets the engine's own refusal,
	// mapped like every other validation error.
	ds.reembedErr = fmt.Errorf("%w: nothing to re-embed against: no llmprovider row declares embedModel", substrate.ErrValidation)
	rec = env.do(t, http.MethodPost, path, tok, map[string]any{})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
}
