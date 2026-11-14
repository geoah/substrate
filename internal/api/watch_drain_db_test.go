package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// watchDrainEnv is the engine behind a live server whose Draining channel the
// test closes, standing in for http.Server.Shutdown's RegisterOnShutdown hook
// in cmd/substrated.
func watchDrainEnv(t *testing.T) (*dbEnv, chan struct{}, *httptest.Server) {
	t.Helper()
	draining := make(chan struct{})
	env := newDBEnv(t, withSamples("people"), withConfig(func(c *Config) { c.Draining = draining }))
	srv := httptest.NewServer(env.h)
	t.Cleanup(srv.Close)
	return env, draining, srv
}

// watchDrainBacklog writes n people as the engine's own hand, each with a
// details value of size bytes, which a change row read with `values=1`
// carries.
func watchDrainBacklog(t *testing.T, env *dbEnv, n, size int) {
	t.Helper()
	details := strings.Repeat("x", size)
	for i := range n {
		env.put(t, personKind, fmt.Sprintf("b%d", i), map[string]any{"name": "Backlog", "details": details})
	}
}

// A watch never ends on its own, and http.Server.Shutdown waits for every
// open request, so a server with a console tab open once spent its whole
// shutdown budget on the stream and exited 1. When the server starts to
// drain, both watch routes flush what they hold and end cleanly, with no
// error frame, and the client resumes from the last seq it read.
func TestWatchStreamEndsWhenTheServerDrains(t *testing.T) {
	t.Parallel()
	for name, path := range map[string]string{"changes": changesPath + "?watch=1", "records": peopleRecords + "&watch=1"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env, draining, srv := watchDrainEnv(t)

			br, stop := startWatch(t, srv, path, env.token)
			defer stop()
			bm := readLine(t, br)
			if bm["bookmark"] == nil {
				t.Fatalf("first line = %v, want the bookmark", bm)
			}
			env.put(t, personKind, "c1", map[string]any{"name": "Ada"})
			if got := readLine(t, br); got["seq"] != bm["bookmark"].(float64)+1 || got["recordId"] != "c1" {
				t.Fatalf("change = %v, want c1 just past the bookmark %v", got, bm["bookmark"])
			}

			close(draining)
			if rest := readToEOF(t, br, time.Second); strings.Contains(rest, `"error"`) {
				t.Fatalf("a drained stream ended with an error frame: %q", rest)
			}
		})
	}
}

// A client far behind gets one batch, not its whole backlog, once the server
// is draining: the stream stops between batches, so the shutdown is not held
// for as long as the backlog takes to send.
func TestWatchStreamStopsItsBacklogBetweenBatchesWhenTheServerDrains(t *testing.T) {
	t.Parallel()
	env, draining, srv := watchDrainEnv(t)
	backlog := changeBatch + 10
	watchDrainBacklog(t, env, backlog, 0)
	close(draining)

	br, stop := startWatch(t, srv, changesPath+"?watch=1&from=0&kinds="+personKind, env.token)
	defer stop()
	if bm := readLine(t, br); bm["bookmark"] != float64(0) {
		t.Fatalf("first line = %v, want the bookmark at 0", bm)
	}
	rest := readToEOF(t, br, 5*time.Second)
	if rows := strings.Count(rest, "\n"); rows != changeBatch {
		t.Fatalf("a drained stream sent %d rows of a %d-row backlog, want one batch of %d", rows, backlog, changeBatch)
	}
}

// A client that stops reading must not hold its handler for good: each
// frame has streamWriteDeadline to reach it, and a frame that does not ends
// the stream. The backlog, read with its values, is large enough to fill the
// socket buffers of a client that never reads.
//
// Serial: it shortens the package's streamWriteDeadline.
func TestWatchStreamEndsWhenItsClientStopsReading(t *testing.T) {
	prev := streamWriteDeadline
	streamWriteDeadline = 200 * time.Millisecond
	t.Cleanup(func() { streamWriteDeadline = prev })

	env := newDBEnv(t, withSamples("people"))
	watchDrainBacklog(t, env, 32, 1<<20)
	returned := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)
		env.h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+changesPath+"?watch=1&from=0&values=1&kinds="+personKind, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+env.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	// Held open and never read until the handler has returned.
	defer resp.Body.Close()

	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("the handler was still writing to a client that stopped reading 10s after its write deadline")
	}
}

// The deadline is per frame, not per stream: a stream that lives far longer
// than streamWriteDeadline while its client keeps reading stays open, frame
// after frame.
//
// Serial: it shortens the package's streamWriteDeadline.
func TestWatchStreamOutlivesItsWriteDeadlineWhileTheClientReads(t *testing.T) {
	prev := streamWriteDeadline
	streamWriteDeadline = 200 * time.Millisecond
	t.Cleanup(func() { streamWriteDeadline = prev })

	env, _, srv := watchDrainEnv(t)
	br, stop := startWatch(t, srv, changesPath+"?watch=1", env.token)
	defer stop()
	bm := readLine(t, br)
	if bm["bookmark"] == nil {
		t.Fatalf("first line = %v, want the bookmark", bm)
	}
	start := time.Now()
	for i := range 6 {
		time.Sleep(100 * time.Millisecond)
		env.put(t, personKind, fmt.Sprintf("c%d", i+1), map[string]any{"name": "Ada"})
		want := bm["bookmark"].(float64) + float64(i+1)
		if got := readLine(t, br); got["seq"] != want {
			t.Fatalf("change %d = %v, want seq %v: the stream ended after %s", i+1, got, want, time.Since(start))
		}
	}
}
