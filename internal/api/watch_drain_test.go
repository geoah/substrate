package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// drainEnv is a router whose Draining channel the test closes, standing in
// for http.Server.Shutdown's RegisterOnShutdown hook in cmd/substrated.
func drainEnv(t *testing.T) (*fakeService, chan struct{}, *httptest.Server) {
	t.Helper()
	svc := newFakeService()
	draining := make(chan struct{})
	srv := httptest.NewServer(New(Config{Service: svc, Now: (&testClock{}).now, Draining: draining}))
	t.Cleanup(srv.Close)
	return svc, draining, srv
}

// readToEOF reads what is left of a stream and fails the test unless the body
// ends cleanly within d: a shutdown that waited on the stream would hold it
// open, and an aborted one would end in an unexpected EOF.
func readToEOF(t *testing.T, br *bufio.Reader, d time.Duration) string {
	t.Helper()
	type result struct {
		rest []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		rest, err := io.ReadAll(br)
		ch <- result{rest, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("the stream ended with %v after %q, want a clean end", r.err, r.rest)
		}
		return string(r.rest)
	case <-time.After(d):
		t.Fatalf("the stream was still open %s after the server started to drain", d)
		return ""
	}
}

// addChanges appends n changes without waking the watchers, so a stream that
// starts afterwards finds them all as one backlog.
func addChanges(ds *fakeDataset, n int, recordID string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	for range n {
		ds.changes = append(ds.changes, substrate.Change{
			Seq: int64(len(ds.changes) + 1), TS: time.Unix(5, 0).UTC(), Actor: substrate.ActorAPI,
			Op: substrate.OpPut, RecordID: recordID, Kind: personKind,
		})
	}
}

// A watch never ends on its own, and http.Server.Shutdown waits for every
// open request, so a server with a console tab open once spent its whole
// shutdown budget on the stream and exited 1. When the server starts to
// drain, both watch routes flush what they hold and end cleanly, with no
// error frame, and the client resumes from the last seq it read.
func TestWatchStreamEndsWhenTheServerDrains(t *testing.T) {
	for name, path := range map[string]string{"changes": changesPath + "?watch=1", "records": peopleRecords + "&watch=1"} {
		t.Run(name, func(t *testing.T) {
			svc, draining, srv := drainEnv(t)
			tok := svc.token(fakeRepository)
			ds := svc.datasets[fakeRepository]

			br, stop := startWatch(t, srv, path, tok)
			defer stop()
			if bm := readLine(t, br); bm["bookmark"] == nil {
				t.Fatalf("first line = %v, want the bookmark", bm)
			}
			ds.commit(substrate.Change{TS: time.Unix(5, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut, RecordID: "c1", Kind: personKind})
			if got := readLine(t, br); got["seq"] != float64(1) {
				t.Fatalf("change = %v, want seq 1", got)
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
	svc, draining, srv := drainEnv(t)
	tok := svc.token(fakeRepository)
	ds := svc.datasets[fakeRepository]
	addChanges(ds, 2*changeBatch+10, "c1")
	close(draining)

	br, stop := startWatch(t, srv, changesPath+"?watch=1&from=0", tok)
	defer stop()
	if bm := readLine(t, br); bm["bookmark"] != float64(0) {
		t.Fatalf("first line = %v, want the bookmark at 0", bm)
	}
	rest := readToEOF(t, br, 5*time.Second)
	if rows := strings.Count(rest, "\n"); rows != changeBatch {
		t.Fatalf("a drained stream sent %d rows of a %d-row backlog, want one batch of %d", rows, 2*changeBatch+10, changeBatch)
	}
}

// The export is the other request that never ends on the server's schedule:
// once the server drains, its next write fails and the response is aborted,
// which the client reads as a cut archive, and the log says why at WARN
// rather than as a fault.
func TestExportIsCutWhenTheServerDrains(t *testing.T) {
	base := newFakeService()
	ds := &exportingDataset{fakeDataset: base.datasets[fakeRepository]}
	draining := make(chan struct{})
	close(draining)
	srv := httptest.NewServer(New(Config{Service: &exportingService{fakeService: base, ds: ds}, Draining: draining}))
	defer srv.Close()
	logs := captureLogs(t)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/export", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+base.token(fakeRepository))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("the headers must arrive before the cut: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read = %v after %d bytes, want the unexpected EOF of an aborted response", err, len(body))
	}
	warns := logs.at(slog.LevelWarn)
	if len(warns) != 1 || warns[0].msg != "export aborted mid-stream: the server is shutting down" {
		t.Fatalf("WARN lines = %+v, want the one naming the shutdown", warns)
	}
	if errs := logs.at(slog.LevelError); len(errs) != 0 {
		t.Fatalf("a shutdown cut was logged as a fault: %+v", errs)
	}
}

// A client that stops reading must not hold its handler for good: each
// frame has streamWriteDeadline to reach it, and a frame that does not ends
// the stream. The backlog is large enough to fill the socket buffers of a
// client that never reads.
func TestWatchStreamEndsWhenItsClientStopsReading(t *testing.T) {
	prev := streamWriteDeadline
	streamWriteDeadline = 200 * time.Millisecond
	t.Cleanup(func() { streamWriteDeadline = prev })

	svc := newFakeService()
	tok := svc.token(fakeRepository)
	addChanges(svc.datasets[fakeRepository], 4*changeBatch, strings.Repeat("x", 16<<10))
	h := New(Config{Service: svc, Now: (&testClock{}).now})
	returned := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+changesPath+"?watch=1&from=0", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
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

// bigExport writes far more than a client's socket buffers hold, so a client
// that stops reading stalls its writer.
type bigExport struct{ point substrate.ExportPoint }

func (e bigExport) Point() substrate.ExportPoint { return e.point }

func (e bigExport) WriteTo(w io.Writer) (int64, error) {
	chunk := make([]byte, 64<<10)
	var n int64
	for range 1024 {
		m, err := w.Write(chunk)
		n += int64(m)
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

type bigExportDataset struct{ *fakeDataset }

func (d bigExportDataset) Export(context.Context) (substrate.Export, error) {
	return bigExport{point: substrate.ExportPoint{Authority: d.repository.Authority, Head: 1}}, nil
}

// An export whose client stops reading is cut once one write has waited
// streamWriteDeadline, rather than holding its handler and its export slot
// for good, and the line it leaves is a WARN about the client, not a fault.
func TestExportIsCutWhenItsClientStopsReading(t *testing.T) {
	prev := streamWriteDeadline
	streamWriteDeadline = 200 * time.Millisecond
	t.Cleanup(func() { streamWriteDeadline = prev })

	base := newFakeService()
	base.wrap = func(d *fakeDataset) substrate.Dataset { return bigExportDataset{d} }
	h := New(Config{Service: base})
	logs := captureLogs(t)
	returned := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)
		defer func() { _ = recover() }() // the abort, which net/http would take
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/export", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+base.token(fakeRepository))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	// Held open and never read until the handler has returned.
	defer resp.Body.Close()

	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("the export was still writing to a client that stopped reading 10s after its write deadline")
	}
	warns := logs.at(slog.LevelWarn)
	if len(warns) != 1 || warns[0].msg != "export aborted mid-stream: the client stopped reading" {
		t.Fatalf("WARN lines = %+v, want the one naming the stalled client (debug %+v, error %+v)", warns, logs.at(slog.LevelDebug), logs.at(slog.LevelError))
	}
	if errs := logs.at(slog.LevelError); len(errs) != 0 {
		t.Fatalf("a stalled client was logged as a fault: %+v", errs)
	}
}

// The deadline is per frame, not per stream: a stream that lives far longer
// than streamWriteDeadline while its client keeps reading stays open, frame
// after frame.
func TestWatchStreamOutlivesItsWriteDeadlineWhileTheClientReads(t *testing.T) {
	prev := streamWriteDeadline
	streamWriteDeadline = 200 * time.Millisecond
	t.Cleanup(func() { streamWriteDeadline = prev })

	svc, _, srv := drainEnv(t)
	tok := svc.token(fakeRepository)
	ds := svc.datasets[fakeRepository]
	br, stop := startWatch(t, srv, changesPath+"?watch=1", tok)
	defer stop()
	if bm := readLine(t, br); bm["bookmark"] == nil {
		t.Fatalf("first line = %v, want the bookmark", bm)
	}
	for i := range 6 {
		time.Sleep(100 * time.Millisecond)
		ds.commit(substrate.Change{TS: time.Unix(5, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut, RecordID: "c1", Kind: personKind})
		if got := readLine(t, br); got["seq"] != float64(i+1) {
			t.Fatalf("change %d = %v, want seq %d: the stream ended after %s", i+1, got, i+1, time.Duration(i+1)*100*time.Millisecond)
		}
	}
}

// slowExport writes its archive in pieces spread over more than one write
// deadline, each piece well inside it.
type slowExport struct{ point substrate.ExportPoint }

func (e slowExport) Point() substrate.ExportPoint { return e.point }

func (e slowExport) WriteTo(w io.Writer) (int64, error) {
	var n int64
	for range 6 {
		time.Sleep(100 * time.Millisecond)
		m, err := w.Write([]byte("piece\n"))
		n += int64(m)
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

type slowExportDataset struct{ *fakeDataset }

func (d slowExportDataset) Export(context.Context) (substrate.Export, error) {
	return slowExport{point: substrate.ExportPoint{Authority: d.repository.Authority, Head: 1}}, nil
}

// The export's deadline is per write too: an archive that takes longer than
// streamWriteDeadline to produce, read by a client that keeps up, arrives
// whole.
func TestExportOutlivesItsWriteDeadlineWhileTheClientReads(t *testing.T) {
	prev := streamWriteDeadline
	streamWriteDeadline = 200 * time.Millisecond
	t.Cleanup(func() { streamWriteDeadline = prev })

	base := newFakeService()
	base.wrap = func(d *fakeDataset) substrate.Dataset { return slowExportDataset{d} }
	srv := httptest.NewServer(New(Config{Service: base}))
	defer srv.Close()
	logs := captureLogs(t)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/export", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+base.token(fakeRepository))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || strings.Count(string(body), "piece\n") != 6 {
		t.Fatalf("read %q, %v; want all six pieces and a clean end", body, err)
	}
	if lines := append(logs.at(slog.LevelWarn), logs.at(slog.LevelError)...); len(lines) != 0 {
		t.Fatalf("an export that kept up was logged: %+v", lines)
	}
}
