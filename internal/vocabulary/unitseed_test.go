package vocabulary_test

// THE SHIPPED UNIT SEED, written from the unit table (decision 0186).
//
// internal/engine/unitseed/units.yaml and conversions.yaml are the unit and
// conversion records every repository is seeded with. They are the table in
// unitstable.go as records, so a repository starts with the units and the
// answers it has today. TestUnitSeedMatchesTheTable writes them under
//
//	go test ./internal/vocabulary/ -run TestUnitSeedMatchesTheTable -update
//
// and the parity tests below hold the graph built from them to the table,
// until the table is deleted. Do not edit either file by hand.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/big"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	seedUnitsFile       = "../engine/unitseed/units.yaml"
	seedConversionsFile = "../engine/unitseed/conversions.yaml"
)

// seedUnitIDs is the id of every listed code that is not a record id itself
// (the map in docs/plans/dynamic-units.md). Every other code is its own id.
var seedUnitIDs = map[string]string{
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
	"s/(500.m)": "s/500m", "{count}": "count", "{score}": "score",
	// The annotated counts the shipped samples write, which the table lists
	// nowhere: the grammar read them as counts.
	"{steps}": "steps", "{floors}": "floors",
}

// seedAnnotations are the units the seed adds beside the listed codes.
var seedAnnotations = []string{"{steps}", "{floors}"}

// seedUnitNames names every unit the seed ships, by code.
var seedUnitNames = map[string]string{
	"kg": "kilogram", "g": "gram", "mg": "milligram", "ug": "microgram", "ng": "nanogram", "t": "tonne",
	"[lb_av]": "pound", "[oz_av]": "ounce", "[stone_av]": "stone",
	"m": "meter", "km": "kilometer", "cm": "centimeter", "mm": "millimeter", "nm": "nanometer",
	"[in_i]": "inch", "[ft_i]": "foot", "[yd_i]": "yard", "[mi_i]": "mile", "[nmi_i]": "nautical mile",
	"m2": "square meter", "cm2": "square centimeter", "har": "hectare", "[sft_i]": "square foot",
	"m3": "cubic meter", "L": "liter", "dL": "deciliter", "mL": "milliliter",
	"[gal_us]": "US gallon", "[qt_us]": "US quart", "[pt_us]": "US pint", "[cup_us]": "US cup",
	"[foz_us]": "US fluid ounce", "[tbs_us]": "US tablespoon", "[tsp_us]": "US teaspoon",
	"[gal_br]": "imperial gallon", "[foz_br]": "imperial fluid ounce", "[cup_m]": "metric cup",
	"[tbs_m]": "metric tablespoon", "[tsp_m]": "metric teaspoon", "[cft_i]": "cubic foot",
	"s": "second", "ms": "millisecond", "us": "microsecond", "min": "minute", "h": "hour", "d": "day",
	"wk": "week", "mo": "mean Julian month", "a": "mean Julian year",
	"K": "kelvin", "Cel": "degree Celsius", "[degF]": "degree Fahrenheit",
	"J": "joule", "kJ": "kilojoule", "MJ": "megajoule", "cal": "thermochemical calorie", "kcal": "kilocalorie",
	"[Cal]": "nutrition Calorie", "W.h": "watt-hour", "kW.h": "kilowatt-hour", "eV": "electronvolt",
	"[Btu_IT]": "British thermal unit",
	"W": "watt", "mW": "milliwatt", "kW": "kilowatt", "MW": "megawatt", "[HP]": "horsepower",
	"kcal/d": "kilocalorie per day", "kcal/h": "kilocalorie per hour", "[Btu_IT]/h": "British thermal unit per hour",
	"V.A": "volt-ampere", "V.A{reactive}": "volt-ampere reactive",
	"Pa": "pascal", "hPa": "hectopascal", "kPa": "kilopascal", "bar": "bar", "mbar": "millibar",
	"atm": "standard atmosphere", "mm[Hg]": "millimeter of mercury", "cm[H2O]": "centimeter of water",
	"[psi]": "pound per square inch", "[in_i'Hg]": "inch of mercury",
	"m/s": "meter per second", "km/h": "kilometer per hour", "[mi_i]/h": "mile per hour", "[kn_i]": "knot",
	"[ft_i]/s": "foot per second",
	"s/m": "second per meter", "min/km": "minute per kilometer", "s/km": "second per kilometer",
	"min/[mi_i]": "minute per mile", "s/(100.m)": "second per 100 meters", "s/(100.[yd_i])": "second per 100 yards",
	"s/(500.m)": "second per 500 meters",
	"m/s2": "meter per second squared", "[g]": "standard gravity",
	"Hz": "hertz", "kHz": "kilohertz",
	"/s": "per second", "/min": "per minute", "/h": "per hour", "/d": "per day", "/wk": "per week",
	"{count}": "count", "1": "one",
	"%": "percent", "[ppth]": "part per thousand", "[ppm]": "part per million", "[ppb]": "part per billion",
	"mmol/mol": "millimole per mole", "{score}": "score",
	"mol": "mole", "mmol": "millimole", "umol": "micromole",
	"mol/L": "mole per liter", "mmol/L": "millimole per liter", "umol/L": "micromole per liter",
	"nmol/L": "nanomole per liter", "pmol/L": "picomole per liter",
	"g/L": "gram per liter", "g/dL": "gram per deciliter", "mg/dL": "milligram per deciliter",
	"mg/L": "milligram per liter", "ug/L": "microgram per liter", "ng/mL": "nanogram per milliliter",
	"pg/mL": "picogram per milliliter", "ug/dL": "microgram per deciliter", "g/m3": "gram per cubic meter",
	"mg/m3": "milligram per cubic meter", "ug/m3": "microgram per cubic meter", "kg/m3": "kilogram per cubic meter",
	"/L": "per liter", "/uL": "per microliter", "10*3/uL": "thousand per microliter",
	"10*6/uL": "million per microliter", "10*9/L": "billion per liter", "10*12/L": "trillion per liter",
	"U/L": "enzyme unit per liter", "kat": "katal", "U": "enzyme unit",
	"[IU]": "international unit", "[IU]/L": "international unit per liter",
	"m[IU]/L": "milli-international unit per liter", "[IU]/mL": "international unit per milliliter",
	"[arb'U]": "arbitrary unit",
	"lx": "lux", "lm": "lumen", "cd": "candela", "cd/m2": "candela per square meter",
	"W/m2": "watt per square meter",
	"dB": "decibel", "dB[SPL]": "decibel of sound pressure level", "dB[W]": "decibel-watt",
	"dBm": "decibel-milliwatt", "[pH]": "pH",
	"V": "volt", "mV": "millivolt", "A": "ampere", "mA": "milliampere", "Ohm": "ohm", "kOhm": "kiloohm",
	"C": "coulomb", "A.h": "ampere-hour", "mA.h": "milliampere-hour", "uS/cm": "microsiemens per centimeter",
	"bit": "bit", "By": "byte", "kBy": "kilobyte", "MBy": "megabyte", "GBy": "gigabyte",
	"KiBy": "kibibyte", "MiBy": "mebibyte", "GiBy": "gibibyte",
	"bit/s": "bit per second", "kbit/s": "kilobit per second", "Mbit/s": "megabit per second",
	"By/s": "byte per second",
	"deg": "degree", "'": "minute of arc", "''": "second of arc", "rad": "radian",
	"L/min": "liter per minute", "L/h": "liter per hour", "mL/min": "milliliter per minute",
	"m3/h": "cubic meter per hour", "[gal_us]/min": "US gallon per minute",
	"g/s": "gram per second", "N": "newton", "[lbf_av]": "pound-force",
	"N.m": "newton meter", "[lbf_av].[ft_i]": "pound-force foot",
	"Bq/m3": "becquerel per cubic meter", "pCi/L": "picocurie per liter",
	"Gy": "gray", "mGy": "milligray", "Sv": "sievert", "mSv": "millisievert", "uSv": "microsievert",
	"uSv/h": "microsievert per hour", "Bq": "becquerel", "kBq": "kilobecquerel", "MBq": "megabecquerel",
	"kg/m2": "kilogram per square meter", "mL/(kg.min)": "milliliter per kilogram per minute",
	"[MET]": "metabolic equivalent", "W/kg": "watt per kilogram", "kcal/kg": "kilocalorie per kilogram",
	"kW.h/(100.km)": "kilowatt-hour per 100 kilometers", "W.h/km": "watt-hour per kilometer",
	"L/(100.km)": "liter per 100 kilometers", "[mi_i]/[gal_us]": "mile per US gallon",
	"km/L": "kilometer per liter", "mm/h": "millimeter per hour", "[in_i]/h": "inch per hour",
	"{steps}": "steps", "{floors}": "floors",
}

// seedTurns are the angle units' full circles. A radian's is 2 pi to 40
// places, the precision an order key reads pi at; rad keeps no rational
// conversion to deg, so its turn is alone in its component.
var seedTurns = map[string]string{
	"deg": "360", "'": "21600", "''": "1296000",
	"rad": "6.2831853071795864769252867665590057683943",
}

// seedScales are the scales of the logarithmic measures' units: the level of
// a power where a mean is not the mean of its decibels.
var seedScales = map[string]vocabulary.UnitScale{
	"level": vocabulary.UnitScaleLevel, "acidity": vocabulary.UnitScaleLevel,
	"sound-pressure-level": vocabulary.UnitScalePowerLevel, "power-level": vocabulary.UnitScalePowerLevel,
}

var reRecordID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~:@/-]*$`)

// seedID is the unit id the seed gives a code.
func seedID(t *testing.T, code string) string {
	t.Helper()
	if id, ok := seedUnitIDs[code]; ok {
		return id
	}
	if !reRecordID.MatchString(code) {
		t.Fatalf("%s is no record id and seedUnitIDs names none for it", code)
	}
	return code
}

type seedUnit struct {
	group string
	code  string
	rec   vocabulary.UnitRecord
}

type seedConversion struct {
	group string
	rec   vocabulary.ConversionRecord
}

// seedFromTable is the seed as the table states it: every listed code a unit
// in the first measure that lists it, and every code linked to the first code
// of its home measure it converts to, the factor and offset the table's own
// ConvertIn answers. A code two measures list is linked in its home measure
// alone (MeasureOf), so Cel reads as a temperature and 1 as a fraction.
func seedFromTable(t *testing.T) ([]seedUnit, []seedConversion) {
	t.Helper()
	aliases := map[string][]string{}
	for alias, code := range vocabulary.UnitAliases() {
		aliases[code] = append(aliases[code], alias)
	}
	var units []seedUnit
	seen := map[string]bool{}
	add := func(group, code string, scale vocabulary.UnitScale) {
		id := seedID(t, code)
		name, ok := seedUnitNames[code]
		if !ok {
			t.Fatalf("seedUnitNames names no unit for %s", code)
		}
		u := vocabulary.UnitRecord{ID: id, Name: name, Symbol: vocabulary.UnitSymbol(code), Code: code, Scale: scale}
		for _, a := range aliases[code] {
			if a != id {
				u.Aliases = append(u.Aliases, a)
			}
		}
		sort.Strings(u.Aliases)
		if turn, ok := seedTurns[code]; ok {
			r, err := vocabulary.ParseRational(turn)
			if err != nil {
				t.Fatal(err)
			}
			u.Turn = r
		}
		units = append(units, seedUnit{group: group, code: code, rec: u})
		seen[code] = true
	}
	for _, m := range vocabulary.Measures() {
		for _, code := range m.Units {
			if !seen[code] {
				add(m.Name, code, seedScales[m.Name])
			}
		}
	}
	for _, code := range seedAnnotations {
		add("counts the shipped samples write", code, "")
	}

	var conversions []seedConversion
	for _, m := range vocabulary.Measures() {
		var home []string
		for _, code := range m.Units {
			if h, err := vocabulary.MeasureOf(code); err == nil && h.Name == m.Name {
				home = append(home, code)
			}
		}
		for i, code := range home {
			for _, anchor := range home[:i] {
				factor, offset, ok := tableMap(m.Name, code, anchor)
				if !ok {
					continue
				}
				c := vocabulary.ConversionRecord{
					ID:   seedID(t, code) + "-" + seedID(t, anchor),
					From: seedID(t, code), To: seedID(t, anchor), Factor: factor,
				}
				if offset.Sign() != 0 {
					c.Offset = offset
				}
				conversions = append(conversions, seedConversion{group: m.Name, rec: c})
				break
			}
		}
	}
	return units, conversions
}

// tableMap is the table's exact map between two codes of a measure, read from
// ConvertIn at 0 and at 1.
func tableMap(measure, from, to string) (factor, offset *big.Rat, ok bool) {
	at0, err := vocabulary.ConvertIn(measure, new(big.Rat), from, to)
	if err != nil {
		return nil, nil, false
	}
	at1, err := vocabulary.ConvertIn(measure, big.NewRat(1, 1), from, to)
	if err != nil {
		return nil, nil, false
	}
	return new(big.Rat).Sub(at1, at0), at0, true
}

// --- rendering ------------------------------------------------------------------

func seedHeader(what string) string {
	return "# The " + what + " every repository is seeded with (decision 0186): the\n" +
		"# engine's unit table as records. Written by TestUnitSeedMatchesTheTable\n" +
		"# from internal/vocabulary/unitstable.go; rewrite with\n" +
		"#\n" +
		"#   go test ./internal/vocabulary/ -run TestUnitSeedMatchesTheTable -update\n" +
		"#\n" +
		"# and never by hand.\n"
}

func renderSeedUnits(t *testing.T, units []seedUnit) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteString(seedHeader("units"))
	group := ""
	for i, u := range units {
		if i > 0 {
			b.WriteString("---\n")
		}
		if u.group != group {
			group = u.group
			b.WriteString("# " + group + "\n")
		}
		props := seedMapping()
		seedPut(props, "name", seedScalar(u.rec.Name))
		seedPut(props, "symbol", seedScalar(u.rec.Symbol))
		seedPut(props, "code", seedScalar(u.rec.Code))
		if len(u.rec.Aliases) > 0 {
			list := &yaml.Node{Kind: yaml.SequenceNode}
			for _, a := range u.rec.Aliases {
				list.Content = append(list.Content, seedScalar(a))
			}
			seedPut(props, "aliases", list)
		}
		if u.rec.Scale != "" {
			seedPut(props, "scale", seedScalar(string(u.rec.Scale)))
		}
		if u.rec.Turn != nil {
			seedPut(props, "turn", seedRational(u.rec.Turn))
		}
		seedDocument(t, &b, vocabulary.KindUnit, u.rec.ID, props)
	}
	return b.Bytes()
}

func renderSeedConversions(t *testing.T, conversions []seedConversion) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteString(seedHeader("conversions"))
	group := ""
	ref := func(id string) *yaml.Node {
		n := seedMapping()
		seedPut(n, "ref", seedScalar(vocabulary.KindUnit+"/"+id))
		return n
	}
	for i, c := range conversions {
		if i > 0 {
			b.WriteString("---\n")
		}
		if c.group != group {
			group = c.group
			b.WriteString("# " + group + "\n")
		}
		props := seedMapping()
		seedPut(props, "from", ref(c.rec.From))
		seedPut(props, "to", ref(c.rec.To))
		seedPut(props, "factor", seedRational(c.rec.Factor))
		if c.rec.Offset != nil {
			seedPut(props, "offset", seedRational(c.rec.Offset))
		}
		seedDocument(t, &b, vocabulary.KindConversion, c.rec.ID, props)
	}
	return b.Bytes()
}

func seedDocument(t *testing.T, b *bytes.Buffer, kind, id string, props *yaml.Node) {
	t.Helper()
	meta, data, doc := seedMapping(), seedMapping(), seedMapping()
	seedPut(meta, "id", seedScalar(id))
	seedPut(data, "properties", props)
	seedPut(doc, "kind", seedScalar(kind))
	seedPut(doc, "metadata", meta)
	seedPut(doc, "data", data)
	enc := yaml.NewEncoder(b)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
}

func seedMapping() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode} }

func seedPut(m *yaml.Node, key string, v *yaml.Node) {
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, v)
}

// seedScalar is a string, double-quoted where a plain scalar would read as
// something else.
func seedScalar(v string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	if out, err := yaml.Marshal(n); err != nil || strings.HasPrefix(string(out), "'") || strings.HasPrefix(string(out), `"`) {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// seedRational is a rational, always quoted, so no reader takes it for a
// float.
func seedRational(r *big.Rat) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: vocabulary.FormatRational(r)}
}

// --- reading --------------------------------------------------------------------

// readSeed reads the shipped seed files through CheckUnitRecord and
// CheckConversionRecord, as the engine reads a stored row.
func readSeed(t *testing.T) ([]vocabulary.UnitRecord, []vocabulary.ConversionRecord) {
	t.Helper()
	var units []vocabulary.UnitRecord
	var conversions []vocabulary.ConversionRecord
	for _, file := range []string{seedUnitsFile, seedConversionsFile} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v (rewrite it with -update)", file, err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		for {
			var doc struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					ID string `yaml:"id"`
				} `yaml:"metadata"`
				Data struct {
					Properties map[string]any `yaml:"properties"`
				} `yaml:"data"`
			}
			if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			switch doc.Kind {
			case vocabulary.KindUnit:
				u, err := vocabulary.CheckUnitRecord(doc.Metadata.ID, doc.Data.Properties)
				if err != nil {
					t.Fatalf("%s: unit %s: %v", file, doc.Metadata.ID, err)
				}
				units = append(units, u)
			case vocabulary.KindConversion:
				c, err := vocabulary.CheckConversionRecord(doc.Metadata.ID, doc.Data.Properties)
				if err != nil {
					t.Fatalf("%s: conversion %s: %v", file, doc.Metadata.ID, err)
				}
				conversions = append(conversions, c)
			default:
				t.Fatalf("%s: a document of kind %q", file, doc.Kind)
			}
		}
	}
	return units, conversions
}

func shippedGraph(t *testing.T) *vocabulary.UnitGraph {
	t.Helper()
	units, conversions := readSeed(t)
	g, problems := vocabulary.BuildUnitGraph(units, conversions)
	for _, p := range problems {
		t.Errorf("the shipped seed: %v", p)
	}
	return g
}

// --- the tests ------------------------------------------------------------------

func TestUnitSeedMatchesTheTable(t *testing.T) {
	units, conversions := seedFromTable(t)
	for file, want := range map[string][]byte{
		seedUnitsFile:       renderSeedUnits(t, units),
		seedConversionsFile: renderSeedConversions(t, conversions),
	} {
		if *updateGenerated {
			if err := os.WriteFile(file, want, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v (rewrite it with -update)", file, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from the engine's unit table: rewrite it with -update, never by hand", file)
		}
	}
	// Every name the maps carry is a unit the seed ships, so neither map
	// keeps a spelling the table no longer lists.
	shipped := map[string]bool{}
	for _, u := range units {
		shipped[u.code] = true
	}
	for _, m := range []map[string]string{seedUnitIDs, seedUnitNames, seedTurns} {
		for code := range m {
			if !shipped[code] {
				t.Errorf("%s is in a seed map and is no unit the seed ships", code)
			}
		}
	}
	if len(units) != 216 {
		t.Errorf("the seed ships %d units; the map in docs/plans/dynamic-units.md says 216", len(units))
	}
}

func TestShippedUnitIDsAreRecordIDs(t *testing.T) {
	units, conversions := readSeed(t)
	for _, u := range units {
		if !vocabulary.ValidID(u.ID) {
			t.Errorf("unit id %q is not a record id", u.ID)
		}
	}
	for _, c := range conversions {
		if !vocabulary.ValidID(c.ID) {
			t.Errorf("conversion id %q is not a record id", c.ID)
		}
		if c.ID != c.From+"-"+c.To {
			t.Errorf("conversion %s joins %s to %s: a shipped conversion's id is <from>-<to>", c.ID, c.From, c.To)
		}
	}
}

func TestShippedSpellingsAreUnique(t *testing.T) {
	units, _ := readSeed(t)
	owner := map[string]string{}
	normalize := strings.NewReplacer("µ", "u", "μ", "u", "²", "2", "³", "3")
	for _, u := range units {
		spellings := append([]string{u.ID, u.Code}, u.Aliases...)
		for _, s := range spellings {
			for _, key := range []string{s, normalize.Replace(s)} {
				if prev, ok := owner[key]; ok && prev != u.ID {
					t.Errorf("%q names both %s and %s", key, prev, u.ID)
				}
				owner[key] = u.ID
			}
		}
	}
	shippedGraph(t)
}

// TestShippedSpellingsResolve holds every spelling a writer sends today to the
// unit it names today: every listed code, and every alias, to the id of the
// code it maps to.
func TestShippedSpellingsResolve(t *testing.T) {
	g := shippedGraph(t)
	for _, code := range append(vocabulary.UnitCodes(), seedAnnotations...) {
		if got, ok := g.Resolve(code); !ok || got != seedID(t, code) {
			t.Errorf("%s resolves to %q, %v; want %s", code, got, ok, seedID(t, code))
		}
	}
	for alias, code := range vocabulary.UnitAliases() {
		if got, ok := g.Resolve(alias); !ok || got != seedID(t, code) {
			t.Errorf("alias %s resolves to %q, %v; want %s, the id of %s", alias, got, ok, seedID(t, code), code)
		}
	}
	if got, _ := g.Resolve("count"); got != "count" {
		t.Errorf("count resolves to %q; it is the id of {count}", got)
	}
	if got, _ := g.Resolve("μg/m³"); got != "ug/m3" {
		t.Errorf("μg/m³ resolves to %q; normalized it is ug/m3", got)
	}
}

// TestShippedGraphMatchesTheOldTable converts every pair of codes inside every
// measure through ConvertIn and through the graph built from the seed, and
// holds the two to one answer: the same exact map, or a refusal from both,
// and the same digits and exactness for one value. A difference reads with
// its offsets dropped. And no component of the graph reaches outside one
// measure, so the graph converts nothing the table refused.
func TestShippedGraphMatchesTheOldTable(t *testing.T) {
	g := shippedGraph(t)
	ctx := context.Background()
	id := func(code string) string { return seedID(t, code) }
	pairs := 0
	for _, m := range vocabulary.Measures() {
		delta := m.Scale == vocabulary.ScaleDifference
		for _, a := range m.Units {
			for _, b := range m.Units {
				pairs++
				oldF, oldO, oldOK := tableMap(m.Name, a, b)
				newF, newO, err := g.Linear(id(a), id(b), delta)
				switch {
				case oldOK != (err == nil):
					t.Errorf("%s: %s to %s: the table converts %v, the graph %v (%v)", m.Name, a, b, oldOK, err == nil, err)
					continue
				case !oldOK:
					continue
				case oldF.Cmp(newF) != 0 || oldO.Cmp(newO) != 0:
					t.Errorf("%s: %s to %s: the table says x * %s + %s, the graph x * %s + %s",
						m.Name, a, b, oldF.RatString(), oldO.RatString(), newF.RatString(), newO.RatString())
					continue
				}
				for _, v := range []string{"37.5", "-1", "1000000.001"} {
					old, err := vocabulary.ConvertDigits(v, a, b, m.Name)
					if err != nil {
						t.Errorf("%s: %s %s to %s: %v", m.Name, v, a, b, err)
						continue
					}
					got, err := g.Convert(ctx, v, id(a), id(b), delta)
					if err != nil {
						t.Errorf("%s: %s %s to %s through the graph: %v", m.Name, v, a, b, err)
						continue
					}
					if got.Value != old.Value || got.Exact != old.Exact || got.Unit != id(b) {
						t.Errorf("%s: %s %s to %s: the table answers %s (exact %v), the graph %s %s (exact %v)",
							m.Name, v, a, b, old.Value, old.Exact, got.Value, got.Unit, got.Exact)
					}
				}
			}
		}
	}
	if pairs == 0 {
		t.Fatal("no pair converted")
	}

	measures := map[string][]string{}
	for _, m := range vocabulary.Measures() {
		for _, code := range m.Units {
			measures[id(code)] = append(measures[id(code)], m.Name)
		}
	}
	for _, code := range seedAnnotations {
		measures[id(code)] = []string{"count of " + code}
	}
	checked := map[string]bool{}
	for _, u := range g.Units() {
		root, _ := g.Root(u.ID)
		if checked[root] {
			continue
		}
		checked[root] = true
		members := g.Component(u.ID)
		common := slices.Clone(measures[members[0]])
		for _, other := range members[1:] {
			common = slices.DeleteFunc(common, func(m string) bool { return !slices.Contains(measures[other], m) })
		}
		if len(common) == 0 {
			t.Errorf("the component %v reaches across measures", members)
		}
	}
}

func TestShippedUnitsKeepMeasuresApart(t *testing.T) {
	g := shippedGraph(t)
	for _, pair := range [][2]string{
		{"J", "N.m"}, {"Gy", "Sv"}, {"Gy", "J/kg"}, {"Hz", "Bq"}, {"Hz", "1/s"}, {"count", "percent"},
		{"count", "1"}, {"mm/h", "km/h"}, {"V.A", "W"}, {"var", "V.A"}, {"rad", "deg"}, {"dBm", "dBW"},
		{"IU", "IU/L"}, {"score", "percent"}, {"mmol/mol", "percent"}, {"L/100km", "mpg"},
	} {
		a, b := pair[0], pair[1]
		if _, ok := g.Unit(a); !ok {
			if _, ok := g.Resolve(a); !ok {
				continue
			}
		}
		if _, _, err := g.Linear(a, b, false); err == nil {
			t.Errorf("%s and %s convert; the table keeps them apart", a, b)
		}
		ra, _ := g.Root(a)
		rb, _ := g.Root(b)
		if ra != "" && ra == rb {
			t.Errorf("%s and %s share the root %s", a, b, ra)
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
