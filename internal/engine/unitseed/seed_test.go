package unitseed_test

// THE SHIPPED UNIT SEED (decision 0186). units.yaml and conversions.yaml are
// the source of the units and conversions every repository is seeded with,
// edited by hand. These tests hold them to what the engine's old unit table
// promised: every spelling a writer sent before still names the same unit,
// units the table kept apart stay apart, and every unit converts to its
// component's root at the table's exact factor and offset
// (testdata/table_oracle.json, captured from the table).

import (
	"encoding/json"
	"math/big"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/unitseed"
	"github.com/geoah/substrate/internal/vocabulary"
)

func shippedGraph(t *testing.T) *vocabulary.UnitGraph {
	t.Helper()
	g, problems, err := unitseed.Graph()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("the shipped seed: %v", p)
	}
	return g
}

// oldSpellings are spellings writers sent before decision 0186, frozen from
// the engine's old table (its codes that are no record id, and its input
// aliases), each with the id of the unit it named. Every one still resolves
// to that unit.
var oldSpellings = map[string]string{
	"%": "percent", "'": "arcmin", "''": "arcsec",
	"/L": "1/L", "/d": "1/d", "/h": "1/h", "/min": "1/min", "/s": "1/s", "/uL": "1/uL", "/wk": "1/wk",
	"10*12/L": "1e12/L", "10*3/uL": "1e3/uL", "10*6/uL": "1e6/uL", "10*9/L": "1e9/L",
	"L/(100.km)": "L/100km", "V.A{reactive}": "var",
	"[Btu_IT]": "Btu", "[Btu_IT]/h": "Btu/h", "[Cal]": "Cal", "[HP]": "hp", "[IU]": "IU", "[IU]/L": "IU/L",
	"[IU]/mL": "IU/mL", "[MET]": "MET", "[arb'U]": "arbU", "[cft_i]": "ft3",
	"[degF]": "degF", "[foz_br]": "floz_br", "[foz_us]": "floz", "[ft_i]": "ft", "[ft_i]/s": "ft/s",
	"[g]": "gn", "[gal_br]": "gal_br", "[gal_us]": "gal", "[gal_us]/min": "gal/min", "[in_i'Hg]": "inHg",
	"[in_i]": "in", "[in_i]/h": "in/h", "[kn_i]": "kn", "[lb_av]": "lb", "[lbf_av]": "lbf",
	"[lbf_av].[ft_i]": "lbf.ft", "[mi_i]": "mi", "[mi_i]/[gal_us]": "mpg", "[mi_i]/h": "mph",
	"[nmi_i]": "nmi", "[oz_av]": "oz", "[pH]": "pH", "[ppb]": "ppb", "[cup_us]": "cup", "[cup_m]": "cup_m",
	"[ppm]": "ppm", "[ppth]": "ppth", "[psi]": "psi", "[pt_us]": "pt", "[qt_us]": "qt", "[sft_i]": "ft2",
	"[stone_av]": "st", "[tbs_m]": "tbsp_m", "[tbs_us]": "tbsp", "[tsp_m]": "tsp_m", "[tsp_us]": "tsp",
	"[yd_i]": "yd", "cm[H2O]": "cmH2O", "dB[SPL]": "dBSPL", "dB[W]": "dBW",
	"kW.h/(100.km)": "kW.h/100km", "m[IU]/L": "mIU/L", "min/[mi_i]": "min/mi",
	"mL/(kg.min)": "mL/kg/min", "mm[Hg]": "mmHg", "s/(100.[yd_i])": "s/100yd", "s/(100.m)": "s/100m",
	"s/(500.m)": "s/500m", "{count}": "count", "{score}": "score", "{steps}": "steps", "{floors}": "floors",

	"°C": "Cel", "℃": "Cel", "degC": "Cel", "°F": "degF", "℉": "degF", "degF": "degF",
	"mcg": "ug", "mmHg": "mmHg", "inHg": "inHg", "bpm": "1/min", "rpm": "1/min", "count/min": "1/min",
	"kWh": "kW.h", "Wh": "W.h", "Cal": "Cal", "ppm": "ppm", "ppb": "ppb", "VA": "V.A", "var": "var",
	"hr": "h", "IU": "IU", "dBASPL": "dBSPL", "cmAq": "cmH2O", "in": "in", "yd": "yd", "mi": "mi",
	"km/hr": "km/h", "mi/hr": "mph", "lb": "lb", "oz": "oz", "mL/(kg*min)": "mL/kg/min",
	"mmol<180.1558800000541>/L": "mmol/L", "mmol<180.15588000005408>/L": "mmol/L",
	"ml/(kg·min)": "mL/kg/min", "kmph": "km/h",

	"kg": "kg", "Cel": "Cel", "μg/m³": "ug/m3", "µg": "ug", "count": "count",
}

func TestShippedSeedBuildsWithoutAProblem(t *testing.T) {
	g := shippedGraph(t)
	if n := len(g.Units()); n != 216 {
		t.Errorf("the seed ships %d units; docs/plans/dynamic-units.md says 216", n)
	}
	if n := len(g.Conversions()); n != 149 {
		t.Errorf("the seed ships %d conversions; docs/plans/dynamic-units.md says 149", n)
	}
}

func TestShippedUnitIDsAreRecordIDs(t *testing.T) {
	g := shippedGraph(t)
	for _, u := range g.Units() {
		if !vocabulary.ValidID(u.ID) {
			t.Errorf("unit id %q is not a record id", u.ID)
		}
		if u.Name == "" {
			t.Errorf("unit %s has no name", u.ID)
		}
	}
	for _, c := range g.Conversions() {
		if !vocabulary.ValidID(c.ID) {
			t.Errorf("conversion id %q is not a record id", c.ID)
		}
		if c.ID != c.From+"-"+c.To {
			t.Errorf("conversion %s joins %s to %s: a shipped conversion's id is <from>-<to>", c.ID, c.From, c.To)
		}
		if c.Factor == nil {
			t.Errorf("conversion %s is no linear conversion; the seed ships linear ones only", c.ID)
		}
	}
}

// TestShippedSpellingsResolve holds every spelling a writer sent before the
// graph to the unit it named then.
func TestShippedSpellingsResolve(t *testing.T) {
	g := shippedGraph(t)
	for spelling, want := range oldSpellings {
		if got, ok := g.Resolve(spelling); !ok || got != want {
			t.Errorf("%s resolves to %q, %v; want %s", spelling, got, ok, want)
		}
	}
}

// Decision 0163: money is never a unit, so no shipped unit answers a currency
// code and a quantity in one is refused.
func TestShippedSeedHoldsNoCurrency(t *testing.T) {
	g := shippedGraph(t)
	for _, code := range []string{"EUR", "USD", "GBP", "JPY", "CHF", "XAU", "EUR/kg", "[EUR]"} {
		if id, ok := g.Resolve(code); ok {
			t.Errorf("%s resolves to the unit %s", code, id)
		}
	}
}

func TestShippedUnitsKeepMeasuresApart(t *testing.T) {
	g := shippedGraph(t)
	for _, pair := range [][2]string{
		{"J", "N.m"},
		{"Gy", "Sv"},
		{"Gy", "J/kg"},
		{"Hz", "Bq"},
		{"Hz", "1/s"},
		{"count", "percent"},
		{"count", "1"},
		{"mm/h", "km/h"},
		{"V.A", "W"},
		{"var", "V.A"},
		{"rad", "deg"},
		{"dBm", "dBW"},
		{"IU", "IU/L"},
		{"score", "percent"},
		{"mmol/mol", "percent"},
		{"L/100km", "mpg"},
	} {
		a, b := pair[0], pair[1]
		if _, ok := g.Unit(a); !ok {
			continue // a compound the seed does not ship converts to nothing
		}
		if _, _, err := g.Linear(a, b, false); err == nil {
			t.Errorf("%s and %s convert; the seed keeps them apart", a, b)
		}
	}
}

// TestShippedCircularUnits holds the angle units to one turn: deg, arcmin and
// arcsec share a component whose turns agree, and rad's is alone.
func TestShippedCircularUnits(t *testing.T) {
	g := shippedGraph(t)
	for id, want := range map[string]string{"deg": "360", "arcmin": "21600", "arcsec": "1296000"} {
		if got := g.Turn(id); got == nil || vocabulary.FormatRational(got) != want {
			t.Errorf("turn of %s is %v, want %s", id, got, want)
		}
	}
	if got := g.Turn("rad"); got == nil || !strings.HasPrefix(vocabulary.FormatRational(got), "6.28318530717958647692") {
		t.Errorf("turn of rad is %v", got)
	}
	if got := g.Turn("kg"); got != nil {
		t.Errorf("kg has a turn, %v", got)
	}
}

func TestShippedScales(t *testing.T) {
	g := shippedGraph(t)
	for id, want := range map[string]vocabulary.UnitScale{
		"dB": vocabulary.UnitScaleLevel, "pH": vocabulary.UnitScaleLevel,
		"dBSPL": vocabulary.UnitScalePowerLevel, "dBW": vocabulary.UnitScalePowerLevel, "dBm": vocabulary.UnitScalePowerLevel,
		"kg": "", "percent": "",
	} {
		if got := g.Scale(id); got != want {
			t.Errorf("scale of %s is %q, want %q", id, got, want)
		}
	}
}

// A temperature converts with its offset, and as a difference without it.
func TestShippedTemperatures(t *testing.T) {
	g := shippedGraph(t)
	for _, c := range []struct {
		from, to string
		delta    bool
		factor   string
		offset   string
	}{
		{"Cel", "degF", false, "1.8", "32"},
		{"Cel", "degF", true, "1.8", "0"},
		{"Cel", "K", false, "1", "273.15"},
	} {
		f, o, err := g.Linear(c.from, c.to, c.delta)
		if err != nil {
			t.Fatalf("%s to %s: %v", c.from, c.to, err)
		}
		if vocabulary.FormatRational(f) != c.factor || vocabulary.FormatRational(o) != c.offset {
			t.Errorf("%s to %s (delta %v): x * %s + %s, want x * %s + %s", c.from, c.to, c.delta,
				vocabulary.FormatRational(f), vocabulary.FormatRational(o), c.factor, c.offset)
		}
	}
}

// tableOracle is testdata/table_oracle.json: the engine's old unit table's
// answer for every shipped unit, captured from the table itself at the last
// commit that held it (testdata/table_oracle_capture.go.txt says which commit
// and how). It is frozen: nothing in this tree can answer as the table did.
type tableOracle struct {
	Source struct {
		Commit    string `json:"commit"`
		Procedure string `json:"procedure"`
	} `json:"source"`
	// Units maps each unit id to the root of the component the table's
	// conversions joined it into (the smallest id in it), its exact map to
	// that root (root = value * factor + offset) and a difference's factor.
	Units map[string]struct {
		Root   string `json:"root"`
		Factor string `json:"factor"`
		Offset string `json:"offset"`
		Delta  string `json:"delta"`
	} `json:"units"`
	// Spellings maps every spelling the table read (its codes, its input
	// aliases, the annotated counts) and every spelling a seed record
	// declared to the unit id it names.
	Spellings map[string]string `json:"spellings"`
}

func readTableOracle(t *testing.T) tableOracle {
	t.Helper()
	raw, err := os.ReadFile("testdata/table_oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	var o tableOracle
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	if o.Source.Commit == "" || len(o.Units) == 0 || len(o.Spellings) == 0 {
		t.Fatal("testdata/table_oracle.json names no source commit, unit or spelling")
	}
	return o
}

func oracleRat(t *testing.T, s string) *big.Rat {
	t.Helper()
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		t.Fatalf("testdata/table_oracle.json: %q is no rational", s)
	}
	return r
}

// TestShippedSeedMatchesTheTableOracle holds every shipped unit to the old
// table's numbers: the same units, each in the component the table's
// conversions made (so no two units the table kept apart convert, and none
// it joined are apart), at the exact factor and offset to that component's
// root, absolute and as a difference. A factor that is consistent but wrong
// passes every structural test; this one refuses it.
func TestShippedSeedMatchesTheTableOracle(t *testing.T) {
	g := shippedGraph(t)
	o := readTableOracle(t)

	var got, want []string
	for _, u := range g.Units() {
		got = append(got, u.ID)
	}
	for id := range o.Units {
		want = append(want, id)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("the seed ships the units %v; the table answered for %v", got, want)
	}

	members := map[string][]string{}
	for id, u := range o.Units {
		members[u.Root] = append(members[u.Root], id)
	}
	for id, u := range o.Units {
		if root, _ := g.Root(id); root != u.Root {
			t.Errorf("%s: the root is %s; the table's component's is %s", id, root, u.Root)
		}
		component := g.Component(id)
		sort.Strings(component)
		wantMembers := slices.Clone(members[u.Root])
		sort.Strings(wantMembers)
		if !slices.Equal(component, wantMembers) {
			t.Errorf("%s: the component is %v; the table's is %v", id, component, wantMembers)
		}
		f, off, err := g.Linear(id, u.Root, false)
		if err != nil {
			t.Errorf("%s to %s: %v", id, u.Root, err)
			continue
		}
		if f.Cmp(oracleRat(t, u.Factor)) != 0 || off.Cmp(oracleRat(t, u.Offset)) != 0 {
			t.Errorf("%s to %s: x * %s + %s; the table says x * %s + %s",
				id, u.Root, f.RatString(), off.RatString(), u.Factor, u.Offset)
		}
		f, off, err = g.Linear(id, u.Root, true)
		if err != nil {
			t.Errorf("%s to %s as a difference: %v", id, u.Root, err)
			continue
		}
		if f.Cmp(oracleRat(t, u.Delta)) != 0 || off.Sign() != 0 {
			t.Errorf("%s to %s as a difference: x * %s + %s; the table says x * %s",
				id, u.Root, f.RatString(), off.RatString(), u.Delta)
		}
	}
}

// TestShippedSpellingsMatchTheTableOracle holds the seed's spellings to the
// captured map both ways: every spelling the table read or the seed declared
// still names its unit, and the seed declares no spelling the map lacks, so
// a new or moved alias is a change to this file's fixture, made on purpose.
func TestShippedSpellingsMatchTheTableOracle(t *testing.T) {
	g := shippedGraph(t)
	o := readTableOracle(t)
	for spelling, want := range o.Spellings {
		if got, ok := g.Resolve(spelling); !ok || got != want {
			t.Errorf("%q resolves to %q, %v; the table named %s", spelling, got, ok, want)
		}
	}
	for _, u := range g.Units() {
		declared := append([]string{u.ID, u.Code}, u.Aliases...)
		for _, s := range declared {
			if s == "" {
				continue
			}
			if want, ok := o.Spellings[s]; !ok || want != u.ID {
				t.Errorf("%s declares the spelling %q; the captured map says %q, %v", u.ID, s, want, ok)
			}
		}
	}
}
