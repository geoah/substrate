package commands

import (
	"strings"
	"testing"
)

// `substratectl user reset` WRITES sealed material (a password hash and a TOTP seed).
// Without a credential key the engine would store that material as a
// plain-marked payload it later accepts — persistent silent plaintext of the
// account's factors. The command must REFUSE (non-zero exit) unless the key is
// present, and it must refuse BEFORE it opens the database, so nothing is
// written. A missing key is not a warning-and-proceed.
func TestUserResetRefusesWithoutCredentialKey(t *testing.T) {
	h := newHarness(t)
	// The key is the thing under test; a developer's real one must not leak in.
	t.Setenv("SUBSTRATE_CREDENTIAL_KEY", "")
	// A password IS supplied, so the only reason to refuse is the missing key —
	// and the DSN points nowhere reachable, so a refusal that names the key
	// proves the command never tried to connect, let alone write.
	h.stdin.WriteString("a-brand-new-password\n")
	_, _, err := h.run("user", "reset", "victim",
		"--dsn", "postgres://u@127.0.0.1:1/none?sslmode=disable", "--password-stdin")
	if err == nil {
		t.Fatal("user reset without a credential key succeeded; it must refuse")
	}
	if !strings.Contains(err.Error(), credentialKeyEnv) {
		t.Fatalf("reset failed with %q; want a refusal naming %s (a missing key must not fall through to a plaintext write)",
			err, credentialKeyEnv)
	}
}

// The refusal is specific to WRITING sealed material: rebuild only reads and
// re-links it, so it opens with no key at all and fails instead on the
// unreachable database — never on a missing key.
func TestRepositoryRebuildNeedsNoCredentialKey(t *testing.T) {
	h := newHarness(t)
	t.Setenv("SUBSTRATE_CREDENTIAL_KEY", "")
	_, _, err := h.run("repository", "rebuild", "geoah",
		"--dsn", "postgres://u@127.0.0.1:1/none?sslmode=disable")
	if err == nil {
		t.Fatal("rebuild against an unreachable database succeeded")
	}
	if strings.Contains(err.Error(), credentialKeyEnv) {
		t.Fatalf("rebuild refused for a missing credential key (%q); a read-only command must not need it", err)
	}
}
