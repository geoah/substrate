package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The entry `publish --check` prints lists the dialect the closure needs
// (decision 0268): what its keys need where its header states less or
// nothing, so a server older than a key passes the version over; and a
// header that states less than its keys need is refused before any entry is
// printed, naming the key.
func TestPublishCheckListsTheDialectTheClosureNeeds(t *testing.T) {
	closure := func(header string) []byte {
		return []byte("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: example.com/notes\ndata:\n  authority: example.com\n  package: notes\n  version: 2\n" + header +
			"---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: example.com/notes\ndata:\n  authority: example.com\n  package: notes\n  installs: [example.com/notes/note]\n" +
			"---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: example.com/notes/note\ndata:\n  authority: example.com\n  package: notes\n  names:\n    singular: note\n  properties:\n    name:\n      type: string\n")
	}
	check := func(t *testing.T, body []byte) (string, error) {
		t.Helper()
		h := newHarness(t)
		dir := t.TempDir()
		file, doc := filepath.Join(dir, "closure.yaml"), filepath.Join(dir, "authority.json")
		if err := os.WriteFile(file, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(doc, []byte(`{"authority":"example.com","packages":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		out, _, err := h.run("publish", "example.com/notes", "--check", "--file", file, "--version", "2", "--closure", "closure.yaml", "--document", doc)
		return out, err
	}
	t.Run("a header that states none lists what its keys need", func(t *testing.T) {
		out, err := check(t, closure("  stability: stable\n"))
		if err != nil || !strings.Contains(out, `"dialect": 3`) {
			t.Fatalf("publish --check = %v\n%s\nwant the entry listed at dialect 3, which `stability` needs", err, out)
		}
	})
	t.Run("a closure whose keys need none lists none", func(t *testing.T) {
		out, err := check(t, closure(""))
		if err != nil || strings.Contains(out, `"dialect"`) {
			t.Fatalf("publish --check = %v\n%s\nwant the entry with no dialect", err, out)
		}
	})
	t.Run("a header that states less is refused naming the key", func(t *testing.T) {
		_, err := check(t, closure("  dialect: 1\n  stability: stable\n"))
		if err == nil || !strings.Contains(err.Error(), "which dialect 3 introduced") {
			t.Fatalf("publish --check = %v, want refused naming the key and dialect 3", err)
		}
	})
	// Every word `writer:` took before dialect 5 is refused now, so any
	// `writer:` a package carries needs 5 (decision 0290).
	written := func(header string) []byte {
		return []byte(strings.Replace(string(closure(header)), "      type: string\n", "      type: string\n      writer: person\n", 1))
	}
	t.Run("a writer with no stated dialect lists 5", func(t *testing.T) {
		out, err := check(t, written(""))
		if err != nil || !strings.Contains(out, `"dialect": 5`) {
			t.Fatalf("publish --check = %v\n%s\nwant the entry listed at dialect 5, which `writer:` needs", err, out)
		}
	})
	t.Run("a header that states 4 beside a writer is refused naming 5", func(t *testing.T) {
		_, err := check(t, written("  dialect: 4\n"))
		if err == nil || !strings.Contains(err.Error(), "which dialect 5 introduced") {
			t.Fatalf("publish --check = %v, want refused naming `writer` and dialect 5", err)
		}
	})
}
