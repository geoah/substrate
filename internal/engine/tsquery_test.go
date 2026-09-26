package engine

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// rendered is the plan's SQL with every bound argument written in place, so a
// case reads as the query Postgres receives. E(x) is to_tsquery('english', x)
// and S(x) is to_tsquery('simple', x).
func rendered(t *testing.T, q string, relaxed bool) string {
	t.Helper()
	p := parseSearch(q)
	if p.empty() {
		return ""
	}
	b := &builder{}
	sql := p.expr(b, relaxed)
	sql = regexp.MustCompile(`\$(\d+)`).ReplaceAllStringFunc(sql, func(m string) string {
		n, _ := strconv.Atoi(m[1:])
		return fmt.Sprint(b.args[n-1])
	})
	return strings.NewReplacer("to_tsquery('english', ", "E(", "to_tsquery('simple', ", "S(").Replace(sql)
}

// The search grammar, rendered: what a person types and the tsquery it
// becomes. These are the promises the console's hint and docs/api.md make.
func TestSearchQueryGrammar(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		// bare words are conjoined, in order
		{"rack layout", "((E('rack') && E('layout')))"},
		{"  rack   layout  ", "((E('rack') && E('layout')))"},
		// a star anywhere marks the word a prefix of its letters, read
		// unstemmed beside the word's own stem
		{"lay*", "(((E('lay') || S('lay':*))))"},
		{"*lay*", "(((E('lay') || S('lay':*))))"},
		{"la*y", "(((E('lay') || S('lay':*))))"},
		{"rack lay*", "((E('rack') && (E('lay') || S('lay':*))))"},
		// a quoted run is a phrase; a star inside it still prefixes its word
		{`"rack layout"`, "((E('rack' <-> 'layout')))"},
		{`"rack lay*" review`, "((E('rack' <-> 'lay':*) && E('review')))"},
		{`"rack"`, "((E('rack')))"},
		{`"rack layout`, "((E('rack' <-> 'layout')))"},
		// a dash excludes a word or a phrase
		{"rack -lunch", "((E('rack') && (!! E('lunch'))))"},
		{`rack -"weekly sync"`, "((E('rack') && (!! E('weekly' <-> 'sync'))))"},
		// OR disjoins; AND binds tighter, as in websearch
		{"rack OR lunch", "((E('rack')) || (E('lunch')))"},
		{"rack layout OR lunch", "((E('rack') && E('layout')) || (E('lunch')))"},
		{"OR rack", "((E('rack')))"},
		{"rack OR", "((E('rack')))"},
		{"rack OR OR lunch", "((E('rack')) || (E('lunch')))"},
		// a lowercase or is a word, exactly as websearch_to_tsquery reads it
		{"rack or lunch", "((E('rack') && E('or') && E('lunch')))"},
		// a diacritic also matches its folded spelling
		{"José", "(((E('José') || E('Jose'))))"},
		{"Jos*", "(((E('Jos') || S('Jos':*))))"},
		{`"José Ramírez"`, "(((E('José' <-> 'Ramírez') || E('Jose' <-> 'Ramirez'))))"},
		// nothing typed reaches the parser as an operator: the colon is text
		// inside the quotes, and the star is this grammar's prefix mark
		{"geo:*", "(((E('geo:') || S('geo:':*))))"},
		{"a&b|c!d", "((E('a&b|c!d')))"},
		{"(rack)", "((E('(rack)')))"},
		{"it's", "((E('it''s')))"},
		{`back\slash`, `((E('back\\slash')))`},
		{"<->", "((E('<->')))"},
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
		{"- rack", "((E('rack')))"},
		// -OR is the word OR, negated
		{"rack -OR", "((E('rack') && (!! E('OR'))))"},
	} {
		if got := rendered(t, tc.in, false); got != tc.want {
			t.Errorf("parseSearch(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// The ranked read's candidates are the relaxed query: a plain conjunction
// becomes the disjunction of its positive operands with every exclusion kept,
// and a query that says OR is left as written.
func TestRelaxedQuery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"rack layout", "((E('rack') || E('layout')))"},
		{"rack", "((E('rack')))"},
		{"rack layout -lunch", "((E('rack') || E('layout')) && (!! E('lunch')))"},
		{`"rack layout" review`, "((E('rack' <-> 'layout') || E('review')))"},
		{"rack OR lunch", "((E('rack')) || (E('lunch')))"},
		// exclusions alone have nothing to relax
		{"-lunch", "(((!! E('lunch'))))"},
	} {
		if got := rendered(t, tc.in, true); got != tc.want {
			t.Errorf("relaxed %q = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// The scoring terms are the positive words, folded, each once; an exclusion
// matches but never scores.
func TestSearchPlanTerms(t *testing.T) {
	t.Parallel()
	got := parseSearch(`José rack* -lunch "rack layout" OR rack`).terms()
	want := []scoreTerm{{"Jose", false}, {"rack", true}, {"rack", false}, {"layout", false}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("terms = %v, want %v", got, want)
	}
}

// A door that needs a query refuses one with no word to match, as a
// validation problem naming the door and the text.
func TestSearchExprRefusesAnEmptyQuery(t *testing.T) {
	t.Parallel()
	if _, err := searchExpr(&builder{}, "filter.search", "* -"); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("err = %v, want a validation problem", err)
	} else if msg := err.Error(); !strings.Contains(msg, "filter.search") || !strings.Contains(msg, `"* -"`) {
		t.Fatalf("message = %q, want the door and the text named", msg)
	}
	if _, err := searchExpr(&builder{}, "filter.search", "rack"); err != nil {
		t.Fatalf("searchExpr(rack) = %v", err)
	}
}

func TestFoldDiacritics(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"José": "Jose", "Ångström": "Angstrom", "naïve café": "naive cafe",
		"plain": "plain", "straße": "straße", "Łódź": "Łodz",
	} {
		if got := foldDiacritics(in); got != want {
			t.Errorf("foldDiacritics(%q) = %q, want %q", in, got, want)
		}
	}
}
