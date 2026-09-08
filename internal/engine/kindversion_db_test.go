package engine_test

// A RECORD NAMES THE KIND VERSION THAT WROTE IT (#146, decision 0060). The
// edit counter `version` says how often a record moved; `kindVersion` says
// which declaration of its kind validated the data it holds. The stamp is a
// value in the `record` delta, so it moves only with a write that moves the
// row, the fold restores it without asking the registry, and a rebuild hands
// back the version that wrote each record rather than the one the kind holds
// today.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const kvPackage = "stamps.example.substrate.reamde.dev/stamps"

func kvWidget(props map[string]any) map[string]any {
	return vocabulary.KindManifest(kvPackage,
		map[string]any{"singular": "widget", "plural": "widgets"},
		map[string]any{"properties": props})
}

func TestARecordCarriesTheKindVersionThatWroteIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newCoreDataset(t)
	const widget = kvPackage + "/widget"
	name := map[string]any{"name": map[string]any{"type": "string"}}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(kvPackage, 0), kvWidget(name),
	}); err != nil {
		t.Fatalf("declare the widget kind: %v", err)
	}
	v1 := kindVersion(t, ds, widget)
	if v1 == 0 {
		t.Fatal("the installed kind carries no version; the stamp would be indistinguishable from history without one")
	}

	first := mustPut(t, ds, owner, substrate.PutInput{
		Kind: widget, ID: "w1", Properties: map[string]any{"name": "first"},
	})
	if first.KindVersion != v1 {
		t.Fatalf("a record written under kind version %d reads back kindVersion %d", v1, first.KindVersion)
	}
	third := mustPut(t, ds, owner, substrate.PutInput{
		Kind: widget, ID: "w3", Properties: map[string]any{"name": "third"},
	})

	// A new optional property is a widening the guard admits, and a changed
	// declaration lands at stored+1 (decision 0002).
	withNote := map[string]any{
		"name": map[string]any{"type": "string"},
		"note": map[string]any{"type": "string"},
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(kvPackage, 0), kvWidget(withNote),
	}); err != nil {
		t.Fatalf("re-declare the widget kind with a note: %v", err)
	}
	v2 := kindVersion(t, ds, widget)
	if v2 <= v1 {
		t.Fatalf("re-declaring the kind left its version at %d (was %d); the test needs two versions to tell apart", v2, v1)
	}

	second := mustPut(t, ds, owner, substrate.PutInput{
		Kind: widget, ID: "w2", Properties: map[string]any{"name": "second", "note": "written later"},
	})
	if second.KindVersion != v2 {
		t.Fatalf("a record written under kind version %d reads back kindVersion %d", v2, second.KindVersion)
	}
	// The first record was not written since; it keeps the version that
	// validated it, whatever the kind is at now.
	if got := mustGet(t, ds, widget, "w1"); got.KindVersion != v1 {
		t.Fatalf("an untouched record's kindVersion moved to %d, want %d", got.KindVersion, v1)
	}

	// An identical re-put moves nothing: not the edit counter, not the stamp,
	// and no changelog entry. The stamp names the declaration that last WROTE
	// the columns, and this write wrote none.
	head := maxSeq(t, ds)
	again := mustPut(t, ds, owner, substrate.PutInput{
		Kind: widget, ID: "w1", Properties: map[string]any{"name": "first"},
	})
	if again.Version != first.Version || again.KindVersion != v1 {
		t.Fatalf("an identical re-put moved the row: version %d -> %d, kindVersion %d -> %d",
			first.Version, again.Version, v1, again.KindVersion)
	}
	if got := maxSeq(t, ds); got != head {
		t.Fatalf("an identical re-put appended a changelog entry (seq %d -> %d)", head, got)
	}

	// A write that moves the row re-validates it under the current
	// declaration and the stamp follows.
	moved := mustPatch(t, ds, owner, widget, third.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "third, renamed"},
	})
	if moved.KindVersion != v2 {
		t.Fatalf("a patch under kind version %d left kindVersion at %d", v2, moved.KindVersion)
	}

	// The changelog carries every stamp as a value, so the rebuild reproduces
	// the fold, the column included, and reads back the same three answers.
	before := foldOf(t, ds)
	rb, ok := svc.(rebuilder)
	if !ok {
		t.Fatal("the service cannot rebuild a repository")
	}
	if _, err := rb.RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); string(before) != string(after) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, after))
	}
	for id, want := range map[string]int64{"w1": v1, "w2": v2, "w3": v2} {
		if got := mustGet(t, ds, widget, id).KindVersion; got != want {
			t.Errorf("after the rebuild %s carries kindVersion %d, want %d", id, got, want)
		}
	}
}

// Merge and split rewrite rows without validating a property: a merge does
// not migrate properties, and a split revives a tombstoned loser whose values
// the narrowing guard never counted (it counts live rows). Neither may claim
// the current declaration validated the row, so each carries the stamp the
// row already holds, and the winner and the revived loser read back the
// version that wrote them.
func TestMergeAndSplitCarryTheStampUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	const widget = kvPackage + "/widget"
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(kvPackage, 0),
		kvWidget(map[string]any{"name": map[string]any{"type": "string"}}),
	}); err != nil {
		t.Fatalf("declare the widget kind: %v", err)
	}
	v1 := kindVersion(t, ds, widget)
	mustPut(t, ds, owner, substrate.PutInput{Kind: widget, ID: "w1", Properties: map[string]any{"name": "one"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: widget, ID: "w2", Properties: map[string]any{"name": "two"}})

	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(kvPackage, 0),
		kvWidget(map[string]any{
			"name": map[string]any{"type": "string"},
			"note": map[string]any{"type": "string"},
		}),
	}); err != nil {
		t.Fatalf("re-declare the widget kind: %v", err)
	}
	if v2 := kindVersion(t, ds, widget); v2 <= v1 {
		t.Fatalf("re-declaring the kind left its version at %d", v2)
	}

	merged, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: widget, Winner: "w1", Loser: "w2"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := mustGet(t, ds, widget, "w1").KindVersion; got != v1 {
		t.Fatalf("the merge stamped the winner with kind version %d, want %d kept", got, v1)
	}
	if _, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: merged.ID}); err != nil {
		t.Fatalf("split: %v", err)
	}
	if got := mustGet(t, ds, widget, "w2").KindVersion; got != v1 {
		t.Fatalf("the split stamped the revived loser with kind version %d, want %d kept", got, v1)
	}
	if got := mustGet(t, ds, widget, "w1").KindVersion; got != v1 {
		t.Fatalf("the split stamped the winner with kind version %d, want %d kept", got, v1)
	}
}
