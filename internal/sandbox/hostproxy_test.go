package sandbox

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
