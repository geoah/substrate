package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// logCapture is a slog handler that keeps every record, at every level, so a
// test reads what the server logged. A test that captures is serial: no
// parallel test runs beside it, which is what makes swapping the default
// logger safe.
type logCapture struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (c *logCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs = append(c.recs, r.Clone())
	return nil
}

func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *logCapture) WithGroup(string) slog.Handler      { return c }

// at returns every record logged at level, each as its message and attrs.
func (c *logCapture) at(level slog.Level) []capturedLog {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []capturedLog
	for _, r := range c.recs {
		if r.Level != level {
			continue
		}
		l := capturedLog{msg: r.Message, attrs: map[string]string{}}
		r.Attrs(func(a slog.Attr) bool {
			l.attrs[a.Key] = a.Value.String()
			return true
		})
		out = append(out, l)
	}
	return out
}

type capturedLog struct {
	msg   string
	attrs map[string]string
}

func captureLogs(t *testing.T) *logCapture {
	t.Helper()
	c := &logCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(c))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return c
}

// serveWith runs one request through the router under ctx, the way a request
// whose client has gone arrives: its context already canceled.
func serveWith(h http.Handler, ctx context.Context, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	req.RemoteAddr = "10.0.0.1:1234"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func wantOneErrorLog(t *testing.T, logs *logCapture, msg string, attrs map[string]string) {
	t.Helper()
	errs := logs.at(slog.LevelError)
	if len(errs) != 1 {
		t.Fatalf("ERROR lines = %+v, want exactly one %q", errs, msg)
	}
	if errs[0].msg != msg {
		t.Fatalf("ERROR line = %q, want %q", errs[0].msg, msg)
	}
	for k, want := range attrs {
		if got := errs[0].attrs[k]; got != want {
			t.Errorf("ERROR line %s = %q, want %q (attrs %v)", k, got, want, errs[0].attrs)
		}
	}
}

// errRequestLogBoom is a failure no sentinel names: the 500 the handler
// answers for a fault it does not recognize.
var errRequestLogBoom = errors.New("boom")

// requestLogFailingPut is a repository whose every write fails with err: the
// fault (a full disk, a refused restart, a context the server or the client
// canceled) the log line names, which the engine cannot be made to produce on
// demand.
type requestLogFailingPut struct {
	stubKinds
	err error
}

func (d requestLogFailingPut) Put(context.Context, substrate.Actor, substrate.PutInput) (*substrate.Record, error) {
	return nil, d.err
}

// requestLogUnavailable is a service whose bearer check fails with err: a
// token lookup the database refused, a repository that will not open.
type requestLogUnavailable struct {
	*stubService
	err error
}

func (s requestLogUnavailable) Authenticate(context.Context, string) (substrate.Dataset, substrate.TokenInfo, error) {
	return nil, substrate.TokenInfo{}, s.err
}

// A 500 names the request it failed: the method, chi's route pattern (never
// the path with an id in it) and the repository, because a "request failed"
// line without them cannot be traced to anything.
func TestRequestFailedLogNamesTheRoute(t *testing.T) {
	env := newStubEnv(t, requestLogFailingPut{stubKinds: newStubKinds(personKind), err: errRequestLogBoom})
	logs := captureLogs(t)

	rec := env.do(t, http.MethodPost, recordsPath, stubToken, map[string]any{"kind": personKind, "properties": map[string]any{"title": "x"}})
	wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)
	wantOneErrorLog(t, logs, "request failed", map[string]string{
		"method":     http.MethodPost,
		"route":      recordsPath,
		"repository": stubRepository,
		"error":      errRequestLogBoom.Error(),
		// The id the response carries, so a client's report joins this line.
		"request": rec.Header().Get(requestIDHeader),
	})
}

// A 503 is a server condition too (a full disk under the data root fails
// every write as unavailable), and once it went unlogged: the client was told
// to retry every second and the operator's log stayed empty. Every 503 is now
// one WARN line naming the request and the cause, a refusal only a restart
// lifts is an ERROR, and a client's own cancellation is a DEBUG.
func TestAnUnavailableAnswerIsLoggedOnce(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		clientGone bool
		level      slog.Level
		msg        string
		retryAfter string
	}{
		{"unavailable", fmt.Errorf("%w: write changelog: no space left on device", substrate.ErrUnavailable), false, slog.LevelWarn, "request answered unavailable", "1"},
		{"restart_required", fmt.Errorf("%w: the directory is behind the tables", substrate.ErrRestartRequired), false, slog.LevelError, "request refused until the server restarts", "30"},
		{"client_canceled", fmt.Errorf("%w: %w", substrate.ErrUnavailable, context.Canceled), true, slog.LevelDebug, "request canceled by the client", "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newStubEnv(t, requestLogFailingPut{stubKinds: newStubKinds(personKind), err: tc.err})
			logs := captureLogs(t)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.clientGone {
				cancel()
			}
			rec := serveWith(env.h, ctx, http.MethodPost, recordsPath, stubToken,
				`{"kind":"`+personKind+`","properties":{"title":"x"}}`)
			wantErrorCode(t, rec, http.StatusServiceUnavailable, codeUnavailable)
			if got := rec.Header().Get("Retry-After"); got != tc.retryAfter {
				t.Fatalf("Retry-After = %q, want %s", got, tc.retryAfter)
			}
			wantOneLogLine(t, logs, tc.level, tc.msg, map[string]string{
				"method": http.MethodPost, "route": recordsPath, "repository": stubRepository,
				"error": tc.err.Error(), "request": rec.Header().Get(requestIDHeader),
			})
		})
	}
}

// The bearer check's 503 masks its cause from the client (a token lookup the
// database refused, a repository that will not open), so the log is the only
// place the cause appears, under the same levels and Retry-After rule as
// every other 503.
func TestTheBearerChecksUnavailableLogsItsCause(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		level      slog.Level
		msg        string
		retryAfter string
	}{
		{"lookup_failed", errors.New("token lookup: connection refused"), slog.LevelWarn, "request answered unavailable", "1"},
		{"restart_required", fmt.Errorf("open: %w", substrate.ErrRestartRequired), slog.LevelError, "request refused until the server restarts", "30"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newStubEnvOn(t, requestLogUnavailable{stubService: &stubService{}, err: tc.err})
			logs := captureLogs(t)

			rec := env.do(t, http.MethodGet, recordsPath, stubToken, nil)
			wantErrorCode(t, rec, http.StatusServiceUnavailable, codeUnavailable)
			if body := rec.Body.String(); strings.Contains(body, tc.err.Error()) {
				t.Fatalf("the 503 showed the client its cause: %s", body)
			}
			if got := rec.Header().Get("Retry-After"); got != tc.retryAfter {
				t.Fatalf("Retry-After = %q, want %s", got, tc.retryAfter)
			}
			wantOneLogLine(t, logs, tc.level, tc.msg, map[string]string{
				"method": http.MethodGet, "route": recordsPath, "error": tc.err.Error(),
				"request": rec.Header().Get(requestIDHeader),
			})
		})
	}
}

// wantOneLogLine requires exactly one line at level, with msg and attrs, and
// nothing at any level above it.
func wantOneLogLine(t *testing.T, logs *logCapture, level slog.Level, msg string, attrs map[string]string) {
	t.Helper()
	lines := logs.at(level)
	if len(lines) != 1 || lines[0].msg != msg {
		t.Fatalf("%s lines = %+v, want exactly one %q", level, lines, msg)
	}
	for k, want := range attrs {
		if got := lines[0].attrs[k]; got != want {
			t.Errorf("%s line %s = %q, want %q (attrs %v)", level, k, got, want, lines[0].attrs)
		}
	}
	for _, above := range []slog.Level{slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		if above > level && len(logs.at(above)) != 0 {
			t.Fatalf("%s lines = %+v beside the %s line", above, logs.at(above), level)
		}
	}
}

// A client that goes away mid-request is not a server fault, so its
// cancellation is not an ERROR line. Only the client's own cancellation is
// excused: a context.Canceled while the request's context is still live came
// from a context the server canceled, and a deadline is a server-side timeout,
// so both stay errors.
func TestClientCancellationIsNotLoggedAsAnError(t *testing.T) {
	cases := []struct {
		name          string
		err           error
		clientGone    bool
		wantErrorLine bool
	}{
		{"client_canceled", fmt.Errorf("commit: %w", context.Canceled), true, false},
		{"server_canceled", fmt.Errorf("commit: %w", context.Canceled), false, true},
		{"server_deadline", fmt.Errorf("commit: %w", context.DeadlineExceeded), false, true},
		{"deadline_after_client_left", fmt.Errorf("commit: %w", context.DeadlineExceeded), true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newStubEnv(t, requestLogFailingPut{stubKinds: newStubKinds(personKind), err: tc.err})
			logs := captureLogs(t)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.clientGone {
				cancel()
			}
			rec := serveWith(env.h, ctx, http.MethodPost, recordsPath, stubToken,
				`{"kind":"`+personKind+`","properties":{"title":"x"}}`)
			wantStatus(t, rec, http.StatusInternalServerError)

			if tc.wantErrorLine {
				wantOneErrorLog(t, logs, "request failed", map[string]string{"route": recordsPath})
				return
			}
			if errs := logs.at(slog.LevelError); len(errs) != 0 {
				t.Fatalf("a client's own cancellation was logged at ERROR: %+v", errs)
			}
			debug := logs.at(slog.LevelDebug)
			if len(debug) != 1 || debug[0].attrs["route"] != recordsPath {
				t.Fatalf("DEBUG lines = %+v, want one naming route %q", debug, recordsPath)
			}
		})
	}
}

// An export cut mid-stream is an ERROR naming the route, unless the client is
// what went away: then the failed write is to nobody, and the line is DEBUG.
func TestExportAbortLogsTheClientLeavingAtDebug(t *testing.T) {
	for _, clientGone := range []bool{false, true} {
		t.Run(fmt.Sprintf("client_gone=%v", clientGone), func(t *testing.T) {
			env, ds, tok := exportEnv(t)
			ds.exportFailMidway = true
			logs := captureLogs(t)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if clientGone {
				cancel()
			}
			func() {
				// The handler aborts the response with http.ErrAbortHandler,
				// which the recoverer passes on to the server.
				defer func() { _ = recover() }()
				serveWith(env.h, ctx, http.MethodGet, "/api/v1/export", tok, "")
			}()

			if !clientGone {
				wantOneErrorLog(t, logs, "export aborted mid-stream", map[string]string{
					"method": http.MethodGet, "route": "/api/" + APIVersion + exportRoute, "repository": stubRepository,
				})
				return
			}
			if errs := logs.at(slog.LevelError); len(errs) != 0 {
				t.Fatalf("an export the client left was logged at ERROR: %+v", errs)
			}
		})
	}
}
