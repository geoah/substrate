package engine_test

// An edge row's properties are DECLARED (`edges.<rel>.properties`) or they are
// refused, and the declared block evolves under the same narrowing guards a
// property does.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const epAuthority = "edgeprop.example.substrate.reamde.dev"

const epNode = epAuthority + "/node"

// epBaseEdges is the declaration the subtests write against and diff from:
// `peer` carries three flat values, `plain` declares none at all.
func epBaseEdges() map[string]any {
	return map[string]any{
		"peer": map[string]any{
			"to": "node", "many": true,
			"properties": map[string]any{
				"role":  map[string]any{"type": "enum", "values": []any{map[string]any{"value": "lead"}, map[string]any{"value": "member"}}},
				"order": map[string]any{"type": "int"},
				"since": map[string]any{"type": "date"},
				"note":  map[string]any{"type": "string"},
			},
		},
		"plain": map[string]any{"to": "node", "many": true},
	}
}

func epApply(t *testing.T, ds substrate.Dataset, edges map[string]any) error {
	t.Helper()
	_, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabulary.AuthorityManifest(epAuthority, 0),
		vocabulary.KindManifest(epAuthority,
			map[string]any{"singular": "node", "plural": "nodes"},
			map[string]any{
				"displayTemplate": "{label}",
				"properties":      map[string]any{"label": map[string]any{"type": "string"}},
				"edges":           edges,
			}),
	})
	return err
}

func epNodeRecord(t *testing.T, ds substrate.Dataset, label string, edges ...substrate.EdgeInput) *substrate.Record {
	t.Helper()
	return mustPut(t, ds, owner, substrate.PutInput{
		Kind: epNode, Properties: map[string]any{"label": label}, Edges: edges,
	})
}

// epPut is mustPut without the fatal: the refusals below are the point.
func epPut(ds substrate.Dataset, label string, edges ...substrate.EdgeInput) error {
	_, err := ds.Put(context.Background(), owner, substrate.PutInput{
		Kind: epNode, Properties: map[string]any{"label": label}, Edges: edges,
	})
	return err
}

func wantEdgePropRefusal(t *testing.T, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("an edge property the declaration does not admit must be refused")
	}
	wantErr(t, err, substrate.ErrValidation, "edge property")
	for _, f := range fragments {
		if !strings.Contains(err.Error(), f) {
			t.Fatalf("the refusal must name %q, got: %v", f, err)
		}
	}
}

func TestEdgePropertiesAreDeclaredOrRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := epApply(t, ds, epBaseEdges()); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	target := epNodeRecord(t, ds, "target")
	ref := substrate.EdgeRef{ID: target.ID}

	t.Run("declared values store, coerced", func(t *testing.T) {
		src := epNodeRecord(t, ds, "declared", substrate.EdgeInput{
			Rel: "peer", To: ref,
			// `order` arrives as a float, the shape a JSON body decodes to.
			Properties: map[string]any{"role": "lead", "order": float64(2), "since": "2019-04-01"},
		})
		got := edgePropsOf(t, ds, src.Kind, src.ID, "peer", target.ID)
		if got["role"] != "lead" || got["since"] != "2019-04-01" {
			t.Fatalf("stored edge props = %+v", got)
		}
		if n, ok := got["order"].(float64); !ok || n != 2 {
			t.Fatalf("stored order = %#v, want the int 2", got["order"])
		}
	})

	t.Run("an undeclared name is refused", func(t *testing.T) {
		wantEdgePropRefusal(t, epPut(ds, "undeclared", substrate.EdgeInput{
			Rel: "peer", To: ref, Properties: map[string]any{"weight": 3},
		}), "edges.peer.properties.weight", "not declared")
	})

	t.Run("a rel that declares no block accepts nothing", func(t *testing.T) {
		wantEdgePropRefusal(t, epPut(ds, "noblock", substrate.EdgeInput{
			Rel: "plain", To: ref, Properties: map[string]any{"role": "lead"},
		}), "edges.plain.properties.role", "not declared")
	})

	t.Run("a wrong-typed value is refused", func(t *testing.T) {
		wantEdgePropRefusal(t, epPut(ds, "wrongtype", substrate.EdgeInput{
			Rel: "peer", To: ref, Properties: map[string]any{"order": "second"},
		}), "edges.peer.properties.order")
	})

	t.Run("a value outside the enum is refused", func(t *testing.T) {
		wantEdgePropRefusal(t, epPut(ds, "outsideenum", substrate.EdgeInput{
			Rel: "peer", To: ref, Properties: map[string]any{"role": "auditor"},
		}), "edges.peer.properties.role")
	})

	t.Run("the link verb is held to the same declaration", func(t *testing.T) {
		src := epNodeRecord(t, ds, "linked")
		err := ds.Link(ctx, owner, epNode, src.ID, "peer", ref, map[string]any{"weight": 3})
		wantEdgePropRefusal(t, err, "edges.peer.properties.weight", "not declared")
		if err := ds.Link(ctx, owner, epNode, src.ID, "peer", ref, map[string]any{"role": "member"}); err != nil {
			t.Fatalf("a declared edge property must link: %v", err)
		}
		if got := edgePropsOf(t, ds, epNode, src.ID, "peer", target.ID); got["role"] != "member" {
			t.Fatalf("linked edge props = %+v", got)
		}
	})

	t.Run("a null value is dropped, not stored", func(t *testing.T) {
		src := epNodeRecord(t, ds, "nulled", substrate.EdgeInput{
			Rel: "peer", To: ref, Properties: map[string]any{"role": "lead", "order": nil},
		})
		got := edgePropsOf(t, ds, src.Kind, src.ID, "peer", target.ID)
		if _, held := got["order"]; held {
			t.Fatalf("a null edge property must not store: %+v", got)
		}
	})
}

// A SOFT DELETE LEAVES EVERY EDGE ROW STANDING (record 0027). This is the half
// of the dangling split nothing held: the merge tests cover split's restore,
// which is a different mechanism (moveEdges deletes the loser's rows and parks
// them in the merge record), and a plain tombstone touches `records` alone.
//
// Undelete is what the rule is for. A cascade here would make a soft delete
// irreversible, because nothing anywhere would hold the links to rebuild.
func TestTombstoneKeepsEdgeRowsAndUndeleteRestoresThem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := epApply(t, ds, epBaseEdges()); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	target := epNodeRecord(t, ds, "target")
	src := epNodeRecord(t, ds, "src", substrate.EdgeInput{
		Rel: "peer", To: substrate.EdgeRef{ID: target.ID},
		Properties: map[string]any{"role": "lead", "order": 1},
	})

	if _, err := ds.Delete(ctx, owner, src.Kind, src.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	gone := mustGet(t, ds, src.Kind, src.ID)
	if gone.DeletedAt == nil {
		t.Fatal("delete must tombstone, not hard-delete")
	}
	kept := edgePropsOf(t, ds, src.Kind, src.ID, "peer", target.ID)
	if kept == nil {
		t.Fatal("a tombstone dropped the record's edge rows; undelete could never rebuild them")
	}
	if kept["role"] != "lead" {
		t.Fatalf("the tombstoned record's edge props changed: %+v", kept)
	}

	// Undelete is a put on the tombstoned record, naming no edge at all. The
	// links come back with it because they never left.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: epNode, ID: src.ID, Properties: map[string]any{"label": "src again"},
	})
	back := edgePropsOf(t, ds, src.Kind, src.ID, "peer", target.ID)
	if back == nil {
		t.Fatal("undelete returned a record stripped of its edges")
	}
	if back["role"] != "lead" {
		t.Fatalf("undeleted edge props = %+v", back)
	}
}

// `required:` on an edge property is enforced at the write, which is what the
// core declaration has always said it means ("an edge written without it is
// refused"). It can mean one thing here because an edge write REPLACES the
// row's whole props map, so every stored row of the rel carries it.
func TestRequiredEdgePropertyEnforced(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	edges := epBaseEdges()
	edges["peer"].(map[string]any)["properties"].(map[string]any)["role"] = map[string]any{
		"type": "enum", "required": true,
		"values": []any{map[string]any{"value": "lead"}, map[string]any{"value": "member"}},
	}
	if err := epApply(t, ds, edges); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	target := epNodeRecord(t, ds, "target")
	ref := substrate.EdgeRef{ID: target.ID}

	wantEdgePropRefusal(t, epPut(ds, "norole", substrate.EdgeInput{Rel: "peer", To: ref}),
		"edges.peer.properties.role", "requires a value")
	wantEdgePropRefusal(t, epPut(ds, "otheronly", substrate.EdgeInput{
		Rel: "peer", To: ref, Properties: map[string]any{"order": 1},
	}), "edges.peer.properties.role", "requires a value")

	src := epNodeRecord(t, ds, "withrole", substrate.EdgeInput{
		Rel: "peer", To: ref, Properties: map[string]any{"role": "lead"},
	})
	if got := edgePropsOf(t, ds, src.Kind, src.ID, "peer", target.ID); got["role"] != "lead" {
		t.Fatalf("stored edge props = %+v", got)
	}
}

// Edges were classified by nothing: an edge dropped, `many:` taken away and
// `to:` repointed all landed silently, leaving stored rows in the `edges`
// table no declaration described.
func TestEdgeNarrowingRefused(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := epApply(t, ds, epBaseEdges()); err != nil {
		t.Fatalf("install base type: %v", err)
	}
	target := epNodeRecord(t, ds, "target")
	ref := substrate.EdgeRef{ID: target.ID}
	// One live source holding TWO peer edges, one of them carrying every
	// declared property: every narrowing below strands it.
	other := epNodeRecord(t, ds, "other")
	epNodeRecord(t, ds, "src",
		substrate.EdgeInput{
			Rel: "peer", To: ref,
			Properties: map[string]any{"role": "lead", "order": 1, "since": "2019-04-01", "note": "hello"},
		},
		substrate.EdgeInput{Rel: "peer", To: substrate.EdgeRef{ID: other.ID}},
	)

	t.Run("edge dropped", func(t *testing.T) {
		edges := epBaseEdges()
		delete(edges, "peer")
		wantNarrowingGuard(t, epApply(t, ds, edges),
			`edge "peer" dropped`, "2 live edges")
	})

	t.Run("edge stops being many", func(t *testing.T) {
		edges := epBaseEdges()
		delete(edges["peer"].(map[string]any), "many")
		wantNarrowingGuard(t, epApply(t, ds, edges),
			`edge "peer" stops being many`, "1 live records")
	})

	t.Run("edge target narrows", func(t *testing.T) {
		edges := epBaseEdges()
		edges["peer"].(map[string]any)["to"] = "any"
		if err := epApply(t, ds, edges); err != nil {
			t.Fatalf("widening a target to any is additive: %v", err)
		}
		edges["peer"].(map[string]any)["to"] = "core.substrate.reamde.dev/kind"
		wantNarrowingGuard(t, epApply(t, ds, edges),
			`edge "peer" narrows its target`, "2 live edges")
		// Put the declaration back, so the property subtests diff from the base.
		if err := epApply(t, ds, epBaseEdges()); err != nil {
			t.Fatalf("restore the base declaration: %v", err)
		}
	})

	t.Run("edge property dropped", func(t *testing.T) {
		edges := epBaseEdges()
		delete(edges["peer"].(map[string]any)["properties"].(map[string]any), "role")
		wantNarrowingGuard(t, epApply(t, ds, edges),
			`edge "peer" drops property "role"`, "1 live edges")
	})

	t.Run("edge property retyped", func(t *testing.T) {
		edges := epBaseEdges()
		edges["peer"].(map[string]any)["properties"].(map[string]any)["order"] = map[string]any{"type": "string"}
		wantNarrowingGuard(t, epApply(t, ds, edges),
			`edge "peer" changes property "order" from int to string`, "1 live edges")
	})

	// The two retypes the record loop counts BY VALUE are counted by value here
	// too. An edge property held stricter than the identical record property
	// would close the "values leading, declaration following" path for no
	// reason: a backfill rewrites the stored values and the declaration then
	// follows them.
	t.Run("retype to int counts non-integers, not carriers", func(t *testing.T) {
		edges := epBaseEdges()
		// `since` holds the date string "2019-04-01", which is not a number.
		edges["peer"].(map[string]any)["properties"].(map[string]any)["since"] = map[string]any{"type": "int"}
		wantNarrowingGuard(t, epApply(t, ds, edges),
			`edge "peer" changes property "since" from date to int`,
			"values that are not integers", "1 live edges")
	})

	t.Run("edge property loses an enum value", func(t *testing.T) {
		edges := epBaseEdges()
		edges["peer"].(map[string]any)["properties"].(map[string]any)["role"] = map[string]any{
			"type": "enum", "values": []any{map[string]any{"value": "member"}},
		}
		wantNarrowingGuard(t, epApply(t, ds, edges),
			`edge "peer" removes value(s) "lead"`, "1 live edges")
	})

	t.Run("edge property becomes required", func(t *testing.T) {
		edges := epBaseEdges()
		edges["peer"].(map[string]any)["properties"].(map[string]any)["order"] = map[string]any{
			"type": "int", "required": true,
		}
		wantNarrowingGuard(t, epApply(t, ds, edges),
			`edge "peer" makes property "order" required`, "1 live edges")
	})

	t.Run("edge property added as required", func(t *testing.T) {
		edges := epBaseEdges()
		edges["peer"].(map[string]any)["properties"].(map[string]any)["weight"] = map[string]any{
			"type": "int", "required": true,
		}
		wantNarrowingGuard(t, epApply(t, ds, edges),
			`edge "peer" adds property "weight" as required`, "2 live edges")
	})

	t.Run("an added optional edge property is additive", func(t *testing.T) {
		edges := epBaseEdges()
		edges["peer"].(map[string]any)["properties"].(map[string]any)["weight"] = map[string]any{"type": "int"}
		if err := epApply(t, ds, edges); err != nil {
			t.Fatalf("adding an optional edge property must admit: %v", err)
		}
	})

	// LAST, because its admitting half leaves `note` an enum and the way back
	// (enum → string) is an ordinary kind change the guard refuses, exactly as
	// it does on a record property. Every subtest above wants the base
	// declaration.
	t.Run("retype string to enum counts values outside the set", func(t *testing.T) {
		// `note` holds "hello" on the live edge, so a set without it strands
		// that row and a set with it strands nothing. A presence count would
		// refuse both.
		edges := epBaseEdges()
		edges["peer"].(map[string]any)["properties"].(map[string]any)["note"] = map[string]any{
			"type": "enum", "values": []any{map[string]any{"value": "bye"}},
		}
		wantNarrowingGuard(t, epApply(t, ds, edges),
			`edge "peer" changes property "note" from string to enum`,
			`a value outside "bye"`, "1 live edges")

		edges = epBaseEdges()
		edges["peer"].(map[string]any)["properties"].(map[string]any)["note"] = map[string]any{
			"type": "enum", "values": []any{map[string]any{"value": "hello"}, map[string]any{"value": "bye"}},
		}
		if err := epApply(t, ds, edges); err != nil {
			t.Fatalf("string → enum admitting the stored value must admit: %v", err)
		}
	})
}
