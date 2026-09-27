package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

// logCapture is a slog handler that keeps every record, at every level, so a
// test reads what the server logged. The api tests run serially, which is
// what makes swapping the default logger safe.
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

// A 500 names the request it failed: the method, chi's route pattern (never
// the path with an id in it) and the repository, because a "request failed"
// line without them cannot be traced to anything.
func TestRequestFailedLogNamesTheRoute(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	logs := captureLogs(t)

	ds.errs["Put"] = errBoom
	rec := env.do(t, http.MethodPost, recordsPath, tok, map[string]any{"kind": personKind, "properties": map[string]any{"title": "x"}})
	wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)
	wantOneErrorLog(t, logs, "request failed", map[string]string{
		"method":     http.MethodPost,
		"route":      recordsPath,
		"repository": fakeRepository,
		"error":      errBoom.Error(),
	})
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
			env := newTestEnv(t)
			tok := env.svc.token(fakeRepository)
			env.svc.datasets[fakeRepository].errs["Put"] = tc.err
			logs := captureLogs(t)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.clientGone {
				cancel()
			}
			rec := serveWith(env.h, ctx, http.MethodPost, recordsPath, tok,
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
					"method": http.MethodGet, "route": "/api/" + APIVersion + exportRoute, "repository": fakeRepository,
				})
				return
			}
			if errs := logs.at(slog.LevelError); len(errs) != 0 {
				t.Fatalf("an export the client left was logged at ERROR: %+v", errs)
			}
		})
	}
}

// A catalog listing the client abandons cancels its upgrade previews; that is
// not an ERROR line either, while a preview that fails on its own is one
// naming the route.
func TestCatalogPreviewCanceledByTheClientIsNotAnError(t *testing.T) {
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	for _, clientGone := range []bool{false, true} {
		t.Run(fmt.Sprintf("client_gone=%v", clientGone), func(t *testing.T) {
			base := newFakeService()
			previewErr := errBoom
			if clientGone {
				previewErr = fmt.Errorf("upgrade preview: %w", context.Canceled)
			}
			h := New(Config{Service: &upgradeErrService{fakeService: base, err: previewErr}, Now: (&testClock{}).now, Catalog: cat})
			logs := captureLogs(t)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if clientGone {
				cancel()
			}
			serveWith(h, ctx, http.MethodGet, "/api/v1/catalog", base.token(fakeRepository), "")

			errs := logs.at(slog.LevelError)
			if clientGone {
				if len(errs) != 0 {
					t.Fatalf("a preview the client canceled was logged at ERROR: %+v", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatal("a failed preview logged no ERROR line")
			}
			for _, l := range errs {
				if l.msg != "catalog: upgrade preview failed" || l.attrs["route"] != "/api/v1/catalog" || l.attrs["method"] != http.MethodGet {
					t.Errorf("ERROR line = %q %v, want the preview failure naming GET /api/v1/catalog", l.msg, l.attrs)
				}
			}
		})
	}
}
