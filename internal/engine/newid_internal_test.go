package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geoah/substrate/internal/vocabulary"
)

// A minted id is a UUIDv7 in 26 lowercase Crockford characters: a legal
// record id, decodable back to the UUID, and ordered by when it was minted.
func TestNewIDIsAUUIDv7InCrockfordBase32(t *testing.T) {
	prev := ""
	for range 1000 {
		id, err := newID()
		if err != nil {
			t.Fatal(err)
		}
		if !vocabulary.MintedID(id) || !vocabulary.ValidID(id) {
			t.Fatalf("minted %q", id)
		}
		u := decodeCrockford(t, id)
		if u.Version() != 7 || u.Variant() != uuid.RFC4122 {
			t.Fatalf("%q decodes to %s, version %d", id, u, u.Version())
		}
		if sec, nsec := u.Time().UnixTime(); time.Since(time.Unix(sec, nsec)) > time.Minute {
			t.Fatalf("%q carries the time %v", id, time.Unix(sec, nsec))
		}
		// uuid.NewV7 keeps ids minted in one millisecond monotonic, so the
		// string order is the mint order.
		if id <= prev {
			t.Fatalf("%q minted after %q sorts before it", id, prev)
		}
		prev = id
	}
}

// The encoding is pinned on known bytes, so a change to it shows here
// rather than as ids that stop decoding.
func TestEncodeCrockfordKnownValues(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"00000000-0000-0000-0000-000000000000", "00000000000000000000000000"},
		{"ffffffff-ffff-ffff-ffff-ffffffffffff", "7zzzzzzzzzzzzzzzzzzzzzzzzz"},
		// An example from the TypeID specification's valid.yml.
		{"0188bac7-4afa-78aa-bc3b-bd1eef28d881", "01h2xcejqtf2nbrexx3vqjhp41"},
	} {
		if got := vocabulary.EncodeMintedID(uuid.MustParse(tc.in)); got != tc.want {
			t.Errorf("EncodeMintedID(%s) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func decodeCrockford(t *testing.T, s string) uuid.UUID {
	t.Helper()
	var u uuid.UUID
	bits := make([]byte, 0, 130)
	for i := range len(s) {
		v := strings.IndexByte("0123456789abcdefghjkmnpqrstvwxyz", s[i])
		if v < 0 {
			t.Fatalf("%q is not Crockford base32", s)
		}
		for j := 4; j >= 0; j-- {
			bits = append(bits, byte(v>>j)&1)
		}
	}
	bits = bits[2:] // the two padding bits
	for k, b := range bits {
		u[k/8] |= b << (7 - k%8)
	}
	return u
}
