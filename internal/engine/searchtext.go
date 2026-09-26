package engine

import (
	"strings"
	"unicode"
)

// searchIndexVersion names the rule set below together with ftsBands. A
// change to either changes what a row indexes without changing the row, so it
// bumps this number and the next open re-derives every row's `fts`
// (reindexSearch in searchindex.go). 1 is the rule set before the number
// existed: the bands alone.
const searchIndexVersion = 2

// searchText is one band's text as the index reads it: the text, then the
// variants to_tsvector cannot derive from it.
//
// The english parser keeps an email address, a URL and a path as one token,
// so `ada@example.com` indexed as that lexeme alone and a search for `ada`
// never found it. Each such token adds its parts: the address's local part and
// domain, a URL's host and path segments, split again on the characters that
// join words inside them.
//
// A word carrying a diacritic adds its folded spelling (foldDiacritics), so a
// person who types `jose` finds `José`. The query side folds too
// (tsquery.go), so the accented query still matches the accented row.
//
// The variants append after the text, so every position the text had is
// unchanged and a phrase over the text still matches exactly as before.
func searchText(s string) string {
	if s == "" {
		return s
	}
	var extra []string
	for _, tok := range strings.Fields(s) {
		if compoundToken(tok) {
			extra = append(extra, compoundParts(tok)...)
		}
		if folded := foldDiacritics(tok); folded != tok {
			extra = append(extra, folded)
		}
	}
	if len(extra) == 0 {
		return s
	}
	return s + " " + strings.Join(extra, " ")
}

// compoundToken reports whether the parser would keep tok as one lexeme that
// hides words: an address, a URL or a path.
func compoundToken(tok string) bool {
	return strings.ContainsAny(tok, "@/") && strings.IndexFunc(tok, unicode.IsLetter) >= 0
}

// compoundParts splits an address, a URL or a path into what a person would
// type for it: the domain of an address whole (`example.com`), and every run
// of letters and digits. A scheme (`https`) is not a word anyone searches by.
func compoundParts(tok string) []string {
	tok = strings.Trim(tok, `<>()[]{}"',;`)
	var out []string
	if at := strings.LastIndexByte(tok, '@'); at >= 0 && at < len(tok)-1 {
		if domain := strings.TrimRight(tok[at+1:], "."); strings.Contains(domain, ".") {
			out = append(out, domain)
		}
	}
	if i := strings.Index(tok, "://"); i >= 0 {
		tok = tok[i+3:]
		if host, _, _ := strings.Cut(tok, "/"); strings.Contains(host, ".") {
			out = append(out, host)
		}
	}
	return append(out, strings.FieldsFunc(tok, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})...)
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
