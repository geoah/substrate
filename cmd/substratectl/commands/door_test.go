package commands

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The config file is the credential store: what it holds, where it holds it,
// and what it must never hold again.
func TestConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	cfg := &Config{}
	cfg.upsertContext(Context{
		Name: "geoah", Server: "https://substrate.example.com",
		Username: "geoah", Token: "substrate_tok_abc", TokenID: "tk01",
	})
	if err := saveConfig(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.CurrentContext != "geoah" || len(got.Contexts) != 1 {
		t.Fatalf("round trip = %+v", got)
	}
	if c := got.Contexts[0]; c.Server == "" || c.Username != "geoah" || c.Token != "substrate_tok_abc" || c.TokenID != "tk01" {
		t.Fatalf("context = %+v", c)
	}
	// The directory holding secrets is 0700 and the file 0600.
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Errorf("config directory = %o, want 700", perm)
	}

	// Upserting the same name REPLACES rather than appends, or a second login
	// would leave the first secret lying in the file.
	cfg.upsertContext(Context{Name: "geoah", Server: "https://other", Username: "geoah", Token: "substrate_tok_def"})
	if len(cfg.Contexts) != 1 || cfg.Contexts[0].Token != "substrate_tok_def" {
		t.Fatalf("upsert appended: %+v", cfg.Contexts)
	}

	// Forgetting drops the secret and keeps the address.
	if !cfg.forgetToken("geoah") {
		t.Fatal("forgetToken found no context")
	}
	if c := cfg.Contexts[0]; c.Token != "" || c.TokenID != "" || c.Server == "" || c.Username == "" {
		t.Fatalf("after forget = %+v", c)
	}
	if cfg.forgetToken("nobody") {
		t.Error("forgetToken invented a context")
	}
	// A file that does not exist is an empty config, not an error: the first
	// login must not need a file to already be there.
	empty, err := loadConfig(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil || len(empty.Contexts) != 0 {
		t.Fatalf("absent config = %+v, %v", empty, err)
	}
}

// The collection path IS the kind reference, split into segments: three for a
// published kind (authority, package, name), one for a repository-local one.
// An id carrying a slash, and a declaration record's id IS a kind reference,
// travels percent-encoded rather than as more path segments.
func TestCollectionPathIsTheKindReference(t *testing.T) {
	cases := []struct {
		pkg, kind string
		id        []string
		want      string
	}{
		{"samples.substrate.reamde.dev/tasks", "task", nil, "/api/v1/samples.substrate.reamde.dev/tasks/task"},
		{"samples.substrate.reamde.dev/tasks", "task", []string{"t9"}, "/api/v1/samples.substrate.reamde.dev/tasks/task/t9"},
		{"", "task", nil, "/api/v1/task"},
		{"", "task", []string{"t9"}, "/api/v1/task/t9"},
		{
			"substrate.reamde.dev/core", "kind",
			[]string{"samples.substrate.reamde.dev/tasks/task"},
			"/api/v1/substrate.reamde.dev/core/kind/samples.substrate.reamde.dev%2Ftasks%2Ftask",
		},
		{"samples.substrate.reamde.dev/tasks", "task", []string{"t9", "incoming"}, "/api/v1/samples.substrate.reamde.dev/tasks/task/t9/incoming"},
	}
	for _, tc := range cases {
		if got := collectionPath(tc.pkg, tc.kind, tc.id...); got != tc.want {
			t.Errorf("collectionPath(%q, %q, %v) = %q, want %q", tc.pkg, tc.kind, tc.id, got, tc.want)
		}
	}
	// The encoded id survives the round trip a server does on it.
	if got, err := url.PathUnescape("samples.substrate.reamde.dev%2Ftasks%2Ftask"); err != nil || got != "samples.substrate.reamde.dev/tasks/task" {
		t.Fatalf("unescape = %q, %v", got, err)
	}
}

// The enrollment and the commit are one gesture to a person and two requests
// to a paced door. A seed already typed into an authenticator must not be
// thrown away over five seconds.
func TestRegisterWaitsOutTheDoorsPacing(t *testing.T) {
	h := newHarness(t)
	h.fake.paceRegisterOnce = true
	h.fake.retryAfter = "1" // the door's real spacing is seconds; one is enough to prove it
	h.stdin.WriteString("hunter2\nhunter2\n123456\n")
	out, errOut := h.mustRun("register", "--server", h.server, "--invite-code", "let-me-in", "--username", "geoah")
	if !strings.Contains(errOut, "paces this door") {
		t.Errorf("stderr did not explain the wait:\n%s", errOut)
	}
	if !strings.Contains(out, "registered geoah") {
		t.Errorf("the retry did not land:\n%s", out)
	}
	if got := h.fake.doorRequests(); len(got) != 3 {
		t.Fatalf("requests = %v, want the enrollment and TWO commits", got)
	}
}

// A lockout is news, not something to wait out silently: the wait is bounded,
// and anything longer surfaces as the error it is.
func TestRegisterDoesNotWaitOutALockout(t *testing.T) {
	h := newHarness(t)
	h.fake.authStatus = 429
	h.fake.retryAfter = "3600"
	h.stdin.WriteString("hunter2\nhunter2\n123456\n")
	_, _, err := h.run("register", "--server", h.server, "--invite-code", "let-me-in",
		"--username", "geoah", "--totp-secret", "MFRGGZDFMZTWQ2LK")
	if err == nil {
		t.Fatal("an hour-long lockout was waited out")
	}
	if got := h.fake.doorRequests(); len(got) != 1 {
		t.Errorf("requests = %v, want the one refused commit", got)
	}
}
