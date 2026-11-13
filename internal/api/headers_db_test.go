package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// headersExportRefused is a repository whose export is refused as
// unavailable: a store that cannot be read at the moment, which the engine
// cannot be made to answer on demand.
type headersExportRefused struct{ substrate.Dataset }

func (headersExportRefused) Export(context.Context) (substrate.Export, error) {
	return nil, substrate.ErrUnavailable
}

// Every route that takes or hands back a credential answers no-store, the
// refusals among them: a rate-limited attempt, a wrong password and a missing
// bearer are responses a shared cache must not keep either. Each answer is
// the engine's, from registration through the second factor's swap to the
// export.
func TestCredentialResponsesAreNeverStored(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	repository := testdb.Repository(t)
	const password = "correct-horse-battery-staple"
	// Past the auth interval, and onto a fresh second-factor step: a code is
	// spent once.
	pace := func() { env.clock.advance(engine.TOTPPeriod) }

	wantNoStore := func(name string, rec *httptest.ResponseRecorder, status int) {
		t.Helper()
		wantStatus(t, rec, status)
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", name, got)
		}
	}

	rec := env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
		"inviteCode": testInviteCode, "repository": repository,
	})
	wantNoStore("register enroll (the TOTP seed)", rec, http.StatusOK)
	seed := decodeJSON[substrate.TOTPEnrollment](t, rec).Secret

	pace()
	rec = env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": testInviteCode, "repository": repository, "password": password,
		"totpSecret": seed, "totpCode": env.code(t, seed),
	})
	wantNoStore("register (the first secret and the recovery key)", rec, http.StatusCreated)
	tok := decodeJSON[substrate.Registered](t, rec).Secret

	pace()
	wantNoStore("login", env.do(t, http.MethodPost, loginPath, "",
		loginBody(repository, password, env.code(t, seed))), http.StatusCreated)
	wantNoStore("login, rate limited", env.do(t, http.MethodPost, loginPath, "",
		loginBody(repository, password, env.code(t, seed))), http.StatusTooManyRequests)
	pace()
	wantNoStore("login, wrong password", env.do(t, http.MethodPost, loginPath, "",
		loginBody(repository, "wrong", env.code(t, seed))), http.StatusUnauthorized)

	rec = env.do(t, http.MethodPost, "/tokens", tok, map[string]any{"label": "script"})
	wantNoStore("mint a token", rec, http.StatusCreated)
	minted := decodeJSON[substrate.MintedToken](t, rec)
	wantNoStore("list tokens", env.do(t, http.MethodGet, "/tokens", tok, nil), http.StatusOK)
	wantNoStore("revoke a token", env.do(t, http.MethodDelete, "/tokens/"+minted.Token.ID, tok, nil), http.StatusOK)
	wantNoStore("mint without a bearer", env.do(t, http.MethodPost, "/tokens", "", nil), http.StatusUnauthorized)

	pace()
	rec = env.do(t, http.MethodPost, "/totp/enroll", "", map[string]any{
		"repository": repository, "password": password, "totpCode": env.code(t, seed),
	})
	wantNoStore("TOTP enroll (the candidate seed)", rec, http.StatusOK)
	next := decodeJSON[substrate.TOTPEnrollment](t, rec).Secret
	pace()
	wantNoStore("TOTP swap", env.do(t, http.MethodPost, "/totp", "", map[string]any{
		"repository": repository, "password": password, "totpCode": env.code(t, seed),
		"newTotpSecret": next, "newTotpCode": env.code(t, next),
	}), http.StatusOK)
	pace()
	wantNoStore("password change", env.do(t, http.MethodPost, "/password", "", map[string]any{
		"repository": repository, "password": password, "totpCode": env.code(t, next),
		"newPassword": "a-brand-new-passphrase",
	}), http.StatusOK)

	// This substrate configures no OAuth, so the start is refused; the
	// route's header, not the flow, is what is under test.
	wantNoStore("oauth start", env.do(t, http.MethodPost, "/api/v1/oauth/start", tok,
		map[string]any{"record": "providers.substrate.reamde.dev/google/account/owner"}), http.StatusUnprocessableEntity)
	wantNoStore("oauth start without a bearer", env.do(t, http.MethodPost, "/api/v1/oauth/start", "", nil), http.StatusUnauthorized)
	wantNoStore("oauth callback", getCallback(callbackHandler(callbackRecord, nil, "https://console.example.com")), http.StatusOK)

	wantNoStore("export", env.do(t, http.MethodGet, "/api/v1/export", tok, nil), http.StatusOK)
	wantNoStore("export without a bearer", env.do(t, http.MethodGet, "/api/v1/export", "", nil), http.StatusUnauthorized)
	env.wrap = func(ds substrate.Dataset) substrate.Dataset { return headersExportRefused{ds} }
	wantNoStore("export, refused", env.do(t, http.MethodGet, "/api/v1/export", tok, nil), http.StatusServiceUnavailable)
}
