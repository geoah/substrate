package engine

// Conversion step 7: the dialect that may hold an erasure's intent, the
// `erase` effect (erase.go, decision 0304). It rewrites no line: no history
// written before it holds one. It raises the stamp, so a binary that does
// not know the effect refuses the repository at its open, by its dialect,
// rather than replaying a line it would misread.

import "github.com/geoah/substrate/internal/changelogfile"

var conversionErasures = conversionStep{
	Dialect: 7,
	Name:    "erasures",
	Line:    func(*changelogfile.Entry) error { return nil },
}
