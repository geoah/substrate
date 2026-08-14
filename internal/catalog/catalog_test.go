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
	if b.Version != "v1alpha3" {
		t.Errorf("version = %q, want v1alpha3", b.Version)
	}
	if _, ok := b.Inputs["connector"]; !ok {
		t.Errorf("inputs = %v, want a connector input", b.Inputs)
	}
	if b.Description == "" {
		t.Error("description is empty")
	}
}

func TestCatalogDetailEnumeratesTheClosure(t *testing.T) {
	b, ok := realCatalog(t).ByID("web.bundles.substrate.reamde.dev/web")
	if !ok {
		t.Fatal("web bundle missing")
	}
	// The web closure ships two record kinds, four functions, three agents,
	// and writes four trigger records beside them.
	if got := len(b.Closure.Kinds); got != 2 {
		t.Errorf("kinds = %d, want 2 (%v)", got, b.Closure.Kinds)
	}
	if got := len(b.Closure.Functions); got != 4 {
		t.Errorf("functions = %d, want 4 (%v)", got, b.Closure.Functions)
	}
	if got := len(b.Closure.Agents); got != 3 {
		t.Errorf("agents = %d, want 3 (%v)", got, b.Closure.Agents)
	}
	if got := len(b.Closure.Records); got != 4 {
		t.Errorf("records = %d, want 4 (%v)", got, b.Closure.Records)
	}
	for _, r := range b.Closure.Records {
		if r.Kind == "" || r.ID == "" {
			t.Errorf("a shipped record needs both halves of its identity: %+v", r)
		}
	}
	if !contains(b.Closure.Kinds, "web.bundles.substrate.reamde.dev/config") {
		t.Errorf("config kind not in the closure: %v", b.Closure.Kinds)
	}
	// Each kind's own description rides along: before an install there is no
	// registry entry to look one up in, and "what does this ship" is the
	// question the preview exists to answer.
	for _, ident := range b.Closure.Kinds {
		if b.Closure.KindDescriptions[ident] == "" {
			t.Errorf("%s ships no description in the closure preview", ident)
		}
	}
}

// A bundle's DATA records are half of what installing it does, and the llm
// example is the case that proves it: its whole closure is three agents, and
// the two keyless llmprovider rows it writes are the things the reader is then
// told to go and key. A preview that named only the declarations showed that
// bundle as "three agents and nothing else", which is what it looked like on
// the Registry page.
func TestCatalogPreviewsTheRecordsAnInstallWrites(t *testing.T) {
	b, ok := realCatalog(t).ByID("llm.examples.substrate.reamde.dev/llm")
	if !ok {
		t.Fatal("llm bundle missing")
	}
	if got := len(b.Closure.Agents); got != 3 {
		t.Errorf("agents = %d, want 3 (%v)", got, b.Closure.Agents)
	}
	want := map[string]bool{"anthropic": true, "openai": true}
	got := map[string]bool{}
	for _, r := range b.Closure.Records {
		if r.Kind != "core.substrate.reamde.dev/llmprovider" {
			t.Errorf("unexpected shipped record %+v", r)
			continue
		}
		got[r.ID] = true
	}
	for id := range want {
		if !got[id] {
			t.Errorf("the %s provider row is invisible in the preview: %+v", id, b.Closure.Records)
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
