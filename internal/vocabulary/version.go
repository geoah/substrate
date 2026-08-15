package vocabulary

import "cmp"

// The declaration version is an incremental integer: a first declaration is
// version 1, and every change moves it up by at least one. Zero is never a
// version — it is the ABSENT value, ordering below everything, so a stored
// row written before versions were mandatory upgrades on the next open
// rather than sticking. (The Kubernetes-style strings this replaced —
// `v1alpha3` and kin — were migrated to their trailing number.)
//
// Nobody has to bump by hand through the API: a declaration applied without
// a version past the stored one lands at stored+1 when its definition
// changed and keeps the stored version when it did not
// (engine/vocabularywrite.go). The shipped tree under kinds/ still pins
// versions explicitly, because the boot upgrade needs one total order across
// binaries — that is the "explicit version" door, and `mise run kinds:check`
// holds it.

// DefaultVersion is a declaration's version when nothing declares one: the
// first version there is.
const DefaultVersion int64 = 1

// CompareVersions orders two declaration versions. It is THE ordering: the
// boot-time upgrade (engine seed.go), the upgrade preview and the tree
// checker (cmd/vocabularydiff) all diff versions through it, so "newer"
// means one thing everywhere. Zero is the absent version and orders below
// every real one.
//
// It returns -1, 0 or 1 for a < b, a == b, a > b.
func CompareVersions(a, b int64) int {
	return cmp.Compare(a, b)
}

// VersionValue reads a declaration version out of a decoded document value:
// YAML hands an int, JSON a float64, and the engine's own stamps are int64.
// Anything else — a string above all, which is where the retired `v1alpha3`
// spelling would arrive — is not a version, and reports false.
func VersionValue(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n != float64(int64(n)) {
			return 0, false
		}
		return int64(n), true
	default:
		return 0, false
	}
}
