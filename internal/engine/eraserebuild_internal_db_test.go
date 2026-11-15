package engine

// AFTER ANY ERASURE A REBUILD REPLAYS THE HISTORY INTO THE LIVE FOLD
// (#1460): the reproduction, a record that won three merges forgotten and
// rebuilt, first; then every shape of history an erasure meets.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// mergeTrail logs every merge and split line's resync: its scope and the
// former-id rows it restates, and every line's former effects, in seq order.
func mergeTrail(t *testing.T, d *dataset, when string) {
	t.Helper()
	rows, err := d.db.QueryContext(context.Background(), `
		SELECT c.seq, c.op, e->>'kind',
			coalesce((SELECT string_agg(right(x->>'id', 4), '+') FROM jsonb_array_elements(e->'scope') x), ''),
			coalesce((SELECT string_agg(right(f->>'formerId', 4) || '>' || right(f->>'recordId', 4), ' ') FROM jsonb_array_elements(e->'rows'->'formerIds') f), ''),
			coalesce(right(e->>'formerId', 4) || '>' || right(e->>'id', 4), '')
		FROM changelog c CROSS JOIN LATERAL jsonb_array_elements(c.payload->'fold') e
		WHERE e->>'kind' IN ('resync', 'former') ORDER BY c.seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var seq int64
		var op, kind, scope, formers, former string
		if err := rows.Scan(&seq, &op, &kind, &scope, &formers, &former); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: seq %d %s %s scope [%s] rows [%s] %s", when, seq, op, kind, scope, formers, former)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// rebuildsIntoTheLiveFold holds d's history to the rule: a rebuild replays
// it and gives the fold the live repository holds. A record trigger's
// cursor is left out, as the engine's own fold snapshot leaves it out: its
// scan position past rows that did not match is written outside the ledger
// by design (delivery.go), and a rebuild gives the acknowledged position.
func rebuildsIntoTheLiveFold(t *testing.T, d *dataset, when string) {
	t.Helper()
	live := FoldRows(t, d)
	if _, err := d.svc.RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild %s: %v", when, err)
	}
	got := FoldRows(t, d)
	delete(live, "trigger_cursors")
	delete(got, "trigger_cursors")
	if diff := DiffFoldTables(live, got); len(diff) > 0 {
		t.Fatalf("the history %s replays into another fold:\n%s", when, strings.Join(diff, "\n"))
	}
}

// The reproduction: a record that won three merges, forgotten, rebuilds into
// the fold the live repository holds.
func TestARecordThatWonThreeMergesForgottenRebuilds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t)
	put := func(name string) string {
		t.Helper()
		rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": name}})
		if err != nil {
			t.Fatal(err)
		}
		return rec.ID
	}
	winner := put("winner")
	for i := range 3 {
		if _, err := d.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: ledgerTask, Winner: winner, Loser: put(fmt.Sprintf("loser %d", i))}); err != nil {
			t.Fatal(err)
		}
	}
	mergeTrail(t, d, "before the forget")
	rebuildsIntoTheLiveFold(t, d, "before the forget")
	if e, err := d.Forget(ctx, substrate.ActorPerson, ledgerTask, winner, substrate.ForgetInput{Finish: true}); err != nil || e == nil || e.Pending {
		t.Fatalf("forget: %+v (%v), want it done", e, err)
	}
	mergeTrail(t, d, "after the forget")
	rebuildsIntoTheLiveFold(t, d, "after the forget")
	if found := replayFindings(t, d); len(found) > 0 {
		t.Errorf("a verify reports %v, want no line a replay refuses", found)
	}
}

// task writes a task of ledgerTask and n changes to it, as the person.
func task(t *testing.T, d *dataset, name string, n int) string {
	t.Helper()
	ctx := context.Background()
	rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": name}})
	if err != nil {
		t.Fatal(err)
	}
	changed(t, d, rec.ID, n)
	return rec.ID
}

func changed(t *testing.T, d *dataset, id string, n int) {
	t.Helper()
	for i := range n {
		if _, err := d.Patch(context.Background(), substrate.ActorPerson, ledgerTask, id, substrate.PatchInput{
			Properties: map[string]any{"name": fmt.Sprintf("%s, change %d", id, i)},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func mergedInto(t *testing.T, d *dataset, winner, loser string) string {
	t.Helper()
	m, err := d.Merge(context.Background(), substrate.ActorPerson, substrate.MergeInput{Kind: ledgerTask, Winner: winner, Loser: loser})
	if err != nil {
		t.Fatalf("merge %s into %s: %v", loser, winner, err)
	}
	return m.ID
}

func splitBack(t *testing.T, d *dataset, merge string) {
	t.Helper()
	if _, err := d.Split(context.Background(), substrate.ActorPerson, substrate.SplitInput{Merge: merge}); err != nil {
		t.Fatalf("split %s: %v", merge, err)
	}
}

// eraseNowly erases id's history at once: permanently, or forgetting it.
func eraseNowly(t *testing.T, d *dataset, kind, id string, permanent bool) {
	t.Helper()
	ctx := context.Background()
	if permanent {
		rec, err := d.Delete(ctx, substrate.ActorPerson, kind, id, substrate.DeleteInput{Purge: true})
		if err != nil || rec.Erasure == nil || rec.Erasure.Pending {
			var e *substrate.Erasure
			if rec != nil {
				e = rec.Erasure
			}
			t.Fatalf("permanent delete of %s: %+v (%v), want its erasure done", id, e, err)
		}
		return
	}
	if e, err := d.Forget(ctx, substrate.ActorPerson, kind, id, substrate.ForgetInput{Finish: true}); err != nil || e == nil || e.Pending {
		t.Fatalf("forget of %s: %+v (%v), want it done", id, e, err)
	}
}

// laterLife puts id again after its first life was deleted and swept, and
// changes it n times.
func laterLife(t *testing.T, d *dataset, id string, n int) {
	t.Helper()
	ctx := context.Background()
	if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, ID: id, Properties: map[string]any{"name": "a later life"}}); err != nil {
		t.Fatal(err)
	}
	changed(t, d, id, n)
}

// chained merges a into b and then b into c, each changed between, so the
// second merge moves a's former id from b to c. It answers b, c and the
// merge of b into c.
func chained(t *testing.T, d *dataset) (b, c, bIntoC string) {
	t.Helper()
	a, b, c := task(t, d, "a", 1), task(t, d, "b", 1), task(t, d, "c", 1)
	mergedInto(t, d, b, a)
	changed(t, d, b, 1)
	bIntoC = mergedInto(t, d, c, b)
	changed(t, d, c, 1)
	return b, c, bIntoC
}

// replayFindings is what a verify of d reports of a line a replay refuses.
func replayFindings(t *testing.T, d *dataset) []string {
	t.Helper()
	report, err := d.svc.VerifyRepository(context.Background(), testdb.Repository(t))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return slices.DeleteFunc(report.Findings, func(f string) bool { return !strings.Contains(f, "a replay") })
}

// Every shape of history an erasure meets, erased at once as it is today:
// a rebuild after it replays the history into the fold the live repository
// holds.
func TestEveryShapeAnErasureMeetsRebuildsAfterIt(t *testing.T) {
	t.Parallel()
	shapes := []struct {
		name  string
		erase func(t *testing.T, d *dataset)
	}{
		{"a record deleted permanently", func(t *testing.T, d *dataset) {
			eraseNowly(t, d, ledgerTask, task(t, d, "plain", 4), true)
		}},
		{"a record forgotten", func(t *testing.T, d *dataset) {
			eraseNowly(t, d, ledgerTask, task(t, d, "live", 4), false)
		}},
		{"the winner of a merge, forgotten", func(t *testing.T, d *dataset) {
			w := task(t, d, "winner", 2)
			mergedInto(t, d, w, task(t, d, "loser", 2))
			changed(t, d, w, 2)
			eraseNowly(t, d, ledgerTask, w, false)
		}},
		{"the winner of a merge and its loser, deleted permanently", func(t *testing.T, d *dataset) {
			w := task(t, d, "winner", 2)
			mergedInto(t, d, w, task(t, d, "loser", 2))
			changed(t, d, w, 2)
			eraseNowly(t, d, ledgerTask, w, true)
		}},
		{"the winner of three merges, forgotten", func(t *testing.T, d *dataset) {
			w := task(t, d, "winner", 1)
			for i := range 3 {
				mergedInto(t, d, w, task(t, d, fmt.Sprintf("loser %d", i), 1))
				changed(t, d, w, 1)
			}
			eraseNowly(t, d, ledgerTask, w, false)
		}},
		{"the winner of three merges, forgotten right after the last", func(t *testing.T, d *dataset) {
			w := task(t, d, "winner", 1)
			for i := range 3 {
				mergedInto(t, d, w, task(t, d, fmt.Sprintf("loser %d", i), 1))
			}
			eraseNowly(t, d, ledgerTask, w, false)
		}},
		{"the winner of three merges, deleted permanently", func(t *testing.T, d *dataset) {
			w := task(t, d, "winner", 1)
			for i := range 3 {
				mergedInto(t, d, w, task(t, d, fmt.Sprintf("loser %d", i), 1))
				changed(t, d, w, 1)
			}
			eraseNowly(t, d, ledgerTask, w, true)
		}},
		{"a record a split gave back, forgotten", func(t *testing.T, d *dataset) {
			w := task(t, d, "winner", 2)
			splitBack(t, d, mergedInto(t, d, w, task(t, d, "loser", 2)))
			changed(t, d, w, 2)
			eraseNowly(t, d, ledgerTask, w, false)
		}},
		{"the loser of a merge split back, deleted permanently", func(t *testing.T, d *dataset) {
			w, l := task(t, d, "winner", 2), task(t, d, "loser", 2)
			splitBack(t, d, mergedInto(t, d, w, l))
			changed(t, d, l, 2)
			eraseNowly(t, d, ledgerTask, l, true)
		}},
		{"the loser of a merge split back, forgotten", func(t *testing.T, d *dataset) {
			w, l := task(t, d, "winner", 2), task(t, d, "loser", 2)
			splitBack(t, d, mergedInto(t, d, w, l))
			changed(t, d, l, 2)
			eraseNowly(t, d, ledgerTask, l, false)
		}},
		{"a record with an earlier life, forgotten", func(t *testing.T, d *dataset) {
			id := task(t, d, "first life", 2)
			laterLife(t, d, id, 2)
			eraseNowly(t, d, ledgerTask, id, false)
		}},
		{"a record with an earlier life, deleted permanently", func(t *testing.T, d *dataset) {
			id := task(t, d, "first life", 2)
			laterLife(t, d, id, 2)
			eraseNowly(t, d, ledgerTask, id, true)
		}},
		{"a chain of merges, a into b and b into c", func(t *testing.T, d *dataset) {
			chained(t, d)
		}},
		{"a chain of merges, c forgotten", func(t *testing.T, d *dataset) {
			_, c, _ := chained(t, d)
			eraseNowly(t, d, ledgerTask, c, false)
		}},
		{"a chain of merges, c deleted permanently", func(t *testing.T, d *dataset) {
			_, c, _ := chained(t, d)
			eraseNowly(t, d, ledgerTask, c, true)
		}},
		{"a chain of merges with b split back from c, b forgotten", func(t *testing.T, d *dataset) {
			b, _, m := chained(t, d)
			splitBack(t, d, m)
			changed(t, d, b, 1)
			eraseNowly(t, d, ledgerTask, b, false)
		}},
		{"a chain of merges with b split back from c, b deleted permanently", func(t *testing.T, d *dataset) {
			b, _, m := chained(t, d)
			splitBack(t, d, m)
			changed(t, d, b, 1)
			eraseNowly(t, d, ledgerTask, b, true)
		}},
		{"a chain of merges with b split back from c, c forgotten", func(t *testing.T, d *dataset) {
			_, c, m := chained(t, d)
			splitBack(t, d, m)
			changed(t, d, c, 1)
			eraseNowly(t, d, ledgerTask, c, false)
		}},
		{"a chain of merges with b split back from c, c deleted permanently", func(t *testing.T, d *dataset) {
			_, c, m := chained(t, d)
			splitBack(t, d, m)
			changed(t, d, c, 1)
			eraseNowly(t, d, ledgerTask, c, true)
		}},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			d := openInternalDataset(t)
			s.erase(t, d)
			// The history's head is never edited: a line after the erasure.
			task(t, d, "after", 0)
			rebuildsIntoTheLiveFold(t, d, "after the erasure")
			if found := replayFindings(t, d); len(found) > 0 {
				t.Errorf("a verify after the erasure reports %v, want no line a replay refuses", found)
			}
		})
	}
	for _, compacted := range []bool{false, true} {
		t.Run(fmt.Sprintf("a record named in saved positions, compacted %v", compacted), func(t *testing.T) {
			t.Parallel()
			d := supersedeDatasetOn(t, openInternalDataset(t, WithArchiveInterval(time.Hour)), nowUTC().Add(time.Hour), "create")
			ref, _ := checkpointedDrain(compacted)(t, d)
			eraseNowly(t, d, ref.Kind, ref.ID, true)
			rebuildsIntoTheLiveFold(t, d, "after the erasure")
			if found := replayFindings(t, d); len(found) > 0 {
				t.Errorf("a verify reports %v, want no line a replay refuses", found)
			}
		})
	}
	t.Run("a shipped record kept beside one erased", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		clock := time.Now().UTC()
		f := newShippedFixture(t, WithCompactionInterval(-1), WithDeletionsKept(time.Hour),
			WithTestCompactionClock(func() time.Time { return clock }))
		f.shippedDeletions()
		clock = time.Now().UTC().Add(2 * time.Hour)
		if _, err := f.ds.CompactionPass(ctx); err != nil {
			t.Fatal(err)
		}
		eraseNowly(t, f.ds, presetKind, "untouched", true)
		rebuildsIntoTheLiveFold(t, f.ds, "after the erasure")
		if found := replayFindings(t, f.ds); len(found) > 0 {
			t.Errorf("a verify reports %v, want no line a replay refuses", found)
		}
	})
}

// A history an erasure before this fix wrote, a resync rewritten without its
// record that still restates the former-id rows the rewritten scope no
// longer clears, rebuilds into the live fold, and an archive of it restores
// into the same fold.
func TestAHistoryAnEarlierErasureWroteRebuildsAndRestores(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithTestOldResyncErasure(), WithArchiveInterval(time.Hour))
	w := task(t, d, "winner", 1)
	for i := range 3 {
		mergedInto(t, d, w, task(t, d, fmt.Sprintf("loser %d", i), 1))
		changed(t, d, w, 1)
	}
	eraseNowly(t, d, ledgerTask, w, false)
	task(t, d, "after", 0)
	var stale int
	if err := d.db.QueryRowContext(ctx, `
		SELECT count(*) FROM changelog c CROSS JOIN LATERAL jsonb_array_elements(c.payload->'fold') e
		CROSS JOIN LATERAL jsonb_array_elements(coalesce(e->'rows'->'formerIds', '[]'::jsonb)) f
		WHERE e->>'kind' = 'resync' AND NOT EXISTS (
			SELECT 1 FROM jsonb_array_elements(e->'scope') s
			WHERE s->>'id' = f->>'recordId' OR s->>'id' = f->>'formerId')`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale == 0 {
		t.Fatal("the history holds no resync restating a former-id row its scope does not clear: the test proves nothing")
	}
	live := FoldRows(t, d)
	formers := live["former_ids"]
	if len(formers) < 3 {
		t.Fatalf("the live repository holds %d former ids, want the three losers': the test proves nothing", len(formers))
	}
	root := archiveTaken(t, d)
	rebuildsIntoTheLiveFold(t, d, "an earlier erasure left")
	if got := FoldRows(t, d)["former_ids"]; !slices.Equal(got, formers) {
		t.Fatalf("the former ids rebuilt are\n%s\nlive they were\n%s", strings.Join(got, "\n"), strings.Join(formers, "\n"))
	}
	if found := replayFindings(t, d); len(found) > 0 {
		t.Errorf("a verify reports %v, want no line a replay refuses: the fold takes these lines", found)
	}
	restored, err := restoredFrom(t, root)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	got := FoldRows(t, restored)
	delete(live, "trigger_cursors")
	delete(got, "trigger_cursors")
	if diff := DiffFoldTables(live, got); len(diff) > 0 {
		t.Fatalf("the restored repository holds another fold:\n%s", strings.Join(diff, "\n"))
	}
}

// A merge line an erasure before this fix left with no resync, the merge
// whose loser was split back and then deleted permanently (its split line
// lost its resync too), is refused by a
// rebuild and by a restore of the repository's own archive, both naming its
// seq, and a verify reports it. It is the limit decision 0331 states: what
// the resync restated for the live winner is gone, and no replay brings it
// back. A later repair has its fixture here.
func TestAMergeLineAnEarlierErasureLeftWithNoResyncIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithTestOldResyncErasure(), WithArchiveInterval(time.Hour))
	w, l := task(t, d, "winner", 2), task(t, d, "loser", 2)
	splitBack(t, d, mergedInto(t, d, w, l))
	changed(t, d, l, 2)
	eraseNowly(t, d, ledgerTask, l, true)
	task(t, d, "after", 0)
	var line int64
	var resyncs int
	if err := d.db.QueryRowContext(ctx, `
		SELECT seq, (SELECT count(*) FROM jsonb_array_elements(payload->'fold') e WHERE e->>'kind' = 'resync')
		FROM changelog WHERE op = 'merge' AND kind = $1 AND record_id = $2`, ledgerTask, w).Scan(&line, &resyncs); err != nil {
		t.Fatalf("the merge line: %v", err)
	}
	if resyncs != 0 {
		t.Fatalf("the merge line at seq %d holds %d resyncs, want none: the test proves nothing", line, resyncs)
	}
	var split int64
	if err := d.db.QueryRowContext(ctx, `SELECT seq FROM changelog WHERE op = 'split' AND kind = $1`, ledgerTask).Scan(&split); err != nil {
		t.Fatalf("the split line: %v", err)
	}
	refusal := fmt.Sprintf("rebuild refuses seq %d: merge cannot be replayed yet", line)
	found := replayFindings(t, d)
	if len(found) != 2 || !strings.HasPrefix(found[0], fmt.Sprintf("seq %d: a merge line with no resync", line)) ||
		!strings.HasPrefix(found[1], fmt.Sprintf("seq %d: a split line with no resync", split)) {
		t.Errorf("a verify reports %q, want the merge line at seq %d and the split line at seq %d", found, line, split)
	}
	root := archiveTaken(t, d)
	if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err == nil || !strings.Contains(err.Error(), refusal) {
		t.Errorf("rebuild: %v, want %q", err, refusal)
	}
	if _, err := restoredFrom(t, root); err == nil || !strings.Contains(err.Error(), refusal) {
		t.Errorf("restore of the repository's own archive: %v, want %q", err, refusal)
	}
}

// A resync that restates a former id another record owns is refused, as it
// always was: the fold takes a row that stands under the same owner, and no
// second owner.
func TestAResyncRestatingAnotherOwnersFormerIDIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t)
	owner, other := task(t, d, "owner", 0), task(t, d, "other", 0)
	err := d.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		if _, err := tx.exec(`INSERT INTO former_ids (record_kind, former_id, record_id, created_at) VALUES ($1, 'taken', $2, now())`, ledgerTask, owner); err != nil {
			return err
		}
		same := foldOp{Kind: foldResync, Scope: []foldRef{{Kind: ledgerTask, ID: other}}, Rows: &foldRows{
			FormerIDs: []foldFormerRow{{Kind: ledgerTask, FormerID: "taken", RecordID: owner, CreatedAt: time.Now().UTC()}},
		}}
		if err := tx.applyResync(same); err != nil {
			return fmt.Errorf("a resync restating the row as it stands: %w", err)
		}
		second := foldOp{Kind: foldResync, Scope: []foldRef{{Kind: ledgerTask, ID: other}}, Rows: &foldRows{
			FormerIDs: []foldFormerRow{{Kind: ledgerTask, FormerID: "taken", RecordID: other, CreatedAt: time.Now().UTC()}},
		}}
		return tx.applyResync(second)
	})
	if err == nil || !strings.Contains(err.Error(), "already a former id of "+owner) {
		t.Fatalf("a resync giving another record's former id to %s: %v, want it refused, naming the owner", other, err)
	}
}

// The fold's rule for a resync's former-id row reaches no further: a former
// effect outside a resync that names an id already standing points it at its
// new owner as it always did, and a live merge, and the rebuild of one,
// are as they were.
func TestAFormerEffectAndALiveMergeAreAsTheyWere(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t)
	// A former effect rides a write's line, as a merge's rides its own.
	formerOn := func(name string) string {
		var id string
		if err := d.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			rec, err := tx.put(substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": name}})
			if err != nil {
				return err
			}
			id = rec.ID
			return tx.recordFormerID(ledgerTask, "taken", id)
		}); err != nil {
			t.Fatalf("a former effect naming the id for %s: %v", name, err)
		}
		return id
	}
	formerOn("owner")
	other := formerOn("other")
	var now string
	if err := d.db.QueryRowContext(ctx, `SELECT record_id FROM former_ids WHERE record_kind = $1 AND former_id = 'taken'`, ledgerTask).Scan(&now); err != nil {
		t.Fatal(err)
	}
	if now != other {
		t.Errorf("after the second former effect the id belongs to %s, want %s, whom the effect names", now, other)
	}
	rebuildsIntoTheLiveFold(t, d, "after two former effects naming one id")
	w := task(t, d, "winner", 1)
	for i := range 3 {
		mergedInto(t, d, w, task(t, d, fmt.Sprintf("loser %d", i), 1))
	}
	rebuildsIntoTheLiveFold(t, d, "after three live merges")
}

// sideRows is a record's annotations, its marks and the former ids it owns,
// as the tables hold them.
func sideRows(t *testing.T, d *dataset, id string) []string {
	t.Helper()
	return rowsOf(t, d, `
		SELECT 'annotation ' || key || '=' || value::text FROM annotations WHERE record_kind = $1 AND record_id = $2
		UNION ALL
		SELECT 'mark ' || property || ' pinned ' || pinned::text FROM property_marks WHERE record_kind = $1 AND record_id = $2
		UNION ALL
		SELECT 'former ' || former_id || ' at ' || created_at::text FROM former_ids WHERE record_kind = $1 AND record_id = $2
		ORDER BY 1`, ledgerTask, id)
}

// restated is every annotation and mark row a merge's or a split's resync
// restates for id, as sideRows spells them.
func restated(t *testing.T, d *dataset, id string) []string {
	t.Helper()
	return rowsOf(t, d, `
		WITH e AS (
			SELECT e FROM changelog c CROSS JOIN LATERAL jsonb_array_elements(c.payload->'fold') e
			WHERE c.op IN ('merge', 'split') AND c.kind = $1 AND e->>'kind' = 'resync')
		SELECT 'annotation ' || (r->>'key') || '=' || (r->'value')::text
		FROM e CROSS JOIN LATERAL jsonb_array_elements(coalesce(e->'rows'->'annotations', '[]'::jsonb)) r WHERE r->>'id' = $2
		UNION ALL
		SELECT 'mark ' || (r->>'property') || ' pinned ' || coalesce(r->>'pinned', 'false')
		FROM e CROSS JOIN LATERAL jsonb_array_elements(coalesce(e->'rows'->'marks', '[]'::jsonb)) r WHERE r->>'id' = $2
		ORDER BY 1`, ledgerTask, id)
}

func rowsOf(t *testing.T, d *dataset, q string, args ...any) []string {
	t.Helper()
	rows, err := d.db.QueryContext(context.Background(), q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Both records of a merge split back hold an annotation and a pinned mark,
// and either is erased, forgotten or deleted permanently, after a change
// that follows the split or right after it, where the split's resync is
// later in the changelog than the erased record's state. The merge's and
// the split's resyncs held rows of the erased record before; after, they
// hold none of its values but what it or the other record holds live, none
// at all where it was purged, and a rebuild gives the live fold, both
// records' side rows included.
func TestAnErasureTakesItsRecordsValuesOutOfTheResyncs(t *testing.T) {
	t.Parallel()
	for _, loser := range []bool{true, false} {
		for _, permanent := range []bool{false, true} {
			for _, rightAfter := range []bool{false, true} {
				side, how, when := "winner", "forgotten", "changed after the split"
				if loser {
					side = "loser"
				}
				if permanent {
					how = "deleted permanently"
				}
				if rightAfter {
					when = "right after the split"
				}
				t.Run(fmt.Sprintf("the %s, %s, %s", side, how, when), func(t *testing.T) {
					t.Parallel()
					ctx := context.Background()
					d := openInternalDataset(t)
					annotate := func(id, value string) {
						t.Helper()
						if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, id, substrate.PatchInput{Annotations: map[string]any{"test/note": value}}); err != nil {
							t.Fatal(err)
						}
					}
					w := task(t, d, "winner", 1)
					annotate(w, "the winner's")
					mergedInto(t, d, w, task(t, d, "an earlier loser", 1))
					l := task(t, d, "loser", 1)
					annotate(l, "the loser's")
					splitBack(t, d, mergedInto(t, d, w, l))
					erased, other := w, l
					if loser {
						erased, other = l, w
					}
					if !rightAfter {
						annotate(erased, "changed")
						changed(t, d, erased, 1)
					}
					before := restated(t, d, erased)
					if !slices.ContainsFunc(before, func(r string) bool { return strings.HasPrefix(r, "annotation ") }) ||
						!slices.ContainsFunc(before, func(r string) bool { return strings.HasPrefix(r, "mark ") && strings.HasSuffix(r, " pinned true") }) {
						t.Fatalf("before the erasure the resyncs restate %v of the %s, want an annotation and a pinned mark: the test proves nothing", before, side)
					}
					eraseNowly(t, d, ledgerTask, erased, permanent)
					task(t, d, "after", 0)
					live, liveOther := sideRows(t, d, erased), sideRows(t, d, other)
					// A resync later than the state restates the record as of
					// its line: the winner's, right after the split, holds
					// what the merge moved onto it from the loser, which the
					// loser holds live.
					for _, r := range restated(t, d, erased) {
						if permanent || !slices.Contains(live, r) && !slices.Contains(liveOther, r) {
							t.Errorf("after the erasure a resync restates %q of the %s, which holds %v live", r, side, live)
						}
					}
					rebuildsIntoTheLiveFold(t, d, "after the erasure")
					if got := sideRows(t, d, erased); !slices.Equal(got, live) {
						t.Errorf("the %s's side rows rebuilt are %v, live they were %v", side, got, live)
					}
					if got := sideRows(t, d, other); !slices.Equal(got, liveOther) {
						t.Errorf("the other record's side rows rebuilt are %v, live they were %v", got, liveOther)
					}
					if found := replayFindings(t, d); len(found) > 0 {
						t.Errorf("a verify reports %v, want no line a replay refuses", found)
					}
				})
			}
		}
	}
}
