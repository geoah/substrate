package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheInstanceIDIsMintedOnceAndKept(t *testing.T) {
	root := t.TempDir()
	if id, err := loadInstanceID(root, true); err != nil || id != "" {
		t.Fatalf("read-only with no file = %q, %v; want nothing minted", id, err)
	}
	if _, err := os.Stat(filepath.Join(root, instanceFile)); !os.IsNotExist(err) {
		t.Fatalf("a read-only load wrote the file: %v", err)
	}
	first, err := loadInstanceID(root, false)
	if err != nil || !validInstanceID(first) {
		t.Fatalf("first boot = %q, %v", first, err)
	}
	for _, readOnly := range []bool{false, true} {
		if again, err := loadInstanceID(root, readOnly); err != nil || again != first {
			t.Fatalf("a later boot (read-only %v) = %q, %v; want %q", readOnly, again, err, first)
		}
	}
	if err := os.WriteFile(filepath.Join(root, instanceFile), []byte("not/an id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadInstanceID(root, false); err == nil {
		t.Fatal("a damaged instance file was accepted")
	}
}
