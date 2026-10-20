package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
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

// TOKEN says whether the account's sign-in works: an erroring one reads with
// the reason's age and its first line, without the flow's prefix and with
// the provider's error code whole, so the table alone tells a refused grant
// from a provider outage and from a failing run.
func TestSyncStatusShowsTheSignIn(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()

	out, _ := h.mustRun("sync", "status")
	if !strings.Contains(out, "TOKEN") {
		t.Fatalf("sync status has no TOKEN column:\n%s", out)
	}
	if !strings.Contains(out, `erroring 3h: provider answered 400, error code "invalid_grant"`) {
		t.Errorf("sync status does not show the refused sign-in:\n%s", out)
	}
	if strings.Contains(out, "oauthflow:") {
		t.Errorf("the sign-in cell keeps the flow's prefix:\n%s", out)
	}
	if strings.Contains(out, "never shown") {
		t.Errorf("the sign-in cell carries the reason's second line:\n%s", out)
	}

	out, _ = h.mustRun("sync", "status", "-o", "json")
	for _, want := range []string{`"tokenStatus": "erroring"`, `"tokenError"`, `"tokenErrorAt"`} {
		if !strings.Contains(out, want) {
			t.Errorf("sync status -o json lacks %s:\n%s", want, out)
		}
	}

	a := &app{now: func() time.Time { return testNow }}
	at := testNow.Add(-3 * time.Hour)
	long := strings.Repeat("x", 200) + "\nsecond line"
	for _, tc := range []struct {
		name string
		s    substrate.SyncStatus
		want string
	}{
		{"no sign-in", substrate.SyncStatus{}, ""},
		{"connected", substrate.SyncStatus{TokenStatus: "connected"}, "connected"},
		{"pending", substrate.SyncStatus{TokenStatus: "pending"}, "pending"},
		{"erroring alone", substrate.SyncStatus{TokenStatus: "erroring"}, "erroring"},
		{"erroring with when", substrate.SyncStatus{TokenStatus: "erroring", TokenErrorAt: &at}, "erroring 3h"},
		{"erroring with why", substrate.SyncStatus{TokenStatus: "erroring", TokenErrorAt: &at, TokenError: long}, "erroring 3h: " + truncate(strings.Repeat("x", 200), signInReasonMax)},
		// The engine's own strings: the flow's prefix goes, the code stays.
		{"refused grant", substrate.SyncStatus{TokenStatus: "erroring", TokenErrorAt: &at, TokenError: `oauthflow: refresh: provider answered 400, error code "invalid_grant"`}, `erroring 3h: provider answered 400, error code "invalid_grant"`},
		{"refused request", substrate.SyncStatus{TokenStatus: "erroring", TokenErrorAt: &at, TokenError: `oauthflow: refresh: provider answered 400, error code "invalid_request"`}, `erroring 3h: provider answered 400, error code "invalid_request"`},
		{"outage", substrate.SyncStatus{TokenStatus: "erroring", TokenErrorAt: &at, TokenError: "oauthflow: refresh: provider answered 503"}, "erroring 3h: provider answered 503"},
		{"no refresh token", substrate.SyncStatus{TokenStatus: "erroring", TokenError: "oauthflow: no refresh token"}, "erroring: no refresh token"},
	} {
		if got := a.signIn(tc.s); got != tc.want {
			t.Errorf("%s: signIn = %q, want %q", tc.name, got, tc.want)
		}
	}
}
