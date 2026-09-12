package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// peoplePath is the person kind's reference under the version prefix: a
// record's URL is that plus its id (decision 0033), and the path alone names
// nothing (there is no collection route; the list is /records).
const peoplePath = "/api/v1/samples.substrate.reamde.dev/people/person"

func TestRESTUnknownKindIs404(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	rec := env.do(t, http.MethodGet, "/api/v1/samples.substrate.reamde.dev/people/widgets/p1", tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "unknown kind samples.substrate.reamde.dev/people/widgets") {
		t.Fatalf("message = %q, want the kind named", msg)
	}
}

func TestRESTCRUD(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	created := createRecord(t, env, tok, personKind, map[string]any{"title": "Ada", "name": "Ada"})
	if created.Kind != personKind {
		t.Fatalf("POST must stamp the body's kind, got %q", created.Kind)
	}
	if ds.lastPut.Kind != personKind {
		t.Fatalf("put input kind = %q", ds.lastPut.Kind)
	}

	rec := env.do(t, http.MethodGet, peoplePath+"/"+created.ID, tok, nil)
	wantStatus(t, rec, http.StatusOK)

	// PUT to a fresh id CREATES it, so the status is 201: the code
	// reports what the write did, not which method carried it.
	rec = env.do(t, http.MethodPut, peoplePath+"/fixed1", tok, map[string]any{"properties": map[string]any{"title": "Grace"}})
	wantStatus(t, rec, http.StatusCreated)
	if ds.lastPut.ID != "fixed1" {
		t.Fatalf("PUT must address the path id, got %q", ds.lastPut.ID)
	}
	// A second PUT to the same id is an UPDATE, so it is 200.
	rec = env.do(t, http.MethodPut, peoplePath+"/fixed1", tok, map[string]any{"properties": map[string]any{"title": "Grace H"}})
	wantStatus(t, rec, http.StatusOK)

	rec = env.do(t, http.MethodPatch, peoplePath+"/"+created.ID, tok,
		map[string]any{"properties": map[string]any{"company": "Analytical"}})
	wantStatus(t, rec, http.StatusOK)
	patched := decodeJSON[substrate.Record](t, rec)
	if patched.Properties["company"] != "Analytical" {
		t.Fatalf("patched props = %v", patched.Properties)
	}

	rec = env.do(t, http.MethodDelete, peoplePath+"/"+created.ID, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	deleted := decodeJSON[substrate.Record](t, rec)
	if deleted.DeletedAt == nil {
		t.Fatal("delete must be a soft delete (deletedAt set)")
	}

	rec = env.do(t, http.MethodGet, peoplePath+"/nope", tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}

// GET one carries the property provenance; lists never do.
func TestRESTGetOneCarriesPropertyMeta(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	at := time.Unix(1_700_000_000, 0).UTC()
	ds.records["p1"] = &substrate.Record{
		ID: "p1", Kind: "samples.substrate.reamde.dev/people/person",
		Properties: map[string]any{"name": "Sam"},
	}
	ds.meta["p1"] = map[string]substrate.PropertyMeta{
		"name": {Manager: "owner", UpdatedAt: at, Alternatives: []substrate.PropertyAlternative{
			{Actor: "google.connectors.substrate.reamde.dev/google/people", Value: "Samuel Jones", UpdatedAt: at},
		}},
	}

	rec := env.do(t, http.MethodGet, peoplePath+"/p1", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	raw, ok := got["propertyMeta"]
	if !ok {
		t.Fatalf("GET one lost propertyMeta: %s", rec.Body.String())
	}
	var meta map[string]substrate.PropertyMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["name"].Manager != "owner" || len(meta["name"].Alternatives) != 1 ||
		meta["name"].Alternatives[0].Value != "Samuel Jones" {
		t.Fatalf("propertyMeta = %+v", meta)
	}

	// The list projection stays lean: no provenance on a list read.
	rec = env.do(t, http.MethodGet, recordsOf(t, personKind), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "propertyMeta") {
		t.Fatalf("list carries propertyMeta: %s", rec.Body.String())
	}
}

// A record carries what it points at; what points BACK is a list under
// `filter.referencing`, and the target is matched by its canonical id and
// every former one, so a pointer written before a merge still counts.
func TestRESTReferencingFollowsAMergedTarget(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	ds.put(&substrate.Record{ID: "winner", Kind: personKind, Properties: map[string]any{"name": "Sam"}})
	ds.put(&substrate.Record{ID: "loser", Kind: personKind, Properties: map[string]any{}})
	ds.formers["loser"] = "winner"
	ds.put(&substrate.Record{ID: "p2", Kind: personKind, Properties: map[string]any{
		"name": "Ada", "manager": map[string]any{"ref": personKind + "/loser"},
	}})

	// The record body never carries its pointers-back.
	rec := env.do(t, http.MethodGet, peoplePath+"/winner", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if body := rec.Body.String(); strings.Contains(body, "referencing") || strings.Contains(body, "matches") {
		t.Fatalf("the record body carries a reverse read: %s", body)
	}

	rec = env.do(t, http.MethodGet, filterPath(t, substrate.Filter{
		Referencing: &substrate.Referencing{Ref: personKind + "/winner"},
	}), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	if len(page.Records) != 1 || page.Records[0].ID != "p2" {
		t.Fatalf("referencing page = %+v", page.Records)
	}
	if got := page.Matches[personKind+"/p2"]; len(got) != 1 || got[0].Property != "manager" {
		t.Fatalf("matches = %+v", page.Matches)
	}
}

func TestRESTBadFilterIsBadRequest(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	rec := env.do(t, http.MethodGet, recordsPath+"?filter=%7Bnot-json", tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

func TestRESTErrorEnvelopeMapping(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"conflict", fmt.Errorf("version: %w", substrate.ErrConflict), http.StatusConflict, codeConflict},
		{"guard", fmt.Errorf("transition: %w", substrate.ErrGuard), http.StatusForbidden, codeGuard},
		{"forbidden", fmt.Errorf("label ns: %w", substrate.ErrForbidden), http.StatusForbidden, codeForbidden},
		{"auth", fmt.Errorf("token: %w", substrate.ErrAuth), http.StatusUnauthorized, codeAuth},
		{"not_found", fmt.Errorf("id: %w", substrate.ErrNotFound), http.StatusNotFound, codeNotFound},
		{"function_failed", fmt.Errorf("body: %w", substrate.ErrFunctionFault), http.StatusInternalServerError, codeFunctionFailed},
		{"internal", errBoom, http.StatusInternalServerError, codeInternal},
		{"unavailable", fmt.Errorf("vectors: %w", substrate.ErrUnavailable), http.StatusServiceUnavailable, codeUnavailable},
		// The engine's two directory refusals (engine.ErrDirectoryWrite,
		// engine.ErrChangelogFileBehind), as they reach this package: a write
		// rolled back because its files could not be written is retryable and
		// wraps ErrUnavailable; a dataset latched behind its tables is not,
		// and is a plain error.
		{"directory_write_rolled_back", fmt.Errorf("%w: the repository directory could not be written, so the write was rolled back", substrate.ErrUnavailable), http.StatusServiceUnavailable, codeUnavailable},
		{"directory_behind_latched", errors.New("substrate/engine: the repository directory is behind the tables after a failed write; restart the server so the boot check catches it up"), http.StatusInternalServerError, codeInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ds.errs["Put"] = tc.err
			defer delete(ds.errs, "Put")
			rec := env.do(t, http.MethodPost, recordsPath, tok, map[string]any{"kind": personKind, "properties": map[string]any{"title": "x"}})
			wantErrorCode(t, rec, tc.status, tc.code)
			// Every unavailable carries Retry-After.
			if tc.status == http.StatusServiceUnavailable && rec.Header().Get("Retry-After") == "" {
				t.Fatalf("503 without Retry-After: %v", rec.Header())
			}
		})
	}

	ds.errs["Put"] = &substrate.ValidationError{Problems: []string{"name: required", "asin: malformed"}}
	defer delete(ds.errs, "Put")
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
}
