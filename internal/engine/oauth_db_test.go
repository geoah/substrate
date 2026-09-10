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

	consent, err := ds.StartOAuth(ctx, owner, account.ID)
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

	consent, err := ds.StartOAuth(ctx, owner, account.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.CompleteOAuth(ctx, stateFrom(t, consent), "code-123"); err != nil {
		t.Fatalf("callback: %v", err)
	}

	// The echo function answers with its resolved config.
	out, _, err := ds.CallFunction(ctx, mbEchoFn, map[string]any{})
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
	out, _, err = ds.CallFunction(ctx, mbEchoFn, map[string]any{})
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
	svc, ds, p, account := installOAuthBundle(t)

	consent, err := ds.StartOAuth(ctx, owner, account.ID)
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
		engine.WithKindsDir(engine.CoreKindsDir),
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
			out["access_token"] = "at-1"
			out["refresh_token"] = "rt-1"
		case "refresh_token":
			if r.Form.Get("refresh_token") != "rt-1" {
				p.mu.Unlock()
				http.Error(w, "bad refresh", http.StatusBadRequest)
				return
			}
			p.refreshes++
			out["access_token"] = "at-2"
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

func connectOAuthAccount(t *testing.T, svc substrate.Service, ds substrate.Dataset, accountID string) {
	t.Helper()
	consent, err := ds.StartOAuth(context.Background(), owner, accountID)
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

	consent, err := ds.StartOAuth(ctx, owner, account.ID)
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
		engine.WithKindsDir(engine.CoreKindsDir),
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

	consent, err := ds.StartOAuth(ctx, owner, account.ID)
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

	consent, err := ds.StartOAuth(ctx, owner, account.ID)
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
	connectOAuthAccount(t, svc, ds, account.ID)
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
	connectOAuthAccount(t, svc, ds, account.ID)

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
	connectOAuthAccount(t, svc, ds, account.ID)
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

	consent, err := ds.StartOAuth(ctx, owner, account.ID)
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
	consent, err := ds.StartOAuth(ctx, owner, account.ID)
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
	_, err := ds.StartOAuth(ctx, connector, account.ID)
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
