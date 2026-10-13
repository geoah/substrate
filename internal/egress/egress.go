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

// The IPv6 ranges that carry an IPv4 address. A gateway on the path delivers
// a packet for one of them to the IPv4 inside, so each is judged as that
// IPv4, or refused whole where the IPv4 cannot be read out of it.
var (
	// nat64 is the well-known NAT64 prefix (RFC 6052): 64:ff9b::a.b.c.d
	// reaches a.b.c.d through the network's NAT64 gateway.
	nat64 = netip.MustParsePrefix("64:ff9b::/96")
	// nat64Local is the local-use NAT64 prefix (RFC 8215). The deployment
	// chooses where in it the IPv4 sits, so nothing here can read it out.
	nat64Local = netip.MustParsePrefix("64:ff9b:1::/48")
	// sixToFour is 6to4 (RFC 3056): 2002:AABB:CCDD:: is reached through the
	// IPv4 AA.BB.CC.DD.
	sixToFour = netip.MustParsePrefix("2002::/16")
	// ipv4Compatible is the deprecated IPv4-compatible block (RFC 4291
	// 2.5.5.1), ::a.b.c.d, which no host has a use for.
	ipv4Compatible = netip.MustParsePrefix("::/96")
)

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
// CGNAT, past the IPv4 checks. A NAT64 or 6to4 address is judged as the IPv4
// it carries, for the same reason: on a network with the gateway,
// 64:ff9b::a9fe:a9fe is the metadata address. The local-use NAT64 prefix and
// the IPv4-compatible block are refused whole.
func Blocked(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	// A zone names an interface, not a different address, and a prefix never
	// contains a zoned one, so it would carry 64:ff9b::a9fe:a9fe%eth0 past
	// every prefix test below.
	addr = addr.Unmap().WithZone("")
	if addr.Is6() {
		b := addr.As16()
		switch {
		case nat64Local.Contains(addr), ipv4Compatible.Contains(addr):
			return true
		case nat64.Contains(addr):
			return Blocked(netip.AddrFrom4([4]byte(b[12:16])))
		case sixToFour.Contains(addr):
			return Blocked(netip.AddrFrom4([4]byte(b[2:6])))
		}
	}
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
