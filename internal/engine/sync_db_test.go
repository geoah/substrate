package engine_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// personIDFor returns the id of the person with this email, creating one the
// first time. Real connectors keep this mapping in their own store; the
// substrate does not, because nothing matches by value.
func personIDFor(t *testing.T, ds substrate.Dataset, email, name string) string {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{
			Kinds:      []string{"person"},
			Properties: map[string]substrate.Cond{"emails": {Contains: email}},
		},
		First: 1,
	})
	if err != nil {
		t.Fatalf("look up %s: %v", email, err)
	}
	if len(page.Records) == 1 {
		return page.Records[0].ID
	}
	return mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": name, "emails": []any{email}},
	}).ID
}

// fullSync writes a realistic connector sync: accounts, people,
// conversations, messages, calendars and events. Running it twice with the
// same inputs must be silent the second time.
func fullSync(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	beeperAcc := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "beeper-account:acct-1",
		Properties: map[string]any{"provider": "beeper", "label": "Personal", "status": "ok"},
	})
	gcalAcc := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "gcal-account:acct-2",
		Properties: map[string]any{"provider": "gcal", "label": "Work", "status": "ok"},
	})

	// A person's id is server-assigned, so a connector remembers the id it
	// got; nothing matches by value any more. The sync is
	// idempotent because the SOURCE records carry the writer's own keys, and
	// this fixture stands in for the connector's stored mapping.
	humans := map[string]string{}
	for _, p := range []struct{ email, name string }{
		{"alex@acme.com", "Alex Papas"},
		{"nina@acme.com", "Nina Ray"},
	} {
		id := personIDFor(t, ds, p.email, p.name)
		humans[p.email] = id
	}

	for i, chat := range []string{"c1", "c2"} {
		conv := mustPut(t, ds, beeper, substrate.PutInput{
			Kind: "conversation", ID: extID("slack.channel", chat),
			Properties: map[string]any{"kind": "direct"},
			Edges: []substrate.EdgeInput{
				{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: beeperAcc.ID}},
				{Rel: "participants", To: substrate.EdgeRef{ID: humans["alex@acme.com"]}},
			},
		})
		for j := range 3 {
			mustPut(t, ds, beeper, substrate.PutInput{
				Kind:       "conversationmessage",
				ID:         extID("slack.msg", fmt.Sprintf("%s/%d", chat, j)),
				Properties: map[string]any{"at": fmt.Sprintf("2026-08-0%dT1%d:00:00Z", i+1, j), "text": fmt.Sprintf("message %s %d", chat, j)},
				Labels:     map[string]any{"connector:beeper/synced": true},
				Edges: []substrate.EdgeInput{
					{Rel: "conversation", To: substrate.EdgeRef{ID: conv.ID}},
					{Rel: "author", To: substrate.EdgeRef{ID: humans["alex@acme.com"]}},
					{Rel: "mentions", To: substrate.EdgeRef{ID: humans["nina@acme.com"]}},
				},
			})
		}
	}

	cal := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendar", ID: "gcal-cal:primary",
		Properties: map[string]any{"name": "Primary", "timezone": "Europe/Athens"},
		Edges:      []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: gcalAcc.ID}}},
	})
	series := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendareventseries", ID: "gcal-series:standup",
		Properties: map[string]any{"summary": "Standup", "recurrence": "FREQ=WEEKLY;BYDAY=WE"},
		Edges:      []substrate.EdgeInput{{Rel: "calendar", To: substrate.EdgeRef{ID: cal.ID}}},
	})
	for i, day := range []string{"05", "12"} {
		mustPut(t, ds, gcal, substrate.PutInput{
			Kind: "calendarevent", ID: extID("gcal.event", "standup_"+day),
			Properties: map[string]any{"at": "2026-08-" + day + "T13:00:00Z", "endsAt": "2026-08-" + day + "T13:30:00Z", "summary": "Standup", "location": "Meet", "description": "Weekly sync"},
			Edges: []substrate.EdgeInput{
				{Rel: "calendar", To: substrate.EdgeRef{ID: cal.ID}},
				{Rel: "series", To: substrate.EdgeRef{ID: series.ID}},
				{Rel: "attendees", To: substrate.EdgeRef{ID: humans["alex@acme.com"]}},
				{Rel: "attendees", To: substrate.EdgeRef{ID: humans["nina@acme.com"]}},
				{Rel: "organizer", To: substrate.EdgeRef{ID: humans["alex@acme.com"]}},
			},
		})
		_ = i
	}
}

// THE headline test: a full re-sync of identical data is completely silent.
func TestResyncIsSilent(t *testing.T) {
	_, ds := newDataset(t)
	fullSync(t, ds)
	seq := maxSeq(t, ds)
	if seq == 0 {
		t.Fatal("the first sync should have written changelog rows")
	}
	fullSync(t, ds)
	if got := maxSeq(t, ds); got != seq {
		rows := changesSince(t, ds, seq)
		t.Fatalf("re-sync wrote %d changelog rows (seq %d → %d): %+v", len(rows), seq, got, rows)
	}
	// A third replay is silent too, and so are the version numbers.
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"conversationmessage"}}, First: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Records {
		if e.Version != 1 {
			t.Fatalf("message %s reached version %d — a re-sync moved it", e.ID, e.Version)
		}
	}
	fullSync(t, ds)
	if got := maxSeq(t, ds); got != seq {
		t.Fatalf("third sync wrote rows: seq %d → %d", seq, got)
	}
}

// A library row's identity is the PROVIDER'S: the writer names its own record
// , so a re-sync is a primary-key upsert. `asin`/`isbn` are
// denormalized convenience on the edition; the refinements still police them.
func TestWriterKeyDeterminismAndRefinements(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	work := mustPut(t, ds, owner, substrate.PutInput{Kind: "book", Properties: map[string]any{"title": "Piranesi"}})
	toWork := []substrate.EdgeInput{{Rel: "work", To: substrate.EdgeRef{ID: work.ID}}}
	editionID := extID("audible.asin", "B0123ABCDE")
	a := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "bookedition",
		ID:   editionID,
		Properties: map[string]any{
			"title": "Piranesi", "asin": "B0123ABCDE", "isbn": "9781635575637",
			"format": "audiobook", "mediaRef": "https://example.com/a.m4b",
			"duration": "6h47m12s",
		},
		Edges: toWork,
	})
	if a.ID != editionID {
		t.Fatalf("id = %q, want the writer's own %q", a.ID, editionID)
	}
	// A book is a projection target, so its id is server-assigned: 12 chars of
	// lowercase base32, never derived from a provider identifier.
	if len(work.ID) != 12 {
		t.Fatalf("the work should carry a server-assigned id: %+v", work)
	}
	b := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "bookedition", ID: editionID,
		Properties: map[string]any{"mediaRef": "https://example.com/a.m4b"},
		Edges:      toWork,
	})
	if b.ID != a.ID {
		t.Fatalf("a writer's own key is the upsert key: %s vs %s", a.ID, b.ID)
	}
	if b.Properties["title"] != "Piranesi" {
		t.Fatal("types without a displayTemplate keep the writer's title")
	}
	// The custom asin/isbn refinements are enforced.
	for prop, bad := range map[string]string{"asin": "nope", "isbn": "12345"} {
		if _, err := ds.Put(ctx, owner, substrate.PutInput{
			Kind:       "bookedition",
			Properties: map[string]any{prop: bad},
			Edges:      toWork,
		}); err == nil {
			t.Fatalf("expected a %s pattern violation", prop)
		} else {
			wantErr(t, err, substrate.ErrValidation, prop+" pattern")
		}
	}
	// So is the format enum: there is no `audiobook` type, only this value.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "bookedition", Properties: map[string]any{"format": "vinyl"}, Edges: toWork,
	}); err == nil {
		t.Fatal("expected an enum violation")
	}
	// Undeclared properties are rejected.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "task", Properties: map[string]any{"nope": 1},
	}); err == nil {
		t.Fatal("expected an unknown-property error")
	}
	// Refined property types validate.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "calendar", Properties: map[string]any{"name": "x", "timezone": "Mars/Olympus"},
	}); err == nil {
		t.Fatal("expected an IANA timezone error")
	}
}

func TestFinalizersAndOwnerRefGC(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	acc := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "beeper-account:a",
		Properties: map[string]any{"provider": "beeper", "label": "Personal"},
	})
	conv := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversation", ID: "slack-channel:c1",
		Properties: map[string]any{"kind": "direct"},
		Edges:      []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: acc.ID}}},
	})
	msg := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversationmessage", ID: "slack-msg:m1",
		Properties: map[string]any{"at": "2026-08-01T10:00:00Z", "text": "hi"},
		Edges: []substrate.EdgeInput{
			{Rel: "conversation", To: substrate.EdgeRef{ID: conv.ID}},
			{Rel: "author", To: substrate.EdgeRef{ID: personIDFor(t, ds, "alex@acme.com", "Alex")}},
		},
	})
	// The connector holds a finalizer while it manages the conversation.
	mustPatch(t, ds, beeper, conv.Kind, conv.ID, substrate.PatchInput{AddFinalizers: []string{"beeper.connectors.substrate.reamde.dev/teardown"}})

	del, err := ds.Delete(ctx, owner, acc.Kind, acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if del.DeletedAt == nil {
		t.Fatal("delete should stamp deleted_at")
	}
	before := maxSeq(t, ds)
	again, err := ds.Delete(ctx, owner, acc.Kind, acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !again.DeletedAt.Equal(*del.DeletedAt) {
		t.Fatal("delete should be idempotent")
	}
	if rows := changesSince(t, ds, before); len(rows) != 0 {
		t.Fatalf("idempotent delete logged %d rows", len(rows))
	}

	n, err := ds.RunGC(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("first sweep collected %d", n)
	}
	if _, err := ds.Get(ctx, acc.Kind, acc.ID); err == nil {
		t.Fatal("account should be hard-deleted")
	}
	held := mustGet(t, ds, conv.Kind, conv.ID)
	if held.DeletedAt == nil {
		t.Fatal("owner-ref cascade should tombstone the conversation")
	}
	if mustGet(t, ds, msg.Kind, msg.ID).DeletedAt != nil {
		t.Fatal("the finalizer should hold the cascade at the conversation")
	}

	// Release the finalizer: the cascade runs to the leaves.
	mustPatch(t, ds, beeper, conv.Kind, conv.ID, substrate.PatchInput{RemoveFinalizers: []string{"beeper.connectors.substrate.reamde.dev/teardown"}})
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Get(ctx, conv.Kind, conv.ID); err == nil {
		t.Fatal("conversation should be collected")
	}
	if _, err := ds.Get(ctx, msg.Kind, msg.ID); err == nil {
		t.Fatal("message should be collected with its conversation")
	}
	gc, err := ds.Changes(ctx, 0, substrate.ChangeFilter{Ops: []substrate.Op{substrate.OpGC}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(gc) < 5 {
		t.Fatalf("expected gc rows for every step, got %d", len(gc))
	}
}

func TestLinkUnlink(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	c := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "Alex"}})
	team := mustPut(t, ds, owner, substrate.PutInput{Kind: "organization", Properties: map[string]any{"name": "Acme"}})

	if err := ds.Link(ctx, owner, c.Kind, c.ID, "memberOf", substrate.EdgeRef{ID: team.ID}, nil); err != nil {
		t.Fatal(err)
	}
	e := mustGet(t, ds, c.Kind, c.ID)
	if len(e.Edges["memberOf"]) != 1 {
		t.Fatalf("edges = %+v", e.Edges)
	}
	before := maxSeq(t, ds)
	if err := ds.Link(ctx, owner, c.Kind, c.ID, "memberOf", substrate.EdgeRef{ID: team.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if rows := changesSince(t, ds, before); len(rows) != 0 {
		t.Fatalf("re-link logged %d rows", len(rows))
	}
	if err := ds.Unlink(ctx, owner, c.Kind, c.ID, "memberOf", substrate.EdgeRef{ID: team.ID}); err != nil {
		t.Fatal(err)
	}
	if len(mustGet(t, ds, c.Kind, c.ID).Edges["memberOf"]) != 0 {
		t.Fatal("unlink did not remove the edge")
	}
	if err := ds.Link(ctx, owner, c.Kind, c.ID, "nosuch", substrate.EdgeRef{ID: team.ID}, nil); err == nil {
		t.Fatal("expected an undeclared-edge error")
	}
}
