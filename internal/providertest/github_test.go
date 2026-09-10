package providertest

// The GitHub bundle — the substrate's second real integration, SYNC-ONLY
// (no writeback until the outbound outbox, issue 009, exists). Five proofs,
// from the shipped closure at ../../kinds/providers.substrate.reamde.dev/github:
//
//  1. TestGithubBundleAdmitsSchema — the closure ADMITS through the schema
//     loader: the bundle declares the `client` input (facility-read, never
//     injected) the oauth2 block names, the config kind wears oauth2, the
//     account wears
//     accountconfig (and NOT oauth2), the trusted `oauth2:` manifest block
//     compiles (github endpoints + the feature→scope map — read:user on
//     EVERY toggle, user:email on none), the user source type carries
//     its required `person` subject edge, the issue and pull-request mirrors
//     carry their required `repository` edges and every mirror property a
//     displayName, the install-closure balances, and the user→person mapping
//     type-checks. No DB, no uv — pure schema admission.
//
//  2. TestGithubBundleInstalls — the whole closure installs into a live
//     repository and every member (bundle, types, function, mapping, both
//     triggers) lands. This warms the PEP 723 sync body through uv, so it
//     SKIPS when uv is absent or cannot provision (offline).
//
//  3. TestGithubBundleFakeSyncMirrors — the whole flow against a LOOPBACK
//     fake GitHub (httptest; no live providers, ever, in tests): OAuth
//     connect (the consent scopes carry read:user + repo and NOT user:email),
//     the on-connect trigger, all THREE searches (issues involves:, PRs
//     involves:, PRs review-requested:) with cross-search dedupe at the
//     deterministic id, the person mapping, and the per-stage syncCursor +
//     login + lastSyncedAt + syncStatus stamp.
//
//  4. TestGithubBundleCursorAndWatermarks — the paged chain stepped page by
//     page through the runner (no trigger machinery), proving the cursor
//     SHAPE: the stage list pinned at queue-head (a toggle flipped mid-drain
//     neither crashes nor shifts the walk), per-stage floors, the
//     1,000-result partition hop (page cap → boundary floor, page one), the
//     incomplete_results watermark REFUSAL, run-start watermarks on completed
//     stages — and a second round proving the stored per-stage JSON cursor
//     (and its legacy plain-string spelling) drives each stage's window
//     independently, minus the 120s overlap.
//
//  5. TestGithubBundleOriginPinRefusal — a body whose API base is rewritten
//     to a non-loopback, non-github origin REFUSES before sending anything:
//     the account stamps `syncStatus: "erroring: … refusing to send
//     credentials …"` and the chain does not park.
//
// Real GitHub API calls never run in a test — only loopback fakes; live
// OAuth + sync is verified against a connected account.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	githubDir         = providersDir + "/github"
	githubPackage     = "providers.substrate.reamde.dev/github"
	githubConfigType  = githubPackage + "/config"
	githubAccountType = githubPackage + "/account"
	githubUserType    = githubPackage + "/user"
	githubRepoType    = githubPackage + "/repository"
	githubIssueType   = githubPackage + "/issue"
	githubPullType    = githubPackage + "/pullrequest"
	githubSyncFn      = githubPackage + "/githubsync"

	// githubAccountID is the account the stepping cases walk.
	githubAccountID = "acct-step"
)

// TestGithubBundleAdmitsSchema loads the builtin schema, then installs the
// bundle closure on top of it through the ordinary loader/resolver — the same
// admission the batch apply runs, minus the function-body warm. Every
// assertion is a rule the loader enforces at admission time.
func TestGithubBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	reg := bundleRegistry(t, githubDir)

	// The bundle exists and declares the `client` input the oauth2 block
	// names: facility-read, so it must NOT inject.
	b := assertBundleInput(t, reg, githubPackage, "client", githubConfigType, "")

	// The trusted provider metadata compiled off the manifest (review-google
	// #1): the github endpoints, and every feature toggle mapped to scopes —
	// the host reads ONLY this, never a config-record property. read:user
	// rides EVERY toggle (an issues-only account still derives email/login
	// off /user), and user:email rides NONE — the bundle only ever reads the
	// PUBLIC profile email, so consent must not ask for the private list.
	if b.OAuth2 == nil {
		t.Fatalf("bundle compiled no oauth2 manifest metadata")
	}
	if b.OAuth2.AuthorizationEndpoint != "https://github.com/login/oauth/authorize" {
		t.Fatalf("authorizationEndpoint = %q", b.OAuth2.AuthorizationEndpoint)
	}
	if b.OAuth2.TokenEndpoint != "https://github.com/login/oauth/access_token" {
		t.Fatalf("tokenEndpoint = %q", b.OAuth2.TokenEndpoint)
	}
	if b.OAuth2.EmailEndpoint != "https://api.github.com/user" || b.OAuth2.EmailProperty != "email" {
		t.Fatalf("email derivation = %q -> %q", b.OAuth2.EmailEndpoint, b.OAuth2.EmailProperty)
	}
	if b.OAuth2.ClientInput != "client" {
		t.Fatalf("oauth2 clientInput = %q, want %q", b.OAuth2.ClientInput, "client")
	}
	for toggle, want := range map[string][]string{
		"enabledUser":         {"read:user"},
		"enabledRepos":        {"read:user", "repo"},
		"enabledIssues":       {"read:user", "repo"},
		"enabledPullRequests": {"read:user", "repo"},
	} {
		got := b.OAuth2.FeatureScopes[toggle]
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("featureScopes[%s] = %v, want %v", toggle, got, want)
		}
		for _, s := range got {
			if s == "user:email" {
				t.Fatalf("featureScopes[%s] requests user:email — the bundle never reads /user/emails", toggle)
			}
		}
	}

	// The config type: oauth2 (client fields), the client input's kind.
	cfg := mustKind(t, reg, githubConfigType)
	if !cfg.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("%s does not implement %s", githubConfigType, vocabulary.TraitOAuth2Core)
	}

	// The account type: accountconfig (the OAuth facility's hands), and NOT
	// oauth2 — client creds bind on the config, tokens on the account.
	acct := mustKind(t, reg, githubAccountType)
	if !acct.Implements(vocabulary.TraitAccountConfigCore) {
		t.Fatalf("%s does not implement %s", githubAccountType, vocabulary.TraitAccountConfigCore)
	}
	if acct.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("%s implements oauth2 — client creds belong on the config, not the account", githubAccountType)
	}
	// Each connector-state property is the connector's alone to write; login
	// is the display fallback the sync stamps for private-email accounts.
	for prop, writer := range map[string]string{
		"email":        vocabulary.WriterOAuth,
		"login":        vocabulary.WriterConnector,
		"syncCursor":   vocabulary.WriterConnector,
		"lastSyncedAt": vocabulary.WriterConnector,
		"syncStatus":   vocabulary.WriterConnector,
	} {
		p, ok := acct.Prop(prop)
		if !ok {
			t.Fatalf("%s declares no %s", githubAccountType, prop)
		}
		if p.Writer != writer {
			t.Fatalf("%s %s writer = %q, want %q", githubAccountType, prop, p.Writer, writer)
		}
	}

	// The user mirror carries an EMPTY subject slot: single, unpinned and
	// optional, because the kind it reaches belongs to the repository and this
	// package owns none (record 49). A mapping the owner of that kind declares
	// is what pins it.
	user := mustKind(t, reg, githubUserType)
	ed, ok := user.Prop("person")
	if !ok {
		t.Fatalf("%s declares no `person` slot", githubUserType)
	}
	if ed.To != "" || ed.Required || ed.Repeated || !ed.Subject || !ed.MustExist {
		t.Fatalf("person slot shape wrong: to=%q required=%v many=%v subject=%v mustExist=%v",
			ed.To, ed.Required, ed.Repeated, ed.Subject, ed.MustExist)
	}

	// The issue and pull-request mirrors each carry a required, single
	// `repository` parent edge at the repository mirror (and, by the
	// bipartite rule, no edge at the mapped user type — the loader
	// would have refused the closure above otherwise).
	if _, ok := reg.ByIdentity(githubRepoType); !ok {
		t.Fatalf("mirror type %s missing", githubRepoType)
	}
	for _, id := range []string{githubIssueType, githubPullType} {
		ty, ok := reg.ByIdentity(id)
		if !ok {
			t.Fatalf("mirror type %s missing", id)
		}
		re, ok := ty.Prop("repository")
		if !ok {
			t.Fatalf("%s declares no `repository` edge", id)
		}
		if re.To != githubRepoType || !re.Required || re.Repeated {
			t.Fatalf("%s repository edge shape wrong: to=%q required=%v many=%v", id, re.To, re.Required, re.Repeated)
		}
	}

	// Every mirror property carries a human label — the console must never
	// render raw camelCase keys.
	for _, id := range []string{githubUserType, githubRepoType, githubIssueType, githubPullType} {
		ty, _ := reg.ByIdentity(id)
		for _, name := range ty.PropOrder {
			if ty.Props[name].DisplayName == "" {
				t.Fatalf("%s property %s carries no displayName", id, name)
			}
		}
	}

	// The closure ships NO mapping: a mapping onto a kind is the declaration of
	// the package that owns that kind, and this one owns no person (record 49).
	if ms := reg.MappingsFrom(githubUserType); len(ms) != 0 {
		t.Fatalf("the github closure ships %d mappings; a provider ships none", len(ms))
	}
	if ms := reg.MappingsTo("samples.substrate.reamde.dev/people/person"); len(ms) != 0 {
		t.Fatalf("the github closure maps onto person: %v", ms)
	}

	// The sync function is a member of the authority.
	if _, err := reg.ResolveFunction(githubSyncFn); err != nil {
		t.Fatalf("sync function %s did not register: %v", githubSyncFn, err)
	}
}

// TestGithubBundleInstalls applies the whole closure into a live repository and
// asserts every member installs. It warms the PEP 723 sync body through uv,
// so it skips when uv is absent or cannot provision.
func TestGithubBundleInstalls(t *testing.T) {
	t.Parallel()
	requireUV(t)
	_, ds := newDataset(t)

	// The atomic install from the shipped manifest.
	install(t, ds, githubDir, nil)

	// The bundle row and every schema member landed as its own record.
	assertMembers(t, ds, map[string]string{
		githubPackage:     typeBundle,
		githubConfigType:  typeKind,
		githubAccountType: typeKind,
		githubUserType:    typeKind,
		githubRepoType:    typeKind,
		githubIssueType:   typeKind,
		githubPullType:    typeKind,
		githubSyncFn:      typeFunction,
	})

	// Computed status: installed, enabled, and the closure's member counts.
	assertUnresolvedInput(t, ds, githubPackage, "client", githubConfigType, 1)

	// The delivery wiring installs as ordinary data records, both bound to
	// the sync function.
	installTriggers(t, ds, githubDir)
	assertTriggers(t, ds, "github-on-connect", "github-scheduled")
}

// githubInstallRewired installs the closure with every provider reference
// pointed at the loopback fake: the bundle document's trusted oauth2
// endpoints (a static manifest cannot bake a dynamic httptest URL) and the
// sync body's API base constant. The body's origin pin allows loopback as the
// test seam, so the rewritten base admits; API_HOST and the htmlURL stubs are
// left alone.
func githubInstallRewired(t *testing.T, ds substrate.Dataset, baseURL string) {
	t.Helper()
	install(t, ds, githubDir, func(docs []map[string]any) {
		rewriteOAuthEndpoints(t, docs, baseURL, "https://api.github.com", "https://github.com")
		rewriteSource(t, docs, `API = "https://api.github.com"`, `API = "`+baseURL+`"`)
	})
}

// githubStepConfig is one connected account with every feature the case
// names, as the injected config a stepped invocation receives.
func githubStepConfig(props map[string]any) map[string]any {
	return stepConfig(githubAccountType, githubAccountID, props)
}

// fakeGithub is a GitHub in a box: the token exchange the manifest is rewired
// to, the /user profile the facility derives the email from (and the sync
// reads the login off), one /user/repos page, and the /search/issues endpoint
// answering all three qualifiers. It records every search `q` so tests can
// assert windows and the review-requested query.
type fakeGithub struct {
	fakeAPI

	// The stepping scenario's dials (TestGithubBundleCursorAndWatermarks):
	// issue pages come 100-full until the query floor moves off the
	// first-seen floor (the partition hop), and the PRs-involved search
	// answers incomplete when told to.
	pageIssues      bool
	pullsIncomplete bool
	issuesFloor     string
}

// githubSearchFloor extracts the `updated:>=` floor from a search q.
func githubSearchFloor(q string) string {
	const marker = "updated:>="
	i := strings.Index(q, marker)
	if i < 0 {
		return ""
	}
	rest := q[i+len(marker):]
	if j := strings.IndexByte(rest, ' '); j >= 0 {
		return rest[:j]
	}
	return rest
}

// githubPagedBase anchors the capped-window fixture RELATIVE TO NOW: the
// bundle's first-seen floor is backfill-derived (now minus last30d), and the
// fixed 2026-08-01 base this used to be rolled below that floor on
// 2026-08-31, which silently defused the partition scenario. Ten days back
// keeps every generated item inside the window whenever the test runs.
var githubPagedBase = time.Now().UTC().AddDate(0, 0, -10).Truncate(time.Second)

func newFakeGithub(t *testing.T) *fakeGithub {
	t.Helper()
	f := &fakeGithub{}
	issueItem := func(repo string, number int, node, updated, author string) map[string]any {
		return map[string]any{
			"node_id": node, "number": number, "state": "open",
			"title": fmt.Sprintf("issue %d", number), "body": "the body",
			"html_url":       "https://github.com/" + repo + "/issues/" + strconv.Itoa(number),
			"repository_url": f.ts.URL + "/repos/" + repo,
			"created_at":     "2026-08-01T00:00:00Z", "updated_at": updated,
			"user": map[string]any{"login": author, "html_url": "https://github.com/" + author},
		}
	}
	prItem := func(repo string, number int, node, updated, author, mergedAt string) map[string]any {
		it := issueItem(repo, number, node, updated, author)
		it["title"] = fmt.Sprintf("pr %d", number)
		it["html_url"] = "https://github.com/" + repo + "/pull/" + strconv.Itoa(number)
		pr := map[string]any{}
		if mergedAt != "" {
			pr["merged_at"] = mergedAt
		}
		it["pull_request"] = pr
		return it
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") == "refresh_token" {
			writeJSON(w, map[string]any{"token_type": "bearer", "access_token": "at-1"})
			return
		}
		if r.Form.Get("code") != "code-123" {
			http.Error(w, "bad exchange", http.StatusBadRequest)
			return
		}
		// GitHub OAuth apps mint non-expiring tokens with no refresh token.
		writeJSON(w, map[string]any{"token_type": "bearer", "access_token": "at-1"})
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		writeJSON(w, map[string]any{
			"login": "octocat", "name": "George Example", "email": "geo@github.example",
			"html_url": "https://github.com/octocat", "created_at": "2015-01-01T00:00:00Z",
		})
	})
	mux.HandleFunc("/user/repos", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		writeJSON(w, []any{map[string]any{
			"full_name": "octocat/hello-world", "name": "hello-world", "private": true,
			"default_branch": "main", "html_url": "https://github.com/octocat/hello-world",
			"created_at": "2020-01-01T00:00:00Z",
		}})
	})
	mux.HandleFunc("/search/issues", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		q := r.URL.Query().Get("q")
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		f.record(r)
		f.mu.Lock()
		pageIssues, pullsIncomplete := f.pageIssues, f.pullsIncomplete
		if pageIssues && strings.Contains(q, "type:issue") && f.issuesFloor == "" {
			f.issuesFloor = githubSearchFloor(q)
		}
		issuesFloor := f.issuesFloor
		f.mu.Unlock()

		reply := func(incomplete bool, items ...any) {
			writeJSON(w, map[string]any{
				"total_count": len(items), "incomplete_results": incomplete, "items": items,
			})
		}
		base := githubPagedBase
		switch {
		case strings.Contains(q, "review-requested:"):
			// The third search: PR 7 AGAIN (cross-search dedupe at the same
			// deterministic id) plus PR 9 in a repo the listing never saw.
			reply(false,
				prItem("octocat/hello-world", 7, "PR_7", "2026-08-08T10:00:00Z", "octocat", "2026-08-08T09:00:00Z"),
				prItem("acme/tools", 9, "PR_9", "2026-08-08T11:00:00Z", "alice", ""))
		case strings.Contains(q, "type:pr"):
			if pullsIncomplete {
				// A partial provider response: what arrived still mirrors,
				// but the pulls watermark must NOT advance.
				reply(true, prItem("octocat/hello-world", 7, "PR_7", "2026-08-08T10:00:00Z", "octocat", "2026-08-08T09:00:00Z"))
				return
			}
			reply(false, prItem("octocat/hello-world", 7, "PR_7", "2026-08-08T10:00:00Z", "octocat", "2026-08-08T09:00:00Z"))
		default: // type:issue
			if !pageIssues {
				reply(false, issueItem("octocat/hello-world", 3, "I_3", "2026-08-08T08:00:00Z", "octocat"))
				return
			}
			// The stepping scenario: 10 full pages (1,000 items) under the
			// first-seen floor — the search ceiling — then, once the floor
			// moves (the partition hop), a short page that ends the stage.
			if githubSearchFloor(q) == issuesFloor {
				items := make([]any, 0, 100)
				for i := (page - 1) * 100; i < page*100; i++ {
					items = append(items, issueItem("octocat/hello-world", i+1,
						fmt.Sprintf("I_%04d", i+1),
						base.Add(time.Duration(i)*time.Second).Format("2006-01-02T15:04:05Z"),
						"octocat"))
				}
				reply(false, items...)
				return
			}
			reply(false, issueItem("octocat/hello-world", 2000, "I_2000",
				githubPagedBase.Add(time.Hour).Format("2006-01-02T15:04:05Z"), "octocat"))
		}
	})
	f.serve(t, mux)
	return f
}

// queries is every search `q` the run sent, which is where a window clause
// and the review-requested qualifier land.
func (f *fakeGithub) queries() []string {
	var out []string
	for _, raw := range f.seen() {
		v, err := url.ParseQuery(raw)
		if err != nil {
			continue
		}
		if q := v.Get("q"); q != "" {
			out = append(out, q)
		}
	}
	return out
}

// TestGithubBundleFakeSyncMirrors drives the whole integration against the
// loopback fake: install (endpoints + API base rewired), configure, connect
// over the host OAuth facility, then let the on-connect trigger drain the
// three-search sync and assert the mirrors, the dedupe and the stamp.
func TestGithubBundleFakeSyncMirrors(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	fake := newFakeGithub(t)
	svc, ds := newOAuthDataset(t, fake.ts.Client())
	githubInstallRewired(t, ds, fake.ts.URL)

	// Only the on-connect trigger: the schedule would race catch-up fires into
	// the drain below and prove nothing this test is after.
	installTriggers(t, ds, githubDir, "github-on-connect")

	// Configure the client record (the sole record resolves the input), then
	// add one pending account with every feature on.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: githubConfigType,
		Properties: map[string]any{
			"clientId": "client-1", "clientSecret": "s3cret",
		},
	}); err != nil {
		t.Fatalf("create config: %v", err)
	}
	account, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: githubAccountType,
		Properties: map[string]any{
			"enabledUser": true, "enabledRepos": true,
			"enabledIssues": true, "enabledPullRequests": true,
			"syncFrequency": "hourly", "backfillDepth": "last30d",
		},
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	// The host OAuth round trip against the fake: start derives the scope
	// union from the toggles — read:user + repo, and NEVER user:email (the
	// bundle only reads the public profile email).
	consent, err := ds.StartOAuth(ctx, substrate.ActorAPI, account.ID)
	if err != nil {
		t.Fatalf("start oauth: %v", err)
	}
	cu, err := url.Parse(consent)
	if err != nil {
		t.Fatalf("parse consent url: %v", err)
	}
	scope := cu.Query().Get("scope")
	for _, want := range []string{"read:user", "repo"} {
		if !strings.Contains(scope, want) {
			t.Fatalf("consent scope misses %s: %q", want, scope)
		}
	}
	if strings.Contains(scope, "user:email") {
		t.Fatalf("consent asks for user:email the bundle never uses: %q", scope)
	}
	completeOAuth(t, svc, cu.Query().Get("state"), "code-123")
	connected, err := ds.Get(ctx, account.Kind, account.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if connected.Properties["tokenStatus"] != "connected" {
		t.Fatalf("account tokenStatus = %v", connected.Properties["tokenStatus"])
	}
	if connected.Properties["email"] != "geo@github.example" {
		t.Fatalf("derived email = %v", connected.Properties["email"])
	}

	// The on-connect trigger fires the sync; the paged chain walks
	// user → repos → issues → pulls → pullsReview.
	drainTriggers(t, ds)

	// The connected user's mirror, written whole off /user.
	me, err := ds.Get(ctx, githubUserType, runner.ExternalID("github", account.ID, "user/octocat"))
	if err != nil {
		t.Fatalf("%s did not sync: %v", githubUserType, err)
	}
	if me.Kind != githubUserType || me.Properties["email"] != "geo@github.example" {
		t.Fatalf("user wrong: type=%s email=%v", me.Kind, me.Properties["email"])
	}

	// The repository from the listing, and the stub minted for the foreign
	// repo the review-requested PR lives in.
	repo, err := ds.Get(ctx, githubRepoType, runner.ExternalID("github", account.ID, "repo/octocat/hello-world"))
	if err != nil {
		t.Fatalf("repository did not sync: %v", err)
	}
	if repo.Properties["fullName"] != "octocat/hello-world" {
		t.Fatalf("repo fullName = %v", repo.Properties["fullName"])
	}
	if _, err := ds.Get(ctx, githubRepoType, runner.ExternalID("github", account.ID, "repo/acme/tools")); err != nil {
		t.Fatalf("foreign repo stub did not mint: %v", err)
	}

	// The issue mirror off the involves: search.
	issue, err := ds.Get(ctx, githubIssueType, runner.ExternalID("github", account.ID, "issue/octocat/hello-world#3"))
	if err != nil {
		t.Fatalf("%s did not sync: %v", githubIssueType, err)
	}
	if issue.Properties["state"] != "open" || issue.Properties["authorLogin"] != "octocat" {
		t.Fatalf("issue wrong: %v", issue.Properties)
	}

	// PR 7 arrived over BOTH the involves: and review-requested: searches —
	// one row at the deterministic id, upserted, never duplicated. PR 9
	// arrived ONLY over review-requested: (involves: does not cover review
	// requests), with alice's author stub minted beside it.
	pr7, err := ds.Get(ctx, githubPullType, runner.ExternalID("github", account.ID, "pull/octocat/hello-world#7"))
	if err != nil {
		t.Fatalf("pull 7 did not sync: %v", err)
	}
	if pr7.Properties["state"] != "merged" {
		t.Fatalf("pull 7 state = %v, want merged", pr7.Properties["state"])
	}
	pr9, err := ds.Get(ctx, githubPullType, runner.ExternalID("github", account.ID, "pull/acme/tools#9"))
	if err != nil {
		t.Fatalf("review-requested pull 9 did not sync: %v", err)
	}
	if pr9.Properties["authorLogin"] != "alice" {
		t.Fatalf("pull 9 author = %v", pr9.Properties["authorLogin"])
	}
	if _, err := ds.Get(ctx, githubUserType, runner.ExternalID("github", account.ID, "user/alice")); err != nil {
		t.Fatalf("alice's author stub did not mint: %v", err)
	}
	if pulls := countLive(t, ds, githubPullType); pulls != 2 {
		t.Fatalf("pullrequest rows = %d, want 2 (PR 7 deduped across searches)", pulls)
	}

	// All three searches ran.
	var sawReview bool
	for _, q := range fake.queries() {
		if strings.Contains(q, "review-requested:octocat") {
			sawReview = true
		}
	}
	if !sawReview {
		t.Fatalf("no review-requested: search ran: %v", fake.queries())
	}

	// The final stamp: ok, login as the display fallback, and the PER-STAGE
	// watermark JSON — every completed stage stamped the same run-start.
	stamped, err := ds.Get(ctx, account.Kind, account.ID)
	if err != nil {
		t.Fatalf("get stamped account: %v", err)
	}
	if stamped.Properties["syncStatus"] != "ok" {
		t.Fatalf("syncStatus = %v, want ok", stamped.Properties["syncStatus"])
	}
	if stamped.Properties["login"] != "octocat" {
		t.Fatalf("login = %v, want octocat", stamped.Properties["login"])
	}
	if s, _ := stamped.Properties["lastSyncedAt"].(string); s == "" {
		t.Fatalf("lastSyncedAt not stamped: %v", stamped.Properties["lastSyncedAt"])
	}
	raw, _ := stamped.Properties["syncCursor"].(string)
	var cursors map[string]string
	if err := json.Unmarshal([]byte(raw), &cursors); err != nil {
		t.Fatalf("syncCursor is not the per-stage JSON: %q (%v)", raw, err)
	}
	for _, stage := range []string{"issues", "pulls", "pullsReview"} {
		v := cursors[stage]
		if v == "" {
			t.Fatalf("syncCursor misses stage %s: %q", stage, raw)
		}
		if _, err := time.Parse(time.RFC3339, v); err != nil {
			t.Fatalf("syncCursor[%s] = %q is not RFC 3339: %v", stage, v, err)
		}
		if v != cursors["issues"] {
			t.Fatalf("completed stages stamped different watermarks: %q", raw)
		}
	}
}

// TestGithubBundleCursorAndWatermarks proves the paged cursor's shape and the
// per-stage watermark discipline without any trigger machinery: the stage
// list pins at queue-head (a mid-drain toggle-off neither crashes nor shifts
// the walk), the 1,000-result ceiling partitions by updated-range, an
// incomplete_results response refuses to advance its stage's watermark while
// completed stages stamp the run-start — and a second round proves the
// stored per-stage JSON (and its legacy plain-string spelling) drives each
// stage's window independently, 120s of overlap behind the floor.
func TestGithubBundleCursorAndWatermarks(t *testing.T) {
	t.Parallel()
	requireUV(t)
	fake := newFakeGithub(t)
	fake.pageIssues = true
	fake.pullsIncomplete = true
	_, ds := newDataset(t)
	githubInstallRewired(t, ds, fake.ts.URL)

	props := map[string]any{
		"enabledUser": true, "enabledRepos": true,
		"enabledIssues": true, "enabledPullRequests": true,
		"syncFrequency": "hourly", "backfillDepth": "last30d",
	}
	s := newStepper(t, ds, githubSyncFn, githubStepConfig(props))

	// Step 1 — the user stage pins the run's shape into the cursor: the
	// stage list, the run-start watermark, and one floor per search stage.
	_, _, cur := s.step(nil)
	if cur == nil {
		t.Fatalf("the chain drained after one page")
	}
	wantStages := []string{"user", "repos", "issues", "pulls", "pullsReview"}
	stages, _ := cur["stages"].([]any)
	if len(stages) != len(wantStages) {
		t.Fatalf("pinned stages = %v, want %v", cur["stages"], wantStages)
	}
	for i, w := range wantStages {
		if stages[i] != w {
			t.Fatalf("pinned stages = %v, want %v", cur["stages"], wantStages)
		}
	}
	if cur["stage"] != "repos" {
		t.Fatalf("stage after user = %v, want repos", cur["stage"])
	}
	runStart, _ := cur["runStart"].(string)
	if _, err := time.Parse(time.RFC3339, runStart); err != nil {
		t.Fatalf("runStart = %q is not RFC 3339: %v", runStart, err)
	}
	floors, _ := cur["floors"].(map[string]any)
	for _, stage := range []string{"issues", "pulls", "pullsReview"} {
		if f, _ := floors[stage].(string); f == "" {
			t.Fatalf("no floor pinned for stage %s: %v", stage, cur["floors"])
		}
	}
	pullsFloor, _ := floors["pulls"].(string)

	// Mid-drain, the owner switches pull requests OFF. The pinned stage list
	// must keep the walk stable: pulls and pullsReview still run, nothing
	// crashes (the old order.index ValueError, review-fleet-claude #11).
	flipped := map[string]any{}
	for k, v := range props {
		flipped[k] = v
	}
	flipped["enabledPullRequests"] = false
	s.setConfig(githubStepConfig(flipped))

	// Walk the rest of the chain, watching the issues partition hop: page 10
	// comes back full (the 1,000-result ceiling), so the stage restarts at
	// page one with the drained boundary as its new floor.
	var sawPartition bool
	resume := any(cur)
	var last []engine.StepEffect
	for range 40 {
		effects, _, next := s.step(resume)
		if next == nil {
			last = effects
			break
		}
		if next["stage"] == "issues" {
			// item 999's updated_at — the boundary of the full window.
			boundary := githubPagedBase.Add(999 * time.Second).Format(time.RFC3339)
			if pf, _ := next["pfloor"].(string); pf == boundary {
				if pg, _ := next["page"].(float64); pg != 1 {
					t.Fatalf("partition hop did not reset the page: %v", next["page"])
				}
				sawPartition = true
			}
		}
		resume = next
	}
	if last == nil {
		t.Fatalf("the paged chain did not drain")
	}
	if !sawPartition {
		t.Fatalf("the issues stage never partitioned at the search ceiling")
	}

	// The searches the fake saw: 11 issues queries (10 capped pages + the
	// partition window), then BOTH pull searches despite the flipped toggle.
	var issueQs, pullQs, reviewQs int
	for _, q := range fake.queries() {
		switch {
		case strings.Contains(q, "review-requested:"):
			reviewQs++
		case strings.Contains(q, "type:pr"):
			pullQs++
		default:
			issueQs++
		}
	}
	if issueQs != 11 || pullQs != 1 || reviewQs != 1 {
		t.Fatalf("searches ran issues=%d pulls=%d review=%d, want 11/1/1", issueQs, pullQs, reviewQs)
	}

	// The stamp: issues and pullsReview completed, so they carry the
	// RUN-START watermark; pulls answered incomplete_results, so its
	// watermark REFUSED to advance — it still reads the original floor.
	stamp := accountStamp(t, last, githubAccountType, githubAccountID)
	if got, _ := stamp["syncStatus"].(string); got != "ok (partial: pulls)" {
		t.Fatalf("syncStatus = %v, want \"ok (partial: pulls)\"", stamp["syncStatus"])
	}
	if stamp["login"] != "octocat" {
		t.Fatalf("stamped login = %v", stamp["login"])
	}
	rawCur, _ := stamp["syncCursor"].(string)
	var cursors map[string]string
	if err := json.Unmarshal([]byte(rawCur), &cursors); err != nil {
		t.Fatalf("stamped syncCursor is not the per-stage JSON: %q (%v)", rawCur, err)
	}
	if cursors["issues"] != runStart || cursors["pullsReview"] != runStart {
		t.Fatalf("completed stages did not stamp the run-start %q: %q", runStart, rawCur)
	}
	if cursors["pulls"] != pullsFloor {
		t.Fatalf("partial pulls stage advanced its watermark: got %q, want the floor %q",
			cursors["pulls"], pullsFloor)
	}

	// Round two: a stored per-stage cursor drives each stage's window
	// independently — every search floor is ITS stage's watermark minus the
	// 120s overlap, never a shared one.
	fake.pullsIncomplete = false
	stored := map[string]any{}
	for k, v := range props {
		stored[k] = v
	}
	stored["syncCursor"] = `{"issues":"2026-08-05T00:00:00Z","pulls":"2026-08-06T00:00:00Z","pullsReview":"2026-08-07T00:00:00Z"}`
	s.setConfig(githubStepConfig(stored))
	before := len(fake.queries())
	s.drain(nil)
	wantFloors := map[string]string{
		"type:issue":        "2026-08-04T23:58:00Z",
		"review-requested:": "2026-08-06T23:58:00Z",
		"type:pr involves:": "2026-08-05T23:58:00Z",
	}
	for _, q := range fake.queries()[before:] {
		for marker, want := range wantFloors {
			if strings.Contains(q, marker) {
				if got := githubSearchFloor(q); got != want {
					t.Fatalf("stage %q searched floor %q, want its own watermark - overlap %q", marker, got, want)
				}
			}
		}
	}

	// A LEGACY plain-RFC-3339 syncCursor (the old single shared watermark)
	// still reads as every stage's floor — an upgrade never re-backfills.
	legacy := map[string]any{}
	for k, v := range props {
		legacy[k] = v
	}
	legacy["syncCursor"] = "2026-08-05T12:00:00Z"
	s.setConfig(githubStepConfig(legacy))
	before = len(fake.queries())
	s.drain(nil)
	for _, q := range fake.queries()[before:] {
		if got := githubSearchFloor(q); got != "2026-08-05T11:58:00Z" {
			t.Fatalf("legacy cursor searched floor %q, want 2026-08-05T11:58:00Z (%q)", got, q)
		}
	}
}

// TestGithubBundleOriginPinRefusal rewires the body's API base to a
// non-loopback, non-github origin and proves the F1 refusal: the token is
// never sent (nothing listens there — the invocation still returns
// immediately), the account stamps `syncStatus: erroring` with the refusal,
// and the chain completes instead of parking.
func TestGithubBundleOriginPinRefusal(t *testing.T) {
	t.Parallel()
	requireUV(t)
	_, ds := newDataset(t)
	docs := loadDocs(t, githubDir+"/bundle.yaml")
	for _, d := range docs {
		data, _ := d["data"].(map[string]any)
		if data == nil || d["kind"] != vocabulary.CoreKind("function") {
			continue
		}
		if src, ok := data["source"].(string); ok {
			data["source"] = strings.ReplaceAll(src,
				`API = "https://api.github.com"`, `API = "https://intercepted.example"`)
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI, docs); err != nil {
		if isUVProvisionError(err) {
			t.Skipf("bundle install could not warm the PEP 723 body (uv offline?): %v", err)
		}
		t.Fatalf("install the github bundle: %v", err)
	}

	s := newStepper(t, ds, githubSyncFn, githubStepConfig(map[string]any{
		"enabledIssues": true, "syncFrequency": "hourly", "backfillDepth": "last30d",
	}))
	effects, steps := s.drain(nil)
	if steps != 1 {
		t.Fatalf("the refusal took %d steps, want 1 (refused before any page)", steps)
	}
	stamp := accountStamp(t, effects, githubAccountType, githubAccountID)
	status, _ := stamp["syncStatus"].(string)
	if !strings.HasPrefix(status, "erroring: ") || !strings.Contains(status, "refusing to send credentials") {
		t.Fatalf("syncStatus = %q, want an erroring refusal", status)
	}
	if !strings.Contains(status, "intercepted.example") {
		t.Fatalf("the refusal does not name the refused origin: %q", status)
	}
	if s, _ := stamp["lastSyncedAt"].(string); s == "" {
		t.Fatalf("erroring stamp misses lastSyncedAt — the on-connect guard would re-fire forever")
	}
	if stamp["syncCursor"] != nil {
		t.Fatalf("erroring stamp wrote a cursor: %v", stamp["syncCursor"])
	}
}
