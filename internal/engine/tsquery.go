package engine

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/geoah/substrate/internal/substrate"
)

// THE SEARCH GRAMMAR. One parser turns what a person typed into a tsquery,
// and every door that matches words against a record reads it: the ranked
// read's lexical arm, the list's `search` arm and a property's `match`. It is
// websearch_to_tsquery's grammar plus the one thing that function cannot
// express, a word prefix:
//
//	rack layout        every word, in any order       'rack' & 'layout'
//	"rack layout"      the words adjacent, in order    'rack' <-> 'layout'
//	-lunch             the word absent                 !'lunch'
//	rack OR lunch      either word                     'rack' | 'lunch'
//	lay*               a word starting with lay        'lay':*
//
// A star anywhere in a word marks it a prefix of its letters: `*lay*` and
// `lay*` are the same query, because a tsvector holds whole lexemes and can
// answer "starts with" but never "contains".
//
// The query is a SQL EXPRESSION of tsquery values, one to_tsquery call per
// operand joined by the tsquery operators, never one tsquery text, because
// the operands need two dictionaries. A whole word is stemmed by `english`,
// exactly as to_tsvector stemmed the row. A prefix is NOT stemmed: the
// stemmer reads `ans` as a word and cuts it to `an`, so a stemmed `ans*`
// matched every lexeme starting with `an`. A prefix reads through `simple`
// (lowercased, nothing cut) and also matches its own stem exactly, so `run*`
// finds `running` through the stem and `runway` through the prefix.
//
// A word carrying a diacritic also matches its folded spelling, and the index
// holds a folded copy of every such word (searchtext.go), so `José` and
// `jose` find the same rows.
//
// Every lexeme is single-quoted inside its operand, so nothing a person types
// reaches the tsquery parser as an operator: a typed `geo:*` is the quoted
// lexeme geo: as a prefix, not a weight label.

// searchOperand is one operand as typed: a word or a quoted phrase, optionally
// negated.
type searchOperand struct {
	words   []string // the typed words, stars removed; one for a word
	phrase  bool
	negated bool
	// prefix marks a word typed with a star. A phrase keeps a star per word
	// in stars instead, because it is one english tsquery.
	prefix bool
	stars  []bool
}

// searchPlan is a parsed query: operands in order, grouped by OR. Each group
// is a conjunction; the plan is the disjunction of its groups, because & binds
// tighter than | exactly as in websearch_to_tsquery.
type searchPlan struct {
	groups [][]searchOperand
	hasOr  bool
}

// empty reports whether nothing in the query can match: stars, quotes and
// dashes alone.
func (p searchPlan) empty() bool { return len(p.groups) == 0 }

// parseSearch reads q in the grammar above.
func parseSearch(q string) searchPlan {
	var p searchPlan
	var cur []searchOperand
	pendingOr := false
	for _, tok := range tokenize(q) {
		if tok.or {
			// A leading or doubled OR has no left operand; it is dropped, as
			// websearch_to_tsquery drops it.
			pendingOr = len(cur) > 0 || len(p.groups) > 0
			continue
		}
		op := searchOperand{phrase: tok.phrase, negated: tok.negated}
		for _, w := range tok.words {
			star := strings.Contains(w, "*")
			w = strings.ReplaceAll(w, "*", "")
			if w == "" {
				continue
			}
			op.words = append(op.words, w)
			op.stars = append(op.stars, star)
		}
		if len(op.words) == 0 {
			continue
		}
		if !op.phrase || len(op.words) == 1 {
			// A one-word phrase is the word.
			op.phrase = false
			op.prefix = op.stars[0]
			op.words, op.stars = op.words[:1], nil
		}
		if pendingOr && len(cur) > 0 {
			p.groups = append(p.groups, cur)
			cur = nil
			p.hasOr = true
		}
		pendingOr = false
		cur = append(cur, op)
	}
	// A trailing OR was never followed by an operand, so it opened no group.
	if len(cur) > 0 {
		p.groups = append(p.groups, cur)
	}
	return p
}

// expr renders the plan as a tsquery-valued SQL expression, binding every
// lexeme through b. Strict is the grammar as typed. Relaxed is the ranked
// read's CANDIDATE set: a conjunction with no OR becomes the disjunction of
// its positive operands, each exclusion still applied, so a query with one
// word no row holds still finds the rows holding the others. A query that
// already says OR is its own relaxation: the person wrote the logic.
func (p searchPlan) expr(b *builder, relaxed bool) string {
	if relaxed && !p.hasOr && len(p.groups) == 1 {
		var pos, neg []string
		for _, op := range p.groups[0] {
			if op.negated {
				neg = append(neg, op.sql(b))
			} else {
				pos = append(pos, op.sql(b))
			}
		}
		if len(pos) > 0 {
			return "(" + strings.Join(append([]string{"(" + strings.Join(pos, " || ") + ")"}, neg...), " && ") + ")"
		}
	}
	groups := make([]string, 0, len(p.groups))
	for _, g := range p.groups {
		ops := make([]string, 0, len(g))
		for _, op := range g {
			ops = append(ops, op.sql(b))
		}
		groups = append(groups, "("+strings.Join(ops, " && ")+")")
	}
	return "(" + strings.Join(groups, " || ") + ")"
}

// sql renders one operand. See the header for why a prefix reads through
// `simple` and a folded spelling rides beside an accented one.
func (op searchOperand) sql(b *builder) string {
	var alts []string
	if op.phrase {
		for _, words := range spellings(op.words) {
			parts := make([]string, len(words))
			for i, w := range words {
				parts[i] = quoteLexeme(w)
				if op.stars[i] {
					parts[i] += ":*"
				}
			}
			alts = append(alts, `to_tsquery('english', `+b.arg(strings.Join(parts, " <-> "))+`)`)
		}
	} else {
		for _, words := range spellings(op.words) {
			q := quoteLexeme(words[0])
			alts = append(alts, `to_tsquery('english', `+b.arg(q)+`)`)
			if op.prefix {
				alts = append(alts, `to_tsquery('simple', `+b.arg(q+":*")+`)`)
			}
		}
	}
	s := alts[0]
	if len(alts) > 1 {
		s = "(" + strings.Join(alts, " || ") + ")"
	}
	if op.negated {
		s = "(!! " + s + ")"
	}
	return s
}

// spellings is the typed words and, where folding changes any of them, the
// folded words beside them.
func spellings(words []string) [][]string {
	folded := make([]string, len(words))
	changed := false
	for i, w := range words {
		folded[i] = foldDiacritics(w)
		changed = changed || folded[i] != w
	}
	if !changed {
		return [][]string{words}
	}
	return [][]string{words, folded}
}

// scoreTerm is one positive word the lexical arm scores (bm25.go): the folded
// word as typed and whether it was a prefix. Exclusions match but never score.
type scoreTerm struct {
	word   string
	prefix bool
}

// terms lists the plan's scoring words, each once.
func (p searchPlan) terms() []scoreTerm {
	seen := map[scoreTerm]bool{}
	var out []scoreTerm
	add := func(t scoreTerm) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, g := range p.groups {
		for _, op := range g {
			if op.negated {
				continue
			}
			for i, w := range op.words {
				prefix := op.prefix || (op.phrase && op.stars[i])
				add(scoreTerm{word: foldDiacritics(w), prefix: prefix})
			}
		}
	}
	return out
}

// searchTerm is one token as typed: a word, or a quoted phrase of words,
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

// quoteLexeme quotes one typed word for to_tsquery: `'…'` with the quote
// doubled and the backslash escaped.
func quoteLexeme(word string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(word) + "'"
}

// foldDiacritics drops the combining marks a decomposition leaves, so `José`
// reads `Jose` and `Ångström` reads `Angstrom`. A letter with no
// decomposition (ß, ø, ł) is kept: folding it is a language's rule, not
// Unicode's.
func foldDiacritics(s string) string {
	if isASCII(s) {
		return s
	}
	var out strings.Builder
	for _, r := range norm.NFD.String(s) {
		if !unicode.Is(unicode.Mn, r) {
			out.WriteRune(r)
		}
	}
	return norm.NFC.String(out.String())
}

// searchExpr parses q for a door that REFUSES an empty result, naming where
// the query came from: a search whose every word was a star, a quote or a dash
// matches nothing and would read as "no results" instead of "no query".
func searchExpr(b *builder, where, q string) (string, error) {
	p := parseSearch(q)
	if p.empty() {
		return "", fmt.Errorf("%w: %s: %q has no word to match", substrate.ErrValidation, where, q)
	}
	return p.expr(b, false), nil
}
