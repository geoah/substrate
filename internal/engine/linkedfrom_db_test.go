package engine_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// `linkedFrom` on the single-record read: the mirrors that converged on one
// subject, which record 0096 moved onto the SOURCE kinds where nothing on the
// subject says they exist (record 0088).

const (
	mappingContactPerson   = "samples.substrate.reamde.dev/people/contactperson"
	mappingSlackUserPerson = "samples.substrate.reamde.dev/people/slackuserperson"
)

// linksOf renders one record's inbound links as comparable tuples.
func linksOf(e *substrate.Record) [][4]string {
	out := make([][4]string, 0, len(e.LinkedFrom))
	for _, l := range e.LinkedFrom {
		out = append(out, [4]string{l.Ref, l.Title, l.Property, l.Mapping})
	}
	return out
}

// Two mappings onto one kind: a person reads back the Google contact AND the
// Slack user that point at it, each naming the slot it points from and the
// mapping that owns that slot, ordered by kind then id.
func TestLinkedFromListsEverySourceThatMapsOntoTheRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	sam := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Sam", "emails": []any{"sam@acme.com"}},
	})
	g := syncSource(t, ds, people, typeGoogleContact, "g-sam", map[string]any{
		"name":   map[string]any{"displayName": "Samuel Jones"},
		"emails": gemails("sam@acme.com"),
	})
	s := syncSource(t, ds, slack, typeSlackUser, "s-sam", map[string]any{
		"realName": "Samuel Jones", "displayName": "sam", "email": "sam@acme.com",
	})
	if got := personOf(t, ds, g); got != sam.ID {
		t.Fatalf("the contact linked %s, want %s", got, sam.ID)
	}
	if got := personOf(t, ds, s); got != sam.ID {
		t.Fatalf("the slack user linked %s, want %s", got, sam.ID)
	}
	// A task pointing at the same person through an ORDINARY reference is not
	// a mapping-owned link and must not appear: `linkedFrom` is the mirror
	// question, and `filter.referencing` is the general one.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/tasks/task", Properties: map[string]any{"name": "Ship it", "assignee": sam.ID},
	})

	want := [][4]string{
		{vocabulary.RecordPath(typeGoogleContact, g.ID), "Samuel Jones", "person", mappingContactPerson},
		{vocabulary.RecordPath(typeSlackUser, s.ID), "sam", "person", mappingSlackUserPerson},
	}
	got := mustGet(t, ds, sam.Kind, sam.ID)
	if links := linksOf(got); !reflect.DeepEqual(links, want) {
		t.Fatalf("linkedFrom = %v, want %v", links, want)
	}
	// The SOURCE side says nothing: no mapping targets a mirror kind, so the
	// key is absent rather than empty.
	if src := mustGet(t, ds, g.Kind, g.ID); src.LinkedFrom != nil {
		t.Fatalf("a source record carried linkedFrom: %v", src.LinkedFrom)
	}
	// A kind no mapping targets carries nothing either.
	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/tasks/task", Properties: map[string]any{"name": "Unmapped"},
	})
	if got := mustGet(t, ds, task.Kind, task.ID); got.LinkedFrom != nil {
		t.Fatalf("a kind no mapping targets carried linkedFrom: %v", got.LinkedFrom)
	}
	// A tombstoned source drops out, exactly as it drops out of the reverse
	// read.
	if _, err := ds.Delete(ctx, slack, s.Kind, s.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the slack user: %v", err)
	}
	if links := linksOf(mustGet(t, ds, sam.Kind, sam.ID)); !reflect.DeepEqual(links, want[:1]) {
		t.Fatalf("after deleting the slack user linkedFrom = %v, want %v", links, want[:1])
	}
}

// A merge repoints nothing, so the loser's mirrors keep naming the loser's id.
// The winner still answers for them, and so does a read addressed by the
// former id — the same former-id trail `filter.referencing` walks.
func TestLinkedFromAnswersForAMergedFormerID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Ada", "emails": []any{"ada@acme.com"}},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "A. Lovelace", "emails": []any{"al@acme.com"}},
	})
	kept := syncSource(t, ds, people, typeGoogleContact, "g-ada", map[string]any{
		"name": map[string]any{"displayName": "Ada Lovelace"}, "emails": gemails("ada@acme.com"),
	})
	moved := syncSource(t, ds, slack, typeSlackUser, "s-ada", map[string]any{
		"realName": "Ada Lovelace", "displayName": "ada", "email": "al@acme.com",
	})
	if got := personOf(t, ds, kept); got != winner.ID {
		t.Fatalf("the contact linked %s, want %s", got, winner.ID)
	}
	if got := personOf(t, ds, moved); got != loser.ID {
		t.Fatalf("the slack user linked %s, want %s", got, loser.ID)
	}
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{
		Kind: winner.Kind, Winner: winner.ID, Loser: loser.ID,
	}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// The slack user's stored pointer still names the loser's id; the winner
	// reads it anyway.
	want := [][4]string{
		{vocabulary.RecordPath(typeGoogleContact, kept.ID), "Ada Lovelace", "person", mappingContactPerson},
		{vocabulary.RecordPath(typeSlackUser, moved.ID), "ada", "person", mappingSlackUserPerson},
	}
	if links := linksOf(mustGet(t, ds, winner.Kind, winner.ID)); !reflect.DeepEqual(links, want) {
		t.Fatalf("the winner's linkedFrom = %v, want %v", links, want)
	}
	// And a read by the FORMER id is the same read: it resolves to the winner
	// and answers with the winner's links.
	byFormer := mustGet(t, ds, loser.Kind, loser.ID)
	if byFormer.CanonicalID != winner.ID {
		t.Fatalf("canonicalId = %q, want %q", byFormer.CanonicalID, winner.ID)
	}
	if links := linksOf(byFormer); !reflect.DeepEqual(links, want) {
		t.Fatalf("the former id's linkedFrom = %v, want %v", links, want)
	}
}
