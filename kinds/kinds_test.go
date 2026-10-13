package kinds_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

// The binary ships the vocabulary, so the embed pattern is part of the
// contract: a directory the pattern misses is an authority that silently stops
// existing in production while every other test still passes.
func TestEveryManifestOnDiskIsEmbedded(t *testing.T) {
	for _, tree := range []struct {
		dir  string
		fsys fs.FS
	}{
		{".", kinds.All()},
		{"../samples", samples.Samples()},
	} {
		var want []string
		err := filepath.WalkDir(tree.dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
				return err
			}
			want = append(want, filepath.ToSlash(path))
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s on disk: %v", tree.dir, err)
		}
		var got []string
		err = fs.WalkDir(tree.fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
				return err
			}
			got = append(got, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walk the embedded %s: %v", tree.dir, err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: embedded %d manifests, %d on disk", tree.dir, len(got), len(want))
		}
	}
}

// Seed and Bundles partition the tree: every authority is in exactly one, and
// an authority directory that appeared in neither would be shipped and unread.
func TestSeedAndBundlesPartitionTheTree(t *testing.T) {
	seed := rootNames(t, kinds.Seed())
	if len(seed) != 1 || seed[0] != kinds.SeedAuthority {
		t.Fatalf("seed root = %v, want just %s", seed, kinds.SeedAuthority)
	}
	bundles := rootNames(t, kinds.Bundles())
	if len(bundles) == 0 {
		t.Fatal("no bundle authorities")
	}
	for _, name := range bundles {
		if name == kinds.SeedAuthority {
			t.Errorf("%s is in both halves", name)
		}
	}
	onDisk, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the tree on disk: %v", err)
	}
	var dirs int
	for _, e := range onDisk {
		if e.IsDir() {
			dirs++
		}
	}
	if len(seed)+len(bundles) != dirs {
		t.Errorf("seed(%d) + bundles(%d) != %d authorities on disk",
			len(seed), len(bundles), dirs)
	}
}

// The seed loads as a registry, and the bundles load as a catalog: the two
// views are handed to two different readers, and each has to be the shape its
// reader walks.
func TestBothViewsLoad(t *testing.T) {
	r, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	if len(r.Packages()) == 0 || len(r.Kinds()) == 0 {
		t.Fatalf("the seed registry is empty: %v", r.Packages())
	}
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	if len(cat.Bundles()) == 0 {
		t.Fatal("the shipped catalog is empty")
	}
}

// TestShippedCallableActorsAreDistinct: a function's or an agent's writing hand
// is `function:<name>` and the trigger loop guard keys on it,
// so two callables that share a LOCAL name share an actor — and each one's
// trigger silently drops the other's writes as though they were its own echo.
// The actor grammar has no room for the authority, so the shipped catalog has
// to keep its own names apart; installing two third-party bundles that collide
// is a known issue, shipping two that do is a bug.
// THE SHIPPED TREE IS AUTHORED IN THE DECLARED SPELLING. The loader admits one
// spelling per key and refuses each retired one by name, so a tree document
// left in an old spelling does not store as something other than what it says:
// it does not load at all. Nothing understands those spellings any more.
//
// The test still earns its keep for what it says when that happens. A load
// failure names one key inside one closure; this names the document (its kind and
// its id) and what took the spelling's place, and it is the one list of every
// retired spelling a rebase could put back into the tree.
func TestTheTreeIsAuthoredInTheDeclaredSpelling(t *testing.T) {
	for _, d := range readTreeDocuments(t) {
		at := d.Kind + " " + d.ID
		if _, wrapped := d.Data["capabilities"]; wrapped {
			t.Errorf("%s: the capability envelope rides `data` itself now", at)
		}
		for _, side := range []string{"input", "output"} {
			if _, nested := d.Data[side]; nested {
				t.Errorf("%s: `%s` is a flat argument list now (`arguments`/`returns`)", at, side)
			}
		}
		for i, tv := range asList(d.Data["tools"]) {
			if _, bare := tv.(string); bare {
				t.Errorf("%s: tools[%d] is a bare string — an entry names its arm ({builtin} or {callable})", at, i)
			}
		}
		if one, has := d.Data["oneOf"]; has {
			if _, isList := one.([]any); !isList {
				t.Errorf("%s: `oneOf` is the variant LIST now", at)
			}
		}
		for name, rv := range asMapping(d.Data["map"]) {
			if _, bare := rv.(string); bare {
				t.Errorf("%s: map rule %q is a bare path — a rule is `{path}`", at, name)
			}
		}
		for i, iv := range asList(d.Data["indices"]) {
			if _, bare := iv.([]any); bare {
				t.Errorf("%s: indices[%d] is a bare list — an index names its properties", at, i)
			}
		}
		for toggle, sv := range asMapping(asMapping(d.Data["oauth2"])["featureScopes"]) {
			if _, bare := sv.([]any); bare {
				t.Errorf("%s: featureScopes[%q] is a bare list — the scopes take a field", at, toggle)
			}
		}
	}
}

// THE TWO STATEMENTS ABOUT WHO OWNS A PROPERTY AGREE. A declaration row carries
// what its document declares plus what the ENGINE stamps, and each half is said
// once: the loader's admitted `data` keys (vocabulary.DeclarationDataKeys) are the
// document's, and `managed: true` on the core declaration is the engine's. A row
// reads back as a document by whitelisting the first set, so a managed property
// that is ALSO a document key would be read back as authored — which is right
// for the two that genuinely are (a kind and an authority may pin their own
// `version`) and wrong for anything else, silently.
//
// This is the pin the engine does not have to repeat: without it, a new managed
// property spelled like a document key would be handed to the loader as authored
// content and refused as an unknown key at the next open.
func TestManagedPropertiesAreNotDocumentKeys(t *testing.T) {
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	// The two deliberate duals: stamped when absent, authored when present.
	dual := map[string]bool{
		"kind.version":      true,
		"package.version":   true,
		"authority.version": true,
	}
	for _, short := range []string{
		vocabulary.DocAuthority, vocabulary.DocPackage, vocabulary.DocActor, vocabulary.DocKind, vocabulary.DocTrait,
		vocabulary.DocPropertyType, vocabulary.DocRecordMapping, vocabulary.DocFunction,
		vocabulary.DocAgent, vocabulary.DocBundle,
	} {
		ty, ok := reg.ByIdentity(vocabulary.CoreKind(short))
		if !ok {
			t.Fatalf("core declares no %s", short)
		}
		keys := vocabulary.DeclarationDataKeys(short)
		for _, name := range ty.PropOrder {
			p := ty.Props[name]
			if !p.Managed || !keys[name] || dual[short+"."+name] {
				continue
			}
			t.Errorf("%s.%s is managed AND a document key — a row would read it back as authored; drop the marker or the key, or add it to the duals with a reason",
				short, name)
		}
	}
}

// EVERY ORIGIN THE ENGINE WRITES IS A DECLARED VALUE. The projection stamps
// `source` on a declaration row from the package it is projecting
// (engine/vocabularywrite.go packageDeclarations), and the row is validated
// against the declaration it lands under: an origin the enum does not list is
// a write the engine refuses, so a seed, an install or the provider promotion
// of decision record 0048 would fail on a kind that fell behind. The three
// values are the vocabulary package's constants, named here so adding a fourth
// without declaring it fails in the tree that has to carry it.
func TestEveryDeclarationKindAdmitsEveryOrigin(t *testing.T) {
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	origins := []string{vocabulary.SourceBuiltin, vocabulary.SourcePublished, vocabulary.SourceInstalled}
	// The declaration kinds that carry an origin at all. A function and an
	// agent do not: `source` on a function is its BODY, and the projection
	// stamps neither.
	carriers := []string{
		vocabulary.DocAuthority, vocabulary.DocPackage, vocabulary.DocActor, vocabulary.DocKind,
		vocabulary.DocTrait, vocabulary.DocPropertyType, vocabulary.DocRecordMapping,
	}
	for _, short := range carriers {
		ty, ok := reg.ByIdentity(vocabulary.CoreKind(short))
		if !ok {
			t.Fatalf("core declares no %s", short)
		}
		p, ok := ty.Props["source"]
		if !ok {
			t.Errorf("%s declares no source property, and the projection stamps one on its rows", short)
			continue
		}
		declared := map[string]bool{}
		for _, v := range p.ValueStrings() {
			declared[v] = true
		}
		for _, origin := range origins {
			if !declared[origin] {
				t.Errorf("%s.source does not admit %q: a %s package's projection would be refused", short, origin, origin)
			}
		}
		if len(declared) != len(origins) {
			t.Errorf("%s.source admits %v, want exactly %v", short, p.ValueStrings(), origins)
		}
	}
}

// A KIND TITLES ITSELF (decision record 0016). The `title` every record
// carries is one untyped column the engine derives from the kind's
// displayTemplate; a kind without one stores whatever a writer sent there
// instead, which is a heading with no type, no description and no per-kind
// filter. The kinds below still do that, each waiting on the work that gives
// it a template, and the list is here so a NEW kind cannot quietly join them:
// a kind that ships without a displayTemplate fails this test, and one that
// gains a template has to leave the list.
func TestEveryKindDeclaresADisplayTemplate(t *testing.T) {
	// The three sample kinds that still carry the writer's title in the
	// built-in slot and move with its retirement (issue 68). The provider
	// mirrors that used to sit here declare a displayTemplate since the
	// bundles were replaced with the mneme-tested versions.
	onTheSlot := map[string]bool{
		"samples.substrate.reamde.dev/firecrawl/webdocument": true,
		"samples.substrate.reamde.dev/notes/note":            true,
		"samples.substrate.reamde.dev/readinglist/page":      true,
	}
	declared := map[string]bool{}
	for _, d := range readTreeDocuments(t) {
		if d.Kind != vocabulary.DocKind {
			continue
		}
		declared[d.ID] = true
		tmpl, has := d.Data["displayTemplate"].(string)
		if has && tmpl != "" {
			if onTheSlot[d.ID] {
				t.Errorf("%s declares a displayTemplate now: drop it from the list this test holds", d.ID)
			}
			continue
		}
		if !onTheSlot[d.ID] {
			t.Errorf("%s declares no displayTemplate, so its title is whatever a writer puts in the built-in slot; declare the heading as a property and render it (decision record 0016)", d.ID)
		}
	}
	for id := range onTheSlot {
		if !declared[id] {
			t.Errorf("%s is listed as titling itself from the built-in slot, but the tree declares no such kind", id)
		}
	}
}

// A SHIPPED KIND'S PURPOSE IS ONE OF THE THREE, AND THE SEED IS MACHINERY
// (decision record 0133). The loader already refuses any other value; this
// reads the documents so the rule holds for every file in both trees, and adds
// the one the loader cannot know: a seeded kind is the substrate's own
// machinery, so an unclassified core or llm kind would read as primary and
// land in every console's navigation.
func TestEveryShippedPurposeIsValidAndTheSeedIsInternal(t *testing.T) {
	valid := map[string]bool{
		vocabulary.PurposePrimary:    true,
		vocabulary.PurposeSupporting: true,
		vocabulary.PurposeInternal:   true,
	}
	declared := 0
	for _, d := range readTreeDocuments(t) {
		if d.Kind != vocabulary.DocKind {
			continue
		}
		raw, has := d.Data["purpose"]
		if has {
			declared++
			if v, ok := raw.(string); !ok || !valid[v] {
				t.Errorf("%s: purpose %v is not primary, supporting or internal", d.ID, raw)
			}
		}
		if authority, _, _ := vocabulary.SplitKindRef(d.ID); authority == kinds.SeedAuthority && raw != vocabulary.PurposeInternal {
			t.Errorf("%s is seeded machinery and declares purpose %v; declare `purpose: internal`", d.ID, raw)
		}
	}
	// A reader that stopped finding the key would pass this test forever.
	if declared == 0 {
		t.Fatal("read no purpose at all: the documents no longer spell it as data.purpose")
	}
}

// EVERY STAMP IN THE TREE NAMES A DECLARED DATETIME PROPERTY. A transition's
// stamp writes a stored property, and the loader SYNTHESIZES an implicit
// datetime one where the author declared none — it has to, because stored
// declarations written before targets were declarable must keep parsing at open
// (vocabulary/load.go). So a missing target in the shipped tree is invisible to
// the loader, and this reads the documents rather than the registry.
//
// The rule is absolute here and nowhere else: the tree is the vocabulary this
// binary ships, an undeclared target hides a stored datetime from every reader
// of the declaration, and the fix is one `type: datetime` property.
func TestEveryStampTargetIsADeclaredDatetime(t *testing.T) {
	stamps := 0
	for _, d := range readTreeDocuments(t) {
		if d.Kind != vocabulary.DocKind {
			continue
		}
		props := asMapping(d.Data["properties"])
		for name, pv := range props {
			// A state property carries its machine INLINE (states, initial,
			// transitions), so the transitions list is the machine.
			for _, tv := range asList(asMapping(pv)["transitions"]) {
				for target := range asMapping(asMapping(tv)["stamps"]) {
					stamps++
					decl := asMapping(props[target])
					if len(decl) == 0 {
						t.Errorf("%s: %s stamps %q, which the kind does not declare; declare it (type: datetime)",
							d.ID, name, target)
						continue
					}
					if decl["type"] != "datetime" || decl["repeated"] == true || decl["keyed"] == true {
						t.Errorf("%s: %s stamps %q, so it must be a single-valued datetime, not %v",
							d.ID, name, target, decl["type"])
					}
				}
			}
		}
	}
	// A reader that stopped finding stamps would pass this test forever.
	if stamps == 0 {
		t.Fatal("read no stamps at all: the documents no longer spell a machine's transitions this way")
	}
}

// readTreeDocuments parses every DECLARATION document in the tree.
func readTreeDocuments(t *testing.T) []vocabulary.Document {
	t.Helper()
	var docs []vocabulary.Document
	// BOTH SHIPPED TREES: the kinds/ tree and the samples/ tree are one
	// vocabulary as far as these rules go, and a rule that read only one of
	// them would stop covering half the shipped packages the day they moved.
	for _, fsys := range []fs.FS{kinds.All(), samples.Samples()} {
		err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
				return err
			}
			raw, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			parsed, err := vocabulary.ParseStream(raw)
			if err != nil {
				// A bundle's delivery wiring (trigger records) sits in the same
				// tree and is not a declaration: those files parse nowhere near
				// here.
				return nil
			}
			docs = append(docs, parsed...)
			return nil
		})
		if err != nil {
			t.Fatalf("read the tree: %v", err)
		}
	}
	if len(docs) == 0 {
		t.Fatal("the tree holds no documents")
	}
	return docs
}

func asList(v any) []any {
	out, _ := v.([]any)
	return out
}

func asMapping(v any) map[string]any {
	out, _ := v.(map[string]any)
	return out
}

func TestShippedCallableActorsAreDistinct(t *testing.T) {
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	seen := map[string]string{}
	for _, b := range cat.Bundles() {
		callables := append(append([]string{}, b.Closure.Functions...), b.Closure.Agents...)
		for _, id := range callables {
			actor := "function:" + vocabulary.KindName(id)
			if prev, clash := seen[actor]; clash {
				t.Errorf("%s and %s both write as %s — one of them has to be renamed", prev, id, actor)
				continue
			}
			seen[actor] = id
		}
	}
}

// A bundle's closure is validated at INSTALL time: catalog.Load only buckets
// decoded YAML, so a broken declaration would ship as nothing more than a
// warning TestBothViewsLoad never reads. EVERY shipped package installs here,
// providers and samples alike — the same loader refuses both (a hyphen in a
// callable's name, an id that is not its package), and skipping them is how a
// shipped example reached a release refusing to install. Requires go first,
// into ONE seed registry, through the same BuildPackages+InstallAll pair
// admission runs, so the whole shipped set has to coexist without a database.
//
// PROVIDERS GO FIRST, then the samples in `requires:` order, because a
// sample's SUGGESTED MAPPINGS name a provider's mirror as their source
// (decision record 0049): this is the install order in which they all land,
// and the mapping set at the end is what they projected.
func TestShippedBundlesInstallOnTheSeed(t *testing.T) {
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	// A malformed directory is dropped into Warnings, not an error, and a
	// dropped bundle would silently vanish from the loop below.
	if ws := cat.Warnings(); len(ws) != 0 {
		t.Fatalf("the shipped catalog carries warnings: %v", ws)
	}
	byPackage := map[string]*catalog.Bundle{}
	for _, b := range cat.Bundles() {
		byPackage[b.ID] = b
	}
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	done := map[string]bool{}
	for _, tier := range []string{substrate.TierProvider, substrate.TierSample} {
		for _, b := range cat.Bundles() {
			if b.Tier != tier {
				continue
			}
			if err := installClosure(reg, byPackage, b, done); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The suggested mappings landed and RESOLVED: five onto the shipped
	// person, one onto the shipped task. `installs:` is the package both
	// directions, so a mapping the tree adds without listing it never reaches
	// here, and a mapping that stops type-checking against a mirror fails the
	// install above.
	for _, want := range []struct {
		to    string
		count int
	}{
		{samples.Authority + "/people/person", 4},
		{samples.Authority + "/tasks/task", 1},
	} {
		if got := reg.MappingsTo(want.to); len(got) != want.count {
			var ids []string
			for _, m := range got {
				ids = append(ids, m.Identity())
			}
			t.Errorf("%d mappings onto %s, want %d: %v", len(got), want.to, want.count, ids)
		}
	}
}

// EVERY sample rehomes and then admits ON CORE ALONE, without a database. The
// import walk touches every string a closure carries, and the samples with
// functions, agents, triggers and Python sources are where it has the most to
// reach: an authority left inside a function body is a closure that admits
// here and fails at the first call. Each is rehomed onto one repository
// authority, so their `requires:` resolve against each other exactly as an
// import chain does.
//
// No provider is installed here, which is the other half of the door: a
// sample's SUGGESTED MAPPINGS name a provider mirror as their source, and the
// import DROPS the ones whose provider is absent (decision record 0049). So
// this is the fresh-repository case, and the filter is what makes it admit:
// without it every sample carrying one would be refused for vocabulary the
// reader never asked for.
func TestEverySampleRehomesOntoARepositoryAuthority(t *testing.T) {
	const home = "ada.example.com"
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	byPackage := map[string]*catalog.Bundle{}
	for _, b := range cat.Bundles() {
		byPackage[b.ID] = b
	}
	done := map[string]bool{}
	dropped := map[string]bool{}
	var rehome func(b *catalog.Bundle) error
	rehome = func(b *catalog.Bundle) error {
		if done[b.ID] {
			return nil
		}
		done[b.ID] = true
		for _, req := range b.Requires {
			rb, ok := byPackage[req]
			if !ok {
				return fmt.Errorf("%s requires %s, which no shipped bundle owns", b.ID, req)
			}
			if err := rehome(rb); err != nil {
				return err
			}
		}
		maps, err := schemaMaps(b.ID)
		if err != nil {
			return err
		}
		// The door's filter: a suggested mapping whose source kind is absent
		// is dropped, with its `installs:` entry.
		drop := map[string]bool{}
		for _, sm := range vocabulary.SuggestedMappings(maps) {
			if _, ok := reg.ByIdentity(sm.From); !ok {
				drop[sm.ID] = true
				dropped[sm.ID] = true
			}
		}
		maps = vocabulary.WithoutMappings(maps, drop)
		rehomed, err := vocabulary.RehomeAuthority(maps, samples.Authority, home)
		if err != nil {
			return fmt.Errorf("rehome %s: %w", b.ID, err)
		}
		if left := vocabulary.AuthorityMentions(rehomed, samples.Authority); len(left) > 0 {
			return fmt.Errorf("%s still names %s after the rehome: %v", b.ID, samples.Authority, left)
		}
		docs, err := documentsFrom(rehomed)
		if err != nil {
			return fmt.Errorf("parse the rehomed %s: %w", b.ID, err)
		}
		packages, err := vocabulary.BuildPackages(docs, vocabulary.SourceInstalled)
		if err != nil {
			return fmt.Errorf("build the rehomed %s: %w", b.ID, err)
		}
		for _, g := range packages {
			reg.Remove(g.Identity)
		}
		if err := reg.InstallAll(packages); err != nil {
			return fmt.Errorf("install the rehomed %s: %w", b.ID, err)
		}
		return nil
	}
	samplesSeen := 0
	for _, b := range cat.Bundles() {
		if b.Tier != substrate.TierSample {
			continue
		}
		samplesSeen++
		if err := rehome(b); err != nil {
			t.Fatal(err)
		}
	}
	if samplesSeen == 0 {
		t.Fatal("no sample bundles in the shipped catalog, so this test proved nothing")
	}
	// The kinds landed under the repository's authority, not the placeholder.
	if _, ok := reg.ByIdentity(home + "/tasks/task"); !ok {
		t.Errorf("%s/tasks/task is not in the registry after the rehome", home)
	}
	if _, ok := reg.ByIdentity(samples.Authority + "/tasks/task"); ok {
		t.Errorf("%s/tasks/task is live after a rehome that should have moved it", samples.Authority)
	}
	// The five suggested mappings were the ones dropped, and nothing that
	// projects onto the repository's own kinds survived: on a repository with
	// no provider, an import delivers kinds and no projection.
	if len(dropped) != 5 {
		t.Errorf("dropped %d suggested mappings, want the 5 the tree ships: %v", len(dropped), dropped)
	}
	for _, to := range []string{home + "/people/person", home + "/tasks/task"} {
		if got := reg.MappingsTo(to); len(got) != 0 {
			t.Errorf("%d mappings onto %s with no provider installed: %v", len(got), to, got)
		}
	}
}

func installClosure(reg *vocabulary.Registry, byPackage map[string]*catalog.Bundle, b *catalog.Bundle, done map[string]bool) error {
	if done[b.ID] {
		return nil
	}
	done[b.ID] = true
	for _, req := range b.Requires {
		rb, ok := byPackage[req]
		if !ok {
			return fmt.Errorf("%s requires %s, which no shipped bundle owns", b.ID, req)
		}
		if err := installClosure(reg, byPackage, rb, done); err != nil {
			return err
		}
	}
	docs, err := schemaDocs(b.ID)
	if err != nil {
		return err
	}
	packages, err := vocabulary.BuildPackages(docs, vocabulary.SourceInstalled)
	if err != nil {
		return fmt.Errorf("build %s: %w", b.ID, err)
	}
	// The authority row travels with every closure published under it, so the
	// second package under one authority re-declares it: an install replaces
	// the group it touches, and so does this.
	for _, g := range packages {
		reg.Remove(g.Identity)
	}
	if err := reg.InstallAll(packages); err != nil {
		return fmt.Errorf("install %s: %w", b.ID, err)
	}
	return nil
}

// treeFor answers the filesystem a package identity's directory lives in, and
// that directory: the shipped tree nests kinds/<authority>/<package>/, the
// samples tree samples/<package>/.
func treeFor(pkg string) (fs.FS, string) {
	if strings.HasPrefix(pkg, samples.Authority+"/") {
		return samples.Samples(), strings.TrimPrefix(pkg, samples.Authority+"/")
	}
	return kinds.All(), pkg
}

// schemaMaps decodes a package directory's schema documents, and the authority
// manifest above it exactly as a closure carries it, into raw envelope maps,
// skipping the data plane (a provider's triggers) as the install seam splits
// it. Raw, because the rehome walks decoded documents rather than parsed ones.
func schemaMaps(pkg string) ([]map[string]any, error) {
	fsys, dir := treeFor(pkg)
	var out []map[string]any
	dirs := []string{dir}
	for d := path.Dir(dir); ; d = path.Dir(d) {
		dirs = append([]string{d}, dirs...)
		if d == "." {
			break
		}
	}
	for _, d := range dirs {
		entries, err := fs.ReadDir(fsys, d)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
				continue
			}
			raw, err := fs.ReadFile(fsys, path.Join(d, e.Name()))
			if err != nil {
				return nil, err
			}
			dec := yaml.NewDecoder(bytes.NewReader(raw))
			for {
				var m map[string]any
				if err := dec.Decode(&m); errors.Is(err, io.EOF) {
					break
				} else if err != nil {
					return nil, fmt.Errorf("%s/%s: %w", d, e.Name(), err)
				}
				ref := fmt.Sprint(m["kind"])
				if vocabulary.KindPackage(ref) != vocabulary.PackageCore ||
					!vocabulary.VocabularyDocumentKind(vocabulary.KindName(ref)) {
					continue
				}
				out = append(out, m)
			}
		}
	}
	return out, nil
}

// schemaDocs is schemaMaps parsed into the loader's own documents.
func schemaDocs(pkg string) ([]vocabulary.Document, error) {
	maps, err := schemaMaps(pkg)
	if err != nil {
		return nil, err
	}
	return documentsFrom(maps)
}

func documentsFrom(maps []map[string]any) ([]vocabulary.Document, error) {
	docs := make([]vocabulary.Document, 0, len(maps))
	for _, m := range maps {
		doc, err := vocabulary.DocumentFromMap(m)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func rootNames(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// THE SHIPPED KINDS WHOSE NAMES READ WRONG DECLARE A LABEL (#675, decision
// 0117). Each of these reads as its humanized name in a client that splits
// and pluralizes it ("Conversations", "Calendar series"), and a person calls
// it something else; the label is what every client shows instead.
func TestShippedKindsDeclareTheirDisplayLabels(t *testing.T) {
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	byPackage := map[string]*catalog.Bundle{}
	for _, b := range cat.Bundles() {
		byPackage[b.ID] = b
	}
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	done := map[string]bool{}
	for _, b := range cat.Bundles() {
		if b.Tier != substrate.TierProvider {
			continue
		}
		if err := installClosure(reg, byPackage, b, done); err != nil {
			t.Fatal(err)
		}
	}
	const p = "providers.substrate.reamde.dev/"
	for id, want := range map[string]vocabulary.KindLabel{
		p + "slack/conversation":     {Singular: "Channel", Plural: "Channels"},
		p + "slack/user":             {Singular: "Member", Plural: "Members"},
		p + "slack/conversationsync": {Singular: "Sync progress", Plural: "Sync progress"},
		p + "google/contactgroup":    {Singular: "Contact label", Plural: "Contact labels"},
		p + "google/calendarseries":  {Singular: "Repeating event", Plural: "Repeating events"},
		p + "google/calendarsync":    {Singular: "Sync progress", Plural: "Sync progress"},
		p + "notion/pagesync":        {Singular: "Sync progress", Plural: "Sync progress"},
		p + "beeper/chatsync":        {Singular: "Sync progress", Plural: "Sync progress"},
	} {
		k, ok := reg.ByIdentity(id)
		if !ok {
			t.Errorf("%s: not in the installed registry", id)
			continue
		}
		if k.Label != want {
			t.Errorf("%s: label = %+v, want %+v", id, k.Label, want)
		}
	}
}

// A python body's PEP 723 block is what `uv sync --script` installs when the
// function registers, so its dependencies ship as surely as the body does.
// Each pins one exact version (`==`), so `audit:python` audits what installs
// and an upgrade is a line a reviewer sees. A package takes ONE version across
// the tree, so a bump cannot land in one provider and miss the next, which is
// how requests sat at 2.32.3 in two bodies and 2.32.4 in four.
func TestShippedPythonDependenciesPinOneExactVersion(t *testing.T) {
	var deps []pythonDependency
	for _, d := range readTreeDocuments(t) {
		if d.Kind != vocabulary.DocFunction || d.Data["runtime"] != "python" {
			continue
		}
		source, _ := d.Data["source"].(string)
		specs, err := pep723Dependencies(source)
		if err != nil {
			t.Errorf("%s: %v", d.ID, err)
			continue
		}
		for _, spec := range specs {
			deps = append(deps, pythonDependency{where: d.ID, spec: spec})
		}
	}
	// A reader that stopped finding the blocks would pass this test forever.
	if len(deps) == 0 {
		t.Fatal("read no PEP 723 dependency at all: the bodies no longer declare them this way")
	}
	for _, problem := range pythonPinProblems(deps) {
		t.Error(problem)
	}
}

// The two refusals on inputs the shipped tree does not hold today, so a green
// tree test means a pinned tree and not a rule that stopped refusing.
func TestPythonPinProblems(t *testing.T) {
	for _, tc := range []struct {
		name string
		deps []pythonDependency
		want []string // a fragment of each problem, in order
	}{
		{"exact pins agree", []pythonDependency{
			{"a", "requests==2.34.2"}, {"b", "Requests == 2.34.2"}, {"c", "requests[socks]==2.34.2 ; python_version >= '3.11'"},
		}, nil},
		{"a range", []pythonDependency{{"a", "requests>=2.34"}}, []string{`a: "requests>=2.34" pins no exact version`}},
		{"a bare name", []pythonDependency{{"a", "requests"}}, []string{`a: "requests" pins no exact version`}},
		{"a wildcard", []pythonDependency{{"a", "requests==2.*"}}, []string{`a: "requests==2.*" pins no exact version`}},
		{"arbitrary equality", []pythonDependency{{"a", "requests===2.34.2"}}, []string{`a: "requests===2.34.2" pins no exact version`}},
		{"a second bound", []pythonDependency{{"a", "requests==2.34.2,<3"}}, []string{`a: "requests==2.34.2,<3" pins no exact version`}},
		{"a URL", []pythonDependency{{"a", "requests @ https://example.com/r.whl"}}, []string{"pins no exact version"}},
		{"two versions", []pythonDependency{
			{"a", "requests==2.32.3"}, {"b", "requests==2.34.2"},
		}, []string{"b pins requests==2.34.2 and a pins requests==2.32.3"}},
		{"two spellings of one name", []pythonDependency{
			{"a", "google_auth==2.40.0"}, {"b", "Google.Auth==2.41.0"},
		}, []string{"b pins google-auth==2.41.0 and a pins google-auth==2.40.0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := pythonPinProblems(tc.deps)
			if len(got) != len(tc.want) {
				t.Fatalf("problems = %q, want %d", got, len(tc.want))
			}
			for i, want := range tc.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("problem %d = %q, want it to say %q", i, got[i], want)
				}
			}
		})
	}
}

func TestPEP723Dependencies(t *testing.T) {
	block := func(lines ...string) string {
		return "# /// script\n" + strings.Join(lines, "\n") + "\n# ///\ndef main(input, host):\n    return {}\n"
	}
	for _, tc := range []struct {
		name, source string
		want         []string
		err          string
	}{
		{"no block", "def main(input, host):\n    return {}\n", nil, ""},
		{"no dependencies", block(`# requires-python = ">=3.11"`), nil, ""},
		{"one line", block(`# dependencies = ["requests==2.34.2", 'six==1.17.0']`), []string{"requests==2.34.2", "six==1.17.0"}, ""},
		{"several lines and a comment", block(
			`# requires-python = ">=3.11"`,
			`# dependencies = [`,
			`#   "requests[socks]==2.34.2",  # the HTTP client`,
			`#   "six==1.17.0",`,
			`# ]`,
		), []string{"requests[socks]==2.34.2", "six==1.17.0"}, ""},
		{"an indented key", block(`#   dependencies = ["requests>=2"]`), []string{"requests>=2"}, ""},
		{"a quoted key", block(`# "dependencies" = ['requests>=2']`), []string{"requests>=2"}, ""},
		{"a longer key is another key", block(`# dev-dependencies = ["requests>=2"]`), nil, ""},
		{"not an array", block(`# dependencies = "requests==2.34.2"`), nil, "not an array"},
		{"a non-string entry", block(`# dependencies = [requests]`), nil, "not a string"},
		{"never closed", block(`# dependencies = [`, `#   "requests==2.34.2",`), nil, "never closes"},
		{"an escape in a value", block(`# dependencies = ["requests\\x3d=2"]`), nil, "escape"},
		{"an escape in the key", block(`# "depen\\u0064encies" = ["requests>=2"]`), nil, "escape"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pep723Dependencies(tc.source)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want one saying %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("dependencies = %q, want %q", got, tc.want)
			}
		})
	}
}

// .mise/pybodies.py reads the bundles line by line, with no YAML parser, so
// `lint:python` and `audit:python` see only the spellings it knows. This holds
// it to the real parser: every python function and bundle module the tree
// declares is written out under its own name, byte for byte, and nothing else
// is.
func TestPybodiesExtractsEveryShippedPythonBody(t *testing.T) {
	got, output, err := runPybodies(t, filepath.Join("..", ".mise", "pybodies.py"))
	if err != nil {
		t.Fatalf("pybodies.py extract: %v\n%s", err, output)
	}
	want := parsedBodies(t, "kinds", kinds.All())
	for name, source := range parsedBodies(t, "samples", samples.Samples()) {
		want[name] = source
	}
	if len(want) == 0 {
		t.Fatal("the tree declares no python function")
	}
	sameBodies(t, got, want)
}

// No shipped bundle declares `modules:` yet, and yamlfmt never writes a quoted
// key, so the shipped tree holds neither path; a fixture does. The script reads
// the tree its own file sits in, so a copy beside a fixture reads the fixture.
func TestPybodiesReadsAFixtureTree(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", ".mise", "pybodies.py"))
	if err != nil {
		t.Fatal(err)
	}
	fixture := func(t *testing.T, doc string) (string, string) {
		root := t.TempDir()
		for name, content := range map[string]string{
			".mise/pybodies.py":           string(script),
			"samples/fixture/bundle.yaml": doc,
		} {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return root, filepath.Join(root, ".mise", "pybodies.py")
	}

	t.Run("modules and quoted keys", func(t *testing.T) {
		const doc = `kind: "substrate.reamde.dev/core/bundle" # quoted, with a comment
metadata:
  id: samples.substrate.reamde.dev/fixture
data:
  package: fixture
  modules:
    connkit.py: |
      def greet(who):
          return "hi " + who
    "quoted.py": |-
      X = 1
---
kind: substrate.reamde.dev/core/function
metadata:
  id: samples.substrate.reamde.dev/fixture/fn
data:
  "runtime": python # quoted
  source: |
    import connkit


    def main(input, host):
        return {"output": connkit.greet("x")}
`
		root, script := fixture(t, doc)
		got, output, err := runPybodies(t, script)
		if err != nil {
			t.Fatalf("pybodies.py extract: %v\n%s", err, output)
		}
		want := parsedBodies(t, "samples", os.DirFS(filepath.Join(root, "samples")))
		if len(want) != 3 {
			t.Fatalf("the fixture parses to %d bodies, want 2 modules and 1 function", len(want))
		}
		sameBodies(t, got, want)
		// The padding is the point: a line in the written file is the line in
		// the YAML file, which is what lets ruff name the bundle.yaml line.
		lines := strings.Split(doc, "\n")
		for name, body := range got {
			first := strings.SplitN(strings.TrimLeft(body, "\n"), "\n", 2)[0]
			at := len(body) - len(strings.TrimLeft(body, "\n"))
			if strings.TrimSpace(lines[at]) != first {
				t.Errorf("%s: its first line sits at line %d, and the YAML holds %q there", name, at+1, lines[at])
			}
		}
	})

	t.Run("a source that is not a literal block", func(t *testing.T) {
		_, script := fixture(t, `kind: substrate.reamde.dev/core/function
metadata:
  id: samples.substrate.reamde.dev/fixture/fn
data:
  runtime: python
  source: >-
    def main(input, host):
        return {}
`)
		_, output, err := runPybodies(t, script)
		if err == nil || !strings.Contains(output, "samples/fixture/bundle.yaml:1: a `runtime: python` document with 0 `source: |` blocks") {
			t.Fatalf("a folded source was not refused by name: %v\n%s", err, output)
		}
	})
}

// runPybodies runs `extract` with the given copy of the script and returns
// every file it wrote, by its path under the output directory.
func runPybodies(t *testing.T, script string) (map[string]string, string, error) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 on PATH")
	}
	out := t.TempDir()
	output, err := exec.Command(python, script, "extract", out).CombinedOutput()
	if err != nil {
		return nil, string(output), err
	}
	got := map[string]string{}
	err = filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(p)
		rel, _ := filepath.Rel(out, p)
		got[filepath.ToSlash(rel)] = string(raw)
		return err
	})
	if err != nil {
		t.Fatalf("read the extracted tree: %v", err)
	}
	return got, string(output), nil
}

// parsedBodies is every python function source and bundle module the YAML
// parser reads under fsys, keyed by the path pybodies.py writes it to.
func parsedBodies(t *testing.T, dir string, fsys fs.FS) map[string]string {
	t.Helper()
	want := map[string]string{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".yaml" {
			return err
		}
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		docs, err := vocabulary.ParseStream(raw)
		if err != nil {
			return nil // delivery wiring, as readTreeDocuments skips it
		}
		for _, doc := range docs {
			switch {
			case doc.Kind == vocabulary.DocFunction && doc.Data["runtime"] == "python":
				source, _ := doc.Data["source"].(string)
				want[dir+"/"+p+"."+path.Base(doc.ID)+".py"] = source
			case doc.Kind == vocabulary.DocBundle:
				for name, source := range asMapping(doc.Data["modules"]) {
					want[dir+"/"+p+".modules."+name], _ = source.(string)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	return want
}

func sameBodies(t *testing.T, got, want map[string]string) {
	t.Helper()
	for name, source := range want {
		body, ok := got[name]
		switch {
		case !ok:
			t.Errorf("%s: declared, not extracted", name)
		case strings.Trim(body, "\n") != strings.Trim(source, "\n"):
			t.Errorf("%s: the extracted body differs from the declared source", name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s: extracted, not declared", name)
		}
	}
}

type pythonDependency struct {
	where string // the function that declares it
	spec  string // the PEP 508 string, verbatim
}

var (
	// PEP 723's own regex, the one internal/runner/pep723.go matches with.
	pep723Block = regexp.MustCompile(`(?m)^# /// (?P<type>[a-zA-Z0-9-]+)$\s(?P<content>(^#(| .*)$\s)+)^# ///$`)
	// The top-level key in every spelling TOML allows: bare or quoted, and
	// indented. Whatever follows the `=` must be the array.
	tomlDeps = regexp.MustCompile(`(?m)^[ \t]*(?:dependencies|"dependencies"|'dependencies')[ \t]*=[ \t]*`)
	// One name, optional extras, `==` and one version, optional markers. A
	// wildcard, a second bound, `===` and a URL all fall outside it.
	exactPin  = regexp.MustCompile(`^([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)\s*(?:\[[^\]]*\])?\s*==\s*([0-9A-Za-z][0-9A-Za-z.+!_-]*)\s*(?:;.*)?$`)
	nameRunes = regexp.MustCompile(`[-_.]+`)
)

// pep723Dependencies reads the `dependencies` array of a body's `script`
// block: the strings in it, verbatim. A body with no block, or a block with no
// array, declares none.
func pep723Dependencies(source string) ([]string, error) {
	var content string
	for _, m := range pep723Block.FindAllStringSubmatch(source, -1) {
		if m[1] == "script" {
			content = m[2]
			break
		}
	}
	var toml strings.Builder
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimPrefix(line, "#")
		toml.WriteString(strings.TrimPrefix(line, " ") + "\n")
	}
	text := toml.String()
	// TOML can spell a key or a value through an escape (`"depen\u0064encies"`),
	// which this reader does not decode, so it refuses one rather than read past
	// it. No shipped block carries a backslash.
	if strings.ContainsRune(text, '\\') {
		return nil, errors.New("PEP 723 block: an escape this reader does not decode")
	}
	loc := tomlDeps.FindStringIndex(text)
	if loc == nil {
		return nil, nil
	}
	if !strings.HasPrefix(text[loc[1]:], "[") {
		return nil, errors.New("PEP 723 dependencies: the value is not an array")
	}
	var out []string
	for rest := text[loc[1]+1:]; rest != ""; {
		switch c := rest[0]; c {
		case ' ', '\t', '\n', '\r', ',':
			rest = rest[1:]
		case '#':
			if i := strings.IndexByte(rest, '\n'); i >= 0 {
				rest = rest[i:]
			} else {
				rest = ""
			}
		case ']':
			return out, nil
		case '"', '\'':
			end := strings.IndexByte(rest[1:], c)
			if end < 0 {
				return nil, errors.New("PEP 723 dependencies: a string never closes")
			}
			out = append(out, rest[1:1+end])
			rest = rest[end+2:]
		default:
			return nil, fmt.Errorf("PEP 723 dependencies: %q is not a string", strings.SplitN(rest, "\n", 2)[0])
		}
	}
	return nil, errors.New("PEP 723 dependencies: the array never closes")
}

// pythonPinProblems holds every dependency to one exact version, and every
// package to one version across the tree, keyed by its normalized name
// (PEP 503), so `google_auth` and `Google.Auth` are one package.
func pythonPinProblems(deps []pythonDependency) []string {
	type pin struct{ where, version string }
	first := map[string]pin{}
	var problems []string
	for _, d := range deps {
		m := exactPin.FindStringSubmatch(d.spec)
		if m == nil {
			problems = append(problems, fmt.Sprintf("%s: %q pins no exact version; write `name==version`", d.where, d.spec))
			continue
		}
		name := nameRunes.ReplaceAllString(strings.ToLower(m[1]), "-")
		prev, seen := first[name]
		switch {
		case !seen:
			first[name] = pin{where: d.where, version: m[2]}
		case prev.version != m[2]:
			problems = append(problems, fmt.Sprintf("%s pins %s==%s and %s pins %s==%s: one package ships at one version, so move every pin together",
				d.where, name, m[2], prev.where, name, prev.version))
		}
	}
	return problems
}
