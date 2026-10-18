package vocabulary

// THE UCUM ATOM INVENTORY, held against the input aliases.
//
// testdata/ucum/atoms.tsv lists every atom code UCUM 2.2 defines (its NOTICE
// says where it came from and under what license), and the grammar carries a
// subset of them (atomDefs). An alias is applied before the grammar reads a
// unit, so an alias spelled like a UCUM expression turns a UCUM writer's unit
// into another one, even over an atom this grammar does not carry yet: UCUM's
// `st` is the stere, a cubic meter, and never the stone. buildTable refuses an
// alias the grammar parses; this test also refuses one the rest of UCUM does.

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// ucumAtoms reads testdata/ucum/atoms.tsv: each atom code UCUM defines, and
// whether UCUM marks it metric.
func ucumAtoms(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open("testdata/ucum/atoms.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	atoms := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		code, metric, ok := strings.Cut(line, "\t")
		if !ok || (metric != "metric" && metric != "nonmetric") {
			t.Fatalf("atoms.tsv: %q is not a code, a tab and metric or nonmetric", line)
		}
		atoms[code] = metric == "metric"
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	// UCUM 2.2 defines 312; a short read is a broken file, not a smaller UCUM.
	if len(atoms) != 312 {
		t.Fatalf("atoms.tsv lists %d atoms, want UCUM 2.2's 312", len(atoms))
	}
	return atoms
}

// ucumUnimplemented is the atoms UCUM defines that the grammar does not
// carry, each with whether it is metric.
func ucumUnimplemented(t *testing.T) map[string]bool {
	t.Helper()
	carried := map[string]bool{}
	for _, a := range atomDefs {
		carried[a.code] = true
	}
	out := map[string]bool{}
	for code, metric := range ucumAtoms(t) {
		if !carried[code] {
			out[code] = metric
		}
	}
	return out
}

// spellsUCUM reports whether s reads as a UCUM expression over the whole
// inventory: the grammar parses it, or every component is an atom the grammar
// carries or one only `unimplemented` lists, alone or under a prefix when the
// atom is metric, with an optional integer exponent, beside integer factors
// and annotations, joined by `.` and `/` and grouped by parentheses.
func spellsUCUM(s string, unimplemented map[string]bool) bool {
	if _, err := ParseUnit(s); err == nil {
		return true
	}
	for _, r := range s {
		if r < '!' || r > '~' {
			return false
		}
	}
	components, ok := ucumComponents(s)
	if !ok {
		return false
	}
	for _, c := range components {
		if !ucumComponent(c, unimplemented) {
			return false
		}
	}
	return true
}

// ucumComponents splits s on `.`, `/` and parentheses outside brackets and
// braces, which an atom (`B[10.nV]`) or an annotation may contain.
func ucumComponents(s string) ([]string, bool) {
	var out []string
	var cur strings.Builder
	square, curly := 0, 0
	for _, r := range s {
		switch {
		case r == '[':
			square++
		case r == ']':
			square--
		case r == '{':
			curly++
		case r == '}':
			curly--
		}
		if square < 0 || curly < 0 {
			return nil, false
		}
		if square == 0 && curly == 0 && strings.ContainsRune("./()", r) {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	if square != 0 || curly != 0 {
		return nil, false
	}
	return append(out, cur.String()), true
}

// ucumComponent reports whether one component is a UCUM term: empty (beside
// an operator), an integer factor, an annotation, or a simple unit with an
// optional annotation and integer exponent.
func ucumComponent(c string, unimplemented map[string]bool) bool {
	if i := strings.IndexByte(c, '{'); i >= 0 && strings.HasSuffix(c, "}") {
		c = c[:i]
	}
	if c == "" || strings.Trim(c, "0123456789") == "" {
		return true
	}
	symbol := strings.TrimRight(c, "0123456789")
	symbol = strings.TrimRight(symbol, "+-")
	if symbol == "" {
		symbol = c
	}
	if _, listed := unimplemented[symbol]; listed {
		return true
	}
	if _, err := ParseUnit(symbol); err == nil {
		return true
	}
	for _, p := range prefixDefs {
		rest, prefixed := strings.CutPrefix(symbol, p.code)
		if metric, listed := unimplemented[rest]; prefixed && listed && metric {
			return true
		}
	}
	return false
}

func TestUnitAliasesSpellNoUCUMCode(t *testing.T) {
	unimplemented := ucumUnimplemented(t)
	t.Logf("UCUM atoms the grammar does not carry: %d", len(unimplemented))
	// The check knows what the grammar does not: the stere alone, prefixed
	// and in a compound, and the femtotonne `ft` the grammar already reads.
	for _, s := range []string{"st", "cst", "st/h", "m3.st-1", "ft"} {
		if !spellsUCUM(s, unimplemented) {
			t.Errorf("%q reads as no UCUM expression, and UCUM defines it", s)
		}
	}
	for alias := range unitAliases {
		if spellsUCUM(alias, unimplemented) {
			t.Errorf("alias %q is a UCUM expression, so it would shadow one", alias)
		}
	}
}
