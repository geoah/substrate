package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The door end to end, as the B3 "done when" states it: register → login →
// write → mint → revoke → change the password with a token alone and see it
// refused.

func TestRegisterThenLogin(t *testing.T) {
	env := newTestEnv(t)

	// Step one issues the enrollment and writes nothing.
	rec := env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
		"inviteCode": testInviteCode, "repository": "ada",
	})
	wantStatus(t, rec, http.StatusOK)
	enrollment := decodeJSON[substrate.TOTPEnrollment](t, rec)
	if enrollment.Secret == "" || enrollment.URI == "" {
		t.Fatalf("enrollment = %+v", enrollment)
	}

	// Step two commits, and registration ends logged in.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": testInviteCode, "repository": "ada",
		"password":   "correct-horse-battery-staple",
		"totpSecret": enrollment.Secret, "totpCode": fakeCode("ada.example.com"),
		"label": "console",
	})
	wantStatus(t, rec, http.StatusCreated)
	out := decodeJSON[substrate.Registered](t, rec)
	if out.Secret == "" || out.Token.Label != "console" {
		t.Fatalf("registration response = %+v", out)
	}
	// A bare repository label is completed under the host the request reached
	// (httptest requests carry `example.com`), and the caller learns the
	// authority it got here.
	if out.Repository != "ada.example.com" {
		t.Fatalf("registered repository = %q, want the label completed under the request host", out.Repository)
	}

	// The token registration handed back is an ordinary bearer.
	wantStatus(t, env.do(t, http.MethodGet, recordsPath, out.Secret, nil),
		http.StatusOK)

	// Login mints another — sessions ARE token records.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, loginPath, "",
		loginBody("ada", "correct-horse-battery-staple", fakeCode("ada.example.com")))
	wantStatus(t, rec, http.StatusCreated)
	if login := decodeJSON[substrate.MintedToken](t, rec); login.Secret == out.Secret {
		t.Fatal("login handed back the registration's secret")
	}
}

// A repository name carrying a dot IS the authority: the host is not appended
// to it, and the engine's answer is echoed verbatim.
func TestRegistrationKeepsADottedRepositoryName(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": testInviteCode, "repository": " ada.example.org ",
		"password":   "correct-horse-battery-staple",
		"totpSecret": "SEED", "totpCode": fakeCode("ada.example.org"),
	})
	wantStatus(t, rec, http.StatusCreated)
	out := decodeJSON[substrate.Registered](t, rec)
	if out.Repository != "ada.example.org" {
		t.Fatalf("registered repository = %q, want the trimmed name the request sent", out.Repository)
	}
	if got := env.svc.datasets["ada.example.org"].Repository().Authority; got != "ada.example.org" {
		t.Fatalf("the repository was created under %q", got)
	}
}

// With an invite code configured, a wrong one is an auth failure that never
// reaches the service.
func TestRegistrationIsGatedByTheInviteCode(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
		"inviteCode": "wrong", "repository": "ada",
	})
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
	if env.svc.registerCalls != 0 {
		t.Fatalf("a bad invite code reached the service (%d calls)", env.svc.registerCalls)
	}
}

// With NO invite code configured the door reads none: a registration carrying
// an empty code, or any code at all, is admitted. This is the local substrate;
// discovery says so (TestDiscoveryReportsNoInviteRequiredWithNoInviteCode) so a
// client stops asking.
func TestRegistrationAdmitsWithoutACodeWhenNoneIsConfigured(t *testing.T) {
	svc := newFakeService()
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	env := &testEnv{svc: svc, h: New(Config{Service: svc, Now: clock.now}), clock: clock}
	rec := env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
		"inviteCode": "", "repository": "ada",
	})
	wantStatus(t, rec, http.StatusOK)
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": "anything-at-all", "repository": "ada",
		"password":   "correct-horse-battery-staple",
		"totpSecret": "SEED", "totpCode": fakeCode("ada.example.com"),
	})
	wantStatus(t, rec, http.StatusCreated)
	if svc.registerCalls != 2 {
		t.Fatalf("registerCalls = %d, want the enrollment and the commit to reach the service", svc.registerCalls)
	}
}

// The password-factor rule at the HTTP door: a bearer token
// alone is refused with 403 — the endpoint does not accept tokens at all —
// and the change goes through only with both current factors in the body.
func TestCredentialChangesRefuseABearerTokenAlone(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	password := env.svc.passwords[fakeRepository]

	for path, body := range map[string]map[string]any{
		"/password":    {"repository": fakeRepository, "newPassword": "a-brand-new-passphrase"},
		"/totp/enroll": {"repository": fakeRepository},
		"/totp": {
			"repository": fakeRepository, "newTotpSecret": "JBSWY3DPEHPK3PXP",
			"newTotpCode": "123456",
		},
	} {
		rec := env.do(t, http.MethodPost, path, tok, body)
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
		env.clock.advance(defaultAuthInterval + time.Millisecond)
	}

	// With both factors presented directly it works, token or no token.
	rec := env.do(t, http.MethodPost, "/password", "", map[string]any{
		"repository": fakeRepository, "password": password, "totpCode": fakeCode(fakeRepository),
		"newPassword": "a-brand-new-passphrase",
	})
	wantStatus(t, rec, http.StatusOK)
	if env.svc.passwords[fakeRepository] != "a-brand-new-passphrase" {
		t.Fatalf("the password did not change: %q", env.svc.passwords[fakeRepository])
	}

	// A wrong current password is refused whatever else is right.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/password", "", map[string]any{
		"repository": fakeRepository, "password": password, "totpCode": fakeCode(fakeRepository),
		"newPassword": "another-passphrase-again",
	})
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

func TestTOTPReenrollmentIsTwoSteps(t *testing.T) {
	env := newTestEnv(t)
	password := env.svc.passwords[fakeRepository]

	rec := env.do(t, http.MethodPost, "/totp/enroll", "", map[string]any{
		"repository": fakeRepository, "password": password, "totpCode": fakeCode(fakeRepository),
	})
	wantStatus(t, rec, http.StatusOK)
	enrollment := decodeJSON[substrate.TOTPEnrollment](t, rec)
	if enrollment.Secret == "" {
		t.Fatalf("enrollment = %+v", enrollment)
	}

	// The swap needs a code from the CANDIDATE seed beside the current
	// factors: nothing changes on a seed nobody holds.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/totp", "", map[string]any{
		"repository": fakeRepository, "password": password, "totpCode": fakeCode(fakeRepository),
		"newTotpSecret": enrollment.Secret, "newTotpCode": "",
	})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)

	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/totp", "", map[string]any{
		"repository": fakeRepository, "password": password, "totpCode": fakeCode(fakeRepository),
		"newTotpSecret": enrollment.Secret, "newTotpCode": "123456",
	})
	wantStatus(t, rec, http.StatusOK)
}

// Every refused factor answers the same way: which one was wrong, and whether
// the user exists at all, are not questions the door answers.
func TestLoginGivesNoExistenceOracle(t *testing.T) {
	env := newTestEnv(t)
	badPassword := env.do(t, http.MethodPost, loginPath, "", loginBody(fakeRepository, "wrong", fakeCode(fakeRepository)))
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	noUser := env.do(t, http.MethodPost, loginPath, "", loginBody("nosuch", "wrong", "000000"))

	wantErrorCode(t, badPassword, http.StatusUnauthorized, codeAuth)
	wantErrorCode(t, noUser, http.StatusUnauthorized, codeAuth)
	if badPassword.Body.String() != noUser.Body.String() {
		t.Fatalf("responses differ:\n%s\n%s", badPassword.Body.String(), noUser.Body.String())
	}
}

// The console owns the GET side of /login and /register: a wrong method on a
// non-API path serves the SPA, never a 405 the browser cannot render.
func TestConsoleRoutesFallThroughToTheSPA(t *testing.T) {
	svc := newFakeService()
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>console"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := &testEnv{svc: svc, h: New(Config{
		Service: svc, Now: clock.now, InviteCode: testInviteCode, WebDir: dir,
	}), clock: clock}

	for _, path := range []string{"/login", "/register"} {
		rec := env.do(t, http.MethodGet, path, "", nil)
		wantStatus(t, rec, http.StatusOK)
		if got := rec.Body.String(); got == "" || got[0] != '<' {
			t.Fatalf("GET %s served %q, want the console", path, got)
		}
	}
	// Under an API prefix the method really is wrong, and says so.
	rec := env.do(t, http.MethodDelete, recordsPath, svc.token(fakeRepository), nil)
	wantStatus(t, rec, http.StatusMethodNotAllowed)
	// Discovery sits outside the API surface, the same class as /healthz: a
	// wrong method there falls to the SPA too, never a JSON 405.
	rec = env.do(t, http.MethodDelete, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	if got := rec.Body.String(); got == "" || got[0] != '<' {
		t.Fatalf("DELETE /.well-known/substrate/server.json served %q, want the console", got)
	}
}

// THE DOOR WITH THE SECOND FACTOR OFF (SUBSTRATE_INSECURE_DISABLE_TOTP): the
// deployment says so in discovery so a client stops asking, the credential
// changes take the password ALONE — and the password-factor rule itself is
// untouched, because it was never about the code: a request with no password
// is still refused outright.
func TestTOTPDisabledDoorTakesThePasswordAlone(t *testing.T) {
	svc := newFakeService()
	svc.totpDisabled = true
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	env := &testEnv{svc: svc, clock: clock, h: New(Config{
		Service: svc, Now: clock.now, InviteCode: testInviteCode, TOTPDisabled: true,
	})}

	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	doc := decodeJSON[map[string]any](t, rec)
	registration, _ := doc["registration"].(map[string]any)
	if registration == nil || registration["totpRequired"] != false {
		t.Fatalf("discovery must report the door's shape: %+v", doc["registration"])
	}

	// A password and no code changes the password.
	rec = env.do(t, http.MethodPost, "/password", "", map[string]any{
		"repository": fakeRepository, "password": "correct-horse-battery-staple",
		"newPassword": "a-new-correct-horse",
	})
	wantStatus(t, rec, http.StatusOK)

	// A bearer token and no password is still refused with 403 — a token is
	// not evidence here and the missing factor is not why.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/password", svc.token(fakeRepository), map[string]any{
		"repository": fakeRepository, "newPassword": "another-correct-horse",
	})
	wantStatus(t, rec, http.StatusForbidden)
}

// The default deployment is unchanged: discovery says a code is required, and
// a credential change without one is refused before the service is reached.
func TestDiscoveryRequiresTOTPByDefault(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	doc := decodeJSON[map[string]any](t, rec)
	registration, _ := doc["registration"].(map[string]any)
	if registration == nil || registration["totpRequired"] != true {
		t.Fatalf("discovery must require the second factor by default: %+v", doc["registration"])
	}
	rec = env.do(t, http.MethodPost, "/password", "", map[string]any{
		"repository": fakeRepository, "password": "correct-horse-battery-staple",
		"newPassword": "a-new-correct-horse",
	})
	wantStatus(t, rec, http.StatusForbidden)
}

// A deployment with no invite code configured says so in discovery, so a
// client stops asking a person for a code nothing reads.
func TestDiscoveryReportsNoInviteRequiredWithNoInviteCode(t *testing.T) {
	svc := newFakeService()
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	env := &testEnv{svc: svc, clock: clock, h: New(Config{Service: svc, Now: clock.now})}

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
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	doc := decodeJSON[map[string]any](t, rec)
	registration, _ := doc["registration"].(map[string]any)
	if registration == nil || registration["inviteRequired"] != true {
		t.Fatalf("discovery must report the invite code as required: %+v", doc["registration"])
	}
}
