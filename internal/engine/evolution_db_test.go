package engine_test

// Evolution-with-data: schema admission classifies
// the definition diff against the stored definition and refuses every
// NARROWING class with the live-row count, while additive changes admit
// freely and a renamedFrom naming a property no row ever carried is admitted,
// stored and round-tripped. The rename that moves live values is
// rename_db_test.go's.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const evoPackage = "evo.example.substrate.reamde.dev/evo"

// evoBaseProps is the base gizmo declaration the narrowing subtests diff
// against: a plain string, an enum, a state machine and an optional string.
func evoBaseProps() map[string]any {
	return map[string]any{
		"size":  map[string]any{"type": "string"},
		"level": map[string]any{"type": "enum", "values": []any{"low", "high"}},
		"phase": map[string]any{
			"type": "state", "states": []any{"open", "done"}, "initial": "open",
			"transitions": []any{map[string]any{"from": "open", "to": "done"}},
		},
		"note": map[string]any{"type": "string"},
	}
}

func evoTypeDoc(props map[string]any) map[string]any {
	return vocabulary.KindManifest(evoPackage,
		map[string]any{"singular": "gizmo"},
		map[string]any{"properties": props})
}

func evoApply(t *testing.T, ds substrate.Dataset, props map[string]any) error {
	t.Helper()
	_, err := ds.ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabulary.PackageManifest(evoPackage, 0),
		evoTypeDoc(props),
	})
	return err
}

// evoApplyRetiring is evoApply with a `retired:` block beside the properties
// (decision 0055).
func evoApplyRetiring(t *testing.T, ds substrate.Dataset, props, retired map[string]any) error {
	t.Helper()
	_, err := ds.ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabulary.PackageManifest(evoPackage, 0),
		vocabulary.KindManifest(evoPackage,
			map[string]any{"singular": "gizmo"},
			map[string]any{"properties": props, "retired": retired}),
	})
	return err
}

// wantNarrowingGuard asserts a refused apply: a guard error naming the class
// and carrying the live-row count.
func wantNarrowingGuard(t *testing.T, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("a narrowing change with stranded live rows must refuse")
	}
	wantErr(t, err, substrate.ErrGuard, "narrowing diff")
	for _, f := range fragments {
		if !strings.Contains(err.Error(), f) {
			t.Fatalf("guard must name %q, got: %v", f, err)
		}
	}
}

func TestSchemaEvolutionNarrowingRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := evoApply(t, ds, evoBaseProps()); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	// One live row that every narrowing below would strand: it carries size
	// and level, occupies the done state, and lacks note.
	gizmo := mustPut(t, ds, owner, substrate.PutInput{
		Kind: evoPackage + "/gizmo",
		Properties: map[string]any{
			"size": "big", "level": "high", "phase": "done",
		},
	})

	t.Run("property dropped", func(t *testing.T) {
		props := evoBaseProps()
		delete(props, "size")
		wantNarrowingGuard(t, evoApply(t, ds, props),
			`property "size" dropped`, "1 live records")
	})

	// A property renamed with `renamedFrom` is not in this list: the apply moves
	// the live values to the new name instead (rename_db_test.go).

	t.Run("kind changed", func(t *testing.T) {
		props := evoBaseProps()
		props["size"] = map[string]any{"type": "int"}
		wantNarrowingGuard(t, evoApply(t, ds, props),
			`property "size" changes kind string → int`, "1 live records")
	})

	t.Run("string retyped to enum missing the held value", func(t *testing.T) {
		props := evoBaseProps()
		props["size"] = map[string]any{"type": "enum", "values": []any{"small"}}
		wantNarrowingGuard(t, evoApply(t, ds, props),
			`property "size" changes kind string → enum`, `a value outside "small"`, "1 live records")
	})

	t.Run("repeated flip is a kind change", func(t *testing.T) {
		props := evoBaseProps()
		props["size"] = map[string]any{"type": "string", "repeated": true}
		wantNarrowingGuard(t, evoApply(t, ds, props),
			`changes kind string → repeated string`, "1 live records")
	})

	t.Run("enum value removed", func(t *testing.T) {
		props := evoBaseProps()
		props["level"] = map[string]any{"type": "enum", "values": []any{"low"}}
		wantNarrowingGuard(t, evoApply(t, ds, props),
			`property "level" removes value(s) "high"`, "1 live records")
	})

	t.Run("state removed", func(t *testing.T) {
		props := evoBaseProps()
		props["phase"] = map[string]any{
			"type": "state", "states": []any{"open"}, "initial": "open",
			"transitions": []any{},
		}
		wantNarrowingGuard(t, evoApply(t, ds, props),
			`state property "phase" removes state(s) "done"`, "1 live records")
	})

	t.Run("state property turned scalar", func(t *testing.T) {
		props := evoBaseProps()
		props["phase"] = map[string]any{"type": "string"}
		wantNarrowingGuard(t, evoApply(t, ds, props),
			`property "phase" changes kind`, "1 live records")
	})

	t.Run("required added", func(t *testing.T) {
		props := evoBaseProps()
		props["note"] = map[string]any{"type": "string", "required": true}
		wantNarrowingGuard(t, evoApply(t, ds, props),
			`property "note" becomes required`, "1 live records")
	})

	t.Run("the count is real", func(t *testing.T) {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind:       evoPackage + "/gizmo",
			Properties: map[string]any{"size": "small"},
		})
		props := evoBaseProps()
		delete(props, "size")
		wantNarrowingGuard(t, evoApply(t, ds, props), "2 live records")
	})

	// Nothing above landed: the stored definition is still the base.
	ty, err := ds.Get(ctx, "substrate.reamde.dev/core/kind", evoPackage+"/gizmo")
	if err != nil {
		t.Fatalf("read stored type: %v", err)
	}
	declared := ty.Properties["properties"]
	if declared == nil {
		t.Fatal("the stored declaration carries no properties")
	}
	if !strings.Contains(fmt.Sprint(declared), "size") {
		t.Fatalf("failed applies must leave the stored declaration alone: %v", declared)
	}
	_ = gizmo
}

func TestSchemaEvolutionAdditiveAdmits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := evoApply(t, ds, evoBaseProps()); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind:       evoPackage + "/gizmo",
		Properties: map[string]any{"size": "big", "level": "high", "phase": "done"},
	})

	// Removing an enum value NO live row holds admits: the guard counts, it
	// does not blanket-refuse the class. Removing it does not spend it either:
	// `low` comes back below, then is RETIRED (decision 0055), and only then
	// is its return refused.
	props := evoBaseProps()
	props["level"] = map[string]any{"type": "enum", "values": []any{"high"}}
	if err := evoApply(t, ds, props); err != nil {
		t.Fatalf("removing an unheld enum value must admit: %v", err)
	}
	props["level"] = map[string]any{"type": "enum", "values": []any{"high", "low"}}
	if err := evoApply(t, ds, props); err != nil {
		t.Fatalf("a removed but unretired enum value must be free to return: %v", err)
	}
	props["level"] = map[string]any{"type": "enum", "values": []any{"high"}}
	if err := evoApplyRetiring(t, ds, props, map[string]any{"values": map[string]any{"level": []any{"low"}}}); err != nil {
		t.Fatalf("retiring an unheld enum value must admit: %v", err)
	}
	props["level"] = map[string]any{"type": "enum", "values": []any{"high", "low"}}
	if err := evoApply(t, ds, props); err == nil || !strings.Contains(err.Error(), "a retired name is never declared again") {
		t.Fatalf("a retired enum value must refuse to return, got: %v", err)
	}

	// Purely additive: a new optional property, a new enum value, a new state
	// with its transition, a new type version of nothing else.
	props["weight"] = map[string]any{"type": "float"}
	props["level"] = map[string]any{"type": "enum", "values": []any{"high", "mid"}}
	props["phase"] = map[string]any{
		"type": "state", "states": []any{"open", "done", "archived"}, "initial": "open",
		"transitions": []any{
			map[string]any{"from": "open", "to": "done"},
			map[string]any{"from": "done", "to": "archived"},
		},
	}
	if err := evoApply(t, ds, props); err != nil {
		t.Fatalf("an additive change must admit: %v", err)
	}
	// The widened declaration is live: the new property takes values.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind:       evoPackage + "/gizmo",
		Properties: map[string]any{"weight": 1.5, "level": "mid"},
	})

	// Dropping the property after nulling it on every row admits: the guard
	// is refuse-while-stranded, not refuse-forever.
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{evoPackage + "/gizmo"}}, First: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Records {
		mustPatch(t, ds, owner, e.Kind, e.ID, substrate.PatchInput{Properties: map[string]any{"size": nil}})
	}
	delete(props, "size")
	if err := evoApply(t, ds, props); err != nil {
		t.Fatalf("dropping a property no live row carries must admit: %v", err)
	}
}

// A string property retyped to enum follows the VALUES, not the presence: it
// lands when every stored value is in the declared set (the runtime kinds'
// status/mode/role sets were engine-held long before they were declared),
// refuses naming the count when one is not, and once landed the set refuses
// at the write.
func TestSchemaEvolutionStringToEnumFollowsTheValues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := evoApply(t, ds, evoBaseProps()); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind:       evoPackage + "/gizmo",
		Properties: map[string]any{"size": "big"},
	})

	// The set covers every stored value: the retype lands.
	props := evoBaseProps()
	props["size"] = map[string]any{"type": "enum", "values": []any{"big", "small"}}
	if err := evoApply(t, ds, props); err != nil {
		t.Fatalf("a string → enum retype every stored value satisfies must admit: %v", err)
	}

	// The payoff: the closed set now refuses at the write.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind:       evoPackage + "/gizmo",
		Properties: map[string]any{"size": "huge"},
	}); err == nil {
		t.Fatal("a value outside the declared enum set must refuse at the write")
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind:       evoPackage + "/gizmo",
		Properties: map[string]any{"size": "small"},
	})
}

// The reserved keys of issue 110 travel the same road renamedFrom does: through
// admission, into the stored declaration row, and back out of it when a later
// write rebuilds the candidate registry from rows. A reference's LINK DATA is
// the block that could not be taken on faith: it is a property declaration
// nested inside a property declaration, and it has to survive the round trip
// with its own reserved keys intact.
func TestSchemaEvolutionReservedKeysRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	props := evoBaseProps()
	props["serial"] = map[string]any{"type": "string", "unique": true}
	props["note"] = map[string]any{"type": "string", "deprecated": true}
	props["level"] = map[string]any{"type": "enum", "values": []any{
		"low", map[string]any{"value": "high", "deprecated": true},
	}}
	props["author"] = map[string]any{
		"type": "reference", "repeated": true,
		"properties": map[string]any{
			"order": map[string]any{"type": "int"},
			"since": map[string]any{"type": "datetime", "deprecated": true},
			"role": map[string]any{"type": "enum", "values": []any{
				map[string]any{"value": "writer"},
				map[string]any{"value": "editor"},
			}},
		},
	}
	props["predecessor"] = map[string]any{"type": "reference", "deprecated": true}
	apply := func() error {
		_, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
			vocabulary.PackageManifest(evoPackage, 0),
			vocabulary.KindManifest(evoPackage,
				map[string]any{"singular": "gizmo"},
				map[string]any{"properties": props}),
		})
		return err
	}
	if err := apply(); err != nil {
		t.Fatalf("the reserved keys must be admitted: %v", err)
	}

	assertStored := func(when string) {
		t.Helper()
		ty, err := ds.Get(ctx, "substrate.reamde.dev/core/kind", evoPackage+"/gizmo")
		if err != nil {
			t.Fatalf("%s: read stored type: %v", when, err)
		}
		stored, _ := ty.Properties["properties"].(map[string]any)
		serial, _ := stored["serial"].(map[string]any)
		if got, _ := serial["unique"].(bool); !got {
			t.Errorf("%s: stored unique = %v", when, serial["unique"])
		}
		note, _ := stored["note"].(map[string]any)
		if got, _ := note["deprecated"].(bool); !got {
			t.Errorf("%s: stored deprecated = %v", when, note["deprecated"])
		}
		level, _ := stored["level"].(map[string]any)
		values, _ := level["values"].([]any)
		if len(values) != 2 {
			t.Fatalf("%s: stored enum values = %v", when, level["values"])
		}
		high, _ := values[1].(map[string]any)
		if got, _ := high["deprecated"].(bool); !got {
			t.Errorf("%s: stored enum value deprecated = %v", when, values[1])
		}
		author, _ := stored["author"].(map[string]any)
		linkProps, _ := author["properties"].(map[string]any)
		order, _ := linkProps["order"].(map[string]any)
		if got, _ := order["type"].(string); got != "int" {
			t.Errorf("%s: stored link property = %v", when, linkProps["order"])
		}
		role, _ := linkProps["role"].(map[string]any)
		roleValues, _ := role["values"].([]any)
		if len(roleValues) != 2 {
			t.Fatalf("%s: stored link enum values = %v", when, role["values"])
		}
		writer, _ := roleValues[0].(map[string]any)
		if got, _ := writer["value"].(string); got != "writer" {
			t.Errorf("%s: stored link enum value = %v", when, roleValues[0])
		}
		pre, _ := stored["predecessor"].(map[string]any)
		if got, _ := pre["deprecated"].(bool); !got {
			t.Errorf("%s: stored reference deprecated = %v", when, pre["deprecated"])
		}
	}
	assertStored("after apply")

	version := func(when string) int64 {
		t.Helper()
		ty, err := ds.Get(ctx, "substrate.reamde.dev/core/kind", evoPackage+"/gizmo")
		if err != nil {
			t.Fatalf("%s: read stored type: %v", when, err)
		}
		v, _ := vocabulary.VersionValue(ty.Properties["version"])
		return v
	}
	first := version("after apply")

	// Applying the SAME document again is not a change. It would be one if the
	// loader rewrote any part of the block on its way into the row, because the
	// bump compares the authored document against the stored one: the version
	// would climb on every apply and every repository would see an upgrade that
	// changed nothing.
	if err := apply(); err != nil {
		t.Fatalf("re-apply of the identical document: %v", err)
	}
	if got := version("after an identical re-apply"); got != first {
		t.Fatalf("an identical re-apply bumped the version %d → %d", first, got)
	}

	// Nothing enforces `unique` yet, and a repository that believed otherwise
	// would be storing rows it cannot keep: two records, one serial.
	for _, id := range []string{"one", "two"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: evoPackage + "/gizmo", ID: id,
			Properties: map[string]any{"serial": "SN-1"},
		})
	}

	// A later write rebuilds the candidate FROM the stored rows, so every
	// reserved key has to survive rowDocument and re-admission.
	props["extra"] = map[string]any{"type": "string"}
	if err := apply(); err != nil {
		t.Fatalf("re-apply over the stored reserved keys: %v", err)
	}
	assertStored("after rebuild from rows")
}

func TestSchemaEvolutionRenamedFromRoundTrips(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	props := evoBaseProps()
	// A brand-new property declaring its previous name: admitted (nothing is
	// stranded — no row ever carried "nickname") and stored as reserved.
	props["alias"] = map[string]any{"type": "string", "renamedFrom": "nickname"}
	if err := evoApply(t, ds, props); err != nil {
		t.Fatalf("renamedFrom must be admitted: %v", err)
	}

	assertStored := func(when string) {
		t.Helper()
		ty, err := ds.Get(ctx, "substrate.reamde.dev/core/kind", evoPackage+"/gizmo")
		if err != nil {
			t.Fatalf("%s: read stored type: %v", when, err)
		}
		p, _ := ty.Properties["properties"].(map[string]any)
		alias, _ := p["alias"].(map[string]any)
		if got, _ := alias["renamedFrom"].(string); got != "nickname" {
			t.Fatalf("%s: stored renamedFrom = %q, want nickname", when, got)
		}
	}
	assertStored("after apply")

	// A later write to the same authority rebuilds the candidate FROM the stored
	// rows: the reserved key must survive the round trip through rowDocument
	// and re-admission.
	props["extra"] = map[string]any{"type": "string"}
	if err := evoApply(t, ds, props); err != nil {
		t.Fatalf("re-apply over stored renamedFrom: %v", err)
	}
	assertStored("after rebuild from rows")
}

// TestSchemaEvolutionRefusesReferenceAndObjectNarrowing: refuse-narrowing recurses into a reference's `to:` target and an object's
// fields, each refused with the stranded live-row count — not silently
// admitted as the outer-kind-only classifier did.
func TestSchemaEvolutionRefusesReferenceAndObjectNarrowing(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := evoRefApply(t, ds, evoRefBaseProps()); err != nil {
		t.Fatalf("install base authority: %v", err)
	}
	// One holder: its reference points at widget, its object carries both fields.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: evoRefPackage + "/holder",
		Properties: map[string]any{
			"ref":  vocabulary.RecordPath(evoRefPackage+"/widget", "w1"),
			"spec": map[string]any{"a": "x", "b": 5},
		},
	})

	t.Run("reference target narrowed", func(t *testing.T) {
		props := evoRefBaseProps()
		props["ref"] = map[string]any{"type": "reference", "kind": "gadget"}
		wantNarrowingGuard(t, evoRefApply(t, ds, props),
			`reference "ref" narrows its target to `+evoRefPackage+`/gadget`, "1 live records")
	})

	t.Run("object field dropped", func(t *testing.T) {
		props := evoRefBaseProps()
		props["spec"] = map[string]any{"type": "object", "fields": map[string]any{
			"a": map[string]any{"type": "string"},
		}}
		wantNarrowingGuard(t, evoRefApply(t, ds, props),
			`object "spec" drops field "b"`, "1 live records")
	})

	t.Run("object field kind changed", func(t *testing.T) {
		props := evoRefBaseProps()
		props["spec"] = map[string]any{"type": "object", "fields": map[string]any{
			"a": map[string]any{"type": "string"},
			"b": map[string]any{"type": "string"},
		}}
		wantNarrowingGuard(t, evoRefApply(t, ds, props),
			`object "spec" field "b" changes kind int → string`, "1 live records")
	})
}

// evoRefDocs builds the authority with two referent kinds and a holder carrying a
// reference and an object property whose fields drive the object-narrowing
// checks.
func evoRefDocs(holderProps map[string]any) []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(evoRefPackage, 0),
		vocabulary.KindManifest(evoRefPackage, map[string]any{"singular": "widget"}, map[string]any{}),
		vocabulary.KindManifest(evoRefPackage, map[string]any{"singular": "gadget"}, map[string]any{}),
		vocabulary.KindManifest(evoRefPackage, map[string]any{"singular": "holder"},
			map[string]any{"properties": holderProps}),
	}
}

func evoRefBaseProps() map[string]any {
	return map[string]any{
		"ref": map[string]any{"type": "reference", "kind": "any"},
		"spec": map[string]any{"type": "object", "fields": map[string]any{
			"a": map[string]any{"type": "string"},
			"b": map[string]any{"type": "int"},
		}},
	}
}

func evoRefApply(t *testing.T, ds substrate.Dataset, holderProps map[string]any) error {
	t.Helper()
	_, err := ds.ApplyVocabularyDocuments(context.Background(), owner, evoRefDocs(holderProps))
	return err
}

const evoRefPackage = "evoref.example.substrate.reamde.dev/evoref"
