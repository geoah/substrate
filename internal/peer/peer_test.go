package peer

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestAddressReadsForwardedForOnlyFromATrustedProxy(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.42.0.0/16"), netip.MustParsePrefix("fd00::/8")}
	cases := []struct {
		name    string
		trusted []netip.Prefix
		peer    string
		xff     []string
		want    string
	}{
		{"no trusted proxy reads no header", nil, "10.42.0.7:5000", []string{"203.0.113.9"}, "10.42.0.7:5000"},
		{"an untrusted peer is its own client", trusted, "198.51.100.4:5000", []string{"203.0.113.9"}, "198.51.100.4:5000"},
		{"a trusted proxy names the client", trusted, "10.42.0.7:5000", []string{"203.0.113.9"}, "203.0.113.9"},
		{"a client's own entry left of the proxy's is ignored", trusted, "10.42.0.7:5000", []string{"192.0.2.1, 203.0.113.9"}, "203.0.113.9"},
		{"trusted hops are walked past", trusted, "10.42.0.7:5000", []string{"203.0.113.9, 10.42.3.3"}, "203.0.113.9"},
		{"several headers read as one list", trusted, "10.42.0.7:5000", []string{"192.0.2.1", "203.0.113.9"}, "203.0.113.9"},
		{"a garbled hop stops at the last trusted one", trusted, "10.42.0.7:5000", []string{"203.0.113.9, nonsense"}, "10.42.0.7"},
		{"no header keeps the proxy", trusted, "10.42.0.7:5000", nil, "10.42.0.7"},
		{"an entry with a port", trusted, "10.42.0.7:5000", []string{"203.0.113.9:443"}, "203.0.113.9"},
		{"IPv6 behind an IPv6 proxy", trusted, "[fd00::1]:5000", []string{"2001:db8::5"}, "2001:db8::5"},
		{"every hop trusted ends at the leftmost", trusted, "10.42.0.7:5000", []string{"10.42.9.9, 10.42.3.3"}, "10.42.9.9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = c.peer
			for _, v := range c.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := Address(r, c.trusted); got != c.want {
				t.Fatalf("Address = %q, want %q", got, c.want)
			}
		})
	}
}

func TestHostDropsThePort(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.9:5000":  "203.0.113.9",
		"[2001:db8::5]:443": "2001:db8::5",
		"203.0.113.9":       "203.0.113.9",
		"2001:db8::5":       "2001:db8::5",
	} {
		if got := Host(in); got != want {
			t.Errorf("Host(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseTrusted(t *testing.T) {
	got, err := ParseTrusted(" 10.42.0.0/16, 127.0.0.1 ,,fd00::/8, ::ffff:10.0.0.9, 10.1.2.3/8")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.42.0.0/16", "127.0.0.1/32", "fd00::/8", "10.0.0.9/32", "10.0.0.0/8"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Fatalf("entry %d = %s, want %s", i, got[i], want[i])
		}
	}
	if got, err := ParseTrusted(""); err != nil || len(got) != 0 {
		t.Fatalf("empty = %v, %v; want none", got, err)
	}
	if _, err := ParseTrusted("10.42.0.0/16, proxy.internal"); err == nil || !strings.Contains(err.Error(), `"proxy.internal"`) {
		t.Fatalf("a hostname entry = %v, want a refusal naming it", err)
	}
}
