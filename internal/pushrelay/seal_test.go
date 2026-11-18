package pushrelay

import (
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/pushwire/pushwiretest"
)

func sealKey(label string) []byte { return pushwiretest.Stream(label, SealKeySize) }

var aKey = PushKey{
	Platform: pushwire.PlatformAPNs, Token: strings.Repeat("ab", 32), Topic: "dev.example.app",
	Environment: pushwire.EnvironmentProduction, PublicKey: pushwire.Encode(make([]byte, 32)),
	Generation: pushwire.Encode(make([]byte, 16)), Issued: 1_800_000_000,
}

func TestASealedKeyOpensUnderItsSealKeyAlone(t *testing.T) {
	s, err := NewSealer(sealKey("current"), nil, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.Seal(aKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "v1.") || strings.Contains(key, aKey.Token) {
		t.Fatalf("the key %s", key)
	}
	if got, err := s.Open(key); err != nil || got != aKey {
		t.Fatalf("Open = %+v, %v", got, err)
	}
	other, _ := NewSealer(sealKey("other"), nil, rand.Reader)
	if _, err := other.Open(key); !errors.Is(err, ErrKeyRefused) {
		t.Errorf("another seal key opened it: %v", err)
	}
	raw, _ := pushwire.Decode(key[3:])
	for i := range raw {
		changed := append([]byte(nil), raw...)
		changed[i] ^= 1
		if _, err := s.Open("v1." + pushwire.Encode(changed)); !errors.Is(err, ErrKeyRefused) {
			t.Fatalf("a key with byte %d changed opened: %v", i, err)
		}
	}
	for _, bad := range []string{"", "v1.", "v2." + key[3:], key[3:], key + "A", "v1.AAAA"} {
		if _, err := s.Open(bad); !errors.Is(err, ErrKeyRefused) {
			t.Errorf("%q opened: %v", bad, err)
		}
	}
}

// Two seals of one install differ, by their nonce, and both open.
func TestEverySealHasItsOwnNonce(t *testing.T) {
	s, _ := NewSealer(sealKey("current"), nil, rand.Reader)
	a, _ := s.Seal(aKey)
	b, _ := s.Seal(aKey)
	if a == b {
		t.Fatal("two seals are the same bytes")
	}
}

// Rotating: the previous key still opens what it sealed, and never seals.
func TestThePreviousSealKeyOpensAndNeverSeals(t *testing.T) {
	old, _ := NewSealer(sealKey("old"), nil, rand.Reader)
	oldKey, _ := old.Seal(aKey)
	rotated, err := NewSealer(sealKey("new"), sealKey("old"), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := rotated.Open(oldKey); err != nil || got != aKey {
		t.Fatalf("the previous key's push key: %+v, %v", got, err)
	}
	newKey, _ := rotated.Seal(aKey)
	if _, err := old.Open(newKey); !errors.Is(err, ErrKeyRefused) {
		t.Fatal("a key sealed after the rotation opens under the previous seal key alone")
	}
	fresh, _ := NewSealer(sealKey("new"), nil, rand.Reader)
	if _, err := fresh.Open(newKey); err != nil {
		t.Fatalf("the current key alone does not open what it sealed: %v", err)
	}
	if _, err := fresh.Open(oldKey); !errors.Is(err, ErrKeyRefused) {
		t.Fatal("a dropped previous key's push key still opens")
	}
}

// What opens must be a push key: a value sealed by the same key with other
// additional data, or a sealed JSON that is not one, is refused.
func TestOnlyAPushKeyOpens(t *testing.T) {
	s, _ := NewSealer(sealKey("current"), nil, rand.Reader)
	nonce := make([]byte, 24)
	for name, plain := range map[string]string{
		"not JSON":         "hello",
		"no token":         `{"p":"apns","o":"t","e":"production","k":"` + aKey.PublicKey + `","g":"g","i":1}`,
		"another platform": `{"p":"fcm","t":"ab","o":"t","e":"production","k":"` + aKey.PublicKey + `","g":"g","i":1}`,
		"a short key":      `{"p":"apns","t":"ab","o":"t","e":"production","k":"AAAA","g":"g","i":1}`,
		"no issued time":   `{"p":"apns","t":"ab","o":"t","e":"production","k":"` + aKey.PublicKey + `","g":"g"}`,
	} {
		key := "v1." + pushwire.Encode(s.current.Seal(nonce, nonce, []byte(plain), []byte(keyAD)))
		if _, err := s.Open(key); !errors.Is(err, ErrKeyRefused) {
			t.Errorf("%s opened: %v", name, err)
		}
	}
	plain := `{"p":"apns","t":"ab","o":"t","e":"production","k":"` + aKey.PublicKey + `","g":"g","i":1}`
	key := "v1." + pushwire.Encode(s.current.Seal(nonce, nonce, []byte(plain), []byte("something else")))
	if _, err := s.Open(key); !errors.Is(err, ErrKeyRefused) {
		t.Error("a value sealed with other additional data opened")
	}
}

// The install's bucket is keyed by its token, whatever registration or case
// it arrives in, and the key is no token.
func TestTheInstallKeyIsTheTokensAlone(t *testing.T) {
	s, _ := NewSealer(sealKey("current"), nil, rand.Reader)
	a, b := s.InstallKey(aKey.Token), s.InstallKey(strings.ToUpper(aKey.Token))
	if a != b || strings.Contains(a, aKey.Token) {
		t.Fatalf("%s, %s", a, b)
	}
	if s.InstallKey(strings.Repeat("cd", 32)) == a {
		t.Fatal("two installs share a bucket")
	}
	other, _ := NewSealer(sealKey("other"), nil, rand.Reader)
	if other.InstallKey(aKey.Token) == a {
		t.Fatal("the bucket key does not depend on the seal key")
	}
}

func TestASealKeyIs32Bytes(t *testing.T) {
	if _, err := NewSealer(make([]byte, 31), nil, rand.Reader); err == nil {
		t.Error("a 31-byte seal key")
	}
	if _, err := NewSealer(make([]byte, 32), make([]byte, 33), rand.Reader); err == nil {
		t.Error("a 33-byte previous seal key")
	}
}
