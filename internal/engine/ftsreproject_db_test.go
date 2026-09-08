package engine_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The search index is derived from the folded row AND the kind's declaration,
// and a rebuild indexes every row under the declarations it ends with. These
// tests hold the live index to the same rule: a kind edit that changes what
// its records index re-indexes them in the apply (fold.go reprojectFTS), so a
// search answers the same before and after a rebuild, and the rows' own
// values, version and timestamps do not move for it.

const (
	ftsPackage = "ftsindex.example.substrate.reamde.dev/notes"
	ftsNote    = ftsPackage + "/note"
)

// declareNotes applies the notes package with `remark` indexed or opted out
// of search; every other part of the declaration is the same on both sides.
func declareNotes(t *testing.T, ds substrate.Dataset, remarkIndexed bool) {
	t.Helper()
	remark := map[string]any{"type": "text"}
	if !remarkIndexed {
		remark["fts"] = false
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabulary.PackageManifest(ftsPackage, 0),
		vocabulary.KindManifest(ftsPackage,
			map[string]any{"singular": "note", "plural": "notes"},
			map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"remark": remark,
			}}),
	}); err != nil {
		t.Fatalf("declare the notes package (remark indexed=%v): %v", remarkIndexed, err)
	}
}

// lexicalHits runs one lexical search and renders its hits in rank order, id
// and score together, so two runs compare as one value.
func lexicalHits(t *testing.T, ds substrate.Dataset, q string) []string {
	t.Helper()
	hits, err := searchHits(ds.Search(context.Background(), substrate.SearchInput{Q: q, Mode: substrate.SearchLexical}))
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	var out []string
	for _, h := range hits {
		out = append(out, fmt.Sprintf("%s@%.6f", h.Record.ID, h.Lexical))
	}
	return out
}

func idsOfHits(hits []string) []string {
	var out []string
	for _, h := range hits {
		for i := range h {
			if h[i] == '@' {
				out = append(out, h[:i])
				break
			}
		}
	}
	return out
}

// TestAKindEditReindexesItsRowsAndTheRebuildAgrees: flipping a property's
// `fts` flag changes what the kind's existing records index, live, in the
// apply that flips it, and a rebuild lands on the same index. The records
// themselves did not change, so their version and updated_at stand and the
// changelog carries nothing about them.
func TestAKindEditReindexesItsRowsAndTheRebuildAgrees(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	declareNotes(t, ds, false)

	titled := mustPut(t, ds, owner, substrate.PutInput{
		Kind: ftsNote, ID: "titled",
		Properties: map[string]any{"name": "quokka in the title", "remark": "nothing to see"},
	})
	remarked := mustPut(t, ds, owner, substrate.PutInput{
		Kind: ftsNote, ID: "remarked",
		Properties: map[string]any{"name": "plain", "remark": "a quokka in the remark"},
	})
	// A tombstone is indexed too, and search never sees it: the rebuild must
	// still agree on its bands.
	gone := mustPut(t, ds, owner, substrate.PutInput{
		Kind: ftsNote, ID: "gone",
		Properties: map[string]any{"name": "deleted", "remark": "a quokka nobody finds"},
	})
	if _, err := ds.Delete(ctx, owner, gone.Kind, gone.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := idsOfHits(lexicalHits(t, ds, "quokka")); !reflect.DeepEqual(got, []string{"titled"}) {
		t.Fatalf("with remark opted out, search finds %v, want [titled]", got)
	}

	rb := svc.(rebuilder)
	rebuildAgrees := func(when string, wantIDs []string) {
		t.Helper()
		live := lexicalHits(t, ds, "quokka")
		if got := idsOfHits(live); !reflect.DeepEqual(got, wantIDs) {
			t.Fatalf("%s: search finds %v, want %v", when, got, wantIDs)
		}
		before := foldOf(t, ds)
		if _, err := rb.RebuildRepository(ctx, testdb.Username(t)); err != nil {
			t.Fatalf("%s: rebuild: %v", when, err)
		}
		if after := foldOf(t, ds); string(before) != string(after) {
			t.Fatalf("%s: the rebuilt fold is not the fold\n%s", when, firstDifference(before, after))
		}
		if rebuilt := lexicalHits(t, ds, "quokka"); !reflect.DeepEqual(rebuilt, live) {
			t.Fatalf("%s: the rebuilt repository searches differently: live %v, rebuilt %v", when, live, rebuilt)
		}
	}

	// Index `remark`: the word in it becomes a hit without any record moving.
	head := maxSeq(t, ds)
	declareNotes(t, ds, true)
	changes, err := ds.Changes(ctx, head, substrate.ChangeFilter{Kinds: []string{ftsNote}}, 100)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("the kind edit appended %d entries about its records, want none: %+v", len(changes), changes)
	}
	for _, want := range []*substrate.Record{titled, remarked} {
		got := mustGet(t, ds, want.Kind, want.ID)
		if got.Version != want.Version || !got.UpdatedAt.Equal(want.UpdatedAt) {
			t.Fatalf("the kind edit moved %s to version %d at %s; it was version %d at %s",
				want.ID, got.Version, got.UpdatedAt, want.Version, want.UpdatedAt)
		}
	}
	// The title (band A) outranks the remark (band C), so the order is fixed.
	rebuildAgrees("after indexing remark", []string{"titled", "remarked"})

	// And back: the word drops out of the index again, live and rebuilt alike.
	declareNotes(t, ds, false)
	rebuildAgrees("after opting remark out again", []string{"titled"})
}

// TestARebuildAgreesAfterAnUninstallLeavesTombstones: a package uninstall
// drops its kinds, and the tombstoned rows of a dropped kind index under the
// unknown-kind bands from then on (fold.go foldFTS), because that is what a
// replay without the declaration computes for them. The uninstall re-indexes
// them so the fold before and after a rebuild is the same document.
func TestARebuildAgreesAfterAnUninstallLeavesTombstones(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	inst, ok := ds.(substrate.BundleInstaller)
	if !ok {
		t.Fatal("dataset does not implement the closure-install seam")
	}
	const pkg = "gizmos.example.substrate.reamde.dev/gizmo"
	const gizmo = pkg + "/gizmo"
	closure := []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
		vocabulary.BundleManifest(pkg, map[string]any{
			"description": "a bundle to uninstall", "installs": []any{gizmo},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "gizmo"},
			map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"notes": map[string]any{"type": "text"},
			}}),
	}
	if _, err := inst.InstallBundleClosure(ctx, substrate.BundleActor(vocabulary.SplitPackageRef(pkg)), closure, nil,
		substrate.BundleInstall{}); err != nil {
		t.Fatalf("install the closure: %v", err)
	}
	g := mustPut(t, ds, owner, substrate.PutInput{
		Kind: gizmo, Properties: map[string]any{"name": "Widget", "notes": "kept in the fold as a tombstone"},
	})
	if _, err := ds.Delete(ctx, owner, g.Kind, g.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := bundler(t, ds).UninstallBundle(ctx, pkg); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	before := foldOf(t, ds)
	if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Username(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); string(before) != string(after) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, after))
	}
}
