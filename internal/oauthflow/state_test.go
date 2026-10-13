package oauthflow

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The signed state is the only authentication the unauthenticated OAuth
// callback has, so every refusal VerifyState makes is pinned here without a
// database: a forged or flipped body, a flipped signature, another key, an
// empty key, an expired state, a state missing what the callback resolves,
// and input that is not a state at all.

var stateEpoch = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func stateClient(key string, now time.Time) *Client {
	return &Client{
		StateKey:    []byte(key),
		CallbackURL: "https://substrate.example/cb",
		Now:         func() time.Time { return now },
	}
}

func goodState() State {
	return State{Repository: "ada.example.com", Record: "ada.example.com/mail/account/a1", Nonce: "n1"}
}

// macOf signs body under key the way signState does, so a test can build a
// correctly signed state around a body signState would never write.
func macOf(key, body string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func wantBadState(t *testing.T, c *Client, signed, why string) {
	t.Helper()
	st, err := c.VerifyState(signed)
	if !errors.Is(err, ErrBadState) {
		t.Fatalf("%s: VerifyState = %+v, %v, want ErrBadState", why, st, err)
	}
}

// AuthCodeURL signs the state into the consent URL with the PKCE challenge,
// and VerifyState under the same key returns it unchanged.
func TestStateRoundTrip(t *testing.T) {
	c := stateClient("k1", stateEpoch)
	verifier := NewVerifier()
	consent, err := c.AuthCodeURL(Endpoints{AuthURL: "https://provider.example/authorize", ClientID: "id"}, goodState(), verifier)
	if err != nil {
		t.Fatalf("AuthCodeURL: %v", err)
	}
	u, err := url.Parse(consent)
	if err != nil {
		t.Fatalf("parse consent url: %v", err)
	}
	q := u.Query()
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("consent url carries no S256 challenge: %s", consent)
	}
	st, err := c.VerifyState(q.Get("state"))
	if err != nil {
		t.Fatalf("VerifyState: %v", err)
	}
	want := goodState()
	want.Exp = stateEpoch.Add(StateTTL).Unix()
	if st != want {
		t.Fatalf("state = %+v, want %+v", st, want)
	}
}

func TestStateRefusesAFlippedBody(t *testing.T) {
	c := stateClient("k1", stateEpoch)
	signed, err := c.signState(State{Repository: "ada.example.com", Record: "ada.example.com/mail/account/a1", Exp: stateEpoch.Add(time.Minute).Unix()})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	body, sig, _ := strings.Cut(signed, ".")

	// One byte of the body flipped.
	flipped := []byte(body)
	flipped[len(flipped)/2] ^= 0x01
	wantBadState(t, c, string(flipped)+"."+sig, "a flipped body byte")

	// A well-formed body naming another account, under the original
	// signature: only the MAC tells it from the real one.
	forged, _ := json.Marshal(State{Repository: "ada.example.com", Record: "ada.example.com/mail/account/a2", Exp: stateEpoch.Add(time.Minute).Unix()})
	wantBadState(t, c, base64.RawURLEncoding.EncodeToString(forged)+"."+sig, "a forged body under the original signature")
}

func TestStateRefusesAFlippedSignature(t *testing.T) {
	c := stateClient("k1", stateEpoch)
	signed, err := c.signState(State{Repository: "ada.example.com", Record: "ada.example.com/mail/account/a1", Exp: stateEpoch.Add(time.Minute).Unix()})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	body, sig, _ := strings.Cut(signed, ".")
	raw, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	raw[0] ^= 0x01
	wantBadState(t, c, body+"."+base64.RawURLEncoding.EncodeToString(raw), "a flipped signature byte")
	wantBadState(t, c, body+".", "an empty signature")
}

func TestStateRefusesAnotherKey(t *testing.T) {
	signed, err := stateClient("k1", stateEpoch).signState(State{Repository: "ada.example.com", Record: "ada.example.com/mail/account/a1", Exp: stateEpoch.Add(time.Minute).Unix()})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	wantBadState(t, stateClient("k2", stateEpoch), signed, "a state signed under another key")
}

// An empty key would make every signature forgeable, so it refuses on both
// sides: no state is minted, and none is accepted, not even one whose MAC
// is the empty key's own arithmetic.
func TestStateRefusesAnEmptyKey(t *testing.T) {
	c := stateClient("", stateEpoch)
	if _, err := c.signState(goodState()); err == nil {
		t.Fatal("signState minted a state under an empty key")
	}
	if _, err := c.AuthCodeURL(Endpoints{AuthURL: "https://provider.example/authorize"}, goodState(), ""); err == nil {
		t.Fatal("AuthCodeURL minted a state under an empty key")
	}
	st := goodState()
	st.Exp = stateEpoch.Add(time.Minute).Unix()
	payload, _ := json.Marshal(st)
	body := base64.RawURLEncoding.EncodeToString(payload)
	wantBadState(t, c, body+"."+macOf("", body), "a state MACed under the empty key")
}

// A state is accepted up to its expiry and refused after it, on the client's
// clock.
func TestStateRefusesAnExpiredState(t *testing.T) {
	signed, err := stateClient("k1", stateEpoch).signState(State{Repository: "ada.example.com", Record: "ada.example.com/mail/account/a1", Exp: stateEpoch.Add(StateTTL).Unix()})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := stateClient("k1", stateEpoch.Add(StateTTL-time.Second)).VerifyState(signed); err != nil {
		t.Fatalf("a state one second inside its TTL was refused: %v", err)
	}
	wantBadState(t, stateClient("k1", stateEpoch.Add(StateTTL+time.Second)), signed, "a state one second past its TTL")
}

// The callback resolves the repository and the record off the state, so a
// state missing either is refused even when correctly signed.
func TestStateRefusesAMissingRepositoryOrRecord(t *testing.T) {
	c := stateClient("k1", stateEpoch)
	exp := stateEpoch.Add(time.Minute).Unix()
	for name, st := range map[string]State{
		"no repository": {Record: "ada.example.com/mail/account/a1", Exp: exp},
		"no record":     {Repository: "ada.example.com", Exp: exp},
	} {
		signed, err := c.signState(st)
		if err != nil {
			t.Fatalf("%s: sign: %v", name, err)
		}
		wantBadState(t, c, signed, name)
	}
}

// Input that is not a signed state at all: no separator, and correctly
// signed bodies that are not base64 or not JSON.
func TestStateRefusesMalformedInput(t *testing.T) {
	c := stateClient("k1", stateEpoch)
	wantBadState(t, c, "", "an empty state")
	wantBadState(t, c, "no-separator-here", "a state with no dot")
	wantBadState(t, c, "!!!."+macOf("k1", "!!!"), "a signed body that is not base64")
	notJSON := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	wantBadState(t, c, notJSON+"."+macOf("k1", notJSON), "a signed body that is not JSON")
}
