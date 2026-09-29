package engine

// A paged drain's cursor in the delivery ledger (decision 0141): a middle
// page names the cursor by hash and the bytes stay in paged_cursors, a park
// carries them whole, and a replay that has neither starts the chain over.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// bigCursorBody pages four times with a cursor of about 200 KB that grows by
// about 20 KB a page, the shape of a provider's pending lists, and finishes
// on page 4.
const bigCursorBody = `
def main(input, host):
    cur = input.get("resume") or {"page": 0}
    page = cur["page"]
    effects = [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                "id": "p-%d" % page, "properties": {"name": "x"}}]
    if page < 4:
        pending = ["item-%06d" % i for i in range(15000 + page * 1500)]
        return {"effects": effects, "more": {"cursor": {"page": page + 1, "pending": pending}}}
    return {"effects": effects}
`

// pageEffect is one `page` effect of a delivery entry as the table stores it,
// with the size of the whole entry it rides and whether that entry parks.
type pageEffect struct {
	seq    int64
	size   int
	parked bool
	page   map[string]any
}

// pageEffectsOf reads every page effect the ledger holds for one trigger, in
// seq order.
func pageEffectsOf(t *testing.T, ds *dataset, triggerID string) []pageEffect {
	t.Helper()
	rows, err := ds.db.QueryContext(context.Background(), `
		SELECT seq, payload::text FROM changelog WHERE op = $1 AND record_id = $2 ORDER BY seq`,
		string(substrate.OpDelivery), triggerID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []pageEffect
	for rows.Next() {
		var seq int64
		var text string
		if err := rows.Scan(&seq, &text); err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Fold []map[string]any `json:"fold"`
		}
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			t.Fatal(err)
		}
		parked := false
		for _, op := range payload.Fold {
			parked = parked || op["kind"] == string(foldPark)
		}
		for _, op := range payload.Fold {
			if op["kind"] != string(foldPage) {
				continue
			}
			page, _ := op["page"].(map[string]any)
			out = append(out, pageEffect{seq: seq, size: len(text), parked: parked, page: page})
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// segmentPayloadSizes reads the byte length of every entry's payload in the
// repository's segment files, by seq.
func segmentPayloadSizes(t *testing.T, ds *dataset) map[int64]int {
	t.Helper()
	log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(ds.dir))
	if err != nil {
		t.Fatal(err)
	}
	cur := log.Cursor(0)
	defer func() { _ = cur.Close() }()
	sizes := map[int64]int{}
	for {
		batch, err := cur.Next(500)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch) == 0 {
			return sizes
		}
		for _, e := range batch {
			sizes[e.Seq] = len(e.Payload)
		}
	}
}

// A middle page's delivery entry names its cursor by SHA-256 and byte size
// and stays under 2 KB while the cursor grows past 200 KB, in the table and in
// the segment file both. The bytes live in paged_cursors, and the park that
// stops the drain carries them whole, once, so a restore can resume it. Not
// parallel: the page cap is package-level.
func TestAPageEntryStaysSmallAsItsCursorGrows(t *testing.T) {
	ds, triggerID := openPagedDataset(t, "pagedsize.test.dev/pagedsize", bigCursorBody)
	ctx := context.Background()
	restore := withMaxPages(3)
	defer restore()

	w, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: "pagedsize.test.dev/pagedsize/widget", Properties: map[string]any{"name": "big"}})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	wch, err := ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	chain := chainKey(ds, triggerID, wch.Seq)

	// The table holds the cursor whole.
	var stored string
	if err := ds.db.QueryRowContext(ctx, `SELECT cursor::text FROM paged_cursors WHERE chain = $1`, chain).Scan(&stored); err != nil {
		t.Fatalf("read the resume row: %v", err)
	}
	if len(stored) < 200_000 {
		t.Fatalf("the resume row holds a %d-byte cursor, want the whole one past 200 KB", len(stored))
	}

	var pages, parks []pageEffect
	for _, e := range pageEffectsOf(t, ds, triggerID) {
		if e.parked {
			parks = append(parks, e)
		} else {
			pages = append(pages, e)
		}
	}
	if len(pages) != 3 || len(parks) != 1 {
		t.Fatalf("%d page effects and %d parks carrying one; want the cap's 3 pages and 1 park", len(pages), len(parks))
	}
	files := segmentPayloadSizes(t, ds)
	for i, e := range pages {
		if e.size >= 2048 {
			t.Fatalf("page %d rides a %d-byte entry, want under 2 KB", i+1, e.size)
		}
		if n, ok := files[e.seq]; !ok || n >= 2048 {
			t.Fatalf("page %d's segment line carries %d payload bytes (found %v), want under 2 KB", i+1, n, ok)
		}
		if _, carried := e.page["cursor"]; carried {
			t.Fatalf("page %d carries its cursor", i+1)
		}
		if n, _ := e.page["cursorBytes"].(float64); n < 200_000 {
			t.Fatalf("page %d names a %v-byte cursor, want the 200 KB it drained", i+1, e.page["cursorBytes"])
		}
	}
	// The hash names the cursor the table holds, as Postgres prints it.
	sum := sha256.Sum256([]byte(stored))
	last := pages[len(pages)-1].page
	if last["cursorSha256"] != hex.EncodeToString(sum[:]) || last["cursorBytes"] != float64(len(stored)) {
		t.Fatalf("the last page names %v (%v bytes), want the stored cursor's %x (%d bytes)",
			last["cursorSha256"], last["cursorBytes"], sum, len(stored))
	}
	// The park is the one entry that carries the cursor, whole.
	if raw, _ := json.Marshal(parks[0].page["cursor"]); len(raw) < 200_000 {
		t.Fatalf("the park carries a %d-byte cursor, want the whole one", len(raw))
	}

	// The drain still resumes from the table: the retry picks up at page 3.
	restore()
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 1 {
		t.Fatalf("failures: %v (%d)", err, len(failures))
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, "samples.substrate.reamde.dev/tasks/task", "p-0", substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.RetryTriggerFailure(ctx, triggerID, failures[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !liveExists(t, ds, "p-4") || liveExists(t, ds, "p-0") {
		t.Fatal("the retry did not resume the drain from its stored cursor")
	}
}

// pagedRow is a resume row as the table holds it.
type pagedRow struct {
	cursor  string
	version int64
	pages   int64
}

// pagedRowOf reads one chain's resume row.
func pagedRowOf(t *testing.T, ds *dataset, chain string) (pagedRow, bool) {
	t.Helper()
	var r pagedRow
	err := ds.db.QueryRowContext(context.Background(),
		`SELECT cursor::text, version, pages FROM paged_cursors WHERE chain = $1`, chain).Scan(&r.cursor, &r.version, &r.pages)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return r, true
}

// keyedPagedBody drains five pages keyed by the delivered record, so two
// chains of one trigger write different tasks.
const keyedPagedBody = `
def main(input, host):
    page = input.get("resume") or 0
    rid = input["envelope"]["change"]["id"]
    effects = [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                "id": "%s-p-%d" % (rid, page), "properties": {"name": "x"}}]
    if page < 4:
        return {"effects": effects, "more": {"cursor": page + 1}}
    return {"effects": effects}
`

// A replay of the three shapes a paged chain leaves in the ledger. Chain A
// PARKED, so the park carried its cursor whole; chain B stopped between pages
// without parking, so its last page named the cursor by hash; chain C is what
// an earlier binary wrote for every middle page, the cursor whole on the
// effect. A rebuild over the same database reproduces all three rows exactly,
// B's from the cursor the table held. An import into an empty database brings
// A and C back and B with a null cursor, and B's redelivery starts the body
// over from page 0 under a fresh budget, though its row says two pages under
// a cap of two and a start an hour ago. A's retry resumes from page 2. Not
// parallel: the page cap is package-level.
func TestAReplayResumesAParkedDrainAndAnImportStartsAnInterruptedOneOver(t *testing.T) {
	ds, triggerID := openPagedDataset(t, "pagedrebuild.test.dev/pagedrebuild", keyedPagedBody)
	ctx := context.Background()
	restore := withMaxPages(2)
	defer restore()
	const widget = "pagedrebuild.test.dev/pagedrebuild/widget"
	const task = "samples.substrate.reamde.dev/tasks/task"

	// Chain A parks at the cap with cursor 2.
	a, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: widget, ID: "a", Properties: map[string]any{"name": "a"}})
	if err != nil {
		t.Fatal(err)
	}
	ach, err := ds.latestChangeOf(ctx, a.Kind, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	chainA := chainKey(ds, triggerID, ach.Seq)
	liveA, ok := pagedRowOf(t, ds, chainA)
	if !ok || liveA.cursor != "2" {
		t.Fatalf("chain a: %+v (%v), want parked at cursor 2", liveA, ok)
	}
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 1 {
		t.Fatalf("failures: %v (%d)", err, len(failures))
	}

	// Chain B is pending and was interrupted after its second page: the pages
	// committed, their entries named the cursor by hash, and no park
	// followed. Its tasks are absent, so a resume from cursor 3 would leave
	// b-p-0 missing. Its cursor holds numbers jsonb respells (a negative zero,
	// a trailing zero), which the rebuild must still match to their digest.
	b, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: widget, ID: "b", Properties: map[string]any{"name": "b"}})
	if err != nil {
		t.Fatal(err)
	}
	bch, err := ds.latestChangeOf(ctx, b.Kind, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	chainB := chainKey(ds, triggerID, bch.Seq)
	owner := pagedOwner{triggerID: triggerID, kind: pagedKindRecord, identity: fmt.Sprintf("%d", bch.Seq)}
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		cursor := json.RawMessage(`{"page": 3, "offset": -0.0, "ratio": 1.50, "token": "<b>"}`)
		if err := tx.claimPagedCursor(chainB, owner, cursor, 2, 2, 2, nowUTC().Add(-time.Hour)); err != nil {
			return err
		}
		return tx.settleDelivery(triggerID)
	}); err != nil {
		t.Fatalf("interrupt chain b: %v", err)
	}
	liveB, ok := pagedRowOf(t, ds, chainB)
	if !ok || liveB.cursor == "null" {
		t.Fatalf("chain b: %+v (%v), want its cursor", liveB, ok)
	}

	// Chain C, in the shape an earlier binary wrote.
	chainC := chainKey(ds, triggerID, 999999)
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		if _, err := tx.fold(foldOp{Kind: foldPage, Ref: typeTrigger, ID: triggerID, Page: &foldPageRow{
			Chain: chainC, Cursor: json.RawMessage(`{"token": "c-7"}`), Version: 4, Pages: 4, Effects: 4, Bytes: 40,
			StartedAt: nowUTC(), Kind: pagedKindRecord, Identity: "999999",
		}}); err != nil {
			return err
		}
		return tx.settleDelivery(triggerID)
	}); err != nil {
		t.Fatalf("write chain c: %v", err)
	}
	wantC := pagedRow{cursor: `{"token": "c-7"}`, version: 4, pages: 4}

	// A rebuild over the same database reproduces the fold, resume rows and
	// all.
	before, err := ds.FoldSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.svc.RebuildRepository(ctx, ds.Repository().ID); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	after, err := ds.FoldSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("the rebuilt fold is not the live one\n%s", firstDifferenceOf(before, after))
	}
	for chain, want := range map[string]pagedRow{chainA: liveA, chainB: liveB, chainC: wantC} {
		if got, ok := pagedRowOf(t, ds, chain); !ok || got != want {
			t.Fatalf("%s after the rebuild: %+v (%v), want %+v", chain, got, ok, want)
		}
	}

	// Import the directory into an empty database.
	d2 := importIntoEmptyDatabase(t, ds)
	if got, ok := pagedRowOf(t, d2, chainA); !ok || got != liveA {
		t.Fatalf("chain a after the import: %+v (%v), want %+v", got, ok, liveA)
	}
	if got, ok := pagedRowOf(t, d2, chainB); !ok || got != (pagedRow{cursor: "null", version: 1, pages: 2}) {
		t.Fatalf("chain b after the import: %+v (%v), want a null cursor at version 1 with 2 pages", got, ok)
	}
	if got, ok := pagedRowOf(t, d2, chainC); !ok || got != wantC {
		t.Fatalf("chain c after the import: %+v (%v), want %+v", got, ok, wantC)
	}

	// B's redelivery starts over. The cap stays at two: only a fresh budget
	// lets the chain commit a page at all.
	if _, err := d2.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process after the import: %v", err)
	}
	for page := range 2 {
		if id := fmt.Sprintf("b-p-%d", page); !liveExists(t, d2, id) {
			t.Fatalf("%s is missing: the interrupted chain did not start over", id)
		}
	}
	restore()
	parked, err := d2.TriggerFailures(ctx, triggerID)
	if err != nil {
		t.Fatal(err)
	}
	var bFailure int64
	for _, f := range parked {
		if f.Seq == bch.Seq {
			bFailure = f.ID
		}
	}
	if len(parked) != 2 || bFailure == 0 {
		t.Fatalf("parked failures %+v, want chain a's and chain b's cap parks", parked)
	}
	if _, err := d2.RetryTriggerFailure(ctx, triggerID, bFailure); err != nil {
		t.Fatalf("retry chain b: %v", err)
	}
	for page := range 5 {
		if id := fmt.Sprintf("b-p-%d", page); !liveExists(t, d2, id) {
			t.Fatalf("%s is missing after chain b's retry", id)
		}
	}
	if _, ok := pagedRowOf(t, d2, chainB); ok {
		t.Fatal("chain b's resume row outlived its drain")
	}

	// A's retry resumes from page 2: pages 0 and 1 are erased first.
	for _, id := range []string{"a-p-0", "a-p-1"} {
		if _, err := d2.Delete(ctx, substrate.ActorAPI, task, id, substrate.DeleteInput{}); err != nil {
			t.Fatalf("delete %s: %v", id, err)
		}
	}
	if _, err := d2.RetryTriggerFailure(ctx, triggerID, failures[0].ID); err != nil {
		t.Fatalf("retry chain a: %v", err)
	}
	for _, id := range []string{"a-p-2", "a-p-3", "a-p-4"} {
		if !liveExists(t, d2, id) {
			t.Fatalf("resumed page %s missing", id)
		}
	}
	for _, id := range []string{"a-p-0", "a-p-1"} {
		if liveExists(t, d2, id) {
			t.Fatalf("%s was re-run: the parked chain restarted instead of resuming", id)
		}
	}
}

// importIntoEmptyDatabase closes ds's service, copies its repository
// directory under a fresh data root and opens it over a fresh database: the
// boot import folds the directory into tables that hold nothing.
func importIntoEmptyDatabase(t *testing.T, ds *dataset) *dataset {
	t.Helper()
	ctx := context.Background()
	id := ds.Repository().ID
	src := ds.dir
	if err := ds.svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	root := t.TempDir()
	dst, err := changelogfile.RepoDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	svc, err := OpenForTest(t, ctx, MigratedDSN(t), WithDataRoot(root), WithCredentialKey(TestCredentialKey),
		WithKindsDir(SeedKindsDir))
	if err != nil {
		t.Fatalf("import the directory: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	imported, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the imported repository: %v", err)
	}
	return imported.(*dataset)
}
