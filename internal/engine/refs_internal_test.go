package engine

// deriveRefs is the ONE function from a record's stored state to its rows in
// the refs index, and it is pure, so it is tested without a database. Every
// case here is an ADDRESSING case: what `property`, `path` and `ord` are for a
// shape, because those three plus the source record are the primary key, and a
// shape whose rows collide would silently lose pointers.

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

const derivedHub = "graph.example.substrate.reamde.dev/hub"

// hubPath renders a referent the derivation should read.
func hubPath(id string) string { return vocabulary.RecordPath(derivedHub, id) }

// derivedKind assembles a kind whose properties are the ones named, in order.
func derivedKind(props map[string]*vocabulary.Property, order ...string) *vocabulary.Kind {
	return &vocabulary.Kind{
		Identity: "graph.example.substrate.reamde.dev/src",
		Props:    props, PropOrder: order,
	}
}

func reference(mods func(*vocabulary.Property)) *vocabulary.Property {
	p := &vocabulary.Property{Datatype: vocabulary.DatatypeReference, To: derivedHub}
	if mods != nil {
		mods(p)
	}
	return p
}

// site is one expected row, spelled the way the primary key addresses it.
type site struct {
	property, path string
	ord            int
	dst            string
}

func wantRows(t *testing.T, got []refRow, want []site) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("derived %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Property != w.property || g.Path != w.path || g.Ord != w.ord || g.Dst.ID != w.dst {
			t.Fatalf("row %d = {%s %s %d -> %s}, want {%s %s %d -> %s}",
				i, g.Property, g.Path, g.Ord, g.Dst.ID, w.property, w.path, w.ord, w.dst)
		}
		if g.Dst.Kind != derivedHub {
			t.Fatalf("row %d points at kind %q, want %q", i, g.Dst.Kind, derivedHub)
		}
	}
}

func TestDeriveRefsAddressesEveryShape(t *testing.T) {
	t.Parallel()
	ty := derivedKind(map[string]*vocabulary.Property{
		"one":  reference(nil),
		"many": reference(func(p *vocabulary.Property) { p.Repeated = true }),
		"keys": reference(func(p *vocabulary.Property) { p.Keyed = true }),
	}, "keys", "many", "one")

	got := deriveRefs(ty, map[string]any{
		"one":  hubPath("h1"),
		"many": []any{hubPath("h2"), hubPath("h3")},
		"keys": map[string]any{"work": hubPath("h4"), "home": hubPath("h5")},
	})
	// Sorted by (property, path, ord), which is the index's own key order.
	wantRows(t, got, []site{
		{"keys", "home", 0, "h5"},
		{"keys", "work", 0, "h4"},
		// A repeated reference addresses its elements by `ord`, so its path
		// stays the property's own, empty.
		{"many", "", 0, "h2"},
		{"many", "", 1, "h3"},
		{"one", "", 0, "h1"},
	})
}

func TestDeriveRefsReachesNestedSites(t *testing.T) {
	t.Parallel()
	// A repeated OBJECT whose elements each hold a reference field: the element
	// index has to be a path segment, or two elements' rows would collide in
	// the primary key.
	tool := &vocabulary.Property{
		Datatype: vocabulary.DatatypeObject, Repeated: true,
		Fields:     map[string]*vocabulary.Property{"callable": reference(nil)},
		FieldOrder: []string{"callable"},
	}
	single := &vocabulary.Property{
		Datatype:   vocabulary.DatatypeObject,
		Fields:     map[string]*vocabulary.Property{"callable": reference(nil)},
		FieldOrder: []string{"callable"},
	}
	ty := derivedKind(map[string]*vocabulary.Property{"tools": tool, "tool": single}, "tool", "tools")

	got := deriveRefs(ty, map[string]any{
		"tool": map[string]any{"callable": hubPath("h0")},
		"tools": []any{
			map[string]any{"callable": hubPath("h1")},
			map[string]any{"callable": hubPath("h2")},
		},
	})
	wantRows(t, got, []site{
		{"tool", "callable", 0, "h0"},
		{"tools", "0.callable", 0, "h1"},
		{"tools", "1.callable", 0, "h2"},
	})
}

func TestDeriveRefsReadsTheLinkDataShape(t *testing.T) {
	t.Parallel()
	ty := derivedKind(map[string]*vocabulary.Property{
		"peer": reference(func(p *vocabulary.Property) {
			p.Repeated = true
			p.Properties = map[string]*vocabulary.Property{"role": {Datatype: vocabulary.DatatypeString}}
			p.PropertyOrder = []string{"role"}
		}),
	}, "peer")

	got := deriveRefs(ty, map[string]any{
		"peer": []any{
			map[string]any{vocabulary.ReferenceValueKey: hubPath("h1"), "role": "lead"},
			// A value stored before the link data was declared: still a pointer.
			hubPath("h2"),
		},
	})
	wantRows(t, got, []site{{"peer", "", 0, "h1"}, {"peer", "", 1, "h2"}})
	if got[0].Props["role"] != "lead" {
		t.Fatalf("the link data did not travel: %+v", got[0].Props)
	}
	if got[1].Props != nil {
		t.Fatalf("a bare path invented link data: %+v", got[1].Props)
	}
}

// A value the declaration cannot read yields NO ROW rather than a kindless one:
// the write path refuses these, so reaching one here means a row predates a
// declaration change, and the index says nothing about it rather than
// something wrong.
func TestDeriveRefsSkipsWhatItCannotRead(t *testing.T) {
	t.Parallel()
	ty := derivedKind(map[string]*vocabulary.Property{
		"one":   reference(nil),
		"many":  reference(func(p *vocabulary.Property) { p.Repeated = true }),
		"plain": {Datatype: vocabulary.DatatypeString},
	}, "many", "one", "plain")

	for _, props := range []map[string]any{
		{"one": nil},
		{"one": "not-a-path"},
		{"one": 7},
		{"many": "not-a-list"},
		{"many": []any{"not-a-path", 7}},
		{"plain": hubPath("h1")},
		{},
	} {
		if got := deriveRefs(ty, props); len(got) != 0 {
			t.Fatalf("%+v derived %+v, want nothing", props, got)
		}
	}
}

// A kind this binary no longer declares has no sites to walk, so it projects
// nothing: syncRefsOf hands a nil declaration here on exactly that row.
func TestDeriveRefsOfAnUnknownKindIsEmpty(t *testing.T) {
	t.Parallel()
	if got := deriveRefs(nil, map[string]any{"one": hubPath("h1")}); got != nil {
		t.Fatalf("an undeclared kind derived %+v", got)
	}
}
