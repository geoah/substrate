package egress

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestGuard(t *testing.T) {
	allow := []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	cases := []struct {
		name    string
		address string
		allow   []netip.Prefix
		blocked bool
	}{
		{"loopback refused", "127.0.0.1:443", nil, true},
		{"rfc1918 refused", "10.1.2.3:443", nil, true},
		{"metadata refused", "169.254.169.254:80", nil, true},
		{"cgnat refused", "100.64.0.1:443", nil, true},
		{"ipv6 loopback refused", "[::1]:443", nil, true},
		{"ipv4-mapped loopback refused", "[::ffff:127.0.0.1]:443", nil, true},
		{"public allowed", "1.1.1.1:443", nil, false},
		{"public ipv6 allowed", "[2606:4700:4700::1111]:443", nil, false},
		{"unparseable refused", "not-an-address", nil, true},
		{"allowlisted loopback permitted", "127.0.0.1:11434", allow, false},
		{"allowlist does not widen to other private", "10.1.2.3:443", allow, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := guard(c.address, c.allow)
			if c.blocked && err == nil {
				t.Fatalf("guard(%q) = nil, want an egress-blocked error", c.address)
			}
			if !c.blocked && err != nil {
				t.Fatalf("guard(%q) = %v, want nil", c.address, err)
			}
			if c.blocked && err != nil && !strings.Contains(err.Error(), "egress blocked") {
				t.Fatalf("guard(%q) error = %q, want it to name egress blocked", c.address, err)
			}
		})
	}
}

func TestParseAllow(t *testing.T) {
	got := parseAllow(" 127.0.0.0/8 , 10.0.0.5 , , garbage , ::1 ")
	want := map[string]bool{
		"127.0.0.0/8": true,
		"10.0.0.5/32": true,
		"::1/128":     true,
	}
	if len(got) != len(want) {
		t.Fatalf("parseAllow returned %d prefixes (%v), want %d", len(got), got, len(want))
	}
	for _, p := range got {
		if !want[p.String()] {
			t.Fatalf("parseAllow returned unexpected prefix %s", p)
		}
	}
	if parseAllow("") != nil {
		t.Fatal("parseAllow(\"\") must be nil: no allowlist means block every private range")
	}
}

// A client built on the default transport (no allowlist) refuses a connection to
// a loopback server at DIAL time, and the same transport with the loopback range
// allowlisted lets it through. httptest binds 127.0.0.1, so this exercises the
// Control hook against the exact range #241's escape opens.
func TestTransportGatesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	blocked := &http.Client{Transport: newTransport(nil)}
	if resp, err := blocked.Get(srv.URL); err == nil {
		_ = resp.Body.Close()
		t.Fatal("a loopback dial returned no error: the gate did not fire")
	} else if !strings.Contains(err.Error(), "egress blocked") {
		t.Fatalf("loopback dial error = %v, want it to name egress blocked", err)
	}

	allowed := &http.Client{Transport: newTransport([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})}
	resp, err := allowed.Get(srv.URL)
	if err != nil {
		t.Fatalf("an allowlisted loopback dial was refused: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}
