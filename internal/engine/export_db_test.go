package engine_test

// The owner's recovery export over the API: the archive a running service
// streams is a data root a fresh server imports, it records the point it
// holds, the point is pinned while writes go on, and the blob bytes come out
// of whichever store the server runs.

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/geoah/substrate/internal/api"
	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// exportResponse is one GET /api/v1/export, read whole.
type exportResponse struct {
	status int
	header http.Header
	body   []byte
}

// downloadExport GETs the export with the bearer token.
func downloadExport(t *testing.T, serverURL, secret string) exportResponse {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, serverURL+"/api/v1/export", nil)
	if err != nil {
		t.Fatal(err)
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/export: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the export: %v", err)
	}
	return exportResponse{status: resp.StatusCode, header: resp.Header, body: body}
}

// tarNames lists the archive's entries in order.
func tarNames(t *testing.T, archive []byte) []string {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(archive))
	var names []string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}
		names = append(names, h.Name)
	}
}

// extractExport unpacks the archive under root the way `tar -x -C root`
// would, holding every entry to repositories/ and to the mode the header
// carries.
func extractExport(t *testing.T, root string, archive []byte) {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(archive))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}
		if !filepath.IsLocal(h.Name) || !strings.HasPrefix(h.Name, changelogfile.RepositoriesDir+"/") {
			t.Fatalf("the archive names %s outside repositories/", h.Name)
		}
		target := filepath.Join(root, filepath.FromSlash(h.Name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(h.Mode)); err != nil {
				t.Fatal(err)
			}
		case tar.TypeReg:
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, data, os.FileMode(h.Mode)); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("the archive carries %s as type %v", h.Name, h.Typeflag)
		}
	}
}

// exportToRoot pins and streams an export through the seam and extracts it
// under a fresh data root, returning the root, the pinned point and the
// snapshot the archive recorded.
func exportToRoot(t *testing.T, ds substrate.Dataset) (string, substrate.ExportPoint, changelogfile.Snapshot) {
	t.Helper()
	ex, err := ds.(substrate.Exporter).Export(context.Background())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var buf bytes.Buffer
	if _, err := ex.WriteTo(&buf); err != nil {
		t.Fatalf("stream the export: %v", err)
	}
	root := t.TempDir()
	extractExport(t, root, buf.Bytes())
	dir, err := changelogfile.RepoDir(root, ds.Repository().ID)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := changelogfile.ReadSnapshot(dir)
	if err != nil {
		t.Fatalf("the archive carries no readable %s: %v", changelogfile.SnapshotName, err)
	}
	return root, ex.Point(), snap
}

// A repository exported from a running service over its API, with several
// finished segments, an active one, a sealed secret and a blob, is a data root
// a fresh server imports: the fold, the blob, the secret and the token come
// back, verify passes with the key and reports the recorded point, and the
// restored repository writes on past it without a finding. The archive holds
// the manifest with its host-wrapped DEK (the same-key restore needs nothing
// else) and nothing the fold does not need.
func TestExportOverTheAPIRestoresIntoAnEmptyDatabase(t *testing.T) {
	t.Parallel()
	// Small segments, so the archive carries finished segments with their
	// sidecars and an active one cut at the point.
	svc, dsn := newService(t, engine.WithChangelogSegmentBytes(8<<10))
	ctx := context.Background()
	_, token, secret := registerUser(t, svc, "ada")
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	for i := range 12 {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "task " + strconv.Itoa(i)}})
	}
	ref := putProvider(t, ds, dsn, "openai", "sk-export")
	digest := putBlob(t, ds, []byte("export bytes"))
	before := foldOf(t, ds)
	head := maxSeq(t, ds)
	secretBefore := openSecret(t, dsn, ref)
	id := repositoryIDOf(t, ds)

	srv := httptest.NewServer(api.New(api.Config{Service: svc}))
	defer srv.Close()
	if resp := downloadExport(t, srv.URL, ""); resp.status != http.StatusUnauthorized {
		t.Fatalf("an export with no token answered %d", resp.status)
	}
	resp := downloadExport(t, srv.URL, secret)
	archive := resp.body
	if resp.status != http.StatusOK || resp.header.Get("Content-Type") != "application/x-tar" {
		t.Fatalf("status %d, Content-Type %q, body %s", resp.status, resp.header.Get("Content-Type"), archive[:min(len(archive), 200)])
	}
	_, params, err := mime.ParseMediaType(resp.header.Get("Content-Disposition"))
	if err != nil || params["filename"] != id+"-"+strconv.FormatInt(head, 10)+".tar" {
		t.Fatalf("Content-Disposition = %q, want %s-%d.tar", resp.header.Get("Content-Disposition"), id, head)
	}

	names := tarNames(t, archive)
	root := changelogfile.RepositoriesDir + "/" + id + "/"
	var segments, sidecars int
	seen := map[string]bool{}
	for _, name := range names {
		seen[name] = true
		switch {
		case strings.HasSuffix(name, ".ndjson"):
			segments++
		case strings.HasSuffix(name, ".sha256"):
			sidecars++
		case strings.Contains(name, changelogfile.LockFileName) || strings.HasSuffix(name, ".pending"):
			t.Fatalf("the archive carries %s, which is not a file of the repository", name)
		}
	}
	if segments < 2 || sidecars != segments-1 {
		t.Fatalf("%d segments and %d sidecars: want several finished segments and one active one\n%s", segments, sidecars, strings.Join(names, "\n"))
	}
	for _, want := range []string{root + changelogfile.ManifestName, root + "blobs/" + digest, root + "sealed/" + changelogfile.SealedFileName(ref)} {
		if !seen[want] {
			t.Fatalf("the archive lacks %s:\n%s", want, strings.Join(names, "\n"))
		}
	}
	if last := names[len(names)-1]; last != root+changelogfile.SnapshotName {
		t.Fatalf("the last entry is %s, want %s", last, changelogfile.SnapshotName)
	}

	root2 := t.TempDir()
	extractExport(t, root2, archive)
	dir2, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := changelogfile.ReadSnapshot(dir2)
	if err != nil {
		t.Fatalf("the archive carries no readable %s: %v", changelogfile.SnapshotName, err)
	}
	if snap.Head != head || snap.BlobStore != "fs" || snap.BlobLocation != "" || len(snap.Blobs) != 1 || snap.Blobs[0] != digest {
		t.Fatalf("snapshot = %+v, want head %d with the one blob %s under fs", snap, head, digest)
	}
	m, err := changelogfile.ReadManifest(dir2)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.DEK) == 0 || !m.SealedDEKOnly {
		t.Fatalf("the exported manifest lost its wrapped DEK or its marker: %+v", m)
	}
	if info, err := os.Stat(dir2); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("the extracted directory is %v, want 0700", info.Mode())
	}

	// The restore: an empty database, the same host key.
	dsn2 := testdb.NewSchema(t)
	svc2 := mustReopen(t, dsn2, root2)
	ds2, err := svc2.Dataset(ctx, "ada")
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the restored fold is not the original\n%s", firstDifference(before, after))
	}
	if got := getBlob(t, ds2, digest); string(got) != "export bytes" {
		t.Fatalf("blob bytes = %q", got)
	}
	if got := openSecret(t, dsn2, ref); got != secretBefore {
		t.Fatalf("secret = %q, want %q", got, secretBefore)
	}
	if _, info, err := svc2.Authenticate(ctx, secret); err != nil || info.ID != token.ID {
		t.Fatalf("the token does not open the restored repository: %v (%+v)", err, info)
	}
	verified := mustVerify(t, svc2, "ada")
	if !verified.OK || verified.Head != head || verified.FileHead != head {
		t.Fatalf("the restored repository does not verify: %+v", verified)
	}
	if verified.Snapshot == nil || verified.Snapshot.Head != head || verified.Snapshot.HeadHash != verified.HeadHash {
		t.Fatalf("verify did not report the recorded point: %+v (head checksum %s)", verified.Snapshot, verified.HeadHash)
	}
	if verified.SealedOpened != verified.SealedFiles || verified.SealedOpened == 0 || verified.Blobs != 1 {
		t.Fatalf("verify opened %d of %d sealed files and hashed %d blobs", verified.SealedOpened, verified.SealedFiles, verified.Blobs)
	}
	mustPut(t, ds2, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "after the restore"}})
	if grown := mustVerify(t, svc2, "ada"); !grown.OK || grown.Head != head+1 || grown.Snapshot == nil || grown.Snapshot.Head != head {
		t.Fatalf("a write past the recorded point must not be a finding: %+v", grown)
	}

	// A read-only process is not the writer and cannot pin a point; the
	// refusal is an unavailability, which the API answers as a 503.
	readOnly := reopenWith(t, dsn, engine.DataRootOf(svc), engine.WithDirectoryReadOnly())
	roDS, err := readOnly.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roDS.(substrate.Exporter).Export(ctx); !errors.Is(err, engine.ErrExportNoWriter) || !errors.Is(err, substrate.ErrUnavailable) {
		t.Fatalf("a read-only export must be refused as unavailable, got %v", err)
	}
}

// gatedWriter blocks the stream at its first byte until the test opens the
// gate, and says when it is blocked, so the test knows WriteTo is running.
type gatedWriter struct {
	buf     bytes.Buffer
	once    sync.Once
	blocked chan struct{}
	gate    chan struct{}
}

func (g *gatedWriter) Write(p []byte) (int, error) {
	g.once.Do(func() {
		close(g.blocked)
		<-g.gate
	})
	return g.buf.Write(p)
}

// The export pins its point and streams afterwards: writes committed between
// Export and WriteTo, and writes committed while WriteTo is blocked on its
// first byte (so the stream is provably in progress), are not in the archive,
// and the archive's changelog ends at exactly the pinned head, with the
// active segment cut there even though the live one has rolled since. A
// fresh server imports it and verifies at that head, and the record written
// after the pin is not in the restored fold.
func TestExportPinsAPointWhileWritesContinue(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, engine.WithChangelogSegmentBytes(4<<10))
	ctx := context.Background()
	registerUser(t, svc, "ada")
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	for i := range 4 {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "before " + strconv.Itoa(i)}})
	}
	head := maxSeq(t, ds)
	id := repositoryIDOf(t, ds)
	dir := repoDirOf(t, svc, ds)
	segmentsAtPin, err := changelogfile.Segments(changelogfile.ChangelogDir(dir))
	if err != nil {
		t.Fatal(err)
	}

	ex, err := ds.(substrate.Exporter).Export(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if ex.Point().Head != head {
		t.Fatalf("pinned head = %d, want %d", ex.Point().Head, head)
	}
	// A second export while this one holds the slot is refused, whether or
	// not the first has started streaming.
	if _, err := ds.(substrate.Exporter).Export(ctx); !errors.Is(err, engine.ErrExportInProgress) || !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a second export must be refused as a conflict, got %v", err)
	}
	// Between the pin and the stream.
	between := mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "between pin and stream"}})

	gw := &gatedWriter{blocked: make(chan struct{}), gate: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := ex.WriteTo(gw)
		done <- err
	}()
	select {
	case <-gw.blocked:
	case <-time.After(30 * time.Second):
		t.Fatal("WriteTo never wrote its first byte")
	}
	// During the stream: enough writes to roll the 4 KiB active segment, so
	// the live directory no longer looks like the pinned one.
	for i := range 12 {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "during the stream " + strconv.Itoa(i)}})
	}
	segmentsNow, err := changelogfile.Segments(changelogfile.ChangelogDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(segmentsNow) <= len(segmentsAtPin) {
		t.Fatalf("the writes during the stream did not roll a segment (%d before, %d now); the test proves less than it should", len(segmentsAtPin), len(segmentsNow))
	}
	close(gw.gate)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stream the export: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("WriteTo did not finish")
	}
	// The slot is free again once the stream has ended.
	ex2, err := ds.(substrate.Exporter).Export(ctx)
	if err != nil {
		t.Fatalf("an export after the first finished must be admitted: %v", err)
	}
	if _, err := ex2.WriteTo(io.Discard); err != nil {
		t.Fatal(err)
	}

	root2 := t.TempDir()
	extractExport(t, root2, gw.buf.Bytes())
	dir2, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := changelogfile.ReadSnapshot(dir2)
	if err != nil {
		t.Fatal(err)
	}
	files, err := changelogfile.Verify(changelogfile.ChangelogDir(dir2))
	if err != nil || snap.Head != head || files.Head != head || files.TruncatedBytes != 0 {
		t.Fatalf("the archive ends at %d with %d uncommitted bytes (%v) and records %d, want the pinned head %d", files.Head, files.TruncatedBytes, err, snap.Head, head)
	}
	if files.Segments != len(segmentsAtPin) {
		t.Fatalf("the archive holds %d segments, the directory held %d at the pin", files.Segments, len(segmentsAtPin))
	}

	svc2 := mustReopen(t, testdb.NewSchema(t), root2)
	verified := mustVerify(t, svc2, "ada")
	if !verified.OK || verified.Head != head || verified.Snapshot == nil || verified.Snapshot.Head != head {
		t.Fatalf("the restored repository does not verify at the pinned point: %+v", verified)
	}
	ds2, err := svc2.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds2.Get(ctx, taskKind, between.ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the record written after the pin is in the archive: %v", err)
	}
}

// The pin takes its pool connection before the writer mutex. Every write
// holds a connection from its start to the commit that runs under the mutex,
// and the writes behind it wait on the changelog advisory lock inside their
// own transactions, connections held. Here one write is held at its commit
// under the mutex, seven more hold the pool's other connections behind the
// advisory lock and a ninth waits on the pool itself; an export started then
// completes once the held write is released, and every write lands. An export
// that took the mutex first and asked the pool under it would hand the held
// write's connection to the ninth writer and wait for one no writer could
// release.
func TestExportPinsWithEveryPoolConnectionHeld(t *testing.T) {
	t.Parallel()
	// The gate: once armed, the first write to reach its commit blocks there
	// under writerMu until released; every other call passes.
	var armed, gated atomic.Bool
	atCommit := make(chan struct{})
	release := make(chan struct{})
	_, ds, _ := newDatasetWithDSN(t, engine.WithTestCommitFault(func(stage string) error {
		if stage == engine.CommitAfterPrepare && armed.Load() && gated.CompareAndSwap(false, true) {
			close(atCommit)
			<-release
		}
		return nil
	}))
	ctx := context.Background()
	head := maxSeq(t, ds)
	stats := engine.PoolStats(ds)
	const writers = 9
	if stats.MaxOpenConnections >= writers {
		t.Fatalf("the pool allows %d connections; the test needs fewer than %d writers to fill it", stats.MaxOpenConnections, writers)
	}

	armed.Store(true)
	writeErrs := make(chan error, writers)
	for range writers {
		go func() {
			_, err := ds.Put(ctx, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "queued behind the export"}})
			writeErrs <- err
		}()
	}
	select {
	case <-atCommit:
	case <-time.After(30 * time.Second):
		t.Fatal("no write reached its commit")
	}
	deadline := time.Now().Add(30 * time.Second)
	for s := engine.PoolStats(ds); s.InUse < s.MaxOpenConnections || s.WaitCount == 0; s = engine.PoolStats(ds) {
		if time.Now().After(deadline) {
			t.Fatalf("the pool never filled with a writer waiting on it: %+v", s)
		}
		time.Sleep(10 * time.Millisecond)
	}

	type pinned struct {
		ex  substrate.Export
		err error
	}
	result := make(chan pinned, 1)
	go func() {
		ex, err := ds.(substrate.Exporter).Export(ctx)
		result <- pinned{ex, err}
	}()
	// The export is queued on the pool behind the ninth writer (or, with the
	// old order, on the mutex); the held write is released only now, so the
	// export never had a connection to itself.
	time.Sleep(200 * time.Millisecond)
	close(release)
	select {
	case r := <-result:
		if r.err != nil {
			t.Fatalf("export: %v", r.err)
		}
		if p := r.ex.Point(); p.Head < head+1 {
			t.Fatalf("pinned head = %d, want at least the released write past %d", p.Head, head)
		}
		if _, err := r.ex.WriteTo(io.Discard); err != nil {
			t.Fatal(err)
		}
	case <-time.After(60 * time.Second):
		t.Fatalf("the export did not pin its point with the pool full: %+v", engine.PoolStats(ds))
	}
	for range writers {
		select {
		case err := <-writeErrs:
			if err != nil {
				t.Fatalf("a write queued behind the export failed: %v", err)
			}
		case <-time.After(60 * time.Second):
			t.Fatal("a write queued behind the export never finished")
		}
	}
	if got := maxSeq(t, ds); got != head+writers {
		t.Fatalf("head = %d after %d writes from %d", got, writers, head)
	}
}

// The MinIO container the s3 export test runs against, started once per
// test binary as internal/blobbytes starts its own. Throwaway root
// credentials, reachable only from this test's docker network.
const (
	minioImage  = "minio/minio:RELEASE.2025-09-07T16-13-09Z"
	minioUser   = "substratetest"
	minioSecret = "substratetestsecret"
	minioBucket = "export-test"
)

var (
	minioOnce     sync.Once
	minioEndpoint string
	minioErr      error
)

func minioURL(t *testing.T) string {
	t.Helper()
	minioOnce.Do(func() {
		ctx := context.Background()
		c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			Started: true,
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        minioImage,
				ExposedPorts: []string{"9000/tcp"},
				Cmd:          []string{"server", "/data"},
				Env: map[string]string{
					"MINIO_ROOT_USER":     minioUser,
					"MINIO_ROOT_PASSWORD": minioSecret,
				},
				// `/cluster` is the one health endpoint that waits for the
				// object layer (internal/blobbytes/s3_db_test.go, #353).
				WaitingFor: wait.ForHTTP("/minio/health/cluster").
					WithPort("9000/tcp").WithStartupTimeout(120 * time.Second),
			},
		})
		if err != nil {
			minioErr = err
			return
		}
		host, err := c.Host(ctx)
		if err != nil {
			minioErr = err
			return
		}
		port, err := c.MappedPort(ctx, "9000/tcp")
		if err != nil {
			minioErr = err
			return
		}
		minioEndpoint = fmt.Sprintf("http://%s:%s", host, port.Port())
		// The bucket, made with the image's own client: a substrate is
		// pointed at a bucket an operator already made, so the backend has
		// no door for this and the test does what the operator would.
		for _, cmd := range [][]string{
			{"mc", "alias", "set", "local", "http://127.0.0.1:9000", minioUser, minioSecret},
			{"mc", "mb", "--ignore-existing", "local/" + minioBucket},
		} {
			code, out, err := c.Exec(ctx, cmd)
			if err == nil && code != 0 {
				raw, _ := io.ReadAll(out)
				err = fmt.Errorf("%s exited %d: %s", strings.Join(cmd, " "), code, raw)
			}
			if err != nil {
				minioErr = err
				return
			}
		}
	})
	if minioErr != nil {
		t.Fatalf("start the minio container: %v", minioErr)
	}
	return minioEndpoint
}

// A repository whose blob bytes live in a bucket exports them into the
// archive under blobs/, hashed on the way, and the archive records fs: a
// host running the fs store restores it with nothing else, and verify hashes
// the blob out of the restored directory.
func TestExportStreamsBlobsOutOfS3(t *testing.T) {
	t.Parallel()
	s3, err := blobbytes.NewS3(blobbytes.S3Config{
		Endpoint: minioURL(t), Bucket: minioBucket, Region: "us-east-1",
		AccessKeyID: minioUser, SecretAccessKey: minioSecret, PathStyle: true,
	})
	if err != nil {
		t.Fatalf("open the s3 backend: %v", err)
	}
	svc, ds := newDataset(t, engine.WithBlobStore(s3))
	ctx := context.Background()
	payload := bytes.Repeat([]byte("bytes in the bucket "), 4096)
	digest := putBlob(t, ds, payload)
	id := repositoryIDOf(t, ds)
	if _, err := os.Stat(filepath.Join(changelogfile.BlobsDir(repoDirOf(t, svc, ds)), digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the bytes landed in the directory, not the bucket: %v", err)
	}
	before := foldOf(t, ds)
	head := maxSeq(t, ds)

	root2, point, snap := exportToRoot(t, ds)
	if point.Blobs != 1 || snap.BlobStore != "fs" || snap.BlobLocation != "" || len(snap.Blobs) != 1 || snap.Blobs[0] != digest {
		t.Fatalf("point %+v, snapshot %+v: want the one blob carried under fs", point, snap)
	}
	dir2, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(changelogfile.BlobsDir(dir2), digest))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("the archive's blob is not the bucket's bytes (%d bytes, %v)", len(got), err)
	}

	// An fs host imports the archive as it is.
	svc2 := mustReopen(t, testdb.NewSchema(t), root2)
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the restored fold is not the original\n%s", firstDifference(before, after))
	}
	if got := getBlob(t, ds2, digest); !bytes.Equal(got, payload) {
		t.Fatalf("blob bytes = %d bytes, want %d", len(got), len(payload))
	}
	verified := mustVerify(t, svc2, "geoah")
	if !verified.OK || verified.Head != head || verified.Blobs != 1 || verified.BlobBytes != int64(len(payload)) {
		t.Fatalf("the restored repository does not verify: %+v", verified)
	}
}
