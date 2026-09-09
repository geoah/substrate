package engine_test

// The narrowing guards over the widened dialect: a field dropped, retyped or
// re-boxed at depth, a keyed map's key contract tightened, and a nested
// reference's target narrowed. Each arm is a GENERATED jsonb count — one notch
// per declared level, an array notch for a repeated container and a jsonb_each
// notch for a keyed one — so every one of them is exercised against a live row
// (it must refuse, naming the count) and against an empty store (it must
// admit). A query that never runs is a guard that does not exist.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
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
	// A LEVEL-4 object with a scalar leaf: the deepest the dialect admits, and
	// the shape core's own `permissions.reads.budgets.calls` now wears. The
	// property-level arms stop three notches short of it, so a guard that walks
	// one level too few shows up here and nowhere else.
	spec["fields"].(map[string]any)["limits"].(map[string]any)["fields"].(map[string]any)["budgets"] = map[string]any{"type": "object", "fields": map[string]any{
		"calls": map[string]any{"type": "int"},
	}}
	props["plain"] = map[string]any{"type": "string"}
	// The three value constraints, at every position the write path enforces
	// them: a kind's own property in each container, a field at depth, and a
	// reference's link data. dnRow holds a value at each that the tightened
	// constraint in dnCases refuses.
	props["code"] = map[string]any{"type": "string", "pattern": "^[a-z]+$"}
	props["codes"] = map[string]any{"type": "string", "pattern": "^[a-z]+$", "repeated": true}
	props["count"] = map[string]any{"type": "int", "min": 1, "max": 10}
	props["ratio"] = map[string]any{"type": "float"}
	props["price"] = map[string]any{"type": "decimal", "min": 0}
	spec["fields"].(map[string]any)["code"] = map[string]any{"type": "string", "pattern": "^[a-z]+$"}
	// The two sensitive datatypes the write path also holds to a pattern: a
	// digest is stored as its hex string, a secret as a ref into the sealed
	// store, so only the first can be matched.
	props["fingerprint"] = map[string]any{"type": "digest", "pattern": "^ab"}
	props["token"] = map[string]any{"type": "secret", "pattern": "^old-"}
	props["pinned"] = map[string]any{"type": "reference", "kind": "target", "properties": map[string]any{
		"note":   map[string]any{"type": "string", "pattern": "^[a-z]+$"},
		"weight": map[string]any{"type": "int", "min": 1},
	}}
	return props
}

// dnRow carries a value at every position the arms count, so a guard that fails
// to reach one shows up as an admitted narrowing rather than a passing test.
func dnRow(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwPackage + "/target", ID: "a", Properties: map[string]any{"name": "Ada"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwPackage + "/holder", ID: "h1",
		Properties: map[string]any{
			"grant": map[string]any{"scopes": []any{"read"}, "subject": "ada"},
			"spec": map[string]any{
				"mode": "high",
				"code": "abcd",
				"limits": map[string]any{
					"depth": 3, "ref": "a", "grade": "high",
					// The level-4 leaf, so the deepest arm counts a live row too.
					"budgets": map[string]any{"calls": 4},
				},
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
			"code":   "abcd",
			"codes":  []any{"abcd"},
			"count":  5,
			"ratio":  0.25,
			"price":  "5.50",
			"pinned": map[string]any{"ref": "a", "note": "abcd", "weight": 5},

			"fingerprint": strings.Repeat("ab", 32),
			"token":       "old-secret",
		},
	})
}

// dnCases are the narrowing variants: each mutates the base declaration and
// names the fragment its guard must carry.
func dnCases() map[string]struct {
	mutate func(props map[string]any)
	says   string
	refs   bool // the count is over live references, not records
} {
	// spec.fields, reached the same way by every case that edits a level-2 field.
	specFields := func(props map[string]any) map[string]any {
		return props["spec"].(map[string]any)["fields"].(map[string]any)
	}
	limitFields := func(props map[string]any) map[string]any {
		return specFields(props)["limits"].(map[string]any)["fields"].(map[string]any)
	}
	// The level-4 fields: a kind's own property is level 1, so this is the last
	// level the dialect admits and its leaves are scalars.
	budgetFields := func(props map[string]any) map[string]any {
		return limitFields(props)["budgets"].(map[string]any)["fields"].(map[string]any)
	}
	return map[string]struct {
		mutate func(props map[string]any)
		says   string
		refs   bool
	}{
		"level-2 field dropped": {
			mutate: func(props map[string]any) { delete(limitFields(props), "depth") },
			says:   `object "spec.limits" drops field "depth"`,
		},
		"level-4 field dropped": {
			mutate: func(props map[string]any) { delete(budgetFields(props), "calls") },
			says:   `object "spec.limits.budgets" drops field "calls"`,
		},
		"level-4 field retyped": {
			mutate: func(props map[string]any) {
				budgetFields(props)["calls"] = map[string]any{"type": "string"}
			},
			says: `object "spec.limits.budgets" field "calls" changes kind int → string`,
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
				props["tools"].(map[string]any)["fields"].(map[string]any)["callable"] = map[string]any{"type": "reference", "kind": "other"}
			},
			says: `object "tools" reference "callable" narrows its target to ` + dwPackage + `/other`,
		},
		"reference inside a keyed map narrows": {
			mutate: func(props map[string]any) {
				props["installs"].(map[string]any)["fields"].(map[string]any)["source"] = map[string]any{"type": "reference", "kind": "other"}
			},
			says: `object "installs" reference "source" narrows its target to ` + dwPackage + `/other`,
		},
		"reference at level 3 narrows": {
			mutate: func(props map[string]any) {
				limitFields(props)["ref"] = map[string]any{"type": "reference", "kind": "other"}
			},
			says: `object "spec.limits" reference "ref" narrows its target to ` + dwPackage + `/other`,
		},
		// A string retyped to enum counts by VALUE: the held value is outside
		// the declared set here, so both arms refuse; without rows the same
		// diffs admit, which is the string→enum path's whole point.
		"string retyped to enum missing the held value": {
			mutate: func(props map[string]any) {
				props["plain"] = map[string]any{"type": "enum", "values": []any{"other"}}
			},
			says: `property "plain" changes kind string → enum while`,
		},
		"keyed string field retyped to enum missing the held value": {
			mutate: func(props map[string]any) {
				specFields(props)["tags"] = map[string]any{
					"type": "enum", "values": []any{"blue"}, "keyed": true,
				}
			},
			says: `object "spec" field "tags" changes kind keyed string → keyed enum while`,
		},
		// The shape recordpatchpolicy's `selector.ops` took when the door's
		// three verbs were declared after the fact: a repeated string field,
		// closed to the set the engine already matched against.
		"repeated string field retyped to enum missing the held value": {
			mutate: func(props map[string]any) {
				props["grant"].(map[string]any)["fields"].(map[string]any)["scopes"] = map[string]any{
					"type": "enum", "values": []any{"write"}, "repeated": true,
				}
			},
			says: `object "grant" field "scopes" changes kind repeated string → repeated enum while`,
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
		// A value constraint tightened. Any change to a pattern counts, a
		// bound counts in its narrowing direction, and each is counted in the
		// value's own container and at its own depth, the link data included.
		"property pattern changed": {
			mutate: func(props map[string]any) {
				props["code"] = map[string]any{"type": "string", "pattern": "^[a-z]{3}$"}
			},
			says: `property "code" changes its pattern to ^[a-z]{3}$`,
		},
		"repeated property pattern changed": {
			mutate: func(props map[string]any) {
				props["codes"] = map[string]any{"type": "string", "pattern": "^[a-z]{3}$", "repeated": true}
			},
			says: `property "codes" changes its pattern to ^[a-z]{3}$`,
		},
		"property pattern added": {
			mutate: func(props map[string]any) {
				props["plain"] = map[string]any{"type": "string", "pattern": "^x"}
			},
			says: `property "plain" changes its pattern to ^x`,
		},
		"keyed property pattern added": {
			mutate: func(props map[string]any) {
				props["notes"] = map[string]any{"type": "string", "keyed": true, "pattern": "^x"}
			},
			says: `property "notes" changes its pattern to ^x`,
		},
		"digest pattern changed": {
			mutate: func(props map[string]any) {
				props["fingerprint"] = map[string]any{"type": "digest", "pattern": "^cd"}
			},
			says: `property "fingerprint" changes its pattern to ^cd`,
		},
		// A sealed value cannot be matched, so any pattern change on a secret
		// counts every row holding one, and the guard says so.
		"secret pattern changed": {
			mutate: func(props map[string]any) {
				props["token"] = map[string]any{"type": "secret", "pattern": "^new-"}
			},
			says: `property "token" changes its pattern to ^new- while 1 live records hold a sealed value, which cannot be checked against a pattern`,
		},
		"property min raised": {
			mutate: func(props map[string]any) {
				props["count"] = map[string]any{"type": "int", "min": 6, "max": 10}
			},
			says: `property "count" requires values >= 6`,
		},
		"property max lowered": {
			mutate: func(props map[string]any) {
				props["count"] = map[string]any{"type": "int", "min": 1, "max": 4}
			},
			says: `property "count" requires values <= 4`,
		},
		"float min added": {
			mutate: func(props map[string]any) { props["ratio"] = map[string]any{"type": "float", "min": 0.5} },
			says:   `property "ratio" requires values >= 0.5`,
		},
		"float max added": {
			mutate: func(props map[string]any) { props["ratio"] = map[string]any{"type": "float", "max": 0.1} },
			says:   `property "ratio" requires values <= 0.1`,
		},
		"decimal min raised": {
			mutate: func(props map[string]any) { props["price"] = map[string]any{"type": "decimal", "min": 10} },
			says:   `property "price" requires values >= 10`,
		},
		"decimal max added": {
			mutate: func(props map[string]any) {
				props["price"] = map[string]any{"type": "decimal", "min": 0, "max": 5}
			},
			says: `property "price" requires values <= 5`,
		},
		"field pattern changed": {
			mutate: func(props map[string]any) {
				specFields(props)["code"] = map[string]any{"type": "string", "pattern": "^[a-z]{3}$"}
			},
			says: `object "spec" field "code" changes its pattern to ^[a-z]{3}$`,
		},
		"field pattern added inside a repeated object": {
			mutate: func(props map[string]any) {
				props["tools"].(map[string]any)["fields"].(map[string]any)["label"] = map[string]any{"type": "string", "pattern": "^[a-z]{3}$"}
			},
			says: `object "tools" field "label" changes its pattern to ^[a-z]{3}$`,
		},
		"field pattern added inside a keyed map": {
			mutate: func(props map[string]any) {
				props["installs"].(map[string]any)["fields"].(map[string]any)["version"] = map[string]any{"type": "string", "pattern": "^[0-9]+$"}
			},
			says: `object "installs" field "version" changes its pattern to ^[0-9]+$`,
		},
		"level-3 field min added": {
			mutate: func(props map[string]any) { limitFields(props)["depth"] = map[string]any{"type": "int", "min": 5} },
			says:   `object "spec.limits" field "depth" requires values >= 5`,
		},
		"level-4 field max added": {
			mutate: func(props map[string]any) { budgetFields(props)["calls"] = map[string]any{"type": "int", "max": 2} },
			says:   `object "spec.limits.budgets" field "calls" requires values <= 2`,
		},
		"link property pattern changed": {
			mutate: func(props map[string]any) {
				props["pinned"].(map[string]any)["properties"].(map[string]any)["note"] = map[string]any{"type": "string", "pattern": "^[a-z]{3}$"}
			},
			says: `reference "pinned" changes link property "note"'s pattern to ^[a-z]{3}$`,
			refs: true,
		},
		"link property min raised": {
			mutate: func(props map[string]any) {
				props["pinned"].(map[string]any)["properties"].(map[string]any)["weight"] = map[string]any{"type": "int", "min": 6}
			},
			says: `reference "pinned" requires link property "weight" >= 6`,
			refs: true,
		},
		"link property max added": {
			mutate: func(props map[string]any) {
				props["pinned"].(map[string]any)["properties"].(map[string]any)["weight"] = map[string]any{"type": "int", "min": 1, "max": 4}
			},
			says: `reference "pinned" requires link property "weight" <= 4`,
			refs: true,
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
			count := "1 live records"
			if c.refs {
				count = "1 live references"
			}
			wantNarrowingGuard(t, dwApply(t, ds, props), c.says, count)
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
		Kind: dwPackage + "/target", ID: "a", Properties: map[string]any{"name": "Ada"},
	})
	target := vocabulary.RecordPath(dwPackage+"/target", "a")
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwPackage + "/holder", ID: "conforms",
		Properties: map[string]any{
			// The object is present and its optional reference is not.
			"loose": map[string]any{"note": "no pointer here"},
			// The same shape one level over: `grant` is present and its
			// repeated `scopes` is not.
			"grant":     map[string]any{"subject": "ada"},
			"keyedRefs": map[string]any{"primary": target},
			"notes":     map[string]any{"greeting": "hei"},
		},
	})
	// An EMPTY keyed map is still a stored value, and it points nowhere.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwPackage + "/holder", ID: "empties",
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
	toTarget := map[string]any{"type": "reference", "kind": "target"}

	t.Run("absent optional nested reference does not block", func(t *testing.T) {
		if err := narrow(t, func(props map[string]any) {
			props["loose"].(map[string]any)["fields"].(map[string]any)["ref"] = toTarget
		}); err != nil {
			t.Fatalf("a row whose optional reference is absent points nowhere: %v", err)
		}
		restore(t)
	})

	// AN ABSENT LIST HOLDS NO VALUE. The container walk used to box a missing
	// repeated field as `[null]`, which the complement predicate read as a
	// value outside the declared set: a string→enum retype then refused every
	// row that simply left the field out, which for recordpatchpolicy's
	// `selector.ops` is the "empty means all three" spelling the kind
	// documents.
	t.Run("an absent repeated field does not block a string to enum retype", func(t *testing.T) {
		if err := narrow(t, func(props map[string]any) {
			props["grant"].(map[string]any)["fields"].(map[string]any)["scopes"] = map[string]any{
				"type": "enum", "values": []any{"write"}, "repeated": true,
			}
		}); err != nil {
			t.Fatalf("a row whose repeated field is absent holds nothing outside the set: %v", err)
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
		Kind: dwPackage + "/holder", ID: "strays",
		Properties: map[string]any{
			"loose":     map[string]any{"ref": vocabulary.RecordPath(dwPackage+"/other", "o1")},
			"keyedRefs": map[string]any{"primary": vocabulary.RecordPath(dwPackage+"/other", "o1")},
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
	return map[string]any{"type": "reference", "kind": kind, "keyed": true}
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
		Kind: dwPackage + "/holder", ID: "empty", Properties: map[string]any{"plain": "text"},
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
		Kind: dwPackage + "/holder", ID: "carrier",
		Properties: map[string]any{"spec": map[string]any{"limits": map[string]any{"depth": 1}}},
	})
	wantNarrowingGuard(t, dwApply(t, ds, props), `drops field "depth"`, "1 live records")
}

// A constraint change admits when every stored value already satisfies it, and
// a loosening always admits. The other direction of the guard: one that refused
// every pattern edit would block the ordinary evolution of a kind whose rows
// already conform.
func TestConstraintChangesAdmitWhatTheDataSatisfies(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := dwApply(t, ds, dnBaseProps()); err != nil {
		t.Fatalf("install the base authority: %v", err)
	}
	dnRow(t, ds)

	cases := map[string]func(props map[string]any){
		"pattern dropped": func(props map[string]any) {
			props["code"] = map[string]any{"type": "string"}
		},
		"pattern changed to one the value matches": func(props map[string]any) {
			props["code"] = map[string]any{"type": "string", "pattern": "^[a-d]+$"}
		},
		// An escape RE2 reads and Postgres' ARE does not: the count runs the
		// write path's regexp, so the value is judged as its next write would
		// judge it, not as SQL would.
		"pattern with an RE2-only escape the value matches": func(props map[string]any) {
			props["code"] = map[string]any{"type": "string", "pattern": `^[a-z]+\z`}
		},
		"bounds loosened": func(props map[string]any) {
			props["count"] = map[string]any{"type": "int", "min": 0, "max": 100}
		},
		"min raised to the held value": func(props map[string]any) {
			props["count"] = map[string]any{"type": "int", "min": 5, "max": 10}
		},
		"float max added at the held value": func(props map[string]any) {
			props["ratio"] = map[string]any{"type": "float", "max": 0.25}
		},
		"decimal max added at the held value": func(props map[string]any) {
			props["price"] = map[string]any{"type": "decimal", "min": 0, "max": 5.5}
		},
		"digest pattern changed to one the value matches": func(props map[string]any) {
			props["fingerprint"] = map[string]any{"type": "digest", "pattern": "^(ab)+$"}
		},
		"link property pattern changed to one the value matches": func(props map[string]any) {
			props["pinned"].(map[string]any)["properties"].(map[string]any)["note"] = map[string]any{"type": "string", "pattern": "^[a-d]+$"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			props := dnBaseProps()
			mutate(props)
			if err := dwApply(t, ds, props); err != nil {
				t.Fatalf("every stored value satisfies the new constraint, so it must land: %v", err)
			}
			if err := dwApply(t, ds, dnBaseProps()); err != nil {
				t.Fatalf("restore the base declaration: %v", err)
			}
		})
	}

	// The same RE2-only escape, refusing: the row is judged by the regexp the
	// write path compiles, in both directions.
	props := dnBaseProps()
	props["code"] = map[string]any{"type": "string", "pattern": `^[a-c]+\z`}
	wantNarrowingGuard(t, dwApply(t, ds, props), `property "code" changes its pattern to ^[a-c]+\z`, "1 live records")
}

// A constraint can reach a kind through a `propertytype` it refines from. The
// kind's own declaration does not move; the type it names does, and the guard
// counts the kind's rows all the same, because the candidate registry resolves
// the refinement exactly as the write path will.
func TestConstraintTightenedThroughAPropertyTypeIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	const pkg = "constraints.example.substrate.reamde.dev/codes"
	propertyType := func(pattern string) map[string]any {
		return map[string]any{
			"kind":     vocabulary.CoreKind(vocabulary.DocPropertyType),
			"metadata": map[string]any{"id": pkg + "/code"},
			"data": map[string]any{
				"authority": "constraints.example.substrate.reamde.dev",
				"package":   "codes",
				"base":      "string",
				"pattern":   pattern,
			},
		}
	}
	item := vocabulary.KindManifest(pkg,
		map[string]any{"singular": "item"},
		map[string]any{"properties": map[string]any{"code": map[string]any{"type": "code"}}})
	sa := applier(t, ds)
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(pkg, 0), propertyType("^[a-z]+$"), item,
	}); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: pkg + "/item", ID: "one", Properties: map[string]any{"code": "abcd"},
	})

	// The property type alone tightens; the kind document is not resent.
	_, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{propertyType("^[a-z]{3}$")})
	wantNarrowingGuard(t, err, `type `+pkg+`/item: property "code" changes its pattern to ^[a-z]{3}$`, "1 live records")

	// A change the stored value satisfies lands through the same channel.
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{propertyType("^[a-z]{1,8}$")}); err != nil {
		t.Fatalf("a refinement every row satisfies must land: %v", err)
	}
}
