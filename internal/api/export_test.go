package api

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// exportingDataset is a repository whose export is cannedExport, or a
// refusal: the faults an export meets before its first byte (exportErr) and
// after it (exportFailMidway), which the engine cannot be made to produce on
// demand. What the engine's own archive holds is export_db_test.go's.
type exportingDataset struct {
	stubDataset
	// exportErr refuses Export before a point is pinned; exportFailMidway
	// makes the stream fail after its first entry.
	exportErr        error
	exportFailMidway bool
}

// cannedExport is a two-entry archive: the manifest and snapshot.json, in the
// order the engine writes them. Midway failure stops after the first.
type cannedExport struct {
	ds    *exportingDataset
	point substrate.ExportPoint
}

func (d *exportingDataset) Export(context.Context) (substrate.Export, error) {
	if d.exportErr != nil {
		return nil, d.exportErr
	}
	return &cannedExport{ds: d, point: substrate.ExportPoint{
		Authority: d.Repository().Authority, Head: 42, HeadHash: strings.Repeat("ab", 32),
		TakenAt: time.Unix(1_700_000_000, 0).UTC(), Pieces: 1, SealedFiles: 2,
	}}, nil
}

func (e *cannedExport) Point() substrate.ExportPoint { return e.point }

func (e *cannedExport) Close() {}

func (e *cannedExport) WriteTo(w io.Writer) (int64, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	root := "repositories/" + e.point.Authority + "/"
	write := func(name, body string) {
		_ = tw.WriteHeader(&tar.Header{Name: root + name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	write("repository.json", `{"format":2}`+"\n")
	if e.ds.exportFailMidway {
		_ = tw.Flush()
		n, _ := w.Write(buf.Bytes())
		return int64(n), errors.New("the blob store went away")
	}
	write("snapshot.json", fmt.Sprintf(`{"format":1,"head":%d}`, e.point.Head)+"\n")
	_ = tw.Close()
	n, err := w.Write(buf.Bytes())
	return int64(n), err
}

// exportEnv is a stub env whose one repository exports.
func exportEnv(t *testing.T) (*testEnv, *exportingDataset, string) {
	t.Helper()
	ds := &exportingDataset{}
	return newStubEnv(t, ds), ds, stubToken
}

// No token, no archive: the route sits behind the ordinary bearer check.
func TestExportSitsBehindTheBearerCheck(t *testing.T) {
	env, _, _ := exportEnv(t)
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/export", "", nil), http.StatusUnauthorized, codeAuth)
}

// A refusal before the first byte is an ordinary error body with its status,
// not half an archive.
func TestExportRefusesWithAStatusBeforeTheFirstByte(t *testing.T) {
	env, ds, tok := exportEnv(t)
	ds.exportErr = fmt.Errorf("%w: the directory is behind the tables", substrate.ErrUnavailable)
	rec := env.do(t, http.MethodGet, "/api/v1/export", tok, nil)
	wantErrorCode(t, rec, http.StatusServiceUnavailable, codeUnavailable)
	if cd := rec.Header().Get("Content-Disposition"); cd != "" {
		t.Fatalf("a refused export still named a file: %q", cd)
	}
}

// A failure after the first byte cannot change the status, so the response is
// aborted: the client reads an unexpected end of the body, and the bytes it
// did get hold no snapshot.json. The request goes through the real router,
// with recoverer mounted, because that middleware is what could swallow the
// abort: it does not, it re-panics http.ErrAbortHandler, and the net/http
// server then closes the connection without the terminating chunk.
func TestExportAbortsTheResponseWhenTheStreamFailsMidway(t *testing.T) {
	env, ds, tok := exportEnv(t)
	ds.exportFailMidway = true
	// env.h is New(Config{...}): the router with peerAddress, requestID and
	// recoverer, not the bare handler.
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/export", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("the response headers must arrive before the failure: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err == nil {
		t.Fatalf("the body ended cleanly after a mid-stream failure: %d bytes", len(body))
	}
	// The transport error a cut chunked body produces, not a clean EOF.
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("the client saw %v, want an unexpected EOF from the aborted connection", err)
	}
	if bytes.Contains(body, []byte("snapshot.json")) {
		t.Fatal("a failed export still carried snapshot.json")
	}
}
