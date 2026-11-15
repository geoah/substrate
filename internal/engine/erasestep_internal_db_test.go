package engine

// AN ERASURE IN STEPS (erasestep.go, decision 0330), held by a differential
// test: every shape of history is built once, its erasure's intent written
// and the erasure not yet run, and the archive restored twice. One copy is
// erased with bounds so small it takes many steps, the other with the
// default bounds, which erase it in one transaction as before; the two
// histories that result are the same line for line (seq, op, payload,
// checksum), and so are their folds. A third copy is erased in the same
// small steps with a rebuild after each, which gives the fold the live
// repository holds every time, and ends with the same history again.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// errBeforeErasure stops a permanent delete or a forget after its intent
// commits and before its erasure runs (erasureBetween).
var errBeforeErasure = errors.New("stopped before the erasure")

func stopBeforeErasure(stage string) error {
	if stage == erasureBetween {
		return errBeforeErasure
	}
	return nil
}

// storedLines is every line of d's history as stored, one string a line.
func storedLines(t *testing.T, d *dataset) []string {
	t.Helper()
	rows, err := d.db.QueryContext(context.Background(), `
		SELECT seq, op, kind, record_id, actor, on_behalf_of, coalesce(txn, 0), payload::text, encode(coalesce(hash, ''::bytea), 'hex')
		FROM changelog ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var seq, txn int64
		var op, kind, id, actor, behalf, payload, hash string
		if err := rows.Scan(&seq, &op, &kind, &id, &actor, &behalf, &txn, &payload, &hash); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("seq %d txn %d %s %s %s by %s for %s %s %s", seq, txn, op, kind, id, actor, behalf, payload, hash))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// erasedFrom restores root's archive and erases every pending erasure in it,
// a step of each a call, past every trigger: with the default bounds, or,
// stepped, with bounds of a few lines. It answers the dataset and how many
// calls it took.
func erasedFrom(t *testing.T, root string, stepped, rebuilding bool, lines int) (*dataset, int) {
	t.Helper()
	ctx := context.Background()
	opts := []Option{WithCompactionInterval(-1)}
	if stepped {
		opts = append(opts, WithTestCompactionBatches(0, lines, nil))
	}
	d, err := restoredFrom(t, root, opts...)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	for range 50 {
		indexed, err := d.backfillForeign(ctx, func(context.Context) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		if indexed {
			break
		}
	}
	calls := 0
	for ; calls < 500; calls++ {
		pending, err := d.pendingErasures(ctx, d.db, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			break
		}
		report, err := d.eraseNow(ctx, nil, true, time.Time{}, nil)
		if err != nil {
			t.Fatalf("erase: %v", err)
		}
		for _, w := range report.Waiting {
			if w.Held != "" && !strings.HasPrefix(w.Held, "its erasure goes on in steps") {
				t.Fatalf("the erasure of %s %s is held: %s", w.Kind, w.ID, w.Held)
			}
		}
		if rebuilding {
			rebuildsIntoTheLiveFold(t, d, fmt.Sprintf("after step %d", calls+1))
		}
	}
	if pending, err := d.pendingErasures(ctx, d.db, nil); err != nil || len(pending) > 0 {
		t.Fatalf("after %d calls %d erasures are pending (%v)", calls, len(pending), err)
	}
	return d, calls
}

// sameErasure holds the shape build writes to the differential test: the
// stepped erasure, in steps of lines, takes more calls than the whole one,
// and leaves the same history (where folds is false) and the same fold.
func sameErasure(t *testing.T, lines int, folds bool, build func(t *testing.T, d *dataset)) {
	t.Helper()
	d := openInternalDataset(t, WithCompactionInterval(-1), WithArchiveInterval(time.Hour), WithTestCompactionFault(stopBeforeErasure))
	build(t, d)
	if pending, err := d.pendingErasures(context.Background(), d.db, nil); err != nil || len(pending) == 0 {
		t.Fatalf("the shape left %d erasures pending (%v), want its intent written and its erasure not run: the test proves nothing", len(pending), err)
	}
	root := archiveTaken(t, d)
	whole, wholeCalls := erasedFrom(t, root, false, false, lines)
	stepped, steppedCalls := erasedFrom(t, root, true, false, lines)
	rebuilt, _ := erasedFrom(t, root, true, true, lines)
	if steppedCalls <= wholeCalls {
		t.Errorf("the stepped erasure took %d calls and the whole one %d: the bounds did not step it", steppedCalls, wholeCalls)
	}
	for name, d := range map[string]*dataset{"whole": whole, "stepped": stepped} {
		live := FoldRows(t, d)
		if _, err := d.svc.RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
			t.Errorf("rebuild after the %s erasure: %v", name, err)
			continue
		}
		if diff := DiffFoldTables(live, FoldRows(t, d)); len(diff) > 0 {
			t.Errorf("the history the %s erasure left replays into another fold:\n%s", name, strings.Join(diff, "\n"))
		}
	}
	a, b := storedLines(t, whole), storedLines(t, stepped)
	if c := storedLines(t, rebuilt); !slices.Equal(c, b) {
		t.Errorf("the erasure rebuilt between its steps left another history than the one in the same steps")
	}
	for i := range max(len(a), len(b)) {
		if folds {
			break
		}
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			t.Errorf("the histories differ at line %d of %d and %d:\nwhole:   %s\nstepped: %s", i, len(a), len(b), x, y)
		}
	}
	if diff := DiffFoldTables(FoldRows(t, whole), FoldRows(t, stepped)); len(diff) > 0 {
		t.Errorf("the folds differ:\n%s", strings.Join(diff, "\n"))
	}
}

// longTask writes a task and n changes to it, as the person.
func longTask(t *testing.T, d *dataset, name string, n int) string {
	t.Helper()
	ctx := context.Background()
	rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": name}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{
			Properties: map[string]any{"name": fmt.Sprintf("%s, change %d", name, i)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return rec.ID
}

// purgeBeforeErasure deletes id permanently, its intent written and its
// erasure left.
func purgeBeforeErasure(t *testing.T, d *dataset, id string) {
	t.Helper()
	if _, err := d.Delete(context.Background(), substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{Purge: true}); !errors.Is(err, errBeforeErasure) {
		t.Fatalf("permanent delete: %v, want it stopped before its erasure", err)
	}
}

// forgetBeforeErasure forgets id's history, its intent written and its
// erasure left.
func forgetBeforeErasure(t *testing.T, d *dataset, id string) {
	t.Helper()
	if _, err := d.Forget(context.Background(), substrate.ActorPerson, ledgerTask, id, substrate.ForgetInput{}); !errors.Is(err, errBeforeErasure) {
		t.Fatalf("forget: %v, want it stopped before its erasure", err)
	}
}

// head writes a line no erasure touches, so none is the history's head.
func head(t *testing.T, d *dataset) {
	t.Helper()
	if _, err := d.Put(context.Background(), substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "the head"}}); err != nil {
		t.Fatal(err)
	}
}

func merged(t *testing.T, d *dataset, winner, loser string) string {
	t.Helper()
	m, err := d.Merge(context.Background(), substrate.ActorPerson, substrate.MergeInput{Kind: ledgerTask, Winner: winner, Loser: loser})
	if err != nil {
		t.Fatalf("merge %s into %s: %v", loser, winner, err)
	}
	return m.ID
}

func patched(t *testing.T, d *dataset, id string, n int) {
	t.Helper()
	for i := range n {
		if _, err := d.Patch(context.Background(), substrate.ActorPerson, ledgerTask, id, substrate.PatchInput{
			Properties: map[string]any{"name": fmt.Sprintf("%s, later change %d", id, i)},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// manyMerges is a task that won n merges, each loser with a short history
// of its own, and changes between them: each merge leaves the winner two
// lines its erasure retains, the merge's and the one its id trail rides.
func manyMerges(t *testing.T, d *dataset, n int) string {
	t.Helper()
	winner := longTask(t, d, "winner", 4)
	for i := range n {
		merged(t, d, winner, longTask(t, d, fmt.Sprintf("loser %d", i), 2))
		patched(t, d, winner, 2)
	}
	return winner
}

// winnersLaterLife is a record that won a merge, was deleted and collected,
// so a purge took the former id the merge gave it, written again at its id
// and changed past the bounds, and is deleted permanently.
func winnersLaterLife(t *testing.T, d *dataset) {
	t.Helper()
	w := longTask(t, d, "winner", 1)
	merged(t, d, w, longTask(t, d, "loser", 2))
	laterLife(t, d, w, 12)
	head(t, d)
	purgeBeforeErasure(t, d, w)
}

// Every shape's stepped erasure leaves the history and the fold its whole
// one leaves.
func TestAnErasureInStepsLeavesWhatTheWholeOneLeaves(t *testing.T) {
	t.Parallel()
	// A shape's stepped copies take steps of lines, 3 where it says none.
	shapes := []struct {
		name  string
		lines int
		folds bool
		build func(t *testing.T, d *dataset)
	}{
		{"a record deleted permanently", 0, false, func(t *testing.T, d *dataset) {
			id := longTask(t, d, "plain", 12)
			head(t, d)
			purgeBeforeErasure(t, d, id)
		}},
		{"a live record forgotten", 0, false, func(t *testing.T, d *dataset) {
			id := longTask(t, d, "live", 12)
			head(t, d)
			forgetBeforeErasure(t, d, id)
		}},
		{"a record deleted, then deleted permanently", 0, false, func(t *testing.T, d *dataset) {
			id := longTask(t, d, "tombstoned", 12)
			if _, err := d.Delete(context.Background(), substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{}); err != nil {
				t.Fatal(err)
			}
			head(t, d)
			purgeBeforeErasure(t, d, id)
		}},
		{"the winner of a merge, deleted permanently", 0, false, func(t *testing.T, d *dataset) {
			winner, loser := longTask(t, d, "winner", 8), longTask(t, d, "loser", 8)
			merged(t, d, winner, loser)
			patched(t, d, winner, 8)
			head(t, d)
			purgeBeforeErasure(t, d, winner)
		}},
		{"the winner of a merge, forgotten", 0, false, func(t *testing.T, d *dataset) {
			winner, loser := longTask(t, d, "winner", 8), longTask(t, d, "loser", 8)
			merged(t, d, winner, loser)
			patched(t, d, winner, 8)
			head(t, d)
			forgetBeforeErasure(t, d, winner)
		}},
		{"the loser of a merge split back, deleted permanently", 0, false, func(t *testing.T, d *dataset) {
			winner, loser := longTask(t, d, "winner", 8), longTask(t, d, "loser", 8)
			m := merged(t, d, winner, loser)
			if _, err := d.Split(context.Background(), substrate.ActorPerson, substrate.SplitInput{Merge: m}); err != nil {
				t.Fatal(err)
			}
			patched(t, d, loser, 8)
			head(t, d)
			purgeBeforeErasure(t, d, loser)
		}},
		{"a record a split gave back, forgotten", 0, false, func(t *testing.T, d *dataset) {
			winner, loser := longTask(t, d, "winner", 8), longTask(t, d, "loser", 8)
			m := merged(t, d, winner, loser)
			if _, err := d.Split(context.Background(), substrate.ActorPerson, substrate.SplitInput{Merge: m}); err != nil {
				t.Fatal(err)
			}
			patched(t, d, winner, 8)
			head(t, d)
			forgetBeforeErasure(t, d, winner)
		}},
		{"a record with an earlier life, forgotten", 0, false, func(t *testing.T, d *dataset) {
			ctx := context.Background()
			id := longTask(t, d, "first life", 8)
			if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{}); err != nil {
				t.Fatal(err)
			}
			if _, err := d.RunGC(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, ID: id, Properties: map[string]any{"name": "second life"}}); err != nil {
				t.Fatal(err)
			}
			patched(t, d, id, 8)
			head(t, d)
			forgetBeforeErasure(t, d, id)
		}},
		{"a record with an earlier life, deleted permanently", 0, false, func(t *testing.T, d *dataset) {
			ctx := context.Background()
			id := longTask(t, d, "first life", 8)
			if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{}); err != nil {
				t.Fatal(err)
			}
			if _, err := d.RunGC(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, ID: id, Properties: map[string]any{"name": "second life"}}); err != nil {
				t.Fatal(err)
			}
			patched(t, d, id, 8)
			head(t, d)
			purgeBeforeErasure(t, d, id)
		}},
		{"the winner of more merges than the line bound, forgotten", 0, false, func(t *testing.T, d *dataset) {
			winner := manyMerges(t, d, 6)
			head(t, d)
			forgetBeforeErasure(t, d, winner)
		}},
		{"the winner of more merges than the line bound, deleted permanently", 0, false, func(t *testing.T, d *dataset) {
			winner := manyMerges(t, d, 6)
			head(t, d)
			purgeBeforeErasure(t, d, winner)
		}},
		// Steps of 8 lines span the earlier purge, below which a former id
		// named the record; steps of 3 meet it on its own line.
		{"a record that won a merge, deleted and collected, written again, deleted permanently", 0, false, winnersLaterLife},
		{"a record that won a merge, deleted and collected, written again, deleted permanently, in steps of 8 lines", 8, false, winnersLaterLife},
		// The whole erasure, a forgetting, passes a trigger's delivery lines
		// by; the steps before the last leave its ledger entry in place of
		// theirs (#1471). The histories differ there, and the folds agree.
		{"a trigger of a delivery history, deleted permanently", 0, true, func(t *testing.T, d *dataset) {
			installLedgerConnector(t, d, nowUTC().Add(-time.Hour).Truncate(time.Minute))
			sizes := make([]int, 12)
			for i := range sizes {
				sizes[i] = 1 << 10
			}
			largeDeliveries(t, d, ledgerHourly, sizes...)
			head(t, d)
			if _, err := d.Delete(context.Background(), substrate.ActorPerson, typeTrigger, ledgerHourly, substrate.DeleteInput{Purge: true}); !errors.Is(err, errBeforeErasure) {
				t.Fatalf("permanent delete: %v, want it stopped before its erasure", err)
			}
		}},
		{"a record a package ships, changed past the bounds, deleted permanently", 0, false, func(t *testing.T, d *dataset) {
			installShipper(t, d, "first")
			for i := range 6 {
				if _, err := d.Patch(context.Background(), substrate.ActorPerson, presetKind, "edited", substrate.PatchInput{
					Properties: map[string]any{"name": fmt.Sprintf("mine, change %d", i)},
				}); err != nil {
					t.Fatal(err)
				}
			}
			head(t, d)
			if _, err := d.Delete(context.Background(), substrate.ActorPerson, presetKind, "edited", substrate.DeleteInput{Purge: true}); !errors.Is(err, errBeforeErasure) {
				t.Fatalf("permanent delete: %v, want it stopped before its erasure", err)
			}
		}},
		{"a record with an annotation and a pinned mark, deleted permanently", 0, false, func(t *testing.T, d *dataset) {
			id := longTask(t, d, "annotated", 6)
			if _, err := d.Patch(context.Background(), substrate.ActorPerson, ledgerTask, id, substrate.PatchInput{Annotations: map[string]any{"test/note": "the record's"}}); err != nil {
				t.Fatal(err)
			}
			patched(t, d, id, 4)
			head(t, d)
			purgeBeforeErasure(t, d, id)
		}},
		{"a record the pass compacted before, deleted permanently", 0, false, func(t *testing.T, d *dataset) {
			id := longTask(t, d, "compacted", 10)
			at, err := d.Head(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := d.withCompactionTx(context.Background(), true, func(tx *txn) error {
				_, err := tx.compactRecords(at.Seq, []eref{{Kind: ledgerTask, ID: id}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			patched(t, d, id, 6)
			head(t, d)
			purgeBeforeErasure(t, d, id)
		}},
		{"a record forgotten, then deleted permanently, both before the erasure", 0, false, func(t *testing.T, d *dataset) {
			id := longTask(t, d, "twice", 10)
			head(t, d)
			forgetBeforeErasure(t, d, id)
			patched(t, d, id, 2)
			purgeBeforeErasure(t, d, id)
		}},
		{"a record named in a drain's saved positions, deleted permanently", 0, false, func(t *testing.T, d *dataset) {
			d = supersedeDatasetOn(t, d, nowUTC().Add(time.Hour), "create")
			ref, _ := checkpointedDrain(false)(t, d)
			head(t, d)
			if _, err := d.Delete(context.Background(), substrate.ActorPerson, ref.Kind, ref.ID, substrate.DeleteInput{Purge: true}); !errors.Is(err, errBeforeErasure) {
				t.Fatalf("permanent delete: %v, want it stopped before its erasure", err)
			}
		}},
		{"a chain of merges with b split back from c, b forgotten", 0, false, func(t *testing.T, d *dataset) {
			b, _, m := chained(t, d)
			splitBack(t, d, m)
			patched(t, d, b, 2)
			head(t, d)
			forgetBeforeErasure(t, d, b)
		}},
		{"a chain of merges with b split back from c, c deleted permanently", 0, false, func(t *testing.T, d *dataset) {
			_, c, m := chained(t, d)
			splitBack(t, d, m)
			patched(t, d, c, 2)
			head(t, d)
			purgeBeforeErasure(t, d, c)
		}},
		{"a record with records merged into it, deleted permanently", 0, false, func(t *testing.T, d *dataset) {
			winner := longTask(t, d, "winner", 8)
			for i := range 3 {
				merged(t, d, winner, longTask(t, d, fmt.Sprintf("loser %d", i), 4))
				patched(t, d, winner, 4)
			}
			head(t, d)
			purgeBeforeErasure(t, d, winner)
		}},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			sameErasure(t, cmp.Or(s.lines, 3), s.folds, s.build)
		})
	}
}

// stepBounds are the tests' bounds: a few lines, and a few lines of
// marked values in bytes.
const (
	stepLines = 8
	stepBytes = 16 << 10
)

// stepRead is what one step of an erasure read, as sawBatch tells it.
type stepRead struct {
	ids                    []string
	lines                  int
	bytes, largest, second int64
}

// steppedDataset is a dataset held to stepLines and stepBytes, recording
// every step's read; opts come after.
func steppedDataset(t *testing.T, opts ...Option) (*dataset, func() []stepRead) {
	t.Helper()
	var mu sync.Mutex
	var reads []stepRead
	d := openInternalDataset(t, append([]Option{
		WithCompactionInterval(-1),
		WithTestCompactionBatches(0, stepLines, nil),
		WithTestCompactionBytes(stepBytes, func(_ string, ids []string, lines int, bytes, largest, second int64) {
			mu.Lock()
			defer mu.Unlock()
			reads = append(reads, stepRead{ids: slices.Clone(ids), lines: lines, bytes: bytes, largest: largest, second: second})
		}),
	}, opts...)...)
	return d, func() []stepRead {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(reads)
	}
}

// secretTask writes a task whose every value carries a marker of its own,
// secret-NNN-, padded to size bytes, and n changes, so its lines pass both
// bounds: it answers the record and the last marker.
func secretTask(t *testing.T, d *dataset, n, size int) (string, string) {
	t.Helper()
	ctx := context.Background()
	value := func(i int) string { return fmt.Sprintf("secret-%03d-%s", i, opaque(t, size)) }
	rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": value(0)}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": value(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	return rec.ID, fmt.Sprintf("secret-%03d-", n)
}

// markersLeft is every secret-NNN- marker some line of d's history holds.
func markersLeft(t *testing.T, d *dataset) []string {
	t.Helper()
	return rowsOf(t, d, `
		SELECT DISTINCT m[1] FROM changelog, regexp_matches(payload::text, '(secret-[0-9]{3}-)', 'g') m ORDER BY 1`)
}

// passesUntilErased runs passes until no erasure is pending, at most n.
func passesUntilErased(t *testing.T, d *dataset, n int) int {
	t.Helper()
	ctx := context.Background()
	for i := range n {
		pending, err := d.pendingErasures(ctx, d.db, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			return i
		}
		if _, err := d.CompactionPass(ctx); err != nil {
			t.Fatalf("pass %d: %v", i+1, err)
		}
	}
	t.Fatalf("an erasure is still pending after %d passes", n)
	return n
}

// heldToTheBounds fails for every read past the line bound or past the byte
// bound beyond its two largest lines (compactpass.go stepAt). It sees what
// sawBatch is told of a step, the lines it took as input, not every
// statement the step ran (#1453's reads, the doors').
func heldToTheBounds(t *testing.T, reads []stepRead) {
	t.Helper()
	for i, r := range reads {
		if r.lines > stepLines {
			t.Errorf("step %d read %d lines of %v, over the bound %d", i+1, r.lines, r.ids, stepLines)
		}
		if over := r.bytes - r.largest - r.second; over > stepBytes {
			t.Errorf("step %d read %d bytes of %v, %d past its two largest lines, over the bound %d", i+1, r.bytes, r.ids, over, stepBytes)
		}
	}
}

// A record longer than the bounds in lines and in bytes is deleted
// permanently, or its history forgotten while it stays: the request does a
// bounded part and answers pending, the record goes at once where it was
// deleted, the passes finish the erasure in steps that each stay within the
// bounds, and the history holds no value of the record but, where it stays,
// its current one.
func TestALongRecordIsErasedInStepsWithinTheBounds(t *testing.T) {
	t.Parallel()
	for _, permanent := range []bool{true, false} {
		name := "forgotten"
		if permanent {
			name = "deleted permanently"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			d, reads := steppedDataset(t)
			id, current := secretTask(t, d, 40, 1<<10)
			head(t, d)
			if size := sizeOf(t, d, eref{Kind: ledgerTask, ID: id}); size.lines <= stepLines || size.over() <= stepBytes {
				t.Fatalf("the record is %d lines, %d bytes past its largest: not past both bounds, the test proves nothing", size.lines, size.over())
			}
			var e *substrate.Erasure
			if permanent {
				rec, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{Purge: true})
				if err != nil {
					t.Fatal(err)
				}
				e = rec.Erasure
				if _, err := d.Get(ctx, ledgerTask, id); !errors.Is(err, substrate.ErrNotFound) {
					t.Errorf("the record after its permanent delete: %v, want it gone at once", err)
				}
			} else {
				var err error
				if e, err = d.Forget(ctx, substrate.ActorPerson, ledgerTask, id, substrate.ForgetInput{}); err != nil {
					t.Fatal(err)
				}
			}
			if e == nil || !e.Pending || !strings.HasPrefix(e.Held, "its erasure goes on in steps") {
				t.Fatalf("the request answered %+v, want the erasure pending, going on in steps", e)
			}
			passes := passesUntilErased(t, d, 50)
			got := reads()
			if len(got) < 3 {
				t.Errorf("the erasure took %d steps, want it in steps", len(got))
			}
			heldToTheBounds(t, got)
			want := []string(nil)
			if !permanent {
				want = []string{current}
			}
			if left := markersLeft(t, d); !slices.Equal(left, want) {
				t.Errorf("after the erasure the history holds the markers %v, want %v", left, want)
			}
			rebuildsIntoTheLiveFold(t, d, "after the erasure")
			t.Logf("%d steps over %d passes", len(got), passes)
		})
	}
}

// A record within the bounds is erased in the request's one transaction, as
// it always was: the reply reads done, after one step.
func TestARecordWithinTheBoundsIsErasedInOneTransaction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d, reads := steppedDataset(t)
	id, _ := secretTask(t, d, 2, 64)
	head(t, d)
	rec, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{Purge: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Erasure == nil || rec.Erasure.Pending {
		t.Fatalf("the permanent delete answered %+v, want the erasure done", rec.Erasure)
	}
	if got := reads(); len(got) != 1 {
		t.Errorf("the erasure took %d steps, want one: %+v", len(got), got)
	}
	if left := markersLeft(t, d); len(left) > 0 {
		t.Errorf("after the erasure the history holds %v", left)
	}
}

// A crash between two steps, a step that committed and a server that then
// stopped, ends the pass; the intent stays, and the next pass goes on from
// what the history holds and finishes.
func TestAnErasureInStepsResumesAfterACrashBetweenSteps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var crash atomic.Int32
	d, _ := steppedDataset(t, WithTestCompactionFault(func(stage string) error {
		if stage == erasureAfterStep && crash.Add(-1) == 0 {
			return context.Canceled
		}
		return nil
	}))
	id, _ := secretTask(t, d, 40, 1<<10)
	head(t, d)
	crash.Store(1 << 20)
	if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatal(err)
	}
	before := len(storedLines(t, d))
	crash.Store(3)
	if _, err := d.CompactionPass(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("the pass: %v, want it ended by the crash after its third step", err)
	}
	if pending, err := d.pendingErasures(ctx, d.db, nil); err != nil || len(pending) != 1 {
		t.Fatalf("after the crash %d erasures are pending (%v), want the one", len(pending), err)
	}
	if after := len(storedLines(t, d)); after >= before {
		t.Fatalf("the history is %d lines after three steps, %d before: the steps before the crash took nothing, the test proves nothing", after, before)
	}
	rebuildsIntoTheLiveFold(t, d, "after the crash")
	crash.Store(1 << 20)
	passesUntilErased(t, d, 50)
	if left := markersLeft(t, d); len(left) > 0 {
		t.Errorf("after the erasure the history holds %v", left)
	}
	rebuildsIntoTheLiveFold(t, d, "after the erasure")
}

// An erasure whose every step fails does not stop the pass: another pending
// erasure finishes and another record is compacted in the same pass, and
// the failing one reads pending, saying its last step failed, until a pass
// where it no longer fails finishes it.
func TestAnErasureWhoseStepFailsEveryTimeLeavesThePassItsWork(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var failing atomic.Value
	failing.Store("")
	var between atomic.Bool
	clock := time.Now().UTC()
	d, _ := steppedDataset(t,
		WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != ledgerTask, 0 }),
		WithTestCompactionClock(func() time.Time { return clock }),
		WithTestCompactionFault(func(stage string) error {
			if between.Load() && stage == erasureBetween {
				return errBeforeErasure
			}
			if f := failing.Load().(string); f != "" && stage == erasureInStep+": "+ledgerTask+"/"+f {
				return errors.New("this step fails every time")
			}
			return nil
		}))
	stuck, _ := secretTask(t, d, 40, 1<<10)
	small, _ := secretTask(t, d, 1, 64)
	other := longTask(t, d, "another record", 6)
	head(t, d)
	failing.Store(stuck)
	rec, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, stuck, substrate.DeleteInput{Purge: true})
	if err != nil || rec.Erasure == nil || !rec.Erasure.Pending {
		t.Fatalf("the permanent delete whose erasure fails: %+v (%v), want it deleted and its erasure pending", rec, err)
	}
	between.Store(true)
	if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, small, substrate.DeleteInput{Purge: true}); !errors.Is(err, errBeforeErasure) {
		t.Fatalf("the small permanent delete: %v, want it stopped before its erasure", err)
	}
	between.Store(false)
	before := writtenLines(t, d, eref{Kind: ledgerTask, ID: other})
	clock = time.Now().UTC().Add(2 * time.Hour)
	if _, err := d.CompactionPass(ctx); err != nil {
		t.Fatalf("the pass: %v, want it to go on past the failing erasure", err)
	}
	pending, err := d.pendingErasures(ctx, d.db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != stuck {
		t.Fatalf("after the pass %+v is pending, want the failing erasure alone", pending)
	}
	if held, err := d.erasureHeldNote(ctx, eref{Kind: ledgerTask, ID: stuck}); err != nil || !strings.HasPrefix(held, "its last step failed") {
		t.Errorf("the failing erasure's progress row says %q (%v), want its last step failed", held, err)
	}
	if after := writtenLines(t, d, eref{Kind: ledgerTask, ID: other}); after >= before {
		t.Errorf("the other record has %d written lines after the pass, %d before: the pass compacted nothing", after, before)
	}
	failing.Store("")
	passesUntilErased(t, d, 50)
	if left := markersLeft(t, d); len(left) > 0 {
		t.Errorf("after the erasures the history holds %v", left)
	}
	rebuildsIntoTheLiveFold(t, d, "after the erasures")
}

// A record put again at a purged id while the purge's erasure goes on in
// steps keeps its own history: the erasure takes the history up to its
// intent and no further, and the new life's values stay.
func TestARecordPutAtAPurgedIDKeepsItsHistoryWhileTheErasureSteps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d, _ := steppedDataset(t)
	id, _ := secretTask(t, d, 40, 1<<10)
	head(t, d)
	rec, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{Purge: true})
	if err != nil || rec.Erasure == nil || !rec.Erasure.Pending {
		t.Fatalf("the permanent delete: %+v (%v), want its erasure pending", rec, err)
	}
	if _, err := d.eraseNow(ctx, nil, true, time.Time{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, ID: id, Properties: map[string]any{"name": "secret-900-a later life"}}); err != nil {
		t.Fatalf("put at the purged id: %v", err)
	}
	if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, id, substrate.PatchInput{Properties: map[string]any{"name": "secret-901-a later life, changed"}}); err != nil {
		t.Fatal(err)
	}
	passesUntilErased(t, d, 50)
	if left := markersLeft(t, d); !slices.Equal(left, []string{"secret-900-", "secret-901-"}) {
		t.Errorf("after the erasure the history holds the markers %v, want the later life's alone", left)
	}
	got, err := d.Get(ctx, ledgerTask, id)
	if err != nil || got.Properties["name"] != "secret-901-a later life, changed" {
		t.Errorf("the later life after the erasure: %+v (%v)", got, err)
	}
	rebuildsIntoTheLiveFold(t, d, "after the erasure")
}

// A trigger of a delivery history past the bounds is deleted permanently in
// steps, and one within them in one transaction. Each erasure ends, the
// history verifies, a rebuild gives the live fold, and nothing of the
// trigger record's values is left: no line writes its row. Its delivery
// lines are not erased (#1471): one within the bounds keeps every one, in
// steps the steps before the last leave the trigger's ledger entry in place
// of theirs, and the last, a forgetting that passes them by, keeps the lines
// it reaches, at most what fits within the bounds.
func TestATriggersDeliveryHistoryIsErasedInSteps(t *testing.T) {
	t.Parallel()
	for _, long := range []bool{true, false} {
		name := "within the bounds"
		if long {
			name = "past the bounds"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			// Within the bounds means the trigger lines written since its
			// first line fit too: the default bounds, not the small ones.
			d, reads := steppedDataset(t, WithArchiveInterval(time.Hour))
			if !long {
				d = openInternalDataset(t, WithCompactionInterval(-1), WithArchiveInterval(time.Hour))
				reads = func() []stepRead { return nil }
			}
			installLedgerConnector(t, d, nowUTC().Add(-time.Hour).Truncate(time.Minute))
			n := 2
			if long {
				n = 40
			}
			sizes := make([]int, n)
			for i := range sizes {
				sizes[i] = 2 << 10
			}
			largeDeliveries(t, d, ledgerHourly, sizes...)
			head(t, d)
			deliveries := func() []string {
				t.Helper()
				return rowsOf(t, d, `
					SELECT seq || ' ' || (SELECT string_agg(e->>'kind', ',') FROM jsonb_array_elements(payload->'fold') e) || ' ' || pg_column_size(payload)
					FROM changelog WHERE kind = $1 AND record_id = $2 AND op = 'delivery' ORDER BY seq`, typeTrigger, ledgerHourly)
			}
			before := deliveries()
			if size := sizeOf(t, d, eref{Kind: typeTrigger, ID: ledgerHourly}); long != (size.lines > stepLines && size.over() > stepBytes) {
				t.Fatalf("the trigger is %d lines, %d bytes past its largest: the test proves nothing", size.lines, size.over())
			}
			rec, err := d.Delete(ctx, substrate.ActorPerson, typeTrigger, ledgerHourly, substrate.DeleteInput{Purge: true})
			if err != nil || rec.Erasure == nil || rec.Erasure.Pending != long {
				var e substrate.Erasure
				if rec != nil && rec.Erasure != nil {
					e = *rec.Erasure
				}
				t.Fatalf("the permanent delete: %+v (%v), want its erasure pending %v", e, err, long)
			}
			passesUntilErased(t, d, 50)
			heldToTheBounds(t, reads())
			if written := rowsOf(t, d, `
				SELECT c.seq || ' ' || (e->>'kind') FROM changelog c CROSS JOIN LATERAL jsonb_array_elements(c.payload->'fold') e
				WHERE e->>'ref' = $1 AND e->>'id' = $2
				  AND (e->>'kind' IN ('record', 'annotation', 'mark') OR (e->>'kind' = 'state' AND e->'state' ? 'row'))`, typeTrigger, ledgerHourly); len(written) > 0 {
				t.Errorf("after the erasure lines still write the trigger record: %v", written)
			}
			left := deliveries()
			for _, l := range left {
				t.Logf("left: %s", l)
			}
			t.Logf("%d delivery lines before, %d left", len(before), len(left))
			switch {
			case !long && slices.ContainsFunc(before, func(l string) bool { return !slices.Contains(left, l) }):
				t.Errorf("a trigger within the bounds keeps %v of its delivery lines %v, want every one, beside the delete's own (#1471)", left, before)
			case long && (len(left) == 0 || len(left) > stepLines || !strings.Contains(left[0], " ledger ")):
				t.Errorf("a trigger erased in steps keeps the delivery lines %v, want its ledger entry and at most %d lines the last step reached (#1471)", left, stepLines)
			}
			report, err := d.svc.VerifyRepositoryWith(ctx, testdb.Repository(t), VerifyOptions{Recanonicalize: true})
			if err != nil || len(report.Findings) > 0 {
				t.Errorf("verify: %v %v", report.Findings, err)
			}
			rebuildsIntoTheLiveFold(t, d, "after the erasure")
		})
	}
}

// A record deleted permanently, its erasure's search past the head, is
// written again and delivered to a record trigger that saves a position
// about the new line, the first erasure's search reads past that position,
// which names no line up to its intent, and the record is deleted
// permanently again before the erasure finished. The wider intent searches
// again from the record's first line: the position holds no value of the
// record when the erasure reads done, and the history verifies and
// rebuilds.
func TestAWiderIntentSearchesAgainForTheSavedPositions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stepped, _ := steppedDataset(t, WithArchiveInterval(time.Hour))
	d := supersedeDatasetOn(t, stepped, nowUTC().Add(time.Hour), "create")
	job := eref{Kind: supersedeJob, ID: fid("checkpointed")}
	shapePut(t, d, substrate.PutInput{Kind: job.Kind, ID: job.ID, Properties: map[string]any{"name": "the first life"}})
	processOnce(t, d)
	failures, err := d.TriggerFailures(ctx, supersedeRec)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range failures {
		if err := d.ForgetTriggerFailure(ctx, supersedeRec, f.ID); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 30 {
		if _, err := d.Patch(ctx, substrate.ActorPerson, job.Kind, job.ID, substrate.PatchInput{Properties: map[string]any{"name": fmt.Sprintf("the first life, change %d", i)}}); err != nil {
			t.Fatal(err)
		}
	}
	head(t, d)
	if rec, err := d.Delete(ctx, substrate.ActorPerson, job.Kind, job.ID, substrate.DeleteInput{Purge: true}); err != nil || rec.Erasure == nil || !rec.Erasure.Pending {
		t.Fatalf("the first permanent delete: %v, want its erasure pending", err)
	}
	searchedToHead := func(when string) {
		t.Helper()
		for i := range 20 {
			var searched, top int64
			if err := d.db.QueryRowContext(ctx, `
				SELECT coalesce((SELECT searched FROM compaction_erasure_progress WHERE record_kind = $1 AND record_id = $2), 0),
					(SELECT max(seq) FROM changelog)`, job.Kind, job.ID).Scan(&searched, &top); err != nil {
				t.Fatal(err)
			}
			if searched >= top {
				return
			}
			if i > 0 {
				if pending, err := d.pendingErasures(ctx, d.db, []eref{job}); err != nil || len(pending) == 0 {
					t.Fatalf("%s the first erasure finished before its search reached the head (%v): the test proves nothing", when, err)
				}
			}
			if _, err := d.eraseNow(ctx, []eref{job}, true, time.Time{}, nil); err != nil {
				t.Fatal(err)
			}
		}
		t.Fatalf("%s the first erasure's search never reached the head", when)
	}
	searchedToHead("before the second life:")
	_, values := checkpointedDrain(false)(t, d)
	searchedToHead("past the saved position:")
	if pending, err := d.pendingErasures(ctx, d.db, []eref{job}); err != nil || len(pending) == 0 {
		t.Fatalf("the first erasure finished before the second delete (%v): the test proves nothing", err)
	}
	if _, err := d.Delete(ctx, substrate.ActorPerson, job.Kind, job.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("the second permanent delete: %v", err)
	}
	passesUntilErased(t, d, 100)
	for _, v := range values {
		var n int
		if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE strpos(payload::text, $1) > 0`, v).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			t.Errorf("after the erasure %d lines still hold %q", n, v)
		}
	}
	report, err := d.svc.VerifyRepositoryWith(ctx, testdb.Repository(t), VerifyOptions{Recanonicalize: true})
	if err != nil || len(report.Findings) > 0 {
		t.Errorf("verify: %v %v", report.Findings, err)
	}
	rebuildsIntoTheLiveFold(t, d, "after the erasure")
}

// More long erasures than one pass's share reaches are pending: each pass
// takes a step of the least recently stepped and still compacts another
// record, of a kind whose history it may take (the tasks keep theirs, so
// the pass does not shorten the long ones), and over the passes every
// erasure takes steps.
func TestAPassSharesItsErasuresAndStillCompacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var between atomic.Bool
	clock := time.Now().UTC()
	d, _ := steppedDataset(t,
		WithTestErasureShare(time.Nanosecond),
		WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != presetKind, 0 }),
		WithTestCompactionClock(func() time.Time { return clock }),
		WithTestCompactionFault(func(stage string) error {
			if between.Load() && stage == erasureBetween {
				return errBeforeErasure
			}
			return nil
		}))
	var long []string
	for range 3 {
		id, _ := secretTask(t, d, 40, 1<<10)
		long = append(long, id)
	}
	installShipper(t, d, "first")
	other := eref{Kind: presetKind, ID: "edited"}
	for i := range 6 {
		if _, err := d.Patch(ctx, substrate.ActorPerson, other.Kind, other.ID, substrate.PatchInput{Properties: map[string]any{"name": fmt.Sprintf("mine, change %d", i)}}); err != nil {
			t.Fatal(err)
		}
	}
	head(t, d)
	between.Store(true)
	for _, id := range long {
		if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{Purge: true}); !errors.Is(err, errBeforeErasure) {
			t.Fatalf("permanent delete: %v, want it stopped before its erasure", err)
		}
	}
	between.Store(false)
	stepped := func() []string {
		t.Helper()
		return rowsOf(t, d, `SELECT record_id FROM compaction_erasure_progress ORDER BY record_id`)
	}
	before := writtenLines(t, d, other)
	clock = time.Now().UTC().Add(2 * time.Hour)
	for pass := 1; pass <= len(long); pass++ {
		if _, err := d.CompactionPass(ctx); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if got := stepped(); len(got) != pass {
			t.Errorf("after pass %d the erasures of %v took steps, want %d of them, one a pass", pass, got, pass)
		}
		if pass == 1 {
			if after := writtenLines(t, d, other); after >= before {
				t.Errorf("the other record has %d written lines after the first pass, %d before: the pass compacted nothing", after, before)
			}
		}
	}
	passesUntilErased(t, d, 200)
	if left := markersLeft(t, d); len(left) > 0 {
		t.Errorf("after the erasures the history holds %v", left)
	}
	rebuildsIntoTheLiveFold(t, d, "after the erasures")
}

// A plain compaction of the whole history after every step of an erasure
// leaves the erasure to finish: no value of the record is left, the history
// verifies and a rebuild gives the live fold.
func TestAWholeCompactionAfterEveryStepLeavesNoValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d, _ := steppedDataset(t, WithArchiveInterval(time.Hour))
	w := longTask(t, d, "winner", 2)
	merged(t, d, w, longTask(t, d, "loser", 2))
	id, _ := secretTask(t, d, 40, 1<<10)
	merged(t, d, id, w)
	head(t, d)
	if rec, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{Purge: true}); err != nil || rec.Erasure == nil || !rec.Erasure.Pending {
		t.Fatalf("the permanent delete: %v, want its erasure pending", err)
	}
	steps := 0
	for ; steps < 200; steps++ {
		pending, err := d.pendingErasures(ctx, d.db, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			break
		}
		if _, err := d.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil {
			t.Fatalf("the whole compaction after step %d: %v", steps, err)
		}
		if _, err := d.eraseNow(ctx, nil, true, time.Time{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if steps < 3 {
		t.Errorf("the erasure took %d steps, want it in steps", steps)
	}
	if left := markersLeft(t, d); len(left) > 0 {
		t.Errorf("after the erasure the history holds %v", left)
	}
	report, err := d.svc.VerifyRepositoryWith(ctx, testdb.Repository(t), VerifyOptions{Recanonicalize: true})
	if err != nil || len(report.Findings) > 0 {
		t.Errorf("verify: %v %v", report.Findings, err)
	}
	rebuildsIntoTheLiveFold(t, d, "after the erasure")
}

// A merge of two records of another kind whose pair shares a task's id, on
// a line that carries an effect on the task (one its transaction left
// pending rides it), is listed for the task by the foreign-effects index.
// The task's permanent delete leaves that merge line as it was: it is no
// merge of the task's, and what it moved is no copy of the task's values.
func TestAnErasureLeavesAnotherKindsMergeOfTheSameID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithCompactionInterval(-1), WithArchiveInterval(time.Hour))
	installShipper(t, d, "first")
	id := task(t, d, "a task", 0)
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: presetKind, ID: id, Properties: map[string]any{"name": "a preset of the task's id"}}); err != nil {
		t.Fatal(err)
	}
	if err := d.inTx(ctx, substrate.ActorPerson, false, func(tx *txn) error {
		if err := tx.recordFormerID(ledgerTask, "a former task", id); err != nil {
			return err
		}
		_, err := tx.mergeRecord(eref{Kind: presetKind, ID: id}, eref{Kind: presetKind, ID: "deleted"})
		return err
	}); err != nil {
		t.Fatalf("merge the presets: %v", err)
	}
	var line int64
	var payload string
	if err := d.db.QueryRowContext(ctx, `SELECT seq, payload::text FROM changelog WHERE op = 'merge' AND kind = $1 AND record_id = $2`, presetKind, id).Scan(&line, &payload); err != nil {
		t.Fatalf("the presets' merge line: %v", err)
	}
	var listed bool
	if err := d.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM changelog_foreign WHERE seq = $1 AND record_kind = $2 AND record_id = $3)`, line, ledgerTask, id).Scan(&listed); err != nil {
		t.Fatal(err)
	}
	if !listed || !strings.Contains(payload, `"moved"`) {
		t.Fatalf("the presets' merge line at seq %d is listed for the task %v and reads %s: the test proves nothing", line, listed, payload)
	}
	rec, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{Purge: true})
	if err != nil || rec.Erasure == nil || rec.Erasure.Pending {
		t.Fatalf("the task's permanent delete: %v, want its erasure done", err)
	}
	var after string
	if err := d.db.QueryRowContext(ctx, `SELECT payload::text FROM changelog WHERE seq = $1`, line).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != payload {
		t.Errorf("the task's erasure rewrote the presets' merge line:\nbefore %s\nafter  %s", payload, after)
	}
}
