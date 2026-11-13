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
	"github.com/geoah/substrate/internal/testdb"
)

// domainHost is the Host every domain test reaches the server on, so the
// record's server= is this origin.
const (
	domainHost   = "substrate.example.net"
	domainOrigin = "https://" + domainHost
)

// domainEnv is the engine served with `_substrate` lookups answering from
// dns and server.json fetches answering from instances; this server's own
// origin answers the engine's instance id.
type domainEnv struct {
	*dbEnv
	t         *testing.T
	dns       domains.Resolver
	instances map[string]string
	// tokens holds a full-access bearer per repository verifyAs created.
	tokens map[string]string
}

func newDomainEnv(t *testing.T, dns domains.Resolver) *domainEnv {
	t.Helper()
	return newDomainEnvAt(t, dns, "")
}

// newDomainEnvAt is newDomainEnv on a server configured with publicURL
// (SUBSTRATE_PUBLIC_URL); "" configures none.
func newDomainEnvAt(t *testing.T, dns domains.Resolver, publicURL string) *domainEnv {
	t.Helper()
	e := &domainEnv{t: t, dns: dns, instances: map[string]string{}, tokens: map[string]string{}}
	e.dbEnv = newDBEnv(t, withConfig(func(c *Config) {
		c.PublicURL = publicURL
		c.DomainResolver = resolverFunc(func(ctx context.Context, name string) ([]string, error) { return e.dns.LookupTXT(ctx, name) })
		c.ServerInstances = func(_ context.Context, origin string) (string, error) {
			id, ok := e.instances[origin]
			if !ok {
				return "", errors.New("connection refused")
			}
			return id, nil
		}
	}))
	e.instances[domainOrigin] = e.svc.InstanceID()
	return e
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
	rec := e.post(domainRoute, e.token)
	wantStatus(e.t, rec, http.StatusOK)
	return decodeJSON[substrate.DomainProof](e.t, rec)
}

func (e *domainEnv) verify() substrate.DomainCheck {
	e.t.Helper()
	rec := e.post(domainVerifyRoute, e.token)
	wantStatus(e.t, rec, http.StatusOK)
	return decodeJSON[substrate.DomainCheck](e.t, rec)
}

// publish answers rec at the repository's `_substrate` label.
func (e *domainEnv) publish(rec string) domains.Static {
	return domains.Static{domains.Label + "." + e.repository + ".": {rec}}
}

// verifyAs runs the check for repository, created on first use, with a
// request naming host.
func (e *domainEnv) verifyAs(repository, host string) substrate.DomainCheck {
	e.t.Helper()
	token, ok := e.tokens[repository]
	if !ok {
		ctx := context.Background()
		if _, err := e.svc.CreateRepository(ctx, repository); err != nil {
			e.t.Fatalf("create %s: %v", repository, err)
		}
		ds, err := e.svc.Dataset(ctx, repository)
		if err != nil {
			e.t.Fatalf("open %s: %v", repository, err)
		}
		if _, token, err = ds.MintToken(ctx, substrate.MintTokenInput{Label: "test"}); err != nil {
			e.t.Fatalf("mint into %s: %v", repository, err)
		}
		e.tokens[repository] = token
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1"+domainVerifyRoute, nil)
	req.Host = host
	req.Header.Set("Authorization", "Bearer "+token)
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

func TestVerifyDomainStampsTheRepositoryWhenTheRecordCarriesTheProofAndNamesThisServer(t *testing.T) {
	t.Parallel()
	e := newDomainEnv(t, domains.Static{})
	proof := e.start()
	if proof.Name != "_substrate."+e.repository {
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

	e.dns = e.publish(proof.Record)
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

// domainRepositoryMark stands for the env's repository in a reason a table
// case builds before the env exists.
const domainRepositoryMark = "<repository>"

func TestVerifyDomainNamesWhatTheOwnerChangesWhenTheCheckFails(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		dns       func(e *domainEnv, proof string) domains.Resolver
		instances map[string]string
		want      string
	}{
		"no record": {
			dns:  func(*domainEnv, string) domains.Resolver { return domains.Static{} },
			want: "no TXT record opening with",
		},
		"a malformed record": {
			dns: func(e *domainEnv, _ string) domains.Resolver {
				return e.publish("v=substrate1 server=http://substrate.example.net")
			},
			want: "cannot be read",
		},
		"no proof": {
			dns:  func(e *domainEnv, _ string) domains.Resolver { return e.publish("v=substrate1 server=" + domainOrigin) },
			want: "carries no proof=",
		},
		"the proof differs": {
			dns: func(e *domainEnv, _ string) domains.Resolver {
				return e.publish("v=substrate1 server=" + domainOrigin + " proof=stale00000")
			},
			want: "carries proof=stale00000, and the pending proof is",
		},
		"no server": {
			dns:  func(e *domainEnv, p string) domains.Resolver { return e.publish("v=substrate1 proof=" + p) },
			want: "names no server=",
		},
		"another server": {
			dns: func(e *domainEnv, p string) domains.Resolver {
				return e.publish("v=substrate1 server=https://other.example.org proof=" + p)
			},
			instances: map[string]string{"https://other.example.org": "someoneelse0000000000000a"},
			want:      "server=https://other.example.org names another substrate: point it at " + domainOrigin,
		},
		"an unreachable server": {
			dns: func(e *domainEnv, p string) domains.Resolver {
				return e.publish("v=substrate1 server=https://gone.example.org proof=" + p)
			},
			want: "could not be read as a substrate",
		},
		"the lookup failed": {
			dns: func(*domainEnv, string) domains.Resolver {
				return resolverFunc(func(context.Context, string) ([]string, error) {
					return nil, &net.DNSError{Err: "server misbehaving", Name: "x", IsTemporary: true}
				})
			},
			want: "the DNS lookup of _substrate." + domainRepositoryMark + " did not answer",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newDomainEnv(t, domains.Static{})
			for k, v := range tc.instances {
				e.instances[k] = v
			}
			want := strings.ReplaceAll(tc.want, domainRepositoryMark, e.repository)
			proof := e.start()
			e.dns = tc.dns(e, proof.Proof)
			got := e.verify()
			if got.Verified || got.VerifiedAt != nil || !strings.Contains(got.Reason, want) {
				t.Fatalf("verify = %+v, want unverified with a reason containing %q", got, want)
			}
			// A failed check spends nothing: the pending proof stands.
			if again := e.start(); again.Proof != proof.Proof {
				t.Errorf("a failed check replaced the proof %q with %q", proof.Proof, again.Proof)
			}
		})
	}
}

func TestVerifyDomainWithNothingPendingSaysToStartOne(t *testing.T) {
	t.Parallel()
	e := newDomainEnv(t, domains.Static{})
	if got := e.verify(); got.Verified || !strings.Contains(got.Reason, "no verification is pending") {
		t.Fatalf("verify = %+v", got)
	}
}

func TestARepositoryUnderThePublicURLsHostIsVerifiedWithoutARecord(t *testing.T) {
	t.Parallel()
	e := newDomainEnvAt(t, noDNS(t), "https://substrate.example.com")
	repository := testdb.RepositoryLabel(t) + ".substrate.example.com"
	// Whatever Host the request names, the public URL decides.
	for _, host := range []string{"substrate.example.com", "evil.example.org", ""} {
		if got := e.verifyAs(repository, host); !got.Verified || got.VerifiedAt == nil {
			t.Fatalf("Host %q: verify = %+v, want verified", host, got)
		}
	}
}

func TestAForgedHostNeverVerifiesARepositoryWithoutARecord(t *testing.T) {
	t.Parallel()
	for name, publicURL := range map[string]string{
		"no public URL":   "",
		"with public URL": "https://substrate.example.com",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newDomainEnvAt(t, domains.Static{}, publicURL)
			// The request names the host the repository sits one label
			// under, and the name publishes no record.
			got := e.verifyAs(testdb.RepositoryLabel(t)+".evil.example.org", "evil.example.org")
			if got.Verified || got.VerifiedAt != nil || got.Reason == "" {
				t.Fatalf("verify = %+v, want a refusal", got)
			}
		})
	}
}

func TestWithAPublicURLServerMustNameItExactly(t *testing.T) {
	t.Parallel()
	e := newDomainEnvAt(t, domains.Static{}, domainOrigin)
	// A proxy at another origin relays this server's server.json, so the
	// instance id alone matches.
	e.instances["https://relay.example.org"] = e.svc.InstanceID()
	proof := e.start()
	e.dns = e.publish("v=substrate1 server=https://relay.example.org proof=" + proof.Proof)
	if got := e.verify(); got.Verified || !strings.Contains(got.Reason, "names another substrate: point it at "+domainOrigin) {
		t.Fatalf("verify through a relay = %+v, want a refusal naming the public URL", got)
	}
	e.dns = e.publish(proof.Record)
	if got := e.verify(); !got.Verified {
		t.Fatalf("verify naming the public URL = %+v, want verified", got)
	}
}

// With a public URL, server= naming it exactly decides the server check: a
// server whose public name resolves to a private address from inside cannot
// read its own server.json, and reading it would prove nothing more.
func TestWithAPublicURLTheServerIsNotReadBack(t *testing.T) {
	t.Parallel()
	e := newDomainEnvAt(t, domains.Static{}, domainOrigin)
	delete(e.instances, domainOrigin)
	proof := e.start()
	e.dns = e.publish(proof.Record)
	if got := e.verify(); !got.Verified {
		t.Fatalf("verify = %+v, want verified without reading server.json", got)
	}
}

// A name that also publishes (doc=, sha256=, a key this binary does not
// read) is shown its own record with server= and proof= merged in, because
// one label carries one `v=substrate1` record; a check of that merged record
// passes, and the publisher's keys survive it.
func TestVerifyDomainKeepsWhatTheNameAlreadyPublishes(t *testing.T) {
	t.Parallel()
	sum := strings.Repeat("ab", 32)
	e := newDomainEnvAt(t, domains.Static{}, domainOrigin)
	e.dns = e.publish("v=substrate1 doc=https://example.org/authority.json sha256=" + sum + " later=1")
	proof := e.start()
	want := "v=substrate1 server=" + domainOrigin + " proof=" + proof.Proof +
		" doc=https://example.org/authority.json sha256=" + sum + " later=1"
	if proof.Record != want {
		t.Fatalf("record = %q, want %q", proof.Record, want)
	}
	e.dns = e.publish(proof.Record)
	if got := e.verify(); !got.Verified {
		t.Fatalf("verify of the merged record = %+v, want verified", got)
	}
}

// A lookup that fails says so in fixed words: the resolver's own error names
// the deployment's network, and the answer goes to whoever asked.
func TestAFailedLookupNamesNothingOfTheServersNetwork(t *testing.T) {
	t.Parallel()
	e := newDomainEnv(t, domains.Static{})
	e.start()
	e.dns = resolverFunc(func(context.Context, string) ([]string, error) {
		return nil, &net.DNSError{Err: "read udp 10.43.0.10:53: i/o timeout", Name: "x", Server: "10.43.0.10:53", IsTimeout: true}
	})
	if got := e.verify(); got.Verified || strings.Contains(got.Reason, "10.43") || strings.Contains(got.Reason, "timeout") {
		t.Fatalf("verify = %+v, want a refusal that names no address", got)
	}
}

// Without a public URL the instance comparison is the whole server check: an
// origin that relays this server's server.json passes, and the proof in DNS is
// what the name's controller alone can publish (decision 0208).
func TestWithoutAPublicURLAnOriginReachingThisServerPasses(t *testing.T) {
	t.Parallel()
	e := newDomainEnv(t, domains.Static{})
	e.instances["https://relay.example.org"] = e.svc.InstanceID()
	proof := e.start()
	e.dns = e.publish("v=substrate1 server=https://relay.example.org proof=" + proof.Proof)
	if got := e.verify(); !got.Verified {
		t.Fatalf("verify = %+v, want verified", got)
	}
}

// Both routes refuse a scoped app token, which mints no proof; a full-access
// approval is a token without scopes, a sign-in that acts as the owner
// (decision 0204).
func TestAScopedAppTokenIsRefusedAndAFullAccessOneAdmitted(t *testing.T) {
	t.Parallel()
	e := newDomainEnv(t, domains.Static{})
	scoped := e.mint(t, substrate.MintTokenInput{
		Label: "notes", Scopes: []string{"read:" + personKind}, ClientID: appClientID, ClientName: "Notes",
	})
	for _, path := range []string{domainRoute, domainVerifyRoute} {
		rec := e.post(path, scoped)
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
	}
	state, err := e.ds.DomainState(context.Background())
	if err != nil {
		t.Fatalf("domain state: %v", err)
	}
	if state.Proof != "" {
		t.Errorf("a scoped app's request minted the proof %q", state.Proof)
	}
	full := e.mint(t, substrate.MintTokenInput{Label: "notes", ClientID: appClientID, ClientName: "Notes"})
	wantStatus(t, e.post(domainRoute, full), http.StatusOK)
	wantStatus(t, e.post(domainVerifyRoute, full), http.StatusOK)
}
