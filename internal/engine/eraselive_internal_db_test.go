package engine

// A LIVE RECORD'S ERASURE OVER WHAT A COMPACTION KEEPS. After a live
// record's erasure no kept effect holds a value of it: a merge's or a
// split's resync that named it restates its partner alone, and an earlier
// purge is erased across where nothing it removed can come back at a
// replay. The fold is the fold before, and a rebuild gives it again.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// forgetHeldToTheFold forgets ref's history and holds the erasure to what a
// live record's erasure keeps: done, no intent left, the fold as it was, a
// rebuild giving the same fold, and none of values left in any store.
func forgetHeldToTheFold(t *testing.T, d *dataset, ref eref, values ...string) *substrate.Erasure {
	t.Helper()
	return forgetKeepingOnlyIn(t, d, ref, nil, values...)
}

// forgetKeepingOnlyIn is forgetHeldToTheFold where values may stay in the
// records keptIn alone: their rows, their own lines, the search index over
// those rows and the archive's pieces of the changelog, and nowhere else.
func forgetKeepingOnlyIn(t *testing.T, d *dataset, ref eref, keptIn []eref, values ...string) *substrate.Erasure {
	t.Helper()
	ctx := context.Background()
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	for _, v := range values {
		if len(valuesLeft(t, d, v)) == 0 {
			t.Fatalf("%q is nowhere before the forget: the test proves nothing", v)
		}
	}
	before := FoldRows(t, d)
	e, err := d.Forget(ctx, substrate.ActorPerson, ref.Kind, ref.ID, substrate.ForgetInput{})
	if err != nil || e == nil || e.Pending {
		t.Fatalf("the forget answered %+v (%v), want its erasure done", e, err)
	}
	after := FoldRows(t, d)
	if diff := DiffFoldTables(before, after); len(diff) > 0 {
		t.Fatalf("the forget changed the fold:\n%s", strings.Join(diff, "\n"))
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, l := range valuesLeft(t, d, values...) {
		if len(keptIn) > 0 && (strings.HasPrefix(l, "changelog holds") || strings.HasPrefix(l, "records holds") ||
			strings.HasPrefix(l, "the search index holds") || (strings.HasPrefix(l, "file archive/") && strings.Contains(l, "/changelog/"))) {
			continue
		}
		left = append(left, l)
	}
	if len(left) > 0 {
		t.Fatalf("values remain after the forget:\n%s", strings.Join(left, "\n"))
	}
	for _, v := range values {
		for what, q := range map[string]string{
			"a line":           `SELECT kind, record_id FROM changelog WHERE strpos(payload::text, $1) > 0`,
			"a row":            `SELECT kind, id FROM records WHERE strpos(props::text || coalesce(labels::text, ''), $1) > 0`,
			"the search index": `SELECT kind, id FROM records WHERE fts @@ plainto_tsquery('english', $1)`,
		} {
			for _, h := range refsIn(t, d, q, v) {
				if !slices.Contains(keptIn, h) {
					t.Fatalf("%s of %s %s holds %q after the forget, want only %v", what, h.Kind, h.ID, v, keptIn)
				}
			}
		}
	}
	if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	if diff := DiffFoldTables(after, FoldRows(t, d)); len(diff) > 0 {
		t.Fatalf("the erased history replays into another fold:\n%s", strings.Join(diff, "\n"))
	}
	var pending int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures`).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("%d erasures pending after the forget (%v), want none", pending, err)
	}
	return e
}

// thingWithOldValues is a thing whose first name and note are the values
// its forget must take, and the loser of the merge beside it.
func thingWithOldValues(t *testing.T, d *dataset) (*substrate.Record, *substrate.Record) {
	t.Helper()
	winner := shapePut(t, d, substrate.PutInput{
		Kind: shapesThing, Properties: map[string]any{"name": "erasable-winner-old"},
		Annotations: map[string]any{"owner/note": map[string]any{"why": "erasable-note-old"}},
	})
	loser := shapePut(t, d, substrate.PutInput{
		Kind: shapesThing, Properties: map[string]any{"name": "the loser"},
		Annotations: map[string]any{"owner/other": map[string]any{"why": "the loser's note"}},
	})
	return winner, loser
}

func renameThing(t *testing.T, d *dataset, r *substrate.Record) {
	t.Helper()
	if _, err := d.Patch(context.Background(), substrate.ActorPerson, r.Kind, r.ID, substrate.PatchInput{
		Properties:  map[string]any{"name": "the winner now"},
		Annotations: map[string]any{"owner/note": map[string]any{"why": "the note now"}},
	}); err != nil {
		t.Fatalf("rename: %v", err)
	}
}

// A record that won a merge, forgotten: the merge's resync restated the
// winner's old note, and after the forget restates the loser alone; the
// loser and the former-id trail are as they were. Once with the winner
// written after the merge, once with the merge's line its newest, where
// the state and the resync share the line.
func TestForgettingARecordThatWonAMerge(t *testing.T) {
	t.Parallel()
	for _, newest := range []bool{false, true} {
		name := "written after the merge"
		if newest {
			name = "the merge's line its newest"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := shapesDataset(t)
			winner, loser := thingWithOldValues(t, d)
			if newest {
				renameThing(t, d, winner)
			}
			if _, err := d.Merge(context.Background(), substrate.ActorPerson, substrate.MergeInput{Kind: shapesThing, Winner: winner.ID, Loser: loser.ID}); err != nil {
				t.Fatalf("merge: %v", err)
			}
			if !newest {
				renameThing(t, d, winner)
			}
			forgetHeldToTheFold(t, d, shapeRef(winner), "erasable-winner-old", "erasable-note-old")
		})
	}
}

// A record a split rewrote, forgotten: the merge's and the split's resyncs
// each restate the other record alone afterwards. What the split moved back
// stays in the split's own record (#1408), the one place the old note is
// left, and the reply names it and the merge's record.
func TestForgettingARecordASplitRewrote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := shapesDataset(t)
	winner, loser := thingWithOldValues(t, d)
	merge, err := d.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: shapesThing, Winner: winner.ID, Loser: loser.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	split, err := d.Split(ctx, substrate.ActorPerson, substrate.SplitInput{Merge: merge.ID})
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	renameThing(t, d, winner)
	splitRef := eref{Kind: split.Kind, ID: split.ID}
	e := forgetKeepingOnlyIn(t, d, shapeRef(winner), []eref{splitRef}, "erasable-winner-old", "erasable-note-old")
	var named []string
	for _, k := range e.Kept {
		named = append(named, k.Record)
		if k.Why == "" {
			t.Errorf("the reply keeps %s with no reason", k.Record)
		}
	}
	for _, want := range []string{vocabulary.RecordPath(merge.Kind, merge.ID), vocabulary.RecordPath(split.Kind, split.ID)} {
		if !slices.Contains(named, want) {
			t.Errorf("the reply's kept is %v, want %s named", named, want)
		}
	}
}

// A merged person as the mapped target of a contact permanently deleted:
// the copy the mapping made goes from the person's history, its merge
// notwithstanding, and no intent is left.
func TestAPermanentDeleteErasesTheCopyInAMergedTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := shapesDataset(t)
	person := shapePut(t, d, substrate.PutInput{Kind: subjPerson, Properties: map[string]any{"emails": []any{"ada@example.com"}}})
	other := shapePut(t, d, substrate.PutInput{Kind: subjPerson, Properties: map[string]any{"emails": []any{"ada@example.org"}}})
	if _, err := d.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: subjPerson, Winner: person.ID, Loser: other.ID}); err != nil {
		t.Fatalf("merge the people: %v", err)
	}
	const copied = "erasable-copied-name"
	contact := shapePut(t, d, substrate.PutInput{Kind: shapesContact, Properties: map[string]any{"email": "ada@example.com", "name": copied}})
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	var got *string
	if err := d.db.QueryRow(`SELECT props->>'name' FROM records WHERE kind = $1 AND id = $2`, person.Kind, person.ID).Scan(&got); err != nil || got == nil || *got != copied {
		t.Fatalf("the contact did not give the merged person its name (%v): the test proves nothing", err)
	}
	deleted, err := d.Delete(ctx, substrate.ActorPerson, contact.Kind, contact.ID, substrate.DeleteInput{Purge: true})
	if err != nil || deleted.Erasure == nil || deleted.Erasure.Pending {
		t.Fatalf("the permanent delete answered %+v (%v), want its erasure done", deleted, err)
	}
	var pending int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures`).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("%d erasures pending after the delete (%v), want none: the merged person's copy waits", pending, err)
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if left := valuesLeft(t, d, copied); len(left) > 0 {
		t.Fatalf("the copy remains:\n%s", strings.Join(left, "\n"))
	}
	want := FoldRows(t, d)
	if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	if diff := DiffFoldTables(want, FoldRows(t, d)); len(diff) > 0 {
		t.Fatalf("the erased history replays into another fold:\n%s", strings.Join(diff, "\n"))
	}
}

// oldPurgeDataset is a shapes repository whose permanent delete keeps the
// record's history, as a release before the erasure did.
func oldPurgeDataset(t *testing.T) *dataset {
	d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithTestPurgeKeepsHistory())
	installShapes(t, d)
	return d
}

// A record purged before this release and written again, forgotten: the
// purge removed nothing a replay could bring back, so the forget erases
// across it.
func TestForgettingARecordAcrossAnEarlierPurge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := oldPurgeDataset(t)
	id := fid("purged-once")
	shapePut(t, d, substrate.PutInput{Kind: shapesThing, ID: id, Properties: map[string]any{"name": "erasable-before-the-purge"}})
	if _, err := d.Delete(ctx, substrate.ActorPerson, shapesThing, id, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	thing := shapePut(t, d, substrate.PutInput{Kind: shapesThing, ID: id, Properties: map[string]any{"name": "the thing now"}})
	forgetHeldToTheFold(t, d, shapeRef(thing), "erasable-before-the-purge")
}

// A record purged before this release, written again, whose purge took a
// former id a merge had given it: a replay without that purge would bring
// the trail back, so the forget is refused, saying so, and leaves nothing.
func TestAForgetAcrossAPurgeThatTookAFormerIDIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := oldPurgeDataset(t)
	id := fid("merged-then-purged")
	winner := shapePut(t, d, substrate.PutInput{Kind: shapesThing, ID: id, Properties: map[string]any{"name": "erasable-merged-then-purged"}})
	loser := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "the loser"}})
	if _, err := d.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: shapesThing, Winner: winner.ID, Loser: loser.ID}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := d.Delete(ctx, substrate.ActorPerson, shapesThing, id, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	thing := shapePut(t, d, substrate.PutInput{Kind: shapesThing, ID: id, Properties: map[string]any{"name": "the thing now"}})
	head := historyHead(t, d)
	_, err := d.Forget(ctx, substrate.ActorPerson, thing.Kind, thing.ID, substrate.ForgetInput{})
	if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), "1401") {
		t.Fatalf("the forget answered %v, want a conflict naming the earlier deletion (#1401)", err)
	}
	if again := historyHead(t, d); again != head {
		t.Fatalf("the refused forget moved the history from %v to %v", head, again)
	}
	var pending int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures`).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("%d erasures pending after the refusal (%v), want none", pending, err)
	}
}

// refsIn runs a query that returns kind and id pairs on the repository's
// own handle.
func refsIn(t *testing.T, d *dataset, query string, args ...any) []eref {
	t.Helper()
	rows, err := d.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	var out []eref
	for rows.Next() {
		var r eref
		if err := rows.Scan(&r.Kind, &r.ID); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	return out
}
