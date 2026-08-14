package engine_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func assertDeepEqual(t *testing.T, where string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", where, got, want)
	}
}

const pathAuthority = "paths.example.substrate.reamde.dev"

// pathDocs installs one authority reaching every POSITION a reference is
// declared at, each pinned or unpinned on purpose: a scalar, an unconstrained
// scalar, a repeated one, a keyed map of them, one inside an object, one inside
// a repeated object's element, and one pinned at a kind whose records are
// DECLARATIONS — whose ids are themselves kind references, so the stored path
// has four segments and the id half carries its own slash.
func pathDocs() []map[string]any {
	return []map[string]any{
		vocabulary.AuthorityManifest(pathAuthority, ""),
		vocabulary.KindManifest(pathAuthority,
			map[string]any{"singular": "target", "plural": "targets"},
			map[string]any{}),
		vocabulary.KindManifest(pathAuthority,
			map[string]any{"singular": "holder", "plural": "holders"},
			map[string]any{"properties": map[string]any{
				"pinned":   map[string]any{"type": "reference", "kind": pathAuthority + "/target"},
				"local":    map[string]any{"type": "reference", "kind": "target"},
				"free":     map[string]any{"type": "reference", "kind": "any"},
				"declared": map[string]any{"type": "reference", "kind": "core.substrate.reamde.dev/kind"},
				"many": map[string]any{
					"type": "reference", "kind": pathAuthority + "/target", "repeated": true,
				},
				"keyed": map[string]any{
					"type": "reference", "kind": pathAuthority + "/target",
					"keyed": true, "keyPattern": "camel",
				},
				"spec": map[string]any{"type": "object", "fields": map[string]any{
					"ref": map[string]any{"type": "reference", "kind": pathAuthority + "/target"},
				}},
				"entries": map[string]any{
					"type": "object", "repeated": true,
					"fields": map[string]any{
						"ref": map[string]any{"type": "reference", "kind": pathAuthority + "/target"},
					},
				},
			}}),
	}
}

// THE VALUE MODEL, ONE TABLE: what a writer AUTHORS, what the row STORES, and
// what a get reads back, for every reference position and every pin. The three
// columns are the whole contract — an authored bare id is completed by the pin,
// a full path is kept as written, and a get answers exactly what was stored, so
// `get -o yaml | apply -f` is a fixed point.
func TestReferencePathAuthoredStoredAndReadBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, pathDocs()); err != nil {
		t.Fatalf("install the paths authority: %v", err)
	}

	target := vocabulary.RecordPath(pathAuthority+"/target", "a")
	// A DECLARATION record's id is a kind reference, so this path has FOUR
	// segments and the id half keeps its slash. It is the case the grammar
	// exists for.
	declared := vocabulary.RecordPath("core.substrate.reamde.dev/kind", pathAuthority+"/target")

	for name, tc := range map[string]struct {
		authored any
		stored   any
	}{
		// A pinned scalar: the bare id is the authored short form, and the pin
		// supplies the kind the value omits.
		"pinned, authored bare":      {authored: "a", stored: target},
		"pinned, authored full path": {authored: target, stored: target},
		// A pin naming a repository-LOCAL kind resolves to the full identity, so
		// the stored path is spelled one way whatever the writer typed.
		"local pin, authored bare": {authored: "a", stored: target},
		// Unpinned: no kind to borrow, so only a full path says what it names.
		"unpinned, authored full path": {authored: target, stored: target},
		// The four-segment case, both ways round. The bare form here CARRIES A
		// SLASH and is still unambiguous: a kind identity cannot be read as a
		// path, because splitting it leaves nothing for the id. This is the
		// ordinary spelling for every property that names a kind or a function.
		"declaration pin, authored bare": {authored: pathAuthority + "/target", stored: declared},
		"declaration pin, authored path": {authored: declared, stored: declared},
		// An id that is EQUAL to a kind identity works on purpose, not by
		// accident: same reasoning, pinned at an ordinary kind this time.
		"pinned, id equal to a kind identity": {
			authored: "core.substrate.reamde.dev/kind",
			stored:   vocabulary.RecordPath(pathAuthority+"/target", "core.substrate.reamde.dev/kind"),
		},
		// A slash-bearing id that WOULD parse as a path is refused as a short
		// form (below) and accepted written out in full.
		"pinned, slash-bearing id in full path form": {
			authored: vocabulary.RecordPath(pathAuthority+"/target", "foo.bar/baz/qux"),
			stored:   vocabulary.RecordPath(pathAuthority+"/target", "foo.bar/baz/qux"),
		},
		// Unpinned, the dot rule alone decides: no dot in the first segment
		// means a repository-LOCAL kind, which then resolves to its full
		// identity, so the stored path is spelled one way either way.
		"unpinned, local kind path": {
			authored: "target/abc",
			stored:   vocabulary.RecordPath(pathAuthority+"/target", "abc"),
		},
		// Containers carry the same rule elementwise, at every declared depth.
		"repeated, mixed forms": {
			authored: []any{"a", target},
			stored:   []any{target, target},
		},
		"keyed, mixed forms": {
			authored: map[string]any{"one": "a", "two": target},
			stored:   map[string]any{"one": target, "two": target},
		},
		"inside an object": {
			authored: map[string]any{"ref": "a"},
			stored:   map[string]any{"ref": target},
		},
		"inside a repeated object": {
			authored: []any{map[string]any{"ref": "a"}},
			stored:   []any{map[string]any{"ref": target}},
		},
	} {
		prop := map[string]string{
			"pinned, authored bare":                      "pinned",
			"pinned, authored full path":                 "pinned",
			"local pin, authored bare":                   "local",
			"unpinned, authored full path":               "free",
			"declaration pin, authored bare":             "declared",
			"declaration pin, authored path":             "declared",
			"pinned, id equal to a kind identity":        "pinned",
			"pinned, slash-bearing id in full path form": "pinned",
			"unpinned, local kind path":                  "free",
			"repeated, mixed forms":                      "many",
			"keyed, mixed forms":                         "keyed",
			"inside an object":                           "spec",
			"inside a repeated object":                   "entries",
		}[name]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			row := mustPut(t, ds, owner, substrate.PutInput{
				Kind:       pathAuthority + "/holder",
				Properties: map[string]any{prop: tc.authored},
			})
			assertDeepEqual(t, "stored "+prop, row.Properties[prop], tc.stored)

			// The read answers what was stored, and re-applying that read is a
			// fixed point: the apply round trip cannot drift a reference.
			got := mustGet(t, ds, row.Kind, row.ID)
			assertDeepEqual(t, "read "+prop, got.Properties[prop], tc.stored)
			again := mustPut(t, ds, owner, substrate.PutInput{
				Kind: row.Kind, ID: row.ID,
				Properties: map[string]any{prop: got.Properties[prop]},
			})
			assertDeepEqual(t, "re-applied "+prop, again.Properties[prop], tc.stored)
		})
	}
}

// The refusals the value model owes: a bare id with no pin to complete it names
// no kind and therefore no record, and a full path that contradicts its pin is
// refused NAMING BOTH, so the writer can tell which end to change.
func TestReferencePathRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, pathDocs()); err != nil {
		t.Fatalf("install the paths authority: %v", err)
	}

	for name, tc := range map[string]struct {
		prop  string
		value any
		want  []string
	}{
		"a bare id with nothing to complete it": {
			prop: "free", value: "a",
			want: []string{"any kind", "full", `"a"`},
		},
		"a path contradicting the pin": {
			prop:  "pinned",
			value: vocabulary.RecordPath("core.substrate.reamde.dev/kind", pathAuthority+"/target"),
			want: []string{
				"core.substrate.reamde.dev/kind",
				pathAuthority + "/target",
			},
		},
		"a pointer at a kind nothing declares": {
			prop: "free", value: "nosuch.example.com/thing/x",
			want: []string{"unknown"},
		},
		// The retired pair is refused by NAME wherever it appears: the rung is
		// what migrates a stored one, so nothing may author a new one.
		"the retired pair": {
			prop: "free", value: map[string]any{"id": "a"},
			want: []string{"retired", "rung"},
		},
		// THE AMBIGUOUS CORNER. Under a pin, a value that parses as a path whose
		// kind is not the pin reads two ways, and both are named rather than one
		// being guessed.
		"a slash-bearing id that also parses as a path": {
			prop: "pinned", value: "foo.bar/baz/qux",
			want: []string{
				"ambiguous",
				"a pointer at foo.bar/baz",
				vocabulary.RecordPath(pathAuthority+"/target", "foo.bar/baz/qux"),
				"the declaration pins " + pathAuthority + "/target",
			},
		},
		// Empty-id shapes name no record, pinned or not.
		"a trailing slash under a pin": {
			prop: "pinned", value: "target/",
			want: []string{"empty segment"},
		},
		"the bare pin itself, with nothing after it": {
			prop: "pinned", value: pathAuthority + "/target/",
			want: []string{"empty"},
		},
		// Unpinned, a two-segment value with a DOTTED first segment names a kind
		// and leaves nothing for the id, so it is not a path.
		"unpinned, a kind identity with no id": {
			prop: "free", value: "foo.bar/baz",
			want: []string{"any kind", "full", "foo.bar/baz"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ds.Put(ctx, owner, substrate.PutInput{
				Kind:       pathAuthority + "/holder",
				Properties: map[string]any{tc.prop: tc.value},
			})
			if err == nil {
				t.Fatalf("%s was admitted", name)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal must name %q, got: %v", want, err)
				}
			}
		})
	}
}
