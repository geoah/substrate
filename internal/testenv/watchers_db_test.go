package testenv_test

// A hundred clients watching one repository cost one read of the changelog
// per change: the repository's one reader of the tail answers every stream
// from memory, over a real socket and the real engine.

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/testenv"
)

func TestAHundredWatchersCostOneReadPerChange(t *testing.T) {
	var reads atomic.Int64
	e := testenv.Start(t, testenv.WithRepository(detailsAuthority),
		testenv.WithEngineOptions(engine.WithTestChangeReadHook(func(string) { reads.Add(1) })))
	e.ApplyVocabularyYAML(detailsVocabulary("string"))
	put := func(n int) {
		t.Helper()
		if status, body := e.Do(http.MethodPut, detailNotesPath+"/"+noteID(n),
			map[string]any{"properties": map[string]any{"name": noteID(n)}}); status != http.StatusCreated {
			t.Fatalf("put %d: %d %s", n, status, body)
		}
	}
	put(1)

	const watchers = 100
	filter, _ := json.Marshal(map[string]any{"kinds": []string{detailsRef + "/note"}})
	path := "/api/v1/records?watch=1&filter=" + url.QueryEscape(string(filter))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Each stream reports every record id it delivers.
	seen := make([]chan string, watchers)
	var opened sync.WaitGroup
	for i := range watchers {
		seen[i] = make(chan string, 8)
		resp := e.Request(ctx, http.MethodGet, path, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("open watch %d: %d", i, resp.StatusCode)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		opened.Add(1)
		go func(out chan<- string) {
			sc := bufio.NewScanner(resp.Body)
			sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
			if sc.Scan() {
				opened.Done()
			}
			for sc.Scan() {
				var row struct {
					Seq      int64  `json:"seq"`
					RecordID string `json:"recordId"`
				}
				if json.Unmarshal(sc.Bytes(), &row) == nil && row.Seq > 0 {
					out <- row.RecordID
				}
			}
		}(seen[i])
	}
	opened.Wait()

	waitAll := func(id string) {
		t.Helper()
		for i := range watchers {
			select {
			case got := <-seen[i]:
				if got != id {
					t.Fatalf("watch %d delivered %s, want %s", i, got, id)
				}
			case <-ctx.Done():
				t.Fatalf("watch %d never delivered %s", i, id)
			}
		}
	}
	// The first change after the streams open settles their opening reads.
	put(2)
	waitAll(noteID(2))

	reads.Store(0)
	put(3)
	waitAll(noteID(3))
	if got := reads.Load(); got != 1 {
		t.Fatalf("%d watchers cost %d reads of the changelog for one change, want 1", watchers, got)
	}
}

// A write whose commit answer was lost after it landed reaches an open
// stream with no other write after it: the stream had drained everything
// before, and the lost answer signaled nothing.
func TestAStreamReceivesAWriteWhoseCommitAnswerWasLost(t *testing.T) {
	var lose atomic.Bool
	e := testenv.Start(t, testenv.WithRepository(detailsAuthority),
		testenv.WithEngineOptions(engine.WithTestCommitInDoubt(lose.Load)))
	e.ApplyVocabularyYAML(detailsVocabulary("string"))
	filter, _ := json.Marshal(map[string]any{"kinds": []string{detailsRef + "/note"}})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp := e.Request(ctx, http.MethodGet, "/api/v1/records?watch=1&filter="+url.QueryEscape(string(filter)), nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open the watch: %d", resp.StatusCode)
	}
	defer func() { _ = resp.Body.Close() }()
	ids := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for sc.Scan() {
			var row struct {
				Seq      int64  `json:"seq"`
				RecordID string `json:"recordId"`
			}
			if json.Unmarshal(sc.Bytes(), &row) == nil && row.Seq > 0 {
				ids <- row.RecordID
			}
		}
	}()
	want := func(id string) {
		t.Helper()
		select {
		case got := <-ids:
			if got != id {
				t.Fatalf("the stream delivered %s, want %s", got, id)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("the stream never delivered %s", id)
		}
	}
	put := func(n int) int {
		t.Helper()
		status, _ := e.Do(http.MethodPut, detailNotesPath+"/"+noteID(n),
			map[string]any{"properties": map[string]any{"name": noteID(n)}})
		return status
	}
	if status := put(1); status != http.StatusCreated {
		t.Fatalf("put: %d", status)
	}
	want(noteID(1))

	lose.Store(true)
	status := put(2)
	lose.Store(false)
	if status != http.StatusInternalServerError {
		t.Fatalf("the put whose answer was lost answered %d, want 500", status)
	}
	want(noteID(2))
}
