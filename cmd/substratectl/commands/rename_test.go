package commands

// `rename` presents the person and the factors and no token, asks for the
// code the way a login does, and leaves the context naming the repository by its new name,
// the old one listed with the moment it lapses.

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenamePresentsTheFactorsAndTheContextTakesTheNewName(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	before, err := loadConfig(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	old := before.Contexts[0].Repository
	h.stdin.WriteString("hunter2\n")
	out, _ := h.mustRun("rename", "ada.example.org", "--password-stdin", "--totp-code", "654321")
	if got := h.lastRequest(); got != "POST "+pathRename {
		t.Fatalf("rename hit %q, want the rename door", got)
	}
	if h.fake.lastAuth != "" {
		t.Errorf("rename sent a bearer token (%q): the factors are the evidence", h.fake.lastAuth)
	}
	var identity, repository string
	_ = json.Unmarshal(h.fake.lastBody["identity"], &identity)
	_ = json.Unmarshal(h.fake.lastBody["repository"], &repository)
	if want := firstNonEmpty(before.Contexts[0].Identity, old); identity != want || repository != old {
		t.Errorf("rename named the person %q and the repository %q, want %q and %q", identity, repository, want, old)
	}
	if !strings.Contains(out, "renamed "+old+" to ada.example.org") || !strings.Contains(out, old+" still resolves to it until") {
		t.Errorf("rename output:\n%s", out)
	}
	cfg, err := loadConfig(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Contexts[0]; got.Repository != "ada.example.org" || got.Token != before.Contexts[0].Token {
		t.Errorf("the context after the rename = %+v, want the new name and the same token", got)
	}
}

// A rename sent to another server than the context's, by --server, leaves the
// saved context as it is: it names a repository the rename did not touch.
func TestRenameThroughAnotherServerLeavesTheContextAlone(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	before, err := loadConfig(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	saved := before.Contexts[0]
	saved.Server = "https://other.example.com"
	before.upsertContext(saved)
	if err := saveConfig(h.configPath, before); err != nil {
		t.Fatal(err)
	}
	h.stdin.WriteString("hunter2\n")
	_, errOut := h.mustRun("rename", "ada.example.org", "--server", h.server, "--password-stdin", "--totp-code", "654321")
	if !strings.Contains(errOut, "left as it is") {
		t.Errorf("rename through another server said:\n%s", errOut)
	}
	after, err := loadConfig(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := after.Contexts[0]; got.Repository != saved.Repository || got.Server != saved.Server {
		t.Errorf("the context after a rename through another server = %+v, want it as it was, %+v", got, saved)
	}
}
