package engine

// The Notion bundle — the sync-only workspace mirror, from the shipped
// closure at ../../kinds/notion.bundles.substrate.reamde.dev. Three tests:
//
//  1. TestNotionBundleAdmitsSchema — the closure ADMITS through the schema
//     loader: the bundle declares one `connector` input injected into its
//     functions (and the config deliberately NOT oauth2 —
//     the bundle authenticates by pasted integration token because the host's
//     token exchange speaks AuthStyleInParams only, never Notion's required
//     Basic auth), the account wears accountconfig, the page mirror carries
//     its optional `parent: any` edge, and the bundle ships no oauth2
//     manifest block. No DB, no uv — pure schema admission.
//
//  2. TestNotionBundleInstallsAndSyncs — the whole closure installs into a
//     live repository (uv warms the PEP 723 body, so it SKIPS when uv is absent
//     or cannot provision) and then drives the sync END TO END against a
//     fake Notion API (2025-09-03, data-source results) reached through the
//     config's apiBase seam, in stages:
//
//     - a mid-drain account disable ends the drain with FRESH state: no
//       stale search cursor is ever replayed, no pending link leaks, and
//       nothing parks;
//     - the full first sync: the search phase lands rows, the BLOCKS phase
//       patches content on its own paged leg, the links phase resolves the
//       parent whose target mirrored after the child, and the account is
//       stamped;
//     - the delta short-circuit: a direct-call re-sync (the call path
//       drains fully in one invocation) fetches no blocks and bumps no
//       version;
//     - pendingParent repair: a parent shared LATER gets its edge even
//       though the child's content is unchanged;
//     - cutoff early-stop: the descending walk ends at the first
//       below-cutoff result and never requests the next search page;
//     - one-account enforcement: a second account row is stamped
//       "ignored: duplicate account …" and never syncs;
//     - the erroring stamp: a provider failure writes
//       `syncStatus: erroring: …` instead of parking, and the next healthy
//       sync restores "ok".
//
//  3. TestNotionSyncResolvesParentsBesideASecondPageType — the same install
//     with the web bundle beside it, so `page` names two types and every
//     read the sync makes must be a FULL identity. It also plants the
//     pre-destutter pendingParent shape and proves the repair still lands.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/runner/substratefn"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	notionExampleDir  = "../../kinds/notion.bundles.substrate.reamde.dev"
	notionAuthority   = "notion.bundles.substrate.reamde.dev"
	notionBundleRow   = notionAuthority + "/notion"
	notionConfigType  = notionAuthority + "/config"
	notionAccountType = notionAuthority + "/account"
	notionPageType    = notionAuthority + "/page"
	notionDBType      = notionAuthority + "/database"
	notionSyncFn      = notionAuthority + "/workspacesync"

	// The pinned wire version the body must speak — the fake 400s anything
	// older, because 2022-06-28 clients silently lose multi-source databases
	// from search.
	notionAPIVersion = "2025-09-03"

	// 32-hex Notion object ids (the fake API serves them dashed, the way
	// Notion does; the body normalizes). notionDSID is a DATA SOURCE — under
	// 2025-09-03 search returns data_source objects, each naming its
	// containing database (notionDBID).
	notionDSID  = "11111111111111111111111111111111"
	notionDBID  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	notionPg1ID = "22222222222222222222222222222222"
	notionPg2ID = "33333333333333333333333333333333"
	notionPg4ID = "44444444444444444444444444444444"
	notionPg5ID = "55555555555555555555555555555555"
	notionNewID = "66666666666666666666666666666666"
	notionOldID = "77777777777777777777777777777777"

	// The exact duplicate-account stamp the body writes (F8).
	notionDuplicateNote = "ignored: duplicate account (this provider is one-account-per-repository until issue 011)"
)

// notionDashed renders the canonical 8-4-4-4-12 form the API speaks.
func notionDashed(id string) string {
	return id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:32]
}

// TestNotionBundleAdmitsSchema loads the builtin schema, then installs the
// bundle closure on top of it through the ordinary loader/resolver — the same
// admission the batch apply runs, minus the function-body warm.
func TestNotionBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	// The registry an install actually admits into: the seeded tree (core
	// alone) plus the shipped VOCABULARY bundles this repository imported —
	// what a closure declaring onto people/tasks/messaging/calendar/media
	// needs present, and what `requires:` names.
	reg, err := enginetest.SeededRegistry("../../kinds/core.substrate.reamde.dev")
	if err != nil {
		t.Fatalf("build the repository registry: %v", err)
	}
	data, err := os.ReadFile(notionExampleDir + "/bundle.yaml")
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

	// The bundle exists, declares the one `connector` input injected into
	// its functions, and — the token model — declares NO oauth2 manifest
	// block: the host's exchange cannot speak Notion's Basic-auth token
	// endpoint, so nothing here pretends to.
	b, ok := reg.BundleOf(notionAuthority)
	if !ok {
		t.Fatalf("no bundle owns %s after install", notionAuthority)
	}
	in, ok := b.Inputs["connector"]
	if !ok {
		t.Fatalf("bundle declares no connector input: %v", b.InputOrder)
	}
	if in.Kind != notionConfigType {
		t.Fatalf("connector input kind = %q, want %q", in.Kind, notionConfigType)
	}
	if in.Inject != vocabulary.BundleInputInjectFunctions {
		t.Fatalf("connector input inject = %q, want %q", in.Inject, vocabulary.BundleInputInjectFunctions)
	}
	if b.OAuth2 != nil {
		t.Fatalf("bundle declares oauth2 provider metadata — the token model must not")
	}

	// The config type: NOT oauth2 — its integrationToken is the connector's
	// own secret, injected plaintext.
	cfg, ok := reg.ByIdentity(notionConfigType)
	if !ok {
		t.Fatalf("config type %s missing", notionConfigType)
	}
	if cfg.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("%s implements oauth2 — a token-model bundle must not (the loader pairs the trait with an oauth2 manifest block)", notionConfigType)
	}

	// The account type: accountconfig (the console's accounts view and the
	// runner's config resolution), and not oauth2.
	acct, ok := reg.ByIdentity(notionAccountType)
	if !ok {
		t.Fatalf("account type %s missing", notionAccountType)
	}
	if !acct.Implements(vocabulary.TraitAccountConfigCore) {
		t.Fatalf("%s does not implement %s", notionAccountType, vocabulary.TraitAccountConfigCore)
	}
	if acct.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("%s implements oauth2 — nothing in this bundle does", notionAccountType)
	}

	// The page mirror carries its optional polymorphic parent edge — a page's
	// parent is a page OR a data source, so the target is `any` and never
	// required (workspace-parented pages have none).
	page, ok := reg.ByIdentity(notionPageType)
	if !ok {
		t.Fatalf("page type %s missing", notionPageType)
	}
	ed, ok := page.Prop("parent")
	if !ok {
		t.Fatalf("%s declares no `parent` reference", notionPageType)
	}
	// UNPINNED: a page's parent is a page or a data source, so the declaration
	// names no kind and the value carries one.
	if ed.To != "" || ed.Required || ed.Repeated {
		t.Fatalf("parent reference shape wrong: kind=%q required=%v repeated=%v", ed.To, ed.Required, ed.Repeated)
	}
	if _, ok := reg.ByIdentity(notionDBType); !ok {
		t.Fatalf("database type %s missing", notionDBType)
	}

	// The sync function is a member of the authority.
	if _, err := reg.ResolveFunction(notionSyncFn); err != nil {
		t.Fatalf("sync function %s did not register: %v", notionSyncFn, err)
	}
}

// notionSearchPage is one fake search response: its results and the cursor
// it hands out ("" means the feed ends here). A non-empty next must be
// "cursor-N" where N indexes the page it leads to.
type notionSearchPage struct {
	results []map[string]any
	next    string
}

// notionFake is the Notion API in a box: a reconfigurable search feed and a
// block-children endpoint that counts its hits — the delta short-circuit's
// observable. It enforces the pinned 2025-09-03 version and records every
// start_cursor it is asked for, so a replayed stale cursor is visible.
type notionFake struct {
	ts *httptest.Server

	mu         sync.Mutex
	searches   int
	blockHits  int
	cursors    []string
	pages      []notionSearchPage
	onSearch   func(n int)
	failSearch bool
}

func (f *notionFake) blockCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.blockHits
}

func (f *notionFake) searchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.searches
}

func (f *notionFake) seenCursors() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cursors...)
}

func (f *notionFake) setPages(pages ...notionSearchPage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages = pages
}

func (f *notionFake) setOnSearch(hook func(n int)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onSearch = hook
}

func (f *notionFake) setFail(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failSearch = fail
}

func newNotionFake(t *testing.T) *notionFake {
	t.Helper()
	f := &notionFake{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "search is POST", http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "no bearer token", http.StatusUnauthorized)
			return
		}
		if v := r.Header.Get("Notion-Version"); v != notionAPIVersion {
			// The old pin loses multi-source databases; the fake holds the
			// body to the data-source version.
			http.Error(w, "unsupported Notion-Version "+v, http.StatusBadRequest)
			return
		}
		var body struct {
			StartCursor string `json:"start_cursor"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.searches++
		n := f.searches
		f.cursors = append(f.cursors, body.StartCursor)
		hook := f.onSearch
		fail := f.failSearch
		pages := append([]notionSearchPage(nil), f.pages...)
		f.mu.Unlock()
		if hook != nil {
			hook(n)
		}
		if fail {
			http.Error(w, "notion is down", http.StatusInternalServerError)
			return
		}
		idx := 0
		if body.StartCursor != "" {
			i, err := strconv.Atoi(strings.TrimPrefix(body.StartCursor, "cursor-"))
			if err != nil {
				http.Error(w, "unknown cursor "+body.StartCursor, http.StatusBadRequest)
				return
			}
			idx = i
		}
		if idx >= len(pages) {
			http.Error(w, "cursor past the feed", http.StatusBadRequest)
			return
		}
		p := pages[idx]
		resp := map[string]any{
			"object": "list", "results": p.results,
			"has_more": p.next != "",
		}
		if p.next != "" {
			resp["next_cursor"] = p.next
		} else {
			resp["next_cursor"] = nil
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/v1/blocks/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.blockHits++
		f.mu.Unlock()
		blocks := []map[string]any{
			{
				"object": "block", "type": "heading_1", "has_children": false,
				"heading_1": map[string]any{"rich_text": []map[string]any{{"plain_text": "Heading"}}},
			},
			{
				"object": "block", "type": "paragraph", "has_children": false,
				"paragraph": map[string]any{"rich_text": []map[string]any{{"plain_text": "Some paragraph text."}}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list", "results": blocks, "has_more": false, "next_cursor": nil,
		})
	})
	f.ts = httptest.NewServer(mux)
	t.Cleanup(f.ts.Close)
	return f
}

// notionTS renders a UTC time the way Notion does.
func notionTS(at time.Time) string {
	return at.UTC().Format("2006-01-02T15:04:05.000") + "Z"
}

// notionDataSource builds a 2025-09-03 data_source search result naming its
// containing database.
func notionDataSource(dsID, dbID, edited, title string) map[string]any {
	return map[string]any{
		"object": "data_source", "id": notionDashed(dsID),
		"last_edited_time": edited, "archived": false,
		"url":    "https://www.notion.so/" + dsID,
		"parent": map[string]any{"type": "database_id", "database_id": notionDashed(dbID)},
		"title":  []map[string]any{{"plain_text": title}},
	}
}

// notionPageObj builds a page search result; parent may be nil (workspace).
func notionPageObj(id, edited, title string, parent map[string]any) map[string]any {
	p := map[string]any{
		"object": "page", "id": notionDashed(id),
		"last_edited_time": edited, "archived": false,
		"url": "https://www.notion.so/" + id,
		"properties": map[string]any{
			"title": map[string]any{"type": "title", "title": []map[string]any{{"plain_text": title}}},
		},
	}
	if parent != nil {
		p["parent"] = parent
	} else {
		p["parent"] = map[string]any{"type": "workspace", "workspace": true}
	}
	return p
}

// TestNotionBundleInstallsAndSyncs applies the whole closure into a live
// repository, asserts every member installs, then drives the staged scenarios
// described in the file header against the fake API. It warms the PEP 723
// sync body through uv, so it skips when uv is absent or cannot provision.
func TestNotionBundleInstallsAndSyncs(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the sync body warms through uv at install")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)

	// The atomic install from the shipped manifest. A schema problem is
	// already caught deterministically by the loader test above, so an apply
	// error here is treated as a uv provisioning failure (offline) and skips.
	vocabularyDocs := loadYAMLDocs(t, notionExampleDir+"/bundle.yaml")
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, vocabularyDocs); err != nil {
		if isUVProvisionError(err) {
			t.Skipf("bundle install could not warm the PEP 723 body (uv offline?): %v", err)
		}
		t.Fatalf("install the notion bundle: %v", err)
	}

	// The bundle row and every schema member landed as its own record.
	for id, wantType := range map[string]string{
		notionBundleRow:   "core.substrate.reamde.dev/bundle",
		notionConfigType:  "core.substrate.reamde.dev/kind",
		notionAccountType: "core.substrate.reamde.dev/kind",
		notionPageType:    "core.substrate.reamde.dev/kind",
		notionDBType:      "core.substrate.reamde.dev/kind",
		notionSyncFn:      "core.substrate.reamde.dev/function",
	} {
		row, err := ds.Get(ctx, wantType, id)
		if err != nil {
			t.Fatalf("member %s did not install: %v", id, err)
		}
		if row.Kind != wantType {
			t.Fatalf("member %s is a %s, want %s", id, row.Kind, wantType)
		}
	}

	// Computed status: installed, enabled, one function, the connector input
	// unresolved so far.
	st, err := ds.BundleStatus(ctx, notionAuthority)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if !st.Installed || !st.Enabled {
		t.Fatalf("bundle not live: installed=%v enabled=%v", st.Installed, st.Enabled)
	}
	if len(st.Inputs) != 1 || st.Inputs[0].Name != "connector" || st.Inputs[0].Kind != notionConfigType {
		t.Fatalf("status inputs = %+v, want the one connector input", st.Inputs)
	}
	if st.Inputs[0].Record != "" || st.Inputs[0].Via != "" {
		t.Fatalf("connector input resolved with no config record created: %+v", st.Inputs[0])
	}
	if len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupMissing || st.Setup[0].Input != "connector" {
		t.Fatalf("status setup = %+v, want the one missing-input item", st.Setup)
	}
	if st.Functions != 1 {
		t.Fatalf("status functions = %d, want 1", st.Functions)
	}

	// The delivery wiring installs as ordinary data records.
	for _, m := range loadYAMLDocs(t, notionExampleDir+"/triggers.yaml") {
		putDataDoc(t, ds, m)
	}
	for _, id := range []string{"notion-on-connect", "notion-scheduled"} {
		row, err := ds.Get(ctx, typeTrigger, id)
		if err != nil {
			t.Fatalf("trigger %s did not install: %v", id, err)
		}
		if row.Kind != typeTrigger {
			t.Fatalf("trigger %s is a %s", id, row.Kind)
		}
	}

	// --- the fake API and the one config record (token + apiBase seam) -----
	fake := newNotionFake(t)
	mustPutInternal(t, ds, substrate.PutInput{
		Kind: notionConfigType,
		Properties: map[string]any{
			"integrationToken": "secret-notion-integration-token",
			"apiBase":          fake.ts.URL,
		},
	})

	recent := notionTS(time.Now().Add(-2 * time.Hour))

	// --- stage 1: a mid-drain disable ends the drain with FRESH state ------
	// The feed dangles a second page behind "cursor-1"; the search handler's
	// hook disables the account after the FIRST search, so the resumed
	// invocation finds its queue head gone. The regression this proves dead:
	// the old skip-loop kept the previous drain's search cursor and pending
	// links alive — now the drain must end cleanly, with exactly one search
	// (never a replay of cursor-1 under fresh filters), page-one effects
	// committed, no stamp, and nothing parked.
	fake.setPages(
		notionSearchPage{
			results: []map[string]any{notionDataSource(notionDSID, notionDBID, recent, "Projects")},
			next:    "cursor-1",
		},
		notionSearchPage{results: nil, next: ""},
	)
	var flipOnce sync.Once
	fake.setOnSearch(func(n int) {
		flipOnce.Do(func() {
			if _, err := ds.Patch(ctx, substrate.ActorAPI, notionAccountType, "notion-acct-1", substrate.PatchInput{
				Properties: map[string]any{"enabledPages": false, "enabledDatabases": false},
			}); err != nil {
				t.Errorf("mid-drain disable: %v", err)
			}
		})
	})

	acct := mustPutInternal(t, ds, substrate.PutInput{
		Kind: notionAccountType, ID: "notion-acct-1",
		Properties: map[string]any{
			"displayName": "Test workspace", "enabledPages": true,
			"enabledDatabases": true, "syncFrequency": "hourly", "backfillDepth": "all",
		},
	})
	parkedBefore := parkedFailures(t, ds)
	drainTriggers(t, ds)

	if got := fake.searchCount(); got != 1 {
		t.Fatalf("mid-drain disable: %d searches, want exactly 1 (a stale cursor was replayed)", got)
	}
	if cs := fake.seenCursors(); len(cs) != 1 || cs[0] != "" {
		t.Fatalf("mid-drain disable: search cursors %v, want one empty", cs)
	}
	db := substratefn.ExternalID("notion", acct.ID, notionDSID)
	if _, err := ds.Get(ctx, notionDBType, db); err != nil {
		t.Fatalf("page-one effects did not commit before the disable: %v", err)
	}
	if row := mustGetInternal(t, ds, notionAccountType, acct.ID); row.Properties["lastSyncedAt"] != nil {
		t.Fatalf("a half-drained account must not be stamped: lastSyncedAt=%v", row.Properties["lastSyncedAt"])
	}
	if got := parkedFailures(t, ds); got != parkedBefore {
		t.Fatalf("mid-drain disable parked a delivery: %d -> %d", parkedBefore, got)
	}
	fake.setOnSearch(nil)

	// --- stage 2: the full first sync (search -> blocks -> links) ----------
	// Feed order matters twice: pg2 precedes its parent pg1 (exercising the
	// deferred links phase), pg1's parent is the data source (inline via the
	// staged set), and pg4's parent pg5 is NOT shared at all — it must land
	// as a stored pendingParent, not a silent skip.
	fake.setPages(notionSearchPage{results: []map[string]any{
		notionDataSource(notionDSID, notionDBID, recent, "Projects"),
		notionPageObj(notionPg2ID, recent, "Meeting notes",
			map[string]any{"type": "page_id", "page_id": notionDashed(notionPg1ID)}),
		notionPageObj(notionPg1ID, recent, "Hello world",
			map[string]any{
				"type":           "data_source_id",
				"data_source_id": notionDashed(notionDSID),
				"database_id":    notionDashed(notionDBID),
			}),
		notionPageObj(notionPg4ID, recent, "Orphan child",
			map[string]any{"type": "page_id", "page_id": notionDashed(notionPg5ID)}),
	}})
	if _, err := ds.Patch(ctx, substrate.ActorAPI, acct.Kind, acct.ID, substrate.PatchInput{
		Properties: map[string]any{"enabledPages": true, "enabledDatabases": true},
	}); err != nil {
		t.Fatalf("re-enable the account: %v", err)
	}
	drainTriggers(t, ds)

	pg1 := substratefn.ExternalID("notion", acct.ID, notionPg1ID)
	pg2 := substratefn.ExternalID("notion", acct.ID, notionPg2ID)
	pg4 := substratefn.ExternalID("notion", acct.ID, notionPg4ID)
	pg5 := substratefn.ExternalID("notion", acct.ID, notionPg5ID)

	// The data-source mirror: keyed by the data source, naming its
	// containing database — the 2025-09-03 model, where a multi-source
	// database yields one mirror per source instead of vanishing.
	dbe, err := ds.Get(ctx, notionDBType, db)
	if err != nil {
		t.Fatalf("data-source mirror did not land: %v", err)
	}
	if dbe.Title != "Projects" || dbe.Properties["dataSourceId"] != notionDSID {
		t.Fatalf("data-source mirror wrong: title=%q dataSourceId=%v", dbe.Title, dbe.Properties["dataSourceId"])
	}
	if dbe.Properties["databaseId"] != notionDBID {
		t.Fatalf("data-source mirror does not model its parent database: databaseId=%v", dbe.Properties["databaseId"])
	}

	p1, err := ds.Get(ctx, notionPageType, pg1)
	if err != nil {
		t.Fatalf("page mirror pg1 did not land: %v", err)
	}
	if p1.Title != "Hello world" {
		t.Fatalf("pg1 title = %q, want Hello world", p1.Title)
	}
	// Content arrives through the BLOCKS phase — its own paged leg after the
	// search walk — so its presence proves the phase handoff.
	content, _ := p1.Properties["content"].(string)
	if !strings.Contains(content, "# Heading") || !strings.Contains(content, "Some paragraph text.") {
		t.Fatalf("pg1 content not normalized: %q", content)
	}
	if storedReferencePath(p1.Properties["account"]) != notionAccountType+"/"+acct.ID || p1.Properties["archived"] != false {
		t.Fatalf("pg1 props wrong: account=%v archived=%v", p1.Properties["account"], p1.Properties["archived"])
	}
	if _, ok := p1.Properties["lastEditedAt"]; !ok {
		t.Fatalf("pg1 carries no lastEditedAt")
	}
	// pg1's parent (the data source) appeared BEFORE it in the feed: inline edge.
	if tg := parentRef(p1); tg != vocabulary.RecordPath(notionDBType, db) {
		t.Fatalf("pg1 parent = %q, want the data-source mirror %s", tg, db)
	}

	// pg2's parent (pg1) appeared AFTER it in the feed: the deferred links
	// phase resolved the edge once the walk drained, and cleared the stored
	// pendingParent.
	p2, err := ds.Get(ctx, notionPageType, pg2)
	if err != nil {
		t.Fatalf("page mirror pg2 did not land: %v", err)
	}
	if tg := parentRef(p2); tg != notionPageRef(pg1) {
		t.Fatalf("pg2 parent = %q, want the page mirror %s", tg, pg1)
	}
	if _, ok := p2.Properties["pendingParent"]; ok {
		t.Fatalf("pg2 still carries pendingParent after its edge resolved: %v", p2.Properties["pendingParent"])
	}

	// pg4's parent pg5 is not shared with the integration: no edge, but the
	// reference is STORED — pendingParent — so a later sync can repair it.
	p4, err := ds.Get(ctx, notionPageType, pg4)
	if err != nil {
		t.Fatalf("page mirror pg4 did not land: %v", err)
	}
	if tg := parentRef(p4); tg != "" {
		t.Fatalf("pg4 has a parent %q with its parent unshared", tg)
	}
	// The stored kind is the FULL type identity: it is fed straight back to
	// host.records.get on the repair pass, which accepts nothing less.
	pend, ok := p4.Properties["pendingParent"].([]any)
	if !ok || len(pend) != 2 || pend[0] != notionPageType || pend[1] != pg5 {
		t.Fatalf("pg4 pendingParent = %v, want [%s %s]", p4.Properties["pendingParent"], notionPageType, pg5)
	}

	// The account is stamped: the completion marker and the ok status, both
	// connector-written; the on-connect guard has dropped.
	acctRow := mustGetInternal(t, ds, notionAccountType, acct.ID)
	if _, ok := acctRow.Properties["lastSyncedAt"]; !ok {
		t.Fatalf("account carries no lastSyncedAt after the first sync")
	}
	if acctRow.Properties["syncStatus"] != "ok" {
		t.Fatalf("account syncStatus = %v, want ok", acctRow.Properties["syncStatus"])
	}
	if got := fake.blockCount(); got != 3 {
		t.Fatalf("first sync walked %d block trees, want 3 (pg1, pg2, pg4)", got)
	}

	// --- stage 3: the delta short-circuit over an unchanged workspace ------
	// A direct call: the call path has no paged checkpoints, so the body
	// drains the whole account inside this one invocation.
	blocksBefore := fake.blockCount()
	p1Version := p1.Version
	if _, _, err := ds.CallFunction(ctx, notionSyncFn, map[string]any{"account": acct.ID}); err != nil {
		t.Fatalf("manual re-sync: %v", err)
	}
	if got := fake.blockCount(); got != blocksBefore {
		t.Fatalf("re-sync fetched blocks (%d -> %d) — the last_edited_time short-circuit is dead", blocksBefore, got)
	}
	p1Again := mustGetInternal(t, ds, notionPageType, pg1)
	if p1Again.Version != p1Version {
		t.Fatalf("re-sync bumped the unchanged pg1 mirror: version %d -> %d", p1Version, p1Again.Version)
	}
	// The re-sync still stamps the account — completion, not change.
	if got := mustGetInternal(t, ds, notionAccountType, acct.ID).Properties["syncStatus"]; got != "ok" {
		t.Fatalf("re-sync left syncStatus = %v", got)
	}

	// --- stage 4: pendingParent repair when the parent is shared LATER -----
	// pg5 joins the feed (newest first); pg4 is UNCHANGED, so the delta
	// short-circuit skips its content — but the stored pendingParent must
	// still get its edge, independent of that skip.
	fake.setPages(notionSearchPage{results: []map[string]any{
		notionPageObj(notionPg5ID, notionTS(time.Now().Add(-time.Hour)), "Found parent", nil),
		notionDataSource(notionDSID, notionDBID, recent, "Projects"),
		notionPageObj(notionPg2ID, recent, "Meeting notes",
			map[string]any{"type": "page_id", "page_id": notionDashed(notionPg1ID)}),
		notionPageObj(notionPg1ID, recent, "Hello world",
			map[string]any{
				"type":           "data_source_id",
				"data_source_id": notionDashed(notionDSID),
				"database_id":    notionDashed(notionDBID),
			}),
		notionPageObj(notionPg4ID, recent, "Orphan child",
			map[string]any{"type": "page_id", "page_id": notionDashed(notionPg5ID)}),
	}})
	if _, _, err := ds.CallFunction(ctx, notionSyncFn, map[string]any{"account": acct.ID}); err != nil {
		t.Fatalf("repair re-sync: %v", err)
	}
	if _, err := ds.Get(ctx, notionPageType, pg5); err != nil {
		t.Fatalf("pg5 did not mirror once shared: %v", err)
	}
	p4Fixed := mustGetInternal(t, ds, notionPageType, pg4)
	if tg := parentRef(p4Fixed); tg != notionPageRef(pg5) {
		t.Fatalf("pg4 parent = %q after repair, want %s", tg, pg5)
	}
	if _, ok := p4Fixed.Properties["pendingParent"]; ok {
		t.Fatalf("pg4 pendingParent survived its repair: %v", p4Fixed.Properties["pendingParent"])
	}
	if got := fake.blockCount(); got != blocksBefore+1 {
		t.Fatalf("repair walked %d block trees beyond pg5's one (%d -> %d) — the delta skip broke", got-blocksBefore-1, blocksBefore, got)
	}

	// --- stage 5: cutoff early-stop on the descending walk -----------------
	// The feed's first page holds a fresh page then a 2020 relic, and dangles
	// a second page. With backfillDepth last30d the walk must mirror the
	// fresh page, stop AT the relic, and never request the dangled page.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, acct.Kind, acct.ID, substrate.PatchInput{
		Properties: map[string]any{"backfillDepth": "last30d"},
	}); err != nil {
		t.Fatalf("narrow the backfill: %v", err)
	}
	fake.setPages(
		notionSearchPage{
			results: []map[string]any{
				notionPageObj(notionNewID, notionTS(time.Now().Add(-24*time.Hour)), "Fresh page", nil),
				notionPageObj(notionOldID, "2020-01-01T00:00:00.000Z", "Ancient relic", nil),
			},
			next: "cursor-1",
		},
		notionSearchPage{
			results: []map[string]any{notionPageObj(notionOldID, "2019-01-01T00:00:00.000Z", "Deeper history", nil)},
			next:    "",
		},
	)
	searchesBefore := fake.searchCount()
	if _, _, err := ds.CallFunction(ctx, notionSyncFn, map[string]any{"account": acct.ID}); err != nil {
		t.Fatalf("cutoff re-sync: %v", err)
	}
	if got := fake.searchCount(); got != searchesBefore+1 {
		t.Fatalf("cutoff walk ran %d searches, want 1 — the below-cutoff result must end the walk", got-searchesBefore)
	}
	if _, err := ds.Get(ctx, notionPageType, substratefn.ExternalID("notion", acct.ID, notionNewID)); err != nil {
		t.Fatalf("the fresh page did not mirror: %v", err)
	}
	if _, err := ds.Get(ctx, notionPageType, substratefn.ExternalID("notion", acct.ID, notionOldID)); err == nil {
		t.Fatalf("the below-cutoff relic mirrored — the cutoff is dead")
	}
	if got := mustGetInternal(t, ds, notionAccountType, acct.ID).Properties["syncStatus"]; got != "ok" {
		t.Fatalf("cutoff-stopped sync left syncStatus = %v, want ok", got)
	}

	// --- stage 6: one account per repository --------------------------------
	// The config holds ONE workspace token; a second account row would
	// mirror the same workspace twice. The lexicographically-first row is
	// the connection — the second is stamped, never synced.
	searchesBefore = fake.searchCount()
	acct2 := mustPutInternal(t, ds, substrate.PutInput{
		Kind: notionAccountType, ID: "notion-acct-2",
		Properties: map[string]any{
			"displayName": "Duplicate workspace", "enabledPages": true,
			"enabledDatabases": true, "syncFrequency": "hourly", "backfillDepth": "all",
		},
	})
	drainTriggers(t, ds)
	acct2Row := mustGetInternal(t, ds, notionAccountType, acct2.ID)
	if got := acct2Row.Properties["syncStatus"]; got != notionDuplicateNote {
		t.Fatalf("duplicate account syncStatus = %v, want %q", got, notionDuplicateNote)
	}
	if acct2Row.Properties["lastSyncedAt"] != nil {
		t.Fatalf("duplicate account was synced: lastSyncedAt=%v", acct2Row.Properties["lastSyncedAt"])
	}
	if got := fake.searchCount(); got != searchesBefore {
		t.Fatalf("duplicate account reached the provider: %d -> %d searches", searchesBefore, got)
	}
	if _, err := ds.Get(ctx, notionPageType, substratefn.ExternalID("notion", acct2.ID, notionNewID)); err == nil {
		t.Fatalf("duplicate account minted its own mirrors")
	}

	// --- stage 7: provider failure stamps erroring, then recovers ----------
	fake.setFail(true)
	if _, _, err := ds.CallFunction(ctx, notionSyncFn, map[string]any{"account": acct.ID}); err != nil {
		t.Fatalf("erroring sync must degrade, not fail the call: %v", err)
	}
	status, _ := mustGetInternal(t, ds, notionAccountType, acct.ID).Properties["syncStatus"].(string)
	if !strings.HasPrefix(status, "erroring: ") {
		t.Fatalf("provider failure left syncStatus = %q, want an erroring stamp", status)
	}
	fake.setFail(false)
	if _, _, err := ds.CallFunction(ctx, notionSyncFn, map[string]any{"account": acct.ID}); err != nil {
		t.Fatalf("recovery sync: %v", err)
	}
	if got := mustGetInternal(t, ds, notionAccountType, acct.ID).Properties["syncStatus"]; got != "ok" {
		t.Fatalf("recovery left syncStatus = %v, want ok", got)
	}
}

// TestNotionSyncResolvesParentsBesideASecondPageType installs the web bundle
// ALONGSIDE notion, so two authorities declare a type whose local name is `page`,
// and then drives the parent-resolution path.
//
// The regression: the body used to hand `host.records.get` the bare local
// name "page". A bare name resolves only while it is unique across authorities, so
// a notion-only repository passed and a repository with any second `page`-declaring
// bundle got `ambiguous type "page"` — a HostError, which fails the
// invocation and parks the sync after its retries. Both bundles ship and both
// are catalog-installable, so that pairing is an ordinary install.
//
// The second leg covers the PERSISTED side: pendingParent rows written by
// pre-destutter builds hold the legacy singular "notionpage", which resolves
// to nothing at all after the rename. The read normalizes them.
func TestNotionSyncResolvesParentsBesideASecondPageType(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the sync body warms through uv at install")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)

	// Two bundles, two `page` types. Order does not matter; both must admit.
	for _, dir := range []string{exampleDir, notionExampleDir} {
		if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, loadYAMLDocs(t, dir+"/bundle.yaml")); err != nil {
			if isUVProvisionError(err) {
				t.Skipf("bundle install could not warm the PEP 723 bodies (uv offline?): %v", err)
			}
			t.Fatalf("install %s: %v", dir, err)
		}
	}
	// The precondition this test exists for: the bare name is now ambiguous,
	// so anything still passing it to the engine fails.
	if _, err := ds.resolveType("page"); err == nil {
		t.Fatalf("bare \"page\" still resolves — the two-bundle precondition did not hold")
	} else if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("bare \"page\" failed with %v, want an ambiguity", err)
	}
	for _, m := range loadYAMLDocs(t, notionExampleDir+"/triggers.yaml") {
		putDataDoc(t, ds, m)
	}

	fake := newNotionFake(t)
	mustPutInternal(t, ds, substrate.PutInput{
		Kind: notionConfigType,
		Properties: map[string]any{
			"integrationToken": "secret-notion-integration-token",
			"apiBase":          fake.ts.URL,
		},
	})

	recent := notionTS(time.Now().Add(-2 * time.Hour))
	// pg2 precedes its parent pg1 (the deferred links phase), and pg4's
	// parent pg5 is not shared at all (the stored pendingParent). Both legs
	// call host.records.get with the parent's kind.
	fake.setPages(notionSearchPage{results: []map[string]any{
		notionPageObj(notionPg2ID, recent, "Meeting notes",
			map[string]any{"type": "page_id", "page_id": notionDashed(notionPg1ID)}),
		notionPageObj(notionPg1ID, recent, "Hello world", nil),
		notionPageObj(notionPg4ID, recent, "Orphan child",
			map[string]any{"type": "page_id", "page_id": notionDashed(notionPg5ID)}),
	}})
	acct := mustPutInternal(t, ds, substrate.PutInput{
		Kind: notionAccountType, ID: "notion-acct-1",
		Properties: map[string]any{
			"displayName": "Test workspace", "enabledPages": true,
			"enabledDatabases": true, "syncFrequency": "hourly", "backfillDepth": "all",
		},
	})
	parkedBefore := parkedFailures(t, ds)
	drainTriggers(t, ds)
	if got := parkedFailures(t, ds); got != parkedBefore {
		t.Fatalf("the sync parked beside a second `page` type: %d -> %d parked deliveries", parkedBefore, got)
	}

	pg1 := substratefn.ExternalID("notion", acct.ID, notionPg1ID)
	pg2 := substratefn.ExternalID("notion", acct.ID, notionPg2ID)
	pg4 := substratefn.ExternalID("notion", acct.ID, notionPg4ID)
	pg5 := substratefn.ExternalID("notion", acct.ID, notionPg5ID)

	if tg := parentRef(mustGetInternal(t, ds, notionPageType, pg2)); tg != notionPageRef(pg1) {
		t.Fatalf("pg2 parent = %q, want the page mirror %s", tg, pg1)
	}
	pend, ok := mustGetInternal(t, ds, notionPageType, pg4).Properties["pendingParent"].([]any)
	if !ok || len(pend) != 2 || pend[0] != notionPageType || pend[1] != pg5 {
		t.Fatalf("pg4 pendingParent = %v, want [%s %s]", pend, notionPageType, pg5)
	}
	// The web mirror is untouched: nothing wrote across the authority boundary.
	if n := countLivePages(t, ds); n != 0 {
		t.Fatalf("the notion sync minted %d web pages", n)
	}

	// --- the legacy pendingParent shape ------------------------------------
	// Rewrite pg4's stored reference the way a pre-destutter build left it,
	// then mirror pg5 in its own sync so the repair pass cannot short-circuit
	// on `seen` and must ask the engine for it by the stored kind.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, notionPageType, pg4, substrate.PatchInput{
		Properties: map[string]any{"pendingParent": []any{"notionpage", pg5}},
	}); err != nil {
		t.Fatalf("plant the legacy pendingParent: %v", err)
	}
	fake.setPages(notionSearchPage{results: []map[string]any{
		notionPageObj(notionPg5ID, notionTS(time.Now().Add(-time.Hour)), "Found parent", nil),
	}})
	if _, _, err := ds.CallFunction(ctx, notionSyncFn, map[string]any{"account": acct.ID}); err != nil {
		t.Fatalf("mirror pg5: %v", err)
	}
	fake.setPages(notionSearchPage{results: []map[string]any{
		notionPageObj(notionPg4ID, recent, "Orphan child",
			map[string]any{"type": "page_id", "page_id": notionDashed(notionPg5ID)}),
	}})
	if _, _, err := ds.CallFunction(ctx, notionSyncFn, map[string]any{"account": acct.ID}); err != nil {
		t.Fatalf("legacy pendingParent repair: %v", err)
	}
	p4 := mustGetInternal(t, ds, notionPageType, pg4)
	if tg := parentRef(p4); tg != notionPageRef(pg5) {
		t.Fatalf("pg4 parent = %q after the legacy repair, want %s", tg, pg5)
	}
	if _, ok := p4.Properties["pendingParent"]; ok {
		t.Fatalf("the legacy pendingParent survived its repair: %v", p4.Properties["pendingParent"])
	}
}

// parentRef reads a record's `parent` reference as its record path, "" when
// unset.
func parentRef(e *substrate.Record) string {
	return storedReferencePath(e.Properties["parent"])
}

// notionPageRef is the stored path a page mirror's `parent` carries when it
// names another page mirror. The parent is unpinned, so the value is a full
// path and a test comparing bare ids would be comparing the wrong thing.
func notionPageRef(id string) string { return vocabulary.RecordPath(notionPageType, id) }

// mustGetInternal is mustPutInternal's read twin, local to this file.
func mustGetInternal(t *testing.T, ds *dataset, typ, id string) *substrate.Record {
	t.Helper()
	e, err := ds.Get(context.Background(), typ, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return e
}
