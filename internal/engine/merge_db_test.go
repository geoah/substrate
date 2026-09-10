package engine_test

// The merge and split surface: what merge refuses (a tombstoned participant,
// an already-merged loser, a core kind, a cross-kind pair), what it moves (the
// loser's labels and annotations, never its properties or its link data), what
// a split puts back and what it leaves alone (an owner write made after the
// merge), and the merge REQUEST a client files instead of merging directly.

import (
	"context"
	"errors"
	"strings"
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

	if _, err := ds.Delete(ctx, owner, b.Kind, b.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: a.Kind, Winner: a.ID, Loser: b.ID})
	wantErr(t, err, substrate.ErrConflict, "merging a tombstoned loser")
	_, err = ds.Merge(ctx, owner, substrate.MergeInput{Kind: b.Kind, Winner: b.ID, Loser: a.ID})
	wantErr(t, err, substrate.ErrConflict, "merging into a tombstoned winner")

	rec, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: a.Kind, Winner: a.ID, Loser: c.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	// Re-merging the same loser into the same winner is a VERIFIED no-op
	// returning the existing record (replay idempotence) — while merging the
	// loser into a THIRD record is still a conflict.
	again, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: a.Kind, Winner: a.ID, Loser: c.ID})
	if err != nil {
		t.Fatalf("re-merging the same loser: %v", err)
	}
	if again.ID != rec.ID {
		t.Fatalf("re-merge minted a new record: %s, want %s", again.ID, rec.ID)
	}
	_, err = ds.Merge(ctx, owner, substrate.MergeInput{Kind: d.Kind, Winner: d.ID, Loser: c.ID})
	wantErr(t, err, substrate.ErrConflict, "merging a loser into a third record")

	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/recordmerge"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 {
		t.Fatalf("expected one merge record, got %d", len(page.Records))
	}
	// The one merge stays reversible, identifiers and all.
	if _, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: rec.ID}); err != nil {
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
	rec, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: winner.Kind, Winner: winner.ID, Loser: loser.ID})
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

	split, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: rec.ID})
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

	_, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: "substrate.reamde.dev/core/kind", Winner: "samples.substrate.reamde.dev/people/person", Loser: "samples.substrate.reamde.dev/people/organization"})
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
	_, err = ds.Merge(ctx, owner, substrate.MergeInput{Kind: x.Kind, Winner: x.ID, Loser: org.ID})
	wantErr(t, err, substrate.ErrNotFound, "merging across types")
	if o := mustGet(t, ds, org.Kind, org.ID); o.DeletedAt != nil {
		t.Fatal("the organization was touched by a refused cross-type merge")
	}

	// Control: two records of one type still merge manually.
	y := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "Y"}})
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: x.Kind, Winner: x.ID, Loser: y.ID}); err != nil {
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
	rec, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: winner.Kind, Winner: winner.ID, Loser: loser.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if p := linkDataOf(t, ds, winner.Kind, winner.ID, "memberOf", team.ID); p["role"] != "guest" {
		t.Fatalf("merge rewrote the winner's link data: %v", p)
	}
	if _, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: rec.ID}); err != nil {
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
				map[string]any{"singular": "node"},
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
	rec, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: winner.Kind, Winner: winner.ID, Loser: loser.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: rec.ID}); err != nil {
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

// The suggestion flow's engine half (GO.md stage 1): recordmergerequest's
// decision machine, applyMerge in the transition's transaction, and the
// diffConflict rollback+annotation pattern on a stale request.

const requestType = "substrate.reamde.dev/core/recordmergerequest"

// mergeRequest writes one request under a deterministic id — the legality of
// caller-composed ids on this type is itself part of the contract (the pair
// id is the suggestion dedupe and the rejection memory).
func mergeRequest(t *testing.T, ds substrate.Dataset, id, winner, loser string) *substrate.Record {
	t.Helper()
	return mustPut(t, ds, owner, substrate.PutInput{
		Kind: requestType,
		ID:   id,
		Properties: map[string]any{
			"rationale": "they look like one person",
			"evidence":  map[string]any{"signals": []any{map[string]any{"signal": "email"}}},
			"winner":    vocabulary.RecordPath(typePerson, winner),
			"loser":     vocabulary.RecordPath(typePerson, loser),
		},
	})
}

func decide(ds substrate.Dataset, id, decision string) (*substrate.Record, error) {
	return ds.Patch(context.Background(), owner, requestType, id, substrate.PatchInput{
		Properties: map[string]any{"decision": decision},
	})
}

func TestMergeRequestAcceptMerges(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	ctx := context.Background()

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Alexandra Chen"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Alex Chen"},
	})
	req := mergeRequest(t, ds, "dupe-"+winner.ID+"-"+loser.ID, winner.ID, loser.ID)

	accepted, err := decide(ds, req.ID, "accepted")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if accepted.Properties["decision"] != "accepted" {
		t.Fatalf("decision: %v", accepted.Properties["decision"])
	}
	if _, ok := accepted.Properties["decidedAt"]; !ok {
		t.Fatal("decidedAt not stamped")
	}
	// The merge happened in the same transaction: the canonical-id contract
	// holds for the loser's id, and the loser is tombstoned under the merge
	// finalizer.
	got := mustGet(t, ds, loser.Kind, loser.ID)
	if got.CanonicalID != winner.ID || got.ID != winner.ID {
		t.Fatalf("loser id did not canonicalize: %+v", got)
	}
	// The command record exists: split stays possible.
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/recordmerge"}}})
	if err != nil {
		t.Fatalf("list merges: %v", err)
	}
	if len(page.Records) != 1 {
		t.Fatalf("merge records: %d", len(page.Records))
	}
}

func TestMergeRequestRejectStamps(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)

	a := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "A"}})
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "B"}})
	req := mergeRequest(t, ds, "dupe-"+a.ID+"-"+b.ID, a.ID, b.ID)

	rejected, err := decide(ds, req.ID, "rejected")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Properties["decision"] != "rejected" || rejected.Properties["decidedAt"] == nil {
		t.Fatalf("rejected: %+v", rejected.Properties)
	}
	// Nothing merged: both persons stand.
	if got := mustGet(t, ds, b.Kind, b.ID); got.CanonicalID != "" || got.DeletedAt != nil {
		t.Fatalf("reject touched the pair: %+v", got)
	}
}

// decidedAt is declared `managed`: the decision transition writes it, a
// client's own value is refused, and the round-trip echo of the stored value
// still applies — the same contract checkDeclarationWrite holds declaration
// rows to.
func TestManagedStampIsEngineWritten(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	ctx := context.Background()

	a := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "A"}})
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "B"}})

	// A creating write may not supply the stamp.
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: requestType, ID: "forged-create",
		Properties: map[string]any{
			"decidedAt": "2020-01-01T00:00:00Z",
			"winner":    vocabulary.RecordPath(typePerson, a.ID),
			"loser":     vocabulary.RecordPath(typePerson, b.ID),
		},
	})
	wantErr(t, err, substrate.ErrValidation, "creating with a forged decidedAt")

	req := mergeRequest(t, ds, "dupe-"+a.ID+"-"+b.ID, a.ID, b.ID)

	// Neither may a patch, before or after the decision.
	_, err = ds.Patch(ctx, owner, requestType, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decidedAt": "2020-01-01T00:00:00Z"},
	})
	wantErr(t, err, substrate.ErrValidation, "patching decidedAt onto an undecided request")

	rejected, err := decide(ds, req.ID, "rejected")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	stamped, _ := rejected.Properties["decidedAt"].(string)
	if stamped == "" {
		t.Fatal("decidedAt not stamped")
	}
	_, err = ds.Patch(ctx, owner, requestType, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decidedAt": "2020-01-01T00:00:00Z"},
	})
	wantErr(t, err, substrate.ErrValidation, "rewriting a stamped decidedAt")

	// The echo of the stored value is fine: get | apply round-trips.
	echoed, err := ds.Patch(ctx, owner, requestType, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decidedAt": stamped, "rationale": "still one person"},
	})
	if err != nil {
		t.Fatalf("echoing the stored stamp: %v", err)
	}
	if echoed.Properties["decidedAt"] != stamped {
		t.Fatalf("echo moved the stamp: %v", echoed.Properties["decidedAt"])
	}
}

func TestMergeRequestStaleFailsWholeAndAnnotates(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "A"}})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "B"}})
	other := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "C"}})
	first := mergeRequest(t, ds, "req-first", winner.ID, loser.ID)
	second := mergeRequest(t, ds, "req-second", other.ID, loser.ID)

	if _, err := decide(ds, first.ID, "accepted"); err != nil {
		t.Fatalf("accept first: %v", err)
	}
	// The second request still names the loser it was written against — nothing
	// repoints — and that record has been merged away, so applying it is
	// refused, and the refusal fails the TRANSITION whole.
	_, err := decide(ds, second.ID, "accepted")
	wantErr(t, err, substrate.ErrConflict, "accepting a stale merge request")

	got := mustGet(t, ds, second.Kind, second.ID)
	if got.Properties["decision"] != "proposed" {
		t.Fatalf("stale accept moved the machine: %v", got.Properties["decision"])
	}
	if _, ok := got.Properties["decidedAt"]; ok {
		t.Fatal("stale accept stamped decidedAt")
	}
	note, ok := got.Annotations["substrate/conflict"].(map[string]any)
	if !ok {
		t.Fatalf("conflict not annotated: %+v", got.Annotations)
	}
	// The annotation records the REFUSAL (the applyDiff pattern): here the
	// re-run guard sees a loser another merge already tombstoned.
	reason, _ := note["reason"].(string)
	if !strings.Contains(reason, "merge needs two live records") {
		t.Fatalf("annotation reason: %q", reason)
	}
}

func TestMergeRequestRefusesDifferentTypes(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)

	person := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "A"}})
	task := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "t"}})
	req := mustPut(t, ds, owner, substrate.PutInput{
		Kind: requestType,
		ID:   "req-cross",
		Properties: map[string]any{
			"winner": vocabulary.RecordPath(typePerson, person.ID),
			"loser":  vocabulary.RecordPath(taskType, task.ID),
		},
	})

	_, err := decide(ds, req.ID, "accepted")
	if err == nil || !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("cross-type accept: %v", err)
	}
	if got := mustGet(t, ds, req.Kind, req.ID); got.Properties["decision"] != "proposed" {
		t.Fatalf("cross-type accept moved the machine: %v", got.Properties["decision"])
	}
}

func TestManualMergeMovesLabelsAndAnnotations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Nina Ray"},
		Labels:      map[string]any{"owner/pinned": true},
		Annotations: map[string]any{"owner/note": "first"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "N. Ray"},
		Labels:      map[string]any{"owner/shelf": "audio"},
		Annotations: map[string]any{"owner/note": "second", "owner/extra": 7},
	})
	rec, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: winner.Kind, Winner: winner.ID, Loser: loser.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	got := mustGet(t, ds, winner.Kind, winner.ID)
	if got.Labels["owner/shelf"] != "audio" || got.Labels["owner/pinned"] != true {
		t.Fatalf("labels = %v", got.Labels)
	}
	// Newest annotation wins; the loser's extra key moves over.
	if got.Annotations["owner/note"] != "second" {
		t.Fatalf("annotations = %v", got.Annotations)
	}
	if got.Annotations["owner/extra"] == nil {
		t.Fatalf("annotations = %v", got.Annotations)
	}
	if got.Properties["name"] != "Nina Ray" {
		t.Fatal("merge must not merge properties")
	}

	if _, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: rec.ID}); err != nil {
		t.Fatalf("split: %v", err)
	}
	w := mustGet(t, ds, winner.Kind, winner.ID)
	l := mustGet(t, ds, loser.Kind, loser.ID)
	if _, ok := w.Labels["owner/shelf"]; ok {
		t.Fatalf("split should strip moved labels: %v", w.Labels)
	}
	if w.Annotations["owner/note"] != "first" {
		t.Fatalf("split should restore the overwritten annotation: %v", w.Annotations)
	}
	if w.Annotations["owner/extra"] != nil {
		t.Fatalf("split should strip moved annotations: %v", w.Annotations)
	}
	if l.Annotations["owner/note"] != "second" || l.Annotations["owner/extra"] == nil {
		t.Fatalf("loser annotations not restored: %v", l.Annotations)
	}
	if l.DeletedAt != nil {
		t.Fatal("loser should be live again")
	}
	if got := mustGet(t, ds, loser.Kind, loser.ID); got.CanonicalID != "" {
		t.Fatalf("the loser answers to its own id again: %+v", got)
	}
}

func TestMergeLoserSurvivesGC(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	a := mustPut(t, ds, owner, substrate.PutInput{Kind: "organization", Properties: map[string]any{"name": "A"}})
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: "organization", Properties: map[string]any{"name": "B"}})
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: a.Kind, Winner: a.ID, Loser: b.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Get(ctx, b.Kind, b.ID); err != nil {
		t.Fatalf("GC collected a merge loser: %v", err)
	}
}
