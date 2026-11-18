// Package pushwiretest reads the push wire's fixtures, internal/pushwire's
// testdata/, for the tests of every side that speaks it: the relay, the
// server and, through copies, the apps. Only tests import it.
package pushwiretest

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/pushwire"
)

// Dir is the fixtures' directory, wherever the test runs from.
func Dir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata")
}

// Keys is keys.json: the keys and values every other fixture was made with.
// None of them is anybody's: they exist only here.
type Keys struct {
	// Now is when every push fixture was signed and sent, in unix seconds.
	Now int64 `json:"now"`
	// RegisteredAt is when the fixtures' push keys were sealed.
	RegisteredAt int64 `json:"registeredAt"`
	// SealKey is the relay's seal key, standard base64 as the relay reads it.
	SealKey string `json:"sealKey"`
	// SigningSeed is the Ed25519 seed the fixtures' pushes are signed with,
	// and PublicKey its public half, which the push keys seal.
	SigningSeed string `json:"signingSeed"`
	PublicKey   string `json:"publicKey"`
	// OtherSeed and OtherPublicKey are a second key, which signs nothing a
	// push key names.
	OtherSeed      string `json:"otherSeed"`
	OtherPublicKey string `json:"otherPublicKey"`
	// Topic is the one topic the fixtures' relay serves.
	Topic string `json:"topic"`
}

// Sealed is what a fixture's push key seals, which only the relay can read,
// written out for a reader of the fixture.
type Sealed struct {
	Token       string `json:"token"`
	Topic       string `json:"topic"`
	Environment string `json:"environment"`
	PublicKey   string `json:"publicKey"`
	Generation  string `json:"generation"`
	Issued      int64  `json:"issued"`
}

// Answer is one HTTP answer: its status and its body.
type Answer struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

// Apple is the request the relay makes to Apple for a push.
type Apple struct {
	Environment string            `json:"environment"`
	Path        string            `json:"path"`
	Headers     map[string]string `json:"headers"`
	Payload     json.RawMessage   `json:"payload"`
}

// Register is register.json: the app's request, made at
// Keys.RegisteredAt, and the relay's answer.
type Register struct {
	Request  json.RawMessage `json:"request"`
	Response Answer          `json:"response"`
}

// Push is a push-*.json fixture: one signed push, the request the relay
// makes to Apple for it, and the relay's answer once Apple answers 200.
type Push struct {
	Now       int64  `json:"now"`
	Key       Sealed `json:"key"`
	Signature string `json:"signature"`
	// Body is the exact bytes signed, as a string.
	Body     string `json:"body"`
	Apple    Apple  `json:"apple"`
	Response Answer `json:"response"`
}

// Refusal is one row of errors.json.
type Refusal struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

// SignatureCase is one vector of signatures.json.
type SignatureCase struct {
	Name      string `json:"name"`
	Header    string `json:"header"`
	Body      string `json:"body"`
	PublicKey string `json:"publicKey"`
	Now       int64  `json:"now"`
	// Verify is what pushwire.Verify answers: ok, stale or refused.
	Verify string `json:"verify"`
	// Relay is what a relay answers when it differs from Verify's: a
	// signature that verifies a second time is replayed.
	Relay string `json:"relay,omitempty"`
}

// Signatures is signatures.json.
type Signatures struct {
	Seed        string          `json:"seed"`
	PublicKey   string          `json:"publicKey"`
	T           int64           `json:"t"`
	Body        string          `json:"body"`
	SignedBytes string          `json:"signedBytes"`
	Header      string          `json:"header"`
	Cases       []SignatureCase `json:"cases"`
}

// Fingerprint is one vector of fingerprints.json.
type Fingerprint struct {
	EventID     string `json:"eventId"`
	Fingerprint string `json:"fingerprint"`
}

// Endpoint is one row of endpoints.json: a relay and its two addresses, or
// Refused.
type Endpoint struct {
	Relay    string `json:"relay"`
	Register string `json:"register,omitempty"`
	Push     string `json:"push,omitempty"`
	Refused  bool   `json:"refused,omitempty"`
}

// Read decodes the named fixture into v, failing the test when it cannot.
func Read(t testing.TB, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(Dir(), name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// LoadKeys reads keys.json.
func LoadKeys(t testing.TB) Keys {
	var k Keys
	Read(t, "keys.json", &k)
	return k
}

// LoadPush reads one push-*.json.
func LoadPush(t testing.TB, name string) Push {
	var p Push
	Read(t, name, &p)
	return p
}

// PushNames are the push fixtures, in the order the README lists them.
var PushNames = []string{
	"push-reply.json", "push-failed.json", "push-request-card-1.json", "push-request-card-2.json",
	"push-read.json", "push-reply-largest.json",
}

// SealKeyBytes decodes Keys.SealKey.
func (k Keys) SealKeyBytes(t testing.TB) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(k.SealKey)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Signer is the private key Keys.SigningSeed makes.
func (k Keys) Signer(t testing.TB) ed25519.PrivateKey {
	return seed(t, k.SigningSeed)
}

// Other is the private key Keys.OtherSeed makes.
func (k Keys) Other(t testing.TB) ed25519.PrivateKey {
	return seed(t, k.OtherSeed)
}

// Time is Now as a time.
func (k Keys) Time() time.Time { return time.Unix(k.Now, 0) }

func seed(t testing.TB, s string) ed25519.PrivateKey {
	t.Helper()
	b, err := pushwire.Decode(s)
	if err != nil || len(b) != ed25519.SeedSize {
		t.Fatalf("seed %q: %v", s, err)
	}
	return ed25519.NewKeyFromSeed(b)
}
