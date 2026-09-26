package engine_test

// A package row names the actor that first declared it (decision record
// 0106): the engine stamps the transaction's actor on the row it creates,
// keeps it across every later apply, and no document may write it.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const kindPackageRef = "substrate.reamde.dev/core/package"

func TestPackageRecordsTheActorThatDeclaredIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)

	const declarer = substrate.ActorConsole
	if _, err := ds.ApplyVocabularyDocuments(ctx, declarer, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("gizmo", map[string]any{"label": map[string]any{"type": "string"}}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := mustGet(t, ds, kindPackageRef, swPackage).Properties["declaredBy"]; got != string(declarer) {
		t.Fatalf("declaredBy = %v, want %q", got, declarer)
	}

	// A later apply by another hand changes the package and not its declarer.
	edited := vocabulary.PackageManifest(swPackage, 0)
	edited["data"].(map[string]any)["description"] = "gizmos, edited"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		edited,
		swTypeDoc("gizmo", map[string]any{"label": map[string]any{"type": "string"}}),
		swTypeDoc("gadget", map[string]any{"label": map[string]any{"type": "string"}}),
	}); err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	row := mustGet(t, ds, kindPackageRef, swPackage)
	if row.Properties["description"] != "gizmos, edited" {
		t.Fatalf("the re-apply did not land: %v", row.Properties)
	}
	if got := row.Properties["declaredBy"]; got != string(declarer) {
		t.Fatalf("declaredBy after a re-apply by %s = %v, want %q", substrate.ActorAPI, got, declarer)
	}

	// The seed declares core under the seed actor.
	if got := mustGet(t, ds, kindPackageRef, "substrate.reamde.dev/core").Properties["declaredBy"]; got != string(substrate.ActorSeed) {
		t.Fatalf("core declaredBy = %v, want %q", got, substrate.ActorSeed)
	}
}

func TestPackageDocumentCannotWriteDeclaredBy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)

	forged := vocabulary.PackageManifest(swPackage, 0)
	forged["data"].(map[string]any)["declaredBy"] = "agent:example.com:notes:notekeeper"
	_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{forged})
	if err == nil || !strings.Contains(err.Error(), "declaredBy") {
		t.Fatalf("a document writing declaredBy was not refused by name: %v", err)
	}
}
