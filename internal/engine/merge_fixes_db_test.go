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

	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/recordmerge"}}})
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

	_, err := ds.Merge(ctx, owner, "core.substrate.reamde.dev/kind", "people.substrate.reamde.dev/person", "people.substrate.reamde.dev/organization")
	wantErr(t, err, substrate.ErrForbidden, "merging two type projections")
	if ty := mustGet(t, ds, "core.substrate.reamde.dev/kind", "people.substrate.reamde.dev/organization"); ty.DeletedAt != nil {
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

// A collision on (rel, src, dst) keeps the winner's props; the loser's own
// travel in the merge record so split gives them back.
func TestMergeSplitRestoresLoserEdgeProps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	team := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "organization", Properties: map[string]any{"name": "Platform"},
	})
	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Nina Ray"},
		Edges: []substrate.EdgeInput{{
			Rel: "memberOf", To: substrate.EdgeRef{ID: team.ID},
			Properties: map[string]any{"role": "guest"},
		}},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "N. Ray"},
		Edges: []substrate.EdgeInput{{
			Rel: "memberOf", To: substrate.EdgeRef{ID: team.ID},
			Properties: map[string]any{"role": "admin", "since": "2019"},
		}},
	})
	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if p := edgePropsOf(t, ds, winner.Kind, winner.ID, "memberOf", team.ID); p["role"] != "guest" {
		t.Fatalf("merge overwrote the winner's edge props: %v", p)
	}
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	back := edgePropsOf(t, ds, loser.Kind, loser.ID, "memberOf", team.ID)
	if back == nil {
		t.Fatal("the loser's edge is gone")
	}
	if back["role"] != "admin" || back["since"] != "2019" {
		t.Fatalf("the loser's edge props came back as %v", back)
	}
	if p := edgePropsOf(t, ds, winner.Kind, winner.ID, "memberOf", team.ID); p["role"] != "guest" {
		t.Fatalf("the winner's edge props changed: %v", p)
	}
}

// An edge between the winner and the loser has nowhere to move to; it is
// recorded with its props so split can put it back.
func TestMergeSplitRestoresPairInternalEdge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	const authority = "peers.connectors.substrate.reamde.dev"
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, enginetest.Manifest{
		Name: "peers", Authority: authority,
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest(authority, 1),
			vocabulary.ActorManifest(authority, "connector:peers"),
			vocabulary.KindManifest(authority,
				map[string]any{"singular": "node", "plural": "nodes"},
				map[string]any{
					"displayTemplate": "{label}",
					"properties":      map[string]any{"label": map[string]any{"type": "string"}},
					"edges":           map[string]any{"peer": map[string]any{"to": "node"}},
				}),
		},
	}); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	const nodeType = "peers.connectors.substrate.reamde.dev/node"
	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: nodeType, Properties: map[string]any{"label": "w"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: nodeType, Properties: map[string]any{"label": "l"},
		Edges: []substrate.EdgeInput{{
			Rel: "peer", To: substrate.EdgeRef{ID: winner.ID},
			Properties: map[string]any{"src": "beeper"},
		}},
	})
	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	back := edgePropsOf(t, ds, loser.Kind, loser.ID, "peer", winner.ID)
	if back == nil {
		t.Fatal("the winner<->loser edge is gone after merge+split")
	}
	if back["src"] != "beeper" {
		t.Fatalf("the winner<->loser edge props came back as %v", back)
	}
}

func edgePropsOf(t *testing.T, ds substrate.Dataset, typ, id, rel, other string) map[string]any {
	t.Helper()
	e := mustGet(t, ds, typ, id)
	for _, tgt := range e.Edges[rel] {
		if tgt.ID == other {
			if tgt.Properties == nil {
				return map[string]any{}
			}
			return tgt.Properties
		}
	}
	return nil
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

// A merge performed by the DEPLOYED build spells its moved set `bindings` and
// names the source `rep`. Splitting one has to re-point those edges: skipping
// them would leave the graph pointing at a resurrected loser's winner, which
// is corruption rather than a clean failure.
func TestSplitReadsTheLegacyMovedSet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, raw, _ := newDatasetWithDB(t)
	installPeopleSources(t, ds)

	g := syncSource(t, ds, people, typeGoogleContact, "g-c1", map[string]any{"name": aname("Alex")})
	s := syncSource(t, ds, slack, typeSlackUser, "s-U1", map[string]any{"realName": "alex"})
	winner, loser := personOf(t, ds, g), personOf(t, ds, s)

	rec, err := ds.Merge(ctx, owner, typePerson, winner, loser)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	// Rewrite the record into the shape the deployed build wrote.
	if _, err := raw.ExecContext(ctx, `
		UPDATE records
		SET props = jsonb_set(
			props #- '{moved,subjects}',
			'{moved,bindings}',
			(SELECT jsonb_agg(jsonb_build_object('rel', e->>'rel', 'rep', e->>'source', 'kind', e->>'kind'))
			 FROM jsonb_array_elements(props->'moved'->'subjects') AS e))
		WHERE id = $1`, rec.ID); err != nil {
		t.Fatalf("rewrite the merge record: %v", err)
	}

	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	if got := personOf(t, ds, s); got != loser {
		t.Fatalf("the legacy moved set was skipped: the slack record points at %s, want %s", got, loser)
	}
	if got := personOf(t, ds, g); got != winner {
		t.Fatalf("the winner's own source moved: %s", got)
	}
}
