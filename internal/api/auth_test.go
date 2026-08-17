package api

import (
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestAuthMissingBearer(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/api/v1/people.substrate.reamde.dev/person", "", nil)
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

func TestAuthUnknownToken(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/api/v1/people.substrate.reamde.dev/person", "substrate_tok_nope", nil)
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

// The actor is ATTRIBUTION: the header names what wrote, defaulting to the
// owner, and the token constrains nothing.
func TestActorDefaultsToOwnerAndHeaderNamesTheWriter(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	rec := env.do(t, http.MethodPost, "/api/v1/people.substrate.reamde.dev/person", tok,
		map[string]any{"properties": map[string]any{"name": "Ada"}})
	wantStatus(t, rec, http.StatusCreated)
	if ds.lastActor != substrate.ActorAPI {
		t.Fatalf("default actor = %q, want owner", ds.lastActor)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/people.substrate.reamde.dev/person", tok,
		map[string]any{"properties": map[string]any{"name": "Grace"}},
		actorHeader, "gmail.google.connectors.substrate.reamde.dev")
	wantStatus(t, rec, http.StatusCreated)
	if ds.lastActor != "gmail.google.connectors.substrate.reamde.dev" {
		t.Fatalf("header actor = %q", ds.lastActor)
	}
}

// The actor is asserted, the PRINCIPAL is resolved: whatever door a caller
// names, the write carries the id of the token the bearer secret resolved to,
// and the engine stamps that on the changelog entry (#102). Two tokens of the
// same repository writing under the same actor are therefore still told
// apart.
func TestWriteCarriesTheResolvedTokenID(t *testing.T) {
	env := newTestEnv(t)
	first := env.svc.token("geoah")
	second := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	rec := env.do(t, http.MethodPost, "/api/v1/people.substrate.reamde.dev/person", first,
		map[string]any{"properties": map[string]any{"name": "Ada"}})
	wantStatus(t, rec, http.StatusCreated)
	if ds.lastPrincipal != env.svc.tokens[first].info.ID {
		t.Fatalf("principal = %q, want the first token's id %q", ds.lastPrincipal, env.svc.tokens[first].info.ID)
	}

	// A different token, the same asserted actor: the principal moves, the
	// actor does not.
	rec = env.do(t, http.MethodPost, "/api/v1/people.substrate.reamde.dev/person", second,
		map[string]any{"properties": map[string]any{"name": "Grace"}},
		actorHeader, string(substrate.ActorConsole))
	wantStatus(t, rec, http.StatusCreated)
	if ds.lastActor != substrate.ActorConsole {
		t.Fatalf("actor = %q, want console", ds.lastActor)
	}
	if ds.lastPrincipal != env.svc.tokens[second].info.ID {
		t.Fatalf("principal = %q, want the second token's id %q", ds.lastPrincipal, env.svc.tokens[second].info.ID)
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
		rec := env.do(t, http.MethodPost, "/api/v1/people.substrate.reamde.dev/person", tok,
			map[string]any{"properties": map[string]any{"name": "forged"}},
			actorHeader, actor)
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
	}
	// A name that merely RESEMBLES the namespace is ordinary.
	rec := env.do(t, http.MethodPost, "/api/v1/people.substrate.reamde.dev/person", tok,
		map[string]any{"properties": map[string]any{"name": "fine"}},
		actorHeader, "substrateish.example.com")
	wantStatus(t, rec, http.StatusCreated)
}
