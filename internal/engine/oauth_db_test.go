package engine_test

// The host OAuth facility's acceptance gate, against a FAKE provider
// (httptest — no live providers): the start/callback round trip stores the
// grant as a credential-store ref (secret-typed, redacted everywhere), the
// refresh loop keeps it fresh against the bundle's declared token endpoint,
// the runner's config resolution injects the live token per invocation, and
// account deletion revokes and releases through the finalizer flow.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// fakeProvider is an OAuth provider in a box: /token answers code exchanges
// and refreshes, /revoke records revocations.
type fakeProvider struct {
	ts *httptest.Server

	mu        sync.Mutex
	exchanges int
	refreshes int
	revoked   []string
	expiresIn int
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	p := &fakeProvider{expiresIn: 3600}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		out := map[string]any{"token_type": "Bearer", "expires_in": p.expiresIn}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "code-123" || r.Form.Get("client_id") != "client-1" {
				http.Error(w, "bad exchange", http.StatusBadRequest)
				return
			}
			p.exchanges++
			out["access_token"] = "at-1"
			out["refresh_token"] = "rt-1"
		case "refresh_token":
			if r.Form.Get("refresh_token") != "rt-1" {
				http.Error(w, "bad refresh", http.StatusBadRequest)
				return
			}
			p.refreshes++
			out["access_token"] = "at-2"
		default:
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		p.revoked = append(p.revoked, r.Form.Get("token"))
		p.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	// The account-info endpoint the OAuth facility reads the connected address
	// off (People-shaped, primary email marked). Requires the bearer token so a
	// missing grant is a 401, exercising the best-effort skip.
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"emailAddresses": []any{
				map[string]any{"value": "connected@example.com", "metadata": map[string]any{"primary": true}},
			},
		})
	})
	p.ts = httptest.NewServer(mux)
	t.Cleanup(p.ts.Close)
	return p
}

func (p *fakeProvider) configProps() map[string]any {
	return map[string]any{
		"authorizationEndpoint": p.ts.URL + "/authorize",
		"tokenEndpoint":         p.ts.URL + "/token",
		"revocationEndpoint":    p.ts.URL + "/revoke",
		"clientId":              "client-1",
		"clientSecret":          "s3cret",
		"scopes":                []any{"mail.read", "mail.send"},
	}
}

// oauthCompleter is the service seam the unauthenticated callback rides.
type oauthCompleter interface {
	CompleteOAuth(ctx context.Context, state, code string) (string, error)
}

// installOAuthBundle stands up a repository with the oauth facility on, the
// mail bundle installed, its config record created against the fake
// provider, and one pending account record.
func installOAuthBundle(t *testing.T) (substrate.Service, substrate.Dataset, bundleOps, *fakeProvider, *substrate.Record) {
	t.Helper()
	p := newFakeProvider(t)
	svc, ds := newDataset(t,
		engine.WithOAuth("test-state-key", "https://substrate.example/api/v1/core.substrate.reamde.dev/oauth/callback", p.ts.Client()),
		engine.WithCredentialKey("test-cred-key"),
	)
	docs := mbStandardDocs()
	mbPointOAuthAt(docs, p.ts.URL)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: p.configProps()})
	account := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "geo@example.com", "enabledMail": true},
	})
	return svc, ds, bundler(t, ds), p, account
}

// stateFrom lifts the signed state out of a consent URL.
func stateFrom(t *testing.T, consentURL string) string {
	t.Helper()
	u, err := url.Parse(consentURL)
	if err != nil {
		t.Fatalf("parse consent url: %v", err)
	}
	return u.Query().Get("state")
}

// The round trip: start builds the consent URL off the config record's
// declared fields, the callback exchanges the code and stores the grant as a
// ref — redacted on every read — and the finalizer lands on the account.
func TestOAuthRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, ops, p, account := installOAuthBundle(t)

	consent, err := ops.StartOAuth(ctx, owner, account.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.HasPrefix(consent, p.ts.URL+"/authorize") {
		t.Fatalf("consent url: %s", consent)
	}
	u, _ := url.Parse(consent)
	q := u.Query()
	if q.Get("client_id") != "client-1" || !strings.Contains(q.Get("scope"), "mail.read") {
		t.Fatalf("consent query: %v", q)
	}

	oc, ok := svc.(oauthCompleter)
	if !ok {
		t.Fatal("service does not implement the oauth completer seam")
	}
	record, err := oc.CompleteOAuth(ctx, stateFrom(t, consent), "code-123")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if record != account.ID {
		t.Fatalf("callback record: %s", record)
	}
	if p.exchanges != 1 {
		t.Fatalf("exchanges: %d", p.exchanges)
	}

	got := mustGet(t, ds, account.Kind, account.ID)
	if got.Properties["tokenStatus"] != "connected" {
		t.Fatalf("tokenStatus: %v", got.Properties["tokenStatus"])
	}
	// The ref is secret-typed: every read surface redacts it, and no raw
	// token ever lands on the record.
	if got.Properties["tokenRef"] != "<redacted>" {
		t.Fatalf("tokenRef leaked: %v", got.Properties["tokenRef"])
	}
	holds := false
	for _, f := range got.Finalizers {
		if f == "substrate.oauth" {
			holds = true
		}
	}
	if !holds {
		t.Fatalf("account finalizers: %v", got.Finalizers)
	}

	// A tampered state is refused, never resolved.
	if _, err := oc.CompleteOAuth(ctx, stateFrom(t, consent)+"x", "code-123"); err == nil {
		t.Fatal("a tampered state completed")
	}
}

// The runner's config resolution: a bundle function's invocation carries the
// config record (secret resolved) and the accounts with live tokens; the
// refresh loop trades an expiring token before its expiry.
func TestOAuthRunnerConfigAndRefresh(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, ops, p, account := installOAuthBundle(t)
	p.expiresIn = 120 // inside the 10m refresh window, outside the 1m inline one

	consent, err := ops.StartOAuth(ctx, owner, account.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.(oauthCompleter).CompleteOAuth(ctx, stateFrom(t, consent), "code-123"); err != nil {
		t.Fatalf("callback: %v", err)
	}

	// The echo function answers with its resolved config.
	out, _, err := ds.(fnOps).CallFunction(ctx, mbEchoFn, map[string]any{})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	cfg, _ := out.(map[string]any)["config"].(map[string]any)
	if cfg == nil {
		t.Fatalf("no config in output: %v", out)
	}
	if cfg["bundle"] != mbAuthority {
		t.Fatalf("config bundle: %v", cfg["bundle"])
	}
	ins, _ := cfg["inputs"].(map[string]any)
	conf, _ := ins["client"].(map[string]any)
	confProps, _ := conf["properties"].(map[string]any)
	// The client secret is NEVER injected now: the body
	// receives the config with every secret-typed property OMITTED, so a
	// compromised dependency has no client secret to exfiltrate. clientId (not
	// secret) is still present.
	if _, leaked := confProps["clientSecret"]; leaked {
		t.Fatalf("clientSecret was injected into the function config: %v", confProps["clientSecret"])
	}
	if confProps["clientId"] != "client-1" {
		t.Fatalf("clientId should still be injected: %v", confProps["clientId"])
	}
	accounts, _ := cfg["accounts"].([]any)
	if len(accounts) != 1 {
		t.Fatalf("accounts: %v", cfg["accounts"])
	}
	acc, _ := accounts[0].(map[string]any)
	accProps, _ := acc["properties"].(map[string]any)
	// tokenRef (secret) is omitted too; only the live token is injected, and it
	// is scrubbed on the way out.
	if _, leaked := accProps["tokenRef"]; leaked {
		t.Fatalf("tokenRef was injected into the function config: %v", accProps["tokenRef"])
	}
	if acc["id"] != account.ID || acc["token"] != engine.Redacted {
		t.Fatalf("account entry: %v", acc)
	}

	// The refresh loop trades the expiring token centrally.
	n, err := ops.RefreshOAuthTokens(ctx)
	if err != nil || n != 1 {
		t.Fatalf("refresh: %d %v", n, err)
	}
	if p.refreshes != 1 {
		t.Fatalf("provider refreshes: %d", p.refreshes)
	}
	out, _, err = ds.(fnOps).CallFunction(ctx, mbEchoFn, map[string]any{})
	if err != nil {
		t.Fatalf("call after refresh: %v", err)
	}
	cfg, _ = out.(map[string]any)["config"].(map[string]any)
	accounts, _ = cfg["accounts"].([]any)
	acc, _ = accounts[0].(map[string]any)
	// Still injected (the provider's refresh counter above proves the trade
	// happened), still scrubbed on the way out.
	if acc["token"] != engine.Redacted {
		t.Fatalf("refreshed token escaped the invocation: %v", acc["token"])
	}
}

// Deleting a connected account rides the finalizer flow: the facility
// revokes the grant, drops the stored credential, releases its hold, and GC
// collects the tombstone.
func TestOAuthAccountDeletionRevokes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, ops, p, account := installOAuthBundle(t)

	consent, err := ops.StartOAuth(ctx, owner, account.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.(oauthCompleter).CompleteOAuth(ctx, stateFrom(t, consent), "code-123"); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if _, err := ds.Delete(ctx, owner, account.Kind, account.ID); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	// The tombstone waits on the facility's hold; the pass revokes, drops
	// the credential and releases.
	released, err := ops.ProcessOAuthFinalizers(ctx)
	if err != nil || released != 1 {
		t.Fatalf("finalizers: %d %v", released, err)
	}
	p.mu.Lock()
	revoked := append([]string(nil), p.revoked...)
	p.mu.Unlock()
	if len(revoked) != 1 || revoked[0] != "rt-1" {
		t.Fatalf("revoked: %v", revoked)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if _, err := ds.Get(ctx, account.Kind, account.ID); err == nil {
		t.Fatal("account survived gc")
	}
}
