package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const gcal = substrate.Actor("connector:calendar")

// §10.2 A meeting, its people, its transcript, its task.
func TestMeetingScenario(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	acc := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "gcal-account:george-at-acme.com",
		Properties: map[string]any{"provider": "gcal", "label": "Work"},
	})
	cal := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendar", ID: "gcal-cal:primary",
		Properties: map[string]any{"name": "Primary", "timezone": "Europe/Athens", "account": enginetest.AccountType + "/" + acc.ID},
	})
	// The connector writes the recurring definition; the substrate never
	// expands RRULEs.
	series := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendareventseries", ID: "gcal-series:abc-at-work",
		Properties: map[string]any{
			"summary": "Standup", "recurrence": "FREQ=WEEKLY;BYDAY=WE",
			"calendar": cal.ID,
		},
	})

	// A reference value is a full "<kind>/<id>" path — or a bare id where the
	// declaration already pins the kind.
	alex := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alex", "emails": []any{"alex@acme.com"}},
	})
	nina := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Nina", "emails": []any{"nina@acme.com"}},
	})
	george := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "George", "emails": []any{"george@acme.com"}},
	})
	event := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendarevent",
		ID:   "gcal-event:abc-at-work_20260805",
		Properties: map[string]any{
			"at": "2026-08-05T13:00:00Z", "endsAt": "2026-08-05T13:30:00Z", "summary": "Standup", "location": "Meet",
			"calendar":  cal.ID,
			"series":    vocabulary.RecordPath("calendar.substrate.reamde.dev/calendareventseries", series.ID),
			"attendees": []any{alex.ID, nina.ID},
			"organizer": george.ID,
		},
	})
	if event.Properties["title"] != "Standup" {
		t.Fatalf("title = %v", event.Properties["title"])
	}
	if attendees, _ := event.Properties["attendees"].([]any); len(attendees) != 2 {
		t.Fatalf("attendees = %+v", event.Properties["attendees"])
	}
	if event.Properties["series"] != vocabulary.RecordPath(series.Kind, series.ID) {
		t.Fatalf("series = %+v", event.Properties["series"])
	}

	// After the meeting, the transcript points at the concrete instance.
	transcript := mustPut(t, ds, substrate.Actor("connector:fireflies"), substrate.PutInput{
		Kind: "transcript", ID: "fireflies-transcript:f81k",
		Properties: map[string]any{
			"title": "Standup notes", "at": "2026-08-05T13:00:00Z", "endsAt": "2026-08-05T13:28:00Z",
			"text":     "Alex asked for the rack layout.",
			"meeting":  event.ID,
			"speakers": []any{alex.ID},
		},
	})
	if speakers, _ := transcript.Properties["speakers"].([]any); len(speakers) != 1 ||
		speakers[0] != vocabulary.RecordPath(alex.Kind, alex.ID) {
		t.Fatal("the speakers reference should name the same person")
	}

	// A learner watching the changelog proposes a task: a creating write may
	// NAME any declared state, and `source` is `to: any`, so the
	// reference carries the type.
	task := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "task",
		Properties: map[string]any{
			"title": "Send rack layout to Alex", "dueAt": "2026-08-08T00:00:00Z",
			"status": "proposed",
			"source": vocabulary.RecordPath("calendar.substrate.reamde.dev/transcript", transcript.ID),
		},
	})
	if task.Properties["status"] != "proposed" {
		t.Fatalf("task states = %v", task.Properties)
	}
	if task.Properties["dueAt"] == nil {
		t.Fatal("temporal(point: dueAt) should fill dueAt")
	}
	// The owner's proposed-list is one filter query.
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{"task"},
		Properties: map[string]substrate.Cond{"status": {Eq: "proposed"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != task.ID {
		t.Fatalf("proposed list = %v", ids(page.Records))
	}
	accepted := mustPatch(t, ds, owner, task.Kind, task.ID, substrate.PatchInput{Properties: map[string]any{"status": "open"}})
	if accepted.Properties["status"] != "open" {
		t.Fatalf("states = %v", accepted.Properties)
	}
}

// §10.3 The UI renders a conversation — one query.
func TestQueryGrammar(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	acc := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "beeper-account:a",
		Properties: map[string]any{"provider": "beeper", "label": "Personal"},
	})
	conv := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversation", ID: "slack-channel:x1",
		Properties: map[string]any{"category": "direct", "account": enginetest.AccountType + "/" + acc.ID},
	})
	other := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversation", ID: "slack-channel:x2",
		Properties: map[string]any{"category": "group", "name": "Family", "account": enginetest.AccountType + "/" + acc.ID},
	})
	alex := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alex"},
	})
	var msgs []*substrate.Record
	for i, at := range []string{"2026-08-01T10:00:00Z", "2026-08-02T10:00:00Z", "2026-08-03T10:00:00Z"} {
		m := mustPut(t, ds, beeper, substrate.PutInput{
			Kind: "conversationmessage",
			ID:   extID("slack.msg", string(rune('a'+i))+"1"),
			Properties: map[string]any{
				"at": at, "text": "message " + string(rune('a'+i)),
				"conversation": conv.ID,
				"author":       alex.ID,
			},
		})
		msgs = append(msgs, m)
	}
	mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversationmessage", ID: "slack-msg:z1",
		Properties: map[string]any{
			"at": "2026-08-04T10:00:00Z", "text": "elsewhere",
			"conversation": other.ID,
			"author":       alex.ID,
		},
	})
	mustPatch(t, ds, owner, msgs[0].Kind, msgs[0].ID, substrate.PatchInput{Labels: map[string]any{"owner/seen": true}})

	// Kind + reference predicate, newest first.
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{
			Kinds:      []string{"conversationmessage"},
			Properties: map[string]substrate.Cond{"conversation": {Eq: conv.ID}},
		},
		OrderBy: []substrate.Order{{Property: "at", Desc: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Records); len(got) != 3 || got[0] != msgs[2].ID || got[2] != msgs[0].ID {
		t.Fatalf("ordered reference query = %v", got)
	}
	if page.Records[0].Properties["author"] != vocabulary.RecordPath(alex.Kind, alex.ID) {
		t.Fatalf("the pointer is not on the listed record: %+v", page.Records[0].Properties)
	}

	// Temporal range, cross-authority via the capability interface.
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Implements: "Temporal",
		Properties: map[string]substrate.Cond{
			"at": {Gte: "2026-08-02T00:00:00Z", Lt: "2026-08-04T00:00:00Z"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Records); len(got) != 2 {
		t.Fatalf("temporal range = %v", got)
	}

	// Labels are first-class filters; annotations are not.
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{"conversationmessage"}, Labels: map[string]substrate.Cond{"owner/seen": {Eq: true}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Records); len(got) != 1 || got[0] != msgs[0].ID {
		t.Fatalf("label filter = %v", got)
	}

	// Pagination.
	first, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{
			Kinds:      []string{"conversationmessage"},
			Properties: map[string]substrate.Cond{"conversation": {Eq: conv.ID}},
		},
		OrderBy: []substrate.Order{{Property: "at", Desc: true}},
		First:   2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != 2 || first.Cursor == "" {
		t.Fatalf("page 1 = %v cursor %q", ids(first.Records), first.Cursor)
	}
	second, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{
			Kinds:      []string{"conversationmessage"},
			Properties: map[string]substrate.Cond{"conversation": {Eq: conv.ID}},
		},
		OrderBy: []substrate.Order{{Property: "at", Desc: true}},
		First:   2, After: first.Cursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 1 || second.Cursor != "" {
		t.Fatalf("page 2 = %v cursor %q", ids(second.Records), second.Cursor)
	}

	// Props filters and states filters.
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{"conversation"},
		Properties: map[string]substrate.Cond{"category": {In: []any{"group", "channel"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Records); len(got) != 1 || got[0] != other.ID {
		t.Fatalf("enum in filter = %v", got)
	}
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{"conversationmessage"}, Properties: map[string]substrate.Cond{"delivery": {Eq: "received"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 4 {
		t.Fatalf("state filter = %v", ids(page.Records))
	}
	// A lookup by the writer's own id is a plain id filter now.
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{IDs: []string{conv.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != conv.ID {
		t.Fatalf("id filter = %v", ids(page.Records))
	}
}

// §10.4 Replying — no send endpoint exists.
func TestReplyLifecycle(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	acc := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "beeper-account:a",
		Properties: map[string]any{"provider": "beeper", "label": "Personal"},
	})
	conv := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversation", ID: "slack-channel:x1",
		Properties: map[string]any{"category": "direct", "account": enginetest.AccountType + "/" + acc.ID},
	})
	me := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "George"},
	})

	// A creating write may NAME any declared state: an outbound
	// message is born a draft.
	msg := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "conversationmessage",
		Properties: map[string]any{
			"at": "2026-08-05T09:00:00Z", "text": "on my way", "delivery": "draft",
			"conversation": conv.ID,
			"author":       me.ID,
		},
	})
	if msg.Properties["delivery"] != "draft" {
		t.Fatalf("owner message states = %v", msg.Properties)
	}
	// Transitions carry no guard: "utterances require a human decision" is a
	// convention between clients until authorization lands.
	mustPatch(t, ds, owner, msg.Kind, msg.ID, substrate.PatchInput{Properties: map[string]any{"delivery": "queued"}})
	// The connector reconciles it outward.
	mustPatch(t, ds, beeper, msg.Kind, msg.ID, substrate.PatchInput{Properties: map[string]any{"delivery": "sending"}})
	sent := mustPatch(t, ds, beeper, msg.Kind, msg.ID, substrate.PatchInput{
		Properties: map[string]any{"delivery": "sent"},
	})
	if sent.Properties["delivery"] != "sent" {
		t.Fatalf("states = %v", sent.Properties)
	}
	if sent.Properties["sentAt"] == nil {
		t.Fatalf("sentAt not stamped: %v", sent.Properties)
	}

	// The provider echoes the message back in an ordinary sync: the connector
	// puts at the id it already holds and nothing changes.
	before := maxSeq(t, ds)
	echo := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversationmessage", ID: msg.ID,
		Properties: map[string]any{
			"at": "2026-08-05T09:00:00Z", "text": "on my way",
			"conversation": conv.ID,
			"author":       me.ID,
		},
	})
	if echo.ID != msg.ID {
		t.Fatalf("echo created a duplicate: %s vs %s", echo.ID, msg.ID)
	}
	if rows := changesSince(t, ds, before); len(rows) != 0 {
		t.Fatalf("provider echo wrote %d changelog rows: %+v", len(rows), rows)
	}
	if echo.Version != sent.Version {
		t.Fatalf("echo bumped version %d → %d", sent.Version, echo.Version)
	}
}
