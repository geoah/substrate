// Package e2e drives a LIVE substrate over HTTP, exactly as a user's client
// would: it registers a fresh user through the real door, walks the cases in
// CASES.md, writes a markdown report of every step under .dev/e2e/, and
// LEAVES the repository in place so a human can open the console or
// substratectl and review what the run built. `mise run dev:wipe` is the
// cleanup; nothing here deletes anything.
//
// The suite runs only when SUBSTRATE_E2E_SERVER names a live substrate
// (`mise run test:e2e` arranges that); everywhere else every case skips, so
// `go test ./...` stays green on a machine with no server up.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	envServer    = "SUBSTRATE_E2E_SERVER"
	envInvite    = "SUBSTRATE_E2E_INVITE"
	envReportDir = "SUBSTRATE_E2E_REPORT_DIR"
	envDSN       = "SUBSTRATE_E2E_DSN"
	envCtl       = "SUBSTRATE_E2E_CTL"
	envTimeout   = "SUBSTRATE_E2E_TIMEOUT"
)

// defaultRequestTimeout bounds one exchange: it is the CLIENT's timeout, and
// tripping it abandons a request the server is still serving. A write
// abandoned mid-commit leaves the repository refusing writes until the server
// restarts (issue #516), so on a loaded machine raise the timeout rather than
// trip it: SUBSTRATE_E2E_TIMEOUT takes any Go duration.
const defaultRequestTimeout = 30 * time.Second

func requestTimeout(t *testing.T) time.Duration {
	raw := os.Getenv(envTimeout)
	if raw == "" {
		return defaultRequestTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		t.Fatalf("%s=%q is not a positive Go duration", envTimeout, raw)
	}
	return d
}

// streamDeadline bounds a WATCH, which outlives any single exchange: the
// stream is open until the case has what it waited for. The case's own
// minimum is the floor (a heartbeat case has to outlast the heartbeat
// interval) and SUBSTRATE_E2E_TIMEOUT raises it, so one knob moves every
// deadline the suite holds.
func (r *run) streamDeadline(min time.Duration) time.Duration {
	if r.timeout > min {
		return r.timeout
	}
	return min
}

// authWindow paces the credential endpoints: the door admits one attempt per
// five seconds per (peer, repository), so the suite waits the window out
// between attempts instead of reading its own 429 as a failure.
const authWindow = 5200 * time.Millisecond

// run is one suite invocation: one server, one fresh user, one report.
type run struct {
	t      *testing.T
	base   string
	invite string
	hc     *http.Client
	rep    *report

	stub *llmStub // the scripted OpenAI-wire model the story agents buy from

	repository string
	// authority is the name the repository resolved to at registration, and
	// the id the operator hat addresses it by.
	authority  string
	password   string
	token      string // the first token's secret; every case call carries it
	tokenID    string
	totpSecret string        // the enrolled seed, when the server enforces the factor
	timeout    time.Duration // what one exchange gets before the client gives up
	lastAuth   time.Time     // when the door last saw a credential attempt
	lastStep   int64         // the TOTP step a code was last consumed at
}

func newRun(t *testing.T, base string) *run {
	invite := os.Getenv(envInvite)
	if invite == "" {
		invite = "let-me-in"
	}
	// The repository name is fresh per run because registration is one-shot
	// and the repository is left behind on purpose; base36 nanoseconds keep
	// it a legal DNS label and two runs in the same second apart. It is a
	// bare label, which the door completes under its own host.
	repository := "e2e" + strconv.FormatInt(time.Now().UnixNano(), 36)
	timeout := requestTimeout(t)
	return &run{
		t:          t,
		base:       strings.TrimRight(base, "/"),
		invite:     invite,
		timeout:    timeout,
		hc:         &http.Client{Timeout: timeout},
		repository: repository,
		password:   "correct-horse-battery-staple",
		rep:        &report{Server: base, Started: time.Now()},
	}
}

// report is what a run leaves for the human: which cases ran, what each one
// tests, every step it took, and how the repository looks afterwards.
type report struct {
	Server     string
	Repository string
	Password   string
	TOTPSecret string // printed only when the server enforces the factor: without it the reviewer cannot sign in
	Started    time.Time
	Finished   time.Time
	Cases      []*caseReport
	Appendix   []string
}

type caseReport struct {
	ID       string
	Title    string
	Tests    string // what the case proves, one sentence
	Steps    []string
	Result   string // PASS or FAIL
	Failure  string
	Duration time.Duration
}

// C is one case's context: it records every exchange and every fact into the
// case's report entry as it asserts.
type C struct {
	t  *testing.T
	r  *run
	cr *caseReport
}

// runCase runs one case as a subtest. A failing case stops itself, never the
// run: the report still gets every remaining case.
func (r *run) runCase(id, title, tests string, fn func(*C)) {
	cr := &caseReport{ID: id, Title: title, Tests: tests}
	r.rep.Cases = append(r.rep.Cases, cr)
	r.t.Run(id, func(t *testing.T) {
		start := time.Now()
		defer func() {
			cr.Duration = time.Since(start)
			if p := recover(); p != nil {
				cr.Result = "FAIL"
				cr.Failure = fmt.Sprintf("panic: %v", p)
				t.Errorf("panic: %v", p)
			}
			if cr.Result == "" {
				cr.Result = "PASS"
				if t.Failed() {
					cr.Result = "FAIL"
				}
			}
		}()
		fn(&C{t: t, r: r, cr: cr})
	})
}

// step records one line of what the case did.
func (c *C) stepf(format string, args ...any) {
	c.cr.Steps = append(c.cr.Steps, fmt.Sprintf(format, args...))
}

// skipf ends the case as SKIP: the report must never show PASS for a case
// whose preconditions kept it from asserting anything.
func (c *C) skipf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	c.cr.Result = "SKIP"
	c.cr.Steps = append(c.cr.Steps, "SKIPPED: "+msg)
	c.t.Skip(msg)
}

// require asserts; a failure is recorded in the report and ends the case.
func (c *C) requiref(cond bool, format string, args ...any) {
	c.t.Helper()
	if cond {
		return
	}
	msg := fmt.Sprintf(format, args...)
	c.cr.Result = "FAIL"
	c.cr.Failure = msg
	c.cr.Steps = append(c.cr.Steps, "FAILED: "+msg)
	c.t.Fatal(msg)
}

// paceAuth waits out the door's rate window before a credential attempt.
func (c *C) paceAuth() {
	if !c.r.lastAuth.IsZero() {
		if wait := authWindow - time.Since(c.r.lastAuth); wait > 0 {
			c.stepf("waited %s for the door's rate window", wait.Round(100*time.Millisecond))
			time.Sleep(wait)
		}
	}
	c.r.lastAuth = time.Now()
}

// do sends one JSON exchange with the run's token and records it as a step.
// A nil body sends none; a non-nil out decodes a 2xx body into it.
func (c *C) do(method, path string, body, out any) (int, []byte) {
	c.t.Helper()
	return c.doAs(c.r.token, method, path, body, out)
}

// doAs is do with an explicit bearer; empty means unauthenticated.
func (c *C) doAs(token, method, path string, body, out any) (int, []byte) {
	c.t.Helper()
	status, raw, err := httpJSON(c.r.hc, c.r.base, token, method, path, body)
	c.requiref(err == nil, "%s %s: %v", method, path, err)
	c.stepf("`%s %s` answered %d", method, path, status)
	if out != nil && status < 300 {
		c.requiref(json.Unmarshal(raw, out) == nil, "%s %s: undecodable body %s", method, path, raw)
	}
	return status, raw
}

// httpJSON is the one wire helper: marshal, send, read. It reports nothing;
// the callers decide what an answer means.
func httpJSON(hc *http.Client, base, token, method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

// recordsRoute is the one list route: every read of "some records" is a GET
// here, told apart by its parameters, and the one body-addressed create is a
// POST here. A record's own URL stays its reference under the version prefix.
const recordsRoute = "/api/v1/records"

// kindOf reads the kind reference off a kind path constant
// (`/api/v1/<authority>/<package>/<kind>`), which the suite keeps as the
// prefix of every record URL.
func kindOf(kindPath string) string {
	return strings.TrimPrefix(kindPath, "/api/v1/")
}

// listOf is the list of one kind: the records route narrowed by
// `filter.kinds`, with any further parameters appended as given
// ("first=200", "watch=1").
func listOf(kindPath string, extra ...string) string {
	return listWhere(map[string]any{"kinds": []string{kindOf(kindPath)}}, extra...)
}

// listWhere spells a filter document as the records route's `filter`
// parameter, with any further parameters appended as given.
func listWhere(filter map[string]any, extra ...string) string {
	raw, _ := json.Marshal(filter)
	path := recordsRoute + "?filter=" + url.QueryEscape(string(raw))
	for _, p := range extra {
		path += "&" + p
	}
	return path
}

// referencingList is the reverse read: every record pointing at one target,
// narrowed to one reference property when `property` is set. The page's
// `matches` says from which property each record points.
func referencingList(kind, id, property string, extra ...string) string {
	ref := map[string]any{"ref": kind + "/" + id}
	if property != "" {
		ref["property"] = property
	}
	return listWhere(map[string]any{"referencing": ref}, extra...)
}

// recordsPage is the list envelope, narrowed to what the cases read.
type recordsPage struct {
	Records    []record                   `json:"records"`
	Cursor     string                     `json:"cursor"`
	Head       int64                      `json:"head"`
	Generation string                     `json:"generation"`
	Included   map[string]record          `json:"included"`
	Matches    map[string][]referenceSite `json:"matches"`
}

// referenceSite is one place a record points at a referencing target.
type referenceSite struct {
	Property string `json:"property"`
	Path     string `json:"path"`
}

// fetch is the appendix's quiet read: no case, no recording.
func (r *run) fetch(path string, out any) error {
	status, raw, err := httpJSON(r.hc, r.base, r.token, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("GET %s answered %d: %s", path, status, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// writeReport renders the run as markdown and writes it under the report
// directory (SUBSTRATE_E2E_REPORT_DIR, or .dev/e2e from the repo root).
func (r *run) writeReport() (string, error) {
	rep := r.rep
	rep.Finished = time.Now()
	passed, failed, skipped := 0, 0, 0
	for _, cr := range rep.Cases {
		switch cr.Result {
		case "PASS":
			passed++
		case "SKIP":
			skipped++
		default:
			failed++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Substrate live e2e report\n\n")
	fmt.Fprintf(&b, "| | |\n| --- | --- |\n")
	fmt.Fprintf(&b, "| server | %s |\n", rep.Server)
	if rep.Repository == "" {
		// Registration never completed: there is no repository and nothing
		// to sign into, and the report must not pretend otherwise.
		fmt.Fprintf(&b, "| registration | FAILED; no repository was created (see AUTH-01) |\n")
	} else {
		fmt.Fprintf(&b, "| repository | `%s` |\n", rep.Repository)
		fmt.Fprintf(&b, "| password | `%s` (a dev throwaway, printed on purpose) |\n", rep.Password)
		if rep.TOTPSecret != "" {
			fmt.Fprintf(&b, "| totp seed | `%s` (enroll it to sign in; this server enforces the factor) |\n", rep.TOTPSecret)
		}
	}
	fmt.Fprintf(&b, "| started | %s |\n", rep.Started.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "| finished | %s |\n", rep.Finished.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "| result | **%d passed, %d failed, %d skipped** |\n\n", passed, failed, skipped)
	if rep.Repository != "" {
		fmt.Fprintf(&b, "The repository is left in place for review: open the console at the\n")
		fmt.Fprintf(&b, "server URL and sign in with the credentials above, or point substratectl\n")
		fmt.Fprintf(&b, "at it. `mise run dev:wipe` deletes the database when the review is done.\n\n")
	}

	fmt.Fprintf(&b, "## Cases\n\n| id | case | result | duration |\n| --- | --- | --- | --- |\n")
	for _, cr := range rep.Cases {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", cr.ID, cr.Title, cr.Result, cr.Duration.Round(10*time.Millisecond))
	}
	b.WriteString("\n")
	for _, cr := range rep.Cases {
		fmt.Fprintf(&b, "## %s: %s\n\n", cr.ID, cr.Title)
		fmt.Fprintf(&b, "**Tests:** %s\n\n", cr.Tests)
		fmt.Fprintf(&b, "**Result:** %s", cr.Result)
		if cr.Failure != "" {
			fmt.Fprintf(&b, " (%s)", cr.Failure)
		}
		fmt.Fprintf(&b, "\n\n")
		for i, s := range cr.Steps {
			fmt.Fprintf(&b, "%d. %s\n", i+1, s)
		}
		b.WriteString("\n")
	}
	for _, a := range rep.Appendix {
		b.WriteString(a)
		b.WriteString("\n")
	}

	dir := os.Getenv(envReportDir)
	if dir == "" {
		dir = filepath.Join("..", "..", ".dev", "e2e")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// 0600: the report carries working credentials until the next dev:wipe.
	path := filepath.Join(dir, "report-"+r.repository+".md")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
