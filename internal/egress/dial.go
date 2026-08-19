package egress

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"time"
)

// A server dial of a repository-chosen URL is issue #241's SSRF read primitive:
// a repository owner writes an llmprovider row, so the baseURL the engine dials
// is attacker-chosen, and a completion's body returns to the dispatching user.
// A string check on the URL is defeated by DNS (a name that resolves to a
// private address on the connect the check did not see), so the check runs in
// the dialer's Control hook, on the RESOLVED address, once per candidate before
// connect. Every server client built on Transport is confined to public
// destinations Blocked does not mark as the deployment's own.

// Transport returns an http.Transport whose dials are confined to public
// destinations. It reads the operator allowlist (SUBSTRATE_EGRESS_ALLOW) once,
// at construction, and bakes the resolved prefixes into the dialer, so a caller
// that builds a client per request (the engine builds one per provider dial)
// picks up the process's configured escape.
func Transport() *http.Transport {
	return newTransport(operatorAllow())
}

// newTransport is Transport with the allowlist supplied rather than read from
// the environment, so a test can pin the escape it exercises.
func newTransport(allow []netip.Prefix) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	d.Control = func(_, address string, _ syscall.RawConn) error {
		return guard(address, allow)
	}
	t.DialContext = d.DialContext
	return t
}

// guard is the dialer Control decision for one resolved address: the address a
// net.Dialer hands it is the IP:port it is about to connect, already resolved,
// so a hostname that resolves to a private address is caught here rather than
// at the URL string. An address it cannot parse is refused; a destination
// Blocked marks as the deployment's own is refused unless an
// operator-allowlisted prefix contains it.
func guard(address string, allow []netip.Prefix) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("egress blocked: cannot parse dial address %q", address)
	}
	addr := ap.Addr().Unmap()
	for _, p := range allow {
		if p.Contains(addr) {
			return nil
		}
	}
	if Blocked(addr) {
		return fmt.Errorf("egress blocked: %s", address)
	}
	return nil
}

// operatorAllow reads SUBSTRATE_EGRESS_ALLOW, the operator's escape from the
// private-range block for a server dial: a comma-separated list of CIDRs (or
// bare addresses) a repository-chosen provider URL may resolve to even though
// Blocked marks it as the deployment's own. It is how a deployment permits a
// local provider (a loopback Ollama, issue #241's documented escape) without
// opening the whole private range. It mirrors the sandbox connect gate's
// SUBSTRATE_SANDBOX_EGRESS_ALLOW, the same escape for a network function body;
// the two are separate variables because the two gates are separate planes.
func operatorAllow() []netip.Prefix {
	return parseAllow(os.Getenv("SUBSTRATE_EGRESS_ALLOW"))
}

// parseAllow turns the comma-separated allowlist into prefixes, dropping a token
// that is neither a CIDR nor a bare address rather than failing the dial: a
// malformed entry tightens the gate, it does not open it.
func parseAllow(raw string) []netip.Prefix {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []netip.Prefix
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if p, err := netip.ParsePrefix(tok); err == nil {
			out = append(out, p.Masked())
			continue
		}
		if a, err := netip.ParseAddr(tok); err == nil {
			out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
		}
	}
	return out
}
