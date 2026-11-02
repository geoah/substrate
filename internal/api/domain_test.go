package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/domains"
	"github.com/geoah/substrate/internal/substrate"
)

// domainHost is the Host every domain test reaches the server on, so the
// record's server= is this origin.
const (
	domainHost   = "substrate.example.net"
	domainOrigin = "https://" + domainHost
)

// domainEnv is a server whose `_substrate` lookups answer from dns and whose
// server.json fetches answer from instances.
type domainEnv struct {
	t         *testing.T
	svc       *fakeService
	h         http.Handler
	dns       domains.Resolver
	instances map[string]string
	owner     string
}

func newDomainEnv(t *testing.T, dns domains.Resolver) *domainEnv {
	t.Helper()
	return newDomainEnvAt(t, dns, "")
}

// newDomainEnvAt is newDomainEnv on a server configured with publicURL
// (SUBSTRATE_PUBLIC_URL); "" configures none.
func newDomainEnvAt(t *testing.T, dns domains.Resolver, publicURL string) *domainEnv {
	t.Helper()
	svc := newFakeService()
	e := &domainEnv{t: t, svc: svc, dns: dns, instances: map[string]string{domainOrigin: fakeInstanceID}}
	e.owner = svc.token(fakeRepository)
	e.h = New(Config{
		Service:        svc,
		PublicURL:      publicURL,
		DomainResolver: resolverFunc(func(ctx context.Context, name string) ([]string, error) { return e.dns.LookupTXT(ctx, name) }),
		ServerInstances: func(_ context.Context, origin string) (string, error) {
			id, ok := e.instances[origin]
			if !ok {
				return "", errors.New("connection refused")
			}
			return id, nil
		},
	})
	return e
}

type resolverFunc func(ctx context.Context, name string) ([]string, error)

func (f resolverFunc) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return f(ctx, name)
}

func (e *domainEnv) post(path, bearer string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1"+path, nil)
	req.Host = domainHost
	req.Header.Set("Authorization", "Bearer "+bearer)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *domainEnv) start() substrate.DomainProof {
	e.t.Helper()
	rec := e.post(domainRoute, e.owner)
	wantStatus(e.t, rec, http.StatusOK)
	return decodeJSON[substrate.DomainProof](e.t, rec)
}

func (e *domainEnv) verify() substrate.DomainCheck {
	e.t.Helper()
	rec := e.post(domainVerifyRoute, e.owner)
	wantStatus(e.t, rec, http.StatusOK)
	return decodeJSON[substrate.DomainCheck](e.t, rec)
}

// publish answers rec at the repository's `_substrate` label.
func publish(rec string) domains.Static {
	return domains.Static{domains.Label + "." + fakeRepository + ".": {rec}}
}

func TestVerifyDomainStampsTheRepositoryWhenTheRecordCarriesTheProofAndNamesThisServer(t *testing.T) {
	e := newDomainEnv(t, domains.Static{})
	proof := e.start()
	if proof.Name != "_substrate."+fakeRepository {
		t.Errorf("name = %q", proof.Name)
	}
	if proof.Proof == "" || proof.Record != "v=substrate1 server="+domainOrigin+" proof="+proof.Proof {
		t.Fatalf("record = %q, proof %q", proof.Record, proof.Proof)
	}
	// A second start keeps the pending proof, so a record already published
	// stays good.
	if again := e.start(); again.Proof != proof.Proof {
		t.Fatalf("a second start minted %q over the pending %q", again.Proof, proof.Proof)
	}

	e.dns = publish(proof.Record)
	got := e.verify()
	if !got.Verified || got.VerifiedAt == nil || got.Reason != "" {
		t.Fatalf("verify = %+v, want verified", got)
	}
	// The proof is spent: the next start mints a fresh one, and checking the
	// old record again finds nothing pending.
	if next := e.start(); next.Proof == proof.Proof || next.VerifiedAt == nil {
		t.Errorf("after a verify, start = %+v; want a new proof and the stamp", next)
	}
}

func TestVerifyDomainNamesWhatTheOwnerChangesWhenTheCheckFails(t *testing.T) {
	for name, tc := range map[string]struct {
		dns       func(proof string) domains.Resolver
		instances map[string]string
		want      string
	}{
		"no record": {
			dns:  func(string) domains.Resolver { return domains.Static{} },
			want: "no TXT record opening with",
		},
		"a malformed record": {
			dns:  func(string) domains.Resolver { return publish("v=substrate1 server=http://substrate.example.net") },
			want: "cannot be read",
		},
		"no proof": {
			dns:  func(string) domains.Resolver { return publish("v=substrate1 server=" + domainOrigin) },
			want: "carries no proof=",
		},
		"the proof differs": {
			dns: func(string) domains.Resolver {
				return publish("v=substrate1 server=" + domainOrigin + " proof=stale00000")
			},
			want: "carries proof=stale00000, and the pending proof is",
		},
		"no server": {
			dns:  func(p string) domains.Resolver { return publish("v=substrate1 proof=" + p) },
			want: "names no server=",
		},
		"another server": {
			dns: func(p string) domains.Resolver {
				return publish("v=substrate1 server=https://other.example.org proof=" + p)
			},
			instances: map[string]string{"https://other.example.org": "someoneelse0000000000000a"},
			want:      "server=https://other.example.org names another substrate: point it at " + domainOrigin,
		},
		"an unreachable server": {
			dns: func(p string) domains.Resolver {
				return publish("v=substrate1 server=https://gone.example.org proof=" + p)
			},
			want: "could not be read as a substrate",
		},
		"the lookup failed": {
			dns: func(string) domains.Resolver {
				return resolverFunc(func(context.Context, string) ([]string, error) {
					return nil, &net.DNSError{Err: "server misbehaving", Name: "x", IsTemporary: true}
				})
			},
			want: "the DNS lookup of _substrate." + fakeRepository + " failed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newDomainEnv(t, domains.Static{})
			for k, v := range tc.instances {
				e.instances[k] = v
			}
			proof := e.start()
			e.dns = tc.dns(proof.Proof)
			got := e.verify()
			if got.Verified || got.VerifiedAt != nil || !strings.Contains(got.Reason, tc.want) {
				t.Fatalf("verify = %+v, want unverified with a reason containing %q", got, tc.want)
			}
			// A failed check spends nothing: the pending proof stands.
			if again := e.start(); again.Proof != proof.Proof {
				t.Errorf("a failed check replaced the proof %q with %q", proof.Proof, again.Proof)
			}
		})
	}
}

func TestVerifyDomainWithNothingPendingSaysToStartOne(t *testing.T) {
	e := newDomainEnv(t, domains.Static{})
	if got := e.verify(); got.Verified || !strings.Contains(got.Reason, "no verification is pending") {
		t.Fatalf("verify = %+v", got)
	}
}

// verifyAs runs the check for repository with a request naming host.
func (e *domainEnv) verifyAs(repository, host string) substrate.DomainCheck {
	e.t.Helper()
	if _, ok := e.svc.datasets[repository]; !ok {
		e.svc.addRepository(repository)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1"+domainVerifyRoute, nil)
	req.Host = host
	req.Header.Set("Authorization", "Bearer "+e.svc.token(repository))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	wantStatus(e.t, rec, http.StatusOK)
	return decodeJSON[substrate.DomainCheck](e.t, rec)
}

// noDNS fails a test that reaches the resolver.
func noDNS(t *testing.T) domains.Resolver {
	return resolverFunc(func(context.Context, string) ([]string, error) {
		t.Error("the check looked up DNS")
		return nil, errors.New("unreachable")
	})
}

func TestARepositoryUnderThePublicURLsHostIsVerifiedWithoutARecord(t *testing.T) {
	e := newDomainEnvAt(t, noDNS(t), "https://substrate.example.com")
	// Whatever Host the request names, the public URL decides.
	for _, host := range []string{"substrate.example.com", "evil.example.org", ""} {
		if got := e.verifyAs("ada.substrate.example.com", host); !got.Verified || got.VerifiedAt == nil {
			t.Fatalf("Host %q: verify = %+v, want verified", host, got)
		}
	}
}

func TestAForgedHostNeverVerifiesARepositoryWithoutARecord(t *testing.T) {
	for name, publicURL := range map[string]string{
		"no public URL":   "",
		"with public URL": "https://substrate.example.com",
	} {
		t.Run(name, func(t *testing.T) {
			e := newDomainEnvAt(t, domains.Static{}, publicURL)
			// The request names the host the repository sits one label
			// under, and the name publishes no record.
			got := e.verifyAs("ada.evil.example.org", "evil.example.org")
			if got.Verified || got.VerifiedAt != nil || got.Reason == "" {
				t.Fatalf("verify = %+v, want a refusal", got)
			}
		})
	}
}

func TestUnderHostTakesOneLabelUnderTheHost(t *testing.T) {
	for _, tc := range []struct {
		repository, host string
		want             bool
	}{
		{"ada.example.net", "example.net", true},
		{"ada.example.net", "Example.NET", true},
		{"a.b.example.net", "example.net", false},
		{"example.net", "example.net", false},
		{"ada.example.org", "example.net", false},
		{"adaexample.net", "example.net", false},
		{"ada.example.net", "", false},
	} {
		if got := underHost(tc.repository, tc.host); got != tc.want {
			t.Errorf("underHost(%q, %q) = %v, want %v", tc.repository, tc.host, got, tc.want)
		}
	}
}

func TestWithAPublicURLServerMustNameItExactly(t *testing.T) {
	e := newDomainEnvAt(t, domains.Static{}, domainOrigin)
	// A proxy at another origin relays this server's server.json, so the
	// instance id alone matches.
	e.instances["https://relay.example.org"] = fakeInstanceID
	proof := e.start()
	e.dns = publish("v=substrate1 server=https://relay.example.org proof=" + proof.Proof)
	if got := e.verify(); got.Verified || !strings.Contains(got.Reason, "names another substrate: point it at "+domainOrigin) {
		t.Fatalf("verify through a relay = %+v, want a refusal naming the public URL", got)
	}
	e.dns = publish(proof.Record)
	if got := e.verify(); !got.Verified {
		t.Fatalf("verify naming the public URL = %+v, want verified", got)
	}
}

// Without a public URL the instance comparison is the whole server check: an
// origin that relays this server's server.json passes, and the proof in DNS is
// what the name's controller alone can publish (decision 0208).
func TestWithoutAPublicURLAnOriginReachingThisServerPasses(t *testing.T) {
	e := newDomainEnv(t, domains.Static{})
	e.instances["https://relay.example.org"] = fakeInstanceID
	proof := e.start()
	e.dns = publish("v=substrate1 server=https://relay.example.org proof=" + proof.Proof)
	if got := e.verify(); !got.Verified {
		t.Fatalf("verify = %+v, want verified", got)
	}
}

func TestAScopedAppTokenIsRefusedAndAFullAccessOneAdmitted(t *testing.T) {
	e := newDomainEnv(t, domains.Static{})
	scoped := appToken(e.svc, fakeRepository, "read:"+personKind)
	for _, path := range []string{domainRoute, domainVerifyRoute} {
		rec := e.post(path, scoped)
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
	}
	if ds := e.svc.datasets[fakeRepository]; ds.domainProofs != 0 {
		t.Errorf("a scoped app's request minted %d proofs", ds.domainProofs)
	}
	// A full-access approval is a token without scopes: a sign-in that acts
	// as the owner (decision 0204).
	full := appToken(e.svc, fakeRepository)
	wantStatus(t, e.post(domainRoute, full), http.StatusOK)
	wantStatus(t, e.post(domainVerifyRoute, full), http.StatusOK)
}

func TestDiscoveryServesTheInstanceID(t *testing.T) {
	e := newTestEnv(t)
	rec := e.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	doc := decodeJSON[discoveryDoc](t, rec)
	if doc.Server.Instance != fakeInstanceID {
		t.Fatalf("server.instance = %q, want %q", doc.Server.Instance, fakeInstanceID)
	}
}

func TestFetchServerInstanceRefusesALoopbackOrigin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"server":{"instance":"x"}}`))
	}))
	defer srv.Close()
	if id, err := fetchServerInstance(context.Background(), srv.URL); err == nil {
		t.Fatalf("fetched %q from a loopback origin", id)
	}
}

func TestTheAuthorizationEndpointCarriesAValidLoginHintToTheApprovalPage(t *testing.T) {
	e := newAppEnv(t)
	for hint, want := range map[string]string{
		"geoah.example.org": "?login_hint=geoah.example.org",
		"Ada":               "?login_hint=ada.substrate.example.com",
		"":                  "",
		"not a name":        "",
		"ada@example.org":   "",
		"-bad.example.org":  "",
		strings.Repeat("a", 300) + ".example.org": "",
	} {
		params := appRequest("read:" + personKind)
		if hint != "" {
			params["login_hint"] = hint
		}
		rec := e.do(http.MethodGet, authorizeURL(params), "", nil, "")
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("login_hint %q: authorize = %d %s, want 303", hint, rec.Code, rec.Body.String())
		}
		_, query, _ := strings.Cut(rec.Header().Get("Location"), "?")
		if query != strings.TrimPrefix(want, "?") {
			t.Errorf("login_hint %q: redirected with %q, want %q", hint, query, want)
		}
	}
}
