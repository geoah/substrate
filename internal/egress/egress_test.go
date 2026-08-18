package egress

import (
	"net/netip"
	"testing"
)

func TestBlocked(t *testing.T) {
	cases := []struct {
		addr    string
		blocked bool
	}{
		// The deployment's own network and the metadata endpoint.
		{"127.0.0.1", true},
		{"::1", true},
		{"0.0.0.0", true},
		{"10.1.2.3", true},          // RFC1918
		{"172.16.0.5", true},        // RFC1918, the compose DB range
		{"192.168.1.1", true},       // RFC1918
		{"169.254.169.254", true},   // cloud metadata (link-local)
		{"fe80::1", true},           // IPv6 link-local
		{"fc00::1", true},           // IPv6 unique-local
		{"224.0.0.1", true},         // multicast
		{"100.64.0.1", true},        // carrier-grade NAT (a tailnet lives here)
		{"100.81.64.27", true},      // CGNAT, mid-range
		{"0.1.2.3", true},           // the rest of 0.0.0.0/8, not just 0.0.0.0
		{"::ffff:127.0.0.1", true},  // IPv4-mapped loopback must not slip through
		{"::ffff:100.64.0.1", true}, // IPv4-mapped CGNAT must not slip through
		// The public internet.
		{"1.1.1.1", false},
		{"8.8.8.8", false},
		{"93.184.216.34", false}, // example.com
		{"2606:4700:4700::1111", false},
	}
	for _, c := range cases {
		addr, err := netip.ParseAddr(c.addr)
		if err != nil {
			t.Fatalf("parse %s: %v", c.addr, err)
		}
		if got := Blocked(addr); got != c.blocked {
			t.Errorf("Blocked(%s) = %v, want %v", c.addr, got, c.blocked)
		}
	}
}

func TestBlockedInvalid(t *testing.T) {
	if !Blocked(netip.Addr{}) {
		t.Fatal("an invalid address must be blocked")
	}
}
