package api

// Each person's requests are paced once their token is read: past the burst
// a request answers 429 rate_limited with the Retry-After the bucket needs,
// and the rate refills it. The pacing is the door's, before any dataset: a
// request it admits reaches the decoder, which refuses pacedPath's filter
// with 400 and leaves the stub's dataset unread.

import (
	"net/http"
	"testing"
	"time"
)

const pacedPath = recordsPath + "?filter=not-json"

// pacedStubEnv is the stub with the person's rate and burst set.
func pacedStubEnv(t *testing.T, rate float64, burst int) *testEnv {
	t.Helper()
	return newStubEnv(t, nil, func(c *Config) { c.RequestsPerPerson, c.RequestBurstPerPerson = rate, burst })
}

func TestAPersonsRequestsArePaced(t *testing.T) {
	t.Parallel()
	env := pacedStubEnv(t, 1, 2)
	for i := range 2 {
		if rec := env.do(t, http.MethodGet, pacedPath, stubToken, nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("request %d of the burst: %d %s, want the decoder's 400", i+1, rec.Code, rec.Body.String())
		}
	}
	rec := env.do(t, http.MethodGet, pacedPath, stubToken, nil)
	wantErrorCode(t, rec, http.StatusTooManyRequests, codeRateLimited)
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1: one request a second refills in a second", got)
	}
	env.clock.advance(time.Second)
	if rec := env.do(t, http.MethodGet, pacedPath, stubToken, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("after the refill: %d %s, want the decoder's 400", rec.Code, rec.Body.String())
	}
	// Discovery reads no token, and is not paced.
	for range 3 {
		if rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil); rec.Code != http.StatusOK {
			t.Fatalf("discovery: %d", rec.Code)
		}
	}
}

// A server that sets no rate paces nothing.
func TestNoRateIsNoPacing(t *testing.T) {
	t.Parallel()
	env := pacedStubEnv(t, 0, 0)
	for range 50 {
		if rec := env.do(t, http.MethodGet, pacedPath, stubToken, nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("unpaced request: %d %s, want the decoder's 400", rec.Code, rec.Body.String())
		}
	}
}
