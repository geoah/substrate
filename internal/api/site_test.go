package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/site"
)

// siteDir writes a site directory with a logo, a landing fragment, a nested
// file, a dotfile, a file with no extension and a symlink out of the
// directory, and loads it.
func siteDir(t *testing.T) *site.Site {
	t.Helper()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for rel, body := range map[string]string{
		"site.yaml":     "name: Example\ntagline: A place for friends\nlogo: logo.svg\nlanding: landing.html\n",
		"logo.svg":      "<svg xmlns=\"http://www.w3.org/2000/svg\"/>",
		"landing.html":  "<h1>Welcome</h1>",
		"img/photo.png": "\x89PNG",
		".env":          "SECRET=1",
		"README":        "<html><script>alert(1)</script></html>",
	} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "out.txt")); err != nil {
		t.Fatal(err)
	}
	s, err := site.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// consoleDir is a built console with nothing but its shell.
func consoleDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>console"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func siteHandler(t *testing.T, s *site.Site) http.Handler {
	t.Helper()
	return New(Config{
		Service: newFakeService(),
		Now:     func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		WebDir:  consoleDir(t),
		Site:    s,
	})
}

func serve(h http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSiteServesItsFiles(t *testing.T) {
	h := siteHandler(t, siteDir(t))
	for target, want := range map[string]struct{ ctype, body string }{
		"/site/logo.svg":      {"image/svg+xml", "<svg xmlns=\"http://www.w3.org/2000/svg\"/>"},
		"/site/landing.html":  {"text/html; charset=utf-8", "<h1>Welcome</h1>"},
		"/site/img/photo.png": {"image/png", "\x89PNG"},
		"/site/site.yaml":     {"", "name: Example"},
		// No extension: never sniffed, so the HTML inside is not a page.
		"/site/README": {"application/octet-stream", "<html>"},
	} {
		rec := serve(h, http.MethodGet, target, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", target, rec.Code)
			continue
		}
		if want.ctype != "" && rec.Header().Get("Content-Type") != want.ctype {
			t.Errorf("%s: Content-Type = %q, want %q", target, rec.Header().Get("Content-Type"), want.ctype)
		}
		if !strings.HasPrefix(rec.Body.String(), want.body) {
			t.Errorf("%s: body = %q, want it to start %q", target, rec.Body.String(), want.body)
		}
		for k, v := range map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"Cache-Control":           "no-cache",
			"Content-Security-Policy": sitePolicy,
		} {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s: %s = %q, want %q", target, k, got, v)
			}
		}
		if rec.Header().Get("ETag") == "" || rec.Header().Get("Last-Modified") == "" {
			t.Errorf("%s: want an ETag and a Last-Modified, got %v", target, rec.Header())
		}
	}
}

// chi routes on the raw path, so an encoded slash in the prefix misses the
// /site/ route; the decoded path is still a site path and must not be the
// console shell.
func TestSiteEncodedPrefixIsStillTheSite(t *testing.T) {
	h := siteHandler(t, siteDir(t))
	rec := serve(h, http.MethodGet, "/site%2Flanding.html", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "<h1>Welcome</h1>" {
		t.Fatalf("/site%%2Flanding.html: status = %d, body = %q; want the site file", rec.Code, rec.Body.String())
	}
	rec = serve(h, http.MethodGet, "/site%2F.env", nil)
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "console") {
		t.Fatalf("/site%%2F.env: status = %d, body = %q; want a 404", rec.Code, rec.Body.String())
	}
}

func TestSiteHeadAndRevalidation(t *testing.T) {
	h := siteHandler(t, siteDir(t))
	rec := serve(h, http.MethodHead, "/site/logo.svg", nil)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD: status = %d, body %d bytes; want 200 and no body", rec.Code, rec.Body.Len())
	}
	if rec.Header().Get("Content-Type") != "image/svg+xml" || rec.Header().Get("Content-Length") == "" {
		t.Fatalf("HEAD: headers = %v", rec.Header())
	}
	etag := rec.Header().Get("ETag")
	rec = serve(h, http.MethodGet, "/site/logo.svg", map[string]string{"If-None-Match": etag})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match: status = %d, want 304", rec.Code)
	}
}

func TestSiteRefusesWhatItMayNotServe(t *testing.T) {
	h := siteHandler(t, siteDir(t))
	for _, target := range []string{
		"/site/",
		"/site/img",
		"/site/img/",
		"/site/.env",
		"/site/out.txt",
		"/site/missing.svg",
		"/site/../../etc/passwd",
		"/site/%2e%2e/%2e%2e/etc/passwd",
		"/site/img/..%2flogo.svg",
		"/site/img/%2e%2e/logo.svg",
		"/site//logo.svg",
		"/site/img%5clogo.svg",
	} {
		rec := serve(h, http.MethodGet, target, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "console") || strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("%s: body = %q, want neither the console shell nor the file", target, rec.Body.String())
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := serve(h, method, "/site/logo.svg", nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s /site/logo.svg: status = %d, want 404", method, rec.Code)
		}
	}
}

// With no site, /site/ is the console's like every other path: the shell for
// a route, a 404 for a missing script, as it was before the site existed.
func TestNoSiteLeavesSitePathsToTheConsole(t *testing.T) {
	h := siteHandler(t, nil)
	for target, want := range map[string]int{
		"/site/":             http.StatusOK,
		"/site/logo.svg":     http.StatusOK,
		"/site/landing.html": http.StatusOK,
		"/site/app.js":       http.StatusNotFound,
	} {
		rec := serve(h, http.MethodGet, target, nil)
		if rec.Code != want {
			t.Errorf("%s: status = %d, want %d", target, rec.Code, want)
		}
		if want == http.StatusOK && rec.Body.String() != "<!doctype html>console" {
			t.Errorf("%s: body = %q, want the console shell", target, rec.Body.String())
		}
	}
}

func TestDiscoveryAnnouncesTheSite(t *testing.T) {
	rec := serve(siteHandler(t, siteDir(t)), http.MethodGet, "/.well-known/substrate/server.json", nil)
	wantStatus(t, rec, http.StatusOK)
	doc := decodeJSON[map[string]any](t, rec)
	got, _ := doc["site"].(map[string]any)
	want := map[string]any{
		"name": "Example", "tagline": "A place for friends",
		"logo": "/site/logo.svg", "landing": "/site/landing.html",
	}
	if len(got) != len(want) {
		t.Fatalf("site = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("site.%s = %v, want %v", k, got[k], v)
		}
	}
}

// A site with a name alone carries the name alone: no empty tagline, logo or
// landing for a client to render.
func TestDiscoveryOmitsWhatTheSiteDoesNotSet(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "site.yaml"), []byte("name: Example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := site.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(siteHandler(t, s), http.MethodGet, "/.well-known/substrate/server.json", nil)
	if !strings.Contains(rec.Body.String(), `"site":{"name":"Example"}`) {
		t.Fatalf("body = %s, want site to carry the name alone", rec.Body.String())
	}
}

func TestDiscoveryOmitsSiteWithoutOne(t *testing.T) {
	rec := serve(siteHandler(t, nil), http.MethodGet, "/.well-known/substrate/server.json", nil)
	wantStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), `"site"`) {
		t.Fatalf("body = %s, want no site key", rec.Body.String())
	}
}
