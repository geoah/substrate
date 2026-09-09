package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

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
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	for i := range 3 {
		ds.commit(substrate.Change{
			TS: time.Unix(int64(i), 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
			RecordID: "c" + string(rune('1'+i)), Kind: "samples.substrate.reamde.dev/people/person",
		})
	}

	br, stop := startWatch(t, srv, "/api/v1/changes?watch=1&from=1&generation="+ds.generation, tok)
	defer stop()

	if bm := readLine(t, br); bm["bookmark"] != float64(1) || bm["generation"] != ds.generation {
		t.Fatalf("bookmark = %v, want the supplied cursor under the repository's generation", bm)
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
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
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

// seedPeople commits n person writes and returns the head.
func seedPeople(ds *fakeDataset, n int) int64 {
	for i := range n {
		ds.commit(substrate.Change{
			TS: time.Unix(int64(i), 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
			RecordID: "c" + strconv.Itoa(i+1), Kind: "samples.substrate.reamde.dev/people/person",
		})
	}
	return int64(len(ds.changes))
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
// as a page, the feed as a stream, and a collection's own stream. The rule
// is one rule, so each door is held to it.
var resumeEntryPoints = map[string]string{
	"feed page":        "/api/v1/changes?",
	"feed watch":       "/api/v1/changes?watch=1&",
	"collection watch": peoplePath + "?watch=1&",
}

func TestBookmarkCarriesTheGeneration(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	head := seedPeople(ds, 2)

	for name, prefix := range resumeEntryPoints {
		if name == "feed page" {
			rec := env.do(t, http.MethodGet, prefix+"from=0", tok, nil)
			wantStatus(t, rec, http.StatusOK)
			bm := readLine(t, bufio.NewReader(rec.Body))
			if bm["bookmark"] != float64(0) || bm["generation"] != ds.generation {
				t.Fatalf("%s: bookmark = %v, want seq 0 under %q", name, bm, ds.generation)
			}
			continue
		}
		br, stop := startWatch(t, srv, prefix[:len(prefix)-1], tok)
		bm := readLine(t, br)
		stop()
		if bm["bookmark"] != float64(head) || bm["generation"] != ds.generation {
			t.Fatalf("%s: bookmark = %v, want the head %d under %q", name, bm, head, ds.generation)
		}
	}
}

// A cursor is a seq under a generation. Above the head, under another
// generation, or with no generation at all, it is refused with the head to
// re-list from, on every door alike, before a byte of stream is written.
func TestResumeCursorIsHeldToTheHead(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	head := seedPeople(ds, 3)

	for name, prefix := range resumeEntryPoints {
		for _, query := range []string{
			"from=4&generation=" + ds.generation, // above the head
			"from=2&generation=some-other-history",
			"from=2", // no generation: unverifiable
			"from=0&generation=some-other-history",
		} {
			status, got := resumeRefusal(t, srv, prefix+query, tok)
			if status != http.StatusGone {
				t.Fatalf("%s %s: status %d, want 410", name, query, status)
			}
			wantReset(t, status, got, head, ds.generation)
		}
	}
}

// Seq 0 is the start of every history and names no entry, so it needs no
// generation: `from=0` reads everything, on the page and on the stream.
func TestFromZeroNeedsNoGeneration(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	seedPeople(ds, 2)

	rec := env.do(t, http.MethodGet, "/api/v1/changes?from=0", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	lines := 0
	sc := bufio.NewScanner(rec.Body)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			lines++
		}
	}
	if lines != 3 {
		t.Fatalf("got %d ndjson lines, want bookmark + 2 changes", lines)
	}
	wantNotRefused(t, env, peoplePath+"?watch=1&from=0", tok)
}

// The ticket's case: head 7, a client saves cursor 5, an OLDER copy of the
// directory (head 3) is imported over an emptied database. The resume is
// refused with the new head and generation; the client re-lists there and
// from then on misses nothing, including the writes that re-fill seqs 4 and 5.
func TestRestoredHistoryResetsTheCursorAndLosesNothing(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	seedPeople(ds, 7)
	saved, savedGeneration := int64(5), ds.generation

	ds.restore(3)

	status, reset := resumeRefusal(t, srv, fmt.Sprintf("/api/v1/changes?watch=1&from=%d&generation=%s", saved, savedGeneration), tok)
	wantReset(t, status, reset, 3, ds.generation)

	br, stop := startWatch(t, srv, fmt.Sprintf("/api/v1/changes?watch=1&from=%d&generation=%s", *reset.Head, reset.Generation), tok)
	defer stop()
	if bm := readLine(t, br); bm["bookmark"] != float64(3) {
		t.Fatalf("bookmark = %v, want the restored head 3", bm)
	}
	for i := range 4 {
		ds.commit(substrate.Change{
			TS: time.Unix(int64(100+i), 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
			RecordID: "r" + strconv.Itoa(i+1), Kind: "samples.substrate.reamde.dev/people/person",
		})
	}
	for want := int64(4); want <= 7; want++ {
		if got := readLine(t, br); got["seq"] != float64(want) || got["recordId"] != "r"+strconv.FormatInt(want-3, 10) {
			t.Fatalf("replacement write = %v, want seq %d", got, want)
		}
	}
	// The old cursor stays refused: nothing about the history growing past
	// seq 5 makes a cursor from another generation resumable.
	status, reset = resumeRefusal(t, srv, fmt.Sprintf("/api/v1/changes?from=%d&generation=%s", saved, savedGeneration), tok)
	wantReset(t, status, reset, 7, ds.generation)
}

// A `before` continuation is a position in one history too: a client that
// fetched a page, then had an older directory restored under it, must not
// walk on through the replacement's rows. `before=0` is the head of whatever
// history is there and needs no generation.
func TestHistoryContinuationIsHeldToTheGeneration(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	head := seedPeople(ds, 3)

	for _, query := range []string{
		"first=2&before=3",
		"first=2&before=3&generation=some-other-history",
		"first=2&generation=some-other-history",
		"first=2&before=4&generation=" + ds.generation,  // above the head
		"first=2&before=-1&generation=" + ds.generation, // below every horizon
	} {
		status, got := resumeRefusal(t, srv, "/api/v1/changes?"+query, tok)
		if status != http.StatusGone {
			t.Fatalf("%s: status %d, want 410", query, status)
		}
		wantReset(t, status, got, head, ds.generation)
	}
	for _, query := range []string{
		"first=2",
		"first=2&before=0",
		"first=2&before=3&generation=" + ds.generation,
	} {
		rec := env.do(t, http.MethodGet, "/api/v1/changes?"+query, tok, nil)
		wantStatus(t, rec, http.StatusOK)
	}
}

// The list envelope and the history page carry the generation beside the
// head, so a client can hand either straight to `watch?from=&generation=`.
func TestListAndHistoryCarryTheHandoff(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	head := seedPeople(ds, 2)

	rec := env.do(t, http.MethodGet, peoplePath, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[map[string]any](t, rec)
	if page["head"] != float64(head) || page["generation"] != ds.generation {
		t.Fatalf("list envelope head/generation = %v/%v, want %d/%q", page["head"], page["generation"], head, ds.generation)
	}

	rec = env.do(t, http.MethodGet, "/api/v1/changes?first=1", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	history := decodeJSON[map[string]any](t, rec)
	if history["head"] != float64(head) || history["generation"] != ds.generation {
		t.Fatalf("history page head/generation = %v/%v, want %d/%q", history["head"], history["generation"], head, ds.generation)
	}
}
