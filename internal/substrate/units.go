package substrate

// UnitTable is GET /api/v1/units: the repository's unit graph, and beside it
// the engine's unit table, every measure with
// the codes it lists, each code's print symbol and its exact factor to the
// measure's base, and the input aliases a write reads. A code the grammar
// reads and no measure lists (`GJ`, `kg/h`) is not here; the conversion route
// converts it.
type UnitTable struct {
	Measures []UnitMeasure `json:"measures"`
	// Aliases are the spellings a write reads (`°C`, `bpm`) and the code
	// each is stored as.
	Aliases map[string]string `json:"aliases"`
	// Units and Conversions are the repository's unit graph (decision 0186)
	// at Revision, the counter every write of a unit or conversion record
	// moves; a client caches the graph by it. Measures and Aliases stay until
	// every reader converts through the graph.
	Units       []RepositoryUnit       `json:"units"`
	Conversions []RepositoryConversion `json:"conversions"`
	Revision    int64                  `json:"revision"`
	// Unseeded are the shipped records the repository's last seed skipped,
	// each with why.
	Unseeded []UnseededRecord `json:"unseeded"`
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

// RepositoryUnit is one unit record of the repository's graph (decision
// 0186), as GET /api/v1/units answers it.
type RepositoryUnit struct {
	// ID is what a stored quantity carries.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Symbol is what a title prints after a value, empty for a unit that
	// prints nothing (`1`).
	Symbol string `json:"symbol"`
	// Code is the UCUM spelling, for interchange.
	Code    string   `json:"code,omitempty"`
	Aliases []string `json:"aliases"`
	// Scale is `level` or `powerlevel`, absent for a ratio unit.
	Scale string `json:"scale,omitempty"`
	// Turn is one full circle in the unit, absent for a unit that is not
	// circular.
	Turn string `json:"turn,omitempty"`
	// Root is the smallest id of the unit's linear component, and Factor and
	// Offset its exact map to it: root = value x factor + offset. Two units
	// sharing a root convert exactly.
	Root   string `json:"root"`
	Factor string `json:"factor"`
	// Offset is absent where it is zero.
	Offset string `json:"offset,omitempty"`
}

// RepositoryConversion is one conversion record of the repository's graph:
// linear (Factor, and Offset where it writes one) or an expression (Expr and
// Inverse).
type RepositoryConversion struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	To      string `json:"to"`
	Factor  string `json:"factor,omitempty"`
	Offset  string `json:"offset,omitempty"`
	Expr    string `json:"expr,omitempty"`
	Inverse string `json:"inverse,omitempty"`
}

// UnseededRecord is one shipped unit or conversion record the seed did not
// write because it would contradict the repository's own graph: a spelling
// another unit answers, or a cycle that does not compose to the identity.
type UnseededRecord struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// UnitConvert is one value to convert through the repository's graph: exact
// digits and two unit spellings, Delta for a difference, whose conversion
// drops every offset.
type UnitConvert struct {
	Value string
	From  string
	To    string
	Delta bool
}
