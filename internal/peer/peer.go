// Package peer reads the address a request came from, the one every
// per-client pace keys on: the transport peer, or, when that peer is a proxy
// the operator listed in SUBSTRATE_TRUSTED_PROXIES, the client the proxies
// name in X-Forwarded-For. The server and the push relay both read it here,
// so a caller is the same caller to both.
package peer

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Address walks X-Forwarded-For from the right, past every trusted hop, and
// answers the first address no trusted proxy is: every entry left of it was
// written by someone the operator does not trust, the client included, so it
// may be forged. An entry that does not parse ends the walk at the last
// trusted hop, which keeps a garbled header from moving the key anywhere a
// client could choose. With no trusted proxy no header is read, so a request
// can never name its own address.
func Address(r *http.Request, trusted []netip.Prefix) string {
	if len(trusted) == 0 {
		return r.RemoteAddr
	}
	peer, ok := parseHop(r.RemoteAddr)
	if !ok || !isTrusted(peer, trusted) {
		return r.RemoteAddr
	}
	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		hop, ok := parseHop(hops[i])
		if !ok {
			break
		}
		client = hop
		if !isTrusted(hop, trusted) {
			break
		}
	}
	return client.String()
}

// Host is an address Address answered without its port: a transport peer
// carries one, and a pace that keyed on it would give every connection of
// one client a bucket of its own.
func Host(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// ParseTrusted parses SUBSTRATE_TRUSTED_PROXIES: a comma-separated list
// whose every entry is a CIDR (`10.42.0.0/16`) or one address, which stands
// for itself alone. Empty entries are skipped.
func ParseTrusted(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		if p, err := netip.ParsePrefix(entry); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("SUBSTRATE_TRUSTED_PROXIES entry %q is neither a CIDR (10.42.0.0/16) nor an address (10.42.0.7)", entry)
		}
		a = a.Unmap()
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

// parseHop reads one address as a proxy writes it: a bare IP, or an IP with a
// port (IPv6 bracketed).
func parseHop(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap(), true
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap(), true
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		if a, err := netip.ParseAddr(host); err == nil {
			return a.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
