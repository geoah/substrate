package engine

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// An import's refold replays the declarations alone in its first pass, and
// commits the fold its two whole replays committed: the kept directory,
// imported both ways, refolds to the same snapshot, and the first way took
// no fallback to them.
func TestARefoldFromTheDeclarationsFoldsWhatTwoWholeReplaysFold(t *testing.T) {
	t.Parallel()
	kept := filepath.Join("..", "testenv", "testdata", "kept", "v0.123.1")
	fast, logs, declarations, lines := refoldedKeptDirectory(t, kept, false)
	whole, _, _, _ := refoldedKeptDirectory(t, kept, true)
	if declarations == 0 || declarations >= lines {
		t.Fatalf("%d of the history's %d lines carry an effect on a declaration row; the first pass reads none or all of it", declarations, lines)
	}
	if fast != whole {
		t.Errorf("the refold from the declarations folded another fold than two whole replays:\n%s", firstDifferenceOf([]byte(fast), []byte(whole)))
	}
	if strings.Contains(logs, "replaying it twice") || strings.Contains(logs, "replaying the whole history twice") {
		t.Errorf("the refold fell back to two whole replays:\n%s", logs)
	}
}

// refoldedKeptDirectory imports the kept directory into a database of its
// own and returns the fold its refold's last replay left, read inside the
// refold's transaction (before the open writes anything of its own), with
// what the boot logged, how many lines the declarations pass reads and how
// many the history holds.
func refoldedKeptDirectory(t *testing.T, kept string, whole bool) (string, string, int, int) {
	t.Helper()
	ctx := context.Background()
	raw, err := os.ReadFile(filepath.Join(kept, "fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Repository    string `json:"repository"`
		CredentialKey string `json:"credentialKey"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := copyTree(filepath.Join(kept, "repositories"), filepath.Join(root, "repositories")); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var last string
	declarations, lines := -1, -1
	end := func(tx *sql.Tx) {
		mu.Lock()
		if lines < 0 {
			seqs, err := declarationLineSeqs(ctx, tx)
			if err != nil {
				t.Errorf("read the declaration lines: %v", err)
			}
			declarations = len(seqs)
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM changelog`).Scan(&lines); err != nil {
				t.Errorf("count the lines: %v", err)
			}
		}
		mu.Unlock()
		snap, err := foldSnapshot(ctx, tx)
		if err != nil {
			t.Errorf("snapshot the replay's fold: %v", err)
			return
		}
		out, err := json.MarshalIndent(snap, "", "  ")
		if err != nil {
			t.Errorf("snapshot the replay's fold: %v", err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		last = string(out)
	}
	var logs lockedBuffer
	svc, err := OpenForTest(t, ctx, MigratedDSN(t), WithKindsFS(SeedKinds), WithDataRoot(root),
		WithCredentialKey(fixture.CredentialKey), WithTestReplay(nil, end),
		WithLogger(slog.New(slog.NewTextHandler(&logs, nil))),
		func(o *options) { o.refoldWhole = whole })
	if err != nil {
		t.Fatalf("import the kept directory: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	mu.Lock()
	defer mu.Unlock()
	if last == "" {
		t.Fatal("the import replayed nothing")
	}
	return last, logs.String(), declarations, lines
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
