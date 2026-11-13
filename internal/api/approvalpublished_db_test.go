package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
)

// publishingEnv is a set up environment whose handler resolves packages
// through the publishing catalog and refreshes through the recorder.
func publishingEnv(t *testing.T, opts ...dbOption) (*appSetupEnv, *refreshes) {
	t.Helper()
	refresher := &refreshes{}
	e := newAppSetupEnv(t, append([]dbOption{withConfig(func(c *Config) {
		c.Packages, c.PackageRefresher = newPublishingCatalog(t, c.Catalog), refresher
	})}, opts...)...)
	return e, refresher
}

// holdTraining installs example.org's training package as its document
// listed it at version, over the version held before it (0 for none).
func (e *appSetupEnv) holdTraining(version, before int64) {
	e.t.Helper()
	dec := yaml.NewDecoder(bytes.NewReader([]byte(trainingClosure(version))))
	var docs []map[string]any
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			e.t.Fatalf("parse the training closure: %v", err)
		}
		docs = append(docs, doc)
	}
	if _, err := e.db.ds.InstallBundleClosure(context.Background(), substrate.BundleActor(dpAuthority, "training"), docs, nil, substrate.BundleInstall{
		Published: true, ShippedVersion: version, PublishedFrom: dpDocURL,
		PublishedDigest: strings.Repeat(fmt.Sprintf("%02d", version), 32), ExpectedPublishedVersion: before,
	}); err != nil {
		e.t.Fatalf("install training at version %d: %v", version, err)
	}
}

// A BUILD'S VERSION AND FLOORS RIDE THE TOKEN (decision 0213): the
// authorization request's `software_version` and the floor of each package
// detail are minted onto the token the approval mints; a request without a
// version mints the floors alone.
func TestABuildsVersionAndFloorsRideTheToken(t *testing.T) {
	t.Parallel()
	e := newAppSetupEnv(t)
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
		return e.token(decodeJSON[tokenResponse](t, tok).AccessToken)
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
	t.Parallel()
	e, refresher := publishingEnv(t)
	repo := e.db.repository
	app := e.appToken()
	e.holdTraining(1, 0)
	e.install(healthPkg)
	e.heldAt(appleHealthPkg, 2)
	details := `[{"type":"package","package":"` + dpTraining + `","version":3},
	{"type":"package","package":"` + appleHealthPkg + `","version":5},
	{"type":"package","package":"` + applePkg + `","version":3}]`
	body := `{"scope":"read:~/notes/*","details":` + details + `,"softwareVersion":"1.4.0"}`
	// A refresh that fails leaves the plan to say what the package needs.
	refresher.mu.Lock()
	refresher.err = fmt.Errorf("%w: the document could not be read", substrate.ErrUnavailable)
	refresher.mu.Unlock()
	if row := e.check(app, []byte(body)).Setup[0]; row.Action != substrate.SetupUpgrade || row.Package.Held != 1 {
		t.Fatalf("after a failed refresh the row = %+v %+v", row, row.Package)
	}
	if asked := refresher.list(); strings.Join(asked, ",") != repo+" "+dpTraining {
		t.Fatalf("refreshed %v, want the published package below its floor alone", asked)
	}
	if build := e.token(app); build.AppSoftwareVersion != "1.4.0" || len(build.AppPackages) != 3 ||
		build.AppPackages[dpTraining] != 3 || build.AppPackages[appleHealthPkg] != 5 {
		t.Fatalf("the check recorded %q %v", build.AppSoftwareVersion, build.AppPackages)
	}
	// Held at 2 and refreshed: the recorder lands nothing, so the row still
	// upgrades. The same body asked again within the memo's window is
	// answered from the memo, so the window goes first.
	refresher.mu.Lock()
	refresher.err = nil
	refresher.mu.Unlock()
	e.holdTraining(2, 1)
	e.clock.advance(checkMemoTTL)
	got := e.check(app, []byte(body))
	if asked := refresher.list(); len(asked) != 2 || asked[1] != repo+" "+dpTraining {
		t.Fatalf("refreshed %v, want the published package below its floor again", asked)
	}
	if row := got.Setup[0]; row.Action != substrate.SetupUpgrade || row.Package.Held != 2 || row.Package.Shipped != 3 || row.Blocked != nil {
		t.Fatalf("the published row = %+v %+v", row, row.Package)
	}
	// Held at the floor now, as a refresh that landed leaves it: nothing is
	// asked again, and the row is in place. A check with no version records
	// the floors alone.
	e.holdTraining(3, 2)
	got = e.check(app, []byte(`{"scope":"read:~/notes/*","details":`+details+`}`))
	if asked := refresher.list(); len(asked) != 2 {
		t.Fatalf("a package at its floor was refreshed: %v", asked)
	}
	if row := got.Setup[0]; row.Action != substrate.SetupNone || row.Package.Held != 3 {
		t.Fatalf("the refreshed row = %+v %+v", row, row.Package)
	}
	if build := e.token(app); build.AppSoftwareVersion != "" || len(build.AppPackages) != 3 {
		t.Fatalf("a check without a version recorded %q %v", build.AppSoftwareVersion, build.AppPackages)
	}
	// A build the token cannot record is the check's answer.
	e.seam.mu.Lock()
	e.seam.buildErr = fmt.Errorf("%w: the database refused the write", substrate.ErrUnavailable)
	e.seam.mu.Unlock()
	wantStatus(t, e.do(http.MethodPost, "/api/v1/authorizations/check", app, []byte(body), "application/json"), http.StatusServiceUnavailable)
}

// A DOCUMENT-PUBLISHED PACKAGE ROW NAMES ITS PUBLISHER on the approval page,
// with the version approving would install, and a package nothing publishes
// blocks the approval in the catalog's words.
func TestTheApprovalPageShowsADocumentPublishedPackage(t *testing.T) {
	t.Parallel()
	e, _ := publishingEnv(t)
	id := e.park(setupRequest("write:example.org/training/*", `[{"type":"package","package":"`+dpTraining+`","version":1}]`))
	v := e.view(id)
	row := v.Setup[0]
	if row.Action != substrate.SetupInstall || row.Blocked != nil || row.Package.Shipped != 3 || row.Package.Held != 0 ||
		row.Package.Tier != substrate.TierPublished || row.Package.Description != "Strength sessions." {
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
	rec := e.approveWith(id, []string{"read:" + e.db.repository + "/notes/*"}, v.PlanHash)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "not published") {
		t.Fatalf("approving a blocked row: %d %s", rec.Code, rec.Body.String())
	}
	if got := e.installs(); len(got) != 0 {
		t.Fatalf("a refused approval installed %v", got)
	}
}

// AN AGENT IS NO CHANNEL TO MORE THAN A CALLER READS (decision 0214, for
// every agent): an app's `agents:*` reaches an agent, its row and its
// threads only when its own read scopes cover every kind the agent reads,
// exact kinds and globs alike, an internal kind only by its exact scope.
func TestAnAppReachesAnAgentOnlyWhenItReadsEverythingTheAgentReads(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withSamples("tasks"))
	repo := e.db.repository
	const people = "samples.substrate.reamde.dev/people/*"
	var (
		coachPkg   = repo + "/trainingcoach"
		coach      = coachPkg + "/coach"
		ledger     = repo + "/money/ledger"
		wideAgent  = coachPkg + "/librarian"
		innerAgent = coachPkg + "/auditor"
		thread     = llmPackage + "/thread"
	)
	agent := func(id, reads string) string {
		return `---
kind: substrate.reamde.dev/core/agent
metadata:
  id: ` + id + `
data:
  purpose: primary
  authority: ` + repo + `
  package: trainingcoach
  description: Coaches.
  prompt: Coach.
  provider: openai
  model: gpt-5
  budgets:
    maxTurns: 2
  permissions:
    reads:
      kinds: [` + reads + `]
    writes: []
`
	}
	// The ledger is internal, as the substrate's own kinds are, so a glob
	// never reads it whole.
	e.db.declare(t, `kind: substrate.reamde.dev/core/package
metadata:
  id: `+repo+`/money
data:
  authority: `+repo+`
  package: money
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: `+ledger+`
data:
  authority: `+repo+`
  package: money
  description: A ledger.
  purpose: internal
  names:
    singular: ledger
  properties:
    total:
      type: int
      description: what it holds
---
kind: substrate.reamde.dev/core/package
metadata:
  id: `+coachPkg+`
data:
  authority: `+repo+`
  package: trainingcoach
  version: 1
`+agent(coach, personKind+", "+taskKind)+agent(wideAgent, `"`+people+`"`)+agent(innerAgent, ledger))
	e.db.put(t, thread, "coachthread", map[string]any{
		"agent": map[string]any{"ref": corePackage + "/agent/" + coach}, "provider": "openai", "model": "gpt-5",
		"mode": "manual", "status": "ok", "agentDepth": 0, "startedAt": "2026-11-01T09:00:00Z",
	})
	get := func(tok, kind, id string) int {
		return e.do(http.MethodGet, "/api/v1/"+kind+"/"+url.PathEscape(id), tok, nil, "").Code
	}
	// The owner reads every one of them: the refusals below are the grant's.
	for _, id := range []string{coach, wideAgent, innerAgent} {
		if code := get(e.owner, corePackage+"/agent", id); code != http.StatusOK {
			t.Fatalf("the owner reads agent %s: %d", id, code)
		}
	}
	notes := e.grant("agents:* read:~/notes/*", []string{"agents:*", "read:" + repo + "/notes/*"})
	if code := get(notes, corePackage+"/agent", coach); code == http.StatusOK {
		t.Errorf("an app that reads only its notes reads the coach's row: %d", code)
	}
	if code := get(notes, thread, "coachthread"); code == http.StatusOK {
		t.Errorf("an app that reads only its notes reads the coach's thread: %d", code)
	}
	body, _ := json.Marshal(map[string]any{"message": "List my medications."})
	if rec := e.do(http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(coach)+"/chat", notes, body, "application/json"); rec.Code != http.StatusNotFound {
		t.Errorf("an app that reads only its notes chats with the coach: %d %s", rec.Code, rec.Body.String())
	}
	exact := e.grant("agents:* read:"+personKind+" read:"+taskKind, []string{"agents:*", "read:" + personKind, "read:" + taskKind})
	if code := get(exact, corePackage+"/agent", coach); code != http.StatusOK {
		t.Errorf("an app reading exactly what the coach reads does not reach it: %d", code)
	}
	if code := get(exact, thread, "coachthread"); code != http.StatusOK {
		t.Errorf("an app reading exactly what the coach reads does not read its thread: %d", code)
	}
	// A glob the agent reads is covered by a glob at least as wide, never by
	// one kind in it.
	if code := get(exact, corePackage+"/agent", wideAgent); code == http.StatusOK {
		t.Errorf("one kind of people covers an agent reading all of people: %d", code)
	}
	wide := e.grant("agents:* read:samples.substrate.reamde.dev/*", []string{"agents:*", "read:samples.substrate.reamde.dev/*"})
	if code := get(wide, corePackage+"/agent", wideAgent); code != http.StatusOK {
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
