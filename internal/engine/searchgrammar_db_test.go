package engine_test

// The search grammar at its three doors: the ranked read's `q`, the list's
// `filter.search` arm over the record's index, and a property's `match`. One
// parser renders all three (engine tsquery.go), so a star, a phrase, an
// exclusion and OR mean the same thing wherever words are matched.

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// seedEvents writes the calendar fixture the grammar tests read: three
// events whose title lines and prose share and split the words the queries
// probe for.
func seedEvents(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	cal := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendar", ID: "gcal-cal:primary",
		Properties: map[string]any{"name": "Primary"},
	})
	for _, ev := range []struct{ id, summary, description, location string }{
		{"gcal-event:e1", "Rack layout review", "Weekly sync about the racks", "Room 4"},
		{"gcal-event:e2", "Standup", "we will discuss the rack layout", "Room 4"},
		{"gcal-event:e3", "Lunch", "food and drinks", "Rooftop garden"},
	} {
		mustPut(t, ds, gcal, substrate.PutInput{
			Kind: "calendarevent", ID: ev.id,
			Properties: map[string]any{
				"at": "2026-08-05T13:00:00Z", "endsAt": "2026-08-05T14:00:00Z",
				"summary": ev.summary, "description": ev.description, "location": ev.location,
				"calendar": cal.ID,
			},
		})
	}
}

func listIDs(t *testing.T, ds substrate.Dataset, q substrate.Query) []string {
	t.Helper()
	page, err := ds.List(context.Background(), q)
	if err != nil {
		t.Fatalf("list %+v: %v", q.Filter, err)
	}
	ids := make([]string, 0, len(page.Records))
	for _, e := range page.Records {
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)
	return ids
}

func eventQuery(f substrate.Filter) substrate.Query {
	f.Kinds = []string{"calendarevent"}
	return substrate.Query{Filter: f}
}

// The list's `search` arm is a predicate over the record's index: every
// indexed text at once, in the search grammar, composed with the other arms
// and the caller's order.
func TestFilterSearchMatchesTheRecordIndex(t *testing.T) {
	t.Parallel()
	_, ds := newVocabularyDataset(t, "calendar")
	seedEvents(t, ds)

	for name, tc := range map[string]struct {
		search string
		want   []string
	}{
		"a word, wherever the kind indexes it":       {"rack", []string{"gcal-event:e1", "gcal-event:e2"}},
		"stemmed, so the plural in the prose counts": {"racks", []string{"gcal-event:e1", "gcal-event:e2"}},
		"two words conjoin":                          {"rack layout", []string{"gcal-event:e1", "gcal-event:e2"}},
		"a star is a word prefix":                    {"lay*", []string{"gcal-event:e1", "gcal-event:e2"}},
		"a star on both sides is the same prefix":    {"*lay*", []string{"gcal-event:e1", "gcal-event:e2"}},
		"a prefix nothing starts with":               {"ayout*", nil},
		"a phrase wants the words adjacent":          {`"rack layout"`, []string{"gcal-event:e1", "gcal-event:e2"}},
		"a phrase the prose reorders misses":         {`"layout rack"`, nil},
		"an exclusion":                               {"rack -standup", []string{"gcal-event:e1"}},
		"OR":                                         {"standup OR lunch", []string{"gcal-event:e2", "gcal-event:e3"}},
		"the location is a string property, indexed": {"rooftop", []string{"gcal-event:e3"}},
		"case does not matter":                       {"RACK Layout", []string{"gcal-event:e1", "gcal-event:e2"}},
		"a word no record has":                       {"quarterly", nil},
	} {
		t.Run(name, func(t *testing.T) {
			got := listIDs(t, ds, eventQuery(substrate.Filter{Search: tc.search}))
			if !equalStrings(got, tc.want) {
				t.Fatalf("search %q = %v, want %v", tc.search, got, tc.want)
			}
		})
	}

	// It composes: a property predicate beside it narrows further.
	got := listIDs(t, ds, eventQuery(substrate.Filter{
		Search:     "rack",
		Properties: map[string]substrate.Cond{"summary": {Eq: "Standup"}},
	}))
	if !equalStrings(got, []string{"gcal-event:e2"}) {
		t.Fatalf("search + eq = %v", got)
	}
	// The list keeps the caller's order and pages by keyset like any list.
	page, err := ds.List(context.Background(), substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{"calendarevent"}, Search: "rack"},
		OrderBy: []substrate.Order{{Property: "summary"}},
		First:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != "gcal-event:e1" || page.Cursor == "" {
		t.Fatalf("first page = %v cursor %q, want e1 (Rack… before Standup) and a cursor", pageIDs(page), page.Cursor)
	}
	page, err = ds.List(context.Background(), substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{"calendarevent"}, Search: "rack"},
		OrderBy: []substrate.Order{{Property: "summary"}},
		First:   1, After: page.Cursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != "gcal-event:e2" || page.Cursor != "" {
		t.Fatalf("second page = %v cursor %q, want e2 and the end", pageIDs(page), page.Cursor)
	}
	// A query with no word to match is refused, not answered empty.
	if _, err := ds.List(context.Background(), eventQuery(substrate.Filter{Search: "* -"})); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("search of stars: err = %v, want validation", err)
	} else if !strings.Contains(err.Error(), "filter.search") {
		t.Fatalf("message = %q, want the arm named", err)
	}
}

// `match` is the grammar over ONE property's own words: the title line, the
// prose, a repeated string's items, the built-in title; and refused, by name,
// on a property that has no words.
func TestCondMatchIsOnePropertysWords(t *testing.T) {
	t.Parallel()
	_, ds := newVocabularyDataset(t, "calendar", "people")
	seedEvents(t, ds)

	match := func(prop, q string) substrate.Filter {
		return substrate.Filter{Properties: map[string]substrate.Cond{prop: {Match: q}}}
	}
	for name, tc := range map[string]struct {
		prop, q string
		want    []string
	}{
		"the title line, a word":                   {"summary", "rack", []string{"gcal-event:e1"}},
		"the title line, a prefix":                 {"summary", "lay*", []string{"gcal-event:e1"}},
		"the prose, not the title line":            {"description", "rack", []string{"gcal-event:e1", "gcal-event:e2"}},
		"the prose, a phrase":                      {"description", `"rack layout"`, []string{"gcal-event:e2"}},
		"the prose, an exclusion":                  {"description", "rack -weekly", []string{"gcal-event:e2"}},
		"another string property alone":            {"location", "room", []string{"gcal-event:e1", "gcal-event:e2"}},
		"a word the property never holds":          {"location", "rack", nil},
		"the built-in title is the title property": {"title", "standup", []string{"gcal-event:e2"}},
	} {
		t.Run(name, func(t *testing.T) {
			got := listIDs(t, ds, eventQuery(match(tc.prop, tc.q)))
			if !equalStrings(got, tc.want) {
				t.Fatalf("match %s %q = %v, want %v", tc.prop, tc.q, got, tc.want)
			}
		})
	}

	// A repeated string property matches over its items.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", ID: "p-ada",
		Properties: map[string]any{"name": "Ada", "phones": []any{"+44 20 7946 0958", "office landline"}},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", ID: "p-sam",
		Properties: map[string]any{"name": "Sam", "phones": []any{"mobile"}},
	})
	got := listIDs(t, ds, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{"person"}, Properties: map[string]substrate.Cond{"phones": {Match: "landline"}},
	}})
	if !equalStrings(got, []string{"p-ada"}) {
		t.Fatalf("match over a repeated string = %v", got)
	}

	// Refused, naming the property, where there are no words to match.
	for name, tc := range map[string]struct {
		kind, prop, want string
	}{
		"an instant in a hot column": {"calendarevent", "at", "at is not a text property"},
		"an instant in props":        {"calendarevent", "originalAt", "originalAt is datetime"},
		"a reference":                {"calendarevent", "calendar", "calendar is a reference"},
		"a state":                    {"person", "prominence", "prominence is a state"},
		"the id column":              {"person", "id", "id is not a text property"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
				Kinds: []string{tc.kind}, Properties: map[string]substrate.Cond{tc.prop: {Match: "x"}},
			}})
			if !errors.Is(err, substrate.ErrValidation) {
				t.Fatalf("err = %v, want validation", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("message = %q, want %q", err, tc.want)
			}
		})
	}
	// And a match with no word is refused, naming the property.
	if _, err := ds.List(context.Background(), eventQuery(match("summary", "*"))); !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "summary") {
		t.Fatalf("match of a star: err = %v", err)
	}
}

// The ranked read ranks by the same grammar, so a prefix, a phrase and an
// exclusion rank the rows the list arm admits.
func TestRankedReadTakesTheSearchGrammar(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newVocabularyDataset(t, "calendar")
	seedEvents(t, ds)

	for name, tc := range map[string]struct {
		q    string
		want []string
	}{
		"a prefix":     {"lay*", []string{"gcal-event:e1", "gcal-event:e2"}},
		"a phrase":     {`"rack layout"`, []string{"gcal-event:e1", "gcal-event:e2"}},
		"an exclusion": {"rack -standup", []string{"gcal-event:e1"}},
		"OR":           {"standup OR lunch", []string{"gcal-event:e2", "gcal-event:e3"}},
	} {
		t.Run(name, func(t *testing.T) {
			hits, err := searchHits(ds.Search(ctx, substrate.SearchInput{Q: tc.q, Mode: substrate.SearchLexical}))
			if err != nil {
				t.Fatal(err)
			}
			got := hitIDs(hits)
			sort.Strings(got)
			if !equalStrings(got, tc.want) {
				t.Fatalf("q %q = %v, want %v", tc.q, got, tc.want)
			}
		})
	}
	// The title band still outranks the prose with a prefix query.
	hits, err := searchHits(ds.Search(ctx, substrate.SearchInput{Q: "rack lay*", Mode: substrate.SearchLexical}))
	if err != nil {
		t.Fatal(err)
	}
	if ids := hitIDs(hits); len(ids) != 2 || ids[0] != "gcal-event:e1" {
		t.Fatalf("ranking = %v, want the titled event first", ids)
	}
	// A query of stars alone is refused as a validation problem naming q.
	if _, err := ds.Search(ctx, substrate.SearchInput{Q: "**", Mode: substrate.SearchLexical}); !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "q:") {
		t.Fatalf("q of stars: err = %v", err)
	}
}

func pageIDs(page *substrate.Page) []string {
	ids := make([]string, 0, len(page.Records))
	for _, e := range page.Records {
		ids = append(ids, e.ID)
	}
	return ids
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
