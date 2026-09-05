package engine_test

// A store written before references absorbed the edge refuses to OPEN
// (decision 0044). Migration 0010 drops the `edges` table, so a dialect-1
// changelog has already lost what its `link` and `unlink` entries fold into:
// opening it would serve a repository whose links are silently gone and leave
// the refusal to the day somebody rebuilds.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

func TestOpenRefusesAChangelogThatPredatesReferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	_ = svc.Close()

	maint, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleMaint)
	if err != nil {
		t.Fatalf("open the maintenance pool: %v", err)
	}
	defer func() { _ = maint.Close() }()

	// A pre-0044 store, as an in-place upgrade would find it: entries written
	// with the retired op, under the dialect that wrote them. The application
	// role may raise a stamp but not wind one back, so this runs as maint.
	if _, err := maint.ExecContext(ctx, `UPDATE changelog_dialect SET dialect = 1`); err != nil {
		t.Fatalf("wind the stamp back: %v", err)
	}
	if _, err := maint.ExecContext(ctx,
		`UPDATE changelog SET op = 'link' WHERE seq = (SELECT min(seq) FROM changelog)`); err != nil {
		t.Fatalf("plant a legacy link entry: %v", err)
	}

	svc2 := open()
	defer func() { _ = svc2.Close() }()
	_, err = svc2.Dataset(ctx, "geoah")
	if err == nil {
		t.Fatal("a changelog holding `link` entries must refuse the open")
	}
	if !errors.Is(err, engine.ErrChangelogPredatesReferences) {
		t.Fatalf("open = %v, want ErrChangelogPredatesReferences", err)
	}
	for _, want := range []string{"predates reference-only links", "wipe the store"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("open = %q, want it to say %q", err, want)
		}
	}

	// And the refusal is about the ENTRIES, not the stamp: a store below the
	// binary's maximum with nothing retired in it opens, which is every fresh
	// repository between migrations.
	if _, err := maint.ExecContext(ctx,
		`UPDATE changelog SET op = 'put' WHERE op = 'link'`); err != nil {
		t.Fatalf("restore the entry: %v", err)
	}
	svc3 := open()
	defer func() { _ = svc3.Close() }()
	if _, err := svc3.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("a store below the maximum with no retired entry must open: %v", err)
	}
}
