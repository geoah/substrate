package vocabulary

import (
	"fmt"
)

// THE VOCABULARY DIALECT is the number of the declaration format this loader
// reads: the closed key sets of every declaration document and their meaning
// (decision record 0268, which amends 0020). A key added to any of them is a
// new dialect, so the number goes up with it, and
// testdata/dialect.golden.json records the key sets under the number they
// were admitted at: TestDialectKeysMatchTheGolden refuses a key set that
// moved under an unchanged number. The server publishes the number in
// discovery, a repository is stamped with it at open (engine/dialect.go),
// and a package states the dialect it needs in its header's `dialect:`, and
// each version in its publisher's document, so a server picks the newest
// version it can read instead of failing on the newest one listed.
//
// 1 and 2 are the dialects before the number was held to the keys; 3 adds
// a package header's `stability`, `interface` and `dialect`.
const Dialect = 3

// parsePackageDialect reads the header's `dialect:`, the declaration format
// the package needs. An absent key stores nothing and needs nothing beyond
// what this loader reads. A number above Dialect is refused naming both, so
// the author learns which server the package needs rather than meeting an
// unknown key further down.
func (l *loader) parsePackageDialect(where string, d map[string]any, g *Package) {
	raw, present := d["dialect"]
	if !present {
		return
	}
	v, ok := VersionValue(raw)
	if !ok || v < 1 {
		l.errf("%s: data.dialect: must be a whole number of at least 1, the vocabulary dialect the package needs, not %v — or drop the key", where, raw)
		return
	}
	if v > Dialect {
		l.errf("%s: data.dialect: %s", where, DialectRefusal(v))
		return
	}
	g.Dialect = v
}

// DialectRefusal is why a package that needs the given dialect is refused
// here, in the words every door uses.
func DialectRefusal(needs int64) string {
	return fmt.Sprintf("the package needs vocabulary dialect %d, and this server reads dialect %d and below — run a newer server, or install a version of the package written for dialect %d",
		needs, Dialect, Dialect)
}
