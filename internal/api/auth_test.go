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

// retiredActorHeader is the header a client once named its actor by. The
// server reads it no more (decision 0270); the tests send it to show that.
const retiredActorHeader = "X-Substrate-Actor"
