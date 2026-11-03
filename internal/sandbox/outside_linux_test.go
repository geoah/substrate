//go:build linux

package sandbox

import (
	"net/netip"
	"testing"
)

func TestOutsideConnectGateAdmitsOnlyItsProxy(t *testing.T) {
	g := &connectGate{proxyAddress: "127.0.0.1:43210", resolvers: []netip.Addr{netip.MustParseAddr("127.0.0.53")}}
	for _, address := range []string{"127.0.0.1:43211", "127.0.0.53:53", "1.1.1.1:443", "10.0.0.1:80"} {
		ap := netip.MustParseAddrPort(address)
		if g.allowDest(ap.Addr(), ap.Port()) {
			t.Errorf("admitted %s", address)
		}
	}
	if !g.allowDest(netip.MustParseAddr("127.0.0.1"), 43210) {
		t.Fatal("proxy refused")
	}
}
