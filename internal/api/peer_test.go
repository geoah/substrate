package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func TestClientAddressReadsForwardedForOnlyFromATrustedProxy(t *testing.T) {
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
			if got := clientAddress(r, c.trusted); got != c.want {
				t.Fatalf("clientAddress = %q, want %q", got, c.want)
			}
		})
	}
}

// Behind a proxy every client shares the proxy's address. Once the operator
// lists it, a client spending its webhook pace on tokens that do not exist
// leaves another client's delivery through the same proxy untouched (#1114).
func TestAWebhookPaceBehindATrustedProxyIsEachClients(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trusted []netip.Prefix
		want    int
	}{
		{"trusted", []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, http.StatusAccepted},
		// The same requests with no trusted proxy: the shared pace refuses the
		// app, which is the bug the setting exists for.
		{"untrusted", nil, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &countingAuthSvc{grantHookSvc: newGrantHookSvc()}
			now := time.Unix(1_000_000, 0)
			h := New(Config{Service: svc, AuthInterval: time.Minute, Now: func() time.Time { return now }, TrustedProxies: tc.trusted})
			hook := "/webhooks/" + fakeRepository + "/" + appHook
			bogus := map[string]string{
				"Authorization":   "Bearer " + substrate.TokenSecretPrefix + "0123456789abcdef",
				"X-Forwarded-For": "203.0.113.66",
			}
			for range 10 {
				postHook(h, hook, []byte(`{}`), bogus)
			}
			app := appToken(svc.fakeService, fakeRepository, "webhook:"+appHook)
			rec := postHook(h, hook, []byte(`{"n":1}`), map[string]string{
				"Content-Type":    "application/json",
				"Authorization":   "Bearer " + app,
				"X-Forwarded-For": "198.51.100.7",
			})
			wantStatus(t, rec, tc.want)
		})
	}
}
