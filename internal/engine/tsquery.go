package engine

import (
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// THE SEARCH GRAMMAR. One parser turns what a person typed into the text
// to_tsquery('english', …) parses, and every door that matches words against
// a record reads it: the ranked read's lexical arm, the list's `search` arm
// and a property's `match`. It is websearch_to_tsquery's grammar plus the one
// thing that function cannot express, a word prefix:
//
//	rack layout        every word, in any order       'rack' & 'layout'
//	"rack layout"      the words adjacent, in order    'rack' <-> 'layout'
//	-lunch             the word absent                 !'lunch'
//	rack OR lunch      either word                     'rack' | 'lunch'
//	lay*               a word starting with lay        'lay':*
//
// A star anywhere in a word marks it a prefix of its letters: `*lay*` and
// `lay*` are the same query, because a tsvector holds whole lexemes and can
// answer "starts with" but never "contains". Every lexeme is single-quoted
// on the way out, so nothing a person types reaches the tsquery parser as an
// operator: a typed `geo:*` is the quoted lexeme geo:, not a prefix.
// Stemming and stop words are the dictionary's, applied by to_tsquery to the
// quoted text exactly as to_tsvector applied them to the row.

// searchQuery renders q in the grammar above, or "" when nothing in it can
// match: a query of stars, quotes and dashes alone.
func searchQuery(q string) string {
	var out []string // the rendered operands and operators, in order
	pendingOr := false
	pushOperand := func(s string) {
		if len(out) > 0 {
			if pendingOr {
				out = append(out, "|")
			} else {
				out = append(out, "&")
			}
		}
		pendingOr = false
		out = append(out, s)
	}
	for _, tok := range tokenize(q) {
		if tok.or {
			// A leading or doubled OR has no left operand; it is dropped, as
			// websearch_to_tsquery drops it.
			pendingOr = len(out) > 0
			continue
		}
		var terms []string
		for _, w := range tok.words {
			if t := lexeme(w); t != "" {
				terms = append(terms, t)
			}
		}
		if len(terms) == 0 {
			continue
		}
		operand := strings.Join(terms, " <-> ")
		if tok.phrase && len(terms) > 1 {
			operand = "(" + operand + ")"
		}
		if tok.negated {
			operand = "!" + operand
		}
		pushOperand(operand)
	}
	// A trailing OR was never followed by an operand; the join list is whole
	// without it because the operator is only ever emitted between operands.
	return strings.Join(out, " ")
}

// searchTerm is one operand as typed: a word, or a quoted phrase of words,
// each optionally negated; or the OR keyword.
type searchTerm struct {
	words   []string
	phrase  bool
	negated bool
	or      bool
}

// tokenize splits q into operands: whitespace-separated words, with a quoted
// run kept together as one phrase (an unclosed quote runs to the end), a
// leading `-` negating what follows it, and the bare word OR (uppercase, as
// websearch_to_tsquery spells it) the disjunction.
func tokenize(q string) []searchTerm {
	var terms []searchTerm
	rest := q
	for {
		rest = strings.TrimLeft(rest, " \t\r\n")
		if rest == "" {
			return terms
		}
		negated := false
		if strings.HasPrefix(rest, "-") {
			negated = true
			rest = rest[1:]
		}
		if strings.HasPrefix(rest, `"`) {
			rest = rest[1:]
			body := rest
			if i := strings.IndexByte(rest, '"'); i >= 0 {
				body, rest = rest[:i], rest[i+1:]
			} else {
				rest = ""
			}
			terms = append(terms, searchTerm{words: strings.Fields(body), phrase: true, negated: negated})
			continue
		}
		word := rest
		if i := strings.IndexAny(rest, " \t\r\n"); i >= 0 {
			word, rest = rest[:i], rest[i:]
		} else {
			rest = ""
		}
		if word == "" {
			// A dash alone: nothing to negate.
			continue
		}
		if word == "OR" && !negated {
			terms = append(terms, searchTerm{or: true})
			continue
		}
		terms = append(terms, searchTerm{words: []string{word}, negated: negated})
	}
}

// lexeme quotes one typed word for to_tsquery: `'…'` with the quote doubled
// and the backslash escaped, `:*` appended when the word carried a star. A
// word that was stars alone is "" and matches nothing.
func lexeme(word string) string {
	prefix := strings.Contains(word, "*")
	word = strings.ReplaceAll(word, "*", "")
	if word == "" {
		return ""
	}
	quoted := "'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(word) + "'"
	if prefix {
		quoted += ":*"
	}
	return quoted
}

// tsqueryText is searchQuery for a door that REFUSES an empty result, naming
// where the query came from: a search whose every word was a star, a quote or
// a dash matches nothing and would read as "no results" instead of "no
// query".
func tsqueryText(where, q string) (string, error) {
	tq := searchQuery(q)
	if tq == "" {
		return "", fmt.Errorf("%w: %s: %q has no word to match", substrate.ErrValidation, where, q)
	}
	return tq, nil
}
