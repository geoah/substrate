// Package egress classifies a destination address as one the deployment's own
// network holds rather than the public internet. A function body that declares
// permissions.network is meant to reach the internet, not the substrate's own
// Postgres or metadata endpoint (issue #239); a server dial of a
// repository-chosen URL is the same question from the other side (issue #241).
// Both ask Blocked.
package egress

import "net/netip"

// Blocked reports whether addr belongs to a range a network-granted body (or a
// server dial) must not reach: loopback, the unspecified address, link-local
// (the cloud metadata address 169.254.169.254 is one), multicast, and the
// private RFC1918 and IPv6 unique-local ranges. A global-unicast public address
// returns false.
//
// An invalid address is Blocked: a destination that cannot be classified is not
// one to allow. An IPv4-mapped IPv6 address is judged as its IPv4 self, so
// ::ffff:127.0.0.1 cannot smuggle loopback past the IPv4 checks.
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
		addr.IsPrivate():
		return true
	}
	// Everything the standard library does not call global unicast is reserved
	// space (documentation ranges, benchmarking, future use); fail closed on it.
	return !addr.IsGlobalUnicast()
}
