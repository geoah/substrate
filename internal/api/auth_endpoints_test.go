package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
		"inviteCode": testInviteCode, "username": "ada",
	})
	wantStatus(t, rec, http.StatusOK)
	enrollment := decodeJSON[substrate.TOTPEnrollment](t, rec)
	if enrollment.Secret == "" || enrollment.URI == "" {
		t.Fatalf("enrollment = %+v", enrollment)
	}

	// Step two commits, and registration ends logged in.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": testInviteCode, "username": "ada",
		"password":   "correct-horse-battery-staple",
		"totpSecret": enrollment.Secret, "totpCode": fakeCode("ada"),
		"label": "console",
	})
	wantStatus(t, rec, http.StatusCreated)
	out := decodeJSON[registerResponse](t, rec)
	if out.Secret == "" || out.Token.Label != "console" {
		t.Fatalf("registration response = %+v", out)
	}
	// Registration hands back the signing PIN and no private key material:
	// `signingPublicKey` is what `repository verify --expect-public-key` wants,
	// and the seed that mints the signatures stays sealed server-side.
	if out.SigningPublicKey != strings.Repeat("cd", 32) {
		t.Fatalf("registration did not carry the signing public key: %+v", out)
	}
	if strings.Contains(rec.Body.String(), "signingSeed") {
		t.Fatal("registration carried a signing seed; no response may hand out private key material")
	}

	// The token registration handed back is an ordinary bearer.
	wantStatus(t, env.do(t, http.MethodGet, "/api/v1/people.substrate.reamde.dev/people", out.Secret, nil),
		http.StatusOK)

	// Login mints another — sessions ARE token records.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, loginPath, "",
		loginBody("ada", "correct-horse-battery-staple", fakeCode("ada")))
	wantStatus(t, rec, http.StatusCreated)
	if login := decodeJSON[tokenResponse](t, rec); login.Secret == out.Secret {
		t.Fatal("login handed back the registration's secret")
	}
	if strings.Contains(rec.Body.String(), "signingSeed") {
		t.Fatal("login carried a signing seed; no response hands out private key material")
	}
}

// Registration is OFF unless the invite code is configured, and a wrong code
// is an auth failure that never reaches the service.
func TestRegistrationIsGatedByTheInviteCode(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
		"inviteCode": "wrong", "username": "ada",
	})
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
	if env.svc.registerCalls != 0 {
		t.Fatalf("a bad invite code reached the service (%d calls)", env.svc.registerCalls)
	}

	closed := newFakeService()
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	shut := &testEnv{svc: closed, h: New(Config{Service: closed, Now: clock.now}), clock: clock}
	for _, path := range []string{registerEnrolPath, registerPath} {
		rec := shut.do(t, http.MethodPost, path, "", map[string]any{
			"inviteCode": "anything", "username": "ada",
		})
		wantErrorCode(t, rec, http.StatusNotImplemented, codeUnsupported)
		clock.advance(defaultAuthInterval + time.Millisecond)
	}
	if closed.registerCalls != 0 {
		t.Fatalf("a closed substrate reached the service (%d calls)", closed.registerCalls)
	}
}

// The password-factor rule at the HTTP door: a bearer token
// alone is refused with 403 — the endpoint does not accept tokens at all —
// and the change goes through only with both current factors in the body.
func TestCredentialChangesRefuseABearerTokenAlone(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	password := env.svc.passwords["geoah"]

	for path, body := range map[string]map[string]any{
		"/password":    {"username": "geoah", "newPassword": "a-brand-new-passphrase"},
		"/totp/enroll": {"username": "geoah"},
		"/totp": {
			"username": "geoah", "newTotpSecret": "JBSWY3DPEHPK3PXP",
			"newTotpCode": "123456",
		},
	} {
		rec := env.do(t, http.MethodPost, path, tok, body)
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
		env.clock.advance(defaultAuthInterval + time.Millisecond)
	}

	// With both factors presented directly it works, token or no token.
	rec := env.do(t, http.MethodPost, "/password", "", map[string]any{
		"username": "geoah", "password": password, "totpCode": fakeCode("geoah"),
		"newPassword": "a-brand-new-passphrase",
	})
	wantStatus(t, rec, http.StatusOK)
	if env.svc.passwords["geoah"] != "a-brand-new-passphrase" {
		t.Fatalf("the password did not change: %q", env.svc.passwords["geoah"])
	}

	// A wrong current password is refused whatever else is right.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/password", "", map[string]any{
		"username": "geoah", "password": password, "totpCode": fakeCode("geoah"),
		"newPassword": "another-passphrase-again",
	})
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

func TestTOTPReenrollmentIsTwoSteps(t *testing.T) {
	env := newTestEnv(t)
	password := env.svc.passwords["geoah"]

	rec := env.do(t, http.MethodPost, "/totp/enroll", "", map[string]any{
		"username": "geoah", "password": password, "totpCode": fakeCode("geoah"),
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
		"username": "geoah", "password": password, "totpCode": fakeCode("geoah"),
		"newTotpSecret": enrollment.Secret, "newTotpCode": "",
	})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)

	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/totp", "", map[string]any{
		"username": "geoah", "password": password, "totpCode": fakeCode("geoah"),
		"newTotpSecret": enrollment.Secret, "newTotpCode": "123456",
	})
	wantStatus(t, rec, http.StatusOK)
}

// Every refused factor answers the same way: which one was wrong, and whether
// the user exists at all, are not questions the door answers.
func TestLoginGivesNoExistenceOracle(t *testing.T) {
	env := newTestEnv(t)
	badPassword := env.do(t, http.MethodPost, loginPath, "", loginBody("geoah", "wrong", fakeCode("geoah")))
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
	rec := env.do(t, http.MethodDelete, "/api/v1/graphql", svc.token("geoah"), nil)
	wantStatus(t, rec, http.StatusMethodNotAllowed)
	// Discovery sits outside the API surface, the same class as /healthz: a
	// wrong method there falls to the SPA too, never a JSON 405.
	rec = env.do(t, http.MethodDelete, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	if got := rec.Body.String(); got == "" || got[0] != '<' {
		t.Fatalf("DELETE /.well-known/substrate/server.json served %q, want the console", got)
	}
}

// The recovery enrollment carries the password-factor rule: both current
// factors in the body buy the one-time enrollment, a bearer or bad factors
// buy nothing, and the second attempt conflicts.
func TestRecoveryEnrollFactorsAndOneTime(t *testing.T) {
	env := newTestEnv(t)

	// No factors: refused before the service is reached, exactly as the
	// credential changes refuse a bearer as evidence.
	rec := env.do(t, http.MethodPost, "/recovery/enroll", "", map[string]any{
		"username": "geoah",
	})
	wantStatus(t, rec, http.StatusForbidden)

	// Wrong factors: the one auth error.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/recovery/enroll", "", map[string]any{
		"username": "geoah", "password": "wrong", "totpCode": "000000",
	})
	wantStatus(t, rec, http.StatusUnauthorized)

	// Right factors: enrolled once, the server-minted key delivered once.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/recovery/enroll", "", map[string]any{
		"username": "geoah", "password": "correct-horse-battery-staple",
		"totpCode": fakeCode("geoah"),
	})
	wantStatus(t, rec, http.StatusCreated)
	out := decodeJSON[map[string]string](t, rec)
	if !strings.HasPrefix(out["recoveryKey"], "AGE-SECRET-KEY-1") {
		t.Fatalf("no server-minted key: %+v", out)
	}
	if !strings.HasPrefix(out["recoveryPublicKey"], "age1") {
		t.Fatalf("no recipient: %+v", out)
	}

	// One-time: the slot is claimed.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/recovery/enroll", "", map[string]any{
		"username": "geoah", "password": "correct-horse-battery-staple",
		"totpCode": fakeCode("geoah"),
	})
	wantStatus(t, rec, http.StatusConflict)
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
		"username": "geoah", "password": "correct-horse-battery-staple",
		"newPassword": "a-new-correct-horse",
	})
	wantStatus(t, rec, http.StatusOK)

	// A bearer token and no password is still refused with 403 — a token is
	// not evidence here and the missing factor is not why.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/password", svc.token("geoah"), map[string]any{
		"username": "geoah", "newPassword": "another-correct-horse",
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
		"username": "geoah", "password": "correct-horse-battery-staple",
		"newPassword": "a-new-correct-horse",
	})
	wantStatus(t, rec, http.StatusForbidden)
}

// A deployment with no invite code configured says so in discovery, before a
// caller wastes a round trip finding out registration answers `unsupported`.
func TestDiscoveryReportsRegistrationClosedWithNoInviteCode(t *testing.T) {
	svc := newFakeService()
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	env := &testEnv{svc: svc, clock: clock, h: New(Config{Service: svc, Now: clock.now})}

	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	doc := decodeJSON[map[string]any](t, rec)
	registration, _ := doc["registration"].(map[string]any)
	if registration == nil || registration["open"] != false {
		t.Fatalf("discovery must report registration as closed: %+v", doc["registration"])
	}
}

// The ordinary test deployment carries an invite code, so discovery says
// registration is open.
func TestDiscoveryReportsRegistrationOpenWithInviteCode(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	doc := decodeJSON[map[string]any](t, rec)
	registration, _ := doc["registration"].(map[string]any)
	if registration == nil || registration["open"] != true {
		t.Fatalf("discovery must report registration as open: %+v", doc["registration"])
	}
}
