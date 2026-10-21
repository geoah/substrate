// Package unitseed holds the unit and conversion records a repository is
// seeded with (decision 0186). Both files are the source and are edited by
// hand: seed_test.go holds them to the old table's spellings, separations
// and exact maps (testdata/table_oracle.json), and a change to either rewrites docs/units.md and the
// console's test fixture (see their tests).
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
