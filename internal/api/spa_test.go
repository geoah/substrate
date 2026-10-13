package api

import (
	"bytes"
	"compress/gzip"
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
	"sync/atomic"
	"testing"
	"time"
)

// The console routes on real history-API paths now, several of which carry
// dots inside a segment (`/types/people.samples.substrate.reamde.dev/people`,
// `/records/samples.substrate.reamde.dev/people/person/<id>`). The SPA fallback must serve
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
		"/types/people.samples.substrate.reamde.dev/people",
		"/schema/tasks.samples.substrate.reamde.dev/tasks",
		"/records/samples.substrate.reamde.dev/people/person/9f2kq1x7m0zb",
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
		"/data/samples.substrate.reamde.dev/people/person/9f2kq1x7m0zb/edit",
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

// The console's chunks are text, about a third of their size under gzip, and
// nothing between the server and the browser compresses them on the
// documented deployment. A client that accepts gzip gets the compressed copy
// of a content-hashed asset, under the same immutable caching, and every
// answer for such an asset says it varies by Accept-Encoding. index.html is
// never compressed, whatever the client accepts.
func TestSPAHandlerServesGzipAssets(t *testing.T) {
	dir := t.TempDir()
	index := "<!doctype html>console" + strings.Repeat("<!-- padding -->", 200)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	js := strings.Repeat("console.log('substrate');\n", 400)
	jsPath := filepath.Join(dir, "assets", "index-7f3a2b.js")
	if err := os.WriteFile(jsPath, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	// Too small for gzip to shrink: served as it is.
	if err := os.WriteFile(filepath.Join(dir, "assets", "tiny-1a2b.css"), []byte("a{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := New(Config{
		Service: newFakeService(),
		Now:     func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		WebDir:  dir,
	})
	get := func(method, target, accept string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		if accept != "" {
			req.Header.Set("Accept-Encoding", accept)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	gunzip := func(t *testing.T, body []byte) string {
		t.Helper()
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("gzip reader: %v", err)
		}
		out, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("gunzip: %v", err)
		}
		return string(out)
	}

	t.Run("gzip when accepted", func(t *testing.T) {
		for _, accept := range []string{"gzip, deflate, br, zstd", "br;q=1.0, gzip;q=0.8", "x-gzip", "*"} {
			rec := get(http.MethodGet, "/assets/index-7f3a2b.js", accept)
			if rec.Code != http.StatusOK {
				t.Fatalf("%q: status = %d, want 200", accept, rec.Code)
			}
			if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
				t.Fatalf("%q: content-encoding = %q, want gzip", accept, got)
			}
			if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
				t.Errorf("%q: vary = %q, want Accept-Encoding", accept, got)
			}
			if got := rec.Header().Get("Cache-Control"); got != cacheImmutable {
				t.Errorf("%q: cache-control = %q, want immutable", accept, got)
			}
			if got := rec.Header().Get("Content-Type"); got != mime.TypeByExtension(".js") {
				t.Errorf("%q: content-type = %q, want %q", accept, got, mime.TypeByExtension(".js"))
			}
			if got, want := rec.Header().Get("Content-Length"), strconv.Itoa(rec.Body.Len()); got != want {
				t.Errorf("%q: content-length = %q, want %q", accept, got, want)
			}
			if rec.Body.Len() >= len(js) {
				t.Errorf("%q: %d bytes on the wire for a %d byte file", accept, rec.Body.Len(), len(js))
			}
			if got := gunzip(t, rec.Body.Bytes()); got != js {
				t.Errorf("%q: decompressed body differs from the file", accept)
			}
		}
	})

	t.Run("the file as it is otherwise", func(t *testing.T) {
		for _, accept := range []string{"", "identity", "gzip;q=0", "br, *;q=0", "gzip;q=0, *", "gzip;q=junk"} {
			rec := get(http.MethodGet, "/assets/index-7f3a2b.js", accept)
			if rec.Code != http.StatusOK {
				t.Fatalf("%q: status = %d, want 200", accept, rec.Code)
			}
			if got := rec.Header().Get("Content-Encoding"); got != "" {
				t.Errorf("%q: content-encoding = %q, want none", accept, got)
			}
			if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
				t.Errorf("%q: vary = %q, want Accept-Encoding", accept, got)
			}
			if rec.Body.String() != js {
				t.Errorf("%q: body is not the file itself", accept)
			}
		}
		rec := get(http.MethodGet, "/assets/tiny-1a2b.css", "gzip")
		if got := rec.Header().Get("Content-Encoding"); got != "" || rec.Body.String() != "a{}" {
			t.Errorf("tiny asset: content-encoding = %q, body = %q, want the file as it is", got, rec.Body.String())
		}
	})

	t.Run("a HEAD carries the gzip headers and no body", func(t *testing.T) {
		rec := get(http.MethodHead, "/assets/index-7f3a2b.js", "gzip")
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("HEAD: status = %d, content-encoding = %q", rec.Code, rec.Header().Get("Content-Encoding"))
		}
		if rec.Body.Len() != 0 {
			t.Errorf("HEAD: %d body bytes, want none", rec.Body.Len())
		}
	})

	// A range of the gzip copy is a range of gzip bytes, and several ranges
	// are a multipart body no client decodes as gzip, so the copy goes whole.
	t.Run("a range request gets the whole gzip copy", func(t *testing.T) {
		for _, ranges := range []string{"bytes=0-9", "bytes=0-9,20-29"} {
			req := httptest.NewRequest(http.MethodGet, "/assets/index-7f3a2b.js", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			req.Header.Set("Range", ranges)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200", ranges, rec.Code)
			}
			if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
				t.Fatalf("%s: content-encoding = %q, want gzip", ranges, got)
			}
			if got := rec.Header().Get("Content-Range"); got != "" {
				t.Errorf("%s: content-range = %q, want none", ranges, got)
			}
			if got := rec.Header().Get("Accept-Ranges"); got != "" {
				t.Errorf("%s: accept-ranges = %q, want none on the gzip copy", ranges, got)
			}
			if got := gunzip(t, rec.Body.Bytes()); got != js {
				t.Errorf("%s: decompressed body differs from the file", ranges)
			}
		}
		// Without gzip, a range is still a range of the file.
		req := httptest.NewRequest(http.MethodGet, "/assets/index-7f3a2b.js", nil)
		req.Header.Set("Range", "bytes=0-9")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusPartialContent || rec.Body.String() != js[:10] {
			t.Errorf("identity range: status = %d, body = %q, want 206 with %q", rec.Code, rec.Body.String(), js[:10])
		}
	})

	t.Run("index.html and a missing asset are never compressed", func(t *testing.T) {
		for _, target := range []string{"/", "/login", "/data/tasks/new"} {
			rec := get(http.MethodGet, target, "gzip")
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200", target, rec.Code)
			}
			if got := rec.Header().Get("Content-Encoding"); got != "" {
				t.Errorf("%s: content-encoding = %q, want none", target, got)
			}
			if rec.Body.String() != index {
				t.Errorf("%s: body is not index.html as it is", target)
			}
			if got := rec.Header().Get("Cache-Control"); got != cacheNever {
				t.Errorf("%s: cache-control = %q, want no-store", target, got)
			}
		}
		rec := get(http.MethodGet, "/assets/gone-9z8y.js", "gzip")
		if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Encoding") != "" {
			t.Errorf("missing asset: status = %d, content-encoding = %q, want a plain 404",
				rec.Code, rec.Header().Get("Content-Encoding"))
		}
	})

	// ServeFile refuses a `..` segment in the URL path, and the gzip copy
	// answers no differently.
	t.Run("a dot-dot path is refused either way", func(t *testing.T) {
		const target = "/assets/../assets/index-7f3a2b.js"
		plain := get(http.MethodGet, target, "")
		zipped := get(http.MethodGet, target, "gzip")
		if plain.Code != http.StatusBadRequest {
			t.Fatalf("identity: status = %d, want 400", plain.Code)
		}
		if zipped.Code != plain.Code || zipped.Header().Get("Content-Encoding") != "" {
			t.Errorf("gzip: status = %d, content-encoding = %q, want %d and none",
				zipped.Code, zipped.Header().Get("Content-Encoding"), plain.Code)
		}
	})

	// A 304 or a 412 has no body, so it names no encoding and no length.
	t.Run("a precondition answer carries no gzip headers", func(t *testing.T) {
		info, err := os.Stat(jsPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			header string
			at     time.Time
			code   int
		}{
			{"If-Modified-Since", info.ModTime().Add(time.Hour), http.StatusNotModified},
			{"If-Unmodified-Since", info.ModTime().Add(-time.Hour), http.StatusPreconditionFailed},
		} {
			req := httptest.NewRequest(http.MethodGet, "/assets/index-7f3a2b.js", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			req.Header.Set(tc.header, tc.at.UTC().Format(http.TimeFormat))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.code {
				t.Fatalf("%s: status = %d, want %d", tc.header, rec.Code, tc.code)
			}
			if got := rec.Header().Get("Content-Encoding"); got != "" {
				t.Errorf("%s: content-encoding = %q, want none", tc.header, got)
			}
			if got := rec.Header().Get("Content-Length"); got != "" && got != "0" {
				t.Errorf("%s: content-length = %q for an empty body", tc.header, got)
			}
			if rec.Body.Len() != 0 {
				t.Errorf("%s: %d body bytes, want none", tc.header, rec.Body.Len())
			}
		}
	})

	t.Run("a file replaced in place is compressed again", func(t *testing.T) {
		next := strings.Repeat("console.log('rebuilt');\n", 500)
		if err := os.WriteFile(jsPath, []byte(next), 0o644); err != nil {
			t.Fatal(err)
		}
		rec := get(http.MethodGet, "/assets/index-7f3a2b.js", "gzip")
		if got := gunzip(t, rec.Body.Bytes()); got != next {
			t.Errorf("after a rewrite: decompressed body is the old file")
		}
	})
}

// A burst of requests for an asset nobody has asked for yet waits on one
// compression: compressing per request would hold a copy of the file per
// request, on a route anyone may call.
func TestGzipAssetsCompressOncePerFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "index-7f3a2b.js")
	if err := os.WriteFile(file, []byte(strings.Repeat("console.log(1);\n", 400)), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}

	g := newGzipAssets()
	var calls atomic.Int32
	release := make(chan struct{})
	g.compress = func(name string, info os.FileInfo) ([]byte, error) {
		calls.Add(1)
		<-release
		return gzipFile(name, info)
	}

	const burst = 16
	var started, finished sync.WaitGroup
	bodies := make([][]byte, burst)
	for i := range burst {
		started.Add(1)
		finished.Add(1)
		go func() {
			defer finished.Done()
			started.Done()
			body, err := g.body(file, info)
			if err != nil {
				t.Errorf("body: %v", err)
			}
			bodies[i] = body
		}()
	}
	started.Wait()
	// Every request is in, or about to be, before the one compression ends.
	time.Sleep(50 * time.Millisecond)
	close(release)
	finished.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("compressed %d times for %d requests, want once", got, burst)
	}
	for i, body := range bodies {
		if len(body) == 0 || !bytes.Equal(body, bodies[0]) {
			t.Errorf("request %d got %d bytes, want the one copy", i, len(body))
		}
	}
}

// A file replaced while its first copy is still compressing gets a copy of
// its own, and only the copy the map still holds counts toward the bound.
// The replacing request reaches the lock after the first copy is written but
// before it is counted: it must not subtract what was never added, nor read
// that body while its compressor writes it (run under -race).
func TestGzipAssetsReplacedWhileCompressing(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "index-7f3a2b.js")
	write := func(text string) os.FileInfo {
		t.Helper()
		if err := os.WriteFile(file, []byte(strings.Repeat(text, 400)), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	first := write("console.log(1);\n")

	g := newGzipAssets()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	g.compress = func(name string, info os.FileInfo) ([]byte, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return []byte("first copy"), nil
		}
		return gzipFile(name, info)
	}

	firstBody := make(chan []byte, 1)
	go func() {
		body, err := g.body(file, first)
		if err != nil {
			t.Errorf("first body: %v", err)
		}
		firstBody <- body
	}()
	<-entered
	second := write("console.log('rebuilt');\n")

	// Holding the lock queues the replacing request on it first, and the
	// first request, once its copy is written, behind it.
	g.mu.Lock()
	secondBody := make(chan []byte, 1)
	go func() {
		body, err := g.body(file, second)
		if err != nil {
			t.Errorf("second body: %v", err)
		}
		secondBody <- body
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)
	time.Sleep(50 * time.Millisecond)
	g.mu.Unlock()

	want := <-secondBody
	if got := <-firstBody; string(got) != "first copy" {
		t.Errorf("the first request got %q, want its own copy", got)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.size != len(want) {
		t.Errorf("held size = %d, want %d: only the copy the map holds counts", g.size, len(want))
	}
	if g.held[file].counted != len(want) {
		t.Errorf("the held copy counts %d, want %d", g.held[file].counted, len(want))
	}
}

// Passing the bound drops the copies held, never one still compressing: its
// waiters and the next request for it share the one compression.
func TestGzipAssetsEvictionKeepsPending(t *testing.T) {
	dir := t.TempDir()
	stat := func(name string) (string, os.FileInfo) {
		t.Helper()
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, []byte(strings.Repeat("console.log(1);\n", 400)), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		return file, info
	}
	slow, slowInfo := stat("slow-1a.js")
	fast, fastInfo := stat("fast-2b.js")
	other, otherInfo := stat("other-3c.js")

	g := newGzipAssets()
	g.maxHeld = 1 // every copy passes the bound
	entered, release := make(chan struct{}), make(chan struct{})
	var slowCalls atomic.Int32
	g.compress = func(name string, info os.FileInfo) ([]byte, error) {
		if name == slow {
			if slowCalls.Add(1) == 1 {
				close(entered)
				<-release
			}
		}
		return gzipFile(name, info)
	}

	var waiting sync.WaitGroup
	waiting.Add(1)
	go func() {
		defer waiting.Done()
		if _, err := g.body(slow, slowInfo); err != nil {
			t.Errorf("slow body: %v", err)
		}
	}()
	<-entered
	// Two completed copies: the second passes the bound and evicts the first.
	for _, f := range []struct {
		name string
		info os.FileInfo
	}{{fast, fastInfo}, {other, otherInfo}} {
		if _, err := g.body(f.name, f.info); err != nil {
			t.Fatal(err)
		}
	}
	waiting.Add(1)
	go func() {
		defer waiting.Done()
		if _, err := g.body(slow, slowInfo); err != nil {
			t.Errorf("second slow body: %v", err)
		}
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)
	waiting.Wait()

	if got := slowCalls.Load(); got != 1 {
		t.Errorf("the pending file was compressed %d times, want once", got)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.held[fast]; ok {
		t.Errorf("the first completed copy is still held past the bound")
	}
}

// The copy must be of the bytes its request's stat describes: a copy of a
// replacement would go out under the old Last-Modified, and a 304 matched
// against it would keep a stale file.
func TestGzipFileRefusesAChangedFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "index-7f3a2b.js")
	if err := os.WriteFile(file, []byte(strings.Repeat("console.log(1);\n", 400)), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if body, err := gzipFile(file, stale); err != nil || len(body) == 0 {
		t.Fatalf("gzipFile(current) = %d bytes, %v; want a copy", len(body), err)
	}
	// The same length, rewritten later: only the modification time tells.
	if err := os.WriteFile(file, []byte(strings.Repeat("console.log(2);\n", 400)), 0o644); err != nil {
		t.Fatal(err)
	}
	later := stale.ModTime().Add(time.Second)
	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatal(err)
	}
	if _, err := gzipFile(file, stale); !errors.Is(err, errAssetChanged) {
		t.Errorf("gzipFile(rewritten, same length) err = %v, want errAssetChanged", err)
	}
	if err := os.WriteFile(file, []byte(strings.Repeat("console.log('rebuilt');\n", 500)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gzipFile(file, stale); !errors.Is(err, errAssetChanged) {
		t.Errorf("gzipFile(replaced) err = %v, want errAssetChanged", err)
	}
}

func TestAcceptsGzip(t *testing.T) {
	for header, want := range map[string]bool{
		"":                    false,
		"gzip":                true,
		"GZIP":                true,
		"x-gzip":              true,
		"gzip;q=0.001":        true,
		"gzip; q=0":           false,
		"gzip;q=0.0":          false,
		"gzip;q=nope":         false,
		"deflate, br":         false,
		"*":                   true,
		"*;q=0":               false,
		"gzip;q=0, *":         false,
		"br, *;q=0.5":         true,
		"identity;q=1, *;q=0": false,
		" gzip ; q=1 , br":    true,
	} {
		if got := acceptsGzip(header); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}
