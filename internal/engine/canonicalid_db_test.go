package engine_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The canonical-id contract, conformance-shaped:
// every clause below is a promise the rest of the system builds on, and the
// tests are deliberately blunt about which clause they are checking.

// §4.1 — any read by a former id returns the canonical record AND says so.
func TestCanonicalIDReadByFormerID(t *testing.T) {
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

// §4.1 — edge resolution by a former id lands on the canonical record, so a
// connector holding a stale id cannot re-attach the graph to a tombstone.
func TestCanonicalIDEdgeResolution(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "A"}})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "B"}})
	if _, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	book := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{"title": "Piranesi"},
		Edges: []substrate.EdgeInput{{Rel: "author", To: substrate.EdgeRef{ID: loser.ID}}},
	})
	authors := mustGet(t, ds, book.Kind, book.ID).Edges["author"]
	if len(authors) != 1 || authors[0].ID != winner.ID {
		t.Fatalf("an edge written at a former id = %+v", authors)
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

// §4.2 — merge rewrites EVERY stored edge to the winner, in both directions.
func TestCanonicalIDMergeRewritesEveryEdge(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "A"}})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "B"}})
	org := mustPut(t, ds, owner, substrate.PutInput{Kind: "organization", Properties: map[string]any{"name": "Acme"}})

	// Outgoing from the loser…
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", ID: loser.ID,
		Edges: []substrate.EdgeInput{{Rel: "memberOf", To: substrate.EdgeRef{ID: org.ID}}},
	})
	// …and incoming to it.
	book := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{"title": "Piranesi"},
		Edges: []substrate.EdgeInput{{Rel: "author", To: substrate.EdgeRef{ID: loser.ID}}},
	})

	if _, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := mustGet(t, ds, book.Kind, book.ID).Edges["author"]; len(got) != 1 || got[0].ID != winner.ID {
		t.Fatalf("incoming edge not rewritten: %+v", got)
	}
	if got := mustGet(t, ds, winner.Kind, winner.ID).Edges["memberOf"]; len(got) != 1 || got[0].ID != org.ID {
		t.Fatalf("outgoing edge not rewritten: %+v", got)
	}
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Edge: &substrate.EdgeFilter{To: loser.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// One edge still names the loser, deliberately: the merge record's own
	// `loser` edge, which is what makes the merge splittable (MODEL §11.5).
	if len(page.Records) != 1 || page.Records[0].Kind != "core.substrate.reamde.dev/recordmerge" {
		t.Fatalf("edges still pointing at the loser: %+v", page.Records)
	}
	// …and none leave it either: the loser is out of the graph in both
	// directions, not just the one a reader happens to check.
	dead, err := ds.List(ctx, substrate.Query{
		Filter:    substrate.Filter{IDs: []string{loser.ID}, Deleted: ptr(true)},
		WithEdges: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dead.Records) != 1 || len(dead.Records[0].Edges) != 0 {
		t.Fatalf("the loser still has outgoing edges: %+v", dead.Records)
	}
}

// §4.2 — former-id trails are FLATTENED: A→B then B→C leaves A and B both
// aliasing C directly, so no consumer ever walks a chain.
func TestCanonicalIDTrailsFlatten(t *testing.T) {
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

// §4.1 — link and unlink canonicalize BOTH ends. An edge written at a former
// id belongs to the record that id now denotes; writing it onto the tombstone
// puts it somewhere no read will ever look.
func TestCanonicalIDLinkUnlinkBothEnds(t *testing.T) {
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

	// Both ends addressed by their former ids — full identities, ticket 001.
	if err := ds.Link(ctx, owner, "person", loser.ID, "memberOf",
		substrate.EdgeRef{Kind: "organization", ID: orgLoser.ID}, nil); err != nil {
		t.Fatalf("link: %v", err)
	}
	edges := mustGet(t, ds, winner.Kind, winner.ID).Edges["memberOf"]
	if len(edges) != 1 || edges[0].ID != org.ID {
		t.Fatalf("link at former ids landed on %+v", edges)
	}
	if err := ds.Unlink(ctx, owner, "person", loser.ID, "memberOf",
		substrate.EdgeRef{Kind: "organization", ID: orgLoser.ID}); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if edges := mustGet(t, ds, winner.Kind, winner.ID).Edges["memberOf"]; len(edges) != 0 {
		t.Fatalf("unlink at former ids left %+v", edges)
	}
}

// Ticket 001 — identity is the (type, id) pair: two TYPES hold
// the SAME id without collision, and every read, write and delete stays
// scoped to its own type.
func TestRekeySameIDAcrossTypes(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)

	const shared = "shared-id-1"
	person := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", ID: shared, Properties: map[string]any{"name": "Pat"},
	})
	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "task", ID: shared, Properties: map[string]any{"title": "file taxes"},
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
		Kind: "task", ID: loser.ID, Properties: map[string]any{"title": "unrelated"},
	})
	if err != nil {
		t.Fatalf("a task may wear a person's former id: %v", err)
	}
	if tk.ID != loser.ID || tk.CanonicalID != "" {
		t.Fatalf("the task should hold the id plainly: id=%s canonicalId=%q", tk.ID, tk.CanonicalID)
	}
	if got := mustGet(t, ds, "task", loser.ID); got.Kind != "tasks.substrate.reamde.dev/task" || got.ID != loser.ID {
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
