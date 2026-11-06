package egress

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

const fetchTimeout = 5 * time.Second

// ConfinedClient is an HTTP client for a URL anybody may name, the same one
// the app document fetch uses: public unicast addresses only, checked on the
// resolved address at connect, no proxy, no redirects and a short timeout.
// A caller still bounds the body it reads.
func ConfinedClient() *http.Client {
	c := &http.Client{Transport: confinedTransport(), Timeout: fetchTimeout}
	c.CheckRedirect = RefuseRedirects
	return c
}

// RefuseRedirects is an http.Client CheckRedirect that follows no redirect:
// the client answers the redirect response itself, and the caller treats it
// as the refusal it is. A request carrying a credential uses it, because Go
// drops Authorization on a redirect to another host but keeps every other
// header, an `x-api-key` among them.
func RefuseRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// CredentialClient is the client a request carrying a credential goes out
// on: transport, no redirect followed (RefuseRedirects), and a redirect
// answer returned as an error rather than handed to a decoder that might read
// its body as a reply. A zero timeout is none.
func CredentialClient(transport http.RoundTripper, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport:     redirectRefusing{base: transport},
		CheckRedirect: RefuseRedirects,
		Timeout:       timeout,
	}
}

// RedirectError is a redirect a CredentialClient refused to follow.
type RedirectError struct {
	Status int
}

func (e *RedirectError) Error() string {
	return fmt.Sprintf("egress: the endpoint answered %d %s, and a request carrying a credential follows no redirect",
		e.Status, http.StatusText(e.Status))
}

// redirectRefusing turns a 3xx answer into a RedirectError.
type redirectRefusing struct {
	base http.RoundTripper
}

func (t redirectRefusing) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := t.base.RoundTrip(req)
	if err != nil {
		return res, err
	}
	if res.StatusCode >= http.StatusMultipleChoices && res.StatusCode < http.StatusBadRequest && res.StatusCode != http.StatusNotModified {
		_ = res.Body.Close()
		return nil, &RedirectError{Status: res.StatusCode}
	}
	return res, nil
}

// confinedTransport dials public unicast addresses and nothing else, checked
// on the RESOLVED address at connect so a DNS answer cannot steer it, and
// uses no proxy.
func confinedTransport() *http.Transport {
	d := &net.Dialer{Timeout: fetchTimeout}
	d.Control = func(_, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil || !Public(ap.Addr()) {
			return errors.New("destination refused")
		}
		return nil
	}
	return &http.Transport{
		Proxy:                  nil,
		DialContext:            d.DialContext,
		TLSHandshakeTimeout:    fetchTimeout,
		ResponseHeaderTimeout:  fetchTimeout,
		MaxResponseHeaderBytes: 16 << 10,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           4,
		IdleConnTimeout:        30 * time.Second,
	}
}

// special is every RFC 6890 special-use range the standard library calls
// global unicast, which Blocked therefore lets through: documentation,
// benchmarking, the IETF protocol block, the reserved class E, and the IPv6
// protocol, documentation, discard and deprecated site-local blocks.
var special = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24",
		"203.0.113.0/24", "240.0.0.0/4",
		"2001::/23", "2001:db8::/32", "3fff::/20", "100::/64", "5f00::/16",
		// Site-local, deprecated (RFC 3879) and still routed on some
		// networks; the standard library calls it global unicast.
		"fec0::/10",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// Public reports whether a document fetch may connect to addr.
func Public(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("")
	if Blocked(addr) {
		return false
	}
	candidates := []netip.Addr{addr}
	if addr.Is6() {
		b := addr.As16()
		switch {
		case nat64.Contains(addr):
			candidates = append(candidates, netip.AddrFrom4([4]byte(b[12:16])))
		case sixToFour.Contains(addr):
			candidates = append(candidates, netip.AddrFrom4([4]byte(b[2:6])))
		}
	}
	for _, a := range candidates {
		for _, p := range special {
			if p.Contains(a) {
				return false
			}
		}
	}
	return true
}
