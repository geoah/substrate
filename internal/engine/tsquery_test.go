package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The search grammar, rendered: what a person types and the tsquery text it
// becomes. These are the promises the console's hint and docs/api.md make.
func TestSearchQueryGrammar(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		// bare words are conjoined, in order
		{"rack layout", "'rack' & 'layout'"},
		{"  rack   layout  ", "'rack' & 'layout'"},
		// a star anywhere marks the word a prefix of its letters
		{"lay*", "'lay':*"},
		{"*lay*", "'lay':*"},
		{"*lay", "'lay':*"},
		{"la*y", "'lay':*"},
		{"rack lay*", "'rack' & 'lay':*"},
		// a quoted run is a phrase; a star inside it still prefixes its word
		{`"rack layout"`, "('rack' <-> 'layout')"},
		{`"rack lay*" review`, "('rack' <-> 'lay':*) & 'review'"},
		{`"rack"`, "'rack'"},
		{`"rack layout`, "('rack' <-> 'layout')"},
		// a dash excludes a word or a phrase
		{"rack -lunch", "'rack' & !'lunch'"},
		{`rack -"weekly sync"`, "'rack' & !('weekly' <-> 'sync')"},
		// OR disjoins; & binds tighter than | in tsquery, as in websearch
		{"rack OR lunch", "'rack' | 'lunch'"},
		{"rack layout OR lunch", "'rack' & 'layout' | 'lunch'"},
		{"OR rack", "'rack'"},
		{"rack OR", "'rack'"},
		{"rack OR OR lunch", "'rack' | 'lunch'"},
		// a lowercase or is a word, exactly as websearch_to_tsquery reads it
		{"rack or lunch", "'rack' & 'or' & 'lunch'"},
		// nothing typed reaches the parser as an operator: the colon is text
		// inside the quotes, and the star is this grammar's prefix mark
		{"geo:*", "'geo:':*"},
		{"a&b|c!d", "'a&b|c!d'"},
		{"(rack)", "'(rack)'"},
		{"it's", "'it''s'"},
		{`back\slash`, `'back\\slash'`},
		{"<->", "'<->'"},
		// stars, quotes and dashes alone match nothing
		{"*", ""},
		{"* **", ""},
		{`""`, ""},
		{"-", ""},
		{"- -", ""},
		{`-"" *`, ""},
		{"", ""},
		{"   ", ""},
		// a dash alone before a word is dropped, not applied to the next word
		{"- rack", "'rack'"},
		// -OR is the word OR, negated
		{"rack -OR", "'rack' & !'OR'"},
	} {
		if got := searchQuery(tc.in); got != tc.want {
			t.Errorf("searchQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A door that needs a query refuses one with no word to match, as a
// validation problem naming the door and the text.
func TestTsqueryTextRefusesAnEmptyQuery(t *testing.T) {
	t.Parallel()
	if _, err := tsqueryText("filter.search", "* -"); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("err = %v, want a validation problem", err)
	} else if msg := err.Error(); !strings.Contains(msg, "filter.search") || !strings.Contains(msg, `"* -"`) {
		t.Fatalf("message = %q, want the door and the text named", msg)
	}
	got, err := tsqueryText("filter.search", "rack")
	if err != nil || got != "'rack'" {
		t.Fatalf("tsqueryText(rack) = %q, %v", got, err)
	}
}
