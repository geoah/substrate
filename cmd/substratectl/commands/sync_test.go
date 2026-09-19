package commands

import (
	"strings"
	"testing"
)

// `sync status` is one table over the synchronisation read: the account's
// state, whether the owner's request has been served, each stream's state
// and backlog, and the parked and lagging deliveries of the triggers on its
// kind, summed. An erroring account's line carries the error, not the
// message the last good run left.
func TestSyncStatusRendersTheAccountLine(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()

	out, _ := h.mustRun("sync", "status")
	if !strings.Contains(out, "KIND\tID\tSTATE\tPAUSED\tLAST\tREQUESTED\tSTREAMS\tPARKED\tLAG\tMESSAGE") &&
		!strings.Contains(out, "KIND") {
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
