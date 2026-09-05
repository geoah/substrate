package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed by the returned stop func
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

func TestWatchCollectionStreamsBookmarkThenChanges(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	br, stop := startWatch(t, srv, peoplePath+"?watch=1", tok)
	defer stop()

	first := readLine(t, br)
	if _, ok := first["bookmark"]; !ok {
		t.Fatalf("first line = %v, want a bookmark", first)
	}

	ds.commit(substrate.Change{
		TS: time.Unix(5, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "c1", Kind: "samples.substrate.reamde.dev/people/person",
	})
	// A change in another collection must not appear on this stream.
	ds.commit(substrate.Change{
		TS: time.Unix(6, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "m1", Kind: "samples.substrate.reamde.dev/messaging/conversationmessage",
	})
	ds.commit(substrate.Change{
		TS: time.Unix(7, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPatch,
		RecordID: "c2", Kind: "samples.substrate.reamde.dev/people/person",
	})

	got := readLine(t, br)
	if got["recordId"] != "c1" {
		t.Fatalf("first change = %v", got)
	}
	got = readLine(t, br)
	if got["recordId"] != "c2" {
		t.Fatalf("second change = %v, want the message change filtered out", got)
	}
}

func TestWatchResumesFromCursor(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	for i := range 3 {
		ds.commit(substrate.Change{
			TS: time.Unix(int64(i), 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
			RecordID: "c" + string(rune('1'+i)), Kind: "samples.substrate.reamde.dev/people/person",
		})
	}

	br, stop := startWatch(t, srv, "/api/v1/changes?watch=1&from=1", tok)
	defer stop()

	if bm := readLine(t, br); bm["bookmark"] != float64(1) {
		t.Fatalf("bookmark = %v, want the supplied cursor", bm["bookmark"])
	}
	if got := readLine(t, br); got["seq"] != float64(2) {
		t.Fatalf("first replayed change = %v, want seq 2", got)
	}
	if got := readLine(t, br); got["seq"] != float64(3) {
		t.Fatalf("second replayed change = %v, want seq 3", got)
	}
}

func TestChangesWithoutWatchIsASinglePage(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	ds.commit(substrate.Change{
		TS: time.Unix(1, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "c1", Kind: "samples.substrate.reamde.dev/people/person",
	})

	rec := env.do(t, http.MethodGet, "/api/v1/changes?kinds=samples.substrate.reamde.dev/people/person", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("content-type = %q", got)
	}
	lines := 0
	sc := bufio.NewScanner(rec.Body)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			lines++
		}
	}
	if lines != 2 {
		t.Fatalf("got %d ndjson lines, want bookmark + 1 change", lines)
	}
}
