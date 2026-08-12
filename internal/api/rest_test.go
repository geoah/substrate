package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

const peoplePath = "/api/v1/people.substrate.reamde.dev/people"

func TestRESTUnknownCollectionIs404(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/people.substrate.reamde.dev/widgets", tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}

func TestRESTCRUD(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	rec := env.do(t, http.MethodPost, peoplePath, tok, map[string]any{
		"properties": map[string]any{"title": "Ada", "name": "Ada"},
	})
	wantStatus(t, rec, http.StatusCreated)
	created := decodeJSON[substrate.Record](t, rec)
	if created.Kind != "people.substrate.reamde.dev/person" {
		t.Fatalf("POST must stamp the collection's type, got %q", created.Kind)
	}
	if ds.lastPut.Kind != "people.substrate.reamde.dev/person" {
		t.Fatalf("put input type = %q", ds.lastPut.Kind)
	}

	rec = env.do(t, http.MethodGet, peoplePath+"/"+created.ID, tok, nil)
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
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	at := time.Unix(1_700_000_000, 0).UTC()
	ds.records["p1"] = &substrate.Record{
		ID: "p1", Kind: "people.substrate.reamde.dev/person",
		Properties: map[string]any{"name": "Sam"},
	}
	ds.meta["p1"] = map[string]substrate.PropertyMeta{
		"name": {Manager: "owner", UpdatedAt: at, Alternatives: []substrate.PropertyAlternative{
			{Actor: "google.connectors.substrate.reamde.dev/people", Value: "Samuel Jones", UpdatedAt: at},
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

	// The list projection stays lean: no provenance on collection reads.
	rec = env.do(t, http.MethodGet, peoplePath, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "propertyMeta") {
		t.Fatalf("list carries propertyMeta: %s", rec.Body.String())
	}
}

// Reverse edges page independently from the canonical record read.
func TestRESTIncomingIsSeparateAndPaged(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	ds.records["p1"] = &substrate.Record{
		ID: "p1", Kind: "people.substrate.reamde.dev/person",
		Properties: map[string]any{"name": "Sam"},
	}
	ds.incoming["p1"] = []substrate.IncomingEdge{
		{Rel: "person", From: substrate.EdgeTarget{
			ID: "people-c1001", Kind: "google.connectors.substrate.reamde.dev/contact", Title: "Samuel Jones",
		}},
	}

	rec := env.do(t, http.MethodGet, peoplePath+"/p1", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	// The record manifest never carries reverse edges: the
	// Record.Incoming field was removed at the v1 freeze; the
	// paged resource below is the only way to read them.
	_ = decodeJSON[substrate.Record](t, rec)

	rec = env.do(t, http.MethodGet, peoplePath+"/p1/incoming?first=1", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.IncomingPage](t, rec)
	if page.Total != 1 || len(page.Incoming) != 1 || page.Incoming[0].From.ID != "people-c1001" {
		t.Fatalf("incoming page = %+v", page)
	}
}

func TestRESTListForcesTheCollectionType(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	// The path names the type. A filter that leaves `types` unset carries its
	// other arms through; the collection type is applied on top.
	filter := substrate.Filter{
		Properties: map[string]substrate.Cond{"company": {Eq: "Analytical"}},
	}
	raw, err := json.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("%s?filter=%s&orderBy=%s&first=7&after=cur1",
		peoplePath, url.QueryEscape(string(raw)), url.QueryEscape("at:desc,created_at"))

	rec := env.do(t, http.MethodGet, path, tok, nil)
	wantStatus(t, rec, http.StatusOK)

	q := ds.lastQuery
	if len(q.Filter.Kinds) != 1 || q.Filter.Kinds[0] != "people.substrate.reamde.dev/person" {
		t.Fatalf("filter types = %v, want the collection's type", q.Filter.Kinds)
	}
	if q.Filter.Properties["company"].Eq != "Analytical" {
		t.Fatalf("filter props lost: %+v", q.Filter.Properties)
	}
	want := []substrate.Order{{Property: "at", Desc: true}, {Property: "created_at"}}
	if len(q.OrderBy) != 2 || q.OrderBy[0] != want[0] || q.OrderBy[1] != want[1] {
		t.Fatalf("orderBy = %+v, want %+v", q.OrderBy, want)
	}
	if q.First != 7 || q.After != "cur1" {
		t.Fatalf("paging = first %d after %q", q.First, q.After)
	}
}

// A caller-supplied filter.types that conflicts with the path is not silently
// overwritten — it is a bad_request naming the param: silent
// override let a client believe it had filtered when it had not.
func TestRESTListRejectsConflictingFilterTypes(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	filter := substrate.Filter{Kinds: []string{"messaging.substrate.reamde.dev/conversationmessage"}}
	raw, err := json.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	rec := env.do(t, http.MethodGet, peoplePath+"?filter="+url.QueryEscape(string(raw)), tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

func TestRESTBadFilterIsBadRequest(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, peoplePath+"?filter=%7Bnot-json", tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

func TestRESTErrorEnvelopeMapping(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

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
		{"internal", errBoom, http.StatusInternalServerError, codeInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ds.errs["Put"] = tc.err
			defer delete(ds.errs, "Put")
			rec := env.do(t, http.MethodPost, peoplePath, tok, map[string]any{"properties": map[string]any{"title": "x"}})
			wantErrorCode(t, rec, tc.status, tc.code)
		})
	}

	ds.errs["Put"] = &substrate.ValidationError{Problems: []string{"name: required", "asin: malformed"}}
	defer delete(ds.errs, "Put")
	rec := env.do(t, http.MethodPost, peoplePath, tok, map[string]any{"properties": map[string]any{"title": "x"}})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	env2 := decodeJSON[errorEnvelope](t, rec)
	if len(env2.Error.Problems) != 2 || env2.Error.Problems[0] != "name: required" {
		t.Fatalf("problems = %v", env2.Error.Problems)
	}
}
