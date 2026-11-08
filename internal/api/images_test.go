package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newImageEnv is a test env whose image proxy reaches a local TLS server,
// which the confined client the server runs with would refuse.
func newImageEnv(t *testing.T, upstream *httptest.Server) *testEnv {
	t.Helper()
	svc := newFakeService()
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	return &testEnv{
		svc:   svc,
		h:     New(Config{Service: svc, Now: clock.now, InviteCode: testInviteCode, ImageClient: upstream.Client()}),
		clock: clock,
	}
}

var pngBytes = []byte("\x89PNG\r\n\x1a\nnot really a png")

func imageUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/cover.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	})
	mux.HandleFunc("/moved.png", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/cover.png", http.StatusFound)
	})
	mux.HandleFunc("/drawing.svg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	})
	mux.HandleFunc("/huge.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(bytes.Repeat([]byte{0}, imageMaxBytes+1))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func imagePath(u string) string { return "/api/v1/images?url=" + url.QueryEscape(u) }

func TestTheImageProxyServesAnImageItsHostServes(t *testing.T) {
	up := imageUpstream(t)
	env := newImageEnv(t, up)
	tok := env.svc.token(fakeRepository)

	for _, p := range []string{"/cover.png", "/moved.png"} {
		rec := env.do(t, http.MethodGet, imagePath(up.URL+p), tok, nil)
		wantStatus(t, rec, http.StatusOK)
		if rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), pngBytes) {
			t.Fatalf("%s: %q %q", p, rec.Header().Get("Content-Type"), rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); !strings.HasPrefix(cc, "private, max-age=") {
			t.Fatalf("%s: Cache-Control %q", p, cc)
		}
		if csp := rec.Header().Get("Content-Security-Policy"); csp != "default-src 'none'; sandbox" {
			t.Fatalf("%s: CSP %q", p, csp)
		}
	}
}

// What the proxy refuses: no token, an address that is not https, a type
// that runs script on its own, an image past the bound, a host that fails.
func TestTheImageProxyRefusesWhatIsNoImageItServes(t *testing.T) {
	up := imageUpstream(t)
	env := newImageEnv(t, up)
	tok := env.svc.token(fakeRepository)

	wantErrorCode(t, env.do(t, http.MethodGet, imagePath(up.URL+"/cover.png"), "", nil), http.StatusUnauthorized, codeAuth)
	wantErrorCode(t, env.do(t, http.MethodGet, imagePath("http://example.com/cover.png"), tok, nil), http.StatusBadRequest, codeBadRequest)
	wantErrorCode(t, env.do(t, http.MethodGet, imagePath("javascript:alert(1)"), tok, nil), http.StatusBadRequest, codeBadRequest)
	for _, p := range []string{"/drawing.svg", "/huge.png", "/missing.png"} {
		wantErrorCode(t, env.do(t, http.MethodGet, imagePath(up.URL+p), tok, nil), http.StatusNotFound, codeNotFound)
	}
}

// The client the server runs with is egress's confined one: a loopback
// address, which a local test server is, is never dialed.
func TestTheImageProxyReachesNoPrivateAddress(t *testing.T) {
	up := imageUpstream(t)
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	rec := env.do(t, http.MethodGet, imagePath(up.URL+"/cover.png"), tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if !strings.Contains(rec.Body.String(), "could not be fetched") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}
