package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
)

// harness is one CLI invocation's world: a temp config, captured streams.
type harness struct {
	t          *testing.T
	fake       *fakeSubstrate
	server     string
	configPath string
	stdin      *bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	// The CLI reads these as overrides; a developer's shell must not leak in.
	t.Setenv("SUBSTRATE_SERVER", "")
	t.Setenv("SUBSTRATE_TOKEN", "")
	t.Setenv("SUBSTRATECTL_CONFIG", "")
	// The operator hat reads the database from the environment, and a
	// developer's DATABASE_URL must not turn a refusal test into a connection.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SUBSTRATE_DATABASE_URL", "")
	fake, srv := newFake(t)
	return &harness{
		t:          t,
		fake:       fake,
		server:     srv.URL,
		configPath: filepath.Join(t.TempDir(), "config.yaml"),
		stdin:      &bytes.Buffer{},
	}
}

// writeConfig installs a logged-in context pointing at the fake.
func (h *harness) writeConfig() {
	h.t.Helper()
	cfg := &Config{
		CurrentContext: "test",
		Contexts: []Context{{
			Name: "test", Server: h.server, Username: "geoah",
			Token: "substrate_tok_geoah_test", TokenID: "tk01",
		}},
	}
	if err := saveConfig(h.configPath, cfg); err != nil {
		h.t.Fatalf("seed config: %v", err)
	}
}

func (h *harness) run(args ...string) (stdout, stderr string, err error) {
	h.t.Helper()
	var out, errOut bytes.Buffer
	a := newApp("test")
	a.in, a.out, a.errOut = h.stdin, &out, &errOut
	a.now = func() time.Time { return testNow }
	a.configPath = h.configPath
	root := a.rootCommand()
	root.SetArgs(args)
	err = root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

// lastRequest is the "METHOD /path" of the last request the fake saw, or "".
func (h *harness) lastRequest() string {
	h.t.Helper()
	if len(h.fake.requests) == 0 {
		return ""
	}
	return h.fake.requests[len(h.fake.requests)-1]
}

func (h *harness) mustRun(args ...string) (stdout, stderr string) {
	h.t.Helper()
	out, errOut, err := h.run(args...)
	if err != nil {
		h.t.Fatalf("substratectl %s: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, out, errOut)
	}
	return out, errOut
}

func seedTask(h *harness) {
	h.fake.seed(&substrate.Record{
		ID: "t9", Kind: "samples.substrate.reamde.dev/tasks/task",
		// One properties map. `title` is a property with a column behind it,
		// and `lifecycle` is the type's state property — the declaration is the
		// only thing that says which is which.
		Properties: map[string]any{
			"title":     "Send rack layout to Alex",
			"detail":    "rack layout",
			"lifecycle": "open",
		},
		Labels:    map[string]any{"owner/pinned": true},
		Version:   3,
		CreatedAt: testNow.Add(-48 * time.Hour),
		UpdatedAt: testNow.Add(-2 * time.Hour),
	})
	// The managed-property block a single-record read carries beside the
	// record: `detail` is owner-held (with one live source offering a
	// different value), `title` is a replaceable machine value, `lifecycle`
	// a bundle pin — the three tiers, the shape `status.properties`
	// renders and input ignores.
	h.fake.seedMeta("t9", map[string]statusProperty{
		"detail": {
			Manager:   "owner",
			Tier:      substrate.TierOwner,
			UpdatedAt: testNow.Add(-2 * time.Hour),
			Alternatives: []statusAlternative{
				{Actor: "google.connectors.substrate.reamde.dev/google/tasks", Value: "rack layout, cold aisle", UpdatedAt: testNow.Add(-time.Hour)},
			},
		},
		"title": {
			Manager:   "google.connectors.substrate.reamde.dev/google/tasks",
			Tier:      substrate.TierMachine,
			UpdatedAt: testNow.Add(-3 * time.Hour),
		},
		"lifecycle": {
			Manager:   "function.closer.samples.substrate.reamde.dev/function/tasks",
			Tier:      substrate.TierBundle,
			UpdatedAt: testNow.Add(-4 * time.Hour),
		},
	})
	// A server may still answer with an incoming block on the record. The
	// document must ignore it: incoming references page on their own resource.
	h.fake.seedIncoming("t9", []substrate.IncomingReference{
		{Property: "person", From: substrate.IncomingSource{
			ID: "people-c1001", Kind: "google.connectors.substrate.reamde.dev/google/contact", Title: "Alex Chen",
		}},
	})
}

func TestVersion(t *testing.T) {
	h := newHarness(t)
	out, _ := h.mustRun("version")
	if want := "substratectl test (api v1)\n"; out != want {
		t.Fatalf("version output = %q, want %q", out, want)
	}
}

func TestHelpListsEveryCommand(t *testing.T) {
	h := newHarness(t)
	out, _ := h.mustRun("--help")
	for _, want := range []string{
		"register", "login", "logout", "token", "kinds", "get", "apply",
		"delete", "edit", "watch", "user", "repository", "version",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("--help output missing %q:\n%s", want, out)
		}
	}
}

// The vocabulary is the contract too: `identity` and `tenant` are dead words,
// and help text is where a dead word survives longest.
func TestHelpSpeaksTheV1Vocabulary(t *testing.T) {
	h := newHarness(t)
	var all strings.Builder
	for _, args := range [][]string{
		{"--help"},
		{"login", "--help"},
		{"register", "--help"},
		{"logout", "--help"},
		{"token", "--help"},
		{"token", "create", "--help"},
		{"user", "--help"},
		{"user", "reset", "--help"},
		{"repository", "--help"},
		{"repository", "inspect", "--help"},
		{"repository", "rebuild", "--help"},
	} {
		out, _ := h.mustRun(args...)
		all.WriteString(out)
	}
	for _, dead := range []string{"tenant", "identity", "schemagroup", "otp exchange", "scopes"} {
		if strings.Contains(strings.ToLower(all.String()), dead) {
			t.Errorf("help text still says %q", dead)
		}
	}
}

func TestLoginStoresTokenWithTightPermissions(t *testing.T) {
	h := newHarness(t)
	h.stdin.WriteString("hunter2\n")
	out, _, err := h.run("login", "--server", h.server, "--username", "geoah", "--totp-code", "123456")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !strings.Contains(out, "logged in to "+h.server+" as geoah") {
		t.Fatalf("login output:\n%s", out)
	}

	info, err := os.Stat(h.configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != fs.FileMode(0o600) {
		t.Errorf("config permissions = %o, want 600", perm)
	}
	b, err := os.ReadFile(h.configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg.CurrentContext != "geoah" || len(cfg.Contexts) != 1 {
		t.Fatalf("config = %+v", cfg)
	}
	got := cfg.Contexts[0]
	if got.Server != h.server || got.Username != "geoah" || !strings.HasPrefix(got.Token, "substrate_tok_") {
		t.Fatalf("stored context = %+v", got)
	}
	// The token id is stored so that `substratectl logout` can revoke the very token
	// it forgets, and NOTHING names a repository: the token implies it.
	if got.TokenID != "tk01" {
		t.Errorf("stored token id = %q, want the minted record's", got.TokenID)
	}
	if strings.Contains(string(b), "repository") {
		t.Errorf("the stored context still names a repository:\n%s", b)
	}
	// The label defaults to substratectl@<hostname>.
	var label string
	if err := json.Unmarshal(h.fake.lastBody["label"], &label); err != nil {
		t.Fatalf("decode minted label: %v", err)
	}
	if !strings.HasPrefix(label, "substratectl@") {
		t.Errorf("token label = %q, want substratectl@<hostname>", label)
	}
}

// Both factors go in the body, and the password NEVER goes anywhere else: not
// a flag, not an argument, not the process table.
func TestLoginSendsBothFactors(t *testing.T) {
	h := newHarness(t)
	h.stdin.WriteString("hunter2\n")
	h.mustRun("login", "--server", h.server, "--username", "geoah", "--password-stdin", "--totp-code", "654321")
	if got := h.lastRequest(); got != "POST /login" {
		t.Fatalf("login hit %q, want POST /login", got)
	}
	for field, want := range map[string]string{
		"username": "geoah", "password": "hunter2", "totpCode": "654321",
	} {
		var got string
		if err := json.Unmarshal(h.fake.lastBody[field], &got); err != nil {
			t.Fatalf("decode %s: %v", field, err)
		}
		if got != want {
			t.Errorf("%s sent = %q, want %q", field, got, want)
		}
	}
	if _, ok := h.fake.lastBody["actors"]; ok {
		t.Error("login still sends an actors set; a token has full access and no actor set")
	}
}

func TestLoginPromptsForWhatItIsNotGiven(t *testing.T) {
	h := newHarness(t)
	h.stdin.WriteString("geoah\nhunter2\n123456\n")
	_, errOut := h.mustRun("login", "--server", h.server)
	for _, want := range []string{"Username: ", "Password: ", "TOTP code: "} {
		if !strings.Contains(errOut, want) {
			t.Errorf("prompt %q missing from stderr: %q", want, errOut)
		}
	}
	var username string
	if err := json.Unmarshal(h.fake.lastBody["username"], &username); err != nil {
		t.Fatalf("decode username: %v", err)
	}
	if username != "geoah" {
		t.Fatalf("username sent = %q", username)
	}
}

// Every auth attempt is rate limited and a failure counts toward a lockout, so
// a code that cannot be right must never spend one.
func TestLoginRejectsNonSixDigitCodesWithoutARequest(t *testing.T) {
	for _, code := range []string{"12345", "1234567", "12345a", "sso_deadbeef", "   "} {
		h := newHarness(t)
		h.stdin.WriteString("hunter2\n")
		_, _, err := h.run("login", "--server", h.server, "--username", "geoah", "--totp-code", code)
		if err == nil {
			t.Fatalf("code %q was accepted", code)
		}
		if !strings.Contains(err.Error(), "6 digits") && !strings.Contains(err.Error(), "6-digit") {
			t.Errorf("code %q: error = %v", code, err)
		}
		if len(h.fake.requests) != 0 {
			t.Errorf("code %q spent requests: %v", code, h.fake.requests)
		}
		if _, err := os.Stat(h.configPath); !os.IsNotExist(err) {
			t.Errorf("code %q wrote a config file", code)
		}
	}
}

// A substrate that verifies no second factor is not asked for a code: the
// prompt would have nothing to read it from, and the substrate ignores what it
// is sent. The stdin below holds ONLY a password, so a prompt for a code would
// hang or fail rather than pass quietly.
func TestLoginAsksForNoCodeWhereNoneIsVerified(t *testing.T) {
	h := newHarness(t)
	h.fake.totpDisabled = true
	h.stdin.WriteString("hunter2\n")
	h.mustRun("login", "--server", h.server, "--username", "geoah")
	var code string
	if err := json.Unmarshal(h.fake.lastBody["totpCode"], &code); err != nil {
		t.Fatalf("decode totpCode: %v", err)
	}
	if code != "" {
		t.Fatalf("code sent = %q, want none", code)
	}
	if got := h.fake.doorRequests(); len(got) != 1 || got[0] != "POST /login" {
		t.Fatalf("requests = %v, want the login alone", got)
	}
}

// Registration against the same substrate: no enrollment round trip, no code,
// and an EMPTY seed — which is what asks the substrate to mint the one it
// seals, so the credential still has a factor for the day the flag comes off.
func TestRegisterSkipsTheEnrollmentWhereNoCodeIsVerified(t *testing.T) {
	h := newHarness(t)
	h.fake.totpDisabled = true
	h.stdin.WriteString("hunter2\nhunter2\n")
	out, _ := h.mustRun("register", "--server", h.server, "--invite-code", "let-me-in", "--username", "geoah")
	if strings.Contains(out, "TOTP enrollment") {
		t.Errorf("an enrollment was shown for a substrate that verifies none:\n%s", out)
	}
	if got := h.fake.doorRequests(); len(got) != 1 || got[0] != "POST /register" {
		t.Fatalf("requests = %v, want the commit alone", got)
	}
	for field, want := range map[string]string{"totpSecret": "", "totpCode": ""} {
		var got string
		if err := json.Unmarshal(h.fake.lastBody[field], &got); err != nil {
			t.Fatalf("decode %s: %v", field, err)
		}
		if got != want {
			t.Errorf("%s sent = %q, want %q", field, got, want)
		}
	}
}

func TestLoginAcceptsASpacedCode(t *testing.T) {
	h := newHarness(t)
	h.stdin.WriteString("hunter2\n")
	h.mustRun("login", "--server", h.server, "--username", "geoah", "--totp-code", "123 456")
	var code string
	if err := json.Unmarshal(h.fake.lastBody["totpCode"], &code); err != nil {
		t.Fatalf("decode totpCode: %v", err)
	}
	if code != "123456" {
		t.Fatalf("code sent = %q", code)
	}
}

// The door answers the same 401 for an unknown username, a wrong password and
// a wrong code — deliberately — so the CLI must report all three, not guess.
func TestLogin401NamesEveryFactorAndTheLockout(t *testing.T) {
	h := newHarness(t)
	h.fake.authStatus = 401
	h.stdin.WriteString("hunter2\n")
	_, _, err := h.run("login", "--server", h.server, "--username", "geoah", "--totp-code", "000000")
	if err == nil {
		t.Fatal("expected an error from a 401")
	}
	var buf bytes.Buffer
	renderError(&buf, err)
	got := buf.String()
	for _, want := range []string{
		"refused the username, the password or the code",
		"repeated failures lock the account out",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered 401 missing %q:\n%s", want, got)
		}
	}
}

func TestLoginRateLimitedRendersRetryHint(t *testing.T) {
	h := newHarness(t)
	h.fake.authStatus = 429
	h.stdin.WriteString("hunter2\n")
	_, _, err := h.run("login", "--server", h.server, "--username", "geoah", "--totp-code", "123456")
	if err == nil {
		t.Fatal("expected an error from a 429")
	}
	var buf bytes.Buffer
	renderError(&buf, err)
	got := buf.String()
	for _, want := range []string{
		"error: rate limited",
		"too many requests",
		"hint: wait 5s and try again",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered 429 missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(h.configPath); !os.IsNotExist(err) {
		t.Errorf("a failed login must not write a config file")
	}
}

// Registration is two calls and one write: the enrollment creates NOTHING, and
// only the commit that carries a code back writes anything at all.
func TestRegisterEnrollsThenCommitsAndEndsLoggedIn(t *testing.T) {
	h := newHarness(t)
	h.stdin.WriteString("hunter2\nhunter2\n123456\n")
	out, _ := h.mustRun("register", "--server", h.server, "--invite-code", "let-me-in", "--username", "geoah")
	for _, want := range []string{
		"TOTP enrollment",
		"otpauth URI: " + fakeOtpauthURI,
		"secret:      " + fakeTOTPSecret,
		"nothing is stored until the code below is accepted",
		"registered geoah on " + h.server,
		// The authority the repository owns, defaulted by the substrate from
		// its own host when none is named.
		"authority: geoah.127.0.0.1",
		// The signing PIN, printed so the reader can keep it outside the
		// substrate: it is what `verify --expect-public-key` is fed.
		"signing public key: " + fakeSigningPublicKey,
		"--expect-public-key",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("register output missing %q:\n%s", want, out)
		}
	}
	// AND NO PRIVATE KEY MATERIAL. Registration used to disclose the Ed25519
	// SEED under the label "signing key" and offer to save it; the server stopped
	// sending one (#217), so a CLI printing that again would be printing
	// something it was never given. ("shown ONCE" is not forbidden: the recovery
	// key beside it genuinely is.)
	for _, forbidden := range []string{"signingSeed", "signing key ", "signing key:"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("register output carries %q, which is the retired seed ceremony:\n%s", forbidden, out)
		}
	}
	if got := h.fake.doorRequests(); len(got) != 2 || got[0] != "POST /register/enroll" || got[1] != "POST /register" {
		t.Fatalf("requests = %v, want the enrollment then the commit", got)
	}
	// The commit carries the seed the caller was issued, plus one code from it.
	for field, want := range map[string]string{
		"inviteCode": "let-me-in", "username": "geoah", "password": "hunter2",
		"totpSecret": fakeTOTPSecret, "totpCode": "123456",
	} {
		var got string
		if err := json.Unmarshal(h.fake.lastBody[field], &got); err != nil {
			t.Fatalf("decode %s: %v", field, err)
		}
		if got != want {
			t.Errorf("%s sent = %q, want %q", field, got, want)
		}
	}
	cfg, err := loadConfig(h.configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if len(cfg.Contexts) != 1 || cfg.Contexts[0].Token == "" {
		t.Fatalf("registration must end logged in: %+v", cfg)
	}
}

// The stored context carries the repository's AUTHORITY, the public name a
// webhook URL is built from. `register` records the one the server answered
// with, and `login`, which is told no authority at all, must carry the stored
// one forward instead of replacing the context without it.
func TestRegisterStoresTheAuthorityAndLoginKeepsIt(t *testing.T) {
	h := newHarness(t)
	h.stdin.WriteString("hunter2\nhunter2\n123456\n")
	h.mustRun("register", "--server", h.server, "--invite-code", "let-me-in", "--username", "geoah")
	cfg, err := loadConfig(h.configPath)
	if err != nil {
		t.Fatalf("read config after register: %v", err)
	}
	if len(cfg.Contexts) != 1 || cfg.Contexts[0].Authority != "geoah.127.0.0.1" {
		t.Fatalf("register stored %+v, want the authority the server answered", cfg.Contexts)
	}

	h.stdin.WriteString("hunter2\n")
	h.mustRun("login", "--server", h.server, "--username", "geoah", "--totp-code", "123456")
	cfg, err = loadConfig(h.configPath)
	if err != nil {
		t.Fatalf("read config after login: %v", err)
	}
	if len(cfg.Contexts) != 1 || cfg.Contexts[0].Authority != "geoah.127.0.0.1" {
		t.Fatalf("login dropped the stored authority: %+v", cfg.Contexts)
	}
	if cfg.Contexts[0].Token == "" {
		t.Fatalf("login stored no token: %+v", cfg.Contexts)
	}
}

// --totp-secret brings your own seed, which is what makes an unattended
// registration possible: no enrollment round trip, nothing to read back.
func TestRegisterWithOwnSeedSkipsTheEnrollment(t *testing.T) {
	h := newHarness(t)
	h.stdin.WriteString("hunter2\n")
	h.mustRun("register", "--server", h.server, "--invite-code", "let-me-in",
		"--username", "geoah", "--password-stdin",
		"--totp-secret", "MFRGGZDFMZTWQ2LK", "--totp-code", "123456")
	if got := h.fake.requests; len(got) != 1 || got[0] != "POST /register" {
		t.Fatalf("requests = %v, want the commit alone", got)
	}
}

func TestRegisterRefusesAMismatchedPassword(t *testing.T) {
	h := newHarness(t)
	h.stdin.WriteString("hunter2\nhunter3\n")
	_, _, err := h.run("register", "--server", h.server, "--invite-code", "x", "--username", "geoah")
	if err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("err = %v, want a mismatch refusal", err)
	}
	if len(h.fake.requests) != 0 {
		t.Errorf("a mismatched password spent requests: %v", h.fake.requests)
	}
}

// Logging out revokes the token record and forgets the secret. The context
// keeps its server and username, so logging back in is one command.
func TestLogoutRevokesTheStoredTokenAndForgetsIt(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("logout")
	if got := h.lastRequest(); got != "DELETE /tokens/tk01" {
		t.Fatalf("logout hit %q, want the token record's delete", got)
	}
	if !strings.Contains(out, "logged out of "+h.server) {
		t.Errorf("logout output:\n%s", out)
	}
	cfg, err := loadConfig(h.configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := cfg.Contexts[0]
	if got.Token != "" || got.TokenID != "" {
		t.Errorf("logout kept the secret: %+v", got)
	}
	if got.Server == "" || got.Username == "" {
		t.Errorf("logout threw away the context: %+v", got)
	}
}

// A token already gone on the server is the state logout wanted; it must still
// forget the local copy rather than fail and leave a dead secret on disk.
func TestLogoutForgetsATokenAlreadyGone(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.revokeStatus = 404
	_, errOut := h.mustRun("logout")
	if !strings.Contains(errOut, "already gone") {
		t.Errorf("stderr missing the note:\n%s", errOut)
	}
	cfg, _ := loadConfig(h.configPath)
	if cfg.Contexts[0].Token != "" {
		t.Error("logout kept a secret the server no longer honors")
	}
}

func TestErrorRenderingProblemsAndHints(t *testing.T) {
	cases := []struct {
		name string
		err  *apiError
		want []string
	}{
		{
			name: "validation",
			err: &apiError{
				Status: 422, Code: "validation", Message: "3 problems",
				Problems: []string{"properties.detail: required", "at: not a timestamp"},
				Method:   "PUT", Path: "/api/v1/samples.substrate.reamde.dev/tasks/task/t9",
			},
			want: []string{
				"error: the substrate rejected this write as invalid",
				"  problems:",
				"    - properties.detail: required",
				"    - at: not a timestamp",
				"request: PUT /api/v1/samples.substrate.reamde.dev/tasks/task/t9 (422)",
			},
		},
		{
			name: "conflict",
			err:  &apiError{Status: 409, Code: "conflict"},
			want: []string{"hint: re-run `substratectl get`"},
		},
		{
			name: "auth",
			err:  &apiError{Status: 401, Code: "auth"},
			want: []string{"hint: run `substratectl login`"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			renderError(&buf, tc.err)
			for _, want := range tc.want {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("missing %q in:\n%s", want, buf.String())
				}
			}
		})
	}
}

// The whole registry, identity-ordered, which under the package grammar
// means the table is read down the AUTHORITY and PACKAGE columns, not the
// NAME one.
func TestKindsTable(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("kinds")
	want := "NAME                  AUTHORITY                                PACKAGE     PLURAL                 VERSION   SOURCE\n" +
		"syncrun               google.connectors.substrate.reamde.dev   google      syncruns               1         installed\n" +
		"book                  library.substrate.reamde.dev             library     books                  1         builtin\n" +
		"bookseries            library.substrate.reamde.dev             library     bookseries             1         builtin\n" +
		"movie                 library.substrate.reamde.dev             library     movies                 1         builtin\n" +
		"podcast               library.substrate.reamde.dev             library     podcasts               1         builtin\n" +
		"tvseries              library.substrate.reamde.dev             library     tvseries               1         builtin\n" +
		"calendarevent         samples.substrate.reamde.dev             calendar    calendarevents         1         builtin\n" +
		"calendareventseries   samples.substrate.reamde.dev             calendar    calendareventseries    1         builtin\n" +
		"conversationmessage   samples.substrate.reamde.dev             messaging   conversationmessages   1         builtin\n" +
		"organization          samples.substrate.reamde.dev             people      organizations          1         builtin\n" +
		"person                samples.substrate.reamde.dev             people      people                 1         builtin\n" +
		"task                  samples.substrate.reamde.dev             tasks       tasks                  1         builtin\n" +
		"syncrun               slack.connectors.substrate.reamde.dev    slack       syncruns               1         installed\n" +
		"kind                  substrate.reamde.dev                     core        kinds                  1         builtin\n" +
		"recordmerge           substrate.reamde.dev                     core        recordmerges           1         builtin\n" +
		"recordsplit           substrate.reamde.dev                     core        recordsplits           1         builtin\n" +
		"token                 substrate.reamde.dev                     core        tokens                 1         builtin\n"
	if out != want {
		t.Fatalf("kinds table:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestGetListTable(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	out, _ := h.mustRun("get", "tasks")
	want := "ID   TITLE                      STATE            UPDATED\n" +
		"t9   Send rack layout to Alex   lifecycle=open   2h\n"
	if out != want {
		t.Fatalf("get table:\ngot:\n%q\nwant:\n%q", out, want)
	}
}

// The canonical-id contract, CLI side: a read addressed by a
// former id returns the canonical record and SAYS SO. The saying-so is a note
// on stderr — stdout is byte-for-byte the read of the canonical id, whatever
// the output format, because a `get -o yaml | apply` and a piped table must
// not change shape the day one of their ids turns out to have been merged.
func TestGetByFormerIDNotesTheCanonicalIDOnStderr(t *testing.T) {
	for _, output := range []string{"yaml", "table", "wide", "json"} {
		t.Run(output, func(t *testing.T) {
			h := newHarness(t)
			h.writeConfig()
			seedTask(h)
			h.fake.mergeInto("t8", "t9")

			byFormer, stderr := h.mustRun("get", "tasks", "t8", "-o", output)
			byCanonical, quiet := h.mustRun("get", "tasks", "t9", "-o", output)

			if want := "resolved via former id; canonical: t9\n"; stderr != want {
				t.Fatalf("stderr = %q, want %q", stderr, want)
			}
			if byFormer != byCanonical {
				t.Fatalf("output differs from the canonical read:\ngot:\n%s\nwant:\n%s", byFormer, byCanonical)
			}
			// …and an ordinary read says nothing at all: the note's presence is
			// the whole signal, so it must never fire on the common path.
			if quiet != "" {
				t.Fatalf("canonical read wrote to stderr: %q", quiet)
			}
		})
	}
}

func TestGetWideTable(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	out, _ := h.mustRun("get", "tasks", "-o", "wide")
	if !strings.Contains(out, "ID") || !strings.Contains(out, "TYPE") || !strings.Contains(out, "VERSION") {
		t.Fatalf("wide table missing columns:\n%s", out)
	}
	if !strings.Contains(out, "samples.substrate.reamde.dev/tasks/task") {
		t.Fatalf("wide table missing type:\n%s", out)
	}
}

// A qualified plural is resolved syntactically: addressing the collection costs
// no round trip, and a format with nothing to look up makes exactly one
// request.
func TestGetQualifiedPluralResolvesWithoutTheRegistry(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	h.mustRun("get", "samples.substrate.reamde.dev/tasks/task", "-o", "yaml")
	for _, req := range h.fake.requests {
		if strings.Contains(req, "/substrate.reamde.dev/core/kind") {
			t.Fatalf("qualified plural should not hit the type registry: %v", h.fake.requests)
		}
	}
}

// The table is the one format that does ask, and only for its STATE column: a
// state is an ordinary property, so the declaration is the
// only thing that can say which property that is. The read still goes first —
// resolution did not wait on it.
func TestGetTableAsksTheRegistryOnlyForTheStateColumn(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	out, _ := h.mustRun("get", "samples.substrate.reamde.dev/tasks/task")
	if !strings.Contains(out, "lifecycle=open") {
		t.Fatalf("qualified plural lost the STATE column:\n%s", out)
	}
	if h.fake.requests[0] != "GET /api/v1/samples.substrate.reamde.dev/tasks/task" {
		t.Fatalf("the collection read must come first: %v", h.fake.requests)
	}
}

// The column names what the type declares as a state, and nothing else: an
// ordinary property sitting in the same map stays out of it, and a type that
// declares no state renders an empty column rather than guessing.
func TestStateColumnComesFromTheDeclaration(t *testing.T) {
	properties := map[string]any{"detail": "rack layout", "lifecycle": "open"}
	if got := joinStates(properties, []string{"lifecycle"}); got != "lifecycle=open" {
		t.Fatalf("joinStates = %q", got)
	}
	if got := joinStates(properties, nil); got != "-" {
		t.Fatalf("a type with no state property = %q, want -", got)
	}
	if got := joinStates(properties, []string{"lifecycle", "missing"}); got != "lifecycle=open" {
		t.Fatalf("a declared state the row lacks = %q", got)
	}
	ti := substrate.KindInfo{Definition: taskDefinition}
	if got := stateProperties(ti); len(got) != 1 || got[0] != "lifecycle" {
		t.Fatalf("stateProperties = %v, want [lifecycle]", got)
	}
	if got := stateProperties(substrate.KindInfo{}); len(got) != 0 {
		t.Fatalf("stateProperties of an undeclared type = %v", got)
	}
}

// A bare plural that exactly one package declares still resolves without a
// package: splitting the vocabulary namespaced the names, it did not make
// every command spell a package out. The fake serves only the tasks
// collection, so
// most of these reads 404; what is under test is the collection the CLI
// addressed, not what came back.
func TestGetBarePluralResolvesWhenUniqueAcrossGroups(t *testing.T) {
	cases := []struct{ arg, path string }{
		{"people", "/api/v1/samples.substrate.reamde.dev/people/person"},
		{"calendarevents", "/api/v1/samples.substrate.reamde.dev/calendar/calendarevent"},
		{"conversationmessages", "/api/v1/samples.substrate.reamde.dev/messaging/conversationmessage"},
		{"books", "/api/v1/library.substrate.reamde.dev/library/book"},
		{"movies", "/api/v1/library.substrate.reamde.dev/library/movie"},
		{"podcasts", "/api/v1/library.substrate.reamde.dev/library/podcast"},
		{"bookseries", "/api/v1/library.substrate.reamde.dev/library/bookseries"},
		// The singular resolves too, so `get person` is not a usage error.
		{"person", "/api/v1/samples.substrate.reamde.dev/people/person"},
		{"book", "/api/v1/library.substrate.reamde.dev/library/book"},
	}
	for _, tc := range cases {
		t.Run(tc.arg, func(t *testing.T) {
			h := newHarness(t)
			h.writeConfig()
			_, _, _ = h.run("get", tc.arg)
			want := "GET " + tc.path
			for _, req := range h.fake.requests {
				if req == want {
					return
				}
			}
			t.Errorf("get %s made %v, want one of them to be %q", tc.arg, h.fake.requests, want)
		})
	}
}

// `tvseries` is its own plural — there is nothing to pluralize and nothing to
// strip, so the bare argument, the qualified one and the type name are all the
// same string and must all address the same collection. A resolver that
// appended an "s" or assumed plural != name would only be caught here.
func TestGetPluralEqualToSingular(t *testing.T) {
	for _, arg := range []string{"tvseries", "library.substrate.reamde.dev/library/tvseries"} {
		t.Run(arg, func(t *testing.T) {
			h := newHarness(t)
			h.writeConfig()
			_, _, _ = h.run("get", arg)
			want := "GET /api/v1/library.substrate.reamde.dev/library/tvseries"
			for _, req := range h.fake.requests {
				if req == want {
					return
				}
			}
			t.Errorf("get %s made %v, want one of them to be %q", arg, h.fake.requests, want)
		})
	}
}

// Every connector installs a type named exactly `syncrun` in its own package,
// so `syncruns` is ambiguous the moment a second connector is registered: the
// one plural in a real repository that can never resolve bare.
func TestGetAmbiguousPluralErrors(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	_, _, err := h.run("get", "syncruns")
	if err == nil {
		t.Fatal("expected an ambiguity error")
	}
	for _, want := range []string{
		"ambiguous",
		"google.connectors.substrate.reamde.dev/google/syncrun",
		"slack.connectors.substrate.reamde.dev/slack/syncrun",
		"qualify it as authority/package/name",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ambiguity error missing %q: %v", want, err)
		}
	}
	// The error names the two ways out, and both of them work.
	h2 := newHarness(t)
	h2.writeConfig()
	// Both escape hatches the error names must actually address the right
	// collection; only the recorded requests matter here.
	h2.run("get", "slack.connectors.substrate.reamde.dev/slack/syncrun")                   //nolint:dogsled // requests are the assertion
	h2.run("get", "syncrun", "--package", "google.connectors.substrate.reamde.dev/google") //nolint:dogsled // requests are the assertion
	for _, want := range []string{
		"GET /api/v1/slack.connectors.substrate.reamde.dev/slack/syncrun",
		"GET /api/v1/google.connectors.substrate.reamde.dev/google/syncrun",
	} {
		var saw bool
		for _, req := range h2.fake.requests {
			saw = saw || req == want
		}
		if !saw {
			t.Errorf("requests = %v, want %q", h2.fake.requests, want)
		}
	}
}

// The registry is an ordinary collection: it pages, newest first, and the
// shipped vocabulary is the OLDEST rows in it. A client that takes the
// server's default page and ignores the cursor therefore sees the newest 50
// types and nothing else — which reports shipped vocabulary as unknown, and,
// worse, silently resolves a name several authorities declare to whichever single
// row happened to stay visible. `substratectl` must read the registry whole.
func TestTypeRegistryIsReadWhole(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	// More recent rows than one MAX-size page holds, so the fix's cursor walk
	// is exercised and not just its bigger `first`.
	extra := make([]map[string]any, 0, 520)
	for i := range 520 {
		name := fmt.Sprintf("padtype%03d", i)
		extra = append(extra, installed(name, "pad.bundles.substrate.reamde.dev/pad", name+"s"))
	}
	h.fake.extraTypes = extra

	// `books` sits at the oldest end of the registry. The fake serves no
	// library collection, so the read itself 404s — the assertion is that it
	// RESOLVED and addressed that collection at all.
	if _, _, err := h.run("get", "books"); err != nil && strings.Contains(err.Error(), "no type with plural") {
		t.Fatalf("`get books` lost shipped vocabulary past the first page: %v", err)
	}
	var sawBooks bool
	pages := 0
	for _, req := range h.fake.requests {
		sawBooks = sawBooks || req == "GET /api/v1/library.substrate.reamde.dev/library/book"
		if req == "GET "+typesPath {
			pages++
		}
	}
	if !sawBooks {
		t.Fatalf("`get books` did not reach the library collection: %v", h.fake.requests)
	}
	if pages < 2 {
		t.Fatalf("the registry was read in %d request(s); a %d-row registry pages", pages, len(extra)+len(fakeRegistry))
	}

	// An ambiguous plural must still REPORT its ambiguity rather than lose one
	// of its candidates past the page boundary and silently pick the other.
	h2 := newHarness(t)
	h2.writeConfig()
	h2.fake.extraTypes = extra
	_, _, err := h2.run("get", "syncruns")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("`get syncruns` past the first page: err = %v, want an ambiguity", err)
	}

	// And `types` lists every row, not a silently truncated 50.
	h3 := newHarness(t)
	h3.writeConfig()
	h3.fake.extraTypes = extra
	out, _ := h3.mustRun("kinds")
	if got, want := strings.Count(out, "\n")-1, len(extra)+len(fakeRegistry); got != want {
		t.Fatalf("`types` listed %d rows, want %d", got, want)
	}
}

func TestGetUnknownPluralErrors(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	_, _, err := h.run("get", "widgets")
	if err == nil || !strings.Contains(err.Error(), `no kind named "widgets"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestGetSingleYAMLRoundTripsThroughApply(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	out, _ := h.mustRun("get", "tasks", "t9")

	var d document
	if err := yaml.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("get -o yaml is not parseable: %v\n%s", err, out)
	}
	// The envelope: one `kind`, carrying the kind reference and no version;
	// the id is metadata.id.
	if d.Kind != "samples.substrate.reamde.dev/tasks/task" {
		t.Fatalf("envelope kind = %q", d.Kind)
	}
	if d.Metadata.ID != "t9" {
		t.Fatalf("metadata.id = %q", d.Metadata.ID)
	}
	if d.Metadata.Labels["owner/pinned"] != true {
		t.Errorf("labels belong to metadata: %+v", d.Metadata.Labels)
	}
	// Everything authored is a property, `title` among them: one map, and the
	// hot columns are storage rather than a second shape.
	if d.Data.Properties["title"] != "Send rack layout to Alex" {
		t.Errorf("title is a property: %+v", d.Data.Properties)
	}
	if d.Data.Properties["detail"] != "rack layout" {
		t.Errorf("properties belong to data: %+v", d.Data.Properties)
	}
	// A state is a property too, so it rides in the same block — no `states`
	// beside it, and nothing to tell the two apart in the document.
	if d.Data.Properties["lifecycle"] != "open" {
		t.Errorf("a state is a property: %+v", d.Data.Properties)
	}
	if d.Status == nil || d.Status.Version != 3 {
		t.Fatalf("status block = %+v", d.Status)
	}
	// The status block is server-set and must never be sent back.
	in, err := d.putInput()
	if err != nil {
		t.Fatalf("putInput: %v", err)
	}
	if in.Properties["title"] != "Send rack layout to Alex" {
		t.Fatalf("put input properties = %+v", in.Properties)
	}
	if in.Labels["owner/pinned"] != true {
		t.Fatalf("put input labels = %+v", in.Labels)
	}

	// Feeding the exact output back in is a no-op apply.
	h2 := newHarness(t)
	h2.writeConfig()
	h2.fake.records = h.fake.records
	h2.stdin.WriteString(out)
	applied, _ := h2.mustRun("apply", "-f", "-")
	if !strings.Contains(applied, "unchanged") {
		t.Fatalf("re-applying get output should be unchanged, got:\n%s", applied)
	}
}

// -o json is the same envelope as -o yaml, not the raw record.
func TestGetJSON(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	out, _ := h.mustRun("get", "tasks", "t9", "-o", "json")
	var d document
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("get -o json is not parseable: %v\n%s", err, out)
	}
	if d.Kind != "samples.substrate.reamde.dev/tasks/task" || d.Metadata.ID != "t9" {
		t.Fatalf("json envelope = %+v", d)
	}
	if d.Status == nil || d.Status.Version != 3 {
		t.Fatalf("json status = %+v", d.Status)
	}
}

// A list in json is the array of the same manifests the yaml stream separates
// with ---.
func TestGetListJSONIsAnArrayOfManifests(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	out, _ := h.mustRun("get", "tasks", "-o", "json")
	var docs []document
	if err := json.Unmarshal([]byte(out), &docs); err != nil {
		t.Fatalf("get list -o json is not an array: %v\n%s", err, out)
	}
	if len(docs) != 1 || docs[0].Kind != "samples.substrate.reamde.dev/tasks/task" || docs[0].Metadata.ID != "t9" {
		t.Fatalf("json list = %+v", docs)
	}
}

// Schema types get no special casing: whatever the server serves as a record
// renders through the same envelope, so a kind is
// `kind: substrate.reamde.dev/core/kind`.
func TestGetKindsYAML(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("get", "kinds", "-o", "yaml")
	first, _, _ := strings.Cut(out, "\n---\n")
	var d document
	if err := yaml.Unmarshal([]byte(first), &d); err != nil {
		t.Fatalf("kinds -o yaml is not parseable: %v\n%s", err, out)
	}
	if d.Kind != "substrate.reamde.dev/core/kind" {
		t.Fatalf("envelope kind = %q", d.Kind)
	}
	if d.Metadata.ID == "" || !strings.Contains(d.Metadata.ID, "/") {
		t.Fatalf("kind metadata.id = %q, want <authority>/<name>", d.Metadata.ID)
	}
	if strings.Count(out, "kind: substrate.reamde.dev/core/kind\n") != len(fakeRegistry) {
		t.Fatalf("expected one manifest per registry entry:\n%s", out)
	}
}

// The merge trail is server-set, so it reads back in `status` — beside the
// version and the finalizers. A former id is this record's own discarded name,
// which is store bookkeeping and never authored.
func TestGetRendersFormerIDsInStatus(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	h.fake.mergeInto("t8", "t9")
	out, _ := h.mustRun("get", "tasks", "t9")

	var d document
	if err := yaml.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("get -o yaml is not parseable: %v\n%s", err, out)
	}
	if d.Status == nil || len(d.Status.FormerIDs) != 1 || d.Status.FormerIDs[0] != "t8" {
		t.Fatalf("status.formerIds = %+v", d.Status)
	}
	if !strings.Contains(out, "formerIds:") {
		t.Fatalf("the document must say it:\n%s", out)
	}
	// …and it never travels back: status is ignored on input, so the write
	// carries the authored half and nothing else.
	in, err := d.putInput()
	if err != nil {
		t.Fatalf("putInput: %v", err)
	}
	if in.ID != "" {
		t.Fatalf("the id is addressed by the path, not the body: %+v", in)
	}
}

// Managed properties read back in `status.properties`: per property its
// manager, when it changed, and the alternatives — every live offer whose
// value differs from the stored one. Server-set through and through, so it is
// ignored on input like the rest of the block — and a LIST never carries it,
// because the wire omits `propertyMeta` on lists.
func TestGetRendersManagedPropertiesInStatus(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	out, _ := h.mustRun("get", "tasks", "t9")

	var d document
	if err := yaml.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("get -o yaml is not parseable: %v\n%s", err, out)
	}
	if d.Status == nil {
		t.Fatalf("no status block:\n%s", out)
	}
	prop, ok := d.Status.Properties["detail"]
	if !ok {
		t.Fatalf("status.properties missing detail: %+v", d.Status.Properties)
	}
	if prop.Manager != "owner" {
		t.Errorf("manager = %q, want owner", prop.Manager)
	}
	if len(prop.Alternatives) != 1 ||
		prop.Alternatives[0].Actor != "google.connectors.substrate.reamde.dev/google/tasks" ||
		prop.Alternatives[0].Value != "rack layout, cold aisle" {
		t.Errorf("alternatives = %+v", prop.Alternatives)
	}
	// The tier survives the decode, all three values: an owner hold, an
	// bundle pin and a replaceable machine value are distinguishable.
	for property, want := range map[string]substrate.Tier{
		"detail":    substrate.TierOwner,
		"title":     substrate.TierMachine,
		"lifecycle": substrate.TierBundle,
	} {
		if got := d.Status.Properties[property].Tier; got != want {
			t.Errorf("%s tier = %q, want %q", property, got, want)
		}
	}
	// The document says it, under status, in the envelope's own words.
	for _, want := range []string{
		"  properties:", "      manager: owner", "      alternatives:",
		"actor: google.connectors.substrate.reamde.dev/google/tasks",
		"tier: owner", "tier: bundle", "tier: machine",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("document missing %q:\n%s", want, out)
		}
	}
	// …and none of it travels back: status is ignored on input wholesale, so
	// the write carries the authored value, never the offer.
	in, err := d.putInput()
	if err != nil {
		t.Fatalf("putInput: %v", err)
	}
	if in.Properties["detail"] != "rack layout" {
		t.Fatalf("put input properties = %+v", in.Properties)
	}

	list, _ := h.mustRun("get", "tasks", "-o", "yaml")
	if strings.Contains(list, "manager:") || strings.Contains(list, "alternatives:") {
		t.Fatalf("a list document must not carry status.properties:\n%s", list)
	}
}

func TestGetOmitsIncomingFromManifest(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	out, _ := h.mustRun("get", "tasks", "t9")

	for _, unwanted := range []string{"incoming:", "people-c1001", "Alex Chen"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("manifest carries %q:\n%s", unwanted, out)
		}
	}
}

func TestGetFilterIsSentAsJSON(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	q, err := listQuery(`{"properties":{"lifecycle":{"eq":"open"}}}`, []string{"owner/pinned=true"}, 10, "at:desc", "CUR-abc123")
	if err != nil {
		t.Fatalf("listQuery: %v", err)
	}
	var f substrate.Filter
	if err := json.Unmarshal([]byte(q.Get("filter")), &f); err != nil {
		t.Fatalf("filter is not JSON: %v", err)
	}
	// A state filters through the properties arm; the grammar has no other.
	if f.Properties["lifecycle"].Eq != "open" {
		t.Errorf("properties filter lost: %+v", f.Properties)
	}
	if f.Labels["owner/pinned"].Eq != true {
		t.Errorf("selector not merged into labels: %+v", f.Labels)
	}
	if q.Get("first") != "10" || q.Get("orderBy") != "at:desc" {
		t.Errorf("query = %v", q)
	}
	// The opaque keyset cursor rides verbatim as `after`.
	if q.Get("after") != "CUR-abc123" {
		t.Errorf("after cursor not sent verbatim: %q", q.Get("after"))
	}
}

// A transition travels as a property write — `--state` is how you say the value
// you are writing is a declared move, and the wire has one block for both. The
// line the CLI prints back is where the record now stands, named off the
// declaration like the STATE column.
func TestPatchStateWritesTheProperty(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	out, _ := h.mustRun("patch", "tasks", "t9", "--state", "lifecycle=done")

	var in substrate.PatchInput
	if err := json.Unmarshal(mustRaw(h.fake.lastBody), &in); err != nil {
		t.Fatalf("patch body: %v", err)
	}
	if in.Properties["lifecycle"] != "done" {
		t.Fatalf("patch body = %+v, want the state in properties", in.Properties)
	}
	if !strings.Contains(out, "samples.substrate.reamde.dev/tasks/task/t9 patched") {
		t.Fatalf("patch output:\n%s", out)
	}
	if !strings.Contains(out, "states: lifecycle=done") {
		t.Fatalf("patch output missing the resulting state:\n%s", out)
	}
	// …and an ordinary property written the same way is not reported as a state.
	out, _ = h.mustRun("patch", "tasks", "t9", "--prop", "detail=cold aisle")
	if strings.Contains(out, "detail") {
		t.Fatalf("the states line names declared states only:\n%s", out)
	}
}

// The spellings the wire no longer has are refused rather than read past:
// decoding ignores an unknown key, so a document whose whole property block
// went unread would apply clean having written nothing.
func TestApplyRefusesTheOldSpellings(t *testing.T) {
	for _, tc := range []struct{ name, data, want string }{
		{"props", "  props:\n    detail: rack layout\n", "`data.props`, which is `data.properties`"},
		{"states", "  states:\n    lifecycle: done\n", "`data.states`, which is not a block"},
		{"ifVersion", "  ifVersion: 3\n", "`data.ifVersion`, which is `metadata.ifVersion`"},
		{"title", "  title: beside the map\n", "`data.title`, which is `data.properties.title`"},
		{"dueAt", "  dueAt: 2026-08-08T00:00:00Z\n", "`data.dueAt`, which is `data.properties.dueAt`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.writeConfig()
			h.stdin.WriteString("kind: samples.substrate.reamde.dev/tasks/task\nmetadata:\n  id: t9\ndata:\n" + tc.data)
			_, _, err := h.run("apply", "-f", "-")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to name %s", err, tc.want)
			}
			for _, req := range h.fake.requests {
				if strings.HasPrefix(req, "PUT ") || strings.HasPrefix(req, "POST /api/v1/tasks") {
					t.Fatalf("nothing may be written: %v", h.fake.requests)
				}
			}
		})
	}
}

// manifest is one task in the envelope, titled. The title is a property like
// everything else authored.
func manifest(title string) string {
	return "kind: samples.substrate.reamde.dev/tasks/task\nmetadata:\n  id: t9\ndata:\n  properties:\n    title: " + title + "\n"
}

func TestApplyCreateUpdateUnchanged(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	dir := t.TempDir()
	first := filepath.Join(dir, "task.yaml")
	if err := os.WriteFile(first, []byte(manifest("Draft the rack layout")), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _ := h.mustRun("apply", "-f", first)
	if want := "samples.substrate.reamde.dev/tasks/task/t9 created\n"; out != want {
		t.Fatalf("apply create = %q, want %q", out, want)
	}

	// Same document again: the server suppresses the no-op, so does the CLI.
	out, _ = h.mustRun("apply", "-f", first)
	if want := "samples.substrate.reamde.dev/tasks/task/t9 unchanged\n"; out != want {
		t.Fatalf("apply no-op = %q, want %q", out, want)
	}

	second := filepath.Join(dir, "task2.yaml")
	if err := os.WriteFile(second, []byte(manifest("Send rack layout to Alex")), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ = h.mustRun("apply", "-f", second)
	if want := "samples.substrate.reamde.dev/tasks/task/t9 updated\n"; out != want {
		t.Fatalf("apply update = %q, want %q", out, want)
	}
	if got := h.fake.record("t9"); got == nil || got.Properties["title"] != "Send rack layout to Alex" || got.Version != 2 {
		t.Fatalf("stored record = %+v", got)
	}
}

func TestApplyMultiDocumentFromStdin(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.stdin.WriteString(`kind: samples.substrate.reamde.dev/tasks/task
metadata: {}
data:
  properties:
    title: no id, POSTed
---
kind: samples.substrate.reamde.dev/tasks/task
metadata:
  id: t7
  labels: {owner/pinned: true}
  ifVersion: 1
data:
  properties:
    title: addressed by id
    detail: rack layout
    source: samples.substrate.reamde.dev/calendar/transcript/f81k
---
# a trailing separator is an empty document, not an error
`)
	out, _ := h.mustRun("apply", "-f", "-")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected one line per document, got:\n%s", out)
	}
	if !strings.HasSuffix(lines[0], " created") || !strings.HasSuffix(lines[1], " created") {
		t.Fatalf("apply output:\n%s", out)
	}
	var sawPost, sawPut bool
	for _, req := range h.fake.requests {
		sawPost = sawPost || req == "POST /api/v1/samples.substrate.reamde.dev/tasks/task"
		sawPut = sawPut || req == "PUT /api/v1/samples.substrate.reamde.dev/tasks/task/t7"
	}
	if !sawPost || !sawPut {
		t.Fatalf("expected a POST for the id-less doc and a PUT for t7: %v", h.fake.requests)
	}
}

// The pre-envelope format is a hard cut: it errors, and the error shows the
// same document in the shape it now needs.
func TestApplyRejectsThePreEnvelopeFormat(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.stdin.WriteString("id: t9\ntitle: flat\n")
	_, _, err := h.run("apply", "-f", "-")
	if err == nil {
		t.Fatal("the old flat format must not be accepted")
	}
	for _, want := range []string{
		"stdin document 1",
		"has no `kind`",
		"kind: <authority>/<name>",
		"id: t9",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("old-format error missing %q:\n%v", want, err)
		}
	}
	for _, req := range h.fake.requests {
		if strings.HasPrefix(req, "PUT ") || strings.HasPrefix(req, "POST ") {
			t.Fatalf("a rejected document must not be written: %v", h.fake.requests)
		}
	}
}

func TestApplyRejectsDocumentWithoutEnvelope(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.stdin.WriteString("data:\n  properties:\n    title: orphan\n")
	_, _, err := h.run("apply", "-f", "-")
	if err == nil || !strings.Contains(err.Error(), "has no `kind`") {
		t.Fatalf("err = %v", err)
	}
}

// The envelope's PACKAGE is what settles a kind name several packages declare.
func TestApplyResolvesTheKindThroughItsPackage(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.stdin.WriteString("kind: slack.connectors.substrate.reamde.dev/slack/syncrun\nmetadata:\n  id: r1\ndata: {}\n")
	_, _, _ = h.run("apply", "-f", "-") //nolint:dogsled // the addressed collection is the assertion
	var saw bool
	for _, req := range h.fake.requests {
		saw = saw || req == "PUT /api/v1/slack.connectors.substrate.reamde.dev/slack/syncrun/r1"
	}
	if !saw {
		t.Fatalf("requests = %v, want the slack package's syncruns", h.fake.requests)
	}

	// A kind that is real but not in the named package says where it lives.
	h2 := newHarness(t)
	h2.writeConfig()
	h2.stdin.WriteString("kind: samples.substrate.reamde.dev/tasks/syncrun\nmetadata: {}\ndata: {}\n")
	_, _, err := h2.run("apply", "-f", "-")
	if err == nil {
		t.Fatal("expected a kind/package mismatch error")
	}
	for _, want := range []string{`no kind "syncrun" in package "samples.substrate.reamde.dev/tasks"`, "google.connectors.substrate.reamde.dev/google"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("mismatch error missing %q: %v", want, err)
		}
	}
}

// Schema documents ride the batch apply verb — one POST for all of them, one
// transaction server-side — while record documents keep their collections
// (schema is records; apply is the one verb).
func TestApplyCarriesSchemaDocuments(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.stdin.WriteString(`kind: substrate.reamde.dev/core/package
metadata:
  id: widgets.example.substrate.reamde.dev/widgets
data:
  authority: widgets.example.substrate.reamde.dev
  package: widgets
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: widgets.example.substrate.reamde.dev/widgets/widget
data:
  authority: widgets.example.substrate.reamde.dev
  package: widgets
  names: {singular: widget, plural: widgets}
  properties:
    name: {type: string}
---
kind: samples.substrate.reamde.dev/tasks/task
metadata:
  id: t9
data:
  properties:
    title: rides beside the schema
`)
	out, _ := h.mustRun("apply", "-f", "-")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected three lines, got:\n%s", out)
	}
	if lines[0] != "package/widgets.example.substrate.reamde.dev/widgets applied" ||
		lines[1] != "kind/widgets.example.substrate.reamde.dev/widgets/widget applied" {
		t.Fatalf("schema apply output:\n%s", out)
	}
	if !strings.HasSuffix(lines[2], " created") {
		t.Fatalf("record apply output:\n%s", out)
	}
	batches, puts := 0, 0
	for _, req := range h.fake.requests {
		switch req {
		case "POST /api/v1/vocabulary/apply":
			batches++
		case "PUT /api/v1/samples.substrate.reamde.dev/tasks/task/t9":
			puts++
		}
	}
	if batches != 1 || puts != 1 {
		t.Fatalf("expected ONE schema batch and one record put: %v", h.fake.requests)
	}
}

func TestApplyWithNoInputErrors(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	_, _, err := h.run("apply")
	if err == nil || !strings.Contains(err.Error(), "no input") {
		t.Fatalf("err = %v", err)
	}
}

func TestDelete(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	out, _ := h.mustRun("delete", "tasks", "t9")
	if !strings.Contains(out, "samples.substrate.reamde.dev/tasks/task/t9 deleted") {
		t.Fatalf("delete output:\n%s", out)
	}
	if !strings.Contains(out, "waiting on finalizers") {
		t.Fatalf("delete should report finalizers:\n%s", out)
	}
	if h.fake.record("t9").DeletedAt == nil {
		t.Fatal("record was not tombstoned")
	}
}

func TestDeleteNotFoundRendersNotFound(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	_, _, err := h.run("delete", "samples.substrate.reamde.dev/tasks/task", "nope")
	if err == nil {
		t.Fatal("expected an error")
	}
	var buf bytes.Buffer
	renderError(&buf, err)
	if !strings.Contains(buf.String(), "error: no such resource") {
		t.Fatalf("rendered:\n%s", buf.String())
	}
}

func TestWatchPrintsOneLinePerChange(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.changes = []substrate.Change{
		{Seq: 42, TS: testNow, Actor: substrate.ActorAPI, Op: substrate.OpPut, RecordID: "t9", Kind: "samples.substrate.reamde.dev/tasks/task"},
		{Seq: 43, TS: testNow, Actor: "connector:gmail", Op: substrate.OpPatch, RecordID: "m3", Kind: "samples.substrate.reamde.dev/messaging/conversationmessage"},
	}
	out, _ := h.mustRun("watch", "--from", "41", "--kinds", "samples.substrate.reamde.dev/tasks/task,samples.substrate.reamde.dev/messaging/conversationmessage")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected bookmark + header + 2 change lines, got:\n%s", out)
	}
	if lines[0] != "# watching from seq 41" {
		t.Errorf("bookmark line = %q", lines[0])
	}
	if lines[1] != changeHeader {
		t.Errorf("header line = %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "42 ") || !strings.Contains(lines[2], "put") || !strings.Contains(lines[2], "api") {
		t.Errorf("change line = %q", lines[2])
	}
	if !strings.HasPrefix(lines[3], "43 ") || !strings.Contains(lines[3], "connector:gmail") {
		t.Errorf("change line = %q", lines[3])
	}
}

func TestWatchSendsFilters(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.mustRun("watch", "--from", "7", "--kinds", "a,b", "--ops", "put", "--actors", "api")
	// The fake records only the path; assert the request happened at all and
	// that the client built the query without error.
	var saw bool
	for _, req := range h.fake.requests {
		saw = saw || req == "GET /api/v1/changes"
	}
	if !saw {
		t.Fatalf("watch did not hit the changes endpoint: %v", h.fake.requests)
	}
}

func TestGetWatchStreamsCollectionChanges(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.changes = []substrate.Change{
		{Seq: 42, TS: testNow, Actor: substrate.ActorAPI, Op: substrate.OpPut, RecordID: "t9", Kind: "samples.substrate.reamde.dev/tasks/task"},
		{Seq: 43, TS: testNow, Actor: substrate.ActorAPI, Op: substrate.OpPatch, RecordID: "t9", Kind: "samples.substrate.reamde.dev/tasks/task"},
	}
	out, _ := h.mustRun("get", "samples.substrate.reamde.dev/tasks/task", "-w")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("get -w should stream bookmark + header + 2 changes, got:\n%s", out)
	}
	if !strings.HasPrefix(lines[2], "42 ") || !strings.HasPrefix(lines[3], "43 ") {
		t.Fatalf("get -w change lines:\n%s", out)
	}
}

func TestStreamChangesSkipsHeartbeats(t *testing.T) {
	// The collection watch shares the ndjson decoder; drive it directly.
	body := strings.NewReader(`{"bookmark":10}
{}
{"seq":11,"ts":"2026-08-02T12:00:00Z","actor":"api","op":"put","recordId":"t9","type":"samples.substrate.reamde.dev/tasks/task"}
`)
	var buf bytes.Buffer
	if err := streamChanges(&buf, body); err != nil {
		t.Fatalf("streamChanges: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "# watching from seq 10") || !strings.Contains(got, "t9") {
		t.Fatalf("stream output:\n%s", got)
	}
	if strings.Count(got, "\n") != 3 { // bookmark + header + one change
		t.Fatalf("heartbeat should be skipped:\n%q", got)
	}
}

// The operator hat is a different world: no token, no server, a database URL.
// Without one it must refuse cleanly rather than dial something at random.
func TestOperatorCommandsRefuseWithoutADatabaseURL(t *testing.T) {
	for _, args := range [][]string{
		{"repository", "list"},
		{"repository", "inspect", "geoah"},
		{"repository", "rebuild", "geoah"},
		{"user", "reset", "geoah", "--password-stdin"},
	} {
		h := newHarness(t)
		h.writeConfig() // a perfectly good token changes nothing here
		h.stdin.WriteString("hunter2\n")
		_, _, err := h.run(args...)
		if err == nil {
			t.Fatalf("substratectl %s ran without a database URL", strings.Join(args, " "))
		}
		if !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "--dsn") {
			t.Errorf("substratectl %s: error = %v, want it to name --dsn and DATABASE_URL", strings.Join(args, " "), err)
		}
		if len(h.fake.requests) != 0 {
			t.Errorf("substratectl %s spoke HTTP: %v", strings.Join(args, " "), h.fake.requests)
		}
	}
}

// The operator hat never borrows the user hat's credentials, and the user hat
// never learns the database URL: two hats, two worlds.
func TestOperatorHatDoesNotSpeakHTTP(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	// A DSN that resolves to nothing: the command must fail on the DATABASE,
	// not fall back to the server in the context.
	_, _, err := h.run("repository", "list", "--dsn", "postgres://127.0.0.1:1/none?connect_timeout=1&sslmode=disable")
	if err == nil {
		t.Fatal("expected a database failure")
	}
	if len(h.fake.requests) != 0 {
		t.Errorf("an operator command spoke HTTP: %v", h.fake.requests)
	}
}

func TestTokenCreateRequiresALabel(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	if _, _, err := h.run("token", "create"); err == nil || !strings.Contains(err.Error(), "label") {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenCreatePrintsSecretOnce(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("token", "create", "--label", "backup-script")
	for _, want := range []string{
		"token backup-script created",
		"id:      tk02",
		"secret (shown once, stored hashed): substrate_tok_",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("token create output missing %q:\n%s", want, out)
		}
	}
	if got := h.lastRequest(); got != "POST /tokens" {
		t.Errorf("mint hit %q, want POST /tokens", got)
	}
	if h.fake.lastAuth != "Bearer substrate_tok_geoah_test" {
		t.Errorf("a mint must send the context token, got %q", h.fake.lastAuth)
	}
	if _, ok := h.fake.lastBody["actors"]; ok {
		t.Error("mint still sends an actors set; a token has full access and no actor set")
	}
}

// --expires takes either form, because neither is derivable from the other and
// making the caller do date arithmetic is how expiries get skipped.
func TestTokenCreateExpiry(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"720h", testNow.Add(720 * time.Hour).UTC().Format(time.RFC3339)},
		{"2026-12-25T00:00:00Z", "2026-12-25T00:00:00Z"},
	}
	for _, tc := range cases {
		h := newHarness(t)
		h.writeConfig()
		out, _ := h.mustRun("token", "create", "--label", "seasonal", "--expires", tc.in)
		var got string
		if err := json.Unmarshal(h.fake.lastBody["expiresAt"], &got); err != nil {
			t.Fatalf("--expires %s: decode expiresAt: %v", tc.in, err)
		}
		if parsed, err := time.Parse(time.RFC3339, got); err != nil || !parsed.Equal(mustTime(t, tc.want)) {
			t.Errorf("--expires %s sent %q, want %q", tc.in, got, tc.want)
		}
		if !strings.Contains(out, "expires: ") {
			t.Errorf("--expires %s printed no expiry:\n%s", tc.in, out)
		}
	}
	h := newHarness(t)
	h.writeConfig()
	if _, _, err := h.run("token", "create", "--label", "x", "--expires", "next tuesday"); err == nil {
		t.Fatal("--expires accepted nonsense")
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return parsed
}

// A token list is metadata and never a secret; a token without an expiry lives
// until it is deleted, and the table says so rather than leaving a blank.
func TestTokenListShowsMetadataAndNeverASecret(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("token", "list")
	want := "ID     LABEL                 CREATED   EXPIRES\n" +
		"tk01   substratectl@laptop   2d        never\n" +
		"tk02   backup-script         1h        2026-09-01T12:00:00Z\n"
	if out != want {
		t.Fatalf("token list =\n%q\nwant\n%q", out, want)
	}
	if strings.Contains(out, "substrate_tok_") {
		t.Error("a token list printed a secret")
	}
	if got := h.lastRequest(); got != "GET /tokens" {
		t.Errorf("list hit %q, want GET /tokens", got)
	}
}

// Revoking is deleting the token record — no row means no access, and there is
// no revocation list to consult.
func TestTokenRevokeDeletesTheRecord(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("token", "revoke", "tk02")
	if got := h.lastRequest(); got != "DELETE /tokens/tk02" {
		t.Fatalf("revoke hit %q", got)
	}
	if !strings.Contains(out, "token tk02 revoked") {
		t.Errorf("revoke output:\n%s", out)
	}
}

// The password-factor rule: the current password and code go in
// the BODY, and a bearer token is not evidence — so the change carries no
// Authorization header at all.
func TestUserPasswordSendsBothFactorsAndNoBearer(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.stdin.WriteString("hunter2\nhunter3\n")
	out, _ := h.mustRun("user", "password", "--username", "geoah", "--totp-code", "123456",
		"--password-stdin", "--new-password-stdin")
	if got := h.lastRequest(); got != "POST /password" {
		t.Fatalf("password change hit %q", got)
	}
	for field, want := range map[string]string{
		"username": "geoah", "password": "hunter2", "totpCode": "123456", "newPassword": "hunter3",
	} {
		var got string
		if err := json.Unmarshal(h.fake.lastBody[field], &got); err != nil {
			t.Fatalf("decode %s: %v", field, err)
		}
		if got != want {
			t.Errorf("%s sent = %q, want %q", field, got, want)
		}
	}
	if h.fake.lastAuth != "" {
		t.Errorf("the credential change sent a bearer token (%q); the endpoint refuses one as evidence", h.fake.lastAuth)
	}
	if !strings.Contains(out, "password changed for geoah") {
		t.Errorf("output:\n%s", out)
	}
}

// Re-enrollment is the same two calls registration is: a candidate seed that
// writes nothing, then the swap proving a code from it.
func TestUserTOTPEnrollsThenSwaps(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.stdin.WriteString("hunter2\n654321\n")
	out, _ := h.mustRun("user", "totp", "--username", "geoah", "--totp-code", "123456", "--password-stdin")
	if got := h.fake.requests; len(got) != 2 || got[0] != "POST /totp/enroll" || got[1] != "POST /totp" {
		t.Fatalf("requests = %v, want the candidate then the swap", got)
	}
	for field, want := range map[string]string{
		"newTotpSecret": fakeTOTPSecret, "newTotpCode": "654321", "password": "hunter2",
	} {
		var got string
		if err := json.Unmarshal(h.fake.lastBody[field], &got); err != nil {
			t.Fatalf("decode %s: %v", field, err)
		}
		if got != want {
			t.Errorf("%s sent = %q, want %q", field, got, want)
		}
	}
	for _, want := range []string{
		"otpauth URI: " + fakeOtpauthURI,
		"the old secret keeps working until the code below is accepted",
		"second factor replaced for geoah",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestEnvOverridesConfig(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	t.Setenv("SUBSTRATE_SERVER", h.server)
	t.Setenv("SUBSTRATE_TOKEN", "substrate_tok_geoah_fromenv")
	h.mustRun("get", "samples.substrate.reamde.dev/tasks/task")
	if h.fake.lastAuth != "Bearer substrate_tok_geoah_fromenv" {
		t.Fatalf("SUBSTRATE_TOKEN did not override the config: %q", h.fake.lastAuth)
	}
}

func TestCanonicalAndAliasEnvVars(t *testing.T) {
	// SUBSTRATE_* is canonical and SS_* is the one accepted alias; the
	// canonical variable wins when both are set.
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"canonical", map[string]string{"SUBSTRATE_TOKEN": "substrate_tok_geoah_canon"}, "Bearer substrate_tok_geoah_canon"},
		{"ss alias", map[string]string{"SS_TOKEN": "substrate_tok_geoah_ss"}, "Bearer substrate_tok_geoah_ss"},
		{"canonical wins over the alias", map[string]string{
			"SUBSTRATE_TOKEN": "substrate_tok_geoah_canon",
			"SS_TOKEN":        "substrate_tok_geoah_ss",
		}, "Bearer substrate_tok_geoah_canon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.writeConfig()
			seedTask(h)
			t.Setenv("SUBSTRATE_SERVER", h.server)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			h.mustRun("get", "samples.substrate.reamde.dev/tasks/task")
			if h.fake.lastAuth != tc.want {
				t.Fatalf("auth header = %q, want %q", h.fake.lastAuth, tc.want)
			}
		})
	}
}

func TestNoServerConfiguredIsAClearError(t *testing.T) {
	h := newHarness(t)
	_, _, err := h.run("get", "samples.substrate.reamde.dev/tasks/task")
	if err == nil || !strings.Contains(err.Error(), "no substrate server configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestActorFlagIsSent(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	h.mustRun("--actor", "gmail.google.connectors.substrate.reamde.dev", "get", "samples.substrate.reamde.dev/tasks/task")
	// The fake records the auth header only; assert via a direct client too.
	cl := newClient(h.server, "substrate_tok_geoah_test", nil)
	cl.actor = "gmail.google.connectors.substrate.reamde.dev"
	req, err := cl.newRequest(context.Background(), "GET", "/api/v1/substrate.reamde.dev/core/kind", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("X-Substrate-Actor") != "gmail.google.connectors.substrate.reamde.dev" {
		t.Fatalf("actor header = %q", req.Header.Get("X-Substrate-Actor"))
	}
}

func TestHumanAge(t *testing.T) {
	base := testNow
	cases := []struct {
		then time.Time
		want string
	}{
		{base.Add(-30 * time.Second), "30s"},
		{base.Add(-5 * time.Minute), "5m"},
		{base.Add(-3 * time.Hour), "3h"},
		{base.Add(-72 * time.Hour), "3d"},
		{base.Add(-800 * 24 * time.Hour), "2y"},
		{time.Time{}, "<unknown>"},
	}
	for _, tc := range cases {
		if got := humanAge(base, tc.then); got != tc.want {
			t.Errorf("humanAge(%v) = %q, want %q", tc.then, got, tc.want)
		}
	}
}

func TestEditDiffAndRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	// A fake editor that rewrites the title in place — inside `data.properties`,
	// where everything authored lives. Written portably: BSD sed has no GNU
	// `-i`, so the rewrite goes through a temp file.
	editor := filepath.Join(t.TempDir(), "editor.sh")
	script := "#!/bin/sh\nsed 's/^    title: .*/    title: Edited by hand/' \"$1\" > \"$1.tmp\" && mv \"$1.tmp\" \"$1\"\n"
	if err := os.WriteFile(editor, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUBSTRATE_EDITOR", editor)
	out, _ := h.mustRun("edit", "tasks", "t9")
	if !strings.Contains(out, "-     title: Send rack layout to Alex") ||
		!strings.Contains(out, "+     title: Edited by hand") {
		t.Fatalf("edit diff:\n%s", out)
	}
	if !strings.Contains(out, "samples.substrate.reamde.dev/tasks/task/t9 updated") {
		t.Fatalf("edit apply result:\n%s", out)
	}
	if got := h.fake.record("t9"); got.Properties["title"] != "Edited by hand" {
		t.Fatalf("record title = %q", got.Properties["title"])
	}
}

func TestEditNoChangeIsNoWrite(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	t.Setenv("SUBSTRATE_EDITOR", "true")
	out, _ := h.mustRun("edit", "tasks", "t9")
	if !strings.Contains(out, "unchanged (edit canceled)") {
		t.Fatalf("edit output:\n%s", out)
	}
	for _, req := range h.fake.requests {
		if strings.HasPrefix(req, "PUT ") {
			t.Fatalf("a canceled edit must not write: %v", h.fake.requests)
		}
	}
}
