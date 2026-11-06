package sandbox

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOutsideHostPatterns(t *testing.T) {
	for _, tc := range []struct {
		host string
		want bool
	}{
		{"api.example.com", true}, {"a.cdn.example.com", true}, {"cdn.example.com", false}, {"notexample.com", false}, {"other.example.com", false}, {"127.0.0.1", false}, {"API.EXAMPLE.COM.", true},
	} {
		if got := declaredHost(tc.host, []string{"api.example.com", "*.cdn.example.com"}); got != tc.want {
			t.Errorf("%s: got %v", tc.host, got)
		}
	}
}

func TestOutsideProxyRefusesUndeclaredHostsAndIPs(t *testing.T) {
	p, err := newHostProxy([]string{"api.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	u, err := url.Parse("http://" + p.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(u)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	for _, address := range []string{"http://other.example.com/", "http://127.0.0.1/", "https://other.example.com/", "https://127.0.0.1/"} {
		response, err := client.Get(address)
		if err != nil {
			if response != nil {
				response.Body.Close()
			}
			continue
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("%s: got %d", address, response.StatusCode)
		}
	}
}

func TestOutsideProxyKeepsHostChecksWhenPrivateAddressesAreAllowed(t *testing.T) {
	t.Setenv(EgressAllowVar, "127.0.0.1,::1")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://other.example.com/", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("permitted"))
	}))
	defer target.Close()
	p, err := newHostProxy([]string{"localhost"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	proxyURL, err := url.Parse("http://" + p.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	address := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	for _, tc := range []struct {
		url    string
		status int
	}{{address, http.StatusOK}, {target.URL, http.StatusForbidden}, {address + "/redirect", http.StatusForbidden}} {
		response, err := client.Get(tc.url)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Errorf("%s: %d", tc.url, response.StatusCode)
		}
	}
}

// A confined egress holds an outside body's proxy too: it dials a declared
// host only at an address inside the prefixes, whatever the allowlist says.
func TestOutsideProxyDialsOnlyInsideAConfinedEgress(t *testing.T) {
	t.Setenv(EgressAllowVar, "127.0.0.1,::1")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("permitted"))
	}))
	defer target.Close()
	address := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	for _, tc := range []struct {
		only   string
		status int
	}{{"127.0.0.0/8", http.StatusOK}, {"192.0.2.0/24", http.StatusForbidden}} {
		p, err := newHostProxy([]string{"localhost"}, netip.MustParsePrefix(tc.only))
		if err != nil {
			t.Fatal(err)
		}
		proxyURL, err := url.Parse("http://" + p.listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(proxyURL)
		response, err := (&http.Client{Transport: transport}).Get(address)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		transport.CloseIdleConnections()
		p.Close()
		if response.StatusCode != tc.status {
			t.Errorf("confined to %s: %d %s, want %d", tc.only, response.StatusCode, body, tc.status)
		}
		if tc.status == http.StatusForbidden && !strings.Contains(string(body), "egress is confined") {
			t.Errorf("confined to %s: the refusal %q does not say the egress is confined", tc.only, body)
		}
	}
}

// A confined proxy asks no name server for a declared name the hosts file
// does not map: the request fails with the confinement's refusal, and the
// system resolver, stood in for here by one that counts and refuses, is never
// asked, over plain HTTP or through CONNECT. The stand-in also keeps a run
// against a proxy that would ask it from sending a query anywhere.
func TestAConfinedProxyAsksNoNameServer(t *testing.T) {
	var asked atomic.Int32
	system := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		asked.Add(1)
		return nil, errors.New("the system resolver was asked")
	}}
	t.Cleanup(func() { net.DefaultResolver = system })

	const unmapped = "unmapped.invalid."
	p, err := newHostProxy([]string{unmapped}, netip.MustParsePrefix("127.0.0.0/8"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	proxyURL, err := url.Parse("http://" + p.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}

	response, err := client.Get("http://" + unmapped + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden || !strings.Contains(string(body), errConfinedLookup.Error()) {
		t.Errorf("a declared, unmapped name over HTTP = %d %q, want 403 naming the confined lookup", response.StatusCode, body)
	}
	if response, err := client.Get("https://" + unmapped + "/"); err == nil {
		response.Body.Close()
		t.Errorf("a declared, unmapped name through CONNECT = %d, want the tunnel refused", response.StatusCode)
	}
	if n := asked.Load(); n != 0 {
		t.Fatalf("the system resolver was asked %d times for a confined proxy's name", n)
	}
}
