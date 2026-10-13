package api

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// shortBodyDeadlines shrinks every request body deadline so a test can watch
// one expire. Not parallel: it rewrites package variables.
func shortBodyDeadlines(t *testing.T) time.Duration {
	t.Helper()
	const d = 150 * time.Millisecond
	plain, vocab := jsonBody.deadline, vocabularyBody.deadline
	inline, multi, blob := webhookInlineDeadline, webhookMultipartDeadline, blobTransferDeadline
	jsonBody.deadline, vocabularyBody.deadline = d, d
	webhookInlineDeadline, webhookMultipartDeadline, blobTransferDeadline = d, d, d
	t.Cleanup(func() {
		jsonBody.deadline, vocabularyBody.deadline = plain, vocab
		webhookInlineDeadline, webhookMultipartDeadline, blobTransferDeadline = inline, multi, blob
	})
	return d
}

// stallBody sends a request whose headers promise a body of 1 KiB, then
// sends one byte of it and nothing more. It answers the status of whatever
// response arrives within wait, or 0 when none does.
func stallBody(t *testing.T, srv *httptest.Server, method, path string, headers map[string]string, wait time.Duration) int {
	t.Helper()
	return stallAfter(t, srv, method, path, headers, "{", wait)
}

// stallAfter is stallBody sending sent as the start of the body.
func stallAfter(t *testing.T, srv *httptest.Server, method, path string, headers map[string]string, sent string, wait time.Duration) int {
	t.Helper()
	status, conn, _ := stallConn(t, srv, method, path, headers, sent, wait)
	_ = conn.Close()
	return status
}

// stallConn is stallAfter leaving the connection open, with the response
// read off it, so a test can watch what the server does with it next. The
// caller closes the connection.
func stallConn(t *testing.T, srv *httptest.Server, method, path string, headers map[string]string, sent string, wait time.Duration) (int, net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var req strings.Builder
	fmt.Fprintf(&req, "%s %s HTTP/1.1\r\nHost: substrate.test\r\nContent-Length: 1024\r\n", method, path)
	for k, v := range headers {
		fmt.Fprintf(&req, "%s: %s\r\n", k, v)
	}
	req.WriteString("\r\n" + sent)
	if _, err := io.WriteString(conn, req.String()); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(wait))
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return 0, conn, br
	}
	_ = resp.Body.Close()
	return resp.StatusCode, conn, br
}

func postRaw(h http.Handler, path, token string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A JSON body past its route's cap is a 413 with code bad_request, on the
// unauthenticated door and on the API alike; a body under the cap that does
// not decode stays a 400.
func TestOversizedJSONBodyIsTooLarge(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	over := func(limit int64) []byte {
		return []byte(`{"repository":"` + strings.Repeat("a", int(limit)) + `"}`)
	}
	for _, c := range []struct {
		name, path, token string
		body              []byte
	}{
		{"login", "/login", "", over(maxRequestBody)},
		{"records", "/api/" + APIVersion + "/records", tok, over(maxRequestBody)},
		{"vocabulary apply", "/api/" + APIVersion + "/vocabulary/apply", tok, over(maxVocabularyApplyBody)},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := postRaw(env.h, c.path, c.token, c.body)
			wantErrorCode(t, rec, http.StatusRequestEntityTooLarge, codeBadRequest)
		})
	}
	wantErrorCode(t, postRaw(env.h, "/login", "", []byte(`{"repositry":"x"}`)), http.StatusBadRequest, codeBadRequest)
}

// A client that sends the headers and then stalls on the body is answered
// at the route's deadline instead of holding its goroutine and connection
// for as long as it likes. The server sets no ReadTimeout, so the per-route
// deadline is the only thing that ends the wait.
func TestStalledBodyIsAnsweredAtTheDeadline(t *testing.T) {
	shortBodyDeadlines(t)
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	bearer := map[string]string{"Authorization": "Bearer " + tok, "Content-Type": "application/json"}
	for _, c := range []struct {
		name, method, path string
		headers            map[string]string
		want               int
	}{
		{"login", http.MethodPost, "/login", map[string]string{"Content-Type": "application/json"}, http.StatusBadRequest},
		{"merge", http.MethodPost, "/api/" + APIVersion + "/merge", bearer, http.StatusBadRequest},
		{"vocabulary apply", http.MethodPost, "/api/" + APIVersion + "/vocabulary/apply", bearer, http.StatusBadRequest},
		{"blob put", http.MethodPut, "/api/" + APIVersion + "/blobs", bearer, http.StatusBadRequest},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := stallBody(t, srv, c.method, c.path, c.headers, 3*time.Second); got != c.want {
				t.Fatalf("a stalled body got status %d within 3s, want %d (0 is no answer)", got, c.want)
			}
		})
	}
}

// A request with no body has net/http reading the connection in the
// background before the handler starts, and that read takes whatever
// deadline the handler sets. Unless readBody clears it, the request's
// context ends at the deadline while the handler is still at work.
func TestEmptyBodyOutlivesItsDeadline(t *testing.T) {
	d := shortBodyDeadlines(t)
	canceled := make(chan bool, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := readBody(w, r, jsonBody); !ok {
			canceled <- true
			return
		}
		select {
		case <-r.Context().Done():
			canceled <- true
		case <-time.After(4 * d):
			canceled <- false
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	resp, err := srv.Client().Post(srv.URL, "application/json", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if <-canceled {
		t.Fatalf("the request's context ended within %s of an empty body: the body deadline outlived the read", 4*d)
	}
}
