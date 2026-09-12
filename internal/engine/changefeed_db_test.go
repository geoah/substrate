package engine_test

// The change feed: which changes a filter admits, in what order a page hands
// them back, and what a cursor walk sees. The cursor algorithm every consumer
// runs (Changes(after=cursor), cursor = max seq observed) is pinned against
// concurrent writers, because a lower seq committing after a higher one would
// make the poller skip a row for good. The last case is the watch resume
// across a restart.

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The cross-collection feed (changefeed.go): newest-first history paging, the
// q substring filter, and per-change trigger states.

func TestChangesBeforePagesNewestFirst(t *testing.T) {
	t.Parallel()
	ds := newFnDataset(t, nil)
	ctx := context.Background()

	for _, name := range []string{"one", "two", "three"} {
		mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": name}})
	}
	forward, err := ds.Changes(ctx, 0, substrate.ChangeFilter{}, 500)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(forward) < 3 {
		t.Fatalf("changelog rows: %d", len(forward))
	}
	head := forward[len(forward)-1].Seq

	// before=0 reads from the head; each page continues strictly below the
	// previous page's oldest row.
	page, err := ds.ChangesBefore(ctx, 0, substrate.ChangeFilter{}, 2)
	if err != nil {
		t.Fatalf("changes before: %v", err)
	}
	if len(page) != 2 || page[0].Seq != head || page[1].Seq != head-1 {
		t.Fatalf("first page seqs = %v, want %d,%d", seqsOf(page), head, head-1)
	}
	rest, err := ds.ChangesBefore(ctx, page[1].Seq, substrate.ChangeFilter{}, 500)
	if err != nil {
		t.Fatalf("changes before: %v", err)
	}
	if len(rest) != len(forward)-2 || rest[0].Seq != head-2 {
		t.Fatalf("second page seqs = %v", seqsOf(rest))
	}
}

func seqsOf(changes []substrate.Change) []int64 {
	out := make([]int64, 0, len(changes))
	for _, c := range changes {
		out = append(out, c.Seq)
	}
	return out
}

func TestChangesQSubstringFilter(t *testing.T) {
	t.Parallel()
	ds := newFnDataset(t, nil)
	ctx := context.Background()

	ada := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"assignee": "kim"}})
	mustPut(t, ds, fnActor, substrate.PutInput{Kind: gadgetType, Properties: map[string]any{"count": 2.0}})

	// Case-insensitive over payload text. The payload is a delta CARRYING
	// VALUES now, so the haystack is what was written, not
	// just the property name: "kim" appears in exactly one row's payload, and
	// the feed's one search box finds a change by the value it wrote.
	hits, err := ds.ChangesBefore(ctx, 0, substrate.ChangeFilter{Q: "KIM"}, 500)
	if err != nil {
		t.Fatalf("q filter: %v", err)
	}
	if len(hits) != 1 || hits[0].RecordID != ada.ID {
		t.Fatalf("q=KIM hits = %+v", hits)
	}
	// …and over the record id, on the forward read the watch drains too.
	hits, err = ds.Changes(ctx, 0, substrate.ChangeFilter{Q: ada.ID}, 500)
	if err != nil {
		t.Fatalf("q filter: %v", err)
	}
	if len(hits) != 1 || hits[0].RecordID != ada.ID {
		t.Fatalf("q=id hits = %+v", hits)
	}
	// LIKE metacharacters are literals: nothing here contains a percent sign.
	hits, err = ds.ChangesBefore(ctx, 0, substrate.ChangeFilter{Q: "%"}, 500)
	if err != nil {
		t.Fatalf("q filter: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("q=%% hits = %+v", hits)
	}
}

func TestChangeTriggersStates(t *testing.T) {
	t.Parallel()
	// The mirror errors on a widget without a name (record.properties.name),
	// which is what parks a delivery; taskType is in the source so the
	// function's own task writes exercise self-actor exclusion.
	ds := newFnDataset(t,
		[]enginetest.Trigger{trigOn("mirror", map[string]any{"kinds": []any{widgetType, taskType}})},
		pyFn("mirror", map[string]any{}, []any{taskType}, mirrorSource))
	ctx := context.Background()
	const mirror = fnPackage + "/mirror"

	processed := mustPut(t, ds, owner, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "fine"}})
	poisoned := mustPut(t, ds, owner, substrate.PutInput{Kind: widgetType})
	process(t, ds)
	pending := mustPut(t, ds, owner, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "later"}})
	unmatched := mustPut(t, ds, owner, substrate.PutInput{Kind: gadgetType, Properties: map[string]any{"count": 1.0}})

	changes, err := ds.Changes(ctx, 0, substrate.ChangeFilter{}, 500)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	states, err := ds.ChangeTriggers(ctx, changes)
	if err != nil {
		t.Fatalf("change triggers: %v", err)
	}

	chipOf := func(recordID string, actor substrate.Actor) (substrate.ChangeTrigger, bool) {
		for _, ch := range changes {
			if ch.RecordID != recordID || ch.Actor != actor {
				continue
			}
			for _, ct := range states[ch.Seq] {
				if ct.Trigger == trigID("mirror") {
					return ct, true
				}
			}
		}
		return substrate.ChangeTrigger{}, false
	}

	if ct, ok := chipOf(processed.ID, owner); !ok || ct.State != substrate.ChangeTriggerProcessed || ct.Callable != mirror {
		t.Fatalf("processed chip = %+v (%v)", ct, ok)
	}
	if ct, ok := chipOf(poisoned.ID, owner); !ok || ct.State != substrate.ChangeTriggerParked || ct.Error == "" {
		t.Fatalf("parked chip = %+v (%v)", ct, ok)
	}
	if ct, ok := chipOf(pending.ID, owner); !ok || ct.State != substrate.ChangeTriggerPending {
		t.Fatalf("pending chip = %+v (%v)", ct, ok)
	}
	// A gadget never fires the mirror: no chip at all, not a fourth state.
	if ct, ok := chipOf(unmatched.ID, owner); ok {
		t.Fatalf("unmatched change carries a chip: %+v", ct)
	}
	// The callable's own task write matches the source by type but is its
	// own echo: self-actor exclusion drops the chip.
	if ct, ok := chipOf("t-"+processed.ID, substrate.FunctionActor(vocabulary.SplitKindRef(mirror))); ok {
		t.Fatalf("self write carries a chip: %+v", ct)
	}
}

// A merge writes one entry addressed to the winner and a split one addressed
// to the loser, and each changes both records. The record scope matches the
// payload's winner and loser too, so a feed following either id sees both
// entries, on the forward read and the backward page alike.
func TestRecordFilterMatchesMergeAndSplitForBothRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "Nina Ray"}})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "N. Ray"}})
	other := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "Someone Else"}})
	rec, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: winner.Kind, Winner: winner.ID, Loser: loser.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: rec.ID}); err != nil {
		t.Fatalf("split: %v", err)
	}

	opsOf := func(changes []substrate.Change) map[substrate.Op]int {
		out := map[substrate.Op]int{}
		for _, c := range changes {
			out[c.Op]++
		}
		return out
	}
	both := map[substrate.Op]int{substrate.OpPut: 1, substrate.OpMerge: 1, substrate.OpSplit: 1}
	for _, tc := range []struct {
		name string
		id   string
		want map[substrate.Op]int
	}{
		{"winner", winner.ID, both},
		{"loser", loser.ID, both},
		{"unrelated", other.ID, map[substrate.Op]int{substrate.OpPut: 1}},
	} {
		// The API always pairs recordId with recordKind (parseChangeFilter),
		// so the scope here carries both.
		scope := substrate.ChangeFilter{RecordID: tc.id, Kinds: []string{winner.Kind}}
		forward, err := ds.Changes(ctx, 0, scope, 500)
		if err != nil {
			t.Fatalf("%s: changes: %v", tc.name, err)
		}
		if got := opsOf(forward); !maps.Equal(got, tc.want) {
			t.Fatalf("%s: ops = %v, want %v", tc.name, got, tc.want)
		}
		backward, err := ds.ChangesBefore(ctx, 0, scope, 500)
		if err != nil {
			t.Fatalf("%s: changes before: %v", tc.name, err)
		}
		want := seqsOf(forward)
		slices.Reverse(want)
		if got := seqsOf(backward); !slices.Equal(got, want) {
			t.Fatalf("%s: backward seqs = %v, want %v", tc.name, got, want)
		}
	}
}

// TestChangesCursorWalkSkipsNoSeqUnderConcurrentWriters runs a poller's exact
// cursor algorithm (Changes(after=cursor), cursor = max seq observed) against
// the real engine while concurrent writers commit. If bigserial under READ
// COMMITTED lets a lower seq commit after a higher one, the poller permanently
// skips rows.
func TestChangesCursorWalkSkipsNoSeqUnderConcurrentWriters(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	ctx := context.Background()

	const writers = 8
	const perWriter = 60
	total := writers * perWriter

	stop := make(chan struct{})
	observed := map[int64]bool{}
	var mu sync.Mutex
	var cursor int64

	var pollWG sync.WaitGroup
	pollWG.Add(1)
	go func() {
		defer pollWG.Done()
		for {
			select {
			case <-stop:
				// final drain
				for {
					chs, err := ds.Changes(ctx, cursor, substrate.ChangeFilter{}, 200)
					if err != nil || len(chs) == 0 {
						return
					}
					mx := cursor
					for _, c := range chs {
						mu.Lock()
						observed[c.Seq] = true
						mu.Unlock()
						if c.Seq > mx {
							mx = c.Seq
						}
					}
					cursor = mx
				}
			default:
			}
			chs, err := ds.Changes(ctx, cursor, substrate.ChangeFilter{}, 200)
			if err != nil {
				t.Errorf("changes: %v", err)
				return
			}
			mx := cursor
			for _, c := range chs {
				mu.Lock()
				observed[c.Seq] = true
				mu.Unlock()
				if c.Seq > mx {
					mx = c.Seq
				}
			}
			cursor = mx
			time.Sleep(time.Millisecond)
		}
	}()

	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range perWriter {
				_, err := ds.Put(ctx, owner, substrate.PutInput{
					Kind:       enginetest.AccountType,
					ID:         extID("gmail.account", fmt.Sprintf("w%d-%d@acme.com", w, i)),
					Properties: map[string]any{"provider": "gmail", "label": "L", "status": "ok"},
				})
				if err != nil {
					t.Errorf("put: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	time.Sleep(300 * time.Millisecond)
	close(stop)
	pollWG.Wait()

	// Ground truth: every seq actually in the changelog.
	var all []int64
	var last int64
	for {
		chs, err := ds.Changes(ctx, last, substrate.ChangeFilter{}, 500)
		if err != nil {
			t.Fatalf("changes: %v", err)
		}
		if len(chs) == 0 {
			break
		}
		for _, c := range chs {
			all = append(all, c.Seq)
			last = c.Seq
		}
	}
	var missed []int64
	for _, s := range all {
		mu.Lock()
		ok := observed[s]
		mu.Unlock()
		if !ok {
			missed = append(missed, s)
		}
	}
	t.Logf("wrote %d records, changelog rows %d, poller observed %d, MISSED %d: %v",
		total, len(all), len(observed), len(missed), missed)
	if len(missed) > 0 {
		t.Fatalf("poller permanently skipped %d changelog rows: %v", len(missed), missed)
	}
}

// TestChangesCursorWalkSeesEveryPatchDuringASyncBurst models the shape a
// provider sync has in production: a burst writing message rows concurrently
// with the owner queueing outbound messages, watched by a delivery poller on
// the same filter and the same cursor algorithm. Any owner "delivery: queued"
// patch whose seq the poller never observes is a message never delivered.
func TestChangesCursorWalkSeesEveryPatchDuringASyncBurst(t *testing.T) {
	t.Parallel()
	_, ds := newVocabularyDataset(t, "messaging")
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	ctx := context.Background()

	acc := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "slack-account:T1",
		Properties: map[string]any{"provider": "slack", "label": "Acme"},
	})
	conv := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversation", ID: "slack-channel:T1:C1",
		Properties: map[string]any{"category": "channel", "name": "general", "account": enginetest.AccountType + "/" + acc.ID},
	})
	author := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "alex"},
	})

	newMsg := func(actor substrate.Actor, ext, text, delivery string) *substrate.Record {
		return mustPut(t, ds, actor, substrate.PutInput{
			Kind: "conversationmessage", ID: extID("slack.msg", ext),
			Properties: map[string]any{
				"at": "2026-08-03T10:00:00Z", "text": text, "delivery": delivery,
				"conversation": conv.ID,
				"author":       author.ID,
			},
		})
	}

	// Owner drafts N outbound messages up front; it will queue them mid-burst.
	const drafts = 40
	var draftIDs []string
	for i := range drafts {
		e := newMsg(owner, fmt.Sprintf("out/%d", i), "outbound", "draft")
		draftIDs = append(draftIDs, e.ID)
	}

	observed := map[int64]bool{}
	var mu sync.Mutex
	var cursor int64
	stop := make(chan struct{})

	// The connectors' delivery poller, verbatim in shape.
	poll := func() {
		chs, err := ds.Changes(ctx, cursor, substrate.ChangeFilter{
			Kinds: []string{"conversationmessage"},
			Ops:   []substrate.Op{substrate.OpPut, substrate.OpPatch},
		}, 200)
		if err != nil {
			t.Errorf("changes: %v", err)
			return
		}
		mx := cursor
		for _, c := range chs {
			mu.Lock()
			observed[c.Seq] = true
			mu.Unlock()
			if c.Seq > mx {
				mx = c.Seq
			}
		}
		cursor = mx
	}
	var pollWG sync.WaitGroup
	pollWG.Add(1)
	go func() {
		defer pollWG.Done()
		for {
			select {
			case <-stop:
				for range 5 {
					poll()
				}
				return
			default:
			}
			poll()
			time.Sleep(2 * time.Millisecond)
		}
	}()

	var wg sync.WaitGroup
	// Connector sync burst.
	for w := range 4 {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range 60 {
				newMsg(slack, fmt.Sprintf("in/%d/%d", w, i), "inbound", "received")
			}
		}(w)
	}
	// Owner queues its drafts concurrently; record the seq each patch earned.
	queuedSeq := map[string]int64{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, id := range draftIDs {
			if _, err := ds.Patch(ctx, owner, "conversationmessage", id, substrate.PatchInput{
				Properties: map[string]any{"delivery": "queued"},
			}); err != nil {
				t.Errorf("patch: %v", err)
				return
			}
			time.Sleep(3 * time.Millisecond)
		}
	}()
	wg.Wait()
	time.Sleep(200 * time.Millisecond)
	close(stop)
	pollWG.Wait()

	// Ground truth: the seq of every owner queued-patch row.
	var last int64
	for {
		chs, err := ds.Changes(ctx, last, substrate.ChangeFilter{
			Kinds:  []string{"conversationmessage"},
			Ops:    []substrate.Op{substrate.OpPatch},
			Actors: []substrate.Actor{owner},
		}, 500)
		if err != nil {
			t.Fatalf("changes: %v", err)
		}
		if len(chs) == 0 {
			break
		}
		for _, c := range chs {
			queuedSeq[c.RecordID] = c.Seq
			last = c.Seq
		}
	}
	var lost []string
	for id, s := range queuedSeq {
		mu.Lock()
		ok := observed[s]
		mu.Unlock()
		if !ok {
			lost = append(lost, fmt.Sprintf("%s@seq%d", id, s))
		}
	}
	t.Logf("queued patches: %d, never observed by the poller: %d %v", len(queuedSeq), len(lost), lost)
	if len(lost) > 0 {
		t.Fatalf("%d queued messages would never be delivered: %v", len(lost), lost)
	}
}

// The watch contract, per repository: the cursor IS `seq`, resume from a
// remembered one is gapless, and the horizon is 0 so any cursor at all is
// resumable. `seq` is allocated under the repository's own advisory lock, so
// it survives the only thing that could break it — a process that goes away
// between the write and the resume.
func TestWatchResumesGaplesslyAcrossARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	open := func() (substrate.Service, substrate.Dataset) {
		svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir(engine.SeedKindsDir))
		if err != nil {
			t.Fatalf("open engine: %v", err)
		}
		ds, err := svc.Dataset(ctx, testdb.Repository(t))
		if err != nil {
			t.Fatalf("open dataset: %v", err)
		}
		return svc, ds
	}

	svc1, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithKindsDir(engine.SeedKindsDir))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	if _, err := svc1.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds1, err := svc1.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	importVocabulary(t, ds1, "tasks")
	mustPut(t, ds1, owner, substrate.PutInput{
		Kind:       "samples.substrate.reamde.dev/tasks/task",
		Properties: map[string]any{"name": "before the restart"},
	})
	// What a consumer remembers: the last seq it saw.
	cursor := maxSeq(t, ds1)
	if cursor == 0 {
		t.Fatal("the changelog is empty before the restart")
	}
	if err := svc1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	svc2, ds2 := open()
	t.Cleanup(func() { _ = svc2.Close() })
	mustPut(t, ds2, owner, substrate.PutInput{
		Kind:       "samples.substrate.reamde.dev/tasks/task",
		Properties: map[string]any{"name": "after the restart"},
	})
	mustPut(t, ds2, owner, substrate.PutInput{
		Kind:       "samples.substrate.reamde.dev/tasks/task",
		Properties: map[string]any{"name": "and one more"},
	})

	rows := changesSince(t, ds2, cursor)
	if len(rows) < 2 {
		t.Fatalf("the resume returned %d changes, want the two writes past the cursor", len(rows))
	}
	// Gapless: the first row past the cursor is the very next seq, and the
	// sequence runs without a hole. A restart resets nothing — the counter is
	// the changelog's own max, per repository.
	if rows[0].Seq != cursor+1 {
		t.Fatalf("resume from %d starts at seq %d, want %d", cursor, rows[0].Seq, cursor+1)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].Seq != rows[i-1].Seq+1 {
			t.Fatalf("a gap across the restart: seq %d follows %d", rows[i].Seq, rows[i-1].Seq)
		}
	}
	// Nothing before the cursor comes back.
	for _, ch := range rows {
		if ch.Seq <= cursor {
			t.Fatalf("the resume redelivered seq %d, at or below the cursor %d", ch.Seq, cursor)
		}
	}
	// And the head keeps climbing from where the old process left it.
	if head := maxSeq(t, ds2); head <= cursor {
		t.Fatalf("the head went backwards across the restart: %d then %d", cursor, head)
	}
}
