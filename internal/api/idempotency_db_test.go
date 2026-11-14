package api

import (
	"net/http"
	"net/http/httptest"
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

// A create whose answer was lost, repeated under its key, is refused when
// the first attempt's outcome was too large to keep: the refusal is about
// that first attempt, which landed, and says so, so a client does not take
// its write for rolled back. Every `<` in the description is one byte in the
// request and six in the stored answer, so the request fits its 1 MiB and
// the outcome passes the 1 MiB it may keep.
func TestARepeatAfterALostAnswerSaysTheFirstAttemptLanded(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	// Written by hand: an encoder would escape each `<` as the server does.
	body := `{"kind":"` + taskKind + `","properties":{"name":"Large","description":"` + strings.Repeat("<", 200_000) + `"}}`
	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, recordsPath, strings.NewReader(body))
		req.RemoteAddr = "10.0.0.1:1234"
		req.Header.Set("Authorization", "Bearer "+env.token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(idempotencyHeader, "lost-answer")
		rec := httptest.NewRecorder()
		env.h.ServeHTTP(rec, req)
		return rec
	}
	first := post()
	wantStatus(t, first, http.StatusCreated)
	landed := decodeJSON[substrate.Record](t, first)

	// The client never saw that answer, and sends the request again.
	repeat := post()
	wantErrorCode(t, repeat, http.StatusConflict, codeConflict)
	refusal := decodeJSON[substrate.ErrorEnvelope](t, repeat).Error
	if refusal.Reason != substrate.ReasonFirstAttemptLanded || !strings.Contains(refusal.Message, landed.ID) {
		t.Fatalf("the repeat answered %+v, want the reason %s and the record the first attempt wrote", refusal, substrate.ReasonFirstAttemptLanded)
	}
	if got := env.get(t, taskKind, landed.ID); got == nil {
		t.Fatal("the first attempt's record is not there")
	}
}
