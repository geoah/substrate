package pushrelay

import (
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/geoah/substrate/internal/pushwire"
)

// SealKeySize is the size of a seal key: XChaCha20-Poly1305's.
const SealKeySize = chacha20poly1305.KeySize

// keyPrefix opens every push key this relay seals; a v2 would open with v2.
const keyPrefix = "v1."

// keyAD is the additional data every push key is sealed with, so a sealed
// value of any other use under the same key never opens as one.
const keyAD = "substrate-push-key-v1"

// installInfo derives, from the seal key, the key the per-install limit
// hashes Apple's token under, so the limit holds no token in memory.
const installInfo = "substrate-push-relay install limit v1"

// ErrKeyRefused is a push key that does not open under the relay's seal
// keys, or opens to something that is not a push key.
var ErrKeyRefused = errors.New("the push key does not open under this relay's seal keys")

// PushKey is what a push key seals: everything the relay needs to send to
// one registration of one install, so it keeps nothing.
type PushKey struct {
	// Platform is the push service, PlatformAPNs.
	Platform string `json:"p"`
	// Token is Apple's device token, lowercase hex.
	Token string `json:"t"`
	// Topic is the app's bundle id.
	Topic string `json:"o"`
	// Environment is the Apple environment that issued Token.
	Environment string `json:"e"`
	// PublicKey verifies the pushes the install's server signs.
	PublicKey string `json:"k"`
	// Generation is the install's registration, echoed in every payload.
	Generation string `json:"g"`
	// Issued is when the relay sealed it, in unix seconds.
	Issued int64 `json:"i"`
}

// Sealer seals push keys under the current seal key and opens them under
// the current or the previous one, so rotating is setting the old key as
// previous and a new one as current.
type Sealer struct {
	current, previous cipher.AEAD
	installKey        []byte
	rand              io.Reader
}

// NewSealer takes the current seal key, the previous one or nil, and the
// source of nonces (crypto/rand.Reader outside tests).
func NewSealer(current, previous []byte, rand io.Reader) (*Sealer, error) {
	cur, err := chacha20poly1305.NewX(current)
	if err != nil {
		return nil, fmt.Errorf("the seal key is not %d bytes", SealKeySize)
	}
	s := &Sealer{current: cur, rand: rand}
	if previous != nil {
		if s.previous, err = chacha20poly1305.NewX(previous); err != nil {
			return nil, fmt.Errorf("the previous seal key is not %d bytes", SealKeySize)
		}
	}
	if s.installKey, err = hkdf.Key(sha256.New, current, nil, installInfo, sha256.Size); err != nil {
		return nil, err
	}
	return s, nil
}

// Seal writes k as a push key: `v1.` and the base64url of a random 24-byte
// nonce and the ciphertext of k's JSON under the current seal key.
func (s *Sealer) Seal(k PushKey) (string, error) {
	plain, err := json.Marshal(k)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX, chacha20poly1305.NonceSizeX+len(plain)+chacha20poly1305.Overhead)
	if _, err := io.ReadFull(s.rand, nonce); err != nil {
		return "", fmt.Errorf("read a nonce: %w", err)
	}
	key := keyPrefix + pushwire.Encode(s.current.Seal(nonce, nonce, plain, []byte(keyAD)))
	if len(key) > pushwire.MaxPushKey {
		return "", fmt.Errorf("a sealed push key is %d characters, over %d", len(key), pushwire.MaxPushKey)
	}
	return key, nil
}

// Open reads a push key sealed under the current seal key, then the
// previous one. Anything else is ErrKeyRefused.
func (s *Sealer) Open(key string) (PushKey, error) {
	enc, ok := strings.CutPrefix(key, keyPrefix)
	if !ok {
		return PushKey{}, ErrKeyRefused
	}
	raw, err := pushwire.Decode(enc)
	if err != nil || len(raw) < chacha20poly1305.NonceSizeX+chacha20poly1305.Overhead {
		return PushKey{}, ErrKeyRefused
	}
	nonce, box := raw[:chacha20poly1305.NonceSizeX], raw[chacha20poly1305.NonceSizeX:]
	plain, err := s.current.Open(nil, nonce, box, []byte(keyAD))
	if err != nil && s.previous != nil {
		plain, err = s.previous.Open(nil, nonce, box, []byte(keyAD))
	}
	if err != nil {
		return PushKey{}, ErrKeyRefused
	}
	var k PushKey
	if err := json.Unmarshal(plain, &k); err != nil || k.Platform != pushwire.PlatformAPNs || k.Token == "" ||
		k.Topic == "" || k.Environment == "" || k.Generation == "" || k.Issued <= 0 {
		return PushKey{}, ErrKeyRefused
	}
	pub, err := pushwire.Decode(k.PublicKey)
	if err != nil || len(pub) != pushwire.PublicKeySize {
		return PushKey{}, ErrKeyRefused
	}
	return k, nil
}

// InstallKey is the per-install limit's key for Apple's token: an HMAC
// under a key derived from the current seal key, so a new registration of
// the same install shares its bucket and no token sits in memory.
func (s *Sealer) InstallKey(token string) string {
	m := hmac.New(sha256.New, s.installKey)
	m.Write([]byte(strings.ToLower(token)))
	return pushwire.Encode(m.Sum(nil))
}
