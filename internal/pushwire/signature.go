package pushwire

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// signaturePrefix opens the signed bytes, so a signature over a push can be
// nothing else's.
const signaturePrefix = "substrate-push-v1\n"

// ErrStale is a signature whose time is more than SignatureWindow from the
// relay's clock, either way. ErrRefused is no signature, one that does not
// parse, or one the public key does not verify over the body.
var (
	ErrStale   = errors.New("the signature's time is outside the window")
	ErrRefused = errors.New("the signature does not verify")
)

// Signature is a parsed SignatureHeader value.
type Signature struct {
	// T is the time the signer stamped, to the second.
	T time.Time
	// Sig is the Ed25519 signature over SignedBytes(T, body).
	Sig []byte
}

// SignedBytes is what a push's signature covers: the prefix, the time in
// unix seconds and the lowercase hex of SHA-256 of the exact body bytes,
// each line ended by "\n" but the last. The path is not signed, so a relay
// behind a prefix or a proxy that rewrites it still verifies.
func SignedBytes(t time.Time, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(signaturePrefix + strconv.FormatInt(t.Unix(), 10) + "\n" + hex.EncodeToString(sum[:]))
}

// Sign returns the SignatureHeader value for body at t:
// `t=<unix seconds>,s=<base64url of the Ed25519 signature>`.
func Sign(key ed25519.PrivateKey, t time.Time, body []byte) string {
	return "t=" + strconv.FormatInt(t.Unix(), 10) + ",s=" + Encode(ed25519.Sign(key, SignedBytes(t, body)))
}

// ParseSignature reads a SignatureHeader value in the one form Sign writes:
// `t=` then `,s=`, the time a decimal of unix seconds and the signature 64
// bytes of base64url. Anything else is ErrRefused.
func ParseSignature(header string) (Signature, error) {
	ts, ss, ok := strings.Cut(header, ",")
	if !ok || !strings.HasPrefix(ts, "t=") || !strings.HasPrefix(ss, "s=") {
		return Signature{}, ErrRefused
	}
	ts, ss = ts[2:], ss[2:]
	if ts == "" || len(ts) > 19 || (len(ts) > 1 && ts[0] == '0') {
		return Signature{}, ErrRefused
	}
	for i := 0; i < len(ts); i++ {
		if ts[i] < '0' || ts[i] > '9' {
			return Signature{}, ErrRefused
		}
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return Signature{}, ErrRefused
	}
	sig, err := Decode(ss)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return Signature{}, ErrRefused
	}
	return Signature{T: time.Unix(unix, 0), Sig: sig}, nil
}

// Verify checks a SignatureHeader value against pub, at now, over body: the
// time first, so a request out of the window costs no verification, then the
// signature. It answers nil, ErrStale or ErrRefused.
func Verify(header string, pub ed25519.PublicKey, now time.Time, body []byte) error {
	s, err := ParseSignature(header)
	if err != nil {
		return err
	}
	return s.Verify(pub, now, body)
}

// Verify is the package Verify over a parsed signature.
func (s Signature) Verify(pub ed25519.PublicKey, now time.Time, body []byte) error {
	if d := now.Sub(s.T); d > SignatureWindow || d < -SignatureWindow {
		return ErrStale
	}
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, SignedBytes(s.T, body), s.Sig) {
		return ErrRefused
	}
	return nil
}
