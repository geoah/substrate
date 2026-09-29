package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// wantSecurityHeaders fails unless rec carries every browser-facing
// protection, with the values a browser acts on. The CSP is checked for what
// it refuses rather than character by character, because the OAuth return
// page carries a different one.
func wantSecurityHeaders(t *testing.T, name string, rec *httptest.ResponseRecorder) {
	t.Helper()
	h := rec.Header()
	for header, want := range map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "no-referrer",
	} {
		if got := h.Get(header); got != want {
			t.Errorf("%s: %s = %q, want %q", name, header, got, want)
		}
	}
	csp := h.Get("Content-Security-Policy")
	if got := cspSources(csp, "frame-ancestors"); strings.Join(got, " ") != "'none'" {
		t.Errorf("%s: frame-ancestors = %q, want 'none' (CSP %q)", name, got, csp)
	}
	if inlineScriptRuns(csp, "") {
		t.Errorf("%s: the CSP runs an inline script that carries no nonce (CSP %q)", name, csp)
	}
}

// cspSources is the source list policy applies to directive, falling back to
// default-src the way a browser does for a fetch directive. A directive the
// policy does not name and default-src does not cover answers nil.
func cspSources(policy, directive string) []string {
	var fallback []string
	for _, d := range strings.Split(policy, ";") {
		fields := strings.Fields(d)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case directive:
			return fields[1:]
		case "default-src":
			if strings.HasSuffix(directive, "-src") {
				fallback = fields[1:]
			}
		}
	}
	return fallback
}

// inlineScriptRuns reports whether a browser enforcing policy runs an inline
// <script> whose nonce attribute is nonce ("" for none), by the CSP Level 3
// rule: a matching 'nonce-...' source allows it, and 'unsafe-inline' allows it
// only while the list holds no nonce or hash source. A policy with no
// script-src and no default-src restricts nothing.
func inlineScriptRuns(policy, nonce string) bool {
	if !strings.Contains(policy, "script-src") && !strings.Contains(policy, "default-src") {
		return true
	}
	unsafeInline, keyed := false, false
	for _, s := range cspSources(policy, "script-src") {
		switch {
		case s == "'unsafe-inline'":
			unsafeInline = true
		case strings.HasPrefix(s, "'nonce-"):
			keyed = true
			if nonce != "" && s == "'nonce-"+nonce+"'" {
				return true
			}
		case strings.HasPrefix(s, "'sha256-"), strings.HasPrefix(s, "'sha384-"), strings.HasPrefix(s, "'sha512-"):
			keyed = true
		}
	}
	return unsafeInline && !keyed
}

// The helper is the test's own evaluator, so it is held to the rule first: a
// test that asks it about a nonce must be asking something that can say no.
func TestInlineScriptRunsFollowsTheCSPRule(t *testing.T) {
	for _, c := range []struct {
		policy, nonce string
		want          bool
	}{
		{"", "", true},
		{"default-src 'self'", "", false},
		{"script-src 'self' 'unsafe-inline'", "", true},
		{"script-src 'unsafe-inline' 'nonce-abc'", "", false},
		{"script-src 'unsafe-inline' 'nonce-abc'", "abc", true},
		{"script-src 'nonce-abc'", "abd", false},
		{"default-src 'none'; script-src 'nonce-abc'", "abc", true},
		{"default-src 'unsafe-inline'", "", true},
	} {
		if got := inlineScriptRuns(c.policy, c.nonce); got != c.want {
			t.Errorf("inlineScriptRuns(%q, %q) = %v, want %v", c.policy, c.nonce, got, c.want)
		}
	}
}

// Every response carries the protections, whatever answered it: an API read,
// the console's shell, a missing asset, the API's own 404 and 405, and the
// health probe. The console's policy frames nothing and runs no inline script.
func TestEveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>console"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := newFakeService()
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	env := &testEnv{svc: svc, h: New(Config{Service: svc, Now: clock.now, WebDir: dir}), clock: clock}
	tok := svc.token(fakeRepository)

	for _, c := range []struct {
		name, method, path, token string
		status                    int
	}{
		{"records read", http.MethodGet, recordsPath, tok, http.StatusOK},
		{"console shell", http.MethodGet, "/", "", http.StatusOK},
		{"console deep link", http.MethodGet, "/records/9f2kq1x7m0zb", "", http.StatusOK},
		{"missing asset", http.MethodGet, "/assets/gone-1234.js", "", http.StatusNotFound},
		{"API 404", http.MethodGet, "/api/v1/nosuch", tok, http.StatusNotFound},
		{"API 405", http.MethodDelete, recordsPath, tok, http.StatusMethodNotAllowed},
		{"unauthenticated read", http.MethodGet, recordsPath, "", http.StatusUnauthorized},
		{"health", http.MethodGet, "/healthz", "", http.StatusOK},
	} {
		rec := env.do(t, c.method, c.path, c.token, nil)
		wantStatus(t, rec, c.status)
		wantSecurityHeaders(t, c.name, rec)
	}

	// The console's own policy, beyond what every response shares: scripts
	// come from this origin and nowhere else.
	rec := env.do(t, http.MethodGet, "/", "", nil)
	csp := rec.Header().Get("Content-Security-Policy")
	if got := cspSources(csp, "script-src"); strings.Join(got, " ") != "'self'" {
		t.Errorf("console script-src = %q, want 'self' (CSP %q)", got, csp)
	}
	if got := cspSources(csp, "object-src"); strings.Join(got, " ") != "'none'" {
		t.Errorf("console object-src = %q, want 'none' (CSP %q)", got, csp)
	}
}

// Every route that takes or hands back a credential answers no-store, the
// refusals among them: a rate-limited attempt, a wrong password and a missing
// bearer are responses a shared cache must not keep either.
func TestCredentialResponsesAreNeverStored(t *testing.T) {
	env := newTestEnv(t)
	password := env.svc.passwords[fakeRepository]
	tok := env.svc.token(fakeRepository)
	pace := func() { env.clock.advance(defaultAuthInterval + time.Millisecond) }

	wantNoStore := func(name string, rec *httptest.ResponseRecorder, status int) {
		t.Helper()
		wantStatus(t, rec, status)
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", name, got)
		}
	}

	rec := env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
		"inviteCode": testInviteCode, "repository": "ada",
	})
	wantNoStore("register enroll (the TOTP seed)", rec, http.StatusOK)
	seed := decodeJSON[substrate.TOTPEnrollment](t, rec).Secret

	pace()
	wantNoStore("register (the first secret and the recovery key)", env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": testInviteCode, "repository": "ada", "password": "correct-horse-battery-staple",
		"totpSecret": seed, "totpCode": fakeCode("ada.example.com"),
	}), http.StatusCreated)

	pace()
	wantNoStore("login", env.do(t, http.MethodPost, loginPath, "",
		loginBody(fakeRepository, password, fakeCode(fakeRepository))), http.StatusCreated)
	wantNoStore("login, rate limited", env.do(t, http.MethodPost, loginPath, "",
		loginBody(fakeRepository, password, fakeCode(fakeRepository))), http.StatusTooManyRequests)
	pace()
	wantNoStore("login, wrong password", env.do(t, http.MethodPost, loginPath, "",
		loginBody(fakeRepository, "wrong", fakeCode(fakeRepository))), http.StatusUnauthorized)

	rec = env.do(t, http.MethodPost, "/tokens", tok, map[string]any{"label": "script"})
	wantNoStore("mint a token", rec, http.StatusCreated)
	minted := decodeJSON[substrate.MintedToken](t, rec)
	wantNoStore("list tokens", env.do(t, http.MethodGet, "/tokens", tok, nil), http.StatusOK)
	// The fake mints no token record, so the revoke answers 404: the route's
	// header, not the delete, is what is under test.
	wantNoStore("revoke a token", env.do(t, http.MethodDelete, "/tokens/"+minted.Token.ID, tok, nil), http.StatusNotFound)
	wantNoStore("mint without a bearer", env.do(t, http.MethodPost, "/tokens", "", nil), http.StatusUnauthorized)

	pace()
	rec = env.do(t, http.MethodPost, "/totp/enroll", "", map[string]any{
		"repository": fakeRepository, "password": password, "totpCode": fakeCode(fakeRepository),
	})
	wantNoStore("TOTP enroll (the candidate seed)", rec, http.StatusOK)
	pace()
	wantNoStore("TOTP swap", env.do(t, http.MethodPost, "/totp", "", map[string]any{
		"repository": fakeRepository, "password": password, "totpCode": fakeCode(fakeRepository),
		"newTotpSecret": decodeJSON[substrate.TOTPEnrollment](t, rec).Secret, "newTotpCode": "123456",
	}), http.StatusOK)
	pace()
	wantNoStore("password change", env.do(t, http.MethodPost, "/password", "", map[string]any{
		"repository": fakeRepository, "password": password, "totpCode": fakeCode(fakeRepository),
		"newPassword": "a-brand-new-passphrase",
	}), http.StatusOK)

	wantNoStore("oauth start", env.do(t, http.MethodPost, "/api/v1/oauth/start", tok,
		map[string]any{"record": "providers.substrate.reamde.dev/google/account/owner"}), http.StatusNotFound)
	wantNoStore("oauth start without a bearer", env.do(t, http.MethodPost, "/api/v1/oauth/start", "", nil), http.StatusUnauthorized)
	wantNoStore("oauth callback", getCallback(callbackHandler(callbackRecord, nil, "https://console.example.com")), http.StatusOK)

	exporting, ds, exportToken := exportEnv(t)
	wantNoStore("export", exporting.do(t, http.MethodGet, "/api/v1/export", exportToken, nil), http.StatusOK)
	wantNoStore("export without a bearer", exporting.do(t, http.MethodGet, "/api/v1/export", "", nil), http.StatusUnauthorized)
	ds.exportErr = substrate.ErrUnavailable
	wantNoStore("export, refused", exporting.do(t, http.MethodGet, "/api/v1/export", exportToken, nil), http.StatusServiceUnavailable)
}

var (
	scriptTag = regexp.MustCompile(`<script\b([^>]*)>`)
	nonceAttr = regexp.MustCompile(`\bnonce="([^"]*)"`)
)

// The return page's one inline script runs under its own response's policy
// by the nonce it carries, and a script without that nonce does not: the
// page is not exempt from the policy, it is keyed into it. Each response
// draws a fresh nonce, and the page keeps every protection the console has.
func TestOAuthReturnPageScriptRunsByItsNonceAlone(t *testing.T) {
	h := callbackHandler(callbackRecord, nil, "https://console.example.com")
	seen := map[string]bool{}
	for range 2 {
		rec := getCallback(h)
		wantStatus(t, rec, http.StatusOK)
		wantSecurityHeaders(t, "oauth return page", rec)
		csp := rec.Header().Get("Content-Security-Policy")

		tags := scriptTag.FindAllStringSubmatch(rec.Body.String(), -1)
		if len(tags) != 1 {
			t.Fatalf("the return page carries %d script tags, want 1\n%s", len(tags), rec.Body.String())
		}
		m := nonceAttr.FindStringSubmatch(tags[0][1])
		if m == nil || m[1] == "" {
			t.Fatalf("the return page's script carries no nonce: <script%s>", tags[0][1])
		}
		nonce := m[1]
		if !inlineScriptRuns(csp, nonce) {
			t.Errorf("the return page's own script (nonce %q) is refused by its CSP %q", nonce, csp)
		}
		if inlineScriptRuns(csp, "") {
			t.Errorf("a script with no nonce runs under the return page's CSP %q", csp)
		}
		if inlineScriptRuns(csp, nonce+"x") {
			t.Errorf("a script with another nonce runs under the return page's CSP %q", csp)
		}
		if got := cspSources(csp, "connect-src"); strings.Join(got, " ") != "'none'" {
			t.Errorf("the return page may connect to %q, want 'none' (CSP %q)", got, csp)
		}
		if seen[nonce] {
			t.Fatalf("two responses drew the same nonce %q", nonce)
		}
		seen[nonce] = true
	}
}
