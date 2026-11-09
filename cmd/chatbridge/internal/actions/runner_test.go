package actions

import (
	"context"
	"sync"
	"testing"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/fakenet"
	"github.com/geoah/substrate/cmd/chatbridge/internal/localmatrix"
	"github.com/geoah/substrate/cmd/chatbridge/internal/networks"
	"github.com/geoah/substrate/cmd/chatbridge/internal/server"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// records is the server's records, in memory.
type records struct {
	mu sync.Mutex
	m  map[string]*server.Record
}

func (r *records) GetRecord(_ context.Context, kind, id string) (*server.Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.m[kind+"/"+id]
	if !ok {
		return nil, nil
	}
	cp := *rec
	cp.Properties = map[string]any{}
	for k, v := range rec.Properties {
		cp.Properties[k] = v
	}
	return &cp, nil
}

func (r *records) put(rec *server.Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[rec.Kind+"/"+rec.ID] = rec
}

func ref(kind, id string) map[string]any { return map[string]any{"ref": kind + "/" + id} }

// harness is the fake network, its store, the server's records and a
// runner over them.
type harness struct {
	t    *testing.T
	env  *fakenet.Env
	recs *records
	r    *Runner
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	settings := NewSettings()
	env := fakenet.Start(t, settings, nil)
	env.Conn.Contacts[fakenet.Other] = "Ada"
	h := &harness{t: t, env: env, recs: &records{m: map[string]*server.Record{}}}
	h.r = h.runner(settings)
	env.Run.Matrix.SetListener(h.r)
	h.recs.put(&server.Record{
		ID: "acct-1", Kind: server.KindAccount, Version: 1,
		Properties: map[string]any{"network": fakenet.Network, "loginId": fakenet.Login},
	})
	h.recs.put(&server.Record{
		ID: "chat-1", Kind: server.KindChat, Version: 1,
		Properties: map[string]any{"network": fakenet.Network, "chatId": fakenet.Chat, "account": ref(server.KindAccount, "acct-1")},
	})
	// The chat exists on the network: the other side wrote first.
	env.Receive(t, "200@lid:200@lid:AAA", fakenet.Other, "hi there", time.Now().Add(-time.Minute))
	h.recs.put(&server.Record{
		ID: "msg-1", Kind: server.KindMessage, Version: 1,
		Properties: map[string]any{
			"network": fakenet.Network, "messageId": "200@lid:200@lid:AAA",
			"chat": ref(server.KindChat, "chat-1"), "account": ref(server.KindAccount, "acct-1"),
		},
	})
	return h
}

func (h *harness) runner(settings *Settings) *Runner {
	run := h.env.Run
	return &Runner{
		Store:    h.env.Store,
		Records:  h.recs,
		Settings: settings,
		Recheck:  50 * time.Millisecond,
		Networks: map[string]*Network{fakenet.Network: {
			ID:    fakenet.Network,
			Exec:  run.Matrix,
			TxnID: networks.WhatsAppTxnID,
			Runs: func(account string) bool {
				return run.Bridge.GetCachedUserLoginByID(networkid.UserLoginID(account)) != nil
			},
		}},
	}
}

// action writes a queued chataction record.
func (h *harness) action(id string, props map[string]any) *server.Record {
	p := map[string]any{"chat": ref(server.KindChat, "chat-1"), "state": "queued"}
	for k, v := range props {
		p[k] = v
	}
	rec := &server.Record{ID: id, Kind: server.KindAction, Version: 1, CreatedAt: time.Now(), Properties: p}
	h.recs.put(rec)
	return rec
}

// run starts the runner and offers the actions.
func (h *harness) run(recs ...*server.Record) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = h.r.Run(ctx) }()
	h.t.Cleanup(cancel)
	for _, rec := range recs {
		h.r.Action(ctx, rec)
	}
	return cancel
}

// outcome waits for an action's outcome in the store.
func (h *harness) outcome(id string) *store.Claim {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := h.env.Store.GetClaim(context.Background(), id)
		if err != nil {
			h.t.Fatal(err)
		}
		if c != nil && c.Outcome != "" {
			return c
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("action %s has no outcome (claim %+v)", id, c)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// events reads the action events and their messages from the outbox.
func (h *harness) events(typ string) []map[string]any {
	var out []map[string]any
	for _, e := range h.env.Events(h.t, 0) {
		if e["type"] == typ {
			out = append(out, e)
		}
	}
	return out
}

func (h *harness) calls(kind string) []fakenet.Call {
	var out []fakenet.Call
	for _, c := range h.env.Conn.Calls() {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

func TestSendIsClaimedExecutedOnceAndEchoed(t *testing.T) {
	h := newHarness(t)
	h.run(h.action("act-1", map[string]any{"action": "send", "text": "see you at **7**"}))
	c := h.outcome("act-1")
	if c.Outcome != contract.OutcomeDone {
		t.Fatalf("outcome %+v", c)
	}
	calls := h.calls("send")
	if len(calls) != 1 {
		t.Fatalf("the network got %d sends, want 1", len(calls))
	}
	txn := networks.WhatsAppTxnID("act-1")
	if calls[0].Txn != txn || calls[0].ID != txn {
		t.Fatalf("the network message id is %q (txn %q), want the action's %q", calls[0].ID, calls[0].Txn, txn)
	}
	claimed, results := h.events(contract.TypeActionClaimed), h.events(contract.TypeActionResult)
	if len(claimed) != 1 || len(results) != 1 {
		t.Fatalf("claimed %d results %d", len(claimed), len(results))
	}
	if claimed[0]["attempt"] != results[0]["attempt"] || claimed[0]["version"] != float64(1) {
		t.Fatalf("claim %v result %v", claimed[0], results[0])
	}
	if results[0]["outcome"] != contract.OutcomeDone || results[0]["message"] != c.Message || c.Message == "" {
		t.Fatalf("result %v, claim message %q", results[0], c.Message)
	}
	var echo map[string]any
	for _, e := range h.events(contract.TypeMessageUpsert) {
		if m := e["message"].(map[string]any); m["action"] == "act-1" {
			echo = m
		}
	}
	if echo == nil || echo["id"] != c.Message || echo["fromOwner"] != true || echo["text"] != "see you at **7**" {
		t.Fatalf("echo %v", echo)
	}
	if claimed[0]["seq"].(float64) >= results[0]["seq"].(float64) {
		t.Fatal("the claim was not written before the result")
	}
}

func TestCrashBeforeTheNetworkCallRunsTheClaimOnce(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	rec := h.action("act-1", map[string]any{"action": "send", "text": "hello"})
	req, err := h.r.build(ctx, rec, target{network: fakenet.Network, account: fakenet.Login, chat: fakenet.Chat}, h.r.Networks[fakenet.Network])
	if err != nil {
		t.Fatal(err)
	}
	h.r.init()
	if _, won, err := h.r.claim(ctx, rec, req, target{network: fakenet.Network, account: fakenet.Login, chat: fakenet.Chat}); err != nil || !won {
		t.Fatalf("claim: %v %v", won, err)
	}
	// The process stops here: the claim is durable, nothing left.
	fresh := h.runner(h.r.Settings)
	if err := fresh.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if c := h.outcome("act-1"); c.Outcome != contract.OutcomeDone {
		t.Fatalf("outcome %+v", c)
	}
	if n := len(h.calls("send")); n != 1 {
		t.Fatalf("the network got %d sends, want 1", n)
	}
	// A second restart finds nothing open.
	if err := h.runner(h.r.Settings).Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(h.calls("send")); n != 1 || len(h.events(contract.TypeActionResult)) != 1 {
		t.Fatalf("a settled claim ran again: %d sends", n)
	}
}

func TestCrashAfterTheNetworkAcceptedIsDoneWithoutAResend(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	rec := h.action("act-1", map[string]any{"action": "send", "text": "hello"})
	tg := target{network: fakenet.Network, account: fakenet.Login, chat: fakenet.Chat}
	net := h.r.Networks[fakenet.Network]
	req, err := h.r.build(ctx, rec, tg, net)
	if err != nil {
		t.Fatal(err)
	}
	h.r.init()
	c, _, err := h.r.claim(ctx, rec, req, tg)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.env.Store.Dispatched(ctx, c.Action); err != nil {
		t.Fatal(err)
	}
	// The network takes it; the process stops before the outcome is kept.
	if out := net.Exec.Execute(ctx, req); out.Outcome != contract.OutcomeDone {
		t.Fatalf("execute %+v", out)
	}
	if err := h.runner(h.r.Settings).Recover(ctx); err != nil {
		t.Fatal(err)
	}
	got := h.outcome("act-1")
	if got.Outcome != contract.OutcomeDone || got.Message == "" {
		t.Fatalf("outcome %+v", got)
	}
	if n := len(h.calls("send")); n != 1 {
		t.Fatalf("the network got %d sends, want 1", n)
	}
}

func TestCrashAfterLeavingWithNothingToFindIsUncertain(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	rec := h.action("act-1", map[string]any{"action": "send", "text": "hello"})
	tg := target{network: fakenet.Network, account: fakenet.Login, chat: fakenet.Chat}
	req, _ := h.r.build(ctx, rec, tg, h.r.Networks[fakenet.Network])
	h.r.init()
	c, _, err := h.r.claim(ctx, rec, req, tg)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.env.Store.Dispatched(ctx, c.Action); err != nil {
		t.Fatal(err)
	}
	if err := h.runner(h.r.Settings).Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.outcome("act-1"); got.Outcome != contract.OutcomeUncertain {
		t.Fatalf("outcome %+v", got)
	}
	if n := len(h.calls("send")); n != 0 {
		t.Fatalf("an uncertain action was sent %d times", n)
	}
}

func TestDisconnectMidSendIsUncertainAndNeverResent(t *testing.T) {
	h := newHarness(t)
	h.env.Conn.Before = func(_ context.Context, c fakenet.Call) error {
		if c.Kind == "send" {
			return bridgev2.ErrRemoteAckTimeout
		}
		return nil
	}
	rec := h.action("act-1", map[string]any{"action": "send", "text": "hello"})
	h.run(rec)
	if got := h.outcome("act-1"); got.Outcome != contract.OutcomeUncertain {
		t.Fatalf("outcome %+v", got)
	}
	// Offered again (a relist), the action is not sent again.
	h.r.Action(context.Background(), rec)
	time.Sleep(200 * time.Millisecond)
	if n := len(h.events(contract.TypeActionClaimed)); n != 1 {
		t.Fatalf("%d claims, want 1", n)
	}
}

func TestDelayedResultReportsTheClaimedVersion(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	h.env.Conn.Before = func(ctx context.Context, c fakenet.Call) error {
		if c.Kind != "send" {
			return nil
		}
		entered <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	rec := h.action("act-1", map[string]any{"action": "send", "text": "first text"})
	h.run(rec)
	<-entered
	// The claim and its event are out before the network answered.
	c, err := h.env.Store.GetClaim(context.Background(), "act-1")
	if err != nil || c == nil || c.DispatchedAt == nil || c.Outcome != "" {
		t.Fatalf("claim during the send: %+v %v", c, err)
	}
	if n := len(h.events(contract.TypeActionClaimed)); n != 1 {
		t.Fatalf("%d claims during the send", n)
	}
	// The owner edits the record meanwhile; the attempt does not change.
	edited := h.action("act-1", map[string]any{"action": "send", "text": "second text"})
	edited.Version = 2
	h.recs.put(edited)
	h.r.Action(context.Background(), edited)
	close(release)
	got := h.outcome("act-1")
	if got.Outcome != contract.OutcomeDone || got.Version != 1 {
		t.Fatalf("outcome %+v", got)
	}
	calls := h.calls("send")
	if len(calls) != 1 || calls[0].Text != "first text" {
		t.Fatalf("calls %+v", calls)
	}
}

func TestTwoWorkersRacingForOneActionSendOnce(t *testing.T) {
	h := newHarness(t)
	other := h.runner(h.r.Settings)
	rec := h.action("act-1", map[string]any{"action": "send", "text": "hello"})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = h.r.Run(ctx) }()
	go func() { _ = other.Run(ctx) }()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(2)
		go func() { defer wg.Done(); h.r.Action(ctx, rec) }()
		go func() { defer wg.Done(); other.Action(ctx, rec) }()
	}
	wg.Wait()
	if got := h.outcome("act-1"); got.Outcome != contract.OutcomeDone {
		t.Fatalf("outcome %+v", got)
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(h.calls("send")); n != 1 {
		t.Fatalf("the network got %d sends, want 1", n)
	}
	if c, r := len(h.events(contract.TypeActionClaimed)), len(h.events(contract.TypeActionResult)); c != 1 || r != 1 {
		t.Fatalf("claimed %d, results %d", c, r)
	}
}

func TestAStaleReportChangesNothing(t *testing.T) {
	h := newHarness(t)
	h.run(h.action("act-1", map[string]any{"action": "send", "text": "hello"}))
	c := h.outcome("act-1")
	late := localmatrix.Outcome{Outcome: contract.OutcomeFailed, ErrorCode: contract.ErrNetwork, ErrorText: "late"}
	if err := h.r.report(context.Background(), c, localmatrix.Request{}, late, localmatrix.Request{}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.env.Store.GetClaim(context.Background(), "act-1"); got.Outcome != contract.OutcomeDone {
		t.Fatalf("a late report moved the outcome to %q", got.Outcome)
	}
	if n := len(h.events(contract.TypeActionResult)); n != 1 {
		t.Fatalf("%d results", n)
	}
}

func TestTheOtherFiveActions(t *testing.T) {
	h := newHarness(t)
	target := ref(server.KindMessage, "msg-1")
	h.run(h.action("act-react", map[string]any{"action": "react", "target": target, "reactionKey": "👍"}))
	if c := h.outcome("act-react"); c.Outcome != contract.OutcomeDone {
		t.Fatalf("react %+v", c)
	}
	h.r.Action(context.Background(), h.action("act-unreact", map[string]any{"action": "unreact", "target": target, "reactionKey": "👍"}))
	if c := h.outcome("act-unreact"); c.Outcome != contract.OutcomeDone {
		t.Fatalf("unreact %+v", c)
	}
	h.r.Action(context.Background(), h.action("act-read", map[string]any{"action": "markread", "target": target}))
	if c := h.outcome("act-read"); c.Outcome != contract.OutcomeDone {
		t.Fatalf("markread %+v", c)
	}
	// An edit and a delete need the owner's own message.
	h.r.Action(context.Background(), h.action("act-send", map[string]any{"action": "send", "text": "draft"}))
	sent := h.outcome("act-send")
	h.recs.put(&server.Record{ID: "msg-own", Kind: server.KindMessage, Version: 1, Properties: map[string]any{
		"network": fakenet.Network, "messageId": sent.Message, "chat": ref(server.KindChat, "chat-1"), "account": ref(server.KindAccount, "acct-1"),
	}})
	own := ref(server.KindMessage, "msg-own")
	h.r.Action(context.Background(), h.action("act-edit", map[string]any{"action": "edit", "target": own, "text": "final"}))
	if c := h.outcome("act-edit"); c.Outcome != contract.OutcomeDone {
		t.Fatalf("edit %+v", c)
	}
	h.r.Action(context.Background(), h.action("act-delete", map[string]any{"action": "delete", "target": own, "forEveryone": true}))
	if c := h.outcome("act-delete"); c.Outcome != contract.OutcomeDone {
		t.Fatalf("delete %+v", c)
	}
	for _, kind := range []string{"react", "unreact", "markread", "send", "edit", "delete"} {
		if n := len(h.calls(kind)); n != 1 {
			t.Fatalf("the network got %d %s calls, want 1", n, kind)
		}
	}
	var sawReactions, sawRead, sawDelete, sawEdit bool
	for _, e := range h.env.Events(t, 0) {
		switch e["type"] {
		case contract.TypeMessageReactions:
			sawReactions = true
		case contract.TypeChatRead:
			sawRead = e["upTo"] == "200@lid:200@lid:AAA"
		case contract.TypeMessageDelete:
			sawDelete = e["message"] == sent.Message
		case contract.TypeMessageUpsert:
			if m := e["message"].(map[string]any); m["id"] == sent.Message && m["text"] == "final" {
				sawEdit = true
			}
		}
	}
	if !sawReactions || !sawRead || !sawDelete || !sawEdit {
		t.Fatalf("echoes: reactions %v read %v delete %v edit %v", sawReactions, sawRead, sawDelete, sawEdit)
	}
}

func TestTheRulesRefuseBeforeTheNetwork(t *testing.T) {
	h := newHarness(t)
	h.r.Settings.Put(fakenet.Network, fakenet.Login, AccountSettings{SendsPerHour: 2})
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	h.run()
	cases := []struct {
		id    string
		props map[string]any
		want  string
		code  string
	}{
		{"cancelled", map[string]any{"action": "send", "text": "x", "cancelRequestedAt": past}, contract.OutcomeCancelled, contract.ErrCancelled},
		{"expired", map[string]any{"action": "send", "text": "x", "notAfter": past}, contract.OutcomeExpired, contract.ErrExpired},
		{"empty", map[string]any{"action": "send", "text": "  "}, contract.OutcomeFailed, contract.ErrInvalid},
		{"first", map[string]any{"action": "send", "text": "one"}, contract.OutcomeDone, ""},
		{"same-text", map[string]any{"action": "send", "text": "one"}, contract.OutcomeFailed, contract.ErrRateLimited},
		{"second", map[string]any{"action": "send", "text": "two"}, contract.OutcomeDone, ""},
		{"over-the-hour", map[string]any{"action": "send", "text": "three"}, contract.OutcomeFailed, contract.ErrRateLimited},
	}
	for _, c := range cases {
		h.r.Action(context.Background(), h.action(c.id, c.props))
		got := h.outcome(c.id)
		if got.Outcome != c.want || got.ErrorCode != c.code {
			t.Fatalf("%s: outcome %q %q, want %q %q (%s)", c.id, got.Outcome, got.ErrorCode, c.want, c.code, got.ErrorMessage)
		}
	}
	if n := len(h.calls("send")); n != 2 {
		t.Fatalf("the network got %d sends, want 2", n)
	}
	// A refusal is a result with its own attempt and no claim before it.
	claimed := map[string]bool{}
	for _, e := range h.events(contract.TypeActionClaimed) {
		claimed[e["action"].(string)] = true
	}
	if len(claimed) != 2 || !claimed["first"] || !claimed["second"] {
		t.Fatalf("claimed %v, want only the two sent", claimed)
	}
	for _, e := range h.events(contract.TypeActionResult) {
		if !claimed[e["action"].(string)] && (e["attempt"] == "" || e["error"] == nil) {
			t.Fatalf("an unclaimed result lacks its attempt or error: %v", e)
		}
	}
}

func TestAStrangerNeedsTheOwnersAllowance(t *testing.T) {
	h := newHarness(t)
	delete(h.env.Conn.Contacts, fakenet.Other)
	h.run(h.action("refused", map[string]any{"action": "send", "text": "hello"}))
	if got := h.outcome("refused"); got.Outcome != contract.OutcomeFailed || got.ErrorCode != contract.ErrBlocked || got.Claimed {
		t.Fatalf("outcome %+v", got)
	}
	h.r.Action(context.Background(), h.action("allowed", map[string]any{"action": "send", "text": "hello", "allowNewChat": true}))
	if got := h.outcome("allowed"); got.Outcome != contract.OutcomeDone {
		t.Fatalf("outcome %+v", got)
	}
}

func (h *harness) state(state string) {
	h.t.Helper()
	h.env.Login.BridgeState.Send(bridgeState(state))
	want := map[string]string{"BAD_CREDENTIALS": contract.StateBadCredentials, "TRANSIENT_DISCONNECT": contract.StateTransientDisconnect}[state]
	deadline := time.Now().Add(5 * time.Second)
	for {
		if st, _ := h.env.Run.Matrix.AccountStatusOf(fakenet.Login); st.State == want {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("the state %s never arrived", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAnAccountThatIsNotComingBackRefuses(t *testing.T) {
	h := newHarness(t)
	h.state("BAD_CREDENTIALS")
	h.run(h.action("refused", map[string]any{"action": "send", "text": "hello"}))
	got := h.outcome("refused")
	if got.Outcome != contract.OutcomeFailed || got.ErrorCode != contract.ErrNetwork || got.Claimed {
		t.Fatalf("outcome %+v", got)
	}
	if n := len(h.events(contract.TypeActionClaimed)); n != 0 {
		t.Fatalf("%d claims for a refused action", n)
	}
}

func TestAnAccountComingBackHoldsItsActions(t *testing.T) {
	h := newHarness(t)
	h.state("TRANSIENT_DISCONNECT")
	h.run(h.action("held", map[string]any{"action": "send", "text": "hello"}))
	time.Sleep(300 * time.Millisecond)
	if c, _ := h.env.Store.GetClaim(context.Background(), "held"); c != nil {
		t.Fatalf("an action on a disconnected account ended: %+v", c)
	}
	h.env.Login.Client.Connect(context.Background())
	h.env.WaitConnected(t)
	if got := h.outcome("held"); got.Outcome != contract.OutcomeDone {
		t.Fatalf("outcome after reconnecting %+v", got)
	}
}

func TestOnlyQueuedRowsAreActedOn(t *testing.T) {
	h := newHarness(t)
	sending := h.action("sending", map[string]any{"action": "send", "text": "x", "state": "sending"})
	unset := h.action("unset", map[string]any{"action": "send", "text": "y"})
	delete(unset.Properties, "state")
	h.recs.put(unset)
	h.run(sending, unset)
	time.Sleep(300 * time.Millisecond)
	for _, id := range []string{"sending", "unset"} {
		if c, _ := h.env.Store.GetClaim(context.Background(), id); c != nil {
			t.Fatalf("a row not queued was acted on: %+v", c)
		}
	}
	if n := len(h.env.Conn.Calls()); n != 0 {
		t.Fatalf("%d network calls", n)
	}
}

func TestAnotherInstancesAccountIsLeftAlone(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.env.Store.Set(ctx, store.KeyInstanceID, "inst-ours"); err != nil {
		t.Fatal(err)
	}
	acct, _ := h.recs.GetRecord(ctx, server.KindAccount, "acct-1")
	acct.Properties["instance"] = ref(server.KindInstance, "inst-theirs")
	h.recs.put(acct)
	h.run(h.action("theirs", map[string]any{"action": "send", "text": "x"}))
	time.Sleep(300 * time.Millisecond)
	if c, _ := h.env.Store.GetClaim(ctx, "theirs"); c != nil {
		t.Fatalf("another instance's action was reported: %+v", c)
	}
}

func TestUnlinkLogsOutAndForgetsOnlyADeletedAccount(t *testing.T) {
	h := newHarness(t)
	type unlink struct {
		account string
		forget  bool
	}
	var got []unlink
	h.r.Networks[fakenet.Network].Unlink = func(_ context.Context, account string, forget bool) error {
		got = append(got, unlink{account, forget})
		return nil
	}
	ctx := context.Background()
	acct, _ := h.recs.GetRecord(ctx, server.KindAccount, "acct-1")
	acct.Properties["unlinkRequestedAt"] = "2026-11-09T10:00:00Z"
	h.r.Account(ctx, acct)
	h.r.Account(ctx, acct)
	h.r.AccountDeleted(ctx, fakenet.Network, fakenet.Login)
	if len(got) != 2 || got[0] != (unlink{fakenet.Login, false}) || got[1] != (unlink{fakenet.Login, true}) {
		t.Fatalf("unlinks %+v, want one kept and one forgotten", got)
	}
}

func bridgeState(state string) status.BridgeState {
	return status.BridgeState{StateEvent: status.BridgeStateEvent(state)}
}
