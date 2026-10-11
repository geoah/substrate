package vocabulary_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func rat(t *testing.T, s string) *big.Rat {
	t.Helper()
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		t.Fatalf("bad rational %q", s)
	}
	return r
}

func convertTo(t *testing.T, measure, value, from, to string) *big.Rat {
	t.Helper()
	var got *big.Rat
	var err error
	if measure == "" {
		got, err = vocabulary.Convert(rat(t, value), from, to)
	} else {
		got, err = vocabulary.ConvertIn(measure, rat(t, value), from, to)
	}
	if err != nil {
		t.Fatalf("%s %s to %s (%s): %v", value, from, to, measure, err)
	}
	return got
}

func refusesConvert(t *testing.T, measure, from, to, why string) {
	t.Helper()
	var err error
	if measure == "" {
		_, err = vocabulary.Convert(big.NewRat(1, 1), from, to)
	} else {
		_, err = vocabulary.ConvertIn(measure, big.NewRat(1, 1), from, to)
	}
	if err == nil {
		t.Fatalf("%s to %s (%s) converted; %s", from, to, measure, why)
	}
}

func TestUnitTableLoads(t *testing.T) {
	ms := vocabulary.Measures()
	if len(ms) == 0 {
		t.Fatal("the unit table did not load")
	}
	codes := 0
	for _, m := range ms {
		codes += len(m.Units)
		for _, c := range m.Units {
			got, err := vocabulary.ResolveMeasure(c, m.Name)
			if err != nil || got.Name != m.Name {
				t.Errorf("%s does not admit its own code %s: %v", m.Name, c, err)
			}
		}
	}
	t.Logf("%d measures, %d rows, %d codes", len(ms), codes, len(vocabulary.UnitCodes()))
}

func TestMeasureOf(t *testing.T) {
	for code, want := range map[string]string{
		// Rows.
		"kg": "mass", "[lb_av]": "mass", "Cel": "temperature", "K": "temperature", "[degF]": "temperature",
		"1": "fraction", "%": "fraction", "N.m": "torque", "J": "energy", "W": "power", "V.A": "apparent-power",
		"V.A{reactive}": "reactive-power", "Hz": "frequency", "/min": "rate", "dB": "level", "dBm": "power-level",
		"[pH]": "acidity", "U/L": "catalytic-concentration", "U": "catalytic-activity", "kat": "catalytic-activity",
		"[IU]": "arbitrary", "kW.h/(100.km)": "energy-per-distance", "mL/(kg.min)": "oxygen-uptake-rate",
		"10*9/L": "number-concentration", "10*3/uL": "number-concentration",
		// A prefix the table does not list belongs to its row's measure.
		"GJ": "energy", "kV.A": "apparent-power", "kV.A{reactive}": "reactive-power", "mK": "temperature",
		"fL": "volume", "pg": "mass", "/ms": "rate", "MHz": "frequency", "TBy": "information",
		"mSv/h": "dose-equivalent-rate", "kN.m": "torque", "mrad": "angle",
		// A compound no row lists, by its dimension.
		"[lb_av]/[ft_i]3": "mass-concentration", "g/(kg)": "fraction", "umol/mol": "fraction", "kg/h": "mass-flow",
		// Annotations.
		"{steps}": "count", "{beats}/min": "rate", "{steps}/h": "rate", "10*3{copies}": "count",
		"kg{body_wt}": "mass",
		// Arbitrary units.
		"u[IU]/mL": "arbitrary", "[iU]": "arbitrary", "k[IU]": "arbitrary",
	} {
		m, err := vocabulary.MeasureOf(code)
		if err != nil {
			t.Errorf("MeasureOf(%s): %v, want %s", code, err, want)
			continue
		}
		if m.Name != want {
			t.Errorf("MeasureOf(%s) = %s, want %s", code, m.Name, want)
		}
	}
}

// A dimension two measures share is refused unless the property names one:
// the review's rule, because the vector alone is a calculator's answer.
func TestAmbiguousDimensionsAreRefusedUndeclared(t *testing.T) {
	for code, measures := range map[string][]string{
		"kg.m2/s2": {"energy", "torque"},
		"kg.m/s2":  {"force", "energy-per-distance"},
		"J/s":      {"power", "apparent-power"},
		"J/m":      {"force", "energy-per-distance"},
		"m/min":    {"speed", "precipitation-rate"},
		"Bq":       {"frequency", "rate"},
		"L/km":     {"area", "fuel-consumption"},
	} {
		_, err := vocabulary.MeasureOf(code)
		if err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Errorf("MeasureOf(%s) = %v, want an ambiguity naming %v", code, err, measures)
			continue
		}
		for _, name := range measures {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("MeasureOf(%s): %v does not name %s", code, err, name)
			}
			if _, err := vocabulary.ResolveMeasure(code, name); err != nil {
				t.Errorf("declared %s refuses %s: %v", name, code, err)
			}
		}
	}
}

func TestDeclaredMeasureRefusesAnotherMeasuresCode(t *testing.T) {
	for _, c := range []struct{ code, measure string }{
		{"V.A", "power"},
		{"W", "apparent-power"},
		{"kV.A", "power"},
		{"N.m", "energy"},
		{"kN.m", "energy"},
		{"Hz", "rate"},
		{"{beats}/min", "frequency"},
		{"km", "mass"},
		{"Cel", "mass"},
		{"{steps}", "fraction"},
		{"%", "count"},
		{"[IU]", "mass"},
		{"kg", "arbitrary"},
		{"dB", "power-level"},
		{"B", "level"},
		{"V.A", "reactive-power"},
		{"kV.A", "reactive-power"},
		{"mol/L", "mass-concentration"},
		{"mg/dL", "amount-concentration"},
	} {
		if _, err := vocabulary.ResolveMeasure(c.code, c.measure); err == nil {
			t.Errorf("measures: %s admitted %s", c.measure, c.code)
		}
	}
	if _, err := vocabulary.ResolveMeasure("kg", "weight"); err == nil || !strings.Contains(err.Error(), "not a measure") {
		t.Errorf("an unknown measure name: %v", err)
	}
}

func TestOffsetTemperatureConvertsAffinely(t *testing.T) {
	for _, c := range []struct{ measure, value, from, to, want string }{
		{"", "37", "Cel", "[degF]", "98.6"},
		{"", "0", "Cel", "[degF]", "32"},
		{"", "-40", "Cel", "[degF]", "-40"},
		{"", "1", "[degF]", "Cel", "-155/9"},
		{"", "300", "K", "Cel", "26.85"},
		{"", "0", "K", "[degF]", "-459.67"},
		{"temperature", "98.6", "[degF]", "K", "310.15"},
		{"temperature", "1000", "mK", "Cel", "-272.15"},
		// A difference converts by the factor alone.
		{"temperature-difference", "0.3", "Cel", "[degF]", "0.54"},
		{"temperature-difference", "1", "K", "Cel", "1"},
		{"temperature-difference", "9", "[degF]", "K", "5"},
	} {
		got := convertTo(t, c.measure, c.value, c.from, c.to)
		if got.Cmp(rat(t, c.want)) != 0 {
			t.Errorf("%s %s to %s (%s) = %s, want %s", c.value, c.from, c.to, c.measure, got.RatString(), c.want)
		}
	}
	// An offset temperature is a unit alone: never a compound, never prefixed.
	for _, code := range []string{"Cel/h", "mCel", "Cel2", "[degF].m", "/Cel"} {
		if _, err := vocabulary.MeasureOf(code); err == nil {
			t.Errorf("MeasureOf(%s) admitted an offset temperature inside a compound", code)
		}
	}
}

func TestLevelsConvertToNothingButThemselves(t *testing.T) {
	for _, code := range []string{"dB", "dB[SPL]", "dB[W]", "dBm", "[pH]"} {
		if got := convertTo(t, "", "7.4", code, code); got.Cmp(rat(t, "7.4")) != 0 {
			t.Errorf("%s to itself = %s", code, got.RatString())
		}
	}
	refusesConvert(t, "", "dB", "dB[SPL]", "two references are two measures")
	refusesConvert(t, "power-level", "dB[W]", "dBm", "a level converts to nothing but itself")
	refusesConvert(t, "", "[pH]", "mol/L", "pH is a logarithm of an activity, not a concentration")
	refusesConvert(t, "", "dB", "1", "a level is not a fraction")
	for _, code := range []string{"B", "cB", "dB2", "dB/s", "kdBm", "dBm/Hz"} {
		if _, err := vocabulary.MeasureOf(code); err == nil {
			t.Errorf("MeasureOf(%s) admitted a level the table does not list", code)
		}
	}
	if _, _, err := vocabulary.ToBase("level", "dB"); err == nil {
		t.Error("a level has a base to convert to")
	}
}

// The substrate's own extension, labeled as one: an arbitrary unit converts to the same
// atom and annotation under another metric prefix, and to nothing else. UCUM
// itself converts an arbitrary unit to nothing.
func TestArbitraryUnitsConvertOnlyByPrefix(t *testing.T) {
	for _, c := range []struct{ value, from, to, want string }{
		{"5", "m[IU]/L", "[IU]/L", "0.005"},
		{"1", "u[IU]/mL", "m[IU]/L", "1"},
		{"2", "[IU]/mL", "[IU]/L", "2000"},
		{"3", "k[IU]", "[IU]", "3000"},
	} {
		got := convertTo(t, "", c.value, c.from, c.to)
		if got.Cmp(rat(t, c.want)) != 0 {
			t.Errorf("%s %s to %s = %s, want %s", c.value, c.from, c.to, got.RatString(), c.want)
		}
	}
	refusesConvert(t, "", "[IU]", "[arb'U]", "two assay units are two procedures")
	refusesConvert(t, "", "[IU]", "[iU]", "the substrate keeps the two spellings apart")
	refusesConvert(t, "", "[IU]{a}", "[IU]{b}", "two annotations are two assays")
	refusesConvert(t, "", "[IU]", "[IU]/L", "an amount is not a concentration")
	// [IU] reaches no proper unit at all: not one the table lists, not a
	// number, not a mole.
	targets := append(vocabulary.UnitCodes(), "1", "10*3", "mol", "{IU}", "[arb'U]/L")
	for _, to := range targets {
		for _, from := range []string{"[IU]", "[IU]/L", "m[IU]/L"} {
			if strings.HasSuffix(to, "[IU]/L") && from != "[IU]" || to == from {
				continue
			}
			if strings.Contains(to, "[IU]") && strings.Count(to, "/") == strings.Count(from, "/") {
				continue
			}
			if got, err := vocabulary.Convert(big.NewRat(1, 1), from, to); err == nil {
				t.Errorf("%s converted to %s (%s)", from, to, got.RatString())
			}
		}
	}
}

// Annotations are part of a unit's identity, which UCUM discards: heart rate
// and cadence are both /min and never one quantity.
func TestAnnotationsArePartOfIdentity(t *testing.T) {
	if got := convertTo(t, "", "120", "{beats}/min", "{beats}/s"); got.Cmp(rat(t, "2")) != 0 {
		t.Errorf("120 {beats}/min = %s {beats}/s, want 2", got.RatString())
	}
	if got := convertTo(t, "", "8", "10*3{steps}", "{steps}"); got.Cmp(rat(t, "8000")) != 0 {
		t.Errorf("8 thousand steps = %s steps", got.RatString())
	}
	refusesConvert(t, "", "{beats}/min", "{steps}/min", "two annotations never convert")
	refusesConvert(t, "", "{beats}/min", "/min", "an annotated rate converts only to the same annotation")
	refusesConvert(t, "", "{steps}", "{beats}", "two counts of different things")
	refusesConvert(t, "", "{steps}", "1", "a count of steps is not a number")
	refusesConvert(t, "", "%", "{beats}", "a fraction is not a count (UCUM converts it)")
	refusesConvert(t, "", "Hz", "/min", "a frequency is not a rate of events")
}

func TestIrrationalFactorsAreRefused(t *testing.T) {
	if got := convertTo(t, "", "1", "deg", "''"); got.Cmp(rat(t, "3600")) != 0 {
		t.Errorf("1 deg = %s arc seconds", got.RatString())
	}
	if got := convertTo(t, "", "2", "rad", "rad"); got.Cmp(rat(t, "2")) != 0 {
		t.Errorf("rad to itself = %s", got.RatString())
	}
	refusesConvert(t, "", "deg", "rad", "deg to rad is pi/180, which no rational number states")
	refusesConvert(t, "", "rad", "deg", "the same, the other way")
	if _, _, err := vocabulary.ToBase("angle", "rad"); err == nil {
		t.Error("rad has a rational factor to deg")
	}
}

func TestConvertIsExact(t *testing.T) {
	for _, c := range []struct{ value, from, to, want string }{
		{"72.4", "kg", "[lb_av]", "72400000000/453592370"},
		{"155", "[lb_av]", "kg", "70.30681735"},
		{"1", "kcal/d", "W", "523/10800"},
		{"1", "[psi]", "Pa", "8896443230521/1290320000"},
		{"5", "min/km", "min/[mi_i]", "8.04672"},
		{"1", "kW.h", "J", "3600000"},
		{"1", "[MET]", "mL/(kg.min)", "3.5"},
		{"100", "mg/dL", "g/L", "1"},
		{"1", "U/L", "ukat/L", "1/60"},
		{"60", "{count}/min", "{count}/s", "1"},
		// A white cell count in SI and in US units is one number.
		{"6.2", "10*9/L", "10*3/uL", "6.2"},
	} {
		got := convertTo(t, "", c.value, c.from, c.to)
		if got.Cmp(rat(t, c.want)) != 0 {
			t.Errorf("%s %s to %s = %s, want %s", c.value, c.from, c.to, got.RatString(), c.want)
		}
	}
	refusesConvert(t, "", "kg", "J", "two measures")
	refusesConvert(t, "", "mg/dL", "mmol/L", "a molar mass is a derivation, not a unit conversion")
	// IFCC and NGSP HbA1c are two metrics, related by a formula, never a
	// factor; the unit table converts the units, and the metric refuses the
	// pooling.
	if got := convertTo(t, "", "48", "mmol/mol", "%"); got.Cmp(rat(t, "4.8")) != 0 {
		t.Errorf("48 mmol/mol = %s %%, want 4.8", got.RatString())
	}
	refusesConvert(t, "", "N.m", "J", "torque is not energy")
	refusesConvert(t, "", "V.A", "W", "apparent power is not power")
}

func TestCanonicalUnitAppliesAliases(t *testing.T) {
	for in, want := range map[string]string{
		"°C": "Cel", "℃": "Cel", "degC": "Cel", "°F": "[degF]", "℉": "[degF]", "degF": "[degF]",
		"µg": "ug", "μg": "ug", "mcg": "ug", "µg/dL": "ug/dL", "μmol/L": "umol/L",
		"mmHg": "mm[Hg]", "bpm": "{beats}/min", "rpm": "{rev}/min", "kWh": "kW.h", "Wh": "W.h",
		"Cal": "[Cal]", "ppm": "[ppm]", "m³": "m3", "kg/m²": "kg/m2", "count/min": "{count}/min",
		"kg": "kg", " kg ": "kg", "Cel": "Cel", "kW.h/(100.km)": "kW.h/(100.km)",
	} {
		got, err := vocabulary.CanonicalUnit(in)
		if err != nil || got != want {
			t.Errorf("CanonicalUnit(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "KG", "kgs", "lbs", "°K", "m/", "g/12h"} {
		if got, err := vocabulary.CanonicalUnit(in); err == nil {
			t.Errorf("CanonicalUnit(%q) = %q, want a refusal", in, got)
		}
	}
	for alias := range vocabulary.UnitAliases() {
		if _, err := vocabulary.ParseUnit(alias); err == nil {
			t.Errorf("alias %q is itself a code, so it would shadow one", alias)
		}
	}
}

func TestFormatQuantity(t *testing.T) {
	for _, c := range []struct {
		v    map[string]any
		want string
	}{
		{map[string]any{"value": "72.5", "unit": "kg"}, "72.5 kg"},
		{map[string]any{"value": "72.50", "unit": "kg"}, "72.50 kg"},
		{map[string]any{"value": "37", "unit": "Cel"}, "37 °C"},
		{map[string]any{"value": "0.5", "unit": "ng/mL", "comparator": "<"}, "<0.5 ng/mL"},
		{map[string]any{"value": "8000", "unit": "{steps}"}, "8000 steps"},
		{map[string]any{"value": "62", "unit": "{beats}/min"}, "62 beats/min"},
		{map[string]any{"value": "90", "unit": "deg"}, "90°"},
		{map[string]any{"value": "0.42", "unit": "1"}, "0.42"},
		{map[string]any{"value": "5", "unit": "ug/m3"}, "5 μg/m³"},
		{map[string]any{"value": "12", "unit": "kW.h"}, "12 kWh"},
		{map[string]any{"value": "120", "unit": "mm[Hg]"}, "120 mmHg"},
	} {
		if got := vocabulary.FormatQuantity(c.v); got != c.want {
			t.Errorf("FormatQuantity(%v) = %q, want %q", c.v, got, c.want)
		}
	}
	for _, v := range []any{nil, "72.5 kg", map[string]any{"value": 72.5, "unit": "kg"}, map[string]any{"value": "72.5"}} {
		if got := vocabulary.FormatQuantity(v); got != "" {
			t.Errorf("FormatQuantity(%#v) = %q, want nothing", v, got)
		}
	}
}

func TestCanonicalDecimal(t *testing.T) {
	for in, want := range map[string]string{
		"72.50": "72.50", "+1": "1", "007.5": "7.5", "-0.00": "0.00", "-0": "0", "-1.5": "-1.5", ".5": "",
	} {
		got, err := vocabulary.CanonicalDecimal(in)
		if want == "" {
			if err == nil {
				t.Errorf("CanonicalDecimal(%q) = %q, want a refusal", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("CanonicalDecimal(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	if _, err := vocabulary.CanonicalDecimal("1e3"); err == nil || !strings.Contains(err.Error(), "exponent") {
		t.Errorf("an exponent: %v", err)
	}
}

func TestRatDecimal(t *testing.T) {
	for _, c := range []struct {
		in    string
		want  string
		exact bool
	}{
		{"0", "0", true},
		{"362/5", "72.4", true},
		{"-1/8", "-0.125", true},
		{"1/10000", "0.0001", true},
		{"1200", "1200", true},
		{"493/5", "98.6", true},
		{"523/10800", "0.048425925925925925926", false},
		{"1/3", "0.33333333333333333333", false},
		{"-2/3", "-0.66666666666666666667", false},
		{"200000000000000000000000/3", "66666666666666666667000", false},
		{"99999999999999999999999/99999999999999999999990", "1", false},
	} {
		r, ok := new(big.Rat).SetString(c.in)
		if !ok {
			t.Fatalf("bad case %s", c.in)
		}
		got, exact := vocabulary.RatDecimal(r, vocabulary.ConvertedDigits)
		if got != c.want || exact != c.exact {
			t.Errorf("RatDecimal(%s) = %q, %v; want %q, %v", c.in, got, exact, c.want, c.exact)
		}
	}
}
