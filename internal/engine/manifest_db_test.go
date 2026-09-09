package engine_test

// The manifest as the directory's record of what a binary must understand to
// read it: it moves with the stamps, ahead of the first append a stamp
// covers, not with the next boot; a boot import refuses a manifest above
// either maximum, or a retired entry, before it writes a row; and the
// directories v0.46.0 (repodir_db_test.go buildLegacyFixture) and v0.47.0
// through v0.53.0 wrote still import.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// reopenWith is reopen with extra options, for a seam.
func reopenWith(t *testing.T, dsn, root string, opts ...engine.Option) substrate.Service {
	t.Helper()
	all := []engine.Option{
		engine.WithKindsDir(engine.CoreKindsDir),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(engine.TestCredentialKey),
	}
	svc, err := engine.OpenForTest(t, context.Background(), dsn, append(all, opts...)...)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// stampedBelowTheMaximum registers ada and winds her repository's changelog
// stamp back one rung, the store a binary one release older left. Every test
// that uses it watches a dialect TRANSITION — the stamp moving, and the
// manifest moving with it — which is only observable while a rung exists
// between the floor and the maximum. While the two are equal there is no such
// store: one stamped lower is refused at the open (0074), so the fixture
// skips and re-arms itself when the next rung lands.
func stampedBelowTheMaximum(t *testing.T) (dsn, root, dir string, db *sql.DB, below int) {
	t.Helper()
	if engine.MinChangelogDialect() >= engine.MaxChangelogDialect() {
		t.Skipf("no rung between the floor (%d) and the maximum (%d): a store below the maximum does not open, so a transition cannot be staged",
			engine.MinChangelogDialect(), engine.MaxChangelogDialect())
	}
	below = engine.MaxChangelogDialect() - 1
	svc, dsn := newService(t)
	registerUser(t, svc, "ada")
	root = engine.DataRootOf(svc)
	id := testdb.RepositoryID(t, dsn, "ada")
	_ = svc.Close()
	dir, err := changelogfile.RepoDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	db, err = engine.OpenScopedDB(dsn, id, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(context.Background(), `UPDATE changelog_dialect SET dialect = $1`, below); err != nil {
		t.Fatalf("wind the stamp back: %v", err)
	}
	return dsn, root, dir, db, below
}

func fileHead(t *testing.T, dir string) int64 {
	t.Helper()
	log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	return log.Head()
}

func readManifest(t *testing.T, dir string) changelogfile.Manifest {
	t.Helper()
	m, err := changelogfile.ReadManifest(dir)
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}
	return m
}

// rewriteChangelogDir re-encodes a changelog directory with edit applied to
// every entry, recomputing each line's checksum, so the result reads as a
// changelog another binary wrote.
func rewriteChangelogDir(t *testing.T, dir string, edit func(*changelogfile.Entry)) {
	t.Helper()
	log, err := changelogfile.OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	var entries []changelogfile.Entry
	if err := log.Walk(func(e changelogfile.Entry) error {
		e.Payload = append(json.RawMessage(nil), e.Payload...)
		edit(&e)
		entries = append(entries, e)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	w, err := changelogfile.OpenWriter(dir, changelogfile.WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(entries); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func storedVocabularyDialect(t *testing.T, db *sql.DB) int {
	t.Helper()
	var d int
	if err := db.QueryRowContext(context.Background(), `SELECT dialect FROM vocabulary_dialect`).Scan(&d); err != nil {
		t.Fatalf("read the stamped vocabulary dialect: %v", err)
	}
	return d
}

// The stamp rides the first append a binary makes, and the manifest follows
// it in the same after-commit step: a directory copied between an upgrade's
// first write and the next restart says what its segments require. The
// fixture is the store v0.47.0 left, stamped 2, opened and written to by this
// binary without a restart in between.
func TestManifestFollowsTheChangelogStampBeforeARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, dir, db, below := stampedBelowTheMaximum(t)
	svc2 := mustReopen(t, dsn, root)
	ds, err := svc2.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	// Opened, not yet written to: the stamp is the old binary's and the
	// manifest agrees with it, in the format this binary writes and with the
	// vocabulary dialect the open stamped.
	if got := storedChangelogDialect(t, db); got != below {
		t.Fatalf("the open moved the changelog stamp to %d before appending anything, want the older %d", got, below)
	}
	m := readManifest(t, dir)
	if m.Format != changelogfile.ManifestFormat || m.ChangelogDialect != below || m.VocabularyDialect != engine.MaxSchemaDialect() {
		t.Fatalf("manifest after the open = %+v, want format %d, changelog dialect %d, vocabulary dialect %d", m, changelogfile.ManifestFormat, below, engine.MaxSchemaDialect())
	}
	// The first write claims the dialect, and the manifest says so before
	// any restart.
	if _, _, err := ds.MintToken(ctx, "test", nil); err != nil {
		t.Fatalf("mint a token: %v", err)
	}
	if got := storedChangelogDialect(t, db); got != engine.MaxChangelogDialect() {
		t.Fatalf("stamped changelog dialect = %d, want %d", got, engine.MaxChangelogDialect())
	}
	if m = readManifest(t, dir); m.ChangelogDialect != engine.MaxChangelogDialect() {
		t.Fatalf("the manifest says changelog dialect %d after the stamping write, want %d", m.ChangelogDialect, engine.MaxChangelogDialect())
	}
	if storedVocabularyDialect(t, db) != m.VocabularyDialect {
		t.Fatalf("the manifest's vocabulary dialect %d is not the stamp's", m.VocabularyDialect)
	}
	// A restart agrees: the boot's manifest, written from the tables, is the
	// one the write left.
	_ = svc2.Close()
	mustReopen(t, dsn, root)
	if again := readManifest(t, dir); again.Format != m.Format || again.ChangelogDialect != m.ChangelogDialect ||
		again.VocabularyDialect != m.VocabularyDialect || !again.CreatedAt.Equal(m.CreatedAt) || string(again.DEK) != string(m.DEK) {
		t.Fatalf("the boot's manifest %+v is not the write's %+v", again, m)
	}
}

// A manifest naming a dialect above the binary's maximum, either one, refuses
// the boot before the import writes anything: no `repositories` row, no
// `changelog` row, no import marker. The refusal is the named one the open
// gives, so an operator reads the same error either way.
func TestImportRefusesAManifestAboveTheBinaryBeforeAnyRow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*changelogfile.Manifest)
		want   error
	}{
		{"changelog dialect", func(m *changelogfile.Manifest) { m.ChangelogDialect = engine.MaxChangelogDialect() + 1 }, engine.ErrChangelogDialectNewer},
		{"vocabulary dialect", func(m *changelogfile.Manifest) { m.VocabularyDialect = engine.MaxSchemaDialect() + 1 }, engine.ErrVocabularyDialectNewer},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc, ds, _ := newDatasetWithDSN(t)
			mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "written by a newer binary"}})
			id := repositoryIDOf(t, ds)
			root := engine.DataRootOf(svc)
			_ = svc.Close()

			root2 := copyRepositoryDir(t, root, id)
			dir, err := changelogfile.RepoDir(root2, id)
			if err != nil {
				t.Fatal(err)
			}
			m := readManifest(t, dir)
			c.mutate(&m)
			if err := changelogfile.WriteManifest(dir, m); err != nil {
				t.Fatal(err)
			}
			dsn2 := engine.MigratedDSN(t)
			_, err = reopen(t, dsn2, root2)
			if !errors.Is(err, c.want) {
				t.Fatalf("boot over the directory: err = %v, want %v", err, c.want)
			}
			db := rawDB(t, dsn2)
			for _, table := range []string{"repositories", "changelog_dialect", "vocabulary_dialect", "changelog", "import_progress", "records"} {
				if n := countRows(t, db, table); n != 0 {
					t.Fatalf("the refused import left %d %s row(s)", n, table)
				}
			}
		})
	}
}

// A `link` entry in the files refuses the import before the `repositories`
// row and the dialect rows exist, so the directory reserves neither its
// username nor its authority. The manifest's stamp cannot stand in for this:
// a directory written from the tables of a store whose stamp ran ahead of its
// entries says the current dialect and carries the retired spelling anyway,
// so the import reads the entries themselves (decision 0044).
func TestImportRefusesARetiredLinkEntryBeforeAnyRow(t *testing.T) {
	t.Parallel()
	svc, ds, _ := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "linked"}})
	head := maxSeq(t, ds)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dir, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	rewriteChangelogDir(t, changelogfile.ChangelogDir(dir), func(e *changelogfile.Entry) {
		if e.Seq == head {
			e.Op = "link"
		}
	})
	dsn2 := engine.MigratedDSN(t)
	_, err = reopen(t, dsn2, root2)
	if !errors.Is(err, engine.ErrChangelogRetiredEntry) {
		t.Fatalf("boot over a directory holding a link entry: err = %v, want ErrChangelogRetiredEntry", err)
	}
	db := rawDB(t, dsn2)
	for _, table := range []string{"repositories", "changelog_dialect", "vocabulary_dialect", "changelog", "import_progress", "records", "sealed"} {
		if n := countRows(t, db, table); n != 0 {
			t.Fatalf("the refused import left %d %s row(s)", n, table)
		}
	}
}

// A manifest stamping a changelog dialect BELOW the floor refuses the import
// before the `repositories` row and the dialect rows exist, so the directory
// reserves neither its username nor its authority and a later boot has no row
// to export an empty repository from. This is the import's half of the floor
// (0074): the adoption steps that brought such a store forward are in an
// earlier release, and a directory imported without them would fold into a
// repository missing what the step carried.
func TestImportRefusesARetiredDialectBeforeAnyRow(t *testing.T) {
	t.Parallel()
	svc, ds, _ := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "written by an older release"}})
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dir, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, dir)
	m.ChangelogDialect = engine.MinChangelogDialect() - 1
	if err := changelogfile.WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	dsn2 := engine.MigratedDSN(t)
	_, err = reopen(t, dsn2, root2)
	if !errors.Is(err, engine.ErrChangelogDialectRetired) {
		t.Fatalf("boot over a directory stamped below the floor: err = %v, want ErrChangelogDialectRetired", err)
	}
	db := rawDB(t, dsn2)
	for _, table := range []string{"repositories", "changelog_dialect", "vocabulary_dialect", "changelog", "import_progress", "records", "sealed"} {
		if n := countRows(t, db, table); n != 0 {
			t.Fatalf("the refused import left %d %s row(s)", n, table)
		}
	}
}

// The manifest is written BEFORE the stamping transaction commits or appends:
// a process that dies between the two leaves a manifest that already carries
// the new dialect over segments that do not yet need it, never the reverse.
// The fault stands in for the death: the transaction rolls back, the file and
// the table hold what they held, and the manifest says 3.
func TestManifestCarriesTheDialectBeforeTheFirstStampedAppend(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, dir, db, below := stampedBelowTheMaximum(t)
	injected := errors.New("the process died between the manifest and the append")
	var fail atomic.Bool
	svc := reopenWith(t, dsn, root, engine.WithTestCommitFault(func(stage string) error {
		if stage == engine.CommitAfterManifest && fail.Load() {
			return injected
		}
		return nil
	}))
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	tableHead, fHead := changelogHead(t, db), fileHead(t, dir)
	if m := readManifest(t, dir); m.ChangelogDialect != below {
		t.Fatalf("manifest before the first write = %+v, want changelog dialect %d", m, below)
	}
	fail.Store(true)
	if _, _, err := ds.MintToken(ctx, "test", nil); !errors.Is(err, injected) {
		t.Fatalf("the stamping write with the fault after the manifest: err = %v", err)
	}
	// Manifest first: 3 on disk while nothing in the file or the table
	// requires it yet.
	if m := readManifest(t, dir); m.ChangelogDialect != engine.MaxChangelogDialect() {
		t.Fatalf("the manifest was not written before the append: %+v", m)
	}
	if got := storedChangelogDialect(t, db); got != below {
		t.Fatalf("the rolled-back transaction moved the stamp to %d, want the older %d", got, below)
	}
	if got := changelogHead(t, db); got != tableHead {
		t.Fatalf("the rolled-back transaction reached the table: head %d, was %d", got, tableHead)
	}
	if got := fileHead(t, dir); got != fHead {
		t.Fatalf("the rolled-back transaction reached the file: head %d, was %d", got, fHead)
	}
	// The next write claims again, and this time the entries follow.
	fail.Store(false)
	if _, _, err := ds.MintToken(ctx, "test", nil); err != nil {
		t.Fatalf("the write after the fault: %v", err)
	}
	if got := storedChangelogDialect(t, db); got != engine.MaxChangelogDialect() {
		t.Fatalf("stamped changelog dialect = %d", got)
	}
	if got := fileHead(t, dir); got <= fHead || got != changelogHead(t, db) {
		t.Fatalf("file head %d after the write, was %d, table %d", got, fHead, changelogHead(t, db))
	}
	if m := readManifest(t, dir); m.ChangelogDialect != engine.MaxChangelogDialect() {
		t.Fatalf("manifest after the write = %+v", m)
	}
}

// A manifest that cannot be written refuses the stamping write before it
// commits: the tables and the file do not move, nothing latches, and the
// next write claims again once the manifest can be written.
func TestAFailedManifestWriteRefusesTheStampingWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, dir, db, below := stampedBelowTheMaximum(t)
	injected := errors.New("the manifest could not be written")
	var fail atomic.Bool
	svc := reopenWith(t, dsn, root, engine.WithTestCommitFault(func(stage string) error {
		if stage == engine.CommitBeforeManifest && fail.Load() {
			return injected
		}
		return nil
	}))
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	tableHead, fHead := changelogHead(t, db), fileHead(t, dir)
	fail.Store(true)
	if _, _, err := ds.MintToken(ctx, "test", nil); !errors.Is(err, injected) || !errors.Is(err, engine.ErrDirectoryWrite) || !errors.Is(err, substrate.ErrUnavailable) {
		t.Fatalf("the stamping write with the manifest unwritable: err = %v, want the injected error as ErrDirectoryWrite, an ErrUnavailable", err)
	}
	if m := readManifest(t, dir); m.ChangelogDialect != below {
		t.Fatalf("manifest after the refused write = %+v, want changelog dialect %d", m, below)
	}
	if got := storedChangelogDialect(t, db); got != below || changelogHead(t, db) != tableHead || fileHead(t, dir) != fHead {
		t.Fatalf("the refused write reached a store: stamp %d, table %d (was %d), file %d (was %d)",
			got, changelogHead(t, db), tableHead, fileHead(t, dir), fHead)
	}
	// Not latched: the next write is admitted and does the whole claim.
	fail.Store(false)
	if _, _, err := ds.MintToken(ctx, "test", nil); err != nil {
		t.Fatalf("the write after the manifest became writable: %v", err)
	}
	if m := readManifest(t, dir); m.ChangelogDialect != engine.MaxChangelogDialect() || storedChangelogDialect(t, db) != engine.MaxChangelogDialect() {
		t.Fatalf("manifest %+v, stamp %d after the write", m, storedChangelogDialect(t, db))
	}
}
