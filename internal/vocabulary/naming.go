package vocabulary

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

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
	// kindRefRE is the kind REFERENCE grammar (ref.go): a bare local name, or
	// an authority, a package and a name split by the two slashes.
	kindRefRE = `(` + authorityRE + `/` + wordRE + `/)?` + wordRE
	// The two halves of a label/annotation key, kept apart so the validator
	// and the diagnosis (MetaKeyProblem) read one grammar: a refusal that
	// names a rule the checker does not enforce is worse than a vague one.
	// The namespace carries an actor, so it admits the domain's colons and an
	// authority's dots; the `*` is admitted and matched by nothing — no key
	// is ever read as a glob.
	metaNamespaceRE = `[a-z][a-z0-9_.*:-]*`
	metaNameRE      = `[a-z][a-z0-9_.-]*`
)

// The characters each half of a key admits after its first letter, spelled for
// the refusal message. They are the classes above, read out.
const (
	metaNamespaceChars = `lowercase letters, digits and "_", ".", "-", ":", "*"`
	metaNameChars      = `lowercase letters, digits and "_", ".", "-"`
	// metaKeyShape is the form every refusal repeats, so a reader who has
	// only the message knows what to write.
	metaKeyShape = `a namespaced key ("<actor>/<name>")`
)

// Naming rules enforced at load.
// Identifiers are storage keys and URL segments; readability lives in display
// templates.
var (
	reAuthority = regexp.MustCompile("^" + authorityRE + "$")
	reWord      = regexp.MustCompile("^" + wordRE + "$")
	reCamel     = regexp.MustCompile("^" + camelRE + "$")
	// An actor is one of the closed domain's names: a bare word (`console`,
	// `substratectl`, `api`, `substrate`) or a prefixed machine hand carrying
	// the full authority and the package — `bundle:<authority>:<package>`,
	// `function:<authority>:<package>:<name>`,
	// `agent:<authority>:<package>:<name>` (records 0025 and 0047). The first
	// segment after the prefix admits dots so an authority fits, the second is
	// the package, and the optional third is the callable's local name. The
	// retired `connector:<label>` spelling still matches, because a repository
	// written before 0025 has actor declarations carrying it and they must
	// keep loading.
	reActor = regexp.MustCompile(`^` + wordRE + `(:[a-z][a-z0-9.-]*)?(:` + wordRE + `)?(:` + camelRE + `)?$`)
	// A namespaced label/annotation key is "<actor>/<name>", and an actor may
	// carry the domain's colons and an authority's dots
	// (`function:web.bundles.example.com:harvest/synced`).
	reMetaKey       = regexp.MustCompile(`^` + metaNamespaceRE + `/` + metaNameRE + `$`)
	reMetaNamespace = regexp.MustCompile(`^` + metaNamespaceRE + `$`)
	reMetaName      = regexp.MustCompile(`^` + metaNameRE + `$`)
	// reID is the record-id alphabet. Minted ids are 12 lowercase base32
	// characters; a writer's own id is its provider key ENCODED into this set,
	// which is RFC 3986 unreserved (ALPHA / DIGIT / "-" / "." / "_" / "~")
	// plus the two extra pchars a path segment admits, ":" and "@", plus "/".
	//
	// The "/" is there for exactly one reason: a DECLARATION's id is a KIND
	// REFERENCE ("samples.substrate.reamde.dev/tasks/task"), and the grammar has one
	// spelling. A "/" is legal in a URI path segment only when percent-
	// encoded, so a client writes `%2F` and the API decodes it once
	// (api/rest.go pathParam). No "%" in the alphabet, so nothing on the wire
	// is percent-decoded twice.
	reID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~:@/-]*$`)
)

// MaxIDLen caps a writer-supplied id: long enough for any encoded provider
// key, short enough that ids stay index keys rather than payloads.
const MaxIDLen = 128

// ValidAuthority reports whether s is a legal DNS-style authority name.
func ValidAuthority(s string) bool { return reAuthority.MatchString(s) }

// ValidName reports whether s is a legal kind name.
func ValidName(s string) bool { return reWord.MatchString(s) }

// ValidPackage reports whether s is a legal package name: the kind-name
// grammar, one lowercase word (decision 0047).
func ValidPackage(s string) bool { return reWord.MatchString(s) }

// ValidCamel reports whether s is a legal declared name — a property or a
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

// The DNS limits an authority is held to when a repository claims one as its
// own: 253 characters in all and 63 per label (RFC 1035 §2.3.4). A published
// kind's authority is also held to reAuthority, which has no length rule, so
// the limit lives here, on the one door that mints an authority from user
// input.
const (
	MaxAuthorityLen      = 253
	MaxAuthorityLabelLen = 63
)

// ValidRepositoryAuthority reports whether s may be a repository's own
// authority: the DNS-style authority grammar every kind carries
// (ValidAuthority), within the DNS length limits.
func ValidRepositoryAuthority(s string) bool {
	if len(s) > MaxAuthorityLen || !ValidAuthority(s) {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) > MaxAuthorityLabelLen {
			return false
		}
	}
	return true
}

// RepositoryAuthority resolves the repository name a caller typed into the
// authority that name means. A name carrying a dot IS the authority
// (`ada.example.com`); a bare label is completed under the host the request
// reached (`ada` -> `ada.example.com`), the way a handle sits under its
// server. host is the request's Host header, so a port and a trailing dot are
// stripped and the case folded. The result is NOT validated here: a host that
// is no DNS name (an IPv6 literal) yields a string the caller refuses with
// the same message as any other bad authority, and a bare label under no host
// yields "".
func RepositoryAuthority(name, host string) string {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	if name == "" {
		return ""
	}
	if strings.Contains(name, ".") {
		return name
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return ""
	}
	return name + "." + host
}

// MetaKeyProblem says why key is NOT a legal label/annotation key, naming the
// half and the character at fault, and returns "" for a legal one. The message
// opens with the key quoted, so a caller adds its own context and nothing
// else. It is the only place a key refusal is worded: a writer told that an
// already-namespaced key "must be namespaced" has no way to find the rule
// actually broken (issue 548).
//
// ValidMetaKey is asked first, so a legal key costs one match and the
// diagnosis runs only on the way to a refusal.
func MetaKeyProblem(key string) string {
	if ValidMetaKey(key) {
		return ""
	}
	ns, name, cut := strings.Cut(key, "/")
	switch {
	case !cut:
		return fmt.Sprintf("%q must be %s", key, metaKeyShape)
	case strings.Contains(name, "/"):
		return fmt.Sprintf("%q must be %s: it carries %d slashes, and a key carries one",
			key, metaKeyShape, strings.Count(key, "/"))
	case ns == "":
		return fmt.Sprintf("%q must be %s: the namespace before the slash is empty", key, metaKeyShape)
	case name == "":
		return fmt.Sprintf("%q must be %s: the name after the slash is empty", key, metaKeyShape)
	case !reMetaNamespace.MatchString(ns):
		return fmt.Sprintf("%q: %s", key,
			metaSegmentProblem(ns, "namespace before the slash", metaNamespaceChars, reMetaNamespace))
	case !reMetaName.MatchString(name):
		return fmt.Sprintf("%q: %s", key,
			metaSegmentProblem(name, "name after the slash", metaNameChars, reMetaName))
	}
	// Unreachable while reMetaKey is those two halves and one slash. A vague
	// refusal still beats calling an illegal key legal.
	return fmt.Sprintf("%q must be %s", key, metaKeyShape)
}

// metaSegmentProblem names the rule one half of a key breaks. ok is that
// half's grammar, and it is asked about single characters rather than a second
// set of classes being written out here: a lone rune is the first character,
// and a rune after an "a" is a later one.
func metaSegmentProblem(seg, which, chars string, ok *regexp.Regexp) string {
	// camelCase is the mistake worth a suggestion, because every DECLARED
	// name in the substrate is camelCase and a key is the one thing that is
	// not: keys stay lowercase so `feedbackNote` and `feedbacknote` cannot be
	// two keys nobody can tell apart.
	if lower := strings.ToLower(seg); lower != seg && ok.MatchString(lower) {
		if kebab := metaKebab(seg); kebab != lower && ok.MatchString(kebab) {
			return fmt.Sprintf("the %s is lowercase: %q or %q", which, lower, kebab)
		}
		return fmt.Sprintf("the %s is lowercase: %q", which, lower)
	}
	for i, r := range seg {
		if i == 0 {
			if !ok.MatchString(string(r)) {
				return fmt.Sprintf("the %s starts with %q: it starts with a lowercase letter", which, string(r))
			}
			continue
		}
		if !ok.MatchString("a" + string(r)) {
			return fmt.Sprintf("the %s may not carry %q: it is %s", which, string(r), chars)
		}
	}
	return fmt.Sprintf("the %s is %s, starting with a letter", which, chars)
}

// metaKebab is the camelCase spelling of seg written the way a key may be
// written: a boundary is an upper-case letter after a lower-case one or a
// digit, which is where a reader of `feedbackNote` wanted a word break.
func metaKebab(seg string) string {
	var b strings.Builder
	for i, r := range seg {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := seg[i-1]
			if prev >= 'a' && prev <= 'z' || prev >= '0' && prev <= '9' {
				b.WriteByte('-')
			}
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// MetaKeyNamespace returns the writer namespace of a label/annotation key.
func MetaKeyNamespace(key string) string {
	for i := range len(key) {
		if key[i] == '/' {
			return key[:i]
		}
	}
	return ""
}
