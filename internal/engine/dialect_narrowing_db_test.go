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
	// A keyed enum FIELD: the last position an enum can occupy, and the one where
	// both the container walk and the value comparison have to be right at once.
	spec["fields"].(map[string]any)["slots"] = map[string]any{
		"type": "enum", "values": []any{"low", "high"}, "keyed": true,
	}
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
				"mode":   "high",
				"limits": map[string]any{"depth": 3, "ref": "a", "grade": "high"},
				// Keys no camelCase contract would admit: a tightening is a
				// narrowing for the rows holding a key it REFUSES, so the row that
				// drives those arms has to hold one.
				"tags":  map[string]any{"Not Camel": "green"},
				"slots": map[string]any{"primary": "high"},
			},
			"effects": map[string]any{"createdRecords": 1},
			"notes":   map[string]any{"not a camel key": "hei"},
			"plain":   "text",
			"installs": map[string]any{
				"task": map[string]any{"version": "v1", "source": "a", "channel": "high"},
			},
			"tools":  []any{map[string]any{"callable": "a", "label": "first", "role": "high"}},
			"level":  "high",
			"levels": []any{"high"},
			"slots":  map[string]any{"primary": "high"},
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
		// An enum value removed, once per container an enum can sit in. The keyed
		// one is the case a containment test on the whole map could never see.
		"scalar enum value removed": {
			mutate: func(props map[string]any) { props["level"] = dwEnumNarrowed() },
			says:   `property "level" removes value(s) "high"`,
		},
		"repeated enum value removed": {
			mutate: func(props map[string]any) {
				props["levels"] = map[string]any{"type": "enum", "values": []any{"low"}, "repeated": true}
			},
			says: `property "levels" removes value(s) "high"`,
		},
		"keyed enum value removed": {
			mutate: func(props map[string]any) {
				props["slots"] = map[string]any{"type": "enum", "values": []any{"low"}, "keyed": true}
			},
			says: `property "slots" removes value(s) "high"`,
		},
		"field enum value removed": {
			mutate: func(props map[string]any) { specFields(props)["mode"] = dwEnumNarrowed() },
			says:   `object "spec" field "mode" removes value(s) "high"`,
		},
		"field enum value removed at level 3": {
			mutate: func(props map[string]any) { limitFields(props)["grade"] = dwEnumNarrowed() },
			says:   `object "spec.limits" field "grade" removes value(s) "high"`,
		},
		"field enum value removed inside a repeated object": {
			mutate: func(props map[string]any) {
				props["tools"].(map[string]any)["fields"].(map[string]any)["role"] = dwEnumNarrowed()
			},
			says: `object "tools" field "role" removes value(s) "high"`,
		},
		"field enum value removed inside a keyed map": {
			mutate: func(props map[string]any) {
				props["installs"].(map[string]any)["fields"].(map[string]any)["channel"] = dwEnumNarrowed()
			},
			says: `object "installs" field "channel" removes value(s) "high"`,
		},
		"keyed field enum value removed": {
			mutate: func(props map[string]any) {
				specFields(props)["slots"] = map[string]any{
					"type": "enum", "values": []any{"low"}, "keyed": true,
				}
			},
			says: `object "spec" field "slots" removes value(s) "high"`,
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

// A guard must refuse a narrowing that strands data and ADMIT one the data
// already satisfies. This is the direction that fails silently in the other
// sense: a guard which refuses everything reads as "safe" until it blocks a
// legitimate evolution nobody can then perform.
//
// The three cases here each counted every populated row before: an absent
// optional reference inside a present object, a keyed reference read as one
// value (its `kind` is the MAP's, which no map has), and a tightened key
// contract every stored key already satisfies.
func TestNarrowingAdmitsWhatTheDataAlreadySatisfies(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := dwApply(t, ds, dnBaseProps()); err != nil {
		t.Fatalf("install the base authority: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwAuthority + "/target", ID: "a", Properties: map[string]any{"name": "Ada"},
	})
	target := map[string]any{"kind": dwAuthority + "/target", "id": "a"}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwAuthority + "/holder", ID: "conforms",
		Properties: map[string]any{
			// The object is present and its optional reference is not.
			"loose":     map[string]any{"note": "no pointer here"},
			"keyedRefs": map[string]any{"primary": target},
			"notes":     map[string]any{"greeting": "hei"},
		},
	})
	// An EMPTY keyed map is still a stored value, and it points nowhere.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwAuthority + "/holder", ID: "empties",
		Properties: map[string]any{"keyedRefs": map[string]any{}, "notes": map[string]any{}},
	})

	narrow := func(t *testing.T, mutate func(props map[string]any)) error {
		t.Helper()
		props := dnBaseProps()
		mutate(props)
		return dwApply(t, ds, props)
	}
	restore := func(t *testing.T) {
		t.Helper()
		if err := dwApply(t, ds, dnBaseProps()); err != nil {
			t.Fatalf("restore the base declaration: %v", err)
		}
	}
	toTarget := map[string]any{"type": "reference", "to": "target"}

	t.Run("absent optional nested reference does not block", func(t *testing.T) {
		if err := narrow(t, func(props map[string]any) {
			props["loose"].(map[string]any)["fields"].(map[string]any)["ref"] = toTarget
		}); err != nil {
			t.Fatalf("a row whose optional reference is absent points nowhere: %v", err)
		}
		restore(t)
	})

	t.Run("conforming keyed references do not block", func(t *testing.T) {
		if err := narrow(t, func(props map[string]any) { props["keyedRefs"] = keyedRefTo("target") }); err != nil {
			t.Fatalf("every stored keyed reference points at the new target: %v", err)
		}
		restore(t)
	})

	t.Run("conforming keys do not block a tightened contract", func(t *testing.T) {
		if err := narrow(t, func(props map[string]any) {
			props["notes"] = map[string]any{"type": "string", "keyed": true, "keyPattern": "camel"}
		}); err != nil {
			t.Fatalf("every stored key is already camelCase: %v", err)
		}
		restore(t)
	})

	// The same three diffs, one row later. Nothing about the declarations
	// changed: the answers move with the DATA, which is the whole contract.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwAuthority + "/holder", ID: "strays",
		Properties: map[string]any{
			"loose":     map[string]any{"ref": map[string]any{"kind": dwAuthority + "/other", "id": "o1"}},
			"keyedRefs": map[string]any{"primary": map[string]any{"kind": dwAuthority + "/other", "id": "o1"}},
			"notes":     map[string]any{"not a camel key": "x"},
		},
	})

	t.Run("a present nested reference elsewhere blocks", func(t *testing.T) {
		wantNarrowingGuard(t, narrow(t, func(props map[string]any) {
			props["loose"].(map[string]any)["fields"].(map[string]any)["ref"] = toTarget
		}), `object "loose" reference "ref" narrows its target`, "1 live records")
	})

	t.Run("a keyed reference elsewhere blocks", func(t *testing.T) {
		wantNarrowingGuard(t, narrow(t, func(props map[string]any) {
			props["keyedRefs"] = keyedRefTo("target")
		}), `reference "keyedRefs" narrows its target`, "1 live records")
	})

	t.Run("a refused key blocks a tightened contract", func(t *testing.T) {
		wantNarrowingGuard(t, narrow(t, func(props map[string]any) {
			props["notes"] = map[string]any{"type": "string", "keyed": true, "keyPattern": "camel"}
		}), `property "notes" tightens its keys to camel`, "1 live records")
	})
}

// keyedRefTo is the keyed reference declaration pinned to one kind.
func keyedRefTo(kind string) map[string]any {
	return map[string]any{"type": "reference", "to": kind, "keyed": true}
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
