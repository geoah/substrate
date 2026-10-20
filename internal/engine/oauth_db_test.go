package engine_test

// The host OAuth facility's acceptance gate, against a FAKE provider
// (httptest — no live providers): the start/callback round trip stores the
// grant as a credential-store ref (secret-typed, redacted everywhere), the
// refresh loop keeps it fresh against the bundle's declared token endpoint,
// the runner's config resolution injects the live token per invocation, and
// account deletion revokes and releases through the finalizer flow.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
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
	// refuseRefresh answers every refresh with an RFC 6749 `invalid_grant`
	// whose description echoes the client secret, as a provider may.
	refuseRefresh bool
	// unavailable answers every refresh with a 503 and no RFC 6749 code: a
	// failure that may pass.
	unavailable bool
	// refuseStatus, when set, answers every refresh with that status and
	// refuseCode as the RFC 6749 error code.
	refuseStatus int
	refuseCode   string
	// refreshCalls counts every refresh request, answered or refused.
	refreshCalls int
	// rotate makes the provider rotate refresh tokens the way WHOOP does:
	// each refresh must send current, is answered with the next one, and
	// spends the one it sent, which is refused afterwards with `400
	// invalid_request`. A code exchange starts over at rt-1.
	rotate  bool
	current string
	issued  int
	// sent is every refresh token a refresh request carried, in order.
	sent []string
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
			p.current, p.issued = "rt-1", 1
			out["access_token"] = "at-1"
			out["refresh_token"] = "rt-1"
		case "refresh_token":
			p.refreshCalls++
			p.sent = append(p.sent, r.Form.Get("refresh_token"))
			if p.unavailable {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			if p.refuseStatus != 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(p.refuseStatus)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": p.refuseCode})
				return
			}
			if p.refuseRefresh {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":             "invalid_grant",
					"error_description": "SECRET-PROVIDER-DETAIL client-1:s3cret rt-1",
				})
				return
			}
			if p.rotate {
				if r.Form.Get("refresh_token") != p.current {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_request"})
					return
				}
				p.refreshes++
				p.issued++
				p.current = fmt.Sprintf("rt-%d", p.issued)
				out["access_token"] = fmt.Sprintf("at-%d", p.issued)
				out["refresh_token"] = p.current
				break
			}
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

// installOAuthBundle stands up a repository with the oauth facility on, the
// mail bundle installed, its config record created against the fake
// provider, and one pending account record.
func installOAuthBundle(t *testing.T) (substrate.Service, substrate.Dataset, *fakeProvider, *substrate.Record) {
	t.Helper()
	p := newFakeProvider(t)
	svc, ds := newDataset(t,
		engine.WithOAuth("test-state-key", "https://substrate.example/api/v1/substrate.reamde.dev/core/oauth/callback", p.ts.Client()),
		engine.WithCredentialKey(engine.TestCredentialKey),
	)
	docs := mbStandardDocs()
	mbPointOAuthAt(docs, p.ts.URL)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: p.configProps()})
	account := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "geo@example.com", "enabledMail": true},
	})
	return svc, ds, p, account
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
	svc, ds, p, account := installOAuthBundle(t)

	// A bare id is refused even where one account alone holds it: the FORM is
	// what is refused, not an ambiguity (record 0102), and the refusal names
	// the one spelling that works.
	_, err := ds.StartOAuth(ctx, owner, account.ID)
	wantErr(t, err, substrate.ErrValidation, "a bare id with one live account")
	for _, want := range []string{
		fmt.Sprintf("%q is a bare record id", account.ID),
		"named in full as <authority>/<package>/<kind>/<id>",
		"this repository holds " + vocabulary.RecordPath(account.Kind, account.ID),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}

	consent, err := ds.StartOAuth(ctx, owner, vocabulary.RecordPath(account.Kind, account.ID))
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

	record, err := svc.CompleteOAuth(ctx, stateFrom(t, consent), "code-123")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if record != vocabulary.RecordPath(account.Kind, account.ID) {
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
	if _, err := svc.CompleteOAuth(ctx, stateFrom(t, consent)+"x", "code-123"); err == nil {
		t.Fatal("a tampered state completed")
	}
}

// The runner's config resolution: a bundle function's invocation carries the
// config record (secret resolved) and the accounts with live tokens; the
// refresh loop trades an expiring token before its expiry.
func TestOAuthRunnerConfigAndRefresh(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, p, account := installOAuthBundle(t)
	p.expiresIn = 120 // inside the 10m refresh window, outside the 1m inline one

	consent, err := ds.StartOAuth(ctx, owner, vocabulary.RecordPath(account.Kind, account.ID))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.CompleteOAuth(ctx, stateFrom(t, consent), "code-123"); err != nil {
		t.Fatalf("callback: %v", err)
	}

	// The echo function answers with its resolved config.
	out, _, err := ds.CallFunction(ctx, substrate.ActorAPI, mbEchoFn, map[string]any{})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	cfg, _ := out.(map[string]any)["config"].(map[string]any)
	if cfg == nil {
		t.Fatalf("no config in output: %v", out)
	}
	if cfg["bundle"] != mbPackage {
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
	n, err := ds.RefreshOAuthTokens(ctx)
	if err != nil || n != 1 {
		t.Fatalf("refresh: %d %v", n, err)
	}
	if p.refreshes != 1 {
		t.Fatalf("provider refreshes: %d", p.refreshes)
	}
	out, _, err = ds.CallFunction(ctx, substrate.ActorAPI, mbEchoFn, map[string]any{})
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

// A refresh the provider refuses leaves its reason and time on the account
// beside `erroring`, from the central loop and from the on-demand refresh an
// invocation makes alike; the reason carries the RFC 6749 code and never the
// provider's description, and a later good refresh or a reconnect clears it.
// The refusal backs the credential off, so the clock moves past each step
// before the provider is asked again.
func TestOAuthRefreshFailureStoresItsReason(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	svc, ds := newDataset(t,
		engine.WithOAuth("test-state-key", "https://substrate.example/api/v1/substrate.reamde.dev/core/oauth/callback", p.ts.Client()),
		engine.WithCredentialKey(engine.TestCredentialKey),
	)
	clock := &engine.TestClock{}
	engine.SetOAuthClock(svc, clock.Now)
	docs := mbStandardDocs()
	for _, d := range docs {
		if meta, _ := d["metadata"].(map[string]any); d["kind"] == vocabulary.CoreKind(vocabulary.DocKind) && meta["id"] == mbAccountType {
			props, _ := d["data"].(map[string]any)["properties"].(map[string]any)
			props["tokenError"] = map[string]any{"type": "string", "writer": "oauth"}
			props["tokenErrorAt"] = map[string]any{"type": "datetime", "writer": "oauth"}
		}
	}
	mbPointOAuthAt(docs, p.ts.URL)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: p.configProps()})
	account := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "geo@example.com", "enabledMail": true},
	})
	p.expiresIn = 30 // inside both the loop's window and the inline one
	connectOAuthAccount(t, svc, ds, account)
	refuse := func(on bool) {
		p.mu.Lock()
		p.refuseRefresh = on
		p.mu.Unlock()
	}

	wantFailure := func(when string) *substrate.Record {
		t.Helper()
		got := mustGet(t, ds, account.Kind, account.ID)
		if got.Properties["tokenStatus"] != "erroring" {
			t.Fatalf("%s: tokenStatus = %v, want erroring", when, got.Properties["tokenStatus"])
		}
		reason, _ := got.Properties["tokenError"].(string)
		if !strings.Contains(reason, "400") || !strings.Contains(reason, "invalid_grant") {
			t.Fatalf("%s: tokenError = %q, want the status and the RFC 6749 code", when, reason)
		}
		for _, leak := range []string{"SECRET-PROVIDER-DETAIL", "s3cret", "rt-1", "at-1"} {
			if strings.Contains(reason, leak) {
				t.Fatalf("%s: tokenError carries %q: %q", when, leak, reason)
			}
		}
		at, _ := got.Properties["tokenErrorAt"].(string)
		if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
			t.Fatalf("%s: tokenErrorAt = %v, want a timestamp", when, got.Properties["tokenErrorAt"])
		}
		return got
	}
	wantCleared := func(when string) {
		t.Helper()
		got := mustGet(t, ds, account.Kind, account.ID)
		if got.Properties["tokenStatus"] != "connected" {
			t.Fatalf("%s: tokenStatus = %v, want connected", when, got.Properties["tokenStatus"])
		}
		for _, name := range []string{"tokenError", "tokenErrorAt"} {
			if v, ok := got.Properties[name]; ok {
				t.Fatalf("%s: %s = %v, want it cleared", when, name, v)
			}
		}
	}

	// The central loop. An `invalid_grant` parks the credential there
	// (TestOAuthRefreshParksAnInvalidGrantUntilReconnect), so the repeats
	// below are on-demand refreshes an invocation makes.
	refuse(true)
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
		t.Fatalf("refused refresh: %d %v", n, err)
	}
	first := wantFailure("after a refused loop refresh")
	// Past the first backoff step an invocation asks the provider again, and
	// the same reason is not written again.
	clock.Advance(time.Minute + time.Second)
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, mbEchoFn, map[string]any{}); err != nil {
		t.Fatalf("call: %v", err)
	}
	p.mu.Lock()
	calls := p.refreshCalls
	p.mu.Unlock()
	if calls != 2 {
		t.Fatalf("%d provider refresh calls, want 2: the invocation past the backoff did not ask", calls)
	}
	if again := mustGet(t, ds, account.Kind, account.ID); again.Version != first.Version {
		t.Fatalf("a repeated failure rewrote the account: version %d, then %d", first.Version, again.Version)
	}
	refuse(false)
	clock.Advance(2*time.Minute + time.Second)
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, mbEchoFn, map[string]any{}); err != nil {
		t.Fatalf("call: %v", err)
	}
	wantCleared("after a good on-demand refresh")

	// The on-demand refresh an invocation makes: the stored token expires
	// inside the inline window, so the config resolution refreshes it.
	refuse(true)
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, mbEchoFn, map[string]any{}); err != nil {
		t.Fatalf("call: %v", err)
	}
	wantFailure("after a refused on-demand refresh")

	// A reconnect clears it.
	connectOAuthAccount(t, svc, ds, account)
	wantCleared("after a reconnect")
}

// A refresh the provider refuses with `invalid_grant` parks the credential:
// the loop's next passes make no provider call, the new credential a reconnect
// writes is refreshed again, and a failure that may pass (a 503) is still
// retried every pass.
func TestOAuthRefreshParksAnInvalidGrantUntilReconnect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, p, account := installOAuthBundle(t)
	p.expiresIn = 120 // inside the 10m refresh window, outside the 1m inline one
	connectOAuthAccount(t, svc, ds, account)
	provider := func(refuse, unavailable bool) {
		p.mu.Lock()
		p.refuseRefresh, p.unavailable = refuse, unavailable
		p.mu.Unlock()
	}
	pass := func(when string, wantRefreshed, wantCalls int) {
		t.Helper()
		n, err := ds.RefreshOAuthTokens(ctx)
		if err != nil || n != wantRefreshed {
			t.Fatalf("%s: refreshed %d (%v), want %d", when, n, err, wantRefreshed)
		}
		p.mu.Lock()
		calls := p.refreshCalls
		p.mu.Unlock()
		if calls != wantCalls {
			t.Fatalf("%s: %d provider refresh calls, want %d", when, calls, wantCalls)
		}
	}

	provider(true, false)
	pass("the refused pass", 0, 1)
	if got := mustGet(t, ds, account.Kind, account.ID); got.Properties["tokenStatus"] != "erroring" {
		t.Fatalf("tokenStatus = %v, want erroring", got.Properties["tokenStatus"])
	}
	pass("the pass after invalid_grant", 0, 1)
	pass("a later pass", 0, 1)

	provider(false, false)
	connectOAuthAccount(t, svc, ds, account)
	pass("the pass after a reconnect", 1, 2)
	if got := mustGet(t, ds, account.Kind, account.ID); got.Properties["tokenStatus"] != "connected" {
		t.Fatalf("tokenStatus = %v, want connected", got.Properties["tokenStatus"])
	}

	provider(false, true)
	pass("the unavailable pass", 0, 3)
	pass("the pass after a 503", 0, 4)
	provider(false, false)
	pass("the pass after the provider recovers", 1, 5)
}

// Deleting a connected account rides the finalizer flow: the facility
// revokes the grant, drops the stored credential, releases its hold, and GC
// collects the tombstone.
func TestOAuthAccountDeletionRevokes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, p, account := installOAuthBundle(t)

	consent, err := ds.StartOAuth(ctx, owner, vocabulary.RecordPath(account.Kind, account.ID))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.CompleteOAuth(ctx, stateFrom(t, consent), "code-123"); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if _, err := ds.Delete(ctx, owner, account.Kind, account.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	// The tombstone waits on the facility's hold; the pass revokes, drops
	// the credential and releases.
	released, err := ds.ProcessOAuthFinalizers(ctx)
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

// newDatasetWithSchemaDB provisions a repository like newDataset, but keeps a raw database
// handle on the repository schema so tests can assert storage-level facts (the
// credentials table has no read API by design).
func newDatasetWithSchemaDB(t *testing.T, opts ...engine.Option) (substrate.Service, substrate.Dataset, *sql.DB) {
	t.Helper()
	dsn := engine.MigratedDSN(t)
	all := []engine.Option{
		engine.WithKindsDir(engine.SeedKindsDir),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(engine.TestCredentialKey),
	}
	all = append(all, opts...)
	svc, err := engine.OpenForTest(t, context.Background(), dsn, all...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	db, err := engine.OpenScopedDB(dsn, testdb.Repository(t), engine.RoleApp)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return svc, ds, db
}

func oauthCredentialCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	// OAuth token credentials only: secret-typed PROPERTY values (the
	// config's clientSecret) live in the same table now under the `secret:`
	// ref namespace, and teardown of an account must not be judged by them.
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sealed WHERE ref NOT LIKE 'secret:%'`).Scan(&n); err != nil {
		t.Fatalf("count credentials: %v", err)
	}
	return n
}

// barrierProvider is an OAuth provider in a box whose /token handler can be held
// at a barrier, so teardown can be interleaved with an in-flight exchange or
// refresh: the races a teardown can lose.
type barrierProvider struct {
	ts *httptest.Server

	mu           sync.Mutex
	exchanges    int
	refreshes    int
	verifiers    []string
	revoked      []string
	revokeStatus int
	expiresIn    int
	holdExchange chan struct{} // exchange waits for close when non-nil
	holdRefresh  chan struct{} // refresh waits for close when non-nil
	arrived      chan string   // provider signals "exchange"/"refresh" arrival
	// rotate, current and issued are fakeProvider's rotation.
	rotate  bool
	current string
	issued  int
	// refuseHeld answers a held refresh with `400 invalid_request` when it
	// is set by the time the refresh is released.
	refuseHeld bool
}

func newBarrierProvider(t *testing.T) *barrierProvider {
	t.Helper()
	p := &barrierProvider{expiresIn: 3600, revokeStatus: http.StatusOK}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		arrived, holdEx, holdRe := p.arrived, p.holdExchange, p.holdRefresh
		out := map[string]any{"token_type": "Bearer", "expires_in": p.expiresIn}
		grant := r.Form.Get("grant_type")
		switch grant {
		case "authorization_code":
			if r.Form.Get("code") != "code-123" || r.Form.Get("client_id") != "client-1" {
				p.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":             "invalid_grant",
					"error_description": "SECRET-PROVIDER-DETAIL client-1:s3cret must never be reflected",
				})
				return
			}
			p.exchanges++
			p.verifiers = append(p.verifiers, r.Form.Get("code_verifier"))
			p.current, p.issued = "rt-1", 1
			out["access_token"] = "at-1"
			out["refresh_token"] = "rt-1"
		case "refresh_token":
			want := "rt-1"
			if p.rotate {
				want = p.current
			}
			if r.Form.Get("refresh_token") != want {
				p.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_request"})
				return
			}
			p.refreshes++
			out["access_token"] = "at-2"
			if p.rotate {
				p.issued++
				p.current = fmt.Sprintf("rt-%d", p.issued)
				out["access_token"] = fmt.Sprintf("at-%d", p.issued)
				out["refresh_token"] = p.current
			}
		default:
			p.mu.Unlock()
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}
		p.mu.Unlock()
		if arrived != nil {
			select {
			case arrived <- grant:
			default:
			}
		}
		if grant == "authorization_code" && holdEx != nil {
			<-holdEx
		}
		if grant == "refresh_token" && holdRe != nil {
			<-holdRe
			p.mu.Lock()
			refuse := p.refuseHeld
			p.mu.Unlock()
			if refuse {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_request"})
				return
			}
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
		status := p.revokeStatus
		p.mu.Unlock()
		w.WriteHeader(status)
	})
	p.ts = httptest.NewServer(mux)
	t.Cleanup(p.ts.Close)
	return p
}

func (p *barrierProvider) configProps() map[string]any {
	return map[string]any{
		"authorizationEndpoint": p.ts.URL + "/authorize",
		"tokenEndpoint":         p.ts.URL + "/token",
		"revocationEndpoint":    p.ts.URL + "/revoke",
		"clientId":              "client-1",
		"clientSecret":          "s3cret",
		"scopes":                []any{"mail.read"},
	}
}

// installBarrierOAuthBundle stands up the mail bundle against the barrier-capable
// provider, with one pending account.
func installBarrierOAuthBundle(t *testing.T) (substrate.Service, substrate.Dataset, *sql.DB, *barrierProvider, *substrate.Record) {
	t.Helper()
	p := newBarrierProvider(t)
	svc, ds, db := newDatasetWithSchemaDB(t,
		engine.WithOAuth("test-state-key", "https://substrate.example/api/v1/substrate.reamde.dev/core/oauth/callback", p.ts.Client()),
		engine.WithCredentialKey(engine.TestCredentialKey),
	)
	docs := mbStandardDocs()
	mbPointOAuthAt(docs, p.ts.URL)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: p.configProps()})
	account := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "mail@example.com", "enabledMail": true},
	})
	return svc, ds, db, p, account
}

func connectOAuthAccount(t *testing.T, svc substrate.Service, ds substrate.Dataset, account *substrate.Record) {
	t.Helper()
	consent, err := ds.StartOAuth(context.Background(), owner, vocabulary.RecordPath(account.Kind, account.ID))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.CompleteOAuth(context.Background(), stateFrom(t, consent), "code-123"); err != nil {
		t.Fatalf("callback: %v", err)
	}
}

// A signed state completes exactly once: the pending flow is consumed
// atomically, so replaying the same state — the callback's sole
// authentication — is refused and never reaches the provider again.
func TestOAuthStateCompletesOnceAndCarriesPKCE(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, _, p, account := installBarrierOAuthBundle(t)

	consent, err := ds.StartOAuth(ctx, owner, vocabulary.RecordPath(account.Kind, account.ID))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// PKCE: the consent URL carries the S256 challenge.
	if !strings.Contains(consent, "code_challenge=") || !strings.Contains(consent, "code_challenge_method=S256") {
		t.Fatalf("consent url carries no PKCE challenge: %s", consent)
	}
	state := stateFrom(t, consent)
	if _, err := svc.CompleteOAuth(ctx, state, "code-123"); err != nil {
		t.Fatalf("first callback: %v", err)
	}
	p.mu.Lock()
	exchanges, verifiers := p.exchanges, append([]string(nil), p.verifiers...)
	p.mu.Unlock()
	if exchanges != 1 || len(verifiers) != 1 || verifiers[0] == "" {
		t.Fatalf("first exchange: %d, verifiers %v — the code must redeem WITH the flow's verifier", exchanges, verifiers)
	}

	// The replay: same signed state, same (or another) valid code.
	if _, err := svc.CompleteOAuth(ctx, state, "code-123"); err == nil {
		t.Fatal("a replayed state completed a second time")
	} else if !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("replay refusal: %v", err)
	}
	p.mu.Lock()
	exchanges = p.exchanges
	p.mu.Unlock()
	if exchanges != 1 {
		t.Fatalf("the replay reached the provider: %d exchanges", exchanges)
	}
}

// WithOAuth with an empty state key refuses the boot: an empty HMAC key
// makes every state forgeable, and the state is the unauthenticated
// callback's whole authentication.
func TestOAuthEmptyStateKeyRefusesTheBoot(t *testing.T) {
	t.Parallel()
	dsn := engine.MigratedDSN(t)
	_, err := engine.OpenForTest(t, context.Background(), dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithKindsDir(engine.SeedKindsDir),
		engine.WithOAuth("", "https://substrate.example/callback", nil),
	)
	if err == nil || !strings.Contains(err.Error(), "state key") {
		t.Fatalf("an empty oauth state key must refuse the boot, got: %v", err)
	}
}

func TestOAuthExchangeErrorHidesProviderDetail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, _, _, account := installBarrierOAuthBundle(t)

	consent, err := ds.StartOAuth(ctx, owner, vocabulary.RecordPath(account.Kind, account.ID))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	_, err = svc.CompleteOAuth(ctx, stateFrom(t, consent), "wrong-code")
	if err == nil {
		t.Fatal("a bad code completed")
	}
	if strings.Contains(err.Error(), "SECRET-PROVIDER-DETAIL") || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("the exchange error reflects provider detail: %v", err)
	}
}

// A delete that lands while the code exchange is blocked at the provider
// wins: the callback's one transaction re-checks the account live and
// refuses, so no unreferenced credential is ever stored.
func TestOAuthCallbackRefusesAnAccountDeletedMidExchange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, db, p, account := installBarrierOAuthBundle(t)

	consent, err := ds.StartOAuth(ctx, owner, vocabulary.RecordPath(account.Kind, account.ID))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	p.mu.Lock()
	p.holdExchange = make(chan struct{})
	p.arrived = make(chan string, 2)
	p.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		_, err := svc.CompleteOAuth(ctx, stateFrom(t, consent), "code-123")
		done <- err
	}()
	select {
	case <-p.arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the exchange never reached the provider")
	}
	// The exchange is blocked at the provider; the account dies meanwhile.
	if _, err := ds.Delete(ctx, owner, account.Kind, account.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete during exchange: %v", err)
	}
	if _, err := ds.ProcessOAuthFinalizers(ctx); err != nil {
		t.Fatalf("finalizers: %v", err)
	}
	close(p.holdExchange)
	if err := <-done; err == nil {
		t.Fatal("the callback connected a deleted account")
	}
	if n := oauthCredentialCount(t, db); n != 0 {
		t.Fatalf("an orphan credential survived the callback-versus-delete race: %d rows", n)
	}
}

// A refresh blocked at the provider while the finalizer tears the account
// down must not recreate the deleted credential: persistence is update-only
// with a live-account compare-and-swap, never an upsert.
func TestOAuthRefreshDoesNotRecreateATornDownCredential(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, db, p, account := installBarrierOAuthBundle(t)
	p.mu.Lock()
	p.expiresIn = 120 // inside the 10m refresh window
	p.mu.Unlock()
	connectOAuthAccount(t, svc, ds, account)
	if n := oauthCredentialCount(t, db); n != 1 {
		t.Fatalf("connected credential rows: %d", n)
	}

	p.mu.Lock()
	p.holdRefresh = make(chan struct{})
	p.arrived = make(chan string, 2)
	p.mu.Unlock()

	type res struct {
		n   int
		err error
	}
	done := make(chan res, 1)
	go func() {
		n, err := ds.RefreshOAuthTokens(ctx)
		done <- res{n, err}
	}()
	select {
	case <-p.arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the refresh never reached the provider")
	}
	// The refresh is blocked at the provider; teardown completes meanwhile.
	if _, err := ds.Delete(ctx, owner, account.Kind, account.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete during refresh: %v", err)
	}
	if released, err := ds.ProcessOAuthFinalizers(ctx); err != nil || released != 1 {
		t.Fatalf("finalizers: %d %v", released, err)
	}
	if n := oauthCredentialCount(t, db); n != 0 {
		t.Fatalf("teardown left credentials: %d", n)
	}
	close(p.holdRefresh)
	r := <-done
	if r.err != nil {
		t.Fatalf("refresh pass: %v", r.err)
	}
	if r.n != 0 {
		t.Fatalf("a refresh against a torn-down account counted as refreshed: %d", r.n)
	}
	if n := oauthCredentialCount(t, db); n != 0 {
		t.Fatalf("the late refresh recreated an orphan credential: %d rows", n)
	}
}

// Purging a bundle with connected accounts revokes the provider grants: the
// accounts tombstone FIRST and their finalizers run while the configuration
// record — the revocation endpoint's home — is still live.
func TestBundlePurgeRevokesItsConnectedAccounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, db, p, account := installBarrierOAuthBundle(t)
	connectOAuthAccount(t, svc, ds, account)

	if err := ds.DisableBundle(ctx, mbPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := ds.PurgeBundle(ctx, mbPackage); err != nil {
		t.Fatalf("purge: %v", err)
	}
	p.mu.Lock()
	revoked := append([]string(nil), p.revoked...)
	p.mu.Unlock()
	if len(revoked) != 1 || revoked[0] != "rt-1" {
		t.Fatalf("purge did not revoke the provider grant: %v", revoked)
	}
	if n := oauthCredentialCount(t, db); n != 0 {
		t.Fatalf("purge left credentials: %d", n)
	}
	st, err := ds.BundleStatus(ctx, mbPackage)
	if err != nil || st.LiveRecords != 0 {
		t.Fatalf("post-purge status: %+v %v", st, err)
	}
}

func TestOAuthRevokeFailureStillReleasesTheAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, db, p, account := installBarrierOAuthBundle(t)
	connectOAuthAccount(t, svc, ds, account)
	p.mu.Lock()
	p.revokeStatus = http.StatusInternalServerError
	p.mu.Unlock()

	if _, err := ds.Delete(ctx, owner, account.Kind, account.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	released, err := ds.ProcessOAuthFinalizers(ctx)
	if err != nil || released != 1 {
		t.Fatalf("a provider 500 on revoke must not strand the deletion: %d %v", released, err)
	}
	p.mu.Lock()
	attempts := len(p.revoked)
	p.mu.Unlock()
	if attempts != 1 {
		t.Fatalf("revoke attempts: %d", attempts)
	}
	if n := oauthCredentialCount(t, db); n != 0 {
		t.Fatalf("credentials survived the release: %d", n)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if _, err := ds.Get(ctx, account.Kind, account.ID); err == nil {
		t.Fatal("account survived gc")
	}
}

// mbConfigOf returns the id of the bundle's single live config record.
func mbConfigOf(t *testing.T, ds substrate.Dataset) string {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{mbConfigType}},
	})
	if err != nil || len(page.Records) == 0 {
		t.Fatalf("no config record: %v", err)
	}
	return page.Records[0].ID
}

// scopeOf lifts the space-joined scope out of a consent URL.
func scopeOf(t *testing.T, consentURL string) string {
	t.Helper()
	u, err := url.Parse(consentURL)
	if err != nil {
		t.Fatalf("parse consent url: %v", err)
	}
	return u.Query().Get("scope")
}

// A config patch that sets a would-be endpoint property does NOT redirect
// the flow — the consent URL and the token exchange both use the manifest's
// (fake-provider) endpoints. Scopes derive from the account's toggles.
func TestOAuthEndpointsComeFromTheManifestNotTheConfigRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, p, account := installOAuthBundle(t)

	// An authenticated caller patches a would-be token endpoint on the config.
	// The property still exists on the mail config kind, but the engine ignores
	// it — endpoints come only from the manifest.
	mustPatch(t, ds, owner, mbConfigType, mbConfigOf(t, ds), substrate.PatchInput{
		Properties: map[string]any{
			"authorizationEndpoint": "https://evil.example/authorize",
			"tokenEndpoint":         "https://evil.example/token",
		},
	})

	consent, err := ds.StartOAuth(ctx, owner, vocabulary.RecordPath(account.Kind, account.ID))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.HasPrefix(consent, p.ts.URL+"/authorize") {
		t.Fatalf("consent url was redirected off the manifest endpoint: %s", consent)
	}
	// Scopes derive from the account's enabled toggles (enabledMail=true).
	if scope := scopeOf(t, consent); !strings.Contains(scope, "mail.read") {
		t.Fatalf("consent scope not derived from toggles: %q", scope)
	}

	// The code exchange lands on the MANIFEST token endpoint (the fake
	// provider), never the patched evil one — the exchange succeeds.
	if _, err := svc.CompleteOAuth(ctx, stateFrom(t, consent), "code-123"); err != nil {
		t.Fatalf("callback (exchange must hit the manifest endpoint): %v", err)
	}
	if p.exchanges != 1 {
		t.Fatalf("the exchange did not reach the manifest (fake) provider: %d", p.exchanges)
	}
}

// A disabled toggle requests NO scope: an unwired feature (here, a
// toggled-off enabledMail) is never requested.
func TestOAuthScopesComeFromTheAccountToggles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _, account := installOAuthBundle(t)

	mustPatch(t, ds, owner, account.Kind, account.ID, substrate.PatchInput{
		Properties: map[string]any{"enabledMail": false},
	})
	consent, err := ds.StartOAuth(ctx, owner, vocabulary.RecordPath(account.Kind, account.ID))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if scope := scopeOf(t, consent); scope != "" {
		t.Fatalf("a disabled toggle still requested scope %q", scope)
	}
}

// oauth/start is owner-gated: a bundle actor is refused.
func TestOAuthStartRefusesANonOwnerActor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _, account := installOAuthBundle(t)
	connector := substrate.FunctionActor(vocabulary.SplitKindRef(mbEchoFn))
	_, err := ds.StartOAuth(ctx, connector, vocabulary.RecordPath(account.Kind, account.ID))
	wantErr(t, err, substrate.ErrForbidden, "non-owner oauth/start")
}

// External create and delete of a config or account record are owner-only.
func TestAccountRecordCreateAndDeleteAreOwnerOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _, account := installOAuthBundle(t)
	connector := substrate.FunctionActor(vocabulary.SplitKindRef(mbEchoFn))

	if _, err := ds.Put(ctx, connector, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "intruder@example.com"},
	}); err == nil {
		t.Fatal("a non-owner created an account record")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "non-owner account create")
	}
	if _, err := ds.Delete(ctx, connector, account.Kind, account.ID, substrate.DeleteInput{}); err == nil {
		t.Fatal("a non-owner deleted an account record")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "non-owner account delete")
	}
}

// Per-property ownership: tokenRef and tokenStatus are the OAuth facility's;
// syncToken is the connector's; the owner writes neither. The owner writes the
// account's email and toggles.
func TestAccountPropertiesAreWriterOwned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _, account := installOAuthBundle(t)

	for _, prop := range []map[string]any{
		{"tokenRef": "forged-ref"},
		{"tokenStatus": "connected"},
		{"syncToken": "forged-sync"},
	} {
		if _, err := ds.Patch(ctx, owner, account.Kind, account.ID, substrate.PatchInput{Properties: prop}); err == nil {
			t.Fatalf("owner wrote a host/connector-owned property %v", prop)
		} else {
			wantErr(t, err, substrate.ErrForbidden, "owner writes a restricted property")
		}
	}
	// A connector actor may write its own sync state, but not the owner's
	// account settings.
	connector := substrate.FunctionActor(vocabulary.SplitKindRef(mbEchoFn))
	if _, err := ds.Patch(ctx, connector, account.Kind, account.ID, substrate.PatchInput{
		Properties: map[string]any{"syncToken": "s1"},
	}); err != nil {
		t.Fatalf("the connector could not write its own syncToken: %v", err)
	}
	if _, err := ds.Patch(ctx, connector, account.Kind, account.ID, substrate.PatchInput{
		Properties: map[string]any{"address": "hijack@example.com"},
	}); err == nil {
		t.Fatal("the connector wrote an owner-owned property")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "connector writes an owner property")
	}
	// The owner writes what is theirs.
	mustPatch(t, ds, owner, account.Kind, account.ID, substrate.PatchInput{
		Properties: map[string]any{"address": "owner@example.com", "enabledMail": true},
	})
}

// A SECOND PROVIDER'S ACCOUNT KIND, so two accountconfig kinds can hold a row
// under the same id — which is what a repository that named both accounts
// `owner` has (issue #574). It ships no oauth2 metadata: nothing here connects
// it, it exists to be the other row a bare-id refusal lists.
const (
	chatPackage     = "chat.bundles.substrate.reamde.dev/chat"
	chatAccountType = chatPackage + "/chataccount"
)

func chatBundleDocs() []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(chatPackage, 0),
		vocabulary.ActorManifest(chatPackage, vocabulary.PackageActor(chatPackage)),
		vocabulary.BundleManifest(chatPackage, map[string]any{
			"description": "the chat bundle",
			"installs":    []any{chatAccountType},
		}),
		vocabulary.KindManifest(chatPackage,
			map[string]any{"singular": "chataccount"},
			map[string]any{
				"traits": []any{"substrate.reamde.dev/core/accountconfig"},
				"properties": map[string]any{
					"tokenRef":      map[string]any{"type": "secret", "writer": "oauth"},
					"tokenStatus":   map[string]any{"type": "string", "writer": "oauth"},
					"grantedScopes": map[string]any{"type": "string", "repeated": true, "writer": "oauth"},
					"handle":        map[string]any{"type": "string", "writer": "owner"},
				},
			}),
	}
}

// TWO PROVIDERS, ONE ACCOUNT NAME. A bare id names nothing on its own —
// identity is the (kind, id) pair — so the surface takes the full identity
// and nothing shorter (record 0102), and the refusal lists both spellings the
// repository holds. The callback holds the flow row to the state's path, so
// the consent lands on the account it was started for.
func TestOAuthStartTakesTheAccountsFullIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, _, _ := installOAuthBundle(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, chatBundleDocs()); err != nil {
		t.Fatalf("install the second bundle: %v", err)
	}
	mail := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, ID: "owner",
		Properties: map[string]any{"address": "geo@example.com", "enabledMail": true},
	})
	chat := mustPut(t, ds, owner, substrate.PutInput{
		Kind: chatAccountType, ID: "owner", Properties: map[string]any{"handle": "geo"},
	})

	// The bare id is refused as a bare id, not as an ambiguity, and the
	// refusal hands back both identities, in kind order, in the spelling that
	// works.
	_, err := ds.StartOAuth(ctx, owner, "owner")
	wantErr(t, err, substrate.ErrValidation, "a bare id held by two account kinds")
	for _, want := range []string{
		`"owner" is a bare record id`,
		"named in full as <authority>/<package>/<kind>/<id>",
		"this repository holds " + chatAccountType + "/owner, " + mbAccountType + "/owner",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}

	// The full identity resolves, and so does the callback: the mail account
	// is connected and the chat account of the same name is untouched.
	consent, err := ds.StartOAuth(ctx, owner, vocabulary.RecordPath(mail.Kind, mail.ID))
	if err != nil {
		t.Fatalf("start by full identity: %v", err)
	}
	record, err := svc.CompleteOAuth(ctx, stateFrom(t, consent), "code-123")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if record != vocabulary.RecordPath(mail.Kind, mail.ID) {
		t.Fatalf("callback record: %s", record)
	}
	if got := mustGet(t, ds, mail.Kind, mail.ID); got.Properties["tokenStatus"] != "connected" {
		t.Fatalf("the addressed account is %v, not connected", got.Properties["tokenStatus"])
	}
	if got := mustGet(t, ds, chat.Kind, chat.ID); got.Properties["tokenStatus"] != nil {
		t.Fatalf("the OTHER provider's account of the same name was connected: %v", got.Properties)
	}

	// A full identity that names no row is a not-found, not a search.
	_, err = ds.StartOAuth(ctx, owner, vocabulary.RecordPath(mbAccountType, "nosuch"))
	wantErr(t, err, substrate.ErrNotFound, "a full identity naming no account")
}

// syncAccountDocs is the mail closure with its account kind bound to the
// core `sync` trait and declaring the facility's tokenError and tokenErrorAt
// (as the shipped github and google account kinds do), carrying extra
// members beside the standard ones, its OAuth endpoints pointed at baseURL.
func syncAccountDocs(baseURL string, extra ...map[string]any) []map[string]any {
	account := mbAccountTypeDoc()
	data, _ := account["data"].(map[string]any)
	data["traits"] = append(data["traits"].([]any), "substrate.reamde.dev/core/sync")
	props, _ := data["properties"].(map[string]any)
	for name, decl := range syncProps() {
		if name != "name" {
			props[name] = decl
		}
	}
	props["tokenError"] = map[string]any{"type": "string", "writer": "oauth"}
	props["tokenErrorAt"] = map[string]any{"type": "datetime", "writer": "oauth"}
	members := append([]map[string]any{
		mbConfigTypeDoc(), account, mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("mark", mbMarkSource), mbFnDoc("echo", mbEchoSource),
	}, extra...)
	docs := mbDocs(nil, members...)
	mbPointOAuthAt(docs, baseURL)
	return docs
}

// installSyncOAuthBundle stands up the oauth facility, the sync-trait mail
// closure (syncAccountDocs), its config record and one pending account.
func installSyncOAuthBundle(t *testing.T, extra ...map[string]any) (substrate.Service, substrate.Dataset, *fakeProvider, *substrate.Record) {
	t.Helper()
	p := newFakeProvider(t)
	svc, ds := newDataset(t,
		engine.WithOAuth("test-state-key", "https://substrate.example/api/v1/substrate.reamde.dev/core/oauth/callback", p.ts.Client()),
		engine.WithCredentialKey(engine.TestCredentialKey),
	)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, syncAccountDocs(p.ts.URL, extra...)); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: p.configProps()})
	account := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "geo@example.com", "enabledMail": true},
	})
	return svc, ds, p, account
}

// syncConnector is a function actor of the mail package: the connector's
// hand on the account's sync properties.
var syncConnector = substrate.FunctionActor(vocabulary.SplitKindRef(mbEchoFn))

// failSyncedAccount puts the account where a failed refresh and the run it
// broke leave it: synced once, its last run erroring with the refresh's
// cause, and the facility's own tokenStatus at erroring.
func failSyncedAccount(t *testing.T, ds substrate.Dataset, p *fakeProvider, account *substrate.Record) {
	t.Helper()
	mustPatch(t, ds, syncConnector, account.Kind, account.ID, substrate.PatchInput{Properties: map[string]any{
		"lastSyncedAt":      "2026-10-19T10:00:00Z",
		"lastSyncStartedAt": "2026-10-20T06:00:00Z",
		"syncState":         substrate.SyncStateErroring,
		"syncError":         "oauthflow: refresh: provider answered 400, error code invalid_request",
		"syncErrorAt":       "2026-10-20T06:00:01Z",
	}})
	p.mu.Lock()
	p.refuseRefresh = true
	p.mu.Unlock()
	if _, err := ds.RefreshOAuthTokens(context.Background()); err != nil {
		t.Fatalf("refused refresh: %v", err)
	}
	p.mu.Lock()
	p.refuseRefresh = false
	p.mu.Unlock()
	if got := mustGet(t, ds, account.Kind, account.ID); got.Properties["tokenStatus"] != "erroring" {
		t.Fatalf("tokenStatus = %v after a refused refresh, want erroring", got.Properties["tokenStatus"])
	}
}

// accountChanges lists the changelog entries addressed to the account.
func accountChanges(t *testing.T, ds substrate.Dataset, account *substrate.Record) []substrate.Change {
	t.Helper()
	out, err := ds.Changes(context.Background(), 0, substrate.ChangeFilter{
		Kinds: []string{account.Kind}, RecordID: account.ID,
	}, 500)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	return out
}

// TestOAuthReconnectClearsTheSyncError: the error pair on a `sync`-trait
// account describes the grant a reconnect replaces, so the reconnect clears
// it. The pair is `writer: connector`, which the facility's actor may not
// write; the clear goes through the bundle tier. The state stays for the
// next run to settle, and an account that never synced is asked for no run:
// its first sync is the on-connect trigger's.
func TestOAuthReconnectClearsTheSyncError(t *testing.T) {
	t.Parallel()
	svc, ds, _, account := installSyncOAuthBundle(t)
	connectOAuthAccount(t, svc, ds, account)

	mustPatch(t, ds, syncConnector, account.Kind, account.ID, substrate.PatchInput{Properties: map[string]any{
		"syncState":   substrate.SyncStateErroring,
		"syncError":   "token refresh failed: invalid_grant",
		"syncErrorAt": "2026-09-20T10:00:00Z",
	}})

	connectOAuthAccount(t, svc, ds, account)

	got := mustGet(t, ds, account.Kind, account.ID)
	if got.Properties["tokenStatus"] != "connected" {
		t.Fatalf("tokenStatus = %v, want connected", got.Properties["tokenStatus"])
	}
	for _, name := range []string{"syncError", "syncErrorAt"} {
		if v, ok := got.Properties[name]; ok {
			t.Errorf("%s survived the reconnect: %v", name, v)
		}
	}
	if got.Properties["syncState"] != substrate.SyncStateErroring {
		t.Errorf("syncState = %v, want the stored erroring left for the next run", got.Properties["syncState"])
	}
	if v, ok := got.Properties["syncRequestedAt"]; ok {
		t.Errorf("an account that never synced was asked for a sync: syncRequestedAt = %v", v)
	}
}

// TestOAuthFirstConnectRequestsNoSync: the first connect leaves the first
// sync to the provider's on-connect trigger; a request beside it would run
// the backfill twice.
func TestOAuthFirstConnectRequestsNoSync(t *testing.T) {
	t.Parallel()
	svc, ds, _, account := installSyncOAuthBundle(t)
	connectOAuthAccount(t, svc, ds, account)

	got := mustGet(t, ds, account.Kind, account.ID)
	if got.Properties["tokenStatus"] != "connected" {
		t.Fatalf("tokenStatus = %v, want connected", got.Properties["tokenStatus"])
	}
	if v, ok := got.Properties["syncRequestedAt"]; ok {
		t.Fatalf("the first connect stamped syncRequestedAt = %v", v)
	}
}

// TestOAuthReconnectRequestsASync: reconnecting an account that synced
// before and is failing asks for a sync, so the run that says whether the new
// grant works happens now. The request, the new status and the cleared error
// pair are ONE changelog entry, the facility's, so a record trigger sees them
// together; each property's manager row stands at its writer's tier.
func TestOAuthReconnectRequestsASync(t *testing.T) {
	t.Parallel()
	svc, ds, p, account := installSyncOAuthBundle(t)
	p.expiresIn = 30 // inside the refresh loop's window
	connectOAuthAccount(t, svc, ds, account)
	failSyncedAccount(t, ds, p, account)
	before := len(accountChanges(t, ds, account))

	connectOAuthAccount(t, svc, ds, account)

	changes := accountChanges(t, ds, account)
	if len(changes) != before+1 {
		t.Fatalf("the reconnect wrote %d entries on the account, want 1: %+v", len(changes)-before, changes[before:])
	}
	entry := changes[before]
	if entry.Actor != "substrate.oauth" {
		t.Fatalf("the reconnect entry's actor = %s, want the OAuth facility", entry.Actor)
	}
	written := map[string]bool{}
	names, _ := entry.Payload["properties"].([]any)
	for _, n := range names {
		written[fmt.Sprint(n)] = true
	}
	for _, name := range []string{"tokenStatus", "syncRequestedAt", "syncError", "syncErrorAt"} {
		if !written[name] {
			t.Errorf("the reconnect entry does not carry %s: %v", name, names)
		}
	}
	if written["syncState"] {
		t.Errorf("the reconnect wrote syncState: %v", names)
	}

	got := mustGet(t, ds, account.Kind, account.ID)
	if got.Properties["tokenStatus"] != "connected" {
		t.Fatalf("tokenStatus = %v, want connected", got.Properties["tokenStatus"])
	}
	for _, name := range []string{"syncError", "syncErrorAt"} {
		if v, ok := got.Properties[name]; ok {
			t.Errorf("%s survived the reconnect: %v", name, v)
		}
	}
	if got.Properties["syncState"] != substrate.SyncStateErroring {
		t.Errorf("syncState = %v, want erroring left for the requested run", got.Properties["syncState"])
	}
	requested := propTime(t, got, "syncRequestedAt")
	if started := propTime(t, got, "lastSyncStartedAt"); !requested.After(started) {
		t.Errorf("syncRequestedAt %s is not after the failed run's start %s", requested, started)
	}
	if m := got.PropertyMeta["syncRequestedAt"]; m.Manager != "substrate.oauth" || m.Tier != substrate.TierOwner {
		t.Errorf("syncRequestedAt manager = %+v, want the facility at the owner tier", m)
	}
	if m := got.PropertyMeta["tokenStatus"]; m.Manager != "substrate.oauth" || m.Tier != substrate.TierMachine {
		t.Errorf("tokenStatus manager = %+v, want the facility at the machine tier", m)
	}
}

// TestOAuthReconnectOfAPausedAccountRequestsNoSync: the owner's pause stops
// the sync, and a reconnect does not undo it by asking for a run.
func TestOAuthReconnectOfAPausedAccountRequestsNoSync(t *testing.T) {
	t.Parallel()
	svc, ds, p, account := installSyncOAuthBundle(t)
	p.expiresIn = 30
	connectOAuthAccount(t, svc, ds, account)
	failSyncedAccount(t, ds, p, account)
	mustPatch(t, ds, owner, account.Kind, account.ID, substrate.PatchInput{Properties: map[string]any{"syncPaused": true}})

	connectOAuthAccount(t, svc, ds, account)

	got := mustGet(t, ds, account.Kind, account.ID)
	if v, ok := got.Properties["syncRequestedAt"]; ok {
		t.Fatalf("a paused account was asked for a sync: syncRequestedAt = %v", v)
	}
	if _, ok := got.Properties["syncError"]; ok {
		t.Errorf("the error pair survived the reconnect of a paused account: %v", got.Properties)
	}
}

// TestSyncStatusCarriesTheSignIn: a refused refresh shows on the account's
// sync status row as well as on the record, so a reader of the status alone
// can tell a dead sign-in from a failing run; a reconnect clears it there.
func TestSyncStatusCarriesTheSignIn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, p, account := installSyncOAuthBundle(t)
	p.expiresIn = 30
	connectOAuthAccount(t, svc, ds, account)
	failSyncedAccount(t, ds, p, account)

	row := func() substrate.SyncStatus {
		t.Helper()
		statuses, err := ds.SyncStatuses(ctx)
		if err != nil {
			t.Fatalf("sync statuses: %v", err)
		}
		for _, s := range statuses {
			if s.Kind == account.Kind && s.ID == account.ID {
				return s
			}
		}
		t.Fatalf("sync statuses = %+v, want a row for the account", statuses)
		return substrate.SyncStatus{}
	}

	s := row()
	if s.TokenStatus != "erroring" {
		t.Fatalf("tokenStatus = %q, want erroring", s.TokenStatus)
	}
	if !strings.Contains(s.TokenError, "400") || !strings.Contains(s.TokenError, "invalid_grant") {
		t.Fatalf("tokenError = %q, want the status and the RFC 6749 code", s.TokenError)
	}
	if s.TokenErrorAt == nil {
		t.Fatal("tokenErrorAt is absent on an erroring sign-in")
	}
	if want := propTime(t, mustGet(t, ds, account.Kind, account.ID), "tokenErrorAt"); !s.TokenErrorAt.Equal(want) {
		t.Fatalf("tokenErrorAt = %s, want the record's %s", s.TokenErrorAt, want)
	}

	connectOAuthAccount(t, svc, ds, account)
	s = row()
	if s.TokenStatus != "connected" || s.TokenError != "" || s.TokenErrorAt != nil {
		t.Fatalf("after a reconnect: tokenStatus %q, tokenError %q, tokenErrorAt %v; want connected and no reason",
			s.TokenStatus, s.TokenError, s.TokenErrorAt)
	}
}

// TestOAuthReconnectRequestDeliversOnce: the request the reconnect stamps
// fires the account's on-request trigger exactly once, and the run's outcome
// is what the account says next: a healthy run acknowledges the request and
// settles ok, a failing one parks and stamps erroring again with a start
// after the request, which is what brings the failure back on the console.
func TestOAuthReconnectRequestDeliversOnce(t *testing.T) {
	t.Parallel()
	const okSource = `
def main(input, host):
    env = input["envelope"]
    props = env["record"]["properties"]
    return {"effects": [{"action": "patch", "kind": env["change"]["kind"], "id": env["change"]["id"],
                         "properties": {"syncRequestedAck": props["syncRequestedAt"],
                                        "lastSyncedAt": "2026-10-20T12:00:00Z"}}]}
`
	const failSource = `
def main(input, host):
    raise RuntimeError("oauthflow: refresh: provider answered 400, error code invalid_request")
`
	for _, tc := range []struct {
		name, source string
		ok           bool
	}{
		{"a healthy run settles ok", okSource, true},
		{"a failing run is erroring again", failSource, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			fn := vocabulary.FunctionManifest(mbPackage, "accountsync", map[string]any{
				"description": "syncs one mail account",
				"runtime":     vocabulary.RuntimePython,
				"source":      tc.source,
				"permissions": map[string]any{"writes": []any{mbAccountType}},
			})
			svc, ds, p, account := installSyncOAuthBundle(t, fn)
			p.expiresIn = 30
			connectOAuthAccount(t, svc, ds, account)
			failSyncedAccount(t, ds, p, account)
			// The guard every shipped provider's on-request trigger carries:
			// connected, and a request its acknowledgement has not answered.
			const triggerID = "mail-on-request"
			mustPut(t, ds, owner, substrate.PutInput{
				Kind: "substrate.reamde.dev/core/trigger", ID: triggerID,
				Properties: map[string]any{
					"enabled": true,
					"source": map[string]any{"record": map[string]any{
						"kinds": []any{mbAccountType}, "ops": []any{"create", "update"},
						"when": `record != null && "tokenStatus" in record.properties && record.properties.tokenStatus == "connected" && "syncRequestedAt" in record.properties && (!("syncRequestedAck" in record.properties) || string(record.properties.syncRequestedAck) != string(record.properties.syncRequestedAt))`,
					}},
					"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", mbPackage+"/accountsync"),
				},
			})

			connectOAuthAccount(t, svc, ds, account)
			process(t, ds)
			process(t, ds)

			got := mustGet(t, ds, account.Kind, account.ID)
			requested := propTime(t, got, "syncRequestedAt")
			if tc.ok {
				if runs := runRowsOf(t, ds, triggerID, "ok"); len(runs) != 1 {
					t.Fatalf("ok runs = %d, want 1", len(runs))
				}
				if got.Properties["syncState"] != substrate.SyncStateOK {
					t.Fatalf("syncState = %v, want ok (properties %v)", got.Properties["syncState"], got.Properties)
				}
				if got.Properties["syncRequestedAck"] == nil || !propTime(t, got, "syncRequestedAck").Equal(requested) {
					t.Fatalf("syncRequestedAck = %v, want the request %s", got.Properties["syncRequestedAck"], requested)
				}
				return
			}
			parked, err := ds.TriggerFailures(ctx, triggerID)
			if err != nil {
				t.Fatalf("failures: %v", err)
			}
			if len(parked) != 1 {
				t.Fatalf("parked = %d, want the one requested run", len(parked))
			}
			if got.Properties["syncState"] != substrate.SyncStateErroring {
				t.Fatalf("syncState = %v, want erroring", got.Properties["syncState"])
			}
			if msg, _ := got.Properties["syncError"].(string); !strings.Contains(msg, "provider answered 400") {
				t.Fatalf("syncError = %q, want the run's cause", msg)
			}
			if started := propTime(t, got, "lastSyncStartedAt"); !started.After(requested) {
				t.Fatalf("lastSyncStartedAt %s is not after the request %s", started, requested)
			}
		})
	}
}

// propTime reads a datetime property off a record.
func propTime(t *testing.T, r *substrate.Record, name string) time.Time {
	t.Helper()
	s, _ := r.Properties[name].(string)
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("%s = %v, want a timestamp", name, r.Properties[name])
	}
	return at
}
