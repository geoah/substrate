package engine

// A time zone value names a zone that is the same on every host, so the two
// spellings time.LoadLocation reads by the host's own lights are refused.

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func TestTimezoneRefusesLocalAndTheEmptyName(t *testing.T) {
	t.Parallel()
	p := &vocabulary.Property{Name: "zone", Datatype: vocabulary.DatatypeTimezone}
	for _, c := range []struct {
		value string
		says  string
	}{
		{"", "the empty string names no zone"},
		{"Local", `"Local" is the server's own zone`},
		{"Mars/Olympus_Mons", "expected an IANA time zone name"},
	} {
		_, err := coerceScalar(p, c.value)
		if err == nil {
			t.Errorf("zone %q admitted, want it refused", c.value)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("zone %q refused with %q, want it to say %q", c.value, err, c.says)
		}
	}
	for _, zone := range []string{"UTC", "Europe/Athens", "America/New_York"} {
		if _, err := coerceScalar(p, zone); err != nil {
			t.Errorf("zone %q refused: %v", zone, err)
		}
	}
}
