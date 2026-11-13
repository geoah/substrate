package api

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
)

// The archive comes back as a tar under the authority's file name, with the
// point in the name: the repository's directory laid out as a data root, its
// manifest in it and snapshot.json, recording that point, as the last entry.
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

	dir := changelogfile.RepositoriesDir + "/" + env.repository + "/"
	tr := tar.NewReader(rec.Body)
	var names []string
	var snapshot []byte
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
		if h.Name == dir+changelogfile.SnapshotName {
			if snapshot, err = io.ReadAll(tr); err != nil {
				t.Fatalf("read %s: %v", h.Name, err)
			}
		}
	}
	manifest := false
	for _, name := range names {
		manifest = manifest || name == dir+"repository.json"
	}
	if !manifest || len(names) == 0 || names[len(names)-1] != dir+changelogfile.SnapshotName {
		t.Fatalf("entries = %v, want the manifest and %s last", names, changelogfile.SnapshotName)
	}
	var point struct {
		Head int64 `json:"head"`
	}
	if err := json.Unmarshal(snapshot, &point); err != nil || point.Head != head {
		t.Fatalf("%s = %s (%v), want head %d", changelogfile.SnapshotName, snapshot, err, head)
	}
}
