package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const peopleV1 = "/api/v1/people.substrate.reamde.dev/person"

// A reference is written like every other property: one put carries the
// pointer, and a second put drops it by writing null. There is no link verb
// and no unlink verb, so a put is the whole mutation surface for a pointer.
func TestRESTReferenceIsWrittenAsAProperty(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	ds.records["p1"] = &substrate.Record{
		ID: "p1", Kind: "people.substrate.reamde.dev/person", Version: 1,
		Properties: map[string]any{"name": "Sam"},
	}

	rec := env.do(t, http.MethodPut, peopleV1+"/p1", tok, map[string]any{
		"properties": map[string]any{"name": "Sam", "manager": "people.substrate.reamde.dev/person/p2"},
	})
	wantStatus(t, rec, http.StatusOK)
	e := decodeJSON[substrate.Record](t, rec)
	// The put sent the bare path (write-time shorthand) and the read serves
	// the object it normalized to (0044).
	manager, _ := e.Properties["manager"].(map[string]any)
	if manager["ref"] != "people.substrate.reamde.dev/person/p2" {
		t.Fatalf("after the put, properties = %+v", e.Properties)
	}

	rec = env.do(t, http.MethodPatch, peopleV1+"/p1", tok, map[string]any{
		"properties": map[string]any{"manager": nil},
	})
	wantStatus(t, rec, http.StatusOK)
	if got := ds.lastPatch.Properties["manager"]; got != nil {
		t.Fatalf("the patch must carry the null that drops the pointer, got %v", got)
	}
}

// The edge routes are GONE, not tombstoned: no route binds
// `…/{id}/edges/{rel}` in either method, so the path is the router's 404.
func TestRESTEdgeRoutesAreGone(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	env.svc.datasets["geoah"].records["p1"] = &substrate.Record{ID: "p1", Kind: "people.substrate.reamde.dev/person"}

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		rec := env.do(t, method, peopleV1+"/p1/edges/member_of", tok, map[string]any{"id": "org1"})
		wantStatus(t, rec, http.StatusNotFound)
	}
}

// `edges` no longer names a sub-resource, so it is no longer a reserved record
// id: a record may be called `edges` and read back at its own path.
func TestRESTEdgesIsAnOrdinaryRecordID(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	rec := env.do(t, http.MethodPut, peopleV1+"/edges", tok, map[string]any{
		"properties": map[string]any{"name": "Sam"},
	})
	wantStatus(t, rec, http.StatusCreated)
	rec = env.do(t, http.MethodGet, peopleV1+"/edges", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if e := decodeJSON[substrate.Record](t, rec); e.ID != "edges" {
		t.Fatalf("record = %+v", e)
	}

	// `incoming` still IS one, in both directions.
	rec = env.do(t, http.MethodPut, peopleV1+"/incoming", tok, map[string]any{"properties": map[string]any{}})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

// A body that still writes `edges` beside `properties` is refused NAMING the
// key AND what replaced it: the strict decode is what stops a pointer write
// from landing under a spelling nothing reads, and a raw HTTP client has no
// substratectl to tell it where the data goes now. Both write verbs, because
// only one of them was pinned and the two decode the same body.
func TestRESTRefusesAnEdgesKeyNamingItsReplacement(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	edges := map[string]any{"member_of": []any{map[string]any{"id": "org1"}}}

	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		rec := env.do(t, method, peopleV1+"/p1", tok, map[string]any{
			"properties": map[string]any{"name": "Sam"},
			"edges":      edges,
		})
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		msg := decodeJSON[errorEnvelope](t, rec).Error.Message
		for _, want := range []string{`"edges"`, "data.properties", "type: reference"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("%s error must name %s: %q", method, want, msg)
			}
		}
	}
}

// TestTriggerVerbsLiveUnderCore is ruling A8's verb placement: the trigger
// verbs answer AT the resource, and trigger records are CORE's — the substrate
// maintains its own delivery plumbing, so it publishes it (the former
// automation.substrate.reamde.dev folded into core, 2026-08-12, and that path is gone
// rather than deprecated). The fake runs no triggers, so the route answers 501
// unsupported — which still proves it reached the trigger handler (an unknown
// collection would be 404).
func TestTriggerVerbsLiveUnderCore(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	rec := env.do(t, http.MethodGet, "/api/v1/core.substrate.reamde.dev/trigger/status", tok, nil)
	wantErrorCode(t, rec, http.StatusNotImplemented, codeUnsupported)
	if w := rec.Header().Get("Warning"); w != "" {
		t.Fatalf("the resource path must not carry a deprecation Warning: %q", w)
	}

	// The folded-away authority is not a route: it resolves as an unknown
	// collection, never as a second spelling of the verbs.
	rec = env.do(t, http.MethodGet, "/api/v1/automation.substrate.reamde.dev/triggers/status", tok, nil)
	if rec.Code == http.StatusNotImplemented {
		t.Fatal("the retired automation.substrate.reamde.dev path still reaches the trigger verbs")
	}
}

// TestWatchRejectsListParams and TestIncomingRejectsListParams are ruling A8's
// unsupported-param rule: a param a mode does not honor is a bad_request naming
// it, not a silent success.
func TestWatchRejectsListParams(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, peopleV1+"?watch=1&orderBy=at:desc", tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if msg := decodeJSON[errorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "orderBy") {
		t.Fatalf("error must name orderBy: %q", msg)
	}
}

func TestIncomingRejectsListParams(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	env.svc.datasets["geoah"].records["p1"] = &substrate.Record{ID: "p1", Kind: "people.substrate.reamde.dev/person"}
	rec := env.do(t, http.MethodGet, peopleV1+"/p1/incoming?filter=%7B%7D", tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if msg := decodeJSON[errorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "filter") {
		t.Fatalf("error must name filter: %q", msg)
	}
}
