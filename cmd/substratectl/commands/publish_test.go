package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishCheckPrintsEntryAndRefusesMutations(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "closure.yaml")
	doc := filepath.Join(dir, "authority.json")
	body := []byte("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: example.com/notes\ndata:\n  authority: example.com\n  package: notes\n  version: 2\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: example.com/notes\ndata:\n  authority: example.com\n  package: notes\n")
	body = append(body, []byte("  installs: [example.com/notes/note]\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: example.com/notes/note\ndata:\n  authority: example.com\n  package: notes\n  names:\n    singular: note\n  properties:\n    name:\n      type: string\n")...)
	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc, []byte(`{"authority":"example.com","packages":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"publish", "example.com/notes", "--check", "--file", file, "--version", "2", "--closure", "closure.yaml", "--document", doc}
	out, _ := h.mustRun(args...)
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if !strings.Contains(out, digest) || !strings.Contains(out, `"version": 2`) {
		t.Fatal(out)
	}
	raw := fmt.Sprintf(`{"authority":"example.com","packages":[{"package":"notes","versions":[{"version":2,"closure":"closure.yaml","sha256":"%s"}]}]}`, strings.Repeat("a", 64))
	if err := os.WriteFile(doc, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := h.run(args...)
	if err == nil || !strings.Contains(err.Error(), "another digest") {
		t.Fatal(err)
	}
	raw = strings.Replace(raw, `"version":2`, `"version":3`, 1)
	if err := os.WriteFile(doc, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = h.run(args...)
	if err == nil || !strings.Contains(err.Error(), "below") {
		t.Fatal(err)
	}
}
