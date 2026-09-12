package engine_test

// What `/vocabulary/apply` does with `movedFrom` (record 0078): it STORES the
// key and refuses the move, because this door publishes its candidate registry
// before any rewrite could run and a move reaches past the write path's
// admission rules. The move itself is the shipped boot upgrade's, and
// internal/testenv's drill is where it is performed.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const mvPackage = "ada.example.com/mv"

// mvKind renders one kind of the move package.
func mvKind(singular string, data map[string]any) map[string]any {
	return vocabulary.KindManifest(mvPackage, map[string]any{"singular": singular}, data)
}

func mvProps(extra map[string]any) map[string]any {
	props := map[string]any{"name": map[string]any{"type": "string"}}
	for k, v := range extra {
		props[k] = v
	}
	return props
}

// mvRepository declares the OLD kind and returns the dataset holding it.
func mvRepository(t *testing.T, oldSingular string, props map[string]any) substrate.Dataset {
	t.Helper()
	svc, _ := newService(t)
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(mvPackage, 1),
		mvKind(oldSingular, map[string]any{"properties": props}),
	}); err != nil {
		t.Fatalf("declare %s: %v", oldSingular, err)
	}
	return ds
}

// mvMove applies the declaration that would move oldSingular onto newSingular.
func mvMove(ctx context.Context, ds substrate.Dataset, oldSingular, newSingular string, props map[string]any) error {
	_, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(mvPackage, 2),
		mvKind(oldSingular, map[string]any{"properties": props}),
		mvKind(newSingular, map[string]any{
			"movedFrom":  mvPackage + "/" + oldSingular,
			"properties": props,
		}),
	})
	return err
}

// THE USER DOOR STORES AND REFUSES. A declaration naming a kind the repository
// still declares is refused, naming the key, and nothing moves.
func TestKindMoveIsRefusedAtTheApplyDoor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := mvRepository(t, "widget", mvProps(nil))
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: mvPackage + "/widget", ID: "w-1", Properties: map[string]any{"name": "stays"},
	})
	err := mvMove(ctx, ds, "widget", "gadget", mvProps(nil))
	if err == nil {
		t.Fatal("the apply door must refuse a move, not perform one")
	}
	if !strings.Contains(err.Error(), "movedFrom is honored for the shipped vocabulary only") {
		t.Fatalf("the refusal does not name the reason: %v", err)
	}
	// The row is where it was, and the destination kind did not land either:
	// the batch is one transaction and the refusal took all of it.
	if got := mustGet(t, ds, mvPackage+"/widget", "w-1"); got.Properties["name"] != "stays" {
		t.Fatalf("the row moved anyway: %+v", got.Properties)
	}
	if _, err := ds.KindByRef(ctx, mvPackage+"/gadget"); err == nil {
		t.Fatal("the refused batch landed its destination kind")
	}
}

// The preview says the same thing as the door, which is what makes a preview
// worth reading.
func TestKindMovePreviewBlocksAtTheApplyDoor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := mvRepository(t, "widget", mvProps(nil))
	plan, err := ds.PlanVocabularyApply(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(mvPackage, 2),
		mvKind("widget", map[string]any{"properties": mvProps(nil)}),
		mvKind("gadget", map[string]any{
			"movedFrom": mvPackage + "/widget", "properties": mvProps(nil),
		}),
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(plan.Blockers) == 0 ||
		!strings.Contains(strings.Join(plan.Blockers, "; "), "movedFrom is honored for the shipped vocabulary only") {
		t.Fatalf("the preview does not block the move: %q", plan.Blockers)
	}
	for _, step := range plan.ConversionPlan.Steps {
		if step.Step == substrate.StepMove {
			t.Fatalf("the preview plans a move this door will not run: %+v", step)
		}
	}
}

// A `movedFrom` naming a kind the repository does NOT declare is inert: it is
// stored, refuses nothing, and the declaration lands.
func TestKindMovedFromAnAbsentKindIsInert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := mvRepository(t, "widget", mvProps(nil))
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(mvPackage, 2),
		mvKind("widget", map[string]any{"properties": mvProps(nil)}),
		mvKind("gadget", map[string]any{
			"movedFrom": mvPackage + "/nothing", "properties": mvProps(nil),
		}),
	}); err != nil {
		t.Fatalf("a movedFrom naming an absent kind must land: %v", err)
	}
	ty, err := ds.KindByRef(ctx, mvPackage+"/gadget")
	if err != nil {
		t.Fatalf("the kind did not land: %v", err)
	}
	if ty.Definition["movedFrom"] != mvPackage+"/nothing" {
		t.Fatalf("movedFrom = %v, want it stored as written", ty.Definition["movedFrom"])
	}
}
