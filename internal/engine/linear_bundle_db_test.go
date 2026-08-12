package engine

// The Linear bundle — sync-only issue mirroring with a jointly-owned task
// projection. Three proofs, from the shipped closure at.
// ./../kinds/linear.bundles.substrate.reamde.dev:
//
//  1. TestLinearBundleAdmitsSchema — the closure ADMITS through the schema
//     loader: the config types wear the right host-recognized traits
//     (bundleconfig+oauth2 on the config, accountconfig on the account), the
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
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/runner/substratefn"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	linearExampleDir   = "../../kinds/linear.bundles.substrate.reamde.dev"
	linearAuthority    = "linear.bundles.substrate.reamde.dev"
	linearBundleRow    = linearAuthority + "/linear"
	linearConfigType   = linearAuthority + "/config"
	linearAccountType  = linearAuthority + "/account"
	linearUserType     = linearAuthority + "/user"
	linearTeamType     = linearAuthority + "/team"
	linearIssueType    = linearAuthority + "/issue"
	linearSyncFn       = linearAuthority + "/issuessync"
	linearProjFn       = linearAuthority + "/taskprojection"
	linearUserMapping  = linearAuthority + "/userperson"
	linearIssueMapping = linearAuthority + "/issueperson"

	linearPersonType = "people.substrate.reamde.dev/person"
	linearTaskType   = "tasks.substrate.reamde.dev/task"

	// The sync body's live GraphQL endpoint — the exact string the fake-API
	// fixture substitutes for its loopback stub.
	linearLiveGraphQL = "https://api.linear.app/graphql"

	linearViewerEmail = "geo@linear.example"
)

// TestLinearBundleAdmitsSchema loads the builtin schema, then installs the
// bundle closure on top of it through the ordinary loader/resolver — the same
// admission the batch apply runs, minus the function-body warm. Every
// assertion is a rule the loader enforces at admission time.
func TestLinearBundleAdmitsSchema(t *testing.T) {
	// The registry an install actually admits into: the seeded tree (core
	// alone) plus the shipped VOCABULARY bundles this repository imported —
	// what a closure declaring onto people/tasks/messaging/calendar/media
	// needs present, and what `requires:` names.
	reg, err := enginetest.SeededRegistry("../../kinds/core.substrate.reamde.dev")
	if err != nil {
		t.Fatalf("build the repository registry: %v", err)
	}
	data, err := os.ReadFile(linearExampleDir + "/bundle.yaml")
	if err != nil {
		t.Fatalf("read bundle.yaml: %v", err)
	}
	docs, err := vocabulary.ParseStream(data)
	if err != nil {
		t.Fatalf("parse bundle.yaml: %v", err)
	}
	authorities, err := vocabulary.BuildAuthorities(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("build the bundle authority: %v", err)
	}
	if err := reg.InstallAll(authorities); err != nil {
		t.Fatalf("the bundle closure did not admit: %v", err)
	}

	// The bundle exists, names its config type, and carries the TRUSTED
	// oauth2 provider metadata: Linear's endpoints and the
	// enabledIssues→read scope map live on the immutable install artifact.
	b, ok := reg.BundleOf(linearAuthority)
	if !ok {
		t.Fatalf("no bundle owns %s after install", linearAuthority)
	}
	if b.ConfigType != linearConfigType {
		t.Fatalf("bundle configType = %q, want %q", b.ConfigType, linearConfigType)
	}
	if b.OAuth2 == nil {
		t.Fatal("the bundle compiled no oauth2 manifest metadata")
	}
	if b.OAuth2.AuthorizationEndpoint != "https://linear.app/oauth/authorize" ||
		b.OAuth2.TokenEndpoint != "https://api.linear.app/oauth/token" {
		t.Fatalf("oauth2 endpoints wrong: %+v", b.OAuth2)
	}
	if scopes := b.OAuth2.FeatureScopes["enabledIssues"]; len(scopes) != 1 || scopes[0] != "read" {
		t.Fatalf("enabledIssues scope map = %v, want [read]", scopes)
	}

	// The config type: bundleconfig (host singleton) + oauth2 (client fields).
	cfg, ok := reg.ByIdentity(linearConfigType)
	if !ok {
		t.Fatalf("config type %s missing", linearConfigType)
	}
	if !cfg.Implements(vocabulary.TraitBundleConfigCore) {
		t.Fatalf("config type does not implement %s", vocabulary.TraitBundleConfigCore)
	}
	if !cfg.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("config type does not implement %s", vocabulary.TraitOAuth2Core)
	}

	// The account type: accountconfig, and NOT oauth2 — client creds bind on
	// the config, tokens on the account.
	acct, ok := reg.ByIdentity(linearAccountType)
	if !ok {
		t.Fatalf("account type %s missing", linearAccountType)
	}
	if !acct.Implements(vocabulary.TraitAccountConfigCore) {
		t.Fatalf("account type does not implement %s", vocabulary.TraitAccountConfigCore)
	}
	if acct.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("account type implements oauth2 — client creds belong on the config, not the account")
	}

	// The mirror types carry their subject edges — required, single, at
	// person — and the issue's team edge points at the team mirror.
	user, ok := reg.ByIdentity(linearUserType)
	if !ok {
		t.Fatalf("mirror type %s missing", linearUserType)
	}
	if ed, ok := user.Edge("person"); !ok || ed.To != linearPersonType || !ed.Required || ed.Many {
		t.Fatalf("user person edge shape wrong: %+v (ok=%v)", ed, ok)
	}
	issue, ok := reg.ByIdentity(linearIssueType)
	if !ok {
		t.Fatalf("mirror type %s missing", linearIssueType)
	}
	if ed, ok := issue.Edge("assignee"); !ok || ed.To != linearPersonType || !ed.Required || ed.Many {
		t.Fatalf("issue assignee edge shape wrong: %+v (ok=%v)", ed, ok)
	}
	if ed, ok := issue.Edge("team"); !ok || ed.To != linearTeamType || ed.Required {
		t.Fatalf("issue team edge shape wrong: %+v (ok=%v)", ed, ok)
	}

	// Both mappings resolved: user→person on the person edge, issue→person on
	// the assignee edge, each probing an email.
	um, ok := reg.MappingFor(linearUserType)
	if !ok {
		t.Fatalf("no mapping registered from %s", linearUserType)
	}
	if um.To != linearPersonType || um.Edge != "person" || len(um.Match) == 0 {
		t.Fatalf("user mapping resolves wrong: to=%q edge=%q match=%d", um.To, um.Edge, len(um.Match))
	}
	im, ok := reg.MappingFor(linearIssueType)
	if !ok {
		t.Fatalf("no mapping registered from %s", linearIssueType)
	}
	if im.To != linearPersonType || im.Edge != "assignee" || len(im.Match) == 0 {
		t.Fatalf("issue mapping resolves wrong: to=%q edge=%q match=%d", im.To, im.Edge, len(im.Match))
	}

	// Both functions are members of the authority.
	for _, fn := range []string{linearSyncFn, linearProjFn} {
		if _, err := reg.ResolveFunction(fn); err != nil {
			t.Fatalf("function %s did not register: %v", fn, err)
		}
	}
}

// TestLinearBundleInstalls applies the whole closure into a live repository and
// asserts every member installs. It warms the PEP 723 sync body through uv,
// so it skips when uv is absent or cannot provision.
func TestLinearBundleInstalls(t *testing.T) {
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the sync body warms through uv at install")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)

	vocabularyDocs := loadYAMLDocs(t, linearExampleDir+"/bundle.yaml")
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, vocabularyDocs); err != nil {
		if isUVProvisionError(err) {
			t.Skipf("bundle install could not warm the PEP 723 body (uv offline?): %v", err)
		}
		t.Fatalf("install the linear bundle: %v", err)
	}

	// The bundle row and every schema member landed as its own record.
	for id, wantType := range map[string]string{
		linearBundleRow:    "core.substrate.reamde.dev/bundle",
		linearConfigType:   "core.substrate.reamde.dev/kind",
		linearAccountType:  "core.substrate.reamde.dev/kind",
		linearUserType:     "core.substrate.reamde.dev/kind",
		linearTeamType:     "core.substrate.reamde.dev/kind",
		linearIssueType:    "core.substrate.reamde.dev/kind",
		linearSyncFn:       "core.substrate.reamde.dev/function",
		linearProjFn:       "core.substrate.reamde.dev/function",
		linearUserMapping:  "core.substrate.reamde.dev/recordmapping",
		linearIssueMapping: "core.substrate.reamde.dev/recordmapping",
	} {
		row, err := ds.Get(ctx, wantType, id)
		if err != nil {
			t.Fatalf("member %s did not install: %v", id, err)
		}
		if row.Kind != wantType {
			t.Fatalf("member %s is a %s, want %s", id, row.Kind, wantType)
		}
	}

	// Computed status: installed, enabled, unconfigured, both functions.
	st, err := ds.BundleStatus(ctx, linearAuthority)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if !st.Installed || !st.Enabled {
		t.Fatalf("bundle not live: installed=%v enabled=%v", st.Installed, st.Enabled)
	}
	if st.Configured {
		t.Fatalf("bundle reports configured with no config record created")
	}
	if st.ConfigType != linearConfigType {
		t.Fatalf("status configType = %q, want %q", st.ConfigType, linearConfigType)
	}
	if st.Functions != 2 {
		t.Fatalf("status functions = %d, want 2", st.Functions)
	}

	// The delivery wiring installs as ordinary data records.
	for _, m := range loadYAMLDocs(t, linearExampleDir+"/triggers.yaml") {
		putDataDoc(t, ds, m)
	}
	for _, id := range []string{
		"linear-issues-on-connect", "linear-issues-scheduled", "linear-task-projection",
	} {
		row, err := ds.Get(ctx, typeTrigger, id)
		if err != nil {
			t.Fatalf("trigger %s did not install: %v", id, err)
		}
		if row.Kind != typeTrigger {
			t.Fatalf("trigger %s is a %s", id, row.Kind)
		}
	}
}

// linearFakeProvider is Linear's OAuth half in a box: /token answers the code
// exchange (and a refresh, should one fire) with a fixed grant.
type linearFakeProvider struct {
	ts *httptest.Server

	mu        sync.Mutex
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
	p.ts = httptest.NewServer(mux)
	t.Cleanup(p.ts.Close)
	return p
}

// linearFakeAPI is the GraphQL half: a two-page viewer.assignedIssues read,
// the viewer identity on every page, and a hard refusal of any mutation — the
// bundle is sync-only and must never send one. Both issues' workflow states
// and issue B's team are mutable, so a re-sync can replay an upstream
// transition (an open-family drag, a completion, a team move, a team loss)
// against mirrors that already exist.
type linearFakeAPI struct {
	ts *httptest.Server

	mu             sync.Mutex
	pages          int
	mutations      int
	badAuth        int
	issueAState    string // workflow state name
	issueAStateTyp string // workflow state type
	issueBState    string
	issueBStateTyp string
	issueBTeam     map[string]any // nil means the issue lost its team
}

var linearFakeTeamEng = map[string]any{"id": "uuid-t", "key": "ENG", "name": "Engineering"}

func newLinearFakeAPI(t *testing.T) *linearFakeAPI {
	t.Helper()
	f := &linearFakeAPI{
		issueAState: "In Progress", issueAStateTyp: "started",
		issueBState: "Todo", issueBStateTyp: "unstarted",
		issueBTeam: linearFakeTeamEng,
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
			"id": "uuid-a", "identifier": "ENG-1", "title": "Fix the flux capacitor",
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
	f.ts = httptest.NewServer(mux)
	t.Cleanup(f.ts.Close)
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

// moveIssueBTeam reassigns issue B's team; nil drops the team entirely.
func (f *linearFakeAPI) moveIssueBTeam(team map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issueBTeam = team
}

// linearOpenDataset opens a throwaway repository with the OAuth facility and the
// credential key ON (openInternalDataset, plus the two options the connect
// flow needs), returning the concrete service for its callback seam.
func linearOpenDataset(t *testing.T, client *http.Client) (*service, *dataset) {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	svc, err := Open(ctx, dsn,
		WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		WithOAuth("test-state-key", "https://substrate.example/api/v1/core.substrate.reamde.dev/oauth/callback", client),
		WithCredentialKey("test-cred-key"))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	s, ok := svc.(*service)
	if !ok {
		t.Fatalf("service is a %T", svc)
	}
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	d, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	ds, ok := d.(*dataset)
	if !ok {
		t.Fatalf("dataset is a %T", d)
	}
	importVocabulary(t, ds)
	return s, ds
}

// linearPointAt rewrites the closure's TRUSTED oauth2 endpoints and the sync
// body's GraphQL URL at the loopback fakes — a fixture cannot bake a dynamic
// httptest URL into a static manifest, so it injects them exactly where the
// trusted metadata is authored (the mbPointOAuthAt pattern). The shipped file
// itself stays pinned to the live endpoints.
func linearPointAt(t *testing.T, docs []map[string]any, oauthBase, graphqlURL string) {
	t.Helper()
	var oauthDone, sourceDone bool
	for _, d := range docs {
		data, _ := d["data"].(map[string]any)
		if d["kind"] == vocabulary.CoreKind(vocabulary.DocBundle) {
			o, _ := data["oauth2"].(map[string]any)
			if o == nil {
				t.Fatal("the bundle document carries no oauth2 block")
			}
			o["authorizationEndpoint"] = oauthBase + "/authorize"
			o["tokenEndpoint"] = oauthBase + "/token"
			o["revocationEndpoint"] = oauthBase + "/revoke"
			oauthDone = true
		}
		meta, _ := d["metadata"].(map[string]any)
		if d["kind"] == vocabulary.CoreKind(vocabulary.DocFunction) && meta["id"] == linearSyncFn {
			src, _ := data["source"].(string)
			if !strings.Contains(src, linearLiveGraphQL) {
				t.Fatalf("the sync body no longer names %s — the fixture substitution broke", linearLiveGraphQL)
			}
			data["source"] = strings.ReplaceAll(src, linearLiveGraphQL, graphqlURL)
			sourceDone = true
		}
	}
	if !oauthDone || !sourceDone {
		t.Fatalf("closure rewrite incomplete: oauth=%v source=%v", oauthDone, sourceDone)
	}
}

func linearGet(t *testing.T, ds *dataset, typ, id string) *substrate.Record {
	t.Helper()
	e, err := ds.Get(context.Background(), typ, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return e
}

// linearResync clears the account's completion marker under a connector
// actor (`lastSyncedAt` is writer: connector — only bundle code may touch
// it) and drains: the removal is itself an account update the on-connect
// guard now matches, so the one patch both resets the guard and fires the
// re-sync. The actor is the PROJECTION function, not the sync — a callable
// never sees its own writes (the dispatcher's self-exclusion), so a clear
// stamped as the sync's own actor would be skipped, not delivered.
func linearResync(t *testing.T, ds *dataset, accountID string) {
	t.Helper()
	syncActor := substrate.FunctionActor(vocabulary.KindName(linearProjFn))
	if _, err := ds.Patch(context.Background(), syncActor, linearAccountType, accountID, substrate.PatchInput{
		Properties: map[string]any{"lastSyncedAt": nil},
	}); err != nil {
		t.Fatalf("clear lastSyncedAt: %v", err)
	}
	drainTriggers(t, ds)
}

// TestLinearBundleFakeSyncJointOwnership drives the whole connector against
// loopback fakes: install → configure → connect (host OAuth round trip) →
// on-connect backfill (two pages, off the causal chain) → mirrors + person +
// tasks — then the joint-ownership regressions the projection exists for.
func TestLinearBundleFakeSyncJointOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the sync body runs through uv")
	}
	ctx := context.Background()
	p := newLinearFakeProvider(t)
	api := newLinearFakeAPI(t)
	svc, ds := linearOpenDataset(t, p.ts.Client())

	// Install the closure from the shipped files, endpoints re-pointed at the
	// fakes (loopback http is admissible manifest metadata; the live file
	// stays https-only).
	docs := loadYAMLDocs(t, linearExampleDir+"/bundle.yaml")
	linearPointAt(t, docs, p.ts.URL, api.ts.URL+"/graphql")
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		if isUVProvisionError(err) {
			t.Skipf("bundle install could not warm the PEP 723 body (uv offline?): %v", err)
		}
		t.Fatalf("install the linear bundle: %v", err)
	}
	for _, m := range loadYAMLDocs(t, linearExampleDir+"/triggers.yaml") {
		putDataDoc(t, ds, m)
	}

	// Configure and connect: config singleton, pending account, host OAuth
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
	if _, err := svc.CompleteOAuth(ctx, cu.Query().Get("state"), "code-123"); err != nil {
		t.Fatalf("oauth callback: %v", err)
	}

	// The backfill: on-connect fires the sync, which drains both pages off
	// the causal chain, and each mirror write flows through the projection.
	drainTriggers(t, ds)

	issueAID := substratefn.ExternalID("linear", account.ID, "issue:uuid-a")
	issueBID := substratefn.ExternalID("linear", account.ID, "issue:uuid-b")
	teamID := substratefn.ExternalID("linear", account.ID, "team:uuid-t")
	userID := substratefn.ExternalID("linear", account.ID, "user:uuid-v")
	taskAID := substratefn.ExternalID("linear-task", account.ID, "uuid-a")
	taskBID := substratefn.ExternalID("linear-task", account.ID, "uuid-b")

	if n := api.pageCount(); n < 2 {
		t.Fatalf("the paged drain made %d GraphQL reads, want >= 2 (one per page)", n)
	}

	// The issue mirror, in Linear's shape, with its team edge and the adopted
	// projection baseline.
	issueA := linearGet(t, ds, linearIssueType, issueAID)
	for k, want := range map[string]any{
		"identifier": "ENG-1", "state": "In Progress", "stateType": "started",
		"priority": "high", "assigneeEmail": linearViewerEmail, "projectedState": "started",
	} {
		if got := issueA.Properties[k]; got != want {
			t.Fatalf("issue mirror %s = %v, want %v", k, got, want)
		}
	}
	if tg := issueA.Edges["team"]; len(tg) != 1 || tg[0].ID != teamID {
		t.Fatalf("issue team edge = %+v, want %s", tg, teamID)
	}
	if got := linearGet(t, ds, linearTeamType, teamID).Properties["name"]; got != "Engineering" {
		t.Fatalf("team mirror name = %v", got)
	}

	// Identity: the viewer's user matched-or-minted a person, and the
	// issue's assignee edge resolved onto the SAME human.
	user := linearGet(t, ds, linearUserType, userID)
	pe := user.Edges["person"]
	if len(pe) != 1 {
		t.Fatalf("user person edge unresolved: %+v", user.Edges)
	}
	personID := pe[0].ID
	person := linearGet(t, ds, linearPersonType, personID)
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
	if ae := issueA.Edges["assignee"]; len(ae) != 1 || ae[0].ID != personID {
		t.Fatalf("issue assignee edge = %+v, want person %s", ae, personID)
	}

	// The projection minted both tasks open, titled off the issues.
	taskA := linearGet(t, ds, linearTaskType, taskAID)
	if taskA.Kind != linearTaskType {
		t.Fatalf("task A is a %s, want %s", taskA.Kind, linearTaskType)
	}
	if taskA.Properties["status"] != "open" || taskA.Title != "Fix the flux capacitor" {
		t.Fatalf("task A wrong: status=%v title=%q", taskA.Properties["status"], taskA.Title)
	}
	if linearGet(t, ds, linearTaskType, taskBID).Properties["status"] != "open" {
		t.Fatal("task B did not mint open")
	}

	// The completion stamp: lastSyncedAt, syncStatus, and the viewer's email
	// (writer: connector — Linear has no userinfo GET for the facility).
	acct := linearGet(t, ds, linearAccountType, account.ID)
	if acct.Properties["lastSyncedAt"] == nil || acct.Properties["syncStatus"] != "ok" {
		t.Fatalf("account not stamped: %v / %v", acct.Properties["lastSyncedAt"], acct.Properties["syncStatus"])
	}
	if acct.Properties["email"] != linearViewerEmail {
		t.Fatalf("account email = %v, want %s", acct.Properties["email"], linearViewerEmail)
	}

	// JOINT OWNERSHIP, half one: the owner ticks task A off; an idle re-sync
	// (upstream still `started`, exactly as last seen) must NOT reopen it —
	// "Linear still says started" is not news (the v4 policy this projection
	// ports). The idle mirrors are no-op-suppressed patches, so the baseline
	// never moves and the task is never touched.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, linearTaskType, taskAID, substrate.PatchInput{
		Properties: map[string]any{"status": "done"},
	}); err != nil {
		t.Fatalf("owner completes task A: %v", err)
	}
	linearResync(t, ds, account.ID)
	if got := linearGet(t, ds, linearTaskType, taskAID).Properties["status"]; got != "done" {
		t.Fatalf("an idle re-sync clobbered the owner's done: status=%v", got)
	}
	if linearGet(t, ds, linearAccountType, account.ID).Properties["lastSyncedAt"] == nil {
		t.Fatal("the re-sync did not run (no fresh completion stamp)")
	}

	// Half one-and-a-half, the fleet review's folded-baseline regression: an
	// OPEN-FAMILY transition upstream (started → backlog) changes the raw
	// stateType but folds to the same "open" — churn between open columns is
	// not news, and must NOT reopen the task the owner completed. (Compared
	// unfolded, baseline "started" != "backlog" would read as a departure
	// and clobber the owner's done with upstream's open.)
	api.moveIssueA("Backlog", "backlog")
	linearResync(t, ds, account.ID)
	if got := linearGet(t, ds, linearTaskType, taskAID).Properties["status"]; got != "done" {
		t.Fatalf("a backlog move reopened the owner-done task: status=%v", got)
	}
	movedA := linearGet(t, ds, linearIssueType, issueAID)
	if movedA.Properties["stateType"] != "backlog" || movedA.Properties["projectedState"] != "backlog" {
		t.Fatalf("the mirror did not adopt the open-family move: stateType=%v baseline=%v",
			movedA.Properties["stateType"], movedA.Properties["projectedState"])
	}
	// ...and the reverse hop (backlog → started) against the fresh backlog
	// baseline is the review's exact headline case.
	api.moveIssueA("In Progress", "started")
	linearResync(t, ds, account.ID)
	if got := linearGet(t, ds, linearTaskType, taskAID).Properties["status"]; got != "done" {
		t.Fatalf("backlog→started reopened the owner-done task: status=%v", got)
	}

	// Half two: the issue actually completing upstream IS news — the state
	// departs from the adopted baseline, so the projection moves the task to
	// done (under its if_version guard) and re-stamps the baseline.
	api.completeIssueB()
	linearResync(t, ds, account.ID)
	if got := linearGet(t, ds, linearTaskType, taskBID).Properties["status"]; got != "done" {
		t.Fatalf("an upstream completion did not close task B: status=%v", got)
	}
	issueB := linearGet(t, ds, linearIssueType, issueBID)
	if issueB.Properties["projectedState"] != "completed" || issueB.Properties["stateType"] != "completed" {
		t.Fatalf("issue B baseline not re-adopted: %v / %v",
			issueB.Properties["projectedState"], issueB.Properties["stateType"])
	}

	// Provider-owned edge hygiene: issue B moves to another team. `team` is
	// a SINGLE edge, so the sync's re-link must leave exactly ONE edge — the
	// new team — never an accumulated pair.
	team2ID := substratefn.ExternalID("linear", account.ID, "team:uuid-t2")
	api.moveIssueBTeam(map[string]any{"id": "uuid-t2", "key": "OPS", "name": "Operations"})
	linearResync(t, ds, account.ID)
	movedB := linearGet(t, ds, linearIssueType, issueBID)
	if tg := movedB.Edges["team"]; len(tg) != 1 || tg[0].ID != team2ID {
		t.Fatalf("team move did not stay current: edges=%+v, want exactly %s", tg, team2ID)
	}
	if got := linearGet(t, ds, linearTeamType, team2ID).Properties["name"]; got != "Operations" {
		t.Fatalf("the new team did not mirror: name=%v", got)
	}
	// ...and an issue that LOST its team upstream sheds the stale edge (a
	// patch cannot clear an edge; the sync reads the mirror and unlinks).
	api.moveIssueBTeam(nil)
	linearResync(t, ds, account.ID)
	if tg := linearGet(t, ds, linearIssueType, issueBID).Edges["team"]; len(tg) != 0 {
		t.Fatalf("a stale team edge survived the team's removal: %+v", tg)
	}

	// And through it all, the owner's task A stayed theirs...
	if got := linearGet(t, ds, linearTaskType, taskAID).Properties["status"]; got != "done" {
		t.Fatalf("task A moved: status=%v", got)
	}
	// ...and NOTHING went back to Linear: sync-only means zero mutations.
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
