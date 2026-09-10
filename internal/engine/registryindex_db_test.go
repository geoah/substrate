package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A stale ordinal index never survives a changed definition. Indexes are built
// before the apply's transaction, so a transaction that fails afterwards
// leaves them; a corrected retry that changes what ordinal zero indexes must
// then rebuild it rather than find the old one "already there".
func TestAChangedIndexDefinitionRebuildsTheStaleOrdinalIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	const kind = publishPackage + "/meter"
	meter := func(indexed string) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(publishPackage, 0),
			vocabulary.KindManifest(publishPackage,
				map[string]any{"singular": "meter"},
				map[string]any{
					"properties": map[string]any{
						"name":  map[string]any{"type": "string"},
						"email": map[string]any{"type": "string"},
					},
					"indices": []any{map[string]any{"properties": []any{indexed}}},
				}),
		}
	}
	indexDef := func() string {
		var def string
		if err := ds.db.QueryRowContext(ctx, `SELECT indexdef FROM pg_indexes WHERE indexname = $1`,
			"idx_"+derivedID(kind, "0")).Scan(&def); err != nil {
			t.Fatalf("the ordinal-zero index: %v", err)
		}
		return def
	}

	// The first apply builds the [name] index and then fails inside its
	// transaction, so the index stays and the kind does not land.
	docs, err := parseVocabularyDocs(meter("name"))
	if err != nil {
		t.Fatal(err)
	}
	failed := errors.New("the batch's data write failed")
	_, err = ds.applyVocabularyBatch(ctx, substrate.ActorAPI, vocabularyBatch{docs: docs, extra: func(*txn) error {
		return failed
	}})
	if !errors.Is(err, failed) {
		t.Fatalf("the failing apply: %v", err)
	}
	if def := indexDef(); !strings.Contains(def, "'name'") {
		t.Fatalf("the failed apply did not leave the [name] index this test needs: %s", def)
	}

	// The corrected retry indexes email at the same ordinal.
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, meter("email")); err != nil {
		t.Fatalf("the corrected apply: %v", err)
	}
	if def := indexDef(); !strings.Contains(def, "'email'") || strings.Contains(def, "'name'") {
		t.Fatalf("the committed kind declares email indexed, but the ordinal-zero index is: %s", def)
	}
}
