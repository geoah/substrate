package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A repeat under one key answers the first record with its `201`, and the
// same key with another body is the closed error set's `409 conflict` whose
// message names the key, the code the docs promise.
func TestIdempotencyKeyRepeatReplaysAndMismatchIs409(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	path := recordsPath
	body := map[string]any{"kind": personKind, "properties": map[string]any{"name": "Ada"}}

	first := env.do(t, http.MethodPost, path, env.token, body, idempotencyHeader, "reused")
	wantStatus(t, first, http.StatusCreated)
	repeat := env.do(t, http.MethodPost, path, env.token, body, idempotencyHeader, "reused")
	wantStatus(t, repeat, http.StatusCreated)
	a, b := decodeJSON[substrate.Record](t, first), decodeJSON[substrate.Record](t, repeat)
	if a.ID != b.ID {
		t.Fatalf("the repeat created %s, want the first attempt's %s", b.ID, a.ID)
	}
	if got := env.get(t, personKind, a.ID); got == nil || got.Version != a.Version {
		t.Fatalf("the repeat wrote the record again: %+v, want version %d", got, a.Version)
	}

	rec := env.do(t, http.MethodPost, path, env.token,
		map[string]any{"kind": personKind, "properties": map[string]any{"name": "Grace"}}, idempotencyHeader, "reused")
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, `Idempotency-Key "reused"`) {
		t.Fatalf("the conflict does not name the key: %q", msg)
	}
}
