// Package actions carries out what the owner asks for in a chat: each
// chataction record is claimed durably before anything leaves for the
// network, executed once under the action's own transaction id, and
// reported (docs/plans/chat-bridge.md, section 6.5). It also holds the rules
// that keep the bridge behaving as the owner rather than as a bot
// (section 9): pace, limits, strangers, and stopping on a warning.
package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/id"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/localmatrix"
	"github.com/geoah/substrate/cmd/chatbridge/internal/server"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// Executor is one network's way to carry an action out: the local Matrix
// side.
type Executor interface {
	Inspect(ctx context.Context, account, chat string) (localmatrix.ChatInfo, error)
	MessageAge(ctx context.Context, account, messageID string) (time.Time, bool, error)
	Execute(ctx context.Context, req localmatrix.Request) localmatrix.Outcome
	FindSent(ctx context.Context, req localmatrix.Request) (localmatrix.Outcome, bool)
	Echo(ctx context.Context, tx *store.Tx, req localmatrix.Request, out localmatrix.Outcome) error
	AccountStatusOf(account string) (localmatrix.AccountStatus, bool)
}

// Network is one network the runner acts on.
type Network struct {
	ID   string
	Exec Executor
	// TxnID is an action's transaction id in the network's format.
	TxnID func(action string) string
	// Runs reports whether this instance runs a login.
	Runs func(account string) bool
	// Unlink logs a login out of the network, forgets it and reports
	// account.removed; forget also forgets what was sent for it, for an
	// account whose row the owner deleted.
	Unlink func(ctx context.Context, account string, forget bool) error
	// Pause disconnects a login while the owner paused its sync, and
	// connects it again after.
	Pause func(ctx context.Context, account string, paused bool)
}

// Records reads records from the server.
type Records interface {
	GetRecord(ctx context.Context, kind, id string) (*server.Record, error)
}

// Metrics count what the runner did.
type Metrics struct {
	Claimed   atomic.Int64
	Done      atomic.Int64
	Failed    atomic.Int64
	Uncertain atomic.Int64
	Expired   atomic.Int64
	Cancelled atomic.Int64
}

// Runner is the action runner: one worker per account, so one account's
// actions run in order.
type Runner struct {
	Store    *store.Store
	Records  Records
	Networks map[string]*Network
	Settings *Settings
	Log      *slog.Logger
	Metrics  *Metrics
	// Now is the clock; tests move it.
	Now func() time.Time
	// TTL is how long a send or an edit stays sendable when the request
	// names no notAfter.
	TTL time.Duration
	// ChatGap is the least time between two sends in one chat.
	ChatGap time.Duration
	// Recheck is how often a held action is looked at again when nothing
	// else wakes it.
	Recheck time.Duration
	// ConnectGrace is how long after its creation an action waits for an
	// account that is connecting or briefly disconnected.
	ConnectGrace time.Duration

	ctx     context.Context
	offers  chan offer
	mu      sync.Mutex
	workers map[string]*worker
	timers  map[string]*time.Timer
	held    map[string]string
	once    sync.Once
}

type offer struct {
	rec     *server.Record
	deleted string
}

// target is where an action runs.
type target struct {
	network string
	account string
	chat    string
	// instance is the record id of the instance row the account names.
	instance string
}

func (t target) key() string { return t.network + "\x00" + t.account }

type worker struct {
	t       target
	queue   chan string
	mu      sync.Mutex
	pending map[string]bool
}

func (r *Runner) init() {
	r.once.Do(func() {
		if r.Log == nil {
			r.Log = slog.Default()
		}
		if r.Metrics == nil {
			r.Metrics = &Metrics{}
		}
		if r.Now == nil {
			r.Now = time.Now
		}
		if r.TTL <= 0 {
			r.TTL = time.Hour
		}
		if r.ChatGap <= 0 {
			r.ChatGap = 3 * time.Second
		}
		if r.Recheck <= 0 {
			r.Recheck = time.Minute
		}
		if r.ConnectGrace <= 0 {
			r.ConnectGrace = 5 * time.Minute
		}
		if r.Settings == nil {
			r.Settings = NewSettings()
		}
		r.offers = make(chan offer, 1024)
		r.workers = map[string]*worker{}
		r.timers = map[string]*time.Timer{}
		r.held = map[string]string{}
	})
}

// Run routes offered actions to their workers until ctx ends. Recover runs
// before it.
func (r *Runner) Run(ctx context.Context) error {
	r.init()
	r.mu.Lock()
	r.ctx = ctx
	r.mu.Unlock()
	tick := time.NewTicker(r.Recheck)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			r.stopTimers()
			return ctx.Err()
		case o := <-r.offers:
			if o.deleted != "" {
				r.drop(o.deleted)
				continue
			}
			r.route(ctx, o.rec)
		case <-tick.C:
			r.recheckHeld()
		}
	}
}

func (r *Runner) stopTimers() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, t := range r.timers {
		t.Stop()
		delete(r.timers, id)
	}
}

// Action implements server.Sink: a chataction as it stands.
func (r *Runner) Action(_ context.Context, rec *server.Record) {
	r.init()
	select {
	case r.offers <- offer{rec: rec}:
	default:
		r.Log.Warn("the action queue is full; the action waits for the next listing", "action", rec.ID)
	}
}

// ActionDeleted implements server.Sink.
func (r *Runner) ActionDeleted(_ context.Context, id string) {
	r.init()
	select {
	case r.offers <- offer{deleted: id}:
	default:
	}
}

func (r *Runner) drop(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.timers[id]; ok {
		t.Stop()
		delete(r.timers, id)
	}
	delete(r.held, id)
}

// route resolves which account an action is for and hands it to that
// account's worker. An action for a login this instance does not run is
// another instance's.
func (r *Runner) route(ctx context.Context, rec *server.Record) {
	if !queued(rec) {
		return
	}
	if c, err := r.Store.GetClaim(ctx, rec.ID); err != nil || c != nil {
		return
	}
	t, err := r.resolveChat(ctx, rec)
	if err != nil {
		r.Log.Warn("resolving an action's chat failed", "action", rec.ID, "error", err)
		r.hold(rec.ID, "", r.Now().Add(r.Recheck))
		return
	}
	if t == nil || r.foreign(ctx, *t) {
		return
	}
	r.enqueue(ctx, *t, rec.ID)
}

// queued reports a row the runner may act on: only one in queued.
func queued(rec *server.Record) bool { return rec.String("state") == "queued" }

// foreign reports an account another instance runs: its row names an
// instance that is not this one. Instances trust each other, and act only on
// the accounts they run.
func (r *Runner) foreign(ctx context.Context, t target) bool {
	ours, _, _ := r.Store.Get(ctx, store.KeyInstanceID)
	return ours != "" && t.instance != "" && t.instance != ours
}

func (r *Runner) enqueue(ctx context.Context, t target, id string) {
	r.mu.Lock()
	w, ok := r.workers[t.key()]
	if !ok {
		w = &worker{t: target{network: t.network, account: t.account}, queue: make(chan string, 256), pending: map[string]bool{}}
		r.workers[t.key()] = w
		go r.work(ctx, w)
	}
	r.mu.Unlock()
	w.mu.Lock()
	if w.pending[id] {
		w.mu.Unlock()
		return
	}
	w.pending[id] = true
	w.mu.Unlock()
	select {
	case w.queue <- id:
	default:
		w.mu.Lock()
		delete(w.pending, id)
		w.mu.Unlock()
		r.hold(id, t.key(), r.Now().Add(r.Recheck))
	}
}

// work runs one account's actions in order.
func (r *Runner) work(ctx context.Context, w *worker) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-w.queue:
			w.mu.Lock()
			delete(w.pending, id)
			w.mu.Unlock()
			if err := r.process(ctx, w.t, id); err != nil && ctx.Err() == nil {
				r.Log.Error("processing an action failed", "action", id, "error", err)
				r.hold(id, w.t.key(), r.Now().Add(r.Recheck))
			}
		}
	}
}

// hold parks an action until at, or until something wakes it.
func (r *Runner) hold(id, workerKey string, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx == nil {
		return
	}
	r.held[id] = workerKey
	if t, ok := r.timers[id]; ok {
		t.Stop()
	}
	d := max(at.Sub(r.Now()), 0)
	r.timers[id] = time.AfterFunc(d, func() { r.wake(id) })
}

// wake reads an held action again.
func (r *Runner) wake(id string) {
	r.mu.Lock()
	delete(r.timers, id)
	delete(r.held, id)
	ctx := r.ctx
	r.mu.Unlock()
	if ctx == nil || ctx.Err() != nil {
		return
	}
	rec, err := r.Records.GetRecord(ctx, server.KindAction, id)
	if err != nil {
		r.hold(id, "", r.Now().Add(r.Recheck))
		return
	}
	if rec == nil || rec.DeletedAt != nil {
		return
	}
	r.Action(ctx, rec)
}

// recheckHeld wakes every held action whose account may have changed.
func (r *Runner) recheckHeld() {
	r.mu.Lock()
	ids := make([]string, 0, len(r.held))
	for id := range r.held {
		if _, timed := r.timers[id]; !timed {
			ids = append(ids, id)
		}
	}
	r.mu.Unlock()
	for _, id := range ids {
		go r.wake(id)
	}
}

// AccountChanged wakes the held actions of an account whose state or
// settings changed.
func (r *Runner) AccountChanged(network, account string) {
	r.init()
	k := key(network, account)
	r.mu.Lock()
	var ids []string
	for id, wk := range r.held {
		if wk == k {
			ids = append(ids, id)
		}
	}
	r.mu.Unlock()
	for _, id := range ids {
		go r.wake(id)
	}
}

// AccountState implements localmatrix.StatusListener.
func (r *Runner) AccountState(network, account string, _ localmatrix.AccountStatus) {
	r.AccountChanged(network, account)
}

// MessageStatus implements localmatrix.StatusListener. Outcomes are read
// from bridgev2's synchronous result, so the asynchronous statuses carry
// nothing the runner needs.
func (r *Runner) MessageStatus(string, id.EventID, *bridgev2.MessageStatus, *bridgev2.MessageStatusEventInfo) {
}

// resolveChat reads which network, login and chat an action's chat record
// is.
func (r *Runner) resolveChat(ctx context.Context, rec *server.Record) (*target, error) {
	kind, chatID, ok := rec.Ref("chat")
	if !ok || kind != server.KindChat {
		return nil, nil
	}
	ref, err := r.Store.GetRef(ctx, store.RefChat, chatID)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		chat, err := r.Records.GetRecord(ctx, server.KindChat, chatID)
		if err != nil || chat == nil {
			return nil, err
		}
		login, err := r.accountLogin(ctx, chat)
		if err != nil || login == "" {
			return nil, err
		}
		ref = &store.Ref{Network: chat.String("network"), LoginID: login, ChatID: chat.String("chatId")}
		if ref.Network == "" || ref.ChatID == "" {
			return nil, nil
		}
		if err := r.Store.PutRef(ctx, store.RefChat, chatID, *ref); err != nil {
			return nil, err
		}
	}
	t := &target{network: ref.Network, account: ref.LoginID, chat: ref.ChatID}
	if a, err := r.Store.AccountByLogin(ctx, ref.Network, ref.LoginID); err == nil && a != nil {
		t.instance = a.Instance
	}
	return t, nil
}

// accountLogin reads the login a chat or message record's account names.
func (r *Runner) accountLogin(ctx context.Context, rec *server.Record) (string, error) {
	_, accountID, ok := rec.Ref("account")
	if !ok {
		return "", nil
	}
	if a, err := r.Store.AccountByRecord(ctx, accountID); err != nil || a != nil {
		if a == nil {
			return "", err
		}
		return a.LoginID, nil
	}
	acct, err := r.Records.GetRecord(ctx, server.KindAccount, accountID)
	if err != nil || acct == nil {
		return "", err
	}
	login := acct.String("loginId")
	if login != "" {
		_, instance, _ := acct.Ref("instance")
		_ = r.Store.PutAccountRef(ctx, store.AccountRef{
			RecordID: acct.ID, Network: acct.String("network"), LoginID: login,
			Settings: settingsJSON(settingsOf(acct)), Version: acct.Version, Instance: instance,
		})
	}
	return login, nil
}

// resolveMessage reads the network message id a message reference names,
// checking it is in the action's chat.
func (r *Runner) resolveMessage(ctx context.Context, rec *server.Record, prop string, t target) (string, bool, error) {
	kind, msgID, ok := rec.Ref(prop)
	if !ok {
		return "", false, nil
	}
	if kind != server.KindMessage {
		return "", true, errInvalid(prop + " does not name a message")
	}
	ref, err := r.Store.GetRef(ctx, store.RefMessage, msgID)
	if err != nil {
		return "", true, err
	}
	if ref == nil {
		msg, err := r.Records.GetRecord(ctx, server.KindMessage, msgID)
		if err != nil {
			return "", true, err
		}
		if msg == nil {
			return "", true, errNotFound("the message " + prop + " names does not exist")
		}
		login, err := r.accountLogin(ctx, msg)
		if err != nil {
			return "", true, err
		}
		_, chatRecord, _ := msg.Ref("chat")
		chatRef, err := r.Store.GetRef(ctx, store.RefChat, chatRecord)
		if err != nil {
			return "", true, err
		}
		chatID := ""
		if chatRef != nil {
			chatID = chatRef.ChatID
		}
		ref = &store.Ref{Network: msg.String("network"), LoginID: login, ChatID: chatID, MessageID: msg.String("messageId")}
		if ref.MessageID == "" {
			return "", true, errNotFound("the message " + prop + " names has no network id")
		}
		if err := r.Store.PutRef(ctx, store.RefMessage, msgID, *ref); err != nil {
			return "", true, err
		}
	}
	if ref.Network != t.network || ref.LoginID != t.account || (ref.ChatID != "" && ref.ChatID != t.chat) {
		return "", true, errInvalid("the message " + prop + " names is in another chat")
	}
	return ref.MessageID, true, nil
}

// refusal is a check that fails an action, with its error code.
type refusal struct {
	code string
	text string
}

func (e *refusal) Error() string { return e.text }

func errInvalid(text string) error  { return &refusal{code: contract.ErrInvalid, text: text} }
func errNotFound(text string) error { return &refusal{code: contract.ErrNotFound, text: text} }

// hold is a check that holds an action until a time or until its account
// changes.
type holdUntil struct {
	at     time.Time
	reason string
}

func (h *holdUntil) Error() string { return h.reason }

// requestDigest is the digest of the request properties a claim executes.
func requestDigest(rec *server.Record) string {
	req := map[string]any{}
	for _, p := range []string{"action", "chat", "target", "text", "replyTo", "reactionKey", "forEveryone", "notBefore", "notAfter", "allowNewChat"} {
		if v, ok := rec.Properties[p]; ok {
			req[p] = v
		}
	}
	b, _ := json.Marshal(req)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func settingsJSON(s AccountSettings) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// process reads an action fresh and takes it as far as it can go now.
func (r *Runner) process(ctx context.Context, t target, id string) error {
	rec, err := r.Records.GetRecord(ctx, server.KindAction, id)
	if err != nil {
		return err
	}
	if rec == nil || rec.DeletedAt != nil || !queued(rec) {
		return nil
	}
	if c, err := r.Store.GetClaim(ctx, id); err != nil || c != nil {
		return err
	}
	ct, err := r.resolveChat(ctx, rec)
	if err != nil {
		return err
	}
	if ct == nil || r.foreign(ctx, *ct) {
		return nil
	}
	t.chat, t.instance = ct.chat, ct.instance
	net := r.Networks[t.network]
	now := r.Now()

	// Before a claim, an action may still end as cancelled or expired, or
	// wait.
	if _, ok := rec.Time("cancelRequestedAt"); ok {
		return r.finishUnclaimed(ctx, rec, contract.OutcomeCancelled, contract.ErrCancelled, "the owner cancelled the action before it was sent")
	}
	kind := rec.String("action")
	notAfter, hasNotAfter := rec.Time("notAfter")
	if !hasNotAfter && (kind == localmatrix.ActionSend || kind == localmatrix.ActionEdit) {
		notAfter, hasNotAfter = rec.CreatedAt.Add(r.TTL), !rec.CreatedAt.IsZero()
	}
	if hasNotAfter && now.After(notAfter) {
		return r.finishUnclaimed(ctx, rec, contract.OutcomeExpired, contract.ErrExpired, "the action was not sent before its notAfter")
	}
	// A held action wakes at notAfter at the latest, so it expires on time.
	wakeAt := func(at time.Time) time.Time {
		if hasNotAfter && notAfter.Before(at) {
			return notAfter.Add(time.Millisecond)
		}
		return at
	}
	if notBefore, ok := rec.Time("notBefore"); ok && now.Before(notBefore) {
		r.hold(id, t.key(), wakeAt(notBefore))
		return nil
	}

	if net == nil {
		return r.refuse(ctx, rec, contract.ErrNotFound, fmt.Sprintf("this bridge runs no %s account", t.network))
	}
	if net.Runs != nil && !net.Runs(t.account) {
		return r.refuse(ctx, rec, contract.ErrNotFound, "this bridge does not run the chat's account")
	}
	status, _ := net.Exec.AccountStatusOf(t.account)
	if stopped, why := status.Stopped(now); stopped {
		return r.refuse(ctx, rec, contract.ErrBlocked, "sending is stopped on this account: "+why)
	}
	if !status.Connected() {
		// An account coming back holds its actions for a while; one that is
		// not coming back on its own refuses them.
		coming := status.State == "" || status.State == contract.StateConnecting || status.State == contract.StateTransientDisconnect
		if coming && now.Before(rec.CreatedAt.Add(r.ConnectGrace)) {
			r.hold(id, t.key(), wakeAt(now.Add(r.Recheck)))
			return nil
		}
		state := status.State
		if state == "" {
			state = "unknown"
		}
		return r.refuse(ctx, rec, contract.ErrNetwork, "the account is not connected ("+state+")")
	}

	req, err := r.build(ctx, rec, t, net)
	if err == nil {
		err = r.check(ctx, rec, req, t, net, now)
	}
	var ref *refusal
	var h *holdUntil
	switch {
	case errors.As(err, &h):
		r.hold(id, t.key(), wakeAt(h.at))
		return nil
	case errors.As(err, &ref):
		return r.refuse(ctx, rec, ref.code, ref.text)
	case err != nil:
		return err
	}
	return r.execute(ctx, rec, req, t, net)
}

// build resolves an action's references into a request.
func (r *Runner) build(ctx context.Context, rec *server.Record, t target, net *Network) (localmatrix.Request, error) {
	req := localmatrix.Request{
		Action:      rec.ID,
		Kind:        rec.String("action"),
		Account:     t.account,
		Chat:        t.chat,
		Text:        rec.String("text"),
		Key:         rec.String("reactionKey"),
		ForEveryone: rec.Bool("forEveryone"),
		Txn:         net.TxnID(rec.ID),
	}
	needsTarget := false
	switch req.Kind {
	case localmatrix.ActionSend:
		if strings.TrimSpace(req.Text) == "" {
			return req, errInvalid("a send needs text")
		}
	case localmatrix.ActionEdit:
		needsTarget = true
		if strings.TrimSpace(req.Text) == "" {
			return req, errInvalid("an edit needs text")
		}
	case localmatrix.ActionReact, localmatrix.ActionUnreact:
		needsTarget = true
		if req.Key == "" {
			return req, errInvalid("a reaction needs a reactionKey")
		}
	case localmatrix.ActionDelete:
		needsTarget = true
	case localmatrix.ActionMarkRead:
	default:
		return req, errInvalid(fmt.Sprintf("no action %q", req.Kind))
	}
	if strings.ContainsRune(req.Text, 0) {
		return req, errInvalid("the text holds a NUL character")
	}
	target, has, err := r.resolveMessage(ctx, rec, "target", t)
	if err != nil {
		return req, err
	}
	if needsTarget && !has {
		return req, errInvalid("the action needs a target message")
	}
	req.Target = target
	if req.Kind == localmatrix.ActionSend {
		replyTo, _, err := r.resolveMessage(ctx, rec, "replyTo", t)
		if err != nil {
			return req, err
		}
		req.ReplyTo = replyTo
	}
	return req, nil
}

// check holds the rules of section 9 and what the chat allows.
func (r *Runner) check(ctx context.Context, rec *server.Record, req localmatrix.Request, t target, net *Network, now time.Time) error {
	status, _ := net.Exec.AccountStatusOf(t.account)
	if stopped, why := status.Stopped(now); stopped {
		return &refusal{code: contract.ErrBlocked, text: "sending is stopped on this account: " + why}
	}
	info, err := net.Exec.Inspect(ctx, t.account, t.chat)
	if err != nil {
		return err
	}
	if !info.Exists {
		return errNotFound("the chat is not known to this bridge")
	}
	feat := info.Features
	switch req.Kind {
	case localmatrix.ActionSend:
		if feat != nil && feat.MaxTextLength > 0 && len([]rune(req.Text)) > feat.MaxTextLength {
			return errInvalid(fmt.Sprintf("the text is longer than the chat allows (%d characters)", feat.MaxTextLength))
		}
		if req.ReplyTo != "" && feat != nil && !feat.Reply.Partial() {
			return &refusal{code: contract.ErrUnsupported, text: "the chat takes no replies"}
		}
		if err := r.strangers(rec, info); err != nil {
			return err
		}
		return r.pace(ctx, req, t, now)
	case localmatrix.ActionEdit:
		if feat != nil && !feat.Edit.Partial() {
			return &refusal{code: contract.ErrUnsupported, text: "the chat takes no edits"}
		}
		if feat != nil && feat.EditMaxAge != nil && feat.EditMaxAge.Duration > 0 {
			if sent, ok, err := net.Exec.MessageAge(ctx, t.account, req.Target); err != nil {
				return err
			} else if ok && now.Sub(sent) > feat.EditMaxAge.Duration {
				return &refusal{code: contract.ErrRejected, text: "the message is too old to edit"}
			}
		}
	case localmatrix.ActionDelete:
		if feat != nil && !feat.Delete.Partial() {
			return &refusal{code: contract.ErrUnsupported, text: "the chat takes no deletions"}
		}
		if feat != nil && feat.DeleteMaxAge != nil && feat.DeleteMaxAge.Duration > 0 {
			if sent, ok, err := net.Exec.MessageAge(ctx, t.account, req.Target); err != nil {
				return err
			} else if ok && now.Sub(sent) > feat.DeleteMaxAge.Duration {
				return &refusal{code: contract.ErrRejected, text: "the message is too old to delete for everyone"}
			}
		}
	case localmatrix.ActionReact, localmatrix.ActionUnreact:
		if feat != nil && !feat.Reaction.Partial() {
			return &refusal{code: contract.ErrUnsupported, text: "the chat takes no reactions"}
		}
	}
	return nil
}

// strangers is rule 4: a first message to someone who is not a contact on
// the network, or into a chat they started and the owner never accepted,
// is the owner's call alone (allowNewChat is `writer: owner`).
func (r *Runner) strangers(rec *server.Record, info localmatrix.ChatInfo) error {
	if rec.Bool("allowNewChat") {
		return nil
	}
	if info.MessageRequest {
		return &refusal{code: contract.ErrBlocked, text: "the chat is a message request the owner has not accepted; allowNewChat is the owner's to set"}
	}
	if info.Direct && !info.OwnerSent && (!info.ContactKnown || !info.OtherIsContact) {
		return &refusal{code: contract.ErrBlocked, text: "a first message to someone who is not a contact needs allowNewChat, which only the owner sets"}
	}
	return nil
}

// pace is rule 3: the account's hourly and daily limits, three seconds
// between sends in one chat, and the same text to one chat once an hour.
func (r *Runner) pace(ctx context.Context, req localmatrix.Request, t target, now time.Time) error {
	s := r.Settings.Get(t.network, t.account)
	stats, err := r.Store.Sends(ctx, t.account, t.chat, textDigest(req.Text), now)
	if err != nil {
		return err
	}
	switch {
	case stats.LastHour >= s.perHour():
		return &refusal{code: contract.ErrRateLimited, text: fmt.Sprintf("the account sent %d messages in the last hour, its limit", stats.LastHour)}
	case stats.LastDay >= s.perDay():
		return &refusal{code: contract.ErrRateLimited, text: fmt.Sprintf("the account sent %d messages in the last day, its limit", stats.LastDay)}
	case stats.SameTextHour:
		return &refusal{code: contract.ErrRateLimited, text: "the same text went to this chat within the hour"}
	}
	if !stats.LastInChat.IsZero() {
		if next := stats.LastInChat.Add(r.ChatGap); now.Before(next) {
			return &holdUntil{at: next, reason: "pacing sends in one chat"}
		}
	}
	return nil
}

func textDigest(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:16])
}
