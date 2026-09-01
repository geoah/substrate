package engine

// deriveRefs is the ONE function from a record's stored state to its rows in
// the refs index, and it is pure, so it is tested without a database. Every
// case here is an ADDRESSING case: what `property`, `path` and `ord` are for a
// shape, because those three plus the source record are the primary key, and a
// shape whose rows collide would silently lose pointers.

import (
	"strings"
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

// A KEYED MAP'S KEYS ARE FREE TEXT, and a key holding a dot must not spell the
// same address as a nesting that has the dot as its separator. Without the
// escape both sites below flatten to path "a.b.c", the two rows collide in the
// primary key (src, property, path, ord), and the write either loses a pointer
// or fails on the duplicate.
func TestDeriveRefsEscapesDottedKeyedMapKeys(t *testing.T) {
	t.Parallel()
	// `outer` is a keyed map of objects, each declaring a reference field. Key
	// "a.b" + field "c" against key "a" + field "b.c" is the collision from
	// issue #322. The second key's field name cannot itself carry a dot, so
	// the second half is a nested keyed map.
	inner := &vocabulary.Property{
		Datatype: vocabulary.DatatypeObject, Keyed: true,
		Fields:     map[string]*vocabulary.Property{"c": reference(nil)},
		FieldOrder: []string{"c"},
	}
	_ = inner
	outer := &vocabulary.Property{
		Datatype: vocabulary.DatatypeObject, Keyed: true,
		Fields:     map[string]*vocabulary.Property{"c": reference(nil)},
		FieldOrder: []string{"c"},
	}
	ty := derivedKind(map[string]*vocabulary.Property{"outer": outer}, "outer")

	got := deriveRefs(ty, map[string]any{
		"outer": map[string]any{
			"a.b": map[string]any{"c": hubPath("h1")},
			"a":   map[string]any{"c": hubPath("h2")},
		},
	})
	// "a.b" escapes to "a~1b", so the two rows are distinct addresses. Sorted
	// by path: "a.c" < "a~1b.c" (Go compares bytes, and '.' is 0x2e < '~').
	wantRows(t, got, []site{
		{"outer", "a.c", 0, "h2"},
		{"outer", "a~1b.c", 0, "h1"},
	})
}

// The escape is unambiguous both ways round: a key holding a literal "~1" must
// not decode to a key holding a dot, which is why "~" escapes first.
func TestDeriveRefsEscapesTheEscapeCharacter(t *testing.T) {
	t.Parallel()
	ty := derivedKind(map[string]*vocabulary.Property{
		"keys": reference(func(p *vocabulary.Property) { p.Keyed = true }),
	}, "keys")

	got := deriveRefs(ty, map[string]any{
		"keys": map[string]any{"a.b": hubPath("h1"), "a~1b": hubPath("h2")},
	})
	// "a~1b" escapes to "a~01b" and "a.b" to "a~1b": the two never meet.
	wantRows(t, got, []site{
		{"keys", "a~01b", 0, "h2"},
		{"keys", "a~1b", 0, "h1"},
	})
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

// THE FINGERPRINT SEES EVERY NODE a nested pointer is addressed through. A
// container or datatype change on an ANCESTOR moves (or removes) the address
// deriveRefs computes, so two declarations that differ there must not
// fingerprint the same: reprojectedKinds is what decides whether a stored
// record's rows are re-derived at all.
func TestReferenceShapeSeesTheContainersAboveAReference(t *testing.T) {
	t.Parallel()
	object := func(mods func(*vocabulary.Property)) *vocabulary.Property {
		p := &vocabulary.Property{
			Datatype:   vocabulary.DatatypeObject,
			Fields:     map[string]*vocabulary.Property{"callable": reference(nil)},
			FieldOrder: []string{"callable"},
		}
		if mods != nil {
			mods(p)
		}
		return p
	}
	shape := func(p *vocabulary.Property) string {
		var b strings.Builder
		appendReferenceShape(&b, "tool", p)
		return b.String()
	}

	single := shape(object(nil))
	for name, p := range map[string]*vocabulary.Property{
		// `callable` moves from "callable" to "0.callable".
		"repeated": object(func(p *vocabulary.Property) { p.Repeated = true }),
		// ... and to "<key>.callable".
		"keyed": object(func(p *vocabulary.Property) { p.Keyed = true }),
		// A datatype change stops the walk at the ancestor: the nested row goes.
		"json": object(func(p *vocabulary.Property) { p.Datatype = vocabulary.DatatypeJSON }),
	} {
		if got := shape(p); got == single {
			t.Fatalf("a %s ancestor fingerprints as the single one: %q", name, got)
		}
	}

	// A property with no reference under it contributes nothing, so an edit to
	// one never re-projects a kind's records.
	plain := &vocabulary.Property{
		Datatype:   vocabulary.DatatypeObject,
		Fields:     map[string]*vocabulary.Property{"note": {Datatype: vocabulary.DatatypeString}},
		FieldOrder: []string{"note"},
	}
	if got := shape(plain); got != "" {
		t.Fatalf("a reference-free property fingerprints as %q", got)
	}
}
