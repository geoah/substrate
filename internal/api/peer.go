package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// peerAddress records the address every per-client pace keys on: the
// transport peer, or, when that peer is a proxy the operator listed in
// SUBSTRATE_TRUSTED_PROXIES, the client the proxies name in X-Forwarded-For.
// With no trusted proxy no header is read, so a request can never name its
// own address.
func peerAddress(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), ctxKeyPeer, clientAddress(r, trusted))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// clientAddress walks X-Forwarded-For from the right, past every trusted hop,
// and answers the first address no trusted proxy is: every entry left of it
// was written by someone the operator does not trust, the client included, so
// it may be forged. An entry that does not parse ends the walk at the last
// trusted hop, which keeps a garbled header from moving the key anywhere a
// client could choose.
func clientAddress(r *http.Request, trusted []netip.Prefix) string {
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
