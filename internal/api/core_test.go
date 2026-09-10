package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

const tokensPath = "/tokens"

// Minting is authenticated and unmetered: the rate limit guards the DOOR
// (registration and login), not a caller who already holds a token.
func TestMintTokenIsAuthenticatedAndUnmetered(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	for range 3 {
		rec := env.do(t, http.MethodPost, tokensPath, tok, map[string]any{"label": "scripted"})
		wantStatus(t, rec, http.StatusCreated)
		out := decodeJSON[substrate.MintedToken](t, rec)
		if out.Secret == "" || out.Token.Label != "scripted" {
			t.Fatalf("minted %+v", out)
		}
	}
	// Without a bearer it is an ordinary auth failure — there is no
	// unauthenticated mint any more.
	wantErrorCode(t, env.do(t, http.MethodPost, tokensPath, "", map[string]any{"label": "x"}),
		http.StatusUnauthorized, codeAuth)
}

// The mint body's `expiresAt` reaches the dataset. The engine's own expiry
// test mints through the Go surface, so without this nothing proves the HTTP
// door decodes the key and hands it down: a handler that dropped it would
// mint a token that never expires and still answer 201.
func TestMintTokenCarriesTheExpiryToTheDataset(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	// The fake's MintToken echoes the expiry it was handed, so the answer is
	// the assertion: an expiry in the reply is one the dataset received.
	expiresAt := time.Date(2027, 3, 4, 5, 6, 7, 0, time.UTC)
	rec := env.do(t, http.MethodPost, tokensPath, tok,
		map[string]any{"label": "expiring", "expiresAt": expiresAt.Format(time.RFC3339)})
	wantStatus(t, rec, http.StatusCreated)
	out := decodeJSON[substrate.MintedToken](t, rec)
	if out.Token.ExpiresAt == nil || !out.Token.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("the dataset was handed expiry %v, want %v", out.Token.ExpiresAt, expiresAt)
	}
	// A mint that names no expiry hands down none. A zero time here would be
	// an expiry in the past, which authenticate refuses.
	rec = env.do(t, http.MethodPost, tokensPath, tok, map[string]any{"label": "immortal"})
	wantStatus(t, rec, http.StatusCreated)
	if got := decodeJSON[substrate.MintedToken](t, rec).Token.ExpiresAt; got != nil {
		t.Fatalf("a mint with no expiresAt handed down %v", got)
	}
}

func TestTokenListAndRevoke(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)

	rec := env.do(t, http.MethodGet, tokensPath, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[substrate.OperationalList[substrate.TokenInfo]](t, rec)
	if len(out.Items) != 1 || out.Items[0].Label != "console" {
		t.Fatalf("tokens = %+v", out.Items)
	}

	// Revoking IS deleting the record — the same write the generic record
	// surface performs.
	ds := env.svc.datasets[fakeRepository]
	ds.records["tok1"] = &substrate.Record{ID: "tok1", Kind: "substrate.reamde.dev/core/token"}
	rec = env.do(t, http.MethodDelete, tokensPath+"/tok1", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if ds.lastDeleteType != "substrate.reamde.dev/core/token" || ds.lastDeleteID != "tok1" {
		t.Fatalf("revoke deleted %q/%q", ds.lastDeleteType, ds.lastDeleteID)
	}
}

// The repository-management endpoints (create, list, rotate) went with the control
// plane that gated them, and B3 did not bring them back: registration is
// `/register` behind the invite code, and `repositories` is an ordinary system-type
// collection the engine refuses generic writes to.
func TestRepositoryCollectionHasNoInstallVerb(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	rec := env.do(t, http.MethodPost, "/api/v1/substrate.reamde.dev/core/repositories/geoah/rotateotp", tok, map[string]any{})
	wantStatus(t, rec, http.StatusNotFound)
}

func TestMergeSplit(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)

	rec := env.do(t, http.MethodPost, "/api/v1/merge", tok,
		map[string]any{"kind": "samples.substrate.reamde.dev/people/person", "winner": "a1", "loser": "b2"})
	wantStatus(t, rec, http.StatusCreated)
	merged := decodeJSON[substrate.Record](t, rec)
	// The merge record names both sides with reference properties, each served
	// as the object holding the full record path under `ref` (0044).
	winner, _ := merged.Properties["winner"].(map[string]any)
	loser, _ := merged.Properties["loser"].(map[string]any)
	if winner["ref"] != "samples.substrate.reamde.dev/people/person/a1" ||
		loser["ref"] != "samples.substrate.reamde.dev/people/person/b2" {
		t.Fatalf("merge record properties = %v", merged.Properties)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/split", tok, map[string]any{"merge": "merge1"})
	wantStatus(t, rec, http.StatusCreated)

	// There is no identify: nothing matches records by value.
	rec = env.do(t, http.MethodPost, "/api/v1/substrate.reamde.dev/core/identifications", tok, map[string]any{
		"kind": "samples.substrate.reamde.dev/people/person",
	})
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}
