package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// peoplePath is the person kind's reference under the version prefix: a
// record's URL is that plus its id (decision 0033), and the path alone names
// nothing (there is no collection route; the list is /records).
const peoplePath = "/api/v1/samples.substrate.reamde.dev/people/person"

// restRefusing is a repository that resolves the person kind and refuses
// every write with err: the sentinels the handler maps include an
// authentication failure on a write, a function fault and a repository
// latched behind its tables, which no request can make the engine return.
type restRefusing struct {
	stubKinds
	err error
}

func newRestRefusing(err error) *restRefusing {
	return &restRefusing{stubKinds: newStubKinds(personKind), err: err}
}

func (d *restRefusing) Put(context.Context, substrate.Actor, substrate.PutInput) (*substrate.Record, error) {
	return nil, d.err
}

func (d *restRefusing) Patch(context.Context, substrate.Actor, string, string, substrate.PatchInput) (*substrate.Record, error) {
	return nil, d.err
}

func TestRESTBadFilterIsBadRequest(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodGet, recordsPath+"?filter=%7Bnot-json", stubToken, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

func TestRESTErrorEnvelopeMapping(t *testing.T) {
	ds := newRestRefusing(nil)
	env := newStubEnv(t, ds)
	tok := stubToken

	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"conflict", fmt.Errorf("version: %w", substrate.ErrConflict), http.StatusConflict, codeConflict},
		{"accept_conflict", &substrate.AcceptConflictError{Reason: "the diff applied no change"}, http.StatusConflict, codeConflict},
		{"guard", fmt.Errorf("transition: %w", substrate.ErrGuard), http.StatusForbidden, codeGuard},
		{"forbidden", fmt.Errorf("label ns: %w", substrate.ErrForbidden), http.StatusForbidden, codeForbidden},
		{"auth", fmt.Errorf("token: %w", substrate.ErrAuth), http.StatusUnauthorized, codeAuth},
		{"not_found", fmt.Errorf("id: %w", substrate.ErrNotFound), http.StatusNotFound, codeNotFound},
		{"function_failed", fmt.Errorf("body: %w", substrate.ErrFunctionFault), http.StatusInternalServerError, codeFunctionFailed},
		{"parked", fmt.Errorf("%w: trigger t: parked delivery 7 ran again and failed", substrate.ErrParked), http.StatusConflict, codeParked},
		{"internal", errors.New("boom"), http.StatusInternalServerError, codeInternal},
		{"unavailable", fmt.Errorf("vectors: %w", substrate.ErrUnavailable), http.StatusServiceUnavailable, codeUnavailable},
		// The engine's two directory refusals (engine.ErrDirectoryWrite,
		// engine.ErrChangelogFileBehind), as they reach this package: a write
		// rolled back because its files could not be written is retryable and
		// wraps ErrUnavailable; a dataset latched behind its tables needs a
		// restart and wraps ErrRestartRequired. Both are 503 unavailable.
		{"directory_write_rolled_back", fmt.Errorf("%w: the repository directory could not be written, so the write was rolled back", substrate.ErrUnavailable), http.StatusServiceUnavailable, codeUnavailable},
		{"directory_behind_latched", fmt.Errorf("%w: the repository directory is behind the tables after a failed write; restart the server so the boot check catches it up", substrate.ErrRestartRequired), http.StatusServiceUnavailable, codeUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ds.err = tc.err
			rec := env.do(t, http.MethodPost, recordsPath, tok, map[string]any{"kind": personKind, "properties": map[string]any{"title": "x"}})
			wantErrorCode(t, rec, tc.status, tc.code)
			// Every unavailable carries Retry-After.
			if tc.status == http.StatusServiceUnavailable && rec.Header().Get("Retry-After") == "" {
				t.Fatalf("503 without Retry-After: %v", rec.Header())
			}
		})
	}

	ds.err = &substrate.ValidationError{Problems: []string{"name: required", "asin: malformed"}}
	rec := env.do(t, http.MethodPost, recordsPath, tok, map[string]any{"kind": personKind, "properties": map[string]any{"title": "x"}})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	env2 := decodeJSON[substrate.ErrorEnvelope](t, rec)
	if len(env2.Error.Problems) != 2 || env2.Error.Problems[0] != "name: required" {
		t.Fatalf("problems = %v", env2.Error.Problems)
	}
	// The structured sibling splits each problem on its first ": " so a form
	// maps it to the field it concerns without parsing prose.
	want := []substrate.ProblemDetail{{Path: "name", Message: "required"}, {Path: "asin", Message: "malformed"}}
	if !reflect.DeepEqual(env2.Error.ProblemDetails, want) {
		t.Fatalf("problemDetails = %v, want %v", env2.Error.ProblemDetails, want)
	}
	if env2.Error.Reason != "" {
		t.Fatalf("a plain validation refusal answered reason %q", env2.Error.Reason)
	}
}

// A write whose revision is below the stored one is refused `validation`, as
// it always was, and narrowed by the reason `stale_revision`, so a client's
// outbox drops it without matching the message, which stays the same words
// for a client that still does. A removed revision is no stale write and
// carries no reason. This is the envelope for the engine's refusal on every
// write door; the engine's own refusal is
// TestRESTStaleRevisionIsRefusedWithItsReason.
func TestRESTStaleRevisionIsValidationWithItsReason(t *testing.T) {
	ds := newRestRefusing(nil)
	env := newStubEnv(t, ds)
	tok := stubToken

	const problem = "props.revision: 2 is below the stored revision 3; a rewrite never lowers it, so this is a stale delivery"
	stale := &substrate.ValidationError{Problems: []string{problem}, Reason: substrate.ReasonStaleRevision}
	writes := []struct {
		method, path string
		body         map[string]any
	}{
		{http.MethodPost, recordsPath, map[string]any{"kind": personKind, "properties": map[string]any{"revision": 2}}},
		{http.MethodPut, peoplePath + "/w", map[string]any{"properties": map[string]any{"revision": 2}}},
		{http.MethodPatch, peoplePath + "/w", map[string]any{"properties": map[string]any{"revision": 2}}},
	}
	ds.err = stale
	for _, w := range writes {
		t.Run(w.method, func(t *testing.T) {
			rec := env.do(t, w.method, w.path, tok, w.body)
			wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
			// The reason sits at error.reason, beside the code.
			var raw struct {
				Error map[string]any `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			if raw.Error["reason"] != "stale_revision" {
				t.Fatalf("error.reason = %v, want stale_revision (body %s)", raw.Error["reason"], rec.Body.String())
			}
			p := decodeJSON[substrate.ErrorEnvelope](t, rec).Error
			if p.Message != stale.Error() || !strings.Contains(p.Message, "below the stored revision") {
				t.Fatalf("message = %q, want the words it always had", p.Message)
			}
			want := []substrate.ProblemDetail{{Path: "props.revision", Message: strings.TrimPrefix(problem, "props.revision: ")}}
			if !reflect.DeepEqual(p.ProblemDetails, want) || !reflect.DeepEqual(p.Problems, []string{problem}) {
				t.Fatalf("problems = %v, problemDetails = %v", p.Problems, p.ProblemDetails)
			}
		})
	}

	ds.err = &substrate.ValidationError{Problems: []string{"props.revision: the stored revision 3 is never removed; a rewrite carries one at least as high"}}
	rec := env.do(t, http.MethodPut, peoplePath+"/w", tok, map[string]any{"properties": map[string]any{}})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	if strings.Contains(rec.Body.String(), `"reason"`) {
		t.Fatalf("a removed revision answered a reason: %s", rec.Body.String())
	}
}

// Issue 516: a repository latched behind its tables refuses every write until
// the server restarts. The refusal is 503 unavailable, not the 500 internal it
// was, with a Retry-After longer than a transient refusal's and the engine's
// message, so the reply itself tells an operator to restart.
func TestRESTRestartRequiredIsUnavailableNamingTheRestart(t *testing.T) {
	const msg = "the repository directory is behind the tables after a failed write; restart the server so the boot check catches it up"
	ds := newRestRefusing(fmt.Errorf("%w: %s: repository %s: prepare seq 206..206", substrate.ErrRestartRequired, msg, stubRepository))
	env := newStubEnv(t, ds)
	tok := stubToken
	rec := env.do(t, http.MethodPost, recordsPath, tok, map[string]any{"kind": personKind, "properties": map[string]any{"title": "x"}})
	wantErrorCode(t, rec, http.StatusServiceUnavailable, codeUnavailable)
	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Fatalf("Retry-After = %q, want 30", got)
	}
	body := decodeJSON[substrate.ErrorEnvelope](t, rec)
	if !strings.Contains(body.Error.Message, "restart the server") || !strings.Contains(body.Error.Message, "until the server restarts") {
		t.Fatalf("the message does not name the restart: %q", body.Error.Message)
	}

	// A transient refusal keeps its one-second Retry-After.
	ds.err = fmt.Errorf("vectors: %w", substrate.ErrUnavailable)
	rec = env.do(t, http.MethodPost, recordsPath, tok, map[string]any{"kind": personKind, "properties": map[string]any{"title": "x"}})
	wantErrorCode(t, rec, http.StatusServiceUnavailable, codeUnavailable)
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After on a transient refusal = %q, want 1", got)
	}
}

// Issue 553: a failed accept of a change request answers ONE refusal. The
// engine's AcceptConflictError carries the reason with no inner sentinel, so
// the wire says 409 conflict with the reason whatever the cause — never the
// 422 a no-op diff's validation failure used to surface as, and never the
// words "version conflict", which belong to the caller's own ifVersion. This
// is the envelope for each error; the engine's own failed accept is
// TestRESTFailedAcceptOfANoOpDiffIsConflict.
func TestRESTFailedAcceptIsConflictWithItsReason(t *testing.T) {
	const reason = "the diff applied no change — the target already matches the proposed values"
	ds := newRestRefusing(&substrate.AcceptConflictError{Reason: reason})
	env := newStubEnv(t, ds)
	tok := stubToken

	rec := env.do(t, http.MethodPatch, peoplePath+"/anyone", tok,
		map[string]any{"properties": map[string]any{"decision": "accepted"}, "ifVersion": 1})
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	body := decodeJSON[substrate.ErrorEnvelope](t, rec)
	if !strings.Contains(body.Error.Message, reason) {
		t.Fatalf("message does not name the reason: %q", body.Error.Message)
	}
	if strings.Contains(body.Error.Message, "version conflict") {
		t.Fatalf("a failed accept must not claim a version conflict: %q", body.Error.Message)
	}
	if len(body.Error.Problems) != 0 {
		t.Fatalf("a failed accept is not a validation refusal: %v", body.Error.Problems)
	}

	// The same PATCH under a stale ifVersion is still the version conflict it
	// always was: the two stay tellable apart by message under one code.
	ds.err = fmt.Errorf("%w: ifVersion 1, stored 4", substrate.ErrConflict)
	rec = env.do(t, http.MethodPatch, peoplePath+"/anyone", tok,
		map[string]any{"properties": map[string]any{"name": "Ada"}, "ifVersion": 1})
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if stale := decodeJSON[substrate.ErrorEnvelope](t, rec); !strings.Contains(stale.Error.Message, "version conflict") {
		t.Fatalf("a stale ifVersion still reads as a version conflict: %q", stale.Error.Message)
	}
}
