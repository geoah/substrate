package vocabulary

import (
	"strconv"
	"strings"
)

// CompareVersions orders two declaration versions the way Kubernetes orders
// API versions, because that is the shape the manifests use (`v1alpha1`,
// `v1beta2`, `v1`, `v2`): a GA version outranks any pre-release of the same
// major, beta outranks alpha, and the trailing number breaks the tie.
// Anything unparseable falls back to a plain string comparison, so two
// spellings of the same convention still order deterministically and an
// unfamiliar one never silently wins.
//
// It is THE ordering: the boot-time upgrade (engine seed.go), the upgrade
// preview and the tree checker (cmd/vocabularydiff) all diff versions through
// it, so "newer" means one thing everywhere.
//
// It returns -1, 0 or 1 for a < b, a == b, a > b.
func CompareVersions(a, b string) int {
	if a == b {
		return 0
	}
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return strings.Compare(a, b)
	}
	if av.major != bv.major {
		return cmpInt(av.major, bv.major)
	}
	if av.stage != bv.stage {
		return cmpInt(av.stage, bv.stage)
	}
	return cmpInt(av.minor, bv.minor)
}

// declarationVersion is a parsed `v<major>[alpha|beta<minor>]`.
type declarationVersion struct {
	major int
	// stage orders the maturity: alpha 0, beta 1, GA 2.
	stage int
	minor int
}

func parseVersion(s string) (declarationVersion, bool) {
	var v declarationVersion
	rest, ok := strings.CutPrefix(s, "v")
	if !ok || rest == "" {
		return v, false
	}
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return v, false
	}
	major, err := strconv.Atoi(rest[:digits])
	if err != nil {
		return v, false
	}
	v.major = major
	tail := rest[digits:]
	switch {
	case tail == "":
		v.stage = 2 // GA
		return v, true
	case strings.HasPrefix(tail, "alpha"):
		v.stage = 0
		tail = strings.TrimPrefix(tail, "alpha")
	case strings.HasPrefix(tail, "beta"):
		v.stage = 1
		tail = strings.TrimPrefix(tail, "beta")
	default:
		return v, false
	}
	if tail == "" {
		return v, true
	}
	minor, err := strconv.Atoi(tail)
	if err != nil {
		return v, false
	}
	v.minor = minor
	return v, true
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
