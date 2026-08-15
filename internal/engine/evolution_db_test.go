package engine_test

// Evolution-with-data: schema admission classifies
// the definition diff against the stored definition and refuses every
// NARROWING class with the live-row count, while additive changes admit
// freely and renamedFrom is admitted, stored and round-tripped as reserved.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const evoAuthority = "evo.example.substrate.reamde.dev"

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
	return vocabulary.KindManifest(evoAuthority,
		map[string]any{"singular": "gizmo", "plural": "gizmos"},
		map[string]any{"properties": props})
}

func evoApply(t *testing.T, ds substrate.Dataset, props map[string]any) error {
	t.Helper()
	_, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabulary.AuthorityManifest(evoAuthority, 0),
		evoTypeDoc(props),
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
		Kind: evoAuthority + "/gizmo",
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

	t.Run("property renamed via renamedFrom is still refused", func(t *testing.T) {
		props := evoBaseProps()
		delete(props, "size")
		props["dimensions"] = map[string]any{"type": "string", "renamedFrom": "size"}
		wantNarrowingGuard(t, evoApply(t, ds, props),
			`renamed to "dimensions"`, "reserved and not yet acted on", "1 live records")
	})

	t.Run("kind changed", func(t *testing.T) {
		props := evoBaseProps()
		props["size"] = map[string]any{"type": "int"}
		wantNarrowingGuard(t, evoApply(t, ds, props),
			`property "size" changes kind string → int`, "1 live records")
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
			Kind:       evoAuthority + "/gizmo",
			Properties: map[string]any{"size": "small"},
		})
		props := evoBaseProps()
		delete(props, "size")
		wantNarrowingGuard(t, evoApply(t, ds, props), "2 live records")
	})

	// Nothing above landed: the stored definition is still the base.
	ty, err := ds.Get(ctx, "core.substrate.reamde.dev/kind", evoAuthority+"/gizmo")
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
		Kind:       evoAuthority + "/gizmo",
		Properties: map[string]any{"size": "big", "level": "high", "phase": "done"},
	})

	// Removing an enum value NO live row holds admits: the guard counts, it
	// does not blanket-refuse the class.
	props := evoBaseProps()
	props["level"] = map[string]any{"type": "enum", "values": []any{"high"}}
	if err := evoApply(t, ds, props); err != nil {
		t.Fatalf("removing an unheld enum value must admit: %v", err)
	}

	// Purely additive: a new optional property, a returned enum value, a new
	// state with its transition, a new type version of nothing else.
	props["weight"] = map[string]any{"type": "float"}
	props["level"] = map[string]any{"type": "enum", "values": []any{"high", "low", "mid"}}
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
		Kind:       evoAuthority + "/gizmo",
		Properties: map[string]any{"weight": 1.5, "level": "mid"},
	})

	// Dropping the property after nulling it on every row admits: the guard
	// is refuse-while-stranded, not refuse-forever.
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{evoAuthority + "/gizmo"}}, First: 10})
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
		ty, err := ds.Get(ctx, "core.substrate.reamde.dev/kind", evoAuthority+"/gizmo")
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
