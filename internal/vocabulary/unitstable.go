package vocabulary

// THE UNIT TABLE: the atoms the grammar reads, the measures a quantity may
// declare, the codes each measure lists, and the input aliases.
//
// Every number here is a definition, copied from UCUM 2.2's ucum-essence.xml
// (copyright Regenstrief Institute, Inc., used under the UCUM license) as a
// decimal and a unit expression, and resolved to an exact rational by the
// grammar itself, so the table carries no hand-computed factor. Where the
// substrate departs from UCUM it says so on the line.
//
// THE TABLE ONLY GROWS. A stored quantity is read through it: remove a code and
// its rows refuse their next write; change a definition and every stored value
// in it means a different amount, silently. Either is a migration of the rows,
// never an edit here. TestUnitTableOnlyGrows pins every measure, code, factor
// and offset as shipped.
//
// The console carries a copy of the measures, codes, print symbols and aliases
// (web/console/src/lib/units.ts) that TestConsoleUnitsMatchTheEngine holds to
// this one, and docs/units.md is written from it by TestUnitsPage.

// atomClass is how an atom converts.
type atomClass uint8

const (
	// atomProper converts by a factor. It is the zero value, which is what
	// most definitions leave the class at.
	_ atomClass = iota
	// atomAffine converts by a factor and an offset, and only alone: Cel and
	// [degF] on the temperature scale.
	atomAffine
	// atomLevel is logarithmic: a level converts to nothing but itself.
	atomLevel
	// atomArbitrary is a unit defined by an assay procedure: it converts to
	// nothing but itself and its own metric prefixes.
	atomArbitrary
	// atomPi is the number pi, carried as an exponent so a factor stays exact.
	atomPi
)

// atomDef is one atom as UCUM defines it: `value` times the unit expression
// `unit`, or, for a base atom, `value` of the coherent unit of `base`.
type atomDef struct {
	code   string
	print  string
	metric bool
	class  atomClass
	value  string
	unit   string
	base   []dimComponent
	// offset is an affine atom's zero, in kelvin: K = x * factor + offset.
	offset string
	// note says where the substrate departs from UCUM.
	note string
}

// dimComponent is one exponent of a base atom's dimension.
type dimComponent struct {
	dim int
	exp int8
}

// The nine components of a dimension: the SI seven, then plane angle and
// information. UCUM folds amount of substance into a number and information
// into one; the substrate keeps both apart, so mmol/L never converts to a cell
// count and By never converts to %.
const (
	dimLength = iota
	dimMass
	dimTime
	dimCurrent
	dimTemperature
	dimAmount
	dimLuminous
	dimAngle
	dimInformation
	dimCount
)

var dimNames = [dimCount]string{"L", "M", "T", "I", "Θ", "N", "J", "A", "B"}

// prefixDef is one UCUM prefix: its code, its print symbol and its value.
type prefixDef struct {
	code  string
	print string
	value string
}

var prefixDefs = []prefixDef{
	{"Y", "Y", "1e24"},
	{"Z", "Z", "1e21"},
	{"E", "E", "1e18"},
	{"P", "P", "1e15"},
	{"T", "T", "1e12"},
	{"G", "G", "1e9"},
	{"M", "M", "1e6"},
	{"k", "k", "1e3"},
	{"h", "h", "1e2"},
	{"da", "da", "1e1"},
	{"d", "d", "1e-1"},
	{"c", "c", "1e-2"},
	{"m", "m", "1e-3"},
	{"u", "μ", "1e-6"},
	{"n", "n", "1e-9"},
	{"p", "p", "1e-12"},
	{"f", "f", "1e-15"},
	{"a", "a", "1e-18"},
	{"z", "z", "1e-21"},
	{"y", "y", "1e-24"},
	{"Ki", "Ki", "1024"},
	{"Mi", "Mi", "1048576"},
	{"Gi", "Gi", "1073741824"},
	{"Ti", "Ti", "1099511627776"},
}

// atomDefs are the atoms the grammar reads: every atom a table code is built
// from, the atoms those are defined by, and the SI derived units. An atom UCUM
// defines and this list does not carry is refused as unknown, and is added
// here, with its definition, when a measure needs it.
var atomDefs = []atomDef{
	// Base atoms. UCUM's bases are m, s, g, rad, K, C and cd; the coherent mass
	// is the kilogram, so g is a thousandth of it.
	{code: "m", print: "m", metric: true, value: "1", base: []dimComponent{{dimLength, 1}}},
	{code: "s", print: "s", metric: true, value: "1", base: []dimComponent{{dimTime, 1}}},
	{code: "g", print: "g", metric: true, value: "1e-3", base: []dimComponent{{dimMass, 1}}},
	{code: "rad", print: "rad", metric: true, value: "1", base: []dimComponent{{dimAngle, 1}}},
	{code: "K", print: "K", metric: true, value: "1", base: []dimComponent{{dimTemperature, 1}}},
	{code: "C", print: "C", metric: true, value: "1", base: []dimComponent{{dimCurrent, 1}, {dimTime, 1}}},
	{code: "cd", print: "cd", metric: true, value: "1", base: []dimComponent{{dimLuminous, 1}}},
	{
		code: "mol", print: "mol", metric: true, value: "1", base: []dimComponent{{dimAmount, 1}},
		note: "UCUM defines mol as the number 6.02214076e23; the substrate keeps amount of substance its own component",
	},
	{
		code: "bit", print: "bit", metric: true, value: "1", base: []dimComponent{{dimInformation, 1}},
		note: "UCUM defines bit as the number 1; the substrate keeps information its own component",
	},

	// Numbers.
	{code: "10*", print: "10", value: "10", unit: "1"},
	{code: "10^", print: "10", value: "10", unit: "1"},
	{code: "[pi]", print: "π", class: atomPi},
	{code: "%", print: "%", value: "1", unit: "10*-2"},
	{code: "[ppth]", print: "‰", value: "1", unit: "10*-3"},
	{code: "[ppm]", print: "ppm", value: "1", unit: "10*-6"},
	{code: "[ppb]", print: "ppb", value: "1", unit: "10*-9"},
	{code: "[pptr]", print: "pptr", value: "1", unit: "10*-12"},

	// SI derived units.
	{code: "sr", print: "sr", metric: true, value: "1", unit: "rad2"},
	{code: "Hz", print: "Hz", metric: true, value: "1", unit: "s-1"},
	{code: "N", print: "N", metric: true, value: "1", unit: "kg.m/s2"},
	{code: "Pa", print: "Pa", metric: true, value: "1", unit: "N/m2"},
	{code: "J", print: "J", metric: true, value: "1", unit: "N.m"},
	{code: "W", print: "W", metric: true, value: "1", unit: "J/s"},
	{code: "A", print: "A", metric: true, value: "1", unit: "C/s"},
	{code: "V", print: "V", metric: true, value: "1", unit: "J/C"},
	{code: "F", print: "F", metric: true, value: "1", unit: "C/V"},
	{code: "Ohm", print: "Ω", metric: true, value: "1", unit: "V/A"},
	{code: "S", print: "S", metric: true, value: "1", unit: "Ohm-1"},
	{code: "Wb", print: "Wb", metric: true, value: "1", unit: "V.s"},
	{code: "T", print: "T", metric: true, value: "1", unit: "Wb/m2"},
	{code: "H", print: "H", metric: true, value: "1", unit: "Wb/A"},
	{code: "lm", print: "lm", metric: true, value: "1", unit: "cd.sr"},
	{code: "lx", print: "lx", metric: true, value: "1", unit: "lm/m2"},
	{code: "Bq", print: "Bq", metric: true, value: "1", unit: "s-1"},
	{code: "Gy", print: "Gy", metric: true, value: "1", unit: "J/kg"},
	{code: "Sv", print: "Sv", metric: true, value: "1", unit: "J/kg"},
	{code: "Cel", print: "°C", metric: true, class: atomAffine, value: "1", unit: "K", offset: "273.15"},

	// ISO 1000 units.
	{code: "deg", print: "°", value: "2", unit: "[pi].rad/360"},
	{code: "'", print: "′", value: "1", unit: "deg/60"},
	{code: "''", print: "″", value: "1", unit: "'/60"},
	{code: "l", print: "l", metric: true, value: "1", unit: "dm3"},
	{code: "L", print: "L", metric: true, value: "1", unit: "l"},
	{code: "ar", print: "a", metric: true, value: "100", unit: "m2"},
	{code: "min", print: "min", value: "60", unit: "s"},
	{code: "h", print: "h", value: "60", unit: "min"},
	{code: "d", print: "d", value: "24", unit: "h"},
	{code: "a_j", print: "a", value: "365.25", unit: "d"},
	{code: "a", print: "a", value: "1", unit: "a_j"},
	{code: "wk", print: "wk", value: "7", unit: "d"},
	{code: "mo_j", print: "mo", value: "1", unit: "a_j/12"},
	{code: "mo", print: "mo", value: "1", unit: "mo_j"},
	{code: "t", print: "t", metric: true, value: "1e3", unit: "kg"},
	{code: "bar", print: "bar", metric: true, value: "1e5", unit: "Pa"},
	{
		code: "eV", print: "eV", metric: true, value: "1.602176634e-19", unit: "J",
		note: "UCUM writes [e].V; the elementary charge is exact since 2019, so the value is the same",
	},
	{code: "[g]", print: "gn", metric: true, value: "980665e-5", unit: "m/s2"},
	{code: "atm", print: "atm", value: "101325", unit: "Pa"},
	{code: "[lbf_av]", print: "lbf", value: "1", unit: "[lb_av].[g]"},

	// International customary lengths, areas and volumes.
	{code: "[in_i]", print: "in", value: "254e-2", unit: "cm"},
	{code: "[ft_i]", print: "ft", value: "12", unit: "[in_i]"},
	{code: "[yd_i]", print: "yd", value: "3", unit: "[ft_i]"},
	{code: "[mi_i]", print: "mi", value: "5280", unit: "[ft_i]"},
	{code: "[nmi_i]", print: "nmi", value: "1852", unit: "m"},
	{code: "[kn_i]", print: "kn", value: "1", unit: "[nmi_i]/h"},
	{code: "[sft_i]", print: "ft²", value: "1", unit: "[ft_i]2"},
	{code: "[cft_i]", print: "ft³", value: "1", unit: "[ft_i]3"},

	// US and British volumes, metric cooking measures.
	{code: "[gal_us]", print: "gal", value: "231", unit: "[in_i]3"},
	{code: "[qt_us]", print: "qt", value: "1", unit: "[gal_us]/4"},
	{code: "[pt_us]", print: "pt", value: "1", unit: "[qt_us]/2"},
	{code: "[gil_us]", print: "gi", value: "1", unit: "[pt_us]/4"},
	{code: "[foz_us]", print: "fl oz", value: "1", unit: "[gil_us]/4"},
	{code: "[tbs_us]", print: "tbsp", value: "1", unit: "[foz_us]/2"},
	{code: "[tsp_us]", print: "tsp", value: "1", unit: "[tbs_us]/3"},
	{code: "[cup_us]", print: "cup", value: "16", unit: "[tbs_us]"},
	{code: "[cup_m]", print: "cup", value: "240", unit: "mL"},
	{code: "[tsp_m]", print: "tsp", value: "5", unit: "mL"},
	{code: "[tbs_m]", print: "tbsp", value: "15", unit: "mL"},
	{code: "[gal_br]", print: "gal", value: "4.54609", unit: "l"},
	{code: "[qt_br]", print: "qt", value: "1", unit: "[gal_br]/4"},
	{code: "[pt_br]", print: "pt", value: "1", unit: "[qt_br]/2"},
	{code: "[gil_br]", print: "gi", value: "1", unit: "[pt_br]/4"},
	{code: "[foz_br]", print: "fl oz", value: "1", unit: "[gil_br]/5"},

	// Avoirdupois masses.
	{code: "[gr]", print: "gr", value: "64.79891", unit: "mg"},
	{code: "[lb_av]", print: "lb", value: "7000", unit: "[gr]"},
	{code: "[oz_av]", print: "oz", value: "1", unit: "[lb_av]/16"},
	{code: "[stone_av]", print: "st", value: "14", unit: "[lb_av]"},

	// Heat.
	{code: "[degF]", print: "°F", class: atomAffine, value: "5", unit: "K/9", offset: "45967/180"},
	{code: "cal_th", print: "cal", metric: true, value: "4.184", unit: "J"},
	{code: "cal", print: "cal", metric: true, value: "1", unit: "cal_th"},
	{code: "[Cal]", print: "Cal", value: "1", unit: "kcal_th"},
	{code: "[Btu_IT]", print: "Btu", value: "1.05505585262", unit: "kJ"},
	{code: "[HP]", print: "hp", value: "550", unit: "[ft_i].[lbf_av]/s"},

	// Clinical.
	{code: "m[H2O]", print: "mH₂O", metric: true, value: "980665e-5", unit: "kPa"},
	{code: "m[Hg]", print: "mHg", metric: true, value: "133.3220", unit: "kPa"},
	{code: "[in_i'Hg]", print: "inHg", value: "1", unit: "m[Hg].[in_i]/m"},
	{code: "[MET]", print: "MET", value: "3.5", unit: "mL/min/kg"},
	{code: "[pH]", print: "pH", class: atomLevel},

	// Chemical.
	{code: "kat", print: "kat", metric: true, value: "1", unit: "mol/s"},
	{code: "U", print: "U", metric: true, value: "1", unit: "umol/min"},
	{code: "[iU]", print: "IU", metric: true, class: atomArbitrary},
	{
		code: "[IU]", print: "IU", metric: true, class: atomArbitrary,
		note: "UCUM defines [IU] as 1 [iU]; the substrate keeps the two spellings apart, as two arbitrary atoms",
	},
	{code: "[arb'U]", print: "arb. U", class: atomArbitrary},

	// Levels.
	{code: "B", print: "B", metric: true, class: atomLevel},
	{code: "B[SPL]", print: "B SPL", metric: true, class: atomLevel},
	{code: "B[W]", print: "BW", metric: true, class: atomLevel},

	// Miscellaneous.
	{code: "[psi]", print: "psi", value: "1", unit: "[lbf_av]/[in_i]2"},
	{code: "Ci", print: "Ci", metric: true, value: "37e9", unit: "Bq"},
	{code: "By", print: "B", metric: true, value: "8", unit: "bit"},
}

// localUnits are codes the substrate defines because UCUM has no spelling for
// them. Each is used exactly as written: no prefix, no exponent, no compound.
var localUnits = map[string]atomDef{
	"dBm": {
		code: "dBm", print: "dBm", class: atomLevel,
		note: "10 lg(P/1 mW) dB, no UCUM spelling: a power level against one milliwatt, which every radio reports",
	},
}

// Scale says how a measure's values convert and combine.
type Scale string

const (
	// ScaleRatio converts by a factor: mass, length, energy.
	ScaleRatio Scale = "ratio"
	// ScaleAffine converts by a factor and an offset: an absolute
	// temperature.
	ScaleAffine Scale = "affine"
	// ScaleDifference converts by the factor alone: a temperature
	// difference, where 0.3 Cel is 0.54 [degF].
	ScaleDifference Scale = "difference"
	// ScaleLogarithmic is a level: it converts to nothing but itself.
	ScaleLogarithmic Scale = "logarithmic"
	// ScaleArbitrary is a unit an assay defines: it converts to nothing but
	// the same atom under another metric prefix.
	ScaleArbitrary Scale = "arbitrary"
	// ScaleCount is a count of things, named by an annotation ({steps}); two
	// annotations never convert into each other.
	ScaleCount Scale = "count"
)

// measureDef is one measure as the table ships it.
type measureDef struct {
	name    string
	base    string
	scale   Scale
	display string
	units   []string
	// annotation is the one annotation every unit of the measure carries:
	// `reactive` for reactive power, which UCUM can only spell as an annotated
	// V.A.
	annotation string
	note       string
}

// measureDefs is the measure list, in the order the docs and the console list
// it. The first measure that lists a code is not its home: primaryMeasure says
// where a code that two measures list reads when a property declares neither.
var measureDefs = []measureDef{
	{
		name: "mass", base: "kg", scale: ScaleRatio, display: "kg",
		units: []string{"kg", "g", "mg", "ug", "ng", "t", "[lb_av]", "[oz_av]", "[stone_av]"},
	},
	{
		name: "length", base: "m", scale: ScaleRatio, display: "m",
		units: []string{"m", "km", "cm", "mm", "nm", "[in_i]", "[ft_i]", "[yd_i]", "[mi_i]", "[nmi_i]"},
	},
	{
		name: "area", base: "m2", scale: ScaleRatio, display: "m2",
		units: []string{"m2", "cm2", "har", "[sft_i]"},
	},
	{
		name: "volume", base: "m3", scale: ScaleRatio, display: "L",
		units: []string{
			"m3", "L", "dL", "mL", "[gal_us]", "[qt_us]", "[pt_us]", "[cup_us]", "[foz_us]", "[tbs_us]",
			"[tsp_us]", "[gal_br]", "[foz_br]", "[cup_m]", "[tbs_m]", "[tsp_m]", "[cft_i]",
		},
	},
	{
		name: "duration", base: "s", scale: ScaleRatio, display: "min",
		units: []string{"s", "ms", "us", "min", "h", "d", "wk", "mo", "a"},
		note:  "mo and a are UCUM's mean Julian month (30.4375 d) and year (365.25 d), never a calendar month or year",
	},
	{
		name: "temperature", base: "K", scale: ScaleAffine, display: "Cel",
		units: []string{"K", "Cel", "[degF]"},
		note:  "an absolute temperature: Cel and [degF] convert with their offsets",
	},
	{
		name: "temperature-difference", base: "K", scale: ScaleDifference, display: "K",
		units: []string{"K", "Cel", "[degF]"},
		note:  "a difference or deviation: the same codes, the offsets suppressed",
	},
	{
		name: "energy", base: "J", scale: ScaleRatio, display: "kJ",
		units: []string{"J", "kJ", "MJ", "cal", "kcal", "[Cal]", "W.h", "kW.h", "eV", "[Btu_IT]"},
		note:  "cal is the thermochemical calorie (4.184 J); [Cal] is the nutrition Calorie, a kcal",
	},
	{
		name: "power", base: "W", scale: ScaleRatio, display: "W",
		units: []string{"W", "mW", "kW", "MW", "[HP]", "kcal/d", "kcal/h", "[Btu_IT]/h"},
	},
	{
		name: "apparent-power", base: "V.A", scale: ScaleRatio, display: "V.A",
		units: []string{"V.A"},
		note:  "volt-amperes: the same factor as the watt and never converted into it",
	},
	{
		name: "reactive-power", base: "V.A{reactive}", scale: ScaleRatio, display: "V.A{reactive}",
		units: []string{"V.A{reactive}"}, annotation: "reactive",
		note: "var: UCUM has no code, so it is the annotated volt-ampere",
	},
	{
		name: "pressure", base: "Pa", scale: ScaleRatio, display: "hPa",
		units: []string{"Pa", "hPa", "kPa", "bar", "mbar", "atm", "mm[Hg]", "cm[H2O]", "[psi]", "[in_i'Hg]"},
	},
	{
		name: "speed", base: "m/s", scale: ScaleRatio, display: "km/h",
		units: []string{"m/s", "km/h", "[mi_i]/h", "[kn_i]", "[ft_i]/s"},
	},
	{
		name: "pace", base: "s/m", scale: ScaleRatio, display: "min/km",
		units: []string{"s/m", "min/km", "s/km", "min/[mi_i]", "s/(100.m)", "s/(100.[yd_i])", "s/(500.m)"},
		note:  "the reciprocal of a speed: a mean pace is total time over total distance",
	},
	{
		name: "acceleration", base: "m/s2", scale: ScaleRatio, display: "m/s2",
		units: []string{"m/s2", "[g]"},
	},
	{
		name: "frequency", base: "Hz", scale: ScaleRatio, display: "Hz",
		units: []string{"Hz", "kHz"},
	},
	{
		name: "rate", base: "/s", scale: ScaleRatio, display: "/min",
		units: []string{"/s", "/min", "/h", "/d", "/wk", "{count}/min"},
		note:  "events per time; an annotated rate ({beats}/min) converts only to the same annotation",
	},
	{
		name: "count", base: "1", scale: ScaleCount, display: "{count}",
		units: []string{"{count}", "1"},
		note:  "a count of things, named by an annotation alone ({steps}); two annotations never convert",
	},
	{
		name: "fraction", base: "1", scale: ScaleRatio, display: "%",
		units: []string{"1", "%", "[ppth]", "[ppm]", "[ppb]", "mmol/mol"},
		note:  "a part of a whole; mmol/mol (HbA1c IFCC) never converts to an NGSP %, which a metric says",
	},
	{
		name: "amount-of-substance", base: "mol", scale: ScaleRatio, display: "mmol",
		units: []string{"mol", "mmol", "umol"},
	},
	{
		name: "amount-concentration", base: "mol/m3", scale: ScaleRatio, display: "mmol/L",
		units: []string{"mol/L", "mmol/L", "umol/L", "nmol/L", "pmol/L"},
	},
	{
		name: "mass-concentration", base: "kg/m3", scale: ScaleRatio, display: "mg/dL",
		units: []string{"g/L", "g/dL", "mg/dL", "mg/L", "ug/L", "ng/mL", "pg/mL", "ug/dL", "g/m3", "mg/m3", "ug/m3", "kg/m3"},
	},
	{
		name: "catalytic-concentration", base: "mol/(s.m3)", scale: ScaleRatio, display: "U/L",
		units: []string{"U/L"},
		note:  "enzyme activity per volume: U is 1 umol/min, a proper unit",
	},
	{
		name: "catalytic-activity", base: "mol/s", scale: ScaleRatio, display: "U",
		units: []string{"kat", "U"},
	},
	{
		name: "arbitrary", base: "", scale: ScaleArbitrary, display: "[IU]",
		units: []string{"[IU]", "[IU]/L", "m[IU]/L", "[IU]/mL", "[arb'U]"},
		note:  "defined by an assay: converts only to the same atom and annotation under another metric prefix",
	},
	{name: "illuminance", base: "lx", scale: ScaleRatio, display: "lx", units: []string{"lx"}},
	{name: "luminous-flux", base: "lm", scale: ScaleRatio, display: "lm", units: []string{"lm"}},
	{name: "luminous-intensity", base: "cd", scale: ScaleRatio, display: "cd", units: []string{"cd"}},
	{name: "luminance", base: "cd/m2", scale: ScaleRatio, display: "cd/m2", units: []string{"cd/m2"}},
	{name: "irradiance", base: "W/m2", scale: ScaleRatio, display: "W/m2", units: []string{"W/m2"}},
	{
		name: "level", base: "dB", scale: ScaleLogarithmic, display: "dB", units: []string{"dB"},
		note: "a ratio in decibels against no stated reference: never converted, never summed",
	},
	{
		name: "sound-pressure-level", base: "dB[SPL]", scale: ScaleLogarithmic, display: "dB[SPL]",
		units: []string{"dB[SPL]"}, note: "20 lg(p/20 uPa) dB",
	},
	{
		name: "power-level", base: "dB[W]", scale: ScaleLogarithmic, display: "dBm",
		units: []string{"dB[W]", "dBm"},
		note:  "dB[W] is 10 lg(P/1 W) dB; dBm is 10 lg(P/1 mW) dB, a substrate code because UCUM has none",
	},
	{name: "acidity", base: "[pH]", scale: ScaleLogarithmic, display: "[pH]", units: []string{"[pH]"}},
	{name: "electric-potential", base: "V", scale: ScaleRatio, display: "V", units: []string{"V", "mV"}},
	{name: "electric-current", base: "A", scale: ScaleRatio, display: "A", units: []string{"A", "mA"}},
	{name: "electric-resistance", base: "Ohm", scale: ScaleRatio, display: "Ohm", units: []string{"Ohm", "kOhm"}},
	{name: "electric-charge", base: "C", scale: ScaleRatio, display: "mA.h", units: []string{"C", "A.h", "mA.h"}},
	{name: "electric-conductivity", base: "S/m", scale: ScaleRatio, display: "uS/cm", units: []string{"uS/cm"}},
	{
		name: "information", base: "bit", scale: ScaleRatio, display: "By",
		units: []string{"bit", "By", "kBy", "MBy", "GBy", "KiBy", "MiBy", "GiBy"},
	},
	{
		name: "data-rate", base: "bit/s", scale: ScaleRatio, display: "Mbit/s",
		units: []string{"bit/s", "kbit/s", "Mbit/s", "By/s"},
	},
	{
		name: "angle", base: "deg", scale: ScaleRatio, display: "deg",
		units: []string{"deg", "'", "''", "rad"},
		note:  "rad is 180/pi deg, which no rational number states, so rad converts to nothing but itself",
	},
	{
		name: "volume-flow", base: "m3/s", scale: ScaleRatio, display: "L/min",
		units: []string{"L/min", "L/h", "mL/min", "m3/h", "[gal_us]/min"},
	},
	{name: "mass-flow", base: "kg/s", scale: ScaleRatio, display: "g/s", units: []string{"g/s"}},
	{name: "force", base: "N", scale: ScaleRatio, display: "N", units: []string{"N", "[lbf_av]"}},
	{
		name: "torque", base: "N.m", scale: ScaleRatio, display: "N.m", units: []string{"N.m", "[lbf_av].[ft_i]"},
		note: "the same dimension as energy, and never converted into it",
	},
	{
		name: "activity-concentration", base: "Bq/m3", scale: ScaleRatio, display: "Bq/m3",
		units: []string{"Bq/m3", "pCi/L"},
	},
	{name: "dose-equivalent-rate", base: "Sv/s", scale: ScaleRatio, display: "uSv/h", units: []string{"uSv/h"}},
	{name: "body-mass-index", base: "kg/m2", scale: ScaleRatio, display: "kg/m2", units: []string{"kg/m2"}},
	{
		name: "oxygen-uptake-rate", base: "m3/(kg.s)", scale: ScaleRatio, display: "mL/(kg.min)",
		units: []string{"mL/(kg.min)", "[MET]"}, note: "one MET is 3.5 mL/(kg.min)",
	},
	{name: "specific-power", base: "W/kg", scale: ScaleRatio, display: "W/kg", units: []string{"W/kg"}},
	{name: "specific-energy", base: "J/kg", scale: ScaleRatio, display: "kcal/kg", units: []string{"kcal/kg"}},
	{
		name: "energy-per-distance", base: "J/m", scale: ScaleRatio, display: "kW.h/(100.km)",
		units: []string{"kW.h/(100.km)", "W.h/km"},
	},
	{
		name: "fuel-consumption", base: "m3/m", scale: ScaleRatio, display: "L/(100.km)",
		units: []string{"L/(100.km)"}, note: "the reciprocal of a fuel economy, never converted into one",
	},
	{
		name: "fuel-economy", base: "m/m3", scale: ScaleRatio, display: "km/L",
		units: []string{"[mi_i]/[gal_us]", "km/L"},
	},
	{
		name: "precipitation-rate", base: "m/s", scale: ScaleRatio, display: "mm/h",
		units: []string{"mm/h", "[in_i]/h"},
	},
}

// primaryMeasure is where a code two measures list reads when the property
// names neither: a thermometer reports a temperature, not a deviation, and a
// bare 1 is a fraction.
var primaryMeasure = map[string]string{
	"K": "temperature", "Cel": "temperature", "[degF]": "temperature",
	"1": "fraction",
}

// unitAliases are spellings people and vendors write for a code. They are read
// on input and never stored: the stored unit is always the code. None of them
// is a valid code itself (TestUnitAliasesAreNotCodes).
var unitAliases = map[string]string{
	"°C": "Cel", "℃": "Cel", "degC": "Cel",
	"°F": "[degF]", "℉": "[degF]", "degF": "[degF]",
	"mcg": "ug", "mmHg": "mm[Hg]", "inHg": "[in_i'Hg]",
	"bpm": "{beats}/min", "rpm": "{rev}/min", "count/min": "{count}/min",
	"kWh": "kW.h", "Wh": "W.h", "Cal": "[Cal]", "ppm": "[ppm]", "ppb": "[ppb]",
	"VA": "V.A", "var": "V.A{reactive}",
}

// unitCharacters are the characters an alias lookup rewrites before it tries
// again: both micro signs, which look alike and are two code points, and the
// superscript digits.
var unitCharacters = map[rune]string{
	'µ': "u", // MICRO SIGN
	'μ': "u", // GREEK SMALL LETTER MU
	'²': "2",
	'³': "3",
}

// unitPrints are the print symbols of table codes whose atom-by-atom rendering
// is not what a reader expects to see.
var unitPrints = map[string]string{
	"kW.h": "kWh", "W.h": "Wh", "V.A": "VA", "V.A{reactive}": "var",
	"mm[Hg]": "mmHg", "cm[H2O]": "cmH₂O", "har": "ha",
	"[mi_i]/h": "mph", "[mi_i]/[gal_us]": "mpg",
	"kW.h/(100.km)": "kWh/100 km", "L/(100.km)": "L/100 km",
	"s/(100.m)": "s/100 m", "s/(100.[yd_i])": "s/100 yd", "s/(500.m)": "s/500 m",
	"[gal_br]": "gal (UK)", "[foz_br]": "fl oz (UK)",
	"[cup_m]": "cup (metric)", "[tbs_m]": "tbsp (metric)", "[tsp_m]": "tsp (metric)",
	"dB[SPL]": "dB SPL", "dB[W]": "dBW", "A.h": "Ah", "mA.h": "mAh", "W.h/km": "Wh/km", "1": "",
}
