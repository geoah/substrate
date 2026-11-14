package engine

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A PAGED DRAIN'S SAVED CONTINUATION IS THE ENGINE'S. A body that pages
// through the repository's records keeps the list's cursor in it, as the
// Gmail sweep and the WHOOP scan do. When the substrate refuses a list
// cursor the continuation holds (a restore replaced the history, a unit or a
// conversion the list converts through changed), the drain parks at once,
// the saved position dropped so nothing sends it again, and a retry runs it
// from its first page (functions.go refusedContinuation). A restore keeps
// every continuation (decisions 0064 and 0141), so an outside service's page
// token resumes after it, and a cursor a body keeps anywhere else is its
// own: the continuation stays.

const staleTask = "samples.substrate.reamde.dev/tasks/task"

// sweepSource sweeps every item in id order, one a page, the list's cursor
// its continuation.
const sweepSource = `
def main(input, host):
    page = host.records.list(ITEM, first=1, order="id", after=input.get("resume") or None)
    effects = [{"action": "put", "kind": TASK, "id": "seen-" + r["id"], "properties": {"name": r["id"]}}
               for r in page.get("records") or []]
    if page.get("cursor"):
        return {"effects": effects, "more": {"cursor": page["cursor"]}}
    return {"effects": effects}
`

// weighedSweepSource is sweepSource over the items heavier than 100 g: its
// filter converts through the unit graph, so the list's cursor carries the
// graph's revision.
const weighedSweepSource = `
def main(input, host):
    page = host.records.list(ITEM, where={"weight": {"gt": {"value": "100", "unit": "g"}}},
                             first=1, order="id", after=input.get("resume") or None)
    effects = [{"action": "put", "kind": TASK, "id": "seen-" + r["id"], "properties": {"name": r["id"]}}
               for r in page.get("records") or []]
    if page.get("cursor"):
        return {"effects": effects, "more": {"cursor": page["cursor"]}}
    return {"effects": effects}
`

// pageTokenSource pages through an outside service, as a provider's
// backfill does: its continuation is the service's own token (here a page
// number), and it marks the item of each page seen. While a bookmark record
// names a list cursor, each run also lists from it, a cursor the body keeps
// outside its continuation.
const pageTokenSource = `
def main(input, host):
    page = input.get("resume") or 0
    mark = (host.records.get(MARK, "bookmark") or {}).get("properties") or {}
    if mark.get("name"):
        host.records.list(ITEM, first=1, order="id", after=mark["name"])
    effects = [{"action": "put", "kind": TASK, "id": "seen-" + "abcd"[page], "properties": {"name": "abcd"[page]}}]
    if page < 3:
        return {"effects": effects, "more": {"cursor": page + 1}}
    return {"effects": effects}
`

// twoCursorSource is sweepSource that first lists, and lets go, a cursor a
// bookmark record names, as a body that keeps one of its own does.
const twoCursorSource = `
def main(input, host):
    mark = (host.records.get(MARK, "bookmark") or {}).get("properties") or {}
    if mark.get("name"):
        try:
            host.records.list(ITEM, first=1, order="id", after=mark["name"])
        except host.Compacted:
            pass
    page = host.records.list(ITEM, first=1, order="id", after=input.get("resume") or None)
    effects = [{"action": "put", "kind": TASK, "id": "seen-" + r["id"], "properties": {"name": r["id"]}}
               for r in page.get("records") or []]
    if page.get("cursor"):
        return {"effects": effects, "more": {"cursor": page["cursor"]}}
    return {"effects": effects}
`

// hangingSource is sweepSource that, refused, catches the refusal and then
// outlives its timeout.
const hangingSource = `
def main(input, host):
    try:
        page = host.records.list(ITEM, first=1, order="id", after=input.get("resume") or None)
    except host.Compacted:
        import time
        time.sleep(30)
        raise
    effects = [{"action": "put", "kind": TASK, "id": "seen-" + r["id"], "properties": {"name": r["id"]}}
               for r in page.get("records") or []]
    if page.get("cursor"):
        return {"effects": effects, "more": {"cursor": page["cursor"]}}
    return {"effects": effects}
`

// answersAfterTheRefusalSource is sweepSource that, refused, catches the
// refusal and answers with an effect the engine refuses.
const answersAfterTheRefusalSource = `
def main(input, host):
    try:
        page = host.records.list(ITEM, first=1, order="id", after=input.get("resume") or None)
    except host.Compacted:
        return {"effects": [{"action": "put", "kind": "nowhere.test.dev/nowhere/thing", "id": "x", "properties": {"name": "x"}}]}
    effects = [{"action": "put", "kind": TASK, "id": "seen-" + r["id"], "properties": {"name": r["id"]}}
               for r in page.get("records") or []]
    if page.get("cursor"):
        return {"effects": effects, "more": {"cursor": page["cursor"]}}
    return {"effects": effects}
`

// answersWithAWriteRefusedSource is answersAfterTheRefusalSource whose
// effect decodes, a put of the kind the sweep writes, and is refused only as
// its page commits: the kind declares no such property.
const answersWithAWriteRefusedSource = `
def main(input, host):
    try:
        page = host.records.list(ITEM, first=1, order="id", after=input.get("resume") or None)
    except host.Compacted:
        return {"effects": [{"action": "put", "kind": TASK, "id": "x", "properties": {"no_such_property": 1}}]}
    effects = [{"action": "put", "kind": TASK, "id": "seen-" + r["id"], "properties": {"name": r["id"]}}
               for r in page.get("records") or []]
    if page.get("cursor"):
        return {"effects": effects, "more": {"cursor": page["cursor"]}}
    return {"effects": effects}
`

// handsItDownSource is sweepSource that has its helper make the list, from
// the cursor it saved.
const handsItDownSource = `
def main(input, host):
    page = host.call(HELPER, {"after": input.get("resume") or None})
    effects = [{"action": "put", "kind": TASK, "id": "seen-" + r["id"], "properties": {"name": r["id"]}}
               for r in page.get("records") or []]
    if page.get("cursor"):
        return {"effects": effects, "more": {"cursor": page["cursor"]}}
    return {"effects": effects}
`

// staleSweep is a repository with four items, a paged function sweeping
// them, and a drain of it parked after its first two pages, on a database
// and a data root the test can open again, and an archive it can restore
// from.
type staleSweep struct {
	dsn, root, repo string
	archive         string
	// opts are the options of every open after the first.
	opts      []Option
	svc       *service
	ds        *dataset
	triggerID string
	chain     string
	items     []string
	itemType  string
	markType  string
}

// newStaleSweep builds the sweep with source; weighed gives each item a
// weight in pounds, over a unit graph of the repository's own, and fn's
// fields join the function's declaration.
func newStaleSweep(t *testing.T, pkg, source string, weighed bool, fn ...map[string]any) *staleSweep {
	t.Helper()
	ctx := context.Background()
	authority, _ := vocabulary.SplitPackageRef(pkg)
	widgetType := pkg + "/widget"
	s := &staleSweep{
		dsn: MigratedDSN(t), root: t.TempDir(), repo: testdb.Repository(t),
		archive:   filepath.Join(t.TempDir(), "archive"),
		triggerID: "on-sweep." + authority, items: []string{"a", "b", "c", "d"},
		itemType: pkg + "/item", markType: pkg + "/mark",
	}
	s.svc = s.open(t)
	if _, err := s.svc.CreateRepository(ctx, s.repo); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	s.ds = s.dataset(t)
	importVocabulary(t, s.ds, "tasks")
	itemProps := map[string]any{"name": map[string]any{"type": "string"}}
	if weighed {
		for id, props := range map[string]map[string]any{
			"kg": {"name": "kilogram", "code": "kg"},
			"g":  {"name": "gram", "code": "g"},
			"lb": {"name": "pound", "code": "[lb_av]"},
		} {
			putUnit(t, s.ds, id, props)
		}
		putConversion(t, s.ds, "lb-g", "lb", "g", map[string]any{"factor": "453.59237"})
		putConversion(t, s.ds, "g-kg", "g", "kg", map[string]any{"factor": "0.001"})
		itemProps["weight"] = map[string]any{"type": "quantity", "unit": "kg"}
	}
	helper := pkg + "/helper"
	source = strings.NewReplacer("ITEM", `"`+s.itemType+`"`, "MARK", `"`+s.markType+`"`, "TASK", `"`+staleTask+`"`,
		"HELPER", `"`+helper+`"`).Replace(source)
	declaration := map[string]any{
		"description": "sweeps the items in id order, one a page",
		"runtime":     vocabulary.RuntimePython,
		"permissions": map[string]any{
			"reads":  map[string]any{"kinds": []any{s.itemType, s.markType}},
			"writes": []any{staleTask},
			"call":   []any{helper},
		},
		"source": source,
	}
	for _, f := range fn {
		maps.Copy(declaration, f)
	}
	if err := enginetest.Install(ctx, s.ds, substrate.ActorPerson, enginetest.Manifest{
		Name: "sweep", Authority: authority,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "widget"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "mark"}, map[string]any{
				"ids":        map[string]any{"type": vocabulary.IDsNamed},
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "item"}, map[string]any{
				"ids":        map[string]any{"type": vocabulary.IDsNamed},
				"properties": itemProps,
			}),
			vocabulary.FunctionManifest(pkg, "sweep", declaration),
			vocabulary.FunctionManifest(pkg, "helper", map[string]any{
				"description": "lists one item after the cursor it is handed",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"reads": map[string]any{"kinds": []any{s.itemType}}},
				"source": `
def main(input, host):
    return {"output": host.records.list("` + s.itemType + `", first=1, order="id", after=(input.get("args") or {}).get("after") or None)}
`,
			}),
		},
		Triggers: []enginetest.Trigger{{
			ID: s.triggerID,
			Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{widgetType}}},
				"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/sweep"),
			},
		}},
	}); err != nil {
		t.Fatalf("install the sweep: %v", err)
	}
	for i, id := range s.items {
		props := map[string]any{"name": id}
		if weighed {
			props["weight"] = qty(string(rune('1'+i)), "lb")
		}
		if _, err := s.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: s.itemType, ID: id, Properties: props}); err != nil {
			t.Fatalf("put item %s: %v", id, err)
		}
	}
	restore := withMaxPages(2)
	defer restore()
	w, err := s.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "go"}})
	if err != nil {
		t.Fatal(err)
	}
	wch, err := s.ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ds.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	s.chain = chainKey(s.ds, s.triggerID, wch.Seq)
	if got := s.seen(t); strings.Join(got, ",") != "a,b" {
		failures, _ := s.ds.TriggerFailures(ctx, s.triggerID)
		t.Fatalf("the parked drain saw %v, want a and b (parked: %+v)", got, failures)
	}
	return s
}

func (s *staleSweep) open(t *testing.T) *service {
	t.Helper()
	svc, err := OpenForTest(t, context.Background(), s.dsn, append([]Option{
		WithDataRoot(s.root), WithKindsFS(SeedKinds),
		WithCredentialKey(TestCredentialKey), WithArchive(archiveDir(t, s.archive)), WithArchiveInterval(time.Hour),
	}, s.opts...)...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc.(*service)
}

func (s *staleSweep) dataset(t *testing.T) *dataset {
	t.Helper()
	d, err := s.svc.Dataset(context.Background(), s.repo)
	if err != nil {
		t.Fatalf("open the repository: %v", err)
	}
	return d.(*dataset)
}

// restore archives the repository, stops the server, and starts one on an
// empty database and data root that restores it from a copy of that
// archive: a new history generation, as every restore mints.
func (s *staleSweep) restore(t *testing.T) {
	t.Helper()
	if err := s.ds.ArchivePass(context.Background()); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := s.svc.Close(); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "archive")
	if err := copyTree(s.archive, restored); err != nil {
		t.Fatal(err)
	}
	s.dsn, s.root, s.archive = MigratedDSN(t), t.TempDir(), restored
	s.svc = s.open(t)
	s.ds = s.dataset(t)
}

// forget deletes the tasks the drain wrote for ids, so a page done again
// shows.
func (s *staleSweep) forget(t *testing.T, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := s.ds.Delete(context.Background(), substrate.ActorPerson, staleTask, "seen-"+id, substrate.DeleteInput{}); err != nil {
			t.Fatalf("delete seen-%s: %v", id, err)
		}
	}
}

func (s *staleSweep) seen(t *testing.T) (out []string) {
	t.Helper()
	for _, id := range s.items {
		if liveExists(t, s.ds, "seen-"+id) {
			out = append(out, id)
		}
	}
	return out
}

// parked is the one parked delivery of the sweep.
func (s *staleSweep) parked(t *testing.T) substrate.TriggerFailure {
	t.Helper()
	failures, err := s.ds.TriggerFailures(context.Background(), s.triggerID)
	if err != nil || len(failures) != 1 {
		t.Fatalf("the parked drain: %v (%d failures)", err, len(failures))
	}
	return failures[0]
}

// retry retries the parked delivery by hand, as an operator does.
func (s *staleSweep) retry(t *testing.T) error {
	t.Helper()
	_, err := s.ds.RetryTriggerFailure(context.Background(), s.triggerID, s.parked(t).ID)
	return err
}

func (s *staleSweep) failures(t *testing.T) int {
	t.Helper()
	failures, err := s.ds.TriggerFailures(context.Background(), s.triggerID)
	if err != nil {
		t.Fatal(err)
	}
	return len(failures)
}

// savedContinuation is the chain's stored resume cursor as JSON: "null" for
// a chain started over, "" when the chain has no row.
func (s *staleSweep) savedContinuation(t *testing.T) string {
	t.Helper()
	var raw []byte
	err := s.ds.db.QueryRowContext(context.Background(), `SELECT cursor FROM paged_cursors WHERE chain = $1`, s.chain).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// parksOnce retries the drain, whose saved list cursor the substrate refuses
// for why, and holds it to one run: parked at once, not waiting for an
// automatic retry, no page past the refusal, the saved position dropped, and
// words that say why and how to run the drain again.
func (s *staleSweep) parksOnce(t *testing.T, why string) {
	t.Helper()
	before := s.parked(t)
	if err := s.retry(t); err == nil {
		t.Fatal("the retry from the refused cursor succeeded")
	}
	after := s.parked(t)
	if after.Attempts != before.Attempts+1 {
		t.Fatalf("the retry ran %d times, want once", after.Attempts-before.Attempts)
	}
	if strings.HasPrefix(after.LastError, temporaryRetryMark) {
		t.Fatalf("the refusal waits for an automatic retry: %s", after.LastError)
	}
	for _, says := range []string{
		"the list cursor this drain saved can no longer be resumed",
		why,
		"substratectl trigger retry",
		"from its first page",
	} {
		if !strings.Contains(after.LastError, says) {
			t.Errorf("the parked delivery says %q, want it to say %q", after.LastError, says)
		}
	}
	if got := s.seen(t); strings.Join(got, ",") != "a,b" {
		t.Fatalf("the refused drain went on: saw %v", got)
	}
	if got := s.savedContinuation(t); got != "null" {
		t.Fatalf("the refused cursor was kept: %s", got)
	}
}

// finishes retries the drain started over and holds it to every item, a and
// b done again, with nothing parked and no resume row after.
func (s *staleSweep) finishes(t *testing.T) {
	t.Helper()
	s.forget(t, "a", "b")
	if err := s.retry(t); err != nil {
		t.Fatalf("the retry of the drain started over: %v", err)
	}
	if got := s.seen(t); strings.Join(got, ",") != "a,b,c,d" {
		t.Fatalf("the drain started over saw %v, want every item, a and b again", got)
	}
	if got := s.savedContinuation(t); got != "" {
		t.Fatalf("the finished drain kept the continuation %s", got)
	}
	if n := s.failures(t); n != 0 {
		t.Fatalf("%d failures after the drain finished, want none", n)
	}
}

// resumes retries the drain and holds it to the pages after the one it
// reached: c and d, with a and b not done again.
func (s *staleSweep) resumes(t *testing.T) {
	t.Helper()
	s.forget(t, "a", "b")
	if err := s.retry(t); err != nil {
		t.Fatalf("the retry: %v", err)
	}
	if got := s.seen(t); strings.Join(got, ",") != "c,d" {
		t.Fatalf("the resumed drain saw %v, want c and d alone", got)
	}
	if n := s.failures(t); n != 0 {
		t.Fatalf("%d failures after the drain finished, want none", n)
	}
}

// A conversion written between two pages of a drain whose list converts
// through the unit graph makes the list cursor its continuation holds
// unresumable: the drain parks once, its saved position dropped, and its
// retry runs it from the first page.
func TestADrainRefusedThroughTheUnitGraphParksOnceAndItsRetryStartsOver(t *testing.T) {
	s := newStaleSweep(t, "pagedunits.test.dev/pagedunits", weighedSweepSource, true)
	putConversion(t, s.ds, "lb-g", "lb", "g", map[string]any{"factor": "453.6"})
	s.parksOnce(t, "a unit or a conversion its list converts through changed since it was saved")
	s.finishes(t)
}

// A list cursor a body keeps outside its continuation, refused, is the
// body's: the run fails like any other, the continuation stays, the words
// still say a list cursor was refused and why, and once the body stops
// sending it a retry resumes from the last committed page.
func TestACursorTheBodyKeepsInARecordLeavesTheContinuation(t *testing.T) {
	ctx := context.Background()
	s := newStaleSweep(t, "pagedmark.test.dev/pagedmark", pageTokenSource, false)
	saved := s.savedContinuation(t)
	if saved != "2" {
		t.Fatalf("the parked drain saved %s, want page 2", saved)
	}
	// A list cursor minted under another history, as a body that kept one
	// across a restore holds it.
	page, err := s.ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{s.itemType}}, OrderBy: []substrate.Order{{Property: "id"}}, First: 1})
	if err != nil || page.Cursor == "" {
		t.Fatalf("a first page: %v, %v", page, err)
	}
	k, err := decodeKeyset(page.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	k.G = "another history"
	raw, _ := json.Marshal(k)
	if _, err := s.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: s.markType, ID: "bookmark",
		Properties: map[string]any{"name": base64.RawURLEncoding.EncodeToString(raw)},
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.retry(t); err == nil {
		t.Fatal("the retry listing from a refused cursor succeeded")
	}
	parked := s.parked(t)
	for _, says := range []string{
		"a list cursor the body sent can no longer be resumed",
		"the repository's history was replaced since it was saved",
		"it is kept",
	} {
		if !strings.Contains(parked.LastError, says) {
			t.Errorf("the parked delivery says %q, want it to say %q", parked.LastError, says)
		}
	}
	if got := s.savedContinuation(t); got != saved {
		t.Fatalf("the drain's own position went from %s to %s for a cursor it never held", saved, got)
	}

	if _, err := s.ds.Patch(ctx, substrate.ActorPerson, s.markType, "bookmark", substrate.PatchInput{
		Properties: map[string]any{"name": nil},
	}); err != nil {
		t.Fatal(err)
	}
	s.resumes(t)
}

// A restore keeps every continuation (decisions 0064 and 0141): a drain
// whose continuation is an outside service's page token resumes from the
// page it reached.
func TestARestoredDrainOverAnOutsideServiceResumes(t *testing.T) {
	s := newStaleSweep(t, "pagedtoken.test.dev/pagedtoken", pageTokenSource, false)
	s.restore(t)
	if got := s.savedContinuation(t); got != "2" {
		t.Fatalf("the restored drain holds %s, want page 2", got)
	}
	s.resumes(t)
}

// A restore keeps a continuation that holds the server's own list cursor
// too, which the restore's new history generation refuses: the drain parks
// once with that reason, and its retry runs it from the first page.
func TestARestoredDrainHoldingTheServersListCursorParksOnceAndStartsOver(t *testing.T) {
	s := newStaleSweep(t, "pagedrestore.test.dev/pagedrestore", sweepSource, false)
	before := s.savedContinuation(t)
	s.restore(t)
	if got := s.savedContinuation(t); got != before {
		t.Fatalf("the restore moved the continuation from %s to %s", before, got)
	}
	s.parksOnce(t, "the repository's history was replaced since it was saved")
	s.finishes(t)
}

// bookmark writes a list cursor of the items, minted now, as the bookmark a
// body keeps of its own.
func (s *staleSweep) bookmark(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	page, err := s.ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{s.itemType}}, OrderBy: []substrate.Order{{Property: "id"}}, First: 1})
	if err != nil || page.Cursor == "" {
		t.Fatalf("a first page: %v, %v", page, err)
	}
	if _, err := s.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: s.markType, ID: "bookmark", Properties: map[string]any{"name": page.Cursor}}); err != nil {
		t.Fatal(err)
	}
}

// Every refused list of a run reaches the engine: a body whose own cursor is
// refused first, and caught, and whose continuation's is refused after,
// parks once with the continuation dropped, rather than keep the one the
// first refusal did not name.
func TestARefusalAfterTheBodysOwnStillDropsTheRefusedContinuation(t *testing.T) {
	s := newStaleSweep(t, "pagedtwo.test.dev/pagedtwo", twoCursorSource, false)
	s.bookmark(t)
	s.restore(t)
	s.parksOnce(t, "the repository's history was replaced since it was saved")
	s.finishes(t)
}

// A body that catches the refusal of its continuation's cursor and then
// outlives its timeout parks at once, not as a temporary failure retried over
// minutes, with the continuation dropped.
func TestARunThatHangsAfterTheRefusalParksAtOnce(t *testing.T) {
	s := newStaleSweep(t, "pagedhang.test.dev/pagedhang", hangingSource, false, map[string]any{"timeout": "PT2S"})
	s.restore(t)
	s.parksOnce(t, "the repository's history was replaced since it was saved")
	s.finishes(t)
}

// The drop of a refused continuation and its delivery's park are one write:
// a stop between the two leaves both undone, the delivery parked as it was
// and its position as it was, never the position gone with the delivery free
// to run the drain from its first page unasked.
func TestARefusedContinuationsDropAndItsParkAreOneWrite(t *testing.T) {
	s := newStaleSweep(t, "pagedonewrite.test.dev/pagedonewrite", sweepSource, false)
	var stop atomic.Bool
	stop.Store(true)
	s.opts = []Option{WithTestCommitFault(func(stage string) error {
		if stage == parkAfterRefusedDrop && stop.Load() {
			return errors.New("the server stopped")
		}
		return nil
	})}
	before, saved := s.parked(t), s.savedContinuation(t)
	s.restore(t)
	if err := s.retry(t); err == nil {
		t.Fatal("the retry stopped between the drop and the park succeeded")
	}
	if after := s.parked(t); after.Attempts != before.Attempts || after.LastError != before.LastError {
		t.Fatalf("a park landed without its drop: %+v, before %+v", after, before)
	}
	if got := s.savedContinuation(t); got != saved {
		t.Fatalf("the position went from %s to %s with no park", saved, got)
	}
	stop.Store(false)
	s.parksOnce(t, "the repository's history was replaced since it was saved")
	s.finishes(t)
}

// A body that catches the refusal of its continuation's cursor and answers,
// with an effect the engine then refuses, parks once with the position
// dropped: what fails after the reply carries the refusals the run caught,
// whether the effect is refused as it decodes or as its page commits.
func TestARunThatCaughtTheRefusalAndAnsweredStillDropsTheRefusedContinuation(t *testing.T) {
	for name, c := range map[string]struct{ pkg, source string }{
		"refused as it decodes": {"pagedanswers.test.dev/pagedanswers", answersAfterTheRefusalSource},
		"refused as it commits": {"pagedcommits.test.dev/pagedcommits", answersWithAWriteRefusedSource},
	} {
		t.Run(name, func(t *testing.T) {
			s := newStaleSweep(t, c.pkg, c.source, false)
			s.restore(t)
			s.parksOnce(t, "the repository's history was replaced since it was saved")
			s.finishes(t)
		})
	}
}

// A drain that hands its saved cursor to a helper meets the helper's
// refusal as its own: it parks once with the position dropped.
func TestARefusalInAHelperTheDrainCalledDropsTheRefusedContinuation(t *testing.T) {
	s := newStaleSweep(t, "pagedhelper.test.dev/pagedhelper", handsItDownSource, false)
	s.restore(t)
	s.parksOnce(t, "the repository's history was replaced since it was saved")
	s.finishes(t)
}
