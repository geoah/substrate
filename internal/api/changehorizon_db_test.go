package api

// A forgetting that lands while a reader of the changes holds a position:
// between two batches of an open stream, or between a forward read's check of
// its cursor and its read. The batch after it is read with the horizon in one
// snapshot (engine changehorizon.go), so a reader below the new horizon is
// refused `compacted`, naming the head, and never served the rows past its
// cursor without the deletion that was forgotten; a reader at or above it
// reads on.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// forgetting has the pass forget every purged record: deletions are kept an
// hour, and the pass's clock reads two hours on.
func forgetting() dbOption {
	return withEngine(engine.WithCompactionInterval(-1), engine.WithDeletionsKept(time.Hour),
		engine.WithTestCompactionClock(func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }))
}

// forget runs the compaction pass, which forgets the one purged record.
func forget(t *testing.T, env *dbEnv) {
	t.Helper()
	report, err := env.ds.(interface {
		CompactionPass(context.Context) (engine.CompactionPassReport, error)
	}).CompactionPass(context.Background())
	if err != nil || report.Forgotten != 1 {
		t.Errorf("the pass forgot %d records (%v), want the purged one", report.Forgotten, err)
	}
}

// writeAndPurge writes a person, purges it and writes one more, so the
// purge is never the head, which a forgetting leaves alone.
func writeAndPurge(t *testing.T, env *dbEnv) {
	t.Helper()
	env.put(t, personKind, "gone", map[string]any{"name": "Gone"})
	if _, err := env.ds.Delete(context.Background(), substrate.ActorSystem, personKind, "gone", substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	env.put(t, personKind, "after", map[string]any{"name": "After"})
}

// changesHold runs a test's step inside the next read of the changes after
// a position, before the read reaches the engine: the seam for a forgetting
// that lands while a reader is between two batches, or between its cursor's
// check and its read, which no request can time.
type changesHold struct {
	mu    sync.Mutex
	after int64
	step  func()
}

func (h *changesHold) before(after int64, step func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.after, h.step = after, step
}

func (h *changesHold) wrap(ds substrate.Dataset) substrate.Dataset {
	return heldChanges{Dataset: ds, hold: h}
}

type heldChanges struct {
	substrate.Dataset
	hold *changesHold
}

func (d heldChanges) Changes(ctx context.Context, after int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error) {
	d.hold.mu.Lock()
	step := d.hold.step
	if step != nil && after == d.hold.after {
		d.hold.step = nil
	} else {
		step = nil
	}
	d.hold.mu.Unlock()
	if step != nil {
		step()
	}
	return d.Dataset.Changes(ctx, after, f, limit)
}

// compactedFrame reads a terminal frame: the refusal and the head it names.
func compactedFrame(t *testing.T, frame map[string]any) (substrate.ErrorPayload, bool) {
	t.Helper()
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	var env substrate.ErrorEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return env.Error, env.Error.Code == codeCompacted
}

// A stream held between two batches while a forgetting raises the horizon
// above its cursor: its next frame is the refusal naming the head, and the
// stream ends. The reader recovers as the console's rule has it
// (web/console use-live-records.test.tsx, "recovers a stream a forgetting
// overtook"): it lists again from the first page, which holds no forgotten
// record, and follows from the list's head. A stream above the new
// horizon is not disturbed.
func TestAStreamAForgettingOvertakesEndsCompacted(t *testing.T) {
	t.Parallel()
	hold := &changesHold{}
	env := newDBEnv(t, withSamples("people"), forgetting())
	env.wrap = hold.wrap
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	watch := "/api/v1/changes?watch=1&kinds=" + personKind

	br, stop := startWatch(t, srv, watch, env.token)
	defer stop()
	if bm := readLine(t, br); bm["bookmark"] == nil {
		t.Fatalf("first line = %v, want a bookmark", bm)
	}
	env.put(t, personKind, "c1", map[string]any{"name": "Ada"})
	first := readLine(t, br)
	if first["recordId"] != "c1" {
		t.Fatalf("first batch = %v, want c1", first)
	}
	cursor := int64(first["seq"].(float64))

	// The stream's next read waits for the forgetting.
	held, release := make(chan struct{}), make(chan struct{})
	hold.before(cursor, func() {
		close(held)
		<-release
	})
	env.put(t, personKind, "c2", map[string]any{"name": "Grace"})
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream read nothing after its first batch")
	}
	writeAndPurge(t, env)
	// A stream that holds the head: above any horizon the forgetting moves.
	above, stopAbove := startWatch(t, srv, watch, env.token)
	defer stopAbove()
	if bm := readLine(t, above); bm["bookmark"] == nil {
		t.Fatalf("the stream above: first line = %v, want a bookmark", bm)
	}
	forget(t, env)
	head := changesHead(t, env)
	if head.Horizon <= cursor {
		t.Fatalf("the horizon is %d, want one above the stream's cursor %d", head.Horizon, cursor)
	}
	close(release)

	frame := readLine(t, br)
	if p, ok := compactedFrame(t, frame); !ok || p.Head == nil || *p.Head != head.Seq || p.Generation != head.Generation {
		t.Fatalf("the frame after the forgetting = %v, want the compacted refusal naming head %d under %q", frame, head.Seq, head.Generation)
	}
	if line, err := br.ReadBytes('\n'); err == nil {
		t.Fatalf("the stream went on after its refusal: %s", line)
	}

	// The recovery: the first page again, then a stream from the head.
	rec := env.do(t, http.MethodGet, peopleRecords, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	var ids []string
	for _, r := range page.Records {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	if got := strings.Join(ids, ","); got != "after,c1,c2" || page.Head != head.Seq {
		t.Fatalf("the first page lists %s at head %d, want after,c1,c2 at %d", got, page.Head, head.Seq)
	}
	again, stopAgain := startWatch(t, srv, fmt.Sprintf("%s&from=%d&generation=%s", watch, page.Head, page.Generation), env.token)
	defer stopAgain()
	if bm := readLine(t, again); bm["bookmark"] != float64(head.Seq) {
		t.Fatalf("the stream from the list's head: bookmark %v, want %d", bm, head.Seq)
	}
	env.put(t, personKind, "later", map[string]any{"name": "Later"})
	if got := readLine(t, again); got["recordId"] != "later" {
		t.Fatalf("the stream from the head read %v, want later", got)
	}
	if got := readLine(t, above); got["recordId"] != "later" {
		t.Fatalf("the stream above the horizon read %v, want later", got)
	}
}

// The same between two pages of a forward read: a forgetting that lands
// after the second page's cursor was held to the horizon and before its rows
// are read is answered 410 compacted with the head, never the page without
// the deletion; a page from the horizon on reads on.
func TestAForwardPageAForgettingOvertakesIsToldCompacted(t *testing.T) {
	t.Parallel()
	hold := &changesHold{}
	env := newDBEnv(t, withSamples("people"), forgetting())
	env.wrap = hold.wrap
	pages := "/api/v1/changes?kinds=" + personKind
	env.put(t, personKind, "c1", map[string]any{"name": "Ada"})

	lines := func(rec *httptest.ResponseRecorder) []map[string]any {
		t.Helper()
		var out []map[string]any
		sc := bufio.NewScanner(rec.Body)
		for sc.Scan() {
			var line map[string]any
			if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
				t.Fatalf("decode %q: %v", sc.Bytes(), err)
			}
			out = append(out, line)
		}
		return out
	}
	rec := env.do(t, http.MethodGet, pages, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	first := lines(rec)
	if len(first) != 2 || first[1]["recordId"] != "c1" {
		t.Fatalf("the first page = %v, want its bookmark and c1", first)
	}
	cursor, generation := int64(first[1]["seq"].(float64)), first[0]["generation"].(string)

	writeAndPurge(t, env)
	hold.before(cursor, func() { forget(t, env) })
	rec = env.do(t, http.MethodGet, fmt.Sprintf("%s&from=%d&generation=%s", pages, cursor, generation), env.token, nil)
	head := changesHead(t, env)
	if head.Horizon <= cursor {
		t.Fatalf("the horizon is %d, want one above the page's cursor %d", head.Horizon, cursor)
	}
	var refusal substrate.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &refusal); rec.Code != http.StatusGone || err != nil ||
		refusal.Error.Code != codeCompacted || refusal.Error.Head == nil || *refusal.Error.Head != head.Seq {
		t.Fatalf("the second page: %d %s, want 410 compacted naming head %d", rec.Code, rec.Body.String(), head.Seq)
	}

	rec = env.do(t, http.MethodGet, fmt.Sprintf("%s&from=%d&generation=%s", pages, head.Horizon, head.Generation), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := lines(rec); len(got) != 2 || got[1]["recordId"] != "after" {
		t.Fatalf("a page from the horizon = %v, want its bookmark and after", got)
	}
}
