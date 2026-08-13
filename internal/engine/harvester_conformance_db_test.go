package engine

// The build's conformance gate (substrate-primitives §8): the URL-harvester
// bundle, installed as a REAL closure from the shipped example manifests
// (../../../kinds/web.bundles.substrate.reamde.dev), driven end to end through the one
// delivery mechanism, and held to the two properties the throwaway prototype
// proved — put-if-absent re-mints nothing, replay-from-zero is quiet in the
// data — at a causal depth well under the cap. Nothing here composes from a
// bespoke workflow/connector/link/reflection primitive: the chain is only
// bundle + kind + trait + function + trigger + agent + llm.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/runner/substratefn"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	webAuthority  = "web.bundles.substrate.reamde.dev"
	webBundleRow  = webAuthority + "/web"
	webConfigType = webAuthority + "/config"
	webPageType   = webAuthority + "/page"
	convMsgType   = "messaging.substrate.reamde.dev/conversationmessage"

	// The models the shipped agents name on the seeded `default` provider:
	// distinct, so one fake server drives the whole chain by model.
	modelStrong = "anthropic/claude-opus-5"    // pageclassifier
	modelMid    = "anthropic/claude-sonnet-5"  // readinglistagent
	modelCheap  = "anthropic/claude-haiku-4-5" // weeklyrollup

	exampleDir = "../../kinds/web.bundles.substrate.reamde.dev"

	blogURL    = "https://blog.example.com/how-substrates-compose"
	trackerURL = "https://tracker.example/evil"
)

// webPageID mirrors the findurls body's id EXACTLY: the id is host.ids.url(u),
// a hash of the URL (no truncated-slug collision). Computing it through the Go
// SDK's substratefn.URLID also proves the two runtimes agree byte-for-byte — the
// Python body minted these ids, this Go mirror recomputes them.
func webPageID(u string) string {
	return substratefn.URLID(u)
}

// loadYAMLDocs decodes a `---`-separated manifest file into raw envelope maps.
func loadYAMLDocs(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var out []map[string]any
	for {
		var m map[string]any
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if len(m) == 0 {
			continue
		}
		out = append(out, m)
	}
	return out
}

// putDataDoc applies one data-record manifest (a trigger) the way `substratectl
// apply` routes a non-schema document — through the ordinary write path.
func putDataDoc(t *testing.T, ds *dataset, m map[string]any) *substrate.Record {
	t.Helper()
	typ, _ := m["kind"].(string)
	meta, _ := m["metadata"].(map[string]any)
	id, _ := meta["id"].(string)
	data, _ := m["data"].(map[string]any)
	props, _ := data["properties"].(map[string]any)
	e, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: typ, ID: id, Properties: props,
	})
	if err != nil {
		t.Fatalf("put data doc %s/%s: %v", typ, id, err)
	}
	return e
}

// drainTriggers runs the one delivery mechanism to quiescence.
func drainTriggers(t *testing.T, ds *dataset) {
	t.Helper()
	ctx := context.Background()
	for range 40 {
		n, err := ds.ProcessTriggers(ctx)
		if err != nil {
			t.Fatalf("process triggers: %v", err)
		}
		if n == 0 {
			return
		}
	}
	t.Fatal("the trigger chain did not drain")
}

func countLivePages(t *testing.T, ds *dataset) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL`,
		webPageType).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// maxDataSeq is the newest changelog seq EXCLUDING the run/thread/message
// ledger — "the data" is everything but the record of the deliveries.
func maxDataSeq(t *testing.T, ds *dataset) int64 {
	t.Helper()
	changes, err := ds.Changes(context.Background(), 0, substrate.ChangeFilter{
		ExcludeKinds: []string{typeRun, typeThread, typeMessage},
	}, 1_000_000)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(changes) == 0 {
		return 0
	}
	return changes[len(changes)-1].Seq
}

// changelogHead is the newest changelog seq of ALL kinds — the boundary a
// "nothing wrote after this" assertion measures against.
func changelogHead(t *testing.T, ds *dataset) int64 {
	t.Helper()
	var head int64
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT COALESCE(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
		t.Fatal(err)
	}
	return head
}

// parkedFailures counts the trigger_failures rows: a parked delivery leaves
// one, so a delta of zero across an operation means nothing parked.
func parkedFailures(t *testing.T, ds *dataset) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM trigger_failures`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// feedMessage stands up the message's required context (an account, a
// conversation, an author) and creates one conversationmessage carrying text.
func feedMessage(t *testing.T, ds *dataset, id, personID, convID, text string) *substrate.Record {
	t.Helper()
	return mustPutInternal(t, ds, substrate.PutInput{
		Kind: convMsgType, ID: id,
		Properties: map[string]any{"text": text, "at": "2026-08-08T10:00:00Z"},
		Edges: []substrate.EdgeInput{
			// Single-target edges: the declaration supplies the type, so a bare
			// id resolves.
			{Rel: "conversation", To: substrate.EdgeRef{ID: convID}},
			{Rel: "author", To: substrate.EdgeRef{ID: personID}},
		},
	})
}

func mustPutInternal(t *testing.T, ds *dataset, in substrate.PutInput) *substrate.Record {
	t.Helper()
	e, err := ds.Put(context.Background(), substrate.ActorAPI, in)
	if err != nil {
		t.Fatalf("put %s/%s: %v", in.Kind, in.ID, err)
	}
	return e
}

func TestURLHarvesterBundleConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	if err := enginetest.InstallAccountType(ctx, ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	// Point the HOST gateway at the fake server: the shipped agents name the
	// seeded `default` provider, which carries no baseURL of its own — no
	// custom llmprovider row is installed.
	fake := newFakeLLM(t)
	ds.svc.llmBaseURL = fake.srv.URL
	ds.svc.llmAPIKey = "host-gateway-key"

	// --- install the bundle atomically, from the shipped manifests ---------
	vocabularyDocs := loadYAMLDocs(t, exampleDir+"/bundle.yaml")
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, vocabularyDocs); err != nil {
		t.Fatalf("install the harvester bundle: %v", err)
	}
	row, err := ds.Get(ctx, "core.substrate.reamde.dev/bundle", webBundleRow)
	if err != nil || row.Kind != "core.substrate.reamde.dev/bundle" {
		t.Fatalf("bundle row: %+v %v", row, err)
	}
	// The wiring: the four trigger data records, applied as ordinary puts.
	for _, m := range loadYAMLDocs(t, exampleDir+"/triggers.yaml") {
		putDataDoc(t, ds, m)
	}

	// --- the bundle's ONE config record (denyDomains + a secret key) --------
	cfg := mustPutInternal(t, ds, substrate.PutInput{
		Kind: webConfigType,
		Properties: map[string]any{
			"denyDomains":  []any{"tracker.example"},
			"firecrawlKey": "fc-secret-placeholder",
		},
	})

	// The two agents that fire during the reactive chain. weeklyrollup
	// is scripted later, just before the schedule tick.
	blogPage := webPageID(blogURL)
	fake.script(modelStrong, // pageclassifier
		fakeTurn{calls: []fakeCall{{"setclass", fmt.Sprintf(`{"id":%q,"class":"article"}`, blogPage)}}},
		fakeTurn{calls: []fakeCall{{"readinglistagent", `{"input":"please save this page"}`}}},
		fakeTurn{content: "classified as article and routed to the reading list."},
	)
	fake.script(modelMid, // readinglistagent
		// First it reaches for stampconfig — a write of config, which the
		// child's OWN emit allows but the classifier's ceiling denies. The tool
		// must refuse and nothing may land. THEN it makes the proposal, which
		// survives because recordpatchrequest is in both.
		fakeTurn{calls: []fakeCall{{"stampconfig", fmt.Sprintf(`{"id":%q}`, cfg.ID)}}},
		fakeTurn{calls: []fakeCall{{"propose", fmt.Sprintf(`{"kind":"web.bundles.substrate.reamde.dev/page","target":%q,"diff":{"properties":{"saved":true}},"rationale":"worth reading"}`, blogPage)}}},
		fakeTurn{content: "proposed the reading-list add."},
	)

	// --- feed one message carrying two URLs, one deny-listed ---------------
	person := mustPutInternal(t, ds, substrate.PutInput{Kind: "people.substrate.reamde.dev/person", ID: "p-alice", Properties: map[string]any{"name": "Alice"}})
	acct := mustPutInternal(t, ds, substrate.PutInput{Kind: enginetest.AccountType, ID: "acct-test", Properties: map[string]any{"provider": "test", "label": "test inbox"}})
	conv := mustPutInternal(t, ds, substrate.PutInput{
		Kind: "messaging.substrate.reamde.dev/conversation", ID: "conv-1",
		Properties: map[string]any{"kind": "direct", "name": "reading chat"},
		Edges:      []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: acct.ID}}},
	})
	feedMessage(t, ds, "msg-1", person.ID, conv.ID,
		fmt.Sprintf("read %s and %s later", blogURL, trackerURL))

	drainTriggers(t, ds)

	// --- assert the chain --------------------------------------------------

	// Pages minted: the allowed one exists; the deny-listed one never did.
	if got := countLivePages(t, ds); got != 1 {
		t.Fatalf("live pages: %d, want 1 (deny-listed url filtered)", got)
	}
	if _, err := ds.Get(ctx, webPageType, webPageID(trackerURL)); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the deny-listed page was minted: %v", err)
	}

	// --- deny-list: never minted, not merely absent ------------------------
	// A message carrying ONLY the deny-listed URL. Its findurls delivery must
	// SUCCEED (the deny is a filter, not a failure), yet the tracker page must
	// have ZERO changelog rows across all of history — proving it was never
	// minted, as opposed to minted-then-removed (which final absence permits).
	denyMsg := feedMessage(t, ds, "msg-deny", person.ID, conv.ID,
		fmt.Sprintf("nothing but junk here: %s", trackerURL))
	drainTriggers(t, ds)
	var denyStatus string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT props->>'status' FROM records
		WHERE kind = $1 AND deleted_at IS NULL
		  AND props->>'trigger' = $2 AND props->>'record' = $3
		ORDER BY created_at DESC, id DESC LIMIT 1`,
		typeRun, "web-findurls-on-message", denyMsg.ID).Scan(&denyStatus); err != nil {
		t.Fatalf("the deny-only findurls run: %v", err)
	}
	if denyStatus != runStatusOK {
		t.Fatalf("deny-only findurls delivery status = %q, want ok (the deny is a filter, not a failure)", denyStatus)
	}
	var trackerRows int
	if err := ds.db.QueryRowContext(ctx,
		`SELECT count(*) FROM changelog WHERE record_id = $1`, webPageID(trackerURL)).Scan(&trackerRows); err != nil {
		t.Fatal(err)
	}
	if trackerRows != 0 {
		t.Fatalf("the deny-listed page has %d changelog rows — it was minted at some point, not never", trackerRows)
	}
	page, err := ds.Get(ctx, webPageType, blogPage)
	if err != nil {
		t.Fatalf("the harvested page: %v", err)
	}
	// Fetched, then classified.
	if page.Properties["fetch"] != "fetched" {
		t.Fatalf("page fetch state: %v", page.Properties["fetch"])
	}
	if page.Properties["class"] != "article" {
		t.Fatalf("page class: %v", page.Properties["class"])
	}
	if page.Properties["title"] == nil || page.Properties["content"] == nil {
		t.Fatalf("fetchpage wrote no title/content: %+v", page.Properties)
	}

	// The class patch — the page's newest write before approval — landed under
	// the CLASSIFIER agent's actor (bundle tier), proving the tool wrote it.
	var classActor string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT actor FROM changelog WHERE record_id = $1 ORDER BY seq DESC LIMIT 1`,
		blogPage).Scan(&classActor); err != nil {
		t.Fatalf("the class-patch changelog row: %v", err)
	}
	if classActor != "function:pageclassifier" {
		t.Fatalf("class patched by %q, not the classifier agent", classActor)
	}

	// The sub-agent's proposal landed as a recordpatchrequest targeting the
	// page, authored by the reading-list agent.
	var reqID, reqActor string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT e.id, c.actor FROM records e
		JOIN changelog c ON c.record_id = e.id
		WHERE e.kind = $1 AND e.deleted_at IS NULL AND e.props->>'rationale' = 'worth reading'
		ORDER BY c.seq LIMIT 1`, vocabulary.KindRecordPatchRequest).Scan(&reqID, &reqActor); err != nil {
		t.Fatalf("the reading-list proposal: %v", err)
	}
	if reqActor != "function:readinglistagent" {
		t.Fatalf("proposal authored by %q, not the reading-list agent", reqActor)
	}
	var target string
	if err := ds.db.QueryRowContext(ctx, `SELECT dst FROM edges WHERE rel = 'target' AND src = $1`, reqID).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if target != blogPage {
		t.Fatalf("proposal targets %q, not the page", target)
	}

	// Threads: one classifier (trigger) root, one reading-list child with the
	// parent edge — the sub-agent hop.
	if n := threadCountOf(t, ds, webAuthority+"/pageclassifier"); n != 1 {
		t.Fatalf("classifier threads: %d", n)
	}
	if n := threadCountOf(t, ds, webAuthority+"/readinglistagent"); n != 1 {
		t.Fatalf("reading-list threads: %d", n)
	}

	// --- EMIT CEILING: a child-allowed effect the parent denies is refused --
	// Before proposing, the reading-list agent tried stampconfig — a config
	// write its OWN emit allows. As the classifier's sub-agent its effective
	// emit is its own ∩ the classifier's (page + recordpatchrequest, no
	// config), so the tool must have refused. The refusal is a tool result
	// the model saw, and NOTHING may have landed on the config.
	var childThread string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL AND props->>'agent' = $2
		ORDER BY created_at DESC, id DESC LIMIT 1`,
		typeThread, webAuthority+"/readinglistagent").Scan(&childThread); err != nil {
		t.Fatalf("the reading-list child thread: %v", err)
	}
	var refused bool
	for _, m := range threadMessages(t, ds, childThread) {
		if m["role"] == "tool" && strings.Contains(fmt.Sprint(m["content"]), "emit allowlist") {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("the child's stampconfig call was not refused by the emit ceiling")
	}
	// No changelog row wrote the config under the reading-list agent's actor:
	// the ceiling refusal is real, not merely reported.
	var childConfigWrites int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM changelog WHERE record_id = $1 AND actor = $2`,
		cfg.ID, "function:readinglistagent").Scan(&childConfigWrites); err != nil {
		t.Fatal(err)
	}
	if childConfigWrites != 0 {
		t.Fatalf("the ceilinged stampconfig landed %d config writes under the child actor", childConfigWrites)
	}

	// The run ledger: an ok run for each record trigger's delivery.
	for _, trID := range []string{"web-findurls-on-message", "web-fetch-on-page", "web-classify-on-page"} {
		var oks int
		if err := ds.db.QueryRowContext(ctx, `
			SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL
			  AND props->>'trigger' = $2 AND props->>'status' = 'ok'`, typeRun, trID).Scan(&oks); err != nil {
			t.Fatal(err)
		}
		if oks < 1 {
			t.Fatalf("trigger %s has no ok run", trID)
		}
	}

	// --- owner approves the proposal → the durable note (the page saved) ----
	// The as-built approval mechanism is applyDiff patching the target, so the
	// prototype's freely-minted note becomes a durable mark on the page.
	reqEnt, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, reqID)
	if err != nil {
		t.Fatalf("read the proposal: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, reqID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &reqEnt.Version,
	}); err != nil {
		t.Fatalf("approve the proposal: %v", err)
	}
	saved, err := ds.Get(ctx, webPageType, blogPage)
	if err != nil {
		t.Fatalf("get saved page: %v", err)
	}
	if saved.Properties["saved"] != true {
		t.Fatalf("approval did not mark the page saved: %+v", saved.Properties)
	}

	// --- causal depth is EXACTLY the chain's, well under the cap -----------
	// The proposal request is the far end of the as-built reactive chain, and
	// its depth is a precise fact, not a range: the triggering message is a
	// direct owner write (depth 0), findurls mints the page (1), fetchpage
	// marks it fetched (2), and the classifier — firing on that fetched
	// update — routes to the reading-list agent whose propose lands at depth 3.
	// Owner approval is a SEPARATE direct owner write, off this causal path, so
	// it never extends the depth. Accepting the 0..8 range would let a chain
	// that lost every causal link (depth 0) pass; the exact value is the point.
	var reqSeq int64
	if err := ds.db.QueryRowContext(ctx, `SELECT min(seq) FROM changelog WHERE record_id = $1`, reqID).Scan(&reqSeq); err != nil {
		t.Fatal(err)
	}
	depth, err := ds.causalDepth(ctx, reqSeq)
	if err != nil {
		t.Fatal(err)
	}
	if depth != 3 {
		t.Fatalf("causal depth %d, want exactly 3 (msg → mint → fetch → propose); cap is 16", depth)
	}
	t.Logf("chain causal depth: %d (cap 16)", depth)

	// --- CONFORMANCE PROPERTY 1: re-feeding re-mints nothing (put-if-absent)
	pagesBefore := countLivePages(t, ds)
	// Re-put the identical message: a no-op that never even redelivers.
	feedMessage(t, ds, "msg-1", person.ID, conv.ID,
		fmt.Sprintf("read %s and %s later", blogURL, trackerURL))
	// A DISTINCT message carrying the SAME url: findurls runs and its
	// put-if-absent must be a no-op because the page already exists.
	feedMessage(t, ds, "msg-2", person.ID, conv.ID,
		fmt.Sprintf("still worth reading: %s", blogURL))
	// The changelog head after the messages are in but BEFORE findurls re-runs:
	// a correct put-if-absent writes NOTHING to the page past this point. A
	// broken one that upserts would reset `fetch` to pending and drop the
	// classifier's class and the accepted `saved` — and leave a page changelog
	// row behind, which the live-count check alone would miss.
	headBeforeReput := changelogHead(t, ds)
	drainTriggers(t, ds)
	if got := countLivePages(t, ds); got != pagesBefore {
		t.Fatalf("re-feeding minted a page: %d → %d", pagesBefore, got)
	}
	var pageWritesAfter int
	if err := ds.db.QueryRowContext(ctx,
		`SELECT count(*) FROM changelog WHERE seq > $1 AND kind = $2`,
		headBeforeReput, webPageType).Scan(&pageWritesAfter); err != nil {
		t.Fatal(err)
	}
	if pageWritesAfter != 0 {
		t.Fatalf("put-if-absent upserted the page: %d new page changelog rows after re-feed", pageWritesAfter)
	}
	again, err := ds.Get(ctx, webPageType, blogPage)
	if err != nil {
		t.Fatalf("get re-fed page: %v", err)
	}
	if again.Properties["fetch"] != "fetched" || again.Properties["class"] != "article" ||
		again.Properties["saved"] != true || again.Properties["title"] == nil || again.Properties["content"] == nil {
		t.Fatalf("put-if-absent upsert-and-reset the page's later-stage state: %+v", again.Properties)
	}

	// --- CONFORMANCE PROPERTY 2: replay-from-zero is quiet in the data ------
	// Quiet in the data AND quiet in delivery: replaying every record trigger
	// from seq 0 must write only ledger rows, park NOTHING, and settle every
	// re-delivery in a terminal SUCCESS status (ok, or skipped where the guard
	// now reads false against current state) — never parked.
	dataSeqBefore := maxDataSeq(t, ds)
	parkedBefore := parkedFailures(t, ds)
	runsHeadBefore := changelogHead(t, ds)
	replayed := []string{"web-findurls-on-message", "web-fetch-on-page", "web-classify-on-page"}
	for _, trID := range replayed {
		if err := ds.ReplayTrigger(ctx, trID, 0); err != nil {
			t.Fatalf("replay %s: %v", trID, err)
		}
	}
	drainTriggers(t, ds)
	after, err := ds.Changes(ctx, dataSeqBefore, substrate.ChangeFilter{}, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range after {
		switch c.Kind {
		case typeRun, typeThread, typeMessage:
			// ledger rows are the record of the re-deliveries, not data.
		default:
			t.Fatalf("replay-from-zero disturbed the data at seq %d: %s (%s)", c.Seq, c.Kind, c.Op)
		}
	}
	if now := parkedFailures(t, ds); now != parkedBefore {
		t.Fatalf("replay-from-zero parked %d new deliveries (quiet replay must not park)", now-parkedBefore)
	}
	// Every replayed trigger produced a FRESH settled run and none parked. A
	// replay bug that retried and parked, writing only ledger rows, would slip
	// past the data check above but not this.
	for _, trID := range replayed {
		var settled, parked int
		if err := ds.db.QueryRowContext(ctx, `
			SELECT
				count(*) FILTER (WHERE e.props->>'status' IN ('ok','skipped')),
				count(*) FILTER (WHERE e.props->>'status' = 'parked')
			FROM records e JOIN changelog c ON c.record_id = e.id AND c.op = 'put'
			WHERE e.kind = $1 AND e.deleted_at IS NULL AND e.props->>'trigger' = $2 AND c.seq > $3`,
			typeRun, trID, runsHeadBefore).Scan(&settled, &parked); err != nil {
			t.Fatal(err)
		}
		if parked != 0 {
			t.Fatalf("replay of %s parked %d deliveries", trID, parked)
		}
		if settled < 1 {
			t.Fatalf("replay of %s produced no fresh settled (SUCCEEDED) run", trID)
		}
	}

	// --- the schedule tick produces a rollup proposal ----------------------
	fake.script(modelCheap, // weeklyrollup
		fakeTurn{calls: []fakeCall{{"propose", fmt.Sprintf(`{"kind":"web.bundles.substrate.reamde.dev/config","target":%q,"diff":{"properties":{"lastDigest":"1 page harvested this week"}},"rationale":"weekly digest"}`, cfg.ID)}}},
		fakeTurn{content: "digest proposed."},
	)
	// Make the weekly occurrence overdue, then tick.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE trigger_schedule SET fired_at = fired_at - interval '8 days'
		WHERE trigger_id = $1`, "web-rollup-weekly"); err != nil {
		t.Fatalf("rewind schedule: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("schedule tick: %v", err)
	}
	var digestReq, digestTarget string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL
		  AND props->>'rationale' = 'weekly digest'`, vocabulary.KindRecordPatchRequest).Scan(&digestReq); err != nil {
		t.Fatalf("the weekly digest proposal: %v", err)
	}
	if err := ds.db.QueryRowContext(ctx, `SELECT dst FROM edges WHERE rel = 'target' AND src = $1`, digestReq).Scan(&digestTarget); err != nil {
		t.Fatal(err)
	}
	if digestTarget != cfg.ID {
		t.Fatalf("digest proposal targets %q, not the config", digestTarget)
	}
	var rollupOK int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL
		  AND props->>'trigger' = $2 AND props->>'status' = 'ok'`, typeRun, "web-rollup-weekly").Scan(&rollupOK); err != nil {
		t.Fatal(err)
	}
	if rollupOK < 1 {
		t.Fatalf("the schedule trigger has no ok run")
	}
}
