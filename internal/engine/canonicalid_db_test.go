package engine_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The canonical-id contract, conformance-shaped:
// every clause below is a promise the rest of the system builds on, and the
// tests are deliberately blunt about which clause they are checking.

// §4.1 — any read by a former id returns the canonical record AND says so.
func TestCanonicalIDReadByFormerID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Nina Ray"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "N. Ray"},
	})
	if _, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	got := mustGet(t, ds, loser.Kind, loser.ID)
	if got.ID != winner.ID {
		t.Fatalf("a former id resolved to %s, want %s", got.ID, winner.ID)
	}
	if got.CanonicalID != winner.ID {
		t.Fatalf("canonicalId = %q, want %q", got.CanonicalID, winner.ID)
	}
	// The canonical id itself never carries the field: it says nothing new.
	if direct := mustGet(t, ds, winner.Kind, winner.ID); direct.CanonicalID != "" {
		t.Fatalf("canonicalId set on a canonical read: %q", direct.CanonicalID)
	}
	// An id that never existed is still not found: the redirect is not a
	// wildcard.
	if _, err := ds.Get(ctx, "person", "zzzzzzzzzzzz"); err == nil {
		t.Fatal("an unknown id must not resolve")
	} else {
		wantErr(t, err, substrate.ErrNotFound, "unknown id")
	}
}

// §4.1 — a reference written at a former id keeps that id as its value and
// still resolves to the canonical record on the reverse read, so a connector
// holding a stale id neither re-attaches the graph to a tombstone nor has its
// pointer rewritten behind its back.
func TestCanonicalIDReferenceResolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installShelf(t, ds)

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "A"}})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "B"}})
	if _, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	book := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{
			"title":  "Piranesi",
			"author": []any{loser.ID},
		},
	})
	// The stored value is what the writer typed, canonicalized only in its
	// spelling: the former id stands. Each entry is the object holding its path
	// under `ref` (decision 0044), so the comparison reads the paths out.
	want := []string{vocabulary.RecordPath(typePerson, loser.ID)}
	if got := storedRefPaths(mustGet(t, ds, book.Kind, book.ID).Properties["author"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("author = %+v, want the former id kept verbatim %+v", got, want)
	}
	// The reverse read is where the trail resolves: the winner sees the book.
	page, err := ds.Incoming(ctx, winner.Kind, winner.ID, substrate.IncomingOptions{Property: "author"})
	if err != nil {
		t.Fatalf("incoming: %v", err)
	}
	if page.Total != 1 || page.Incoming[0].From.ID != book.ID {
		t.Fatalf("the winner's incoming `author` = %+v", page.Incoming)
	}
	// A write addressed at a former id lands on the winner too.
	if got := mustPatch(t, ds, owner, loser.Kind, loser.ID, substrate.PatchInput{
		Properties: map[string]any{"displayName": "Al"},
	}); got.ID != winner.ID {
		t.Fatalf("a patch at a former id wrote %s", got.ID)
	}
	if mustGet(t, ds, winner.Kind, winner.ID).Properties["displayName"] != "Al" {
		t.Fatal("the write did not land on the canonical record")
	}
}

// §4.2 — merge REPOINTS NOTHING. Every pointer at the loser keeps naming the
// loser's id and the reverse read resolves it forward through the trail; the
// loser's own outbound pointers stay on its tombstone.
func TestCanonicalIDMergeRepointsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installShelf(t, ds)

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "A"}})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "B"}})
	org := mustPut(t, ds, owner, substrate.PutInput{Kind: "organization", Properties: map[string]any{"name": "Acme"}})

	// Outgoing from the loser…
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", ID: loser.ID,
		Properties: map[string]any{"memberOf": []any{org.ID}},
	})
	// …and incoming to it.
	book := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{
			"title":  "Piranesi",
			"author": []any{loser.ID},
		},
	})

	if _, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	wantAuthor := []string{vocabulary.RecordPath(typePerson, loser.ID)}
	if got := storedRefPaths(mustGet(t, ds, book.Kind, book.ID).Properties["author"]); !reflect.DeepEqual(got, wantAuthor) {
		t.Fatalf("the merge rewrote an inbound pointer: %+v", got)
	}
	// Nothing migrated onto the winner either: its own properties are what they
	// were before the merge.
	if got := mustGet(t, ds, winner.Kind, winner.ID).Properties["memberOf"]; got != nil {
		t.Fatalf("the merge moved an outbound pointer onto the winner: %+v", got)
	}
	// The reverse read at the winner sees every pointer into the pair: the
	// book's author, plus the merge record's own `winner` and `loser`, which name
	// both sides deliberately and are what make the merge splittable
	// (MODEL §11.5).
	page := readIncoming(t, ds, winner.Kind, winner.ID, 50, "")
	if page.Total != 3 {
		t.Fatalf("incoming at the winner = %+v", page.Incoming)
	}
	var books, merges int
	for _, in := range page.Incoming {
		switch in.From.Kind {
		case book.Kind:
			books++
		case "substrate.reamde.dev/core/recordmerge":
			merges++
		}
	}
	if books != 1 || merges != 2 {
		t.Fatalf("incoming at the winner = %+v", page.Incoming)
	}
	// The loser's own outbound pointer is still on its tombstone: a merge takes
	// the record out of every LIVE read, and puts none of its values anywhere.
	dead, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{IDs: []string{loser.ID}, Deleted: ptr(true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantMember := []any{map[string]any{
		vocabulary.ReferenceValueKey: vocabulary.RecordPath("samples.substrate.reamde.dev/people/organization", org.ID),
	}}
	if len(dead.Records) != 1 || !reflect.DeepEqual(dead.Records[0].Properties["memberOf"], wantMember) {
		t.Fatalf("the loser's own pointer did not stay on its tombstone: %+v", dead.Records)
	}
}

// §4.2 — former-id trails are FLATTENED: A→B then B→C leaves A and B both
// aliasing C directly, so no consumer ever walks a chain.
func TestCanonicalIDTrailsFlatten(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	a := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "A"}})
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "B"}})
	c := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "C"}})

	if _, err := ds.Merge(ctx, owner, b.Kind, b.ID, a.ID); err != nil {
		t.Fatalf("merge a into b: %v", err)
	}
	if _, err := ds.Merge(ctx, owner, c.Kind, c.ID, b.ID); err != nil {
		t.Fatalf("merge b into c: %v", err)
	}

	// The trail is server-set state on the survivor: C says which ids it
	// answers to.
	canonical := mustGet(t, ds, c.Kind, c.ID)
	former := canonical.FormerIDs
	sort.Strings(former)
	want := []string{a.ID, b.ID}
	sort.Strings(want)
	if len(former) != 2 || former[0] != want[0] || former[1] != want[1] {
		t.Fatalf("formerIds = %v, want the flattened %v", canonical.FormerIDs, want)
	}
	for _, id := range want {
		// One hop: each id names C directly, not the record in between.
		got := mustGet(t, ds, c.Kind, id)
		if got.ID != c.ID || got.CanonicalID != c.ID {
			t.Fatalf("get(%s) = %s (canonicalId %q)", id, got.ID, got.CanonicalID)
		}
		if len(got.FormerIDs) != 2 {
			t.Fatalf("a read by a former id answers as the canonical record: %v", got.FormerIDs)
		}
	}
}

// §6.3 — canonical ids are never reused. A write ADDRESSED at a merged-away
// id lands on the winner, and a writer-supplied id colliding with a former id
// is a conflict: nothing is ever re-minted at a dead id.
func TestCanonicalIDsAreNeverReused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "A"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "B"},
	})
	if _, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// A patch addressed at the loser's id writes the winner.
	if got := mustPatch(t, ds, owner, loser.Kind, loser.ID, substrate.PatchInput{
		Properties: map[string]any{"displayName": "Al"},
	}); got.ID != winner.ID {
		t.Fatalf("a write at a former id landed on %s, want %s", got.ID, winner.ID)
	}

	// A writer supplying a former id as its OWN key is a conflict: it would
	// silently address a merge the writer never made.
	src := mustPut(t, ds, people, substrate.PutInput{
		Kind: typeGoogleContact, ID: "g-c1", Properties: map[string]any{"name": aname("Alex")},
	})
	srcLoser := mustPut(t, ds, people, substrate.PutInput{
		Kind: typeGoogleContact, ID: "g-c2", Properties: map[string]any{"name": aname("Alex 2")},
	})
	if _, err := ds.Merge(ctx, owner, src.Kind, src.ID, srcLoser.ID); err != nil {
		t.Fatalf("merge sources: %v", err)
	}
	if _, err := ds.Put(ctx, people, substrate.PutInput{
		Kind: typeGoogleContact, ID: "g-c2", Properties: map[string]any{"name": aname("Alex 2")},
	}); err == nil {
		t.Fatal("a writer-supplied former id must be a conflict")
	} else {
		wantErr(t, err, substrate.ErrConflict, "former id as a writer key")
	}

	// And the id itself is still the tombstone's, forever: nothing new wears
	// it.
	dead, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		IDs: []string{loser.ID}, Deleted: ptr(true),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(dead.Records) != 1 || dead.Records[0].DeletedAt == nil {
		t.Fatalf("the loser id should still name its tombstone: %+v", dead.Records)
	}
}

// A former id denotes its canonical record for DELETE too — a merged-away id
// must not 404 on the one mutation the console's record page offers.
func TestCanonicalIDDeleteByFormerID(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	ctx := context.Background()

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "A"}})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "B"}})
	if _, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	del, err := ds.Delete(ctx, owner, loser.Kind, loser.ID)
	if err != nil {
		t.Fatalf("delete by former id: %v", err)
	}
	if del.ID != winner.ID {
		t.Fatalf("delete resolved to %s, want the canonical %s", del.ID, winner.ID)
	}
	if del.DeletedAt == nil {
		t.Fatal("canonical record not tombstoned")
	}
}

// §4.1 — a reference write addressed at a former id canonicalizes the SOURCE
// end and leaves the value's own end alone. Writing onto the tombstone would
// put the pointer somewhere no read will ever look; rewriting the value would
// be the write repointing a record it was not asked about.
func TestCanonicalIDReferenceWriteAtFormerIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "A"}})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "B"}})
	org := mustPut(t, ds, owner, substrate.PutInput{Kind: "organization", Properties: map[string]any{"name": "Acme"}})
	orgLoser := mustPut(t, ds, owner, substrate.PutInput{Kind: "organization", Properties: map[string]any{"name": "Acme Inc"}})
	if _, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge people: %v", err)
	}
	if _, err := ds.Merge(ctx, owner, org.Kind, org.ID, orgLoser.ID); err != nil {
		t.Fatalf("merge orgs: %v", err)
	}

	// Both ends addressed by their former ids. A PATCH resolves the addressed
	// record forward (a put with a supplied id is refused outright: an id is
	// the writer's own key and a former one is never reused), and the VALUE is
	// left exactly as written.
	if _, err := ds.Patch(ctx, owner, "person", loser.ID, substrate.PatchInput{
		Properties: map[string]any{"memberOf": []any{orgLoser.ID}},
	}); err != nil {
		t.Fatalf("patch at a former id: %v", err)
	}
	want := []any{map[string]any{
		vocabulary.ReferenceValueKey: vocabulary.RecordPath("samples.substrate.reamde.dev/people/organization", orgLoser.ID),
	}}
	got := mustGet(t, ds, winner.Kind, winner.ID).Properties["memberOf"]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a write at a former id landed on %+v, want the winner holding %+v", got, want)
	}
	// The value names a tombstone, and the surviving organization still sees it.
	page, err := ds.Incoming(ctx, org.Kind, org.ID, substrate.IncomingOptions{Property: "memberOf"})
	if err != nil {
		t.Fatalf("incoming: %v", err)
	}
	if page.Total != 1 || page.Incoming[0].From.ID != winner.ID {
		t.Fatalf("the surviving organization's incoming `memberOf` = %+v", page.Incoming)
	}
	// Clearing is writing the property away; there is no second verb.
	if _, err := ds.Patch(ctx, owner, "person", loser.ID, substrate.PatchInput{
		Properties: map[string]any{"memberOf": nil},
	}); err != nil {
		t.Fatalf("clear at a former id: %v", err)
	}
	if got := mustGet(t, ds, winner.Kind, winner.ID).Properties["memberOf"]; got != nil {
		t.Fatalf("clearing at a former id left %+v", got)
	}
}

// Ticket 001 — identity is the (type, id) pair: two TYPES hold
// the SAME id without collision, and every read, write and delete stays
// scoped to its own type.
func TestRekeySameIDAcrossTypes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	const shared = "shared-id-1"
	person := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", ID: shared, Properties: map[string]any{"name": "Pat"},
	})
	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "task", ID: shared, Properties: map[string]any{"name": "file taxes"},
	})
	if person.ID != shared || task.ID != shared {
		t.Fatalf("both puts should keep the shared id: %s / %s", person.ID, task.ID)
	}

	// Each (type, id) resolves to its own record.
	if got := mustGet(t, ds, "person", shared); got.Kind != person.Kind {
		t.Fatalf("get(person, %s) = %s", shared, got.Kind)
	}
	if got := mustGet(t, ds, "task", shared); got.Kind != task.Kind {
		t.Fatalf("get(task, %s) = %s", shared, got.Kind)
	}

	// A write to one never touches the other.
	mustPatch(t, ds, owner, "person", shared, substrate.PatchInput{
		Properties: map[string]any{"displayName": "Patricia"},
	})
	if got := mustGet(t, ds, "task", shared); got.Properties["displayName"] != nil {
		t.Fatalf("a person patch leaked onto the task: %+v", got.Properties)
	}

	// A delete of one leaves the other live.
	if _, err := ds.Delete(ctx, owner, "task", shared); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	if got := mustGet(t, ds, "person", shared); got.DeletedAt != nil {
		t.Fatal("deleting the task tombstoned the person")
	}
	if got := mustGet(t, ds, "task", shared); got.DeletedAt == nil {
		t.Fatal("the task should be tombstoned")
	}
}

// Ticket 001 — former-id trails are PER TYPE: a person's merged-away id
// resolves within `person`, while another type may wear the same id as a
// live record of its own, and a writer-supplied id only conflicts with a
// former id OF ITS OWN TYPE.
func TestRekeyFormerIDTrailIsPerType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", ID: "trail-w", Properties: map[string]any{"name": "A"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", ID: "trail-l", Properties: map[string]any{"name": "B"},
	})
	if _, err := ds.Merge(ctx, owner, "person", winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// Within the type: the former id resolves to the winner and says so.
	got := mustGet(t, ds, "person", loser.ID)
	if got.ID != winner.ID || got.CanonicalID != winner.ID {
		t.Fatalf("get(person, %s) = %s (canonicalId %q)", loser.ID, got.ID, got.CanonicalID)
	}

	// Another type may use the loser's id as its OWN key: the trail is the
	// person type's, not the repository's.
	tk, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "task", ID: loser.ID, Properties: map[string]any{"name": "unrelated"},
	})
	if err != nil {
		t.Fatalf("a task may wear a person's former id: %v", err)
	}
	if tk.ID != loser.ID || tk.CanonicalID != "" {
		t.Fatalf("the task should hold the id plainly: id=%s canonicalId=%q", tk.ID, tk.CanonicalID)
	}
	if got := mustGet(t, ds, "task", loser.ID); got.Kind != "samples.substrate.reamde.dev/tasks/task" || got.ID != loser.ID {
		t.Fatalf("get(task, %s) = %s %s", loser.ID, got.Kind, got.ID)
	}
	// And the person read still resolves through the trail, unshaken.
	if got := mustGet(t, ds, "person", loser.ID); got.ID != winner.ID {
		t.Fatalf("the person trail broke: %s", got.ID)
	}

	// The per-type collision rule stands where it belongs: a PERSON put at
	// the former id is still a conflict.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "person", ID: loser.ID, Properties: map[string]any{"name": "C"},
	}); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a person put at a person former id must conflict, got %v", err)
	}
}
