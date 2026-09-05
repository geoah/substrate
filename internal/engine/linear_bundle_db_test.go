package engine

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
	linearExampleDir  = "../../kinds/providers.substrate.reamde.dev/linear"
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
)

// TestLinearBundleAdmitsSchema loads the builtin schema, then installs the
// bundle closure on top of it through the ordinary loader/resolver — the same
// admission the batch apply runs, minus the function-body warm. Every
// assertion is a rule the loader enforces at admission time.
func TestLinearBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	// The registry an install actually admits into: the seeded tree (core
	// alone) plus the shipped VOCABULARY bundles this repository imported —
	// what a closure declaring onto people/tasks/messaging/calendar/media
	// needs present, and what `requires:` names.
	reg, err := enginetest.SeededRegistry("../../kinds/substrate.reamde.dev/core")
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
	authorities, err := vocabulary.BuildPackages(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("build the bundle authority: %v", err)
	}
	if err := reg.InstallAll(authorities); err != nil {
		t.Fatalf("the bundle closure did not admit: %v", err)
	}

	// The bundle exists, declares the `client` input the oauth2 block names
	// (facility-read, never injected), and carries the TRUSTED
	// oauth2 provider metadata: Linear's endpoints and the
	// enabledIssues→read scope map live on the immutable install artifact.
	b, ok := reg.BundleOf(linearPackage)
	if !ok {
		t.Fatalf("no bundle owns %s after install", linearPackage)
	}
	in, ok := b.Inputs["client"]
	if !ok {
		t.Fatalf("bundle declares no client input: %v", b.InputOrder)
	}
	if in.Kind != linearConfigType {
		t.Fatalf("client input kind = %q, want %q", in.Kind, linearConfigType)
	}
	if in.Inject != "" {
		t.Fatalf("client input inject = %q, but the OAuth client is facility-read, never injected", in.Inject)
	}
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
	cfg, ok := reg.ByIdentity(linearConfigType)
	if !ok {
		t.Fatalf("config type %s missing", linearConfigType)
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

	// The mirror types carry their subject SLOTS: single, unpinned and
	// optional, because the kind they reach is the repository's to choose
	// (record 49). The issue's team edge is an ordinary pinned reference.
	user, ok := reg.ByIdentity(linearUserType)
	if !ok {
		t.Fatalf("mirror type %s missing", linearUserType)
	}
	if ed, ok := user.Prop("person"); !ok || ed.To != "" || ed.Required || ed.Repeated || !ed.Subject {
		t.Fatalf("user person slot shape wrong: %+v (ok=%v)", ed, ok)
	}
	issue, ok := reg.ByIdentity(linearIssueType)
	if !ok {
		t.Fatalf("mirror type %s missing", linearIssueType)
	}
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
		linearPackage:     "substrate.reamde.dev/core/bundle",
		linearConfigType:  "substrate.reamde.dev/core/kind",
		linearAccountType: "substrate.reamde.dev/core/kind",
		linearUserType:    "substrate.reamde.dev/core/kind",
		linearTeamType:    "substrate.reamde.dev/core/kind",
		linearIssueType:   "substrate.reamde.dev/core/kind",
		linearSyncFn:      "substrate.reamde.dev/core/function",
	} {
		row, err := ds.Get(ctx, wantType, id)
		if err != nil {
			t.Fatalf("member %s did not install: %v", id, err)
		}
		if row.Kind != wantType {
			t.Fatalf("member %s is a %s, want %s", id, row.Kind, wantType)
		}
	}

	// Computed status: installed, enabled, unconfigured, one function.
	st, err := ds.BundleStatus(ctx, linearPackage)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if !st.Installed || !st.Enabled {
		t.Fatalf("bundle not live: installed=%v enabled=%v", st.Installed, st.Enabled)
	}
	if len(st.Inputs) != 1 || st.Inputs[0].Name != "client" || st.Inputs[0].Kind != linearConfigType {
		t.Fatalf("status inputs = %+v, want the one client input", st.Inputs)
	}
	if st.Inputs[0].Record != "" || st.Inputs[0].Via != "" {
		t.Fatalf("client input resolved with no config record created: %+v", st.Inputs[0])
	}
	if len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupMissing || st.Setup[0].Input != "client" {
		t.Fatalf("status setup = %+v, want the one missing-input item", st.Setup)
	}
	if st.Functions != 1 {
		t.Fatalf("status functions = %d, want the sync alone", st.Functions)
	}

	// The delivery wiring installs as ordinary data records.
	for _, m := range loadYAMLDocs(t, linearExampleDir+"/triggers.yaml") {
		putDataDoc(t, ds, m)
	}
	for _, id := range []string{
		"linear-issues-on-connect", "linear-issues-scheduled",
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
		WithCredentialKey(TestCredentialKey), WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		WithOAuth("test-state-key", "https://substrate.example/api/v1/substrate.reamde.dev/core/oauth/callback", client),
		WithCredentialKey(TestCredentialKey))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	s, ok := svc.(*service)
	if !ok {
		t.Fatalf("service is a %T", svc)
	}
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
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

// linearResync clears the account's completion marker and drains: the removal
// is itself an account update the on-connect guard matches, so the one patch
// both resets the guard and fires the re-sync.
//
// Two constraints decide whose hand it is. `lastSyncedAt` is
// `writer: connector`, so only a BUNDLE-tier actor may clear it, and a
// declared function's actor is that tier. And a callable never sees its own
// writes (the dispatcher's self-exclusion), so the clear cannot be stamped as
// the sync itself. The linear closure ships one callable now, the sync, so the
// hand is a declared function of the TEST's own: installLinearResyncHand.
func linearResync(t *testing.T, ds *dataset, accountID string) {
	t.Helper()
	hand := substrate.FunctionActor(vocabulary.SplitKindRef(linearResyncFn))
	if _, err := ds.Patch(context.Background(), hand, linearAccountType, accountID, substrate.PatchInput{
		Properties: map[string]any{"lastSyncedAt": nil},
	}); err != nil {
		t.Fatalf("clear lastSyncedAt: %v", err)
	}
	drainTriggers(t, ds)
}

// linearResyncFn is the test's own bundle-tier hand: a declared function that
// never runs, installed only so its actor resolves at the bundle tier.
const linearResyncFn = "resync.example.com/hand/clear"

func installLinearResyncHand(t *testing.T, ds *dataset) {
	t.Helper()
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI,
		[]map[string]any{
			vocabulary.PackageManifest("resync.example.com/hand", 1),
			vocabulary.FunctionManifest("resync.example.com/hand", "clear", map[string]any{
				"description": "a bundle-tier hand for the re-sync clear; never called",
				"runtime":     vocabulary.RuntimePython,
				"source":      "def main(input, host):\n    return {}\n",
			}),
		}); err != nil {
		t.Fatalf("install the re-sync hand: %v", err)
	}
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
	// The closure ships no mapping (record 49): the repository declares how
	// linear's mirrors reach the person kind it owns, and this test wants that
	// identity resolution, so it declares both.
	if err := enginetest.DeclareMappings(ctx, ds,
		enginetest.PeopleMapping("linearuserperson", map[string]any{
			"from": linearUserType, "property": "person",
			"match": []any{map[string]any{"from": "email", "to": "emails"}},
			"map": map[string]any{
				"name":        map[string]any{"path": "name"},
				"displayName": map[string]any{"path": "displayName"},
				"emails":      map[string]any{"path": "email", "merge": "union"},
			},
		}),
		enginetest.PeopleMapping("linearissueperson", map[string]any{
			"from": linearIssueType, "property": "assignee",
			"match": []any{map[string]any{"from": "assigneeEmail", "to": "emails"}},
		}),
	); err != nil {
		t.Fatalf("declare the linear mappings: %v", err)
	}
	for _, m := range loadYAMLDocs(t, linearExampleDir+"/triggers.yaml") {
		putDataDoc(t, ds, m)
	}

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
	if _, err := svc.CompleteOAuth(ctx, cu.Query().Get("state"), "code-123"); err != nil {
		t.Fatalf("oauth callback: %v", err)
	}

	// The backfill: on-connect fires the sync, which drains both pages off
	// the causal chain.
	installLinearResyncHand(t, ds)
	drainTriggers(t, ds)

	issueAID := substratefn.ExternalID("linear", account.ID, "issue:uuid-a")
	issueBID := substratefn.ExternalID("linear", account.ID, "issue:uuid-b")
	teamID := substratefn.ExternalID("linear", account.ID, "team:uuid-t")
	userID := substratefn.ExternalID("linear", account.ID, "user:uuid-v")

	if n := api.pageCount(); n < 2 {
		t.Fatalf("the paged drain made %d GraphQL reads, want >= 2 (one per page)", n)
	}

	// The issue mirror, in Linear's shape, with its team reference.
	issueA := linearGet(t, ds, linearIssueType, issueAID)
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
	if got := linearGet(t, ds, linearTeamType, teamID).Properties["name"]; got != "Engineering" {
		t.Fatalf("team mirror name = %v", got)
	}

	// Identity: the viewer's user matched-or-minted a person, and the issue's
	// assignee resolved onto the SAME human.
	user := linearGet(t, ds, linearUserType, userID)
	pe := refIDs(user, "person")
	if len(pe) != 1 {
		t.Fatalf("user person unresolved: %+v", user.Properties)
	}
	personID := pe[0]
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
	if ae := refIDs(issueA, "assignee"); len(ae) != 1 || ae[0] != personID {
		t.Fatalf("issue assignee = %+v, want person %s", ae, personID)
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

	// NO TASK ROW, from anything in this closure: `task` belongs to a package
	// the provider does not own, so the repository is what writes one.
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{linearTaskType}}, First: 10,
	})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(page.Records) != 0 {
		t.Fatalf("the linear closure wrote %d task rows", len(page.Records))
	}

	// An idle re-sync writes nothing: the mirrors are patched, never re-put
	// whole, so no-op suppression holds end to end.
	beforeVersion := linearGet(t, ds, linearIssueType, issueAID).Version
	linearResync(t, ds, account.ID)
	if got := linearGet(t, ds, linearIssueType, issueAID).Version; got != beforeVersion {
		t.Fatalf("an idle re-sync rewrote the issue mirror: version %d -> %d",
			beforeVersion, got)
	}

	// Provider-owned edge hygiene: issue B moves to another team. `team` is
	// a SINGLE reference, so the sync's re-write must leave exactly ONE target
	// — the new team — never an accumulated pair.
	team2ID := substratefn.ExternalID("linear", account.ID, "team:uuid-t2")
	api.moveIssueBTeam(map[string]any{"id": "uuid-t2", "key": "OPS", "name": "Operations"})
	linearResync(t, ds, account.ID)
	movedB := linearGet(t, ds, linearIssueType, issueBID)
	if tg := refIDs(movedB, "team"); len(tg) != 1 || tg[0] != team2ID {
		t.Fatalf("team move did not stay current: team=%+v, want exactly %s", tg, team2ID)
	}
	if got := linearGet(t, ds, linearTeamType, team2ID).Properties["name"]; got != "Operations" {
		t.Fatalf("the new team did not mirror: name=%v", got)
	}
	// ...and an issue that LOST its team upstream sheds the stale pointer: the
	// sync writes the property null, which is what clears a reference.
	api.moveIssueBTeam(nil)
	linearResync(t, ds, account.ID)
	if tg := refIDs(linearGet(t, ds, linearIssueType, issueBID), "team"); len(tg) != 0 {
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
