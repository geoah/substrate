package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The window read's engine half (window.go): on one snapshot, the rows after
// the key in slot order over each kind's OWN slot column, every candidate
// series whole, and the overrides claiming a slot inside the window whatever
// their own `at`. internal/window expands; this proves the SQL underneath.

const (
	winAuthority = "window.e2e.example"
	winPackage   = winAuthority + "/timeline"
	winDose      = winPackage + "/dose"      // temporal(point: dueAt) + recurring + override
	winMeeting   = winPackage + "/meeting"   // temporal(range) alone
	winSeries    = winPackage + "/series"    // temporal(range) + recurring
	winException = winPackage + "/exception" // temporal(range) + override → series
)

func winManifest() []map[string]any {
	kind := func(id, name string, traits []any, props map[string]any) map[string]any {
		return map[string]any{
			"kind":     "substrate.reamde.dev/core/kind",
			"metadata": map[string]any{"id": id},
			"data": map[string]any{
				"authority": winAuthority, "package": "timeline",
				"names":  map[string]any{"singular": name},
				"traits": traits, "properties": props,
			},
		}
	}
	rule := map[string]any{
		"name":       map[string]any{"type": "string"},
		"recurrence": map[string]any{"type": "recurrence"},
		"rdates":     map[string]any{"type": "datetime", "repeated": true},
		"exdates":    map[string]any{"type": "datetime", "repeated": true},
		"timezone":   map[string]any{"type": "timezone"},
	}
	return []map[string]any{
		vocabulary.PackageManifest(winPackage, 1),
		kind(winDose, "dose", []any{"substrate.reamde.dev/core/temporal(point: dueAt)", "substrate.reamde.dev/core/recurring", "substrate.reamde.dev/core/override"}, map[string]any{
			"name":         map[string]any{"type": "string"},
			"recurrence":   map[string]any{"type": "recurrence"},
			"rdates":       map[string]any{"type": "datetime", "repeated": true},
			"exdates":      map[string]any{"type": "datetime", "repeated": true},
			"timezone":     map[string]any{"type": "timezone"},
			"recurrenceOf": map[string]any{"type": "reference", "kind": winDose},
			"originalAt":   map[string]any{"type": "datetime"},
		}),
		kind(winMeeting, "meeting", []any{"substrate.reamde.dev/core/temporal(range)"}, map[string]any{
			"name": map[string]any{"type": "string"},
			// A property merely CALLED recurrence, on a kind that does not
			// bind the trait: its rows are rows, never series.
			"recurrence": map[string]any{"type": "string"},
		}),
		kind(winSeries, "series", []any{"substrate.reamde.dev/core/temporal(range)", "substrate.reamde.dev/core/recurring"}, rule),
		kind(winException, "exception", []any{"substrate.reamde.dev/core/temporal(range)", "substrate.reamde.dev/core/override"}, map[string]any{
			"name":         map[string]any{"type": "string"},
			"recurrenceOf": map[string]any{"type": "reference", "kind": winSeries, "onDelete": "cascade"},
			"originalAt":   map[string]any{"type": "datetime"},
		}),
	}
}

func TestWindowReadsRowsSeriesAndOverridesOnOneSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, winManifest()); err != nil {
		t.Fatalf("declare the timeline kinds: %v", err)
	}
	put := func(kind, id string, props map[string]any) {
		t.Helper()
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: kind, ID: id, Properties: props}); err != nil {
			t.Fatalf("put %s/%s: %v", kind, id, err)
		}
	}
	// A weekly meeting series anchored on July 1st, two exceptions: the 8th
	// moved out of the window entirely, the 15th moved within it.
	put(winSeries, "standup", map[string]any{
		"name": "Standup", "recurrence": "RRULE:FREQ=WEEKLY", "timezone": "Europe/London",
		"at": "2026-07-01T08:00:00Z", "endsAt": "2026-07-01T08:30:00Z",
	})
	put(winException, "standup_20260708T080000Z", map[string]any{
		"name": "Standup (moved to August)", "at": "2026-08-20T08:00:00Z", "endsAt": "2026-08-20T08:30:00Z",
		"recurrenceOf": winSeries + "/standup", "originalAt": "2026-07-08T08:00:00Z",
	})
	put(winException, "standup_20260715T080000Z", map[string]any{
		"name": "Standup (later)", "at": "2026-07-15T14:00:00Z", "endsAt": "2026-07-15T14:30:00Z",
		"recurrenceOf": winSeries + "/standup", "originalAt": "2026-07-15T08:00:00Z",
	})
	// A dose series whose slot is dueAt, a plain meeting, and a dose override
	// at its own dueAt.
	put(winDose, "levo", map[string]any{"name": "Levo", "recurrence": "FREQ=DAILY", "dueAt": "2026-07-01T06:00:00Z"})
	put(winDose, "levo_20260703T060000Z", map[string]any{
		"name": "Levo (evening)", "dueAt": "2026-07-03T18:00:00Z",
		"recurrenceOf": winDose + "/levo", "originalAt": "2026-07-03T06:00:00Z",
	})
	put(winMeeting, "dentist", map[string]any{"name": "Dentist", "at": "2026-07-02T12:00:00Z", "endsAt": "2026-07-02T13:00:00Z", "recurrence": "every year, roughly"})
	// A series outside the window's kinds must not be a candidate; a deleted
	// row must not be anything.
	put(winMeeting, "gone", map[string]any{"name": "Gone", "at": "2026-07-04T12:00:00Z", "endsAt": "2026-07-04T13:00:00Z"})
	if _, err := ds.Delete(ctx, substrate.ActorAPI, winMeeting, "gone", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	from, to := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	page, err := ds.Window(ctx, substrate.WindowQuery{
		Filter: substrate.Filter{Implements: "substrate.reamde.dev/core/temporal", Properties: map[string]substrate.Cond{
			"at": {Gte: from.Format(time.RFC3339), Lt: to.Format(time.RFC3339)},
		}},
		From: from, To: to, First: 10,
	})
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	// Rows: the plain meeting, the in-window exception and the dose override,
	// in slot order over their OWN columns (the dose's is due_at), no series
	// row and no deleted row.
	var rows []string
	for _, r := range page.Rows {
		rows = append(rows, r.ID)
	}
	want := "dentist levo_20260703T060000Z standup_20260715T080000Z"
	if got := join(rows); got != want {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	if page.More {
		t.Fatal("three rows under first=10 must not report more")
	}
	var series []string
	for _, s := range page.Series {
		series = append(series, s.ID)
	}
	if got := join(series); got != "levo standup" {
		t.Fatalf("series = %q, want both series", got)
	}
	// Overrides: every override whose ORIGINAL slot is in the window, the
	// one that moved to August included, the dose's included.
	var overrides []string
	for _, o := range page.Overrides {
		overrides = append(overrides, o.ID)
	}
	if got := join(overrides); got != "levo_20260703T060000Z standup_20260708T080000Z standup_20260715T080000Z" {
		t.Fatalf("overrides = %q", got)
	}

	// The key seeks strictly past a row, in both directions, and More says
	// when a bound applies.
	page, err = ds.Window(ctx, substrate.WindowQuery{
		Filter: substrate.Filter{Implements: "substrate.reamde.dev/core/temporal", Properties: map[string]substrate.Cond{
			"at": {Gte: from.Format(time.RFC3339), Lt: to.Format(time.RFC3339)},
		}},
		From: from, To: to, First: 1,
		After: &substrate.WindowKey{At: time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC), Kind: winMeeting, ID: "dentist"},
	})
	if err != nil {
		t.Fatalf("Window after: %v", err)
	}
	if len(page.Rows) != 1 || page.Rows[0].ID != "levo_20260703T060000Z" || !page.More {
		t.Fatalf("after dentist, first=1: rows %v more %v", page.Rows, page.More)
	}
	page, err = ds.Window(ctx, substrate.WindowQuery{
		Filter: substrate.Filter{Kinds: []string{winMeeting, winSeries, winException}, Properties: map[string]substrate.Cond{
			"at": {Gte: from.Format(time.RFC3339), Lt: to.Format(time.RFC3339)},
		}},
		From: from, To: to, First: 5, Desc: true,
	})
	if err != nil {
		t.Fatalf("Window desc: %v", err)
	}
	rows = rows[:0]
	for _, r := range page.Rows {
		rows = append(rows, r.ID)
	}
	if got := join(rows); got != "standup_20260715T080000Z dentist" {
		t.Fatalf("descending rows over three kinds = %q", got)
	}
	if len(page.Series) != 1 || page.Series[0].ID != "standup" {
		t.Fatalf("kinds narrow the candidates: %v", page.Series)
	}

	// A window over kinds that do not sit on the timeline is refused.
	if _, err := ds.Window(ctx, substrate.WindowQuery{
		Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/token"}},
		From:   from, To: to,
	}); err == nil {
		t.Fatal("a window over a non-temporal kind must be a validation error")
	}

	// A series id long enough to push `<id>_<slot>` past the id alphabet's
	// bound is refused at write; the same id on a kind without the trait is
	// fine.
	long := strings.Repeat("x", vocabulary.MaxSeriesIDLen+1)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: winSeries, ID: long, Properties: map[string]any{
		"name": "too long", "recurrence": "FREQ=DAILY", "at": "2026-07-01T08:00:00Z", "endsAt": "2026-07-01T08:30:00Z",
	}}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a %d-character series id must be refused, got %v", len(long), err)
	}
	put(winMeeting, long, map[string]any{"name": "long but plain", "at": "2026-07-01T08:00:00Z", "endsAt": "2026-07-01T08:30:00Z"})
	// An override of the recurring kind carries the slot suffix already and
	// is held to MaxIDLen alone.
	put(winSeries, strings.Repeat("y", vocabulary.MaxSeriesIDLen)+"_20260708T080000Z", map[string]any{
		"name": "an override with a long id", "at": "2026-07-08T09:00:00Z", "endsAt": "2026-07-08T09:30:00Z",
	})
}

func join(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}
