package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	applePkg       = "providers.substrate.reamde.dev/apple"
	appleHealthPkg = "providers.substrate.reamde.dev/applehealth"
	healthPkg      = "substrate.reamde.dev/health"
	healthAccount  = appleHealthPkg + "/account"
	healthWebhook  = "applehealth-webhook"
	syncAccountID  = "substrate-sync-1a2b3c4d"
	syncScopes     = "write:" + applePkg + "/* read:" + healthAccount + " read:" + appleHealthPkg + "/export"
)

// syncDetails is what Substrate Sync asks to have set up.
var syncDetails = `[
  {"type": "package", "package": "` + applePkg + `", "version": 3},
  {"type": "package", "package": "` + appleHealthPkg + `", "version": 5},
  {"type": "account", "kind": "` + healthAccount + `", "id": "` + syncAccountID + `",
   "properties": {"displayName": "Ada's iPhone", "timezone": "Europe/London"}},
  {"type": "webhook", "trigger": "` + healthWebhook + `"}
]`

// setupRepo is what the fake's repository holds beyond the fake itself: the
// packages installed through the catalog's door, in the order they landed.
type setupRepo struct {
	mu       sync.Mutex
	statuses map[string]substrate.BundleStatus
	kinds    []substrate.KindInfo
	installs []string
	// gate, when set, holds every install until it is closed; fail refuses
	// the install of the bundles it names, and explode panics in it.
	gate    chan struct{}
	fail    map[string]error
	explode string
	// previews counts the upgrade previews the catalog asked for.
	previews int
}

// setupDataset is the flow's dataset with the catalog's install door, a put
// that merges and honors ifVersion as the engine's does, and the token list
// the flow mints into.
type setupDataset struct {
	*mintingDataset
	repo *setupRepo
}

func (d *setupDataset) Kinds(context.Context) ([]substrate.KindInfo, error) {
	d.repo.mu.Lock()
	defer d.repo.mu.Unlock()
	return append(slices.Clone(d.types), d.repo.kinds...), nil
}

func (d *setupDataset) KindByRef(ctx context.Context, identity string) (substrate.KindInfo, error) {
	kinds, _ := d.Kinds(ctx)
	for _, k := range kinds {
		if k.Identity == identity {
			return k, nil
		}
	}
	return d.fakeDataset.KindByRef(ctx, identity)
}

func (d *setupDataset) BundleStatusesUncounted(context.Context) ([]substrate.BundleStatus, error) {
	d.repo.mu.Lock()
	defer d.repo.mu.Unlock()
	out := []substrate.BundleStatus{}
	for _, st := range d.repo.statuses {
		out = append(out, st)
	}
	return out, nil
}

func (d *setupDataset) BundleStatuses(ctx context.Context) ([]substrate.BundleStatus, error) {
	return d.BundleStatusesUncounted(ctx)
}

func (d *setupDataset) BundleStatus(_ context.Context, id string) (substrate.BundleStatus, error) {
	d.repo.mu.Lock()
	defer d.repo.mu.Unlock()
	st, ok := d.repo.statuses[id]
	if !ok {
		return substrate.BundleStatus{}, noSuch("bundle", id)
	}
	return st, nil
}

func (d *setupDataset) InstallBundleClosure(ctx context.Context, _ substrate.Actor, docs []map[string]any, data []substrate.PutInput, opts substrate.BundleInstall) ([]*substrate.Record, error) {
	d.repo.mu.Lock()
	gate := d.repo.gate
	d.repo.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var id string
	var kinds []substrate.KindInfo
	for _, doc := range docs {
		data, _ := doc["data"].(map[string]any)
		switch doc["kind"] {
		case vocabulary.CoreKind(vocabulary.DocBundle):
			id = docMetadataID(doc)
		case vocabulary.CoreKind(vocabulary.DocKind):
			ref := docMetadataID(doc)
			authority, pkg, name := vocabulary.SplitKindRef(ref)
			kinds = append(kinds, substrate.KindInfo{
				Identity: ref, Name: name, Authority: authority, Package: pkg, Version: 1,
				Source: vocabulary.SourcePublished, Definition: data, Label: definitionLabel(data),
			})
		}
	}
	d.repo.mu.Lock()
	if err := d.repo.fail[id]; err != nil {
		d.repo.mu.Unlock()
		return nil, err
	}
	if d.repo.explode == id {
		d.repo.mu.Unlock()
		panic("the install of " + id + " exploded")
	}
	authority, pkg := vocabulary.SplitPackageRef(id)
	d.repo.statuses[id] = substrate.BundleStatus{
		ID: id, Name: pkg, Authority: authority, Package: pkg, Installed: true, Enabled: true,
		Version: opts.ShippedVersion, ShippedVersion: opts.ShippedVersion,
	}
	d.repo.kinds = append(d.repo.kinds, kinds...)
	d.repo.installs = append(d.repo.installs, id)
	d.repo.mu.Unlock()
	d.fakeDataset.mu.Lock()
	defer d.fakeDataset.mu.Unlock()
	for _, in := range data {
		version := int64(1)
		if held, ok := d.records[in.ID]; ok {
			version = held.Version + 1
		}
		d.put(&substrate.Record{ID: in.ID, Kind: in.Kind, Properties: cloneProperties(in.Properties), Version: version})
	}
	return nil, nil
}

func (d *setupDataset) PlanBundleUpgrade(context.Context, []map[string]any) (substrate.BundleUpgrade, error) {
	d.repo.mu.Lock()
	defer d.repo.mu.Unlock()
	d.repo.previews++
	return substrate.BundleUpgrade{}, nil
}

// Put merges into the row it finds and holds ifVersion to it, a tombstone
// included, as the engine's put does.
func (d *setupDataset) Put(ctx context.Context, actor substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	d.fakeDataset.mu.Lock()
	held := d.records[in.ID]
	if held != nil && held.Kind != in.Kind {
		held = nil
	}
	if err := fakeCAS(held, in.IfVersion); err != nil {
		d.fakeDataset.mu.Unlock()
		return nil, err
	}
	if held != nil {
		merged := cloneProperties(held.Properties)
		for k, v := range in.Properties {
			merged[k] = v
		}
		in.Properties = merged
	}
	d.fakeDataset.mu.Unlock()
	return d.fakeDataset.Put(ctx, actor, in)
}

func (d *setupDataset) Tokens(context.Context) ([]substrate.TokenInfo, error) {
	d.svc.mu.Lock()
	defer d.svc.mu.Unlock()
	var out []substrate.TokenInfo
	for _, t := range d.svc.tokens {
		if t.repository == d.repository.Authority {
			out = append(out, t.info)
		}
	}
	return out, nil
}

type setupEnv struct {
	*appEnv
	repo  *setupRepo
	repos func(string) *setupRepo
}

func newSetupEnv(t *testing.T) *setupEnv {
	t.Helper()
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	svc := newFakeService()
	var mu sync.Mutex
	repos := map[string]*setupRepo{}
	repoFor := func(name string) *setupRepo {
		mu.Lock()
		defer mu.Unlock()
		if repos[name] == nil {
			repos[name] = &setupRepo{statuses: map[string]substrate.BundleStatus{}, fail: map[string]error{}}
		}
		return repos[name]
	}
	svc.wrap = func(d *fakeDataset) substrate.Dataset {
		return &setupDataset{mintingDataset: &mintingDataset{fakeDataset: d, svc: svc}, repo: repoFor(d.repository.Authority)}
	}
	docs := &memDocs{docs: map[string]authorize.Document{
		appClientID: {ClientID: appClientID, RedirectURIs: []string{appRedirect}, Name: "Sync"},
	}}
	e := &appEnv{
		t: t, svc: svc, docs: docs, owner: svc.token(fakeRepository),
		h: New(Config{Service: svc, ClientDocuments: docs, AuthInterval: time.Nanosecond, Catalog: cat}),
	}
	return &setupEnv{appEnv: e, repo: repoFor(fakeRepository), repos: repoFor}
}

func setupRequest(scopes, details string) map[string]string {
	p := appRequest(scopes)
	p["authorization_details"] = details
	return p
}

func (e *setupEnv) view(id string) substrate.AuthorizationRequest {
	e.t.Helper()
	rec := e.show(id)
	wantStatus(e.t, rec, http.StatusOK)
	return decodeJSON[substrate.AuthorizationRequest](e.t, rec)
}

func (e *setupEnv) approveWith(id string, scopes []string, hash string) *httptest.ResponseRecorder {
	days := 30
	body, _ := json.Marshal(substrate.AuthorizationApproval{Scopes: scopes, LifetimeDays: &days, PlanHash: hash})
	return e.do(http.MethodPost, "/api/v1/authorizations/"+id, e.owner, body, "application/json")
}

// settle reads the request until its set up has ended: approved, or pending
// again with a failure.
func (e *setupEnv) settle(id string) substrate.AuthorizationRequest {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		v := e.view(id)
		if v.State == substrate.AuthorizationApproved || (v.State == substrate.AuthorizationPending && v.Failure != nil) {
			return v
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("the set up did not end: %+v", v)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func resolvedSyncScopes() []string {
	return []string{"write:" + applePkg + "/*", "read:" + healthAccount, "read:" + appleHealthPkg + "/export"}
}

func TestTheApprovalPageShowsWhatApprovingSetsUp(t *testing.T) {
	e := newSetupEnv(t)
	md := decodeJSON[authorizationServerMetadata](t, e.do(http.MethodGet, asMetadataPath, "", nil, ""))
	if strings.Join(md.AuthorizationDetailsTypesSupported, ",") != "package,account,webhook" {
		t.Fatalf("authorization_details_types_supported = %v", md.AuthorizationDetailsTypesSupported)
	}
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	if v.State != substrate.AuthorizationPending || v.PlanHash == "" || len(v.Details) != 4 || len(v.Setup) != 4 {
		t.Fatalf("view = state %q hash %q, %d details, %d rows", v.State, v.PlanHash, len(v.Details), len(v.Setup))
	}
	apple := v.Setup[0]
	if apple.Action != substrate.SetupInstall || apple.Package.Tier != substrate.TierProvider || apple.Package.Shipped != 3 ||
		apple.Package.Held != 0 || apple.Package.Required != 3 || len(apple.Package.Kinds) == 0 || len(apple.Package.Functions) != 0 {
		t.Fatalf("apple row = %+v %+v", apple, apple.Package)
	}
	for _, k := range apple.Package.Kinds {
		if !k.Added || vocabulary.KindPackage(k.Identity) != applePkg {
			t.Fatalf("apple adds %+v", k)
		}
	}
	health := v.Setup[1]
	if health.Action != substrate.SetupInstall || health.Package.Shipped != 5 ||
		len(health.Package.Requires) != 1 || health.Package.Requires[0].ID != healthPkg ||
		health.Package.Requires[0].Action != substrate.SetupInstall {
		t.Fatalf("applehealth row = %+v %+v", health, health.Package)
	}
	var adapter *substrate.AuthorizationFunction
	for i, f := range health.Package.Functions {
		if f.ID == appleHealthPkg+"/adaptapplehealth" {
			adapter = &health.Package.Functions[i]
		}
	}
	if adapter == nil || !slices.Contains(adapter.Writes, "substrate.reamde.dev/observations/observation") {
		t.Fatalf("applehealth's functions = %+v", health.Package.Functions)
	}
	var hook, record *substrate.AuthorizationTrigger
	for i, tr := range health.Package.Triggers {
		switch tr.ID {
		case healthWebhook:
			hook = &health.Package.Triggers[i]
		case "applehealth-to-health":
			record = &health.Package.Triggers[i]
		}
	}
	if hook == nil || !hook.Webhook || hook.Enabled || record == nil || record.Webhook || !record.Enabled {
		t.Fatalf("applehealth's triggers = %+v", health.Package.Triggers)
	}
	// The requirement's kinds land too, and are listed as added.
	if !slices.ContainsFunc(health.Package.Kinds, func(k substrate.AuthorizationKind) bool { return k.Identity == healthPkg+"/sleep" }) {
		t.Fatalf("applehealth adds %+v, want health's kinds among them", health.Package.Kinds)
	}
	acct := v.Setup[2]
	if acct.Action != substrate.SetupCreate || acct.Account.Kind != healthAccount || acct.Account.ID != syncAccountID ||
		acct.Account.Properties["displayName"] != "Ada's iPhone" {
		t.Fatalf("account row = %+v %+v", acct, acct.Account)
	}
	hookRow := v.Setup[3]
	if hookRow.Action != substrate.SetupArm || hookRow.Webhook.Package != appleHealthPkg || hookRow.Webhook.Enabled || hookRow.Webhook.Keyed {
		t.Fatalf("webhook row = %+v %+v", hookRow, hookRow.Webhook)
	}
	// A scope row lists the kinds the set up would add that it reaches,
	// an internal one through its exact scope alone.
	byScope := map[string]substrate.AuthorizationScope{}
	for _, s := range v.Scopes {
		byScope[s.Scope] = s
	}
	accountScope := byScope["read:"+healthAccount]
	if len(accountScope.Kinds) != 1 || !accountScope.Kinds[0].Added || accountScope.Kinds[0].Purpose != vocabulary.PurposeInternal {
		t.Fatalf("the account scope reaches %+v", accountScope.Kinds)
	}
	appleScope := byScope["write:"+applePkg+"/*"]
	if len(appleScope.Kinds) == 0 || slices.ContainsFunc(appleScope.Kinds, func(k substrate.AuthorizationKind) bool {
		return !k.Added || k.Purpose == vocabulary.PurposeInternal
	}) {
		t.Fatalf("the apple glob reaches %+v", appleScope.Kinds)
	}
	// The plan is the same on a second read.
	if again := e.view(id); again.PlanHash != v.PlanHash {
		t.Fatalf("plan hash moved between two reads: %s, %s", v.PlanHash, again.PlanHash)
	}
	// A malformed detail is refused at the door, with a page.
	rec := e.do(http.MethodGet, authorizeURL(setupRequest(syncScopes, `[{"type":"record"}]`)), "", nil, "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("a malformed detail = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestASetUpThatCannotRunIsBlockedAndRefused(t *testing.T) {
	e := newSetupEnv(t)
	// A trigger of another package's, already here under the id the app
	// names.
	ds := e.svc.datasets[fakeRepository]
	ds.put(&substrate.Record{ID: "notes-webhook", Kind: setupTriggerKind, Version: 1, Properties: map[string]any{
		"enabled":  false,
		"source":   map[string]any{"webhook": map[string]any{}},
		"callable": map[string]any{"ref": "substrate.reamde.dev/core/function/providers.substrate.reamde.dev/google/googlesync"},
	}})
	// The owner's own package, installed, whose keyed and enabled trigger
	// an app names: nothing on the page says it is the app's, so neither
	// row may pass as already in place.
	own := fakeRepository + "/stripe"
	e.repo.mu.Lock()
	e.repo.statuses[own] = substrate.BundleStatus{ID: own, Installed: true, Enabled: true, Version: 1}
	e.repo.mu.Unlock()
	ds.put(&substrate.Record{ID: "stripe-webhook", Kind: setupTriggerKind, Version: 1, Properties: map[string]any{
		"enabled":  true,
		"source":   map[string]any{"webhook": map[string]any{"key": "the-owners-key-0123456789"}},
		"callable": map[string]any{"ref": "substrate.reamde.dev/core/function/" + own + "/handle"},
	}})
	cases := map[string]struct {
		details string
		row     int
		fix     string
		problem string
	}{
		"a sample": {
			details: `[{"type":"package","package":"samples.substrate.reamde.dev/tasks","version":1}]`,
			fix:     "/data", problem: "sample",
		},
		"a version the server does not ship": {
			details: `[{"type":"package","package":"` + appleHealthPkg + `","version":99}]`,
			problem: "This server ships version 5 of applehealth, and the app needs version 99. The server needs an update.",
		},
		"a package outside the catalog": {
			details: `[{"type":"package","package":"example.com/notes","version":1}]`,
			problem: "does not offer",
		},
		"an installed package of your own": {
			details: `[{"type":"package","package":"` + own + `","version":1},{"type":"webhook","trigger":"stripe-webhook"}]`,
			problem: "does not offer",
		},
		"an account of a package not requested": {
			details: `[{"type":"package","package":"` + applePkg + `","version":3},
			{"type":"account","kind":"` + healthAccount + `","id":"a"}]`,
			row: 1, problem: "does not set up",
		},
		"an account kind that is no account": {
			details: `[{"type":"package","package":"` + applePkg + `","version":3},
			{"type":"account","kind":"` + applePkg + `/contact","id":"a"}]`,
			row: 1, problem: "not an account kind",
		},
		"a secret property": {
			details: `[{"type":"package","package":"` + appleHealthPkg + `","version":5},
			{"type":"account","kind":"` + healthAccount + `","id":"a","properties":{"tokenRef":"x"}}]`,
			row: 1, problem: "secret",
		},
		"another hand's property": {
			details: `[{"type":"package","package":"` + appleHealthPkg + `","version":5},
			{"type":"account","kind":"` + healthAccount + `","id":"a","properties":{"tokenStatus":"x"}}]`,
			row: 1, problem: "the provider's sign-in",
		},
		"an undeclared property": {
			details: `[{"type":"package","package":"` + appleHealthPkg + `","version":5},
			{"type":"account","kind":"` + healthAccount + `","id":"a","properties":{"nickname":"x"}}]`,
			row: 1, problem: "no nickname property",
		},
		"a webhook another package's callable takes": {
			details: `[{"type":"package","package":"` + applePkg + `","version":3},{"type":"webhook","trigger":"notes-webhook"}]`,
			row:     1, problem: "code from google",
		},
		"a webhook of a package not requested": {
			details: `[{"type":"package","package":"` + applePkg + `","version":3},{"type":"webhook","trigger":"` + healthWebhook + `"}]`,
			row:     1, problem: "no such trigger",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			id := e.park(setupRequest("read:"+personKind, c.details))
			v := e.view(id)
			row := v.Setup[c.row]
			if row.Action != substrate.SetupBlocked || row.Fix != c.fix || !strings.Contains(row.Problem, c.problem) {
				t.Fatalf("row %d = %+v, want blocked with %q and fix %q", c.row, row, c.problem, c.fix)
			}
			// The owner reads it as a sentence, and the technical view
			// shows the ids beside it.
			if !regexp.MustCompile(`^[A-Z][^:/]*\.$`).MatchString(row.Problem) {
				t.Fatalf("problem %q is not a sentence free of ids", row.Problem)
			}
			// Blocked keeps the denial and refuses the approval.
			rec := e.approveWith(id, []string{"read:" + personKind}, v.PlanHash)
			wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
			if got := e.view(id); got.State != substrate.AuthorizationPending {
				t.Fatalf("a refused approval left the request %q", got.State)
			}
		})
	}
	e.svc.mu.Lock()
	defer e.svc.mu.Unlock()
	for _, tok := range e.svc.tokens {
		if slices.ContainsFunc(tok.info.Scopes, func(s string) bool { return strings.HasPrefix(s, "webhook:") }) {
			t.Fatalf("a blocked set up minted %v", tok.info.Scopes)
		}
	}
}

func TestApprovingRunsTheSetUpAndTheTokenCarriesIt(t *testing.T) {
	e := newSetupEnv(t)
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	// The approval names the plan it read.
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), ""), http.StatusUnprocessableEntity)
	rec := e.approveWith(id, resolvedSyncScopes(), v.PlanHash)
	wantStatus(t, rec, http.StatusAccepted)
	if a := decodeJSON[substrate.AuthorizationAnswer](t, rec); a.State != substrate.AuthorizationSettingUp || a.Redirect != "" {
		t.Fatalf("answer = %+v", a)
	}
	done := e.settle(id)
	if done.State != substrate.AuthorizationApproved || !strings.HasPrefix(done.Next, appRedirect+"?") ||
		strings.Join(done.Progress, ",") != "done,done,done,done" {
		t.Fatalf("after the set up = state %q next %q progress %v failure %+v", done.State, done.Next, done.Progress, done.Failure)
	}
	// The requirement landed before what requires it.
	e.repo.mu.Lock()
	installs := strings.Join(e.repo.installs, ",")
	e.repo.mu.Unlock()
	if installs != applePkg+","+healthPkg+","+appleHealthPkg {
		t.Fatalf("installed %s", installs)
	}
	ds := e.svc.datasets[fakeRepository]
	ds.mu.Lock()
	account, trigger := ds.records[syncAccountID], ds.records[healthWebhook]
	ds.mu.Unlock()
	if account == nil || account.Kind != healthAccount || account.Properties["displayName"] != "Ada's iPhone" {
		t.Fatalf("account = %+v", account)
	}
	key := triggerKeyOf(trigger.Properties)
	if trigger.Properties["enabled"] != true || !regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`).MatchString(key) ||
		referencePath(trigger.Properties["callable"]) == "" {
		t.Fatalf("trigger = %+v", trigger.Properties)
	}
	// Answered once: the page reads where to go, nothing answers it again.
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodDelete, "/api/v1/authorizations/"+id, e.owner, nil, ""), http.StatusNotFound)

	tok := e.exchange(tokenForm(codeFrom(t, done.Next)))
	wantStatus(t, tok, http.StatusOK)
	res := decodeJSON[tokenResponse](t, tok)
	if !strings.Contains(" "+res.Scope+" ", " webhook:"+healthWebhook+" ") {
		t.Fatalf("token scope = %q, want the webhook's", res.Scope)
	}
	got := res.AuthorizationDetails
	if len(got) != 4 || got[0].Version != 3 || got[1].Version != 5 || got[2].Kind != healthAccount || got[2].ID != syncAccountID ||
		got[2].Properties != nil || got[3].URL != "https://substrate.example.com/webhooks/"+fakeRepository+"/"+healthWebhook {
		t.Fatalf("authorization_details = %+v", got)
	}
	// The key never leaves the server.
	if strings.Contains(tok.Body.String(), key) || strings.Contains(e.show(id).Body.String(), key) {
		t.Fatal("the webhook key reached an answer")
	}

	// The app asks again: everything is in place, the token it holds is
	// the previous one, and the approval is today's.
	again := e.park(setupRequest(syncScopes, syncDetails))
	v = e.view(again)
	for i, row := range v.Setup {
		if row.Action != substrate.SetupNone {
			t.Fatalf("row %d after the set up = %+v", i, row)
		}
	}
	if v.Setup[0].Package.Held != 3 || v.Setup[3].Webhook.Keyed != true {
		t.Fatalf("rows after the set up = %+v %+v", v.Setup[0].Package, v.Setup[3].Webhook)
	}
	if v.Previous == nil || !slices.Contains(v.Previous.Scopes, "webhook:"+healthWebhook) {
		t.Fatalf("previous = %+v", v.Previous)
	}
	rec = e.approveWith(again, resolvedSyncScopes(), v.PlanHash)
	wantStatus(t, rec, http.StatusOK)
	tok = e.exchange(tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)))
	wantStatus(t, tok, http.StatusOK)
	if res := decodeJSON[tokenResponse](t, tok); !strings.Contains(res.Scope, "webhook:"+healthWebhook) || len(res.AuthorizationDetails) != 4 {
		t.Fatalf("a re-approval's token = %+v", res)
	}
	// Arming again never replaced the key.
	ds.mu.Lock()
	still := triggerKeyOf(ds.records[healthWebhook].Properties)
	ds.mu.Unlock()
	if still != key {
		t.Fatal("the key changed")
	}
}

func TestAnApprovalOfAPlanThatMovedIsRefused(t *testing.T) {
	e := newSetupEnv(t)
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	wantErrorCode(t, e.approveWith(id, resolvedSyncScopes(), "0000"), http.StatusConflict, codeConflict)
	// apple lands from the provider page meanwhile: the plan the page
	// showed is not the one approving would run.
	e.repo.mu.Lock()
	e.repo.statuses[applePkg] = substrate.BundleStatus{ID: applePkg, Installed: true, Version: 3, ShippedVersion: 3}
	e.repo.mu.Unlock()
	wantErrorCode(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusConflict, codeConflict)
	moved := e.view(id)
	if moved.State != substrate.AuthorizationPending || moved.Setup[0].Action != substrate.SetupNone || moved.PlanHash == v.PlanHash {
		t.Fatalf("after the refusals = state %q apple %q", moved.State, moved.Setup[0].Action)
	}
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), moved.PlanHash), http.StatusAccepted)
	if done := e.settle(id); done.State != substrate.AuthorizationApproved {
		t.Fatalf("the set up ended %+v", done.Failure)
	}
	e.repo.mu.Lock()
	defer e.repo.mu.Unlock()
	if slices.Contains(e.repo.installs, applePkg) {
		t.Fatalf("a package already in place was installed again: %v", e.repo.installs)
	}
}

func TestNothingElseAnswersARequestWhileItIsSettingUp(t *testing.T) {
	e := newSetupEnv(t)
	other := "other.example.com"
	e.svc.addRepository(other)
	stranger := e.svc.token(other)
	gate := make(chan struct{})
	e.repo.mu.Lock()
	e.repo.gate = gate
	e.repo.mu.Unlock()
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	days := 30
	body, _ := json.Marshal(substrate.AuthorizationApproval{Scopes: resolvedSyncScopes(), LifetimeDays: &days, PlanHash: v.PlanHash})
	// Another repository is refused before anything runs.
	wantStatus(t, e.do(http.MethodPost, "/api/v1/authorizations/"+id, stranger, body, "application/json"), http.StatusNotFound)
	if e.repos(other).installs != nil || e.repo.installs != nil {
		t.Fatal("another repository's approval ran a set up")
	}
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusAccepted)
	// The first install is held at the gate: the page reads it running.
	deadline := time.Now().Add(10 * time.Second)
	for {
		running := e.view(id)
		if running.State != substrate.AuthorizationSettingUp {
			t.Fatalf("while it runs = state %q", running.State)
		}
		if len(running.Progress) == 4 && running.Progress[0] == setupRunning && running.Progress[1] == setupWaiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the set up never reported its first row running: %v", running.Progress)
		}
		time.Sleep(5 * time.Millisecond)
	}
	wantErrorCode(t, e.do(http.MethodDelete, "/api/v1/authorizations/"+id, e.owner, nil, ""), http.StatusConflict, codeConflict)
	wantErrorCode(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusConflict, codeConflict)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		wantStatus(t, e.do(method, "/api/v1/authorizations/"+id, stranger, body, "application/json"), http.StatusNotFound)
	}
	close(gate)
	if done := e.settle(id); done.State != substrate.AuthorizationApproved {
		t.Fatalf("the set up ended %+v", done.Failure)
	}
}

func TestAFailedSetUpIsPendingAgainAndTriesWhatIsLeft(t *testing.T) {
	e := newSetupEnv(t)
	e.repo.mu.Lock()
	e.repo.fail[appleHealthPkg] = fmt.Errorf("%w: the closure does not admit", substrate.ErrValidation)
	e.repo.mu.Unlock()
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusAccepted)
	failed := e.settle(id)
	if failed.State != substrate.AuthorizationPending || failed.Failure == nil || failed.Failure.Row != 1 ||
		!strings.Contains(failed.Failure.Message, "does not admit") || failed.Progress[0] != setupDone ||
		failed.Progress[1] != setupFailed || failed.Progress[2] != setupWaiting {
		t.Fatalf("after a failure = %+v progress %v", failed.Failure, failed.Progress)
	}
	// What landed stays, and the plan says so.
	if failed.Setup[0].Action != substrate.SetupNone || failed.Setup[1].Action != substrate.SetupInstall {
		t.Fatalf("rows after a failure = %q %q", failed.Setup[0].Action, failed.Setup[1].Action)
	}
	e.repo.mu.Lock()
	delete(e.repo.fail, appleHealthPkg)
	e.repo.mu.Unlock()
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), failed.PlanHash), http.StatusAccepted)
	done := e.settle(id)
	if done.State != substrate.AuthorizationApproved || done.Failure != nil {
		t.Fatalf("trying again = state %q failure %+v", done.State, done.Failure)
	}
	e.repo.mu.Lock()
	defer e.repo.mu.Unlock()
	if n := len(slices.DeleteFunc(slices.Clone(e.repo.installs), func(s string) bool { return s != applePkg })); n != 1 {
		t.Fatalf("apple was installed %d times: %v", n, e.repo.installs)
	}
}

// holdTurn starts a set up of applehealth whose install waits at a gate, so
// it holds the repository's one turn until the gate closes.
func (e *setupEnv) holdTurn() (string, chan struct{}) {
	e.t.Helper()
	gate := make(chan struct{})
	e.repo.mu.Lock()
	e.repo.gate = gate
	e.repo.mu.Unlock()
	details := `[{"type":"package","package":"` + appleHealthPkg + `","version":5}]`
	id := e.park(setupRequest("read:"+healthAccount, details))
	wantStatus(e.t, e.approveWith(id, []string{"read:" + healthAccount}, e.view(id).PlanHash), http.StatusAccepted)
	deadline := time.Now().Add(10 * time.Second)
	for p := e.view(id).Progress; len(p) == 0 || p[0] != setupRunning; p = e.view(id).Progress {
		if time.Now().After(deadline) {
			e.t.Fatal("the set up holding the turn never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return id, gate
}

// A set up that waited for another one runs the rows that landed meanwhile
// as in place, and nothing it was not approved for: a row the owner saw in
// place that needs work again stops it before it writes anything.
func TestASetUpThatWaitedRunsOnlyWhatWasApproved(t *testing.T) {
	inPlace := func(e *setupEnv) {
		e.repo.mu.Lock()
		defer e.repo.mu.Unlock()
		e.repo.statuses[applePkg] = substrate.BundleStatus{ID: applePkg, Installed: true, Enabled: true, Version: 3, ShippedVersion: 3}
	}
	t.Run("what landed meanwhile is not run again", func(t *testing.T) {
		e := newSetupEnv(t)
		inPlace(e)
		holder, gate := e.holdTurn()
		id := e.park(setupRequest(syncScopes, syncDetails))
		v := e.view(id)
		if v.Setup[1].Action != substrate.SetupInstall {
			t.Fatalf("applehealth = %q, want install", v.Setup[1].Action)
		}
		wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusAccepted)
		close(gate)
		if done := e.settle(holder); done.State != substrate.AuthorizationApproved {
			t.Fatalf("the set up holding the turn ended %+v", done.Failure)
		}
		if done := e.settle(id); done.State != substrate.AuthorizationApproved {
			t.Fatalf("the set up that waited ended %+v", done.Failure)
		}
		e.repo.mu.Lock()
		defer e.repo.mu.Unlock()
		if got := strings.Join(e.repo.installs, ","); got != healthPkg+","+appleHealthPkg {
			t.Fatalf("installed %s, want applehealth's closure once", got)
		}
	})
	t.Run("a row that came to need work stops it", func(t *testing.T) {
		e := newSetupEnv(t)
		inPlace(e)
		holder, gate := e.holdTurn()
		id := e.park(setupRequest(syncScopes, syncDetails))
		v := e.view(id)
		if v.Setup[0].Action != substrate.SetupNone {
			t.Fatalf("apple = %q, want in place", v.Setup[0].Action)
		}
		wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusAccepted)
		// The owner removes apple while the set up waits for its turn.
		e.repo.mu.Lock()
		delete(e.repo.statuses, applePkg)
		e.repo.mu.Unlock()
		close(gate)
		e.settle(holder)
		failed := e.settle(id)
		if failed.State != substrate.AuthorizationPending || failed.Failure == nil || failed.Failure.Row != 0 ||
			!strings.Contains(failed.Failure.Message, "changed") || failed.Setup[0].Action != substrate.SetupInstall {
			t.Fatalf("after the plan moved = state %q failure %+v apple %q", failed.State, failed.Failure, failed.Setup[0].Action)
		}
		e.repo.mu.Lock()
		installs := slices.Clone(e.repo.installs)
		e.repo.mu.Unlock()
		if slices.Contains(installs, applePkg) {
			t.Fatalf("installed %v, apple among them", installs)
		}
		ds := e.svc.datasets[fakeRepository]
		ds.mu.Lock()
		defer ds.mu.Unlock()
		if ds.records[syncAccountID] != nil || ds.records[healthWebhook].Properties["enabled"] == true {
			t.Fatal("a set up whose plan moved wrote its account or armed its webhook")
		}
	})
}

// A panic inside a set up fails its request and leaves the server, and the
// repository's turn, to the next one.
func TestASetUpThatPanicsFailsItsRequestAlone(t *testing.T) {
	e := newSetupEnv(t)
	e.repo.mu.Lock()
	e.repo.explode = appleHealthPkg
	e.repo.mu.Unlock()
	details := `[{"type":"package","package":"` + appleHealthPkg + `","version":5}]`
	id := e.park(setupRequest("read:"+healthAccount, details))
	wantStatus(t, e.approveWith(id, []string{"read:" + healthAccount}, e.view(id).PlanHash), http.StatusAccepted)
	failed := e.settle(id)
	if failed.State != substrate.AuthorizationPending || failed.Failure == nil || failed.Failure.Row != setupWholeRow ||
		!strings.Contains(failed.Failure.Message, "server") {
		t.Fatalf("after a panic = state %q failure %+v", failed.State, failed.Failure)
	}
	e.repo.mu.Lock()
	e.repo.explode = ""
	e.repo.mu.Unlock()
	wantStatus(t, e.approveWith(id, []string{"read:" + healthAccount}, failed.PlanHash), http.StatusAccepted)
	if done := e.settle(id); done.State != substrate.AuthorizationApproved {
		t.Fatalf("trying again after a panic ended %+v", done.Failure)
	}
}

// A set up whose request lapsed while it waited for its turn writes nothing:
// nobody can read or answer the request any more.
func TestASetUpStopsWithItsRequest(t *testing.T) {
	e := newSetupEnv(t)
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatal(err)
	}
	h := &handler{svc: e.svc, now: clock, catalog: cat, authz: authorize.NewStore(clock)}
	ds := e.svc.wrap(e.svc.datasets[fakeRepository])
	details, err := authorize.ParseDetails(`[{"type":"package","package":"` + applePkg + `","version":3}]`)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := h.authz.Park(authorize.Request{Details: details})
	if _, err := h.authz.Snapshot(req.ID, fakeRepository, authorize.Document{}); err != nil {
		t.Fatal(err)
	}
	if req, err = h.authz.Reserve(req.ID, fakeRepository, func(authorize.Request) error { return nil }); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rows, _, err := h.planSetup(ctx, ds, details)
	if err != nil {
		t.Fatal(err)
	}
	release, err := h.setupTurn(ctx, fakeRepository)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.runSetup(ctx, ds, substrate.ActorAPI, req, rows, func(authorize.Request) (authorize.Approval, error) {
			return authorize.Approval{Scopes: []string{"read:*"}}, nil
		})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := h.authz.Claim(req.ID, fakeRepository)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Progress) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the set up never reported its rows")
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	now = now.Add(authorize.SetUpTTL)
	mu.Unlock()
	release()
	<-done
	e.repo.mu.Lock()
	defer e.repo.mu.Unlock()
	if len(e.repo.installs) != 0 {
		t.Fatalf("a set up whose request lapsed installed %v", e.repo.installs)
	}
}

func TestAnAccountAlreadyThereIsLeftAlone(t *testing.T) {
	e := newSetupEnv(t)
	details := `[{"type":"package","package":"` + appleHealthPkg + `","version":5},
	{"type":"account","kind":"` + healthAccount + `","id":"` + syncAccountID + `","properties":{"displayName":"new"}}]`
	id := e.park(setupRequest("read:"+healthAccount, details))
	v := e.view(id)
	wantStatus(t, e.approveWith(id, []string{"read:" + healthAccount}, v.PlanHash), http.StatusAccepted)
	e.settle(id)
	ds := e.svc.datasets[fakeRepository]
	ds.mu.Lock()
	ds.records[syncAccountID].Properties["displayName"] = "the owner's"
	now := time.Unix(10, 0).UTC()
	ds.records[syncAccountID].DeletedAt = &now
	ds.mu.Unlock()
	// Deleted is still there: the approval never resurrects it.
	again := e.park(setupRequest("read:"+healthAccount, details))
	if row := e.view(again).Setup[1]; row.Action != substrate.SetupNone {
		t.Fatalf("a deleted account = %+v", row)
	}
}

func TestAnAppAsksWhetherItIsStillSetUp(t *testing.T) {
	e := newSetupEnv(t)
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusAccepted)
	done := e.settle(id)
	tok := e.exchange(tokenForm(codeFrom(t, done.Next)))
	wantStatus(t, tok, http.StatusOK)
	app := decodeJSON[tokenResponse](t, tok).AccessToken

	check := func(bearer, scopes, details string) substrate.AuthorizationCheckResult {
		t.Helper()
		body := `{"scope":` + jsonString(scopes) + `,"authorization_details":` + details + `}`
		rec := e.do(http.MethodPost, "/api/v1/authorizations/check", bearer, []byte(body), "application/json")
		wantStatus(t, rec, http.StatusOK)
		return decodeJSON[substrate.AuthorizationCheckResult](t, rec)
	}
	// Its own token sees every row its scopes name, all in place, and
	// nothing missing.
	got := check(app, syncScopes, syncDetails)
	if len(got.Scopes.Missing) != 0 || got.ExpiresAt == nil {
		t.Fatalf("check = missing %v expires %v", got.Scopes.Missing, got.ExpiresAt)
	}
	for i, row := range got.Setup {
		if row.Action != substrate.SetupNone {
			t.Fatalf("row %d = %+v", i, row)
		}
	}
	if p := got.Setup[0].Package; p.Held != 3 || p.Shipped != 3 || p.Name != "" || p.Kinds != nil {
		t.Fatalf("a package row carries %+v, want versions alone", p)
	}
	if w := got.Setup[3].Webhook; !w.Enabled || !w.Keyed || w.Package != "" {
		t.Fatalf("the webhook row = %+v", w)
	}
	if a := got.Setup[2].Account; a.Properties != nil || a.Label != nil {
		t.Fatalf("the account row = %+v", a)
	}
	// A newer version and another scope: both are asked about.
	newer := strings.Replace(syncDetails, `"version": 5`, `"version": 99`, 1)
	got = check(app, syncScopes+" read:~/notes/*", newer)
	if strings.Join(got.Scopes.Missing, ",") != "read:"+fakeRepository+"/notes/*" || got.Setup[1].Action != substrate.SetupBlocked {
		t.Fatalf("check = missing %v row %+v", got.Scopes.Missing, got.Setup[1])
	}

	// A token that reaches none of it learns nothing of it.
	narrow := e.grant("read:"+personKind, []string{"read:" + personKind})
	got = check(narrow, syncScopes, syncDetails)
	if len(got.Scopes.Missing) != 3 {
		t.Fatalf("a narrow token is missing %v", got.Scopes.Missing)
	}
	for i, row := range got.Setup {
		if row.Action != substrate.SetupUnknown {
			t.Fatalf("row %d = %+v, want unknown", i, row)
		}
	}
	if p := got.Setup[0].Package; p.ID != applePkg || p.Held != 0 || p.Shipped != 0 {
		t.Fatalf("an unknown package row = %+v", p)
	}
	if w := got.Setup[3].Webhook; w.Enabled || w.Keyed {
		t.Fatalf("an unknown webhook row = %+v", w)
	}

	// The owner's token sees everything.
	got = check(e.owner, syncScopes, syncDetails)
	if len(got.Scopes.Missing) != 0 || got.ExpiresAt != nil || slices.ContainsFunc(got.Setup, func(r substrate.AuthorizationSetup) bool {
		return r.Action != substrate.SetupNone
	}) {
		t.Fatalf("the owner's check = %+v", got)
	}

	// A malformed request is the app's to fix.
	for _, body := range []string{
		`{"scope":"everything","authorization_details":[]}`,
		`{"scope":"read:*","authorization_details":[{"type":"webhook","trigger":"t","url":"https://example.com"}]}`,
		`{"scope":"read:*","authorization_details":[],"extra":1}`,
	} {
		wantStatus(t, e.do(http.MethodPost, "/api/v1/authorizations/check", app, []byte(body), "application/json"), http.StatusBadRequest)
	}

	// The check is the app's door, and the approval is not: chi routes
	// each to its own.
	pending := e.park(setupRequest(syncScopes, syncDetails))
	wantStatus(t, e.show(pending), http.StatusOK)
	days := 30
	body, _ := json.Marshal(substrate.AuthorizationApproval{Scopes: resolvedSyncScopes(), LifetimeDays: &days})
	wantStatus(t, e.do(http.MethodPost, "/api/v1/authorizations/"+pending, app, body, "application/json"), http.StatusForbidden)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/authorizations/check", app, nil, ""), http.StatusForbidden)
}

// A check plans a package row the token cannot see no further than its
// action: no upgrade preview is counted against the owner's data for a row
// it answers as unknown.
func TestACheckPlansNoFurtherThanTheTokenSees(t *testing.T) {
	e := newSetupEnv(t)
	e.repo.mu.Lock()
	e.repo.statuses[applePkg] = substrate.BundleStatus{ID: applePkg, Installed: true, Enabled: true, Version: 2, ShippedVersion: 2}
	e.repo.mu.Unlock()
	body := []byte(`{"scope":"read:` + personKind + `","authorization_details":[{"type":"package","package":"` + applePkg + `","version":3}]}`)
	previews := func() int {
		e.repo.mu.Lock()
		defer e.repo.mu.Unlock()
		return e.repo.previews
	}
	narrow := e.grant("read:"+personKind, []string{"read:" + personKind})
	rec := e.do(http.MethodPost, "/api/v1/authorizations/check", narrow, body, "application/json")
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.AuthorizationCheckResult](t, rec); got.Setup[0].Action != substrate.SetupUnknown || previews() != 0 {
		t.Fatalf("a token that cannot see apple = %+v after %d previews", got.Setup[0], previews())
	}
	rec = e.do(http.MethodPost, "/api/v1/authorizations/check", e.owner, body, "application/json")
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.AuthorizationCheckResult](t, rec); got.Setup[0].Action != substrate.SetupUpgrade || previews() != 1 {
		t.Fatalf("the owner's check = %+v after %d previews", got.Setup[0], previews())
	}
}

// Each token asks at its own pace: a loop of checks is answered 429.
func TestACheckIsPaced(t *testing.T) {
	e := newSetupEnv(t)
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatal(err)
	}
	e.h = New(Config{Service: e.svc, ClientDocuments: e.docs, AuthInterval: time.Hour, Catalog: cat})
	body := []byte(`{"scope":"read:*","authorization_details":[]}`)
	for range 2 {
		wantStatus(t, e.do(http.MethodPost, "/api/v1/authorizations/check", e.owner, body, "application/json"), http.StatusOK)
	}
	rec := e.do(http.MethodPost, "/api/v1/authorizations/check", e.owner, body, "application/json")
	wantErrorCode(t, rec, http.StatusTooManyRequests, codeRateLimited)
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("a paced check names no Retry-After")
	}
	wantStatus(t, e.do(http.MethodPost, "/api/v1/authorizations/check", e.svc.token(fakeRepository), body, "application/json"), http.StatusOK)
}

func jsonString(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}
