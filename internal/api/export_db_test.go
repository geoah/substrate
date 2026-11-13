package api

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
)

// The archive comes back as a tar under the authority's file name, with the
// point in the name: the repository's archive, named by its id, its manifest,
// recording that point, as the last entry.
func TestExportStreamsTheArchiveUnderTheBearerToken(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	env.put(t, personKind, "p1", map[string]any{"name": "Ada"})
	head := changesHead(t, env).Seq

	rec := env.do(t, http.MethodGet, "/api/v1/export", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-tar" {
		t.Fatalf("Content-Type = %q, want application/x-tar", ct)
	}
	if cd, want := rec.Header().Get("Content-Disposition"), fmt.Sprintf("attachment; filename=%s-%d.tar", env.repository, head); cd != want {
		t.Fatalf("Content-Disposition = %q, want %q", cd, want)
	}

	dir := changelogfile.RepositoriesDir + "/" + env.ds.Repository().ID + "/"
	tr := tar.NewReader(rec.Body)
	var names []string
	var manifest []byte
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}
		if h.Name != changelogfile.RepositoriesDir+"/" && !strings.HasPrefix(h.Name, dir) {
			t.Fatalf("entry %s lies outside the repository's directory %s", h.Name, dir)
		}
		names = append(names, h.Name)
		if k, ok := changelogfile.ParseArchiveKey(h.Name); ok && k.Manifests && k.Name != changelogfile.DeletedMark {
			if manifest, err = io.ReadAll(tr); err != nil {
				t.Fatalf("read %s: %v", h.Name, err)
			}
		}
	}
	if len(names) == 0 || !strings.HasPrefix(names[len(names)-1], dir+changelogfile.ManifestsSubdir+"/") {
		t.Fatalf("entries = %v, want the manifest last", names)
	}
	m, err := changelogfile.DecodeArchiveManifest(manifest, env.ds.Repository().ID)
	if err != nil || m.Head != head {
		t.Fatalf("the manifest = %s (%v), want head %d", manifest, err, head)
	}
}
