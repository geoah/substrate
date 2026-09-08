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

// Every framing, opened with and without the marker: the marker refuses the
// plain framing and the host-key fallback, names what it found and what it
// expected, and changes nothing for a payload under the DEK.
func TestOpenRepoPayloadRefusesLegacyFormsOnceMarked(t *testing.T) {
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

	opens := func(payload []byte, dekOnly bool) error {
		got, err := openRepoPayload(payload, dek, host, aad, dekOnly)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, raw) {
			t.Fatalf("payload %q opened to %q", payload[0], got)
		}
		return nil
	}

	// Under the DEK, marked or not: the two forms the store writes and wrote.
	for _, p := range [][]byte{bound, unbound} {
		for _, marked := range []bool{false, true} {
			if err := opens(p, marked); err != nil {
				t.Fatalf("payload %q under the DEK did not open (marked=%v): %v", p[0], marked, err)
			}
		}
	}
	// Unmarked: the legacy forms open, plain as is and host-key by fallback.
	if err := opens(plain, false); err != nil {
		t.Fatalf("an unmarked repository refused a plain payload: %v", err)
	}
	if err := opens(hostBound, false); err != nil {
		t.Fatalf("an unmarked repository refused the host-key fallback: %v", err)
	}
	// Marked: both refused, by name.
	err = opens(plain, true)
	if !errors.Is(err, errPlainRefused) || !strings.Contains(err.Error(), "'p'") || !strings.Contains(err.Error(), "repository DEK") {
		t.Fatalf("a marked repository did not refuse the plain framing by name: %v", err)
	}
	err = opens(hostBound, true)
	if err == nil || !strings.Contains(err.Error(), `'a'`) || !strings.Contains(err.Error(), "repository DEK") || !strings.Contains(err.Error(), "host-key fallback is refused") {
		t.Fatalf("a marked repository did not refuse the host-key payload by name: %v", err)
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
