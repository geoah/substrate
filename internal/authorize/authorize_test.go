package authorize

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestAClientIDIsAnHTTPSDocumentURLOrALoopbackApp(t *testing.T) {
	for raw, local := range map[string]bool{
		"https://notes.example.org/client.json":       false,
		"https://notes.example.org:8443/oauth/client": false,
		"https://notes.example.org/":                  false,
		"http://127.0.0.1/cli":                        true,
		"http://127.0.0.1:43123/cli":                  true,
		"http://[::1]:43123/cli":                      true,
		"http://localhost:43123/cli":                  true,
	} {
		c, err := ParseClientID(raw)
		if err != nil {
			t.Errorf("ParseClientID(%q) = %v", raw, err)
			continue
		}
		if c.Local != local || c.ID != raw {
			t.Errorf("ParseClientID(%q) = %+v", raw, c)
		}
	}
	for _, raw := range []string{
		"",
		"notes.example.org",
		"https://notes.example.org",
		"http://notes.example.org/client.json",
		"http://10.0.0.7/client",
		"https://user:pw@notes.example.org/client.json",
		"https://notes.example.org/client.json#x",
		"https://notes.example.org/client.json?v=1",
		"https://notes.example.org/a/../client.json",
		"https://notes.example.org/a/%2e%2e/client.json",
		"https://notes.example.org/a/.%2e/client.json",
		"https://notes.example.org/a/%2e./client.json",
		"https://notes.example.org/a/..%2Fb/client.json",
		// A Unicode host is dialed as its punycode: the page would head with
		// a lookalike of another domain.
		"https://\u0430pple.com/client.json",
		"https://%D0%B0pple.com/client.json",
		"ftp://notes.example.org/client.json",
		"https://notes.example.org/" + strings.Repeat("a", 2100),
	} {
		if _, err := ParseClientID(raw); err == nil {
			t.Errorf("ParseClientID(%q) admitted", raw)
		}
	}
}

func TestARedirectIsHTTPSOrLoopbackAndNeverAScriptOrAFragment(t *testing.T) {
	for _, raw := range []string{
		"https://notes.example.org/callback",
		"https://notes.example.org/callback?x=1",
		"http://127.0.0.1:5000/callback",
		"http://[::1]/callback",
	} {
		if _, err := ParseRedirect(raw); err != nil {
			t.Errorf("ParseRedirect(%q) = %v", raw, err)
		}
	}
	for _, raw := range []string{
		"javascript:alert(1)",
		"data:text/html,x",
		"http://notes.example.org/callback",
		"https://notes.example.org/callback#frag",
		"https://user@notes.example.org/callback",
		"/callback",
		"com.example.app:/callback",
		"https:\\\\evil.example/x",
		"https://\u0430pple.com/cb",
	} {
		if _, err := ParseRedirect(raw); err == nil {
			t.Errorf("ParseRedirect(%q) admitted", raw)
		}
	}
}

func TestARedirectMatchesItsListingExactlyOrOnAnyLoopbackPort(t *testing.T) {
	listed := []string{"https://notes.example.org/callback", "http://127.0.0.1/cb"}
	for redirect, want := range map[string]bool{
		"https://notes.example.org/callback":     true,
		"https://notes.example.org/callback/":    false,
		"https://notes.example.org/callback?x=1": false,
		"https://NOTES.example.org/callback":     false,
		"https://notes.example.org:443/callback": false,
		"http://127.0.0.1/cb":                    true,
		"http://127.0.0.1:53021/cb":              true,
		"http://127.0.0.1:53021/other":           false,
		"http://[::1]:53021/cb":                  false,
		"https://evil.example.org/callback":      false,
	} {
		if got := RedirectListed(listed, redirect); got != want {
			t.Errorf("RedirectListed(%q) = %v, want %v", redirect, got, want)
		}
	}
}

func TestPKCEHoldsTheChallengeAndTheVerifierToRFC7636(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	// RFC 7636 Appendix B.
	challenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if !ValidChallenge(challenge) || !ValidVerifier(verifier) || !VerifyS256(verifier, challenge) {
		t.Fatal("the RFC 7636 example does not verify")
	}
	if VerifyS256(verifier+"x", challenge) {
		t.Fatal("a different verifier verified")
	}
	for _, v := range []string{strings.Repeat("a", 42), strings.Repeat("a", 129), strings.Repeat("a", 42) + "+"} {
		if ValidVerifier(v) {
			t.Errorf("ValidVerifier(%q) admitted", v)
		}
	}
	for _, c := range []string{challenge + "=", challenge[:42], strings.Repeat("a", 42) + "/"} {
		if ValidChallenge(c) {
			t.Errorf("ValidChallenge(%q) admitted", c)
		}
	}
}

func TestADocumentIsHeldToWhatAPublicClientMayClaim(t *testing.T) {
	id := "https://notes.example.org/client.json"
	ok := `{"client_id":"` + id + `","client_name":"Notes","client_uri":"https://notes.example.org","redirect_uris":["https://notes.example.org/cb"],"token_endpoint_auth_method":"none","grant_types":["authorization_code"],"response_types":["code"]}`
	doc, err := ParseDocument([]byte(ok), id)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "Notes" || doc.URI != "https://notes.example.org" || len(doc.RedirectURIs) != 1 {
		t.Fatalf("doc = %+v", doc)
	}
	// A claimed URI that is not https is dropped, not shown.
	doc, err = ParseDocument([]byte(`{"client_id":"`+id+`","client_uri":"javascript:alert(1)","redirect_uris":["https://notes.example.org/cb"]}`), id)
	if err != nil || doc.URI != "" {
		t.Fatalf("doc = %+v, %v: want the script URI dropped", doc, err)
	}
	for name, body := range map[string]string{
		"not JSON":        `<html>hello</html>`,
		"not an object":   `["x"]`,
		"other client_id": `{"client_id":"https://evil.example.org/c","redirect_uris":["https://notes.example.org/cb"]}`,
		"no redirects":    `{"client_id":"` + id + `"}`,
		"empty redirects": `{"client_id":"` + id + `","redirect_uris":[]}`,
		"script redirect": `{"client_id":"` + id + `","redirect_uris":["javascript:alert(1)"]}`,
		"secret":          `{"client_id":"` + id + `","redirect_uris":["https://notes.example.org/cb"],"client_secret":"s"}`,
		"jwks":            `{"client_id":"` + id + `","redirect_uris":["https://notes.example.org/cb"],"jwks_uri":"https://notes.example.org/jwks"}`,
		"basic auth":      `{"client_id":"` + id + `","redirect_uris":["https://notes.example.org/cb"],"token_endpoint_auth_method":"client_secret_basic"}`,
		"no code grant":   `{"client_id":"` + id + `","redirect_uris":["https://notes.example.org/cb"],"grant_types":["client_credentials"]}`,
	} {
		if _, err := ParseDocument([]byte(body), id); err == nil {
			t.Errorf("%s: admitted", name)
		}
	}
}

func TestTheFetcherDialsPublicAddressesAlone(t *testing.T) {
	for _, s := range []string{
		"127.0.0.1", "::1", "10.0.0.1", "192.168.1.1", "172.16.0.1", "169.254.169.254", "100.64.0.1",
		"0.0.0.0", "192.0.2.1", "198.51.100.1", "203.0.113.1", "198.18.0.1", "240.0.0.1", "192.0.0.8",
		"2001:db8::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "64:ff9b::a9fe:a9fe", "100::1", "2001:2::1",
		// The special ranges, reached through a NAT64 or 6to4 gateway.
		"64:ff9b::c612:1", "2002:c612:1::1", "64:ff9b::c000:8",
	} {
		if Public(netip.MustParseAddr(s)) {
			t.Errorf("Public(%s) = true, want refused", s)
		}
	}
	for _, s := range []string{"93.184.215.14", "2606:2800:21f:cb07:6820:80da:af6b:8b2c", "1.1.1.1"} {
		if !Public(netip.MustParseAddr(s)) {
			t.Errorf("Public(%s) = false, want admitted", s)
		}
	}

	// A document served on loopback is never reached, whatever the URL says.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the fetcher reached a loopback server")
	}))
	defer srv.Close()
	f := NewFetcher(nil)
	_, err := f.Fetch(context.Background(), Client{ID: srv.URL + "/client.json", Host: "x"})
	var fe *FetchError
	if !errors.As(err, &fe) || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("fetch of a loopback document = %v, want a refusal that names no address", err)
	}
}

func TestTheFetcherReadsADocumentAndCachesOnlySuccess(t *testing.T) {
	calls := 0
	status := http.StatusOK
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/client.json", http.StatusFound)
			return
		}
		if r.URL.Path == "/big" {
			_, _ = w.Write([]byte(strings.Repeat(" ", maxDocument+1)))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"client_id":"` + srv.URL + `/client.json","redirect_uris":["https://notes.example.org/cb"]}`))
	}))
	defer srv.Close()
	now := time.Unix(1_000_000, 0)
	// The real transport refuses loopback; this test is about everything
	// after the dial.
	f := newFetcher(srv.Client(), func() time.Time { return now })
	c := Client{ID: srv.URL + "/client.json"}

	status = http.StatusInternalServerError
	if _, err := f.Fetch(context.Background(), c); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("a 500 = %v, want a refusal naming it", err)
	}
	status = http.StatusOK
	if _, err := f.Fetch(context.Background(), c); err != nil {
		t.Fatalf("after a failure, a good fetch = %v: a failure must not be cached", err)
	}
	if _, err := f.Fetch(context.Background(), c); err != nil || calls != 2 {
		t.Fatalf("second fetch = %v after %d calls, want the cache", err, calls)
	}
	now = now.Add(cacheFor + time.Second)
	if _, err := f.Fetch(context.Background(), c); err != nil || calls != 3 {
		t.Fatalf("after the cache lapsed = %v after %d calls, want a refetch", err, calls)
	}
	if _, err := f.Fetch(context.Background(), Client{ID: srv.URL + "/redirect"}); err == nil {
		t.Fatal("a redirect was followed")
	}
	if _, err := f.Fetch(context.Background(), Client{ID: srv.URL + "/big"}); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("an oversized document = %v", err)
	}
	if _, err := f.Fetch(context.Background(), Client{ID: "http://127.0.0.1/x", Local: true}); err == nil {
		t.Fatal("a local app was fetched")
	}
}

func challenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestARequestIsClaimedByOneRepositoryAndAnsweredOnce(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := NewStore(func() time.Time { return now })
	r, err := s.Park(Request{Issuer: "https://s.example.com", Client: Client{ID: "https://a.example.org/c"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ID) != 32 {
		t.Fatalf("request id %q is not 128 bits of hex", r.ID)
	}
	if _, err := s.Claim(r.ID, "ada.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(r.ID, "eve.example.com"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("a second repository's claim = %v, want ErrNotPending", err)
	}
	refuse := errors.New("refused")
	if _, _, err := s.Approve(r.ID, "ada.example.com", func(Request) (Approval, error) { return Approval{}, refuse }); !errors.Is(err, refuse) {
		t.Fatalf("a refused approval = %v", err)
	}
	if _, err := s.Claim(r.ID, "ada.example.com"); err != nil {
		t.Fatalf("a refused approval left the request = %v, want it pending", err)
	}
	code, _, err := s.Approve(r.ID, "ada.example.com", func(Request) (Approval, error) { return Approval{Scopes: []string{"read:*"}}, nil })
	if err != nil || len(code) != 64 {
		t.Fatalf("approval = %q, %v", code, err)
	}
	if _, _, err := s.Approve(r.ID, "ada.example.com", func(Request) (Approval, error) { return Approval{}, nil }); !errors.Is(err, ErrNotPending) {
		t.Fatalf("a second approval = %v, want ErrNotPending", err)
	}
	if _, err := s.Deny(r.ID, "ada.example.com", func(Request) error { return nil }); !errors.Is(err, ErrNotPending) {
		t.Fatalf("a denial after the approval = %v, want ErrNotPending", err)
	}
	// A request lapses unanswered.
	r2, _ := s.Park(Request{})
	now = now.Add(RequestTTL)
	if _, err := s.Claim(r2.ID, "ada.example.com"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("a lapsed request = %v, want ErrNotPending", err)
	}
}

func TestACodeIsSpentOnlyByAPresentationThatProvesItsBindings(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := NewStore(func() time.Time { return now })
	verifier := strings.Repeat("v", 43)
	r, _ := s.Park(Request{Challenge: challenge(verifier)})
	code, _, err := s.Approve(r.ID, "ada.example.com", func(Request) (Approval, error) { return Approval{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	proves := func(v string) func(Code) error {
		return func(c Code) error {
			if !VerifyS256(v, c.Request.Challenge) {
				return ErrBadCode
			}
			return nil
		}
	}
	if _, _, err := s.Redeem(code, proves(strings.Repeat("w", 43))); !errors.Is(err, ErrBadCode) {
		t.Fatalf("a wrong verifier = %v", err)
	}
	if _, first, err := s.Redeem(code, proves(verifier)); err != nil || !first {
		t.Fatalf("after a wrong verifier, the right one = first %v, %v: the failure spent the code", first, err)
	}
	// A verified replay before the mint finished asks the mint to undo itself.
	if _, first, err := s.Redeem(code, proves(verifier)); err != nil || first {
		t.Fatalf("a verified replay = first %v, %v", first, err)
	}
	if !s.Minted(code, "tok1") {
		t.Fatal("a mint after a verified replay was not told to revoke")
	}
	// A replay after the mint names the token to revoke.
	c, first, err := s.Redeem(code, proves(verifier))
	if err != nil || first || c.MintedTokenID != "tok1" {
		t.Fatalf("a later replay = %+v first %v, %v", c, first, err)
	}
	// An unredeemed code lapses.
	r2, _ := s.Park(Request{Challenge: challenge(verifier)})
	code2, _, _ := s.Approve(r2.ID, "ada.example.com", func(Request) (Approval, error) { return Approval{}, nil })
	now = now.Add(CodeTTL)
	if _, _, err := s.Redeem(code2, proves(verifier)); !errors.Is(err, ErrBadCode) {
		t.Fatalf("a lapsed code = %v", err)
	}
}
