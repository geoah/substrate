package engine_test

// The engine against a blob store Postgres cannot join: the bytes and the
// manifest cannot commit together, so what these assert is the ORDER, that no
// crash between them leaves a reader looking at a `stored` manifest whose
// bytes are missing, and that whatever a crash does leave is collectable.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/api"
	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// blobDigestOf is the digest the engine derives, spelled out here so a test
// can name bytes it has not uploaded.
func blobDigestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return substrate.BlobDigestPrefix + hex.EncodeToString(sum[:])
}

// fsBackedDataset is a repository whose blob bytes live in its directory under
// the data root, which is what an engine opened with no WithBlobStore does.
func fsBackedDataset(t *testing.T) (substrate.Service, substrate.Dataset, string) {
	t.Helper()
	root := t.TempDir()
	svc, ds := newDataset(t, engine.WithDataRoot(root))
	return svc, ds, root
}

// objectPath is where the fs backend keeps one repository's blob:
// <root>/repositories/<authority>/blobs/<digest>.
func objectPath(root string, ds substrate.Dataset, digest string) string {
	return filepath.Join(root, "repositories", ds.Repository().ID, "blobs", digest)
}

func TestBlobFSRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, root := fsBackedDataset(t)

	data := []byte("an attachment that never enters WAL")
	info, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{
		Name: "notes.txt", MediaType: "text/plain",
	}, data, "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	// The bytes are on disk under <root>/repositories/<authority>/blobs/<digest> and
	// nowhere in the database.
	onDisk, err := os.ReadFile(objectPath(root, ds, info.Digest))
	if err != nil {
		t.Fatalf("the object is not at its key: %v", err)
	}
	if string(onDisk) != string(data) {
		t.Fatalf("the object holds %q, uploaded %q", onDisk, data)
	}

	got, read, err := ds.GetBlob(ctx, info.Digest)
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
	// writer's: the digest is the identity, and a name is descriptive.
	again, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{Name: "other.txt"}, data, "")
	if err != nil {
		t.Fatalf("re-put: %v", err)
	}
	if again.Name != "notes.txt" {
		t.Fatalf("a second upload renamed the blob to %q", again.Name)
	}
}

// One byte flipped in the store, the length unchanged, is a blob only the hash
// can tell from its digest's. Every read that serves the bytes refuses it,
// naming the digest: the engine's read, the API's `GET /blobs/{digest}` (a
// 500 whose body names the digest, and no byte of the blob) and the export,
// which stops before the blob's last byte. `repository verify` reports it.
func TestBlobFSReadRefusesAFlippedByte(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	_, _, secret := registerUser(t, svc, "ada.example.com")
	ds, err := svc.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	root := engine.DataRootOf(svc)
	srv := httptest.NewServer(api.New(api.Config{Service: svc}))
	defer srv.Close()

	data := []byte("bytes a disk fault flips one bit of")
	info, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, data, "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	// Before the damage the read keeps its size and its bytes.
	status, body := getBlobOverAPI(t, srv.URL, secret, info.Digest)
	if status != http.StatusOK || !bytes.Equal(body, data) {
		t.Fatalf("the intact blob read %d %q, want 200 %q", status, body, data)
	}
	got, read, err := ds.GetBlob(ctx, info.Digest)
	if err != nil || !bytes.Equal(read, data) || got.Size != int64(len(data)) {
		t.Fatalf("the intact blob read (%v, %q, %v), want %d bytes %q", got, read, err, len(data), data)
	}

	flipped := bytes.Clone(data)
	flipped[len(flipped)/2] ^= 0x01
	if err := os.WriteFile(objectPath(root, ds, info.Digest), flipped, 0o600); err != nil {
		t.Fatal(err)
	}

	_, read, err = ds.GetBlob(ctx, info.Digest)
	wantErr(t, err, substrate.ErrCorrupt, "read of a flipped byte")
	if !strings.Contains(err.Error(), info.Digest) || !strings.Contains(err.Error(), "hashes to "+blobDigestOf(flipped)) {
		t.Fatalf("the refusal does not name the digest and what the bytes hash to: %v", err)
	}
	if read != nil {
		t.Fatalf("a refused read handed out %q", read)
	}

	status, body = getBlobOverAPI(t, srv.URL, secret, info.Digest)
	if status != http.StatusInternalServerError {
		t.Fatalf("GET of the flipped blob answered %d, want 500: %s", status, body)
	}
	var env substrate.ErrorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("the 500 is not a problem body: %v: %s", err, body)
	}
	if env.Error.Code != "internal" || !strings.Contains(env.Error.Message, info.Digest) {
		t.Fatalf("the problem body = %+v, want code internal naming %s", env.Error, info.Digest)
	}
	if bytes.Contains(body, flipped) {
		t.Fatalf("the 500 carried the damaged bytes: %s", body)
	}

	ex, err := ds.Export(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var archive bytes.Buffer
	_, err = ex.WriteTo(&archive)
	if !errors.Is(err, blobbytes.ErrDigestMismatch) || !strings.Contains(err.Error(), info.Digest) {
		t.Fatalf("the export of a flipped blob ended with %v, want a digest mismatch naming %s", err, info.Digest)
	}
	if bytes.Contains(archive.Bytes(), flipped) {
		t.Fatal("the export wrote the damaged blob whole before it failed")
	}

	report := mustVerify(t, svc, ds.Repository().ID)
	if report.OK || !findingContaining(report, "blob "+info.Digest+": the stored bytes hash to "+blobDigestOf(flipped)) {
		t.Fatalf("verify did not report the flipped blob: %+v", report)
	}
}

// getBlobOverAPI is one GET /api/v1/blobs/{digest}, read whole.
func getBlobOverAPI(t *testing.T, serverURL, secret, digest string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, serverURL+"/api/v1/blobs/"+digest, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET the blob: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the blob response: %v", err)
	}
	return resp.StatusCode, body
}

func TestBlobFSIsRepositoryScoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, root := fsBackedDataset(t)
	data := []byte("one repository's archive")
	info, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{}, data, "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	if _, err := svc.CreateRepository(ctx, "otheruser.example.com"); err != nil {
		t.Fatalf("create repository B: %v", err)
	}
	dsB, err := svc.Dataset(ctx, "otheruser.example.com")
	if err != nil {
		t.Fatalf("open repository B: %v", err)
	}
	// The digest is the same string in both repositories, and the second one
	// still cannot read the first one's bytes: the repository is half the key,
	// and it comes from the authenticated dataset rather than the request.
	_, _, err = dsB.GetBlob(ctx, info.Digest)
	wantErr(t, err, substrate.ErrNotFound, "cross-repository blob read")

	if _, err := dsB.PutBlob(ctx, owner, substrate.BlobUpload{}, data, ""); err != nil {
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

	// The bytes, written as the store would write them, with no manifest
	// behind them — the state a crash after step 2 leaves.
	backend, err := blobbytes.NewFS(root)
	if err != nil {
		t.Fatalf("open the fs backend: %v", err)
	}
	store, err := backend.Repository(ds.Repository().ID)
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
	_, _, err = ds.GetBlob(ctx, digest)
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

	data := []byte("bytes the store refused")
	digest := blobDigestOf(data)
	if _, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{}, data, ""); err == nil {
		t.Fatal("a store that refuses the bytes must fail the upload")
	}

	// The manifest exists and is PENDING: the intent is recorded, but nothing
	// claims the bytes are there.
	rec, err := ds.Get(ctx, "substrate.reamde.dev/core/blob", digest)
	if err != nil {
		t.Fatalf("the pending manifest is missing: %v", err)
	}
	if got := rec.Properties["status"]; got != string(substrate.BlobPending) {
		t.Fatalf("manifest status is %q, want pending", got)
	}
	// And it does not read as a blob.
	_, _, err = ds.GetBlob(ctx, digest)
	wantErr(t, err, substrate.ErrNotFound, "read of a pending blob")

	// It is collectable, which is what keeps a failed upload from accumulating.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	rec, err = ds.Get(ctx, "substrate.reamde.dev/core/blob", digest)
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
	info, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{}, []byte("unreferenced, and so collectable"), "")
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
	if _, _, err := ds.GetBlob(ctx, info.Digest); err == nil {
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
	info, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{}, []byte("fresh, not yet referenced"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc within the grace: %v", err)
	}
	if _, err := os.Stat(objectPath(root, ds, info.Digest)); err != nil {
		t.Fatalf("the grace must spare a fresh upload: %v", err)
	}
	if _, _, err := ds.GetBlob(ctx, info.Digest); err != nil {
		t.Fatalf("the fresh blob was collected: %v", err)
	}
}

// The bytes live in the repository directory, so a service reopened on the
// same data root reads what the last one stored, with no option beyond the
// root itself.
func TestBlobFSReopenOnTheSameRootReadsTheBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	svc, dsn := newService(t, engine.WithDataRoot(root))
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	data := []byte("stored by one process, read by the next")
	info, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{}, data, "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	if _, err := os.Stat(objectPath(root, ds, info.Digest)); err != nil {
		t.Fatalf("the object is not in the repository directory: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	again, err := engine.OpenForTest(t, ctx, dsn,
		engine.WithDataRoot(t.TempDir()),
		engine.WithDataRoot(root),
		engine.WithKindsDir(engine.SeedKindsDir),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err != nil {
		t.Fatalf("reopen on the same root: %v", err)
	}
	t.Cleanup(func() { _ = again.Close() })
	ds2, err := again.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset again: %v", err)
	}
	_, read, err := ds2.GetBlob(ctx, info.Digest)
	if err != nil {
		t.Fatalf("get blob after the reopen: %v", err)
	}
	if string(read) != string(data) {
		t.Fatalf("read back %q, stored %q", read, data)
	}
}

// refusingBackend hands out a store that refuses every write, which is the
// crash the engine cannot otherwise be made to have: the bytes never land.
type refusingBackend struct{ blobbytes.Backend }

func (b refusingBackend) Repository(repository string) (blobbytes.Store, error) {
	s, err := b.Backend.Repository(repository)
	if err != nil {
		return nil, err
	}
	return refusingStore{s}, nil
}

type refusingStore struct{ blobbytes.Store }

func (refusingStore) Put(context.Context, string, int64, io.Reader) error {
	return errors.New("the store is not accepting bytes")
}
