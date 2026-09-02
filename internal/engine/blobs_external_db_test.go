package engine_test

// The engine against a blob store Postgres cannot join: bytes and manifest no
// longer commit together, so what these assert is the ORDER — that no crash
// between them leaves a reader looking at a `stored` manifest whose bytes are
// missing, and that whatever a crash does leave is collectable.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// blobDigestOf is the digest the engine derives, spelled out here so a test
// can name bytes it has not uploaded.
func blobDigestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return substrate.BlobDigestPrefix + hex.EncodeToString(sum[:])
}

// fsBackedDataset is a repository whose blob bytes live in a directory.
func fsBackedDataset(t *testing.T) (substrate.Service, substrate.Dataset, string) {
	t.Helper()
	root := t.TempDir()
	backend, err := blobbytes.NewFS(root)
	if err != nil {
		t.Fatalf("open the fs backend: %v", err)
	}
	svc, ds := newDataset(t, engine.WithBlobStore(backend))
	return svc, ds, root
}

// objectPath is where the fs backend keeps one repository's blob.
func objectPath(root string, ds substrate.Dataset, digest string) string {
	return filepath.Join(root, ds.Repository().ID, digest)
}

func TestBlobFSRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, root := fsBackedDataset(t)
	bs := blobStoreOf(t, ds)

	data := []byte("an attachment that never enters WAL")
	info, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{
		Name: "notes.txt", MediaType: "text/plain",
	}, data, "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	// The bytes are on disk under <root>/<repository>/<digest> and nowhere in
	// the database.
	onDisk, err := os.ReadFile(objectPath(root, ds, info.Digest))
	if err != nil {
		t.Fatalf("the object is not at its key: %v", err)
	}
	if string(onDisk) != string(data) {
		t.Fatalf("the object holds %q, uploaded %q", onDisk, data)
	}

	got, read, err := bs.GetBlob(ctx, info.Digest)
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}
	if string(read) != string(data) {
		t.Fatalf("read back %q, uploaded %q", read, data)
	}
	// The manifest is still the truth, so the metadata a read reports comes
	// back whole even though the store holds nothing but bytes.
	if got.Name != "notes.txt" || got.MediaType != "text/plain" || got.Size != int64(len(data)) {
		t.Fatalf("manifest reports (%q, %q, %d)", got.Name, got.MediaType, got.Size)
	}
	if got.Status != substrate.BlobStored {
		t.Fatalf("status is %q, want stored", got.Status)
	}

	// A dedup PUT of the same bytes under another name returns the first
	// writer's, exactly as it does on the postgres backend.
	again, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{Name: "other.txt"}, data, "")
	if err != nil {
		t.Fatalf("re-put: %v", err)
	}
	if again.Name != "notes.txt" {
		t.Fatalf("a second upload renamed the blob to %q", again.Name)
	}
}

func TestBlobFSIsRepositoryScoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, root := fsBackedDataset(t)
	data := []byte("one repository's archive")
	info, err := blobStoreOf(t, ds).PutBlob(ctx, owner, substrate.BlobUpload{}, data, "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	if _, err := svc.CreateRepository(ctx, "otheruser", "otheruser.example.com"); err != nil {
		t.Fatalf("create repository B: %v", err)
	}
	dsB, err := svc.Dataset(ctx, "otheruser")
	if err != nil {
		t.Fatalf("open repository B: %v", err)
	}
	// The digest is the same string in both repositories, and the second one
	// still cannot read the first one's bytes: the repository is half the key,
	// and it comes from the authenticated dataset rather than the request.
	_, _, err = blobStoreOf(t, dsB).GetBlob(ctx, info.Digest)
	wantErr(t, err, substrate.ErrNotFound, "cross-repository blob read")

	if _, err := blobStoreOf(t, dsB).PutBlob(ctx, owner, substrate.BlobUpload{}, data, ""); err != nil {
		t.Fatalf("put the same bytes in repository B: %v", err)
	}
	// Storing the same bytes twice stores them twice: there is no
	// cross-repository dedup, deliberately.
	for _, d := range []substrate.Dataset{ds, dsB} {
		if _, err := os.Stat(objectPath(root, d, info.Digest)); err != nil {
			t.Fatalf("repository %s has no object of its own: %v", d.Repository().ID, err)
		}
	}
}

// A crash between the bytes landing and the manifest settling leaves an object
// nothing names. No reader can see it, and the sweep reaps it.
func TestBlobFSOrphanObjectIsUnreadableAndSwept(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	ctx := context.Background()
	_, ds, root := fsBackedDataset(t)
	bs := blobStoreOf(t, ds)

	// The bytes, written as the store would write them, with no manifest
	// behind them — the state a crash after step 2 leaves.
	backend, err := blobbytes.NewFS(root)
	if err != nil {
		t.Fatalf("open the fs backend: %v", err)
	}
	store, err := backend.Repository(ds.Repository().ID, nil)
	if err != nil {
		t.Fatalf("bind the store: %v", err)
	}
	data := []byte("bytes whose manifest never settled")
	digest := blobDigestOf(data)
	if err := store.Put(ctx, digest, int64(len(data)), strings.NewReader(string(data))); err != nil {
		t.Fatalf("write the orphan object: %v", err)
	}

	// The read resolves through the manifest, so bytes with no manifest are
	// not a blob: a caller who guesses a digest gets a not-found, not a body.
	_, _, err = bs.GetBlob(ctx, digest)
	wantErr(t, err, substrate.ErrNotFound, "read of an orphan object")

	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if _, err := os.Stat(objectPath(root, ds, digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the orphan object survived the sweep: %v", err)
	}
}

// A crash between the pending manifest and the bytes leaves a manifest that
// never says `stored`. The guard is what holds that line, and the sweep
// collects what is left.
func TestBlobFSFailedWriteLeavesNoStoredManifest(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	ctx := context.Background()
	root := t.TempDir()
	fs, err := blobbytes.NewFS(root)
	if err != nil {
		t.Fatalf("open the fs backend: %v", err)
	}
	_, ds := newDataset(t, engine.WithBlobStore(refusingBackend{fs}))
	bs := blobStoreOf(t, ds)

	data := []byte("bytes the store refused")
	digest := blobDigestOf(data)
	if _, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{}, data, ""); err == nil {
		t.Fatal("a store that refuses the bytes must fail the upload")
	}

	// The manifest exists and is PENDING: the intent is recorded, but nothing
	// claims the bytes are there.
	rec, err := ds.Get(ctx, "core.substrate.reamde.dev/blob", digest)
	if err != nil {
		t.Fatalf("the pending manifest is missing: %v", err)
	}
	if got := rec.Properties["status"]; got != string(substrate.BlobPending) {
		t.Fatalf("manifest status is %q, want pending", got)
	}
	// And it does not read as a blob.
	_, _, err = bs.GetBlob(ctx, digest)
	wantErr(t, err, substrate.ErrNotFound, "read of a pending blob")

	// It is collectable, which is what keeps a failed upload from accumulating.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	rec, err = ds.Get(ctx, "core.substrate.reamde.dev/blob", digest)
	if err != nil {
		t.Fatalf("the manifest should be tombstoned, not vanished: %v", err)
	}
	if rec.DeletedAt == nil {
		t.Fatal("the pending manifest survived the sweep")
	}
}

func TestBlobFSGCDeletesTheObject(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	ctx := context.Background()
	_, ds, root := fsBackedDataset(t)
	bs := blobStoreOf(t, ds)
	info, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{}, []byte("unreferenced, and so collectable"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	if _, err := os.Stat(objectPath(root, ds, info.Digest)); err != nil {
		t.Fatalf("the object is not at its key: %v", err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if _, err := os.Stat(objectPath(root, ds, info.Digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the collected blob's bytes are still on disk: %v", err)
	}
	if _, _, err := bs.GetBlob(ctx, info.Digest); err == nil {
		t.Fatal("a collected blob is still readable")
	}
}

// The unreferenced-upload grace covers the window between PUT and the record
// write that references the blob, on this backend too.
func TestBlobFSGraceSparesAFreshUpload(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = time.Hour
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	ctx := context.Background()
	_, ds, root := fsBackedDataset(t)
	bs := blobStoreOf(t, ds)
	info, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{}, []byte("fresh, not yet referenced"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc within the grace: %v", err)
	}
	if _, err := os.Stat(objectPath(root, ds, info.Digest)); err != nil {
		t.Fatalf("the grace must spare a fresh upload: %v", err)
	}
	if _, _, err := bs.GetBlob(ctx, info.Digest); err != nil {
		t.Fatalf("the fresh blob was collected: %v", err)
	}
}

// Switching backends with bytes already stored would 404 half the blobs, and a
// 404 reads like a deletion. The boot refuses instead, in both directions.
func TestBlobBackendSwitchIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	if _, err := blobStoreOf(t, ds).PutBlob(ctx, owner, substrate.BlobUpload{}, []byte("stored in postgres"), ""); err != nil {
		t.Fatalf("put blob: %v", err)
	}

	fs, err := blobbytes.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("open the fs backend: %v", err)
	}
	switched, err := engine.Open(ctx, dsn,
		engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithBlobStore(fs))
	if err == nil {
		_ = switched.Close()
		t.Fatal("a boot pointed at an empty fs store opened over blobs that are in postgres")
	}
	if !strings.Contains(err.Error(), "blobs migrate") {
		t.Fatalf("the refusal must name the way across, got: %v", err)
	}
}

func TestBlobBackendSwitchBackIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	fs, err := blobbytes.NewFS(root)
	if err != nil {
		t.Fatalf("open the fs backend: %v", err)
	}
	svc, dsn := newService(t, engine.WithBlobStore(fs))
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	if _, err := blobStoreOf(t, ds).PutBlob(ctx, owner, substrate.BlobUpload{}, []byte("stored on disk"), ""); err != nil {
		t.Fatalf("put blob: %v", err)
	}

	switched, err := engine.Open(ctx, dsn,
		engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err == nil {
		_ = switched.Close()
		t.Fatal("a boot on the postgres backend opened over blobs whose bytes are on disk")
	}
	if !strings.Contains(err.Error(), "blobs migrate") {
		t.Fatalf("the refusal must name the way across, got: %v", err)
	}
}

// refusingBackend hands out a store that refuses every write, which is the
// crash the engine cannot otherwise be made to have: the bytes never land.
type refusingBackend struct{ blobbytes.Backend }

func (b refusingBackend) Repository(repository string, db blobbytes.DB) (blobbytes.Store, error) {
	s, err := b.Backend.Repository(repository, db)
	if err != nil {
		return nil, err
	}
	return refusingStore{s}, nil
}

type refusingStore struct{ blobbytes.Store }

func (refusingStore) Put(context.Context, string, int64, io.Reader) error {
	return errors.New("the store is not accepting bytes")
}
