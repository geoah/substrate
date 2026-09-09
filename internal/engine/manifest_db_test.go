package engine_test

// The manifest as the directory's record of what a binary must understand to
// read it: it moves with the stamps, ahead of the first append a stamp
// covers, not with the next boot, and a boot import refuses a manifest above
// either maximum before it writes a row.

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
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

// unstamped registers ada and then unclaims her changelog: the stamp rides
// an append, so this is the state a repository is in before any binary has
// appended to it. It returns the DSN, the data root, the repository directory
// and the repository's own pool.
func unstamped(t *testing.T) (dsn, root, dir string, db *sql.DB) {
	t.Helper()
	svc, dsn := newService(t)
	registerUser(t, svc, "ada.example.com")
	root = engine.DataRootOf(svc)
	id := "ada.example.com"
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
	// The unclaim runs as MAINT: the application role may raise a stamp and
	// not erase one, so an unclaimed changelog is reached from the side that
	// erases a repository.
	maint, err := engine.OpenScopedDB(dsn, id, engine.RoleMaint)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = maint.Close() }()
	if _, err := maint.ExecContext(context.Background(), `DELETE FROM changelog_dialect`); err != nil {
		t.Fatalf("unclaim the changelog: %v", err)
	}
	return dsn, root, dir, db
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

func storedVocabularyDialect(t *testing.T, db *sql.DB) int {
	t.Helper()
	var d int
	if err := db.QueryRowContext(context.Background(), `SELECT dialect FROM vocabulary_dialect`).Scan(&d); err != nil {
		t.Fatalf("read the stamped vocabulary dialect: %v", err)
	}
	return d
}

// The stamp rides the first append a binary makes, and the manifest follows
// it in the same after-commit step: a directory copied between the first
// write and the next restart says what its segments require. The fixture is
// an unclaimed changelog, opened and written to without a restart in between.
func TestManifestFollowsTheChangelogStampBeforeARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, dir, db := unstamped(t)
	svc2 := mustReopen(t, dsn, root)
	ds, err := svc2.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	// Opened, not yet written to: the changelog is still unclaimed and the
	// manifest says so, in the format this binary writes and with the
	// vocabulary dialect the open stamped.
	if got := storedChangelogDialect(t, db); got != 0 {
		t.Fatalf("the open claimed changelog dialect %d before appending anything", got)
	}
	m := readManifest(t, dir)
	if m.Format != changelogfile.ManifestFormat || m.ChangelogDialect != 0 || m.VocabularyDialect != engine.MaxSchemaDialect() {
		t.Fatalf("manifest after the open = %+v, want format %d, no changelog dialect, vocabulary dialect %d", m, changelogfile.ManifestFormat, engine.MaxSchemaDialect())
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

// The manifest is written BEFORE the stamping transaction commits or appends:
// a process that dies between the two leaves a manifest that already carries
// the new dialect over segments that do not yet need it, never the reverse.
// The fault stands in for the death: the transaction rolls back, the file and
// the table hold what they held, and the manifest carries the claim.
func TestManifestCarriesTheDialectBeforeTheFirstStampedAppend(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, dir, db := unstamped(t)
	injected := errors.New("the process died between the manifest and the append")
	var fail atomic.Bool
	svc := reopenWith(t, dsn, root, engine.WithTestCommitFault(func(stage string) error {
		if stage == engine.CommitAfterManifest && fail.Load() {
			return injected
		}
		return nil
	}))
	ds, err := svc.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	tableHead, fHead := changelogHead(t, db), fileHead(t, dir)
	if m := readManifest(t, dir); m.ChangelogDialect != 0 {
		t.Fatalf("manifest before the first write = %+v", m)
	}
	fail.Store(true)
	if _, _, err := ds.MintToken(ctx, "test", nil); !errors.Is(err, injected) {
		t.Fatalf("the stamping write with the fault after the manifest: err = %v", err)
	}
	// Manifest first: the claim is on disk while nothing in the file or the
	// table requires it yet.
	if m := readManifest(t, dir); m.ChangelogDialect != engine.MaxChangelogDialect() {
		t.Fatalf("the manifest was not written before the append: %+v", m)
	}
	if got := storedChangelogDialect(t, db); got != 0 {
		t.Fatalf("the rolled-back transaction moved the stamp to %d", got)
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
	dsn, root, dir, db := unstamped(t)
	injected := errors.New("the manifest could not be written")
	var fail atomic.Bool
	svc := reopenWith(t, dsn, root, engine.WithTestCommitFault(func(stage string) error {
		if stage == engine.CommitBeforeManifest && fail.Load() {
			return injected
		}
		return nil
	}))
	ds, err := svc.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	tableHead, fHead := changelogHead(t, db), fileHead(t, dir)
	fail.Store(true)
	if _, _, err := ds.MintToken(ctx, "test", nil); !errors.Is(err, injected) || !errors.Is(err, engine.ErrDirectoryWrite) || !errors.Is(err, substrate.ErrUnavailable) {
		t.Fatalf("the stamping write with the manifest unwritable: err = %v, want the injected error as ErrDirectoryWrite, an ErrUnavailable", err)
	}
	if m := readManifest(t, dir); m.ChangelogDialect != 0 {
		t.Fatalf("manifest after the refused write = %+v", m)
	}
	if got := storedChangelogDialect(t, db); got != 0 || changelogHead(t, db) != tableHead || fileHead(t, dir) != fHead {
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
