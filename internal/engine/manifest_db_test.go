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
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// formatOneVocabularyDialect is the vocabulary dialect every format-1
// manifest implies: v0.46.0 and v0.47.0 stored declarations in dialect 3.
const formatOneVocabularyDialect = 3

// reopenWith is reopen with extra options, for a seam.
func reopenWith(t *testing.T, dsn, root string, opts ...engine.Option) substrate.Service {
	t.Helper()
	all := []engine.Option{
		engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(engine.TestCredentialKey),
	}
	svc, err := engine.Open(context.Background(), dsn, append(all, opts...)...)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// stampedAtTwo registers ada and leaves her repository as a v0.47.0 through
// v0.51.0 binary would: stamped changelog dialect 2. It returns the DSN, the
// data root, the repository directory and the repository's own pool.
func stampedAtTwo(t *testing.T) (dsn, root, dir string, db *sql.DB) {
	t.Helper()
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
	if _, err := db.ExecContext(context.Background(), `UPDATE changelog_dialect SET dialect = 2`); err != nil {
		t.Fatalf("wind the stamp back: %v", err)
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

// unframe strips the transaction frame: the line v0.46.0 through v0.51.0 wrote.
func unframe(e *changelogfile.Entry) { e.Txn = 0 }

// writeFormatOneManifest writes the manifest v0.47.0 through v0.51.0 wrote
// for m: format 1, changelog dialect 2 (those binaries' maximum), no
// vocabulary dialect, no key id and no marker.
func writeFormatOneManifest(t *testing.T, dir string, m changelogfile.Manifest) {
	t.Helper()
	raw, err := json.MarshalIndent(map[string]any{
		"format": 1, "username": m.Username, "authority": m.Authority,
		"createdAt": m.CreatedAt.Format(changelogfile.TSFormat), "changelogDialect": 2,
		"dek": base64.StdEncoding.EncodeToString(m.DEK),
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, changelogfile.ManifestName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readManifest(t, dir); got.Format != 1 || got.ChangelogDialect != 2 || got.VocabularyDialect != 0 {
		t.Fatalf("the fixture's manifest is not the format-1 shape: %+v", got)
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
	dsn, root, dir, db := stampedAtTwo(t)
	svc2 := mustReopen(t, dsn, root)
	ds, err := svc2.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	// Opened, not yet written to: the stamp is the old binary's and the
	// manifest agrees with it, in the format this binary writes and with the
	// vocabulary dialect the open stamped.
	if got := storedChangelogDialect(t, db); got != 2 {
		t.Fatalf("the open moved the changelog stamp to %d before appending anything", got)
	}
	m := readManifest(t, dir)
	if m.Format != changelogfile.ManifestFormat || m.ChangelogDialect != 2 || m.VocabularyDialect != engine.MaxSchemaDialect() {
		t.Fatalf("manifest after the open = %+v, want format %d, changelog dialect 2, vocabulary dialect %d", m, changelogfile.ManifestFormat, engine.MaxSchemaDialect())
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

// A directory v0.47.0 wrote: a format-1 manifest stamped changelog dialect 2
// with no vocabulary dialect, and lines without `txn`. It imports; the import
// stamps the store with the dialects the manifest recorded and implied, not
// this binary's maxima; the manifest comes back in format 2; and the first
// write moves the changelog stamp and the manifest together.
func TestBootImportsAFormatOneDirectory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "from v0.47.0"}})
	ref := putProvider(t, ds, dsn, "openai", "sk-format-one")
	before := foldOf(t, ds)
	head := maxSeq(t, ds)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dir, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	rewriteChangelogDir(t, changelogfile.ChangelogDir(dir), unframe)
	writeFormatOneManifest(t, dir, readManifest(t, dir))

	dsn2 := engine.MigratedDSN(t)
	svc2 := mustReopen(t, dsn2, root2)
	db, err := engine.OpenScopedDB(dsn2, id, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	// Stamped from the manifest at the import, before any open: the writer's
	// requirements, not the reader's.
	if got := storedChangelogDialect(t, db); got != 2 {
		t.Fatalf("changelog dialect after the import = %d, want the manifest's 2", got)
	}
	if got := storedVocabularyDialect(t, db); got != formatOneVocabularyDialect {
		t.Fatalf("vocabulary dialect after the import = %d, want the %d a format-1 manifest implies", got, formatOneVocabularyDialect)
	}
	m := readManifest(t, dir)
	if m.Format != changelogfile.ManifestFormat || m.ChangelogDialect != 2 || m.VocabularyDialect != formatOneVocabularyDialect {
		t.Fatalf("manifest after the import = %+v", m)
	}

	ds2, err := svc2.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("open the imported repository: %v", err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the imported fold is not the original\n%s", firstDifference(before, after))
	}
	if got := openSecret(t, dsn2, ref); got != "sk-format-one" {
		t.Fatalf("secret = %q", got)
	}
	report := mustVerify(t, svc2, testdb.Username(t))
	if !report.OK || report.Head != head || report.FileHead != head {
		t.Fatalf("the imported directory does not verify: %+v", report)
	}
	mustPut(t, ds2, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "written by this binary"}})
	if got := storedChangelogDialect(t, db); got != engine.MaxChangelogDialect() {
		t.Fatalf("changelog dialect after the first write = %d, want %d", got, engine.MaxChangelogDialect())
	}
	if m = readManifest(t, dir); m.ChangelogDialect != engine.MaxChangelogDialect() {
		t.Fatalf("the manifest says changelog dialect %d after the first write, want %d", m.ChangelogDialect, engine.MaxChangelogDialect())
	}
}

// A `link` entry in the files, which dialect 1 wrote and nothing folds any
// more, refuses the import before the `repositories` row and the dialect rows
// exist, so the directory reserves neither its username nor its authority
// and a later boot has no row to export an empty repository from. The fold
// would refuse it too, with the rows inserted and the import marker set.
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
	if !errors.Is(err, engine.ErrChangelogPredatesReferences) {
		t.Fatalf("boot over a directory holding a link entry: err = %v, want ErrChangelogPredatesReferences", err)
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
	dsn, root, dir, db := stampedAtTwo(t)
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
	if m := readManifest(t, dir); m.ChangelogDialect != 2 {
		t.Fatalf("manifest before the first write = %+v", m)
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
	if got := storedChangelogDialect(t, db); got != 2 {
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
	dsn, root, dir, db := stampedAtTwo(t)
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
	if m := readManifest(t, dir); m.ChangelogDialect != 2 {
		t.Fatalf("manifest after the refused write = %+v", m)
	}
	if got := storedChangelogDialect(t, db); got != 2 || changelogHead(t, db) != tableHead || fileHead(t, dir) != fHead {
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
