package engine_test

// The owner's recovery export over the API: the archive a running service
// streams is a data root a fresh server imports, it records the point it
// holds, and the point is pinned while writes go on.

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
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/api"
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
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "task " + string(rune('a'+i))}})
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

	// A read-only process is not the writer and cannot pin a point.
	readOnly := reopenWith(t, dsn, engine.DataRootOf(svc), engine.WithDirectoryReadOnly())
	roDS, err := readOnly.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roDS.(substrate.Exporter).Export(ctx); !errors.Is(err, engine.ErrExportNoWriter) {
		t.Fatalf("a read-only export must be refused as such, got %v", err)
	}
}

// The export pins its point under the writer mutex and streams afterwards, so
// a repository written to throughout the download still exports one committed
// state: the archive's head is between the heads before and after, and a
// fresh server imports it and verifies at exactly that head.
func TestExportPinsAPointWhileWritesContinue(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, engine.WithChangelogSegmentBytes(4<<10))
	ctx := context.Background()
	_, _, secret := registerUser(t, svc, "ada")
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	for range 4 {
		mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "before"}})
	}
	id := repositoryIDOf(t, ds)
	srv := httptest.NewServer(api.New(api.Config{Service: svc}))
	defer srv.Close()

	before := maxSeq(t, ds)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var writeErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := ds.Put(ctx, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "during"}}); err != nil {
				writeErr = err
				return
			}
		}
	}()
	resp := downloadExport(t, srv.URL, secret)
	archive := resp.body
	close(stop)
	wg.Wait()
	if writeErr != nil {
		t.Fatalf("a write during the export failed: %v", writeErr)
	}
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, archive[:min(len(archive), 200)])
	}
	after := maxSeq(t, ds)

	root2 := t.TempDir()
	extractExport(t, root2, archive)
	dir2, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := changelogfile.ReadSnapshot(dir2)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Head < before || snap.Head > after {
		t.Fatalf("the archive's head %d is outside [%d, %d]", snap.Head, before, after)
	}
	// The files alone end at the recorded point: nothing past it leaked into
	// the cut active segment.
	files, err := changelogfile.Verify(changelogfile.ChangelogDir(dir2))
	if err != nil || files.Head != snap.Head || files.TruncatedBytes != 0 {
		t.Fatalf("the archive's changelog ends at %d with %d uncommitted bytes (%v), the snapshot says %d", files.Head, files.TruncatedBytes, err, snap.Head)
	}

	svc2 := mustReopen(t, testdb.NewSchema(t), root2)
	verified := mustVerify(t, svc2, "ada")
	if !verified.OK || verified.Head != snap.Head || verified.Snapshot == nil || verified.Snapshot.Head != snap.Head {
		t.Fatalf("the restored repository does not verify at the recorded point: %+v", verified)
	}
}
