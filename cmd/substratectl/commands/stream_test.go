package commands

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// streamBody is a watch stream that opens at seq 40 under gen-test and
// carries the change at seq 41.
const streamBody = `{"bookmark":40,"generation":"gen-test"}
{}
{"seq":41,"ts":"2026-08-02T12:00:00Z","actor":"api","op":"put","recordId":"t9","type":"samples.substrate.reamde.dev/tasks/task"}
`

// streamErrorFrame is the server's terminal error frame (internal/api
// writeWatchError): the problem object that ended the stream.
const streamErrorFrame = `{"error":{"code":"unavailable","message":"the changelog read failed: connection reset"}}
`

// The terminal error frame is the server saying the stream failed. It was
// once skipped as a heartbeat, so `watch` exited 0 on a failed stream; now it
// is the command's error, with the server's code and message and the cursor
// to resume from.
func TestStreamChangesReturnsTheErrorFrame(t *testing.T) {
	var out, errOut bytes.Buffer
	err := streamChanges(&out, &errOut, strings.NewReader(streamBody+streamErrorFrame))
	var ae *apiError
	if !errors.As(err, &ae) {
		t.Fatalf("streamChanges = %v, want the error frame as an *apiError", err)
	}
	if ae.Code != "unavailable" || ae.Message != "the changelog read failed: connection reset" {
		t.Fatalf("apiError = %+v, want the frame's code and message", ae)
	}
	if !strings.Contains(out.String(), "t9") {
		t.Fatalf("the change before the frame was not printed:\n%s", out.String())
	}

	var rendered bytes.Buffer
	renderError(&rendered, err)
	for _, want := range []string{
		"error: the server ended the watch stream with an error (unavailable)",
		"the changelog read failed: connection reset",
		"hint: resume with --from 41 --generation gen-test",
	} {
		if !strings.Contains(rendered.String(), want) {
			t.Fatalf("rendered error missing %q:\n%s", want, rendered.String())
		}
	}
}

// A clean end is what every server shutdown now makes of a stream, so the
// command says on stderr where to resume, leaving stdout to the changes.
func TestStreamChangesNamesTheResumeCursorOnACleanEnd(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := streamChanges(&out, &errOut, strings.NewReader(streamBody)); err != nil {
		t.Fatalf("streamChanges: %v", err)
	}
	if got, want := errOut.String(), "# stream ended after seq 41; resume with --from 41 --generation gen-test\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if strings.Contains(out.String(), "stream ended") {
		t.Fatalf("the resume line went to stdout:\n%s", out.String())
	}

	// A stream with no change in it resumes from its bookmark.
	errOut.Reset()
	if err := streamChanges(&out, &errOut, strings.NewReader(`{"bookmark":40,"generation":"gen-test"}`+"\n")); err != nil {
		t.Fatalf("streamChanges: %v", err)
	}
	if !strings.Contains(errOut.String(), "resume with --from 40 --generation gen-test") {
		t.Fatalf("stderr = %q, want the bookmark as the resume cursor", errOut.String())
	}
}

// Both commands that tail the changelog fail on the frame: `watch` on the
// changes route and `get -w` on the records route.
func TestWatchAndGetWatchFailOnTheErrorFrame(t *testing.T) {
	for _, tc := range []struct {
		args []string
		path string
	}{
		{[]string{"watch"}, pathChanges},
		{[]string{"get", "samples.substrate.reamde.dev/tasks/task", "-w"}, pathRecords},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			h := newHarness(t)
			streamed := make(chan string, 4)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("watch") != "1" {
					http.NotFound(w, r)
					return
				}
				streamed <- r.URL.Path
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(streamBody + streamErrorFrame))
			}))
			defer srv.Close()
			h.server = srv.URL
			h.writeConfig()

			_, stderr, err := h.run(tc.args...)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Code != "unavailable" {
				t.Fatalf("substratectl %s = %v, want the frame as an *apiError", strings.Join(tc.args, " "), err)
			}
			if n := len(streamed); n != 1 {
				t.Fatalf("%d streams opened, want one", n)
			}
			if got := <-streamed; got != tc.path {
				t.Fatalf("the stream opened at %s, want %s", got, tc.path)
			}
			if strings.Contains(stderr, "stream ended") {
				t.Fatalf("a failed stream printed the clean-end line:\n%s", stderr)
			}
		})
	}
}
