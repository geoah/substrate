package engine_test

// The owner's recovery export over the API: the repository's archive as one
// file (decision 0278). A running service streams it, a fresh server restores
// it from an archive it is extracted into, its manifest records the point it
// holds, the point is pinned while writes go on, and the blob bytes come out
// with it.

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/api"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
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

// extractExport unpacks the archive into the archive a server under root
// reads (<root>/archive), the way `tar -x -C <root>/archive` would, holding
// every entry to repositories/ and to the mode the header carries.
func extractExport(t *testing.T, root string, archive []byte) {
	t.Helper()
	root = filepath.Join(root, "archive")
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
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
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
// into the archive of a fresh data root, returning the root, the pinned point
// and the manifest the archive carries.
func exportToRoot(t *testing.T, ds substrate.Dataset) (string, substrate.ExportPoint, changelogfile.Manifest) {
	t.Helper()
	ex, err := ds.Export(context.Background())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var buf bytes.Buffer
	if _, err := ex.WriteTo(&buf); err != nil {
		t.Fatalf("stream the export: %v", err)
	}
	root := t.TempDir()
	extractExport(t, root, buf.Bytes())
	return root, ex.Point(), exportManifest(t, root, ds.Repository().ID)
}

// exportManifest reads the manifest of an export extracted under root.
func exportManifest(t *testing.T, root, id string) changelogfile.Manifest {
	t.Helper()
	m, _, err := changelogfile.ReadArchiveManifestDir(filepath.Join(root, "archive", changelogfile.RepositoriesDir, id))
	if err != nil {
		t.Fatalf("the export carries no manifest: %v", err)
	}
	return m
}

// A repository exported from a running service over its API, its history in
// several pieces, with a sealed secret and a blob, is an archive a fresh
// server restores: the fold, the blob, the secret and the token come back,
// verify passes with the key, and the restored repository writes on past the
// point. The manifest is the last entry, so an export cut short is none.
func TestExportOverTheAPIRestoresIntoAnEmptyDatabase(t *testing.T) {
	t.Parallel()
	// Small pieces, so the export carries several.
	svc, dsn := newService(t, engine.WithArchivePieceBytes(8<<10))
	ctx := context.Background()
	_, token, secret := registerUser(t, svc, "ada.example.com")
	ds, err := svc.Dataset(ctx, "ada.example.com")
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
	if err != nil || params["filename"] != "ada.example.com-"+strconv.FormatInt(head, 10)+".tar" {
		t.Fatalf("Content-Disposition = %q, want ada.example.com-%d.tar", resp.header.Get("Content-Disposition"), head)
	}

	names := tarNames(t, archive)
	last := names[len(names)-1]
	lastKey, ok := changelogfile.ParseArchiveKey(last)
	if _, _, isManifest := changelogfile.ParseArchiveManifestName(lastKey.Name); !ok || !lastKey.Manifests || !isManifest || lastKey.Repository != id {
		t.Fatalf("the last entry is %s, want the manifest", last)
	}
	root := changelogfile.ArchivePrefix(id, lastKey.Archive)
	pieces, blob := 0, false
	for _, name := range names {
		k, ok := changelogfile.ParseArchiveKey(name)
		if !ok || k.Manifests {
			continue
		}
		if k.Archive != lastKey.Archive {
			t.Fatalf("the export holds %s, of another archive than its manifest's", name)
		}
		if _, _, _, _, ok := changelogfile.ParsePieceObjectName(k.Name); ok {
			pieces++
		}
		if d, _, ok := changelogfile.ParseBlobObjectName(k.Name); ok && d == digest {
			blob = true
		}
	}
	if pieces < 2 {
		t.Fatalf("%d pieces: want several\n%s", pieces, strings.Join(names, "\n"))
	}
	if !blob {
		t.Fatalf("the archive lacks the blob %s:\n%s", digest, strings.Join(names, "\n"))
	}
	version := root + "sealed/" + strings.TrimSuffix(changelogfile.SealedFileName(ref), ".json") + "."
	if !slices.ContainsFunc(names, func(name string) bool { return strings.HasPrefix(name, version) }) {
		t.Fatalf("the archive holds no version of the sealed value %s:\n%s", ref, strings.Join(names, "\n"))
	}

	root2 := t.TempDir()
	extractExport(t, root2, archive)
	if m := exportManifest(t, root2, id); m.Head != head || len(m.DEK) == 0 {
		t.Fatalf("the exported manifest: %+v, want head %d and the wrapped DEK", m, head)
	}

	// The restore: an empty database, the same host key.
	dsn2 := engine.MigratedDSN(t)
	svc2 := mustReopen(t, dsn2, root2)
	ds2, err := svc2.Dataset(ctx, "ada.example.com")
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
	// The restore started a new archive; its first pass publishes it.
	if err := engine.ArchivePass(ctx, ds2); err != nil {
		t.Fatalf("the restored repository's first pass: %v", err)
	}
	verified := mustVerify(t, svc2, "ada.example.com")
	if !verified.OK || verified.Head != head || verified.ArchiveHead != head {
		t.Fatalf("the restored repository does not verify: %+v", verified)
	}
	if verified.SealedOpened != verified.SealedRows || verified.SealedOpened == 0 || verified.Blobs != 1 {
		t.Fatalf("verify opened %d of %d sealed rows and hashed %d blobs", verified.SealedOpened, verified.SealedRows, verified.Blobs)
	}
	mustPut(t, ds2, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "after the restore"}})
	if grown := mustVerify(t, svc2, "ada.example.com"); !grown.OK || grown.Head != head+1 {
		t.Fatalf("a write past the restored point must not be a finding: %+v", grown)
	}

	// A read-only process is not the writer and cannot pin a point; the
	// refusal is an unavailability, which the API answers as a 503.
	readOnly := reopenWith(t, dsn, engine.DataRootOf(svc), engine.WithDirectoryReadOnly())
	roDS, err := readOnly.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roDS.Export(ctx); !errors.Is(err, engine.ErrExportNoWriter) || !errors.Is(err, substrate.ErrUnavailable) {
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
// whose history ends at exactly the pinned head. A fresh server restores it
// at that head, and the record written after the pin is not in its fold.
func TestExportPinsAPointWhileWritesContinue(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	ctx := context.Background()
	registerUser(t, svc, "ada.example.com")
	ds, err := svc.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	for i := range 4 {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "before " + strconv.Itoa(i)}})
	}
	head := maxSeq(t, ds)
	id := repositoryIDOf(t, ds)

	ex, err := ds.Export(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if ex.Point().Head != head {
		t.Fatalf("pinned head = %d, want %d", ex.Point().Head, head)
	}
	// A second export while this one holds the slot is refused, whether or
	// not the first has started streaming.
	if _, err := ds.Export(ctx); !errors.Is(err, engine.ErrExportInProgress) || !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a second export must be refused as a conflict, got %v", err)
	}
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
	for i := range 12 {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "during the stream " + strconv.Itoa(i)}})
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
	ex2, err := ds.Export(ctx)
	if err != nil {
		t.Fatalf("an export after the first finished must be admitted: %v", err)
	}
	if _, err := ex2.WriteTo(io.Discard); err != nil {
		t.Fatal(err)
	}

	root2 := t.TempDir()
	extractExport(t, root2, gw.buf.Bytes())
	if m := exportManifest(t, root2, id); m.Head != head {
		t.Fatalf("the archive's head = %d, want the pinned head %d", m.Head, head)
	}
	svc2 := mustReopen(t, engine.MigratedDSN(t), root2)
	verified := mustVerify(t, svc2, "ada.example.com")
	if !verified.OK || verified.Head != head {
		t.Fatalf("the restored repository does not verify at the pinned point: %+v", verified)
	}
	ds2, err := svc2.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds2.Get(ctx, taskKind, between.ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the record written after the pin is in the archive: %v", err)
	}
}

// A blob the export listed at its pin and that blob collection reaches before
// the stream does (a webhook body whose trigger retired, #1022) stays in the
// store until the stream ends: the export completes, the archive carries the
// bytes its restored manifest names, and the restore reads them. Once the
// stream has ended the next collection deletes the bytes.
//
// Not parallel: it sets BlobUploadGrace, so the unreferenced blob is
// collectable at once.
func TestExportHoldsListedBlobsAgainstCollection(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	svc, _ := newService(t)
	ctx := context.Background()
	registerUser(t, svc, "ada.example.com")
	ds, err := svc.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	// Nothing references the blob, so the next collection takes it.
	digest := putBlob(t, ds, []byte("a webhook body nobody references"))
	head := maxSeq(t, ds)
	id := repositoryIDOf(t, ds)
	onDisk := filepath.Join(changelogfile.BlobsDir(repoDirOf(t, svc, ds)), digest)

	ex, err := ds.Export(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if ex.Point().Blobs != 1 {
		t.Fatalf("the pin listed %d blobs, want the one", ex.Point().Blobs)
	}
	// Between the pin and the stream, collection tombstones the manifest.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("run gc: %v", err)
	}
	if m, err := ds.Get(ctx, "substrate.reamde.dev/core/blob", digest); err != nil || m.DeletedAt == nil {
		t.Fatalf("collection did not tombstone the listed blob's manifest (%v); the test proves nothing", err)
	}
	if _, err := os.Stat(onDisk); err != nil {
		t.Fatalf("collection deleted the bytes a streaming export listed: %v", err)
	}

	var buf bytes.Buffer
	if _, err := ex.WriteTo(&buf); err != nil {
		t.Fatalf("stream the export: %v", err)
	}
	root := t.TempDir()
	extractExport(t, root, buf.Bytes())
	if m := exportManifest(t, root, id); m.Head != head {
		t.Fatalf("the archive's head = %d, want %d", m.Head, head)
	}
	repoDir := filepath.Join(root, "archive", changelogfile.RepositoriesDir, id)
	m := exportManifest(t, root, id)
	blobs, err := filepath.Glob(filepath.Join(repoDir, m.Archive, changelogfile.BlobsSubdir, digest+".*"))
	if err != nil || len(blobs) != 1 {
		t.Fatalf("the archive lacks the listed blob's bytes: %v (%v)", blobs, err)
	}

	svc2 := mustReopen(t, engine.MigratedDSN(t), root)
	ds2, err := svc2.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	if got := getBlob(t, ds2, digest); string(got) != "a webhook body nobody references" {
		t.Fatalf("restored blob bytes = %q", got)
	}
	if verified := mustVerify(t, svc2, "ada.example.com"); !verified.OK || verified.Head != head || verified.Blobs != 1 {
		t.Fatalf("the restored repository does not verify: %+v", verified)
	}

	// The stream has ended, so the hold is gone and the orphan sweep deletes
	// the bytes no live manifest names.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("run gc after the stream: %v", err)
	}
	if _, err := os.Stat(onDisk); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the bytes outlived the export's hold: %v", err)
	}
}
