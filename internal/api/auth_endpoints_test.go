package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The door's own refusals and its discovery answer, on the stub: what a
// registration, a login or a credential change does to an account is the
// engine's, in auth_endpoints_db_test.go.

// With an invite code configured, a wrong one is an auth failure that never
// reaches the service: the stub has no BeginRegistration, so a request that
// got past the gate would answer 500.
func TestRegistrationIsGatedByTheInviteCode(t *testing.T) {
	env := newStubEnvOn(t, &stubService{})
	rec := env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
		"inviteCode": "wrong", "repository": "ada",
	})
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

// The password-factor rule at the HTTP door: a bearer token alone is refused
// with 403 — the endpoint does not accept tokens at all — before the service
// is reached, which the stub would answer 500. That the change goes through
// with both current factors in the body is the engine's
// (TestCredentialChangeTakesBothCurrentFactors).
func TestCredentialChangesRefuseABearerTokenAlone(t *testing.T) {
	env := newStubEnv(t, nil)
	for path, body := range map[string]map[string]any{
		"/password":          {"repository": stubRepository, "newPassword": "a-brand-new-passphrase"},
		"/totp/enroll":       {"repository": stubRepository},
		"/repository/delete": {"repository": stubRepository},
		"/totp": {
			"repository": stubRepository, "newTotpSecret": "JBSWY3DPEHPK3PXP",
			"newTotpCode": "123456",
		},
	} {
		rec := env.do(t, http.MethodPost, path, stubToken, body)
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
		env.clock.advance(defaultAuthInterval + time.Millisecond)
	}
}

// The console owns the GET side of /login and /register: a wrong method on a
// non-API path serves the SPA, never a 405 the browser cannot render.
func TestConsoleRoutesFallThroughToTheSPA(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>console"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := newStubEnv(t, nil, func(c *Config) { c.WebDir = dir })

	for _, path := range []string{"/login", "/register"} {
		rec := env.do(t, http.MethodGet, path, "", nil)
		wantStatus(t, rec, http.StatusOK)
		if got := rec.Body.String(); got == "" || got[0] != '<' {
			t.Fatalf("GET %s served %q, want the console", path, got)
		}
	}
	// Under an API prefix the method really is wrong, and says so.
	rec := env.do(t, http.MethodDelete, recordsPath, stubToken, nil)
	wantStatus(t, rec, http.StatusMethodNotAllowed)
	// Discovery sits outside the API surface, the same class as /healthz: a
	// wrong method there falls to the SPA too, never a JSON 405.
	rec = env.do(t, http.MethodDelete, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	if got := rec.Body.String(); got == "" || got[0] != '<' {
		t.Fatalf("DELETE /.well-known/substrate/server.json served %q, want the console", got)
	}
}

// A deployment with no invite code configured says so in discovery, so a
// client stops asking a person for a code nothing reads.
func TestDiscoveryReportsNoInviteRequiredWithNoInviteCode(t *testing.T) {
	env := newStubEnvOn(t, &stubService{}, func(c *Config) { c.InviteCode = "" })

	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	doc := decodeJSON[map[string]any](t, rec)
	registration, _ := doc["registration"].(map[string]any)
	if registration == nil || registration["inviteRequired"] != false {
		t.Fatalf("discovery must report that no invite code is read: %+v", doc["registration"])
	}
}

// The ordinary test deployment carries an invite code, so discovery says one
// is required.
func TestDiscoveryReportsInviteRequiredWithInviteCode(t *testing.T) {
	env := newStubEnvOn(t, &stubService{})
	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	doc := decodeJSON[map[string]any](t, rec)
	registration, _ := doc["registration"].(map[string]any)
	if registration == nil || registration["inviteRequired"] != true {
		t.Fatalf("discovery must report the invite code as required: %+v", doc["registration"])
	}
}
