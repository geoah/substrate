package catalog_test

// A sample's record views (decision record 0166) through the import door: a
// view lands naming the repository's own kind, because the import rehomes its
// `appliesTo` with everything else, and a re-import writes a shipped view only
// where the repository holds none at its id, so the owner's edit survives.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const (
	recordViewKind      = "substrate.reamde.dev/console/recordview"
	observationsSample  = samplesPlacehldr + "/observations"
	healthSample        = samplesPlacehldr + "/health"
	kindDeclarationPath = "substrate.reamde.dev/core/kind/"
)

func appliesTo(t *testing.T, ds substrate.Dataset, id string) string {
	t.Helper()
	view, err := ds.Get(context.Background(), recordViewKind, id)
	if err != nil {
		t.Fatalf("read the %s view: %v", id, err)
	}
	ref, _ := view.Properties["appliesTo"].(map[string]any)
	path, _ := ref["ref"].(string)
	return path
}

func viewName(t *testing.T, ds substrate.Dataset, id string) string {
	t.Helper()
	view, err := ds.Get(context.Background(), recordViewKind, id)
	if err != nil {
		t.Fatalf("read the %s view: %v", id, err)
	}
	name, _ := view.Properties["name"].(string)
	return name
}

func TestImportedRecordViewsFollowTheRehome(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	importSamples(t, c, ds, observationsSample, healthSample)

	for _, kind := range []string{"sleep", "workout"} {
		want := kindDeclarationPath + homeAuthority + "/health/" + kind
		if got := appliesTo(t, ds, "health/"+kind); got != want {
			t.Errorf("the %s view applies to %q, want %q", kind, got, want)
		}
	}
}

func TestReimportKeepsAnEditedRecordView(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	importSamples(t, c, ds, observationsSample, healthSample)
	shipped := viewName(t, ds, "health/workout")

	if _, err := ds.Patch(ctx, substrate.ActorAPI, recordViewKind, "health/workout", substrate.PatchInput{
		Properties: map[string]any{"name": "My runs"},
	}); err != nil {
		t.Fatalf("edit the workout view: %v", err)
	}
	importSamples(t, c, ds, healthSample)
	if got := viewName(t, ds, "health/workout"); got != "My runs" {
		t.Fatalf("a re-import overwrote the owner's view: name = %q", got)
	}

	// Deleting the view is how the owner takes the shipped one again.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, recordViewKind, "health/workout", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the workout view: %v", err)
	}
	importSamples(t, c, ds, healthSample)
	if got := viewName(t, ds, "health/workout"); got != shipped {
		t.Fatalf("a re-import after a delete did not restore the shipped view: name = %q, want %q", got, shipped)
	}
}
