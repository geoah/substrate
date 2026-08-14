package engine_test

// The narrowing guards over the widened dialect: a field dropped, retyped or
// re-boxed at depth, a keyed map's key contract tightened, and a nested
// reference's target narrowed. Each arm is a GENERATED jsonb count — one notch
// per declared level, an array notch for a repeated container and a jsonb_each
// notch for a keyed one — so every one of them is exercised against a live row
// (it must refuse, naming the count) and against an empty store (it must
// admit). A query that never runs is a guard that does not exist.

import (
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// dnBaseProps is dwProps plus one keyed FIELD, which is the position the
// property-level arms cannot reach.
func dnBaseProps() map[string]any {
	props := dwProps()
	spec := props["spec"].(map[string]any)
	spec["fields"].(map[string]any)["tags"] = map[string]any{"type": "string", "keyed": true}
	props["plain"] = map[string]any{"type": "string"}
	return props
}

// dnRow carries a value at every position the arms count, so a guard that fails
// to reach one shows up as an admitted narrowing rather than a passing test.
func dnRow(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwAuthority + "/target", ID: "a", Properties: map[string]any{"name": "Ada"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwAuthority + "/holder", ID: "h1",
		Properties: map[string]any{
			"grant": map[string]any{"scopes": []any{"read"}, "subject": "ada"},
			"spec": map[string]any{
				"limits": map[string]any{"depth": 3, "ref": "a"},
				"tags":   map[string]any{"color": "green"},
			},
			"effects":  map[string]any{"createdRecords": 1},
			"notes":    map[string]any{"greeting": "hei"},
			"plain":    "text",
			"installs": map[string]any{"task": map[string]any{"version": "v1", "source": "a"}},
			"tools":    []any{map[string]any{"callable": "a", "label": "first"}},
		},
	})
}

// dnCases are the narrowing variants: each mutates the base declaration and
// names the fragment its guard must carry.
func dnCases() map[string]struct {
	mutate func(props map[string]any)
	says   string
} {
	// spec.fields, reached the same way by every case that edits a level-2 field.
	specFields := func(props map[string]any) map[string]any {
		return props["spec"].(map[string]any)["fields"].(map[string]any)
	}
	limitFields := func(props map[string]any) map[string]any {
		return specFields(props)["limits"].(map[string]any)["fields"].(map[string]any)
	}
	return map[string]struct {
		mutate func(props map[string]any)
		says   string
	}{
		"level-2 field dropped": {
			mutate: func(props map[string]any) { delete(limitFields(props), "depth") },
			says:   `object "spec.limits" drops field "depth"`,
		},
		"level-2 field retyped": {
			mutate: func(props map[string]any) {
				limitFields(props)["depth"] = map[string]any{"type": "string"}
			},
			says: `object "spec.limits" field "depth" changes kind int → string`,
		},
		"level-1 field list flattened": {
			mutate: func(props map[string]any) {
				props["grant"].(map[string]any)["fields"].(map[string]any)["scopes"] = map[string]any{"type": "string"}
			},
			says: `object "grant" field "scopes" changes kind repeated string → string`,
		},
		"keyed field flattened": {
			mutate: func(props map[string]any) {
				specFields(props)["tags"] = map[string]any{"type": "string"}
			},
			says: `object "spec" field "tags" changes kind keyed string → string`,
		},
		"keyed field tightens its keys": {
			mutate: func(props map[string]any) {
				specFields(props)["tags"] = map[string]any{
					"type": "string", "keyed": true, "keyPattern": "camel",
				}
			},
			says: `object "spec" field "tags" tightens its keys to camel`,
		},
		"keyed property flattened": {
			mutate: func(props map[string]any) { props["effects"] = map[string]any{"type": "int"} },
			says:   `property "effects" changes kind keyed int → int`,
		},
		"property becomes keyed": {
			mutate: func(props map[string]any) {
				props["plain"] = map[string]any{"type": "string", "keyed": true}
			},
			says: `property "plain" changes kind string → keyed string`,
		},
		"keyed property tightens its keys": {
			mutate: func(props map[string]any) {
				props["notes"] = map[string]any{"type": "string", "keyed": true, "keyPattern": "camel"}
			},
			says: `property "notes" tightens its keys to camel`,
		},
		"reference inside a repeated object narrows": {
			mutate: func(props map[string]any) {
				props["tools"].(map[string]any)["fields"].(map[string]any)["callable"] = map[string]any{"type": "reference", "to": "other"}
			},
			says: `object "tools" reference "callable" narrows its target to ` + dwAuthority + `/other`,
		},
		"reference inside a keyed map narrows": {
			mutate: func(props map[string]any) {
				props["installs"].(map[string]any)["fields"].(map[string]any)["source"] = map[string]any{"type": "reference", "to": "other"}
			},
			says: `object "installs" reference "source" narrows its target to ` + dwAuthority + `/other`,
		},
		"reference at level 3 narrows": {
			mutate: func(props map[string]any) {
				limitFields(props)["ref"] = map[string]any{"type": "reference", "to": "other"}
			},
			says: `object "spec.limits" reference "ref" narrows its target to ` + dwAuthority + `/other`,
		},
	}
}

// With a live row at every position, every arm refuses and says what stranded.
func TestNestedNarrowingsRefusedWithLiveRows(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := dwApply(t, ds, dnBaseProps()); err != nil {
		t.Fatalf("install the base authority: %v", err)
	}
	dnRow(t, ds)
	for name, c := range dnCases() {
		t.Run(name, func(t *testing.T) {
			props := dnBaseProps()
			c.mutate(props)
			wantNarrowingGuard(t, dwApply(t, ds, props), c.says, "1 live records")
		})
	}
}

// With nothing stored, every one of those queries runs and counts zero: the
// declaration lands. This is the half that catches a generated query that is
// malformed rather than merely wrong — a SQL error fails here too.
func TestNestedNarrowingsAdmittedWithoutRows(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := dwApply(t, ds, dnBaseProps()); err != nil {
		t.Fatalf("install the base authority: %v", err)
	}
	for name, c := range dnCases() {
		t.Run(name, func(t *testing.T) {
			props := dnBaseProps()
			c.mutate(props)
			if err := dwApply(t, ds, props); err != nil {
				t.Fatalf("no live rows, so the narrowing must land: %v", err)
			}
			// Back to the base, so the next subtest diffs against it.
			if err := dwApply(t, ds, dnBaseProps()); err != nil {
				t.Fatalf("restore the base declaration: %v", err)
			}
		})
	}
}

// The guard counts the rows that actually hold the value at the path, not every
// row of the kind: a holder with nothing at spec.limits does not strand.
func TestNestedNarrowingCountsOnlyTheRowsThatCarryTheValue(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := dwApply(t, ds, dnBaseProps()); err != nil {
		t.Fatalf("install the base authority: %v", err)
	}
	// Two rows, one of them carrying nothing at the narrowed path.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwAuthority + "/holder", ID: "empty", Properties: map[string]any{"plain": "text"},
	})
	props := dnBaseProps()
	delete(props["spec"].(map[string]any)["fields"].(map[string]any)["limits"].(map[string]any)["fields"].(map[string]any), "depth")
	if err := dwApply(t, ds, props); err != nil {
		t.Fatalf("a row that does not carry the field must not strand: %v", err)
	}

	// Now one that does, and the same drop refuses with a count of exactly one.
	// The declaration goes back first: the field it drops has to be declared
	// again before a row can carry it.
	if err := dwApply(t, ds, dnBaseProps()); err != nil {
		t.Fatalf("restore the base declaration: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwAuthority + "/holder", ID: "carrier",
		Properties: map[string]any{"spec": map[string]any{"limits": map[string]any{"depth": 1}}},
	})
	wantNarrowingGuard(t, dwApply(t, ds, props), `drops field "depth"`, "1 live records")
}
