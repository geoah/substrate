package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// readLine reads one ndjson line, failing the test if the stream stalls.
func readLine(t *testing.T, br *bufio.Reader) map[string]any {
	t.Helper()
	type result struct {
		line []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := br.ReadBytes('\n')
		ch <- result{line, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("read stream: %v", r.err)
		}
		var out map[string]any
		if err := json.Unmarshal(r.line, &out); err != nil {
			t.Fatalf("decode %q: %v", r.line, err)
		}
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a stream line")
		return nil
	}
}

func startWatch(t *testing.T, srv *httptest.Server, path, token string) (*bufio.Reader, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stop := func() { cancel(); _ = resp.Body.Close() }
	if resp.StatusCode != http.StatusOK {
		stop()
		t.Fatalf("watch status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/x-ndjson" {
		stop()
		t.Fatalf("content-type = %q", got)
	}
	return bufio.NewReader(resp.Body), stop
}

// seedPeople writes n people, c1 to cn, as the engine's own hand, and
// returns the head they leave.
func seedPeople(t *testing.T, ds substrate.Dataset, n int) int64 {
	t.Helper()
	ctx := context.Background()
	for i := range n {
		id := "c" + strconv.Itoa(i+1)
		if _, err := ds.Put(ctx, substrate.ActorSystem, substrate.PutInput{
			Kind: personKind, ID: id, Properties: map[string]any{"name": "Person " + id},
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	head, err := ds.Head(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	return head.Seq
}

// resumeRefusal opens path against the live server and returns the status
// and, for a 410, its problem object. It goes over a real socket rather than
// a recorder because the failure it guards against is a stream that OPENS on
// a bad cursor: a 200 arrives as soon as the bookmark flushes and is reported
// as the status it is, where a recorder would wait on the tail forever.
func resumeRefusal(t *testing.T, srv *httptest.Server, path, token string) (int, substrate.ErrorPayload) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusGone {
		return resp.StatusCode, substrate.ErrorPayload{}
	}
	var env substrate.ErrorEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode the 410 body: %v", err)
	}
	return resp.StatusCode, env.Error
}

// wantReset asserts a 410 `compacted` problem object names the head and
// generation the client re-lists from.
func wantReset(t *testing.T, status int, got substrate.ErrorPayload, head int64, generation string) {
	t.Helper()
	if status != http.StatusGone || got.Code != codeCompacted {
		t.Fatalf("status %d code %q, want 410 %s", status, got.Code, codeCompacted)
	}
	if got.Head == nil || *got.Head != head || got.Generation != generation {
		t.Fatalf("reset names head %v generation %q, want head %d generation %q", got.Head, got.Generation, head, generation)
	}
}

// resumeEntryPoints are every REST door that takes a `from` cursor: the feed
// as a page, the feed as a stream, and the records route's own stream. The
// rule is one rule, so each door is held to it.
var resumeEntryPoints = map[string]string{
	"feed page":     "/api/v1/changes?",
	"feed watch":    "/api/v1/changes?watch=1&",
	"records watch": peopleRecords + "&watch=1&",
}
