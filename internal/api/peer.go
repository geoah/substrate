package api

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/geoah/substrate/internal/peer"
)

// peerAddress records the address every per-client pace keys on
// (peer.Address): the transport peer, or the client a proxy the operator
// listed in SUBSTRATE_TRUSTED_PROXIES names in X-Forwarded-For.
func peerAddress(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), ctxKeyPeer, peer.Address(r, trusted))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
