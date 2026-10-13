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
		"[lb_av]/[ft_i]3": "mass-concentration", "g/(kg)": "fraction", "kg/h": "mass-flow",
		// A measure that admits its listed codes alone, and their prefixes.
		"mmol/mol": "amount-fraction", "umol/mol": "amount-fraction", "mol/kmol": "amount-fraction",
		"{score}": "score",
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

// Conversions UCUM makes and a person would call wrong are refused by
// construction (decision 0153): torque is not energy, an absorbed dose is
// neither a dose equivalent nor a specific energy, and an activity is not a
// frequency. Gy, Sv and Bq are kept apart from the coherent units they equal,
// in every compound and under every prefix.
func TestQuantityConvertsOnlyWithinAMeasure(t *testing.T) {
	for _, c := range [][2]string{
		{"N.m", "J"}, {"kN.m", "kJ"}, {"V.A", "W"},
		{"Gy", "Sv"}, {"mGy", "mSv"}, {"Gy", "J/kg"}, {"Gy", "kcal/kg"}, {"Sv", "J/kg"},
		{"Hz", "Bq"}, {"kHz", "kBq"}, {"Bq", "/s"}, {"Bq", "s-1"}, {"Ci", "Hz"},
		{"{beats}/min", "{steps}/min"},
	} {
		refusesConvert(t, "", c[0], c[1], "two measures")
		refusesConvert(t, "", c[1], c[0], "two measures, the other way")
	}
	for _, c := range []struct{ measure, from, to string }{
		{"dose-equivalent-rate", "uSv/h", "uGy/h"},
		{"absorbed-dose", "Gy", "J/kg"},
		{"dose-equivalent", "mSv", "mGy"},
		{"specific-energy", "J/kg", "Gy"},
		{"specific-power", "W/kg", "Gy/h"},
		{"frequency", "Hz", "Bq"},
		{"radioactivity", "Bq", "s-1"},
	} {
		refusesConvert(t, c.measure, c.from, c.to, "the code is kept out of the measure")
	}
	for _, c := range []struct{ value, from, to, want string }{
		{"5", "mGy", "Gy", "0.005"},
		{"3", "uSv/h", "mSv/h", "0.003"},
		{"1", "Ci", "Bq", "37000000000"},
		{"2", "kBq", "Bq", "2000"},
		{"1", "J/kg", "kcal/kg", "1/4184"},
	} {
		if got := convertTo(t, "", c.value, c.from, c.to); got.Cmp(rat(t, c.want)) != 0 {
			t.Errorf("%s %s to %s = %s, want %s", c.value, c.from, c.to, got.RatString(), c.want)
		}
	}
	for code, want := range map[string]string{
		"Gy": "absorbed-dose", "mGy": "absorbed-dose", "Sv": "dose-equivalent", "uSv": "dose-equivalent",
		"Bq": "radioactivity", "MBq": "radioactivity", "Ci": "radioactivity", "J/kg": "specific-energy",
		"uSv/h": "dose-equivalent-rate", "Hz": "frequency", "kHz": "frequency",
	} {
		if m, err := vocabulary.MeasureOf(code); err != nil || m.Name != want {
			t.Errorf("MeasureOf(%s) = %v, %v; want %s", code, m.Name, err, want)
		}
	}
	// An absorbed dose rate has no measure yet: it is refused undeclared,
	// never read as a specific power.
	if m, err := vocabulary.MeasureOf("uGy/h"); err == nil {
		t.Errorf("MeasureOf(uGy/h) = %s, want a refusal", m.Name)
	}
}

// Convert reads the two codes the same way whichever is first: where one code
// alone is ambiguous, the other code's measure decides, in both directions.
func TestConvertIsSymmetric(t *testing.T) {
	for _, c := range []struct{ a, b, want string }{
		{"W", "J/s", "1"},
		{"kW", "J/s", "1000"},
		{"/s", "s-1", "1"},
		{"m2", "cm.cm", "10000"},
	} {
		if got := convertTo(t, "", "1", c.a, c.b); got.Cmp(rat(t, c.want)) != 0 {
			t.Errorf("1 %s to %s = %s, want %s", c.a, c.b, got.RatString(), c.want)
		}
		if back := convertTo(t, "", c.want, c.b, c.a); back.Cmp(rat(t, "1")) != 0 {
			t.Errorf("%s %s to %s = %s, want 1", c.want, c.b, c.a, back.RatString())
		}
	}
	for _, c := range [][2]string{{"Hz", "Bq"}, {"J/s", "V.A"}, {"kg.m2/s2", "J"}} {
		_, err1 := vocabulary.Convert(big.NewRat(1, 1), c[0], c[1])
		_, err2 := vocabulary.Convert(big.NewRat(1, 1), c[1], c[0])
		if (err1 == nil) != (err2 == nil) {
			t.Errorf("%s and %s convert one way only: %v, %v", c[0], c[1], err1, err2)
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

// A vendor's score (WHOOP strain, Garmin stress) is a number on the vendor's
// own scale: it converts to nothing but itself, never to a percent, a bare
// number or a count, and a measure listing it admits no other code.
func TestAScoreConvertsToNothingButItself(t *testing.T) {
	if got := convertTo(t, "", "17.5", "{score}", "{score}"); got.Cmp(rat(t, "17.5")) != 0 {
		t.Errorf("{score} to itself = %s", got.RatString())
	}
	for _, to := range []string{"%", "1", "{count}", "[ppth]", "10*3{score}"} {
		refusesConvert(t, "", "{score}", to, "a score converts to nothing but itself")
		refusesConvert(t, "", to, "{score}", "the same, the other way")
	}
	for _, code := range []string{"%", "1", "{count}", "%{score}", "g/kg"} {
		if _, err := vocabulary.ResolveMeasure(code, "score"); err == nil {
			t.Errorf("measures: score admitted %s", code)
		}
	}
	for _, code := range []string{"g/kg", "%", "mol/m3"} {
		if _, err := vocabulary.ResolveMeasure(code, "amount-fraction"); err == nil {
			t.Errorf("measures: amount-fraction admitted %s", code)
		}
	}
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

// An ordering key has a factor for every admitted code: the exact one where
// a rational number states it, pi at 40 places where only pi reaches the
// base, and a prefixed code the table does not list converts by its prefix.
func TestOrderFactorKeysEveryAdmittedCode(t *testing.T) {
	f, o, err := vocabulary.OrderFactor("angle", "rad")
	if err != nil || o.Sign() != 0 || f.FloatString(30) != "57.295779513082320876798154814105" {
		t.Errorf("rad keys at %v + %v (%v), want 180/pi", f.FloatString(30), o, err)
	}
	if f, _, err := vocabulary.OrderFactor("mass", "Mg"); err != nil || f.Cmp(rat(t, "1000")) != 0 {
		t.Errorf("Mg keys at %v (%v), want 1000 kg", f, err)
	}
	if f, o, err := vocabulary.OrderFactor("temperature", "[degF]"); err != nil || f.Cmp(rat(t, "5/9")) != 0 || o.Sign() == 0 {
		t.Errorf("[degF] keys at %v + %v (%v), want ToBase's exact map", f, o, err)
	}
	if _, _, err := vocabulary.OrderFactor("arbitrary", "[IU]"); err == nil {
		t.Error("an arbitrary unit has a base to key by")
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
	// IFCC and NGSP HbA1c relate by a formula, never a factor: mmol/mol is an
	// amount fraction and % a fraction, so the two never convert, read
	// undeclared or under either measure.
	refusesConvert(t, "", "mmol/mol", "%", "an amount fraction is not a fraction")
	refusesConvert(t, "", "%", "mmol/mol", "the same, the other way")
	refusesConvert(t, "fraction", "%", "mmol/mol", "a fraction refuses mmol/mol")
	refusesConvert(t, "amount-fraction", "mmol/mol", "1", "an amount fraction refuses a bare number")
	if got := convertTo(t, "", "48", "mmol/mol", "umol/mol"); got.Cmp(rat(t, "48000")) != 0 {
		t.Errorf("48 mmol/mol = %s umol/mol, want 48000", got.RatString())
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

// HealthKit's HKUnit strings, as Health Auto Export sends them, read as the
// codes they mean, so an adapter passes the app's unit straight to the table.
func TestCanonicalUnitReadsHealthKitSpellings(t *testing.T) {
	for in, want := range map[string]string{
		// The aliases HealthKit needs.
		"count": "{count}", "count/min": "{count}/min", "hr": "h",
		"lb": "[lb_av]", "oz": "[oz_av]",
		"in": "[in_i]", "yd": "[yd_i]", "mi": "[mi_i]", "km/hr": "km/h", "mi/hr": "[mi_i]/h", "kmph": "km/h",
		"mL/(kg*min)": "mL/(kg.min)", "ml/(kg·min)": "mL/(kg.min)",
		"mmol<180.1558800000541>/L": "mmol/L", "mmol<180.15588000005408>/L": "mmol/L",
		"IU": "[IU]", "dBASPL": "dB[SPL]", "cmAq": "cm[H2O]",
		"degC": "Cel", "degF": "[degF]", "Cal": "[Cal]", "mmHg": "mm[Hg]",
		// The ones that are UCUM codes as HealthKit writes them.
		"kcal": "kcal", "kJ": "kJ", "mg/dL": "mg/dL", "%": "%", "ms": "ms", "s": "s", "min": "min", "d": "d",
		"m": "m", "km": "km", "cm": "cm", "kg": "kg", "g": "g", "mL": "mL", "L": "L", "W": "W", "m/s": "m/s",
	} {
		got, err := vocabulary.CanonicalUnit(in)
		if err != nil || got != want {
			t.Errorf("CanonicalUnit(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, c := range []struct{ value, from, to, want string }{
		{"1", "lb", "kg", "45359237/100000000"},
		{"1", "mi", "m", "201168/125"},
		{"2", "hr", "min", "120"},
		{"5.5", "mmol<180.1558800000541>/L", "mmol/L", "5.5"},
		{"42", "ml/(kg·min)", "mL/(kg.min)", "42"},
	} {
		from, err := vocabulary.CanonicalUnit(c.from)
		if err != nil {
			t.Fatalf("CanonicalUnit(%q): %v", c.from, err)
		}
		if got := convertTo(t, "", c.value, from, c.to); got.Cmp(rat(t, c.want)) != 0 {
			t.Errorf("%s %s to %s = %s, want %s", c.value, c.from, c.to, got.RatString(), c.want)
		}
	}
	// A bare count per minute is not a heart rate: the annotation keeps it
	// apart from every other rate, and the writer that knows what was counted
	// converts it.
	refusesConvert(t, "", "{count}/min", "/min", "an annotation is part of the unit")
	// `ft` is the femtotonne, a UCUM code, so it can never be the foot's alias.
	if got, err := vocabulary.CanonicalUnit("ft"); err != nil || got != "ft" {
		t.Errorf("CanonicalUnit(%q) = %q, %v, want the femtotonne code itself", "ft", got, err)
	}
	if m, err := vocabulary.MeasureOf("ft"); err != nil || m.Name != "mass" {
		t.Errorf("ft measures %v, %v, want mass", m.Name, err)
	}
	// `st` is UCUM's stere, a cubic meter, which the table does not carry:
	// it is refused, never read as the stone.
	if got, err := vocabulary.CanonicalUnit("st"); err == nil {
		t.Errorf("CanonicalUnit(%q) = %q, want a refusal: UCUM's st is the stere", "st", got)
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
