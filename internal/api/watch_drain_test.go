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

// The export is the other request that never ends on the server's schedule:
// once the server drains, its next write fails and the response is aborted,
// which the client reads as a cut archive, and the log says why at WARN
// rather than as a fault.
func TestExportIsCutWhenTheServerDrains(t *testing.T) {
	draining := make(chan struct{})
	close(draining)
	srv := httptest.NewServer(newStubEnv(t, &exportingDataset{}, func(c *Config) { c.Draining = draining }).h)
	defer srv.Close()
	logs := captureLogs(t)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/export", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+stubToken)
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

// bigExportDataset is a repository whose export is bigExport: an archive
// larger than a client's socket buffers, which the engine would have to be
// filled with that many blob bytes to produce.
type bigExportDataset struct{ stubDataset }

func (d bigExportDataset) Export(context.Context) (substrate.Export, error) {
	return bigExport{point: substrate.ExportPoint{Authority: d.Repository().Authority, Head: 1}}, nil
}

// An export whose client stops reading is cut once one write has waited
// streamWriteDeadline, rather than holding its handler and its export slot
// for good, and the line it leaves is a WARN about the client, not a fault.
func TestExportIsCutWhenItsClientStopsReading(t *testing.T) {
	prev := streamWriteDeadline
	streamWriteDeadline = 200 * time.Millisecond
	t.Cleanup(func() { streamWriteDeadline = prev })

	h := newStubEnv(t, bigExportDataset{}).h
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
	req.Header.Set("Authorization", "Bearer "+stubToken)
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

// slowExportDataset is a repository whose export is slowExport: an archive
// produced slower than one write deadline, which the engine cannot be made to
// produce on demand.
type slowExportDataset struct{ stubDataset }

func (d slowExportDataset) Export(context.Context) (substrate.Export, error) {
	return slowExport{point: substrate.ExportPoint{Authority: d.Repository().Authority, Head: 1}}, nil
}

// The export's deadline is per write too: an archive that takes longer than
// streamWriteDeadline to produce, read by a client that keeps up, arrives
// whole.
func TestExportOutlivesItsWriteDeadlineWhileTheClientReads(t *testing.T) {
	prev := streamWriteDeadline
	streamWriteDeadline = 200 * time.Millisecond
	t.Cleanup(func() { streamWriteDeadline = prev })

	srv := httptest.NewServer(newStubEnv(t, slowExportDataset{}).h)
	defer srv.Close()
	logs := captureLogs(t)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/export", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+stubToken)
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
