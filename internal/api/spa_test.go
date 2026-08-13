package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The console routes on real history-API paths now, several of which carry
// dots inside a segment (`/types/people.people.substrate.geoah.me`,
// `/records/people.substrate.geoah.me/person/<id>`). The SPA fallback must serve
// index.html for every unmatched GET by *statting the file*, never by
// sniffing an extension out of the path — a dotted segment is not a file.
func TestSPAHandlerServesNestedPathsWithDots(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>console"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("//js"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := New(Config{
		Service: newFakeService(),
		Now:     func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		WebDir:  dir,
	})

	for _, path := range []string{
		"/types",
		"/types/people.people.substrate.geoah.me",
		"/schema/tasks.tasks.substrate.geoah.me",
		"/records/people.substrate.geoah.me/person/9f2kq1x7m0zb",
		"/records/9f2kq1x7m0zb",
		"/integrations",
		"/login",
		"/",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, rec.Code)
		}
		if body := rec.Body.String(); !strings.Contains(body, "console") {
			t.Errorf("%s: body = %q, want index.html fallback", path, body)
		}
	}

	// A path that IS a real file is served as itself, not as index.html.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("asset: status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "//js" {
		t.Fatalf("asset: body = %q, want the file itself", body)
	}
}
