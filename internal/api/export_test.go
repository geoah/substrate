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

// exportingDataset is the fake with the export seam: the route type-asserts
// substrate.Exporter per request, so the plain fake keeps answering 501 and
// this one answers the archive.
type exportingDataset struct{ *fakeDataset }

var _ substrate.Exporter = (*exportingDataset)(nil)

// fakeExport is a two-entry archive: the manifest and snapshot.json, in the
// order the engine writes them. Midway failure stops after the first.
type fakeExport struct {
	ds    *exportingDataset
	point substrate.ExportPoint
}

func (d *exportingDataset) Export(context.Context) (substrate.Export, error) {
	if d.exportErr != nil {
		return nil, d.exportErr
	}
	return &fakeExport{ds: d, point: substrate.ExportPoint{
		Authority: d.repository.Authority, Head: 42, HeadHash: strings.Repeat("ab", 32),
		TakenAt: time.Unix(1_700_000_000, 0).UTC(), Segments: 1, SealedFiles: 2,
	}}, nil
}

func (e *fakeExport) Point() substrate.ExportPoint { return e.point }

func (e *fakeExport) WriteTo(w io.Writer) (int64, error) {
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

// exportingService hands every authenticated request the exporting dataset.
type exportingService struct {
	*fakeService
	ds *exportingDataset
}

func (s *exportingService) Authenticate(ctx context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	_, info, err := s.fakeService.Authenticate(ctx, secret)
	if err != nil {
		return nil, info, err
	}
	return s.ds, info, nil
}

// exportEnv is a test env whose one repository exports.
func exportEnv(t *testing.T) (*testEnv, *exportingDataset, string) {
	t.Helper()
	base := newFakeService()
	ds := &exportingDataset{fakeDataset: base.datasets["geoah"]}
	svc := &exportingService{fakeService: base, ds: ds}
	env := &testEnv{svc: base, h: New(Config{Service: svc, InviteCode: testInviteCode})}
	return env, ds, base.token("geoah")
}

// The archive comes back as a tar under the authority's file name, with the
// point in the name, and the bearer token is the whole credential.
func TestExportStreamsTheArchiveUnderTheBearerToken(t *testing.T) {
	env, _, tok := exportEnv(t)
	rec := env.do(t, http.MethodGet, "/api/v1/export", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-tar" {
		t.Fatalf("Content-Type = %q, want application/x-tar", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename=geoah.example.com-42.tar` {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	tr := tar.NewReader(rec.Body)
	var names []string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}
		names = append(names, h.Name)
	}
	want := []string{"repositories/geoah.example.com/repository.json", "repositories/geoah.example.com/snapshot.json"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", names, want)
	}

	// No token, no archive: the route sits behind the ordinary bearer check.
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/export", "", nil), http.StatusUnauthorized, codeAuth)
}

// A dataset without the seam answers 501, and a refusal before the first byte
// is an ordinary error body with its status, not half an archive.
func TestExportRefusesWithAStatusBeforeTheFirstByte(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/export", tok, nil), http.StatusNotImplemented, codeUnsupported)

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
// with chi's Recoverer mounted, because that middleware is what could swallow
// the abort: it does not, its recover re-panics http.ErrAbortHandler
// (chi v5.3.1 middleware/recoverer.go lines 26 to 29: `if rvr ==
// http.ErrAbortHandler { panic(rvr) }`), and the net/http server then closes
// the connection without the terminating chunk.
func TestExportAbortsTheResponseWhenTheStreamFailsMidway(t *testing.T) {
	env, ds, tok := exportEnv(t)
	ds.exportFailMidway = true
	// env.h is New(Config{...}): the router with peerAddress, RequestID and
	// Recoverer, not the bare handler.
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
