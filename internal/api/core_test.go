package api

import (
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const tokensPath = "/tokens"

// Minting is authenticated and unmetered: the rate limit guards the DOOR
// (registration and login), not a caller who already holds a token.
func TestMintTokenIsAuthenticatedAndUnmetered(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	for range 3 {
		rec := env.do(t, http.MethodPost, tokensPath, tok, map[string]any{"label": "scripted"})
		wantStatus(t, rec, http.StatusCreated)
		out := decodeJSON[tokenResponse](t, rec)
		if out.Secret == "" || out.Token.Label != "scripted" {
			t.Fatalf("minted %+v", out)
		}
	}
	// Without a bearer it is an ordinary auth failure — there is no
	// unauthenticated mint any more.
	wantErrorCode(t, env.do(t, http.MethodPost, tokensPath, "", map[string]any{"label": "x"}),
		http.StatusUnauthorized, codeAuth)
}

func TestTokenListAndRevoke(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	rec := env.do(t, http.MethodGet, tokensPath, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[tokenListResponse](t, rec)
	if len(out.Tokens) != 1 || out.Tokens[0].Label != "console" {
		t.Fatalf("tokens = %+v", out.Tokens)
	}

	// Revoking IS deleting the record — the same write the generic record
	// surface performs.
	ds := env.svc.datasets["geoah"]
	ds.records["tok1"] = &substrate.Record{ID: "tok1", Kind: "core.substrate.reamde.dev/token"}
	rec = env.do(t, http.MethodDelete, tokensPath+"/tok1", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if ds.lastDeleteType != "core.substrate.reamde.dev/token" || ds.lastDeleteID != "tok1" {
		t.Fatalf("revoke deleted %q/%q", ds.lastDeleteType, ds.lastDeleteID)
	}
}

// The repository-management endpoints (create, list, rotate) went with the control
// plane that gated them, and B3 did not bring them back: registration is
// `/register` behind the invite code, and `repositories` is an ordinary system-type
// collection the engine refuses generic writes to.
func TestRepositoryCollectionHasNoInstallVerb(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodPost, "/api/v1/core.substrate.reamde.dev/repositories/geoah/rotateotp", tok, map[string]any{})
	wantStatus(t, rec, http.StatusNotFound)
}

func TestMergeSplit(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	rec := env.do(t, http.MethodPost, "/api/v1/-/recordmerges", tok,
		map[string]any{"kind": "people.substrate.reamde.dev/person", "winner": "a1", "loser": "b2"})
	wantStatus(t, rec, http.StatusCreated)
	merged := decodeJSON[substrate.Record](t, rec)
	if len(merged.Edges["winner"]) != 1 || merged.Edges["winner"][0].ID != "a1" ||
		len(merged.Edges["loser"]) != 1 || merged.Edges["loser"][0].ID != "b2" {
		t.Fatalf("merge record edges = %v", merged.Edges)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/-/recordsplits", tok, map[string]any{"merge": "merge1"})
	wantStatus(t, rec, http.StatusCreated)

	// There is no identify: nothing matches records by value.
	rec = env.do(t, http.MethodPost, "/api/v1/core.substrate.reamde.dev/identifications", tok, map[string]any{
		"kind": "people.substrate.reamde.dev/person",
	})
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}
