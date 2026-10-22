package substrate

// UnitTable is GET /api/v1/units: the repository's unit graph (decision
// 0186) at Revision, the counter every write of a unit or conversion record
// moves, so a client caches the graph by it.
type UnitTable struct {
	Units       []RepositoryUnit       `json:"units"`
	Conversions []RepositoryConversion `json:"conversions"`
	Revision    int64                  `json:"revision"`
	// Unseeded are the shipped records the repository's last seed skipped,
	// each with why.
	Unseeded []UnseededRecord `json:"unseeded"`
}

// UnitConversion is GET /api/v1/units/convert: one value converted through
// the repository's graph.
type UnitConversion struct {
	// Value is every digit where the result has a finite decimal expansion,
	// else 20 significant digits.
	Value string `json:"value"`
	// Unit is the id of the unit converted to: a code or an alias the
	// request named is answered as the id.
	Unit string `json:"unit"`
	// Exact is false where Value was rounded or an expression conversion
	// computed it.
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
