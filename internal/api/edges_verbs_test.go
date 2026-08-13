package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const peopleV1 = "/api/v1/people.substrate.reamde.dev/people"

// TestRESTEdgeLinkUnlinkRoundTrip is ruling A8's edge verbs: a REST client can
// now REMOVE an edge, not only add one on a put. POST …/{id}/edges/{rel} links,
// DELETE unlinks, and each returns the refreshed source record.
func TestRESTEdgeLinkUnlinkRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	ds.records["p1"] = &substrate.Record{
		ID: "p1", Kind: "people.substrate.reamde.dev/person", Properties: map[string]any{"name": "Sam"},
	}

	rec := env.do(t, http.MethodPost, peopleV1+"/p1/edges/member_of", tok, map[string]any{"id": "org1"})
	wantStatus(t, rec, http.StatusOK)
	e := decodeJSON[substrate.Record](t, rec)
	if len(e.Edges["member_of"]) != 1 || e.Edges["member_of"][0].ID != "org1" {
		t.Fatalf("after link, edges = %+v", e.Edges)
	}

	rec = env.do(t, http.MethodDelete, peopleV1+"/p1/edges/member_of", tok, map[string]any{"id": "org1"})
	wantStatus(t, rec, http.StatusOK)
	e = decodeJSON[substrate.Record](t, rec)
	if len(e.Edges["member_of"]) != 0 {
		t.Fatalf("after unlink, edges = %+v", e.Edges)
	}
}

func TestRESTEdgeLinkNeedsATarget(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	env.svc.datasets["geoah"].records["p1"] = &substrate.Record{ID: "p1", Kind: "people.substrate.reamde.dev/person"}
	rec := env.do(t, http.MethodPost, peopleV1+"/p1/edges/member_of", tok, map[string]any{})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
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

	rec := env.do(t, http.MethodGet, "/api/v1/core.substrate.reamde.dev/triggers/status", tok, nil)
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
