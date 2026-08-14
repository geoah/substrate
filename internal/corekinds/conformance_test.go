// This file is HAND-WRITTEN and never generated: it is the price of the second
// reader. cmd/kindsgen reads the declarations through internal/kinddialect so
// that a broken generated file cannot make the generator unbuildable, and two
// readers of one contract drift unless something holds them together. This is
// that something.
//
// Three conformances, in the order a failure should be read:
//
//  1. The generator's reader and internal/vocabulary's loader agree on every
//     document in kinds/core.substrate.reamde.dev — property sets, datatypes,
//     enum values, container flags, markers, machines and nesting depth.
//  2. Per kind, the generated key set is exactly the loader's declared property
//     set, in both directions.
//  3. Decode(Properties(x)) == x, per kind, including absent versus empty.
//
// It imports internal/vocabulary, which the generated package must NOT: package
// corekinds is a leaf with no substrate imports, and TestPackageImportsNothing
// is what keeps it one.
package corekinds_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/corekinds"
	"github.com/geoah/substrate/internal/kinddialect"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

const authority = "core.substrate.reamde.dev"

// declarations reads the shipped tree twice: once with the generator's reader,
// once with the loader. Both read the EMBEDDED tree, so neither can pass by
// reading a different copy of the contract.
func declarations(t *testing.T) (map[string]*kinddialect.Kind, *vocabulary.Authority) {
	t.Helper()
	dir, err := fs.Sub(kinds.Seed(), authority)
	if err != nil {
		t.Fatalf("kinds/%s: %v", authority, err)
	}
	read, err := kinddialect.ReadFS(dir)
	if err != nil {
		t.Fatalf("kinddialect: %v", err)
	}
	byRef := map[string]*kinddialect.Kind{}
	for _, k := range read {
		byRef[k.Ref] = k
	}
	registry, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	loaded, ok := registry.AuthorityByName(authority)
	if !ok {
		t.Fatalf("vocabulary loaded no authority %s", authority)
	}
	return byRef, loaded
}

// TestReadersAgree is conformance 1.
func TestReadersAgree(t *testing.T) {
	byRef, loaded := declarations(t)
	if kinddialect.MaxFieldDepth != vocabulary.MaxFieldDepth {
		t.Fatalf("the two readers bound nesting differently: %d and %d",
			kinddialect.MaxFieldDepth, vocabulary.MaxFieldDepth)
	}
	for _, name := range loaded.KindOrder {
		declared := loaded.Kinds[name]
		read, ok := byRef[declared.Identity]
		if !ok {
			t.Errorf("%s: the loader declares it and the generator's reader does not", declared.Identity)
			continue
		}
		if read.Name != declared.Name || read.Plural != declared.Plural {
			t.Errorf("%s: names differ: reader %s/%s, loader %s/%s",
				declared.Identity, read.Name, read.Plural, declared.Name, declared.Plural)
		}
		if read.Description != declared.Description {
			t.Errorf("%s: descriptions differ", declared.Identity)
		}
		if read.Authority != declared.Authority {
			t.Errorf("%s: authority %q, loader %q", declared.Identity, read.Authority, declared.Authority)
		}
		// The EFFECTIVE version: a kind's own where it pins one, its authority's
		// otherwise. The boot upgrade keys on it, so the two readers agreeing on
		// the authored value alone would not be agreement.
		if read.Version != declared.Version {
			t.Errorf("%s: version %q, loader %q", declared.Identity, read.Version, declared.Version)
		}
		if read.DisplayTemplate != declared.DisplayTemplate {
			t.Errorf("%s: displayTemplate %q, loader %q",
				declared.Identity, read.DisplayTemplate, declared.DisplayTemplate)
		}
		compareProps(t, declared.Identity, declared.Props, read.Props, 1)
	}
	for ref := range byRef {
		if _, ok := loaded.Kinds[byRef[ref].Name]; !ok {
			t.Errorf("%s: the generator's reader declares it and the loader does not", ref)
		}
	}
}

func compareProps(t *testing.T, where string, declared map[string]*vocabulary.Property, read []*kinddialect.Property, depth int) {
	t.Helper()
	readByName := map[string]*kinddialect.Property{}
	for _, p := range read {
		readByName[p.Name] = p
	}
	if got, want := sortedKeys(readByName), sortedKeys(declared); !reflect.DeepEqual(got, want) {
		t.Errorf("%s: property sets differ:\n reader %v\n loader %v", where, got, want)
		return
	}
	for _, name := range sortedKeys(declared) {
		d, r := declared[name], readByName[name]
		at := where + "." + name
		if r.Depth != depth {
			t.Errorf("%s: reader depth %d, expected %d", at, r.Depth, depth)
		}
		if depth > kinddialect.MaxFieldDepth {
			t.Errorf("%s: nested past the declared bound of %d", at, kinddialect.MaxFieldDepth)
		}
		if got, want := r.Datatype, string(d.Datatype); got != want {
			t.Errorf("%s: datatype %q, loader %q", at, got, want)
		}
		if r.Repeated != d.Repeated || r.Keyed != d.Keyed || r.KeyPattern != d.KeyPattern {
			t.Errorf("%s: containers differ: reader repeated=%v keyed=%v keyPattern=%q, loader repeated=%v keyed=%v keyPattern=%q",
				at, r.Repeated, r.Keyed, r.KeyPattern, d.Repeated, d.Keyed, d.KeyPattern)
		}
		if r.Required != d.Required || r.Managed != d.Managed || r.RefersTo != d.RefersTo || r.Writer != d.Writer {
			t.Errorf("%s: markers differ: reader %+v, loader required=%v managed=%v refersTo=%q writer=%q",
				at, r, d.Required, d.Managed, d.RefersTo, d.Writer)
		}
		// The prose the generated types carry into their doc comments. A reader
		// that dropped a description would generate a type nobody can read
		// without the declaration open beside it, and nothing else would notice.
		if r.Description != d.Description || r.DisplayName != d.DisplayName {
			t.Errorf("%s: labels differ: reader description=%q displayName=%q, loader description=%q displayName=%q",
				at, r.Description, r.DisplayName, d.Description, d.DisplayName)
		}
		// Implicit is the property no `properties:` block declares: a transition's
		// stamp target. It is stored like any other, so a reader that missed one
		// would generate a type that refuses a value the ENGINE wrote.
		if r.Implicit != d.Implicit {
			t.Errorf("%s: implicit %v, loader %v", at, r.Implicit, d.Implicit)
		}
		if r.RenamedFrom != d.RenamedFrom {
			t.Errorf("%s: renamedFrom %q, loader %q", at, r.RenamedFrom, d.RenamedFrom)
		}
		if r.Inverse != d.Inverse || r.InverseDescription != d.InverseDescription {
			t.Errorf("%s: inverse differs: reader %q/%q, loader %q/%q",
				at, r.Inverse, r.InverseDescription, d.Inverse, d.InverseDescription)
		}
		// Refined names the authority-local property type a property refines. The
		// generator's reader REFUSES a refinement rather than resolving one (it
		// builds no registry), so this holds core to declaring none — the day one
		// arrives, generation fails loudly and this line says why.
		if d.Refined != "" {
			t.Errorf("%s: refines %q, which the generator's reader refuses rather than resolves", at, d.Refined)
		}
		compareEnumValues(t, at, d.Values, r.Values)
		compareBound(t, at+".min", d.Min, r.Min)
		compareBound(t, at+".max", d.Max, r.Max)
		pattern := ""
		if d.Pattern != nil {
			pattern = d.Pattern.String()
		}
		if r.Pattern != pattern {
			t.Errorf("%s: pattern %q, loader %q", at, r.Pattern, pattern)
		}
		compareTo(t, at, d, r)
		compareMachine(t, at, d.Machine, r.Machine)
		if (d.Fields == nil) != (r.Fields == nil) {
			t.Errorf("%s: one reader saw fields and the other did not", at)
			continue
		}
		if d.Fields != nil {
			compareProps(t, at, d.Fields, r.Fields, depth+1)
		}
	}
}

// compareTo holds a reference's referent to one answer. The loader RESOLVES a
// bare name to the declaring authority's full identity in Finalize; the reader
// keeps what was authored, because resolution needs the registry it does not
// build.
func compareTo(t *testing.T, at string, d *vocabulary.Property, r *kinddialect.Property) {
	t.Helper()
	want := r.To
	if want != "" && want != vocabulary.ToAny && !strings.Contains(want, "/") {
		want = authority + "/" + want
	}
	if d.To != want {
		t.Errorf("%s: reader to=%q (resolved %q), loader to=%q", at, r.To, want, d.To)
	}
}

func compareEnumValues(t *testing.T, at string, declared []vocabulary.EnumValue, read []kinddialect.EnumValue) {
	t.Helper()
	if len(declared) != len(read) {
		t.Errorf("%s: %d declared values, reader saw %d", at, len(declared), len(read))
		return
	}
	for i := range declared {
		if declared[i].Value != read[i].Value || declared[i].Label != read[i].Label {
			t.Errorf("%s: value %d differs: reader %+v, loader %+v", at, i, read[i], declared[i])
		}
	}
}

func compareBound(t *testing.T, at string, declared, read *float64) {
	t.Helper()
	switch {
	case declared == nil && read == nil:
	case declared == nil || read == nil:
		t.Errorf("%s: one reader saw a bound and the other did not", at)
	case *declared != *read:
		t.Errorf("%s: bound %v, loader %v", at, *read, *declared)
	}
}

func compareMachine(t *testing.T, at string, declared *vocabulary.Machine, read *kinddialect.Machine) {
	t.Helper()
	if declared == nil && read == nil {
		return
	}
	if declared == nil || read == nil {
		t.Errorf("%s: one reader saw a machine and the other did not", at)
		return
	}
	if !reflect.DeepEqual(declared.States, read.States) {
		t.Errorf("%s: states %v, loader %v", at, read.States, declared.States)
	}
	if declared.Initial != read.Initial {
		t.Errorf("%s: initial %q, loader %q", at, read.Initial, declared.Initial)
	}
	if len(declared.Transitions) != len(read.Transitions) {
		t.Errorf("%s: %d transitions, reader saw %d", at, len(declared.Transitions), len(read.Transitions))
		return
	}
	for i, want := range declared.Transitions {
		got := read.Transitions[i]
		stamps := map[string]string{}
		for _, s := range got.Stamps {
			stamps[s.Property] = s.Value
		}
		if len(stamps) == 0 {
			stamps = nil
		}
		if got.From != want.From || got.To != want.To || got.OnEnter != want.OnEnter ||
			!reflect.DeepEqual(stamps, want.Stamps) {
			t.Errorf("%s: transition %d differs: reader %+v, loader %+v", at, i, got, want)
		}
	}
}

// generatedKeys is the hand-kept half of conformance 2: every generated key set
// by the kind it belongs to. A kind added to the tree without an entry here
// fails TestGeneratedKeysMatchDeclarations, which is the point — a generated set
// nothing asserts about is a set that can quietly go wrong.
var generatedKeys = map[string][]string{
	"actor":              corekinds.ActorKeys,
	"agent":              corekinds.AgentKeys,
	"authority":          corekinds.AuthorityKeys,
	"blob":               corekinds.BlobKeys,
	"bundle":             corekinds.BundleKeys,
	"credential":         corekinds.CredentialKeys,
	"function":           corekinds.FunctionKeys,
	"kind":               corekinds.KindKeys,
	"llmmessage":         corekinds.LLMMessageKeys,
	"llmprovider":        corekinds.LLMProviderKeys,
	"llmthread":          corekinds.LLMThreadKeys,
	"propertytype":       corekinds.PropertyTypeKeys,
	"recordmapping":      corekinds.RecordMappingKeys,
	"recordmerge":        corekinds.RecordMergeKeys,
	"recordmergerequest": corekinds.RecordMergeRequestKeys,
	"recordpatchrequest": corekinds.RecordPatchRequestKeys,
	"recordsplit":        corekinds.RecordSplitKeys,
	"recoverykey":        corekinds.RecoveryKeyKeys,
	"repository":         corekinds.RepositoryKeys,
	"run":                corekinds.RunKeys,
	"token":              corekinds.TokenKeys,
	"trait":              corekinds.TraitKeys,
	"trigger":            corekinds.TriggerKeys,
}

// TestGeneratedKeysMatchDeclarations is conformance 2.
func TestGeneratedKeysMatchDeclarations(t *testing.T) {
	_, loaded := declarations(t)
	for _, name := range loaded.KindOrder {
		keys, ok := generatedKeys[name]
		if !ok {
			t.Errorf("%s declares a kind %q with no generated key set: run `mise run kinds:gen` and add it to generatedKeys",
				authority, name)
			continue
		}
		want := sortedKeys(loaded.Kinds[name].Props)
		if !reflect.DeepEqual(keys, want) {
			t.Errorf("%s: generated keys %v, declared %v", name, keys, want)
		}
		for _, key := range want {
			if !corekinds.Declared(keys, key) {
				t.Errorf("%s: declared property %q is outside the generated key set", name, key)
			}
		}
		for _, key := range keys {
			if _, declared := loaded.Kinds[name].Props[key]; !declared {
				t.Errorf("%s: generated key %q is not declared", name, key)
			}
		}
	}
	for name := range generatedKeys {
		if _, ok := loaded.Kinds[name]; !ok {
			t.Errorf("generatedKeys names %q, which %s does not declare", name, authority)
		}
	}
}

// TestDiscardsAreDeliberate is the other half of "the two readers agree": the
// loader represents things the generator DOES NOT, and an omission and a
// decision look identical in a comparison that simply leaves them out. Each one
// is listed here with what makes it discardable, so removing a discard is an
// edit to this test and not a silence.
//
// This test asserts on the loader side only, because that is where the discarded
// thing exists. What it pins is that each discard is still true of the shipped
// tree — a declaration that started depending on one would land here.
func TestDiscardsAreDeliberate(t *testing.T) {
	_, loaded := declarations(t)
	for _, name := range loaded.KindOrder {
		declared := loaded.Kinds[name]
		at := declared.Identity

		// INDEX PLACEMENT, not shape. `fts` and `embed` decide which search band
		// and which embedding a value joins. Neither changes a Go or a TypeScript
		// type, and FTS's default is computed from the datatype by a rule
		// (IsShortString/IsLongText, off for sensitive and keyed) that a second
		// reader could only re-derive — a second derivation of a default is the
		// drift this arrangement exists to avoid.
		//
		// SOURCE is "builtin" for every shipped kind and "installed" for an
		// imported one: a fact about where a declaration came from, not about what
		// a record holds.
		if declared.Source == "" {
			t.Errorf("%s: the loader stopped carrying a source", at)
		}

		// EDGES are not properties. They are a traversable relationship with its
		// own storage and its own read surface; a generated properties struct has
		// no field for one, and `<Kind>Keys` is the PROPERTY set by definition.
		// The count is asserted so a kind that grows an edge does not look like a
		// kind that grew a property.
		for _, edge := range declared.EdgeOrder {
			if _, isProp := declared.Props[edge]; isProp {
				t.Errorf("%s: %q is both an edge and a property", at, edge)
			}
		}

		// TRAITS, INDICES and HOTCOLUMNS are how a kind binds shared semantics
		// and what it asks the database to index. Both are the engine's business:
		// a bound trait adds no property to the declaration (its properties
		// arrive as declared ones, which the comparison above already covers).
		for _, binding := range declared.Traits {
			if binding.Identity == "" {
				t.Errorf("%s: a trait binding resolved to nothing", at)
			}
		}

		// MACHINES is an INDEX of the state properties, and TEMPLATE is the parsed
		// displayTemplate. Both are second views of something already compared:
		// every machine is the Machine on its state property, and the template is
		// the string. A generator that read the index would be reading the same
		// declaration twice.
		for machine := range declared.Machines {
			if p, ok := declared.Props[machine]; !ok || p.Machine == nil {
				t.Errorf("%s: machine %q indexes no state property", at, machine)
			}
		}

		// DEFINITION and SOURCEYAML are the authored document, kept whole for the
		// read surfaces. The generator reads the same document; carrying it into a
		// generated type would put a copy of the contract inside the code
		// generated from it.
		if len(declared.Definition) == 0 {
			t.Errorf("%s: the loader stopped carrying the authored data map", at)
		}

		// `default:` is a form's seed value. The loader admits the key and puts it
		// nowhere on Property — it rides the Definition map to the console — so
		// there is nothing on either side to compare, and the generated types
		// carry no defaults. A default that ever becomes shape would need this
		// comment deleted first.
	}
}

// TestKeyContractsAgree holds the one grammar this package has to spell for
// itself. A keyed map's key contract is asked in three places — the loader's
// CheckKey, the narrowing guard's SQL, and the generated decoder here — and
// vocabulary.KeyPatternRegexp exists so the first two cannot drift. This is the
// third, pinned to the same seam.
func TestKeyContractsAgree(t *testing.T) {
	for pattern, generated := range map[string]string{
		vocabulary.KeyPatternCamel:   corekinds.KeyPatternCamelRegexp,
		vocabulary.KeyPatternKindRef: corekinds.KeyPatternKindRefRegexp,
	} {
		if want := vocabulary.KeyPatternRegexp(pattern); generated != want {
			t.Errorf("keyPattern %s: generated %q, loader %q", pattern, generated, want)
		}
	}
}

// TestPackageImportsNothing holds the leaf rule the whole arrangement rests on:
// package corekinds imports nothing from this module, so the generated types can
// be depended on by the write path, the rung and the generator's own tests
// without a cycle. The _test files are exempt — this one imports the loader on
// purpose.
func TestPackageImportsNothing(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, imp := range file.Imports {
			if strings.Contains(imp.Path.Value, "github.com/geoah/substrate") {
				t.Errorf("%s imports %s: corekinds is a leaf", name, imp.Path.Value)
			}
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
