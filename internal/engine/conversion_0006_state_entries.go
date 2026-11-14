package engine

// Conversion step 6: the dialect that may hold state entries (compact.go,
// decision 0296). It rewrites no line: no history written before it holds a
// `state` or a `ledger` effect, so every line is already in the new dialect.
// It raises the stamp, so a binary that does not know the two effects
// refuses the repository at its open, by its dialect, rather than at the
// first rebuild of a compacted history.

import "github.com/geoah/substrate/internal/changelogfile"

var conversionStateEntries = conversionStep{
	Dialect: 6,
	Name:    "state_entries",
	Line:    func(*changelogfile.Entry) error { return nil },
}
