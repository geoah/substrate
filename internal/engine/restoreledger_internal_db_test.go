package engine

// THE LEDGER TRAVELS WITH THE ARCHIVE (decision 0321). An archive's
// manifest names the repository migrations the repository had run as of its
// head, and a restore, an import and a take record them before the
// repository's first open, so the open runs only what the repository had
// not: a restore runs nothing the repository had run. A manifest that names
// none restores as before, every migration running again, and one naming a
// migration this binary does not carry is refused before anything is
// removed or written.

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// countingBinary is this binary's repository migrations, each counting its
// runs into ran.
func countingBinary(ran *atomic.Int32) Option {
	return func(o *options) {
		ms := slices.Clone(repositoryMigrations)
		for i := range ms {
			run := ms[i].Run
			ms[i].Run = func(t *txn) (string, error) {
				ran.Add(1)
				return run(t)
			}
		}
		o.repositoryMigrations = ms
	}
}

// ledgerRow is one row of a repository's ledger as the database holds it.
type ledgerRow struct {
	Version int
	Name    string
	At      time.Time
}

func ledgerDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func ledgerRows(t *testing.T, dsn, id string) []ledgerRow {
	t.Helper()
	rows, err := ledgerDB(t, dsn).Query(`SELECT version, name, applied_at FROM repository_migrations WHERE repository = $1 ORDER BY version`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []ledgerRow
	for rows.Next() {
		var r ledgerRow
		if err := rows.Scan(&r.Version, &r.Name, &r.At); err != nil {
			t.Fatal(err)
		}
		r.At = r.At.UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// ledgerSource is a repository this binary created and opened, so it ran
// every migration this binary carries, written to and archived.
type ledgerSource struct {
	dsn, root, repo, id string
	ledger              []ledgerRow
}

func writeLedgerSource(t *testing.T, write func(t *testing.T, d *dataset)) ledgerSource {
	t.Helper()
	ctx := context.Background()
	src := ledgerSource{dsn: MigratedDSN(t), root: t.TempDir(), repo: testdb.Repository(t)}
	svc, err := OpenForTest(t, ctx, src.dsn, WithDataRoot(src.root), WithArchiveInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	info, err := svc.CreateRepository(ctx, src.repo)
	if err != nil {
		t.Fatal(err)
	}
	src.id = info.ID
	d, err := svc.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: vocabulary.KindContact, ID: fid("ada"), Properties: map[string]any{"name": "Ada"},
	}); err != nil {
		t.Fatal(err)
	}
	if write != nil {
		write(t, d.(*dataset))
	}
	if err := d.(*dataset).ArchivePass(ctx); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	src.ledger = ledgerRows(t, src.dsn, src.id)
	if len(src.ledger) != len(repositoryMigrations) {
		t.Fatalf("the source's ledger holds %d rows, want every migration this binary carries: the test proves nothing", len(src.ledger))
	}
	return src
}

// archiveCopy is a fresh data root whose archive holds a copy of src's.
func archiveCopy(t *testing.T, src ledgerSource) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, "archive"), os.DirFS(filepath.Join(src.root, "archive"))); err != nil {
		t.Fatal(err)
	}
	return root
}

// manifestDir is where src's archive keeps its repository's manifests.
func manifestDir(root, id string) string {
	return filepath.Join(root, "archive", changelogfile.RepositoriesDir, id)
}

// publishManifest publishes, in the archive under root, the next epoch of
// the newest manifest of id's archive, as edit leaves it: what another
// binary's pass would have published.
func publishManifest(t *testing.T, root, id string, edit func(m *changelogfile.Manifest)) {
	t.Helper()
	m, _, err := changelogfile.ReadArchiveManifestDir(manifestDir(root, id))
	if err != nil {
		t.Fatal(err)
	}
	m.Epoch++
	if m.Pass, err = changelogfile.NewPassID(); err != nil {
		t.Fatal(err)
	}
	edit(&m)
	data, err := changelogfile.EncodeArchiveManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "archive", filepath.FromSlash(changelogfile.ArchiveManifestsPrefix(id, m.Archive)))
	if err := os.WriteFile(filepath.Join(dir, changelogfile.ArchiveManifestName(m.Epoch, data)), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// laterMigration is a repository migration a later binary carries.
var laterMigration = changelogfile.RepositoryMigration{Version: 99, Name: "a_later_binarys", AppliedAt: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}

func sameLedger(a, b []ledgerRow) bool {
	return slices.EqualFunc(a, b, func(x, y ledgerRow) bool { return x.Version == y.Version && x.Name == y.Name && x.At.Equal(y.At) })
}

// A repository whose source ran every migration is restored, imported and
// taken, and no repository migration runs at its first open: the ledger
// holds every row the source's did, with the time each first ran.
func TestARestoreRunsNoRepositoryMigrationItsArchiveHadRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := writeLedgerSource(t, nil)
	check := func(t *testing.T, ran *atomic.Int32, dsn string) {
		t.Helper()
		if n := ran.Load(); n != 0 {
			t.Errorf("%d repository migration(s) ran at the first open of a repository whose archive had run them all", n)
		}
		if got := ledgerRows(t, dsn, src.id); !sameLedger(got, src.ledger) {
			t.Errorf("the ledger after it is\n%v\nwant the source's\n%v", got, src.ledger)
		}
	}

	t.Run("restored", func(t *testing.T) {
		t.Parallel()
		var ran atomic.Int32
		dsn := MigratedDSN(t)
		svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(archiveCopy(t, src)), WithArchiveInterval(time.Hour), countingBinary(&ran))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		if _, err := svc.Dataset(ctx, src.repo); err != nil {
			t.Fatalf("open the restored repository: %v", err)
		}
		check(t, &ran, dsn)
	})

	t.Run("imported", func(t *testing.T) {
		t.Parallel()
		var ran atomic.Int32
		dsn, root := MigratedDSN(t), t.TempDir()
		svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithArchiveInterval(time.Hour), countingBinary(&ran))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		// The archive lands in the store after the boot, so the boot
		// restores nothing and the import is the explicit one.
		if err := os.CopyFS(filepath.Join(root, "archive"), os.DirFS(filepath.Join(src.root, "archive"))); err != nil {
			t.Fatal(err)
		}
		s := svc.(*service)
		store, err := s.archiveStore()
		if err != nil {
			t.Fatal(err)
		}
		listed, err := listArchives(ctx, store, src.id)
		if err != nil || len(listed.ids()) != 1 {
			t.Fatalf("the archives of the source: %v (%v)", listed.ids(), err)
		}
		if _, err := s.ImportArchive(ctx, src.id, listed.ids()[0], ""); err != nil {
			t.Fatalf("import: %v", err)
		}
		if _, err := svc.Dataset(ctx, src.repo); err != nil {
			t.Fatalf("open the imported repository: %v", err)
		}
		check(t, &ran, dsn)
	})

	t.Run("taken", func(t *testing.T) {
		t.Parallel()
		taken := writeLedgerSource(t, nil)
		op, err := OpenForTest(t, ctx, taken.dsn, WithDataRoot(taken.root), WithArchiveInterval(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := op.(*service).ResolveArchive(ctx, taken.repo, false); err != nil {
			t.Fatalf("take the archive: %v", err)
		}
		if err := op.Close(); err != nil {
			t.Fatal(err)
		}
		var ran atomic.Int32
		svc, err := OpenForTest(t, ctx, taken.dsn, WithDataRoot(taken.root), WithArchiveInterval(time.Hour), countingBinary(&ran))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		if _, err := svc.Dataset(ctx, taken.repo); err != nil {
			t.Fatalf("open the taken repository: %v", err)
		}
		var takes int
		if err := ledgerDB(t, taken.dsn).QueryRow(`SELECT count(*) FROM archive_takes WHERE repository_id = $1`, taken.id).Scan(&takes); err != nil || takes != 0 {
			t.Fatalf("the take is still recorded (%d, %v): it was not carried out", takes, err)
		}
		if n := ran.Load(); n != 0 {
			t.Errorf("%d repository migration(s) ran at the first open of a taken repository whose archive had run them all", n)
		}
		if got := ledgerRows(t, taken.dsn, taken.id); !sameLedger(got, taken.ledger) {
			t.Errorf("the ledger after the take is\n%v\nwant the source's\n%v", got, taken.ledger)
		}
	})
}

// The two cases of #1362 no longer change a restored repository. Migration 5
// removes the relay rows the Gmail sync wrote before it ran; a repository
// still on the older Google bundle writes them after, and its restore keeps
// them. Migration 2 leaves a bare selector word it could not resolve when it
// ran; one that became resolvable since is not rewritten by the restore.
func TestARestoreLeavesWhatTheRepositoryWroteAfterAMigrationRan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const (
		relay   = "reply+AAEXAMPLELEDGER0@reply.github.com"
		pkg     = "ledger.example.com/shapes"
		alpha   = pkg + "/alpha"
		policy  = "gate-quill"
		bareKey = "quill"
	)
	src := writeLedgerSource(t, func(t *testing.T, d *dataset) {
		installRelayStandIn(t, d)
		relaySync(t, d,
			effect{Action: effectPut, Type: relayAccount, ID: fid("acct"), Properties: map[string]any{"email": "owner@example.com"}},
			relayHub(relay),
			relayMessage("m1", map[string][]any{"replyTo": {relayEntry(relay, "owner/repo")}}),
		)
		if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
			vocabulary.PackageManifest(pkg, 1),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "alpha"}, map[string]any{
				"properties": map[string]any{"score": map[string]any{"type": "int"}},
			}),
		}); err != nil {
			t.Fatalf("apply: %v", err)
		}
		rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{
			Kind: vocabulary.KindRecordPatchPolicy, ID: policy,
			Properties: map[string]any{"selector": map[string]any{"kinds": []any{alpha}, "ops": []any{"put"}}, "action": "gate"},
		})
		if err != nil {
			t.Fatalf("put the policy: %v", err)
		}
		// The word as a binary before decision 0101 stored it, when no kind
		// carried it: migration 2 left it.
		props := rec.Properties
		props["selector"].(map[string]any)["kinds"] = []any{bareKey}
		if err := d.PlantDeclarationRow(ctx, vocabulary.KindRecordPatchPolicy, policy, props); err != nil {
			t.Fatalf("plant the bare word: %v", err)
		}
		// Since then a kind carries it.
		if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
			vocabulary.PackageManifest(pkg, 2),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "alpha"}, map[string]any{
				"properties": map[string]any{"score": map[string]any{"type": "int"}},
			}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": bareKey}, map[string]any{
				"properties": map[string]any{"score": map[string]any{"type": "int"}},
			}),
		}); err != nil {
			t.Fatalf("apply the kind that carries the word: %v", err)
		}
	})
	d, err := restoredFrom(t, archiveCopy(t, src), WithKindsFS(SeedKinds))
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if row := relayRow(t, d, relayHubKind, fid(relay)); row == nil || row.Deleted {
		t.Errorf("the relay row the repository wrote after migration 5 ran is gone after the restore: %+v", row)
	}
	rec, err := d.Get(ctx, vocabulary.KindRecordPatchPolicy, policy)
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.Properties["selector"].(map[string]any)["kinds"]; !reflect.DeepEqual(got, []any{bareKey}) {
		t.Errorf("the policy's selector is %v after the restore, want the word it held, [%s]", got, bareKey)
	}
}

// An archive whose manifest names no ledger, as every archive written before
// it did, restores as before: every migration runs at the first open, and
// the ledger is this database's.
func TestAnArchiveThatNamesNoLedgerRunsEveryMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := writeLedgerSource(t, nil)
	root := archiveCopy(t, src)
	publishManifest(t, root, src.id, func(m *changelogfile.Manifest) { m.RepositoryMigrations = nil })
	var ran atomic.Int32
	dsn := MigratedDSN(t)
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithArchiveInterval(time.Hour), countingBinary(&ran))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.Dataset(ctx, src.repo); err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	if n := int(ran.Load()); n != len(repositoryMigrations) {
		t.Errorf("%d repository migration(s) ran, want every one this binary carries (%d)", n, len(repositoryMigrations))
	}
	if got := ledgerRows(t, dsn, src.id); len(got) != len(repositoryMigrations) || sameLedger(got, src.ledger) {
		t.Errorf("the ledger after the restore is %v, want this database's own rows", got)
	}
}

// An archive naming a repository migration this binary does not carry is a
// later binary's: its restore and its import are refused, naming it, before
// any row is written, and a take of it is refused while the repository it
// would replace is whole and served, when it is asked for and at the boot
// that would carry out a take another binary recorded.
func TestAnArchiveNamingAMigrationThisBinaryLacksIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	later := func(m *changelogfile.Manifest) {
		m.RepositoryMigrations = append(m.RepositoryMigrations, laterMigration)
	}
	refusal := func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, ErrRepositoryMigrationsNewer) || !strings.Contains(err.Error(), "99 (a_later_binarys)") {
			t.Fatalf("got %v, want ErrRepositoryMigrationsNewer naming 99 (a_later_binarys)", err)
		}
	}
	noRow := func(t *testing.T, dsn, id string) {
		t.Helper()
		var n int
		if err := ledgerDB(t, dsn).QueryRow(`SELECT (SELECT count(*) FROM repositories WHERE id = $1) + (SELECT count(*) FROM changelog WHERE repository = $1)`, id).Scan(&n); err != nil || n != 0 {
			t.Fatalf("the refused archive left %d rows (%v)", n, err)
		}
	}

	t.Run("restored and imported", func(t *testing.T) {
		t.Parallel()
		src := writeLedgerSource(t, nil)
		dsn, root := MigratedDSN(t), t.TempDir()
		svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithArchiveInterval(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		if err := os.CopyFS(filepath.Join(root, "archive"), os.DirFS(filepath.Join(src.root, "archive"))); err != nil {
			t.Fatal(err)
		}
		publishManifest(t, root, src.id, later)
		s := svc.(*service)
		store, err := s.archiveStore()
		if err != nil {
			t.Fatal(err)
		}
		listed, err := listArchives(ctx, store, src.id)
		if err != nil || len(listed.ids()) != 1 {
			t.Fatalf("the archives of the source: %v (%v)", listed.ids(), err)
		}
		_, err = s.ImportArchive(ctx, src.id, listed.ids()[0], "")
		refusal(t, err)
		noRow(t, dsn, src.id)
		_ = svc.Close()

		// The boot's restore of it is refused too, before any row: the
		// repository is set aside, and a start that set aside every
		// repository it found refuses with that one's failure.
		boot, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithArchiveInterval(time.Hour))
		if err == nil {
			_ = boot.Close()
		}
		refusal(t, err)
		noRow(t, dsn, src.id)
	})

	t.Run("taken", func(t *testing.T) {
		t.Parallel()
		src := writeLedgerSource(t, nil)
		publishManifest(t, src.root, src.id, later)
		db := ledgerDB(t, src.dsn)
		records := func() int {
			t.Helper()
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM records WHERE repository = $1`, src.id).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		served := func(t *testing.T, svc substrate.Service) {
			t.Helper()
			d, err := svc.Dataset(ctx, src.repo)
			if err != nil {
				t.Fatalf("the repository is not served: %v", err)
			}
			if _, err := d.Get(ctx, vocabulary.KindContact, fid("ada")); err != nil {
				t.Fatalf("the repository's contact: %v", err)
			}
		}
		before := records()

		// Asked for: refused, nothing recorded or staged.
		op, err := OpenForTest(t, ctx, src.dsn, WithDataRoot(src.root), WithArchiveInterval(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		_, err = op.(*service).ResolveArchive(ctx, src.repo, false)
		refusal(t, err)
		var takes int
		if err := db.QueryRow(`SELECT count(*) FROM archive_takes WHERE repository_id = $1`, src.id).Scan(&takes); err != nil || takes != 0 {
			t.Fatalf("the refused take recorded %d choice(s) (%v)", takes, err)
		}
		if entries, err := os.ReadDir(filepath.Join(src.root, "archive-take", src.id)); err == nil && len(entries) != 0 {
			t.Fatalf("the refused take left %d staged copies", len(entries))
		}
		served(t, op)
		_ = op.Close()

		// Recorded by a binary that admitted it: the boot admits it again
		// and leaves the repository whole and served, its choice recorded.
		m, name, err := changelogfile.ReadArchiveManifestDir(manifestDir(src.root, src.id))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO archive_takes (repository_id, stage, archive, manifest) VALUES ($1, $2, $3, $4)`,
			src.id, "a18e7b6f2c41d9a003b2c7e91", m.Archive, name); err != nil {
			t.Fatal(err)
		}
		svc, err := OpenForTest(t, ctx, src.dsn, WithDataRoot(src.root), WithArchiveInterval(time.Hour))
		if err != nil {
			t.Fatalf("the boot beside a take it cannot carry out: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		if after := records(); after != before {
			t.Fatalf("the repository holds %d records after the boot, want the %d it held", after, before)
		}
		if err := db.QueryRow(`SELECT count(*) FROM archive_takes WHERE repository_id = $1`, src.id).Scan(&takes); err != nil || takes != 1 {
			t.Fatalf("the take: %d recorded (%v), want it kept", takes, err)
		}
		served(t, svc)
	})
}

// An export is an archive, and its manifest names the ledger as the
// archive's does.
func TestAnExportNamesTheRepositoryMigrations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(t.TempDir()), WithArchiveInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	repo := testdb.Repository(t)
	info, err := svc.CreateRepository(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	d, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := d.(*dataset).Export(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var buf bytes.Buffer
	if _, err := ex.WriteTo(&buf); err != nil {
		t.Fatalf("stream the export: %v", err)
	}
	var m changelogfile.Manifest
	found := false
	tr := tar.NewReader(&buf)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != tar.TypeReg || !strings.Contains(h.Name, "/"+changelogfile.ManifestsSubdir+"/") {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if m, err = changelogfile.DecodeArchiveManifest(data, info.ID); err != nil {
			t.Fatalf("the export's manifest: %v", err)
		}
		found = true
	}
	if !found {
		t.Fatal("the export holds no manifest")
	}
	want := ledgerRows(t, dsn, info.ID)
	got := make([]ledgerRow, len(m.RepositoryMigrations))
	for i, r := range m.RepositoryMigrations {
		got[i] = ledgerRow{Version: r.Version, Name: r.Name, At: r.AppliedAt}
	}
	if len(want) != len(repositoryMigrations) || !sameLedger(got, want) {
		t.Fatalf("the export's manifest names\n%v\nthe ledger is\n%v", got, want)
	}
}

// A manifest names the ledger only from changelog dialect 8, the first whose
// archive could carry it: a history a shorter chain writes below it is
// archived naming none, as an archive of that age is, and a manifest that
// names a ledger below it is no manifest a binary wrote, refused as
// malformed before any row.
func TestALedgerBelowTheDialectThatCarriesItIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := writeConvertSource(t)
	m, _, err := changelogfile.ReadArchiveManifestDir(filepath.Join(src.archive, changelogfile.RepositoriesDir, src.id))
	if err != nil {
		t.Fatal(err)
	}
	if m.ChangelogDialect >= ledgerDialect || len(m.RepositoryMigrations) != 0 {
		t.Fatalf("the manifest of a history in dialect %d names %d repository migrations, want none below dialect %d",
			m.ChangelogDialect, len(m.RepositoryMigrations), ledgerDialect)
	}
	dsn, root := MigratedDSN(t), t.TempDir()
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithArchiveInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if err := os.CopyFS(filepath.Join(root, "archive"), os.DirFS(src.archive)); err != nil {
		t.Fatal(err)
	}
	publishManifest(t, root, src.id, func(m *changelogfile.Manifest) {
		for _, rm := range repositoryMigrations {
			m.RepositoryMigrations = append(m.RepositoryMigrations,
				changelogfile.RepositoryMigration{Version: rm.Version, Name: rm.Name, AppliedAt: laterMigration.AppliedAt})
		}
	})
	s := svc.(*service)
	store, err := s.archiveStore()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := listArchives(ctx, store, src.id)
	if err != nil || len(listed.ids()) != 1 {
		t.Fatalf("the archives of the source: %v (%v)", listed.ids(), err)
	}
	if _, err := s.ImportArchive(ctx, src.id, listed.ids()[0], ""); !errors.Is(err, changelogfile.ErrManifestFormat) {
		t.Fatalf("import a manifest naming a ledger in dialect %d: %v, want ErrManifestFormat", m.ChangelogDialect, err)
	}
	var n int
	if err := ledgerDB(t, dsn).QueryRow(`SELECT count(*) FROM repositories WHERE id = $1`, src.id).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the refused import left %d rows (%v)", n, err)
	}
}
