package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// panicService panics where the bearer check asks it for the token, inside a
// route behind requireAuth: a fault no engine can be made to produce.
type panicService struct{ *stubService }

func (panicService) Authenticate(context.Context, string) (substrate.Dataset, substrate.TokenInfo, error) {
	panic("token lookup exploded")
}

// panicChangesDataset panics on the first changelog read that finds a row
// past the cursor a stream opened at, so the panic lands after the stream's
// 200 and bookmark: a fault no engine can be made to produce.
type panicChangesDataset struct{ substrate.Dataset }

func (d panicChangesDataset) Changes(ctx context.Context, after int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error) {
	changes, err := d.Dataset.Changes(ctx, after, f, limit)
	if len(changes) > 0 {
		panic("changelog read exploded")
	}
	return changes, err
}

// requestIDPattern is what requestID mints: 16 characters of crypto/rand's
// base32 text.
var requestIDPattern = regexp.MustCompile(`^[A-Z2-7]{16}$`)

// A panic in a handler answers the documented envelope with code `internal`
// rather than an empty 500, and leaves one ERROR line through slog carrying
// the request's id (the one its response names), its route and the stack.
func TestAPanicAnswersTheEnvelopeAndLogsTheRequest(t *testing.T) {
	h := newStubEnvOn(t, panicService{&stubService{}}).h
	logs := captureLogs(t)

	rec := serveWith(h, context.Background(), http.MethodGet, recordsPath, stubToken, "")
	wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)
	id := rec.Header().Get(requestIDHeader)
	if !requestIDPattern.MatchString(id) {
		t.Fatalf("X-Request-Id = %q, want 16 base32 characters", id)
	}
	errs := logs.at(slog.LevelError)
	if len(errs) != 1 || errs[0].msg != "request panicked" {
		t.Fatalf("ERROR lines = %+v, want one request panicked", errs)
	}
	got := errs[0].attrs
	if got["request"] != id || got["route"] != recordsPath || got["method"] != http.MethodGet || got["panic"] != "token lookup exploded" {
		t.Fatalf("panic line attrs = %v, want request %s, route %s and the panic value", got, id, recordsPath)
	}
	if !strings.Contains(got["stack"], "panicService.Authenticate") {
		t.Fatalf("panic line stack does not reach the panicking frame:\n%s", got["stack"])
	}
}

// A flush before any status sends net/http's 200 all the same, so a panic
// after it aborts the response like one after WriteHeader. chi's wrapper,
// which httpMetrics hands every handler, still reports status 0 after a
// flush. No route flushes first today; the chain is built by hand for that.
func TestAPanicAfterAFlushAbortsTheResponse(t *testing.T) {
	for name, flush := range map[string]func(http.ResponseWriter){
		"http.Flusher":            func(w http.ResponseWriter) { w.(http.Flusher).Flush() },
		"http.ResponseController": func(w http.ResponseWriter) { _ = http.NewResponseController(w).Flush() },
	} {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			srv := httptest.NewServer(httpMetrics(recoverer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				flush(w)
				panic("exploded after a flush")
			}))))
			defer srv.Close()

			resp, err := srv.Client().Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("read = %v after %d %q, want the unexpected EOF of an aborted response", err, resp.StatusCode, body)
			}
			if len(logs.at(slog.LevelError)) != 1 {
				t.Fatalf("ERROR lines = %+v, want the one panic line", logs.at(slog.LevelError))
			}
		})
	}
}

// Every response carries a fresh id the server minted, the router's own 404
// included, and an inbound X-Request-Id is never taken: a caller-chosen id
// could repeat another request's and forge the join with the log.
func TestEveryResponseCarriesAFreshRequestID(t *testing.T) {
	env := newStubEnv(t, nil)
	seen := map[string]bool{}
	for _, path := range []string{"/healthz", "/api/v1/nope", recordsPath} {
		rec := env.do(t, http.MethodGet, path, "", nil, requestIDHeader, "caller-chosen/000001")
		id := rec.Header().Get(requestIDHeader)
		if !requestIDPattern.MatchString(id) {
			t.Fatalf("%s: X-Request-Id = %q, want 16 base32 characters of the server's own", path, id)
		}
		if seen[id] {
			t.Fatalf("%s: X-Request-Id %q repeats an earlier request's", path, id)
		}
		seen[id] = true
	}
}
