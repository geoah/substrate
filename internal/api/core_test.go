package api

import (
	"net/http"
	"testing"
)

const tokensPath = "/tokens"

// Minting needs a token already: without a bearer it is an ordinary auth
// failure, and there is no unauthenticated mint any more.
func TestMintTokenWithoutABearerIsRefused(t *testing.T) {
	env := newStubEnv(t, nil)
	wantErrorCode(t, env.do(t, http.MethodPost, tokensPath, "", map[string]any{"label": "x"}),
		http.StatusUnauthorized, codeAuth)
}

// The repository-management endpoints (create, list, rotate) went with the control
// plane that gated them, and B3 did not bring them back: registration is
// `/register` behind the invite code, and `repository` is an ordinary core
// kind the engine refuses generic writes to.
func TestRepositoryKindHasNoInstallVerb(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, "/api/v1/substrate.reamde.dev/core/repositories/geoah/rotateotp", stubToken, map[string]any{})
	wantStatus(t, rec, http.StatusNotFound)
}

// There is no identify: nothing matches records by value, so the path is the
// router's 404 and no dataset is reached.
func TestThereIsNoIdentify(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, "/api/v1/substrate.reamde.dev/core/identifications", stubToken, map[string]any{
		"kind": "samples.substrate.reamde.dev/people/person",
	})
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}
