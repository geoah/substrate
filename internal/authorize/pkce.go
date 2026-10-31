package authorize

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// ValidChallenge reports whether a code_challenge is an S256 challenge: the
// unpadded base64url of a SHA-256, exactly 43 characters (RFC 7636 §4.2).
func ValidChallenge(c string) bool {
	if len(c) != 43 {
		return false
	}
	for i := range len(c) {
		if !base64urlChar(c[i]) {
			return false
		}
	}
	return true
}

// ValidVerifier reports whether a code_verifier is 43 to 128 characters of
// the unreserved alphabet (RFC 7636 §4.1).
func ValidVerifier(v string) bool {
	if len(v) < 43 || len(v) > 128 {
		return false
	}
	for i := range len(v) {
		c := v[i]
		if !base64urlChar(c) && c != '.' && c != '~' {
			return false
		}
	}
	return true
}

// VerifyS256 reports whether verifier hashes to challenge, in constant time.
func VerifyS256(verifier, challenge string) bool {
	if !ValidVerifier(verifier) || !ValidChallenge(challenge) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

func base64urlChar(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}
