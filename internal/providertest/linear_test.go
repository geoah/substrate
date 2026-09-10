package providertest

// The Linear bundle — sync-only issue mirroring with a jointly-owned task
// projection. Three proofs, from the shipped closure at.
// ./../kinds/providers.substrate.reamde.dev/linear:
//
//  1. TestLinearBundleAdmitsSchema — the closure ADMITS through the schema
//     loader: the bundle declares the `client` input (facility-read, never
//     injected) the oauth2 block names, the config kinds wear the right
//     host-recognized traits
//     (oauth2 on the config, accountconfig on the account), the
//     bundle's trusted oauth2 manifest metadata compiles (Linear endpoints +
//     the enabledIssues→read scope map), the mirror types carry their subject
//     edges, both →person mappings type-check, the install-closure balances.
//     No DB, no uv — pure schema admission.
//
//  2. TestLinearBundleInstalls — the whole closure installs into a live
//     repository and every member (bundle, five types, two functions, two
//     mappings, three triggers) lands. This warms the PEP 723 sync body
//     through uv, so it SKIPS when uv is absent or cannot provision.
//
//  3. TestLinearBundleFakeSyncJointOwnership — the end-to-end sync against
//     LOOPBACK fakes (an OAuth provider in a box + a GraphQL stub, never the
//     live API): the on-connect trigger drains a two-page assignedIssues read
//     off the causal chain, the mirrors and the matched person land, tasks
//     mint, and the v4 read-diff-patch policy for JOINT OWNERSHIP holds —
//     a task the owner marked done survives an idle re-sync untouched
//     (upstream unchanged is not news), survives an OPEN-FAMILY upstream
//     transition too (started → backlog → started folds to the same "open":
//     churn between open columns must never reopen an owner-done task — the
//     fleet review's folded-baseline regression), while an issue completed
//     upstream still closes its task. Provider-owned edge hygiene holds as
//     well: an issue moved to another team ends with exactly ONE team edge
//     (the new team — the single-edge link replaces), and an issue that
//     lost its team upstream gets its stale edge unlinked. The bundle stays
//     sync-only: the stub refuses every GraphQL mutation and the test
//     asserts none arrived.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	linearDir         = providersDir + "/linear"
	linearPackage     = "providers.substrate.reamde.dev/linear"
	linearConfigType  = linearPackage + "/config"
	linearAccountType = linearPackage + "/account"
	linearUserType    = linearPackage + "/user"
	linearTeamType    = linearPackage + "/team"
	linearIssueType   = linearPackage + "/issue"
	linearSyncFn      = linearPackage + "/issuessync"
	linearProjFn      = linearPackage + "/taskprojection"

	linearPersonType = "samples.substrate.reamde.dev/people/person"
	linearTaskType   = "samples.substrate.reamde.dev/tasks/task"

	// The sync body's live GraphQL endpoint — the exact string the fake-API
	// fixture substitutes for its loopback stub.
	linearLiveGraphQL = "https://api.linear.app/graphql"

	linearViewerEmail = "geo@linear.example"
	// linearIssueATitle is issue A's heading upstream, and what the tasks
	// sample's mapping projects onto the task it mints.
	linearIssueATitle = "Fix the flux capacitor"
)

// TestLinearBundleAdmitsSchema loads the builtin schema, then installs the
// bundle closure on top of it through the ordinary loader/resolver — the same
// admission the batch apply runs, minus the function-body warm. Every
// assertion is a rule the loader enforces at admission time.
func TestLinearBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	reg := bundleRegistry(t, linearDir)

	// The bundle exists, declares the `client` input the oauth2 block names
	// (facility-read, never injected), and carries the TRUSTED
	// oauth2 provider metadata: Linear's endpoints and the
	// enabledIssues→read scope map live on the immutable install artifact.
	b := assertBundleInput(t, reg, linearPackage, "client", linearConfigType, "")
	if b.OAuth2 == nil {
		t.Fatal("the bundle compiled no oauth2 manifest metadata")
	}
	if b.OAuth2.ClientInput != "client" {
		t.Fatalf("oauth2 clientInput = %q, want %q", b.OAuth2.ClientInput, "client")
	}
	if b.OAuth2.AuthorizationEndpoint != "https://linear.app/oauth/authorize" ||
		b.OAuth2.TokenEndpoint != "https://api.linear.app/oauth/token" {
		t.Fatalf("oauth2 endpoints wrong: %+v", b.OAuth2)
	}
	if scopes := b.OAuth2.FeatureScopes["enabledIssues"]; len(scopes) != 1 || scopes[0] != "read" {
		t.Fatalf("enabledIssues scope map = %v, want [read]", scopes)
	}

	// The config type: oauth2 (client fields), the client input's kind.
	cfg := mustKind(t, reg, linearConfigType)
	if !cfg.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("config type does not implement %s", vocabulary.TraitOAuth2Core)
	}

	// The account type: accountconfig, and NOT oauth2 — client creds bind on
	// the config, tokens on the account.
	acct := mustKind(t, reg, linearAccountType)
	if !acct.Implements(vocabulary.TraitAccountConfigCore) {
		t.Fatalf("account type does not implement %s", vocabulary.TraitAccountConfigCore)
	}
	if acct.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("account type implements oauth2 — client creds belong on the config, not the account")
	}

	// The mirror types carry their subject SLOTS: single, unpinned and
	// optional, because the kind they reach is the repository's to choose
	// (record 49). The issue's team edge is an ordinary pinned reference.
	user := mustKind(t, reg, linearUserType)
	if ed, ok := user.Prop("person"); !ok || ed.To != "" || ed.Required || ed.Repeated || !ed.Subject {
		t.Fatalf("user person slot shape wrong: %+v (ok=%v)", ed, ok)
	}
	issue := mustKind(t, reg, linearIssueType)
	if ed, ok := issue.Prop("assignee"); !ok || ed.To != "" || ed.Required || ed.Repeated || !ed.Subject {
		t.Fatalf("issue assignee slot shape wrong: %+v (ok=%v)", ed, ok)
	}
	if ed, ok := issue.Prop("team"); !ok || ed.To != linearTeamType || ed.Required {
		t.Fatalf("issue team edge shape wrong: %+v (ok=%v)", ed, ok)
	}

	// The closure ships NO mapping: this package owns no person.
	if ms := reg.MappingsFrom(linearUserType); len(ms) != 0 {
		t.Fatalf("the linear closure ships %d mappings from user; a provider ships none", len(ms))
	}
	if ms := reg.MappingsFrom(linearIssueType); len(ms) != 0 {
		t.Fatalf("the linear closure ships %d mappings from issue; a provider ships none", len(ms))
	}
	if ms := reg.MappingsTo(linearPersonType); len(ms) != 0 {
		t.Fatalf("the linear closure maps onto person: %v", ms)
	}

	// The sync is the package's ONE function: the task projection went with
	// the mappings (record 49), because it wrote a `task` row into a package
	// this one does not own.
	sync, err := reg.ResolveFunction(linearSyncFn)
	if err != nil {
		t.Fatalf("function %s did not register: %v", linearSyncFn, err)
	}
	for _, ident := range sync.Caps.Emit {
		if strings.HasPrefix(ident, enginetest.SampleAuthority+"/") {
			t.Fatalf("issuessync may write %s, a kind this package does not own", ident)
		}
	}
	if _, err := reg.ResolveFunction(linearPackage + "/taskprojection"); err == nil {
		t.Fatal("the closure still ships taskprojection")
	}
}

// TestLinearBundleInstalls applies the whole closure into a live repository and
// asserts every member installs. It warms the PEP 723 sync body through uv,
// so it skips when uv is absent or cannot provision.
func TestLinearBundleInstalls(t *testing.T) {
	t.Parallel()
	requireUV(t)
	_, ds := newDataset(t)

	install(t, ds, linearDir, nil)

	// The bundle row and every schema member landed as its own record.
	assertMembers(t, ds, map[string]string{
		linearPackage:     typeBundle,
		linearConfigType:  typeKind,
		linearAccountType: typeKind,
		linearUserType:    typeKind,
		linearTeamType:    typeKind,
		linearIssueType:   typeKind,
		linearSyncFn:      typeFunction,
	})

	// Computed status: installed, enabled, unconfigured, one function.
	assertUnresolvedInput(t, ds, linearPackage, "client", linearConfigType, 1)

	// The delivery wiring installs as ordinary data records.
	installTriggers(t, ds, linearDir)
	assertTriggers(t, ds,
		"linear-issues-on-connect", "linear-issues-scheduled",
	)
}

// linearFakeProvider is Linear's OAuth half in a box: /token answers the code
// exchange (and a refresh, should one fire) with a fixed grant.
type linearFakeProvider struct {
	fakeAPI

	exchanges int
}

func newLinearFakeProvider(t *testing.T) *linearFakeProvider {
	t.Helper()
	p := &linearFakeProvider{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		out := map[string]any{"token_type": "Bearer", "expires_in": 3600}
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
			out["access_token"] = "at-1"
		default:
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	p.serve(t, mux)
	return p
}

// linearFakeAPI is the GraphQL half: a two-page viewer.assignedIssues read,
// the viewer identity on every page, and a hard refusal of any mutation — the
// bundle is sync-only and must never send one. Both issues' workflow states
// and issue B's team are mutable, so a re-sync can replay an upstream
// transition (an open-family drag, a completion, a team move, a team loss)
// against mirrors that already exist.
type linearFakeAPI struct {
	fakeAPI

	pages          int
	mutations      int
	badAuth        int
	issueAState    string // workflow state name
	issueAStateTyp string // workflow state type
	issueBState    string
	issueBStateTyp string
	issueBTeam     map[string]any // nil means the issue lost its team
	// issueATitle is Linear's own heading for issue A, which a mapping
	// projects onto the repository's task: changing it upstream is how the
	// recompute is observed.
	issueATitle string
}

var linearFakeTeamEng = map[string]any{"id": "uuid-t", "key": "ENG", "name": "Engineering"}

func newLinearFakeAPI(t *testing.T) *linearFakeAPI {
	t.Helper()
	f := &linearFakeAPI{
		issueAState: "In Progress", issueAStateTyp: "started",
		issueBState: "Todo", issueBStateTyp: "unstarted",
		issueBTeam: linearFakeTeamEng, issueATitle: linearIssueATitle,
	}
	now := time.Now().UTC().Format(time.RFC3339)
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer at-1" {
			f.badAuth++
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.Contains(req.Query, "mutation") {
			f.mutations++
			http.Error(w, "sync-only: no mutations", http.StatusForbidden)
			return
		}
		f.pages++
		issueA := map[string]any{
			"id": "uuid-a", "identifier": "ENG-1", "title": f.issueATitle,
			"url": "https://linear.app/acme/issue/ENG-1", "dueDate": "2026-09-01",
			"updatedAt": now, "priority": 2.0,
			"state": map[string]any{"name": f.issueAState, "type": f.issueAStateTyp},
			"team":  linearFakeTeamEng, "project": nil,
		}
		issueB := map[string]any{
			"id": "uuid-b", "identifier": "ENG-2", "title": "Write the docs",
			"url":       "https://linear.app/acme/issue/ENG-2",
			"updatedAt": now, "priority": 0.0,
			"state": map[string]any{"name": f.issueBState, "type": f.issueBStateTyp},
			"team":  f.issueBTeam, "project": nil,
		}
		after, _ := req.Variables["after"].(string)
		var nodes []any
		pageInfo := map[string]any{"hasNextPage": false, "endCursor": ""}
		if after == "" {
			nodes = []any{issueA}
			pageInfo = map[string]any{"hasNextPage": true, "endCursor": "cur-1"}
		} else {
			nodes = []any{issueB}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"viewer": map[string]any{
					"id": "uuid-v", "name": "Geo Example", "displayName": "geo",
					"email": linearViewerEmail, "url": "https://linear.app/acme/profiles/geo",
					"assignedIssues": map[string]any{
						"pageInfo": pageInfo,
						"nodes":    nodes,
					},
				},
			},
		})
	})
	f.serve(t, mux)
	return f
}

func (f *linearFakeAPI) pageCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pages
}

func (f *linearFakeAPI) moveIssueA(state, stateType string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issueAState, f.issueAStateTyp = state, stateType
}

func (f *linearFakeAPI) completeIssueB() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issueBState, f.issueBStateTyp = "Done", "completed"
}

// retitleIssueA renames issue A upstream: the heading a mapping projects.
func (f *linearFakeAPI) retitleIssueA(title string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issueATitle = title
}

// moveIssueBTeam reassigns issue B's team; nil drops the team entirely.
func (f *linearFakeAPI) moveIssueBTeam(team map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issueBTeam = team
}

// linearInstallRewired installs the closure with its TRUSTED oauth2
// endpoints and the sync body's GraphQL URL pointed at the loopback fakes: a
// fixture cannot bake a dynamic httptest URL into a static manifest, so the
// substitution lands exactly where the trusted metadata is authored. The
// shipped file itself stays pinned to the live endpoints.
func linearInstallRewired(t *testing.T, ds substrate.Dataset, oauthBase, graphqlURL string) {
	t.Helper()
	install(t, ds, linearDir, func(docs []map[string]any) {
		rewriteOAuthEndpoints(t, docs, oauthBase,
			"https://linear.app/oauth", "https://api.linear.app/oauth")
		rewriteSource(t, docs, linearLiveGraphQL, graphqlURL)
	})
}

// TestLinearBundleFakeSyncMirrors drives the whole connector against loopback
// fakes: install → configure → connect (host OAuth round trip) → on-connect
// backfill (two pages, off the causal chain) → the mirrors, the identity a
// repository-declared mapping resolves, and the provider-owned reference
// hygiene. Mirrors are the whole output since record 49; the joint-ownership
// projection that used to write `task` rows is gone with them, and the tiers
// are what keep an owner edit now (a mapped property recomputes at the machine
// tier and an owner write wins).
func TestLinearBundleFakeSyncMirrors(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	p := newLinearFakeProvider(t)
	api := newLinearFakeAPI(t)
	svc, ds := newOAuthDataset(t, p.ts.Client())

	// Install the closure from the shipped files, endpoints re-pointed at the
	// fakes (loopback http is admissible manifest metadata; the live file
	// stays https-only).
	linearInstallRewired(t, ds, p.ts.URL, api.ts.URL+"/graphql")
	// The closure ships no mapping (record 49): the repository declares how
	// linear's mirrors reach the kinds it owns, and TWO SAMPLES already carry
	// those declarations as suggested mappings: people's `user -> person` and
	// `issue.assignee -> person`, tasks' `issue -> task`. All three were
	// dropped when the samples were imported at open, because this provider
	// was not installed then, so re-importing both closures now is what lands
	// them. That re-import is exactly what the console asks a reader for
	// (decision records 0048 and 0049).
	importVocabulary(t, ds, "people", "tasks")
	installTriggers(t, ds, linearDir)

	// Configure and connect: the client's config record (the sole record
	// resolves the input), pending account, host OAuth
	// round trip against the fake provider. The callback patch (tokenStatus:
	// connected) is the change the on-connect trigger fires on.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind:       linearConfigType,
		Properties: map[string]any{"clientId": "client-1", "clientSecret": "s3cret"},
	}); err != nil {
		t.Fatalf("create config: %v", err)
	}
	account, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: linearAccountType,
		Properties: map[string]any{
			"enabledIssues": true, "syncFrequency": "hourly", "backfillDepth": "all",
		},
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	consent, err := ds.StartOAuth(ctx, substrate.ActorAPI, account.ID)
	if err != nil {
		t.Fatalf("oauth start: %v", err)
	}
	if !strings.HasPrefix(consent, p.ts.URL+"/authorize") {
		t.Fatalf("consent url off the manifest endpoint: %s", consent)
	}
	cu, err := url.Parse(consent)
	if err != nil {
		t.Fatalf("parse consent url: %v", err)
	}
	if scope := cu.Query().Get("scope"); scope != "read" {
		t.Fatalf("requested scope = %q, want read (derived from enabledIssues)", scope)
	}
	completeOAuth(t, svc, cu.Query().Get("state"), "code-123")

	// The backfill: on-connect fires the sync, which drains both pages off
	// the causal chain.
	installResyncHand(t, ds)
	drainTriggers(t, ds)

	issueAID := runner.ExternalID("linear", account.ID, "issue:uuid-a")
	issueBID := runner.ExternalID("linear", account.ID, "issue:uuid-b")
	teamID := runner.ExternalID("linear", account.ID, "team:uuid-t")
	userID := runner.ExternalID("linear", account.ID, "user:uuid-v")

	if n := api.pageCount(); n < 2 {
		t.Fatalf("the paged drain made %d GraphQL reads, want >= 2 (one per page)", n)
	}

	// The issue mirror, in Linear's shape, with its team reference.
	issueA := mustGet(t, ds, linearIssueType, issueAID)
	for k, want := range map[string]any{
		"identifier": "ENG-1", "state": "In Progress", "stateType": "started",
		"priority": "high", "assigneeEmail": linearViewerEmail,
	} {
		if got := issueA.Properties[k]; got != want {
			t.Fatalf("issue mirror %s = %v, want %v", k, got, want)
		}
	}
	if tg := refIDs(issueA, "team"); len(tg) != 1 || tg[0] != teamID {
		t.Fatalf("issue team = %+v, want %s", tg, teamID)
	}
	if got := mustGet(t, ds, linearTeamType, teamID).Properties["name"]; got != "Engineering" {
		t.Fatalf("team mirror name = %v", got)
	}

	// Identity: the viewer's user matched-or-minted a person, and the issue's
	// assignee resolved onto the SAME human.
	user := mustGet(t, ds, linearUserType, userID)
	pe := refIDs(user, "person")
	if len(pe) != 1 {
		t.Fatalf("user person unresolved: %+v", user.Properties)
	}
	personID := pe[0]
	person := mustGet(t, ds, linearPersonType, personID)
	emails, _ := person.Properties["emails"].([]any)
	found := false
	for _, e := range emails {
		if e == linearViewerEmail {
			found = true
		}
	}
	if !found {
		t.Fatalf("the mapped person carries no %s: %v", linearViewerEmail, person.Properties["emails"])
	}
	if ae := refIDs(issueA, "assignee"); len(ae) != 1 || ae[0] != personID {
		t.Fatalf("issue assignee = %+v, want person %s", ae, personID)
	}

	// The completion stamp: lastSyncedAt, syncStatus, and the viewer's email
	// (writer: connector — Linear has no userinfo GET for the facility).
	acct := mustGet(t, ds, linearAccountType, account.ID)
	if acct.Properties["lastSyncedAt"] == nil || acct.Properties["syncStatus"] != "ok" {
		t.Fatalf("account not stamped: %v / %v", acct.Properties["lastSyncedAt"], acct.Properties["syncStatus"])
	}
	if acct.Properties["email"] != linearViewerEmail {
		t.Fatalf("account email = %v, want %s", acct.Properties["email"], linearViewerEmail)
	}

	// NO TASK ROW FROM THIS CLOSURE: `task` belongs to a package the provider
	// does not own, so nothing here writes one. What DOES write one is the
	// tasks sample's own mapping, landed above, and it writes it as the
	// projection of the issue mirror rather than as a row of the sync's.
	taskRefs := refIDs(issueA, "task")
	if len(taskRefs) != 1 {
		t.Fatalf("the issue mirror's task slot = %+v, want the one the mapping minted", taskRefs)
	}
	task := mustGet(t, ds, linearTaskType, taskRefs[0])
	if got := task.Properties["name"]; got != linearIssueATitle {
		t.Fatalf("the mapping did not project the heading: name = %v", got)
	}
	if got := task.Properties["url"]; got != "https://linear.app/acme/issue/ENG-1" {
		t.Fatalf("the mapping did not project the link: url = %v", got)
	}
	// `status` is NOT mapped: a state moves through its transitions and no
	// mapping may name one (record 40), so the minted task sits at the
	// declared initial state and the owner owns it from there.
	if got := task.Properties["status"]; got != "open" {
		t.Fatalf("the minted task's status = %v, want the declared initial open", got)
	}

	// THE OWNER'S EDIT SURVIVES THE SYNC, and Linear's own heading still
	// recomputes. The owner moves the task to `done`; upstream, the issue is
	// retitled. One re-sync later the status is still the owner's and the
	// name is Linear's, which is what the projection's tiers buy: a mapped
	// property recomputes at the machine tier, an owner write wins, and an
	// unmapped one is never touched at all.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, linearTaskType, task.ID, substrate.PatchInput{
		Properties: map[string]any{"status": "done"},
	}); err != nil {
		t.Fatalf("the owner could not close the projected task: %v", err)
	}
	const retitled = "Fix the flux capacitor, properly"
	api.retitleIssueA(retitled)
	resyncAccount(t, ds, linearAccountType, account.ID)
	after := mustGet(t, ds, linearTaskType, task.ID)
	if got := after.Properties["status"]; got != "done" {
		t.Fatalf("the re-sync took the owner's status back: %v", got)
	}
	if got := after.Properties["name"]; got != retitled {
		t.Fatalf("the re-sync did not recompute the mapped heading: %v", got)
	}
	if after.Properties["completedAt"] == nil {
		t.Error("the done transition stamped no completedAt on the projected task")
	}

	// An idle re-sync writes nothing: the mirrors are patched, never re-put
	// whole, so no-op suppression holds end to end.
	beforeVersion := mustGet(t, ds, linearIssueType, issueAID).Version
	resyncAccount(t, ds, linearAccountType, account.ID)
	if got := mustGet(t, ds, linearIssueType, issueAID).Version; got != beforeVersion {
		t.Fatalf("an idle re-sync rewrote the issue mirror: version %d -> %d",
			beforeVersion, got)
	}

	// Provider-owned edge hygiene: issue B moves to another team. `team` is
	// a SINGLE reference, so the sync's re-write must leave exactly ONE target
	// — the new team — never an accumulated pair.
	team2ID := runner.ExternalID("linear", account.ID, "team:uuid-t2")
	api.moveIssueBTeam(map[string]any{"id": "uuid-t2", "key": "OPS", "name": "Operations"})
	resyncAccount(t, ds, linearAccountType, account.ID)
	movedB := mustGet(t, ds, linearIssueType, issueBID)
	if tg := refIDs(movedB, "team"); len(tg) != 1 || tg[0] != team2ID {
		t.Fatalf("team move did not stay current: team=%+v, want exactly %s", tg, team2ID)
	}
	if got := mustGet(t, ds, linearTeamType, team2ID).Properties["name"]; got != "Operations" {
		t.Fatalf("the new team did not mirror: name=%v", got)
	}
	// ...and an issue that LOST its team upstream sheds the stale pointer: the
	// sync writes the property null, which is what clears a reference.
	api.moveIssueBTeam(nil)
	resyncAccount(t, ds, linearAccountType, account.ID)
	if tg := refIDs(mustGet(t, ds, linearIssueType, issueBID), "team"); len(tg) != 0 {
		t.Fatalf("a stale team reference survived the team's removal: %+v", tg)
	}

	// And NOTHING went back to Linear: sync-only means zero mutations.
	api.mu.Lock()
	mutations, badAuth := api.mutations, api.badAuth
	api.mu.Unlock()
	if mutations != 0 {
		t.Fatalf("the sync sent %d GraphQL mutations — the bundle is sync-only", mutations)
	}
	if badAuth != 0 {
		t.Fatalf("%d GraphQL reads arrived without the resolved bearer token", badAuth)
	}
}
