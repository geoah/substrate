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
	svc := newFakeService()
	e := &domainEnv{t: t, svc: svc, dns: dns, instances: map[string]string{domainOrigin: fakeInstanceID}}
	e.owner = svc.token(fakeRepository)
	e.h = New(Config{
		Service:        svc,
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

func TestARepositoryUnderTheHostTheRequestReachedIsVerifiedWithoutARecord(t *testing.T) {
	svc := newFakeService()
	const own = "ada." + domainHost
	svc.addRepository(own)
	h := New(Config{
		Service: svc,
		DomainResolver: resolverFunc(func(context.Context, string) ([]string, error) {
			t.Error("the own-host check looked up DNS")
			return nil, errors.New("unreachable")
		}),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1"+domainVerifyRoute, nil)
	req.Host = domainHost + ":443"
	req.Header.Set("Authorization", "Bearer "+svc.token(own))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.DomainCheck](t, rec); !got.Verified || got.VerifiedAt == nil {
		t.Fatalf("verify = %+v, want verified", got)
	}
}

func TestUnderOwnHostTakesOneLabelUnderTheRequestsHost(t *testing.T) {
	for _, tc := range []struct {
		repository, host string
		want             bool
	}{
		{"ada.example.net", "example.net", true},
		{"ada.example.net", "Example.NET:8080", true},
		{"ada.localhost", "localhost:8080", true},
		{"a.b.example.net", "example.net", false},
		{"example.net", "example.net", false},
		{"ada.example.org", "example.net", false},
		{"adaexample.net", "example.net", false},
		{"ada.example.net", "", false},
	} {
		if got := underOwnHost(tc.repository, tc.host); got != tc.want {
			t.Errorf("underOwnHost(%q, %q) = %v, want %v", tc.repository, tc.host, got, tc.want)
		}
	}
}

func TestAnAppsTokenNeverReachesDomainVerification(t *testing.T) {
	e := newDomainEnv(t, domains.Static{})
	app := appToken(e.svc, fakeRepository, "read:"+personKind)
	for _, path := range []string{domainRoute, domainVerifyRoute} {
		rec := e.post(path, app)
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
	}
	if ds := e.svc.datasets[fakeRepository]; ds.domainProofs != 0 {
		t.Errorf("an app's request minted %d proofs", ds.domainProofs)
	}
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
