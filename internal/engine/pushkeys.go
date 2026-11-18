package engine

// THE KEYS OF ONE TOKEN'S PUSHES (docs/plans/push.md). Two keys per token,
// each derived with HKDF-SHA256 from the repository's DEK, salt `substrate
// push`, and the token's record id under its own info: an Ed25519 key the
// token's pushes are signed with (`signing v1 <id>`), whose public half the
// install hands the relay at register, and an XChaCha20-Poly1305 key its
// event ids are sealed under (`events v1 <id>`). The derivation reads ids,
// never names, so a rename keeps them; it changes only when the DEK does.
// What leaves the engine is a substrate.PushSigner for one token; the DEK
// never does.

import (
	"context"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/substrate"
)

const (
	pushKeySalt     = "substrate push"
	pushSigningInfo = "signing v1 "
	pushEventsInfo  = "events v1 "
	// pushEventAAD binds an event id's seal to its use, as the relay binds
	// a push key's: a ciphertext sealed for anything else under the same
	// key does not open as an event id.
	pushEventAAD = "substrate-push-event-v1"
)

// pushSigner is one token's signer.
type pushSigner struct {
	signing ed25519.PrivateKey
	events  []byte
	now     func() time.Time
}

var _ substrate.PushSigner = (*pushSigner)(nil)

// derivePushSigner derives the signer of tokenID's pushes from dek.
func derivePushSigner(dek []byte, tokenID string, now func() time.Time) (*pushSigner, error) {
	if len(dek) == 0 {
		return nil, errors.New("substrate/engine: dataset has no DEK")
	}
	if tokenID == "" {
		return nil, fmt.Errorf("%w: a push signer is a token's, and none was named", substrate.ErrValidation)
	}
	seed, err := hkdf.Key(sha256.New, dek, []byte(pushKeySalt), pushSigningInfo+tokenID, ed25519.SeedSize)
	if err != nil {
		return nil, err
	}
	events, err := hkdf.Key(sha256.New, dek, []byte(pushKeySalt), pushEventsInfo+tokenID, chacha20poly1305.KeySize)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &pushSigner{signing: ed25519.NewKeyFromSeed(seed), events: events, now: now}, nil
}

// PushSigner is the signer of one token's pushes. It does not ask whether
// the token is live: a signer for a token that is gone signs what nothing
// will send, and opens what nothing resolves.
func (ds *dataset) PushSigner(_ context.Context, tokenID string) (substrate.PushSigner, error) {
	return derivePushSigner(ds.dek, tokenID, ds.svc.now)
}

func (s *pushSigner) PublicKey() ed25519.PublicKey {
	return s.signing.Public().(ed25519.PublicKey)
}

func (s *pushSigner) Sign(t time.Time, body []byte) string {
	return pushwire.Sign(s.signing, t, body)
}

// sealedEvent is an event id's plaintext, keys kept short so the longest
// item fits under pushwire.MaxEventID.
type sealedEvent struct {
	Event   string   `json:"e"`
	Thread  string   `json:"t"`
	Kind    string   `json:"k,omitempty"`
	ID      string   `json:"i,omitempty"`
	Version int64    `json:"v,omitempty"`
	Alerts  []string `json:"a,omitempty"`
	Issued  int64    `json:"at"`
}

func (s *pushSigner) SealEvent(e substrate.PushEvent) (string, error) {
	aead, err := chacha20poly1305.NewX(s.events)
	if err != nil {
		return "", err
	}
	issued := e.IssuedAt
	if issued.IsZero() {
		issued = s.now()
	}
	plain, err := json.Marshal(sealedEvent{
		Event: e.Event, Thread: e.Thread, Kind: e.Item.Kind, ID: e.Item.ID, Version: e.Item.Version,
		Alerts: e.Alerts, Issued: issued.Unix(),
	})
	if err != nil {
		return "", err
	}
	// A plaintext past the id's own bound can never seal into one, and is
	// refused before anything is sized by it.
	if len(plain) > pushwire.MaxEventID {
		return "", fmt.Errorf("%w: an event of %d bytes is past %d before it is sealed", errEventTooLong, len(plain), pushwire.MaxEventID)
	}
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(plain)+aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(aead.Seal(nonce, nonce, plain, []byte(pushEventAAD)))
	if len(id) > pushwire.MaxEventID {
		return "", fmt.Errorf("%w: a sealed event id of %d characters is past %d", errEventTooLong, len(id), pushwire.MaxEventID)
	}
	return id, nil
}

// errEventTooLong is an event whose seal does not fit an event id; a read
// that meets it splits its alerts.
var errEventTooLong = errors.New("event too long")

func (s *pushSigner) OpenEvent(id string) (substrate.PushEvent, error) {
	notFound := fmt.Errorf("%w: no such event", substrate.ErrNotFound)
	if len(id) > pushwire.MaxEventID {
		return substrate.PushEvent{}, notFound
	}
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return substrate.PushEvent{}, notFound
	}
	aead, err := chacha20poly1305.NewX(s.events)
	if err != nil {
		return substrate.PushEvent{}, err
	}
	if len(raw) < aead.NonceSize()+aead.Overhead() {
		return substrate.PushEvent{}, notFound
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(pushEventAAD))
	if err != nil {
		return substrate.PushEvent{}, notFound
	}
	var e sealedEvent
	if err := json.Unmarshal(plain, &e); err != nil {
		return substrate.PushEvent{}, notFound
	}
	issued := time.Unix(e.Issued, 0).UTC()
	if age := s.now().Sub(issued); age > substrate.PushEventLifetime || age < -pushwire.SignatureWindow {
		return substrate.PushEvent{}, notFound
	}
	out := substrate.PushEvent{Event: e.Event, Thread: e.Thread, Alerts: e.Alerts, IssuedAt: issued}
	if e.Kind != "" {
		out.Item = substrate.PushItem{Kind: e.Kind, ID: e.ID, Version: e.Version}
	}
	return out, nil
}
