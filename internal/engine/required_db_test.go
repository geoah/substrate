package engine_test

// `required` requires, and `default` is stored.
//
// The two are one contract: a required property is writable without being
// named only because a declared default fills it, and the default is filled at
// the WRITE so the stored row and the changelog delta carry the same value. A
// default applied on the way out would be derived data, and the records table
// would stop being a fold of the changelog. The rebuild here is what proves it.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const requiredPackage = "required.example.substrate.reamde.dev/required"

// The authority the `required` NARROWING cases declare under, so a store can
// be walked across the narrowing without touching the kinds above.
const (
	narrowingAuthority = "reqreview.example.substrate.reamde.dev"
	narrowingPackage   = narrowingAuthority + "/reqreview"
)

const requiredTicket = requiredPackage + "/ticket"

// badDefaultPackage is where the unstorable default is declared.
const badDefaultPackage = "baddefault.example.substrate.reamde.dev/baddefault"

// requiredVocabulary declares one kind carrying the three cases: a required
// scalar with no default, a required scalar WITH one, and an ordinary optional
// property to patch with.
func requiredVocabulary(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	docs := []map[string]any{
		vocabulary.PackageManifest(requiredPackage, 0),
		vocabulary.KindManifest(requiredPackage,
			map[string]any{"singular": "ticket"},
			map[string]any{
				"displayTemplate": "{name}",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "required": true},
					"priority": map[string]any{
						"type": "enum", "values": []any{"none", "high"},
						"required": true, "default": "none",
					},
					"note": map[string]any{"type": "string"},
				},
			}),
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("apply the required vocabulary: %v", err)
	}
}

// wantProblem asserts the error is the validation error the API turns into a
// 422 with a populated `problems` array, naming the property.
func wantProblem(t *testing.T, err error, want string) {
	t.Helper()
	var ve *substrate.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected a *substrate.ValidationError, got %v", err)
	}
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a validation error must match ErrValidation, got %v", err)
	}
	for _, p := range ve.Problems {
		if strings.Contains(p, want) {
			return
		}
	}
	t.Fatalf("problems %v: expected one naming %q", ve.Problems, want)
}

func TestRequiredScalarIsRefusedNamingTheProperty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	requiredVocabulary(t, ds)

	_, err := ds.Put(ctx, owner, substrate.PutInput{Kind: requiredTicket, ID: "t1"})
	wantProblem(t, err, "props.name")

	// An empty string is the unfilled form field the declaration refuses.
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: requiredTicket, ID: "t1", Properties: map[string]any{"name": ""},
	})
	wantProblem(t, err, "props.name")

	// An EXPLICIT null is the writer saying this record has no value here, so
	// the declared default does not refill it.
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: requiredTicket, ID: "t1",
		Properties: map[string]any{"name": "ship it", "priority": nil},
	})
	wantProblem(t, err, "props.priority")
}

func TestDeclaredDefaultLandsInTheRecordAndSurvivesARebuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	requiredVocabulary(t, ds)

	created := mustPut(t, ds, owner, substrate.PutInput{
		Kind: requiredTicket, ID: "t1", Properties: map[string]any{"name": "ship it"},
	})
	if created.Properties["priority"] != "none" {
		t.Fatalf("the declared default must be the stored value, got %v", created.Properties["priority"])
	}

	// The delta the write appended is what a rebuild replays: if the default
	// had been applied on the way out instead, the rebuilt row would lose it.
	rb, ok := svc.(interface {
		RebuildRepository(ctx context.Context, repository string) (engine.RebuildReport, error)
	})
	if !ok {
		t.Fatal("the service cannot rebuild a repository")
	}
	if _, err := rb.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuilt := mustGet(t, ds, requiredTicket, "t1"); rebuilt.Properties["priority"] != "none" {
		t.Fatalf("the default must ride the changelog, got %v after the rebuild", rebuilt.Properties["priority"])
	}

	// A default seeds a create and never re-asserts itself: a later write that
	// does not name the property leaves what the record holds.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: requiredTicket, ID: "t1", Properties: map[string]any{"priority": "high"},
	})
	after := mustPut(t, ds, owner, substrate.PutInput{
		Kind: requiredTicket, ID: "t1", Properties: map[string]any{"note": "later"},
	})
	if after.Properties["priority"] != "high" {
		t.Fatalf("a default must not overwrite a value on update, got %v", after.Properties["priority"])
	}
}

// `required` is a statement about the RECORD, so the merged row is what
// satisfies it: a patch that clears the value is refused, and one that does not
// mention it is not.
func TestRequiredIsCheckedOnTheMergedRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	requiredVocabulary(t, ds)

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: requiredTicket, ID: "t1", Properties: map[string]any{"name": "ship it"},
	})
	_, err := ds.Patch(ctx, owner, requiredTicket, "t1", substrate.PatchInput{
		Properties: map[string]any{"name": nil},
	})
	wantProblem(t, err, "props.name")

	patched := mustPatch(t, ds, owner, requiredTicket, "t1", substrate.PatchInput{
		Properties: map[string]any{"note": "still here"},
	})
	if patched.Properties["name"] != "ship it" {
		t.Fatalf("a patch that never mentions a required property must pass, got %v", patched.Properties)
	}
}

// A default no write could store is refused where it is declared, not at every
// create of the kind.
func TestDeclaredDefaultMustBeStorable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	_, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(badDefaultPackage, 0),
		vocabulary.KindManifest(badDefaultPackage,
			map[string]any{"singular": "seen"},
			map[string]any{
				"properties": map[string]any{
					// Postgres has no year zero, so no write could ever store it.
					"seenAt": map[string]any{"type": "datetime", "default": "0000-01-01T00:00:00Z"},
				},
			}),
	})
	wantProblem(t, err, "no year zero")
}

// requiredNarrowingDocs declares one kind whose `name` is optional or required as the
// caller asks, so the same store can be walked across the narrowing.
func requiredNarrowingDocs(required bool) []map[string]any {
	name := map[string]any{"type": "string"}
	if required {
		name["required"] = true
	}
	return []map[string]any{
		vocabulary.PackageManifest(narrowingPackage, 0),
		vocabulary.KindManifest(narrowingPackage,
			map[string]any{"singular": "note"},
			map[string]any{"properties": map[string]any{
				"name": name,
				"tags": map[string]any{"type": "string", "repeated": true},
			}}),
	}
}

// Admission and the write path must draw the same line under "has a value". A
// row holding "" satisfies `props ? 'name'` and fails `required`, so counting
// the missing KEY alone admits a narrowing that strands the very rows the guard
// exists to protect: every later write to them is refused, with no way to
// migrate them under the declaration that refuses them.
func TestAddingRequiredCountsAnEmptyValueAsMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, requiredNarrowingDocs(false)); err != nil {
		t.Fatalf("apply the optional declaration: %v", err)
	}
	for id, props := range map[string]map[string]any{
		"empty":   {"name": ""},
		"missing": {"tags": []any{"a"}},
		"list":    {"name": "held", "tags": []any{}},
	} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: narrowingPackage + "/note", ID: id, Properties: props,
		})
	}

	_, err := ds.ApplyVocabularyDocuments(ctx, owner, requiredNarrowingDocs(true))
	if err == nil {
		t.Fatal("adding `required` must be refused while live records hold no value for it")
	}
	// Two records lack a value: the one with no key, and the one with "".
	if !strings.Contains(err.Error(), "2 live records") {
		t.Fatalf("the guard must count the empty value as missing, got %v", err)
	}
}

// A required FIELD is part of the object's declared shape, and an object value
// is written whole, so the value the write stores is what has to satisfy it.
func TestRequiredObjectFieldIsEnforced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	docs := []map[string]any{
		vocabulary.PackageManifest(narrowingAuthority+"/fields", 0),
		vocabulary.KindManifest(narrowingAuthority+"/fields",
			map[string]any{"singular": "profile"},
			map[string]any{"properties": map[string]any{
				"contact": map[string]any{"type": "object", "fields": map[string]any{
					"email": map[string]any{"type": "email", "required": true},
					"label": map[string]any{"type": "string"},
				}},
				"seen": map[string]any{"type": "object", "repeated": true, "fields": map[string]any{
					"stamp": map[string]any{"type": "datetime", "required": true},
				}},
			}}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("apply the field declaration: %v", err)
	}
	kind := narrowingAuthority + "/fields/profile"

	for name, tc := range map[string]struct {
		props map[string]any
		want  string
	}{
		"an empty object":        {map[string]any{"contact": map[string]any{}}, "props.contact: .email"},
		"the field nulled":       {map[string]any{"contact": map[string]any{"email": nil, "label": "home"}}, "props.contact: .email"},
		"a sibling only":         {map[string]any{"contact": map[string]any{"label": "home"}}, "props.contact: .email"},
		"an item of a list":      {map[string]any{"seen": []any{map[string]any{}}}, "props.seen: [0]: .stamp"},
		"the second item of one": {map[string]any{"seen": []any{map[string]any{"stamp": "2026-08-17T00:00:00Z"}, map[string]any{}}}, "props.seen: [1]: .stamp"},
	} {
		_, err := ds.Put(ctx, owner, substrate.PutInput{Kind: kind, ID: "p1", Properties: tc.props})
		if err == nil {
			t.Fatalf("%s: a required field must be refused when the written object lacks it", name)
		}
		wantProblem(t, err, tc.want)
	}

	// The whole object, satisfied, still writes.
	mustPut(t, ds, owner, substrate.PutInput{Kind: kind, ID: "p1", Properties: map[string]any{
		"contact": map[string]any{"email": "ada@example.com"},
		"seen":    []any{map[string]any{"stamp": "2026-08-17T00:00:00Z"}},
	}})
}

// Clearing is writing the property away, so that is where a required reference
// has to be defended: `required` is checked on the merged row after EVERY
// write, never only at birth, or the record stands one patch away from lacking
// the pointer its declaration requires.
func TestClearingARequiredReferenceIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	docs := []map[string]any{
		vocabulary.PackageManifest(narrowingAuthority+"/refs", 0),
		vocabulary.KindManifest(narrowingAuthority+"/refs",
			map[string]any{"singular": "owner"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(narrowingAuthority+"/refs",
			map[string]any{"singular": "asset"},
			map[string]any{
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"holder": map[string]any{
						"type": "reference", "kind": narrowingAuthority + "/refs/owner",
						"required": true, "mustExist": true,
					},
					"seenBy": map[string]any{
						"type": "reference", "kind": narrowingAuthority + "/refs/owner",
						"repeated": true, "mustExist": true,
					},
				},
			}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("apply the reference declaration: %v", err)
	}
	ownerKind, assetKind := narrowingAuthority+"/refs/owner", narrowingAuthority+"/refs/asset"
	mustPut(t, ds, owner, substrate.PutInput{Kind: ownerKind, ID: "ada", Properties: map[string]any{"name": "Ada"}})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: assetKind, ID: "a1",
		Properties: map[string]any{
			"name":   "press",
			"holder": "ada",
			"seenBy": []any{"ada"},
		},
	})

	_, err := ds.Patch(ctx, owner, assetKind, "a1", substrate.PatchInput{
		Properties: map[string]any{"holder": nil},
	})
	if err == nil {
		t.Fatal("clearing a required reference must be refused")
	}
	wantProblem(t, err, "holder")

	// A reference that is not required still clears.
	if _, err := ds.Patch(ctx, owner, assetKind, "a1", substrate.PatchInput{
		Properties: map[string]any{"seenBy": nil},
	}); err != nil {
		t.Fatalf("clear an optional reference: %v", err)
	}
}

// Admission must refuse a FIELD turning required for the same reason it refuses
// a property turning required: the write path enforces it afterwards, so a
// declaration that lands over objects without a value for it leaves those
// records unwritable.
func TestAddingARequiredFieldIsRefusedWhileObjectsLackIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	pkg := narrowingAuthority + "/fieldguard"
	docs := func(required bool) []map[string]any {
		email := map[string]any{"type": "email"}
		if required {
			email["required"] = true
		}
		return []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg,
				map[string]any{"singular": "profile"},
				map[string]any{"properties": map[string]any{
					"contact": map[string]any{"type": "object", "fields": map[string]any{
						"email": email,
						"label": map[string]any{"type": "string"},
					}},
				}}),
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs(false)); err != nil {
		t.Fatalf("apply the optional field: %v", err)
	}
	kind := pkg + "/profile"
	// One object without the field, one with it, and one record with no object
	// at all: only the first is stranded.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, ID: "bare",
		Properties: map[string]any{"contact": map[string]any{"label": "home"}},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, ID: "held",
		Properties: map[string]any{"contact": map[string]any{"email": "ada@example.com"}},
	})
	mustPut(t, ds, owner, substrate.PutInput{Kind: kind, ID: "none"})

	_, err := ds.ApplyVocabularyDocuments(ctx, owner, docs(true))
	if err == nil {
		t.Fatal("a field becoming required must be refused while objects lack a value for it")
	}
	if !strings.Contains(err.Error(), "1 live records") || !strings.Contains(err.Error(), "email") {
		t.Fatalf("the guard must count the one stranded object, got %v", err)
	}

	// With the stranded object backfilled, the same declaration lands.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, ID: "bare",
		Properties: map[string]any{"contact": map[string]any{"email": "bare@example.com", "label": "home"}},
	})
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs(true)); err != nil {
		t.Fatalf("the narrowing must land once nothing is stranded: %v", err)
	}
}

// A create that names NO properties at all still gets its defaults. The
// authored map is nil there, and the copy-on-write has to build one: cloning
// nil answers nil, and a nil map panics on assignment.
func TestDefaultFillsACreateThatNamesNoProperties(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	pkg := narrowingAuthority + "/nilmap"
	docs := []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg,
			map[string]any{"singular": "knob"},
			map[string]any{"properties": map[string]any{
				"mode":  map[string]any{"type": "enum", "values": []any{"off", "on"}, "default": "off"},
				"label": map[string]any{"type": "string"},
			}}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("apply the knob declaration: %v", err)
	}
	created := mustPut(t, ds, owner, substrate.PutInput{Kind: pkg + "/knob", ID: "k1"})
	if created.Properties["mode"] != "off" {
		t.Fatalf("a default must fill a create that names nothing, got %v", created.Properties["mode"])
	}
}
