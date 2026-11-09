package localmatrix_test

import (
	"context"
	"testing"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/fakenet"
)

func types(events []map[string]any) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e["type"].(string)
	}
	return out
}

// seqsContiguous checks the stream has no gap.
func seqsContiguous(t *testing.T, events []map[string]any) {
	t.Helper()
	for i, e := range events {
		if got := int64(e["seq"].(float64)); got != int64(i+1) {
			t.Fatalf("event %d has seq %d, want %d", i, got, i+1)
		}
	}
}

// usersBeforeMessages checks that every user an event names was sent to its
// account before it.
func usersBeforeMessages(t *testing.T, events []map[string]any) {
	t.Helper()
	seen := map[string]bool{}
	for _, e := range events {
		account, _ := e["account"].(string)
		switch e["type"] {
		case contract.TypeUserUpsert:
			seen[account+"/"+e["user"].(map[string]any)["id"].(string)] = true
		case contract.TypeMessageUpsert:
			sender := e["message"].(map[string]any)["sender"].(string)
			if !seen[account+"/"+sender] {
				t.Fatalf("message %v names sender %s before any user.upsert of them", e["message"], sender)
			}
		}
	}
}

func TestReceivedMessageReachesTheOutbox(t *testing.T) {
	env := fakenet.Start(t, nil, nil)
	at := time.Date(2026, 11, 9, 9, 59, 58, 0, time.UTC)
	env.Receive(t, "200@lid:200@lid:AAA", fakenet.Other, "see you at 7", at)

	events := env.Events(t, 0)
	seqsContiguous(t, events)
	usersBeforeMessages(t, events)

	var msg, chat map[string]any
	var states []string
	for _, e := range events {
		switch e["type"] {
		case contract.TypeMessageUpsert:
			msg = e
		case contract.TypeChatUpsert:
			if chat == nil {
				chat = e
			}
		case contract.TypeAccountState:
			states = append(states, e["state"].(string))
		}
	}
	if len(states) == 0 || states[len(states)-1] != contract.StateConnected {
		t.Fatalf("account states %v, want the last connected (all: %v)", states, types(events))
	}
	if msg == nil || chat == nil {
		t.Fatalf("no message or chat in %v", types(events))
	}
	if msg["network"] != fakenet.Network || msg["account"] != fakenet.Login || msg["chat"] != fakenet.Chat {
		t.Fatalf("message head %v %v %v", msg["network"], msg["account"], msg["chat"])
	}
	m := msg["message"].(map[string]any)
	if m["id"] != "200@lid:200@lid:AAA" || m["sender"] != fakenet.Other || m["text"] != "see you at 7" ||
		m["content"] != contract.ContentText || m["fromOwner"] != false || m["at"] != "2026-11-09T09:59:58.000Z" {
		t.Fatalf("message body %v", m)
	}
	if msg["backfill"] != false {
		t.Fatalf("a live message is marked backfill")
	}
	c := chat["chat"].(map[string]any)
	if c["id"] != fakenet.Chat || c["category"] != contract.CategoryDirect || c["otherUser"] != fakenet.Other {
		t.Fatalf("chat %v", c)
	}
	members := c["members"].(map[string]any)
	if members["complete"] != true || len(members["items"].([]any)) != 2 {
		t.Fatalf("members %v", members)
	}
	if f, ok := c["features"].(map[string]any); !ok || f["edit"] != true || f["reactions"] != true {
		t.Fatalf("features %v", c["features"])
	}
}

func TestEditReactionAndDeleteFoldIntoTheMessage(t *testing.T) {
	env := fakenet.Start(t, nil, nil)
	ctx := context.Background()
	now := time.Now()
	id := networkid.MessageID("200@lid:200@lid:BBB")
	env.Receive(t, string(id), fakenet.Other, "first", now.Add(-time.Minute))
	start := int64(len(env.Events(t, 0)))

	res := env.Run.Bridge.QueueRemoteEvent(env.Login, &simplevent.Message[string]{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventEdit, PortalKey: env.Portal,
			Sender: bridgev2.EventSender{Sender: fakenet.Other}, Timestamp: now,
		},
		ID: id, TargetMessage: id, Data: "second",
		ConvertEditFunc: func(_ context.Context, _ *bridgev2.Portal, _ bridgev2.MatrixAPI, existing []*database.Message, data string) (*bridgev2.ConvertedEdit, error) {
			return editOf(existing, data), nil
		},
	})
	if !res.Success {
		t.Fatalf("edit: %+v", res)
	}
	res = env.Run.Bridge.QueueRemoteEvent(env.Login, &simplevent.Reaction{
		EventMeta:     simplevent.EventMeta{Type: bridgev2.RemoteEventReaction, PortalKey: env.Portal, Sender: bridgev2.EventSender{Sender: fakenet.Other}, Timestamp: now},
		TargetMessage: id, Emoji: "👍",
	})
	if !res.Success {
		t.Fatalf("reaction: %+v", res)
	}
	res = env.Run.Bridge.QueueRemoteEvent(env.Login, &simplevent.MessageRemove{
		EventMeta:     simplevent.EventMeta{Type: bridgev2.RemoteEventMessageRemove, PortalKey: env.Portal, Sender: bridgev2.EventSender{Sender: fakenet.Other}, Timestamp: now},
		TargetMessage: id,
	})
	if !res.Success {
		t.Fatalf("remove: %+v", res)
	}
	_ = ctx

	var edit, reactions, del map[string]any
	for _, e := range env.Events(t, start) {
		switch e["type"] {
		case contract.TypeMessageUpsert:
			edit = e
		case contract.TypeMessageReactions:
			reactions = e
		case contract.TypeMessageDelete:
			del = e
		}
	}
	if edit == nil || reactions == nil || del == nil {
		t.Fatalf("missing events after %d: %v", start, types(env.Events(t, start)))
	}
	m := edit["message"].(map[string]any)
	if m["text"] != "second" || m["editCount"] != float64(1) || m["editedAt"] == nil {
		t.Fatalf("edit %v", m)
	}
	rs := reactions["reactions"].([]any)
	if len(rs) != 1 || rs[0].(map[string]any)["key"] != "👍" || rs[0].(map[string]any)["user"] != fakenet.Other {
		t.Fatalf("reactions %v", rs)
	}
	if del["message"] != string(id) || del["scope"] != contract.ScopeEveryone || del["by"] != fakenet.Other {
		t.Fatalf("delete %v", del)
	}
}

func editOf(existing []*database.Message, text string) *bridgev2.ConvertedEdit {
	return &bridgev2.ConvertedEdit{ModifiedParts: []*bridgev2.ConvertedEditPart{{
		Part:    existing[0],
		Type:    event.EventMessage,
		Content: &event.MessageEventContent{MsgType: event.MsgText, Body: text},
	}}}
}

func TestAccountEventsNameTheirNetworkAndLogin(t *testing.T) {
	env := fakenet.Start(t, nil, nil)
	var upserts int
	for _, e := range env.Events(t, 0) {
		if e["type"] == contract.TypeAccountUpsert {
			upserts++
			if e["network"] != fakenet.Network || e["account"] != fakenet.Login {
				t.Fatalf("account.upsert without its network and login: %v", e)
			}
		}
	}
	if upserts == 0 {
		t.Fatal("no account.upsert")
	}
}

// A deleted account's row took its chats and users with it on the server,
// so a new link sends them again before a message names them.
func TestAForgottenAccountSendsItsChatsAndUsersAgain(t *testing.T) {
	env := fakenet.Start(t, nil, nil)
	ctx := context.Background()
	env.Receive(t, "200@lid:200@lid:AAA", fakenet.Other, "one", time.Now().Add(-time.Minute))
	if err := env.Run.Matrix.AccountRemoved(ctx, fakenet.Login, true); err != nil {
		t.Fatal(err)
	}
	start := int64(len(env.Events(t, 0)))
	env.Receive(t, "200@lid:200@lid:BBB", fakenet.Other, "two", time.Now())
	got := types(env.Events(t, start))
	want := []string{contract.TypeUserUpsert, contract.TypeChatUpsert, contract.TypeMessageUpsert}
	idx := 0
	for _, typ := range got {
		if idx < len(want) && typ == want[idx] {
			idx++
		}
	}
	if idx != len(want) {
		t.Fatalf("after forgetting, events %v; want users, then the chat, then the message", got)
	}
}
