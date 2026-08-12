package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type testEnv struct {
	svc   *fakeService
	h     http.Handler
	clock *testClock
}

// testInviteCode is the configured invite code every test env opens with:
// registration is OFF without one, so a test that wants the door shut builds
// its own env instead.
const testInviteCode = "let-me-in"

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	svc := newFakeService()
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	return &testEnv{
		svc:   svc,
		h:     New(Config{Service: svc, Now: clock.now, InviteCode: testInviteCode}),
		clock: clock,
	}
}

// do issues a request; token "" means unauthenticated.
func (e *testEnv) do(t *testing.T, method, path, token string, body any, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	return e.doFrom(t, "10.0.0.1:1234", method, path, token, body, headers...)
}

// doFrom issues a request from a given transport peer address.
func (e *testEnv) doFrom(t *testing.T, peer, method, path, token string, body any, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.RemoteAddr = peer
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func decodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, want, rec.Body.String())
	}
}

func wantErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	wantStatus(t, rec, status)
	env := decodeJSON[errorEnvelope](t, rec)
	if env.Error.Code != code {
		t.Fatalf("error code = %q, want %q (body %s)", env.Error.Code, code, rec.Body.String())
	}
}

func TestHealthzDoesNotTouchTheService(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/healthz", "", nil)
	wantStatus(t, rec, http.StatusOK)
}
