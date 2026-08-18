//go:build linux

package sandbox

import (
	"net/netip"
	"testing"

	"golang.org/x/sys/unix"
)

// The connect-destination decision, tested without a kernel: a resolver is
// allowed on port 53 (so DNS through a loopback stub works), the deployment's
// own ranges are refused, and the public internet is allowed.
func TestConnectGateAllowDest(t *testing.T) {
	g := &connectGate{resolvers: []netip.Addr{
		netip.MustParseAddr("127.0.0.53"),
		netip.MustParseAddr("127.0.0.11"),
	}}
	cases := []struct {
		addr  string
		port  uint16
		allow bool
	}{
		{"127.0.0.53", 53, true},       // the resolver, DNS port
		{"127.0.0.11", 53, true},       // Docker's resolver, DNS port
		{"127.0.0.53", 5432, false},    // the resolver IP, but not port 53
		{"127.0.0.1", 5432, false},     // loopback: the substrate's own ports
		{"10.0.0.5", 5432, false},      // RFC1918: the compose Postgres
		{"172.16.0.2", 5432, false},    // RFC1918
		{"169.254.169.254", 80, false}, // cloud metadata
		{"8.8.8.8", 53, true},          // a PUBLIC resolver is allowed as public
		{"1.1.1.1", 443, true},         // the public internet
	}
	for _, c := range cases {
		got := g.allowDest(netip.MustParseAddr(c.addr), c.port)
		if got != c.allow {
			t.Errorf("allowDest(%s, %d) = %v, want %v", c.addr, c.port, got, c.allow)
		}
	}
}

// The notify filter is hand-assembled, so its shape is asserted directly: it
// must end in an ALLOW then a USER_NOTIF terminal, and every jump must land
// inside the program and forward.
func TestConnectNotifyFilterAssembles(t *testing.T) {
	prog := buildConnectNotifyFilter()
	if len(prog) < 4 {
		t.Fatalf("suspiciously short program (%d)", len(prog))
	}
	for i, insn := range prog {
		if insn.Code&0x07 != 0x05 { // not BPF_JMP
			continue
		}
		for _, off := range []uint8{insn.JT, insn.JF} {
			if target := i + 1 + int(off); target >= len(prog) {
				t.Fatalf("instruction %d jumps to %d, past the end (%d)", i, target, len(prog))
			}
		}
	}
	last := prog[len(prog)-2:]
	if last[0].Code != bpfRetK || last[0].K != seccompRetAllow {
		t.Fatalf("second-to-last terminal = %#x, want ALLOW %#x", last[0].K, seccompRetAllow)
	}
	if last[1].Code != bpfRetK || last[1].K != uint32(unix.SECCOMP_RET_USER_NOTIF) {
		t.Fatalf("last terminal = %#x, want USER_NOTIF %#x", last[1].K, uint32(unix.SECCOMP_RET_USER_NOTIF))
	}
}
