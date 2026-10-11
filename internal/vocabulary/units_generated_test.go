package vocabulary_test

// THE TWO COPIES OF THE UNIT TABLE, written from it.
//
// The console lists measures and units in its quantity picker, renders print
// symbols and reads the aliases a person types (web/console/src/lib/units.ts,
// which imports units.table.json). docs/units.md lists every measure and code
// for a reader. Both are written from vocabulary.Measures() by these tests, so
// neither can drift: a table change fails them until
//
//	go test ./internal/vocabulary/ -run 'TestConsoleUnitsMatchTheEngine|TestUnitsPage' -update
//
// rewrites both. Do not edit either by hand.

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

var updateGenerated = flag.Bool("update", false, "rewrite the console's unit table and docs/units.md from the engine's")

const (
	consoleUnitTable = "../../web/console/src/lib/units.table.json"
	unitsPage        = "../../docs/units.md"
)

type consoleMeasure struct {
	Name      string   `json:"name"`
	Base      string   `json:"base"`
	Scale     string   `json:"scale"`
	Display   string   `json:"display"`
	Dimension string   `json:"dimension"`
	Units     []string `json:"units"`
	Note      string   `json:"note,omitempty"`
}

type consoleTable struct {
	Measures []consoleMeasure `json:"measures"`
	// Symbols is every listed code's print symbol.
	Symbols map[string]string `json:"symbols"`
	// Primary is where a code two measures list reads undeclared.
	Primary map[string]string `json:"primary"`
	// Aliases are the input spellings and the codes they map to.
	Aliases     map[string]string `json:"aliases"`
	Comparators []string          `json:"comparators"`
}

func renderConsoleTable(t *testing.T) []byte {
	t.Helper()
	tbl := consoleTable{Symbols: map[string]string{}, Primary: map[string]string{}, Aliases: vocabulary.UnitAliases(),
		Comparators: vocabulary.QuantityComparators}
	seen := map[string]int{}
	for _, m := range vocabulary.Measures() {
		tbl.Measures = append(tbl.Measures, consoleMeasure{
			Name: m.Name, Base: m.Base, Scale: string(m.Scale), Display: m.Display,
			Dimension: m.Dimension(), Units: m.Units, Note: m.Note,
		})
		for _, code := range m.Units {
			tbl.Symbols[code] = vocabulary.UnitSymbol(code)
			seen[code]++
		}
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

func TestConsoleUnitsMatchTheEngine(t *testing.T) {
	want := renderConsoleTable(t)
	if *updateGenerated {
		if err := os.WriteFile(consoleUnitTable, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(consoleUnitTable)
	if err != nil {
		t.Fatalf("read the console's copy: %v (rewrite it with -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the engine's unit table: rewrite it with -update, never by hand", consoleUnitTable)
	}
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
	w("[data-model.md](data-model.md#quantity) says what a quantity value is and how a\n")
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
	w("prefix (`m[IU]/L` to `[IU]/L`, a substrate extension that UCUM does not make);\n")
	w("`count` converts only between units counting the same annotation.\n\n")

	w("## Codes by measure\n\n")
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
	w("as `2` and `3` anywhere in a code, so `μg/m³` is stored as `ug/m3`.\n\n")
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

func sentence(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
