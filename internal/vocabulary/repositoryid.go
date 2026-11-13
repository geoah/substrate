package vocabulary

import (
	"crypto/rand"
	"regexp"
)

// A repository's id keys its storage: the directory under the data root, the
// `repository` column of every table and the row security that reads it, the
// binding its data key is wrapped under, and the webhook address the server
// gives out (decision 0261). It is minted at creation, random, never shown as
// a name and never changed. Its prefix names how it was made: `rnd-` is 128
// random bits, so an id that can be proven from outside could take another
// prefix later. An id holds no dot, so it is never an authority, and a path
// segment or a webhook address tells the two apart by that alone.
const RepositoryIDPrefix = "rnd-"

var reRepositoryID = regexp.MustCompile(`^rnd-[0-7][0-9a-hjkmnp-tv-z]{25}$`)

// ValidRepositoryID reports whether s is a repository id this binary mints.
func ValidRepositoryID(s string) bool { return reRepositoryID.MatchString(s) }

// NewRepositoryID mints a repository id: the prefix and 128 random bits in the
// minted record id's alphabet.
func NewRepositoryID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return RepositoryIDPrefix + EncodeMintedID(b), nil
}
