package testenv_test

// THE KEPT DIRECTORY ACROSS ITS CONVERSION (decision 0309). A release's
// repository, imported and converted under the current code, is what every
// deployment that moved from it holds: started again it replays nothing, its
// fold rows standing where the first start wrote them. And for every step
// of the chain, its history taken back below that step's dialect converts
// with no line changed, and the fold it leaves is the fold a rebuild of the
// history makes: a step that changes how a line folds without changing the
// line, and does not say Refolds, fails here.

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
)

func TestTheKeptDirectoryAcrossItsConversionReplaysOnlyWhatChanged(t *testing.T) {
	dir := filepath.Join(keptRoot, "v0.123.1")
	var fixture keptFixture
	readKept(t, filepath.Join(dir, "fixture.json"), &fixture)
	root := t.TempDir()
	copyKept(t, filepath.Join(dir, changelogfile.RepositoriesDir), filepath.Join(root, changelogfile.RepositoriesDir))
	dsn := testdb.NewSchema(t)
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var id string
	query := func(q string) string {
		t.Helper()
		var out string
		if err := db.QueryRowContext(ctx, q, id).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	// The open's search reindex and index re-derivation run behind it and
	// rewrite rows as they go: what a start left is read once they are done.
	start := func() (*testenv.Env, []byte) {
		t.Helper()
		e := testenv.Start(t, testenv.WithoutRegistration(), testenv.WithRepository(fixture.Repository),
			testenv.WithDSN(dsn), testenv.WithDataRoot(root), testenv.WithCredentialKey(fixture.CredentialKey))
		e.Token = fixture.Token
		ds, err := e.Service.Dataset(ctx, fixture.Repository)
		if err != nil {
			t.Fatal(err)
		}
		id = ds.Repository().ID
		waitFor(t, "the passes behind the open", func() bool {
			return query(`SELECT (EXISTS (SELECT 1 FROM search_index WHERE repository = $1)
				AND NOT EXISTS (SELECT 1 FROM index_reprojections WHERE repository = $1))::text`) == "true"
		})
		fold, err := ds.(folded).FoldSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return e, fold
	}
	// Where every row stands and which transaction wrote it: a replay
	// rewrites each one.
	rows := func() string {
		t.Helper()
		return query(`SELECT coalesce(string_agg(ctid::text || '@' || xmin::text, ' ' ORDER BY kind, id), '') FROM records WHERE repository = $1`) +
			"|" + query(`SELECT coalesce(string_agg(ctid::text || '@' || xmin::text, ' ' ORDER BY record_kind, record_id, property), '') FROM property_marks WHERE repository = $1`)
	}
	lines := func() string {
		t.Helper()
		return query(`SELECT coalesce(string_agg(seq::text || ':' || encode(hash, 'hex'), ' ' ORDER BY seq), '') FROM changelog WHERE repository = $1`)
	}

	e, fold := start()
	standing, history := rows(), lines()
	e.Stop()

	// A deployment that moved: started again, it converts nothing and
	// replays nothing.
	e, again := start()
	if lines() != history {
		t.Fatal("a start of a repository already converted and folded wrote to its history")
	}
	if rows() != standing {
		t.Fatal("a start of a repository already converted and folded rebuilt its fold")
	}
	if !bytes.Equal(again, fold) {
		t.Fatal("a start of a repository already converted and folded changed its fold")
	}
	e.Stop()

	// Each step, its history taken back below it, and the fold tables it
	// came with emptied as the schema migration beside it left them.
	for dialect := 2; dialect <= engine.MaxChangelogDialect(); dialect++ {
		if _, err := db.ExecContext(ctx, `UPDATE changelog_dialect SET dialect = $2 WHERE repository = $1`, id, dialect-1); err != nil {
			t.Fatal(err)
		}
		for _, table := range keptStepFoldTables[dialect] {
			if _, err := db.ExecContext(ctx, `DELETE FROM `+table+` WHERE repository = $1`, id); err != nil {
				t.Fatal(err)
			}
		}
		e, got := start()
		if lines() != history {
			t.Fatalf("the steps from %d changed a line of a history already in their dialect", dialect)
		}
		if !bytes.Equal(got, fold) {
			t.Fatalf("after the steps from %d the fold is not the one the history folds to", dialect)
		}
		if _, err := e.Service.(interface {
			RebuildRepository(context.Context, string) (engine.RebuildReport, error)
		}).RebuildRepository(ctx, id); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
		ds, err := e.Service.Dataset(ctx, fixture.Repository)
		if err != nil {
			t.Fatal(err)
		}
		rebuilt, err := ds.(folded).FoldSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(rebuilt, fold) {
			t.Fatalf("after the steps from %d a rebuild folds the history to another fold: a step changes how a line folds without changing the line, and says no Refolds", dialect)
		}
		e.Stop()
	}
}

// keptStepFoldTables is the fold tables a conversion step came with, by its
// dialect: a schema migration created each empty beside lines that already
// held its content, so the step says Refolds and the start fills them.
var keptStepFoldTables = map[int][]string{
	5: {"property_marks"},
}
