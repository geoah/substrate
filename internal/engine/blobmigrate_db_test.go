package engine_test

// Moving blob bytes between two stores (blobmigrate.go). The second store is
// a second fs root: the walk only speaks blobbytes.Backend, and the s3
// backend's own suite (internal/blobbytes) holds it to the same contract, so
// these need no bucket.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
)

type blobMover interface {
	MigrateBlobs(ctx context.Context, repository string, from, to blobbytes.Backend, opts engine.BlobMigrateOptions) (engine.BlobMigrateReport, error)
	BlobStatus(ctx context.Context, repository string, from, to blobbytes.Backend, deep bool) (engine.BlobStatusReport, error)
	PruneBlobs(ctx context.Context, repository string, from, kept blobbytes.Backend, opts engine.BlobPruneOptions) (engine.BlobPruneReport, error)
	Close() error
}

func newFSBackend(t *testing.T, root string) *blobbytes.FS {
	t.Helper()
	b, err := blobbytes.NewFS(root)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// stoppedWithBlobs is a repository with blobs whose server has stopped: the
// state every migration starts from.
func stoppedWithBlobs(t *testing.T, contents ...string) (dsn, root string, digests []string) {
	t.Helper()
	svc, dsn := newService(t)
	registerUser(t, svc, "ada.example.com")
	ds, err := svc.Dataset(context.Background(), "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range contents {
		digests = append(digests, putBlob(t, ds, []byte(c)))
	}
	root = engine.DataRootOf(svc)
	_ = svc.Close()
	return dsn, root, digests
}

// overwrite replaces an fs object's bytes in place with others of the same
// length, which a size comparison cannot see.
func overwrite(t *testing.T, root, digest string) {
	t.Helper()
	path := filepath.Join(root, changelogfile.RepositoriesDir, "ada.example.com", "blobs", digest)
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", len(old))), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The whole switch: migrate, status, prune, then a server on the other store
// with the local bytes gone serves every blob and verifies.
func TestBlobsMigrateThenPruneMovesEveryBlob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	contents := []string{"first attachment", "second attachment", "third attachment"}
	dsn, root, digests := stoppedWithBlobs(t, contents...)
	local := newFSBackend(t, root)
	bucketRoot := t.TempDir()
	bucket := newFSBackend(t, bucketRoot)

	op := mustReopen(t, dsn, root).(blobMover)
	status, err := op.BlobStatus(ctx, "ada.example.com", local, bucket, false)
	if err != nil {
		t.Fatal(err)
	}
	if status.Complete() || status.Source != 3 || status.Missing != 3 || status.Manifests != 3 || status.ManifestsMissing != 3 {
		t.Fatalf("status before = %+v, want three missing", status)
	}

	dry, err := op.MigrateBlobs(ctx, "ada.example.com", local, bucket, engine.BlobMigrateOptions{DryRun: true})
	if err != nil || dry.Copied != 3 {
		t.Fatalf("dry run = %+v, %v; want three to copy", dry, err)
	}
	if objects, _ := mustBind(t, bucket).List(ctx, "", 0); len(objects) != 0 {
		t.Fatalf("a dry run wrote %d objects", len(objects))
	}

	report, err := op.MigrateBlobs(ctx, "ada.example.com", local, bucket, engine.BlobMigrateOptions{Concurrency: 2})
	if err != nil || !report.OK() || report.Copied != 3 || report.Source != 3 {
		t.Fatalf("migrate = %+v, %v; want three copied", report, err)
	}
	again, err := op.MigrateBlobs(ctx, "ada.example.com", local, bucket, engine.BlobMigrateOptions{})
	if err != nil || again.Present != 3 || again.Copied != 0 {
		t.Fatalf("second migrate = %+v, %v; want all three present and nothing copied", again, err)
	}
	status, err = op.BlobStatus(ctx, "ada.example.com", local, bucket, true)
	if err != nil || !status.Complete() || status.Matching != 3 || status.Destination != 3 {
		t.Fatalf("status after = %+v, %v; want complete", status, err)
	}

	dryPrune, err := op.PruneBlobs(ctx, "ada.example.com", local, bucket, engine.BlobPruneOptions{DryRun: true})
	if err != nil || dryPrune.Deleted != 3 {
		t.Fatalf("dry prune = %+v, %v", dryPrune, err)
	}
	if objects, _ := mustBind(t, local).List(ctx, "", 0); len(objects) != 3 {
		t.Fatalf("a dry prune deleted: %d objects left", len(objects))
	}
	pruned, err := op.PruneBlobs(ctx, "ada.example.com", local, bucket, engine.BlobPruneOptions{})
	if err != nil || !pruned.OK() || pruned.Deleted != 3 {
		t.Fatalf("prune = %+v, %v; want three deleted", pruned, err)
	}
	if objects, _ := mustBind(t, local).List(ctx, "", 0); len(objects) != 0 {
		t.Fatalf("%d objects left under the data root after the prune", len(objects))
	}
	_ = op.Close()

	// The server on the other store, with the boot check for local bytes on.
	svc, err := engine.OpenForTest(t, ctx, dsn,
		engine.WithDataRoot(root), engine.WithBlobStore(bucket), engine.WithBlobsOutsideDataRoot())
	if err != nil {
		t.Fatalf("open on the bucket: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	ds, err := svc.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range digests {
		if got := getBlob(t, ds, d); string(got) != contents[i] {
			t.Fatalf("blob %d = %q, want %q", i, got, contents[i])
		}
	}
	if v := mustVerify(t, svc, "ada.example.com"); !v.OK || v.Blobs != 3 {
		t.Fatalf("verify on the bucket = %+v", v)
	}
}

func mustBind(t *testing.T, b blobbytes.Backend) blobbytes.Store {
	t.Helper()
	s, err := b.Repository("ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A destination copy of the right size and the wrong bytes is found only by
// reading it: a plain run trusts the size, --deep rewrites it, and a damaged
// SOURCE object is never copied.
func TestBlobsMigrateDeepRepairsAndRefusesDamagedSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, digests := stoppedWithBlobs(t, "kept good", "damaged at the destination")
	local := newFSBackend(t, root)
	bucketRoot := t.TempDir()
	bucket := newFSBackend(t, bucketRoot)
	op := mustReopen(t, dsn, root).(blobMover)
	if r, err := op.MigrateBlobs(ctx, "ada.example.com", local, bucket, engine.BlobMigrateOptions{}); err != nil || r.Copied != 2 {
		t.Fatalf("migrate = %+v, %v", r, err)
	}
	overwrite(t, bucketRoot, digests[1])
	if r, err := op.MigrateBlobs(ctx, "ada.example.com", local, bucket, engine.BlobMigrateOptions{}); err != nil || r.Present != 2 {
		t.Fatalf("a plain run must trust the size: %+v, %v", r, err)
	}
	if s, _ := op.BlobStatus(ctx, "ada.example.com", local, bucket, true); s.Complete() || s.Mismatched != 1 {
		t.Fatalf("deep status = %+v, want one mismatched", s)
	}
	r, err := op.MigrateBlobs(ctx, "ada.example.com", local, bucket, engine.BlobMigrateOptions{Deep: true})
	if err != nil || r.Repaired != 1 || r.Copied != 1 || r.Present != 1 {
		t.Fatalf("deep migrate = %+v, %v; want the one repaired", r, err)
	}
	if got := readBack(t, bucket, digests[1]); got != "damaged at the destination" {
		t.Fatalf("the repaired copy holds %q", got)
	}

	// Now the source is the damaged one, into a fresh destination.
	overwrite(t, root, digests[0])
	fresh := newFSBackend(t, t.TempDir())
	r, err = op.MigrateBlobs(ctx, "ada.example.com", local, fresh, engine.BlobMigrateOptions{})
	if err != nil || r.OK() || len(r.SourceDamaged) != 1 || r.SourceDamaged[0] != digests[0] || r.Copied != 1 {
		t.Fatalf("migrate from a damaged source = %+v, %v; want it reported and not copied", r, err)
	}
	if held, _ := mustBind(t, fresh).Exists(ctx, digests[0]); held {
		t.Fatal("the damaged source object was copied")
	}
}

func readBack(t *testing.T, b blobbytes.Backend, digest string) string {
	t.Helper()
	data, err := blobbytes.ReadAll(context.Background(), mustBind(t, b), digest, -1)
	if err != nil {
		t.Fatalf("read %s: %v", digest, err)
	}
	return string(data)
}

// A prune never deletes the only copy: an object the kept store lacks stays,
// and one whose kept copy does not hash stops the prune.
func TestBlobsPruneKeepsWhatTheOtherStoreLacks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, digests := stoppedWithBlobs(t, "one", "two", "three")
	local := newFSBackend(t, root)
	bucketRoot := t.TempDir()
	bucket := newFSBackend(t, bucketRoot)
	op := mustReopen(t, dsn, root).(blobMover)
	if _, err := op.MigrateBlobs(ctx, "ada.example.com", local, bucket, engine.BlobMigrateOptions{}); err != nil {
		t.Fatal(err)
	}
	// The kept store loses one (the live server's sweep, or a mis-sequenced
	// start that wrote only locally).
	if err := mustBind(t, bucket).Delete(ctx, digests[1]); err != nil {
		t.Fatal(err)
	}
	r, err := op.PruneBlobs(ctx, "ada.example.com", local, bucket, engine.BlobPruneOptions{})
	if err != nil || r.OK() || r.Deleted != 2 || len(r.Absent) != 1 || r.Absent[0] != digests[1] {
		t.Fatalf("prune = %+v, %v; want two deleted and the absent one kept", r, err)
	}
	if held, _ := mustBind(t, local).Exists(ctx, digests[1]); !held {
		t.Fatal("the prune deleted the only copy")
	}

	// Copied again, then damaged at the kept store: the prune stops.
	if _, err := op.MigrateBlobs(ctx, "ada.example.com", local, bucket, engine.BlobMigrateOptions{}); err != nil {
		t.Fatal(err)
	}
	overwrite(t, bucketRoot, digests[1])
	r, err = op.PruneBlobs(ctx, "ada.example.com", local, bucket, engine.BlobPruneOptions{})
	if err != nil || r.Mismatched != digests[1] || r.Deleted != 0 {
		t.Fatalf("prune over a damaged copy = %+v, %v; want it stopped", r, err)
	}
	if held, _ := mustBind(t, local).Exists(ctx, digests[1]); !held {
		t.Fatal("the prune deleted the source of a damaged copy")
	}

	if _, err := op.PruneBlobs(ctx, "ada.example.com", local, newFSBackend(t, root), engine.BlobPruneOptions{}); !errors.Is(err, engine.ErrSameBlobStore) {
		t.Fatalf("a prune into the same store: %v, want ErrSameBlobStore", err)
	}
}

// A server on an external store refuses to boot while a repository directory
// holds bytes, naming it, and boots once they are gone.
func TestBootRefusesLocalBlobsBesideAnExternalStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, _ := stoppedWithBlobs(t, "left behind")
	bucket := newFSBackend(t, t.TempDir())
	_, err := engine.OpenForTest(t, ctx, dsn,
		engine.WithDataRoot(root), engine.WithBlobStore(bucket), engine.WithBlobsOutsideDataRoot())
	if !errors.Is(err, engine.ErrLocalBlobsBesideExternalStore) || !strings.Contains(err.Error(), "ada.example.com") {
		t.Fatalf("boot with local bytes beside a bucket: %v, want ErrLocalBlobsBesideExternalStore naming the repository", err)
	}

	op := mustReopen(t, dsn, root).(blobMover)
	local := newFSBackend(t, root)
	if _, err := op.MigrateBlobs(ctx, "ada.example.com", local, bucket, engine.BlobMigrateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := op.PruneBlobs(ctx, "ada.example.com", local, bucket, engine.BlobPruneOptions{}); err != nil {
		t.Fatal(err)
	}
	_ = op.Close()
	svc, err := engine.OpenForTest(t, ctx, dsn,
		engine.WithDataRoot(root), engine.WithBlobStore(bucket), engine.WithBlobsOutsideDataRoot())
	if err != nil {
		t.Fatalf("boot after the prune: %v", err)
	}
	_ = svc.Close()
}

// Migrate and prune open the repository as its writer, so beside a running
// server they are refused by the lock and touch nothing.
func TestBlobsMigrateRefusesBesideARunningServer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	putBlob(t, ds, []byte("in use"))
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	second, err := reopenBeside(t, dsn, root)
	if err != nil {
		t.Fatal(err)
	}
	local := newFSBackend(t, root)
	bucket := newFSBackend(t, t.TempDir())
	_, err = second.(blobMover).MigrateBlobs(ctx, id, local, bucket, engine.BlobMigrateOptions{})
	if !errors.Is(err, changelogfile.ErrLocked) {
		t.Fatalf("migrate beside a server: %v, want the lock's refusal", err)
	}
	_, err = second.(blobMover).PruneBlobs(ctx, id, local, bucket, engine.BlobPruneOptions{})
	if !errors.Is(err, changelogfile.ErrLocked) {
		t.Fatalf("prune beside a server: %v, want the lock's refusal", err)
	}
	b, err := bucket.Repository(id)
	if err != nil {
		t.Fatal(err)
	}
	if objects, _ := b.List(ctx, "", 0); len(objects) != 0 {
		t.Fatalf("a refused migrate wrote %d objects", len(objects))
	}
}
