package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestMergeRejectsTombstonedAndAlreadyMerged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	mk := func(name string) *substrate.Record {
		return mustPut(t, ds, owner, substrate.PutInput{
			Kind: "person", Properties: map[string]any{"name": name},
		})
	}
	a, b, c, d := mk("A"), mk("B"), mk("C"), mk("D")

	if _, err := ds.Delete(ctx, owner, b.Kind, b.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := ds.Merge(ctx, owner, a.Kind, a.ID, b.ID)
	wantErr(t, err, substrate.ErrConflict, "merging a tombstoned loser")
	_, err = ds.Merge(ctx, owner, b.Kind, b.ID, a.ID)
	wantErr(t, err, substrate.ErrConflict, "merging into a tombstoned winner")

	rec, err := ds.Merge(ctx, owner, a.Kind, a.ID, c.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	// Re-merging the same loser into the same winner is a VERIFIED no-op
	// returning the existing record (replay idempotence) — while merging the
	// loser into a THIRD record is still a conflict.
	again, err := ds.Merge(ctx, owner, a.Kind, a.ID, c.ID)
	if err != nil {
		t.Fatalf("re-merging the same loser: %v", err)
	}
	if again.ID != rec.ID {
		t.Fatalf("re-merge minted a new record: %s, want %s", again.ID, rec.ID)
	}
	_, err = ds.Merge(ctx, owner, d.Kind, d.ID, c.ID)
	wantErr(t, err, substrate.ErrConflict, "merging a loser into a third record")

	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/recordmerge"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 {
		t.Fatalf("expected one merge record, got %d", len(page.Records))
	}
	// The one merge stays reversible, identifiers and all.
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	if back := mustGet(t, ds, c.Kind, c.ID); back.DeletedAt != nil {
		t.Fatal("split did not restore the loser")
	}
}

// Split reverts the merge, not the months of curation after it.
func TestSplitKeepsPostMergeOwnerWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Nina Ray"},
		Annotations: map[string]any{"owner/note": "winner original"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "N. Ray"},
		Labels:      map[string]any{"owner/shelf": "audio", "owner/format": "mp3"},
		Annotations: map[string]any{"owner/note": "loser newer", "owner/extra": 7},
	})
	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	// The owner curates the merged record afterwards.
	mustPatch(t, ds, owner, winner.Kind, winner.ID, substrate.PatchInput{
		Labels: map[string]any{"owner/shelf": "curated shelf"},
		Annotations: map[string]any{
			"owner/note": "curated after the merge", "owner/extra": 9,
		},
	})

	split, err := ds.Split(ctx, owner, rec.ID)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	w := mustGet(t, ds, winner.Kind, winner.ID)
	if w.Labels["owner/shelf"] != "curated shelf" {
		t.Fatalf("split clobbered a post-merge label: %v", w.Labels)
	}
	if w.Annotations["owner/note"] != "curated after the merge" {
		t.Fatalf("split clobbered a post-merge annotation: %v", w.Annotations)
	}
	if w.Annotations["owner/extra"] != float64(9) {
		t.Fatalf("split clobbered a post-merge annotation: %v", w.Annotations)
	}
	// Untouched keys still revert.
	if _, ok := w.Labels["owner/format"]; ok {
		t.Fatalf("split kept an untouched moved label: %v", w.Labels)
	}
	l := mustGet(t, ds, loser.Kind, loser.ID)
	if l.Annotations["owner/note"] != "loser newer" || l.Annotations["owner/extra"] != float64(7) {
		t.Fatalf("loser annotations not restored: %v", l.Annotations)
	}

	result, _ := split.Properties["result"].(map[string]any)
	skipped, _ := result["skipped"].(map[string]any)
	if skipped == nil {
		t.Fatalf("split record does not record what it skipped: %v", split.Properties)
	}
	if got := stringSet(skipped["labels"]); !got["owner/shelf"] {
		t.Fatalf("skipped labels = %v", skipped["labels"])
	}
	if got := stringSet(skipped["annotations"]); !got["owner/note"] || !got["owner/extra"] {
		t.Fatalf("skipped annotations = %v", skipped["annotations"])
	}
}

// The substrate's own state never merges through the generic surface.
func TestMergeRejectsSystemTypes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	_, err := ds.Merge(ctx, owner, "substrate.reamde.dev/core/kind", "samples.substrate.reamde.dev/people/person", "samples.substrate.reamde.dev/people/organization")
	wantErr(t, err, substrate.ErrForbidden, "merging two type projections")
	if ty := mustGet(t, ds, "substrate.reamde.dev/core/kind", "samples.substrate.reamde.dev/people/organization"); ty.DeletedAt != nil {
		t.Fatal("a type projection was tombstoned by merge")
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{Kind: "organization", Properties: map[string]any{"name": "still works"}}); err != nil {
		t.Fatalf("the organization type stopped working: %v", err)
	}

	// A merge across two TYPES is inexpressible under (type, id) identity:
	// the verb addresses ONE type, so the organization's id simply does not
	// exist among people — the refusal is a not-found, and nothing merges.
	x := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "X"}})
	org := mustPut(t, ds, owner, substrate.PutInput{Kind: "organization", Properties: map[string]any{"name": "O"}})
	_, err = ds.Merge(ctx, owner, x.Kind, x.ID, org.ID)
	wantErr(t, err, substrate.ErrNotFound, "merging across types")
	if o := mustGet(t, ds, org.Kind, org.ID); o.DeletedAt != nil {
		t.Fatal("the organization was touched by a refused cross-type merge")
	}

	// Control: two records of one type still merge manually.
	y := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "Y"}})
	if _, err := ds.Merge(ctx, owner, x.Kind, x.ID, y.ID); err != nil {
		t.Fatalf("manual merge of one type: %v", err)
	}
}

// Merge moves no link data. The winner keeps its own, the loser keeps its own
// on the tombstone, and a split hands the loser back with it intact.
func TestMergeSplitLeavesLinkDataWhereItIs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	team := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "organization", Properties: map[string]any{"name": "Platform"},
	})
	teamRef := vocabulary.RecordPath(team.Kind, team.ID)
	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{
			"name": "Nina Ray",
			"memberOf": []any{map[string]any{
				vocabulary.ReferenceValueKey: teamRef, "role": "guest",
			}},
		},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{
			"name": "N. Ray",
			"memberOf": []any{map[string]any{
				vocabulary.ReferenceValueKey: teamRef, "role": "admin", "since": "2019-04-01",
			}},
		},
	})
	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if p := linkDataOf(t, ds, winner.Kind, winner.ID, "memberOf", team.ID); p["role"] != "guest" {
		t.Fatalf("merge rewrote the winner's link data: %v", p)
	}
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	back := linkDataOf(t, ds, loser.Kind, loser.ID, "memberOf", team.ID)
	if back == nil {
		t.Fatal("the loser's pointer is gone")
	}
	if back["role"] != "admin" || back["since"] != "2019-04-01" {
		t.Fatalf("the loser's link data came back as %v", back)
	}
	if p := linkDataOf(t, ds, winner.Kind, winner.ID, "memberOf", team.ID); p["role"] != "guest" {
		t.Fatalf("the winner's link data changed: %v", p)
	}
}

// A reference from the loser to the winner survives the pair being merged and
// split: it is a value in the loser's own properties, which the merge never
// reaches into and the split restores whole.
func TestMergeSplitKeepsThePairInternalReference(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	const pkg = "peers.connectors.substrate.reamde.dev/peers"
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, enginetest.Manifest{
		Name: "peers", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 1),
			vocabulary.ActorManifest(pkg, "connector:peers"),
			vocabulary.KindManifest(pkg,
				map[string]any{"singular": "node", "plural": "nodes"},
				map[string]any{
					"displayTemplate": "{label}",
					"properties": map[string]any{
						"label": map[string]any{"type": "string"},
						"peer": map[string]any{
							"type": "reference",
							"kind": "node",
							// Declared, because an undeclared link property is
							// refused: the link data this test carries through
							// merge and split has to be data a write would accept.
							"properties": map[string]any{
								"src": map[string]any{"type": "string"},
							},
						},
					},
				}),
		},
	}); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	const nodeType = "peers.connectors.substrate.reamde.dev/peers/node"
	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: nodeType, Properties: map[string]any{"label": "w"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: nodeType, Properties: map[string]any{
			"label": "l",
			"peer": map[string]any{
				vocabulary.ReferenceValueKey: winner.ID, "src": "beeper",
			},
		},
	})
	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	// `peer` is single-valued, so its whole value is the object: the pointer
	// under `ref` and the link data beside it.
	back, _ := mustGet(t, ds, loser.Kind, loser.ID).Properties["peer"].(map[string]any)
	if back[vocabulary.ReferenceValueKey] != vocabulary.RecordPath(nodeType, winner.ID) {
		t.Fatalf("the loser->winner pointer is gone after merge+split: %v", back)
	}
	if back["src"] != "beeper" {
		t.Fatalf("the loser->winner link data came back as %v", back)
	}
}

func stringSet(v any) map[string]bool {
	out := map[string]bool{}
	items, ok := v.([]any)
	if !ok {
		return out
	}
	for _, item := range items {
		if s, ok := item.(string); ok {
			out[s] = true
		}
	}
	return out
}
