package vocabulary

import (
	"crypto/rand"
	"regexp"
)

// An identity is a person's sign-in, held by the server beside the
// repositories (decision 0274): a name, a password and an optional second
// factor. Its id keys it in the `identities` table, in the table of the
// repositories it may open and on every token it signed in for; the name is
// what the person types and may one day change. Like a repository id it is
// minted, random and never shown as a name, and `idn-` says both what it
// names and that it is 128 random bits.
const IdentityIDPrefix = "idn-"

var reIdentityID = regexp.MustCompile(`^idn-[0-7][0-9a-hjkmnp-tv-z]{25}$`)

// ValidIdentityID reports whether s is an identity id this binary mints.
func ValidIdentityID(s string) bool { return reIdentityID.MatchString(s) }

// NewIdentityID mints an identity id: the prefix and 128 random bits in the
// minted record id's alphabet.
func NewIdentityID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return IdentityIDPrefix + EncodeMintedID(b), nil
}
