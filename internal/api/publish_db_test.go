package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/domains"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

// recipesDocuments declares ~/recipes (an ingredient, a recipe that
// references one, and a function that writes a recipe) and ~/notes (a note
// that references a recipe).
func recipesDocuments() []any {
	const recipes, notes = "~/recipes", "~/notes"
	return []any{
		vocabulary.PackageManifest(recipes, 1),
		vocabulary.KindManifest(recipes, map[string]any{"singular": "ingredient"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
		vocabulary.KindManifest(recipes, map[string]any{"singular": "recipe"}, map[string]any{
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"main": map[string]any{"type": "reference", "kind": recipes + "/ingredient"},
			},
		}),
		vocabulary.FunctionManifest(recipes, "add", map[string]any{
			"description": "writes one recipe",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{recipes + "/recipe"}},
			"source": `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "~/recipes/recipe", "id": input["args"]["id"],
                         "properties": {"name": input["args"]["name"]}}]}
`,
		}),
		vocabulary.PackageManifest(notes, 1),
		vocabulary.KindManifest(notes, map[string]any{"singular": "note"}, map[string]any{
			"properties": map[string]any{
				"text":   map[string]any{"type": "string"},
				"recipe": map[string]any{"type": "reference", "kind": recipes + "/recipe"},
			},
		}),
	}
}

// otherRepository registers another repository on env's server and answers a
// full-access token into it.
func otherRepository(t *testing.T, env *dbEnv, name string) (substrate.Dataset, string) {
	t.Helper()
	ctx := context.Background()
	if _, err := env.svc.Register(ctx, substrate.RegisterInput{Identity: name, Repository: name, Password: renamePassword, SkipTOTP: true}); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	ds, err := env.svc.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return ds, secret
}

// A repository publishes one of its packages and a repository on another
// server installs it (decision records 0289 and 0291), over HTTP on the real
// engine, with nothing shared between the two servers but the network and a
// stand-in for DNS. The publisher is Ada's repository and publishes under its
// own name, the page's example: its records, references and other packages
// follow the package out of `~`; publishing without the proof, with a proof
// for another domain, under the project's name, under another repository's
// published name, and of a package that is not its own are refused with
// nothing changed; a rename of the publisher moves nothing published; and the
// second release reaches the installed copy.
func TestARepositoryPublishesAPackageAndAnotherServerInstallsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dns := domains.Static{}
	// The publisher's server, at an HTTPS public URL.
	served := httptest.NewUnstartedServer(http.NotFoundHandler())
	served.StartTLS()
	t.Cleanup(served.Close)
	a := newDBEnv(t, withPassword(renamePassword), withConfig(func(c *Config) {
		c.PublicURL, c.DomainResolver = served.URL, dns
	}))
	served.Config.Handler = a.h
	pub := a.repository
	pkg := pub + "/recipes"
	id := a.ds.Repository().ID

	wantStatus(t, a.do(t, http.MethodPost, "/api/v1/vocabulary/apply", a.token, map[string]any{"documents": recipesDocuments()}), http.StatusOK)
	flour, bread, note := mintedID("flour"), mintedID("bread"), mintedID("note")
	for path, props := range map[string]map[string]any{
		"~/recipes/ingredient/" + flour: {"name": "flour"},
		"~/recipes/recipe/" + bread:     {"name": "bread", "main": map[string]any{"ref": "~/recipes/ingredient/" + flour}},
	} {
		wantStatus(t, a.do(t, http.MethodPut, "/api/v1/"+path, a.token, map[string]any{"properties": props}), http.StatusCreated)
	}
	wantStatus(t, a.do(t, http.MethodPut, "/api/v1/~/notes/note/"+note, a.token, map[string]any{"properties": map[string]any{
		"text": "bake on sunday", "recipe": map[string]any{"ref": "~/recipes/recipe/" + bread},
	}}), http.StatusCreated)

	publish := func(env *dbEnv, token, pkg, authority string, confirm bool) *httptest.ResponseRecorder {
		return env.do(t, http.MethodPost, "/api/v1/packages/publish", token, substrate.PublishInput{Package: pkg, Authority: authority, Confirm: confirm})
	}
	rec := publish(a, a.token, "~/recipes", pub, false)
	wantStatus(t, rec, http.StatusOK)
	plan := decodeJSON[substrate.PublishPlan](t, rec)
	document := served.URL + "/published/" + id + "/" + pub
	if plan.Published != pkg || plan.Records != 2 || plan.References != 1 || plan.Proven ||
		plan.DocumentURL != document || !strings.Contains(plan.Record, "doc="+document) || plan.Name != "_substrate."+pub {
		t.Fatalf("the plan before the proof: %+v", plan)
	}

	unchanged := func(what string) {
		t.Helper()
		wantStatus(t, a.do(t, http.MethodGet, "/api/v1/~/recipes/recipe/"+bread, a.token, nil), http.StatusOK)
		if rels, err := a.ds.Releases(ctx, ""); err != nil || len(rels) != 0 {
			t.Fatalf("%s left releases %v (%v)", what, rels, err)
		}
	}
	// No proof.
	wantStatus(t, publish(a, a.token, "~/recipes", pub, true), http.StatusForbidden)
	unchanged("a publish without the proof")
	// A proof for another domain: the record is at other.example.com, so
	// pub has none, and other.example.com's names pub's document.
	dns["_substrate.other.example.com."] = []string{plan.Record}
	wantStatus(t, publish(a, a.token, "~/recipes", pub, true), http.StatusForbidden)
	wantStatus(t, publish(a, a.token, "~/recipes", "other.example.com", true), http.StatusForbidden)
	unchanged("a publish with a proof for another domain")
	// The project's name, and a name beneath it, even with a record.
	for _, name := range []string{"substrate.reamde.dev", "recipes.substrate.reamde.dev"} {
		dns["_substrate."+name+"."] = []string{"v=substrate1 doc=" + served.URL + "/published/" + id + "/" + name}
		wantStatus(t, publish(a, a.token, "~/recipes", name, true), http.StatusForbidden)
	}
	unchanged("a publish under the project's name")
	// A package that is not the repository's own.
	wantStatus(t, publish(a, a.token, "substrate.reamde.dev/core", pub, true), http.StatusUnprocessableEntity)
	unchanged("a publish of a package that is not the repository's own")

	// The proof, and the publish.
	dns["_substrate."+pub+"."] = []string{plan.Record}
	rec = publish(a, a.token, "~/recipes", pub, true)
	wantStatus(t, rec, http.StatusOK)
	done := decodeJSON[substrate.PublishPlan](t, rec)
	if done.Release == nil || done.Release.Package != pkg || !done.Proven {
		t.Fatalf("the publish answered %+v", done)
	}
	first := done.Release.Version

	// Another repository on the same server cannot publish under that name:
	// its record names Ada's document.
	c, cToken := otherRepository(t, a, testdb.RepositoryLabel(t)+"-c.example.com")
	if _, err := c.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest("~/things", 1),
		vocabulary.KindManifest("~/things", map[string]any{"singular": "thing"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, publish(a, cToken, "~/things", pub, true), http.StatusForbidden)

	// Ada's own records and references under the published name, and
	// nothing of the package left in `~`.
	rec = a.do(t, http.MethodGet, "/api/v1/"+pkg+"/recipe/"+bread, a.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.Record](t, rec); got.Kind != pkg+"/recipe" || got.Properties["main"].(map[string]any)["ref"] != pkg+"/ingredient/"+flour {
		t.Fatalf("the published recipe reads %s with %v", got.Kind, got.Properties["main"])
	}
	rec = a.do(t, http.MethodGet, "/api/v1/~/notes/note/"+note, a.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.Record](t, rec); got.Properties["recipe"].(map[string]any)["ref"] != pkg+"/recipe/"+bread {
		t.Fatalf("the note's reference reads %v", got.Properties["recipe"])
	}
	wantStatus(t, a.do(t, http.MethodGet, "/api/v1/~/recipes/recipe/"+bread, a.token, nil), http.StatusNotFound)
	// The name the package is published under is the published package's
	// now, not a spelling of `~`: Ada's other packages are written `~`.
	wantStatus(t, a.do(t, http.MethodGet, "/api/v1/~/notes/note/"+note, a.token, nil), http.StatusOK)
	wantStatus(t, a.do(t, http.MethodGet, "/api/v1/"+pub+"/notes/note/"+note, a.token, nil), http.StatusNotFound)

	// The installing server: its own database, its own catalog, a reader
	// that finds the publisher through the same stand-in for DNS and reaches
	// it over HTTPS.
	var clock atomic.Pointer[time.Time]
	start := time.Now()
	clock.Store(&start)
	reader := authority.NewReader(authority.Options{Resolver: dns, Client: served.Client(), Now: func() time.Time { return *clock.Load() }})
	cat, err := catalog.LoadWithReader(reader, kinds.SeedPackages, catalog.Shipped()...)
	if err != nil {
		t.Fatal(err)
	}
	b := newDBEnv(t, withoutRepository(), withConfig(func(c *Config) { c.Catalog = cat }))
	_, bToken := otherRepository(t, b, testdb.RepositoryLabel(t)+"-b.example.com")
	add := func() substrate.BundleStatus {
		t.Helper()
		rec := b.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(pkg)+"/add", bToken, nil)
		wantStatus(t, rec, http.StatusOK)
		return decodeJSON[substrate.BundleStatus](t, rec)
	}
	if st := add(); st.Version != first {
		t.Fatalf("the installed copy is at %d, want %d", st.Version, first)
	}
	salt, soup := mintedID("salt"), mintedID("soup")
	wantStatus(t, b.do(t, http.MethodPut, "/api/v1/"+pkg+"/ingredient/"+salt, bToken, map[string]any{"properties": map[string]any{"name": "salt"}}), http.StatusCreated)
	wantStatus(t, b.do(t, http.MethodPut, "/api/v1/"+pkg+"/recipe/"+soup, bToken, map[string]any{"properties": map[string]any{
		"name": "soup", "main": map[string]any{"ref": pkg + "/ingredient/" + salt},
	}}), http.StatusCreated)

	// Ada renames her repository: the published name, the document's
	// address and the copy installed elsewhere stay as they were.
	wantStatus(t, a.do(t, http.MethodPost, "/repository/rename", "", substrate.RenameRepositoryRequest{
		Identity: pub, Repository: pub, Password: renamePassword, Name: testdb.RepositoryLabel(t) + "-lovelace.example.com",
	}), http.StatusOK)
	wantStatus(t, a.do(t, http.MethodGet, "/api/v1/"+pkg+"/recipe/"+bread, a.token, nil), http.StatusOK)
	wantStatus(t, a.do(t, http.MethodGet, "/published/"+id+"/"+pub, "", nil), http.StatusOK)

	// The second release: a recipe says how many it serves.
	wantStatus(t, a.do(t, http.MethodPost, "/api/v1/vocabulary/apply", a.token, map[string]any{"documents": []any{
		vocabulary.KindManifest(pkg, map[string]any{"singular": "recipe"}, map[string]any{
			"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"main":   map[string]any{"type": "reference", "kind": pkg + "/ingredient"},
				"serves": map[string]any{"type": "int"},
			},
		}),
	}}), http.StatusOK)
	rec = a.do(t, http.MethodPost, "/api/v1/packages/release", a.token, substrate.ReleaseInput{Package: pkg})
	wantStatus(t, rec, http.StatusOK)
	second := decodeJSON[substrate.Release](t, rec)
	if second.Version <= first {
		t.Fatalf("the second release is version %d, after %d", second.Version, first)
	}
	wantStatus(t, a.do(t, http.MethodPost, "/api/v1/packages/release", a.token, substrate.ReleaseInput{Package: pkg}), http.StatusConflict)

	// The installed copy takes it once the reader reads the document again.
	later := start.Add(11 * time.Minute)
	clock.Store(&later)
	if st := add(); st.Version != second.Version {
		t.Fatalf("after the release the installed copy is at %d, want %d", st.Version, second.Version)
	}
	wantStatus(t, b.do(t, http.MethodPut, "/api/v1/"+pkg+"/recipe/"+soup, bToken, map[string]any{"properties": map[string]any{"serves": 4}}), http.StatusOK)
}
