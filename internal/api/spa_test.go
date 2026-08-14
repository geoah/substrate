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
// dots inside a segment (`/types/people.people.substrate.reamde.dev`,
// `/records/people.substrate.reamde.dev/person/<id>`). The SPA fallback must serve
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
		"/types/people.people.substrate.reamde.dev",
		"/schema/tasks.tasks.substrate.reamde.dev",
		"/records/people.substrate.reamde.dev/person/9f2kq1x7m0zb",
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

// The console's lazy chunks are content-hashed, so a tab left open across a
// rebuild requests URLs that no longer exist. The fallback used to answer those
// with index.html and a 200: the browser then refused HTML where it had asked
// for a module ("'text/html' is not a valid JavaScript MIME type") and a plain
// missing file read as a parse error. A missing asset is a 404, and only an
// extension-less path (a client route) falls back.
func TestSPAHandlerMissingAssetIsNotFound(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>console"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := New(Config{
		Service: newFakeService(),
		Now:     func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		WebDir:  dir,
	})

	for _, path := range []string{
		"/assets/yaml-editor-Ba9xQ1.js",
		"/assets/index-7f3a2b.css",
		"/assets/index-7f3a2b.js.map",
		"/assets/inter-latin.woff2",
		// The extension decides outside /assets/ too: the build's `base` is a
		// deployment's choice, and a chunk is a chunk wherever it is served from.
		"/static/yaml-editor-Ba9xQ1.js",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(body, "console") {
			t.Errorf("%s: body = %q, want a 404 rather than index.html", path, body)
		}
	}

	// The client's own routes carry no extension, and still fall back.
	for _, path := range []string{
		"/data/people.substrate.reamde.dev/people/9f2kq1x7m0zb/edit",
		"/nope",
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
}

// index.html names the build's chunk hashes and the assets under /assets/ are
// named BY their content, so the two want opposite caching: revalidate the one
// every time, keep the others forever. A cached index.html is a tab that cannot
// learn about a deploy, which is the same bug the 404 above fixes from the
// other side.
func TestSPAHandlerCacheHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>console"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "index-7f3a2b.js"), []byte("//js"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := New(Config{
		Service: newFakeService(),
		Now:     func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		WebDir:  dir,
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/index-7f3a2b.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("asset: status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "//js" {
		t.Errorf("asset: body = %q, want the file itself", body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("asset: cache-control = %q, want immutable", got)
	}

	// `/index.html` itself is not in the list: http.ServeFile canonicalizes it
	// to `./` with a 301 before any header of ours is read.
	//
	// no-store rather than a bare no-cache: a revalidation would answer 304 off
	// ServeFile's one-second Last-Modified, so an index.html replaced inside the
	// same second would leave the tab holding the dead chunk names.
	for _, path := range []string{"/", "/login", "/data/tasks/new"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache" {
			t.Errorf("%s: cache-control = %q, want no-store", path, got)
		}
	}
}

// The web directory is the whole world this handler may answer about. A URL path
// carrying `..` or a BACKSLASH (`%5C` decodes to one, and path.Clean does not
// read it as a separator) must not reach a stat outside the directory: an answer
// that differs by whether an outside file exists is an existence oracle over the
// filesystem, even when its bytes never leave.
func TestSPAHandlerRefusesTraversal(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "web")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>console"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Planted OUTSIDE the served directory, one level up: the responses below
	// must not tell it apart from a name that is not there at all.
	if err := os.WriteFile(filepath.Join(base, "outside.js"), []byte("//secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := New(Config{
		Service: newFakeService(),
		Now:     func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		WebDir:  dir,
	})

	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}

	for _, target := range []string{
		"/..%5Coutside.js",
		"/..%5C..%5Coutside.js",
		"/assets/..%5C..%5Coutside.js",
		"/../outside.js",
		"/assets/../../outside.js",
	} {
		rec := get(target)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", target, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(body, "secret") || strings.Contains(body, "console") {
			t.Errorf("%s: body = %q, want a plain 404", target, body)
		}
		// The same shape for a name that exists nowhere: identical status and
		// body, so the response says nothing about what is out there.
		absent := strings.ReplaceAll(target, "outside.js", "nothere.js")
		other := get(absent)
		if other.Code != rec.Code || other.Body.String() != rec.Body.String() {
			t.Errorf("%s vs %s: %d/%q vs %d/%q, want indistinguishable",
				target, absent, rec.Code, rec.Body.String(), other.Code, other.Body.String())
		}
	}

	// A backslash is refused BEFORE the extension rule, so an extension-less one
	// does not reach the fallback either. This is the case that separates
	// "rejected" from "happened to miss": on a POSIX box the paths above miss the
	// stat anyway, `..\outside.js` being a legal filename there.
	rec := get("/..%5C..%5Csome-route")
	if rec.Code != http.StatusNotFound {
		t.Errorf("extension-less backslash: status = %d, want 404", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "console") {
		t.Errorf("extension-less backslash: body = %q, want a plain 404", body)
	}
}
