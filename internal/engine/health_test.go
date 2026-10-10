package engine

import (
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The window names itself in an alert's summary the way a person writes it.
func TestWindowTextDropsTheZeroUnits(t *testing.T) {
	t.Parallel()
	for d, want := range map[time.Duration]string{
		time.Hour:                      "1h",
		90 * time.Minute:               "1h30m",
		15 * time.Minute:               "15m",
		2 * time.Second:                "2s",
		time.Hour + 30*time.Second:     "1h0m30s",
		24*time.Hour + 1*time.Minute:   "24h1m",
		1500 * time.Millisecond:        "2s",
		10*time.Minute + 5*time.Second: "10m5s",
	} {
		if got := windowText(d); got != want {
			t.Errorf("windowText(%s) = %q, want %q", d, got, want)
		}
	}
}

// A sync reads failing when any trigger whose parks it counts is failing,
// since the oldest of theirs, and last ok at the newest ok any of them had.
// A trigger it does not count changes nothing.
func TestSyncHealthIsTheWorstOfItsTriggers(t *testing.T) {
	t.Parallel()
	at := func(h int) *time.Time {
		v := time.Date(2026, 10, 10, h, 0, 0, 0, time.UTC)
		return &v
	}
	byID := map[string]substrate.TriggerStatus{
		"on-record": {ID: "on-record", Health: substrate.HealthFailing, FailingSince: at(6), LastOkAt: at(2)},
		"hourly":    {ID: "hourly", Health: substrate.HealthFailing, FailingSince: at(4), LastOkAt: at(3)},
		"healthy":   {ID: "healthy", Health: substrate.HealthOK, LastOkAt: at(9)},
		"elsewhere": {ID: "elsewhere", Health: substrate.HealthFailing, FailingSince: at(1), LastOkAt: at(11)},
	}

	var st substrate.SyncStatus
	syncHealth(&st, byID, []string{"on-record", "hourly", "healthy", "gone"})
	if st.Health != substrate.HealthFailing {
		t.Fatalf("health is %q, want failing", st.Health)
	}
	if st.FailingSince == nil || !st.FailingSince.Equal(*at(4)) {
		t.Fatalf("failingSince is %v, want the oldest failing trigger's %v", st.FailingSince, at(4))
	}
	if st.LastOkAt == nil || !st.LastOkAt.Equal(*at(9)) {
		t.Fatalf("lastOkAt is %v, want the newest %v", st.LastOkAt, at(9))
	}

	var ok substrate.SyncStatus
	syncHealth(&ok, byID, []string{"healthy"})
	if ok.Health != substrate.HealthOK || ok.FailingSince != nil {
		t.Fatalf("a sync of one healthy trigger reads %q since %v", ok.Health, ok.FailingSince)
	}
}

// The due rule: parks since the bound, the oldest older than the window, and
// an open alert rewritten only after the repeat interval when a run parked
// since its last write.
func TestAFailingReadIsDueOnlyPastTheWindowAndWithSomethingNew(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	window := time.Hour
	streak := failingRead{count: 3, oldest: now.Add(-2 * time.Hour), newest: now.Add(-time.Minute)}
	quiet := failingRead{count: 3, oldest: streak.oldest, newest: now.Add(-20 * time.Minute)}
	for name, tc := range map[string]struct {
		read failingRead
		want bool
	}{
		"no parks since the bound":     {failingRead{}, false},
		"inside the window":            {failingRead{count: 1, oldest: now.Add(-59 * time.Minute), newest: now.Add(-time.Minute)}, false},
		"past the window, no alert":    {streak, true},
		"open, written a minute ago":   {withOpen(streak, now.Add(-time.Minute)), false},
		"open, stale, a park since":    {withOpen(streak, now.Add(-10*time.Minute)), true},
		"open, stale, nothing since":   {withOpen(quiet, now.Add(-10*time.Minute)), false},
		"exactly the window is enough": {failingRead{count: 1, oldest: now.Add(-window), newest: now.Add(-window)}, true},
	} {
		if got := tc.read.due(now, window); got != tc.want {
			t.Errorf("%s: due = %v, want %v", name, got, tc.want)
		}
	}
}

func withOpen(r failingRead, updated time.Time) failingRead {
	r.open, r.updated = true, updated
	return r
}

// A pass reads health at most once per tenth of the window, capped at a
// minute; a clock that stepped back reads at once.
func TestHealthIsReadAtMostOncePerBound(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	ds := &dataset{svc: &service{healthFailingAfter: time.Hour}}
	for i, step := range []struct {
		at   time.Duration
		want bool
	}{
		{0, true},
		{30 * time.Second, false},
		{61 * time.Second, true},
		{90 * time.Second, false},
		{-time.Hour, true},
	} {
		if got := ds.healthDue(t0.Add(step.at)); got != step.want {
			t.Fatalf("step %d at %s: due = %v, want %v", i, step.at, got, step.want)
		}
	}
	short := &dataset{svc: &service{healthFailingAfter: 2 * time.Minute}}
	if !short.healthDue(t0) || short.healthDue(t0.Add(11*time.Second)) || !short.healthDue(t0.Add(13*time.Second)) {
		t.Fatal("a two minute window does not read every twelve seconds")
	}
}
