package engine_test

// A merge repoints no stored reference (decision 0044): the value keeps the path
// its author wrote and every READER resolves it through the former-id trail.
// This file holds the readers that had to learn the trail — the property filter,
// the duplicate check and the cascade sweep — and the rule that a reader never
// consults the declaration to parse a stored value.

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	noteAuthority = "refnotes.example.com"
	notePackage   = noteAuthority + "/refnotes"
	typeNote      = notePackage + "/note"
)

// noteManifest is one kind pointing at people three ways: a cascading owner
// pointer, a plain pointer that gains link data in version 2, and a repeated
// one. `credit` is what the declaration upgrade moves, from `{ref}` alone to
// `{ref, role}`.
func noteManifest(version int, linkProps bool) enginetest.Manifest {
	credit := map[string]any{"type": "reference", "kind": typePerson}
	if linkProps {
		credit["properties"] = map[string]any{"role": map[string]any{"type": "string"}}
	}
	return enginetest.Manifest{
		Name: "refnotes", Authority: noteAuthority,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(notePackage, int64(version)),
			{
				"kind":     "substrate.reamde.dev/core/kind",
				"metadata": map[string]any{"id": typeNote},
				"data": map[string]any{
					"authority":       noteAuthority,
					"package":         "refnotes",
					"names":           map[string]any{"singular": "note", "plural": "notes"},
					"displayTemplate": "{label}",
					"properties": map[string]any{
						"label":    map[string]any{"type": "string"},
						"about":    map[string]any{"type": "reference", "kind": typePerson, "onDelete": "cascade"},
						"credit":   credit,
						"mentions": map[string]any{"type": "reference", "kind": typePerson, "repeated": true},
					},
				},
			},
		},
	}
}

func installNotes(t *testing.T, ds substrate.Dataset, version int, linkProps bool) {
	t.Helper()
	if err := enginetest.Install(context.Background(), ds, substrate.ActorAPI,
		noteManifest(version, linkProps)); err != nil {
		t.Fatalf("install the note kind (version %d): %v", version, err)
	}
}

func newPerson(t *testing.T, ds substrate.Dataset, name string) *substrate.Record {
	t.Helper()
	return mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": name},
	})
}

// notesMatching lists the note ids one property filter answers with, sorted so
// the assertion does not depend on the page order.
func notesMatching(t *testing.T, ds substrate.Dataset, property string, c substrate.Cond) []string {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{
			Kinds:      []string{typeNote},
			Properties: map[string]substrate.Cond{property: c},
		},
		First: 100,
	})
	if err != nil {
		t.Fatalf("list notes by %s: %v", property, err)
	}
	ids := make([]string, 0, len(page.Records))
	for _, r := range page.Records {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	return ids
}

func sorted(ids ...string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A FILTER FOLLOWS THE TRAIL. After a merge one record answers to two paths, and
// a filter naming either must find the rows written under both — the same
// resolution `Get`, `Incoming` and the cascade already do. Without it a merge
// silently splits a query result in two.
func TestReferenceFilterFollowsTheFormerIDTrail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installNotes(t, ds, 1, false)

	winner := newPerson(t, ds, "Ada Lovelace")
	loser := newPerson(t, ds, "A. Lovelace")
	stranger := newPerson(t, ds, "Grace Hopper")

	before := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typeNote, ID: "n-before",
		Properties: map[string]any{"label": "written at the loser", "about": loser.ID},
	})
	after := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typeNote, ID: "n-after",
		Properties: map[string]any{"label": "written at the winner", "about": winner.ID},
	})
	elsewhere := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typeNote, ID: "n-elsewhere",
		Properties: map[string]any{"label": "another person", "about": stranger.ID},
	})

	if _, err := ds.Merge(ctx, owner, typePerson, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// The merge moved no value: `n-before` still spells the loser.
	if got := refPathValue(mustGet(t, ds, typeNote, before.ID), "about"); got != vocabulary.RecordPath(typePerson, loser.ID) {
		t.Fatalf("the merge repointed a stored reference: about = %q", got)
	}

	want := sorted(before.ID, after.ID)
	for _, spelling := range []string{
		vocabulary.RecordPath(typePerson, winner.ID),
		vocabulary.RecordPath(typePerson, loser.ID),
		winner.ID, // the bare id the pin completes
		loser.ID,
	} {
		if got := notesMatching(t, ds, "about", substrate.Cond{Eq: spelling}); !equalIDs(got, want) {
			t.Fatalf("eq %q matched %v, want both notes %v", spelling, got, want)
		}
	}
	// `in` and `contains` reach the same set, and a filter at another record
	// still answers only for it.
	if got := notesMatching(t, ds, "about", substrate.Cond{
		In: []any{vocabulary.RecordPath(typePerson, loser.ID)},
	}); !equalIDs(got, want) {
		t.Fatalf("in [loser] matched %v, want %v", got, want)
	}
	if got := notesMatching(t, ds, "about", substrate.Cond{
		Eq: vocabulary.RecordPath(typePerson, stranger.ID),
	}); !equalIDs(got, []string{elsewhere.ID}) {
		t.Fatalf("eq at an unmerged person matched %v, want only %s", got, elsewhere.ID)
	}
}

// A READER NEVER CONSULTS THE DECLARATION to parse a value. A row written
// before decision 0044 holds a flat path string and nothing rewrites it, so a
// filter that probed only the object every write stores now would go blind to
// exactly those rows. No write path produces a flat value any more, so the row
// is fabricated in the store, the way the pre-0044 release wrote it.
func TestReferenceFilterMatchesBothStoredShapes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, raw, _ := newDatasetWithDB(t)
	installNotes(t, ds, 1, false)

	person := newPerson(t, ds, "Ada Lovelace")
	path := vocabulary.RecordPath(typePerson, person.ID)

	flat := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typeNote, ID: "n-flat",
		Properties: map[string]any{"label": "before the upgrade", "credit": path},
	})
	if _, err := raw.ExecContext(ctx,
		`UPDATE records SET props = jsonb_set(props, '{credit}', to_jsonb($1::text))
		 WHERE kind = $2 AND id = $3`, path, typeNote, flat.ID); err != nil {
		t.Fatalf("fabricate the pre-0044 flat reference: %v", err)
	}
	if got := mustGet(t, ds, typeNote, flat.ID).Properties["credit"]; got != path {
		t.Fatalf("the fabricated row stored %#v, want the flat path", got)
	}

	// The declaration gains link data. Nothing rewrites the row above.
	installNotes(t, ds, 2, true)

	object := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typeNote, ID: "n-object",
		Properties: map[string]any{"label": "after the upgrade", "credit": map[string]any{
			vocabulary.ReferenceValueKey: path, "role": "author",
		}},
	})
	if _, isObject := mustGet(t, ds, typeNote, object.ID).Properties["credit"].(map[string]any); !isObject {
		t.Fatalf("a reference with link data did not store an object: %#v",
			mustGet(t, ds, typeNote, object.ID).Properties["credit"])
	}

	want := sorted(flat.ID, object.ID)
	if got := notesMatching(t, ds, "credit", substrate.Cond{Eq: path}); !equalIDs(got, want) {
		t.Fatalf("eq %q matched %v, want both the flat and the object row %v", path, got, want)
	}
}

// A repeated reference holds each RECORD once, and after a merge one record has
// two spellings: naming it by its own id and by an id it was merged from is the
// same pointer twice, which the refs index would carry under two ordinals.
func TestRepeatedReferenceRefusesOneRecordTwiceAcrossAMerge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installNotes(t, ds, 1, false)

	winner := newPerson(t, ds, "Ada Lovelace")
	loser := newPerson(t, ds, "A. Lovelace")
	other := newPerson(t, ds, "Grace Hopper")
	if _, err := ds.Merge(ctx, owner, typePerson, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: typeNote, ID: "n-dup",
		Properties: map[string]any{"label": "twice", "mentions": []any{
			vocabulary.RecordPath(typePerson, winner.ID),
			vocabulary.RecordPath(typePerson, loser.ID),
		}},
	})
	if err == nil {
		t.Fatal("a repeated reference naming one record by two ids was accepted")
	}
	if !strings.Contains(err.Error(), "twice") {
		t.Fatalf("put = %v, want it to say the record is named twice", err)
	}
	// Two DIFFERENT records at the same site are what the rule allows.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: typeNote, ID: "n-pair",
		Properties: map[string]any{"label": "two people", "mentions": []any{
			vocabulary.RecordPath(typePerson, winner.ID),
			vocabulary.RecordPath(typePerson, other.ID),
		}},
	})
}

// The cascade reads the refs index by TARGET, and a child written before its
// owner won a merge still names the loser. Collecting only the canonical id
// would leave that child behind, live and pointing at nothing.
func TestCascadeFollowsTheFormerIDTrail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installNotes(t, ds, 1, false)

	winner := newPerson(t, ds, "Ada Lovelace")
	loser := newPerson(t, ds, "A. Lovelace")
	stranger := newPerson(t, ds, "Grace Hopper")

	child := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typeNote, ID: "n-child",
		Properties: map[string]any{"label": "owned through the loser", "about": loser.ID},
	})
	survivor := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typeNote, ID: "n-survivor",
		Properties: map[string]any{"label": "owned by somebody else", "about": stranger.ID},
	})

	if _, err := ds.Merge(ctx, owner, typePerson, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := ds.Delete(ctx, owner, typePerson, winner.ID); err != nil {
		t.Fatalf("delete the winner: %v", err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}

	if _, err := ds.Get(ctx, typeNote, child.ID); err == nil {
		t.Fatal("a child pointing at the loser's id survived its owner's collection")
	}
	if mustGet(t, ds, typeNote, survivor.ID).DeletedAt != nil {
		t.Fatal("a child of another person was collected")
	}
}
