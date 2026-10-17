package providertest

// The Slack live events bundle's `slackevents`: a batch of Events API
// callbacks, as the `slack-live-events` webhook trigger delivers it, stepped
// against the loopback fake with every page applied, over the rows the Slack
// bundle's `slacksync` walked first.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	slackLiveDir       = providersDir + "/slacklive"
	slackLivePackage   = "providers.substrate.reamde.dev/slacklive"
	slackEventsFn      = slackLivePackage + "/slackevents"
	slackSyncStateType = slackPackage + "/conversationsync"
)

// slackLiveSetup is slackSetup with the live events bundle installed beside
// the Slack bundle. rewrites edit the `slackevents` body.
func slackLiveSetup(t *testing.T, ds substrate.Dataset, f *fakeSlack, rewrites ...[2]string) {
	t.Helper()
	slackSetup(t, ds, f)
	installRewired(t, ds, slackLiveDir, rewrites...)
}

// slackEvents delivers one batch of callbacks the way the webhook trigger
// does, page by page with every page applied, and returns the body's output
// with the number of pages under `pages`. The injected config carries the
// bundle's settings and NO accounts: the account is another package's kind,
// so the body reads it. It fails the test on any write to the account or to
// a conversationsync row: the events path moves no cursor and stamps nothing.
func slackEvents(t *testing.T, ds substrate.Dataset, f *fakeSlack, callbacks ...map[string]any) map[string]any {
	t.Helper()
	cfg := map[string]any{
		"bundle":   slackLivePackage,
		"settings": map[string]any{"userToken": fakeToken, "apiBase": f.ts.URL},
	}
	body, err := json.Marshal(map[string]any{"version": 1, "events": callbacks})
	if err != nil {
		t.Fatal(err)
	}
	s := newStepper(t, ds, slackEventsFn, cfg)
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
			if ef.Kind == slackAccountKind || ef.Kind == slackSyncStateType {
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

// TestSlackLiveEventsApplyToTheMirror walks one conversation through the events
// a live socket carries and holds each write to what Slack says now: a new
// message, a repeat, a foreign workspace's callback, a reply, an edit and a
// late copy of the original, a reaction added and removed, a reaction on a
// message the walk never read, and a delete. No cursor and no account
// property moves.
func TestSlackLiveEventsApplyToTheMirror(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	f.history["C1"] = []map[string]any{slackMessage("1700000000.000100", slackOwner)}
	slackLiveSetup(t, ds, f)
	slackRun(t, ds, f, nil)

	acct := mustGet(t, ds, slackAccountKind, slackAccountID)
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

	if now := mustGet(t, ds, slackAccountKind, slackAccountID); now.Version != acct.Version {
		t.Fatalf("the account moved from version %d to %d under the events path", acct.Version, now.Version)
	}
	if now := mustGet(t, ds, slackSyncStateType, state.ID); now.Version != state.Version {
		t.Fatalf("the conversationsync row moved from version %d to %d under the events path", state.Version, now.Version)
	}
}

// TestSlackLiveEventsRateLimitEndsTheCalls: a 429 ends the batch's calls, the
// event that needed one is counted `limited`, and an event after it that
// needs none is still applied.
func TestSlackLiveEventsRateLimitEndsTheCalls(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	f.history["C1"] = []map[string]any{slackMessage("1700000000.000100", slackOwner)}
	slackLiveSetup(t, ds, f)
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

// TestSlackLiveEventsConversationsUsersPinsFiles: the events that are not
// messages each set the one fact they name, on the row the walk wrote.
func TestSlackLiveEventsConversationsUsersPinsFiles(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	f.history["C1"] = []map[string]any{slackMessage("1700000000.000100", slackOwner)}
	slackLiveSetup(t, ds, f)
	mustPut(t, ds, substrate.PutInput{Kind: slackAccountKind, ID: slackAccountID, Properties: map[string]any{
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

// TestSlackLiveEventsPageOnTheirBudget: a batch that needs more calls than one
// invocation may make continues on the next page, and every event lands
// once.
func TestSlackLiveEventsPageOnTheirBudget(t *testing.T) {
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
	slackLiveSetup(t, ds, f, [2]string{"EVENT_PAGE_CALLS = 20", "EVENT_PAGE_CALLS = 1"})
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

// TestSlackLiveShipsNoWebhookTrigger: the bundle ships no trigger at all.
// The webhook door checks a key only where the trigger declares one, so a
// shipped keyless record, even a disabled one, is one Resume click away from
// an endpoint anyone can POST Slack rows to. The operator writes the keyed
// record (the README's).
func TestSlackLiveShipsNoWebhookTrigger(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(slackLiveDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		for _, d := range loadDocs(t, slackLiveDir+"/"+e.Name()) {
			if d["kind"] == vocabulary.CoreKind("trigger") {
				meta, _ := d["metadata"].(map[string]any)
				t.Fatalf("%s ships the trigger %v: the operator writes the keyed trigger record, the bundle ships none", e.Name(), meta["id"])
			}
		}
	}
}

// TestSlackLiveEventsStopOnARefusedToken: a token Slack refuses ends the
// batch's calls like a 429 does, the events that need no call still land,
// and the output names the refusal so a stale second paste is visible.
func TestSlackLiveEventsStopOnARefusedToken(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, logs := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	f.history["C1"] = []map[string]any{slackMessage("1700000000.000100", slackOwner)}
	slackLiveSetup(t, ds, f)
	slackRun(t, ds, f, nil)

	f.refuse["reactions.get C1"] = "invalid_auth"
	reaction := func(id, at string) map[string]any {
		return slackCallback(id, slackTeam, map[string]any{
			"type": "reaction_added", "user": slackOwner, "reaction": "eyes", "event_ts": at,
			"item": map[string]any{"type": "message", "channel": "C1", "ts": "1700000000.000100"},
		})
	}
	out := slackEvents(t, ds, f,
		reaction("Ev1", "1700000100.000100"),
		slackCallback("Ev2", slackTeam, map[string]any{
			"type": "message", "channel": "C1", "user": slackOwner,
			"text": "after the refusal", "ts": "1700000200.000100", "event_ts": "1700000200.000100",
		}),
		slackCallback("Ev3", slackTeam, map[string]any{
			"type": "file_shared", "file_id": "F1", "event_ts": "1700000300.000100",
		}),
	)
	if out["unauthorized"] != float64(2) || out["applied"] != float64(1) || out["authError"] != "invalid_auth" {
		t.Fatalf("output %v, want 2 unauthorized, 1 applied, authError invalid_auth", out)
	}
	if n := f.callsTo("reactions.get") + f.callsTo("files.info"); n != 1 {
		t.Fatalf("Slack was called %d times after the token was refused, want the one refused call", n)
	}
	if slackMessageAt(t, ds, "1700000200.000100") == nil {
		t.Fatal("the message after the refusal did not land")
	}
	if logs.lines("Slack refused the userToken (invalid_auth)") == 0 {
		t.Fatal("no log line names the refused token")
	}
}

// TestSlackLiveRequiresTheSlackBundle: the closure declares no kind, so
// every kind its function names is the Slack bundle's, and admission
// refuses it while that package is absent. With the Slack bundle present it
// admits, and its one function writes the Slack mirrors and never the
// account or the sync state.
func TestSlackLiveRequiresTheSlackBundle(t *testing.T) {
	t.Parallel()
	reg := bundleRegistry(t, slackDir)
	data, err := os.ReadFile(slackLiveDir + "/bundle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	docs, err := vocabulary.ParseStream(data)
	if err != nil {
		t.Fatal(err)
	}
	packages, err := vocabulary.BuildPackages(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("build the slacklive package: %v", err)
	}
	if err := reg.InstallAll(packages); err != nil {
		t.Fatalf("slacklive did not admit beside slack: %v", err)
	}
	fn, err := reg.ResolveFunction(slackEventsFn)
	if err != nil {
		t.Fatal(err)
	}
	mustEmit(t, fn,
		slackPackage+"/team", slackPackage+"/user", slackPackage+"/conversation",
		slackPackage+"/message", slackPackage+"/bot", slackPackage+"/file")
	for _, owned := range []string{slackAccountKind, slackSyncStateType} {
		if slices.Contains(fn.Caps.Emit, owned) {
			t.Fatalf("%s may write %s: the events path moves no cursor and stamps no account", slackEventsFn, owned)
		}
	}
	mustRead(t, fn,
		slackAccountKind, slackPackage+"/team", slackPackage+"/conversation",
		slackPackage+"/message", slackPackage+"/user", slackPackage+"/file")

	alone, err := enginetest.SeededRegistry(seedKindsDir)
	if err != nil {
		t.Fatal(err)
	}
	packages, err = vocabulary.BuildPackages(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatal(err)
	}
	err = alone.InstallAll(packages)
	if err == nil || !strings.Contains(err.Error(), "data.requires names "+slackPackage) {
		t.Fatalf("slacklive without slack: %v, want the requires refusal naming %s", err, slackPackage)
	}
}

// slackLiveCopyDiffers names the top-level definitions `slackevents` shares
// with `slacksync` that differ on purpose. Anything else the two bodies both
// define must be identical, so a row maker changed in one and not the other
// fails here rather than writing two shapes of one row.
var slackLiveCopyDiffers = map[string]string{
	"_get":           "takes a timeout: an events page must end inside the 30 s function timeout",
	"_call":          "takes a timeout, passed on to _get",
	"_message_props": "builds reactions through _reaction_props, which reactions.get shares",
	"main":           "each body's own entry point",
}

// slackLiveCompare is run by python3 over the two bodies: it prints one line
// per top-level name both define, `same <name>` or `differs <name>`, comparing
// the parsed trees, so a comment or a blank line is not a difference.
const slackLiveCompare = `
import ast, sys

def defs(path):
    out = {}
    for node in ast.parse(open(path).read()).body:
        names = []
        if isinstance(node, (ast.FunctionDef, ast.ClassDef)):
            names = [node.name]
        elif isinstance(node, ast.Assign):
            names = [t.id for t in node.targets if isinstance(t, ast.Name)]
        for n in names:
            out[n] = ast.dump(node)
    return out

a, b = defs(sys.argv[1]), defs(sys.argv[2])
for name in sorted(set(a) & set(b)):
    print(("same " if a[name] == b[name] else "differs ") + name)
`

// TestSlackLiveCopyMatchesSlacksync: every top-level function, class and
// constant the two bodies share is identical, except the allowlisted ones.
func TestSlackLiveCopyMatchesSlacksync(t *testing.T) {
	t.Parallel()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}
	source := func(dir, fn string) string {
		for _, d := range loadDocs(t, dir+"/bundle.yaml") {
			meta, _ := d["metadata"].(map[string]any)
			if meta["id"] != fn {
				continue
			}
			data, _ := d["data"].(map[string]any)
			src, _ := data["source"].(string)
			path := filepath.Join(t.TempDir(), "body.py")
			if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}
		t.Fatalf("%s declares no function %s", dir, fn)
		return ""
	}
	out, err := exec.Command(python, "-I", "-c", slackLiveCompare,
		source(slackDir, slackSyncFn), source(slackLiveDir, slackEventsFn)).CombinedOutput()
	if err != nil {
		t.Fatalf("comparing the bodies: %v\n%s", err, out)
	}
	seen := map[string]bool{}
	shared := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		verdict, name, _ := strings.Cut(line, " ")
		shared++
		seen[name] = true
		_, allowed := slackLiveCopyDiffers[name]
		switch {
		case verdict == "differs" && !allowed:
			t.Errorf("%s differs between slacksync and slackevents: change both, or allowlist it with a reason", name)
		case verdict == "same" && allowed:
			t.Errorf("%s is allowlisted as differing but the two bodies agree: drop it from slackLiveCopyDiffers", name)
		}
	}
	for name := range slackLiveCopyDiffers {
		if !seen[name] {
			t.Errorf("%s is allowlisted but is not defined by both bodies", name)
		}
	}
	if shared < 50 {
		t.Fatalf("the bodies share %d top-level names; the copy is gone or the comparison broke:\n%s", shared, out)
	}
}
