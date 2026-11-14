package api

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// One route adds every catalog package, and who keeps the package decides
// where it lands (decision record 0258): a published one under the authority
// that publishes it, as the publisher's; a sample copied into this
// repository's own namespace, `~` (decision record 0259), as the
// repository's. The answer is the status of the bundle as it landed.
func TestCatalogAddLandsEachPackageWhereItsKeeperSays(t *testing.T) {
	t.Parallel()
	env := catalogEnv(t, catalogShipped(t))
	add := func(id string) substrate.BundleStatus {
		t.Helper()
		rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(id)+"/add", env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		return decodeJSON[substrate.BundleStatus](t, rec)
	}

	if got := add(googleBundleID); got.ID != googleBundleID || !got.Installed {
		t.Fatalf("adding %s answered %+v, want the package itself, installed", googleBundleID, got)
	}
	if row := env.get(t, corePackage+"/package", googleBundleID); row == nil || row.Properties["source"] != vocabulary.SourcePublished {
		t.Fatalf("the published package landed as %+v, want it the publisher's", row)
	}

	landed := "~/people"
	if got := add(peopleBundleID); got.ID != landed || !got.Installed {
		t.Fatalf("adding %s answered %+v, want the copy at %s, installed", peopleBundleID, got, landed)
	}
	if row := env.get(t, corePackage+"/package", landed); row == nil || row.Properties["source"] != vocabulary.SourceInstalled {
		t.Fatalf("the sample landed as %+v, want the repository's copy", row)
	}
	if row := env.get(t, corePackage+"/package", peopleBundleID); row != nil {
		t.Fatalf("adding a sample declared it under the sample authority too: %+v", row)
	}
}
