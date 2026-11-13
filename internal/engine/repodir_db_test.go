package engine_test

// THE BOOT CHECK (repodir.go): a repository restored from its archive into a
// database that does not hold it, an archive ahead of its table, an import
// cut short and finished by the next boot, a directory a release before the
// archive wrote, and what refuses a boot and what does not.

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const taskKind = "samples.substrate.reamde.dev/tasks/task"

// repoDirOf is the dataset's directory under the service's data root.
func repoDirOf(t *testing.T, svc substrate.Service, ds substrate.Dataset) string {
	t.Helper()
	dir, err := changelogfile.RepoDir(engine.DataRootOf(svc), repositoryIDOf(t, ds))
	if err != nil {
		t.Fatalf("repository directory: %v", err)
	}
	return dir
}

// reopen opens a second service over the same database and data root, the
// shape of a restart.
func reopen(t *testing.T, dsn, root string) (substrate.Service, error) {
	t.Helper()
	svc, err := engine.OpenForTest(t, context.Background(), dsn,
		engine.WithKindsFS(engine.SeedKinds),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err == nil {
		t.Cleanup(func() { _ = svc.Close() })
	}
	return svc, err
}

// reopenBeside opens a second WRITER over a live server's database and data
// root — the arrangement the writer lease refuses at open (decision 0083), so
// it takes none. It is for the tests of the OTHER exclusion, the per-directory
// flock: two writers on one data root, which is what a verb like rebuild or
// snapshot meets, and which must keep refusing on its own.
func reopenBeside(t *testing.T, dsn, root string) (substrate.Service, error) {
	t.Helper()
	svc, err := engine.OpenForTest(t, context.Background(), dsn,
		engine.WithKindsFS(engine.SeedKinds),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithTestSkipWriterLease())
	if err == nil {
		t.Cleanup(func() { _ = svc.Close() })
	}
	return svc, err
}

func mustReopen(t *testing.T, dsn, root string) substrate.Service {
	t.Helper()
	svc, err := reopen(t, dsn, root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	return svc
}

// tableChangelog reads every (seq, hash) of the one repository in the schema
// through the tamperer's seat.
func tableChangelog(t *testing.T, dsn string) map[int64][]byte {
	t.Helper()
	rows, err := rawDB(t, dsn).Query(`SELECT seq, hash FROM changelog ORDER BY seq`)
	if err != nil {
		t.Fatalf("read the changelog table: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64][]byte{}
	for rows.Next() {
		var seq int64
		var hash []byte
		if err := rows.Scan(&seq, &hash); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[seq] = hash
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// copyDir copies a directory tree, the way a backup cron would.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("copy %s to %s: %v", src, dst, err)
	}
}

// openSecret reads one secret-typed value's material the way the engine does:
// the sealed row by ref, the repository's DEK unwrapped from the control plane
// under the host key, the payload opened under the DEK with its row binding.
func openSecret(t *testing.T, dsn, ref string) string {
	t.Helper()
	return openSecretUnder(t, dsn, ref, engine.TestCredentialKeyBytes)
}

// openSecretUnder is openSecret with the host key named: the restoring host's
// key after a rewrap.
func openSecretUnder(t *testing.T, dsn, ref string, hostKey []byte) string {
	t.Helper()
	db := rawDB(t, dsn)
	var payload []byte
	var kind, rid, repoID string
	if err := db.QueryRow(
		`SELECT payload, record_kind, record_id, repository FROM sealed WHERE ref = $1`, ref).
		Scan(&payload, &kind, &rid, &repoID); err != nil {
		t.Fatalf("read sealed payload for %s: %v", ref, err)
	}
	var wrapped []byte
	if err := db.QueryRow(`SELECT dek FROM repositories WHERE id = $1`, repoID).Scan(&wrapped); err != nil {
		t.Fatalf("read wrapped dek: %v", err)
	}
	dek, err := engine.OpenPayloadWithKey(hostKey, wrapped, engine.DEKAAD(repoID))
	if err != nil {
		t.Fatalf("unwrap the DEK: %v", err)
	}
	plain, err := engine.OpenPayloadWithKey(dek, payload, engine.SealedAAD(ref, kind, rid))
	if err != nil {
		t.Fatalf("open payload under the DEK: %v", err)
	}
	return string(plain)
}

// secretRefOf reads the sealed-store ref a record's secret property holds,
// off the stored row: the read surface redacts a secret-typed property.
func secretRefOf(t *testing.T, dsn, kind, id, prop string) string {
	t.Helper()
	var ref string
	if err := rawDB(t, dsn).QueryRow(`SELECT props->>$3 FROM records WHERE kind = $1 AND id = $2`, kind, id, prop).
		Scan(&ref); err != nil {
		t.Fatalf("read %s.%s: %v", kind, prop, err)
	}
	if !strings.HasPrefix(ref, "secret:") {
		t.Fatalf("%s.%s holds %q, not a sealed-store ref", kind, prop, ref)
	}
	return ref
}

// putProvider writes an llm/provider row whose apiKey is a secret-typed
// property, and returns the ref the property stores.
func putProvider(t *testing.T, ds substrate.Dataset, dsn, id, key string) string {
	t.Helper()
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: typeProvider, ID: id,
		Properties: map[string]any{
			"label": id, "wire": "openai", "baseURL": "https://llm.example.com/v1", "apiKey": key,
		},
	})
	return secretRefOf(t, dsn, typeProvider, id, "apiKey")
}

func putBlob(t *testing.T, ds substrate.Dataset, data []byte) string {
	t.Helper()
	info, err := ds.PutBlob(context.Background(), owner,
		substrate.BlobUpload{Name: "note.txt", MediaType: "text/plain"}, data, "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	return info.Digest
}

func getBlob(t *testing.T, ds substrate.Dataset, digest string) []byte {
	t.Helper()
	_, data, err := ds.GetBlob(context.Background(), digest)
	if err != nil {
		t.Fatalf("get blob %s: %v", digest, err)
	}
	return data
}

// sealedRowCount counts the sealed rows under one ref.
func sealedRowCount(t *testing.T, dsn, ref string) int {
	t.Helper()
	var n int
	if err := rawDB(t, dsn).QueryRow(`SELECT count(*) FROM sealed WHERE ref = $1`, ref).Scan(&n); err != nil {
		t.Fatalf("count sealed rows under %s: %v", ref, err)
	}
	return n
}

// longAuthority is a valid authority of exactly n bytes, in 63-byte labels.
func longAuthority(n int) string {
	var b strings.Builder
	for b.Len() < n {
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(strings.Repeat("a", min(63, n-b.Len())))
	}
	return b.String()
}

// copyRepositoryDir copies one repository's archive into the archive of a
// fresh data root and returns that root: the backup a restore reads. The
// archive is the one the source's service left when it closed, which ran one
// last pass (archive.go stopArchiver).
func copyRepositoryDir(t *testing.T, srcRoot, id string) string {
	t.Helper()
	dstRoot := t.TempDir()
	rel := filepath.Join("archive", changelogfile.RepositoriesDir, id)
	copyDir(t, filepath.Join(srcRoot, rel), filepath.Join(dstRoot, rel))
	return dstRoot
}

// archivedRepoDir is repository id's directory in the archive under root,
// where copyRepositoryDir copies it, as an error-returning RepoDir.
func archivedRepoDir(root, id string) (string, error) {
	return filepath.Join(root, "archive", changelogfile.RepositoriesDir, id), nil
}

// reopenBesideNewerDirectory boots the database at dsn, an older dump, beside
// the 0.x directory of the newer history whose archive is under newerRoot:
// the one import past a table that holds history (rule 6), the directory's
// lines past the table's head taken in. The data root is a new one, holding
// that directory alone.
func reopenBesideNewerDirectory(t *testing.T, dsn, newerRoot, id string) substrate.Service {
	t.Helper()
	root := t.TempDir()
	legacyDirectory(t, filepath.Join(newerRoot, "archive"), root, id)
	return mustReopen(t, dsn, root)
}

// legacyDirectory writes the directory a release before the archive kept
// beside the blobs (a 0.x directory) from a repository's archive: the
// manifest in format 2, the history as segment files, the sealed files, and
// the blobs where the server keeps them. It is how a test meets what an
// upgrade from a 0.x release finds.
func legacyDirectory(t *testing.T, archiveRoot, root, id string) string {
	t.Helper()
	ctx := context.Background()
	store, err := archive.NewDir(archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := changelogfile.ReadArchiveManifestDir(filepath.Join(archiveRoot, changelogfile.RepositoriesDir, id))
	if err != nil {
		t.Fatalf("read the archive's manifest: %v", err)
	}
	prefix := changelogfile.ArchivePrefix(id, m.Archive)
	chain, err := m.Chain()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := changelogfile.EnsureRepoDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	w, err := changelogfile.OpenWriter(changelogfile.ChangelogDir(dir), changelogfile.WriterOptions{SegmentBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range chain {
		body, err := archive.ReadAll(ctx, store, prefix+p.Key)
		if err != nil {
			t.Fatal(err)
		}
		var lines []changelogfile.Line
		if err := changelogfile.ReadPiece(bytes.NewReader(body), p.First, p.Last, func(e changelogfile.Entry, line []byte, _ [32]byte) error {
			lines = append(lines, changelogfile.Line{Seq: e.Seq, Txn: e.Txn, Bytes: append([]byte(nil), line...)})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := w.AppendLines(lines); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := store.List(ctx, prefix+changelogfile.BlobsSubdir+"/")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range listed {
		digest, _, ok := changelogfile.ParseBlobObjectName(strings.TrimPrefix(o.Key, prefix))
		if !ok {
			continue
		}
		body, err := archive.ReadAll(ctx, store, o.Key)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, changelogfile.BlobsSubdir), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, changelogfile.BlobsSubdir, digest), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// The sealed values the manifest selects, one file each, as a 0.x
	// directory held them.
	for ref, name := range m.Sealed {
		sum, _, ok := changelogfile.ParseSealedObjectName(ref, name)
		if !ok {
			t.Fatalf("the manifest selects %q for %s", name, ref)
		}
		body, err := archive.ReadAll(ctx, store, prefix+name)
		if err != nil {
			t.Fatal(err)
		}
		rec, err := changelogfile.DecodeSealedVersion(ref, sum, body)
		if err != nil {
			t.Fatal(err)
		}
		if err := changelogfile.WriteSealed(dir, rec); err != nil {
			t.Fatal(err)
		}
	}
	m.Format, m.Head, m.HeadSum, m.Sealed, m.MissingBlobs = changelogfile.ManifestFormat, 0, "", nil, nil
	m.Archive, m.Pass, m.Pieces, m.Spool, m.PendingBlobs, m.Epoch = "", "", nil, nil, nil, 0
	if err := changelogfile.WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A repository the archive holds and the database does not is restored at
// the boot: the same records, the same history line for line, its secrets
// and blobs, under a fresh history generation, and it verifies.
func TestBootRestoresARepositoryFromItsArchive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	writeSomeHistory(t, ds)
	ref := putProvider(t, ds, dsn, "openai", "sk-restored")
	digest := putBlob(t, ds, []byte("restored bytes"))
	before := foldOf(t, ds)
	rows := tableChangelog(t, dsn)
	generation := ds.Repository()
	id := repositoryIDOf(t, ds)
	head, err := ds.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	dsn2 := engine.MigratedDSN(t)
	svc2 := mustReopen(t, dsn2, copyRepositoryDir(t, root, id))
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	if got := ds2.Repository(); got.ID != id || got.Authority != generation.Authority {
		t.Fatalf("restored as %+v, want %s %s", got, id, generation.Authority)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the restored fold is not the original\n%s", firstDifference(before, after))
	}
	after := tableChangelog(t, dsn2)
	if len(after) != len(rows) {
		t.Fatalf("%d rows restored, the history has %d", len(after), len(rows))
	}
	for seq, hash := range rows {
		if !bytes.Equal(after[seq], hash) {
			t.Fatalf("seq %d is not the original's", seq)
		}
	}
	if got := openSecret(t, dsn2, ref); got != "sk-restored" {
		t.Fatalf("the restored secret = %q", got)
	}
	if got := getBlob(t, ds2, digest); string(got) != "restored bytes" {
		t.Fatalf("the restored blob = %q", got)
	}
	head2, err := ds2.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if head2.Seq != head.Seq || head2.Generation == head.Generation {
		t.Fatalf("head after the restore = %+v, want seq %d under a new generation than %q", head2, head.Seq, head.Generation)
	}
	// The restore started a new archive, which its first pass publishes.
	if report := mustVerify(t, svc2, testdb.Repository(t)); !report.OK || report.ArchiveHead != 0 || report.ArchiveUnverified == "" {
		t.Fatalf("the restored repository before its first pass: %+v", report)
	}
	if err := engine.ArchivePass(ctx, ds2); err != nil {
		t.Fatalf("the restored repository's first pass: %v", err)
	}
	if report := mustVerify(t, svc2, testdb.Repository(t)); !report.OK || report.ArchiveHead != head.Seq {
		t.Fatalf("the restored repository does not verify: %+v", report)
	}
}

// A database restored from a backup older than the archive (a dump from
// last night beside this morning's archive) is not taken over by the
// archive at the boot (rule 4): it serves its own history, and its pass
// stops, writing nothing. The operator's choice to take the archive replaces
// the repository whole at the next boot, and it serves what the archive
// held.
func TestBootKeepsADatabaseBehindItsArchiveUntilTheOperatorTakesIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	writeSomeHistory(t, ds)
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatal(err)
	}
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	backup := testdb.Copy(t, dsn)

	// The repository goes on, and its archive with it.
	svc = mustReopen(t, dsn, root)
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "written after the backup"}})
	before := foldOf(t, ds)
	rows := tableChangelog(t, dsn)
	_ = svc.Close()

	// The backup is restored beside the newer archive.
	svc2 := mustReopen(t, backup, root)
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if after := foldOf(t, ds2); string(after) == string(before) {
		t.Fatal("the boot took the archive's history over the database's")
	}
	if err := engine.ArchivePass(ctx, ds2); !errors.Is(err, engine.ErrArchiveDiverged) {
		t.Fatalf("a pass beside an archive the database did not leave: %v, want ErrArchiveDiverged", err)
	}
	if _, err := svc2.(engine.Operator).ResolveArchive(ctx, testdb.Repository(t), false); err != nil {
		t.Fatalf("take the archive: %v", err)
	}
	_ = svc2.Close()
	svc3 := mustReopen(t, backup, root)
	ds3, err := svc3.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if after := foldOf(t, ds3); string(after) != string(before) {
		t.Fatalf("the fold after the take is not the archive's\n%s", firstDifference(before, after))
	}
	if after := tableChangelog(t, backup); len(after) != len(rows) {
		t.Fatalf("%d rows after the take, the archive held %d", len(after), len(rows))
	}
	if err := engine.ArchivePass(ctx, ds3); err != nil {
		t.Fatalf("the first pass after the take: %v", err)
	}
}

// An archive that does not continue the table's history (another line at
// the table's head) is left alone at the boot, which goes on, and the pass
// refuses to write over it.
func TestBootLeavesAnArchiveThatDisagreesWithTheTable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	writeSomeHistory(t, ds)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	other := copyRepositoryDir(t, root, id)
	// Two histories from one point: the table goes one way, the copy of
	// its archive another.
	svc = mustReopen(t, dsn, root)
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "this way"}})
	_ = svc.Close()
	dsnOther := engine.MigratedDSN(t)
	svcOther := mustReopen(t, dsnOther, other)
	dsOther, err := svcOther.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"that way", "and further"} {
		mustPut(t, dsOther, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": name}})
	}
	_ = svcOther.Close()
	rel := filepath.Join("archive", changelogfile.RepositoriesDir, id)
	if err := os.RemoveAll(filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}
	copyDir(t, filepath.Join(other, rel), filepath.Join(root, rel))

	rows := tableChangelog(t, dsn)
	svc = mustReopen(t, dsn, root)
	if after := tableChangelog(t, dsn); len(after) != len(rows) {
		t.Fatalf("the boot imported %d rows from an archive that is another history", len(after)-len(rows))
	}
	ds, err = svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ArchivePass(ctx, ds); !errors.Is(err, engine.ErrArchiveDiverged) {
		t.Fatalf("a pass over another history: %v, want ErrArchiveDiverged", err)
	}
}

// A restore that dies part way lands its rows in batches and folds after the
// last one. Each case kills it at one durable step (the restore first, then
// the row path a resumed boot takes), holds that no dataset opens over what
// the crash left, reboots, and compares the result to the original: the same
// fold, the same changelog rows, nothing appended, the marker gone.
func TestAnInterruptedRestoreResumesAtTheNextBoot(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	writeSomeHistory(t, ds)
	ref := putProvider(t, ds, dsn, "openai", "sk-survives-the-crash")
	before := foldOf(t, ds)
	head := maxSeq(t, ds)
	rows := tableChangelog(t, dsn)
	id := repositoryIDOf(t, ds)
	username := testdb.Repository(t)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	const batch = 7
	batches := int((head + batch - 1) / batch)
	if batches < 3 {
		t.Fatalf("head %d spans %d batches of %d; the schedule below needs at least 3", head, batches, batch)
	}
	type crash struct {
		stage string
		nth   int
	}
	cases := []struct {
		name    string
		crashes []crash
	}{
		{"after the first batch", []crash{{engine.ImportAfterBatch, 1}}},
		{"after the last batch", []crash{{engine.ImportAfterBatch, batches}}},
		{"between the fold passes", []crash{{engine.ImportAfterFirstFold, 1}}},
		{"at every step in turn", []crash{
			{engine.ImportAfterBatch, 1},
			{engine.ImportAfterBatch, batches - 1},
			{engine.ImportAfterFirstFold, 1},
		}},
	}
	errKilled := errors.New("the process died here")
	ctx := context.Background()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root2 := copyRepositoryDir(t, root, id)
			dsn2 := engine.MigratedDSN(t)
			for i, c := range tc.crashes {
				seen := 0
				_, err := engine.OpenForTest(t, ctx, dsn2,
					engine.WithKindsFS(engine.SeedKinds),
					engine.WithDataRoot(root2),
					engine.WithTestImportFault(batch, func(stage string) error {
						if stage != c.stage {
							return nil
						}
						seen++
						if seen == c.nth {
							return errKilled
						}
						return nil
					}))
				if !errors.Is(err, errKilled) {
					t.Fatalf("boot %d did not die %s (occurrence %d): %v", i+1, c.stage, c.nth, err)
				}
				incomplete, err := engine.ImportIncomplete(ctx, rawDB(t, dsn2))
				if err != nil {
					t.Fatal(err)
				}
				if !incomplete {
					t.Fatalf("boot %d died %s and left no import-progress marker", i+1, c.stage)
				}
				ro, err := engine.OpenForTest(t, ctx, dsn2,
					engine.WithKindsFS(engine.SeedKinds),
					engine.WithDataRoot(root2),
					engine.WithDirectoryReadOnly())
				if err != nil {
					t.Fatalf("open read-only after boot %d: %v", i+1, err)
				}
				if _, err := ro.Dataset(ctx, username); !errors.Is(err, engine.ErrImportIncomplete) {
					t.Fatalf("a read-only open after boot %d = %v, want ErrImportIncomplete", i+1, err)
				}
				report := mustVerify(t, ro, username)
				if report.OK || !findingContaining(report, "import of the repository's history has not completed") {
					t.Fatalf("verify after boot %d does not name the unfinished import: %+v", i+1, report)
				}
				_ = ro.Close()
			}

			svc2 := mustReopen(t, dsn2, root2)
			ds2, err := svc2.Dataset(ctx, username)
			if err != nil {
				t.Fatalf("open the repository after the import resumed: %v", err)
			}
			if after := foldOf(t, ds2); string(after) != string(before) {
				t.Fatalf("the resumed import's fold is not the original\n%s", firstDifference(before, after))
			}
			if got := maxSeq(t, ds2); got != head {
				t.Fatalf("head after the resumed import = %d, want %d: the boot appended", got, head)
			}
			after := tableChangelog(t, dsn2)
			if len(after) != len(rows) {
				t.Fatalf("%d changelog rows after the resumed import, want %d", len(after), len(rows))
			}
			for seq, hash := range rows {
				if !bytes.Equal(after[seq], hash) {
					t.Fatalf("seq %d: checksum after the resumed import is not the original's", seq)
				}
			}
			if got := openSecret(t, dsn2, ref); got != "sk-survives-the-crash" {
				t.Fatalf("secret after the resumed import = %q", got)
			}
			if report := mustVerify(t, svc2, username); !report.OK || report.Head != head {
				t.Fatalf("the resumed repository does not verify: %+v", report)
			}
		})
	}
}

// A restore whose data key the host's credential key does not open is
// refused before any row exists, naming the repository and what to do.
func TestARestoreTheKeyCannotOpenRefusesTheBoot(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	_ = putProvider(t, ds, dsn, "openai", "sk-locked")
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	root2 := copyRepositoryDir(t, root, id)
	dsn2 := engine.MigratedDSN(t)
	other := make([]byte, 32)
	other[0] = 1
	_, err := engine.OpenForTest(t, context.Background(), dsn2,
		engine.WithKindsFS(engine.SeedKinds), engine.WithDataRoot(root2),
		engine.WithCredentialKey(base64.StdEncoding.EncodeToString(other)))
	if err == nil || !strings.Contains(err.Error(), "does not open") || !strings.Contains(err.Error(), id) {
		t.Fatalf("a restore under another key: %v, want a refusal naming the repository and the key", err)
	}
	if n := len(tableChangelog(t, dsn2)); n != 0 {
		t.Fatalf("the refused restore left %d rows", n)
	}
}

// A directory a release before the archive wrote, with no row, is imported
// whole at the boot, gives up its history, and keeps its blobs where the
// server reads them.
func TestBootImportsADirectoryFromBeforeTheArchive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	writeSomeHistory(t, ds)
	ref := putProvider(t, ds, dsn, "openai", "sk-legacy")
	digest := putBlob(t, ds, []byte("legacy bytes"))
	before := foldOf(t, ds)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := t.TempDir()
	dir := legacyDirectory(t, filepath.Join(root, "archive"), root2, id)
	dsn2 := engine.MigratedDSN(t)
	svc2 := mustReopen(t, dsn2, root2)
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the imported repository: %v", err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the imported fold is not the original\n%s", firstDifference(before, after))
	}
	if got := openSecret(t, dsn2, ref); got != "sk-legacy" {
		t.Fatalf("the imported secret = %q", got)
	}
	if got := getBlob(t, ds2, digest); string(got) != "legacy bytes" {
		t.Fatalf("the imported blob = %q", got)
	}
	for _, gone := range []string{changelogfile.ManifestName, changelogfile.ChangelogSubdir, changelogfile.SealedSubdir} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the directory still holds %s after its history moved to the table: %v", gone, err)
		}
	}
}

// A directory from before the archive beside a database that holds the
// repository (an upgrade in place) is held to the table where both hold
// lines, gives the table what it holds past the table's head, and then gives
// its history up; one that disagrees with the table refuses the boot and is
// left as it was.
func TestBootAdoptsADirectoryFromBeforeTheArchive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	writeSomeHistory(t, ds)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	dir := legacyDirectory(t, filepath.Join(root, "archive"), root, id)
	svc = mustReopen(t, dsn, root)
	if changelogfile.HasHistory(dir) {
		t.Fatal("the boot left the directory's history beside the table")
	}
	if _, err := svc.Dataset(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	_ = svc.Close()

	// The same directory, with a line the table does not hold at its head.
	dir = legacyDirectory(t, filepath.Join(root, "archive"), root, id)
	raw := rawDB(t, dsn)
	if _, err := raw.Exec(`UPDATE changelog SET hash = sha256(hash) WHERE seq = (SELECT max(seq) FROM changelog)`); err != nil {
		t.Fatal(err)
	}
	if _, err := reopen(t, dsn, root); !errors.Is(err, engine.ErrChangelogDiverged) {
		t.Fatalf("a directory that disagrees with the table: %v, want ErrChangelogDiverged", err)
	}
	if !changelogfile.HasHistory(dir) {
		t.Fatal("the refused boot removed the directory it disagreed with")
	}
}

// A directory from before the archive with no manifest and no row is left
// where it is and named in the log: nothing says whose it is.
func TestBootSkipsADirectoryWithNoManifest(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	id, err := vocabulary.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := changelogfile.EnsureRepoDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(changelogfile.ChangelogDir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	svc2 := mustReopen(t, dsn, root)
	if repos, err := svc2.Repositories(context.Background()); err != nil || len(repos) != 0 {
		t.Fatalf("a directory with no manifest became %+v (%v)", repos, err)
	}
	if !changelogfile.HasHistory(dir) {
		t.Fatal("the skipped directory was removed")
	}
}

// The purge erases a record's sealed rows in its own transaction, and the
// sweep erases those a tombstone outlived; a live record's stay and open.
func TestPurgeErasesTheRecordsSealedMaterial(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	swept := putProvider(t, ds, dsn, "swept", "sk-swept")
	purged := putProvider(t, ds, dsn, "purged", "sk-purged")
	kept := putProvider(t, ds, dsn, "kept", "sk-kept")
	if _, err := ds.Delete(ctx, owner, typeProvider, "swept", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if sealedRowCount(t, dsn, swept) != 1 {
		t.Fatal("the tombstone erased its sealed material; a restore could not read the secret")
	}
	if _, err := ds.Delete(ctx, owner, typeProvider, "purged", substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("delete with purge: %v", err)
	}
	if sealedRowCount(t, dsn, purged) != 0 {
		t.Fatal("the purge left the record's sealed row behind")
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	for name, ref := range map[string]string{"swept": swept, "purged": purged} {
		if n := sealedRowCount(t, dsn, ref); n != 0 {
			t.Errorf("%s: %d sealed row(s) outlived the purge", name, n)
		}
	}
	if got := openSecret(t, dsn, kept); got != "sk-kept" {
		t.Fatalf("the live secret = %q", got)
	}
	if report := mustVerify(t, svc, testdb.Repository(t)); !report.OK || report.SealedOrphans != 0 {
		t.Fatalf("the repository does not verify after the purge: %+v", report)
	}
}

// Anything else under repositories/ refuses the boot by name: a directory
// named by neither a repository id nor an authority (a directory from before
// ids) may be a repository nothing can import, so it is not skipped. The boot
// goes through once the entry is moved out.
func TestBootRefusesAStrayDirectoryThatIsNotAnAuthority(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	for _, name := range []string{"tmp", "Backup-2026", "not an id", "k3j9x2m41pfq"} {
		dir := filepath.Join(root, changelogfile.RepositoriesDir, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := reopen(t, dsn, root)
		if err == nil {
			t.Fatalf("%q under repositories/ booted", name)
		}
		if !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "move it out of the data root") {
			t.Fatalf("the refusal of %q must name it and say what to do: %v", name, err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	mustReopen(t, dsn, root)
}

// A registration mints the repository's id beside its name, and the id keys
// its storage at every layer (decision 0261): the control-plane row's primary
// key, RepositoryInfo.ID, the scope a dataset runs under, the self-description
// record's id, the directory under the data root, the archive's, the fs blob
// path, and the binding of the DEK wrap. The name is the authority column,
// RepositoryInfo.Authority and the manifest's authority, and never the id.
func TestRegistrationMintsTheIdBesideTheAuthority(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	ctx := context.Background()
	registerUser(t, svc, "ada.example.com")
	const authority = "ada.example.com"

	repos, err := svc.Repositories(ctx)
	if err != nil || len(repos) != 1 {
		t.Fatalf("repositories = %+v, %v", repos, err)
	}
	minted := repos[0].ID
	if !vocabulary.ValidRepositoryID(minted) || !strings.HasPrefix(minted, vocabulary.RepositoryIDPrefix) ||
		minted == authority || repos[0].Authority != authority {
		t.Fatalf("RepositoryInfo = %+v, want a minted %s id and the authority %q", repos[0], vocabulary.RepositoryIDPrefix, authority)
	}
	var id, column string
	var wrapped []byte
	if err := rawDB(t, dsn).QueryRow(`SELECT id, authority, dek FROM repositories`).Scan(&id, &column, &wrapped); err != nil {
		t.Fatal(err)
	}
	if id != minted || column != authority {
		t.Fatalf("row id = %q, authority = %q, want %q and %q", id, column, minted, authority)
	}
	ds, err := svc.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := ds.Repository(); got.ID != minted || got.Authority != authority {
		t.Fatalf("the dataset's repository = %+v", got)
	}
	// Every scoped row carries the id as its repository.
	var scoped string
	if err := rawDB(t, dsn).QueryRow(`SELECT DISTINCT repository FROM changelog`).Scan(&scoped); err != nil || scoped != minted {
		t.Fatalf("changelog rows are scoped to %q (%v), want %q", scoped, err, minted)
	}
	self, err := ds.Get(ctx, "substrate.reamde.dev/core/repository", minted)
	if err != nil {
		t.Fatalf("the self-description record under the id: %v", err)
	}
	if self.Properties["name"] != authority || self.Properties["authority"] != authority {
		t.Fatalf("the self-description names %v / %v, want %q", self.Properties["name"], self.Properties["authority"], authority)
	}
	var selves int
	if err := rawDB(t, dsn).QueryRow(`SELECT count(*) FROM records WHERE kind = 'substrate.reamde.dev/core/repository'`).Scan(&selves); err != nil || selves != 1 {
		t.Fatalf("self-description records = %d (%v), want the one under the id", selves, err)
	}

	root := engine.DataRootOf(svc)
	dir := filepath.Join(root, changelogfile.RepositoriesDir, minted)
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m, _, err := changelogfile.ReadArchiveManifestDir(filepath.Join(root, "archive", changelogfile.RepositoriesDir, minted))
	if err != nil || m.Authority != authority || !bytes.Equal(m.DEK, wrapped) {
		t.Fatalf("archive manifest = %+v (%v)", m, err)
	}
	if _, err := os.Stat(filepath.Join(root, changelogfile.RepositoriesDir, authority)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a directory is named by the authority: %v", err)
	}
	digest := putBlob(t, ds, []byte("bytes under the id"))
	if got, err := os.ReadFile(filepath.Join(dir, "blobs", digest)); err != nil || string(got) != "bytes under the id" {
		t.Fatalf("blob at <root>/repositories/%s/blobs/%s: %q, %v", minted, digest, got, err)
	}
	// The DEK wrap is bound to the id and to nothing else.
	if _, err := engine.OpenPayloadWithKey(engine.TestCredentialKeyBytes, wrapped, engine.DEKAAD(minted)); err != nil {
		t.Fatalf("the DEK does not open under dek\\x00%s: %v", minted, err)
	}
	if _, err := engine.OpenPayloadWithKey(engine.TestCredentialKeyBytes, wrapped, engine.DEKAAD(authority)); err == nil {
		t.Fatal("the DEK opened under the authority, a binding that is not the id")
	}
}

// errCrash is what a commit-fault hook returns where the process would die.
var errCrash = errors.New("test: the process died here")

// armedFault is a WithTestCommitFault hook a test arms for one stage, once:
// the first write that reaches the stage dies there, every other write runs.
type armedFault struct {
	mu    sync.Mutex
	stage string
}

func (f *armedFault) arm(stage string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stage = stage
}

func (f *armedFault) hook(stage string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if stage != f.stage {
		return nil
	}
	f.stage = ""
	return errCrash
}

// lockedLog is a log sink the test reads while the service's own goroutines
// may still write to it.
type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLog) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A 0.x directory is held to the table line for line at the first boot
// (rule 6), every seq both hold, before either is converted: a line that
// differs at seq 1, or a seq one side skips, refuses the boot and leaves the
// directory as it was. Past the shorter one's head the longer one's lines
// stand: the table's, which a crash of the release that wrote the directory
// left ahead (it appended to the file after the commit), and the
// directory's, which a database restored from a backup older than the data
// root leaves behind it.
func TestA0xDirectoryIsHeldToTheTableLineForLine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// source is a stopped repository with its archive, and the directory a
	// 0.x release would have kept of it, under the same data root.
	source := func(t *testing.T) (dsn, root, id, dir string, fold []byte) {
		svc, ds, dsn := newDatasetWithDSN(t)
		writeSomeHistory(t, ds)
		fold, id, root = foldOf(t, ds), repositoryIDOf(t, ds), engine.DataRootOf(svc)
		_ = svc.Close()
		dir = legacyDirectory(t, filepath.Join(root, "archive"), root, id)
		return dsn, root, id, dir, fold
	}
	refused := func(t *testing.T, dsn, root, dir, want string) {
		t.Helper()
		before, err := os.ReadFile(filepath.Join(dir, changelogfile.ManifestName))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := reopen(t, dsn, root); !errors.Is(err, engine.ErrChangelogDiverged) || !strings.Contains(err.Error(), want) {
			t.Fatalf("the boot over the directory: %v, want ErrChangelogDiverged naming %q", err, want)
		}
		if after, err := os.ReadFile(filepath.Join(dir, changelogfile.ManifestName)); err != nil || !bytes.Equal(after, before) || !changelogfile.HasHistory(dir) {
			t.Fatalf("the refused boot touched the directory (%v)", err)
		}
	}

	t.Run("a line that differs at seq 1", func(t *testing.T) {
		t.Parallel()
		dsn, root, _, dir, _ := source(t)
		db := rawDB(t, dsn)
		if _, err := db.ExecContext(ctx, `UPDATE changelog SET hash = sha256(hash) WHERE seq = 1`); err != nil {
			t.Fatal(err)
		}
		refused(t, dsn, root, dir, "seq 1")
	})

	t.Run("a seq the table skips", func(t *testing.T) {
		t.Parallel()
		dsn, root, _, dir, _ := source(t)
		if _, err := rawDB(t, dsn).ExecContext(ctx, `DELETE FROM changelog WHERE seq = 2`); err != nil {
			t.Fatal(err)
		}
		refused(t, dsn, root, dir, "seq 2")
	})

	t.Run("the table ahead", func(t *testing.T) {
		t.Parallel()
		svc, ds, dsn := newDatasetWithDSN(t)
		writeSomeHistory(t, ds)
		root, id := engine.DataRootOf(svc), repositoryIDOf(t, ds)
		_ = svc.Close()
		// The directory as the release left it, then lines the table took
		// after it was last appended to.
		root2 := t.TempDir()
		dir := legacyDirectory(t, filepath.Join(root, "archive"), root2, id)
		svc = mustReopen(t, dsn, root)
		ds, err := svc.Dataset(ctx, testdb.Repository(t))
		if err != nil {
			t.Fatal(err)
		}
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "the table's alone"}})
		want := foldOf(t, ds)
		_ = svc.Close()
		svc = mustReopen(t, dsn, root2)
		if ds, err = svc.Dataset(ctx, testdb.Repository(t)); err != nil {
			t.Fatal(err)
		}
		if got := foldOf(t, ds); string(got) != string(want) {
			t.Fatalf("the table's lines past the directory's head did not stand\n%s", firstDifference(want, got))
		}
		if changelogfile.HasHistory(dir) {
			t.Fatal("the directory's history was not given up")
		}
	})

	t.Run("the directory ahead", func(t *testing.T) {
		t.Parallel()
		svc, ds, dsn := newDatasetWithDSN(t)
		writeSomeHistory(t, ds)
		root, id := engine.DataRootOf(svc), repositoryIDOf(t, ds)
		_ = svc.Close()
		backup := testdb.Copy(t, dsn)
		svc = mustReopen(t, dsn, root)
		if ds, err := svc.Dataset(ctx, testdb.Repository(t)); err != nil {
			t.Fatal(err)
		} else {
			mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "the directory's alone"}})
		}
		_ = svc.Close()
		svc = reopenBesideNewerDirectory(t, backup, root, id)
		ds2, err := svc.Dataset(ctx, testdb.Repository(t))
		if err != nil {
			t.Fatal(err)
		}
		if head, err := ds2.Head(ctx); err != nil || head.Seq != int64(len(tableChangelog(t, dsn))) {
			t.Fatalf("the backup's head after the directory's lines came in: %+v (%v), want %d", head, err, len(tableChangelog(t, dsn)))
		}
	})
}
