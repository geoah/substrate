// Package recordid is the record-id grammar and nothing else, so a binary
// that only checks an id (the push relay, which names a thread by one) links
// none of the vocabulary. vocabulary.ValidID and vocabulary.MaxIDLen are this
// package's answers.
package recordid

import "regexp"

// MaxLen caps a writer-supplied id: long enough for any encoded provider
// key, short enough that ids stay index keys rather than payloads.
const MaxLen = 128

// re is the record-id alphabet. Minted ids are 26 lowercase Crockford base32
// characters (vocabulary.MintedID); a writer's own id is its provider key
// ENCODED into this set, which is RFC 3986 unreserved (ALPHA / DIGIT / "-" /
// "." / "_" / "~") plus the two extra pchars a path segment admits, ":" and
// "@", plus "/".
//
// The "/" is there for exactly one reason: a DECLARATION's id is a KIND
// REFERENCE ("samples.substrate.reamde.dev/tasks/task"), and the grammar has
// one spelling. A "/" is legal in a URI path segment only when percent-
// encoded, so a client writes `%2F` and the API decodes it once (api/rest.go
// pathParam). No "%" in the alphabet, so nothing on the wire is
// percent-decoded twice.
//
// A declaration of the repository's own is named under `~` (`~/tasks/task`),
// so an id may also open with `~/` (decision record 0259); the alphabet
// itself is unchanged.
var re = regexp.MustCompile(`^(~/)?[A-Za-z0-9][A-Za-z0-9._~:@/-]*$`)

// Valid reports whether s is a legal record id.
func Valid(s string) bool {
	return len(s) <= MaxLen && re.MatchString(s)
}
