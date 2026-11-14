package api

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// Minting is authenticated and unmetered: the rate limit guards the DOOR
// (registration and login), not a caller who already holds a token. Each mint
// stores a token that opens the repository.
func TestMintTokenIsAuthenticatedAndUnmetered(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	for range 3 {
		rec := env.do(t, http.MethodPost, tokensPath, env.token, map[string]any{"label": "scripted"})
		wantStatus(t, rec, http.StatusCreated)
		out := decodeJSON[substrate.MintedToken](t, rec)
		if out.Secret == "" || out.Token.Label != "scripted" {
			t.Fatalf("minted %+v", out)
		}
		wantStatus(t, env.do(t, http.MethodGet, tokensPath, out.Secret, nil), http.StatusOK)
	}
	wantErrorCode(t, env.do(t, http.MethodPost, tokensPath, "", map[string]any{"label": "x"}),
		http.StatusUnauthorized, codeAuth)
}

// The mint body's `expiresAt` reaches the engine. The engine's own expiry
// test mints through the Go surface, so without this nothing proves the HTTP
// door decodes the key and hands it down: a handler that dropped it would
// mint a token that never expires and still answer 201.
func TestMintTokenCarriesTheExpiryToTheDataset(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	stored := func(id string) *substrate.TokenInfo {
		t.Helper()
		tokens, err := env.ds.Tokens(t.Context())
		if err != nil {
			t.Fatalf("tokens: %v", err)
		}
		for i := range tokens {
			if tokens[i].ID == id {
				return &tokens[i]
			}
		}
		t.Fatalf("token %s is not stored", id)
		return nil
	}
	expiresAt := env.clock.now().Add(30 * 24 * time.Hour)
	rec := env.do(t, http.MethodPost, tokensPath, env.token,
		map[string]any{"label": "expiring", "expiresAt": expiresAt.Format(time.RFC3339)})
	wantStatus(t, rec, http.StatusCreated)
	out := decodeJSON[substrate.MintedToken](t, rec)
	if out.Token.ExpiresAt == nil || !out.Token.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("the mint answered expiry %v, want %v", out.Token.ExpiresAt, expiresAt)
	}
	if got := stored(out.Token.ID).ExpiresAt; got == nil || !got.Equal(expiresAt) {
		t.Fatalf("the engine stored expiry %v, want %v", got, expiresAt)
	}
	// A mint that names no expiry hands down none. A zero time here would be
	// an expiry in the past, which authenticate refuses.
	rec = env.do(t, http.MethodPost, tokensPath, env.token, map[string]any{"label": "immortal"})
	wantStatus(t, rec, http.StatusCreated)
	out = decodeJSON[substrate.MintedToken](t, rec)
	if out.Token.ExpiresAt != nil {
		t.Fatalf("a mint with no expiresAt answered %v", out.Token.ExpiresAt)
	}
	if got := stored(out.Token.ID).ExpiresAt; got != nil {
		t.Fatalf("a mint with no expiresAt stored %v", got)
	}
	wantStatus(t, env.do(t, http.MethodGet, tokensPath, out.Secret, nil), http.StatusOK)
}

// The listing is the repository's tokens, and revoking IS deleting the token
// record, the same write the generic record surface performs: the revoked
// secret opens nothing afterwards.
func TestTokenListAndRevoke(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	labels := func() []string {
		t.Helper()
		rec := env.do(t, http.MethodGet, tokensPath, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		var out []string
		for _, tok := range decodeJSON[substrate.OperationalList[substrate.TokenInfo]](t, rec).Items {
			out = append(out, tok.Label)
		}
		return out
	}
	if got := labels(); !slices.Equal(got, []string{"test"}) {
		t.Fatalf("tokens = %v, want the one the test minted", got)
	}

	rec := env.do(t, http.MethodPost, tokensPath, env.token, map[string]any{"label": "doomed"})
	wantStatus(t, rec, http.StatusCreated)
	doomed := decodeJSON[substrate.MintedToken](t, rec)
	if got := labels(); !slices.Contains(got, "doomed") {
		t.Fatalf("tokens = %v, want the minted one listed", got)
	}
	rec = env.do(t, http.MethodDelete, tokensPath+"/"+doomed.Token.ID, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.Record](t, rec); got.Kind != tokenType || got.ID != doomed.Token.ID || got.DeletedAt == nil {
		t.Fatalf("revoke answered %s/%s deleted at %v", got.Kind, got.ID, got.DeletedAt)
	}
	if got := labels(); slices.Contains(got, "doomed") {
		t.Fatalf("tokens = %v, the revoked one still listed", got)
	}
	wantErrorCode(t, env.do(t, http.MethodGet, tokensPath, doomed.Secret, nil), http.StatusUnauthorized, codeAuth)
}

// A merge joins two records of one kind and answers its recordmerge record,
// which names both sides with reference properties, each served as the object
// holding the full record path under `ref` (0044); a split reverses it.
func TestMergeSplit(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	env.put(t, personKind, "a1", map[string]any{"name": "Ada"})
	env.put(t, personKind, "b2", map[string]any{"name": "A. Lovelace"})

	rec := env.do(t, http.MethodPost, "/api/v1/merge", env.token,
		map[string]any{"kind": personKind, "winner": "a1", "loser": "b2"})
	wantStatus(t, rec, http.StatusCreated)
	merged := decodeJSON[substrate.Record](t, rec)
	winner, _ := merged.Properties["winner"].(map[string]any)
	loser, _ := merged.Properties["loser"].(map[string]any)
	if winner["ref"] != personKind+"/a1" || loser["ref"] != personKind+"/b2" {
		t.Fatalf("merge record properties = %v", merged.Properties)
	}
	// The loser's id now resolves to the winner, or to nothing live.
	if got := env.get(t, personKind, "b2"); got != nil && got.DeletedAt == nil && got.CanonicalID == "" {
		t.Fatalf("the merge left the loser live: %+v", got)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/split", env.token, map[string]any{"merge": merged.ID})
	wantStatus(t, rec, http.StatusCreated)
	if got := env.get(t, personKind, "b2"); got == nil || got.DeletedAt != nil {
		t.Fatalf("the split did not bring the loser back: %+v", got)
	}
}
