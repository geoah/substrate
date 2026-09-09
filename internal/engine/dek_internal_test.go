package engine

// The open order of a repository's sealed payloads under the DEK-only marker
// (decision 0059), and the host-key id beside each DEK wrap. No database: the
// framing, the keys and the marker are all in hand.

import (
	"bytes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestHostKeyIDNamesAKeyWithoutRevealingIt(t *testing.T) {
	t.Parallel()
	a := bytes.Repeat([]byte{1}, 32)
	b := bytes.Repeat([]byte{2}, 32)
	id := hostKeyID(a)
	if len(id) != 16 {
		t.Fatalf("id %q is not 16 hex digits", id)
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Fatalf("id %q is not hex: %v", id, err)
	}
	if hostKeyID(a) != id {
		t.Fatal("the id of one key changed between calls")
	}
	if hostKeyID(b) == id {
		t.Fatal("two keys share an id")
	}
	if strings.Contains(hex.EncodeToString(a), id) {
		t.Fatal("the id is a slice of the key")
	}
	if hostKeyID(nil) != "" {
		t.Fatalf("the keyless service names a key: %q", hostKeyID(nil))
	}
}

// One open order: the DEK, bound or unbound, and nothing else. A plain
// payload and one sealed under the host key are refused, by name, and the
// refusal says what it found and what key it expected.
func TestOpenRepoPayloadOpensUnderTheDEKAlone(t *testing.T) {
	t.Parallel()
	dek := bytes.Repeat([]byte{3}, 32)
	host := bytes.Repeat([]byte{4}, 32)
	aad := sealedAAD("secret:abc", "ada.example.com/core/llmprovider", "a")
	raw := []byte(`"material"`)
	dekAEAD, err := newAEAD(dek)
	if err != nil {
		t.Fatal(err)
	}
	hostAEAD, err := newAEAD(host)
	if err != nil {
		t.Fatal(err)
	}
	seal := func(a cipher.AEAD, bind []byte) []byte {
		out, err := sealWith(a, raw, bind)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	bound := seal(dekAEAD, aad)
	unbound := seal(dekAEAD, nil)
	hostBound := seal(hostAEAD, aad)
	plain := append([]byte{credPlain}, raw...)

	opens := func(payload []byte) error {
		got, err := openRepoPayload(payload, dek, aad)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, raw) {
			t.Fatalf("payload %q opened to %q", payload[0], got)
		}
		return nil
	}

	// Under the DEK, bound and unbound alike.
	for _, p := range [][]byte{bound, unbound} {
		if err := opens(p); err != nil {
			t.Fatalf("payload %q under the DEK did not open: %v", p[0], err)
		}
	}
	// A plain payload and one under the host key are refused, by name.
	err = opens(plain)
	if !errors.Is(err, errPlainRefused) || !strings.Contains(err.Error(), "'p'") || !strings.Contains(err.Error(), "DEK") {
		t.Fatalf("the plain framing was not refused by name: %v", err)
	}
	err = opens(hostBound)
	if err == nil || !strings.Contains(err.Error(), `'a'`) || !strings.Contains(err.Error(), "repository DEK") {
		t.Fatalf("the host-key payload was not refused by name: %v", err)
	}
	// The proof function opens under its key alone and never reads plain.
	if _, err := OpenPayloadWithKey(dek, bound, aad); err != nil {
		t.Fatalf("OpenPayloadWithKey under the right key: %v", err)
	}
	if _, err := OpenPayloadWithKey(dek, plain, aad); !errors.Is(err, errPlainRefused) {
		t.Fatalf("OpenPayloadWithKey read a plain payload: %v", err)
	}
	if _, err := OpenPayloadWithKey(dek, hostBound, aad); err == nil {
		t.Fatal("OpenPayloadWithKey opened a host-key payload under the DEK")
	}
}
