package engine_test

// LINK DATA: the `properties:` block a reference declares, stored inside the
// value beside the pointer. A name it does not declare is refused, and the
// declared block evolves under the same narrowing guards a record property
// does.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const lpAuthority = "linkprop.example.substrate.reamde.dev"

const lpNode = lpAuthority + "/node"

// lpBaseProps is the declaration the subtests write against and diff from:
// `peer` carries four flat link properties, `plain` declares none at all.
func lpBaseProps() map[string]any {
	return map[string]any{
		"label": map[string]any{"type": "string"},
		"peer": map[string]any{
			"type": "reference", "kind": "node", "repeated": true,
			"properties": map[string]any{
				"role":  map[string]any{"type": "enum", "values": []any{map[string]any{"value": "lead"}, map[string]any{"value": "member"}}},
				"order": map[string]any{"type": "int"},
				"since": map[string]any{"type": "date"},
				"note":  map[string]any{"type": "string"},
			},
		},
		"plain": map[string]any{"type": "reference", "kind": "node", "repeated": true},
	}
}

func lpApply(t *testing.T, ds substrate.Dataset, props map[string]any) error {
	t.Helper()
	_, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabulary.AuthorityManifest(lpAuthority, 0),
		vocabulary.KindManifest(lpAuthority,
			map[string]any{"singular": "node", "plural": "nodes"},
			map[string]any{"displayTemplate": "{label}", "properties": props}),
	})
	return err
}

// lpLink renders one repeated-reference element in the stored object shape: the
// pointer under `ref`, the link data beside it.
func lpLink(id string, props map[string]any) any {
	out := map[string]any{vocabulary.ReferenceValueKey: id}
	for k, v := range props {
		out[k] = v
	}
	return out
}

func lpNodeRecord(t *testing.T, ds substrate.Dataset, label string, props map[string]any) *substrate.Record {
	t.Helper()
	in := map[string]any{"label": label}
	for k, v := range props {
		in[k] = v
	}
	return mustPut(t, ds, owner, substrate.PutInput{Kind: lpNode, Properties: in})
}

// lpPut is mustPut without the fatal: the refusals below are the point.
func lpPut(ds substrate.Dataset, label string, props map[string]any) error {
	in := map[string]any{"label": label}
	for k, v := range props {
		in[k] = v
	}
	_, err := ds.Put(context.Background(), owner, substrate.PutInput{Kind: lpNode, Properties: in})
	return err
}

func wantLinkPropRefusal(t *testing.T, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("link data the declaration does not admit must be refused")
	}
	wantErr(t, err, substrate.ErrValidation, "link property")
	for _, f := range fragments {
		if !strings.Contains(err.Error(), f) {
			t.Fatalf("the refusal must name %q, got: %v", f, err)
		}
	}
}

// linkDataOf reads the link data stored beside one target of a repeated
// reference, nil when the record does not name that target at all.
func linkDataOf(t *testing.T, ds substrate.Dataset, typ, id, property, target string) map[string]any {
	t.Helper()
	rec := mustGet(t, ds, typ, id)
	list, _ := rec.Properties[property].([]any)
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path, _ := m[vocabulary.ReferenceValueKey].(string)
		if _, refID, ok := vocabulary.SplitRecordPath(path); !ok || refID != target {
			continue
		}
		out := map[string]any{}
		for k, v := range m {
			if k != vocabulary.ReferenceValueKey {
				out[k] = v
			}
		}
		return out
	}
	return nil
}

func TestLinkPropertiesAreDeclaredOrRefused(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := lpApply(t, ds, lpBaseProps()); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	target := lpNodeRecord(t, ds, "target", nil)

	t.Run("declared values store, coerced", func(t *testing.T) {
		src := lpNodeRecord(t, ds, "declared", map[string]any{
			// `order` arrives as a float, the shape a JSON body decodes to.
			"peer": []any{lpLink(target.ID, map[string]any{"role": "lead", "order": float64(2), "since": "2019-04-01"})},
		})
		got := linkDataOf(t, ds, src.Kind, src.ID, "peer", target.ID)
		if got["role"] != "lead" || got["since"] != "2019-04-01" {
			t.Fatalf("stored link data = %+v", got)
		}
		if n, ok := got["order"].(float64); !ok || n != 2 {
			t.Fatalf("stored order = %#v, want the int 2", got["order"])
		}
	})

	t.Run("an undeclared name is refused", func(t *testing.T) {
		wantLinkPropRefusal(t, lpPut(ds, "undeclared", map[string]any{
			"peer": []any{lpLink(target.ID, map[string]any{"weight": 3})},
		}), `link property "weight" is not declared`)
	})

	t.Run("a reference that declares no block accepts nothing", func(t *testing.T) {
		// Without a `properties:` block the value is a bare path, so the object
		// shape is the retired {kind, id} pair as far as coercion is concerned.
		err := lpPut(ds, "noblock", map[string]any{
			"plain": []any{lpLink(target.ID, map[string]any{"role": "lead"})},
		})
		if err == nil {
			t.Fatal("a reference declaring no link data must refuse an object value")
		}
		wantErr(t, err, substrate.ErrValidation, "props.plain")
	})

	t.Run("a wrong-typed value is refused", func(t *testing.T) {
		wantLinkPropRefusal(t, lpPut(ds, "wrongtype", map[string]any{
			"peer": []any{lpLink(target.ID, map[string]any{"order": "second"})},
		}), `link property "order"`)
	})

	t.Run("a value outside the enum is refused", func(t *testing.T) {
		wantLinkPropRefusal(t, lpPut(ds, "outsideenum", map[string]any{
			"peer": []any{lpLink(target.ID, map[string]any{"role": "auditor"})},
		}), `link property "role"`)
	})

	t.Run("a bare path normalizes to the object shape", func(t *testing.T) {
		// Every link property here is optional, so naming the referent alone is
		// a whole value: a writer that has nothing to say beside the pointer
		// must not have to spell the wrapper.
		src := lpNodeRecord(t, ds, "bare", map[string]any{"peer": []any{target.ID}})
		got := linkDataOf(t, ds, src.Kind, src.ID, "peer", target.ID)
		if got == nil {
			t.Fatal("a bare path did not store as a reference at all")
		}
		if len(got) != 0 {
			t.Fatalf("a bare path invented link data: %+v", got)
		}
	})

	t.Run("a null value is dropped, not stored", func(t *testing.T) {
		src := lpNodeRecord(t, ds, "nulled", map[string]any{
			"peer": []any{lpLink(target.ID, map[string]any{"role": "lead", "order": nil})},
		})
		got := linkDataOf(t, ds, src.Kind, src.ID, "peer", target.ID)
		if _, held := got["order"]; held {
			t.Fatalf("a null link property must not store: %+v", got)
		}
	})
}

// A SOFT DELETE LEAVES EVERY POINTER STANDING. The pointer is a value on the
// record, so a tombstone touches nothing about it, and undelete is a put that
// names no reference at all: the links come back because they never left. A
// cascade here would make a soft delete irreversible.
func TestTombstoneKeepsReferencesAndUndeleteRestoresThem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := lpApply(t, ds, lpBaseProps()); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	target := lpNodeRecord(t, ds, "target", nil)
	src := lpNodeRecord(t, ds, "src", map[string]any{
		"peer": []any{lpLink(target.ID, map[string]any{"role": "lead", "order": 1})},
	})

	if _, err := ds.Delete(ctx, owner, src.Kind, src.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	gone := mustGet(t, ds, src.Kind, src.ID)
	if gone.DeletedAt == nil {
		t.Fatal("delete must tombstone, not hard-delete")
	}
	kept := linkDataOf(t, ds, src.Kind, src.ID, "peer", target.ID)
	if kept == nil {
		t.Fatal("a tombstone dropped the record's pointers; undelete could never rebuild them")
	}
	if kept["role"] != "lead" {
		t.Fatalf("the tombstoned record's link data changed: %+v", kept)
	}

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: lpNode, ID: src.ID, Properties: map[string]any{"label": "src again"},
	})
	back := linkDataOf(t, ds, src.Kind, src.ID, "peer", target.ID)
	if back == nil {
		t.Fatal("undelete returned a record stripped of its references")
	}
	if back["role"] != "lead" {
		t.Fatalf("undeleted link data = %+v", back)
	}
}

// `required:` on a link property is enforced at the write. It can mean one
// thing because a reference value is written WHOLE — a write replaces the
// property, it does not reach inside the value — so every stored value of the
// reference carries it.
func TestRequiredLinkPropertyEnforced(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	props := lpBaseProps()
	props["peer"].(map[string]any)["properties"].(map[string]any)["role"] = map[string]any{
		"type": "enum", "required": true,
		"values": []any{map[string]any{"value": "lead"}, map[string]any{"value": "member"}},
	}
	if err := lpApply(t, ds, props); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	target := lpNodeRecord(t, ds, "target", nil)

	wantLinkPropRefusal(t, lpPut(ds, "norole", map[string]any{"peer": []any{target.ID}}),
		`link property "role" requires a value`)
	wantLinkPropRefusal(t, lpPut(ds, "otheronly", map[string]any{
		"peer": []any{lpLink(target.ID, map[string]any{"order": 1})},
	}), `link property "role" requires a value`)

	src := lpNodeRecord(t, ds, "withrole", map[string]any{
		"peer": []any{lpLink(target.ID, map[string]any{"role": "lead"})},
	})
	if got := linkDataOf(t, ds, src.Kind, src.ID, "peer", target.ID); got["role"] != "lead" {
		t.Fatalf("stored link data = %+v", got)
	}
}

// A reference's own keys evolve under counted guards, exactly as a record
// property's do: the count says how many stored values the new declaration
// would not admit, and the apply is refused until they are migrated.
func TestReferenceNarrowingRefused(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := lpApply(t, ds, lpBaseProps()); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	target := lpNodeRecord(t, ds, "target", nil)
	other := lpNodeRecord(t, ds, "other", nil)
	// One live source naming TWO peers, one of them carrying every declared
	// link property: every narrowing below strands it.
	lpNodeRecord(t, ds, "src", map[string]any{
		"peer": []any{
			lpLink(target.ID, map[string]any{"role": "lead", "order": 1, "since": "2019-04-01", "note": "hello"}),
			lpLink(other.ID, nil),
		},
	})

	t.Run("reference dropped", func(t *testing.T) {
		props := lpBaseProps()
		delete(props, "peer")
		wantNarrowingGuard(t, lpApply(t, ds, props),
			`property "peer" dropped`, "1 live records")
	})

	t.Run("reference stops being repeated", func(t *testing.T) {
		props := lpBaseProps()
		delete(props["peer"].(map[string]any), "repeated")
		wantNarrowingGuard(t, lpApply(t, ds, props),
			`property "peer" changes kind`, "1 live records")
	})

	t.Run("reference target narrows", func(t *testing.T) {
		props := lpBaseProps()
		delete(props["peer"].(map[string]any), "kind")
		if err := lpApply(t, ds, props); err != nil {
			t.Fatalf("widening a pin to unconstrained is additive: %v", err)
		}
		props["peer"].(map[string]any)["kind"] = "core.substrate.reamde.dev/kind"
		wantNarrowingGuard(t, lpApply(t, ds, props),
			`reference "peer" narrows its target`, "1 live records")
		// Put the declaration back, so the link-property subtests diff from the
		// base.
		if err := lpApply(t, ds, lpBaseProps()); err != nil {
			t.Fatalf("restore the base declaration: %v", err)
		}
	})

	t.Run("link property dropped", func(t *testing.T) {
		props := lpBaseProps()
		delete(props["peer"].(map[string]any)["properties"].(map[string]any), "role")
		wantNarrowingGuard(t, lpApply(t, ds, props),
			`reference "peer" drops link property "role"`, "1 live references")
	})

	t.Run("link property retyped", func(t *testing.T) {
		props := lpBaseProps()
		props["peer"].(map[string]any)["properties"].(map[string]any)["order"] = map[string]any{"type": "string"}
		wantNarrowingGuard(t, lpApply(t, ds, props),
			`reference "peer" changes link property "order" from int to string`, "1 live references")
	})

	// The two retypes the record loop counts BY VALUE are counted by value here
	// too. A link property held stricter than the identical record property
	// would close the "values leading, declaration following" path for no
	// reason: a backfill rewrites the stored values and the declaration then
	// follows them.
	t.Run("retype to int counts non-integers, not carriers", func(t *testing.T) {
		props := lpBaseProps()
		// `since` holds the date string "2019-04-01", which is not a number.
		props["peer"].(map[string]any)["properties"].(map[string]any)["since"] = map[string]any{"type": "int"}
		wantNarrowingGuard(t, lpApply(t, ds, props),
			`reference "peer" changes link property "since" from date to int`,
			"values that are not integers", "1 live references")
	})

	t.Run("link property loses an enum value", func(t *testing.T) {
		props := lpBaseProps()
		props["peer"].(map[string]any)["properties"].(map[string]any)["role"] = map[string]any{
			"type": "enum", "values": []any{map[string]any{"value": "member"}},
		}
		wantNarrowingGuard(t, lpApply(t, ds, props),
			`reference "peer" removes value(s) "lead" from link property "role"`, "1 live references")
	})

	t.Run("link property becomes required", func(t *testing.T) {
		props := lpBaseProps()
		props["peer"].(map[string]any)["properties"].(map[string]any)["order"] = map[string]any{
			"type": "int", "required": true,
		}
		wantNarrowingGuard(t, lpApply(t, ds, props),
			`reference "peer" makes link property "order" required`, "1 live references")
	})

	t.Run("link property added as required", func(t *testing.T) {
		props := lpBaseProps()
		props["peer"].(map[string]any)["properties"].(map[string]any)["weight"] = map[string]any{
			"type": "int", "required": true,
		}
		wantNarrowingGuard(t, lpApply(t, ds, props),
			`reference "peer" adds link property "weight" as required`, "2 live references")
	})

	t.Run("an added optional link property is additive", func(t *testing.T) {
		props := lpBaseProps()
		props["peer"].(map[string]any)["properties"].(map[string]any)["weight"] = map[string]any{"type": "int"}
		if err := lpApply(t, ds, props); err != nil {
			t.Fatalf("adding an optional link property must be additive: %v", err)
		}
		if err := lpApply(t, ds, lpBaseProps()); err != nil {
			t.Fatalf("restore the base declaration: %v", err)
		}
	})

	// `onDelete:` says what a future GC sweep does with the record, not which
	// values are admissible, so adding it strands nothing.
	t.Run("onDelete is not a narrowing", func(t *testing.T) {
		props := lpBaseProps()
		props["owner"] = map[string]any{
			"type": "reference", "kind": "node", "onDelete": "cascade",
		}
		if err := lpApply(t, ds, props); err != nil {
			t.Fatalf("declaring onDelete must be additive: %v", err)
		}
		if err := lpApply(t, ds, lpBaseProps()); err != nil {
			t.Fatalf("restore the base declaration: %v", err)
		}
	})
}

// `mustExist: true` added to a reference is a narrowing with a real count
// behind it: a stored pointer at a record that never arrived is one no later
// write to that row could re-assert.
func TestMustExistAddedIsANarrowing(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	props := lpBaseProps()
	props["ghost"] = map[string]any{"type": "reference", "kind": "node"}
	if err := lpApply(t, ds, props); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	// A DANGLING pointer: legal without mustExist, and exactly what the
	// declaration change would stop admitting.
	lpNodeRecord(t, ds, "dangler", map[string]any{"ghost": "never-existed"})

	props["ghost"].(map[string]any)["mustExist"] = true
	wantNarrowingGuard(t, lpApply(t, ds, props),
		`reference "ghost" requires its target to exist`, "1 live references")
}

// mustExist is the admission edges used to give, moved onto the reference: the
// write is refused when the referent is not there, and a TOMBSTONE counts as
// there, because the record exists and a split can bring it back.
func TestMustExistRefusesAMissingTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	props := lpBaseProps()
	props["owner"] = map[string]any{"type": "reference", "kind": "node", "mustExist": true}
	if err := lpApply(t, ds, props); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	target := lpNodeRecord(t, ds, "target", nil)

	err := lpPut(ds, "missing", map[string]any{"owner": "no-such-node"})
	if err == nil {
		t.Fatal("mustExist admitted a reference naming no record")
	}
	wantErr(t, err, substrate.ErrNotFound, "does not exist")

	src := lpNodeRecord(t, ds, "present", map[string]any{"owner": target.ID})
	if got := refPathOf(t, ds, src.Kind, src.ID, "owner"); got != vocabulary.RecordPath(lpNode, target.ID) {
		t.Fatalf("stored owner = %q", got)
	}

	if _, err := ds.Delete(ctx, owner, target.Kind, target.ID); err != nil {
		t.Fatalf("delete the target: %v", err)
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: lpNode, ID: src.ID, Properties: map[string]any{"label": "still pointing", "owner": target.ID},
	}); err != nil {
		t.Fatalf("mustExist refused a tombstoned target, which still exists: %v", err)
	}
}

// A repeated reference is an ordered SET of targets: the same record twice
// says nothing a single entry does not, and the refs index would carry one
// pointer under two ordinals, so a reverse read would report it twice.
func TestRepeatedReferenceRefusesADuplicate(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := lpApply(t, ds, lpBaseProps()); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	target := lpNodeRecord(t, ds, "target", nil)

	err := lpPut(ds, "twice", map[string]any{
		"peer": []any{
			lpLink(target.ID, map[string]any{"role": "lead"}),
			lpLink(target.ID, map[string]any{"role": "member"}),
		},
	})
	if err == nil {
		t.Fatal("a repeated reference naming one record twice must be refused")
	}
	wantErr(t, err, substrate.ErrValidation, "twice")
}

// refPathOf reads a single-valued reference's stored path off a record.
func refPathOf(t *testing.T, ds substrate.Dataset, typ, id, property string) string {
	t.Helper()
	rec := mustGet(t, ds, typ, id)
	s, _ := rec.Properties[property].(string)
	return s
}
