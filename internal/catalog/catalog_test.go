package catalog

import (
	"os"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

// realCatalog loads the shipped example bundles from disk (the same tree
// main.go embeds), so the parse is held against the real closures.
func realCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := Load(ProviderRoot(kinds.Bundles()), SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return c
}

func TestCatalogListsShippedBundle(t *testing.T) {
	c := realCatalog(t)
	// A malformed or half-built neighbor is dropped with a warning, not
	// fatal — but the reading-list closure itself must never be the one
	// dropped.
	for _, w := range c.Warnings() {
		if strings.HasPrefix(w, "samples.substrate.reamde.dev/readinglist") {
			t.Fatalf("the reading-list bundle was dropped: %v", w)
		}
	}
	const rlID = "samples.substrate.reamde.dev/readinglist"
	b, ok := c.ByID(rlID)
	if !ok {
		var ids []string
		for _, x := range c.Bundles() {
			ids = append(ids, x.ID)
		}
		t.Fatalf("reading-list bundle %q not in catalog; have %v", rlID, ids)
	}
	if b.Name != "readinglist" {
		t.Errorf("name = %q, want readinglist", b.Name)
	}
	if b.Authority != "samples.substrate.reamde.dev" {
		t.Errorf("authority = %q", b.Authority)
	}
	if b.Package != "readinglist" {
		t.Errorf("package = %q, want readinglist", b.Package)
	}
	if b.Version != 10 {
		t.Errorf("version = %d, want 10", b.Version)
	}
	// Its configuration is a shipped `setting` record, not an input: a bundle
	// with no declared input is the shape decision record 0076 moved to.
	if len(b.Inputs) != 0 {
		t.Errorf("inputs = %v, want none", b.Inputs)
	}
	if b.Description == "" {
		t.Error("description is empty")
	}
}

func TestCatalogDetailEnumeratesTheClosure(t *testing.T) {
	b, ok := realCatalog(t).ByID("samples.substrate.reamde.dev/readinglist")
	if !ok {
		t.Fatal("reading-list bundle missing")
	}
	// The reading-list closure ships two record kinds, four functions, three
	// agents, and writes four triggers, one setting and the empty digest the
	// rollup patches beside them.
	if got := len(b.Closure.Kinds); got != 2 {
		t.Errorf("kinds = %d, want 2 (%v)", got, b.Closure.Kinds)
	}
	if got := len(b.Closure.Functions); got != 4 {
		t.Errorf("functions = %d, want 4 (%v)", got, b.Closure.Functions)
	}
	if got := len(b.Closure.Agents); got != 3 {
		t.Errorf("agents = %d, want 3 (%v)", got, b.Closure.Agents)
	}
	if got := len(b.Closure.Records); got != 6 {
		t.Errorf("records = %d, want 6 (%v)", got, b.Closure.Records)
	}
	for _, r := range b.Closure.Records {
		if r.Kind == "" || r.ID == "" {
			t.Errorf("a shipped record needs both halves of its identity: %+v", r)
		}
	}
	if !contains(b.Closure.Kinds, "samples.substrate.reamde.dev/readinglist/digest") {
		t.Errorf("digest kind not in the closure: %v", b.Closure.Kinds)
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
// example is the case that proves it: its closure is agents, and the two
// keyless llm/provider rows it writes are the things the reader is then told
// to go and key. A preview that named only the declarations showed that
// bundle as "six agents and nothing else", which is what it looked like on
// the Registry page.
func TestCatalogPreviewsTheRecordsAnInstallWrites(t *testing.T) {
	b, ok := realCatalog(t).ByID("samples.substrate.reamde.dev/llm")
	if !ok {
		t.Fatal("llm bundle missing")
	}
	if got := len(b.Closure.Agents); got != 6 {
		t.Errorf("agents = %d, want 6 (%v)", got, b.Closure.Agents)
	}
	// `default` is the Anthropic row: every shipped sample agent names that
	// id, so the row that makes them runnable is the one shipped at it.
	want := map[string]bool{"default": true, "openai": true}
	got := map[string]bool{}
	for _, r := range b.Closure.Records {
		if r.Kind != "substrate.reamde.dev/llm/provider" {
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

// The TIER is the tree a closure came from, never a guess from its authority
// (decision record 0048): kinds/providers.substrate.reamde.dev is a provider,
// samples/ is a sample. It decides which door a bundle takes, so a wrong
// answer here sends a user to install what they should own.
func TestCatalogTierIsTheTreeTheClosureCameFrom(t *testing.T) {
	c := realCatalog(t)
	cases := map[string]string{
		"providers.substrate.reamde.dev/google":    substrate.TierProvider,
		"providers.substrate.reamde.dev/linear":    substrate.TierProvider,
		"samples.substrate.reamde.dev/readinglist": substrate.TierSample,
		"samples.substrate.reamde.dev/tasks":       substrate.TierSample,
	}
	for id, want := range cases {
		b, ok := c.ByID(id)
		if !ok {
			t.Fatalf("bundle %q not in catalog", id)
		}
		if b.Tier != want {
			t.Errorf("%s tier = %q, want %q", id, b.Tier, want)
		}
	}
	// Every shipped bundle carries one of the two: an entry with an empty
	// tier is a closure no door serves.
	for _, b := range c.Bundles() {
		if b.Tier != substrate.TierProvider && b.Tier != substrate.TierSample {
			t.Errorf("%s tier = %q, want provider or sample", b.ID, b.Tier)
		}
	}
}

// A sample's shipped id addresses the catalog entry; what LANDS is its package
// under the repository's own authority, which is the id the bundle status
// carries afterwards. A provider keeps the id it publishes.
func TestLandedIDRehomesASampleAndLeavesAProvider(t *testing.T) {
	c := realCatalog(t)
	sample, _ := c.ByID("samples.substrate.reamde.dev/tasks")
	if got, want := sample.LandedID("ada.example.com"), "ada.example.com/tasks"; got != want {
		t.Errorf("sample lands as %q, want %q", got, want)
	}
	provider, _ := c.ByID("providers.substrate.reamde.dev/google")
	if got, want := provider.LandedID("ada.example.com"), provider.ID; got != want {
		t.Errorf("provider lands as %q, want %q", got, want)
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
	must("notabundle.bundles.substrate.reamde.dev", "kind: substrate.reamde.dev/core/kind\nmetadata: {id: x.y}\ndata: {authority: y}\n")
	must("broken.bundles.substrate.reamde.dev", "authority: substrate.reamde.dev/core\n  bad: [indent")
	c, err := Load(ProviderRoot(os.DirFS(dir)))
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
