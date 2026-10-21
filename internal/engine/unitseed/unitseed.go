// Package unitseed holds the unit and conversion records a repository is
// seeded with (decision 0186). Both files are written by
// TestUnitSeedMatchesTheTable in internal/vocabulary from the engine's unit
// table, and never edited by hand.
package unitseed

import _ "embed"

var (
	//go:embed units.yaml
	units []byte
	//go:embed conversions.yaml
	conversions []byte
)

// Units is units.yaml, one record envelope per document.
func Units() []byte { return append([]byte(nil), units...) }

// Conversions is conversions.yaml, one record envelope per document.
func Conversions() []byte { return append([]byte(nil), conversions...) }
