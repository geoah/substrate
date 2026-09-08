package commands

import (
	"strings"
	"testing"
)

// The snapshot promises every sealed file opens, so it refuses without the
// key before it reads the DSN or opens anything, and it names the variable.
func TestRepositorySnapshotRefusesWithoutTheCredentialKey(t *testing.T) {
	h := newHarness(t)
	t.Setenv("SUBSTRATE_CREDENTIAL_KEY", "")
	_, _, err := h.run("repository", "snapshot", "ada", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "SUBSTRATE_CREDENTIAL_KEY") {
		t.Fatalf("a keyless snapshot must be refused naming the key, got %v", err)
	}
	if strings.Contains(err.Error(), "database URL") {
		t.Fatalf("the key is checked before the DSN: %v", err)
	}
}
