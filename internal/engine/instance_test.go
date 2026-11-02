package engine

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// Two first boots racing over one data root persist one id, and both answer
// it.
func TestConcurrentFirstBootsAnswerOneInstanceID(t *testing.T) {
	for range 20 {
		root := t.TempDir()
		const n = 16
		ids := make([]string, n)
		errs := make([]error, n)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				ids[i], errs[i] = loadInstanceID(root, false)
			}()
		}
		close(start)
		wg.Wait()
		raw, err := os.ReadFile(filepath.Join(root, instanceFile))
		if err != nil {
			t.Fatal(err)
		}
		persisted := strings.TrimSpace(string(raw))
		for i := range n {
			if errs[i] != nil || ids[i] != persisted {
				t.Fatalf("boot %d answered %q, %v; the file holds %q", i, ids[i], errs[i], persisted)
			}
		}
		left, err := filepath.Glob(filepath.Join(root, "*.tmp"))
		if err != nil || len(left) != 0 {
			t.Fatalf("temporary files left behind: %v, %v", left, err)
		}
	}
}
