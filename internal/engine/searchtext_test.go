package engine

import (
	"math"
	"strings"
	"testing"
)

// The index text: an address, a URL and a path add the words they hide, a
// diacritic adds its folded spelling, and plain text is left as it is.
func TestSearchText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"Rack layout review", "Rack layout review"},
		{"ada@inbox.example", "ada@inbox.example inbox.example ada inbox example"},
		{"mail <ada.lovelace@inbox.example>", "mail <ada.lovelace@inbox.example> inbox.example ada lovelace inbox example"},
		{"see https://github.com/geoah/substrate/issues/12", "see https://github.com/geoah/substrate/issues/12 github.com github com geoah substrate issues 12"},
		{"docs/api.md", "docs/api.md docs api md"},
		{"José Ramírez", "José Ramírez Jose Ramirez"},
		// a version, a date and a hyphenated word are the parser's to split
		{"v0.91.0 on 2026-09-26", "v0.91.0 on 2026-09-26"},
		// a slash between numbers hides no word
		{"1/2 cup", "1/2 cup"},
	} {
		if got := searchText(tc.in); got != tc.want {
			t.Errorf("searchText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// BM25F: a rarer word weighs more, repetition saturates, a longer band
// normalizes down, and the title band outweighs prose.
func TestBM25Score(t *testing.T) {
	t.Parallel()
	st := searchStats{n: 1000, avg: [4]float64{4, 10, 100, 0}}
	doc := func(length, tf [4]float64) bm25Doc {
		return bm25Doc{length: length, tf: map[int][4]float64{0: tf}}
	}
	rare, common := []float64{bm25IDF(1000, 2)}, []float64{bm25IDF(1000, 600)}
	if !(bm25IDF(1000, 2) > bm25IDF(1000, 600) && bm25IDF(1000, 999) > 0) {
		t.Fatalf("idf: rare %v, common %v, near-universal %v", bm25IDF(1000, 2), bm25IDF(1000, 600), bm25IDF(1000, 999))
	}
	one := doc([4]float64{4, 10, 100, 0}, [4]float64{0, 0, 1, 0})
	if s1, s2 := bm25Score(one, rare, st), bm25Score(one, common, st); s1 <= s2 {
		t.Errorf("a rare word scores %v, a common one %v", s1, s2)
	}
	once := bm25Score(doc([4]float64{4, 10, 100, 0}, [4]float64{0, 0, 1, 0}), rare, st)
	tenTimes := bm25Score(doc([4]float64{4, 10, 100, 0}, [4]float64{0, 0, 10, 0}), rare, st)
	if tenTimes <= once || tenTimes > 2.3*once {
		t.Errorf("repetition: once %v, ten times %v: it must grow and saturate", once, tenTimes)
	}
	short := bm25Score(doc([4]float64{4, 10, 50, 0}, [4]float64{0, 0, 1, 0}), rare, st)
	long := bm25Score(doc([4]float64{4, 10, 2000, 0}, [4]float64{0, 0, 1, 0}), rare, st)
	if short <= long {
		t.Errorf("length: short prose %v, long prose %v", short, long)
	}
	title := bm25Score(doc([4]float64{4, 10, 100, 0}, [4]float64{1, 0, 0, 0}), rare, st)
	prose := bm25Score(doc([4]float64{4, 10, 100, 0}, [4]float64{0, 0, 1, 0}), rare, st)
	if title <= prose {
		t.Errorf("band: title %v, prose %v", title, prose)
	}
	// An empty repository's statistics still score, without dividing by zero.
	if s := bm25Score(one, rare, searchStats{}); math.IsNaN(s) || math.IsInf(s, 0) {
		t.Errorf("zero statistics scored %v", s)
	}
}

// Fusion: the lexical arm alone keeps its tier, both arms fuse by rank, the
// purpose weight scales, and a demoted row sorts last.
func TestFuseArms(t *testing.T) {
	t.Parallel()
	a, b, c := eref{Kind: "k", ID: "a"}, eref{Kind: "k", ID: "b"}, eref{Kind: "m", ID: "c"}
	flat := func(eref) string { return "primary" }
	ids := func(hs []rankedHit) []string {
		out := make([]string, len(hs))
		for i, h := range hs {
			out[i] = h.id.ID
		}
		return out
	}
	// The row matching every word ranks first however the scores fall.
	lex := map[eref]arm{a: {score: 1, whole: true}, b: {score: 9}}
	if got := ids(fuseArms(lex, nil, flat)); got[0] != "a" {
		t.Errorf("tier: %v", got)
	}
	// Two arms: a row both rank beats rows only one does.
	lex = map[eref]arm{a: {score: 9}, b: {score: 5}}
	sem := map[eref]arm{c: {score: 0.9}, b: {score: 0.8}}
	if got := ids(fuseArms(lex, sem, flat)); got[0] != "b" {
		t.Errorf("fusion: %v", got)
	}
	// The purpose weight moves a supporting record down.
	lex = map[eref]arm{a: {score: 5}, c: {score: 6}}
	supporting := func(id eref) string {
		if id.Kind == "m" {
			return "supporting"
		}
		return "primary"
	}
	if got := ids(fuseArms(lex, nil, supporting)); got[0] != "a" {
		t.Errorf("purpose weight: %v", got)
	}
	// Within a kind, a demoted row ranks below every undemoted one, even
	// with the best score.
	lex = map[eref]arm{a: {score: 1}, b: {score: 9, demoted: true}}
	if got := ids(fuseArms(lex, nil, flat)); strings.Join(got, ",") != "a,b" {
		t.Errorf("demotion within a kind: %v", got)
	}
	// Across kinds a demoted row competes on its weighed score: it outranks
	// machinery it matches better than, and not data it matches worse than.
	internal := func(id eref) string {
		if id.Kind == "m" {
			return "internal"
		}
		return "primary"
	}
	d := eref{Kind: "n", ID: "d"}
	lex = map[eref]arm{b: {score: 9, demoted: true}, c: {score: 10}, d: {score: 6}}
	if got := ids(fuseArms(lex, nil, internal)); strings.Join(got, ",") != "d,b,c" {
		t.Errorf("demotion across kinds: %v, want d,b,c", got)
	}
}
