// Package egress classifies a destination address as one the deployment's own
// network holds rather than the public internet. A function body that declares
// permissions.network is meant to reach the internet, not the substrate's own
// Postgres or metadata endpoint (issue #239); a server dial of a
// repository-chosen URL is the same question from the other side (issue #241).
// Both ask Blocked.
package egress

import "net/netip"

// cgnat is the carrier-grade NAT range (RFC 6598). The standard library calls
// it global unicast, so it has to be named here. It is where a tailnet
// addresses its nodes, so a body left able to reach it reaches every service on
// the deployment's own tailnet, which is exactly the reachability #239 closes.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// Blocked reports whether addr belongs to a range a network-granted body (or a
// server dial) must not reach: loopback, the unspecified block (0.0.0.0/8),
// link-local (the cloud metadata address 169.254.169.254 is one), multicast,
// the private RFC1918 and IPv6 unique-local ranges, and carrier-grade NAT
// (100.64.0.0/10, where a tailnet lives). A global-unicast public address
// returns false.
//
// An invalid address is Blocked: a destination that cannot be classified is not
// one to allow. An IPv4-mapped IPv6 address is judged as its IPv4 self, so
// ::ffff:127.0.0.1 cannot smuggle loopback, nor ::ffff:100.64.x.x smuggle
// CGNAT, past the IPv4 checks.
func Blocked(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.Unmap()
	switch {
	case addr.IsLoopback(),
		addr.IsUnspecified(),
		addr.IsLinkLocalUnicast(),
		addr.IsLinkLocalMulticast(),
		addr.IsInterfaceLocalMulticast(),
		addr.IsMulticast(),
		addr.IsPrivate(),
		cgnat.Contains(addr):
		return true
	}
	// The whole 0.0.0.0/8 is "this network": IsUnspecified catches only
	// 0.0.0.0 itself, so name the block. It is never a legitimate egress target.
	if addr.Is4() && addr.As4()[0] == 0 {
		return true
	}
	// Everything the standard library does not call global unicast is reserved
	// space (documentation ranges, benchmarking, future use); fail closed on it.
	return !addr.IsGlobalUnicast()
}
