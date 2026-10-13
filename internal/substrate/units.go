package substrate

// UnitTable is GET /api/v1/units: the engine's unit table, every measure with
// the codes it lists, each code's print symbol and its exact factor to the
// measure's base, and the input aliases a write reads. A code the grammar
// reads and no measure lists (`GJ`, `kg/h`) is not here; the conversion route
// converts it.
type UnitTable struct {
	Measures []UnitMeasure `json:"measures"`
	// Aliases are the spellings a write reads (`°C`, `bpm`) and the code
	// each is stored as.
	Aliases map[string]string `json:"aliases"`
}

// UnitMeasure is one measure: the named kind of quantity a value converts
// and compares inside.
type UnitMeasure struct {
	Name string `json:"name"`
	// Base is the code every factor converts to; empty for `arbitrary`,
	// which has none.
	Base string `json:"base"`
	// Scale is how the measure's values convert: ratio, affine, difference,
	// logarithmic, arbitrary or count.
	Scale string `json:"scale"`
	// Display is the code a client shows a value in when nothing else says.
	Display string `json:"display"`
	// Dimension is over the nine base quantities (L M T I Θ N J A B), "1"
	// for a dimensionless measure, empty for a level or an arbitrary unit.
	Dimension string     `json:"dimension"`
	Note      string     `json:"note,omitempty"`
	Units     []UnitCode `json:"units"`
}

// UnitCode is one code a measure lists.
type UnitCode struct {
	Code string `json:"code"`
	// Print is the symbol a title shows (`°C` for Cel), empty for `1`.
	Print string `json:"print"`
	// Factor is the exact rational from the code to the measure's base
	// (`45359237/100000000` for [lb_av]): base = value x factor + offset.
	// Absent where no rational number states it: a level, an arbitrary unit,
	// rad against deg.
	Factor string `json:"factor,omitempty"`
	// Offset is an absolute temperature's zero in the base (`5463/20` for
	// Cel), absent where it is zero.
	Offset string `json:"offset,omitempty"`
}

// UnitConversion is GET /api/v1/units/convert: one value converted exactly.
type UnitConversion struct {
	// Value is every digit where the result has a finite decimal expansion,
	// else 20 significant digits.
	Value string `json:"value"`
	// Unit is the code converted to: an alias the request named is answered
	// as its code.
	Unit string `json:"unit"`
	// Measure is the measure the value converted in.
	Measure string `json:"measure"`
	// Exact is false where Value was rounded.
	Exact bool `json:"exact"`
}
