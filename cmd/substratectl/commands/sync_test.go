package commands

import (
	"strings"
	"testing"
	"time"
)

// `sync status` is one table over the synchronization read: the account's
// state, whether the owner's request has been served, each stream's state
// and backlog, and the parked and lagging deliveries of the triggers on its
// kind, summed. An erroring account's line carries the error, not the
// message the last good run left.
func TestSyncStatusRendersTheAccountLine(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()

	out, _ := h.mustRun("sync", "status")
	if !strings.Contains(out, "KIND") || !strings.Contains(out, "HEALTH") {
		t.Fatalf("no header: %q", out)
	}
	for _, want := range []string{
		"providers.substrate.reamde.dev/google/account", "george-work", "erroring", "pending",
		"contacts=ok gmail=erroring(12 pending)", "gmail: HTTP 403",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sync status output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ok (12 pending)") {
		t.Errorf("an erroring account shows its error, not the last good message:\n%s", out)
	}
	// One parked delivery and two of lag, summed over the kind's triggers.
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "george-work") {
			line = l
		}
	}
	fields := strings.Fields(line)
	if len(fields) < 9 {
		t.Fatalf("account line too short: %q", line)
	}
	if !strings.Contains(line, " 1 ") || !strings.Contains(line, " 2 ") {
		t.Errorf("parked/lag not summed onto the line: %q", line)
	}
}

// A park older than the account's last completed run leaves the account's
// state standing, so the line names the newest park itself: when it parked
// and the first line of why, never the traceback under it.
func TestSyncStatusNamesTheLatestParkedReason(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()

	out, _ := h.mustRun("sync", "status")
	if !strings.Contains(out, "LAST PARKED") {
		t.Fatalf("no LAST PARKED column: %q", out)
	}
	if !strings.Contains(out, "RuntimeError: contacts: HTTP 500") {
		t.Errorf("sync status output lacks the parked reason:\n%s", out)
	}
	if strings.Contains(out, "Traceback") {
		t.Errorf("the parked reason carries the traceback:\n%s", out)
	}
}

// HEALTH reads `ok`, or `failing` with how long the failing alert has been
// open, on both status tables; a server that reports no health leaves the
// cell empty rather than inventing a word.
func TestStatusTablesShowHealth(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()

	out, _ := h.mustRun("trigger", "status")
	if !strings.Contains(out, "HEALTH") {
		t.Fatalf("trigger status has no HEALTH column:\n%s", out)
	}
	if !strings.Contains(out, "failing 3h") {
		t.Errorf("trigger status does not say the trigger has failed for 3h:\n%s", out)
	}
	out, _ = h.mustRun("sync", "status")
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "george-work") {
			line = l
		}
	}
	if fields := strings.Fields(line); len(fields) < 4 || fields[3] != "ok" {
		t.Errorf("sync status HEALTH is not ok on %q", line)
	}

	a := &app{now: func() time.Time { return testNow }}
	since := testNow.Add(-26 * time.Hour)
	for _, tc := range []struct {
		health string
		since  *time.Time
		want   string
	}{
		{"ok", nil, "ok"},
		{"failing", &since, "failing 1d"},
		{"failing", nil, "failing"},
		{"", nil, ""},
	} {
		if got := a.health(tc.health, tc.since); got != tc.want {
			t.Errorf("health(%q, %v) = %q, want %q", tc.health, tc.since, got, tc.want)
		}
	}
}
