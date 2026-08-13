package engine_test

// Wave-3 fixes, bundle/OAuth/lifecycle half
// : shadow-trait admission (#2),
// purge-time revocation (#4), refresh/callback teardown barriers (#5), the
// per-bundle lifecycle fence and the purging transition (#7), merge/split
// bundle admission (#8), one-time OAuth state + PKCE + the state-key
// requirement (#9), bundled-agent upgrade guards (#10), the trigger-versus-
// upgrade registry barrier (#11), callback error sanitization (#15) and
// revoke status checking (#16).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// --- environment ------------------------------------------------------------------

// newW3Env provisions a repository like newDataset, but keeps a raw database
// handle on the repository schema so tests can assert storage-level facts (the
// credentials table has no read API by design).
func newW3Env(t *testing.T, opts ...engine.Option) (substrate.Service, substrate.Dataset, *sql.DB) {
	t.Helper()
	dsn := testdb.NewSchema(t)
	all := []engine.Option{
		engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
	}
	all = append(all, opts...)
	svc, err := engine.Open(context.Background(), dsn, all...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return svc, ds, db
}

func w3CredentialCount(t *testing.T, db *sql.DB) int {
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

// --- a fake provider with barriers -------------------------------------------------

// w3Provider is an OAuth provider in a box whose /token handler can be held
// at a barrier, so teardown can be interleaved with an in-flight exchange or
// refresh — the races findings #4/#5 are about.
type w3Provider struct {
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

func newW3Provider(t *testing.T) *w3Provider {
	t.Helper()
	p := &w3Provider{expiresIn: 3600, revokeStatus: http.StatusOK}
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

func (p *w3Provider) configProps() map[string]any {
	return map[string]any{
		"authorizationEndpoint": p.ts.URL + "/authorize",
		"tokenEndpoint":         p.ts.URL + "/token",
		"revocationEndpoint":    p.ts.URL + "/revoke",
		"clientId":              "client-1",
		"clientSecret":          "s3cret",
		"scopes":                []any{"mail.read"},
	}
}

// installW3OAuthBundle stands up the mail bundle against the barrier-capable
// provider, with one pending account.
func installW3OAuthBundle(t *testing.T) (substrate.Service, substrate.Dataset, *sql.DB, bundleOps, *w3Provider, *substrate.Record) {
	t.Helper()
	p := newW3Provider(t)
	svc, ds, db := newW3Env(t,
		engine.WithOAuth("w3-state-key", "https://substrate.example/api/v1/core.substrate.reamde.dev/oauth/callback", p.ts.Client()),
		engine.WithCredentialKey("w3-cred-key"),
	)
	docs := mbStandardDocs()
	mbPointOAuthAt(docs, p.ts.URL)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: p.configProps()})
	account := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "w3@example.com", "enabledMail": true},
	})
	return svc, ds, db, bundler(t, ds), p, account
}

func w3Connect(t *testing.T, svc substrate.Service, ops bundleOps, accountID string) {
	t.Helper()
	consent, err := ops.StartOAuth(context.Background(), owner, accountID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.(oauthCompleter).CompleteOAuth(context.Background(), stateFrom(t, consent), "code-123"); err != nil {
		t.Fatalf("callback: %v", err)
	}
}

// --- #9: one-time state, PKCE, non-empty key ---------------------------------------

// A signed state completes exactly once: the pending flow is consumed
// atomically, so replaying the same state — the callback's sole
// authentication — is refused and never reaches the provider again.
func TestW3OAuthStateReplayRefused(t *testing.T) {
	ctx := context.Background()
	svc, _, _, ops, p, account := installW3OAuthBundle(t)

	consent, err := ops.StartOAuth(ctx, owner, account.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// PKCE: the consent URL carries the S256 challenge.
	if !strings.Contains(consent, "code_challenge=") || !strings.Contains(consent, "code_challenge_method=S256") {
		t.Fatalf("consent url carries no PKCE challenge: %s", consent)
	}
	state := stateFrom(t, consent)
	oc := svc.(oauthCompleter)
	if _, err := oc.CompleteOAuth(ctx, state, "code-123"); err != nil {
		t.Fatalf("first callback: %v", err)
	}
	p.mu.Lock()
	exchanges, verifiers := p.exchanges, append([]string(nil), p.verifiers...)
	p.mu.Unlock()
	if exchanges != 1 || len(verifiers) != 1 || verifiers[0] == "" {
		t.Fatalf("first exchange: %d, verifiers %v — the code must redeem WITH the flow's verifier", exchanges, verifiers)
	}

	// The replay: same signed state, same (or another) valid code.
	if _, err := oc.CompleteOAuth(ctx, state, "code-123"); err == nil {
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
func TestW3OAuthEmptyStateKeyRefused(t *testing.T) {
	dsn := testdb.NewSchema(t)
	_, err := engine.Open(context.Background(), dsn,
		engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		engine.WithOAuth("", "https://substrate.example/callback", nil),
	)
	if err == nil || !strings.Contains(err.Error(), "state key") {
		t.Fatalf("an empty oauth state key must refuse the boot, got: %v", err)
	}
}

// --- #15: the callback never reflects provider detail -------------------------------

func TestW3OAuthExchangeErrorSanitized(t *testing.T) {
	ctx := context.Background()
	svc, _, _, ops, _, account := installW3OAuthBundle(t)

	consent, err := ops.StartOAuth(ctx, owner, account.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	_, err = svc.(oauthCompleter).CompleteOAuth(ctx, stateFrom(t, consent), "wrong-code")
	if err == nil {
		t.Fatal("a bad code completed")
	}
	if strings.Contains(err.Error(), "SECRET-PROVIDER-DETAIL") || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("the exchange error reflects provider detail: %v", err)
	}
}

// --- #5: callback and refresh against teardown -------------------------------------

// A delete that lands while the code exchange is blocked at the provider
// wins: the callback's one transaction re-checks the account live and
// refuses, so no unreferenced credential is ever stored.
func TestW3OAuthCallbackVsDeleteBarrier(t *testing.T) {
	ctx := context.Background()
	svc, ds, db, ops, p, account := installW3OAuthBundle(t)

	consent, err := ops.StartOAuth(ctx, owner, account.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	p.mu.Lock()
	p.holdExchange = make(chan struct{})
	p.arrived = make(chan string, 2)
	p.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		_, err := svc.(oauthCompleter).CompleteOAuth(ctx, stateFrom(t, consent), "code-123")
		done <- err
	}()
	select {
	case <-p.arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the exchange never reached the provider")
	}
	// The exchange is blocked at the provider; the account dies meanwhile.
	if _, err := ds.Delete(ctx, owner, account.Kind, account.ID); err != nil {
		t.Fatalf("delete during exchange: %v", err)
	}
	if _, err := ops.ProcessOAuthFinalizers(ctx); err != nil {
		t.Fatalf("finalizers: %v", err)
	}
	close(p.holdExchange)
	if err := <-done; err == nil {
		t.Fatal("the callback connected a deleted account")
	}
	if n := w3CredentialCount(t, db); n != 0 {
		t.Fatalf("an orphan credential survived the callback-versus-delete race: %d rows", n)
	}
}

// A refresh blocked at the provider while the finalizer tears the account
// down must not recreate the deleted credential: persistence is update-only
// with a live-account compare-and-swap, never an upsert.
func TestW3OAuthRefreshVsFinalizerBarrier(t *testing.T) {
	ctx := context.Background()
	svc, ds, db, ops, p, account := installW3OAuthBundle(t)
	p.mu.Lock()
	p.expiresIn = 120 // inside the 10m refresh window
	p.mu.Unlock()
	w3Connect(t, svc, ops, account.ID)
	if n := w3CredentialCount(t, db); n != 1 {
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
		n, err := ops.RefreshOAuthTokens(ctx)
		done <- res{n, err}
	}()
	select {
	case <-p.arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the refresh never reached the provider")
	}
	// The refresh is blocked at the provider; teardown completes meanwhile.
	if _, err := ds.Delete(ctx, owner, account.Kind, account.ID); err != nil {
		t.Fatalf("delete during refresh: %v", err)
	}
	if released, err := ops.ProcessOAuthFinalizers(ctx); err != nil || released != 1 {
		t.Fatalf("finalizers: %d %v", released, err)
	}
	if n := w3CredentialCount(t, db); n != 0 {
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
	if n := w3CredentialCount(t, db); n != 0 {
		t.Fatalf("the late refresh recreated an orphan credential: %d rows", n)
	}
}

// --- #4: purge revokes -----------------------------------------------------------

// Purging a bundle with connected accounts revokes the provider grants: the
// accounts tombstone FIRST and their finalizers run while the configuration
// record — the revocation endpoint's home — is still live.
func TestW3BundlePurgeRevokesAccounts(t *testing.T) {
	ctx := context.Background()
	svc, _, db, ops, p, account := installW3OAuthBundle(t)
	w3Connect(t, svc, ops, account.ID)

	if err := ops.DisableBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := ops.PurgeBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("purge: %v", err)
	}
	p.mu.Lock()
	revoked := append([]string(nil), p.revoked...)
	p.mu.Unlock()
	if len(revoked) != 1 || revoked[0] != "rt-1" {
		t.Fatalf("purge did not revoke the provider grant: %v", revoked)
	}
	if n := w3CredentialCount(t, db); n != 0 {
		t.Fatalf("purge left credentials: %d", n)
	}
	st, err := ops.BundleStatus(ctx, mbAuthority)
	if err != nil || st.LiveRecords != 0 {
		t.Fatalf("post-purge status: %+v %v", st, err)
	}
}

// --- #16: a failed revoke is observable, and never strands the deletion -------------

func TestW3OAuthRevoke500StillReleases(t *testing.T) {
	ctx := context.Background()
	svc, ds, db, ops, p, account := installW3OAuthBundle(t)
	w3Connect(t, svc, ops, account.ID)
	p.mu.Lock()
	p.revokeStatus = http.StatusInternalServerError
	p.mu.Unlock()

	if _, err := ds.Delete(ctx, owner, account.Kind, account.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	released, err := ops.ProcessOAuthFinalizers(ctx)
	if err != nil || released != 1 {
		t.Fatalf("a provider 500 on revoke must not strand the deletion: %d %v", released, err)
	}
	p.mu.Lock()
	attempts := len(p.revoked)
	p.mu.Unlock()
	if attempts != 1 {
		t.Fatalf("revoke attempts: %d", attempts)
	}
	if n := w3CredentialCount(t, db); n != 0 {
		t.Fatalf("credentials survived the release: %d", n)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if _, err := ds.Get(ctx, account.Kind, account.ID); err == nil {
		t.Fatal("account survived gc")
	}
}

// --- #2: shadow traits cannot counterfeit the host-recognized core ones -------------

// w3TraitDoc renders a trait document declared inside the mail bundle authority.
func w3TraitDoc(name string, props map[string]any) map[string]any {
	data := map[string]any{"authority": mbAuthority, "description": "a bundle-local trait named like a core one"}
	if len(props) > 0 {
		data["properties"] = props
	}
	return map[string]any{
		"kind":     vocabulary.CoreKind(vocabulary.DocTrait),
		"metadata": map[string]any{"id": mbAuthority + "/" + name},
		"data":     data,
	}
}

// A bundle whose configType binds a LOCAL trait named "bundleconfig" is not
// configured at all: the host key is the resolved identity
// core.substrate.reamde.dev/bundleconfig, and a same-named local trait does not count.
func TestW3ShadowBundleConfigRefused(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	docs := mbDocs(nil,
		w3TraitDoc("bundleconfig", nil),
		vocabulary.KindManifest(mbAuthority,
			map[string]any{"singular": "mailconfig", "plural": "mailconfigs"},
			map[string]any{
				// Resolves in-authority FIRST: this binds the local shadow, never core.
				"traits":     []any{"bundleconfig"},
				"properties": map[string]any{"note": map[string]any{"type": "string"}},
			}),
		mbMessageTypeDoc())
	_, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs)
	if err == nil || !strings.Contains(err.Error(), "does not implement the bundleconfig trait") {
		t.Fatalf("a shadow bundleconfig trait satisfied bundle admission: %v", err)
	}
}

// A bundle-local trait named "accountconfig" shadows the core one for the
// authority's own bindings — and the host then treats NONE of its records as
// connected accounts: no OAuth, no status counts, no runner injection, no
// core-trait query hits.
func TestW3ShadowAccountConfigIsNotAnAccount(t *testing.T) {
	ctx := context.Background()
	_, ds, _ := newW3Env(t,
		engine.WithOAuth("w3-state-key", "https://substrate.example/cb", nil))
	docs := mbDocs(nil,
		mbConfigTypeDoc(),
		w3TraitDoc("accountconfig", nil),
		// Binds bare "accountconfig" — in-authority resolution finds the SHADOW.
		mbAccountTypeDoc(),
		mbMessageTypeDoc(),
		mbFnDoc("echo", mbEchoSource))
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("install: %v", err)
	}
	ops := bundler(t, ds)
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	shadow := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "shadow@example.com"},
	})

	// OAuth refuses: the shadow trait is not the core accountconfig, so the
	// id resolves within NO accountconfig implementor type — a not-found,
	// never an account.
	if _, err := ops.StartOAuth(ctx, owner, shadow.ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("StartOAuth on a shadow-trait record: %v", err)
	}
	// Status counts no accounts.
	st, err := ops.BundleStatus(ctx, mbAuthority)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Accounts != 0 {
		t.Fatalf("a shadow-trait record counted as an account: %+v", st)
	}
	// The core trait query does not list the shadow-bound type.
	types, err := ops.TypesImplementing(ctx, "core.substrate.reamde.dev/accountconfig")
	if err != nil {
		t.Fatalf("implementors: %v", err)
	}
	for _, ti := range types {
		if ti.Identity == mbAccountType {
			t.Fatalf("shadow-bound type answers the core trait query: %+v", types)
		}
	}
	// The BARE name is now ambiguous — core and shadow both declare it — and
	// an ambiguous bare filter errors instead of aggregating look-alikes.
	if _, err := ops.TypesImplementing(ctx, "accountconfig"); err == nil ||
		!strings.Contains(err.Error(), "ambiguous trait") {
		t.Fatalf("ambiguous bare trait filter: %v", err)
	}
	// The runner injects no shadow records as accounts.
	out, _, err := ds.(fnOps).CallFunction(ctx, mbEchoFn, map[string]any{})
	if err != nil {
		t.Fatalf("call echo: %v", err)
	}
	cfg, _ := out.(map[string]any)["config"].(map[string]any)
	if cfg == nil {
		t.Fatalf("no config in output: %v", out)
	}
	if accounts, _ := cfg["accounts"].([]any); len(accounts) != 0 {
		t.Fatalf("the runner injected shadow-trait records as accounts: %v", accounts)
	}
}

// --- #8: merge and split pass bundle admission --------------------------------------

func TestW3MergeSplitBundleLifecycleGuards(t *testing.T) {
	ctx := context.Background()
	ds, ops := installMailBundle(t)

	a1 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbAccountType, Properties: map[string]any{"address": "a1@x.co"}})
	a2 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbAccountType, Properties: map[string]any{"address": "a2@x.co"}})

	// Disabled: accounts are frozen — merge refuses like put/patch/delete do.
	if err := ops.DisableBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("disable: %v", err)
	}
	_, err := ds.Merge(ctx, owner, a1.Kind, a1.ID, a2.ID)
	wantErr(t, err, substrate.ErrGuard, "merge of frozen accounts")
	if err := ops.EnableBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("enable: %v", err)
	}
	rec, err := ds.Merge(ctx, owner, a1.Kind, a1.ID, a2.ID)
	if err != nil {
		t.Fatalf("merge while live: %v", err)
	}
	// Disabled again: the split would resurrect a frozen account.
	if err := ops.DisableBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("disable: %v", err)
	}
	_, err = ds.Split(ctx, owner, rec.ID)
	wantErr(t, err, substrate.ErrGuard, "split resurrecting a frozen account")
	if err := ops.EnableBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split while live: %v", err)
	}

	// Uninstall refuses while the merged account still lives — a guard with the
	// count — so a merge/split cannot strand data behind a torn-down type. It
	// tears the authority down only once purge has cleared the data.
	i1 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "i1"}})
	_ = mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "i2"}})
	err = ops.UninstallBundle(ctx, mbAuthority)
	wantErr(t, err, substrate.ErrGuard, "uninstall with live data")
	if !strings.Contains(err.Error(), "live records") {
		t.Fatalf("uninstall refusal must carry the count: %v", err)
	}
	if err := ops.DisableBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := ops.PurgeBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if err := ops.UninstallBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("uninstall after purge: %v", err)
	}
	// The type is gone: a merge no longer resolves it.
	if _, err := ds.Merge(ctx, owner, i1.Kind, i1.ID, "whatever"); err == nil {
		t.Fatal("merge resolved a torn-down type")
	}
}

// Merge EFFECTS pass the same admission: a function outside the bundle, with
// the merge grant and emit on the account type, cannot merge a disabled
// bundle's frozen accounts either — the effect path and the direct verb are
// one code path, and both refuse.
func TestW3MergeEffectBundleGuard(t *testing.T) {
	ctx := context.Background()
	ds, ops := installMailBundle(t)
	const toolAuthority = "wtool.test.dev"
	mergerDocs := []map[string]any{
		vocabulary.AuthorityManifest(toolAuthority, ""),
		vocabulary.ActorManifest(toolAuthority, vocabulary.AuthorityActor(toolAuthority)),
		vocabulary.FunctionManifest(toolAuthority, "merger", map[string]any{
			"description": "merges two mail accounts",
			"runtime":     vocabulary.RuntimePython,
			"source": `
def main(input, host):
    a = input["args"]
    return {"effects": [{"action": "merge", "kind": "` + mbAccountType + `",
                         "id": a["winner"], "loser": a["loser"]}]}
`,
			"capabilities": map[string]any{
				"emit":      []any{mbAccountType},
				"mutations": []any{"merge"},
			},
		}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, mergerDocs); err != nil {
		t.Fatalf("install merger: %v", err)
	}
	a1 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbAccountType, Properties: map[string]any{"address": "e1@x.co"}})
	a2 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbAccountType, Properties: map[string]any{"address": "e2@x.co"}})
	if err := ops.DisableBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("disable: %v", err)
	}
	args := map[string]any{"winner": a1.ID, "loser": a2.ID}
	_, _, err := ds.(fnOps).CallFunction(ctx, toolAuthority+"/merger", args)
	if err == nil || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("a merge effect bypassed the bundle freeze: %v", err)
	}
	// Both accounts still live and unmerged.
	if got := mustGet(t, ds, a2.Kind, a2.ID); got.DeletedAt != nil {
		t.Fatalf("frozen loser was merged away: %+v", got)
	}
	// Enabled again, the same effect lands.
	if err := ops.EnableBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, n, err := ds.(fnOps).CallFunction(ctx, toolAuthority+"/merger", args); err != nil || n != 1 {
		t.Fatalf("merge effect while live: %d %v", n, err)
	}
}

// A split that would resurrect a bundle's configuration record takes the
// same singleton guard a create does: while another config lives, the
// resurrection refuses. The two-live-config precondition cannot be built
// through the write surface (that is the point), so the merge record is
// fabricated the way legacy/imported data would carry it.
func TestW3SplitConfigSingletonGuard(t *testing.T) {
	ctx := context.Background()
	_, ds, db := newW3Env(t)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, mbStandardDocs()); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	c1 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	if _, err := ds.Delete(ctx, owner, c1.Kind, c1.ID); err != nil {
		t.Fatalf("delete c1: %v", err)
	}
	c2 := mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})

	// The fabricated merge record: c1 was "merged into" c2 before this data
	// arrived here.
	const rec = "w3fabmerge00"
	if _, err := db.Exec(`
		INSERT INTO records (id, kind, props) VALUES ($1, 'core.substrate.reamde.dev/recordmerge', '{"moved": {}}'::jsonb)`,
		rec); err != nil {
		t.Fatalf("insert merge record: %v", err)
	}
	for rel, dst := range map[string]string{"winner": c2.ID, "loser": c1.ID} {
		if _, err := db.Exec(`INSERT INTO edges (rel, src_kind, src, dst_kind, dst) VALUES ($1, 'core.substrate.reamde.dev/recordmerge', $2, $3, $4)`,
			rel, rec, mbConfigType, dst); err != nil {
			t.Fatalf("insert %s edge: %v", rel, err)
		}
	}

	_, err := ds.Split(ctx, owner, rec)
	wantErr(t, err, substrate.ErrGuard, "split resurrecting a second live config")
	if !strings.Contains(err.Error(), "one live") {
		t.Fatalf("singleton refusal message: %v", err)
	}
	// Free the slot: the same split then resurrects cleanly.
	if _, err := ds.Delete(ctx, owner, c2.Kind, c2.ID); err != nil {
		t.Fatalf("delete c2: %v", err)
	}
	if _, err := ds.Split(ctx, owner, rec); err != nil {
		t.Fatalf("split with a free slot: %v", err)
	}
	got := mustGet(t, ds, c1.Kind, c1.ID)
	if got.DeletedAt != nil {
		t.Fatalf("c1 not resurrected: %+v", got)
	}
}

// --- #7: the lifecycle fence and the purging transition -----------------------------

// w3WaiterSource polls for a flag record, then emits one message: the barrier
// that keeps an invocation in flight while a lifecycle verb races it.
const w3WaiterSource = `
import time
def main(input, host):
    for _ in range(150):
        got = host.get("mail.bundles.substrate.reamde.dev/mailitem", "w3-fence-flag")
        if got:
            return {"effects": [{"action": "put", "kind": "mail.bundles.substrate.reamde.dev/mailmessage",
                                 "id": "w3-fence-done", "properties": {"subject": "done"}}]}
        time.sleep(0.05)
    return {"effects": []}
`

func w3WaiterDoc() map[string]any {
	return vocabulary.FunctionManifest(mbAuthority, "waiter", map[string]any{
		"description": "waits for the fence flag",
		"runtime":     vocabulary.RuntimePython,
		"source":      w3WaiterSource,
		"timeoutMs":   20000,
		"capabilities": map[string]any{
			"emit":  []any{mbMessageType},
			"reads": map[string]any{"kinds": []any{mbItemType}, "budgets": map[string]any{"calls": 500}},
		},
	})
}

// Disable takes the exclusive side of the per-bundle fence: an invocation
// already past admission commits its effects BEFORE the disable returns, and
// the next invocation refuses.
func TestW3LifecycleFenceDrainsInvocation(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	docs := mbDocs(nil,
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		w3WaiterDoc())
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("install: %v", err)
	}
	ops := bundler(t, ds)
	fops := ds.(fnOps)
	const waiterFn = mbAuthority + "/waiter"

	type callRes struct {
		effects int
		err     error
	}
	callDone := make(chan callRes, 1)
	go func() {
		_, n, err := fops.CallFunction(ctx, waiterFn, map[string]any{})
		callDone <- callRes{n, err}
	}()
	time.Sleep(1 * time.Second) // the invocation is admitted and polling

	disableDone := make(chan error, 1)
	go func() { disableDone <- ops.DisableBundle(ctx, mbAuthority) }()
	select {
	case err := <-disableDone:
		select {
		case r := <-callDone:
			t.Fatalf("disable returned while an admitted invocation was in flight: %v (call settled early: %d %v)", err, r.effects, r.err)
		default:
			t.Fatalf("disable returned while an admitted invocation was in flight: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		// Blocked at the fence, as it must be.
	}

	// Release the barrier: the invocation finishes and commits, THEN the
	// disable lands.
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, ID: "w3-fence-flag", Properties: map[string]any{"name": "go"}})
	select {
	case err := <-disableDone:
		if err != nil {
			t.Fatalf("disable: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("disable never returned")
	}
	// The invocation's effect committed before the disable returned.
	if got := mustGet(t, ds, mbMessageType, "w3-fence-done"); got.Properties["subject"] != "done" {
		t.Fatalf("drained invocation's effect: %+v", got.Properties)
	}
	r := <-callDone
	if r.err != nil || r.effects != 1 {
		t.Fatalf("drained invocation: %d %v", r.effects, r.err)
	}
	// And the next admission refuses.
	if _, _, err := fops.CallFunction(ctx, waiterFn, map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), "disabled") {
		t.Fatalf("post-disable invocation: %v", err)
	}
}

// Enable refuses the purging transition: an interrupted purge leaves the
// marker standing, and only a purge run to completion clears it.
func TestW3EnableRefusesInterruptedPurge(t *testing.T) {
	ctx := context.Background()
	_, ds, db := newW3Env(t)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, mbStandardDocs()); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	ops := bundler(t, ds)
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbItemType, Properties: map[string]any{"name": "x"}})
	if err := ops.DisableBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("disable: %v", err)
	}
	// Simulate a purge that died mid-run: the marker is set, the data is
	// half-gone or whole — either way the bundle must not come live.
	if _, err := db.Exec(`
		UPDATE records SET props = jsonb_set(props, '{purging}', 'true') WHERE id = $1`, mbBundleRow); err != nil {
		t.Fatalf("fabricate interrupted purge: %v", err)
	}
	if err := ops.EnableBundle(ctx, mbAuthority); err == nil || !errors.Is(err, substrate.ErrGuard) ||
		!strings.Contains(err.Error(), "purging") {
		t.Fatalf("enable during purge: %v", err)
	}
	// A purge run to completion clears the marker; enable then works.
	if _, err := ops.PurgeBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("re-purge: %v", err)
	}
	if err := ops.EnableBundle(ctx, mbAuthority); err != nil {
		t.Fatalf("enable after a completed purge: %v", err)
	}
}

// --- #10: upgrades guard bundled agents too ------------------------------------------

func TestW3BundledAgentUpgradeGuard(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	const wagAuthority = "wagent.bundles.substrate.reamde.dev"
	configDoc := vocabulary.KindManifest(wagAuthority,
		map[string]any{"singular": "wagconfig", "plural": "wagconfigs"},
		map[string]any{"traits": []any{"bundleconfig"}, "properties": map[string]any{
			"note": map[string]any{"type": "string"},
		}})
	agentDoc := vocabulary.AgentManifest(wagAuthority, "helper", map[string]any{
		"description": "a bundled agent", "prompt": "You help.",
		"provider": "default", "model": "claude-opus-5",
	})
	withAgent := []map[string]any{
		vocabulary.AuthorityManifest(wagAuthority, ""),
		vocabulary.BundleManifest(wagAuthority, map[string]any{
			"description": "the agent bundle",
			"configType":  wagAuthority + "/wagconfig",
			"installs":    []any{wagAuthority + "/wagconfig", wagAuthority + "/helper"},
		}),
		configDoc, agentDoc,
	}
	sa := applier(t, ds)
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, withAgent); err != nil {
		t.Fatalf("install: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/trigger", ID: "on-wag-helper",
		Properties: map[string]any{
			"enabled":  true,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{wagAuthority + "/wagconfig"}, "ops": []any{"create"}}},
			"callable": map[string]any{"kind": "core.substrate.reamde.dev/agent", "id": wagAuthority + "/helper"},
		},
	})

	// The upgrade drops the agent while the trigger references it: refused.
	withoutAgent := []map[string]any{
		vocabulary.AuthorityManifest(wagAuthority, ""),
		vocabulary.BundleManifest(wagAuthority, map[string]any{
			"description": "the agent bundle",
			"configType":  wagAuthority + "/wagconfig",
			"installs":    []any{wagAuthority + "/wagconfig"},
		}),
		configDoc,
	}
	_, err := sa.ApplyVocabularyDocuments(ctx, owner, withoutAgent)
	wantErr(t, err, substrate.ErrGuard, "dropping a trigger-referenced agent")
	if !strings.Contains(err.Error(), "referenced by live trigger") || !strings.Contains(err.Error(), wagAuthority+"/helper") {
		t.Fatalf("agent upgrade refusal: %v", err)
	}
	// Rewire the trigger away; the same upgrade then lands.
	if _, err := ds.Delete(ctx, owner, "core.substrate.reamde.dev/trigger", "on-wag-helper"); err != nil {
		t.Fatalf("delete trigger: %v", err)
	}
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, withoutAgent); err != nil {
		t.Fatalf("upgrade after rewire: %v", err)
	}
}

// --- #11: a trigger cannot be created across the upgrade check ----------------------

// The registry-dependency barrier: with an in-flight trigger admission
// holding the shared side (simulated on a raw connection), the upgrade's
// dropped-reference check blocks; whatever the interleaving, an upgrade that
// drops a callable and a trigger create referencing that callable can never
// BOTH succeed.
func TestW3TriggerVsUpgradeBarrier(t *testing.T) {
	ctx := context.Background()
	_, ds, db := newW3Env(t)
	sa := applier(t, ds)
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, mbStandardDocs()); err != nil {
		t.Fatalf("install bundle: %v", err)
	}

	// The raw shared holder: an admission that validated and has not
	// committed yet.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtext($1)::bigint)`, ds.Repository().ID+"|registrydep"); err != nil {
		t.Fatalf("shared lock: %v", err)
	}

	// The upgrade that drops mark; it must block at the exclusive side.
	noMark := mbDocs(nil,
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("echo", mbEchoSource))
	upgradeDone := make(chan error, 1)
	go func() {
		_, err := sa.ApplyVocabularyDocuments(ctx, owner, noMark)
		upgradeDone <- err
	}()
	select {
	case err := <-upgradeDone:
		t.Fatalf("the upgrade committed across a held shared registry lock: %v", err)
	case <-time.After(500 * time.Millisecond):
		// Blocked, as it must be.
	}

	// A trigger create referencing mark races the pending upgrade.
	triggerDone := make(chan error, 1)
	go func() {
		_, err := ds.Put(ctx, owner, substrate.PutInput{
			Kind: "core.substrate.reamde.dev/trigger", ID: "on-w3-barrier",
			Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{mbItemType}, "ops": []any{"create"}}},
				"callable": map[string]any{"kind": "core.substrate.reamde.dev/function", "id": mbMarkFn},
			},
		})
		triggerDone <- err
	}()

	// Release the in-flight admission; the queued exclusive and shared
	// holders settle in whatever order Postgres grants them.
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit raw tx: %v", err)
	}
	var upgradeErr, triggerErr error
	select {
	case upgradeErr = <-upgradeDone:
	case <-time.After(30 * time.Second):
		t.Fatal("upgrade never settled")
	}
	select {
	case triggerErr = <-triggerDone:
	case <-time.After(30 * time.Second):
		t.Fatal("trigger create never settled")
	}
	if upgradeErr == nil && triggerErr == nil {
		t.Fatal("the upgrade dropped mark AND a trigger referencing mark landed — the barrier failed")
	}
	if upgradeErr != nil && triggerErr != nil {
		t.Fatalf("both sides failed: upgrade %v; trigger %v", upgradeErr, triggerErr)
	}
	if upgradeErr != nil && !errors.Is(upgradeErr, substrate.ErrGuard) {
		t.Fatalf("upgrade refusal: %v", upgradeErr)
	}
	if triggerErr != nil && !errors.Is(triggerErr, substrate.ErrValidation) {
		t.Fatalf("trigger refusal: %v", triggerErr)
	}
}
