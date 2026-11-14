package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// A sample has no publisher, so a reviewed publisher's version on adding one
// is refused, never ignored, and refused before the repository is read.
func TestCatalogAddOfASampleRefusesExpect(t *testing.T) {
	env := newCatalogStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(peopleBundleID)+"/add", stubToken,
		map[string]any{"expect": map[string]any{"version": 1, "digest": "cafe"}})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if !strings.Contains(rec.Body.String(), "expect") {
		t.Fatalf("the refusal does not name the key: %s", rec.Body.String())
	}
}

// The two routes the add route replaced are gone.
func TestCatalogInstallAndImportAreGone(t *testing.T) {
	env := newCatalogStubEnv(t, nil)
	for _, gone := range []string{"install", "import"} {
		rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(peopleBundleID)+"/"+gone, stubToken, nil)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST …/%s answered %d; the route is gone", gone, rec.Code)
		}
	}
}
