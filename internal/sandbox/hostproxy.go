package sandbox

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/geoah/substrate/internal/egress"
)

// hostProxy is a per-body HTTP proxy. The kernel gate admits only this
// listener; resolution happens here, after the requested name is checked.
// A raw address, alternate name, redirect or changed DNS answer cannot widen
// the declared hosts. CONNECT carries arbitrary TCP protocols.
type hostProxy struct {
	mu        sync.Mutex
	tunnels   map[net.Conn]bool
	closed    bool
	cancel    context.CancelFunc
	server    *http.Server
	listener  net.Listener
	transport *http.Transport
}

func declaredHost(host string, patterns []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if net.ParseIP(host) != nil || host == "" {
		return false
	}
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSuffix(pattern, "."))
		if host == pattern || (strings.HasPrefix(pattern, "*.") && strings.HasSuffix(host, pattern[1:]) && host != pattern[2:]) {
			return true
		}
	}
	return false
}

// newHostProxy starts the proxy for one outside body declaring hosts. only is
// the policy's EgressOnly: when set, the proxy dials no address outside it,
// whatever the host checks and the allowlist say.
func newHostProxy(hosts []string, only ...netip.Prefix) (*hostProxy, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	allow, _ := egress.ParseAllow(os.Getenv(EgressAllowVar))
	dialer := &net.Dialer{Timeout: 30 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil {
			return err
		}
		if len(only) > 0 {
			if !egressConfinedTo(only, ap.Addr().Unmap()) {
				return fmt.Errorf("outside code cannot reach %s: egress is confined", address)
			}
			return nil
		}
		for _, p := range allow {
			if p.Contains(ap.Addr().Unmap()) {
				return nil
			}
		}
		if egress.Blocked(ap.Addr()) {
			return fmt.Errorf("outside code cannot reach deployment address %s", address)
		}
		return nil
	}}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || !declaredHost(host, hosts) {
			return nil, fmt.Errorf("outside code has not declared host %q", host)
		}
		return dialer.DialContext(ctx, network, address)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dial
	ctx, cancel := context.WithCancel(context.Background())
	p := &hostProxy{listener: ln, transport: transport, tunnels: map[net.Conn]bool{}, cancel: cancel}
	p.server = &http.Server{ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			target, err := dial(r.Context(), "tcp", r.Host)
			if err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			defer target.Close()
			client, buffered, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer client.Close()
			p.mu.Lock()
			if p.closed {
				p.mu.Unlock()
				return
			}
			p.tunnels[client] = true
			p.tunnels[target] = true
			p.mu.Unlock()
			defer func() { p.mu.Lock(); delete(p.tunnels, client); delete(p.tunnels, target); p.mu.Unlock() }()
			_ = client.SetDeadline(time.Now().Add(5 * time.Minute))
			_ = target.SetDeadline(time.Now().Add(5 * time.Minute))
			if _, err = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
				return
			}
			if err = buffered.Flush(); err != nil {
				return
			}
			done := make(chan struct{})
			go func() { _, _ = io.Copy(target, buffered); _ = target.Close(); close(done) }()
			_, _ = io.Copy(client, target)
			_ = client.Close()
			_ = target.Close()
			<-done
			return
		}
		if r.URL.Scheme != "http" || !declaredHost(r.URL.Hostname(), hosts) || r.Host != r.URL.Host {
			http.Error(w, "outside code requested an undeclared host", http.StatusForbidden)
			return
		}
		req := r.Clone(r.Context())
		req.RequestURI = ""
		req.Header.Del("Proxy-Authorization")
		req.Header.Del("Proxy-Connection")
		response, err := transport.RoundTrip(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	})}
	go func() { _ = p.server.Serve(ln) }()
	return p, nil
}

func (p *hostProxy) Close() error {
	p.cancel()
	p.mu.Lock()
	p.closed = true
	for conn := range p.tunnels {
		_ = conn.Close()
	}
	p.mu.Unlock()
	p.transport.CloseIdleConnections()
	return p.server.Close()
}

// egressConfinedTo reports whether addr lies in one of a confined egress's
// prefixes.
func egressConfinedTo(only []netip.Prefix, addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("")
	for _, p := range only {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
