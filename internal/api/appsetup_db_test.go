package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/geoah/substrate/internal/appsetup"
	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

// appSetupState is what an appSetupEnv's seam holds: the faults a test
// turns on and the record of what reached the repository.
type appSetupState struct {
	mu sync.Mutex
	// gate, when set, holds every install until it is closed; fail refuses
	// the install of the bundles it names, and explode panics in it.
	gate    chan struct{}
	fail    map[string]error
	explode string
	// readErr fails the reads the approval page's view makes, the registry
	// and the tokens; buildErr fails a check's record of the build.
	readErr, buildErr error
	// installs is each bundle an install reached the engine for, in order;
	// previews counts the upgrade previews, and statusReads the reads of
	// every bundle's status, which every plan makes.
	installs    []string
	previews    int
	statusReads int
}

// appSetupSeam is the dataset every request of an appSetupEnv reaches: the
// engine's, with the faults the engine cannot be made to produce on cue (an
// install held mid-way, refused and then admitted, or panicking; a database
// that fails the page's reads or a build's check-in), and a record of what
// reached it, since a preview and a re-install of a closure already held
// leave nothing in the repository to read. A pointer per request, so the
// catalog's preview cache, keyed on the dataset, never answers in place of a
// plan.
type appSetupSeam struct {
	substrate.Dataset
	s *appSetupState
}

func (d *appSetupSeam) InstallBundleClosure(ctx context.Context, actor substrate.Actor, docs []map[string]any, data []substrate.PutInput, opts substrate.BundleInstall) ([]*substrate.Record, error) {
	d.s.mu.Lock()
	gate := d.s.gate
	d.s.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var id string
	for _, doc := range docs {
		if doc["kind"] == vocabulary.CoreKind(vocabulary.DocBundle) {
			md, _ := doc["metadata"].(map[string]any)
			id, _ = md["id"].(string)
		}
	}
	d.s.mu.Lock()
	if err := d.s.fail[id]; err != nil {
		d.s.mu.Unlock()
		return nil, err
	}
	if d.s.explode == id {
		d.s.mu.Unlock()
		panic("the install of " + id + " exploded")
	}
	d.s.installs = append(d.s.installs, id)
	d.s.mu.Unlock()
	return d.Dataset.InstallBundleClosure(ctx, actor, docs, data, opts)
}

func (d *appSetupSeam) PlanBundleUpgrade(ctx context.Context, docs []map[string]any) (substrate.BundleUpgrade, error) {
	d.s.mu.Lock()
	d.s.previews++
	d.s.mu.Unlock()
	return d.Dataset.PlanBundleUpgrade(ctx, docs)
}

func (d *appSetupSeam) BundleStatusesUncounted(ctx context.Context) ([]substrate.BundleStatus, error) {
	d.s.mu.Lock()
	d.s.statusReads++
	d.s.mu.Unlock()
	return d.Dataset.BundleStatusesUncounted(ctx)
}

func (d *appSetupSeam) Kinds(ctx context.Context) ([]substrate.KindInfo, error) {
	d.s.mu.Lock()
	err := d.s.readErr
	d.s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return d.Dataset.Kinds(ctx)
}

func (d *appSetupSeam) Tokens(ctx context.Context) ([]substrate.TokenInfo, error) {
	d.s.mu.Lock()
	err := d.s.readErr
	d.s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return d.Dataset.Tokens(ctx)
}

func (d *appSetupSeam) RecordAppBuild(ctx context.Context, packages map[string]int64, version string) error {
	d.s.mu.Lock()
	err := d.s.buildErr
	d.s.mu.Unlock()
	if err != nil {
		return err
	}
	return d.Dataset.RecordAppBuild(ctx, packages, version)
}

// appSetupEnv is the flow on the engine with the shipped catalog: what an
// approval sets up lands through the catalog's own install door. The shipped
// units are seeded, as at registration: the health catalogue's metrics name
// them.
type appSetupEnv struct {
	*authorizeEnv
	catalog *catalog.Catalog
	seam    *appSetupState
}

func newAppSetupEnv(t *testing.T, opts ...dbOption) *appSetupEnv {
	t.Helper()
	cat, err := catalog.Load(catalog.Shipped()...)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	base := []dbOption{withEngine(engine.WithUnitSeed(true)), withConfig(func(c *Config) { c.Catalog = cat })}
	e := newAuthorizeEnv(t, append(base, opts...)...)
	e.docs.docs[appClientID] = authorize.Document{ClientID: appClientID, RedirectURIs: []string{appRedirect}, Name: "Sync"}
	seam := &appSetupState{fail: map[string]error{}}
	e.db.wrap = func(ds substrate.Dataset) substrate.Dataset { return &appSetupSeam{Dataset: ds, s: seam} }
	return &appSetupEnv{authorizeEnv: e, catalog: cat, seam: seam}
}

// shipped is the version of a package this binary ships, read from the
// catalog: what an install lands and a set up's row reports, which moves with
// every change to the package's declarations, so no test names the number.
func (e *appSetupEnv) shipped(id string) int64 {
	e.t.Helper()
	b, ok := e.catalog.ByID(id)
	if !ok {
		e.t.Fatalf("the catalog ships no %s", id)
	}
	return b.Version
}

func (e *appSetupEnv) view(id string) substrate.AuthorizationRequest {
	e.t.Helper()
	rec := e.show(id)
	wantStatus(e.t, rec, http.StatusOK)
	return decodeJSON[substrate.AuthorizationRequest](e.t, rec)
}

// viewWith reads a request as the page does with only scopes left checked.
func (e *appSetupEnv) viewWith(id string, scopes []string) substrate.AuthorizationRequest {
	e.t.Helper()
	rec := e.do(http.MethodGet, "/api/v1/authorizations/"+id+"?"+url.Values{"scope": scopes}.Encode(), e.owner, nil, "")
	wantStatus(e.t, rec, http.StatusOK)
	return decodeJSON[substrate.AuthorizationRequest](e.t, rec)
}

func (e *appSetupEnv) approveWith(id string, scopes []string, hash string) *httptest.ResponseRecorder {
	days := 30
	body, _ := json.Marshal(substrate.AuthorizationApproval{Scopes: scopes, LifetimeDays: &days, PlanHash: hash})
	return e.do(http.MethodPost, "/api/v1/authorizations/"+id, e.owner, body, "application/json")
}

// settle reads the request until its set up has ended: approved, which the
// read that sees it hands over, or pending again with a failure.
func (e *appSetupEnv) settle(id string) substrate.AuthorizationRequest {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
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

func (e *appSetupEnv) check(bearer string, body []byte) substrate.AuthorizationCheckResult {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/v1/authorizations/check", bearer, body, "application/json")
	wantStatus(e.t, rec, http.StatusOK)
	return decodeJSON[substrate.AuthorizationCheckResult](e.t, rec)
}

// holdTurn starts a set up of applehealth whose install waits at a gate, so
// it holds the repository's one turn until the gate closes.
func (e *appSetupEnv) holdTurn() (string, chan struct{}) {
	e.t.Helper()
	gate := make(chan struct{})
	e.seam.mu.Lock()
	e.seam.gate = gate
	e.seam.mu.Unlock()
	details := `[{"type":"package","package":"` + appleHealthPkg + `","version":5}]`
	id := e.park(setupRequest(appleHealthScopes, details))
	wantStatus(e.t, e.approveWith(id, appleHealthApproval(), e.view(id).PlanHash), http.StatusAccepted)
	deadline := time.Now().Add(30 * time.Second)
	for progress(e.view(id)) != substrate.ProgressRunning {
		if time.Now().After(deadline) {
			e.t.Fatal("the set up holding the turn never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return id, gate
}

// installs is every bundle an install reached the engine for, in order.
func (e *appSetupEnv) installs() []string {
	e.seam.mu.Lock()
	defer e.seam.mu.Unlock()
	return slices.Clone(e.seam.installs)
}

func (e *appSetupEnv) previews() int {
	e.seam.mu.Lock()
	defer e.seam.mu.Unlock()
	return e.seam.previews
}

func (e *appSetupEnv) statusReads() int {
	e.seam.mu.Lock()
	defer e.seam.mu.Unlock()
	return e.seam.statusReads
}

// install installs a shipped package underneath the requests, as the owner
// does from its provider page.
func (e *appSetupEnv) install(id string) {
	e.t.Helper()
	if _, _, err := e.catalog.Install(context.Background(), substrate.ActorPerson, id, e.db.ds); err != nil {
		e.t.Fatalf("install %s: %v", id, err)
	}
}

// uninstall removes an installed package underneath the requests.
func (e *appSetupEnv) uninstall(id string) {
	e.t.Helper()
	if err := e.db.ds.UninstallBundle(context.Background(), id); err != nil {
		e.t.Fatalf("uninstall %s: %v", id, err)
	}
}

// heldAt installs a shipped provider's closure as it shipped at an earlier
// version, so the repository holds it below what the catalog ships now.
func (e *appSetupEnv) heldAt(id string, version int64) {
	e.t.Helper()
	b, ok := e.catalog.ByID(id)
	if !ok {
		e.t.Fatalf("the catalog ships no %s", id)
	}
	authority, _ := vocabulary.SplitPackageRef(id)
	tree := kinds.Providers()
	older := fstest.MapFS{}
	if raw, err := fs.ReadFile(tree, authority+"/authority.yaml"); err == nil {
		older[authority+"/authority.yaml"] = &fstest.MapFile{Data: raw}
	}
	err := fs.WalkDir(tree, id, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(tree, path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, "/bundle.yaml") {
			shipped := "\n  version: " + strconv.FormatInt(b.Version, 10) + "\n"
			if !strings.Contains(string(raw), shipped) {
				return fmt.Errorf("%s declares no %q", path, shipped)
			}
			raw = []byte(strings.Replace(string(raw), shipped, "\n  version: "+strconv.FormatInt(version, 10)+"\n", 1))
		}
		older[path] = &fstest.MapFile{Data: raw}
		return nil
	})
	if err != nil {
		e.t.Fatalf("read %s: %v", id, err)
	}
	cat, err := catalog.Load(catalog.ProviderRoot(older))
	if err != nil || len(cat.Warnings()) > 0 {
		e.t.Fatalf("load %s at version %d: %v %v", id, version, err, cat.Warnings())
	}
	if _, _, err := cat.Install(context.Background(), substrate.ActorPerson, id, e.db.ds); err != nil {
		e.t.Fatalf("install %s at version %d: %v", id, version, err)
	}
}

// appToken mints a token without scopes for the Sync app, as its approval of
// full access does: it sees every row of a check, and a check records its
// build on it.
func (e *appSetupEnv) appToken() string {
	e.t.Helper()
	return e.db.mint(e.t, substrate.MintTokenInput{Label: "notes.example.org", ClientID: appClientID, ClientName: "Sync"})
}

// stranger is a full-access token into a second repository on the same
// engine.
func (e *appSetupEnv) stranger() string {
	e.t.Helper()
	ctx := context.Background()
	other := testdb.RepositoryLabel(e.t) + "2.example.com"
	if _, err := e.db.svc.CreateRepository(ctx, other); err != nil {
		e.t.Fatalf("create the other repository: %v", err)
	}
	ds, err := e.db.svc.Dataset(ctx, other)
	if err != nil {
		e.t.Fatalf("open the other repository: %v", err)
	}
	_, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "stranger"})
	if err != nil {
		e.t.Fatalf("mint into the other repository: %v", err)
	}
	return secret
}

func TestTheApprovalPageShowsWhatApprovingSetsUp(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t)
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
	if apple.Action != substrate.SetupInstall || apple.Package.KeptBy != substrate.KeptByPublisher || apple.Package.Shipped != e.shipped(applePkg) ||
		apple.Package.Held != 0 || apple.Package.Required != 3 || len(apple.Package.Kinds) == 0 || len(apple.Package.Functions) != 0 {
		t.Fatalf("apple row = %+v %+v", apple, apple.Package)
	}
	for _, k := range apple.Package.Kinds {
		if !k.Added || vocabulary.KindPackage(k.Identity) != applePkg {
			t.Fatalf("apple adds %+v", k)
		}
	}
	health := v.Setup[1]
	if health.Action != substrate.SetupInstall || health.Package.Shipped != e.shipped(appleHealthPkg) ||
		len(health.Package.Requires) != 1 || health.Package.Requires[0].ID != healthPkg ||
		health.Package.Requires[0].Action != substrate.SetupInstall || health.Package.Requires[0].Shipped == 0 {
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
	// The requirement's kinds land too, listed as added under the step that
	// brings them, and the row lists its own alone.
	step := health.Package.Requires[0]
	if !slices.ContainsFunc(step.Kinds, func(k substrate.AuthorizationKind) bool { return k.Identity == healthPkg+"/sleep" && k.Added }) {
		t.Fatalf("health adds %+v, want its sleep kind among them", step.Kinds)
	}
	if slices.ContainsFunc(health.Package.Kinds, func(k substrate.AuthorizationKind) bool { return vocabulary.KindPackage(k.Identity) != appleHealthPkg }) {
		t.Fatalf("applehealth's own kinds carry another package's: %+v", health.Package.Kinds)
	}
	if slices.ContainsFunc(health.Package.Functions, func(f substrate.AuthorizationFunction) bool { return vocabulary.KindPackage(f.ID) != appleHealthPkg }) {
		t.Fatalf("applehealth's own functions carry another package's: %+v", health.Package.Functions)
	}
	acct := v.Setup[2]
	if acct.Action != substrate.SetupCreate || acct.Account.Kind != healthAccount || acct.Account.ID != syncAccountID ||
		acct.Account.Properties["displayName"] != "Ada's iPhone" {
		t.Fatalf("account row = %+v %+v", acct, acct.Account)
	}
	hookRow := v.Setup[3]
	if hookRow.Action != substrate.SetupEnable || hookRow.Webhook.Package != appleHealthPkg || hookRow.Webhook.Enabled || hookRow.Webhook.Keyed ||
		hookRow.Webhook.Allowed {
		t.Fatalf("webhook row = %+v %+v", hookRow, hookRow.Webhook)
	}
	for i, row := range v.Setup {
		if row.Blocked != nil || row.Progress != "" {
			t.Fatalf("row %d of a pending request = %+v", i, row)
		}
	}
	// A scope row lists the kinds the set up would add that it reaches,
	// an internal one through its exact scope alone.
	byScope := map[string]substrate.AuthorizationScope{}
	for _, s := range v.Scopes {
		byScope[s.Scope] = s
	}
	accountScope := byScope["write:"+healthAccount]
	if len(accountScope.Kinds) != 1 || !accountScope.Kinds[0].Added || accountScope.Kinds[0].Purpose != vocabulary.PurposeInternal {
		t.Fatalf("the account scope reaches %+v", accountScope.Kinds)
	}
	appleScope := byScope["write:"+applePkg+"/*"]
	if len(appleScope.Kinds) == 0 || slices.ContainsFunc(appleScope.Kinds, func(k substrate.AuthorizationKind) bool {
		return !k.Added || k.Purpose == vocabulary.PurposeInternal
	}) {
		t.Fatalf("the apple glob reaches %+v", appleScope.Kinds)
	}
	// Nothing was approved before, so no scope is folded away.
	if v.Previous != nil || slices.ContainsFunc(v.Scopes, func(s substrate.AuthorizationScope) bool { return s.Allowed }) {
		t.Fatalf("a first approval reads as allowed before: %+v", v.Previous)
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
	t.Parallel()
	e := newAppSetupEnv(t, withSamples("people"))
	repo := e.db.repository
	// A trigger of another package's, already here under the id the app
	// names, and the owner's own package, installed, whose keyed and enabled
	// trigger an app names: nothing on the page says it is the app's, so
	// neither row may pass as already in place.
	function := func(pkg, name string) string {
		return `---
kind: substrate.reamde.dev/core/function
metadata:
  id: ` + repo + `/` + pkg + `/` + name + `
data:
  authority: ` + repo + `
  package: ` + pkg + `
  description: Takes a delivery.
  runtime: python
  timeout: PT5S
  source: |
    def main(input, host):
        return {"output": {}}
`
	}
	own := repo + "/stripe"
	e.db.declare(t, `kind: substrate.reamde.dev/core/package
metadata:
  id: `+repo+`/relay
data:
  authority: `+repo+`
  package: relay
  version: 1
`+function("relay", "forward")+`---
kind: substrate.reamde.dev/core/package
metadata:
  id: `+own+`
data:
  authority: `+repo+`
  package: stripe
  version: 1
---
kind: substrate.reamde.dev/core/bundle
metadata:
  id: `+own+`
data:
  authority: `+repo+`
  package: stripe
  description: Payments.
  installs: [`+own+`/handle]
`+function("stripe", "handle"))
	e.db.put(t, vocabulary.TriggerKind, "notes-webhook", map[string]any{
		"enabled":  false,
		"source":   map[string]any{"webhook": map[string]any{}},
		"callable": map[string]any{"ref": "substrate.reamde.dev/core/function/" + repo + "/relay/forward"},
	})
	e.db.put(t, vocabulary.TriggerKind, "stripe-webhook", map[string]any{
		"enabled":  true,
		"source":   map[string]any{"webhook": map[string]any{"key": "the-owners-key-0123456789abcdefg"}},
		"callable": map[string]any{"ref": "substrate.reamde.dev/core/function/" + own + "/handle"},
	})
	cases := map[string]struct {
		details string
		row     int
		action  string
		reason  string
		fix     string
		problem string
	}{
		"a sample": {
			details: `[{"type":"package","package":"samples.substrate.reamde.dev/tasks","version":1}]`,
			action:  substrate.SetupInstall, reason: substrate.BlockSample, fix: "/data", problem: "sample",
		},
		"a version the server does not ship": {
			details: `[{"type":"package","package":"` + appleHealthPkg + `","version":99}]`,
			action:  substrate.SetupInstall, reason: substrate.BlockServerUpdate,
			problem: fmt.Sprintf("This server ships version %d; the app needs version 99. The server needs an update first.", e.shipped(appleHealthPkg)),
		},
		// A package neither the binary nor an authority document offers
		// is not published, in the catalog's words, which name it.
		"a package outside the catalog": {
			details: `[{"type":"package","package":"example.com/notes","version":1}]`,
			action:  substrate.SetupInstall, reason: substrate.BlockNotPublished, problem: "example.com/notes is not in this server's catalog",
		},
		"an installed package of your own": {
			details: `[{"type":"package","package":"` + own + `","version":1},{"type":"webhook","trigger":"stripe-webhook"}]`,
			action:  substrate.SetupNone, reason: substrate.BlockNotPublished, problem: "~/stripe is not in this server's catalog",
		},
		"an account of a package not requested": {
			details: `[{"type":"package","package":"` + applePkg + `","version":3},
			{"type":"account","kind":"` + healthAccount + `","id":"a"}]`,
			row: 1, action: substrate.SetupCreate, reason: substrate.BlockNotRequested, problem: "does not set up",
		},
		"an account kind that is no account": {
			details: `[{"type":"package","package":"` + applePkg + `","version":3},
			{"type":"account","kind":"` + applePkg + `/contact","id":"a"}]`,
			row: 1, action: substrate.SetupCreate, reason: substrate.BlockUnsupported, problem: "not an account kind",
		},
		"a secret property": {
			details: `[{"type":"package","package":"` + appleHealthPkg + `","version":5},
			{"type":"account","kind":"` + healthAccount + `","id":"a","properties":{"tokenRef":"x"}}]`,
			row: 1, action: substrate.SetupCreate, reason: substrate.BlockUnsupported, problem: "secret",
		},
		"another hand's property": {
			details: `[{"type":"package","package":"` + appleHealthPkg + `","version":5},
			{"type":"account","kind":"` + healthAccount + `","id":"a","properties":{"tokenStatus":"x"}}]`,
			row: 1, action: substrate.SetupCreate, reason: substrate.BlockUnsupported, problem: "the provider's sign-in",
		},
		"an undeclared property": {
			details: `[{"type":"package","package":"` + appleHealthPkg + `","version":5},
			{"type":"account","kind":"` + healthAccount + `","id":"a","properties":{"nickname":"x"}}]`,
			row: 1, action: substrate.SetupCreate, reason: substrate.BlockUnsupported, problem: "no nickname property",
		},
		"a webhook another package's callable takes": {
			details: `[{"type":"package","package":"` + applePkg + `","version":3},{"type":"webhook","trigger":"notes-webhook"}]`,
			row:     1, action: substrate.SetupEnable, reason: substrate.BlockNotRequested, problem: "code from a package this request does not set up",
		},
		"a webhook of a package not requested": {
			details: `[{"type":"package","package":"` + applePkg + `","version":3},{"type":"webhook","trigger":"` + healthWebhook + `"}]`,
			row:     1, action: substrate.SetupEnable, reason: substrate.BlockNotRequested, problem: "no such trigger",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			id := e.park(setupRequest("read:"+personKind, c.details))
			v := e.view(id)
			row := v.Setup[c.row]
			if row.Action != c.action || row.Blocked == nil || row.Blocked.Reason != c.reason || row.Blocked.Fix != c.fix ||
				!strings.Contains(row.Blocked.Problem, c.problem) {
				t.Fatalf("row %d = %+v %+v, want %s blocked for %s with %q and fix %q", c.row, row, row.Blocked, c.action, c.reason, c.problem, c.fix)
			}
			// The owner reads it as a sentence, and the technical view
			// shows the ids beside it; the catalog's own words, on a package
			// it could not find, name what they looked for.
			if c.reason != substrate.BlockNotPublished && !regexp.MustCompile(`^[A-Z][^:/]*\.$`).MatchString(row.Blocked.Problem) {
				t.Fatalf("problem %q is not a sentence free of ids", row.Blocked.Problem)
			}
			// Blocked keeps the denial and refuses the approval.
			rec := e.approveWith(id, []string{"read:" + personKind}, v.PlanHash)
			wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
			if got := e.view(id); got.State != substrate.AuthorizationPending {
				t.Fatalf("a refused approval left the request %q", got.State)
			}
		})
	}
	for _, tok := range e.tokens() {
		if slices.ContainsFunc(tok.Scopes, func(s string) bool { return strings.HasPrefix(s, "webhook:") }) {
			t.Fatalf("a blocked set up minted %v", tok.Scopes)
		}
	}
	if got := e.installs(); len(got) != 0 {
		t.Fatalf("a blocked set up installed %v", got)
	}
}

func TestApprovingRunsTheSetUpAndTheTokenCarriesIt(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t)
	// The webhook's address is keyed by the repository's id (decision 0261).
	repo := e.db.ds.Repository().ID
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	// The approval names the plan it read.
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), ""), http.StatusUnprocessableEntity)
	rec := e.approveWith(id, resolvedSyncScopes(), v.PlanHash)
	wantStatus(t, rec, http.StatusAccepted)
	// The answer carries the rows the set up runs, each waiting, so the page
	// shows them before its first read.
	if a := decodeJSON[substrate.AuthorizationAnswer](t, rec); a.State != substrate.AuthorizationSettingUp || a.Redirect != "" ||
		len(a.Setup) != 4 || a.Setup[0].Progress != substrate.ProgressWaiting || a.Setup[1].Package.ID != appleHealthPkg {
		t.Fatalf("answer = %+v", a)
	}
	done := e.settle(id)
	if done.State != substrate.AuthorizationApproved || !strings.HasPrefix(done.Next, appRedirect+"?") ||
		progress(done) != "done,done,done,done" {
		t.Fatalf("after the set up = state %q next %q progress %v failure %+v", done.State, done.Next, progress(done), done.Failure)
	}
	// Answered once: the read that handed the code over was the last.
	wantStatus(t, e.show(id), http.StatusNotFound)
	// The requirement landed before what requires it.
	if installs := strings.Join(e.installs(), ","); installs != applePkg+","+healthPkg+","+appleHealthPkg {
		t.Fatalf("installed %s", installs)
	}
	for _, pkg := range []string{applePkg, healthPkg, appleHealthPkg} {
		if st, err := e.db.ds.BundleStatus(context.Background(), pkg); err != nil || !st.Installed {
			t.Fatalf("%s after the set up = %+v, %v", pkg, st, err)
		}
	}
	account, trigger := e.db.get(t, healthAccount, syncAccountID), e.db.get(t, vocabulary.TriggerKind, healthWebhook)
	if account == nil || account.Kind != healthAccount || account.Properties["displayName"] != "Ada's iPhone" {
		t.Fatalf("account = %+v", account)
	}
	key := vocabulary.TriggerKey(trigger.Properties)
	if trigger.Properties["enabled"] != true || !regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`).MatchString(key) ||
		vocabulary.ReferencePath(trigger.Properties["callable"]) == "" {
		t.Fatalf("trigger = %+v", trigger.Properties)
	}
	// Nothing answers it again.
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodDelete, "/api/v1/authorizations/"+id, e.owner, nil, ""), http.StatusNotFound)

	tok := e.exchange(tokenForm(codeFrom(t, done.Next)))
	wantStatus(t, tok, http.StatusOK)
	res := decodeJSON[tokenResponse](t, tok)
	if !strings.Contains(" "+res.Scope+" ", " webhook:"+healthWebhook+" ") {
		t.Fatalf("token scope = %q, want the webhook's", res.Scope)
	}
	if minted := e.token(res.AccessToken); !slices.Contains(minted.Scopes, "webhook:"+healthWebhook) {
		t.Fatalf("the minted token holds %v, want the webhook's scope", minted.Scopes)
	}
	got := res.AuthorizationDetails
	if len(got) != 4 || got[0].Version != e.shipped(applePkg) || got[1].Version != e.shipped(appleHealthPkg) || got[2].Kind != healthAccount || got[2].ID != syncAccountID ||
		got[2].Properties != nil || got[3].URL != "https://substrate.example.com/webhooks/"+repo+"/"+healthWebhook {
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
	if v.Setup[0].Package.Held != e.shipped(applePkg) || v.Setup[3].Webhook.Keyed != true {
		t.Fatalf("rows after the set up = %+v %+v", v.Setup[0].Package, v.Setup[3].Webhook)
	}
	if v.Previous == nil || !slices.Contains(v.Previous.Scopes, "webhook:"+healthWebhook) {
		t.Fatalf("previous = %+v", v.Previous)
	}
	for _, s := range v.Scopes {
		if !s.Allowed {
			t.Fatalf("scope %s, approved before, is not folded as allowed", s.Scope)
		}
	}
	// The webhook the previous token delivers to says so on its row, and the
	// digest the approval names is the plan's, whatever the row says.
	if !v.Setup[3].Webhook.Allowed {
		t.Fatalf("the webhook row, delivered to before, = %+v", v.Setup[3].Webhook)
	}
	if rows, _, err := appsetup.Plan(context.Background(), e.db.ds, e.catalog, v.Details, appsetup.Options{}); err != nil || rows[3].Webhook.Allowed {
		t.Fatalf("the plan itself = %+v, %v", rows[3].Webhook, err)
	}
	rec = e.approveWith(again, resolvedSyncScopes(), v.PlanHash)
	wantStatus(t, rec, http.StatusOK)
	tok = e.exchange(tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)))
	wantStatus(t, tok, http.StatusOK)
	if res := decodeJSON[tokenResponse](t, tok); !strings.Contains(res.Scope, "webhook:"+healthWebhook) || len(res.AuthorizationDetails) != 4 {
		t.Fatalf("a re-approval's token = %+v", res)
	}
	// Turning it on again never replaced the key.
	if still := vocabulary.TriggerKey(e.db.get(t, vocabulary.TriggerKind, healthWebhook).Properties); still != key {
		t.Fatal("the key changed")
	}
}

func TestAnApprovalOfAPlanThatMovedIsRefused(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t)
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	wantErrorCode(t, e.approveWith(id, resolvedSyncScopes(), "0000"), http.StatusConflict, codeConflict)
	// apple lands from the provider page meanwhile: the plan the page
	// showed is not the one approving would run.
	e.install(applePkg)
	wantErrorCode(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusConflict, codeConflict)
	moved := e.view(id)
	if moved.State != substrate.AuthorizationPending || moved.Setup[0].Action != substrate.SetupNone || moved.PlanHash == v.PlanHash {
		t.Fatalf("after the refusals = state %q apple %q", moved.State, moved.Setup[0].Action)
	}
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), moved.PlanHash), http.StatusAccepted)
	if done := e.settle(id); done.State != substrate.AuthorizationApproved {
		t.Fatalf("the set up ended %+v", done.Failure)
	}
	if installs := e.installs(); slices.Contains(installs, applePkg) {
		t.Fatalf("a package already in place was installed again: %v", installs)
	}
}

func TestNothingElseAnswersARequestWhileItIsSettingUp(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t)
	stranger := e.stranger()
	gate := make(chan struct{})
	e.seam.mu.Lock()
	e.seam.gate = gate
	e.seam.mu.Unlock()
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	days := 30
	body, _ := json.Marshal(substrate.AuthorizationApproval{Scopes: resolvedSyncScopes(), LifetimeDays: &days, PlanHash: v.PlanHash})
	// Another repository is refused before anything runs.
	wantStatus(t, e.do(http.MethodPost, "/api/v1/authorizations/"+id, stranger, body, "application/json"), http.StatusNotFound)
	if got := e.installs(); got != nil {
		t.Fatalf("another repository's approval ran a set up: %v", got)
	}
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusAccepted)
	// The first install is held at the gate: the page reads it running.
	deadline := time.Now().Add(30 * time.Second)
	for {
		running := e.view(id)
		if running.State != substrate.AuthorizationSettingUp {
			t.Fatalf("while it runs = state %q", running.State)
		}
		if progress(running) == "running,waiting,waiting,waiting" && running.PlanHash == v.PlanHash {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the set up never reported its first row running: %v", progress(running))
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
	t.Parallel()
	e := newAppSetupEnv(t)
	e.seam.mu.Lock()
	e.seam.fail[appleHealthPkg] = fmt.Errorf("%w: the closure does not admit", substrate.ErrValidation)
	e.seam.mu.Unlock()
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusAccepted)
	failed := e.settle(id)
	if failed.State != substrate.AuthorizationPending || failed.Failure == nil || failed.Failure.Row == nil || *failed.Failure.Row != 1 ||
		!strings.Contains(failed.Failure.Message, "does not admit") || progress(failed) != "done,failed,," {
		t.Fatalf("after a failure = %+v progress %q", failed.Failure, progress(failed))
	}
	// The page reads the rows the approval was checked against, with what
	// landed, and names their hash to try again.
	if failed.PlanHash != v.PlanHash || failed.Setup[0].Action != substrate.SetupInstall {
		t.Fatalf("rows after a failure = hash %q, apple %q", failed.PlanHash, failed.Setup[0].Action)
	}
	e.seam.mu.Lock()
	delete(e.seam.fail, appleHealthPkg)
	e.seam.mu.Unlock()
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), failed.PlanHash), http.StatusAccepted)
	done := e.settle(id)
	if done.State != substrate.AuthorizationApproved || done.Failure != nil || progress(done) != "done,done,done,done" {
		t.Fatalf("trying again = state %q failure %+v progress %q", done.State, done.Failure, progress(done))
	}
	installs := e.installs()
	if n := len(slices.DeleteFunc(slices.Clone(installs), func(s string) bool { return s != applePkg })); n != 1 {
		t.Fatalf("apple was installed %d times: %v", n, installs)
	}
}

// A request whose set up stopped shows the rows it ran; once what approving
// sets up moved from those, trying them again is refused and the page plans
// the request afresh.
func TestTryingAgainAfterThePlanMovedIsRefusedAndPlannedAfresh(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t)
	e.install(applePkg)
	e.seam.mu.Lock()
	e.seam.fail[appleHealthPkg] = fmt.Errorf("%w: the closure does not admit", substrate.ErrValidation)
	e.seam.mu.Unlock()
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusAccepted)
	failed := e.settle(id)
	if failed.Failure == nil || failed.PlanHash != v.PlanHash || failed.Setup[0].Action != substrate.SetupNone {
		t.Fatalf("after a failure = %+v hash %q", failed.Failure, failed.PlanHash)
	}
	// The owner removes apple, which the approval showed in place.
	e.uninstall(applePkg)
	e.seam.mu.Lock()
	delete(e.seam.fail, appleHealthPkg)
	e.seam.mu.Unlock()
	wantErrorCode(t, e.approveWith(id, resolvedSyncScopes(), failed.PlanHash), http.StatusConflict, codeConflict)
	fresh := e.view(id)
	if fresh.State != substrate.AuthorizationPending || fresh.PlanHash == failed.PlanHash || fresh.Setup[0].Action != substrate.SetupInstall ||
		progress(fresh) != ",,," || fresh.Failure == nil {
		t.Fatalf("after the refusal = state %q hash %q apple %q progress %q failure %+v",
			fresh.State, fresh.PlanHash, fresh.Setup[0].Action, progress(fresh), fresh.Failure)
	}
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), fresh.PlanHash), http.StatusAccepted)
	if done := e.settle(id); done.State != substrate.AuthorizationApproved {
		t.Fatalf("approving the fresh plan ended %+v", done.Failure)
	}
}

// A set up that waited for another one runs the rows that landed meanwhile
// as in place, and nothing it was not approved for: a row the owner saw in
// place that needs work again stops it before it writes anything.
func TestASetUpThatWaitedRunsOnlyWhatWasApproved(t *testing.T) {
	t.Parallel()
	t.Run("what landed meanwhile is not run again", func(t *testing.T) {
		t.Parallel()
		e := newAppSetupEnv(t)
		e.install(applePkg)
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
		if got := strings.Join(e.installs(), ","); got != healthPkg+","+appleHealthPkg {
			t.Fatalf("installed %s, want applehealth's closure once", got)
		}
	})
	t.Run("a row that came to need work stops it", func(t *testing.T) {
		t.Parallel()
		e := newAppSetupEnv(t)
		e.install(applePkg)
		holder, gate := e.holdTurn()
		id := e.park(setupRequest(syncScopes, syncDetails))
		v := e.view(id)
		if v.Setup[0].Action != substrate.SetupNone {
			t.Fatalf("apple = %q, want in place", v.Setup[0].Action)
		}
		wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusAccepted)
		// The owner removes apple while the set up waits for its turn.
		e.uninstall(applePkg)
		close(gate)
		e.settle(holder)
		failed := e.settle(id)
		if failed.State != substrate.AuthorizationPending || failed.Failure == nil || failed.Failure.Row == nil || *failed.Failure.Row != 0 ||
			!strings.Contains(failed.Failure.Message, "changed") || failed.Setup[0].Action != substrate.SetupInstall {
			t.Fatalf("after the plan moved = state %q failure %+v apple %q", failed.State, failed.Failure, failed.Setup[0].Action)
		}
		// The page plans it again: what approving does now, under a new
		// hash, with no progress of the run that stopped.
		if failed.PlanHash == v.PlanHash || progress(failed) != ",,," {
			t.Fatalf("after the plan moved = hash %q progress %q", failed.PlanHash, progress(failed))
		}
		if installs := e.installs(); slices.Contains(installs, applePkg) {
			t.Fatalf("installed %v, apple among them", installs)
		}
		hook := e.db.get(t, vocabulary.TriggerKind, healthWebhook)
		if e.db.get(t, healthAccount, syncAccountID) != nil || hook == nil || hook.Properties["enabled"] == true {
			t.Fatalf("a set up whose plan moved wrote its account or turned its webhook on: %+v", hook)
		}
	})
}

// A panic inside a set up fails its request and leaves the server, and the
// repository's turn, to the next one.
func TestASetUpThatPanicsFailsItsRequestAlone(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t)
	e.seam.mu.Lock()
	e.seam.explode = appleHealthPkg
	e.seam.mu.Unlock()
	details := `[{"type":"package","package":"` + appleHealthPkg + `","version":5}]`
	id := e.park(setupRequest(appleHealthScopes, details))
	wantStatus(t, e.approveWith(id, appleHealthApproval(), e.view(id).PlanHash), http.StatusAccepted)
	failed := e.settle(id)
	if failed.State != substrate.AuthorizationPending || failed.Failure == nil || failed.Failure.Row != nil ||
		!strings.Contains(failed.Failure.Message, "server") || progress(failed) != substrate.ProgressFailed {
		t.Fatalf("after a panic = state %q failure %+v progress %q", failed.State, failed.Failure, progress(failed))
	}
	e.seam.mu.Lock()
	e.seam.explode = ""
	e.seam.mu.Unlock()
	wantStatus(t, e.approveWith(id, appleHealthApproval(), failed.PlanHash), http.StatusAccepted)
	if done := e.settle(id); done.State != substrate.AuthorizationApproved {
		t.Fatalf("trying again after a panic ended %+v", done.Failure)
	}
}

// A set up whose request lapsed while it waited for its turn writes nothing:
// nobody can read or answer the request any more.
func TestASetUpStopsWithItsRequest(t *testing.T) {
	t.Parallel()
	ended := make(chan string, 2)
	e := newAppSetupEnv(t, withConfig(func(c *Config) { c.setUpEnded = func(id string) { ended <- id } }))
	holder, gate := e.holdTurn()
	details := `[{"type":"package","package":"` + applePkg + `","version":3}]`
	id := e.park(setupRequest(appleHealthScopes, details))
	wantStatus(t, e.approveWith(id, appleHealthApproval(), e.view(id).PlanHash), http.StatusAccepted)
	e.clock.advance(authorize.SetUpTTL)
	close(gate)
	// Both lapsed: the one holding the turn finishes its install, and the
	// one waiting for it installs nothing.
	for _, run := range waitEnded(t, ended, 2) {
		if run != holder && run != id {
			t.Fatalf("a set up of %s ended", run)
		}
	}
	if installs := e.installs(); !slices.Contains(installs, appleHealthPkg) || slices.Contains(installs, applePkg) {
		t.Fatalf("installed %v, want what the set up holding the turn ran alone", installs)
	}
	if st, err := e.db.ds.BundleStatus(context.Background(), applePkg); err == nil && st.Installed {
		t.Fatal("the lapsed set up installed apple")
	}
	for _, req := range []string{holder, id} {
		wantStatus(t, e.show(req), http.StatusNotFound)
	}
}

// An approved request is answered as its set up left it: the read that hands
// the code over reads nothing of the repository, so a repository whose reads
// fail by then still lets the page take the code back to the app.
func TestAnApprovedRequestIsHandedOverWithoutReadingTheRepository(t *testing.T) {
	t.Parallel()
	ended := make(chan string, 1)
	e := newAppSetupEnv(t, withConfig(func(c *Config) { c.setUpEnded = func(id string) { ended <- id } }))
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusAccepted)
	waitEnded(t, ended, 1)
	e.seam.mu.Lock()
	e.seam.readErr = fmt.Errorf("%w: the database is away", substrate.ErrUnavailable)
	e.seam.mu.Unlock()
	// The reads do fail: a pending request's page cannot be built.
	wantStatus(t, e.show(e.park(setupRequest(syncScopes, syncDetails))), http.StatusServiceUnavailable)
	reads := e.statusReads()

	done := e.view(id)
	if done.State != substrate.AuthorizationApproved || !strings.HasPrefix(done.Next, appRedirect+"?") || progress(done) != "done,done,done,done" ||
		len(done.Scopes) != len(resolvedSyncScopes()) || done.PlanHash != v.PlanHash || done.App.Name != "Sync" || done.Failure != nil {
		t.Fatalf("the approved request = state %q next %q progress %q, %d scopes, hash %q", done.State, done.Next, progress(done), len(done.Scopes), done.PlanHash)
	}
	if planned := e.statusReads() - reads; planned != 0 {
		t.Fatalf("handing the approved request over planned it %d times", planned)
	}
	wantStatus(t, e.show(id), http.StatusNotFound)
	wantStatus(t, e.exchange(tokenForm(codeFrom(t, done.Next))), http.StatusOK)
}

func TestAnAccountAlreadyThereIsLeftAlone(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t)
	details := `[{"type":"package","package":"` + appleHealthPkg + `","version":5},
	{"type":"account","kind":"` + healthAccount + `","id":"` + syncAccountID + `","properties":{"displayName":"new"}}]`
	id := e.park(setupRequest(appleHealthScopes, details))
	v := e.view(id)
	wantStatus(t, e.approveWith(id, appleHealthApproval(), v.PlanHash), http.StatusAccepted)
	e.settle(id)
	accountPath := "/api/v1/" + healthAccount + "/" + syncAccountID
	wantStatus(t, e.do(http.MethodPatch, accountPath, e.owner, []byte(`{"properties":{"displayName":"the owner's"}}`), "application/json"), http.StatusOK)
	live := e.park(setupRequest(appleHealthScopes, details))
	if row := e.view(live).Setup[1]; row.Action != substrate.SetupNone || row.Blocked != nil {
		t.Fatalf("a live account = %+v", row)
	}
	// Deleted and not collected yet: never brought back, and the page says
	// why approving cannot create it, until the deletion is collected.
	wantStatus(t, e.do(http.MethodDelete, accountPath, e.owner, nil, ""), http.StatusOK)
	again := e.park(setupRequest(appleHealthScopes, details))
	v = e.view(again)
	row := v.Setup[1]
	if row.Action != substrate.SetupCreate || row.Blocked == nil || row.Blocked.Reason != substrate.BlockAccountDeleted || row.Blocked.Fix != "" {
		t.Fatalf("a deleted account = %+v %+v", row, row.Blocked)
	}
	wantErrorCode(t, e.approveWith(again, appleHealthApproval(), v.PlanHash), http.StatusUnprocessableEntity, codeValidation)
	if got := e.db.get(t, healthAccount, syncAccountID); got == nil || got.DeletedAt == nil || got.Properties["displayName"] != "the owner's" {
		t.Fatalf("the refused approval brought the account back: %+v", got)
	}
	wantStatus(t, e.do(http.MethodDelete, accountPath+"?purge=true", e.owner, nil, ""), http.StatusOK)
	if row := e.view(again).Setup[1]; row.Action != substrate.SetupCreate || row.Blocked != nil {
		t.Fatalf("an account whose deletion was collected = %+v", row)
	}
}

func TestAnAppAsksWhetherItIsStillSetUp(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t, withSamples("people"))
	id := e.park(setupRequest(syncScopes, syncDetails))
	v := e.view(id)
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), v.PlanHash), http.StatusAccepted)
	done := e.settle(id)
	tok := e.exchange(tokenForm(codeFrom(t, done.Next)))
	wantStatus(t, tok, http.StatusOK)
	app := decodeJSON[tokenResponse](t, tok).AccessToken

	// Its own token sees every row its scopes name, all in place, and
	// nothing missing.
	got := e.check(app, checkRequest(syncScopes, syncDetails))
	if len(got.Scopes.Missing) != 0 || got.ExpiresAt == nil {
		t.Fatalf("check = missing %v expires %v", got.Scopes.Missing, got.ExpiresAt)
	}
	for i, row := range got.Setup {
		if row.Action != substrate.SetupNone || row.Blocked != nil || row.Progress != "" {
			t.Fatalf("row %d = %+v", i, row)
		}
	}
	if p := got.Setup[0].Package; p.Held != e.shipped(applePkg) || p.Shipped != p.Held || p.Kinds != nil || p.Requires != nil || p.KeptBy != "" {
		t.Fatalf("a package row carries %+v, want versions alone", p)
	}
	if w := got.Setup[3].Webhook; w.Enabled || w.Keyed || w.Package != appleHealthPkg {
		t.Fatalf("the webhook row = %+v, want its trigger and package alone", w)
	}
	if a := got.Setup[2].Account; a.Properties != nil || a.Label != nil {
		t.Fatalf("the account row = %+v", a)
	}
	// A newer version and another scope: both are asked about, the row with
	// why it cannot run, which the app shows the owner.
	newer := strings.Replace(syncDetails, `"version": 5`, `"version": 99`, 1)
	got = e.check(app, checkRequest(syncScopes+" read:~/notes/*", newer))
	blocked := got.Setup[1]
	if strings.Join(got.Scopes.Missing, ",") != "read:~/notes/*" || blocked.Action != substrate.SetupUpgrade ||
		blocked.Blocked == nil || blocked.Blocked.Reason != substrate.BlockServerUpdate || blocked.Blocked.Problem == "" {
		t.Fatalf("check = missing %v row %+v %+v", got.Scopes.Missing, blocked, blocked.Blocked)
	}
	// A scope the token covers under another spelling is not missing: write
	// covers read, and a package glob covers a kind in it.
	got = e.check(app, checkRequest("read:"+applePkg+"/contact read:~/notes/*", "[]"))
	if strings.Join(got.Scopes.Missing, ",") != "read:~/notes/*" || len(got.Setup) != 0 {
		t.Fatalf("a covered scope = missing %v, rows %+v", got.Scopes.Missing, got.Setup)
	}
	// A check that names no details plans none.
	if got := e.check(app, []byte(`{"scope":"read:*"}`)); len(got.Setup) != 0 {
		t.Fatalf("a check without details = %+v", got)
	}

	// A token that reaches none of it learns nothing of it.
	narrow := e.grant("read:"+personKind, []string{"read:" + personKind})
	got = e.check(narrow, checkRequest(syncScopes, newer))
	if len(got.Scopes.Missing) != len(resolvedSyncScopes()) {
		t.Fatalf("a narrow token is missing %v", got.Scopes.Missing)
	}
	for i, row := range got.Setup {
		if row.Action != substrate.SetupUnknown || row.Blocked != nil {
			t.Fatalf("row %d = %+v, want unknown", i, row)
		}
	}
	if p := got.Setup[1].Package; p.ID != appleHealthPkg || p.Required != 99 || p.Held != 0 || p.Shipped != 0 {
		t.Fatalf("an unknown package row = %+v", p)
	}
	if w := got.Setup[3].Webhook; w.Trigger != healthWebhook || w.Package != "" {
		t.Fatalf("an unknown webhook row = %+v", w)
	}
	raw := e.do(http.MethodPost, "/api/v1/authorizations/check", narrow, checkRequest(syncScopes, newer), "application/json").Body.String()
	for _, absent := range []string{`"held"`, `"shipped"`, `"blocked"`, `"enabled"`, `"keyed"`, `"allowed"`} {
		if strings.Contains(raw, absent) {
			t.Fatalf("an unknown row answers %s: %s", absent, raw)
		}
	}

	// The owner's token sees everything.
	got = e.check(e.owner, checkRequest(syncScopes, syncDetails))
	if len(got.Scopes.Missing) != 0 || got.ExpiresAt != nil || slices.ContainsFunc(got.Setup, func(r substrate.AuthorizationSetup) bool {
		return r.Action != substrate.SetupNone
	}) {
		t.Fatalf("the owner's check = %+v", got)
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

// An owner's own token may ask too, and sees every row (docs/apps.md,
// "Check that it is still set up"): every row in place, nothing missing,
// no end, and a check that names no details answers no rows.
func TestAnOwnersCheckSeesEveryRow(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t)
	id := e.park(setupRequest(syncScopes, syncDetails))
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), e.view(id).PlanHash), http.StatusAccepted)
	if done := e.settle(id); done.State != substrate.AuthorizationApproved {
		t.Fatalf("the set up ended %+v", done.Failure)
	}
	got := e.check(e.owner, checkRequest(syncScopes, syncDetails))
	if len(got.Scopes.Missing) != 0 || got.ExpiresAt != nil || slices.ContainsFunc(got.Setup, func(r substrate.AuthorizationSetup) bool {
		return r.Action != substrate.SetupNone
	}) {
		t.Fatalf("the owner's check = %+v", got)
	}
	if got := e.check(e.owner, []byte(`{"scope":"read:*"}`)); len(got.Setup) != 0 {
		t.Fatalf("a check without details = %+v", got)
	}
}

// A full-access app's token revoked after its check signed in is refused, as
// recording its build refuses it, and never answered as an owner's: the
// revocation lands between the sign-in and the check's own read of the
// tokens.
func TestACheckByAnAppRevokedOnTheWayIsRefused(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t)
	ctx := context.Background()
	info, secret, err := e.db.ds.MintToken(ctx, substrate.MintTokenInput{Label: "notes.example.org", ClientID: appClientID, ClientName: "Sync"})
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	seam := e.db.wrap
	e.db.wrap = func(ds substrate.Dataset) substrate.Dataset {
		return revokeBeforeTokens{Dataset: seam(ds), revoke: func() {
			once.Do(func() {
				if _, err := e.db.ds.Delete(ctx, substrate.ActorPerson, tokenType, info.ID, substrate.DeleteInput{}); err != nil {
					t.Errorf("revoke: %v", err)
				}
			})
		}}
	}
	rec := e.do(http.MethodPost, "/api/v1/authorizations/check", secret, checkRequest(syncScopes, syncDetails), "application/json")
	wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
}

// revokeBeforeTokens revokes the request's token before its read of the
// tokens.
type revokeBeforeTokens struct {
	substrate.Dataset
	revoke func()
}

func (r revokeBeforeTokens) Tokens(ctx context.Context) ([]substrate.TokenInfo, error) {
	r.revoke()
	return r.Dataset.Tokens(ctx)
}

// A check plans a package row the token cannot see no further than its
// action: no upgrade preview is counted against the owner's data for a row
// it answers as unknown.
func TestACheckPlansNoFurtherThanTheTokenSees(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t, withSamples("people"))
	e.heldAt(applePkg, 2)
	body := checkRequest("read:"+personKind, `[{"type":"package","package":"`+applePkg+`","version":3}]`)
	narrow := e.grant("read:"+personKind, []string{"read:" + personKind})
	if got := e.check(narrow, body); got.Setup[0].Action != substrate.SetupUnknown || e.previews() != 0 {
		t.Fatalf("a token that cannot see apple = %+v after %d previews", got.Setup[0], e.previews())
	}
	if got := e.check(e.owner, body); got.Setup[0].Action != substrate.SetupUpgrade || e.previews() != 1 {
		t.Fatalf("the owner's check = %+v after %d previews", got.Setup[0], e.previews())
	}
}

// A token asking again within the memo's window is answered the last answer
// for the same body, planned once, and never refused for asking.
func TestACheckIsAnsweredAgainNeverRefused(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t, withConfig(func(c *Config) { c.AuthInterval = time.Hour }))
	e.heldAt(applePkg, 2)
	body := checkRequest("read:*", `[{"type":"package","package":"`+applePkg+`","version":3}]`)
	for range 5 {
		if got := e.check(e.owner, body); got.Setup[0].Action != substrate.SetupUpgrade {
			t.Fatalf("a check asked again = %+v", got.Setup[0])
		}
	}
	if e.previews() != 1 {
		t.Fatalf("five checks planned %d previews, want one", e.previews())
	}
	// Another body, another token, or the window gone: planned afresh.
	e.check(e.owner, checkRequest("read:*", `[{"type":"package","package":"`+applePkg+`","version":2}]`))
	e.check(e.db.mint(t, substrate.MintTokenInput{Label: "another"}), body)
	e.clock.advance(checkMemoTTL)
	e.check(e.owner, body)
	if e.previews() != 3 {
		t.Fatalf("previews = %d, want a fresh plan for the other token and after the window", e.previews())
	}
}

// Installed code reaches no further than the approval (decision 0215): the
// page plans against the scopes left checked, so unchecking one the
// applehealth package's code needs blocks its row, naming each uncovered
// reach and the scope that covers it, and checking it back unblocks it. The
// approval plans against the scopes it names, so the hash the page read
// with those scopes is the plan the approval is held to, and an approval
// that names fewer is refused. A token's check is held to the token's own
// scopes.
func TestUncheckingAScopeBlocksTheCodeItCoversAndCheckingItBackUnblocks(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t)
	id := e.park(setupRequest(syncScopes, syncDetails))
	all := e.view(id)
	if all.Setup[1].Blocked != nil {
		t.Fatalf("every scope checked, applehealth = %+v", all.Setup[1].Blocked)
	}
	reach := all.Setup[1].Package.Reach
	if len(reach) == 0 || slices.ContainsFunc(reach, func(r substrate.AuthorizationReach) bool { return !r.Covered || r.Scope == "" || len(r.By) == 0 }) {
		t.Fatalf("applehealth's reach = %+v, want every entry covered with its scope and what reaches it", reach)
	}
	sleep := slices.IndexFunc(reach, func(r substrate.AuthorizationReach) bool { return r.Pattern == healthPkg+"/sleep" && r.Write })
	if sleep < 0 || reach[sleep].Scope != "write:"+healthPkg+"/sleep" || reach[sleep].By[0].Type != vocabulary.DocFunction || reach[sleep].By[0].ID != appleHealthPkg+"/adaptapplehealth" {
		t.Fatalf("the sleep write = %+v", reach)
	}
	adapter := slices.IndexFunc(all.Setup[1].Package.Functions, func(f substrate.AuthorizationFunction) bool { return f.ID == appleHealthPkg+"/adaptapplehealth" })
	if adapter < 0 || !slices.Contains(all.Setup[1].Package.Functions[adapter].Reads, healthPkg+"/sleep") || all.Setup[1].Package.Functions[adapter].Network != nil {
		t.Fatalf("the adapter = %+v", all.Setup[1].Package.Functions)
	}

	// Without the health write, the adapter's reads and writes of sleep
	// and workout are uncovered: the row blocks and says which scope
	// covers each.
	fewer := slices.DeleteFunc(resolvedSyncScopes(), func(s string) bool { return s == "write:"+healthPkg+"/*" })
	narrowed := e.viewWith(id, fewer)
	row := narrowed.Setup[1]
	if row.Action != substrate.SetupInstall || row.Blocked == nil || row.Blocked.Reason != substrate.BlockReachesPastScopes || row.Blocked.Fix != "" ||
		!strings.Contains(row.Blocked.Problem, "it writes "+healthPkg+"/sleep (write:"+healthPkg+"/sleep covers it)") ||
		!strings.Contains(row.Blocked.Problem, "it writes "+healthPkg+"/workout (write:"+healthPkg+"/workout covers it)") ||
		strings.Contains(row.Blocked.Problem, "observations") {
		t.Fatalf("without the health write, applehealth = %+v %+v", row, row.Blocked)
	}
	if narrowed.PlanHash == all.PlanHash || narrowed.Setup[0].Blocked != nil {
		t.Fatalf("narrowed = hash %q (all %q), apple %+v", narrowed.PlanHash, all.PlanHash, narrowed.Setup[0].Blocked)
	}
	uncovered := 0
	for _, r := range row.Package.Reach {
		if !r.Covered {
			uncovered++
		}
	}
	if uncovered != 4 || !strings.Contains(row.Blocked.Problem, "it reads "+healthPkg+"/sleep (read:"+healthPkg+"/sleep covers it)") {
		t.Fatalf("%d entries uncovered, want the two health reads and writes: %+v", uncovered, row.Package.Reach)
	}
	// Approving with those scopes is held to the same plan, blocked.
	wantErrorCode(t, e.approveWith(id, fewer, narrowed.PlanHash), http.StatusUnprocessableEntity, codeValidation)
	// Approving with fewer scopes than the page read is not the plan the
	// page showed.
	wantErrorCode(t, e.approveWith(id, fewer, all.PlanHash), http.StatusConflict, codeConflict)
	// Checked back, the plan is the first read's.
	back := e.viewWith(id, resolvedSyncScopes())
	if back.PlanHash != all.PlanHash || back.Setup[1].Blocked != nil {
		t.Fatalf("checked back = hash %q (all %q) %+v", back.PlanHash, all.PlanHash, back.Setup[1].Blocked)
	}
	// Nothing checked covers nothing: every package with code blocks, and
	// apple, which ships none, does not.
	none := e.viewWith(id, []string{""})
	if none.Setup[0].Blocked != nil || none.Setup[1].Blocked == nil || none.Setup[1].Blocked.Reason != substrate.BlockReachesPastScopes {
		t.Fatalf("nothing checked = apple %+v applehealth %+v", none.Setup[0].Blocked, none.Setup[1].Blocked)
	}
	// A scope the app did not ask for is refused on the read.
	rec := e.do(http.MethodGet, "/api/v1/authorizations/"+id+"?scope=read:"+personKind, e.owner, nil, "")
	wantErrorCode(t, rec, http.StatusBadRequest, codeValidation)
	// The approval of what the page showed runs.
	wantStatus(t, e.approveWith(id, resolvedSyncScopes(), all.PlanHash), http.StatusAccepted)
	if done := e.settle(id); done.State != substrate.AuthorizationApproved {
		t.Fatalf("the set up ended %+v", done.Failure)
	}

	// A token's check is held to the token's own scopes: one that names the
	// package but not everything its code reaches is told the row blocks.
	narrow := e.grant("write:"+appleHealthPkg+"/*", []string{"write:" + appleHealthPkg + "/*"})
	got := e.check(narrow, checkRequest(syncScopes, syncDetails))
	if row := got.Setup[1]; row.Action != substrate.SetupNone || row.Blocked != nil {
		t.Fatalf("a package in place = %+v", row)
	}
	newer := strings.Replace(syncDetails, `"version": 5`, `"version": 99`, 1)
	got = e.check(narrow, checkRequest(syncScopes, newer))
	if row := got.Setup[1]; row.Action != substrate.SetupUpgrade || row.Blocked == nil || row.Blocked.Reason != substrate.BlockServerUpdate {
		t.Fatalf("a version the server lacks = %+v %+v", row, row.Blocked)
	}
}
