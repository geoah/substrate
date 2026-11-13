package engine

// The operator's OAuth clients (docs/plans/host-defaults.md): a published
// provider the defaults file lists signs in through the operator's client
// where its client input resolves to no record at all, each account records
// the client its grant came from, and a grant from another client than the
// one resolving now is not refreshed. The provider is a loopback fake, and
// the closure is a small published one built here, so no test reaches a real
// vendor.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	opMailAuthority = "opclient.test.dev"
	opMailPackage   = opMailAuthority + "/mail"
	opMailConfig    = opMailPackage + "/config"
	opMailAccount   = opMailPackage + "/account"

	// The operator's client in these fixtures; neither is a real client.
	opClientID     = "operator-client"
	opClientSecret = "operator-secret-fixture"
	// The person's own client.
	ownClientID     = "own-client"
	ownClientSecret = "own-secret-fixture"
)

// opTokenFake is a provider's token endpoint: every exchange and refresh is
// recorded with the client id and secret it carried.
type opTokenFake struct {
	srv *httptest.Server

	mu        sync.Mutex
	expiresIn int
	exchanges []opClientPair
	refreshes []opClientPair
	// onRefresh runs once, outside mu, before the next refresh is answered,
	// and refuseRefresh answers every refresh 400 `invalid_grant`.
	onRefresh     func()
	refuseRefresh bool
}

type opClientPair struct{ id, secret string }

func newOpTokenFake(t *testing.T) *opTokenFake {
	t.Helper()
	f := &opTokenFake{expiresIn: 3600}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		pair := opClientPair{r.Form.Get("client_id"), r.Form.Get("client_secret")}
		f.mu.Lock()
		hook := f.onRefresh
		if r.Form.Get("grant_type") == "refresh_token" {
			f.onRefresh = nil
		} else {
			hook = nil
		}
		f.mu.Unlock()
		if hook != nil {
			hook()
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Form.Get("grant_type") == "refresh_token" && f.refuseRefresh {
			f.refreshes = append(f.refreshes, pair)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
			return
		}
		out := map[string]any{"token_type": "Bearer", "expires_in": f.expiresIn}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			f.exchanges = append(f.exchanges, pair)
			out["access_token"], out["refresh_token"] = "at-"+pair.id, "rt-"+pair.id
		case "refresh_token":
			f.refreshes = append(f.refreshes, pair)
			out["access_token"] = "at2-" + pair.id
		default:
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// set changes the fake under its lock.
func (f *opTokenFake) set(change func(f *opTokenFake)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

func (f *opTokenFake) seen() (exchanges, refreshes []opClientPair) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]opClientPair(nil), f.exchanges...), append([]opClientPair(nil), f.refreshes...)
}

// opMailDocs is a provider closure with one OAuth client input and one
// account kind; withProvenance declares the account's `oauthClient`.
func opMailDocs(tokenURL string, withProvenance bool) []map[string]any {
	accountProps := map[string]any{
		"tokenRef":      map[string]any{"type": "secret", "writer": "oauth"},
		"tokenStatus":   map[string]any{"type": "string", "writer": "oauth"},
		"grantedScopes": map[string]any{"type": "string", "repeated": true, "writer": "oauth"},
		"tokenError":    map[string]any{"type": "string", "writer": "oauth"},
		"tokenErrorAt":  map[string]any{"type": "datetime", "writer": "oauth"},
		"address":       map[string]any{"type": "email", "writer": "owner"},
		"enabledMail":   map[string]any{"type": "bool", "writer": "owner"},
	}
	if withProvenance {
		accountProps[propOAuthClient] = map[string]any{
			"type": "object", "writer": "oauth",
			"fields": map[string]any{
				"clientId": map[string]any{"type": "string"},
				"source": map[string]any{"type": "enum", "values": []any{
					map[string]any{"value": "operator"}, map[string]any{"value": "own"},
				}},
			},
		}
	}
	return []map[string]any{
		vocabulary.PackageManifest(opMailPackage, 0),
		vocabulary.BundleManifest(opMailPackage, map[string]any{
			"description": "a mail provider",
			"inputs": map[string]any{
				"client": map[string]any{"kind": opMailConfig, "inject": "functions"},
			},
			"installs": []any{opMailConfig, opMailAccount},
			"oauth2": map[string]any{
				"clientInput":           "client",
				"authorizationEndpoint": "https://provider.example/authorize",
				"tokenEndpoint":         tokenURL,
				"revocationEndpoint":    strings.TrimSuffix(tokenURL, "/token") + "/revoke",
				"featureScopes": map[string]any{
					"enabledMail": map[string]any{"scopes": []any{"mail.read"}},
				},
			},
		}),
		vocabulary.KindManifest(opMailPackage, map[string]any{"singular": "config"}, map[string]any{
			"traits": []any{"substrate.reamde.dev/core/oauth2"},
			"properties": map[string]any{
				"clientId":     map[string]any{"type": "string"},
				"clientSecret": map[string]any{"type": "secret"},
				"apiBase":      map[string]any{"type": "url"},
			},
		}),
		vocabulary.KindManifest(opMailPackage, map[string]any{"singular": "account"}, map[string]any{
			"traits":     []any{"substrate.reamde.dev/core/accountconfig"},
			"properties": accountProps,
		}),
	}
}

// installOpMail installs the closure as the catalog installs a provider:
// published, under the bundle's own actor. published false is a hand apply,
// which the repository owns.
func installOpMail(t *testing.T, ds *dataset, tokenURL string, published, withProvenance bool) {
	t.Helper()
	docs := opMailDocs(tokenURL, withProvenance)
	actor := substrate.BundleActor(opMailAuthority, "mail")
	if _, err := ds.InstallBundleClosure(context.Background(), actor, docs, nil, substrate.BundleInstall{Published: published}); err != nil {
		t.Fatalf("install the mail closure: %v", err)
	}
}

func opClientsConfig() *operatordefaults.Config {
	return &operatordefaults.Config{
		OAuthClients: map[string]*operatordefaults.OAuthClient{
			opMailPackage: {
				ClientID: opClientID, ClientSecret: operatordefaults.NewSecret(opClientSecret),
				Notice: "Example's sign-in is in testing.",
			},
		},
	}
}

// openOpMail opens a core repository with the OAuth facility on against the
// fake, the operator's file listing the mail provider, and the provider
// installed published, with one account.
func openOpMail(t *testing.T) (*dataset, *opTokenFake, eref) {
	t.Helper()
	f := newOpTokenFake(t)
	ds := openCoreDataset(t,
		WithOAuth("test-state-key", "https://substrate.example/api/v1/substrate.reamde.dev/core/oauth/callback", f.srv.Client()),
		WithOperatorDefaults(opClientsConfig(), "Example"))
	installOpMail(t, ds, f.srv.URL+"/token", true, true)
	return ds, f, opPutAccount(t, ds, "a1")
}

// opPutAccount writes an account under fid(name): the fixture's account kind
// is minted, so the owner may not name it (decision 0228).
func opPutAccount(t *testing.T, ds *dataset, name string) eref {
	t.Helper()
	if _, err := ds.Put(context.Background(), substrate.ActorPerson, substrate.PutInput{
		Kind: opMailAccount, ID: fid(name), Properties: map[string]any{"address": name + "@example.com", "enabledMail": true},
	}); err != nil {
		t.Fatalf("put account: %v", err)
	}
	return eref{Kind: opMailAccount, ID: fid(name)}
}

// opPutOwnClient writes the person's own client record under fid(name). The
// fixture's config kind is minted, so the owner may not write the id
// `default`, and a lone record resolves as the sole one.
func opPutOwnClient(t *testing.T, ds *dataset, name string, props map[string]any) {
	t.Helper()
	if _, err := ds.Put(context.Background(), substrate.ActorPerson, substrate.PutInput{
		Kind: opMailConfig, ID: fid(name), Properties: props,
	}); err != nil {
		t.Fatalf("put client record %s: %v", name, err)
	}
}

// opConnect runs the consent and the callback for one account and returns
// the consent URL's client id.
func opConnect(t *testing.T, ds *dataset, account eref) string {
	t.Helper()
	got, err := opConnectErr(ds, account)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// opConnectErr is opConnect for a hook off the test's goroutine.
func opConnectErr(ds *dataset, account eref) (string, error) {
	ctx := context.Background()
	consent, err := ds.StartOAuth(ctx, substrate.ActorPerson, vocabulary.RecordPath(account.Kind, account.ID))
	if err != nil {
		return "", fmt.Errorf("start: %w", err)
	}
	u, err := url.Parse(consent)
	if err != nil {
		return "", err
	}
	if _, err := ds.svc.CompleteOAuth(ctx, u.Query().Get("state"), "code-1"); err != nil {
		return "", fmt.Errorf("callback: %w", err)
	}
	return u.Query().Get("client_id"), nil
}

// opReconnectThroughOperator deletes the person's own client record
// (opPutOwnClient's "default") and reconnects the account, through the
// operator's client now.
func opReconnectThroughOperator(ds *dataset, account eref) error {
	if _, err := ds.Delete(context.Background(), substrate.ActorPerson, opMailConfig, fid("default"), substrate.DeleteInput{}); err != nil {
		return fmt.Errorf("delete the own client: %w", err)
	}
	got, err := opConnectErr(ds, account)
	if err == nil && got != opClientID {
		err = fmt.Errorf("the reconnect named client %q, want the operator's", got)
	}
	return err
}

// opOwnClientWithBase is the person's own client record with an apiBase,
// the field a body would call instead of the provider's API.
var opOwnClientWithBase = map[string]any{
	"clientId": ownClientID, "clientSecret": ownClientSecret, "apiBase": "http://127.0.0.1:9/",
}

// opConfigEntry builds an invocation's config for the mail package and
// returns it with the first account's entry.
func opConfigEntry(t *testing.T, ds *dataset) (map[string]any, map[string]any) {
	t.Helper()
	cfg, _, _, err := ds.resolveFunctionConfig(context.Background(), &vocabulary.Function{Package: opMailPackage})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	accounts, _ := cfg["accounts"].([]any)
	if len(accounts) != 1 {
		t.Fatalf("config accounts = %v", cfg["accounts"])
	}
	return cfg, accounts[0].(map[string]any)
}

// opInjectedBase is the apiBase the config's client input carries, empty
// for none.
func opInjectedBase(cfg map[string]any) string {
	inputs, _ := cfg["inputs"].(map[string]any)
	client, _ := inputs["client"].(map[string]any)
	props, _ := client["properties"].(map[string]any)
	return propText(props["apiBase"])
}

// opAccountConnectedThroughOperator fails unless the account says connected,
// carries no tokenError and records the operator's client.
func opAccountConnectedThroughOperator(t *testing.T, ds *dataset, account eref) {
	t.Helper()
	row := opAccountRow(t, ds, account)
	recorded, _ := row.Props[propOAuthClient].(map[string]any)
	if propString(row, propTokenStatus) != tokenStatusConnected || row.Props[propTokenError] != nil ||
		recorded["source"] != oauthClientOperator {
		t.Fatalf("account = %v / %v / %v, want connected through the operator's client with no error",
			row.Props[propTokenStatus], row.Props[propTokenError], recorded)
	}
}

// propText is a property value as text, empty for none.
func propText(v any) string {
	s, _ := v.(string)
	return s
}

func opMailItem(t *testing.T, ds *dataset) substrate.OperatorDefault {
	t.Helper()
	read, err := ds.OperatorDefaults(context.Background())
	if err != nil {
		t.Fatalf("operator defaults: %v", err)
	}
	for _, it := range read.Items {
		if it.ID == "oauth/"+opMailPackage {
			return it
		}
	}
	t.Fatalf("no oauth item for %s in %+v", opMailPackage, read.Items)
	return substrate.OperatorDefault{}
}

func opClientSetupItems(t *testing.T, ds *dataset) []substrate.SetupItem {
	t.Helper()
	st, err := ds.BundleStatus(context.Background(), opMailPackage)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	var out []substrate.SetupItem
	for _, it := range st.Setup {
		if it.Input == "client" {
			out = append(out, it)
		}
	}
	return out
}

func opAccountRow(t *testing.T, ds *dataset, account eref) *erow {
	t.Helper()
	row, err := ds.loadRowDB(context.Background(), account)
	if err != nil || row == nil {
		t.Fatalf("load %s: %v", account.ID, err)
	}
	return row
}

// With no client record of the person's, the published provider the file
// lists signs in through the operator's client: the consent names its id,
// the exchange carries its id and secret together, the account records it,
// and neither the bundle status nor the operator defaults read asks for a
// client. The item reads needs-connect before the connect and ready after.
func TestOperatorOAuthClientServesWhereNoClientRecordExists(t *testing.T) {
	t.Parallel()
	ds, f, account := openOpMail(t)

	if items := opClientSetupItems(t, ds); len(items) != 0 {
		t.Fatalf("the bundle status asks for a client the operator covers: %+v", items)
	}
	item := opMailItem(t, ds)
	if item.Slot != substrate.SlotOAuth || item.Target != opMailPackage || item.Label != "mail" ||
		item.Source != substrate.SourceOperator || !item.OperatorOffers ||
		item.Readiness != substrate.ReadinessNeedsConnect || item.Notice != "Example's sign-in is in testing." {
		t.Fatalf("item before the connect = %+v", item)
	}

	if got := opConnect(t, ds, account); got != opClientID {
		t.Fatalf("the consent named client %q, want the operator's", got)
	}
	exchanges, _ := f.seen()
	if len(exchanges) != 1 || exchanges[0] != (opClientPair{opClientID, opClientSecret}) {
		t.Fatalf("the exchange carried %+v, want the operator's id and secret together", exchanges)
	}
	recorded, _ := opAccountRow(t, ds, account).Props[propOAuthClient].(map[string]any)
	if recorded["clientId"] != opClientID || recorded["source"] != oauthClientOperator {
		t.Fatalf("the account recorded %v, want the operator's client", recorded)
	}
	if item := opMailItem(t, ds); item.Readiness != substrate.ReadinessReady || item.Source != substrate.SourceOperator {
		t.Fatalf("item after the connect = %+v", item)
	}
}

// The person's own complete client record wins over the operator's, and the
// account records it as theirs.
func TestOperatorOAuthClientYieldsToAnOwnRecord(t *testing.T) {
	t.Parallel()
	ds, f, account := openOpMail(t)
	opPutOwnClient(t, ds, "default", map[string]any{"clientId": ownClientID, "clientSecret": ownClientSecret})

	if got := opConnect(t, ds, account); got != ownClientID {
		t.Fatalf("the consent named client %q, want the person's own", got)
	}
	exchanges, _ := f.seen()
	if len(exchanges) != 1 || exchanges[0] != (opClientPair{ownClientID, ownClientSecret}) {
		t.Fatalf("the exchange carried %+v, want the person's own pair", exchanges)
	}
	recorded, _ := opAccountRow(t, ds, account).Props[propOAuthClient].(map[string]any)
	if recorded["clientId"] != ownClientID || recorded["source"] != oauthClientOwn {
		t.Fatalf("the account recorded %v, want the person's own client", recorded)
	}
	if item := opMailItem(t, ds); item.Source != substrate.SourceOwn || !item.OperatorOffers || item.Readiness != substrate.ReadinessReady {
		t.Fatalf("item = %+v, want own, offered, ready", item)
	}
}

// The operator's client stands in only for NO record. Records that resolve
// wrong (two with none chosen, a binding to a deleted one) or an incomplete
// one keep their setup item and refuse the flow, and the operator's secret is
// never paired with the person's client id. A copy the repository owns, and a
// published install that cannot record the client, are not covered either.
func TestOperatorOAuthClientNeverStandsInForARecordOrMixesThePair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	refuses := func(t *testing.T, ds *dataset, account eref, wantCode string) {
		t.Helper()
		_, err := ds.StartOAuth(ctx, substrate.ActorPerson, vocabulary.RecordPath(account.Kind, account.ID))
		if !errors.Is(err, substrate.ErrGuard) {
			t.Fatalf("start = %v, want a refusal", err)
		}
		items := opClientSetupItems(t, ds)
		if len(items) != 1 || items[0].Code != wantCode {
			t.Fatalf("setup items = %+v, want one %s", items, wantCode)
		}
		item := opMailItem(t, ds)
		if item.Source == substrate.SourceOperator || item.Readiness != substrate.ReadinessNotSetUp {
			t.Fatalf("item = %+v, want not set up and not the operator's", item)
		}
	}

	t.Run("incomplete", func(t *testing.T) {
		t.Parallel()
		ds, f, account := openOpMail(t)
		opPutOwnClient(t, ds, "default", map[string]any{"clientId": ownClientID})
		refuses(t, ds, account, substrate.SetupOAuthClient)
		if exchanges, refreshes := f.seen(); len(exchanges)+len(refreshes) != 0 {
			t.Fatalf("the provider saw %v %v", exchanges, refreshes)
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		t.Parallel()
		ds, _, account := openOpMail(t)
		opPutOwnClient(t, ds, "c1", map[string]any{"clientId": ownClientID, "clientSecret": ownClientSecret})
		opPutOwnClient(t, ds, "c2", map[string]any{"clientId": ownClientID, "clientSecret": ownClientSecret})
		refuses(t, ds, account, substrate.SetupAmbiguous)
	})
	t.Run("dangling", func(t *testing.T) {
		t.Parallel()
		ds, _, account := openOpMail(t)
		opPutOwnClient(t, ds, "c1", map[string]any{"clientId": ownClientID, "clientSecret": ownClientSecret})
		if err := ds.BindBundleInput(ctx, opMailPackage, "client", fid("c1")); err != nil {
			t.Fatalf("bind: %v", err)
		}
		if _, err := ds.Delete(ctx, substrate.ActorPerson, opMailConfig, fid("c1"), substrate.DeleteInput{}); err != nil {
			t.Fatalf("delete: %v", err)
		}
		refuses(t, ds, account, substrate.SetupDangling)
	})
	t.Run("the repository's own copy", func(t *testing.T) {
		t.Parallel()
		f := newOpTokenFake(t)
		ds := openCoreDataset(t,
			WithOAuth("test-state-key", "https://substrate.example/cb", f.srv.Client()),
			WithOperatorDefaults(opClientsConfig(), "Example"))
		installOpMail(t, ds, f.srv.URL+"/token", false, true)
		refuses(t, ds, opPutAccount(t, ds, "a1"), substrate.SetupMissing)
	})
	t.Run("an install that cannot record the client", func(t *testing.T) {
		t.Parallel()
		f := newOpTokenFake(t)
		ds := openCoreDataset(t,
			WithOAuth("test-state-key", "https://substrate.example/cb", f.srv.Client()),
			WithOperatorDefaults(opClientsConfig(), "Example"))
		installOpMail(t, ds, f.srv.URL+"/token", true, false)
		refuses(t, ds, opPutAccount(t, ds, "a1"), substrate.SetupMissing)
		if msg := opMailItem(t, ds).Message; !strings.Contains(msg, "upgrade it") {
			t.Fatalf("item message = %q, want the upgrade ask", msg)
		}
	})
}

// An account connected through the operator's client is not refreshed once
// the person's own client resolves instead: the provider sees no refresh, the
// account says why in tokenError, the item reads reconnect-needed, and an
// invocation hands the body no token beside the person's apiBase. The
// person's reconnect records their client and refreshes again.
func TestOperatorOAuthClientChangeNeedsAReconnect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, f, account := openOpMail(t)
	f.expiresIn = 120 // inside the refresh window
	opConnect(t, ds, account)

	opPutOwnClient(t, ds, "default", map[string]any{
		"clientId": ownClientID, "clientSecret": ownClientSecret, "apiBase": "http://127.0.0.1:9/",
	})
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
		t.Fatalf("refresh pass = %d, %v; want nothing refreshed", n, err)
	}
	if _, refreshes := f.seen(); len(refreshes) != 0 {
		t.Fatalf("the provider saw refreshes %+v through a client the grant was not issued to", refreshes)
	}
	row := opAccountRow(t, ds, account)
	if propString(row, propTokenStatus) != tokenStatusErroring || propString(row, propTokenError) != reasonOAuthClientChanged {
		t.Fatalf("account = %v / %q, want erroring with the client-change reason", row.Props[propTokenStatus], row.Props[propTokenError])
	}
	if item := opMailItem(t, ds); item.Readiness != substrate.ReadinessReconnectNeeded || item.Message != reasonOAuthClientChanged || item.Source != substrate.SourceOwn {
		t.Fatalf("item = %+v, want reconnect-needed from the person's own client", item)
	}

	cfg, _, _, err := ds.resolveFunctionConfig(ctx, &vocabulary.Function{Package: opMailPackage})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	client, _ := cfg["inputs"].(map[string]any)["client"].(map[string]any)
	if props, _ := client["properties"].(map[string]any); !strings.Contains(propText(props["apiBase"]), "127.0.0.1:9") {
		t.Fatalf("the person's record is not injected: %v", client)
	}
	entry := cfg["accounts"].([]any)[0].(map[string]any)
	if _, has := entry["token"]; has || entry["tokenError"] != reasonOAuthClientChanged {
		t.Fatalf("an operator-issued grant reached a body beside the person's apiBase: %v", entry)
	}

	if got := opConnect(t, ds, account); got != ownClientID {
		t.Fatalf("the reconnect named %q", got)
	}
	row = opAccountRow(t, ds, account)
	if propString(row, propTokenStatus) != tokenStatusConnected || row.Props[propTokenError] != nil {
		t.Fatalf("after the reconnect: %v / %v", row.Props[propTokenStatus], row.Props[propTokenError])
	}
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("refresh after the reconnect = %d, %v", n, err)
	}
	if _, refreshes := f.seen(); len(refreshes) != 1 || refreshes[0] != (opClientPair{ownClientID, ownClientSecret}) {
		t.Fatalf("refreshes = %+v, want one through the person's own pair", refreshes)
	}
}

// An account connected before the client was recorded counts as connected
// through the person's own client: it refreshes while that client resolves,
// and reads as needing a reconnect once only the operator's does.
func TestOperatorOAuthLegacyAccountCountsAsOwn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, f, account := openOpMail(t)
	f.expiresIn = 120
	opPutOwnClient(t, ds, "default", map[string]any{"clientId": ownClientID, "clientSecret": ownClientSecret})
	opConnect(t, ds, account)
	if err := ds.inTx(ctx, actorOAuth, true, func(t *txn) error {
		_, err := t.patch(account, substrate.PatchInput{Properties: map[string]any{propOAuthClient: nil}})
		return err
	}); err != nil {
		t.Fatalf("clear the record: %v", err)
	}
	if _, ok := opAccountRow(t, ds, account).Props[propOAuthClient]; ok {
		t.Fatal("the account still records its client")
	}

	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("refresh through the person's own client = %d, %v; want one", n, err)
	}

	if _, err := ds.Delete(ctx, substrate.ActorPerson, opMailConfig, fid("default"), substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the own client: %v", err)
	}
	// The refreshed token expires inside the window too, so the next pass
	// finds it due.
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
		t.Fatalf("refresh through the operator's client = %d, %v; want none", n, err)
	}
	if _, refreshes := f.seen(); len(refreshes) != 1 {
		t.Fatalf("refreshes = %+v, want only the one through the person's own client", refreshes)
	}
	if propString(opAccountRow(t, ds, account), propTokenError) != reasonOAuthClientChanged {
		t.Fatal("the account does not ask for a reconnect")
	}
	if item := opMailItem(t, ds); item.Source != substrate.SourceOperator || item.Readiness != substrate.ReadinessReconnectNeeded {
		t.Fatalf("item = %+v", item)
	}
}

// The operator's client secret reaches the provider's token endpoint and
// nothing else: not the function config, not a record, not a file of the
// repository (changelog segments, sealed files, the manifest), not the
// operator defaults read. A body of an operator-connected account reads no
// client record, so it calls the provider's own API whatever apiBase the
// person could write, and it still gets its token.
func TestOperatorOAuthClientSecretStaysInTheHost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, f, account := openOpMail(t)
	f.expiresIn = 120
	opConnect(t, ds, account)
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("refresh = %d, %v", n, err)
	}
	if _, refreshes := f.seen(); len(refreshes) != 1 || refreshes[0] != (opClientPair{opClientID, opClientSecret}) {
		t.Fatalf("refreshes = %+v, want one through the operator's pair", refreshes)
	}

	cfg, secrets, _, err := ds.resolveFunctionConfig(ctx, &vocabulary.Function{Package: opMailPackage})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if _, has := cfg["inputs"]; has {
		t.Fatalf("a body of an operator-connected account reads a client record: %v", cfg["inputs"])
	}
	entry := cfg["accounts"].([]any)[0].(map[string]any)
	if entry["token"] != "at2-"+opClientID {
		t.Fatalf("account entry = %v, want the refreshed token", entry)
	}
	raw, err := json.Marshal(map[string]any{"config": cfg, "secrets": secrets})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), opClientSecret) {
		t.Fatalf("the function config carries the operator's client secret: %s", raw)
	}

	read, err := ds.OperatorDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(read); strings.Contains(string(raw), opClientSecret) {
		t.Fatalf("the operator defaults read carries the secret: %s", raw)
	}
	var n int
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE props::text LIKE '%' || $1 || '%'`, opClientSecret).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d records carry the operator's client secret (%v)", n, err)
	}
	if err := filepath.WalkDir(ds.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), opClientSecret) {
			t.Errorf("%s carries the operator's client secret", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A failed install of a listed package reads install-failed with the
// installer's message, and the next open tries again: here, a restart.
func TestOperatorInstallFailureIsReportedAndRetried(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newOpTokenFake(t)
	cfg := opClientsConfig()
	cfg.Install = []string{opMailPackage}
	var mu sync.Mutex
	calls := 0
	installer := func(ctx context.Context, d substrate.Dataset, pkg string) error {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			return errors.New("the catalog is unreachable")
		}
		docs := opMailDocs(f.srv.URL+"/token", true)
		_, err := d.InstallBundleClosure(ctx, substrate.BundleActor(opMailAuthority, "mail"), docs, nil, substrate.BundleInstall{Published: true})
		return err
	}
	dsn, root := MigratedDSN(t), t.TempDir()
	start := func() substrate.Service {
		svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root),
			WithOAuth("test-state-key", "https://substrate.example/cb", f.srv.Client()),
			WithOperatorDefaults(cfg, "Example"), WithOperatorInstaller(installer))
		if err != nil {
			t.Fatalf("open engine: %v", err)
		}
		return svc
	}
	svc := start()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create: %v", err)
	}
	d, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	item := opMailItem(t, d.(*dataset))
	if item.Readiness != substrate.ReadinessInstallFailed || !strings.Contains(item.Message, "the catalog is unreachable") {
		t.Fatalf("item after the failed install = %+v", item)
	}
	_ = svc.Close()

	svc = start()
	t.Cleanup(func() { _ = svc.Close() })
	d, err = svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	ran := calls
	mu.Unlock()
	if ran != 2 {
		t.Fatalf("the installer ran %d times, want a retry at the next open", ran)
	}
	if item := opMailItem(t, d.(*dataset)); item.Readiness != substrate.ReadinessNeedsConnect || item.Message == "the catalog is unreachable" {
		t.Fatalf("item after the retry = %+v", item)
	}
}

// An invocation resolves the client input once, for the record it injects
// and the client it checks grants against. The person's own record goes
// after the inputs are injected and before the accounts get their tokens, so
// the operator's client resolves again by then: the operator-connected
// account still gets no token beside the person's apiBase, and is not
// stamped, because the operator's client is the one resolving now. The next
// invocation reads no client record and gets the token.
func TestOperatorOAuthConfigResolvesTheClientOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _, account := openOpMail(t)
	opConnect(t, ds, account)
	opPutOwnClient(t, ds, "default", opOwnClientWithBase)

	var once sync.Once
	ds.svc.testOAuthPoint = func(p string) {
		if p != oauthPointConfigInputs {
			return
		}
		once.Do(func() {
			if _, err := ds.Delete(ctx, substrate.ActorPerson, opMailConfig, fid("default"), substrate.DeleteInput{}); err != nil {
				t.Errorf("delete the own client: %v", err)
			}
		})
	}
	cfg, entry := opConfigEntry(t, ds)
	ds.svc.testOAuthPoint = nil
	if opInjectedBase(cfg) == "" {
		t.Fatalf("the config does not carry the record it resolved: %v", cfg["inputs"])
	}
	if _, has := entry["token"]; has || entry["tokenError"] != reasonOAuthClientChanged {
		t.Fatalf("an operator-issued token reached a body beside the person's apiBase: %v", entry)
	}
	opAccountConnectedThroughOperator(t, ds, account)

	cfg, entry = opConfigEntry(t, ds)
	if _, has := cfg["inputs"]; has || entry["token"] != "at-"+opClientID {
		t.Fatalf("after the record went: inputs %v, entry %v; want no client record and the token", cfg["inputs"], entry)
	}
}

// A token is handed out, and a refresh is sent, only for the grant whose
// recorded client was checked: the check reads the credential and the
// account's recorded client in one statement, under the refresh's turn too.
// Each case connects through the person's own client, whose record names an
// apiBase, and reconnects through the operator's at one boundary of the
// token's resolution. The account gets no token, the provider sees no
// refresh through the stale client, and the reconnect keeps its `connected`.
func TestOperatorOAuthTokenComesFromTheCheckedGrant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// connectOwn opens the provider and connects an account through the
	// person's own client, its tokens living expiresIn seconds.
	connectOwn := func(t *testing.T, expiresIn int) (*dataset, *opTokenFake, eref) {
		t.Helper()
		ds, f, account := openOpMail(t)
		f.set(func(f *opTokenFake) { f.expiresIn = expiresIn })
		opPutOwnClient(t, ds, "default", opOwnClientWithBase)
		if got := opConnect(t, ds, account); got != ownClientID {
			t.Fatalf("the connect named %q", got)
		}
		return ds, f, account
	}
	// reconnectAt reconnects through the operator's client the first time
	// the facility reaches point.
	reconnectAt := func(t *testing.T, ds *dataset, account eref, point string) {
		var once sync.Once
		ds.svc.testOAuthPoint = func(p string) {
			if p != point {
				return
			}
			once.Do(func() {
				if err := opReconnectThroughOperator(ds, account); err != nil {
					t.Errorf("reconnect at %s: %v", point, err)
				}
			})
		}
	}
	noToken := func(t *testing.T, ds *dataset, f *opTokenFake, account eref, cfg, entry map[string]any) {
		t.Helper()
		ds.svc.testOAuthPoint = nil
		if opInjectedBase(cfg) == "" {
			t.Fatalf("the config does not carry the person's record: %v", cfg["inputs"])
		}
		if tok, has := entry["token"]; has || entry["tokenError"] != reasonOAuthClientChanged {
			t.Fatalf("the operator's grant reached a body beside the person's apiBase: token %v, entry %v", tok, entry)
		}
		if _, refreshes := f.seen(); len(refreshes) != 0 {
			t.Fatalf("the provider saw refreshes %+v", refreshes)
		}
		opAccountConnectedThroughOperator(t, ds, account)
	}

	t.Run("before the token is read", func(t *testing.T) {
		t.Parallel()
		ds, f, account := connectOwn(t, 3600)
		reconnectAt(t, ds, account, oauthPointTokenRead)
		cfg, entry := opConfigEntry(t, ds)
		noToken(t, ds, f, account, cfg, entry)
	})
	t.Run("before the refresh takes its turn", func(t *testing.T) {
		t.Parallel()
		ds, f, account := connectOwn(t, 30) // due: inside a minute of expiry
		reconnectAt(t, ds, account, oauthPointRefreshTurn)
		cfg, entry := opConfigEntry(t, ds)
		noToken(t, ds, f, account, cfg, entry)
	})
	t.Run("while a refused refresh is in flight", func(t *testing.T) {
		t.Parallel()
		ds, f, account := connectOwn(t, 30)
		f.set(func(f *opTokenFake) {
			f.refuseRefresh = true
			f.onRefresh = func() {
				if err := opReconnectThroughOperator(ds, account); err != nil {
					t.Errorf("reconnect in flight: %v", err)
				}
			}
		})
		cfg, entry := opConfigEntry(t, ds)
		ds.svc.testOAuthPoint = nil
		if opInjectedBase(cfg) == "" {
			t.Fatalf("the config does not carry the person's record: %v", cfg["inputs"])
		}
		if tok, has := entry["token"]; has || entry["tokenError"] != reasonOAuthClientChanged {
			t.Fatalf("the reconnect's grant reached a body beside the person's apiBase: token %v, entry %v", tok, entry)
		}
		if _, refreshes := f.seen(); len(refreshes) != 1 || refreshes[0].id != ownClientID {
			t.Fatalf("refreshes = %+v, want the one refused refresh of the person's grant", refreshes)
		}
		opAccountConnectedThroughOperator(t, ds, account)
	})
	t.Run("the refresh loop and a replacement grant", func(t *testing.T) {
		t.Parallel()
		ds, f, account := connectOwn(t, 120) // inside the refresh window
		var once sync.Once
		ds.svc.testOAuthPoint = func(p string) {
			if p != oauthPointRefreshTurn {
				return
			}
			once.Do(func() {
				// The replacement grant is already expired, so nothing but
				// the client check stands between it and a refresh through
				// the person's client.
				f.set(func(f *opTokenFake) { f.expiresIn = -10 })
				if err := opReconnectThroughOperator(ds, account); err != nil {
					t.Errorf("reconnect: %v", err)
				}
			})
		}
		if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
			t.Fatalf("refresh pass = %d, %v; want nothing refreshed", n, err)
		}
		ds.svc.testOAuthPoint = nil
		if _, refreshes := f.seen(); len(refreshes) != 0 {
			t.Fatalf("the operator's grant was refreshed through %+v", refreshes)
		}
		opAccountConnectedThroughOperator(t, ds, account)
	})
}

// A client-change stamp is about the grant its caller checked: a reconnect
// that lands before the stamp takes the account's lock keeps its
// `connected`. Here the refresh pass resolves the person's own client while
// the account records the operator's; the owner then removes their record
// and reconnects through the operator's client before the stamp runs.
func TestOperatorOAuthClientStampSparesAReconnect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, f, account := openOpMail(t)
	f.set(func(f *opTokenFake) { f.expiresIn = 120 })
	opConnect(t, ds, account)
	opPutOwnClient(t, ds, "default", map[string]any{"clientId": ownClientID, "clientSecret": ownClientSecret})

	var once sync.Once
	ds.svc.testOAuthPoint = func(p string) {
		if p != oauthPointClientStamp {
			return
		}
		once.Do(func() {
			if err := opReconnectThroughOperator(ds, account); err != nil {
				t.Errorf("reconnect: %v", err)
			}
		})
	}
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
		t.Fatalf("refresh pass = %d, %v; want nothing refreshed", n, err)
	}
	ds.svc.testOAuthPoint = nil
	if _, refreshes := f.seen(); len(refreshes) != 0 {
		t.Fatalf("the provider saw refreshes %+v", refreshes)
	}
	opAccountConnectedThroughOperator(t, ds, account)
}

// A client-change stamp is decided on the client resolving when it commits,
// not the one its caller resolved. Here the refresh pass resolves the
// person's own client while the account records the operator's; the owner
// then removes their record before the stamp runs, without reconnecting.
// The operator's client resolves again and the grant was issued to it, so
// the account keeps its `connected`.
func TestOperatorOAuthClientStampRereadsTheClient(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, f, account := openOpMail(t)
	f.set(func(f *opTokenFake) { f.expiresIn = 120 })
	opConnect(t, ds, account)
	opPutOwnClient(t, ds, "default", map[string]any{"clientId": ownClientID, "clientSecret": ownClientSecret})

	var once sync.Once
	reached := false
	ds.svc.testOAuthPoint = func(p string) {
		if p != oauthPointClientStamp {
			return
		}
		once.Do(func() {
			reached = true
			if _, err := ds.Delete(ctx, substrate.ActorPerson, opMailConfig, fid("default"), substrate.DeleteInput{}); err != nil {
				t.Errorf("delete the own client: %v", err)
			}
		})
	}
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
		t.Fatalf("refresh pass = %d, %v; want nothing refreshed", n, err)
	}
	ds.svc.testOAuthPoint = nil
	if !reached {
		t.Fatal("the refresh pass never reached the client-change stamp")
	}
	if _, refreshes := f.seen(); len(refreshes) != 0 {
		t.Fatalf("the provider saw refreshes %+v", refreshes)
	}
	opAccountConnectedThroughOperator(t, ds, account)
}

// A token without expiry is never refreshed, so no refresh clears a
// client-change stamp its owner reverts. The token read does: once the
// client the grant was issued to resolves again, the next token handed out
// leaves the account `connected` and the operator defaults item ready.
func TestOperatorOAuthRevertedClientChangeClearsOnTheNextToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, f, account := openOpMail(t)
	f.set(func(f *opTokenFake) { f.expiresIn = 0 }) // expires_in 0: a token without expiry
	opConnect(t, ds, account)

	opPutOwnClient(t, ds, "default", map[string]any{"clientId": ownClientID, "clientSecret": ownClientSecret})
	if _, entry := opConfigEntry(t, ds); entry["tokenError"] != reasonOAuthClientChanged {
		t.Fatalf("entry = %v, want the client-change refusal", entry)
	}
	if row := opAccountRow(t, ds, account); propString(row, propTokenStatus) != tokenStatusErroring || propString(row, propTokenError) != reasonOAuthClientChanged {
		t.Fatalf("account = %v / %v, want the client-change stamp", row.Props[propTokenStatus], row.Props[propTokenError])
	}

	if _, err := ds.Delete(ctx, substrate.ActorPerson, opMailConfig, fid("default"), substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the own client: %v", err)
	}
	if _, entry := opConfigEntry(t, ds); entry["token"] != "at-"+opClientID {
		t.Fatalf("entry = %v, want the operator's grant's token", entry)
	}
	opAccountConnectedThroughOperator(t, ds, account)
	if item := opMailItem(t, ds); item.Readiness != substrate.ReadinessReady {
		t.Fatalf("item = %+v, want ready", item)
	}
	if _, refreshes := f.seen(); len(refreshes) != 0 {
		t.Fatalf("the provider saw refreshes %+v", refreshes)
	}
}

// `operatorOffers` says whether the operator's client would serve the
// package once the person's own record is gone, so it is false where the
// operator's client is refused: an install that cannot record the client,
// and a copy the repository owns. The published provider that records it
// is offered with and without the person's record.
func TestOperatorOAuthOffersOnlyWhereItServes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		name                      string
		published, withProvenance bool
		offers                    bool
	}{
		{"the published provider", true, true, true},
		{"an install that cannot record the client", true, false, false},
		{"the repository's own copy", false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newOpTokenFake(t)
			ds := openCoreDataset(t,
				WithOAuth("test-state-key", "https://substrate.example/cb", f.srv.Client()),
				WithOperatorDefaults(opClientsConfig(), "Example"))
			installOpMail(t, ds, f.srv.URL+"/token", tc.published, tc.withProvenance)
			opPutOwnClient(t, ds, "default", map[string]any{"clientId": ownClientID, "clientSecret": ownClientSecret})
			if item := opMailItem(t, ds); item.Source != substrate.SourceOwn || item.OperatorOffers != tc.offers {
				t.Fatalf("with the person's record: %+v, want own and operatorOffers %v", item, tc.offers)
			}
			if _, err := ds.Delete(ctx, substrate.ActorPerson, opMailConfig, fid("default"), substrate.DeleteInput{}); err != nil {
				t.Fatalf("delete the own client: %v", err)
			}
			item := opMailItem(t, ds)
			if item.OperatorOffers != tc.offers || (item.Source == substrate.SourceOperator) != tc.offers {
				t.Fatalf("without the person's record: %+v, want operatorOffers %v and the operator as source only where offered", item, tc.offers)
			}
		})
	}
}

// openOpMailAt opens a service over dsn and root with the OAuth facility on
// against f and cfg as the operator's file, and returns it with the test
// repository's dataset, creating the repository when create is set.
func openOpMailAt(t *testing.T, dsn, root string, f *opTokenFake, cfg *operatordefaults.Config, create bool, opts ...Option) (*service, *dataset) {
	t.Helper()
	ctx := context.Background()
	svc, err := OpenForTest(t, ctx, dsn, append([]Option{
		WithDataRoot(root), WithKindsFS(SeedKinds), WithCredentialKey(TestCredentialKey),
		WithOAuth("test-state-key", "https://substrate.example/cb", f.srv.Client()),
		WithOperatorDefaults(cfg, "Example"),
	}, opts...)...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if create {
		if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
			t.Fatalf("create repository: %v", err)
		}
	}
	d, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	return svc.(*service), d.(*dataset)
}

// An operator that removes its client from the file leaves the accounts
// connected through it with no client to refresh through: nothing is sent
// to the provider, with or without a secret, and each account reads
// `erroring` with the reconnect ask at the next refresh pass, and an
// invocation gets the same reason in place of a token.
func TestOperatorOAuthClientRemovedFromTheFileNeedsAReconnect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root := MigratedDSN(t), t.TempDir()
	f := newOpTokenFake(t)
	f.set(func(f *opTokenFake) { f.expiresIn = 120 })
	svc, ds := openOpMailAt(t, dsn, root, f, opClientsConfig(), true)
	installOpMail(t, ds, f.srv.URL+"/token", true, true)
	account := opPutAccount(t, ds, "a1")
	opConnect(t, ds, account)
	_ = svc.Close()

	_, ds = openOpMailAt(t, dsn, root, f, &operatordefaults.Config{}, false)
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
		t.Fatalf("refresh pass = %d, %v; want nothing refreshed", n, err)
	}
	if _, refreshes := f.seen(); len(refreshes) != 0 {
		t.Fatalf("the provider saw refreshes %+v with the operator's client gone", refreshes)
	}
	row := opAccountRow(t, ds, account)
	if propString(row, propTokenStatus) != tokenStatusErroring || propString(row, propTokenError) != reasonOAuthClientChanged {
		t.Fatalf("account = %v / %q, want erroring with the reconnect ask", row.Props[propTokenStatus], row.Props[propTokenError])
	}
	_, entry := opConfigEntry(t, ds)
	if _, has := entry["token"]; has || entry["tokenError"] != reasonOAuthClientChanged {
		t.Fatalf("account entry = %v, want no token and the reconnect ask", entry)
	}
}

// Concurrent first opens of a repository whose file lists a package it never
// held run one install: every caller but the opener returns while it runs,
// none surfaces an error, and no failure is remembered for the package.
func TestOperatorInstallConcurrentFirstOpensInstallOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root := MigratedDSN(t), t.TempDir()
	f := newOpTokenFake(t)
	svc, _ := openOpMailAt(t, dsn, root, f, nil, true)
	_ = svc.Close()

	const callers = 8
	returned := make(chan struct{}, callers)
	var mu sync.Mutex
	calls := 0
	installer := func(ctx context.Context, d substrate.Dataset, pkg string) error {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			// The other callers return while this install runs; a guard
			// timer only turns a hang into a failure.
			guard := time.NewTimer(time.Minute)
			defer guard.Stop()
			for range callers - 1 {
				select {
				case <-returned:
				case <-guard.C:
					return errors.New("the other callers did not return while the install ran")
				}
			}
		}
		docs := opMailDocs(f.srv.URL+"/token", true)
		_, err := d.InstallBundleClosure(ctx, substrate.BundleActor(opMailAuthority, "mail"), docs, nil, substrate.BundleInstall{Published: true})
		return err
	}
	cfg := opClientsConfig()
	cfg.Install = []string{opMailPackage}
	svc2, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithKindsFS(SeedKinds), WithCredentialKey(TestCredentialKey),
		WithOAuth("test-state-key", "https://substrate.example/cb", f.srv.Client()),
		WithOperatorDefaults(cfg, "Example"), WithOperatorInstaller(installer))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })

	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc2.Dataset(ctx, testdb.Repository(t))
			errs <- err
			returned <- struct{}{}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("an open surfaced %v", err)
		}
	}
	mu.Lock()
	ran := calls
	mu.Unlock()
	if ran != 1 {
		t.Fatalf("%d concurrent first opens ran %d installs, want 1", callers, ran)
	}
	if msg := svc2.(*service).operatorInstalls.failure(testdb.Repository(t), opMailPackage); msg != "" {
		t.Fatalf("the install is remembered as failed: %s", msg)
	}
	d, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.(*dataset).registry().BundleOf(opMailPackage); !ok {
		t.Fatal("the listed package is not installed")
	}
}
