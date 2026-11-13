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

func TestPublishCheckRequiresConsentToNarrow(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	old := []byte("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: example.com/notes\ndata:\n  authority: example.com\n  package: notes\n  version: 1\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: example.com/notes\ndata:\n  authority: example.com\n  package: notes\n  installs: [example.com/notes/note]\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: example.com/notes/note\ndata:\n  authority: example.com\n  package: notes\n  names:\n    singular: note\n  properties:\n    name:\n      type: string\n")
	candidate := strings.Replace(string(old), "version: 1", "version: 2", 1)
	candidate = strings.Replace(candidate, "    name:\n      type: string\n", "    heading:\n      type: string\n", 1)
	sum := sha256.Sum256(old)
	doc := fmt.Sprintf(`{"authority":"example.com","packages":[{"package":"notes","versions":[{"version":1,"closure":"old.yaml","sha256":"%s"}]}]}`, hex.EncodeToString(sum[:]))
	for name, body := range map[string][]byte{"old.yaml": old, "candidate.yaml": []byte(candidate), "authority.json": []byte(doc)} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"publish", "example.com/notes", "--check", "--file", filepath.Join(dir, "candidate.yaml"), "--version", "2", "--closure", "candidate.yaml", "--document", filepath.Join(dir, "authority.json")}
	_, _, err := h.run(args...)
	if err == nil || !strings.Contains(err.Error(), "--allow-narrowing") {
		t.Fatal(err)
	}
	h.mustRun(append(args, "--allow-narrowing")...)
}

// A published kind stays: a candidate that drops one is refused whatever
// --allow-narrowing says, and so is one that drops a kind an older version
// published, though the newest already lacks it. Declaring a new kind beside
// a removed one is no rename the movedFrom check would catch (decision 0269).
func TestPublishCheckRefusesARemovedKind(t *testing.T) {
	h := newHarness(t)
	release := func(version int, kinds ...string) []byte {
		body := fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: example.com/notes\ndata:\n  authority: example.com\n  package: notes\n  version: %d\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: example.com/notes\ndata:\n  authority: example.com\n  package: notes\n  installs: [", version)
		for i, k := range kinds {
			if i > 0 {
				body += ", "
			}
			body += "example.com/notes/" + k
		}
		body += "]\n"
		for _, k := range kinds {
			body += fmt.Sprintf("---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: example.com/notes/%[1]s\ndata:\n  authority: example.com\n  package: notes\n  names:\n    singular: %[1]s\n  properties:\n    name:\n      type: string\n", k)
		}
		return []byte(body)
	}
	check := func(t *testing.T, published map[int][]byte, candidate []byte, version int, want string) {
		t.Helper()
		dir := t.TempDir()
		var entries []string
		for v, body := range published {
			name := fmt.Sprintf("v%d.yaml", v)
			sum := sha256.Sum256(body)
			entries = append(entries, fmt.Sprintf(`{"version":%d,"closure":%q,"sha256":"%s"}`, v, name, hex.EncodeToString(sum[:])))
			if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		doc := `{"authority":"example.com","packages":[{"package":"notes","versions":[` + strings.Join(entries, ",") + `]}]}`
		for name, body := range map[string][]byte{"candidate.yaml": candidate, "authority.json": []byte(doc)} {
			if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err := h.run("publish", "example.com/notes", "--check", "--allow-narrowing", "--file", filepath.Join(dir, "candidate.yaml"),
			"--version", fmt.Sprint(version), "--closure", "candidate.yaml", "--document", filepath.Join(dir, "authority.json"))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("publish --check --allow-narrowing = %v, want a refusal naming %q", err, want)
		}
	}
	t.Run("from the newest release", func(t *testing.T) {
		check(t, map[int][]byte{1: release(1, "note")}, release(2, "memo"), 2, "removes example.com/notes/note, which version 1 published")
	})
	t.Run("from an older release the newest already dropped", func(t *testing.T) {
		check(t, map[int][]byte{1: release(1, "note"), 2: release(2, "note", "memo"), 3: release(3, "memo")},
			release(4, "memo"), 4, "removes example.com/notes/note, which version")
	})
}
