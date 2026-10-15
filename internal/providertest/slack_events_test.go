package providertest

// The Slack bundle's live events path: a batch of Events API callbacks, as
// the `slack-events` webhook trigger delivers it, stepped through `slacksync`
// against the loopback fake with every page applied.

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const slackSyncStateType = slackPackage + "/conversationsync"

// slackEvents delivers one batch of callbacks the way the webhook trigger
// does, page by page with every page applied, and returns the body's
// output with the number of pages under `pages`. It fails the test on any write to the account or to a
// conversationsync row: the events path moves no cursor and stamps nothing.
func slackEvents(t *testing.T, ds substrate.Dataset, f *fakeSlack, callbacks ...map[string]any) map[string]any {
	t.Helper()
	row := mustGet(t, ds, slackAccountType, slackAccountID)
	conn := mustGet(t, ds, slackConfigType, "default")
	cfg := map[string]any{
		"accounts": []any{map[string]any{
			"id": slackAccountID, "kind": slackAccountType,
			"version": row.Version, "properties": row.Properties,
		}},
		"inputs": map[string]any{"connector": map[string]any{
			"id": conn.ID, "kind": slackConfigType, "version": conn.Version,
			"properties": map[string]any{"userToken": fakeToken, "apiBase": f.ts.URL},
		}},
	}
	body, err := json.Marshal(map[string]any{"version": 1, "events": callbacks})
	if err != nil {
		t.Fatal(err)
	}
	s := newStepper(t, ds, slackSyncFn, cfg)
	s.setEnvelope(map[string]any{
		"fire":       map[string]any{"id": "hook-test", "at": "2026-10-15T00:00:00Z"},
		"repository": map[string]any{"authority": "test.example.com"},
		"request": map[string]any{
			"method": "POST", "contentType": "application/json",
			"headers": map[string]any{}, "query": map[string]any{},
			"body": map[string]any{"text": string(body)},
		},
	})
	var resume any
	for i := 0; ; i++ {
		if i > 100 {
			t.Fatal("the events batch did not end in 100 invocations")
		}
		effects, out, cur := s.step(resume)
		for _, ef := range effects {
			if ef.Kind == slackAccountType || ef.Kind == slackSyncStateType {
				t.Fatalf("the events path wrote %s %s %s: %v", ef.Action, ef.Kind, ef.ID, ef.Properties)
			}
		}
		s.apply()
		if cur == nil {
			o, _ := out.(map[string]any)
			o["pages"] = i + 1
			return o
		}
		resume = cur
	}
}

// slackCallback is one Events API callback, the payload of a Socket Mode
// `events_api` envelope.
func slackCallback(id, team string, event map[string]any) map[string]any {
	return map[string]any{
		"type": "event_callback", "team_id": team, "api_app_id": "A0TEST",
		"event_id": id, "event_time": 1700000000, "event": event,
	}
}

// slackMessageAt is the live message row with this ts, or nil.
func slackMessageAt(t *testing.T, ds substrate.Dataset, ts string) *substrate.Record {
	t.Helper()
	for _, row := range listLive(t, ds, slackPackage+"/message") {
		if row.Properties["ts"] == ts {
			return row
		}
	}
	return nil
}

// reactionNames is the row's reactions as "name:count" strings.
func reactionNames(row *substrate.Record) []string {
	list, _ := row.Properties["reactions"].([]any)
	out := []string{}
	for _, r := range list {
		m, _ := r.(map[string]any)
		out = append(out, fmt.Sprintf("%v:%v", m["name"], m["count"]))
	}
	return out
}

// TestSlackEventsApplyToTheMirror walks one conversation through the events
// a live socket carries and holds each write to what Slack says now: a new
// message, a repeat, a foreign workspace's callback, a reply, an edit and a
// late copy of the original, a reaction added and removed, a reaction on a
// message the walk never read, and a delete. No cursor and no account
// property moves.
func TestSlackEventsApplyToTheMirror(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	f.history["C1"] = []map[string]any{slackMessage("1700000000.000100", slackOwner)}
	slackSetup(t, ds, f)
	slackRun(t, ds, f, nil)

	acct := mustGet(t, ds, slackAccountType, slackAccountID)
	states := listLive(t, ds, slackSyncStateType)
	if len(states) != 1 {
		t.Fatalf("want one conversationsync row after the walk, got %d", len(states))
	}
	state := states[0]

	msg := func(ts, text string) map[string]any {
		return map[string]any{
			"type": "message", "channel": "C1", "user": slackOwner,
			"text": text, "ts": ts, "event_ts": ts, "channel_type": "channel",
		}
	}
	newTS, replyTS := "1700000100.000100", "1700000200.000100"
	reply := msg(replyTS, "a reply")
	reply["thread_ts"] = newTS
	out := slackEvents(t, ds, f,
		slackCallback("Ev1", slackTeam, msg(newTS, "hello <@"+slackOwner+">")),
		slackCallback("Ev1", slackTeam, msg(newTS, "hello <@"+slackOwner+">")),
		slackCallback("Ev2", "T0OTHER", msg("1700000150.000100", "from another workspace")),
		slackCallback("Ev3", slackTeam, map[string]any{"type": "app_home_opened", "user": slackOwner, "event_ts": "1700000160.000100"}),
		slackCallback("Ev4", slackTeam, reply),
		// A DM the walk has not seen: conversations.info refuses it, and a
		// thin conversation row holds the message.
		slackCallback("Ev5", slackTeam, map[string]any{
			"type": "message", "channel": "D9", "user": slackOwner,
			"text": "a new dm", "ts": "1700000210.000100", "event_ts": "1700000210.000100", "channel_type": "im",
		}),
	)
	if out["repeats"] != float64(1) || out["foreign"] != float64(1) || out["ignored"] != float64(1) || out["applied"] != float64(3) {
		t.Fatalf("output %v, want 1 repeat, 1 foreign, 1 ignored, 3 applied", out)
	}
	parent := slackMessageAt(t, ds, newTS)
	if parent == nil || parent.Properties["text"] != "hello <@"+slackOwner+">" {
		t.Fatalf("the new message is not mirrored: %v", parent)
	}
	if slackMessageAt(t, ds, "1700000150.000100") != nil {
		t.Fatal("a callback from another workspace was written")
	}
	child := slackMessageAt(t, ds, replyTS)
	if child == nil || refIDs(child, "parent")[0] != parent.ID {
		t.Fatalf("the reply does not point at its parent %s: %v", parent.ID, child)
	}
	if got := parent.Properties["latestReplyAt"]; got == nil || refIDs(parent, "latestReply")[0] != child.ID {
		t.Fatalf("the parent's latestReply is %v at %v, want the reply %s", parent.Properties["latestReply"], got, child.ID)
	}
	if users := refIDs(parent, "replyUsers"); len(users) != 1 {
		t.Fatalf("the parent's replyUsers = %v, want the reply's author", users)
	}
	if dm := slackMessageAt(t, ds, "1700000210.000100"); dm == nil {
		t.Fatal("the message in a conversation the walk never read did not land")
	}

	// An edit, then the original delivered again: the edit stands.
	edited := msg(newTS, "hello, edited")
	edited["edited"] = map[string]any{"user": slackOwner, "ts": "1700000300.000000"}
	slackEvents(t, ds, f, slackCallback("Ev6", slackTeam, map[string]any{
		"type": "message", "subtype": "message_changed", "channel": "C1", "hidden": true,
		"ts": "1700000300.000200", "event_ts": "1700000300.000200", "message": edited,
		"previous_message": msg(newTS, "hello"),
	}))
	slackEvents(t, ds, f, slackCallback("Ev1", slackTeam, msg(newTS, "hello <@"+slackOwner+">")))
	if row := slackMessageAt(t, ds, newTS); row.Properties["text"] != "hello, edited" || row.Properties["edited"] == nil {
		t.Fatalf("after the edit and a late copy of the original the row reads %q, edited %v", row.Properties["text"], row.Properties["edited"])
	}

	// A reaction added, then removed: each time the row holds the whole set
	// reactions.get answers, whatever the event said.
	withReaction := msg(newTS, "hello, edited")
	withReaction["reactions"] = []any{map[string]any{"name": "eyes", "count": 1, "users": []any{slackOwner}}}
	f.messages["C1 "+newTS] = withReaction
	reaction := func(id, typ, ts string) map[string]any {
		return slackCallback(id, slackTeam, map[string]any{
			"type": typ, "user": slackOwner, "reaction": "eyes", "event_ts": "1700000400.000100",
			"item": map[string]any{"type": "message", "channel": "C1", "ts": ts},
		})
	}
	slackEvents(t, ds, f, reaction("Ev7", "reaction_added", newTS), reaction("Ev8", "reaction_added", newTS))
	if got := reactionNames(slackMessageAt(t, ds, newTS)); len(got) != 1 || got[0] != "eyes:1" {
		t.Fatalf("after reaction_added the row's reactions are %v, want eyes:1", got)
	}
	if n := f.callsTo("reactions.get C1"); n != 1 {
		t.Fatalf("two reactions on one message in one batch read it %d times, want once", n)
	}
	f.messages["C1 "+newTS] = msg(newTS, "hello, edited")
	slackEvents(t, ds, f, reaction("Ev9", "reaction_removed", newTS))
	if got := reactionNames(slackMessageAt(t, ds, newTS)); len(got) != 0 {
		t.Fatalf("after reaction_removed the row's reactions are %v, want none", got)
	}

	// A reaction on a message the walk never read writes the message whole.
	oldTS := "1690000000.000100"
	old := msg(oldTS, "an old message")
	old["reactions"] = []any{map[string]any{"name": "tada", "count": 2, "users": []any{slackOwner, "U0OTHER"}}}
	f.messages["C1 "+oldTS] = old
	slackEvents(t, ds, f, reaction("Ev10", "reaction_added", oldTS))
	row := slackMessageAt(t, ds, oldTS)
	if row == nil || row.Properties["text"] != "an old message" {
		t.Fatalf("the reacted-to message the walk never read is not mirrored: %v", row)
	}
	if got := reactionNames(row); len(got) != 1 || got[0] != "tada:2" {
		t.Fatalf("its reactions are %v, want tada:2", got)
	}

	// A delete, beside a delete of a message the mirror never held.
	deleted := func(id, ts string) map[string]any {
		return slackCallback(id, slackTeam, map[string]any{
			"type": "message", "subtype": "message_deleted", "channel": "C1", "hidden": true,
			"ts": "1700000500.000100", "event_ts": "1700000500.000100", "deleted_ts": ts,
		})
	}
	out = slackEvents(t, ds, f, deleted("Ev11", newTS), deleted("Ev12", "1600000000.000100"))
	if slackMessageAt(t, ds, newTS) != nil {
		t.Fatal("the deleted message is still mirrored")
	}
	if out["applied"] != float64(1) || out["skipped"] != float64(1) {
		t.Fatalf("the delete batch answered %v, want one applied and one skipped", out)
	}

	if now := mustGet(t, ds, slackAccountType, slackAccountID); now.Version != acct.Version {
		t.Fatalf("the account moved from version %d to %d under the events path", acct.Version, now.Version)
	}
	if now := mustGet(t, ds, slackSyncStateType, state.ID); now.Version != state.Version {
		t.Fatalf("the conversationsync row moved from version %d to %d under the events path", state.Version, now.Version)
	}
}

// TestSlackEventsRateLimitEndsTheCalls: a 429 ends the batch's calls, the
// event that needed one is counted `limited`, and an event after it that
// needs none is still applied.
func TestSlackEventsRateLimitEndsTheCalls(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	f.history["C1"] = []map[string]any{slackMessage("1700000000.000100", slackOwner)}
	slackSetup(t, ds, f)
	slackRun(t, ds, f, nil)

	f.limited = true
	reaction := func(id, ts string) map[string]any {
		return slackCallback(id, slackTeam, map[string]any{
			"type": "reaction_added", "user": slackOwner, "reaction": "eyes", "event_ts": ts,
			"item": map[string]any{"type": "message", "channel": "C1", "ts": "1700000000.000100"},
		})
	}
	out := slackEvents(t, ds, f,
		reaction("Ev1", "1700000100.000100"),
		slackCallback("Ev2", slackTeam, map[string]any{
			"type": "message", "channel": "C1", "user": slackOwner,
			"text": "after the 429", "ts": "1700000200.000100", "event_ts": "1700000200.000100",
		}),
		slackCallback("Ev3", slackTeam, map[string]any{
			"type": "reaction_added", "user": slackOwner, "reaction": "eyes", "event_ts": "1700000300.000100",
			"item": map[string]any{"type": "message", "channel": "C1", "ts": "1700000200.000100"},
		}),
	)
	if out["limited"] != float64(2) || out["applied"] != float64(1) || out["rateLimited"] != true {
		t.Fatalf("output %v, want 2 limited, 1 applied, rateLimited", out)
	}
	if n := f.callsTo("reactions.get"); n != 1 {
		t.Fatalf("reactions.get was called %d times, want once: the 429 ends the batch's calls", n)
	}
	if slackMessageAt(t, ds, "1700000200.000100") == nil {
		t.Fatal("the message after the 429 did not land")
	}
}

// TestSlackEventsConversationsUsersPinsFiles: the events that are not
// messages each set the one fact they name, on the row the walk wrote.
func TestSlackEventsConversationsUsersPinsFiles(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	f.history["C1"] = []map[string]any{slackMessage("1700000000.000100", slackOwner)}
	slackSetup(t, ds, f)
	mustPut(t, ds, substrate.PutInput{Kind: slackAccountType, ID: slackAccountID, Properties: map[string]any{
		"enabledMembers": true,
	}})
	slackRun(t, ds, f, nil)
	convs := listLive(t, ds, slackPackage+"/conversation")
	if len(convs) != 1 {
		t.Fatalf("want one conversation after the walk, got %d", len(convs))
	}
	conv := func() *substrate.Record { return mustGet(t, ds, slackPackage+"/conversation", convs[0].ID) }
	if got := refIDs(conv(), "members"); len(got) != 1 {
		t.Fatalf("the walk wrote members %v, want the owner", got)
	}

	ev := func(id string, event map[string]any) map[string]any {
		event["event_ts"] = "1700000100.000100"
		return slackCallback(id, slackTeam, event)
	}
	pinItem := map[string]any{
		"type": "message", "channel": "C1", "created": 1700000100, "created_by": slackOwner,
		"message": slackMessage("1700000000.000100", slackOwner),
	}
	out := slackEvents(t, ds, f,
		ev("Ev1", map[string]any{"type": "channel_rename", "channel": map[string]any{"id": "C1", "name": "renamed", "created": 1700000000}}),
		ev("Ev2", map[string]any{"type": "channel_archive", "channel": "C1", "user": slackOwner}),
		ev("Ev3", map[string]any{"type": "member_joined_channel", "channel": "C1", "user": "U0NEW", "channel_type": "C"}),
		ev("Ev4", map[string]any{"type": "user_change", "user": map[string]any{
			"id": slackOwner, "name": "owner", "team_id": slackTeam, "updated": 1700000100,
			"profile": map[string]any{"real_name": "The Owner", "status_text": "on a train"},
		}}),
		ev("Ev5", map[string]any{"type": "pin_added", "user": slackOwner, "channel_id": "C1", "item": pinItem}),
		ev("Ev6", map[string]any{"type": "file_shared", "file_id": "F1", "user_id": slackOwner, "channel_id": "C1", "file": map[string]any{"id": "F1"}}),
		ev("Ev7", map[string]any{"type": "channel_deleted", "channel": "C1"}),
	)
	if out["applied"] != float64(6) || out["skipped"] != float64(1) {
		t.Fatalf("output %v, want 6 applied and the channel_deleted skipped", out)
	}
	c := conv()
	if c.Properties["name"] != "renamed" || c.Properties["isArchived"] != true {
		t.Fatalf("after rename and archive the conversation reads name %v, isArchived %v", c.Properties["name"], c.Properties["isArchived"])
	}
	if got := refIDs(c, "members"); len(got) != 2 {
		t.Fatalf("after a join the roster is %v, want two members", got)
	}
	owner := listLive(t, ds, slackPackage+"/user")
	var status any
	for _, u := range owner {
		if u.Properties["userId"] == slackOwner {
			status = u.Properties["profile"].(map[string]any)["statusText"]
		}
	}
	if status != "on a train" {
		t.Fatalf("after user_change the owner's statusText is %v", status)
	}
	if got := refIDs(slackMessageAt(t, ds, "1700000000.000100"), "pinnedTo"); len(got) != 1 || got[0] != convs[0].ID {
		t.Fatalf("after pin_added pinnedTo is %v, want the conversation %s", got, convs[0].ID)
	}
	files := listLive(t, ds, slackPackage+"/file")
	if len(files) != 1 || files[0].Properties["name"] != "file F1" {
		t.Fatalf("after file_shared the files are %v, want F1 read from files.info", files)
	}

	out = slackEvents(t, ds, f,
		ev("Ev8", map[string]any{"type": "channel_unarchive", "channel": "C1", "user": slackOwner}),
		ev("Ev9", map[string]any{"type": "member_left_channel", "channel": "C1", "user": slackOwner, "channel_type": "C"}),
		ev("Ev10", map[string]any{"type": "pin_removed", "user": slackOwner, "channel_id": "C1", "item": pinItem}),
		ev("Ev11", map[string]any{"type": "file_deleted", "file_id": "F1"}),
	)
	if out["applied"] != float64(4) {
		t.Fatalf("output %v, want 4 applied", out)
	}
	c = conv()
	if c.Properties["isArchived"] != false || c.Properties["isMember"] != false {
		t.Fatalf("after unarchive and the owner leaving: isArchived %v, isMember %v", c.Properties["isArchived"], c.Properties["isMember"])
	}
	if got := refIDs(c, "members"); len(got) != 1 {
		t.Fatalf("after the owner left the roster is %v, want one member", got)
	}
	if got := slackMessageAt(t, ds, "1700000000.000100").Properties["pinnedTo"]; got != nil {
		t.Fatalf("after pin_removed pinnedTo is %v, want none", got)
	}
	if mode := listLive(t, ds, slackPackage+"/file")[0].Properties["mode"]; mode != "tombstone" {
		t.Fatalf("after file_deleted the file's mode is %v, want tombstone", mode)
	}
}

// TestSlackEventsPageOnTheirBudget: a batch that needs more calls than one
// invocation may make continues on the next page, and every event lands
// once.
func TestSlackEventsPageOnTheirBudget(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	var history []map[string]any
	var callbacks []map[string]any
	for i := range 3 {
		ts := fmt.Sprintf("1700000000.00010%d", i)
		history = append(history, slackMessage(ts, slackOwner))
		reacted := slackMessage(ts, slackOwner)
		reacted["reactions"] = []any{map[string]any{"name": "eyes", "count": 1, "users": []any{slackOwner}}}
		f.messages["C1 "+ts] = reacted
		callbacks = append(callbacks, slackCallback(fmt.Sprintf("Ev%d", i), slackTeam, map[string]any{
			"type": "reaction_added", "user": slackOwner, "reaction": "eyes", "event_ts": fmt.Sprintf("1700000100.00010%d", i),
			"item": map[string]any{"type": "message", "channel": "C1", "ts": ts},
		}))
	}
	f.history["C1"] = history
	slackSetup(t, ds, f, [2]string{"EVENT_PAGE_CALLS = 20", "EVENT_PAGE_CALLS = 1"})
	slackRun(t, ds, f, nil)

	out := slackEvents(t, ds, f, callbacks...)
	if out["applied"] != 3.0 || out["pages"] != 3 {
		t.Fatalf("output %v, want 3 applied over 3 pages", out)
	}
	if n := f.callsTo("reactions.get"); n != 3 {
		t.Fatalf("reactions.get was called %d times over the pages, want 3", n)
	}
	for _, m := range history {
		if got := reactionNames(slackMessageAt(t, ds, m["ts"].(string))); len(got) != 1 {
			t.Fatalf("message %s holds reactions %v after the paged batch", m["ts"], got)
		}
	}
}
