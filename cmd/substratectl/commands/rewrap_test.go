package commands

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

// `repository rewrap` rewrites a manifest so SUBSTRATE_CREDENTIAL_KEY opens
// it, so without the key there is nothing to rewrite under: it refuses,
// naming the variable, BEFORE it reads the recovery key. The DSN plays no
// part: the command is offline.
func TestRepositoryRewrapRefusesWithoutCredentialKey(t *testing.T) {
	h := newHarness(t)
	t.Setenv("SUBSTRATE_CREDENTIAL_KEY", "")
	h.stdin.WriteString("AGE-SECRET-KEY-1NOTREAD\n")
	_, _, err := h.run("repository", "rewrap", t.TempDir(), "--identity-stdin")
	if err == nil {
		t.Fatal("rewrap without a credential key succeeded; it must refuse")
	}
	if !strings.Contains(err.Error(), credentialKeyEnv) {
		t.Fatalf("rewrap failed with %q; want a refusal naming %s", err, credentialKeyEnv)
	}
	if h.stdin.Len() == 0 {
		t.Fatal("the recovery key was read before the missing key was reported")
	}
}

// A directory with no repository.json is refused by name, nothing is
// synthesized, and the recovery key never reaches stdout or stderr. No
// database is opened: the harness clears DATABASE_URL and no --dsn is given.
func TestRepositoryRewrapRefusesADirectoryWithNoManifest(t *testing.T) {
	h := newHarness(t)
	key := make([]byte, 32)
	t.Setenv("SUBSTRATE_CREDENTIAL_KEY", base64.StdEncoding.EncodeToString(key))
	dir := filepath.Join(t.TempDir(), "ada.example.com")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A real identity: the engine holds the recovery key to age's grammar
	// before it opens the directory, so a made-up one would be refused for
	// the wrong reason.
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	identity := id.String()
	h.stdin.WriteString(identity + "\n")
	stdout, stderr, err := h.run("repository", "rewrap", dir, "--identity-stdin")
	if err == nil {
		t.Fatal("a directory with no manifest rewrapped")
	}
	if !strings.Contains(err.Error(), "repository.json") {
		t.Fatalf("the refusal must name the manifest: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "repository.json")); serr == nil {
		t.Fatal("the rewrap synthesized a manifest")
	}
	for _, s := range []string{stdout, stderr, err.Error()} {
		if strings.Contains(s, identity) {
			t.Fatalf("the recovery key reached the output: %q", s)
		}
	}
}
