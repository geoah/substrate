package appsetup

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// An install brings its requirements first, each listed with what it brings
// of its own, and the row lists its own alone.
func TestTheChainCarriesEachPackagesOwnFacts(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	row := plan(t, ds, cat, pkgDetail(appPkg, 4))[0]
	info := row.Package
	if row.Action != substrate.SetupInstall || row.Blocked != nil || info.Shipped != 4 || info.Held != 0 || info.Description == "" {
		t.Fatalf("app = %+v %+v", row, info)
	}
	if len(info.Requires) != 1 {
		t.Fatalf("app brings %+v first, want base alone", info.Requires)
	}
	step := info.Requires[0]
	if step.ID != basePkg || step.Action != substrate.SetupInstall || step.Shipped != 2 || step.Blocked != nil ||
		len(step.Kinds) != 1 || step.Kinds[0].Identity != basePkg+"/thing" || step.Kinds[0].Label == nil ||
		len(step.Functions) != 1 || step.Functions[0].Writes[0] != basePkg+"/thing" {
		t.Fatalf("base step = %+v", step)
	}
	if slices.ContainsFunc(info.Kinds, func(k substrate.AuthorizationKind) bool {
		return vocabulary.KindPackage(k.Identity) != appPkg || !k.Added
	}) {
		t.Fatalf("app's own kinds = %+v", info.Kinds)
	}
	account := slices.IndexFunc(info.Kinds, func(k substrate.AuthorizationKind) bool { return k.Identity == appAccount })
	if account < 0 || info.Kinds[account].Purpose != vocabulary.PurposeInternal {
		t.Fatalf("app's account kind = %+v", info.Kinds)
	}
	if len(info.Functions) != 1 || strings.Join(info.Functions[0].Writes, ",") != appPkg+"/item,"+basePkg+"/*" {
		t.Fatalf("app's functions = %+v, want a bare name completed and a glob kept", info.Functions)
	}
	if len(info.Triggers) != 3 || !info.Triggers[0].Webhook || info.Triggers[0].Enabled || info.Triggers[0].Callable != appFunction {
		t.Fatalf("app's triggers = %+v", info.Triggers)
	}
	// A kind the repository holds already is not one approving adds.
	ds.installed(cat, basePkg, 2)
	if row := plan(t, ds, cat, pkgDetail(appPkg, 4))[0]; len(row.Package.Requires) != 0 {
		t.Fatalf("with base in place, app brings %+v first", row.Package.Requires)
	}

	// A row planned for its action alone keeps its chain and what blocks it,
	// and carries no facts.
	ds = newFakeDataset()
	rows, _, err := Plan(context.Background(), ds, cat, []substrate.AuthorizationDetail{pkgDetail(appPkg, 4)}, Options{ActionOnly: map[string]bool{appPkg: true}})
	if err != nil {
		t.Fatal(err)
	}
	if info := rows[0].Package; rows[0].Action != substrate.SetupInstall || info.Kinds != nil || info.Functions != nil || info.Triggers != nil ||
		len(info.Requires) != 1 || info.Requires[0].Kinds != nil {
		t.Fatalf("an action-only row = %+v", info)
	}
}

// What the provider page asks the owner to confirm, or refuses, or could not
// preview, blocks an upgrade from the approval page, on the package asked
// for and on one it needs first.
func TestAnUpgradeTheProviderPageMustReviewIsBlocked(t *testing.T) {
	cat := testCatalog(t)
	for name, c := range map[string]struct {
		preview substrate.BundleUpgrade
		err     error
		says    string
	}{
		"lossy":          {preview: substrate.BundleUpgrade{Lossy: true}, says: "confirm"},
		"discards edits": {preview: substrate.BundleUpgrade{DiscardsEdits: true}, says: "confirm"},
		"refused":        {preview: substrate.BundleUpgrade{Blockers: []string{"a kind is retired"}}, says: "refused"},
		"failed":         {err: errors.New("the preview broke"), says: "could not be previewed"},
	} {
		t.Run(name, func(t *testing.T) {
			ds := newFakeDataset()
			ds.preview, ds.previewErr = c.preview, c.err
			ds.installed(cat, basePkg, 2)
			ds.installed(cat, appPkg, 3)
			row := plan(t, ds, cat, pkgDetail(appPkg, 4))[0]
			if row.Action != substrate.SetupUpgrade {
				t.Fatalf("app = %q, want upgrade", row.Action)
			}
			wantBlocked(t, "app", row.Blocked, substrate.BlockUpgradeNeedsReview, ProviderPage(appPkg))
			if !strings.Contains(row.Blocked.Problem, c.says) || !strings.Contains(row.Blocked.Problem, "this package") {
				t.Fatalf("app says %q", row.Blocked.Problem)
			}

			// The same, of the requirement an install upgrades first.
			ds = newFakeDataset()
			ds.preview, ds.previewErr = c.preview, c.err
			ds.installed(cat, basePkg, 1)
			row = plan(t, ds, cat, pkgDetail(appPkg, 4))[0]
			step := row.Package.Requires[0]
			if row.Action != substrate.SetupInstall || step.Action != substrate.SetupUpgrade {
				t.Fatalf("app = %q, base = %q", row.Action, step.Action)
			}
			wantBlocked(t, "base", step.Blocked, substrate.BlockUpgradeNeedsReview, ProviderPage(basePkg))
			wantBlocked(t, "app", row.Blocked, substrate.BlockUpgradeNeedsReview, ProviderPage(basePkg))
			if !strings.Contains(row.Blocked.Problem, "a package it needs first") {
				t.Fatalf("app says %q, want the package it needs first named as one", row.Blocked.Problem)
			}
		})
	}
	// A clean preview blocks nothing.
	ds := newFakeDataset()
	ds.installed(cat, basePkg, 2)
	ds.installed(cat, appPkg, 3)
	if row := plan(t, ds, cat, pkgDetail(appPkg, 4))[0]; row.Action != substrate.SetupUpgrade || row.Blocked != nil || ds.previews != 1 {
		t.Fatalf("app = %+v after %d previews", row, ds.previews)
	}
}

// A package the owner declared by hand below what is needed is theirs:
// installing the published one would replace it, asked for or needed first.
func TestAPackageTheOwnerDeclaredIsNeverReplaced(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	ds.installed(cat, basePkg, 2)
	ds.declared(cat, appPkg, 3)
	row := plan(t, ds, cat, pkgDetail(appPkg, 4))[0]
	if row.Action != substrate.SetupUpgrade || row.Package.Held != 3 {
		t.Fatalf("app = %+v %+v", row, row.Package)
	}
	wantBlocked(t, "app", row.Blocked, substrate.BlockDeclaredByOwner, ProviderPage(appPkg))
	if !strings.Contains(row.Blocked.Problem, "version 3") {
		t.Fatalf("app says %q, want the version held", row.Blocked.Problem)
	}
	// At the version needed, it is in place.
	if row := plan(t, ds, cat, pkgDetail(appPkg, 3))[0]; row.Action != substrate.SetupNone || row.Blocked != nil {
		t.Fatalf("an own package at the version needed = %+v", row)
	}

	// Below the floor of a package that needs it first.
	ds = newFakeDataset()
	ds.declared(cat, basePkg, 1)
	row = plan(t, ds, cat, pkgDetail(appPkg, 4))[0]
	step := row.Package.Requires[0]
	if step.ID != basePkg || step.Action != substrate.SetupUpgrade {
		t.Fatalf("base step = %+v", step)
	}
	wantBlocked(t, "base", step.Blocked, substrate.BlockDeclaredByOwner, ProviderPage(basePkg))
	wantBlocked(t, "app", row.Blocked, substrate.BlockDeclaredByOwner, ProviderPage(basePkg))
	if !strings.Contains(row.Blocked.Problem, "needs a package first") {
		t.Fatalf("app says %q", row.Blocked.Problem)
	}
	if ds.previews != 0 {
		t.Fatalf("a blocked chain was previewed %d times", ds.previews)
	}
}

// A quarantined copy is the provider page's to explain, asked for or needed
// first.
func TestAQuarantinedPackageIsBlocked(t *testing.T) {
	cat := testCatalog(t)
	quarantine := func(ds *fakeDataset, id string) {
		ds.mu.Lock()
		defer ds.mu.Unlock()
		ds.statuses[id] = substrate.BundleStatus{ID: id, Quarantined: true, Version: 1}
	}
	ds := newFakeDataset()
	quarantine(ds, appPkg)
	wantBlocked(t, "app", plan(t, ds, cat, pkgDetail(appPkg, 4))[0].Blocked, substrate.BlockQuarantined, ProviderPage(appPkg))

	ds = newFakeDataset()
	quarantine(ds, basePkg)
	row := plan(t, ds, cat, pkgDetail(appPkg, 4))[0]
	wantBlocked(t, "base", row.Package.Requires[0].Blocked, substrate.BlockQuarantined, ProviderPage(basePkg))
	wantBlocked(t, "app", row.Blocked, substrate.BlockQuarantined, ProviderPage(basePkg))
}

// A sample is the console's own door, a package the catalog does not ship is
// nobody's, and a server too old ships too little: asked for or needed first.
func TestWhatTheCatalogCannotInstallIsBlocked(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	for name, c := range map[string]struct {
		detail  substrate.AuthorizationDetail
		reason  string
		fix     string
		step    string
		problem string
	}{
		"a sample":                 {detail: pkgDetail(samplePkg, 1), reason: substrate.BlockSample, fix: "/data"},
		"not shipped":              {detail: pkgDetail("example.com/nowhere", 1), reason: substrate.BlockNotShipped},
		"a version not shipped":    {detail: pkgDetail(appPkg, 5), reason: substrate.BlockServerUpdate, problem: "This server ships version 4; the app needs version 5. The server needs an update first."},
		"a sample needed first":    {detail: pkgDetail(needsSample, 1), reason: substrate.BlockSample, fix: "/data", step: samplePkg},
		"nothing ships what first": {detail: pkgDetail(needsMissing, 1), reason: substrate.BlockNotShipped, step: "example.com/gone"},
		"a floor not shipped":      {detail: pkgDetail(newerBasePkg, 1), reason: substrate.BlockServerUpdate, step: basePkg, problem: "It needs a package first at version 3, and this server ships version 2. The server needs an update first."},
	} {
		t.Run(name, func(t *testing.T) {
			row := plan(t, ds, cat, c.detail)[0]
			if row.Action != substrate.SetupInstall {
				t.Fatalf("row = %q, want install", row.Action)
			}
			wantBlocked(t, "row", row.Blocked, c.reason, c.fix)
			if c.problem != "" && row.Blocked.Problem != c.problem {
				t.Fatalf("row says %q, want %q", row.Blocked.Problem, c.problem)
			}
			if c.step == "" {
				return
			}
			i := slices.IndexFunc(row.Package.Requires, func(s substrate.AuthorizationPackageStep) bool { return s.ID == c.step })
			if i < 0 {
				t.Fatalf("the chain %+v lists no %s", row.Package.Requires, c.step)
			}
			wantBlocked(t, c.step, row.Package.Requires[i].Blocked, c.reason, c.fix)
		})
	}
}

// An account is created once, held to what an approval may write, and never
// brought back from a deletion: until the deletion is collected, the row
// says so.
func TestAnAccountIsCreatedOnceAndNeverBroughtBack(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	ds.installed(cat, basePkg, 2)
	ds.installed(cat, appPkg, 4)
	app := pkgDetail(appPkg, 4)
	if row := plan(t, ds, cat, app, accountDetail("phone", map[string]any{"displayName": "Ada's", "nickname": "a"}))[1]; row.Action != substrate.SetupCreate ||
		row.Blocked != nil || row.Account.Label != nil {
		t.Fatalf("a new account = %+v", row)
	}
	for name, c := range map[string]struct {
		props  map[string]any
		reason string
		says   string
	}{
		"a secret":         {props: map[string]any{"tokenRef": "x"}, reason: substrate.BlockUnsupported, says: "secret"},
		"the server's":     {props: map[string]any{"status": "x"}, reason: substrate.BlockUnsupported, says: "the server's"},
		"another hand's":   {props: map[string]any{"tokenStatus": "x"}, reason: substrate.BlockUnsupported, says: "sign-in"},
		"not declared":     {props: map[string]any{"shade": "x"}, reason: substrate.BlockUnsupported, says: "no shade property"},
		"not requested":    {reason: substrate.BlockNotRequested, says: "does not set up"},
		"not an account":   {reason: substrate.BlockUnsupported, says: "not an account kind"},
		"no such kind":     {reason: substrate.BlockUnsupported, says: "declares no such kind"},
		"a deleted record": {reason: substrate.BlockAccountDeleted, says: "deleted"},
	} {
		t.Run(name, func(t *testing.T) {
			details := []substrate.AuthorizationDetail{app, accountDetail("phone", c.props)}
			switch name {
			case "not requested":
				details = details[1:]
			case "not an account":
				details[1].Kind = appPkg + "/item"
			case "no such kind":
				details[1].Kind = appPkg + "/nothing"
			case "a deleted record":
				ds := newFakeDataset()
				ds.installed(cat, basePkg, 2)
				ds.installed(cat, appPkg, 4)
				at := time.Unix(10, 0).UTC()
				ds.put(substrate.Record{Kind: appAccount, ID: "phone", Version: 2, Properties: map[string]any{}, DeletedAt: &at})
				rows := plan(t, ds, cat, details...)
				if rows[1].Action != substrate.SetupCreate {
					t.Fatalf("a deleted account = %q, want create", rows[1].Action)
				}
				wantBlocked(t, "account", rows[1].Blocked, c.reason, "")
				// Collected, it is created again.
				ds.mu.Lock()
				delete(ds.records, recordKey(appAccount, "phone"))
				ds.mu.Unlock()
				if row := plan(t, ds, cat, details...)[1]; row.Action != substrate.SetupCreate || row.Blocked != nil {
					t.Fatalf("a collected account = %+v", row)
				}
				// Live, it is in place.
				ds.put(substrate.Record{Kind: appAccount, ID: "phone", Version: 1, Properties: map[string]any{}})
				if row := plan(t, ds, cat, details...)[1]; row.Action != substrate.SetupNone || row.Blocked != nil {
					t.Fatalf("a live account = %+v", row)
				}
				return
			}
			row := plan(t, ds, cat, details...)[len(details)-1]
			wantBlocked(t, "account", row.Blocked, c.reason, "")
			if !strings.Contains(row.Blocked.Problem, c.says) {
				t.Fatalf("account says %q, want %q", row.Blocked.Problem, c.says)
			}
		})
	}
}

// A webhook is turned on only where the request names the package its code
// is, and never where its provider signs what it sends.
func TestAWebhookIsTheAppsOnlyWhereItsPackageIsRequested(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	app := pkgDetail(appPkg, 4)
	if row := plan(t, ds, cat, app, webhookDetail(appHook))[1]; row.Action != substrate.SetupEnable || row.Blocked != nil ||
		row.Webhook.Package != appPkg {
		t.Fatalf("a shipped webhook = %+v %+v", row, row.Webhook)
	}
	ds.put(substrate.Record{Kind: vocabulary.TriggerKind, ID: "borrowed", Version: 1, Properties: map[string]any{
		"enabled": true, "callable": map[string]any{"ref": baseFunction},
		"source": map[string]any{"webhook": map[string]any{"key": "the-owners-key-0123456789"}},
	}})
	ds.put(substrate.Record{Kind: vocabulary.TriggerKind, ID: "orphan", Version: 1, Properties: map[string]any{
		"enabled": false, "callable": "example.com/app/ingest", "source": map[string]any{"webhook": map[string]any{}},
	}})
	for name, c := range map[string]struct {
		trigger string
		reason  string
		says    string
	}{
		"signed by its provider":     {trigger: signedHook, reason: substrate.BlockUnsupported, says: "signs what it sends"},
		"no webhook":                 {trigger: "app-records", reason: substrate.BlockUnsupported, says: "takes no webhook"},
		"another package's code":     {trigger: "borrowed", reason: substrate.BlockNotRequested, says: "code from a package this request does not set up"},
		"code of no package":         {trigger: "orphan", reason: substrate.BlockUnsupported, says: "belongs to no package"},
		"shipped by nothing to come": {trigger: "elsewhere", reason: substrate.BlockNotRequested, says: "no such trigger"},
	} {
		t.Run(name, func(t *testing.T) {
			row := plan(t, ds, cat, app, webhookDetail(c.trigger))[1]
			if row.Action != substrate.SetupEnable {
				t.Fatalf("row = %q, want enable", row.Action)
			}
			wantBlocked(t, c.trigger, row.Blocked, c.reason, "")
			if !strings.Contains(row.Blocked.Problem, c.says) {
				t.Fatalf("%s says %q", c.trigger, row.Blocked.Problem)
			}
		})
	}
}

// WHOOP signs its deliveries with the owner's OAuth client secret: an app
// cannot deliver to its webhook, and keying it would refuse WHOOP's own.
func TestTheShippedSignedWebhookIsNeverAnApps(t *testing.T) {
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatal(err)
	}
	whoop, ok := cat.ByID("providers.substrate.reamde.dev/whoop")
	if !ok {
		t.Fatal("the catalog ships no WHOOP")
	}
	row := plan(t, newFakeDataset(), cat, pkgDetail(whoop.ID, whoop.Version), webhookDetail("whoop-webhook"))[1]
	wantBlocked(t, "whoop-webhook", row.Blocked, substrate.BlockUnsupported, "")
}

// The hash names what each row does, never the progress a run reports.
func TestAPlansHashIsItsRowsWithoutTheirProgress(t *testing.T) {
	cat := testCatalog(t)
	rows, hash, err := Plan(context.Background(), newFakeDataset(), cat, []substrate.AuthorizationDetail{pkgDetail(appPkg, 4)}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	started := Started(rows, rows)
	if again, _ := Hash(started); again != hash || started[0].Progress != substrate.ProgressWaiting {
		t.Fatalf("a started row hashes %q, want %q", again, hash)
	}
	if _, empty, _ := Plan(context.Background(), newFakeDataset(), cat, nil, Options{}); empty != "" {
		t.Fatalf("no details hash %q", empty)
	}
}
