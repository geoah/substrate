package vocabulary

import (
	"crypto/sha256"
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
	// an authority (a DNS name, or `~` for the repository's own), a package
	// and a name split by the two slashes.
	kindRefRE = `((~|` + authorityRE + `)/` + wordRE + `/)?` + wordRE
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
	// A namespaced label/annotation key is "<actor>/<name>", and an actor may
	// carry the domain's colons and an authority's dots
	// (`function:web.bundles.example.com:harvest/synced`).
	reMetaKey       = regexp.MustCompile(`^` + metaNamespaceRE + `/` + metaNameRE + `$`)
	reMetaNamespace = regexp.MustCompile(`^` + metaNamespaceRE + `$`)
	reMetaName      = regexp.MustCompile(`^` + metaNameRE + `$`)
	// reID is the record-id alphabet. Minted ids are 26 lowercase Crockford
	// base32 characters (MintedID); a writer's own id is its provider key
	// ENCODED into this set, which is RFC 3986 unreserved (ALPHA / DIGIT /
	// "-" / "." / "_" / "~") plus the two extra pchars a path segment
	// admits, ":" and "@", plus "/".
	//
	// The "/" is there for exactly one reason: a DECLARATION's id is a KIND
	// REFERENCE ("samples.substrate.reamde.dev/tasks/task"), and the grammar has one
	// spelling. A "/" is legal in a URI path segment only when percent-
	// encoded, so a client writes `%2F` and the API decodes it once
	// (api/rest.go pathParam). No "%" in the alphabet, so nothing on the wire
	// is percent-decoded twice.
	//
	// A declaration of the repository's own is named under `~`
	// (`~/tasks/task`), so an id may also open with `~/` (decision record
	// 0259); the alphabet itself is unchanged.
	reID = regexp.MustCompile(`^(~/)?[A-Za-z0-9][A-Za-z0-9._~:@/-]*$`)
)

// MaxIDLen caps a writer-supplied id: long enough for any encoded provider
// key, short enough that ids stay index keys rather than payloads.
const MaxIDLen = 128

// ValidAuthority reports whether s is a legal DNS-style authority name.
func ValidAuthority(s string) bool { return reAuthority.MatchString(s) }

// ValidKindAuthority reports whether s may be the authority a package, a kind,
// a trait, a function or an agent is named under: a DNS name, or Local for
// the repository's own.
func ValidKindAuthority(s string) bool { return s == Local || ValidAuthority(s) }

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

// ValidMetaKey reports whether s is a legal namespaced label/annotation key.
func ValidMetaKey(s string) bool { return reMetaKey.MatchString(s) }

// MintedIDLen is the length of a minted record id.
const MintedIDLen = 26

// reMinted is the shape of a minted record id: a UUIDv7 as 26 lowercase
// Crockford base32 characters, the suffix form of a TypeID. The first
// character carries three bits, so it is 0 to 7.
var reMinted = regexp.MustCompile(`^[0-7][0-9a-hjkmnp-tv-z]{25}$`)

// reUUID is a UUID's canonical text, lowercase and hyphenated: the other
// spelling a client may create a record under, because it is what the apps
// mint and what a HealthKit workout already carries as its sync identifier.
// The server mints the Crockford form alone.
var reUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// crockford is the lowercase Crockford base32 alphabet a minted id is written
// in: no i, l, o or u, so an id read aloud or copied by hand has one spelling.
const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

// EncodeMintedID writes a UUID's 16 bytes as the 26 characters of a minted
// id, most significant first. 26 characters hold 130 bits, so the first
// character carries two zero padding bits and is always 0 to 7.
func EncodeMintedID(b [16]byte) string {
	var out [MintedIDLen]byte
	for i := range MintedIDLen {
		var v byte
		// Character i covers bits i*5-2 to i*5+2 of b; the two before bit 0
		// are the zero padding.
		for k := i*5 - 2; k < i*5+3; k++ {
			v <<= 1
			if k >= 0 {
				v |= b[k/8] >> (7 - k%8) & 1
			}
		}
		out[i] = crockford[v]
	}
	return string(out[:])
}

// DerivedMintedID is an id in the minted shape computed from parts: the
// first 16 bytes of their sha256, marked as a UUIDv8 (RFC 9562's version for
// custom layouts). A writer that must name the same record again, a mirror
// keyed by an upstream id or a retried request, derives it instead of
// minting (decision 0228). It mirrors host.py's `ids.minted` in the
// algorithm, not the argument check: the body refuses an empty part, and
// every caller here passes non-empty ones, which the vectors in
// internal/runner/testdata/id_vectors.json hold.
func DerivedMintedID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	var u [16]byte
	copy(u[:], sum[:16])
	u[6] = u[6]&0x0f | 0x80 // version 8
	u[8] = u[8]&0x3f | 0x80 // RFC 9562 variant
	return EncodeMintedID(u)
}

// MintedID reports whether s has the shape of a minted record id: the id a
// person, an app or an agent may give a new record of a minted kind
// (decision 0228). Two spellings pass: the 26 Crockford characters the
// server mints, and a UUID's canonical lowercase text, which the apps mint.
// The shape is checked, not the UUID version, so a writer minting offline
// needs only the encoding. Both are opaque: neither can spell a title.
func MintedID(s string) bool { return reMinted.MatchString(s) || reUUID.MatchString(s) }

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

// NameRule turns the name a person types into a repository's name (decision
// record 0288). A name carrying a dot is the name (`ada.example.com`). A bare
// label is completed under the server's configured public host, the host of
// SUBSTRATE_PUBLIC_URL (`ada` -> `ada.substrate.example.com`), never under
// whatever address a request arrived at, which a caller chooses. A name under
// `localhost` or an IP address is refused for a new repository outside
// development, which is a server whose public host is itself one of those.
type NameRule struct {
	// Host is the configured public host, lower case, no port; "" when the
	// server has no public URL.
	Host string
	// Development says the public host is `localhost` or a loopback address,
	// where names under it are the only ones that work.
	Development bool
}

// NameRuleFor is the rule of a server whose public URL is publicURL ("" for
// none).
func NameRuleFor(publicURL string) NameRule {
	if publicURL == "" {
		return NameRule{}
	}
	host := publicURL
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	host, _, _ = strings.Cut(host, "/")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	ip := net.ParseIP(host)
	return NameRule{Host: host, Development: host == "localhost" || strings.HasSuffix(host, ".localhost") || (ip != nil && ip.IsLoopback())}
}

// normalizeTyped is a typed name trimmed, lower-cased and without a trailing
// dot.
func normalizeTyped(typed string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(typed)), ".")
}

// New is the name a new repository typed as typed takes, at a registration
// or as a rename's new name, or why it takes none. The result is not held to
// the authority grammar here; the engine refuses a bad one in its own words.
func (r NameRule) New(typed string) (string, error) {
	name := normalizeTyped(typed)
	if name == "" {
		return "", nil
	}
	if !strings.Contains(name, ".") {
		if r.Host == "" {
			return "", fmt.Errorf("%q is a short name, and this server has no public URL to complete it under: type the whole name, such as %s.example.com (or the operator sets SUBSTRATE_PUBLIC_URL)", name, name)
		}
		name += "." + r.Host
	}
	if !r.Development && LocalName(name) {
		return "", fmt.Errorf("%s is under localhost or an IP address, which names no repository outside development: choose a name under a domain, such as %s.example.com", name, strings.Split(name, ".")[0])
	}
	return name, nil
}

// Lookup is the name an existing repository typed as typed has, for a door
// that finds one (a sign-in, a delete, a rename's current name, an app's
// sign-in hint): a short name is completed under the configured public host,
// or under the host the request reached, host, where none is configured, as
// it was named before 0288. A name is never refused here: a repository named
// under `localhost` before the rule is still found, and can be renamed.
func (r NameRule) Lookup(typed, host string) string {
	name := normalizeTyped(typed)
	if name == "" || strings.Contains(name, ".") {
		return name
	}
	under := r.Host
	if under == "" {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		under = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	}
	if under == "" {
		return ""
	}
	return name + "." + under
}

// LocalName reports whether name sits under `localhost` or is, or ends in, an
// IP address: a name only one machine's resolver gives meaning to.
func LocalName(name string) bool {
	name = normalizeTyped(name)
	if name == "localhost" || strings.HasSuffix(name, ".localhost") || net.ParseIP(name) != nil {
		return true
	}
	labels := strings.Split(name, ".")
	if len(labels) >= 4 && net.ParseIP(strings.Join(labels[len(labels)-4:], ".")).To4() != nil {
		return true
	}
	return false
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
