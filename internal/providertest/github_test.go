package providertest

// The github bundle's `githubsync` against a loopback GitHub that answers one
// search of 150 pull requests. The engine parks a paged chain whose middle
// page arrives past its two-minute drain deadline and drops that page, so a
// walk that does not fit one chain has to stop itself, save where it got to,
// and let the next run resume there. Production parked every hourly fire at
// `pullsFetch` from 2026-09-22 on, and each run began every search again at
// its floor.

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

const (
	githubDir         = providersDir + "/github"
	githubPackage     = "providers.substrate.reamde.dev/github"
	githubAccountType = githubPackage + "/account"
	githubPullType    = githubPackage + "/pullrequest"
	githubSyncFn      = githubPackage + "/githubsync"
	githubAccount     = "acct-gh"
	githubLogin       = "ada"
	githubPulls       = 150
)

// githubPull is one pull request the fake serves: the search hit carries its
// ISSUE id and the Issue representation's `/issues/{n}` url, the pull read
// carries its PULL id and `/pulls/{n}`.
type githubPull struct {
	number  int
	updated string
}

func (p githubPull) pullID() int  { return 20000 + p.number }
func (p githubPull) issueID() int { return 10000 + p.number }

// nodeID is the `PR_…` global node id both representations carry: base64url
// of the msgpack array [0, pullId], which the body decodes into the pull id.
func (p githubPull) nodeID() string {
	raw := []byte{0x92, 0x00, 0xce, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(raw[3:], uint32(p.pullID()))
	return "PR_" + base64.RawURLEncoding.EncodeToString(raw)
}

func (p githubPull) apiURL(kind string) string {
	return fmt.Sprintf("https://api.github.com/repos/acme/widgets/%s/%d", kind, p.number)
}

// fakeGitHub serves `GET /user`, the `type:pr involves:` search (every other
// search is empty), the one repository, and each pull request and its empty
// review list.
type fakeGitHub struct {
	fakeAPI
	pulls []githubPull
}

func (f *fakeGitHub) start(t *testing.T) {
	t.Helper()
	user := map[string]any{"login": githubLogin, "id": 1, "node_id": "U_1", "type": "User"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if bearer(w, r) {
			writeJSON(w, user)
		}
	})
	mux.HandleFunc("GET /search/issues", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !bearer(w, r) {
			return
		}
		q := r.URL.Query()
		var hits []githubPull
		if strings.Contains(q.Get("q"), "type:pr involves:"+githubLogin) {
			floor := ""
			for _, term := range strings.Fields(q.Get("q")) {
				if v, ok := strings.CutPrefix(term, "updated:>="); ok {
					floor = v
				}
			}
			for _, p := range f.pulls {
				if p.updated >= floor {
					hits = append(hits, p)
				}
			}
		}
		page, _ := strconv.Atoi(q.Get("page"))
		per, _ := strconv.Atoi(q.Get("per_page"))
		if page < 1 {
			page = 1
		}
		if per < 1 {
			per = 30
		}
		lo, hi := min((page-1)*per, len(hits)), min(page*per, len(hits))
		items := []any{}
		for _, p := range hits[lo:hi] {
			items = append(items, map[string]any{
				"id": p.issueID(), "node_id": p.nodeID(), "number": p.number,
				"title": fmt.Sprintf("pull %d", p.number), "state": "open",
				"url":            p.apiURL("issues"),
				"html_url":       fmt.Sprintf("https://github.com/acme/widgets/pull/%d", p.number),
				"repository_url": "https://api.github.com/repos/acme/widgets",
				"updated_at":     p.updated, "created_at": p.updated,
				"user":         user,
				"pull_request": map[string]any{"url": p.apiURL("pulls"), "merged_at": nil},
			})
		}
		writeJSON(w, map[string]any{
			"total_count": len(hits), "incomplete_results": false, "items": items,
		})
	})
	mux.HandleFunc("GET /repos/acme/widgets", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if bearer(w, r) {
			writeJSON(w, map[string]any{
				"id": 500, "node_id": "R_500", "name": "widgets", "full_name": "acme/widgets",
				"private": false,
				"owner":   map[string]any{"login": "acme", "id": 2, "type": "Organization"},
			})
		}
	})
	mux.HandleFunc("GET /repos/acme/widgets/pulls/{n}", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !bearer(w, r) {
			return
		}
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil || n < 1 || n > len(f.pulls) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		p := f.pulls[n-1]
		writeJSON(w, map[string]any{
			"id": p.pullID(), "node_id": p.nodeID(), "number": p.number,
			"title": fmt.Sprintf("pull %d", p.number), "state": "open",
			"url":        p.apiURL("pulls"),
			"html_url":   fmt.Sprintf("https://github.com/acme/widgets/pull/%d", p.number),
			"updated_at": p.updated, "created_at": p.updated,
			"user": user, "requested_reviewers": []any{}, "requested_teams": []any{},
		})
	})
	mux.HandleFunc("GET /repos/acme/widgets/pulls/{n}/reviews", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if bearer(w, r) {
			writeJSON(w, []any{})
		}
	})
	f.serve(t, mux)
}

// involvesSearches is every `type:pr involves:` search the fake answered
// from request number `from` on, in order, as its decoded query.
func (f *fakeGitHub) involvesSearches(t *testing.T, from int) []url.Values {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []url.Values
	for i := from; i < len(f.paths); i++ {
		if f.paths[i] != "/search/issues" {
			continue
		}
		q, err := url.ParseQuery(f.queries[i])
		if err != nil {
			t.Fatalf("decode %q: %v", f.queries[i], err)
		}
		if strings.Contains(q.Get("q"), "involves:") {
			out = append(out, q)
		}
	}
	return out
}

// requests is how many requests the fake has answered.
func (f *fakeGitHub) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.paths)
}

// githubConfig is the injected config one run receives: the account as it is
// stored, its token, and the API base pointed at the fake.
func githubConfig(t *testing.T, ds substrate.Dataset, fake *fakeGitHub) map[string]any {
	t.Helper()
	row := mustGet(t, ds, githubAccountType, githubAccount)
	return map[string]any{
		"accounts": []any{map[string]any{
			"id": githubAccount, "kind": githubAccountType,
			"properties": row.Properties, "token": fakeToken,
		}},
		"inputs": map[string]any{"client": map[string]any{
			"properties": map[string]any{"apiBase": fake.ts.URL},
		}},
	}
}

// newFakeGitHub starts a fake serving n pull requests, one minute apart in
// update time, the oldest two days ago.
func newFakeGitHub(t *testing.T, n int) *fakeGitHub {
	t.Helper()
	fake := &fakeGitHub{}
	start := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	for i := 1; i <= n; i++ {
		fake.pulls = append(fake.pulls, githubPull{
			number:  i,
			updated: start.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
		})
	}
	fake.start(t)
	return fake
}

// TestGitHubSyncWritesThePullRequestsOwnURL: a search hit is the Issue
// representation, whose `url` is `/issues/{n}`, and the pull read says
// `/pulls/{n}`. Before the fix the search stage wrote the first and the
// hydrate the second, so `url` changed twice on every pull request on every
// walk and fired every trigger on the kind each time.
func TestGitHubSyncWritesThePullRequestsOwnURL(t *testing.T) {
	requireUV(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, githubDir, nil)
	fake := newFakeGitHub(t, 3)
	mustPut(t, ds, substrate.PutInput{
		Kind: githubAccountType, ID: githubAccount,
		Properties: syncProps(nil, "enabledPullRequests"),
	})

	s := newStepper(t, ds, githubSyncFn, githubConfig(t, ds, fake))
	var written int
	for _, ef := range s.drainApplying(nil) {
		if ef.Kind != githubPullType {
			continue
		}
		addr, ok := ef.Properties["url"]
		if !ok {
			continue
		}
		written++
		if str, _ := addr.(string); !strings.Contains(str, "/pulls/") {
			t.Fatalf("a %s on pull request %s wrote url %v, want its /pulls/ address", ef.Action, ef.ID, addr)
		}
	}
	if written == 0 {
		t.Fatal("no effect wrote a pull request's url")
	}
	if n := countLive(t, ds, githubPullType); n != 3 {
		t.Fatalf("%d pull requests are mirrored, want 3", n)
	}
}

// TestGitHubSyncStopsOnTheDrainBudgetAndResumesTheWalk is the checkpoint: a
// run whose chain has spent the drain budget ends the chain itself, with the
// walk's position saved on the account, and the next run hydrates what the
// first found and then resumes the search there rather than at its floor.
// Before the fix the body knew nothing of the chain's age: it returned the
// next middle page (which the engine parks and drops), and a following run,
// if the cadence even called it due, searched again from the floor.
func TestGitHubSyncStopsOnTheDrainBudgetAndResumesTheWalk(t *testing.T) {
	requireUV(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, githubDir, nil)

	fake := newFakeGitHub(t, githubPulls)
	mustPut(t, ds, substrate.PutInput{
		Kind: githubAccountType, ID: githubAccount,
		Properties: syncProps(nil, "enabledPullRequests"),
	})

	// Run one: the user stage, then the first search page of 100.
	s := newStepper(t, ds, githubSyncFn, githubConfig(t, ds, fake))
	_, _, cur := s.step(nil)
	s.apply()
	if cur == nil || cur["stage"] != "pulls" {
		t.Fatalf("after the user stage the cursor = %v, want the pulls search", cur)
	}
	_, _, cur = s.step(cur)
	s.apply()
	if cur == nil || cur["stage"] != "pulls" || fmt.Sprint(cur["page"]) != "2" {
		t.Fatalf("after one search page the cursor = %v, want pulls page 2", cur)
	}
	runStart, _ := cur["runStart"].(string)
	boundary := fake.pulls[99].updated

	// The chain has now been running for ten minutes: the next page must end
	// it, doing no GitHub work, and save the walk.
	cur["chainStart"] = time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	cur["startedAt"] = cur["chainStart"]
	before := fake.requests()
	effects, _, next := s.step(cur)
	s.apply()
	if next != nil {
		t.Fatalf("a page past the drain budget returned another page (%v): the engine parks it and drops its effects", next)
	}
	if n := fake.requests() - before; n != 0 {
		t.Fatalf("the stopping page made %d GitHub requests, want none", n)
	}
	stamp := accountStamp(t, effects, githubAccountType, githubAccount)
	walk, _ := stamp["syncWalk"].(map[string]any)
	if walk == nil {
		t.Fatalf("the stop saved no syncWalk: %v", stamp)
	}
	at, _ := walk["at"].(map[string]any)
	if at["stage"] != "pulls" || fmt.Sprint(at["page"]) != "1" || at["pfloor"] != boundary {
		t.Fatalf("syncWalk.at = %v, want pulls from the newest mirrored update %s, page 1", at, boundary)
	}
	if stages, _ := walk["stages"].([]any); len(stages) == 0 || stages[0] != "pulls" {
		t.Fatalf("syncWalk.stages = %v, want the walk from pulls on", walk["stages"])
	}
	if walk["runStart"] != runStart {
		t.Fatalf("syncWalk.runStart = %v, want the walk's own start %s", walk["runStart"], runStart)
	}
	if stamp["lastSyncedAt"] == nil {
		t.Fatalf("the stop did not stamp lastSyncedAt: %v", stamp)
	}

	// The search stage wrote each pull request's OWN address, not the Issue
	// representation's `/issues/{n}` that the hydrate would flip back.
	pulls := listLive(t, ds, githubPullType)
	if len(pulls) != 100 {
		t.Fatalf("after one search page %d pull requests are mirrored, want 100", len(pulls))
	}
	for _, row := range pulls {
		if addr, _ := row.Properties["url"].(string); !strings.Contains(addr, "/pulls/") {
			t.Fatalf("pull request %s url = %q from the search hit, want its /pulls/ address", row.ID, addr)
		}
	}

	// Run two: the account was stamped a moment ago and syncs hourly, so only
	// the saved walk makes it due. It drains the backlog, then resumes the
	// search at the saved boundary and walks to the end.
	from := fake.requests()
	s2 := newStepper(t, ds, githubSyncFn, githubConfig(t, ds, fake))
	all := s2.drainApplying(nil)
	assertFirstSearchFrom(t, fake, from, boundary, "the run after the stop")
	final := accountStamp(t, all, githubAccountType, githubAccount)
	if v, ok := final["syncWalk"]; !ok || v != nil {
		t.Fatalf("the completed walk left syncWalk = %v (present %v), want it cleared", v, ok)
	}
	cursors, _ := final["syncCursors"].(map[string]any)
	if cursors["pulls"] != runStart {
		t.Fatalf("syncCursors.pulls = %v, want the first run's start %s", cursors["pulls"], runStart)
	}
	pulls = listLive(t, ds, githubPullType)
	if len(pulls) != githubPulls {
		t.Fatalf("%d pull requests are mirrored, want %d", len(pulls), githubPulls)
	}
	for _, row := range pulls {
		if addr, _ := row.Properties["url"].(string); !strings.Contains(addr, "/pulls/") {
			t.Fatalf("pull request %s url = %q after hydration, want its /pulls/ address", row.ID, addr)
		}
	}
	hydrated := 0
	for _, p := range fake.seenPaths() {
		if strings.HasPrefix(p, "/repos/acme/widgets/pulls/") && !strings.HasSuffix(p, "/reviews") {
			hydrated++
		}
	}
	if hydrated < githubPulls {
		t.Fatalf("%d pull request reads, want every one of the %d hydrated", hydrated, githubPulls)
	}

	// Run three, asked for by the account's own update: with the walk done
	// the search starts again from the incremental watermark, as before.
	from = fake.requests()
	s3 := newStepper(t, ds, githubSyncFn, githubConfig(t, ds, fake))
	s3.setEnvelope(accountEnvelope())
	s3.drainApplying(nil)
	assertFirstSearchFrom(t, fake, from, minusOverlap(t, runStart), "the run after the walk")
}

// TestGitHubSyncStopInHydrationResumesTheBacklogThenWalks: a run that spends
// the drain budget after its searches owes only hydration. It leaves the
// backlog on `syncPending` and a `syncWalk` marker without a search, which
// makes the account due on the next hourly tick; that run drains the backlog
// and then walks afresh from the watermarks the searches saved. Before the
// fix the chain ran on into a middle page the engine parks, and an hourly
// account stamped a moment ago was not due again for an hour.
func TestGitHubSyncStopInHydrationResumesTheBacklogThenWalks(t *testing.T) {
	requireUV(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, githubDir, nil)
	fake := newFakeGitHub(t, 3)
	mustPut(t, ds, substrate.PutInput{
		Kind: githubAccountType, ID: githubAccount,
		Properties: syncProps(nil, "enabledPullRequests"),
	})

	s := newStepper(t, ds, githubSyncFn, githubConfig(t, ds, fake))
	var cur map[string]any
	for range 8 {
		_, _, cur = s.step(cur)
		s.apply()
		if cur == nil || cur["stage"] == "pullsFetch" {
			break
		}
	}
	if cur == nil || cur["stage"] != "pullsFetch" {
		t.Fatalf("the walk never reached pullsFetch: %v", cur)
	}
	runStart, _ := cur["runStart"].(string)
	cur["chainStart"] = time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	effects, _, next := s.step(cur)
	s.apply()
	if next != nil {
		t.Fatalf("a page past the drain budget returned another page: %v", next)
	}
	stamp := accountStamp(t, effects, githubAccountType, githubAccount)
	walk, _ := stamp["syncWalk"].(map[string]any)
	stages, _ := walk["stages"].([]any)
	if len(stages) == 0 || stages[0] != "pullsFetch" {
		t.Fatalf("syncWalk = %v, want the rest of the walk from pullsFetch", walk)
	}
	pending, _ := stamp["syncPending"].(map[string]any)
	if rows, _ := pending["pulls"].([]any); len(rows) != 3 {
		t.Fatalf("syncPending = %v, want the three pull requests still to hydrate", pending)
	}

	// The next scheduled tick: stamped a moment ago, hourly, due only for the
	// marker. The backlog drains, and the searches run from their watermarks.
	from := fake.requests()
	s2 := newStepper(t, ds, githubSyncFn, githubConfig(t, ds, fake))
	all := s2.drainApplying(nil)
	assertFirstSearchFrom(t, fake, from, minusOverlap(t, runStart), "the run after a hydration stop")
	final := accountStamp(t, all, githubAccountType, githubAccount)
	if v, ok := final["syncWalk"]; !ok || v != nil {
		t.Fatalf("the completed walk left syncWalk = %v (present %v), want it cleared", v, ok)
	}
	hydrated := 0
	for _, p := range fake.seenPaths()[from:] {
		if strings.HasPrefix(p, "/repos/acme/widgets/pulls/") && !strings.HasSuffix(p, "/reviews") {
			hydrated++
		}
	}
	if hydrated < 3 {
		t.Fatalf("the second run read %d pull requests, want the three the first left", hydrated)
	}
}

// accountEnvelope is the delivery envelope a record trigger on the account
// carries: the body syncs the account it names, whatever the cadence says.
func accountEnvelope() map[string]any {
	return map[string]any{"record": map[string]any{"kind": githubAccountType, "id": githubAccount}}
}

// minusOverlap is a watermark as the body queries it back: 120 seconds early.
func minusOverlap(t *testing.T, ts string) string {
	t.Helper()
	at, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatalf("parse watermark %q: %v", ts, err)
	}
	return at.Add(-120 * time.Second).UTC().Format(time.RFC3339)
}

// assertFirstSearchFrom asserts the first `involves:` search since request
// number `from` asked for page 1 from `floor`.
func assertFirstSearchFrom(t *testing.T, fake *fakeGitHub, from int, floor, which string) {
	t.Helper()
	searches := fake.involvesSearches(t, from)
	if len(searches) == 0 {
		t.Fatalf("%s searched nothing", which)
	}
	want := "updated:>=" + floor
	if first := searches[0]; !strings.HasSuffix(first.Get("q"), want) || first.Get("page") != "1" {
		t.Fatalf("%s first searched q %q page %s, want page 1 of %s",
			which, first.Get("q"), first.Get("page"), want)
	}
}
