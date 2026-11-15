package engine

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A record merged twice keeps a flat trail through a rebuild. Merging A into
// B and then B into C re-points A's trail row at C with an UPDATE no effect
// records (rows.go moveFormerIDs), but the second merge's line restates the
// trail rows of both records in its resync effect (fold.go resyncOf), so a
// replay lands A→C and B→C, never the chain A→B→C. Every reverse read follows
// that trail one hop (idsOf), so on the rebuilt fold what was written at A
// still answers for C, and a permanent delete of C takes what A and B wrote
// (#821, #820).
func TestARecordMergedTwiceRebuildsAFlatTrail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	const pkg = "trail.example.substrate.reamde.dev/trail"
	box, note := pkg+"/box", pkg+"/note"
	if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "box"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"},
			map[string]any{"properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"about": map[string]any{"type": "reference", "kind": box, "onDelete": "cascade"},
			}}),
	}); err != nil {
		t.Fatal(err)
	}
	put := func(in substrate.PutInput) *substrate.Record {
		t.Helper()
		rec, err := d.Put(ctx, substrate.ActorPerson, in)
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	// A and B are each written twice, so each owns more than one line.
	twice := func(first, second string) *substrate.Record {
		t.Helper()
		rec := put(substrate.PutInput{Kind: box, Properties: map[string]any{"name": first}})
		if _, err := d.Patch(ctx, substrate.ActorPerson, box, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": second}}); err != nil {
			t.Fatal(err)
		}
		return rec
	}
	erasable := []string{"erasable-alpha-one", "erasable-alpha-two", "erasable-beta-one", "erasable-beta-two"}
	a := twice(erasable[0], erasable[1])
	b := twice(erasable[2], erasable[3])
	c := put(substrate.PutInput{Kind: box, Properties: map[string]any{"name": "the keeper"}})
	// Written at A before either merge; a merge repoints nothing, so the value
	// names A to the end.
	pointer := put(substrate.PutInput{Kind: note, Properties: map[string]any{
		"name": "a pointer at the oldest", "about": map[string]any{"ref": vocabulary.RecordPath(box, a.ID)},
	}})
	mergeInto(t, d, b, a)
	mergeInto(t, d, c, b)

	flat := []string{a.ID + "→" + c.ID, b.ID + "→" + c.ID}
	slices.Sort(flat)
	trail := func(when string) {
		t.Helper()
		rows, err := d.db.QueryContext(ctx, `SELECT former_id || '→' || record_id FROM former_ids WHERE record_kind = $1 ORDER BY 1`, box)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var got []string
		for rows.Next() {
			var hop string
			if err := rows.Scan(&hop); err != nil {
				t.Fatal(err)
			}
			got = append(got, hop)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, flat) {
			t.Fatalf("%s, the trail is %v, want %v", when, got, flat)
		}
	}
	trail("live")
	before := FoldRows(t, d)
	if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if diff := DiffFoldTables(before, FoldRows(t, d)); len(diff) > 0 {
		t.Fatalf("the rebuilt fold is not the fold:\n%s", strings.Join(diff, "\n"))
	}
	trail("rebuilt")

	ids, err := d.idsOf(ctx, d.db, eref{Kind: box, ID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ids, a.ID) || !slices.Contains(ids, b.ID) {
		t.Fatalf("C answers for %v, want A %s and B %s among them", ids, a.ID, b.ID)
	}
	at := vocabulary.RecordPath(box, c.ID)
	for name, f := range map[string]substrate.Filter{
		"filter.referencing":      {Kinds: []string{note}, Referencing: &substrate.Referencing{Ref: at}},
		"filter.properties.about": {Kinds: []string{note}, Properties: map[string]substrate.Cond{"about": {Eq: at}}},
	} {
		page, err := d.List(ctx, substrate.Query{Filter: f})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var got []string
		for _, r := range page.Records {
			got = append(got, r.ID)
		}
		if !slices.Equal(got, []string{pointer.ID}) {
			t.Fatalf("%s at C answered %v, want the note written at A, %s", name, got, pointer.ID)
		}
	}

	// C goes for good with what was merged into it, and the cascade finds the
	// note through the same trail.
	deleted, err := d.Delete(ctx, substrate.ActorPerson, box, c.ID, substrate.DeleteInput{Purge: true})
	if err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	if deleted.Erasure == nil || deleted.Erasure.Pending {
		t.Fatalf("the delete's erasure is %+v, want it done", deleted.Erasure)
	}
	for _, r := range []*substrate.Record{a, b, c} {
		if row, err := d.loadRowDB(ctx, eref{Kind: box, ID: r.ID}); err != nil || row != nil {
			t.Fatalf("the box %s is %+v (%v) after C's permanent delete, want it gone", r.ID, row, err)
		}
	}
	if row, err := d.loadRowDB(ctx, eref{Kind: note, ID: pointer.ID}); err != nil || row == nil || row.DeletedAt == nil {
		t.Fatalf("the note written at A is %+v (%v) after C's permanent delete, want it tombstoned by the cascade", row, err)
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if left := valuesLeft(t, d, erasable...); len(left) > 0 {
		t.Fatalf("values of A or B remain:\n%s", strings.Join(left, "\n"))
	}
}
