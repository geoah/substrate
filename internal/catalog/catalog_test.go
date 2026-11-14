package catalog

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/substrate"
)

// realCatalog loads the shipped example bundles from disk (the same tree
// main.go embeds), so the parse is held against the real closures.
func realCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := Load(Shipped()...)
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
	if b.Version != 15 {
		t.Errorf("version = %d, want 15", b.Version)
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
// bundle as "three agents and nothing else", which is what it looked like on
// the Registry page.
func TestCatalogPreviewsTheRecordsAnInstallWrites(t *testing.T) {
	b, ok := realCatalog(t).ByID("samples.substrate.reamde.dev/llm")
	if !ok {
		t.Fatal("llm bundle missing")
	}
	if got := len(b.Closure.Agents); got != 3 {
		t.Errorf("agents = %d, want 3 (%v)", got, b.Closure.Agents)
	}
	// `openai` is the row every shipped sample agent names: the seed writes
	// it keyless, and importing this bundle is what makes the agents runnable
	// on a repository that was born before the seed existed.
	want := map[string]bool{"openai": true, "anthropic": true, "gemini": true}
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

// Who KEEPS a closure is the tree it came from, never a guess from its
// authority (decision record 0048): kinds/ is kept by its publisher, samples/
// by the repository. It decides where adding a bundle lands it, so a wrong
// answer here hands a user a publisher's package they should own.
func TestCatalogKeeperIsTheTreeTheClosureCameFrom(t *testing.T) {
	c := realCatalog(t)
	cases := map[string]string{
		"providers.substrate.reamde.dev/google":    substrate.KeptByPublisher,
		"providers.substrate.reamde.dev/linear":    substrate.KeptByPublisher,
		"substrate.reamde.dev/clinical":            substrate.KeptByPublisher,
		"samples.substrate.reamde.dev/readinglist": substrate.KeptByRepository,
		"samples.substrate.reamde.dev/tasks":       substrate.KeptByRepository,
	}
	for id, want := range cases {
		b, ok := c.ByID(id)
		if !ok {
			t.Fatalf("bundle %q not in catalog", id)
		}
		if b.KeptBy != want {
			t.Errorf("%s keptBy = %q, want %q", id, b.KeptBy, want)
		}
	}
	// Every shipped bundle carries one of the two: an entry nobody keeps is a
	// closure adding could not land.
	for _, b := range c.Bundles() {
		if b.KeptBy != substrate.KeptByPublisher && b.KeptBy != substrate.KeptByRepository {
			t.Errorf("%s keptBy = %q, want publisher or repository", b.ID, b.KeptBy)
		}
	}
}

// What a package is FOR is read from its declarations, apart from its keeper
// (decision record 0238): the console lists a provider on Providers, an
// add-on with the provider it extends, and every other published package
// with the collections. A catalogue listed as a provider is the bug this
// record fixed: `clinical` read as an account to connect.
func TestCatalogSaysWhatEachPackageIsFor(t *testing.T) {
	c := realCatalog(t)
	for _, tc := range []struct {
		id       string
		provider bool
		brings   []string
		addonFor string
	}{
		{id: "providers.substrate.reamde.dev/google", provider: true, brings: []string{substrate.BringsAccount, substrate.BringsTools, substrate.BringsCollections}},
		// WHOOP requires health and is still no add-on: it has an account.
		{id: "providers.substrate.reamde.dev/whoop", provider: true, brings: []string{substrate.BringsAccount, substrate.BringsCollections, substrate.BringsMetrics}},
		{id: "providers.substrate.reamde.dev/firecrawl", provider: true, brings: []string{substrate.BringsTools}},
		// Apple declares no account (its devices push) and is a provider
		// because the binary ships it among its providers.
		{id: "providers.substrate.reamde.dev/apple", provider: true, brings: []string{substrate.BringsCollections}},
		{id: "providers.substrate.reamde.dev/slacklive", provider: true, addonFor: "providers.substrate.reamde.dev/slack"},
		{id: "substrate.reamde.dev/clinical", brings: []string{substrate.BringsCollections}},
		{id: "substrate.reamde.dev/health", brings: []string{substrate.BringsCollections, substrate.BringsMetrics}},
		{id: "substrate.reamde.dev/environment", brings: []string{substrate.BringsMetrics}},
		{id: "samples.substrate.reamde.dev/notes", brings: []string{substrate.BringsTools, substrate.BringsCollections, substrate.BringsAgents}},
		{id: "samples.substrate.reamde.dev/llm", brings: []string{substrate.BringsAgents}},
	} {
		b, ok := c.ByID(tc.id)
		if !ok {
			t.Fatalf("bundle %q not in catalog", tc.id)
		}
		if b.Provider != tc.provider || b.AddonFor != tc.addonFor || !slices.Equal(b.Brings, tc.brings) {
			t.Errorf("%s: provider %v, addonFor %q, brings %v; want %v, %q, %v",
				tc.id, b.Provider, b.AddonFor, b.Brings, tc.provider, tc.addonFor, tc.brings)
		}
	}
	for _, b := range c.Bundles() {
		if b.KeptBy == substrate.KeptByRepository && (b.Provider || b.AddonFor != "") {
			t.Errorf("sample %s reads as a provider or an add-on", b.ID)
		}
	}
}

// A package from an authority document is a provider only when it brings an
// account, and an add-on for the provider its publisher's document names,
// else for the first provider it requires.
func TestPublishedPackageSaysWhatItIsFor(t *testing.T) {
	account := "---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: example.com/notes/account\ndata:\n  authority: example.com\n  package: notes\n  names:\n    singular: account\n  traits: [substrate.reamde.dev/core/accountconfig]\n  properties:\n    tokenRef:\n      type: secret\n    tokenStatus:\n      type: string\n    grantedScopes:\n      type: string\n"
	plain, err := ParseClosure([]byte(publishedClosure("example.com/notes", 1)), "example.com/notes", 1)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Provider || plain.KeptBy != substrate.KeptByPublisher {
		t.Errorf("a closure with no account: provider %v, keptBy %q", plain.Provider, plain.KeptBy)
	}
	withAccount := strings.Replace(publishedClosure("example.com/notes", 1), "installs: [example.com/notes/item]", "installs: [example.com/notes/item, example.com/notes/account]", 1) + account
	connected, err := ParseClosure([]byte(withAccount), "example.com/notes", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !connected.Provider || !slices.Contains(connected.Brings, substrate.BringsAccount) {
		t.Errorf("a closure with an account: provider %v, brings %v", connected.Provider, connected.Brings)
	}

	// The document names provider.example/base. Where the binary ships no such
	// provider the console could list it under, the package is no add-on and
	// keeps its own place; the approval still reads the document's word.
	c, _ := publishedFixture(t, 1)
	unlisted, err := c.Resolve(context.Background(), "example.com/notes", 1)
	if err != nil {
		t.Fatal(err)
	}
	if unlisted.AddonFor != "" || unlisted.Origin().AddonFor != "provider.example/base" {
		t.Errorf("a named package the binary does not ship as a provider: addonFor %q, origin's %q", unlisted.AddonFor, unlisted.Origin().AddonFor)
	}
	shipped, err := LoadWithReader(c.reader, nil, ProviderRoot(fstest.MapFS{
		"base/bundle.yaml": {Data: []byte(publishedClosure("provider.example/base", 1))},
	}))
	if err != nil {
		t.Fatal(err)
	}
	listed, err := shipped.Resolve(context.Background(), "example.com/notes", 1)
	if err != nil {
		t.Fatal(err)
	}
	if listed.AddonFor != "provider.example/base" {
		t.Errorf("the document names a provider the binary ships; addonFor = %q", listed.AddonFor)
	}
}

// A sample's shipped id addresses the catalog entry; what LANDS is its package
// in the repository's local namespace, the same in every repository, which is
// the id the bundle status carries afterwards (decision record 0259). A
// provider keeps the id it publishes.
func TestLandedIDCopiesASampleIntoTheLocalNamespaceAndLeavesAProvider(t *testing.T) {
	c := realCatalog(t)
	sample, _ := c.ByID("samples.substrate.reamde.dev/tasks")
	if got, want := sample.LandedID(), "~/tasks"; got != want {
		t.Errorf("sample lands as %q, want %q", got, want)
	}
	provider, _ := c.ByID("providers.substrate.reamde.dev/google")
	if got, want := provider.LandedID(), provider.ID; got != want {
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

// A closure answers what installing it brings as typed facts, read from the
// documents the install applies: each kind with its label and purpose, each
// function with what it may write, each trigger as it ships.
func TestAClosureAnswersWhatItBringsAsFacts(t *testing.T) {
	b, ok := realCatalog(t).ByID("providers.substrate.reamde.dev/applehealth")
	if !ok {
		t.Fatal("the catalog ships no applehealth")
	}
	account, ok := b.Kind("providers.substrate.reamde.dev/applehealth/account")
	if !ok || account.Purpose != "internal" || account.Definition["traits"] == nil {
		t.Fatalf("the account kind = %+v", account)
	}
	if len(b.Kinds()) < 2 {
		t.Fatalf("applehealth declares %d kinds", len(b.Kinds()))
	}
	adapter := -1
	for i, f := range b.Functions() {
		if f.ID == "providers.substrate.reamde.dev/applehealth/adaptapplehealth" {
			adapter = i
		}
	}
	if adapter < 0 || !contains(b.Functions()[adapter].Writes, "substrate.reamde.dev/observations/observation") {
		t.Fatalf("applehealth's functions = %+v", b.Functions())
	}
	hook, ok := b.Trigger("applehealth-webhook")
	if !ok || !hook.Webhook || hook.Enabled ||
		hook.Callable != "substrate.reamde.dev/core/function/providers.substrate.reamde.dev/applehealth/applehealthingest" {
		t.Fatalf("the webhook trigger = %+v", hook)
	}
	if record, ok := b.Trigger("applehealth-to-health"); !ok || record.Webhook || !record.Enabled {
		t.Fatalf("the record trigger = %+v", record)
	}
}
