package vocabulary_test

// THE COPIES OF THE UNIT TABLE, written from it.
//
// The console lists measures and units in its quantity picker, renders print
// symbols with the engine's print rules and reads the aliases a person types
// (web/console/src/lib/units.ts, which imports units.table.json), and its
// tests hold those symbols to the engine's own (units.prints.json).
// docs/units.md lists every measure and code for a reader. All three are
// written from the table by these tests, so none can drift: a table change
// fails them until
//
//	go test ./internal/vocabulary/ -run 'TestConsoleUnitsMatchTheEngine|TestUnitsPage' -update
//
// rewrites them. Do not edit any by hand.

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

var updateGenerated = flag.Bool("update", false, "rewrite the console's unit table and docs/units.md from the engine's")

const (
	consoleUnitTable  = "../../web/console/src/lib/units.table.json"
	consolePrintCases = "../../web/console/src/lib/units.prints.json"
	unitsPage         = "../../docs/units.md"
)

type consoleMeasure struct {
	Name      string   `json:"name"`
	Base      string   `json:"base"`
	Scale     string   `json:"scale"`
	Display   string   `json:"display"`
	Dimension string   `json:"dimension"`
	Units     []string `json:"units"`
	// Converts is the measure's codes in classes the server converts a
	// value between: two codes convert exactly when they share a class. A
	// code no other converts to is a class of one.
	Converts [][]string `json:"converts"`
	// Factors is each code's exact map to the measure's base,
	// base = value * factor + offset, as rationals ("1/1000"), so the console
	// converts a value between two codes of one class without asking the
	// server. A code no rational factor reaches is left out.
	Factors map[string]consoleFactor `json:"factors,omitempty"`
	Note    string                   `json:"note,omitempty"`
}

type consoleFactor struct {
	Factor string `json:"factor"`
	Offset string `json:"offset,omitempty"`
}

// baseFactors is every code of a measure that converts at all, with its
// exact map to the base.
func baseFactors(m vocabulary.Measure) map[string]consoleFactor {
	out := map[string]consoleFactor{}
	for _, code := range m.Units {
		factor, offset, err := vocabulary.ToBase(m.Name, code)
		if err != nil {
			continue
		}
		f := consoleFactor{Factor: factor.RatString()}
		if offset.Sign() != 0 {
			f.Offset = offset.RatString()
		}
		out[code] = f
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type consolePrint struct {
	Prefixes    map[string]string `json:"prefixes"`
	Atoms       map[string]string `json:"atoms"`
	Prints      map[string]string `json:"prints"`
	Wholes      []string          `json:"wholes"`
	Annotations []string          `json:"annotations"`
}

type consoleTable struct {
	Measures []consoleMeasure `json:"measures"`
	// Symbols is every listed code's print symbol.
	Symbols map[string]string `json:"symbols"`
	// SymbolCodes are the print symbols a person may type for a listed code:
	// each one no code and no alias spells, so reading one never turns a
	// stored code into another (`pt` is the picotonne, never the pint).
	SymbolCodes map[string]string `json:"symbolCodes"`
	// Print is what the engine's print renderer reads beside the grammar.
	Print consolePrint `json:"print"`
	// Primary is where a code two measures list reads undeclared.
	Primary map[string]string `json:"primary"`
	// Aliases are the input spellings and the codes they map to.
	Aliases map[string]string `json:"aliases"`
	// Refused are the spellings CanonicalUnit refuses by name, which no
	// print symbol may be.
	Refused     []string `json:"refused"`
	Comparators []string `json:"comparators"`
	// MaxDecimalDigits bounds a quantity value's digits, as CanonicalDecimal
	// holds them.
	MaxDecimalDigits int `json:"maxDecimalDigits"`
}

func renderConsoleTable(t *testing.T) []byte {
	t.Helper()
	rules := vocabulary.UnitPrintRules()
	tbl := consoleTable{Symbols: map[string]string{}, SymbolCodes: map[string]string{}, Primary: map[string]string{},
		Aliases: vocabulary.UnitAliases(), Comparators: vocabulary.QuantityComparators, MaxDecimalDigits: vocabulary.MaxDecimalDigits,
		Print: consolePrint{rules.Prefixes, rules.Atoms, rules.Prints, rules.Wholes, rules.Annotations}}
	seen := map[string]int{}
	for _, m := range vocabulary.Measures() {
		tbl.Measures = append(tbl.Measures, consoleMeasure{
			Name: m.Name, Base: m.Base, Scale: string(m.Scale), Display: m.Display,
			Dimension: m.Dimension(), Units: m.Units, Converts: convertClasses(t, m), Factors: baseFactors(m), Note: m.Note,
		})
		for _, code := range m.Units {
			tbl.Symbols[code] = vocabulary.UnitSymbol(code)
			seen[code]++
		}
	}
	aliases, refused := vocabulary.UnitAliases(), vocabulary.RefusedUnitSpellings()
	for spelling := range refused {
		tbl.Refused = append(tbl.Refused, spelling)
	}
	sort.Strings(tbl.Refused)
	shared := map[string]bool{}
	for code, symbol := range tbl.Symbols {
		_, alias := aliases[symbol]
		_, spent := refused[symbol]
		if _, err := vocabulary.ParseUnit(symbol); symbol == "" || symbol == code || alias || spent || err == nil {
			continue
		}
		if _, dup := tbl.SymbolCodes[symbol]; dup {
			shared[symbol] = true
		}
		tbl.SymbolCodes[symbol] = code
	}
	for symbol := range shared {
		delete(tbl.SymbolCodes, symbol)
	}
	for code, n := range seen {
		if n > 1 {
			m, err := vocabulary.MeasureOf(code)
			if err != nil {
				t.Fatal(err)
			}
			tbl.Primary[code] = m.Name
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(tbl); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// convertClasses partitions a measure's codes by whether ConvertIn converts
// a value between them, in the table's order. It fails where the relation
// is not an equivalence, since a class could not then say it.
func convertClasses(t *testing.T, m vocabulary.Measure) [][]string {
	t.Helper()
	one := big.NewRat(1, 1)
	converts := func(a, b string) bool {
		_, err := vocabulary.ConvertIn(m.Name, one, a, b)
		return err == nil
	}
	var classes [][]string
	for _, code := range m.Units {
		placed := false
		for i, class := range classes {
			if converts(class[0], code) {
				classes[i] = append(class, code)
				placed = true
				break
			}
		}
		if !placed {
			classes = append(classes, []string{code})
		}
	}
	for _, class := range classes {
		for _, a := range class {
			for _, b := range m.Units {
				if converts(a, b) != slices.Contains(class, b) {
					t.Fatalf("%s: converting %s to %s is %v, which no partition into classes says", m.Name, a, b, converts(a, b))
				}
			}
		}
	}
	return classes
}

func TestConsoleUnitsMatchTheEngine(t *testing.T) {
	for path, want := range map[string][]byte{
		consoleUnitTable:  renderConsoleTable(t),
		consolePrintCases: renderPrintCases(t),
	} {
		if *updateGenerated {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read the console's copy: %v (rewrite it with -update)", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s differs from the engine's unit table: rewrite it with -update, never by hand", path)
		}
	}
}

// printCaseCompounds are codes no table row lists, which the console renders
// with the engine's rules: units.test.ts holds its symbol for each of them,
// and for every listed code and alias target, to the engine's.
var printCaseCompounds = []string{
	"{count}/min", "{bpm}", "{count}/h",
	"uL", "fL", "dm3", "cm3", "m+2", "m02", "s-1", "/ms", "GJ", "Mg", "mK", "MHz", "TBy", "kN.m", "mrad",
	"kg/h", "ug/min", "g/(kg)", "umol/mol", "mmol/(kg.h)", "kcal/(kg.h)", "[lb_av]/[ft_i]3", "mSv/h",
	"u[IU]/mL", "k[IU]", "10*3/mL", "10^3/mL", "10*3{copies}", "10*3{copies}/mL", "kg{body_wt}", "mL{total}",
	"{beats}/min", "{steps}", "{steps}/h", "kV.A", "kV.A{reactive}", "W{reactive}", "MW.h", "kA.h", "mGy",
	"kBq", "cm[Hg]", "pt", "ft", "/(s.m2)", "(10*3{copies})",
}

// renderPrintCases is the engine's print symbol of every case the console
// is held to.
func renderPrintCases(t *testing.T) []byte {
	t.Helper()
	cases := map[string]string{}
	for _, code := range append(append(vocabulary.UnitCodes(), printCaseCompounds...), aliasTargets()...) {
		cases[code] = vocabulary.UnitSymbol(code)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cases); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func aliasTargets() []string {
	var out []string
	for _, code := range vocabulary.UnitAliases() {
		out = append(out, code)
	}
	return out
}

func TestUnitsPage(t *testing.T) {
	want := renderUnitsPage(t)
	if *updateGenerated {
		if err := os.WriteFile(unitsPage, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(unitsPage)
	if err != nil {
		t.Fatalf("read %s: %v (rewrite it with -update)", unitsPage, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the engine's unit table: rewrite it with -update, never by hand", unitsPage)
	}
}

func renderUnitsPage(t *testing.T) []byte {
	t.Helper()
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	ms := vocabulary.Measures()
	rows := 0
	for _, m := range ms {
		rows += len(m.Units)
	}
	w("# Units\n\n")
	w("<!-- Written by TestUnitsPage from internal/vocabulary/unitstable.go; rewrite with\n")
	w("     go test ./internal/vocabulary/ -run TestUnitsPage -update, never by hand. -->\n\n")
	w("Every unit a `quantity` value may carry, grouped by **measure**: the named kind of\n")
	w("quantity (mass, temperature, rate) inside which a value converts and compares.\n")
	w("[The data model](data-model.md#property-types) says what a quantity value is and how a\n")
	w("property declares one. This page is the table itself: %d measures and %d codes\n", len(ms), len(vocabulary.UnitCodes()))
	w("(%d rows, because a few codes sit in two measures).\n\n", rows)
	w("A code is a case-sensitive UCUM code. The codes listed here are the ones the table\n")
	w("names; the grammar also reads any compound or prefixed code over the same atoms\n")
	w("(`kg/h`, `GJ`, `fL`), whose measure is found from its prefixes and its\n")
	w("dimension. The table only grows: a code, a measure or a factor is never removed\n")
	w("or changed, because a stored value is read through it.\n\n")
	w("Each factor is exact, a rational number: a value converts to the measure's base as\n")
	w("`value x factor + offset`. \"none\" marks a code no rational factor reaches: a\n")
	w("level, an arbitrary unit, and `rad`, which is 180/pi degrees. Such a code converts\n")
	w("to nothing but itself.\n\n")
	w("`GET /api/v1/units` serves this table with each code's exact factor, and\n")
	w("`GET /api/v1/units/convert` converts one value ([the API](api.md#the-unit-table)).\n\n")
	w("Unit codes are from UCUM 2.2, copyright Regenstrief Institute, Inc., used under the\n")
	w("UCUM license. `dBm` is the substrate's own code; UCUM has no spelling for it.\n\n")

	w("## Measures\n\n")
	w("The dimension is over the nine base quantities: length (L), mass (M), time (T),\n")
	w("electric current (I), temperature (Θ), amount of substance (N), luminous\n")
	w("intensity (J), plane angle (A) and information (B). Two measures may share a\n")
	w("dimension (energy and torque); that is why a measure has a name.\n\n")
	w("| Measure | Scale | Base | Dimension | Shown in | Codes |\n")
	w("| --- | --- | --- | --- | --- | --- |\n")
	for _, m := range ms {
		base, dim := code(m.Base), m.Dimension()
		if base == "" {
			base = "none"
		}
		if dim == "" {
			dim = "none"
		}
		codes := make([]string, len(m.Units))
		for i, c := range m.Units {
			codes[i] = code(c)
		}
		w("| [%s](#%s) | %s | %s | %s | %s | %s |\n", m.Name, m.Name, m.Scale, base, dim, code(m.Display), strings.Join(codes, ", "))
	}
	w("\nThe scales: `ratio` converts by a factor; `affine` by a factor and an offset (an\n")
	w("absolute temperature); `difference` by the factor alone (a temperature\n")
	w("difference: 0.3 `Cel` is 0.54 `[degF]`); `logarithmic` converts to nothing but\n")
	w("itself; `arbitrary` converts only to the same assay unit under another metric\n")
	w("prefix (`m[IU]/L` to `[IU]/L`, a substrate rule UCUM does not have);\n")
	w("`count` converts only between units counting the same annotation.\n\n")

	w("## Codes by measure\n\n")
	w("The symbol is what a title prints. A code whose symbol would be an alias of\n")
	w("another code, or a spelling the table refuses, prints as the code itself:\n")
	w("`{count}` prints `{count}`, because `count` is refused, and `{count}/min` would\n")
	w("print `count/min`, the alias of `/min`.\n\n")
	for _, m := range ms {
		w("### %s\n\n", m.Name)
		if m.Note != "" {
			w("%s.\n\n", sentence(m.Note))
		}
		base := m.Base
		if base == "" {
			base = "base"
		}
		w("| Code | Symbol | Factor to %s | Offset |\n", code(base))
		w("| --- | --- | --- | --- |\n")
		for _, c := range m.Units {
			factor, offset := "none", ""
			if f, o, err := vocabulary.ToBase(m.Name, c); err == nil {
				factor = f.RatString()
				if o.Sign() != 0 {
					offset = o.RatString()
				}
			}
			w("| %s | %s | %s | %s |\n", code(c), vocabulary.UnitSymbol(c), factor, offset)
		}
		w("\n")
	}

	w("## Aliases\n\n")
	w("A writer may send these spellings; the stored value always holds the code. Both\n")
	w("micro signs (U+00B5 and U+03BC) read as `u` and the superscript digits ² and ³\n")
	w("as `2` and `3` anywhere in a code, so `μg/m³` is stored as `ug/m3`. After a bare\n")
	w("`10` a superscript is its power: `10³/μL` is stored as `10*3/uL`, never `103/uL`.\n\n")
	w("An alias adds no annotation to a count or a rate. `bpm`, `rpm` and `count/min`\n")
	w("are `/min`, the bare rate a heart rate, a cadence or a breathing rate is stored\n")
	w("in, so a writer's common spelling converts to it. An annotation is part of a\n")
	w("unit: a writer that knows what was counted writes it (`{beats}/min`), and that\n")
	w("code converts only to the same annotation, never to `/min` or `{steps}/min`.\n")
	w("`var` is the one alias that carries an annotation, because `{reactive}` is the\n")
	w("reactive-power measure's identity rather than a thing counted.\n\n")
	w("| Alias | Code |\n")
	w("| --- | --- |\n")
	aliases := vocabulary.UnitAliases()
	keys := make([]string, 0, len(aliases))
	for k := range aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w("| %s | %s |\n", code(k), code(aliases[k]))
	}
	w("\nThese spellings are refused by name, because no code states what they mean:\n\n")
	w("| Spelling | Why |\n")
	w("| --- | --- |\n")
	refused := vocabulary.RefusedUnitSpellings()
	keys = keys[:0]
	for k := range refused {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w("| %s | %s |\n", code(k), sentence(refused[k]))
	}
	return []byte(b.String())
}

// code backticks a code for a Markdown table, escaping the one character a
// table cell cannot hold.
func code(s string) string {
	if s == "" {
		return ""
	}
	return "`" + strings.ReplaceAll(s, "|", `\|`) + "`"
}

// sentence capitalizes a note for the page, so a note must not open with a
// code: codes are case-sensitive, and `cal` capitalized is `Cal`, an alias of
// `[Cal]`, a thousand times larger.
func sentence(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func TestUnitNotesOpenWithAWord(t *testing.T) {
	known := map[string]bool{}
	for _, c := range vocabulary.UnitCodes() {
		known[c] = true
	}
	for alias := range vocabulary.UnitAliases() {
		known[alias] = true
	}
	for _, m := range vocabulary.Measures() {
		word, _, _ := strings.Cut(m.Note, " ")
		word = strings.TrimRight(word, ":;,")
		// The article is the one English word that is also a code (the year).
		if word != "a" && known[word] {
			t.Errorf("%s: the note opens with the code %q, which the units page would capitalize: %q", m.Name, word, m.Note)
		}
	}
}
