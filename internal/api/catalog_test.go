package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

const rlBundleID = "samples.substrate.reamde.dev/readinglist"

const googleBundleID = "providers.substrate.reamde.dev/google"

const peopleBundleID = "samples.substrate.reamde.dev/people"

// catalogShipped is the catalog the binary ships.
func catalogShipped(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Load(catalog.Shipped()...)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return cat
}

// newCatalogStubEnv is the handler shipping the real catalog over ds (nil is
// a bare stubDataset), for a refusal the repository answers no part of.
func newCatalogStubEnv(t *testing.T, ds substrate.Dataset) *testEnv {
	t.Helper()
	cat := catalogShipped(t)
	return newStubEnv(t, ds, func(c *Config) { c.Catalog = cat })
}

// The two doors are not interchangeable: a PROVIDER installs under the
// authority that publishes it, so importing one is refused and the refusal
// names the verb that does work.
func TestCatalogImportRefusesAProvider(t *testing.T) {
	env := newCatalogStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(googleBundleID)+"/import", stubToken, nil)
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	if body := rec.Body.String(); !strings.Contains(body, "install") {
		t.Errorf("the refusal does not name the verb that works: %s", body)
	}
}

// Import is an owner action, exactly as install is: a request attributed to a
// machine is refused before the closure is touched.
func TestCatalogImportRefusesNonOwner(t *testing.T) {
	env := newCatalogStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(rlBundleID)+"/import", stubToken, nil,
		actorHeader, "reader.substrate.reamde.dev")
	wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
}

// An unknown id is refused by the CATALOG, not by the router, so the message
// names the bundle. A bare 404 would also come back from a path that routes
// nowhere, which is why the body is what this asserts.
func TestCatalogImportUnknownNamesTheBundle(t *testing.T) {
	env := newCatalogStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, "/api/v1/catalog/nope.example.com%2Fnothing/import", stubToken, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if body := rec.Body.String(); !strings.Contains(body, "nope.example.com/nothing") {
		t.Errorf("the refusal does not name the bundle asked for: %s", body)
	}
}

func TestCatalogDetailUnknownIs404(t *testing.T) {
	env := newCatalogStubEnv(t, nil)
	rec := env.do(t, http.MethodGet, "/api/v1/catalog/nope.bundles.substrate.reamde.dev", stubToken, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}

// catalogStatusFails is a repository whose bundle status reads fail: the
// repository or database fault review #11 must surface as an error rather
// than an empty installed set, which no request can make the engine produce.
type catalogStatusFails struct{ stubDataset }

var errCatalogStatusRead = errors.New("the bundle status read failed")

func (catalogStatusFails) BundleStatuses(context.Context) ([]substrate.BundleStatus, error) {
	return nil, errCatalogStatusRead
}

func (catalogStatusFails) BundleStatusesUncounted(context.Context) ([]substrate.BundleStatus, error) {
	return nil, errCatalogStatusRead
}

func (catalogStatusFails) BundleStatus(context.Context, string) (substrate.BundleStatus, error) {
	return substrate.BundleStatus{}, errCatalogStatusRead
}

// A status-read fault is surfaced as an error, never a 200 that silently
// reports an installed integration as available — on both the
// list and the detail read.
func TestCatalogListSurfacesStatusReadFailure(t *testing.T) {
	env := newCatalogStubEnv(t, catalogStatusFails{})
	rec := env.do(t, http.MethodGet, "/api/v1/catalog", stubToken, nil)
	wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)
}

func TestCatalogDetailSurfacesStatusReadFailure(t *testing.T) {
	env := newCatalogStubEnv(t, catalogStatusFails{})
	rec := env.do(t, http.MethodGet, "/api/v1/catalog/"+url.PathEscape(rlBundleID), stubToken, nil)
	wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)
}

// Install is an owner action: a request ATTRIBUTED to something other than
// the owner is refused before the closure is touched. The actor rides on the
// header now — attribution, not authorization — so this is the same refusal
// reached a shorter way.
func TestCatalogInstallRefusesNonOwner(t *testing.T) {
	env := newCatalogStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(rlBundleID)+"/install", stubToken, nil,
		actorHeader, "reader.substrate.reamde.dev")
	wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
}

// The install body declares its keys: one it does not is refused before the
// catalog is asked.
func TestCatalogInstallRefusesAnUndeclaredKey(t *testing.T) {
	env := newCatalogStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(googleBundleID)+"/install", stubToken, map[string]any{"force": true})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

// A reviewed candidate that is not the one the catalog would install is the
// conflict naming a fresh review, before any repository read.
func TestCatalogInstallRejectsAStaleReview(t *testing.T) {
	env := newCatalogStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(googleBundleID)+"/install", stubToken, map[string]any{"expect": map[string]any{"version": 1, "digest": "stale"}})
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if !strings.Contains(rec.Body.String(), "the publisher listed another version since you reviewed this one; review it again") {
		t.Fatal(rec.Body.String())
	}
}

// otherReadinglistBundleID is a second SAMPLE of the package word
// `readinglist`, published by another authority: both land as
// `<home>/readinglist`, so the landed id alone cannot say which one a
// repository imported.
const otherReadinglistBundleID = "other.example.com/readinglist"

// catalogWithOtherReadinglist is the shipped catalog plus a second sample
// root carrying otherReadinglistBundleID.
func catalogWithOtherReadinglist(t *testing.T) *catalog.Catalog {
	t.Helper()
	other := fstest.MapFS{
		"readinglist/bundle.yaml": &fstest.MapFile{Data: []byte(`
kind: substrate.reamde.dev/core/package
metadata:
  id: other.example.com/readinglist
data:
  authority: other.example.com
  package: readinglist
  version: 2
---
kind: substrate.reamde.dev/core/bundle
metadata:
  id: other.example.com/readinglist
data:
  authority: other.example.com
  package: readinglist
  description: another authority's reading-list sample
  installs:
    - other.example.com/readinglist/page
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: other.example.com/readinglist/page
data:
  authority: other.example.com
  package: readinglist
  names:
    singular: page
  properties:
    url:
      type: string
`)},
	}
	cat, err := catalog.Load(append(catalog.Shipped(), catalog.SampleRoot(other))...)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if _, ok := cat.ByID(otherReadinglistBundleID); !ok {
		t.Fatalf("the second reading-list sample did not load: %v", cat.Warnings())
	}
	return cat
}
