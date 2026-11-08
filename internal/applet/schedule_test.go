package applet

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// addDays moves a date as a date and an instant as an instant, takes the
// digits a TextField holds, and answers nothing for nothing, so a filter
// bound to an empty value is dropped.
func TestAddDaysMovesADateOrAnInstant(t *testing.T) {
	r := &resolver{now: time.Date(2026, 11, 8, 10, 30, 0, 0, time.UTC), model: map[string]any{
		"context": map[string]any{"every": "7", "last": "2026-10-30T08:00:00Z"},
	}}
	call := func(value, days any) map[string]any {
		return map[string]any{"@call": "addDays", "args": map[string]any{"value": value, "days": days}}
	}
	cases := []struct {
		name string
		in   map[string]any
		want any
	}{
		{"today plus thirty", call(map[string]any{"@now": "date"}, 30), "2026-12-08"},
		{"an instant back a day", call(map[string]any{"@now": "datetime"}, -1), "2026-11-07T10:30:00Z"},
		{"a record's instant by a typed interval", call(map[string]any{"@path": "/context/last"}, map[string]any{"@path": "/context/every"}), "2026-11-06T08:00:00Z"},
		{"nothing to move", call(map[string]any{"@path": "/context/missing"}, 7), nil},
	}
	for _, tc := range cases {
		got, err := r.resolve(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %v %v, want %v", tc.name, got, err, tc.want)
		}
	}
	if _, err := r.resolve(call("next week", 7)); err == nil {
		t.Error("a value that is no date was moved")
	}
	if _, err := r.resolve(call(map[string]any{"@now": "date"}, 1.5)); err == nil {
		t.Error("half a day was added")
	}
}

func TestCheckHoldsAddDaysToItsArguments(t *testing.T) {
	cases := []struct {
		name string
		call map[string]any
		want string
	}{
		{"no days", map[string]any{"@call": "addDays", "args": map[string]any{"value": map[string]any{"@now": "date"}}}, "args.days: is required by addDays"},
		{"days in words", map[string]any{"@call": "addDays", "args": map[string]any{"value": map[string]any{"@now": "date"}, "days": "a week"}}, "args.days: is a whole number of days"},
		{"a value that is no date", map[string]any{"@call": "addDays", "args": map[string]any{"value": "soon", "days": 7}}, "args.value: is a date"},
		{"a function the server lacks", map[string]any{"@call": "nextWeek"}, `calls formatString or addDays, not "nextWeek"`},
	}
	for _, tc := range cases {
		rec := booksApplet(t)
		rec.Properties["sources"].(map[string]any)["recent"] = map[string]any{
			"kind": bookKind, "where": map[string]any{"finishedAt": map[string]any{"gte": tc.call}},
		}
		problems := CheckRecord(rec.ID, rec.Properties, library)
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, tc.want) }) {
			t.Errorf("%s: want a problem containing %q, got:\n%s", tc.name, tc.want, strings.Join(problems, "\n"))
		}
	}
	rec := booksApplet(t)
	rec.Properties["sources"].(map[string]any)["recent"] = map[string]any{
		"kind": bookKind, "where": map[string]any{"finishedAt": map[string]any{
			"gte": map[string]any{"@call": "addDays", "args": map[string]any{"value": map[string]any{"@now": "date"}, "days": -30}},
		}},
	}
	if problems := CheckRecord(rec.ID, rec.Properties, library); len(problems) > 0 {
		t.Fatalf("a well-formed addDays is refused:\n%s", strings.Join(problems, "\n"))
	}
}

// A kind on the timeline declares none of its timeline columns, and an
// applet still filters a window on at and orders by it.
func TestATimelineKindsColumnsAreItsToFilterAndOrder(t *testing.T) {
	for binding, want := range map[string][]string{
		"substrate.reamde.dev/core/temporal(point)":        {"at"},
		"substrate.reamde.dev/core/temporal(range)":        {"at", "endsAt"},
		"substrate.reamde.dev/core/temporal(point: dueAt)": {"at", "dueAt"},
		"substrate.reamde.dev/core/recurring":              nil,
	} {
		if got := temporalSlots(binding); !slices.Equal(got, want) {
			t.Errorf("%s: got %v, want %v", binding, got, want)
		}
	}
	shape := ShapeOf(substrate.KindInfo{Definition: map[string]any{
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
		"traits":     []any{"substrate.reamde.dev/core/temporal(point: dueAt)", "substrate.reamde.dev/core/recurring"},
	}})
	for _, name := range []string{"name", "at", "dueAt"} {
		if _, ok := shape.Props[name]; !ok {
			t.Errorf("the shape lacks %s: %v", name, shape.Props)
		}
	}
}

// An occurrence a window read computed names the series a step writes.
func TestAnOccurrenceNamesItsSeries(t *testing.T) {
	plant := "ada.example.com/garden/plant"
	occ := recordData(&substrate.Record{ID: "fern_20261115T090000Z", Kind: plant, Computed: true, Properties: map[string]any{"name": "Fern"}}, nil, nil)
	series, _ := occ["series"].(map[string]any)
	if occ["computed"] != true || series["id"] != "fern" || series["ref"] != plant+"/fern" {
		t.Fatalf("occurrence %v", occ)
	}
	stored := recordData(&substrate.Record{ID: "fern", Kind: plant, Properties: map[string]any{}}, nil, nil)
	if _, has := stored["series"]; has {
		t.Fatalf("a stored record names a series: %v", stored)
	}
}

// A source bounding at on both ends reads the window: a repeating record's
// occurrences in it, each naming its series, and the series row itself
// nowhere on the page.
func TestASourceOverAWindowListsWhatComesDue(t *testing.T) {
	plant := "ada.example.com/garden/plant"
	f := newFakeData()
	fern := f.add(plant, "fern", map[string]any{"name": "Fern", "at": "2026-11-05T09:00:00Z", "recurrence": "FREQ=DAILY;INTERVAL=7"})
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	fern.At = &at
	rec := &substrate.Record{ID: "watering", Kind: Kind, Version: 1, Properties: map[string]any{
		"name": "Watering", "start": "due",
		"sources": map[string]any{"due": map[string]any{
			"kind": plant, "orderBy": "at",
			"where": map[string]any{"at": map[string]any{
				"gte": map[string]any{"@now": "datetime"},
				"lt":  map[string]any{"@call": "addDays", "args": map[string]any{"value": map[string]any{"@now": "date"}, "days": 14}},
			}},
		}},
		"screens": map[string]any{"due": map[string]any{"title": "Due", "sources": []any{"due"}, "components": []any{
			map[string]any{"id": "root", "component": "Text", "text": "Due"},
		}}},
	}}
	frame, err := runtime(f).Render(t.Context(), rec, substrate.AppletRenderInput{})
	if err != nil {
		t.Fatal(err)
	}
	var due []any
	for _, m := range frame.Messages {
		if m.CreateSurface != nil {
			due, _ = m.CreateSurface.DataModel["data"].(map[string]any)["due"].([]any)
		}
	}
	var slots []string
	for _, item := range due {
		r := item.(map[string]any)
		series, _ := r["series"].(map[string]any)
		if r["computed"] != true || series["id"] != "fern" {
			t.Fatalf("an occurrence %v", r)
		}
		slots = append(slots, r["properties"].(map[string]any)["at"].(string))
	}
	if want := []string{"2026-11-12T09:00:00Z", "2026-11-19T09:00:00Z"}; !slices.Equal(slots, want) {
		t.Fatalf("slots %v, want %v", slots, want)
	}

	// A window read orders by at alone, so the check refuses another order.
	rec.Properties["sources"].(map[string]any)["due"].(map[string]any)["orderBy"] = "title"
	problems := CheckRecord(rec.ID, rec.Properties, fakeSchema{plant: {Props: map[string]PropShape{"name": {Type: "string"}, "at": {Type: "datetime"}}}})
	if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, "is a window read, ordered by") }) {
		t.Fatalf("problems:\n%s", strings.Join(problems, "\n"))
	}
}
