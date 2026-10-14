package catalog_test

// A sample's record views (decision record 0166) through the import door: a
// view lands naming the repository's own kind, because the import rehomes its
// `appliesTo` with everything else.

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
