package engine_test

// The property dialect's widenings, against a real store: a repeated field, an
// object nested to the declared depth, a keyed map (scalar-valued and
// object-valued, both key contracts) and a reference at every position — each
// written, folded and read back.
//
// Read-back is the load-bearing half. Values return through jsonb, so an int
// arrives as float64 and a map's iteration order is gone: a coercion that only
// worked on the way in would still pass a test that never read the row again.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const dwAuthority = "dialect.example.substrate.reamde.dev"

// dwDocs declares a target kind for the references plus a holder carrying one
// property per widening.
func dwDocs(holderProps map[string]any) []map[string]any {
	return []map[string]any{
		vocabulary.AuthorityManifest(dwAuthority, ""),
		vocabulary.KindManifest(dwAuthority,
			map[string]any{"singular": "target", "plural": "targets"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		// A second referent, so a nested reference has somewhere else to be
		// narrowed to (dialect_narrowing_db_test.go).
		vocabulary.KindManifest(dwAuthority,
			map[string]any{"singular": "other", "plural": "others"}, map[string]any{}),
		vocabulary.KindManifest(dwAuthority,
			map[string]any{"singular": "holder", "plural": "holders"},
			map[string]any{"properties": holderProps}),
	}
}

func dwApply(t *testing.T, ds substrate.Dataset, holderProps map[string]any) error {
	t.Helper()
	_, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, dwDocs(holderProps))
	return err
}

// dwProps is the declaration every round-trip below writes against: a repeated
// field (W1), an object nested three levels (W2), keyed maps of ints, of
// strings and of objects under both key contracts (W3), and references at the
// top level, inside a repeated object, inside a keyed map's values and at the
// bottom of the nesting (W4).
func dwProps() map[string]any {
	return map[string]any{
		"grant": map[string]any{"type": "object", "fields": map[string]any{
			"scopes":  map[string]any{"type": "string", "repeated": true},
			"subject": map[string]any{"type": "string"},
		}},
		"spec": map[string]any{"type": "object", "fields": map[string]any{
			"limits": map[string]any{"type": "object", "fields": map[string]any{
				"depth": map[string]any{"type": "int"},
				"ref":   map[string]any{"type": "reference", "to": "target"},
			}},
		}},
		"effects": map[string]any{"type": "int", "keyed": true, "keyPattern": "camel"},
		"notes":   map[string]any{"type": "string", "keyed": true},
		"pinned":  map[string]any{"type": "reference", "to": "target"},
		"installs": map[string]any{
			"type": "object", "keyed": true, "keyPattern": "kindRef",
			"fields": map[string]any{
				"version": map[string]any{"type": "string"},
				"source":  map[string]any{"type": "reference", "to": "target"},
			},
		},
		"tools": map[string]any{"type": "object", "repeated": true, "fields": map[string]any{
			"callable": map[string]any{"type": "reference", "to": "target"},
			"label":    map[string]any{"type": "string"},
		}},
	}
}

func TestDialectWideningsRoundTrip(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := dwApply(t, ds, dwProps()); err != nil {
		t.Fatalf("install the dialect authority: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwAuthority + "/target", ID: "a", Properties: map[string]any{"name": "Ada"},
	})

	row := mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwAuthority + "/holder", ID: "h1",
		Properties: map[string]any{
			"grant": map[string]any{"scopes": []any{"read", "write"}, "subject": "ada"},
			"spec": map[string]any{"limits": map[string]any{
				// A bare id is admitted wherever `to:` pins the kind, nested or not.
				"depth": 3, "ref": "a",
			}},
			"effects": map[string]any{"createdRecords": 2, "skipped": 0},
			"notes":   map[string]any{"any key/at all": "free text"},
			"pinned":  "a",
			"installs": map[string]any{
				"task": map[string]any{"version": "v1", "source": "a"},
				"tasks.example.com/task": map[string]any{
					"version": "v2",
					"source":  map[string]any{"kind": dwAuthority + "/target", "id": "a"},
				},
			},
			"tools": []any{
				map[string]any{"callable": "a", "label": "first"},
				map[string]any{"callable": "a", "label": "second"},
			},
		},
	})

	// Everything below reads the STORED row back, not the write's echo.
	read := mustGet(t, ds, dwAuthority+"/holder", row.ID)
	props := read.Properties

	grant := props["grant"].(map[string]any)
	scopes := grant["scopes"].([]any)
	if len(scopes) != 2 || scopes[0] != "read" || scopes[1] != "write" {
		t.Fatalf("grant.scopes = %v", grant["scopes"])
	}
	if grant["subject"] != "ada" {
		t.Fatalf("grant.subject = %v", grant["subject"])
	}

	limits := props["spec"].(map[string]any)["limits"].(map[string]any)
	// jsonb has one number type: an int comes back as a float64, and every
	// consumer of a nested value has to expect that.
	if d, ok := limits["depth"].(float64); !ok || d != 3 {
		t.Fatalf("spec.limits.depth = %#v, want float64(3)", limits["depth"])
	}
	wantRef := map[string]any{"kind": dwAuthority + "/target", "id": "a"}
	assertRef(t, "spec.limits.ref", limits["ref"], wantRef)
	assertRef(t, "pinned", props["pinned"], wantRef)

	effects := props["effects"].(map[string]any)
	if v, ok := effects["createdRecords"].(float64); !ok || v != 2 {
		t.Fatalf("effects.createdRecords = %#v", effects["createdRecords"])
	}
	if v, ok := effects["skipped"].(float64); !ok || v != 0 {
		t.Fatalf("effects.skipped = %#v — a zero value is a value", effects["skipped"])
	}
	if props["notes"].(map[string]any)["any key/at all"] != "free text" {
		t.Fatalf("notes = %v — an uncontracted key admits anything non-empty", props["notes"])
	}

	installs := props["installs"].(map[string]any)
	if len(installs) != 2 {
		t.Fatalf("installs = %v", installs)
	}
	// Both key spellings a kindRef contract admits, and the reference under each
	// canonical whichever form the writer sent.
	for _, key := range []string{"task", "tasks.example.com/task"} {
		entry, ok := installs[key].(map[string]any)
		if !ok {
			t.Fatalf("installs[%q] = %#v", key, installs[key])
		}
		assertRef(t, "installs."+key+".source", entry["source"], wantRef)
	}

	tools := props["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %v", tools)
	}
	for _, tool := range tools {
		assertRef(t, "tools[].callable", tool.(map[string]any)["callable"], wantRef)
	}
}

// assertRef pins a stored reference to the canonical {kind, id} pair: the whole
// point of reaching nested positions is that a bare id does not survive as one.
func assertRef(t *testing.T, where string, got any, want map[string]any) {
	t.Helper()
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want a {kind, id} pair", where, got)
	}
	if m["kind"] != want["kind"] || m["id"] != want["id"] {
		t.Fatalf("%s = %v, want %v", where, m, want)
	}
}

// The refusals the write path owns: an undeclared field at depth, a key that
// breaks its contract, a nested reference pointing at the wrong kind.
func TestDialectWideningsRefuseBadValues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := dwApply(t, ds, dwProps()); err != nil {
		t.Fatalf("install the dialect authority: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwAuthority + "/target", ID: "a", Properties: map[string]any{"name": "Ada"},
	})

	cases := map[string]struct {
		props map[string]any
		says  string
	}{
		"undeclared field at depth": {
			props: map[string]any{"spec": map[string]any{"limits": map[string]any{"nope": 1}}},
			says:  "not a declared field",
		},
		"key breaks the camel contract": {
			props: map[string]any{"effects": map[string]any{"created_records": 1}},
			says:  "must be camelCase",
		},
		"key breaks the kindRef contract": {
			props: map[string]any{"installs": map[string]any{"tasks.*": map[string]any{"version": "v1"}}},
			says:  "must be a kind reference",
		},
		"empty key": {
			props: map[string]any{"notes": map[string]any{"": "x"}},
			says:  "never empty",
		},
		"a keyed map is not a list": {
			props: map[string]any{"effects": []any{1, 2}},
			says:  "expected a keyed map",
		},
		"nested reference points at the wrong kind": {
			props: map[string]any{"tools": []any{map[string]any{
				"callable": map[string]any{"kind": dwAuthority + "/holder", "id": "h1"},
			}}},
			says: "not " + dwAuthority + "/target",
		},
		"nested reference names an unknown kind": {
			props: map[string]any{"installs": map[string]any{"task": map[string]any{
				"source": map[string]any{"kind": "nosuchkind", "id": "x"},
			}}},
			says: "is unknown",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ds.Put(ctx, owner, substrate.PutInput{
				Kind: dwAuthority + "/holder", Properties: c.props,
			})
			wantErr(t, err, substrate.ErrValidation, name)
			if !strings.Contains(err.Error(), c.says) {
				t.Fatalf("the refusal must say %q, got: %v", c.says, err)
			}
		})
	}
}

// Containers stay out of search at every level: a value inside an object, a
// nested object or a keyed map is never indexed, so a keyed STRING property
// cannot smuggle text into the band its datatype would otherwise claim.
func TestContainersStayOutOfSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := dwApply(t, ds, dwProps()); err != nil {
		t.Fatalf("install the dialect authority: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: dwAuthority + "/holder", ID: "h1",
		Properties: map[string]any{
			"grant": map[string]any{"scopes": []any{"zorionak"}, "subject": "bezperatan"},
			"notes": map[string]any{"greeting": "urtebetetzez"},
			"tools": []any{map[string]any{"label": "gabonak"}},
		},
	})
	for _, word := range []string{"zorionak", "bezperatan", "urtebetetzez", "gabonak"} {
		hits, err := ds.Search(ctx, substrate.SearchInput{Q: word, Mode: substrate.SearchLexical})
		if err != nil {
			t.Fatalf("search %q: %v", word, err)
		}
		if len(hits) != 0 {
			t.Fatalf("%q is inside a container and must not be indexed, got %d hits", word, len(hits))
		}
	}
	// The filter grammar reaches a container no further than it ever reached an
	// object: the comparison is admitted and matches nothing, because there is no
	// declared path INTO a container to compare against. A keyed map added no
	// filter surface, which is the point.
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{dwAuthority + "/holder"},
		Properties: map[string]substrate.Cond{"notes": {Eq: "urtebetetzez"}},
	}})
	if err != nil {
		t.Fatalf("filter on a keyed map: %v", err)
	}
	if len(page.Records) != 0 {
		t.Fatalf("a keyed map is not filterable by its values, got %d rows", len(page.Records))
	}
}

// {localName} and {id}: derived tokens for a kind whose identity IS its
// name. A declaration's id is a kind reference, so its local name is the
// segment after the slash; an ordinary id has none and both render the whole id.
func TestDerivedTitleTokens(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	docs := []map[string]any{
		vocabulary.AuthorityManifest(dtAuthority, ""),
		vocabulary.KindManifest(dtAuthority,
			map[string]any{"singular": "leaf", "plural": "leaves"},
			map[string]any{"displayTemplate": "{localName}"}),
		vocabulary.KindManifest(dtAuthority,
			map[string]any{"singular": "whole", "plural": "wholes"},
			map[string]any{"displayTemplate": "{localName} ({id})"}),
		// A kind that declares a REAL property of the token's name: the
		// declaration wins, and the token is only the fallback.
		vocabulary.KindManifest(dtAuthority,
			map[string]any{"singular": "claimer", "plural": "claimers"},
			map[string]any{
				"displayTemplate": "{localName}",
				"properties":      map[string]any{"localName": map[string]any{"type": "string"}},
			}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install the template authority: %v", err)
	}

	slashed := mustPut(t, ds, owner, substrate.PutInput{
		Kind: dtAuthority + "/leaf", ID: "people.example.com/person",
	})
	if slashed.Title != "person" {
		t.Fatalf("{localName} of a slashed id = %q, want the last segment", slashed.Title)
	}
	slashless := mustPut(t, ds, owner, substrate.PutInput{Kind: dtAuthority + "/leaf", ID: "person"})
	if slashless.Title != "person" {
		t.Fatalf("{localName} of a slashless id = %q, want the whole id", slashless.Title)
	}
	both := mustPut(t, ds, owner, substrate.PutInput{
		Kind: dtAuthority + "/whole", ID: "people.example.com/person",
	})
	if both.Title != "person (people.example.com/person)" {
		t.Fatalf("{localName} ({id}) = %q", both.Title)
	}
	declared := mustPut(t, ds, owner, substrate.PutInput{
		Kind: dtAuthority + "/claimer", ID: "people.example.com/person",
		Properties: map[string]any{"localName": "the declared one"},
	})
	if declared.Title != "the declared one" {
		t.Fatalf("a declared property must win over the derived token, got %q", declared.Title)
	}
	// With the property absent the derived value is still there to fall back to.
	fallback := mustPut(t, ds, owner, substrate.PutInput{
		Kind: dtAuthority + "/claimer", ID: "people.example.com/other",
	})
	if fallback.Title != "other" {
		t.Fatalf("the token must answer when the property is absent, got %q", fallback.Title)
	}
}

const dtAuthority = "dtemplate.example.substrate.reamde.dev"
