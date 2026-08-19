package vocabulary

import "regexp"

// Three naming rules are kept as regular-expression SOURCE, because one of them
// has to leave the process: the guard that counts a keyed map's key-contract
// violations asks Postgres exactly the question CheckKey asks here
// (engine/schemadiff.go), and a second spelling of a grammar is a second
// grammar. KeyPatternRegexp is the seam and TestKeyPatternRegexpAgreesWithCheckKey
// pins the two sides together. The subset used — literals, classes, groups, ?,
// *, +, anchors — means the same thing to RE2 and to Postgres.
const (
	authorityRE = `[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+`
	wordRE      = `[a-z][a-z0-9]*`
	camelRE     = `[a-z][a-zA-Z0-9]*`
	// kindRefRE is the kind REFERENCE grammar (ref.go): a bare local name, or an
	// authority and a name split by the one slash.
	kindRefRE = `(` + authorityRE + `/)?` + wordRE
)

// Naming rules enforced at load (proposal §3, the contract).
// Identifiers are storage keys and URL segments; readability lives in display
// templates.
var (
	reAuthority = regexp.MustCompile("^" + authorityRE + "$")
	reWord      = regexp.MustCompile("^" + wordRE + "$")
	reCamel     = regexp.MustCompile("^" + camelRE + "$")
	// An actor is one of the closed domain's names: a bare word (`console`,
	// `substratectl`, `api`, `substrate`) or a prefixed machine hand carrying
	// the full authority — `bundle:<authority>`,
	// `function:<authority>:<name>`, `agent:<authority>:<name>` (record 0025).
	// The first segment after the prefix admits dots so an authority fits; the
	// optional second is the callable's local name. The retired
	// `connector:<label>` spelling still matches, because a repository written
	// before 0025 has actor declarations carrying it and they must keep
	// loading.
	reActor = regexp.MustCompile(`^` + wordRE + `(:[a-z][a-z0-9.-]*)?(:` + camelRE + `)?$`)
	// A namespaced label/annotation key is "<actor>/<name>", and an actor may
	// carry the domain's colons and an authority's dots
	// (`function:web.bundles.example.com:harvest/synced`).
	reMetaKey = regexp.MustCompile(`^[a-z][a-z0-9_.*:-]*/[a-z][a-z0-9_.-]*$`)
	// reID is the record-id alphabet. Minted ids are 12 lowercase base32
	// characters; a writer's own id is its provider key ENCODED into this set,
	// which is RFC 3986 unreserved (ALPHA / DIGIT / "-" / "." / "_" / "~")
	// plus the two extra pchars a path segment admits, ":" and "@", plus "/".
	//
	// The "/" is there for exactly one reason: a DECLARATION's id is a KIND
	// REFERENCE ("tasks.substrate.reamde.dev/task"), and the grammar has one
	// spelling. A "/" is legal in a URI path segment only when percent-
	// encoded, so a client writes `%2F` and the API decodes it once
	// (api/rest.go pathParam). No "%" in the alphabet, so nothing on the wire
	// is percent-decoded twice.
	reID     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~:@/-]*$`)
	reRepoNm = regexp.MustCompile(`^[a-z][a-z0-9]{1,29}$`)
)

// MaxIDLen caps a writer-supplied id: long enough for any encoded provider
// key, short enough that ids stay index keys rather than payloads.
const MaxIDLen = 128

// ValidAuthority reports whether s is a legal DNS-style authority name.
func ValidAuthority(s string) bool { return reAuthority.MatchString(s) }

// ValidName reports whether s is a legal kind name.
func ValidName(s string) bool { return reWord.MatchString(s) }

// ValidCamel reports whether s is a legal declared name — property, edge or
// stamp. One rule: camelCase with initialisms uppercase (`displayName`,
// `endsAt`, `icalUID`).
func ValidCamel(s string) bool { return reCamel.MatchString(s) }

// ValidValue reports whether s is a legal enum or state VALUE. Values are
// data, not names: they stay lowercase words.
func ValidValue(s string) bool { return reWord.MatchString(s) }

// ValidActor reports whether s is a legal actor name (see reActor).
func ValidActor(s string) bool { return reActor.MatchString(s) }

// ValidMetaKey reports whether s is a legal namespaced label/annotation key.
func ValidMetaKey(s string) bool { return reMetaKey.MatchString(s) }

// ValidID reports whether s is a legal record id (see reID).
func ValidID(s string) bool {
	return len(s) <= MaxIDLen && reID.MatchString(s)
}

// ValidRepositoryName reports whether s is a legal repository (user) name.
func ValidRepositoryName(s string) bool { return reRepoNm.MatchString(s) }

// MetaKeyNamespace returns the writer namespace of a label/annotation key.
func MetaKeyNamespace(key string) string {
	for i := range len(key) {
		if key[i] == '/' {
			return key[:i]
		}
	}
	return ""
}
