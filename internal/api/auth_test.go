package api

import (
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestAuthMissingBearer(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/api/v1/people.substrate.geoah.me/people", "", nil)
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

func TestAuthUnknownToken(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/api/v1/people.substrate.geoah.me/people", "substrate_tok_nope", nil)
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

// The actor is ATTRIBUTION: the header names what wrote, defaulting to the
// owner, and the token constrains nothing.
func TestActorDefaultsToOwnerAndHeaderNamesTheWriter(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	rec := env.do(t, http.MethodPost, "/api/v1/people.substrate.geoah.me/people", tok,
		map[string]any{"properties": map[string]any{"name": "Ada"}})
	wantStatus(t, rec, http.StatusCreated)
	if ds.lastActor != substrate.ActorAPI {
		t.Fatalf("default actor = %q, want owner", ds.lastActor)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/people.substrate.geoah.me/people", tok,
		map[string]any{"properties": map[string]any{"name": "Grace"}},
		actorHeader, "gmail.google.connectors.substrate.reamde.dev")
	wantStatus(t, rec, http.StatusCreated)
	if ds.lastActor != "gmail.google.connectors.substrate.reamde.dev" {
		t.Fatalf("header actor = %q", ds.lastActor)
	}
}

// The one refusal on that header: the host's own namespace. Those actor names
// are decided by NAME EQUALITY at write time — `substrate.oauth` is the only
// hand that may write a connected account's credential ref — so a request
// allowed to claim one could forge exactly what the facility exists to own.
func TestActorHeaderRefusesTheHostNamespace(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	for _, actor := range []string{
		"substrate",          // the bare namespace label
		"substrate.oauth",    // the OAuth facility's hand
		"substrate.engine",   // the engine's own
		"substrate.anything", // anything else under it
	} {
		rec := env.do(t, http.MethodPost, "/api/v1/people.substrate.geoah.me/people", tok,
			map[string]any{"properties": map[string]any{"name": "forged"}},
			actorHeader, actor)
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
	}
	// A name that merely RESEMBLES the namespace is ordinary.
	rec := env.do(t, http.MethodPost, "/api/v1/people.substrate.geoah.me/people", tok,
		map[string]any{"properties": map[string]any{"name": "fine"}},
		actorHeader, "substrateish.example.com")
	wantStatus(t, rec, http.StatusCreated)
}
