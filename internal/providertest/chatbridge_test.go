package providertest

// The chat bridge suite. The bridge program is not here: every case hands
// `chatevents` a batch as the `chatbridge-events` webhook door builds the
// request (a `batch` text part and one file part per medium, already a blob)
// and asserts the rows it leaves. The batches under testdata/chatbridge are
// the contract's worked examples; the cases below write their own where a
// rule needs one event in isolation.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	chatBridgePkg = "providers.substrate.reamde.dev/chatbridge"
	cbInstance    = chatBridgePkg + "/instance"
	cbAccount     = chatBridgePkg + "/account"
	cbUser        = chatBridgePkg + "/user"
	cbChat        = chatBridgePkg + "/chat"
	cbMessage     = chatBridgePkg + "/message"
	cbAttachment  = chatBridgePkg + "/messageattachment"
	cbAction      = chatBridgePkg + "/chataction"
	cbEvents      = chatBridgePkg + "/chatevents"
	cbFixtures    = "testdata/chatbridge/"
	cbPolicy      = "chatbridge-agent-actions"
	cbTrigger     = "chatbridge-events"
	// The fixtures' invented owner: a bridge at home, Ada's WhatsApp, Ben, and
	// two chats.
	cbUUID   = "0b3f6f2e-6d8c-4c47-9a7e-2d8b1f5d9c11"
	cbNet    = "whatsapp"
	cbLogin  = "447700900123"
	cbAda    = "447700900123@s.whatsapp.net"
	cbBen    = "lid:113920541"
	cbDirect = "447700900456@s.whatsapp.net"
	cbGroup  = "120363025246125486@g.us"
)

// cbFile is one file part a fixture's request carries.
type cbFile struct {
	Part      string `json:"part"`
	Filename  string `json:"filename"`
	MediaType string `json:"mediaType"`
	Text      string `json:"text"`
}

// cbFixture is one golden batch and the files beside it.
type cbFixture struct {
	Fixture struct {
		Note  string   `json:"note"`
		Files []cbFile `json:"files"`
	} `json:"_fixture"`
	Batch map[string]any `json:"batch"`
}

func cbLoad(t *testing.T, name string) cbFixture {
	t.Helper()
	raw, err := os.ReadFile(cbFixtures + name)
	if err != nil {
		t.Fatal(err)
	}
	var f cbFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return f
}

// newChatBridge installs the package through the catalog's door, as an owner
// or an app's approval does, on a repository holding the seed alone.
func newChatBridge(t *testing.T) substrate.Dataset {
	t.Helper()
	requirePython(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	installShipped(t, ds, chatBridgePkg)
	return ds
}

// cbBatch is a batch of the fixtures' instance over the given events, its
// first and last taken from them.
func cbBatch(events ...map[string]any) map[string]any {
	first, last := 0, 0
	list := make([]any, 0, len(events))
	for _, e := range events {
		seq := e["seq"].(int)
		if first == 0 || seq < first {
			first = seq
		}
		last = max(last, seq)
		list = append(list, e)
	}
	return map[string]any{"version": 1, "instance": cbUUID, "first": first, "last": last, "events": list}
}

// cbEvent is one event of the fixtures' WhatsApp account: the type, the
// account's two ids, and the fields given.
func cbEvent(seq int, typ string, fields map[string]any) map[string]any {
	e := map[string]any{
		"seq": seq, "type": typ, "at": "2026-11-09T12:00:00Z",
		"network": cbNet, "account": cbLogin,
	}
	for k, v := range fields {
		e[k] = v
	}
	return e
}

// cbRequest is the request the webhook door hands the function: the batch as
// an inline text part, each file stored as a blob first.
func cbRequest(t *testing.T, ds substrate.Dataset, batch map[string]any, files []cbFile) map[string]any {
	t.Helper()
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	parts := []any{map[string]any{"name": "batch", "value": string(raw)}}
	for _, f := range files {
		info, err := ds.PutBlob(context.Background(), substrate.ActorAPI,
			substrate.BlobUpload{Name: f.Filename, MediaType: f.MediaType}, []byte(f.Text), "")
		if err != nil {
			t.Fatalf("store part %s: %v", f.Part, err)
		}
		parts = append(parts, map[string]any{
			"name": f.Part, "filename": f.Filename, "mediaType": f.MediaType,
			"size": len(f.Text), "blob": info.Digest,
		})
	}
	return map[string]any{
		"method": "POST", "contentType": "multipart/form-data",
		"headers": map[string]any{}, "query": map[string]any{}, "parts": parts,
	}
}

// cbTry runs one delivery of the request and commits its effects as the
// dispatcher does, answering what the delivery would fail with: the body's
// refusal, or the commit's.
func cbTry(t *testing.T, ds substrate.Dataset, req map[string]any, at string) ([]engine.StepEffect, map[string]any, error) {
	t.Helper()
	ctx := context.Background()
	s, err := engine.NewStepper(ds, cbEvents, map[string]any{"bundle": chatBridgePkg})
	if err != nil {
		t.Fatal(err)
	}
	s.SetEnvelope(map[string]any{
		"request":    req,
		"fire":       map[string]any{"id": "hook-" + strings.ReplaceAll(at, ":", ""), "at": at},
		"repository": map[string]any{"authority": "ada.example.com"},
	})
	effects, out, cur, err := s.Step(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	if cur != nil {
		t.Fatal("chatevents paged; one delivery is one batch")
	}
	if err := s.Apply(ctx); err != nil {
		return effects, nil, err
	}
	o, _ := out.(map[string]any)
	return effects, o, nil
}

// cbDeliver is cbTry for a delivery that must apply.
func cbDeliver(t *testing.T, ds substrate.Dataset, batch map[string]any, files []cbFile, at string) (map[string]any, []engine.StepEffect) {
	t.Helper()
	effects, out, err := cbTry(t, ds, cbRequest(t, ds, batch, files), at)
	if err != nil {
		t.Fatalf("deliver %v..%v: %v", batch["first"], batch["last"], err)
	}
	return out, effects
}

// cbGolden delivers one fixture.
func cbGolden(t *testing.T, ds substrate.Dataset, name, at string) map[string]any {
	t.Helper()
	f := cbLoad(t, name)
	out, _ := cbDeliver(t, ds, f.Batch, f.Fixture.Files, at)
	return out
}

func cbID(external string) string { return runner.ExternalID(cbNet, cbLogin, external) }

func cbInstanceID() string { return runner.ExternalID("chatbridge", cbUUID, "instance") }

func cbAccountID() string { return cbID("account") }

func cbUserID(uid string) string { return cbID("user/" + uid) }

func cbChatID(cid string) string { return cbID("chat/" + cid) }

func cbMessageID(cid, mid string) string { return cbID("message/" + cid + "/" + mid) }

func cbAttachmentID(cid, mid, part string) string {
	return cbID("attachment/" + cid + "/" + mid + "/" + part)
}

func cbPath(kind, id string) string { return vocabulary.RecordPath(kind, id) }

// cbNum reads a number a record carries.
func cbNum(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return -1
}

// cbOwnerAction writes a chataction as the owner does from the console:
// under a minted id, with the request properties alone.
func cbOwnerAction(t *testing.T, ds substrate.Dataset, name string, props map[string]any) *substrate.Record {
	t.Helper()
	return mustPut(t, ds, substrate.PutInput{Kind: cbAction, ID: runner.MintedID("test", name), Properties: props})
}

func cbInstanceRow(t *testing.T, ds substrate.Dataset) *substrate.Record {
	t.Helper()
	return mustGet(t, ds, cbInstance, cbInstanceID())
}

// The install lands the gate policy in the same apply as the kinds it names
// (validatePolicyRow resolves its selector against that apply's candidate
// registry), beside the webhook trigger shipped off and keyless and the
// record views a chat and an account read through. An install over a policy
// the owner edited leaves the edit alone.
func TestChatBridgeInstallAdmitsTheGateWithTheKinds(t *testing.T) {
	ds := newChatBridge(t)
	ctx := context.Background()
	for id, kind := range map[string]string{chatBridgePkg + "/chat": cbChat, chatBridgePkg + "/account": cbAccount} {
		view := mustGet(t, ds, "substrate.reamde.dev/console/recordview", id)
		if got := storedRefPath(view.Properties["appliesTo"]); got != vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocKind), kind) {
			t.Errorf("view %s applies to %s, want %s", id, got, kind)
		}
		if len(asList(view.Properties["sections"])) == 0 {
			t.Errorf("view %s draws no section", id)
		}
	}
	policy := mustGet(t, ds, vocabulary.KindRecordPatchPolicy, cbPolicy)
	if policy.Properties["action"] != "gate" {
		t.Fatalf("the shipped policy's action is %v, want gate", policy.Properties["action"])
	}
	sel, _ := policy.Properties["selector"].(map[string]any)
	if kinds := asList(sel["kinds"]); len(kinds) != 1 || kinds[0] != cbAction {
		t.Fatalf("the shipped policy selects %v, want the chataction kind alone", sel["kinds"])
	}
	if ops := asList(sel["ops"]); len(ops) != 3 {
		t.Fatalf("the shipped policy's ops are %v, want put, patch and delete", ops)
	}
	if len(asList(sel["agents"])) != 0 || len(asList(sel["functions"])) != 0 {
		t.Fatalf("the shipped policy names agents or functions: %v", sel)
	}
	trigger := mustGet(t, ds, vocabulary.TriggerKind, cbTrigger)
	if trigger.Properties["enabled"] != false || vocabulary.TriggerKey(trigger.Properties) != "" ||
		vocabulary.TriggerWebhook(trigger.Properties) == nil {
		t.Fatalf("the shipped trigger is %v, want a disabled keyless webhook", trigger.Properties)
	}

	// Installing the unchanged closure again writes nothing.
	head, err := ds.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	installShipped(t, ds, chatBridgePkg)
	if after, err := ds.Head(ctx); err != nil || after != head {
		t.Fatalf("installing the unchanged package again moved the changelog head %v -> %v (%v)", head, after, err)
	}

	// The owner gives the gate a criteria line; installing again keeps it.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchPolicy, cbPolicy, substrate.PatchInput{
		Properties: map[string]any{"criteria": "reactions are fine; ask me before anything is sent"},
	}); err != nil {
		t.Fatalf("edit the policy: %v", err)
	}
	installShipped(t, ds, chatBridgePkg)
	if got := mustGet(t, ds, vocabulary.KindRecordPatchPolicy, cbPolicy); got.Properties["criteria"] == nil {
		t.Fatal("installing again overwrote the owner's edit of the gate")
	}
}

// The golden batches, in order, as the contract's worked example: every row
// lands at the id the vectors give, and each merge rule holds on the result.
func TestChatBridgeGoldenBatches(t *testing.T) {
	ds := newChatBridge(t)
	out := cbGolden(t, ds, "batch_1_link.json", "2026-11-09T10:00:01Z")
	if out["applied"] != float64(13) || out["skipped"] != float64(0) || out["appliedSeq"] != float64(13) {
		t.Fatalf("batch 1 output %v, want 13 applied", out)
	}

	inst := cbInstanceRow(t, ds)
	if inst.Properties["instanceId"] != cbUUID || inst.Properties["instanceName"] != "home" ||
		cbNum(inst.Properties["appliedSeq"]) != 13 || inst.Properties["lastSeenAt"] != "2026-11-09T10:00:01Z" {
		t.Fatalf("instance after batch 1: %v", inst.Properties)
	}
	acct := mustGet(t, ds, cbAccount, cbAccountID())
	if acct.Properties["loginId"] != cbLogin || acct.Properties["connectionState"] != "connected" ||
		acct.Properties["syncState"] != "ok" || acct.Properties["lastSyncedAt"] != "2026-11-09T10:00:01Z" {
		t.Fatalf("account after batch 1: %v", acct.Properties)
	}
	if got := refIDs(acct, "user"); len(got) != 1 || got[0] != cbUserID(cbAda) {
		t.Fatalf("the account's own user is %v, want %s", got, cbUserID(cbAda))
	}
	if got := refIDs(acct, "instance"); len(got) != 1 || got[0] != cbInstanceID() {
		t.Fatalf("the account runs on %v", got)
	}
	ben := mustGet(t, ds, cbUser, cbUserID(cbBen))
	if ben.Properties["fullName"] != "Ben" || ben.Properties["contactName"] != "Ben (work)" ||
		ben.Properties["avatar"] == nil {
		t.Fatalf("Ben after batch 1: %v", ben.Properties)
	}
	direct := mustGet(t, ds, cbChat, cbChatID(cbDirect))
	if direct.Properties["category"] != "direct" || refIDs(direct, "otherUser")[0] != cbUserID(cbBen) ||
		len(asList(direct.Properties["members"])) != 2 {
		t.Fatalf("the direct chat after batch 1: %v", direct.Properties)
	}
	if got := refIDs(direct, "readUpTo"); len(got) != 1 || got[0] != cbMessageID(cbDirect, "3EB0C1F2A9") {
		t.Fatalf("the direct chat is read up to %v", got)
	}
	reply := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "3EB0D4E5F6"))
	if reply.Properties["fromOwner"] != true || reply.Properties["sendStatus"] != "delivered" ||
		refIDs(reply, "replyTo")[0] != cbMessageID(cbDirect, "3EB0C1F2A9") ||
		reply.Properties["at"] != "2026-11-09T10:00:00.05Z" || reply.Properties["backfilled"] != false {
		t.Fatalf("Ada's reply after batch 1: %v", reply.Properties)
	}
	photo := mustGet(t, ds, cbAttachment, cbAttachmentID(cbGroup, "3EB0AA11", "0"))
	if photo.Properties["mediaState"] != "stored" || photo.Properties["file"] == nil ||
		cbNum(photo.Properties["width"]) != 600 {
		t.Fatalf("the photo after batch 1: %v", photo.Properties)
	}
	if streams, _ := acct.Properties["syncStreams"].(map[string]any); streams["history"] == nil {
		t.Fatalf("the account's history stream after batch 1: %v", acct.Properties["syncStreams"])
	}

	out = cbGolden(t, ds, "batch_2_live.json", "2026-11-09T10:10:01Z")
	if out["applied"] != float64(8) || out["skipped"] != float64(0) {
		t.Fatalf("batch 2 output %v, want 8 applied", out)
	}
	msg := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "3EB0C1F2A9"))
	if msg.Properties["text"] != "see you at 8" || cbNum(msg.Properties["editCount"]) != 1 {
		t.Fatalf("Ben's message after his edit: %v", msg.Properties)
	}
	if rs := asList(msg.Properties["reactions"]); len(rs) != 1 {
		t.Fatalf("reactions after Ada's: %v", rs)
	}
	group := mustGet(t, ds, cbChat, cbChatID(cbGroup))
	if got := refIDs(group, "members"); len(got) != 2 || !slices.Contains(got, cbUserID("lid:2001")) ||
		slices.Contains(got, cbUserID(cbBen)) {
		t.Fatalf("the book club's members after Cleo joined and Ben left: %v", got)
	}
	gone := mustGet(t, ds, cbMessage, cbMessageID(cbGroup, "3EB0AA11"))
	if gone.Properties["deleted"] != "everyone" || gone.Properties["text"] != nil ||
		gone.Properties["deletedAt"] != "2026-11-09T10:06:59Z" {
		t.Fatalf("the photo message deleted for everyone: %v", gone.Properties)
	}
	photo = mustGet(t, ds, cbAttachment, cbAttachmentID(cbGroup, "3EB0AA11", "0"))
	if photo.Properties["file"] != nil || photo.Properties["caption"] != nil {
		t.Fatalf("the photo of a message deleted for everyone still holds %v", photo.Properties)
	}
	acct = mustGet(t, ds, cbAccount, cbAccountID())
	if acct.Properties["connectionState"] != "connected" || acct.Properties["warnings"] != nil ||
		acct.Properties["syncProgress"] != nil {
		t.Fatalf("the account back online with history complete: %v", acct.Properties)
	}

	out = cbGolden(t, ds, "batch_3_history.json", "2026-11-09T10:11:01Z")
	if out["applied"] != float64(3) {
		t.Fatalf("batch 3 output %v, want 3 applied", out)
	}
	if got := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "3EB0C1F2A9")); got.Properties["text"] != "see you at 8" {
		t.Fatalf("a page of history undid Ben's edit: %v", got.Properties["text"])
	}
	old := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "3EB0000001"))
	if old.Properties["backfilled"] != true || old.Properties["text"] != "hello from August" {
		t.Fatalf("the August message: %v", old.Properties)
	}
	if got := mustGet(t, ds, cbChat, cbChatID(cbDirect)); got.Properties["lastActivityAt"] != "2026-11-09T09:59:58Z" {
		t.Fatalf("a page of history moved the chat's last activity back to %v", got.Properties["lastActivityAt"])
	}
	if got := cbNum(cbInstanceRow(t, ds).Properties["appliedSeq"]); got != 24 {
		t.Fatalf("appliedSeq after the three batches is %d, want 24", got)
	}
}

// The id vectors are the rows the golden batches write: every vector names a
// live row once the batches applied, at the id runner.ExternalID composes.
func TestChatBridgeIDVectorsNameTheGoldenRows(t *testing.T) {
	ds := newChatBridge(t)
	cbGolden(t, ds, "batch_1_link.json", "2026-11-09T10:00:01Z")
	kinds := map[string]string{
		"instance": cbInstance, "account": cbAccount, "user": cbUser, "chat": cbChat,
		"message": cbMessage, "messageattachment": cbAttachment,
	}
	checked := 0
	for _, v := range cbVectors(t) {
		if v.External.Provider == "signal" {
			continue
		}
		if !live(t, ds, kinds[v.Row], v.ID) {
			t.Errorf("vector %s %v names %s, which batch 1 did not write", v.Row, v.Inputs, v.ID)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no vector was checked")
	}
}

// A resent batch is a no-op, a batch that skips one ahead fails and waits,
// and a batch that overlaps the applied sequence applies only its new
// events.
func TestChatBridgeResentAndReversedBatches(t *testing.T) {
	ds := newChatBridge(t)
	one := cbBatch(
		cbEvent(1, "account.upsert", map[string]any{"remoteName": "Ada"}),
		cbEvent(2, "chat.upsert", map[string]any{"chat": map[string]any{"id": cbDirect, "chatName": "first"}}),
	)
	two := cbBatch(cbEvent(3, "chat.upsert", map[string]any{"chat": map[string]any{"id": cbDirect, "chatName": "second"}}))
	three := cbBatch(cbEvent(4, "chat.upsert", map[string]any{"chat": map[string]any{"id": cbDirect, "chatName": "third"}}))

	cbDeliver(t, ds, one, nil, "2026-11-09T12:00:00Z")
	before := mustGet(t, ds, cbChat, cbChatID(cbDirect))
	out, effects := cbDeliver(t, ds, one, nil, "2026-11-09T12:00:01Z")
	if len(effects) != 0 || out["already"] != float64(2) {
		t.Fatalf("a resent batch staged %v and answered %v", effects, out)
	}
	if got := mustGet(t, ds, cbChat, cbChatID(cbDirect)); got.Version != before.Version {
		t.Fatal("a resent batch moved the chat")
	}

	// Batch three before two: refused, nothing written, and it applies once
	// two has.
	if _, _, err := cbTry(t, ds, cbRequest(t, ds, three, nil), "2026-11-09T12:00:02Z"); err == nil ||
		!strings.Contains(err.Error(), "does not continue its applied sequence") {
		t.Fatalf("a batch past a gap answered %v", err)
	}
	if got := cbNum(cbInstanceRow(t, ds).Properties["appliedSeq"]); got != 2 {
		t.Fatalf("a refused batch moved appliedSeq to %d", got)
	}
	cbDeliver(t, ds, two, nil, "2026-11-09T12:00:03Z")
	cbDeliver(t, ds, three, nil, "2026-11-09T12:00:04Z")
	if got := mustGet(t, ds, cbChat, cbChatID(cbDirect)); got.Properties["chatName"] != "third" {
		t.Fatalf("after two then three the chat is %v", got.Properties["chatName"])
	}

	// A batch reaching back over the applied sequence applies what is new.
	overlap := cbBatch(
		cbEvent(4, "chat.upsert", map[string]any{"chat": map[string]any{"id": cbDirect, "chatName": "stale"}}),
		cbEvent(5, "chat.upsert", map[string]any{"chat": map[string]any{"id": cbDirect, "chatName": "fifth"}}),
	)
	out, _ = cbDeliver(t, ds, overlap, nil, "2026-11-09T12:00:05Z")
	if out["already"] != float64(1) || out["applied"] != float64(1) {
		t.Fatalf("the overlapping batch answered %v", out)
	}
	if got := mustGet(t, ds, cbChat, cbChatID(cbDirect)); got.Properties["chatName"] != "fifth" {
		t.Fatalf("after the overlap the chat is %v", got.Properties["chatName"])
	}

	// Two fires of one batch racing on the instance: the second commits
	// against the version the first moved, and fails rather than apply twice.
	six := cbBatch(cbEvent(6, "chat.upsert", map[string]any{"chat": map[string]any{"id": cbDirect, "archived": true}}))
	ctx := context.Background()
	first, err := engine.NewStepper(ds, cbEvents, map[string]any{"bundle": chatBridgePkg})
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.NewStepper(ds, cbEvents, map[string]any{"bundle": chatBridgePkg})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*engine.Stepper{first, second} {
		s.SetEnvelope(map[string]any{
			"request": cbRequest(t, ds, six, nil),
			"fire":    map[string]any{"id": "hook-six", "at": "2026-11-09T12:00:06Z"},
		})
		if _, _, _, err := s.Step(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.Apply(ctx); err != nil {
		t.Fatalf("the first fire: %v", err)
	}
	if err := second.Apply(ctx); err == nil || !strings.Contains(err.Error(), "ifVersion") {
		t.Fatalf("the second fire of the same batch committed: %v", err)
	}
}

// cbStream numbers one instance's events in order, as the bridge's outbox
// does, so a case reads as the events it sends.
type cbStream struct{ seq int }

// ev is the next event of the fixtures' WhatsApp account.
func (s *cbStream) ev(typ string, fields map[string]any) map[string]any {
	s.seq++
	return cbEvent(s.seq, typ, fields)
}

// raw is the next event as given, with no account: an action's report.
func (s *cbStream) raw(typ string, fields map[string]any) map[string]any {
	s.seq++
	e := map[string]any{"seq": s.seq, "type": typ}
	for k, v := range fields {
		e[k] = v
	}
	return e
}

// chat is the next chat.upsert of one chat with the fields given.
func (s *cbStream) chat(id string, fields map[string]any) map[string]any {
	c := map[string]any{"id": id}
	for k, v := range fields {
		c[k] = v
	}
	return s.ev("chat.upsert", map[string]any{"chat": c})
}

// message is the next message.upsert of one message from Ben.
func (s *cbStream) message(chat, id string, fields map[string]any) map[string]any {
	m := map[string]any{"id": id, "sender": cbBen, "fromOwner": false, "at": "2026-11-09T11:00:00Z", "content": "text"}
	for k, v := range fields {
		m[k] = v
	}
	return s.ev("message.upsert", map[string]any{"chat": chat, "message": m})
}

// cbOpen signs the fixtures' account in and writes the chats named, which is
// how every case below starts: an event needs its account, and a chat's
// events their chat.
func cbOpen(t *testing.T, ds substrate.Dataset, s *cbStream, chats ...string) {
	t.Helper()
	events := []map[string]any{s.ev("account.upsert", map[string]any{"self": cbAda})}
	for _, c := range chats {
		events = append(events, s.chat(c, nil))
	}
	cbDeliver(t, ds, cbBatch(events...), nil, "2026-11-09T11:59:00Z")
}

// The absent, null and empty rule, member lists complete and partial, and a
// chat's flags patched one at a time.
func TestChatBridgeMixedPartialEvents(t *testing.T) {
	ds := newChatBridge(t)
	s := &cbStream{}
	cbOpen(t, ds, s)
	cbDeliver(t, ds, cbBatch(
		s.ev("user.upsert", map[string]any{"user": map[string]any{
			"id": cbBen, "fullName": "Ben", "handle": "ben", "phoneNumbers": []any{"+447700900456"},
			"emails": []any{"ben@example.com", "not an address"},
		}}),
		s.chat(cbGroup, map[string]any{
			"category": "group", "chatName": "Book club", "topic": "monthly",
			"pinned": true, "archived": false,
			"members": map[string]any{"complete": true, "items": []any{
				map[string]any{"user": cbAda, "membership": "join", "role": "admin"},
				map[string]any{"user": cbBen, "membership": "join"},
				map[string]any{"user": "lid:3", "membership": "invite"},
			}},
		}),
	), nil, "2026-11-09T12:00:00Z")
	ben := mustGet(t, ds, cbUser, cbUserID(cbBen))
	if got := asList(ben.Properties["emails"]); len(got) != 1 || got[0] != "ben@example.com" {
		t.Fatalf("Ben's emails %v: an address the engine refuses is left out, not the person", got)
	}

	cbDeliver(t, ds, cbBatch(
		// handle absent keeps it, fullName null clears it, an empty list
		// clears the numbers.
		s.ev("user.upsert", map[string]any{"user": map[string]any{
			"id": cbBen, "fullName": nil, "phoneNumbers": []any{},
		}}),
		// One flag at a time; the member list changes only where it is named.
		s.chat(cbGroup, map[string]any{
			"archived": true, "topic": nil,
			"members": map[string]any{"complete": false, "items": []any{
				map[string]any{"user": "lid:3", "membership": "join", "role": "member"},
				map[string]any{"user": cbBen, "membership": "ban"},
			}},
		}),
	), nil, "2026-11-09T12:00:01Z")
	ben = mustGet(t, ds, cbUser, cbUserID(cbBen))
	if ben.Properties["handle"] != "ben" || ben.Properties["fullName"] != nil || ben.Properties["phoneNumbers"] != nil {
		t.Fatalf("Ben after a partial upsert: %v", ben.Properties)
	}
	group := mustGet(t, ds, cbChat, cbChatID(cbGroup))
	if group.Properties["chatName"] != "Book club" || group.Properties["pinned"] != true ||
		group.Properties["archived"] != true || group.Properties["topic"] != nil {
		t.Fatalf("the group after a partial upsert: %v", group.Properties)
	}
	members := map[string]map[string]any{}
	for _, m := range asList(group.Properties["members"]) {
		mm, _ := m.(map[string]any)
		_, id, _ := vocabulary.SplitRecordPath(storedRefPath(mm))
		members[id] = mm
	}
	if len(members) != 2 || members[cbUserID(cbAda)]["role"] != "admin" ||
		members[cbUserID("lid:3")]["membership"] != "join" || members[cbUserID(cbBen)] != nil {
		t.Fatalf("members after a partial list: %v", members)
	}

	// A complete list replaces the members whole.
	cbDeliver(t, ds, cbBatch(s.chat(cbGroup, map[string]any{"members": map[string]any{
		"complete": true,
		"items":    []any{map[string]any{"user": cbAda, "membership": "join"}},
	}})), nil, "2026-11-09T12:00:02Z")
	if got := refIDs(mustGet(t, ds, cbChat, cbChatID(cbGroup)), "members"); len(got) != 1 || got[0] != cbUserID(cbAda) {
		t.Fatalf("members after a complete list of one: %v", got)
	}
	// null clears them.
	cbDeliver(t, ds, cbBatch(s.chat(cbGroup, map[string]any{"members": nil})), nil, "2026-11-09T12:00:03Z")
	if got := mustGet(t, ds, cbChat, cbChatID(cbGroup)).Properties["members"]; got != nil {
		t.Fatalf("members after null: %v", got)
	}
}

// A large member list arrives split over several chat.upsert events of at
// most 1,000 members each, in one batch or more: the first replaces the
// list, the rest add to it, and a later first replaces it again.
func TestChatBridgeSplitMemberList(t *testing.T) {
	ds := newChatBridge(t)
	s := &cbStream{}
	cbOpen(t, ds, s)
	chunk := func(from, to int, complete bool) map[string]any {
		items := make([]any, 0, to-from)
		for i := from; i < to; i++ {
			items = append(items, map[string]any{"user": fmt.Sprintf("lid:%d", i), "membership": "join"})
		}
		return s.chat(cbGroup, map[string]any{"members": map[string]any{"complete": complete, "items": items}})
	}
	cbDeliver(t, ds, cbBatch(chunk(0, 1000, true), chunk(1000, 2000, false)), nil, "2026-11-09T12:00:00Z")
	cbDeliver(t, ds, cbBatch(chunk(2000, 2500, false)), nil, "2026-11-09T12:00:01Z")
	members := refIDs(mustGet(t, ds, cbChat, cbChatID(cbGroup)), "members")
	if len(members) != 2500 || !slices.Contains(members, cbUserID("lid:0")) || !slices.Contains(members, cbUserID("lid:2499")) {
		t.Fatalf("a list of 2,500 split over three events holds %d members", len(members))
	}
	cbDeliver(t, ds, cbBatch(chunk(500, 1500, true), chunk(1500, 2000, false)), nil, "2026-11-09T12:00:02Z")
	members = refIDs(mustGet(t, ds, cbChat, cbChatID(cbGroup)), "members")
	if len(members) != 1500 || slices.Contains(members, cbUserID("lid:0")) || slices.Contains(members, cbUserID("lid:2499")) {
		t.Fatalf("the next complete list of 1,500 left %d members", len(members))
	}
}

// Events about rows that are not there: a delete, a status or reactions for
// a message the mirror does not hold, and anything for a chat it does not,
// are skipped and counted; a reply naming a message not yet written is
// written anyway. A chat's delete tombstones it, and the engine collects its
// messages when it collects the chat.
func TestChatBridgeDeleteBeforeCreate(t *testing.T) {
	ds := newChatBridge(t)
	ctx := context.Background()
	s := &cbStream{}
	cbOpen(t, ds, s)
	out, _ := cbDeliver(t, ds, cbBatch(
		s.ev("message.delete", map[string]any{
			"chat": cbDirect, "message": "m1", "scope": "everyone",
			"at": "2026-11-09T11:00:01Z",
		}),
		s.ev("message.status", map[string]any{
			"chat": cbDirect, "message": "m1", "status": "read",
			"at": "2026-11-09T11:00:01Z",
		}),
		s.message(cbDirect, "m1", map[string]any{"text": "before its chat"}),
		s.chat(cbDirect, map[string]any{"category": "direct"}),
		s.ev("message.reactions", map[string]any{"chat": cbDirect, "message": "m1", "reactions": []any{}}),
		s.message(cbDirect, "m1", map[string]any{"text": "now it exists", "replyTo": "not-yet"}),
	), nil, "2026-11-09T12:00:00Z")
	if out["skipped"] != float64(4) || out["applied"] != float64(2) {
		t.Fatalf("events before their rows answered %v, want 4 skipped", out)
	}
	inst := cbInstanceRow(t, ds)
	if cbNum(inst.Properties["skippedEvents"]) != 4 ||
		!strings.Contains(fmt.Sprint(inst.Properties["lastSkipped"]), "seq 6 message.reactions") {
		t.Fatalf("the instance counts %v skipped, last %v", inst.Properties["skippedEvents"], inst.Properties["lastSkipped"])
	}
	m1 := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "m1"))
	if m1.Properties["deleted"] != nil || m1.Properties["text"] != "now it exists" ||
		refIDs(m1, "replyTo")[0] != cbMessageID(cbDirect, "not-yet") {
		t.Fatalf("m1 after its delete arrived first: %v", m1.Properties)
	}

	// The chat deleted: a message for it is skipped, and its stored messages
	// go when the engine collects it.
	out, _ = cbDeliver(t, ds, cbBatch(
		s.ev("chat.delete", map[string]any{"chat": cbDirect}),
		s.message(cbDirect, "m2", map[string]any{"text": "into a deleted chat"}),
	), nil, "2026-11-09T12:00:01Z")
	if out["skipped"] != float64(1) || live(t, ds, cbChat, cbChatID(cbDirect)) || live(t, ds, cbMessage, cbMessageID(cbDirect, "m2")) {
		t.Fatalf("after the chat's delete: %v", out)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	if live(t, ds, cbMessage, cbMessageID(cbDirect, "m1")) {
		t.Fatal("the chat's message outlived the chat's collection")
	}
	cbDeliver(t, ds, cbBatch(
		s.message(cbDirect, "m3", map[string]any{"text": "still deleted"}),
		s.chat(cbDirect, map[string]any{"chatName": "back"}),
		s.message(cbDirect, "m1", map[string]any{"text": "m1 again"}),
	), nil, "2026-11-09T12:00:02Z")
	if !live(t, ds, cbChat, cbChatID(cbDirect)) || live(t, ds, cbMessage, cbMessageID(cbDirect, "m3")) {
		t.Fatal("the chat did not come back, or a message before it did")
	}
	if got := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "m1")); got.Properties["text"] != "m1 again" {
		t.Fatalf("m1 written again: %v", got.Properties)
	}

	// Deleted and brought back inside one batch: both writes land, in that
	// order, and the chat keeps what it held, its messages included.
	cbDeliver(t, ds, cbBatch(
		s.ev("chat.delete", map[string]any{"chat": cbDirect}),
		s.chat(cbDirect, map[string]any{"chatName": "again"}),
		s.message(cbDirect, "m4", map[string]any{"text": "after the comeback"}),
	), nil, "2026-11-09T12:00:03Z")
	chat := mustGet(t, ds, cbChat, cbChatID(cbDirect))
	if chat.Properties["chatName"] != "again" || !live(t, ds, cbMessage, cbMessageID(cbDirect, "m4")) ||
		!live(t, ds, cbMessage, cbMessageID(cbDirect, "m1")) {
		t.Fatalf("a chat deleted and brought back in one batch: %v", chat.Properties)
	}
}

// A page of history that carries an older copy of a message never undoes an
// edit, and one that names no edit count is the copy from before any.
func TestChatBridgeStaleHistoryPage(t *testing.T) {
	ds := newChatBridge(t)
	s := &cbStream{}
	cbOpen(t, ds, s, cbDirect)
	upsert := func(backfill bool, fields map[string]any) map[string]any {
		e := s.message(cbDirect, "m1", fields)
		e["backfill"] = backfill
		return e
	}
	cbDeliver(t, ds, cbBatch(upsert(false, map[string]any{"text": "v2", "editCount": 2, "editedAt": "2026-11-09T11:05:00Z"})),
		nil, "2026-11-09T12:00:00Z")
	before := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "m1"))
	_, effects := cbDeliver(t, ds, cbBatch(
		upsert(true, map[string]any{"text": "v0"}),
		upsert(true, map[string]any{"text": "v1", "editCount": 1}),
	), nil, "2026-11-09T12:00:01Z")
	for _, ef := range effects {
		if ef.Kind == cbMessage {
			t.Fatalf("an older copy wrote the message: %v", ef)
		}
	}
	after := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "m1"))
	if after.Properties["text"] != "v2" || after.Version != before.Version || after.Properties["backfilled"] != false {
		t.Fatalf("after two older copies: %v", after.Properties)
	}
	cbDeliver(t, ds, cbBatch(upsert(false, map[string]any{"text": "v3", "editCount": 3})), nil, "2026-11-09T12:00:02Z")
	if got := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "m1")); got.Properties["text"] != "v3" {
		t.Fatalf("a newer edit did not land: %v", got.Properties["text"])
	}
}

// Duplicates: one event delivered again in a later batch changes nothing,
// and reactions converge on the whole set the last report carried,
// duplicates dropped.
func TestChatBridgeDuplicates(t *testing.T) {
	ds := newChatBridge(t)
	s := &cbStream{}
	cbOpen(t, ds, s, cbDirect)
	cbDeliver(t, ds, cbBatch(s.message(cbDirect, "m1", map[string]any{"text": "once"})), nil, "2026-11-09T12:00:00Z")
	before := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "m1"))
	first := cbNum(before.Properties["sourceSeq"])
	cbDeliver(t, ds, cbBatch(s.message(cbDirect, "m1", map[string]any{"text": "once"})), nil, "2026-11-09T12:00:01Z")
	if got := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "m1")); got.Version != before.Version ||
		cbNum(got.Properties["sourceSeq"]) != first {
		t.Fatalf("the same message delivered again moved it: version %d -> %d, sourceSeq %v",
			before.Version, got.Version, got.Properties["sourceSeq"])
	}
	react := func(keys ...string) map[string]any {
		rs := []any{}
		for _, k := range keys {
			rs = append(rs, map[string]any{"key": k, "user": cbAda, "at": "2026-11-09T11:01:00Z"})
		}
		return s.ev("message.reactions", map[string]any{"chat": cbDirect, "message": "m1", "reactions": rs})
	}
	cbDeliver(t, ds, cbBatch(react("👍", "👍", "😂"), react("😂")), nil, "2026-11-09T12:00:02Z")
	if got := asList(mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "m1")).Properties["reactions"]); len(got) != 1 {
		t.Fatalf("reactions after the whole set shrank to one: %v", got)
	}
	cbDeliver(t, ds, cbBatch(react()), nil, "2026-11-09T12:00:03Z")
	if got := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "m1")).Properties["reactions"]; got != nil {
		t.Fatalf("an empty set left reactions %v", got)
	}
}

// One action's whole life, as the owner writes it from the console: queued
// on create, claimed into sending with its attempt and the version claimed,
// done with the network's message, and linked by the echo of the message it
// sent. A claim and a result in one batch move it through both states.
func TestChatBridgeActionLife(t *testing.T) {
	ds := newChatBridge(t)
	s := &cbStream{}
	cbOpen(t, ds, s, cbDirect)
	chat := cbPath(cbChat, cbChatID(cbDirect))
	send := cbOwnerAction(t, ds, "send", map[string]any{"action": "send", "chat": chat, "text": "on my way", "allowNewChat": true})
	if send.Properties["state"] != "queued" {
		t.Fatalf("a new action is %v, want queued", send.Properties["state"])
	}
	cbDeliver(t, ds, cbBatch(s.raw("action.claimed", map[string]any{
		"action": send.ID, "attempt": "att-1",
		"version": float64(send.Version),
	})), nil, "2026-11-09T12:00:01Z")
	claimed := mustGet(t, ds, cbAction, send.ID)
	if claimed.Properties["state"] != "sending" || claimed.Properties["attempt"] != "att-1" ||
		cbNum(claimed.Properties["claimedVersion"]) != send.Version {
		t.Fatalf("the claimed action: %v", claimed.Properties)
	}
	echo := s.message(cbDirect, "3EB0SENT", map[string]any{
		"sender": cbAda, "fromOwner": true, "text": "on my way",
		"action": send.ID,
	})
	cbDeliver(t, ds, cbBatch(
		s.raw("action.result", map[string]any{"action": send.ID, "attempt": "att-1", "outcome": "done", "message": "3EB0SENT"}),
		echo,
	), nil, "2026-11-09T12:00:02Z")
	done := mustGet(t, ds, cbAction, send.ID)
	if done.Properties["state"] != "done" || refIDs(done, "result")[0] != cbMessageID(cbDirect, "3EB0SENT") {
		t.Fatalf("the done action: %v", done.Properties)
	}

	// Claimed and done in one delivery: two writes, one transition each.
	react := cbOwnerAction(t, ds, "react", map[string]any{
		"action": "react", "chat": chat,
		"target": cbPath(cbMessage, cbMessageID(cbDirect, "3EB0SENT")), "reactionKey": "👍",
	})
	cbDeliver(t, ds, cbBatch(
		s.raw("action.claimed", map[string]any{"action": react.ID, "attempt": "att-2", "version": float64(react.Version)}),
		s.raw("action.result", map[string]any{"action": react.ID, "attempt": "att-2", "outcome": "done"}),
	), nil, "2026-11-09T12:00:03Z")
	if got := mustGet(t, ds, cbAction, react.ID); got.Properties["state"] != "done" || got.Properties["attempt"] != "att-2" {
		t.Fatalf("an action claimed and done in one batch: %v", got.Properties)
	}

	// Never claimed: an expiry, a cancel and a refusal each end it from the
	// queue.
	late := cbOwnerAction(t, ds, "late", map[string]any{"action": "markread", "chat": chat})
	withdrawn := cbOwnerAction(t, ds, "withdrawn", map[string]any{"action": "markread", "chat": chat})
	refused := cbOwnerAction(t, ds, "refused", map[string]any{
		"action": "edit", "chat": chat,
		"target": cbPath(cbMessage, cbMessageID(cbDirect, "3EB0SENT")), "text": "x",
	})
	cbDeliver(t, ds, cbBatch(
		s.raw("action.result", map[string]any{
			"action": late.ID, "attempt": "a", "outcome": "expired",
			"error": map[string]any{"code": "expired", "message": "past its notAfter"},
		}),
		s.raw("action.result", map[string]any{"action": withdrawn.ID, "attempt": "b", "outcome": "cancelled"}),
		s.raw("action.result", map[string]any{
			"action": refused.ID, "attempt": "c", "outcome": "failed",
			"error": map[string]any{"code": "not_found", "message": "the network no longer has that message"},
		}),
	), nil, "2026-11-09T12:00:04Z")
	for id, want := range map[string]string{late.ID: "expired", withdrawn.ID: "cancelled", refused.ID: "failed"} {
		if got := mustGet(t, ds, cbAction, id).Properties["state"]; got != want {
			t.Errorf("action %s is %v, want %s", id, got, want)
		}
	}
	if got := mustGet(t, ds, cbAction, refused.ID); got.Properties["errorCode"] != "notfound" ||
		got.Properties["errorMessage"] != "the network no longer has that message" {
		t.Fatalf("the failed action's error: %v", got.Properties)
	}

	// Claimed, and found after a restart never to have left once its window
	// closed: it ends expired from sending.
	stale := cbOwnerAction(t, ds, "stale", map[string]any{"action": "send", "chat": chat, "text": "too late"})
	cbDeliver(t, ds, cbBatch(
		s.raw("action.claimed", map[string]any{"action": stale.ID, "attempt": "d", "version": float64(stale.Version)}),
	), nil, "2026-11-09T12:00:05Z")
	cbDeliver(t, ds, cbBatch(
		s.raw("action.result", map[string]any{
			"action": stale.ID, "attempt": "d", "outcome": "expired",
			"error": map[string]any{"code": "expired"},
		}),
	), nil, "2026-11-09T13:10:00Z")
	if got := mustGet(t, ds, cbAction, stale.ID); got.Properties["state"] != "expired" || got.Properties["errorCode"] != "expired" {
		t.Fatalf("a claim that never left, past its window: %v", got.Properties)
	}
}

// Reports that do not match the row change nothing and are counted: a
// result for another attempt, a claim of an action already claimed, a result
// the state machine has no way to, and an action the repository does not
// hold. The claim the row already carries, reported again, is applied as the
// no-op it is.
func TestChatBridgeStaleResult(t *testing.T) {
	ds := newChatBridge(t)
	s := &cbStream{}
	cbOpen(t, ds, s, cbDirect)
	chat := cbPath(cbChat, cbChatID(cbDirect))
	act := cbOwnerAction(t, ds, "send", map[string]any{"action": "send", "chat": chat, "text": "hi"})
	queued := cbOwnerAction(t, ds, "queued", map[string]any{"action": "send", "chat": chat, "text": "hi"})
	cbDeliver(t, ds, cbBatch(s.raw("action.claimed", map[string]any{
		"action": act.ID, "attempt": "new",
		"version": float64(act.Version),
	})), nil, "2026-11-09T12:00:01Z")
	out, _ := cbDeliver(t, ds, cbBatch(
		s.raw("action.result", map[string]any{"action": act.ID, "attempt": "old", "outcome": "failed"}),
		s.raw("action.claimed", map[string]any{"action": act.ID, "attempt": "other", "version": float64(act.Version)}),
		s.raw("action.result", map[string]any{"action": queued.ID, "attempt": "x", "outcome": "done"}),
		s.raw("action.result", map[string]any{"action": runner.MintedID("nobody"), "attempt": "x", "outcome": "done"}),
		s.raw("action.claimed", map[string]any{"action": act.ID, "attempt": "new", "version": float64(act.Version)}),
	), nil, "2026-11-09T12:00:02Z")
	if out["skipped"] != float64(4) || out["applied"] != float64(1) {
		t.Fatalf("stale reports answered %v, want 4 skipped and the repeated claim applied", out)
	}
	if got := mustGet(t, ds, cbAction, act.ID); got.Properties["state"] != "sending" || got.Properties["attempt"] != "new" {
		t.Fatalf("a stale report moved the action: %v", got.Properties)
	}
	if got := mustGet(t, ds, cbAction, queued.ID); got.Properties["state"] != "queued" {
		t.Fatalf("a result for an unclaimed action moved it: %v", got.Properties)
	}
	cbDeliver(t, ds, cbBatch(s.raw("action.result", map[string]any{"action": act.ID, "attempt": "new", "outcome": "uncertain"})),
		nil, "2026-11-09T12:00:03Z")
	if got := mustGet(t, ds, cbAction, act.ID); got.Properties["state"] != "uncertain" {
		t.Fatalf("the current attempt's result: %v", got.Properties)
	}
}

// A bad event is skipped and counted, and the batch's other events and its
// watermark land: a value outside an enum, a missing id, an unknown type, an
// instant that is not one, a file part the request does not carry.
func TestChatBridgeBadEventSkipped(t *testing.T) {
	ds := newChatBridge(t)
	s := &cbStream{}
	cbOpen(t, ds, s)
	out, _ := cbDeliver(t, ds, cbBatch(
		s.chat(cbDirect, map[string]any{"category": "dm"}),
		s.ev("chat.upsert", map[string]any{"chat": map[string]any{"chatName": "no id"}}),
		s.raw("typing.start", map[string]any{"network": cbNet, "account": cbLogin}),
		s.message(cbGroup, "m1", map[string]any{"at": "yesterday"}),
		s.ev("user.upsert", map[string]any{"user": map[string]any{"id": cbBen, "avatar": map[string]any{"part": "missing"}}}),
		s.chat(cbGroup, map[string]any{"chatName": "fine"}),
	), nil, "2026-11-09T12:00:00Z")
	if out["skipped"] != float64(5) || out["applied"] != float64(1) || out["appliedSeq"] != float64(s.seq) {
		t.Fatalf("a batch with five bad events answered %v", out)
	}
	if !live(t, ds, cbChat, cbChatID(cbGroup)) || live(t, ds, cbChat, cbChatID(cbDirect)) {
		t.Fatal("the good event did not land, or a bad one did")
	}
	inst := cbInstanceRow(t, ds)
	if cbNum(inst.Properties["skippedEvents"]) != 5 || !strings.Contains(fmt.Sprint(inst.Properties["lastSkipped"]), "missing") {
		t.Fatalf("the instance after five skips: %v", inst.Properties)
	}

	// A batch of another version is refused whole, and so is one that is not
	// multipart.
	bad := cbBatch(s.ev("account.upsert", nil))
	bad["version"] = 2
	if _, _, err := cbTry(t, ds, cbRequest(t, ds, bad, nil), "2026-11-09T12:00:01Z"); err == nil ||
		!strings.Contains(err.Error(), "version") {
		t.Fatalf("a version 2 batch answered %v", err)
	}
	if _, _, err := cbTry(t, ds, map[string]any{
		"method": "POST", "contentType": "application/json",
		"body": map[string]any{"text": "{}"},
	}, "2026-11-09T12:00:02Z"); err == nil ||
		!strings.Contains(err.Error(), "multipart") {
		t.Fatalf("a JSON body answered %v", err)
	}
}

// Media rides the request as file parts: each part is a blob before the
// function runs, and the rows carry its digest; a re-upsert that names no
// file keeps the one stored, and a list of attachments replaces the set.
func TestChatBridgeMultipartMedia(t *testing.T) {
	ds := newChatBridge(t)
	s := &cbStream{}
	cbOpen(t, ds, s, cbDirect)
	files := []cbFile{
		{Part: "a", Filename: "voice.ogg", MediaType: "audio/ogg", Text: "not really opus"},
		{Part: "b", Filename: "doc.pdf", MediaType: "application/pdf", Text: "not really a pdf"},
	}
	cbDeliver(t, ds, cbBatch(s.message(cbDirect, "m1", map[string]any{"content": "voice", "attachments": []any{
		map[string]any{
			"part": "0", "content": "voice", "mimeType": "audio/ogg", "durationMs": 4200,
			"file": map[string]any{"part": "a"}, "mediaState": "stored",
		},
		map[string]any{
			"part": "1", "content": "file", "fileName": "doc.pdf",
			"file": map[string]any{"part": "b"}, "mediaState": "stored",
		},
	}})), files, "2026-11-09T12:00:00Z")
	voice := mustGet(t, ds, cbAttachment, cbAttachmentID(cbDirect, "m1", "0"))
	manifest, _ := voice.Properties["file"].(map[string]any)
	if manifest == nil || manifest["mediaType"] != "audio/ogg" || cbNum(voice.Properties["durationMs"]) != 4200 {
		t.Fatalf("the voice note: %v", voice.Properties)
	}
	// The first part again with no file named, the second gone from the list.
	cbDeliver(t, ds, cbBatch(s.message(cbDirect, "m1", map[string]any{"content": "voice", "attachments": []any{
		map[string]any{"part": "0", "content": "voice", "mimeType": "audio/ogg", "mediaState": "stored"},
	}})), nil, "2026-11-09T12:00:01Z")
	if got := mustGet(t, ds, cbAttachment, cbAttachmentID(cbDirect, "m1", "0")); got.Properties["file"] == nil {
		t.Fatal("an upsert naming no file dropped the stored one")
	}
	if live(t, ds, cbAttachment, cbAttachmentID(cbDirect, "m1", "1")) {
		t.Fatal("an attachment the list no longer names is still there")
	}
}

// A message deleted for everyone keeps its row, clears its text and files,
// and stays withdrawn against a later upsert; deleted by the owner on another
// device it keeps them, and a later delete for everyone still clears them.
func TestChatBridgeDeletedForEveryoneStaysWithdrawn(t *testing.T) {
	ds := newChatBridge(t)
	s := &cbStream{}
	cbOpen(t, ds, s, cbDirect)
	del := func(id, scope string) map[string]any {
		return s.ev("message.delete", map[string]any{
			"chat": cbDirect, "message": id, "scope": scope,
			"at": "2026-11-09T11:30:00Z",
		})
	}
	cbDeliver(t, ds, cbBatch(
		s.message(cbDirect, "m1", map[string]any{"text": "secret"}),
		s.message(cbDirect, "m2", map[string]any{"text": "kept"}),
		del("m1", "everyone"), del("m2", "owner"),
	), nil, "2026-11-09T12:00:00Z")
	cbDeliver(t, ds, cbBatch(
		s.message(cbDirect, "m1", map[string]any{"text": "secret again"}),
		s.message(cbDirect, "m2", map[string]any{"text": "kept, edited"}),
	), nil, "2026-11-09T12:00:01Z")
	m1 := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "m1"))
	if m1.Properties["deleted"] != "everyone" || m1.Properties["text"] != nil {
		t.Fatalf("a message deleted for everyone came back: %v", m1.Properties)
	}
	m2 := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "m2"))
	if m2.Properties["deleted"] != "owner" || m2.Properties["text"] != "kept, edited" {
		t.Fatalf("a message the owner deleted elsewhere: %v", m2.Properties)
	}
	cbDeliver(t, ds, cbBatch(del("m2", "everyone")), nil, "2026-11-09T12:00:02Z")
	if got := mustGet(t, ds, cbMessage, cbMessageID(cbDirect, "m2")); got.Properties["deleted"] != "everyone" ||
		got.Properties["text"] != nil {
		t.Fatalf("deleted for everyone after the owner's delete: %v", got.Properties)
	}
}

// The owner deletes an account: the account takes no more events, the
// engine collects everything it brought in with it, and only signing in
// again writes it back.
func TestChatBridgeDeletedAccountTakesNoMoreEvents(t *testing.T) {
	ds := newChatBridge(t)
	ctx := context.Background()
	s := &cbStream{}
	cbOpen(t, ds, s, cbDirect)
	if _, err := ds.Delete(ctx, substrate.ActorAPI, cbAccount, cbAccountID(), substrate.DeleteInput{}); err != nil {
		t.Fatalf("the owner deletes the account: %v", err)
	}
	out, _ := cbDeliver(t, ds, cbBatch(
		s.chat(cbDirect, map[string]any{"chatName": "late"}),
		s.ev("account.state", map[string]any{"state": "connected"}),
		s.ev("account.removed", nil),
	), nil, "2026-11-09T12:00:01Z")
	if out["skipped"] != float64(2) || live(t, ds, cbAccount, cbAccountID()) {
		t.Fatalf("events for a deleted account answered %v", out)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	if live(t, ds, cbChat, cbChatID(cbDirect)) {
		t.Fatal("the account's chat outlived the account's collection")
	}
	out, _ = cbDeliver(t, ds, cbBatch(s.chat(cbDirect, nil)), nil, "2026-11-09T12:00:02Z")
	if out["skipped"] != float64(1) {
		t.Fatalf("a chat of a collected account answered %v", out)
	}
	cbDeliver(t, ds, cbBatch(s.ev("account.upsert", map[string]any{"remoteName": "Ada again"})), nil, "2026-11-09T12:00:03Z")
	if got := mustGet(t, ds, cbAccount, cbAccountID()); got.Properties["remoteName"] != "Ada again" {
		t.Fatalf("signing in again: %v", got.Properties)
	}
}

// The sync trait's last run and the instance's last delivery move at most
// every five minutes; the connection's state reaches the sync state and the
// error the owner reads at once, and a later connect clears it.
func TestChatBridgeStampsTheSyncTrait(t *testing.T) {
	ds := newChatBridge(t)
	s := &cbStream{}
	cbDeliver(t, ds, cbBatch(s.ev("account.upsert", nil)), nil, "2026-11-09T12:00:00Z")
	cbDeliver(t, ds, cbBatch(s.ev("account.upsert", map[string]any{"handle": "ada"})), nil, "2026-11-09T12:04:00Z")
	acct := mustGet(t, ds, cbAccount, cbAccountID())
	if acct.Properties["lastSyncedAt"] != "2026-11-09T12:00:00Z" ||
		cbInstanceRow(t, ds).Properties["lastSeenAt"] != "2026-11-09T12:00:00Z" {
		t.Fatalf("a delivery four minutes later restamped: %v", acct.Properties["lastSyncedAt"])
	}
	cbDeliver(t, ds, cbBatch(s.ev("account.state", map[string]any{
		"state":   "bad_credentials",
		"message": "Logged out from the phone.",
	})), nil, "2026-11-09T12:06:00Z")
	acct = mustGet(t, ds, cbAccount, cbAccountID())
	if acct.Properties["lastSyncedAt"] != "2026-11-09T12:06:00Z" || acct.Properties["syncState"] != "erroring" ||
		acct.Properties["connectionState"] != "badcredentials" ||
		acct.Properties["syncError"] != "Logged out from the phone." || acct.Properties["syncErrorAt"] == nil {
		t.Fatalf("the account signed out: %v", acct.Properties)
	}
	cbDeliver(t, ds, cbBatch(s.ev("account.state", map[string]any{
		"state":    "transient_disconnect",
		"message":  "Waiting for the phone.",
		"warnings": []any{map[string]any{"code": "phone_offline", "message": "The phone has been offline for 12 days."}},
	})),
		nil, "2026-11-09T12:07:00Z")
	acct = mustGet(t, ds, cbAccount, cbAccountID())
	if acct.Properties["syncState"] != "throttled" || acct.Properties["syncError"] != nil ||
		acct.Properties["syncMessage"] != "The phone has been offline for 12 days." ||
		acct.Properties["connectionMessage"] != "Waiting for the phone." {
		t.Fatalf("the account down for a moment, with a warning: %v", acct.Properties)
	}
	cbDeliver(t, ds, cbBatch(s.ev("account.state", map[string]any{"state": "connected"})), nil, "2026-11-09T12:07:30Z")
	acct = mustGet(t, ds, cbAccount, cbAccountID())
	if acct.Properties["syncState"] != "ok" || acct.Properties["syncError"] != nil || acct.Properties["syncMessage"] != nil ||
		acct.Properties["connectionMessage"] != nil || acct.Properties["warnings"] != nil {
		t.Fatalf("the account connected again: %v", acct.Properties)
	}

	// The owner asks for a sync; history complete answers it.
	asked := "2026-11-09T12:08:00Z"
	if _, err := ds.Patch(context.Background(), substrate.ActorAPI, cbAccount, cbAccountID(), substrate.PatchInput{
		Properties: map[string]any{"syncRequestedAt": asked, "sendsPerHour": 10},
	}); err != nil {
		t.Fatalf("the owner's settings: %v", err)
	}
	cbDeliver(t, ds, cbBatch(s.ev("backfill.progress", map[string]any{"complete": true})), nil, "2026-11-09T12:09:00Z")
	if got := mustGet(t, ds, cbAccount, cbAccountID()); got.Properties["syncRequestedAck"] != asked ||
		cbNum(got.Properties["sendsPerHour"]) != 10 {
		t.Fatalf("history complete after a request: %v", got.Properties)
	}
}

// The six tools, called directly as the owner's console or an assistant does
// over the call door: each writes one queued action under a minted id and
// answers it, and refuses a request it can tell is wrong before writing.
func TestChatBridgeToolsWriteOneQueuedAction(t *testing.T) {
	ds := newChatBridge(t)
	ctx := context.Background()
	s := &cbStream{}
	cbOpen(t, ds, s, cbDirect, cbGroup)
	cbDeliver(t, ds, cbBatch(
		s.message(cbDirect, "theirs", map[string]any{"text": "hi"}),
		s.message(cbDirect, "mine", map[string]any{"sender": cbAda, "fromOwner": true, "text": "hey"}),
	), nil, "2026-11-09T12:00:00Z")
	direct := cbChatID(cbDirect)
	theirs, mine := cbMessageID(cbDirect, "theirs"), cbMessageID(cbDirect, "mine")
	for _, tc := range []struct {
		fn   string
		args map[string]any
		want map[string]any
	}{
		{
			"send",
			map[string]any{"chat": direct, "text": "on my way", "replyTo": theirs},
			map[string]any{"action": "send", "text": "on my way", "replyTo": theirs},
		},
		{
			"react",
			map[string]any{"message": cbPath(cbMessage, theirs), "reactionKey": "👍"},
			map[string]any{"action": "react", "target": theirs, "reactionKey": "👍"},
		},
		{
			"unreact",
			map[string]any{"message": theirs, "reactionKey": "👍"},
			map[string]any{"action": "unreact", "target": theirs},
		},
		{
			"edit",
			map[string]any{"message": mine, "text": "hey there"},
			map[string]any{"action": "edit", "target": mine, "text": "hey there"},
		},
		{
			"delete",
			map[string]any{"message": mine, "forEveryone": true},
			map[string]any{"action": "delete", "target": mine, "forEveryone": true},
		},
		{
			"markread",
			map[string]any{"chat": direct, "upTo": theirs},
			map[string]any{"action": "markread", "target": theirs},
		},
	} {
		out, _, err := ds.CallFunction(ctx, substrate.ActorAPI, chatBridgePkg+"/"+tc.fn, tc.args)
		if err != nil {
			t.Fatalf("%s: %v", tc.fn, err)
		}
		id, _ := out.(map[string]any)["action"].(string)
		if !vocabulary.MintedID(id) {
			t.Fatalf("%s answered %v, not a minted id", tc.fn, out)
		}
		row := mustGet(t, ds, cbAction, id)
		if row.Properties["state"] != "queued" || refIDs(row, "chat")[0] != direct {
			t.Fatalf("%s wrote %v", tc.fn, row.Properties)
		}
		for k, v := range tc.want {
			got := row.Properties[k]
			if ids := refIDs(row, k); len(ids) == 1 && ids[0] != "" {
				got = ids[0]
			}
			if got != v {
				t.Errorf("%s: %s = %v, want %v", tc.fn, k, got, v)
			}
		}
	}
	for _, tc := range []struct {
		fn   string
		args map[string]any
		want string
	}{
		{"edit", map[string]any{"message": theirs, "text": "not mine"}, "not sent by the owner"},
		{"send", map[string]any{"chat": cbChatID(cbGroup), "text": "x", "replyTo": theirs}, "is not in chat"},
		{"react", map[string]any{"message": cbMessageID(cbDirect, "nowhere"), "reactionKey": "👍"}, "is not a"},
	} {
		if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, chatBridgePkg+"/"+tc.fn, tc.args); err == nil ||
			!strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %v answered %v, want %q", tc.fn, tc.args, err, tc.want)
		}
	}
	// A retried call names the action it wrote the first time.
	key := substrate.WithIdempotencyKey(ctx, "retry-1")
	first, _, err := ds.CallFunction(key, substrate.ActorAPI, chatBridgePkg+"/send", map[string]any{"chat": direct, "text": "once"})
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := ds.CallFunction(key, substrate.ActorAPI, chatBridgePkg+"/send", map[string]any{"chat": direct, "text": "once"})
	if err != nil {
		t.Fatal(err)
	}
	if first.(map[string]any)["action"] != again.(map[string]any)["action"] {
		t.Fatalf("a retried send wrote a second action: %v, %v", first, again)
	}
}
