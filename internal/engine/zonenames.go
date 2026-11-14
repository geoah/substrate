package engine

import (
	_ "embed"
	"slices"
	"strings"
	"sync"

	// Every name zoneNames holds loads on every host, one with no zone files
	// of its own included: the toolchain's database is the fallback.
	_ "time/tzdata"
)

// zoneNamesText is every time zone name in the IANA database as the
// toolchain embeds it (time/tzdata), its aliases included, one per line in
// byte order. It is the toolchain's lib/time/zoneinfo.zip listed, and
// TestZoneNamesAreTheToolchainsDatabase holds it there; `-update-zones`
// writes it again after a Go upgrade moves the database.
//
//go:embed zonenames.txt
var zoneNamesText string

var zoneNames = sync.OnceValue(func() []string { return strings.Fields(zoneNamesText) })

// zoneNamed reports whether s is a zone name the IANA database holds,
// spelled as it spells it. A host's own zone files are no measure: a
// case-insensitive file system loads `Europe/athens`, and a host's tzdata
// holds files that are not zone names (`posixrules`).
func zoneNamed(s string) bool {
	_, ok := slices.BinarySearch(zoneNames(), s)
	return ok
}
