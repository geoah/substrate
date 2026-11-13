package api

import (
	"net/http"
	"testing"
)

func TestAuthMissingBearer(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodGet, recordsPath, "", nil)
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

func TestAuthUnknownToken(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodGet, recordsPath, "substrate_tok_nope", nil)
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

// The one refusal on that header: the host's own namespace. Those actor names
// are decided by NAME EQUALITY at write time — `substrate.oauth` is the only
// hand that may write a connected account's credential ref — so a request
// allowed to claim one could forge exactly what the facility exists to own.
// The door refuses before the dataset is reached: the stub has no Put, so a
// request that got past it would answer 500. A name that merely resembles
// the namespace is written under it on the engine
// (TestActorHeaderAdmitsALookalikeOfTheHostNamespace).
func TestActorHeaderRefusesTheHostNamespace(t *testing.T) {
	env := newStubEnv(t, nil)
	for _, actor := range []string{
		"substrate",          // the bare namespace label
		"substrate.oauth",    // the OAuth facility's hand
		"substrate.engine",   // the engine's own
		"substrate.anything", // anything else under it
		// A bundle's derived hand: it writes the declarations it ships, so a
		// request that could claim it could forge one.
		"bundle:samples.substrate.reamde.dev:tasks",
	} {
		rec := env.do(t, http.MethodPost, recordsPath, stubToken,
			map[string]any{"kind": personKind, "properties": map[string]any{"name": "forged"}},
			actorHeader, actor)
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
	}
}
