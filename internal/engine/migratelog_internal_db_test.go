package engine

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/testdb"
)

// migrationLog is a log sink the engine's goroutines may write while the test
// reads it.
type migrationLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *migrationLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// migrationLines are the runner's lines b holds, each as its message, its
// version and its name, and whether it says how long it took.
func (b *migrationLog) migrationLines(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("a log line that is not JSON: %q", line)
		}
		if msg, _ := rec["msg"].(string); strings.Contains(msg, "SQL migration") {
			out = append(out, rec)
		}
	}
	return out
}

func (b *migrationLog) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

// EVERY MIGRATION A START APPLIES IS LOGGED (issue #873): one INFO line for
// each, with its number, its name and how long it took; one line when none
// is pending; and nothing from a read-only open, which applies none.
func TestAStartLogsEachMigrationItAppliesAndWhenNoneIsPending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 3 {
		t.Fatalf("the binary embeds %d migrations", len(migrations))
	}
	staged := migrations[len(migrations)-3].Version
	pending := migrations[len(migrations)-2:]
	dsn := testdb.NewSchema(t)
	// The roles first, as a start makes them before it migrates: the grants
	// the staged migrations make are to them.
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if err := ensureRoles(ctx, raw); err != nil {
		t.Fatalf("make the roles: %v", err)
	}
	if err := MigrateThrough(ctx, dsn, staged); err != nil {
		t.Fatalf("stage the database at %d: %v", staged, err)
	}
	logs := &migrationLog{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	root := t.TempDir()
	start := func(which string, opts ...Option) {
		t.Helper()
		svc, err := OpenForTest(t, ctx, dsn, append([]Option{
			WithDataRoot(root), WithKindsFS(SeedKinds), WithCredentialKey(TestCredentialKey), WithLogger(log),
		}, opts...)...)
		if err != nil {
			t.Fatalf("the %s start: %v", which, err)
		}
		if err := svc.Close(); err != nil {
			t.Fatalf("stop the %s start: %v", which, err)
		}
	}

	start("first")
	lines := logs.migrationLines(t)
	if len(lines) != len(pending) {
		t.Fatalf("the start applying %d migrations logged %d lines of them: %v", len(pending), len(lines), lines)
	}
	for i, m := range pending {
		got := lines[i]
		if got["msg"] != "substrate: ran a SQL migration" || got["level"] != "INFO" ||
			got["version"] != float64(m.Version) || got["name"] != m.Name || got["took"] == nil {
			t.Errorf("the line of migration %d (%s) is %v", m.Version, m.Name, got)
		}
	}

	logs.reset()
	start("second")
	lines = logs.migrationLines(t)
	newest := migrations[len(migrations)-1].Version
	if len(lines) != 1 || lines[0]["msg"] != "substrate: ran no SQL migration: none is pending" ||
		lines[0]["level"] != "INFO" || lines[0]["version"] != float64(newest) {
		t.Errorf("a start with nothing pending logged %v, want one line naming %d", lines, newest)
	}

	logs.reset()
	start("read-only", WithDirectoryReadOnly())
	if lines := logs.migrationLines(t); len(lines) != 0 {
		t.Errorf("a read-only open logged %v, want nothing: it applies none", lines)
	}
}
