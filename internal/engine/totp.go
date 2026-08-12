package engine

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// The verifier's parameters are fixed: RFC 6238 SHA-1, 6 digits, 30-second
// step. TOTPEnrollmentURI declares the same values, so a password manager and
// the server never disagree.
const (
	totpDigits = 6
	totpPeriod = 30 * time.Second
	// totpSkew is how many steps either side of the current one verify;
	// wider is a bigger acceptance window for a 10^6 code space.
	totpSkew = 1
	// totpSeedBytes is the minted seed size (RFC 4226 R6 recommends 160
	// bits); totpMinSeedBytes is the smallest seed the operator may supply.
	totpSeedBytes    = 20
	totpMinSeedBytes = 16
)

// The failure LOCKOUT is not here any more: it guards the unauthenticated
// endpoints as a whole — login, registration and the credential changes — so
// it lives with the rate limiter in front of them (api/ratelimit.go), keyed
// on the caller and the username rather than on a control-plane column.

// totpIssuer labels the enrollment in the password manager.
const totpIssuer = "Substrate"

// totpEncoding is RFC 4648 base32, uppercase and unpadded — the only form an
// otpauth:// secret parameter is portable in.
var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret mints an enrollment seed in base32.
func NewTOTPSecret() (string, error) {
	raw := make([]byte, totpSeedBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return totpEncoding.EncodeToString(raw), nil
}

// decodeTOTPSecret accepts the seed as it may be pasted: any case, padded or
// not, spaces grouping the characters.
func decodeTOTPSecret(secret string) ([]byte, error) {
	s := strings.ToUpper(strings.TrimSpace(secret))
	s = strings.NewReplacer(" ", "", "-", "").Replace(s)
	s = strings.TrimRight(s, "=")
	key, err := totpEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("not base32: %w", err)
	}
	return key, nil
}

// normalizeTOTPSecret returns the canonical stored form of a seed.
func normalizeTOTPSecret(secret string) (string, error) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	return totpEncoding.EncodeToString(key), nil
}

// TOTPStep is the RFC 6238 time counter T for an instant.
func TOTPStep(t time.Time) int64 { return t.Unix() / int64(totpPeriod/time.Second) }

// TOTPCode returns the code a base32 seed produces at a time step.
func TOTPCode(secret string, step int64) (string, error) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	return hotp(key, step), nil
}

// hotp is RFC 4226: HMAC-SHA-1 over the big-endian counter, dynamic
// truncation, the low totpDigits decimal digits, zero-padded.
func hotp(key []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	trunc := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for range totpDigits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, trunc%mod)
}

// normalizeTOTPCode strips the grouping an authenticator app displays.
func normalizeTOTPCode(code string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code))
}

// totpVerify accepts code for the current step and totpSkew steps either
// side, and rejects any step at or below lastStep — a code is one-time. It
// evaluates every candidate step so the work does not depend on which one
// (if any) matched.
func totpVerify(key []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	got := normalizeTOTPCode(code)
	cur := TOTPStep(now)
	matched, ok := int64(0), false
	for step := cur - totpSkew; step <= cur+totpSkew; step++ {
		hit := totpEqual(hotp(key, step), got)
		if hit && step > lastStep && !ok {
			matched, ok = step, true
		}
	}
	return matched, ok
}

// totpEqual compares two codes without leaking their contents through
// timing.
func totpEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// dummyTOTPKey keeps a login's work identical for a user that does not exist
// or has no credential: the same HMAC evaluations against a seed no caller
// can ever hold, and the same failure.
var dummyTOTPKey = func() []byte {
	raw := make([]byte, totpSeedBytes)
	if _, err := rand.Read(raw); err != nil {
		panic("substrate/engine: no entropy for the TOTP timing equalizer: " + err.Error())
	}
	return raw
}()

// TOTPEnrollmentURI is the otpauth:// form a password manager imports; its
// parameters must match the verifier's.
func TOTPEnrollmentURI(repository, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=%d&period=%d",
		totpIssuer, url.PathEscape(repository), secret, totpIssuer, totpDigits, int(totpPeriod/time.Second))
}
