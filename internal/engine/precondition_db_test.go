package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func int64p(n int64) *int64 { return &n }

// A delete under IfVersion is the same compare-and-set a put or a patch is: a
// record edited after the caller read it stays live, and the refusal names
// conflict.
func TestDeleteRefusesAStaleVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	rec := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "Ada"}})
	read := rec.Version
	// The concurrent edit the caller never saw.
	mustPatch(t, ds, owner, rec.Kind, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": "Ada Lovelace"}})

	_, err := ds.Delete(ctx, owner, rec.Kind, rec.ID, substrate.DeleteInput{IfVersion: int64p(read)})
	wantErr(t, err, substrate.ErrConflict, "delete under a stale version")
	live := mustGet(t, ds, rec.Kind, rec.ID)
	if live.DeletedAt != nil {
		t.Fatal("a refused delete tombstoned the record")
	}
	if live.Properties["name"] != "Ada Lovelace" {
		t.Fatalf("a refused delete touched the record: %v", live.Properties)
	}

	del, err := ds.Delete(ctx, owner, rec.Kind, rec.ID, substrate.DeleteInput{IfVersion: int64p(live.Version)})
	if err != nil {
		t.Fatalf("delete under the current version: %v", err)
	}
	if del.DeletedAt == nil {
		t.Fatal("the conditioned delete did not tombstone")
	}
	// The tombstone moved the version, so the same precondition replayed is a
	// conflict, the way a replayed put under IfVersion is, not a silent no-op.
	_, err = ds.Delete(ctx, owner, rec.Kind, rec.ID, substrate.DeleteInput{IfVersion: int64p(live.Version)})
	wantErr(t, err, substrate.ErrConflict, "replaying the conditioned delete")

	// Unconditioned, a delete of a tombstone is still the idempotent no-op.
	other := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "Grace"}})
	mustPatch(t, ds, owner, other.Kind, other.ID, substrate.PatchInput{Properties: map[string]any{"name": "Grace Hopper"}})
	if _, err := ds.Delete(ctx, owner, other.Kind, other.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("unconditioned delete: %v", err)
	}
	if _, err := ds.Delete(ctx, owner, other.Kind, other.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("unconditioned delete of a tombstone: %v", err)
	}
}

// A former id resolves to its canonical record before the version is read, so
// the precondition compares the winner's version, which is the row the
// tombstone lands on. The loser's own version means nothing here.
func TestDeleteThroughAFormerIDComparesTheCanonicalRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "A"}})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "B"}})
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: winner.Kind, Winner: winner.ID, Loser: loser.ID}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// An edit on the winner after the merge, so the pair sit at different
	// versions and the test can tell which one the delete compared.
	mustPatch(t, ds, owner, winner.Kind, winner.ID, substrate.PatchInput{Properties: map[string]any{"name": "A edited"}})
	canonical := mustGet(t, ds, winner.Kind, winner.ID)
	deleted := true
	tombstone, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{winner.Kind}, IDs: []string{loser.ID}, Deleted: &deleted,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tombstone.Records) != 1 {
		t.Fatalf("the loser's tombstone is not listed: %d rows", len(tombstone.Records))
	}
	if tombstone.Records[0].Version == canonical.Version {
		t.Fatalf("the test needs the pair at different versions, both are %d", canonical.Version)
	}

	_, err = ds.Delete(ctx, owner, loser.Kind, loser.ID, substrate.DeleteInput{IfVersion: int64p(tombstone.Records[0].Version)})
	wantErr(t, err, substrate.ErrConflict, "delete through a former id under the loser's own version")
	if mustGet(t, ds, winner.Kind, winner.ID).DeletedAt != nil {
		t.Fatal("a refused delete tombstoned the canonical record")
	}

	del, err := ds.Delete(ctx, owner, loser.Kind, loser.ID, substrate.DeleteInput{IfVersion: int64p(canonical.Version)})
	if err != nil {
		t.Fatalf("delete through a former id under the canonical version: %v", err)
	}
	if del.ID != winner.ID || del.DeletedAt == nil {
		t.Fatalf("delete resolved to %s (deleted %v), want the tombstoned canonical %s", del.ID, del.DeletedAt, winner.ID)
	}
}

// A merge under WinnerVersion or LoserVersion refuses when that participant
// moved since the caller read it, and a refusal moves nothing: both records
// stay live, no recordmerge record is written. Either precondition alone is
// enough, and an unconditioned merge is unchanged.
func TestMergeRefusesAStaleParticipantVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	mk := func(name string) *substrate.Record {
		return mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": name}})
	}
	countMerges := func() int {
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/recordmerge"}}})
		if err != nil {
			t.Fatal(err)
		}
		return len(page.Records)
	}
	assertUnmerged := func(what string, a, b *substrate.Record) {
		t.Helper()
		for _, r := range []*substrate.Record{a, b} {
			got := mustGet(t, ds, r.Kind, r.ID)
			if got.DeletedAt != nil || got.ID != r.ID {
				t.Fatalf("%s: %s is %s (deleted %v); a refused merge must move nothing", what, r.ID, got.ID, got.DeletedAt)
			}
		}
	}

	w, l := mk("W"), mk("L")
	wRead, lRead := w.Version, l.Version
	mustPatch(t, ds, owner, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "W edited"}})
	mustPatch(t, ds, owner, l.Kind, l.ID, substrate.PatchInput{Properties: map[string]any{"name": "L edited"}})

	_, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: w.Kind, Winner: w.ID, Loser: l.ID, WinnerVersion: int64p(wRead)})
	wantErr(t, err, substrate.ErrConflict, "merge under a stale winner version")
	assertUnmerged("stale winner", w, l)
	_, err = ds.Merge(ctx, owner, substrate.MergeInput{Kind: w.Kind, Winner: w.ID, Loser: l.ID, LoserVersion: int64p(lRead)})
	wantErr(t, err, substrate.ErrConflict, "merge under a stale loser version")
	assertUnmerged("stale loser", w, l)
	// Current winner, stale loser: the one stale participant is enough.
	wNow := mustGet(t, ds, w.Kind, w.ID).Version
	_, err = ds.Merge(ctx, owner, substrate.MergeInput{
		Kind: w.Kind, Winner: w.ID, Loser: l.ID, WinnerVersion: int64p(wNow), LoserVersion: int64p(lRead),
	})
	wantErr(t, err, substrate.ErrConflict, "merge under a current winner and a stale loser")
	assertUnmerged("one stale", w, l)
	if n := countMerges(); n != 0 {
		t.Fatalf("%d recordmerge records after refused merges, want 0", n)
	}

	lNow := mustGet(t, ds, l.Kind, l.ID).Version
	rec, err := ds.Merge(ctx, owner, substrate.MergeInput{
		Kind: w.Kind, Winner: w.ID, Loser: l.ID, WinnerVersion: int64p(wNow), LoserVersion: int64p(lNow),
	})
	if err != nil {
		t.Fatalf("merge under both current versions: %v", err)
	}
	if got := mustGet(t, ds, l.Kind, l.ID); got.ID != w.ID {
		t.Fatalf("the conditioned merge did not merge: %s resolves to %s", l.ID, got.ID)
	}
	// The merge moved both versions, so the same preconditions replayed are a
	// conflict rather than the unconditioned replay's verified no-op.
	_, err = ds.Merge(ctx, owner, substrate.MergeInput{
		Kind: w.Kind, Winner: w.ID, Loser: l.ID, WinnerVersion: int64p(wNow), LoserVersion: int64p(lNow),
	})
	wantErr(t, err, substrate.ErrConflict, "replaying the conditioned merge")
	again, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: w.Kind, Winner: w.ID, Loser: l.ID})
	if err != nil {
		t.Fatalf("unconditioned replay: %v", err)
	}
	if again.ID != rec.ID {
		t.Fatalf("unconditioned replay minted %s, want the existing %s", again.ID, rec.ID)
	}

	// One participant's version alone, and current, merges.
	a, b := mk("A"), mk("B")
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: a.Kind, Winner: a.ID, Loser: b.ID, LoserVersion: int64p(b.Version)}); err != nil {
		t.Fatalf("merge under the loser's version alone: %v", err)
	}
	c, d := mk("C"), mk("D")
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: c.Kind, Winner: c.ID, Loser: d.ID}); err != nil {
		t.Fatalf("unconditioned merge: %v", err)
	}
}

// A split under IfVersion holds the recordmerge record, not the pair: the pair
// change with every edit after the merge and a split keeps those edits, so a
// stale merge-record version is the one thing that refuses. A refusal leaves
// the loser merged away.
func TestSplitRefusesAStaleMergeRecordVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	w := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "W"}})
	l := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "L"}})
	rec, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: w.Kind, Winner: w.ID, Loser: l.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	// Edits to the pair after the merge do not condition the split.
	mustPatch(t, ds, owner, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "W edited"}})

	_, err = ds.Split(ctx, owner, substrate.SplitInput{Merge: rec.ID, IfVersion: int64p(rec.Version + 1)})
	wantErr(t, err, substrate.ErrConflict, "split under a stale merge-record version")
	if got := mustGet(t, ds, l.Kind, l.ID); got.ID != w.ID {
		t.Fatalf("a refused split restored the loser: %s resolves to %s", l.ID, got.ID)
	}
	if got := mustGet(t, ds, rec.Kind, rec.ID); got.DeletedAt != nil {
		t.Fatal("a refused split tombstoned the merge record")
	}

	split, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: rec.ID, IfVersion: int64p(rec.Version)})
	if err != nil {
		t.Fatalf("split under the current merge-record version: %v", err)
	}
	if split.Kind != "substrate.reamde.dev/core/recordsplit" {
		t.Fatalf("split returned a %s", split.Kind)
	}
	back := mustGet(t, ds, l.Kind, l.ID)
	if back.ID != l.ID || back.DeletedAt != nil {
		t.Fatalf("the conditioned split did not restore the loser: %s (deleted %v)", back.ID, back.DeletedAt)
	}
	// The split tombstoned the merge record and moved its version: the same
	// precondition replayed is a conflict, where the unconditioned replay is the
	// verified no-op returning the split record.
	_, err = ds.Split(ctx, owner, substrate.SplitInput{Merge: rec.ID, IfVersion: int64p(rec.Version)})
	wantErr(t, err, substrate.ErrConflict, "replaying the conditioned split")
	again, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: rec.ID})
	if err != nil {
		t.Fatalf("unconditioned replay: %v", err)
	}
	if again.ID != split.ID {
		t.Fatalf("unconditioned replay minted %s, want the existing %s", again.ID, split.ID)
	}
}

// A declaration record's delete runs through the vocabulary batch, not
// softDelete, and it honors the same precondition there (checkSchemaCAS): a
// stale version refuses and the kind stays installed, the current one deletes.
func TestDeleteOfADeclarationHonorsIfVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)

	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("widget", "widgets", map[string]any{"name": map[string]any{"type": "string"}}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	const kindKind = "substrate.reamde.dev/core/kind"
	row := mustGet(t, ds, kindKind, swPackage+"/widget")

	_, err := ds.Delete(ctx, owner, kindKind, row.ID, substrate.DeleteInput{IfVersion: int64p(row.Version + 1)})
	wantErr(t, err, substrate.ErrConflict, "delete of a declaration under a stale version")
	if _, err := ds.KindByRef(ctx, swPackage+"/widget"); err != nil {
		t.Fatalf("a refused delete uninstalled the kind: %v", err)
	}

	gone, err := ds.Delete(ctx, owner, kindKind, row.ID, substrate.DeleteInput{IfVersion: int64p(row.Version)})
	if err != nil {
		t.Fatalf("delete of a declaration under its current version: %v", err)
	}
	if gone.DeletedAt == nil {
		t.Fatalf("declaration row not tombstoned: %+v", gone)
	}
	if _, err := ds.KindByRef(ctx, swPackage+"/widget"); err == nil {
		t.Fatal("deleted kind still resolves")
	}
}
