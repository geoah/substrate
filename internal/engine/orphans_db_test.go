package engine_test

// THE ORPHAN MARK AND ITS SWEEP (#578). A mapping's target is minted out of a
// source; when the last source goes, the row stays, holding nothing anybody
// wrote — 2,727 task husks and 1,838 empty persons on one seeded repository
// after a re-seed. The engine marks those rows, a deployment that asks for it
// collects them, and both halves are held here.

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// orphans lists the marked records of one kind through the door an owner
// would use: the filter arm, not the column.
func orphans(t *testing.T, ds substrate.Dataset, kind string) []*substrate.Record {
	t.Helper()
	marked := true
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{kind}, Orphaned: &marked}, First: 100,
	})
	if err != nil {
		t.Fatalf("list the orphans of %s: %v", kind, err)
	}
	return page.Records
}

// unorphaned is the other half of the arm: `orphaned: false` is a narrowing
// too, and a marked row must not be in it.
func unorphaned(t *testing.T, ds substrate.Dataset, kind string) []*substrate.Record {
	t.Helper()
	marked := false
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{kind}, Orphaned: &marked}, First: 100,
	})
	if err != nil {
		t.Fatalf("list the unmarked of %s: %v", kind, err)
	}
	return page.Records
}

func holdsID(records []*substrate.Record, id string) bool {
	for _, r := range records {
		if r.ID == id {
			return true
		}
	}
	return false
}

// backdateMark backdates a record's mark, which is how a test spends a grace window:
// the stamp is the transaction clock of the recompute that made it, and there
// is no clock seam on the write path.
func backdateMark(t *testing.T, raw *sql.DB, kind, id string, by time.Duration) {
	t.Helper()
	res, err := raw.Exec(`UPDATE records SET orphaned_at = orphaned_at - $3::interval WHERE kind = $1 AND id = $2`,
		kind, id, by.String())
	if err != nil {
		t.Fatalf("age %s: %v", id, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("age %s: %d rows, want 1", id, n)
	}
}

// Deleting a mapped target's LAST SOURCE marks it, and re-linking it clears
// the mark on the spot: the mark is a reading of the present, never a state
// anybody has to clear.
func TestDeletingTheLastSourceMarksTheTargetOrphaned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	contact := syncSource(t, ds, people, typeGoogleContact, "google:c1", map[string]any{
		"name":   aname("Alex Example"),
		"emails": []any{map[string]any{"value": "alex@example.com", "primary": true}},
	})
	pid := personOf(t, ds, contact)
	if got := orphans(t, ds, typePerson); len(got) != 0 {
		t.Fatalf("%d orphans while the source is live", len(got))
	}
	if !holdsID(unorphaned(t, ds, typePerson), pid) {
		t.Fatal("a described person is not in the unmarked set")
	}

	if _, err := ds.Delete(ctx, people, typeGoogleContact, contact.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the contact: %v", err)
	}
	marked := orphans(t, ds, typePerson)
	if len(marked) != 1 || marked[0].ID != pid {
		t.Fatalf("marked %v, want the person the deleted contact described (%s)", marked, pid)
	}
	// The row is still there and still live: the mark is a reading, not a
	// delete.
	person := mustGet(t, ds, typePerson, pid)
	if person.DeletedAt != nil {
		t.Fatal("the mark tombstoned the person")
	}
	if holdsID(unorphaned(t, ds, typePerson), pid) {
		t.Fatal("a marked person is in the `orphaned: false` set")
	}

	// A second source arrives naming the same person — the re-seed landing,
	// the connector re-syncing — and the mark goes with the next recompute.
	syncSource(t, ds, people, typeGoogleContact, "google:c2", map[string]any{
		"name":   aname("Alex Example"),
		"emails": []any{map[string]any{"value": "alex@example.com", "primary": true}},
	}, pid)
	if got := orphans(t, ds, typePerson); len(got) != 0 {
		t.Fatalf("%d orphans after the re-link", len(got))
	}
	if person := mustGet(t, ds, typePerson, pid); person.Properties["name"] != "Alex Example" {
		t.Fatalf("the re-linked person did not recompute: %v", person.Properties)
	}
}

// A HAND HELD ONE PROPERTY, so the record is not a husk. The mark needs every
// property_managers row at the machine tier: an owner's edit (or a bundle's
// direct write, which pins the same way) is the record's own content, not a
// projection of a source that has gone. This is the strict reading of the
// third condition, and the reason the 2,727 task husks carrying a bundle-tier
// `externalId` are not marked (decision 0092).
func TestAPropertyHeldAboveTheMachineTierIsNotAnOrphan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	contact := syncSource(t, ds, people, typeGoogleContact, "google:c1", map[string]any{
		"name":   aname("Alex Example"),
		"emails": []any{map[string]any{"value": "alex@example.com", "primary": true}},
	})
	pid := personOf(t, ds, contact)
	// The owner writes a note of their own onto the person.
	mustPatch(t, ds, owner, typePerson, pid, substrate.PatchInput{
		Properties: map[string]any{"pronouns": "they/them"},
	})
	if _, err := ds.Delete(ctx, people, typeGoogleContact, contact.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the contact: %v", err)
	}
	if got := orphans(t, ds, typePerson); len(got) != 0 {
		t.Fatalf("marked %v: a person the owner edited is not a husk", got)
	}

	// Giving the property back — the documented release, a null patch — makes
	// the record a husk again, and the next mark reads it as one.
	mustPatch(t, ds, owner, typePerson, pid, substrate.PatchInput{
		Properties: map[string]any{"pronouns": nil},
	})
	marked := orphans(t, ds, typePerson)
	if len(marked) != 1 || marked[0].ID != pid {
		t.Fatalf("marked %v, want %s once the owner's hold was released", marked, pid)
	}
}

// THE SWEEP, with the knob on: an old unreferenced orphan is collected, one
// still inside the grace window is spared, and one something live points at is
// spared however old the mark is.
func TestTheSweepCollectsAnOldUnreferencedOrphan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, raw, _ := newDatasetWithDB(t, engine.WithOrphanCollection(24*time.Hour))
	installPeopleSources(t, ds)

	mint := func(id, name, email string) (string, *substrate.Record) {
		t.Helper()
		src := syncSource(t, ds, people, typeGoogleContact, id, map[string]any{
			"name":   aname(name),
			"emails": []any{map[string]any{"value": email, "primary": true}},
		})
		return personOf(t, ds, src), src
	}
	old, oldSrc := mint("google:c1", "Old Husk", "old@example.com")
	young, youngSrc := mint("google:c2", "Young Husk", "young@example.com")
	held, heldSrc := mint("google:c3", "Held Husk", "held@example.com")

	// Something of the owner's points at the third person, which is what
	// spares it: the collection asks the refs index, over every id the record
	// has ever had, whether anything live still has hold of it.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/tasks/task", ID: "t1",
		Properties: map[string]any{"name": "call them", "assignee": typePerson + "/" + held},
	})

	for _, src := range []*substrate.Record{oldSrc, youngSrc, heldSrc} {
		if _, err := ds.Delete(ctx, people, typeGoogleContact, src.ID, substrate.DeleteInput{}); err != nil {
			t.Fatalf("delete %s: %v", src.ID, err)
		}
	}
	if got := len(orphans(t, ds, typePerson)); got != 3 {
		t.Fatalf("%d marked, want all three", got)
	}
	// Two of them have been husks for a month; the third was orphaned a
	// moment ago.
	backdateMark(t, raw, typePerson, old, 30*24*time.Hour)
	backdateMark(t, raw, typePerson, held, 30*24*time.Hour)

	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := ds.Get(ctx, typePerson, old); err == nil {
		t.Fatalf("person %s survived the sweep", old)
	}
	if _, err := ds.Get(ctx, typePerson, young); err != nil {
		t.Fatalf("a mark inside the grace window was collected: %v", err)
	}
	if _, err := ds.Get(ctx, typePerson, held); err != nil {
		t.Fatalf("a referenced orphan was collected: %v", err)
	}
	// The task still points at a record that is there, which is the whole
	// point of asking.
	task := mustGet(t, ds, "samples.substrate.reamde.dev/tasks/task", "t1")
	if refPathValue(task, "assignee") != typePerson+"/"+held {
		t.Fatalf("the task's assignee moved: %v", task.Properties["assignee"])
	}
}

// THE COLLECTION IS OFF UNLESS THE DEPLOYMENT ASKS. The mark ships on; the
// delete does not, because "the last source went" is also what a connector
// outage looks like from here.
func TestTheSweepCollectsNoOrphanByDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, raw, _ := newDatasetWithDB(t)
	installPeopleSources(t, ds)

	contact := syncSource(t, ds, people, typeGoogleContact, "google:c1", map[string]any{
		"name":   aname("Alex Example"),
		"emails": []any{map[string]any{"value": "alex@example.com", "primary": true}},
	})
	pid := personOf(t, ds, contact)
	if _, err := ds.Delete(ctx, people, typeGoogleContact, contact.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the contact: %v", err)
	}
	backdateMark(t, raw, typePerson, pid, 365*24*time.Hour)

	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := ds.Get(ctx, typePerson, pid); err != nil {
		t.Fatalf("the default sweep collected an orphan: %v", err)
	}
	if got := orphans(t, ds, typePerson); len(got) != 1 {
		t.Fatalf("%d marked, want the one the sweep left alone", len(got))
	}
}
