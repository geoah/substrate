package catalog_test

// A rehomed sample import meets a local retirement (decision record 0055):
// the sample's closure lands under the repository's own authority, which is
// the authority the retirement was written under, so a retired kind name the
// sample declares refuses the import on the same sentence every other door
// says. Rehoming changes the authority's spelling, not who owns the name.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestImportRefusesARetiredKindName(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	importSamples(t, c, ds, peopleSampleID, schedulingSample)

	// The repository retires `task` under its own `tasks` package before the
	// sample arrives: the header is the repository's to write.
	header := vocabulary.PackageManifest(homeAuthority+"/tasks", 0)
	header["data"].(map[string]any)["retired"] = map[string]any{"kinds": []any{"task"}}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{header}); err != nil {
		t.Fatalf("retire task under the repository's tasks package: %v", err)
	}

	_, _, err := c.Import(ctx, substrate.ActorAPI, tasksSampleID, ds)
	if err == nil {
		t.Fatal("imported a sample declaring a kind name the repository retired")
	}
	if !strings.Contains(err.Error(), "kind "+homeAuthority+"/tasks/task is retired and declared; a retired name is never declared again") {
		t.Fatalf("the refusal does not name the retirement under the repository's authority: %v", err)
	}
	if _, err := ds.KindByRef(ctx, homeAuthority+"/tasks/task"); err == nil {
		t.Fatal("the refused import landed the retired kind")
	}
}
