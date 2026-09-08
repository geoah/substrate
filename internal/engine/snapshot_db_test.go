package engine_test

// The snapshot and the side-store half of verify: a copy that misses a file
// the fold needs is named after an import, a sealed file that does not open
// is named under the key, and a snapshot records the point a restore comes
// back at.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

type snapshotter interface {
	SnapshotRepository(ctx context.Context, username, destRoot string) (engine.SnapshotReport, error)
}

// A copy of a stopped repository that lost one stored blob's bytes and one
// live secret's sealed file, with a second blob's bytes damaged in place,
// imports into an empty database and verify names all three. The files the
// fold does not need (a blob nobody references, a note at the top) are not
// findings.
func TestVerifyReportsMissingAndDamagedSideStoreFiles(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	ctx := context.Background()
	registerUser(t, svc, "ada")
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	ref := putProvider(t, ds, dsn, "openai", "sk-goes-missing")
	missing := putBlob(t, ds, []byte("bytes that go missing"))
	damaged := putBlob(t, ds, []byte("bytes that get damaged"))
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dir2, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	blobs, sealed := changelogfile.BlobsDir(dir2), changelogfile.SealedDir(dir2)
	if err := os.Remove(filepath.Join(blobs, missing)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(sealed, changelogfile.SealedFileName(ref))); err != nil {
		t.Fatal(err)
	}
	// The same length, other bytes: only the hash tells.
	if err := os.WriteFile(filepath.Join(blobs, damaged), []byte("bytes that get DAMAGED"), 0o600); err != nil {
		t.Fatal(err)
	}
	stray := "blob-sha256-" + strings.Repeat("0", 64)
	if err := os.WriteFile(filepath.Join(blobs, stray), []byte("nobody references this"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "notes.txt"), []byte("copied by hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	svc2 := mustReopen(t, testdb.NewSchema(t), root2)
	report := mustVerify(t, svc2, "ada")
	if report.OK {
		t.Fatalf("a copy short of a blob's bytes and a live secret's file verified: %+v", report)
	}
	for _, want := range []string{
		"blob " + missing + ": the manifest is stored and the fs store holds no bytes",
		"blob " + damaged + ": the stored bytes hash to blob-sha256-",
		"secret " + ref + ": " + typeProvider + " openai names it in apiKey and sealed/ has no file for it",
	} {
		if !findingContaining(report, want) {
			t.Errorf("no finding contains %q:\n%s", want, strings.Join(report.Findings, "\n"))
		}
	}
	if len(report.Findings) != 3 {
		t.Fatalf("%d findings, want exactly the three damages (the stray blob and the note are not findings):\n%s",
			len(report.Findings), strings.Join(report.Findings, "\n"))
	}
	if report.Blobs != 2 || report.SecretRefs < 3 {
		t.Fatalf("blobs %d (want 2), secret refs %d (want the apiKey and both credential refs at least)", report.Blobs, report.SecretRefs)
	}
}

// A sealed file whose row agrees with it byte for byte passes the mirror
// check; only opening it under the key tells that the ciphertext is not the
// DEK's. Without the key nothing is opened and the damage is invisible, which
// is what the report says with SealedOpened at 0.
func TestVerifyOpensEverySealedFileUnderTheKey(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	ctx := context.Background()
	registerUser(t, svc, "ada")
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	ref := putProvider(t, ds, dsn, "openai", "sk-does-not-open")
	root := engine.DataRootOf(svc)
	dir := repoDirOf(t, svc, ds)
	_ = svc.Close()

	// The boot mirrors the files from the table, so the damage lands after
	// it, on both halves alike: a disk that flipped a byte of a payload the
	// server then read back into its row.
	svc2 := mustReopen(t, dsn, root)
	if report := mustVerify(t, svc2, "ada"); !report.OK || report.SealedOpened != report.SealedFiles || report.SealedOpened == 0 {
		t.Fatalf("the repository does not verify before the damage, or not every file was opened: %+v", report)
	}
	path := filepath.Join(changelogfile.SealedDir(dir), changelogfile.SealedFileName(ref))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec changelogfile.SealedRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	rec.Payload[len(rec.Payload)-1] ^= 0xff
	out, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB(t, dsn).Exec(`UPDATE sealed SET payload = $1 WHERE ref = $2`, rec.Payload, ref); err != nil {
		t.Fatal(err)
	}

	report := mustVerify(t, svc2, "ada")
	want := "sealed/" + changelogfile.SealedFileName(ref) + " (" + typeProvider + " openai): does not open under the DEK"
	if report.OK || !findingContaining(report, want) {
		t.Fatalf("a sealed file that does not open was not named:\n%+v", report)
	}
	if report.SealedOpened != report.SealedFiles-1 {
		t.Fatalf("opened %d of %d files, want every other one", report.SealedOpened, report.SealedFiles)
	}

	// A process without the key compares bytes and opens nothing.
	keyless := reopenWith(t, dsn, root, engine.WithDirectoryReadOnly(), engine.WithCredentialKey(""))
	blind := mustVerify(t, keyless, "ada")
	if !blind.OK || blind.SealedOpened != 0 {
		t.Fatalf("without the key the damage must be invisible and nothing opened: %+v", blind)
	}
}

// A snapshot of a stopped repository is a data root a fresh server imports:
// the fold, the blob and the secret come back, verify passes with the key
// and reports the point the snapshot recorded, and the repository writes on
// past that point without a finding.
func TestSnapshotRecordsThePointAndRestoresIntoAnEmptyDatabase(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	ctx := context.Background()
	registerUser(t, svc, "ada")
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	for _, name := range []string{"one", "two"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": name}})
	}
	ref := putProvider(t, ds, dsn, "openai", "sk-snapshot")
	digest := putBlob(t, ds, []byte("snapshot bytes"))
	before := foldOf(t, ds)
	head := maxSeq(t, ds)
	secretBefore := openSecret(t, dsn, ref)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	// The operator's process: the server is stopped, so it takes the lock.
	operator := mustReopen(t, dsn, root)
	dest := t.TempDir()
	report, err := operator.(snapshotter).SnapshotRepository(ctx, "ada", dest)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	wantDir, _ := changelogfile.RepoDir(dest, id)
	if report.Directory != wantDir || report.Head != head || report.Blobs != 1 || report.BlobStore != "fs" || report.SealedFiles < 3 {
		t.Fatalf("snapshot report = %+v, want dir %s at head %d with one blob and the sealed files", report, wantDir, head)
	}
	snap, err := changelogfile.ReadSnapshot(report.Directory)
	if err != nil {
		t.Fatalf("the copy carries no readable %s: %v", changelogfile.SnapshotName, err)
	}
	if snap.Head != head || hex.EncodeToString(snap.HeadHash[:]) != report.HeadHash || report.HeadHash == "" {
		t.Fatalf("recorded point = seq %d %x, want seq %d %s", snap.Head, snap.HeadHash, head, report.HeadHash)
	}
	if len(snap.Blobs) != 1 || snap.Blobs[0] != digest || snap.BlobLocation != "" || snap.SealedFiles != report.SealedFiles {
		t.Fatalf("snapshot = %+v, want the one stored digest %s in the directory", snap, digest)
	}
	if _, err := operator.(snapshotter).SnapshotRepository(ctx, "ada", dest); !errors.Is(err, engine.ErrSnapshotExists) {
		t.Fatalf("a second snapshot over the first must be refused, got %v", err)
	}
	_ = operator.Close()

	// The restore: the destination is laid out as a data root.
	dsn2 := testdb.NewSchema(t)
	svc2 := mustReopen(t, dsn2, dest)
	ds2, err := svc2.Dataset(ctx, "ada")
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the restored fold is not the original\n%s", firstDifference(before, after))
	}
	if got := getBlob(t, ds2, digest); string(got) != "snapshot bytes" {
		t.Fatalf("blob bytes = %q", got)
	}
	if got := openSecret(t, dsn2, ref); got != secretBefore {
		t.Fatalf("secret = %q, want %q", got, secretBefore)
	}
	verified := mustVerify(t, svc2, "ada")
	if !verified.OK || verified.Head != head {
		t.Fatalf("the restored repository does not verify: %+v", verified)
	}
	if verified.Snapshot == nil || verified.Snapshot.Head != head || verified.Snapshot.HeadHash != verified.HeadHash {
		t.Fatalf("verify did not report the recorded point: %+v (head checksum %s)", verified.Snapshot, verified.HeadHash)
	}
	if verified.SealedOpened != verified.SealedFiles || verified.Blobs != 1 || verified.BlobBytes != int64(len("snapshot bytes")) {
		t.Fatalf("verify opened %d of %d sealed files and hashed %d blobs (%d bytes)", verified.SealedOpened, verified.SealedFiles, verified.Blobs, verified.BlobBytes)
	}
	mustPut(t, ds2, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "after the restore"}})
	if grown := mustVerify(t, svc2, "ada"); !grown.OK || grown.Snapshot == nil || grown.Snapshot.Head != head || grown.Head != head+1 {
		t.Fatalf("a write past the recorded point must not be a finding: %+v", grown)
	}
}

// snapshotFixture registers ada with one secret and one blob and stops the
// server: the repository an operator's process snapshots.
func snapshotFixture(t *testing.T) (dsn, root, id, ref, digest string, head int64) {
	t.Helper()
	svc, dsn := newService(t)
	ctx := context.Background()
	registerUser(t, svc, "ada")
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	ref = putProvider(t, ds, dsn, "openai", "sk-fixture")
	digest = putBlob(t, ds, []byte("fixture bytes"))
	head = maxSeq(t, ds)
	id = repositoryIDOf(t, ds)
	root = engine.DataRootOf(svc)
	_ = svc.Close()
	return dsn, root, id, ref, digest, head
}

// nothingAt asserts the destination root holds neither the repository's
// directory nor a partial the snapshot was building.
func nothingAt(t *testing.T, dest, id string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dest, changelogfile.RepositoriesDir, id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the destination holds the repository directory after a failed snapshot: %v", err)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".incoming-") {
			t.Fatalf("a partial %s was left under the destination", e.Name())
		}
	}
}

// A copied file damaged between the copy and the read-back is caught the way
// the source is verified: a sealed file that is still valid JSON and base64
// but not the source's ciphertext, a blob whose bytes are not its digest's, a
// segment whose line no longer checks. The snapshot is discarded whole, the
// destination is left empty, and a clean retry into the same place succeeds.
func TestSnapshotDiscardsACopyThatDoesNotReadBack(t *testing.T) {
	t.Parallel()
	cases := map[string]func(t *testing.T, dir, ref, digest string){
		"sealed": func(t *testing.T, dir, ref, _ string) {
			path := filepath.Join(changelogfile.SealedDir(dir), changelogfile.SealedFileName(ref))
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var rec changelogfile.SealedRecord
			if err := json.Unmarshal(raw, &rec); err != nil {
				t.Fatal(err)
			}
			rec.Payload[len(rec.Payload)-1] ^= 0xff
			out, err := json.Marshal(rec)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"blob": func(t *testing.T, dir, _, digest string) {
			if err := os.WriteFile(filepath.Join(changelogfile.BlobsDir(dir), digest), []byte("FIXTURE bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"segment": func(t *testing.T, dir, _, _ string) {
			segments, err := changelogfile.Segments(changelogfile.ChangelogDir(dir))
			if err != nil || len(segments) == 0 {
				t.Fatalf("segments of the copy: %v (%d)", err, len(segments))
			}
			path := filepath.Join(changelogfile.ChangelogDir(dir), segments[len(segments)-1].Name)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			raw[len(raw)/2] ^= 0x01
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, damage := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			dsn, root, id, ref, digest, head := snapshotFixture(t)
			armed := true
			operator := reopenWith(t, dsn, root, engine.WithTestSnapshotFault(func(stage, dir string) error {
				if stage == engine.SnapshotAfterCopy && armed {
					damage(t, dir, ref, digest)
				}
				return nil
			}))
			dest := t.TempDir()
			_, err := operator.(snapshotter).SnapshotRepository(ctx, "ada", dest)
			if !errors.Is(err, engine.ErrSnapshotCopyDamaged) {
				t.Fatalf("a damaged copy was kept: %v", err)
			}
			nothingAt(t, dest, id)
			armed = false
			report, err := operator.(snapshotter).SnapshotRepository(ctx, "ada", dest)
			if err != nil || report.Head != head {
				t.Fatalf("the retry into the same destination: %+v, %v", report, err)
			}
			if _, err := changelogfile.ReadSnapshot(report.Directory); err != nil {
				t.Fatalf("the retry left no %s: %v", changelogfile.SnapshotName, err)
			}
		})
	}
}

// A failure in the middle of the copy leaves nothing at the destination, not
// even the partial, and the same destination takes the retry.
func TestSnapshotFailureMidCopyLeavesNothingAndRetries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, id, _, _, head := snapshotFixture(t)
	diskFull := errors.New("test: the disk filled after the changelog")
	armed := true
	operator := reopenWith(t, dsn, root, engine.WithTestSnapshotFault(func(stage, _ string) error {
		if stage == engine.SnapshotAfterChangelog && armed {
			return diskFull
		}
		return nil
	}))
	dest := t.TempDir()
	if _, err := operator.(snapshotter).SnapshotRepository(ctx, "ada", dest); !errors.Is(err, diskFull) {
		t.Fatalf("the mid-copy failure was not the snapshot's error: %v", err)
	}
	nothingAt(t, dest, id)
	armed = false
	report, err := operator.(snapshotter).SnapshotRepository(ctx, "ada", dest)
	if err != nil || report.Head != head {
		t.Fatalf("the retry into the same destination: %+v, %v", report, err)
	}
}

// A snapshot copies nothing beside a running server, and nothing of a
// repository that does not verify.
func TestSnapshotRefusesTheLockAndADamagedRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	digest := putBlob(t, ds, []byte("bytes the snapshot needs"))
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	second, err := reopen(t, dsn, root)
	if err != nil {
		t.Fatalf("a second process could not boot beside the server: %v", err)
	}
	dest := t.TempDir()
	_, err = second.(snapshotter).SnapshotRepository(ctx, "geoah", dest)
	if !errors.Is(err, engine.ErrChangelogLocked) || !errors.Is(err, changelogfile.ErrLocked) {
		t.Fatalf("beside a running server the refusal must be the lock's: %v", err)
	}
	_ = second.Close()
	_ = svc.Close()

	dir, err := changelogfile.RepoDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(changelogfile.BlobsDir(dir), digest)); err != nil {
		t.Fatal(err)
	}
	operator := mustReopen(t, dsn, root)
	_, err = operator.(snapshotter).SnapshotRepository(ctx, "geoah", dest)
	if !errors.Is(err, engine.ErrSnapshotUnverified) || !strings.Contains(err.Error(), digest) {
		t.Fatalf("a repository short of a blob's bytes must refuse the snapshot and name the blob: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dest, changelogfile.RepositoriesDir, id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused snapshot must leave nothing behind, got %v", err)
	}
}
