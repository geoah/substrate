package providertest

// The google bundle's `syncdrive` against a grant that predates
// `drive.readonly`: Google answers the first Drive call 403
// ACCESS_TOKEN_SCOPE_INSUFFICIENT. The run stamps `needsreconsent`, and its
// log line says why, once, rather than "plan failed for <account>: " with
// nothing after the colon on every run.

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/engine"
)

const googleDriveSyncFn = googlePackage + "/syncdrive"

// functionLogs collects the lines a body wrote through host.log, which the
// engine forwards to its slog logger as "substrate: function log".
type functionLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *functionLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// lines is every body log line one function wrote, in order.
func (l *functionLogs) lines(t *testing.T, fn string) []string {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, raw := range bytes.Split(l.buf.Bytes(), []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		var rec struct {
			Msg      string `json:"msg"`
			Function string `json:"function"`
			Line     string `json:"line"`
		}
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Fatalf("decode a log record %q: %v", raw, err)
		}
		if rec.Msg == "substrate: function log" && rec.Function == fn {
			out = append(out, rec.Line)
		}
	}
	return out
}

func (l *functionLogs) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Reset()
}

func TestGoogleDriveScopeRefusalLogsTheReasonOnce(t *testing.T) {
	requirePython(t)
	t.Parallel()
	logs := &functionLogs{}
	_, ds := newCoreDataset(t, engine.WithLogger(slog.New(slog.NewJSONHandler(logs, nil))))
	install(t, ds, googleDir, nil)

	var fake fakeAPI
	mux := http.NewServeMux()
	mux.HandleFunc("/drive/v3/changes/startPageToken", func(w http.ResponseWriter, r *http.Request) {
		fake.record(r)
		if !bearer(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
			"code":    403,
			"message": "Request had insufficient authentication scopes.",
			"status":  "PERMISSION_DENIED",
			"details": []any{map[string]any{"reason": "ACCESS_TOKEN_SCOPE_INSUFFICIENT"}},
		}})
	})
	fake.serve(t, mux)

	config := func(props map[string]any) map[string]any {
		cfg := stepConfig(googleAccountType, "acct-d", props)
		cfg["inputs"] = map[string]any{"client": map[string]any{
			"properties": map[string]any{"apiBase": fake.ts.URL},
		}}
		return cfg
	}
	props := syncProps(nil, "enabledDrive")

	effects, _ := newStepper(t, ds, googleDriveSyncFn, config(props)).drain(nil)
	stamp := accountStamp(t, effects, googleAccountType, "acct-d")
	if stamp["driveSyncState"] != "needsreconsent" {
		t.Fatalf("drive stamp = %v, want driveSyncState needsreconsent", stamp)
	}
	var failed []string
	for _, line := range logs.lines(t, googleDriveSyncFn) {
		if strings.Contains(line, "failed for acct-d") {
			failed = append(failed, line)
		}
	}
	if len(failed) != 1 {
		t.Fatalf("failure log lines = %q, want exactly one", failed)
	}
	if !strings.Contains(failed[0], "needs reconsent") || !strings.Contains(failed[0], "reconnect the account") {
		t.Fatalf("failure log line = %q, want it to name the missing consent and the reconnect", failed[0])
	}

	// The next run finds the state the first one stamped: the account already
	// says needsreconsent, so the run stamps and logs nothing new.
	logs.reset()
	next := maps.Clone(props)
	maps.Copy(next, stamp)
	newStepper(t, ds, googleDriveSyncFn, config(next)).drain(nil)
	for _, line := range logs.lines(t, googleDriveSyncFn) {
		if strings.Contains(line, "failed for acct-d") {
			t.Fatalf("a repeat run logged %q, want no line while the state is unchanged", line)
		}
	}
	if n := len(fake.seenPaths()); n != 2 {
		t.Fatalf("startPageToken calls = %d, want one per run", n)
	}
}
