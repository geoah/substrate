package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// seedOpenAlerts seeds n open alerts, more than one page of `alerts`.
func seedOpenAlerts(h *harness, n int) {
	for i := range n {
		key := fmt.Sprintf("trigger.parked/t%03d", i)
		h.fake.seed(&substrate.Record{
			ID: key, Kind: alertKind, UpdatedAt: testNow,
			Properties: map[string]any{"key": key, "level": "error", "state": "open", "count": float64(1), "summary": key},
		})
	}
}

// alertKeys are the keys an `alerts -o json` run printed, in order.
func alertKeys(t *testing.T, out string) []string {
	t.Helper()
	var docs []struct {
		Metadata struct {
			ID string `json:"id"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(out), &docs); err != nil {
		t.Fatalf("alerts -o json: %v\n%s", err, out)
	}
	keys := make([]string, 0, len(docs))
	for _, d := range docs {
		keys = append(keys, d.Metadata.ID)
	}
	return keys
}

// `alerts` walks every page. Told 410 compacted on a continuation, it starts
// again from the first page and prints the whole list once: nothing missing,
// nothing twice, and the rows of the walk it dropped are not kept.
func TestAlertsStartAgainWhenACursorIsCompacted(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedOpenAlerts(h, alertsPageSize+20)
	h.fake.compactCursors = 1

	out, _ := h.mustRun("alerts", "-o", "json")
	keys := alertKeys(t, out)
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			t.Fatalf("%s is listed twice", k)
		}
		seen[k] = true
	}
	if len(keys) != alertsPageSize+20 {
		t.Fatalf("alerts listed %d, want %d", len(keys), alertsPageSize+20)
	}
	if h.fake.continuations != 2 {
		t.Fatalf("the walk sent %d continuations, want the refused one and the one after its restart", h.fake.continuations)
	}
}

// It cannot loop: a server that refuses the continuation of the fresh walk
// too is a failure after one restart, shown as the server's refusal.
func TestAlertsFailAfterOneRestart(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedOpenAlerts(h, alertsPageSize+20)
	h.fake.compactCursors = 5

	_, _, err := h.run("alerts")
	if err == nil {
		t.Fatal("alerts succeeded against a server that refuses every continuation")
	}
	if h.fake.continuations != 2 {
		t.Fatalf("the walk sent %d continuations, want 2 (one restart)", h.fake.continuations)
	}
	var rendered bytes.Buffer
	renderError(&rendered, err)
	if !strings.Contains(rendered.String(), "could not resume this listing, even after it started again") || strings.Contains(rendered.String(), "--from") {
		t.Fatalf("the failure does not say the listing failed twice:\n%s", rendered.String())
	}
}

// Another refusal of a continuation stays what it is: no restart.
func TestAlertsDoNotRestartOnAnotherRefusal(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedOpenAlerts(h, alertsPageSize+20)
	h.fake.cursorStatus = http.StatusUnprocessableEntity

	if _, _, err := h.run("alerts"); err == nil {
		t.Fatal("alerts succeeded against a 422 continuation")
	}
	if h.fake.continuations != 1 {
		t.Fatalf("the walk sent %d continuations, want 1", h.fake.continuations)
	}
}

// The kind registry, which resolves every kind name the CLI is given, pages
// the same way: a compacted continuation starts it again from the first page,
// and every kind is resolved once.
func TestTheKindRegistryStartsAgainWhenACursorIsCompacted(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	for i := range typesPageSize {
		h.fake.extraTypes = append(h.fake.extraTypes, installed(fmt.Sprintf("kind%03d", i), "extra.example.com/extra"))
	}
	h.fake.compactCursors = 1

	out, _ := h.mustRun("kinds")
	extra, task := 0, false
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) < 3:
		case strings.HasPrefix(fields[0], "kind") && fields[1] == "extra.example.com":
			extra++
		case fields[0] == "task" && fields[1] == "samples.substrate.reamde.dev":
			task = true
		}
	}
	if extra != typesPageSize || !task {
		t.Fatalf("kinds listed %d of the extra kinds (want %d) and the second page's task: %v", extra, typesPageSize, task)
	}
	if h.fake.continuations != 2 {
		t.Fatalf("the walk sent %d continuations, want 2", h.fake.continuations)
	}
}

// `get --after` reads one page, so the listing a script keeps is the
// script's to start again: the refused cursor fails the command with a hint
// that says so, never the changelog's --from advice.
func TestGetAfterACompactedCursorSaysListAgain(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.compactCursors = 1

	_, _, err := h.run("get", "samples.substrate.reamde.dev/tasks/task", "--after", "3")
	if err == nil {
		t.Fatal("get --after succeeded on a refused cursor")
	}
	var rendered bytes.Buffer
	renderError(&rendered, err)
	errOut := rendered.String()
	for _, want := range []string{"the server can no longer resume the --after cursor", "run the same command without --after"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	if strings.Contains(errOut, "--from") {
		t.Fatalf("the hint resumes a changelog the command never read:\n%s", errOut)
	}
}

// A stream the server ends with compacted is not resumed from the refused
// position: the hint is to list again, then resume from the head the frame
// names.
func TestACompactedStreamIsNotResumedFromItsOwnCursor(t *testing.T) {
	frame := `{"error":{"code":"compacted","message":"seq 41 is below the repository's horizon 60: a deletion before it was forgotten; re-list and resume from the head","head":90,"generation":"gen-new"}}` + "\n"
	var out, errOut bytes.Buffer
	err := streamChanges(&out, &errOut, strings.NewReader(streamBody+frame))
	var ae *apiError
	if !errors.As(err, &ae) || ae.Code != "compacted" {
		t.Fatalf("streamChanges = %v, want the compacted frame", err)
	}
	var rendered bytes.Buffer
	renderError(&rendered, err)
	if !strings.Contains(rendered.String(), "re-list, then resume with --from 90 --generation gen-new") {
		t.Fatalf("the hint does not name the frame's head:\n%s", rendered.String())
	}
	if strings.Contains(rendered.String(), "--from 41") {
		t.Fatalf("the hint resumes from the refused position:\n%s", rendered.String())
	}
}
