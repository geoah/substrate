package kinds_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

// The binary ships the vocabulary, so the embed pattern is part of the
// contract: a directory the pattern misses is an authority that silently stops
// existing in production while every other test still passes.
func TestEveryManifestOnDiskIsEmbedded(t *testing.T) {
	var want []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
			return err
		}
		want = append(want, filepath.ToSlash(path))
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree on disk: %v", err)
	}
	var got []string
	err = fs.WalkDir(kinds.All(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
			return err
		}
		got = append(got, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk the embedded tree: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("embedded %d manifests, %d on disk", len(got), len(want))
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
	if len(r.Authorities()) == 0 || len(r.Kinds()) == 0 {
		t.Fatalf("the seed registry is empty: %v", r.Authorities())
	}
	cat, err := catalog.Load(kinds.Bundles())
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
// THE SHIPPED TREE IS AUTHORED IN THE DECLARED SPELLING. A declaration row
// stores the parsed data map, and the loader still admits the spellings that came
// before the typed core, translating each one (vocabulary/canonical.go) — so a
// document left in an old spelling would store as something other than what it
// says, and a reader diffing the tree against a repository would see a change
// nobody wrote. Every spelling the translation touches is named here.
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
		"authority.version": true,
	}
	for _, short := range []string{
		vocabulary.DocAuthority, vocabulary.DocActor, vocabulary.DocKind, vocabulary.DocTrait,
		vocabulary.DocPropertyType, vocabulary.DocRecordMapping, vocabulary.DocFunction,
		vocabulary.DocAgent, vocabulary.DocBundle,
	} {
		ty, ok := reg.ByIdentity(vocabulary.KindRef(vocabulary.AuthorityCore, short))
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

// readTreeDocuments parses every DECLARATION document in the tree.
func readTreeDocuments(t *testing.T) []vocabulary.Document {
	t.Helper()
	var docs []vocabulary.Document
	err := fs.WalkDir(kinds.All(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
			return err
		}
		raw, err := fs.ReadFile(kinds.All(), path)
		if err != nil {
			return err
		}
		parsed, err := vocabulary.ParseStream(raw)
		if err != nil {
			// A bundle's delivery wiring (trigger records) sits in the same tree
			// and is not a declaration: those files parse nowhere near here.
			return nil
		}
		docs = append(docs, parsed...)
		return nil
	})
	if err != nil {
		t.Fatalf("read the tree: %v", err)
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
	cat, err := catalog.Load(kinds.Bundles())
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
// warning TestBothViewsLoad never reads. EVERY shipped bundle installs here,
// extensions included — an extension's declarations are refused by the same
// loader as a vocabulary bundle's (a hyphen in a callable's name, an authority
// its first label cannot name), and skipping them is how a shipped example
// reached a release refusing to install. Requires go first, into ONE seed
// registry, through the same BuildAuthorities+InstallAll pair admission runs,
// so the whole shipped set also has to coexist (GraphQL names, bundle names,
// cross-authority edges) without a database.
func TestShippedBundlesInstallOnTheSeed(t *testing.T) {
	cat, err := catalog.Load(kinds.Bundles())
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	// A malformed directory is dropped into Warnings, not an error, and a
	// dropped bundle would silently vanish from the loop below.
	if ws := cat.Warnings(); len(ws) != 0 {
		t.Fatalf("the shipped catalog carries warnings: %v", ws)
	}
	byAuthority := map[string]*catalog.Bundle{}
	for _, b := range cat.Bundles() {
		byAuthority[b.Authority] = b
	}
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	done := map[string]bool{}
	for _, b := range cat.Bundles() {
		if err := installClosure(reg, byAuthority, b, done); err != nil {
			t.Fatal(err)
		}
	}
}

func installClosure(reg *vocabulary.Registry, byAuthority map[string]*catalog.Bundle, b *catalog.Bundle, done map[string]bool) error {
	if done[b.Authority] {
		return nil
	}
	done[b.Authority] = true
	for _, req := range b.Requires {
		rb, ok := byAuthority[req]
		if !ok {
			return fmt.Errorf("%s requires %s, which no shipped bundle owns", b.ID, req)
		}
		if err := installClosure(reg, byAuthority, rb, done); err != nil {
			return err
		}
	}
	docs, err := schemaDocs(b.Authority)
	if err != nil {
		return err
	}
	authorities, err := vocabulary.BuildAuthorities(docs, vocabulary.SourceInstalled)
	if err != nil {
		return fmt.Errorf("build %s: %w", b.ID, err)
	}
	if err := reg.InstallAll(authorities); err != nil {
		return fmt.Errorf("install %s: %w", b.ID, err)
	}
	return nil
}

// schemaDocs decodes an authority directory's schema documents, skipping the
// data plane (an extension's triggers) exactly as the install seam splits it.
func schemaDocs(authority string) ([]vocabulary.Document, error) {
	entries, err := fs.ReadDir(kinds.All(), authority)
	if err != nil {
		return nil, err
	}
	var docs []vocabulary.Document
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		raw, err := fs.ReadFile(kinds.All(), authority+"/"+e.Name())
		if err != nil {
			return nil, err
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		for {
			var m map[string]any
			if err := dec.Decode(&m); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", authority, e.Name(), err)
			}
			kindAuthority, name := vocabulary.SplitKindRef(fmt.Sprint(m["kind"]))
			if kindAuthority != vocabulary.AuthorityCore || !vocabulary.VocabularyDocumentKind(name) {
				continue
			}
			d, err := vocabulary.DocumentFromMap(m)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", authority, e.Name(), err)
			}
			docs = append(docs, d)
		}
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
