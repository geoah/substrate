package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Decision 0214: a package detail resolves through the binary's catalog and
// the authority documents; `declare:` and the `agent` detail are refused by
// name; a build says its version and the token and the check record it; and
// an app reaches an agent only when its own reads cover the agent's.

const (
	dpAuthority = "example.org"
	dpTraining  = dpAuthority + "/training"
	dpSession   = dpTraining + "/session"
	dpDocURL    = "https://example.org/.well-known/substrate/authority.json"
)

// publishingCatalog is the real catalog with one package an authority
// document publishes beside it, answering Resolve as the catalog's document
// reader will: the binary wins for every id it ships, the listed package
// resolves at the floor or below its version, and anything else is not
// published with the reason.
type publishingCatalog struct {
	*catalog.Catalog
	training *catalog.Bundle
}

func newPublishingCatalog(t *testing.T, cat *catalog.Catalog) *publishingCatalog {
	t.Helper()
	closure := "kind: substrate.reamde.dev/core/package\nmetadata:\n  id: " + dpTraining + "\ndata:\n  authority: " + dpAuthority + "\n  package: training\n  version: 3\n" +
		"---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: " + dpTraining + "\ndata:\n  authority: " + dpAuthority + "\n  package: training\n  description: Strength sessions.\n" +
		"---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: " + dpSession + "\ndata:\n  authority: " + dpAuthority + "\n  package: training\n  name: session\n  label:\n    singular: Session\n    plural: Sessions\n"
	docs, err := catalog.Load(catalog.ProviderRoot(fstest.MapFS{"example.org/training/bundle.yaml": {Data: []byte(closure)}}))
	if err != nil {
		t.Fatal(err)
	}
	b, ok := docs.ByID(dpTraining)
	if !ok || len(docs.Warnings()) > 0 {
		t.Fatalf("the published fixture did not load: %v", docs.Warnings())
	}
	return &publishingCatalog{Catalog: cat, training: b.FromDocument(catalog.Origin{
		Authority: dpAuthority, Name: "Example Publishing", DocumentURL: dpDocURL,
		ClosureURL: "https://example.org/packages/training-3.yaml", SHA256: strings.Repeat("cd", 32),
	})}
}

func (c *publishingCatalog) Resolve(_ context.Context, id string, atLeast int64) (*catalog.Bundle, error) {
	if b, ok := c.Catalog.ByID(id); ok {
		return b, nil
	}
	if id != dpTraining {
		return nil, fmt.Errorf("%w: %s publishes no document listing %s", catalog.ErrNotPublished, dpAuthority, id)
	}
	if c.training.Version < atLeast {
		return nil, fmt.Errorf("%w: %s lists %s at version %d, and the app needs %d or later", catalog.ErrNotPublished, dpAuthority, id, c.training.Version, atLeast)
	}
	return c.training, nil
}

func (c *publishingCatalog) Upgrade(ctx context.Context, id string, ds substrate.Dataset, held *substrate.BundleStatus) (*substrate.BundleUpgrade, error) {
	if id == dpTraining {
		return nil, nil
	}
	return c.Catalog.Upgrade(ctx, id, ds, held)
}

// refreshes records what the handler asked the catalog to refresh.
type refreshes struct {
	mu    sync.Mutex
	asked []string
	err   error
}

func (r *refreshes) RefreshPackage(_ context.Context, ds substrate.Dataset, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, ds.Repository().Authority+" "+id)
	return r.err
}

func (r *refreshes) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.asked...)
}

// publishingEnv is a set up environment whose handler resolves packages
// through the publishing catalog and refreshes through the recorder.
func publishingEnv(t *testing.T) (*setupEnv, *refreshes) {
	t.Helper()
	e := newSetupEnv(t)
	refresher := &refreshes{}
	e.h = New(Config{
		Service: e.svc, ClientDocuments: e.docs, AuthInterval: 0, Catalog: e.catalog,
		Packages: newPublishingCatalog(t, e.catalog), PackageRefresher: refresher,
	})
	return e, refresher
}

// THE RETIRED GRAMMAR IS REFUSED BY NAME: a `declare:` scope and an `agent`
// detail are refused at the authorization endpoint and at the check, each
// pointing at the package detail that replaced them, and the metadata lists
// neither.
func TestADeclareScopeAndAnAgentDetailAreRefusedAtTheDoor(t *testing.T) {
	e := newSetupEnv(t)
	md := decodeJSON[authorizationServerMetadata](t, e.do(http.MethodGet, asMetadataPath, "", nil, ""))
	if strings.Join(md.ScopesSupported, " ") != "read:* write:* agents:* decide:* full" {
		t.Fatalf("scopes_supported = %v", md.ScopesSupported)
	}
	agentDetail := `[{"type":"agent","agent":"~/trainingcoach/workoutCoach","reads":["~/training/routine"],"writes":[],"spendCentsPerDay":100}]`
	for name, params := range map[string]map[string]string{
		"a declare scope": appRequest("declare:~/training write:~/training/*"),
		"an agent detail": setupRequest("write:~/training/*", agentDetail),
		"a version too long": func() map[string]string {
			p := appRequest("read:~/notes/*")
			p["software_version"] = strings.Repeat("9", 65)
			return p
		}(),
	} {
		rec := e.do(http.MethodGet, authorizeURL(params), "", nil, "")
		retired := name != "a version too long"
		if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "retired") != retired {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	for name, body := range map[string]string{
		"a declare scope":    `{"scope":"declare:~/training","details":[]}`,
		"an agent detail":    `{"scope":"read:~/notes/*","details":` + agentDetail + `}`,
		"a version too long": `{"scope":"read:~/notes/*","details":[],"softwareVersion":"` + strings.Repeat("9", 65) + `"}`,
	} {
		if rec := e.do(http.MethodPost, "/api/v1/authorizations/check", e.owner, []byte(body), "application/json"); rec.Code != http.StatusBadRequest {
			t.Errorf("the check with %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if builds := e.svc.datasets[fakeRepository].builds; len(builds) != 0 {
		t.Fatalf("a refused check recorded a build: %+v", builds)
	}
}

// A BUILD'S VERSION AND FLOORS RIDE THE TOKEN (decision 0213): the
// authorization request's `software_version` and the floor of each package
// detail are minted onto the token the approval mints; a request without a
// version mints the floors alone.
func TestABuildsVersionAndFloorsRideTheToken(t *testing.T) {
	e := newSetupEnv(t)
	mint := func(version string) substrate.TokenInfo {
		t.Helper()
		params := setupRequest(syncScopes, syncDetails)
		if version != "" {
			params["software_version"] = version
		}
		id := e.park(params)
		v := e.view(id)
		// The first approval sets the packages up; the next finds them in
		// place and answers its code at once.
		rec := e.approveWith(id, resolvedSyncScopes(), v.PlanHash)
		next := decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect
		if rec.Code == http.StatusAccepted {
			next = e.settle(id).Next
		} else {
			wantStatus(t, rec, http.StatusOK)
		}
		tok := e.exchange(tokenForm(codeFrom(t, next)))
		wantStatus(t, tok, http.StatusOK)
		secret := decodeJSON[tokenResponse](t, tok).AccessToken
		e.svc.mu.Lock()
		defer e.svc.mu.Unlock()
		return e.svc.tokens[secret].info
	}
	info := mint("3.2.1 (418)")
	if info.AppSoftwareVersion != "3.2.1 (418)" || len(info.AppPackages) != 2 || info.AppPackages[applePkg] != 3 || info.AppPackages[appleHealthPkg] != 5 {
		t.Fatalf("the token carries %q %v", info.AppSoftwareVersion, info.AppPackages)
	}
	if info := mint(""); info.AppSoftwareVersion != "" || len(info.AppPackages) != 2 {
		t.Fatalf("without a version the token carries %q %v", info.AppSoftwareVersion, info.AppPackages)
	}
}

// A CHECK RECORDS THE BUILD AND REFRESHES WHAT IT HOLDS BELOW THE FLOOR: the
// floors and the version land on the calling token before the answer, and a
// document-published package the repository holds below the floor the app
// asks for is refreshed from its publisher before the plan, so an upgrade the
// catalog applies on its own answers `none`. A package held at the floor, one
// not held, and a binary package below its floor are not refreshed.
func TestACheckRecordsTheBuildAndRefreshesAPackageBelowItsFloor(t *testing.T) {
	e, refresher := publishingEnv(t)
	ds := e.svc.datasets[fakeRepository]
	e.repo.mu.Lock()
	e.repo.statuses[dpTraining] = substrate.BundleStatus{ID: dpTraining, Installed: true, Enabled: true, Version: 2, ShippedVersion: 2}
	e.repo.statuses[appleHealthPkg] = substrate.BundleStatus{ID: appleHealthPkg, Installed: true, Enabled: true, Version: 2, ShippedVersion: 2}
	e.repo.mu.Unlock()
	details := `[{"type":"package","package":"` + dpTraining + `","version":3},
	{"type":"package","package":"` + appleHealthPkg + `","version":5},
	{"type":"package","package":"` + applePkg + `","version":3}]`
	body := `{"scope":"read:~/notes/*","details":` + details + `,"softwareVersion":"1.4.0"}`
	got := e.check(e.owner, []byte(body))
	if asked := refresher.list(); strings.Join(asked, ",") != fakeRepository+" "+dpTraining {
		t.Fatalf("refreshed %v, want the published package below its floor alone", asked)
	}
	ds.mu.Lock()
	builds := append([]recordedBuild(nil), ds.builds...)
	ds.mu.Unlock()
	if len(builds) != 1 || builds[0].version != "1.4.0" || len(builds[0].packages) != 3 || builds[0].packages[dpTraining] != 3 || builds[0].packages[appleHealthPkg] != 5 {
		t.Fatalf("the check recorded %+v", builds)
	}
	if row := got.Setup[0]; row.Action != substrate.SetupUpgrade || row.Package.Held != 2 || row.Package.Shipped != 3 || row.Blocked != nil {
		t.Fatalf("the published row = %+v %+v", row, row.Package)
	}
	// Held at the floor now, as a refresh that landed leaves it: nothing is
	// asked again, and the row is in place. A check with no version records
	// the floors alone.
	e.repo.mu.Lock()
	e.repo.statuses[dpTraining] = substrate.BundleStatus{ID: dpTraining, Installed: true, Enabled: true, Version: 3, ShippedVersion: 3}
	e.repo.mu.Unlock()
	got = e.check(e.owner, []byte(`{"scope":"read:~/notes/*","details":`+details+`}`))
	if asked := refresher.list(); len(asked) != 1 {
		t.Fatalf("a package at its floor was refreshed: %v", asked)
	}
	if row := got.Setup[0]; row.Action != substrate.SetupNone || row.Package.Held != 3 {
		t.Fatalf("the refreshed row = %+v %+v", row, row.Package)
	}
	ds.mu.Lock()
	last := ds.builds[len(ds.builds)-1]
	ds.mu.Unlock()
	if last.version != "" || len(last.packages) != 3 {
		t.Fatalf("a check without a version recorded %+v", last)
	}
	// A refresh that fails leaves the plan to say what the package needs.
	e.repo.mu.Lock()
	e.repo.statuses[dpTraining] = substrate.BundleStatus{ID: dpTraining, Installed: true, Enabled: true, Version: 1, ShippedVersion: 1}
	e.repo.mu.Unlock()
	refresher.err = fmt.Errorf("%w: the document could not be read", substrate.ErrUnavailable)
	if row := e.check(e.owner, []byte(body)).Setup[0]; row.Action != substrate.SetupUpgrade || row.Package.Held != 1 {
		t.Fatalf("after a failed refresh the row = %+v %+v", row, row.Package)
	}
	// A build the token cannot record is the check's answer.
	ds.errs = map[string]error{"RecordAppBuild": fmt.Errorf("%w: the database refused the write", substrate.ErrUnavailable)}
	wantStatus(t, e.do(http.MethodPost, "/api/v1/authorizations/check", e.owner, []byte(body), "application/json"), http.StatusServiceUnavailable)
}

// A DOCUMENT-PUBLISHED PACKAGE ROW NAMES ITS PUBLISHER on the approval page,
// with the version approving would install, and a package nothing publishes
// blocks the approval in the catalog's words.
func TestTheApprovalPageShowsADocumentPublishedPackage(t *testing.T) {
	e, _ := publishingEnv(t)
	id := e.park(setupRequest("write:example.org/training/*", `[{"type":"package","package":"`+dpTraining+`","version":1}]`))
	v := e.view(id)
	row := v.Setup[0]
	if row.Action != substrate.SetupInstall || row.Blocked != nil || row.Package.Shipped != 3 || row.Package.Held != 0 ||
		row.Package.Tier != substrate.TierProvider || row.Package.Description != "Strength sessions." {
		t.Fatalf("the published row = %+v %+v", row, row.Package)
	}
	if p := row.Package.Publisher; p == nil || p.Authority != dpAuthority || p.Name != "Example Publishing" || p.DocumentURL != dpDocURL || p.FromFile {
		t.Fatalf("the published row's publisher = %+v", row.Package.Publisher)
	}
	if len(row.Package.Kinds) != 1 || row.Package.Kinds[0].Identity != dpSession || !row.Package.Kinds[0].Added {
		t.Fatalf("the published row's kinds = %+v", row.Package.Kinds)
	}
	// The write scope lists the kind the install brings.
	if s := v.Scopes[0]; len(s.Kinds) != 1 || s.Kinds[0].Identity != dpSession || !s.Kinds[0].Added {
		t.Fatalf("the scope's kinds = %+v", s.Kinds)
	}
	id = e.park(setupRequest("read:~/notes/*", `[{"type":"package","package":"example.org/unlisted","version":1}]`))
	v = e.view(id)
	row = v.Setup[0]
	if row.Blocked == nil || row.Blocked.Reason != substrate.BlockNotPublished || !strings.Contains(row.Blocked.Problem, "publishes no document listing example.org/unlisted") {
		t.Fatalf("an unlisted package's row = %+v %+v", row, row.Blocked)
	}
	rec := e.approveWith(id, []string{"read:" + fakeRepository + "/notes/*"}, v.PlanHash)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "not published") {
		t.Fatalf("approving a blocked row: %d %s", rec.Code, rec.Body.String())
	}
}

// AN AGENT IS NO CHANNEL TO MORE THAN A CALLER READS (decision 0214, for
// every agent): an app's `agents:*` reaches an agent, its row and its
// threads only when its own read scopes cover every kind the agent reads,
// exact kinds and globs alike, an internal kind only by its exact scope.
func TestAnAppReachesAnAgentOnlyWhenItReadsEverythingTheAgentReads(t *testing.T) {
	e := newSetupEnv(t).appEnv
	ds := e.svc.datasets[fakeRepository]
	const (
		coachPkg   = fakeRepository + "/trainingcoach"
		coach      = coachPkg + "/workoutCoach"
		sleep      = "substrate.reamde.dev/health/sleep"
		condition  = "substrate.reamde.dev/clinical/condition"
		ledger     = fakeRepository + "/money/ledger"
		wideAgent  = coachPkg + "/librarian"
		innerAgent = coachPkg + "/auditor"
		thread     = "substrate.reamde.dev/llm/thread"
	)
	agentRow := func(id string, reads ...any) *substrate.Record {
		return &substrate.Record{Kind: corePackage + "/agent", ID: id, Properties: map[string]any{
			"purpose":     "primary",
			"permissions": map[string]any{"reads": map[string]any{"kinds": reads}},
		}}
	}
	ds.mu.Lock()
	ds.records[coachPkg] = &substrate.Record{Kind: corePackage + "/package", ID: coachPkg, Properties: map[string]any{substrate.PackageDeclaredBy: "console"}}
	ds.records[coach] = agentRow(coach, sleep, condition)
	ds.records[wideAgent] = agentRow(wideAgent, "substrate.reamde.dev/health/*")
	ds.records[innerAgent] = agentRow(innerAgent, ledger)
	ds.records["coach-thread"] = &substrate.Record{Kind: thread, ID: "coach-thread", Properties: map[string]any{
		"agent": map[string]any{"ref": corePackage + "/agent/" + coach},
	}}
	// The substrate's own kinds are internal, as the shipped registry
	// declares them, so a glob never reads an agent or a thread whole.
	internal := map[string]any{"purpose": "internal"}
	for _, k := range []string{"agent", "kind", "package"} {
		ds.types = append(ds.types, substrate.KindInfo{Identity: corePackage + "/" + k, Authority: coreAuthorityName, Package: "core", Name: k, Definition: internal})
	}
	ds.types = append(ds.types, substrate.KindInfo{Identity: thread, Authority: coreAuthorityName, Package: "llm", Name: "thread", Definition: internal})
	for _, k := range []string{sleep, condition} {
		a, p, n := vocabulary.SplitKindRef(k)
		ds.types = append(ds.types, substrate.KindInfo{Identity: k, Authority: a, Package: p, Name: n})
	}
	ds.types = append(ds.types, substrate.KindInfo{Identity: ledger, Authority: fakeRepository, Package: "money", Name: "ledger", Definition: internal})
	ds.mu.Unlock()
	get := func(tok, kind, id string) int {
		return e.do(http.MethodGet, "/api/v1/"+kind+"/"+url.PathEscape(id), tok, nil, "").Code
	}
	notes := e.grant("agents:* read:~/notes/*", []string{"agents:*", "read:" + fakeRepository + "/notes/*"})
	if code := get(notes, corePackage+"/agent", coach); code == http.StatusOK {
		t.Errorf("an app that reads only its notes reads the coach's row: %d", code)
	}
	if code := get(notes, thread, "coach-thread"); code == http.StatusOK {
		t.Errorf("an app that reads only its notes reads the coach's thread: %d", code)
	}
	body, _ := json.Marshal(map[string]any{"message": "List my medications."})
	if rec := e.do(http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(coach)+"/chat", notes, body, "application/json"); rec.Code != http.StatusNotFound {
		t.Errorf("an app that reads only its notes chats with the coach: %d %s", rec.Code, rec.Body.String())
	}
	exact := e.grant("agents:* read:"+sleep+" read:"+condition, []string{"agents:*", "read:" + sleep, "read:" + condition})
	if code := get(exact, corePackage+"/agent", coach); code != http.StatusOK {
		t.Errorf("an app reading exactly what the coach reads does not reach it: %d", code)
	}
	if code := get(exact, thread, "coach-thread"); code != http.StatusOK {
		t.Errorf("an app reading exactly what the coach reads does not read its thread: %d", code)
	}
	// A glob the agent reads is covered by a glob at least as wide, never by
	// one kind in it.
	if code := get(exact, corePackage+"/agent", wideAgent); code == http.StatusOK {
		t.Errorf("one kind of health covers an agent reading all of health: %d", code)
	}
	health := e.grant("agents:* read:substrate.reamde.dev/*", []string{"agents:*", "read:substrate.reamde.dev/*"})
	if code := get(health, corePackage+"/agent", wideAgent); code != http.StatusOK {
		t.Errorf("an authority glob does not cover an agent reading one of its packages: %d", code)
	}
	// An internal kind is covered by its exact scope alone.
	everything := e.grant("agents:* read:*", []string{"agents:*", "read:*"})
	if code := get(everything, corePackage+"/agent", innerAgent); code == http.StatusOK {
		t.Errorf("read:* covers an agent reading an internal kind: %d", code)
	}
	auditor := e.grant("agents:* read:~/money/ledger", []string{"agents:*", "read:" + ledger})
	if code := get(auditor, corePackage+"/agent", innerAgent); code != http.StatusOK {
		t.Errorf("the exact scope does not cover an agent reading an internal kind: %d", code)
	}
	if code := get(everything, corePackage+"/agent", coach); code != http.StatusOK {
		t.Errorf("read:* does not reach the coach: %d", code)
	}
}
