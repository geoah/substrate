package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The public change event (decision 0061): every row a client reads names the
// records the entry moved with the version each reached, and the stored
// replay effects never leave the engine.

// affectedOf indexes a row's event by record id.
func affectedOf(t *testing.T, c substrate.Change) map[string]substrate.AffectedRecord {
	t.Helper()
	if len(c.Affected) == 0 {
		t.Fatalf("seq %d (%s %s) names no affected record", c.Seq, c.Op, c.RecordID)
	}
	out := map[string]substrate.AffectedRecord{}
	for _, a := range c.Affected {
		if _, dup := out[a.ID]; dup {
			t.Fatalf("seq %d names %s twice: %+v", c.Seq, a.ID, c.Affected)
		}
		out[a.ID] = a
	}
	return out
}

// wantNoFold holds a page of rows to the contract: no `fold` key, and every
// row an event.
func wantNoFold(t *testing.T, changes []substrate.Change) {
	t.Helper()
	for _, c := range changes {
		if _, leaked := c.Payload["fold"]; leaked {
			t.Fatalf("seq %d carries the stored replay effects on the wire: %v", c.Seq, c.Payload)
		}
		affectedOf(t, c)
	}
}

func TestChangesNameEachAffectedRecordWithItsVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	a := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Ada"}})
	a = mustPatch(t, ds, owner, a.Kind, a.ID, substrate.PatchInput{Properties: map[string]any{"name": "Ada L."}})
	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Nina Ray"}})
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "N. Ray"}})
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: winner.Kind, Winner: winner.ID, Loser: loser.ID}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := ds.Delete(ctx, owner, a.Kind, a.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}

	forward := changesSince(t, ds, 0)
	wantNoFold(t, forward)
	backward, err := ds.ChangesBefore(ctx, 0, substrate.ChangeFilter{}, 500)
	if err != nil {
		t.Fatalf("changes before: %v", err)
	}
	wantNoFold(t, backward)

	byOp := map[substrate.Op][]substrate.Change{}
	for _, c := range forward {
		if c.Kind == a.Kind {
			byOp[c.Op] = append(byOp[c.Op], c)
		}
	}
	// The create and the update each name the one record at the version a
	// read of it returned.
	create := affectedOf(t, byOp[substrate.OpPut][0])[a.ID]
	if create.Version != 1 || create.Deleted {
		t.Fatalf("create event = %+v, want version 1, live", create)
	}
	update := affectedOf(t, byOp[substrate.OpPatch][0])[a.ID]
	if update.Version != a.Version || update.Deleted {
		t.Fatalf("update event = %+v, want version %d, live", update, a.Version)
	}
	// The merge is one entry that moves two records: the winner rewritten,
	// the loser tombstoned, each at its own new version.
	merged := affectedOf(t, byOp[substrate.OpMerge][0])
	w, ok := merged[winner.ID]
	if !ok || w.Deleted || w.Version != mustGet(t, ds, winner.Kind, winner.ID).Version {
		t.Fatalf("merge event names the winner as %+v (%v)", w, ok)
	}
	l, ok := merged[loser.ID]
	if !ok || !l.Deleted || l.Version <= loser.Version {
		t.Fatalf("merge event names the loser as %+v (%v), want deleted above version %d", l, ok, loser.Version)
	}
	// The delete tombstones at the next version; the collector's purge names
	// the record deleted with no version, because it has none afterwards.
	del := affectedOf(t, byOp[substrate.OpDelete][0])[a.ID]
	if !del.Deleted || del.Version != a.Version+1 {
		t.Fatalf("delete event = %+v, want deleted at version %d", del, a.Version+1)
	}
	var purge *substrate.AffectedRecord
	for _, c := range byOp[substrate.OpGC] {
		if p, ok := affectedOf(t, c)[a.ID]; ok {
			purge = &p
		}
	}
	if purge == nil || !purge.Deleted || purge.Version != 0 {
		t.Fatalf("purge event = %+v, want deleted with no version", purge)
	}
}

// A patch that wrote only an annotation moves no record's version: an accept
// that lost leaves its conflict note on the request and nothing else. The
// event names the request alone, live and without a version, so a client
// fetches it and reads the note.
func TestAnAnnotationOnlyPatchNamesItsRecordWithoutAVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/tasks/task", Properties: map[string]any{"name": "Draft", "description": "already here"},
	})
	req := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/recordpatchrequest",
		Properties: map[string]any{
			"diff":   map[string]any{"properties": map[string]any{"description": "already here"}},
			"target": vocabulary.RecordPath(task.Kind, task.ID),
		},
	})
	head := maxSeq(t, ds)
	if _, err := ds.Patch(ctx, owner, req.Kind, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(req.Version),
	}); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("accept of a diff that changes nothing = %v, want a conflict", err)
	}
	after := mustGet(t, ds, req.Kind, req.ID)
	if after.Version != req.Version || after.Annotations["substrate/conflict"] == nil {
		t.Fatalf("request after the lost accept = v%d %v, want v%d with a conflict note", after.Version, after.Annotations, req.Version)
	}

	wrote := changesSince(t, ds, head)
	wantNoFold(t, wrote)
	if len(wrote) != 1 {
		t.Fatalf("the lost accept wrote %d entries, want the conflict note alone: %+v", len(wrote), wrote)
	}
	c := wrote[0]
	if c.Op != substrate.OpPatch || c.Kind != req.Kind || c.RecordID != req.ID || c.Payload["conflict"] == nil {
		t.Fatalf("the conflict note's entry = %s %s %s %v", c.Op, c.Kind, c.RecordID, c.Payload)
	}
	if len(c.Affected) != 1 {
		t.Fatalf("the conflict note names %+v, want the request alone", c.Affected)
	}
	if a := c.Affected[0]; a.Kind != req.Kind || a.ID != req.ID || a.Version != 0 || a.Deleted {
		t.Fatalf("the conflict note names %+v, want %s live and without a version", a, req.ID)
	}
}

// The promise the event makes: a client that fetches each affected record as
// the stream names it, and drops the ones the stream says are deleted, holds
// a current copy of the repository, and a copy already at the named version
// need not fetch at all. Nothing else is promised: the client never reads a
// value off the stream.
func TestAClientKeepsACurrentCopyFromTheStreamAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	ada := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Ada"}})
	mustPatch(t, ds, owner, ada.Kind, ada.ID, substrate.PatchInput{Properties: map[string]any{"name": "Ada Lovelace"}})
	bob := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Bob"}})
	dup := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Robert"}})
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: bob.Kind, Winner: bob.ID, Loser: dup.ID}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	gone := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Gone"}})
	if _, err := ds.Delete(ctx, owner, gone.Kind, gone.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Deleted and then put back: `deleted` flips true and then false again,
	// and the copy has to follow both.
	back := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Back"}})
	if _, err := ds.Delete(ctx, owner, back.Kind, back.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	back = mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/people/person", ID: back.ID, Properties: map[string]any{"name": "Back again"}})
	if back.DeletedAt != nil {
		t.Fatalf("the put did not restore the record: %+v", back)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	touched := []string{ada.ID, bob.ID, dup.ID, gone.ID, back.ID}

	// The client: a copy keyed by id, fed from the stream with no value read
	// off it. A fetch of a record the stream says is deleted is not needed,
	// and a copy already at the version named is not refetched.
	copyOf := map[string]*substrate.Record{}
	fetches := 0
	var after int64
	for {
		page := changesSince(t, ds, after)
		if len(page) == 0 {
			break
		}
		for _, c := range page {
			after = c.Seq
			if c.Kind != ada.Kind {
				continue
			}
			for _, a := range c.Affected {
				if a.Deleted {
					delete(copyOf, a.ID)
					continue
				}
				if held, ok := copyOf[a.ID]; ok && a.Version > 0 && held.Version >= a.Version {
					continue
				}
				fetches++
				rec, err := ds.Get(ctx, a.Kind, a.ID)
				if errors.Is(err, substrate.ErrNotFound) {
					delete(copyOf, a.ID)
					continue
				}
				if err != nil {
					t.Fatalf("fetch %s: %v", a.ID, err)
				}
				if rec.DeletedAt != nil || rec.CanonicalID != "" {
					delete(copyOf, a.ID)
					continue
				}
				copyOf[a.ID] = rec
			}
		}
	}

	// The copy is the repository: every touched record is either live and
	// held at its current version and properties, or absent from both.
	for _, id := range touched {
		rec, err := ds.Get(ctx, ada.Kind, id)
		live := err == nil && rec.DeletedAt == nil && rec.CanonicalID == ""
		if errors.Is(err, substrate.ErrNotFound) {
			err = nil
		}
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		held, ok := copyOf[id]
		if live != ok {
			t.Fatalf("%s: live=%v but held=%v", id, live, ok)
		}
		if !live {
			continue
		}
		if held.Version != rec.Version || !sameJSON(t, held.Properties, rec.Properties) {
			t.Fatalf("%s: copy v%d %v, repository v%d %v", id, held.Version, held.Properties, rec.Version, rec.Properties)
		}
	}
	// Three records live at the end (ada, bob, back), and the client fetched
	// six times: the five creates, whose versions the copy did not hold, and
	// back's restoring put, which followed a delete that dropped it from the
	// copy. Ada's patch and bob's merge cost nothing: each create's fetch
	// read the record after every write to it had landed, so the copy was
	// already at or past the version those rows named. The stream named dup,
	// gone and back's tombstone deleted, so none of those cost a fetch.
	if len(copyOf) != 3 || fetches != 6 {
		t.Fatalf("copy holds %d records after %d fetches, want 3 after 6", len(copyOf), fetches)
	}
}

// Taking the effects off the row is what keeps a sensitive value off the
// feed: the event carries no property value at all, so nothing in a page of
// rows can spell one.
func TestChangeRowsCarryNoPropertyValues(t *testing.T) {
	t.Parallel()
	_, ds, _ := newSealingDataset(t)
	sgPutProvider(t, ds, sgPlainKey)

	changes := changesSince(t, ds, 0)
	raw, err := json.Marshal(changes)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(raw), sgPlainKey) {
		t.Fatal("a change row spells the secret's plaintext")
	}
	if strings.Contains(string(raw), `"fold"`) {
		t.Fatal("a change row carries the stored replay effects")
	}
	var sawProvider bool
	for _, c := range changes {
		if c.Kind == sgProviderKind {
			sawProvider = true
			if len(c.Affected) != 1 || c.Affected[0].ID != "prov" || c.Affected[0].Version != 1 {
				t.Fatalf("provider write event = %+v", c.Affected)
			}
		}
	}
	if !sawProvider {
		t.Fatal("the provider write was not read back")
	}
}

func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	ra, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(ra) == string(rb)
}
