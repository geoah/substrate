package catalog

import (
	"os"
	"strings"
	"testing"

	"github.com/geoah/substrate/kinds"
)

// realCatalog loads the shipped example bundles from disk (the same tree
// main.go embeds), so the parse is held against the real closures.
func realCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := Load(kinds.Bundles())
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return c
}

func TestCatalogListsShippedBundle(t *testing.T) {
	c := realCatalog(t)
	// A malformed or half-built neighbor is dropped with a warning, not
	// fatal — but the web closure itself must never be the one dropped.
	for _, w := range c.Warnings() {
		if strings.HasPrefix(w, "web.bundles.substrate.reamde.dev") {
			t.Fatalf("the web bundle was dropped: %v", w)
		}
	}
	const webID = "web.bundles.substrate.reamde.dev/web"
	b, ok := c.ByID(webID)
	if !ok {
		var ids []string
		for _, x := range c.Bundles() {
			ids = append(ids, x.ID)
		}
		t.Fatalf("web bundle %q not in catalog; have %v", webID, ids)
	}
	if b.Name != "web" {
		t.Errorf("name = %q, want web", b.Name)
	}
	if b.Authority != "web.bundles.substrate.reamde.dev" {
		t.Errorf("authority = %q", b.Authority)
	}
	if b.Version != "v1alpha1" {
		t.Errorf("version = %q, want v1alpha1", b.Version)
	}
	if _, ok := b.Inputs["connector"]; !ok {
		t.Errorf("inputs = %v, want a connector input", b.Inputs)
	}
	if b.Description == "" {
		t.Error("description is empty")
	}
}

func TestCatalogDetailEnumeratesResources(t *testing.T) {
	b, ok := realCatalog(t).ByID("web.bundles.substrate.reamde.dev/web")
	if !ok {
		t.Fatal("web bundle missing")
	}
	// The web closure ships two record types, four functions, three agents;
	// its delivery wiring is four triggers.
	if got := len(b.Resources.Kinds); got != 2 {
		t.Errorf("kinds = %d, want 2 (%v)", got, b.Resources.Kinds)
	}
	if got := len(b.Resources.Functions); got != 4 {
		t.Errorf("functions = %d, want 4 (%v)", got, b.Resources.Functions)
	}
	if got := len(b.Resources.Agents); got != 3 {
		t.Errorf("agents = %d, want 3 (%v)", got, b.Resources.Agents)
	}
	if got := len(b.Resources.Triggers); got != 4 {
		t.Errorf("triggers = %d, want 4 (%v)", got, b.Resources.Triggers)
	}
	if !contains(b.Resources.Kinds, "web.bundles.substrate.reamde.dev/config") {
		t.Errorf("config kind not in resources: %v", b.Resources.Kinds)
	}
	// Each kind's own description rides along: before an install there is no
	// registry entry to look one up in, and "what does this ship" is the
	// question the preview exists to answer.
	for _, ident := range b.Resources.Kinds {
		if b.Resources.KindDescriptions[ident] == "" {
			t.Errorf("%s ships no description in the closure preview", ident)
		}
	}
}

// The integration facet is curated catalog metadata keyed by bundle id, NOT
// derived from the closure: the google provider bundle carries integration=true
// and the URL-harvester web bundle integration=false.
func TestCatalogIntegrationFacetIsCurated(t *testing.T) {
	c := realCatalog(t)
	cases := map[string]bool{
		"google.bundles.substrate.reamde.dev/google": true,
		"web.bundles.substrate.reamde.dev/web":       false,
	}
	for id, want := range cases {
		b, ok := c.ByID(id)
		if !ok {
			t.Fatalf("bundle %q not in catalog", id)
		}
		if b.Integration != want {
			t.Errorf("%s integration = %v, want %v", id, b.Integration, want)
		}
	}
}

// A directory with no bundle document is not an entry; a malformed one is
// dropped with a warning rather than failing the whole catalog.
func TestCatalogSkipsNonBundleAndMalformed(t *testing.T) {
	dir := t.TempDir()
	must := func(name, body string) {
		if err := os.MkdirAll(dir+"/"+name, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir+"/"+name+"/bundle.yaml", []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("notabundle.bundles.substrate.reamde.dev", "kind: core.substrate.reamde.dev/kind\nmetadata: {id: x.y}\ndata: {authority: y}\n")
	must("broken.bundles.substrate.reamde.dev", "authority: core.substrate.reamde.dev\n  bad: [indent")
	c, err := Load(os.DirFS(dir))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(c.Bundles()) != 0 {
		t.Errorf("expected no bundles, got %d", len(c.Bundles()))
	}
	if len(c.Warnings()) != 1 {
		t.Errorf("expected 1 warning (the malformed dir), got %v", c.Warnings())
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
