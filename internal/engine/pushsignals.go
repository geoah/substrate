package engine

// PUSH SIGNALS (docs/plans/push.md). The engine finds what a push is about in
// the transactions that make it and hands each to the sink it was given
// (WithPushSink) once the transaction commits; it sends nothing itself:
//
//   - a chat turn settling (agentloop.go settle): the boot's recovery of a
//     lost run settles elsewhere and signals nothing, so a restart sends no
//     burst;
//   - `waitingOnPerson` rising, with the record that raised it, and
//     falling, with the record that was decided (countWaitingOnPerson);
//   - a person's mark moving forward, clearing `markedUnread` or setting
//     `archived` (apply, beside prepareThreadMark).
//
// With no sink, the operator's switch off, nothing is handed out. The
// dispatcher reads back through the methods below, as the system.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// conversationModes are the thread modes whose requests push: every mode a
// person's conversation or an automation's run is held in. A sub-agent's
// and a judge's requests name their root thread.
var conversationModes = map[string]bool{
	agentModeChat: true, "manual": true, "call": true, "record": true, "schedule": true, "webhook": true,
}

const (
	propThreadHeldTo   = "heldTo"
	heldToOnBehalfOf   = "onBehalfOf"
	propThreadFinished = "finishedAt"
	msgRoleAssistant   = "assistant"
)

// pushSignal hands s to the sink once this transaction commits. A replay, a
// dry run, a bulk run, a kind move and a repository migration rewrite
// history and hand out nothing.
func (t *txn) pushSignal(s substrate.PushSignal) {
	sink := t.ds.svc.pushSink
	if sink == nil || t.replaying || t.dryRun || t.bulk != nil || t.movingRecords || t.migrating {
		return
	}
	s.Repository = t.ds.scope.Repository
	if s.At.IsZero() {
		s.At = t.now
	}
	t.afterCommit = append(t.afterCommit, func() { sink.PushSignal(s) })
}

// threadPushInfo is a thread row's mode and the id of the actor of the
// person it acts for, "" for one that acts for nobody.
func threadPushInfo(row *erow) (mode, person string) {
	if row == nil {
		return "", ""
	}
	mode, _ = row.Props["mode"].(string)
	if held, ok := row.Props[propThreadHeldTo].(map[string]any); ok {
		if refs := referenceTargets(held[heldToOnBehalfOf]); len(refs) == 1 && refs[0].Kind == kindActor {
			person = refs[0].ID
		}
	}
	return mode, person
}

// signalSettle is a chat turn's settle, read in its own transaction after
// the thread's patch: wasLast is the `lastMessage` the row held before it,
// and raised the records this turn raised that wait on a person
// (agentLoop.noteRaised). Only a `chat` thread with a person pushes a reply
// or a failure, and a turn that raised a request pushes that request alone:
// its settle names the requests and the message it wrote, which is what the
// person reads the requests in.
func (t *txn) signalSettle(thread, status string, wasLast eref, started time.Time, raised []substrate.PushItem) error {
	if t.ds.svc.pushSink == nil {
		return nil
	}
	switch status {
	case threadOK, threadError, threadOverBudget:
	default:
		return nil
	}
	ref := eref{Kind: typeThread, ID: thread}
	row, err := t.loadRow(ref, false)
	if err != nil || row == nil || row.DeletedAt != nil {
		return err
	}
	mode, person := threadPushInfo(row)
	now := referenceTargetOf(row, propThreadLastMessage)
	if len(raised) > 0 {
		if conversationModes[mode] {
			t.pushSignal(substrate.PushSignal{
				Kind: substrate.PushSettled, Thread: thread, Mode: mode, Person: person, Status: status,
				ThreadVersion: row.Version, TurnStarted: started, Raised: raised, Wrote: now.ID,
			})
		}
		return nil
	}
	if mode != agentModeChat || person == "" {
		return nil
	}
	message := ""
	if now.ID != "" && now != wasLast {
		msg, err := t.loadRow(now, false)
		if err != nil {
			return err
		}
		if msg != nil && msg.DeletedAt == nil && msg.Props["role"] == msgRoleAssistant {
			message = now.ID
		}
	}
	t.pushSignal(substrate.PushSignal{
		Kind: substrate.PushSettled, Thread: thread, Mode: mode, Person: person,
		Status: status, ThreadVersion: row.Version, TurnStarted: started, Message: message, Wrote: now.ID,
	})
	return nil
}

// signalWaiting hands out the records a recount of thread's
// `waitingOnPerson` moved for: those that wait now when it rose, those that
// no longer do when it fell.
func (t *txn) signalWaiting(thread *erow, held, n int, rows []*erow) {
	if t.ds.svc.pushSink == nil || held == n {
		return
	}
	mode, person := threadPushInfo(thread)
	if !conversationModes[mode] {
		return
	}
	kind := substrate.PushRaised
	if n < held {
		kind = substrate.PushDecided
	}
	for _, r := range rows {
		if r == nil || referenceTargetOf(r, msgRelThread).ID != thread.ID {
			continue
		}
		w, ok := waitingKinds[r.Kind]
		if !ok {
			continue
		}
		waits := r.DeletedAt == nil && r.States[w.machine] == w.state
		if waits != (kind == substrate.PushRaised) {
			continue
		}
		item := substrate.PushItem{Kind: r.Kind, ID: r.ID, Version: r.Version}
		t.pushSignal(substrate.PushSignal{Kind: kind, Thread: thread.ID, Mode: mode, Person: person, Item: item, Token: t.principal})
		// The turn running on the thread raised it: its settle says so, and
		// names the message the request is read in.
		if kind == substrate.PushRaised {
			if v, ok := t.ds.runningThreads.Load(thread.ID); ok {
				if l, ok := v.(*agentLoop); ok {
					t.afterCommit = append(t.afterCommit, func() { l.noteRaised(item) })
				}
			}
		}
	}
}

// signalMark hands out a mark's write when it moved the person's position
// forward, cleared `markedUnread` or set `archived`.
func (t *txn) signalMark(before, row *erow, moved bool) {
	if t.ds.svc.pushSink == nil || row == nil {
		return
	}
	cleared := before != nil && before.Props[propMarkUnread] == true && row.Props[propMarkUnread] != true
	archived := row.Props[propMarkArchived] == true && (before == nil || before.Props[propMarkArchived] != true)
	if !moved && !cleared && !archived {
		return
	}
	thread := referenceTargetOf(row, propMarkThread)
	person := referenceTargetOf(row, propMarkReadBy)
	if thread.ID == "" || person.ID == "" {
		return
	}
	t.pushSignal(substrate.PushSignal{Kind: substrate.PushMarked, Thread: thread.ID, Person: person.ID, Token: t.principal})
}

// pushRead runs fn in a read-only snapshot on the pool: no write gate, no
// changelog lock, no hand, so push's rechecks never queue behind a write
// and answer on a read-only process too. fn reads and writes nothing else.
func (ds *dataset) pushRead(ctx context.Context, fn func(t *txn) error) error {
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	return fn(&txn{ctx: ctx, ds: ds, tx: tx, now: nowUTC(), internal: true})
}

// PushRecheck answers whether an alert is still worth sending, or what
// covers it now: a reply the mark reads through, a failure the person read
// past since its turn began, a thread archived, a request no longer pending.
func (ds *dataset) PushRecheck(ctx context.Context, c substrate.PushCheck) (string, error) {
	answer := substrate.PushSend
	err := ds.pushRead(ctx, func(t *txn) error {
		answer = substrate.PushSend
		var err error
		answer, err = t.pushRecheck(c)
		return err
	})
	return answer, err
}

func (t *txn) pushRecheck(c substrate.PushCheck) (string, error) {
	mark, err := t.loadRow(eref{Kind: typeThreadMark, ID: threadMarkID(c.Thread, c.Person)}, false)
	if err != nil {
		return "", err
	}
	if mark != nil && mark.DeletedAt != nil {
		mark = nil
	}
	archived := mark != nil && mark.Props[propMarkArchived] == true
	readThrough := referenceTargetOf(mark, propMarkReadThrough)
	// readsThrough reports whether the mark's position is message or later,
	// by 0313's order.
	readsThrough := func(message string) (bool, error) {
		if readThrough.ID == "" || message == "" {
			return false, nil
		}
		if readThrough.ID == message {
			return true, nil
		}
		at, ok, err := t.messagePlaceIn(message, c.Thread)
		if err != nil || !ok {
			// A message that is gone is nothing to alert for.
			return !ok, err
		}
		read, ok, err := t.messagePlaceIn(readThrough.ID, c.Thread)
		if err != nil || !ok {
			return false, err
		}
		return !read.before(at), nil
	}
	switch c.Event {
	case substrate.PushEventReply:
		if archived {
			return substrate.PushArchived, nil
		}
		if seen, err := readsThrough(c.Message); err != nil || seen {
			return substrate.PushSeen, err
		}
	case substrate.PushEventFailed:
		if archived {
			return substrate.PushArchived, nil
		}
		// Read at or after the settle: the person saw how the turn ended. A
		// mark taken during the turn (a client marking the message it just
		// sent) says nothing of the failure.
		if mark != nil {
			if at, ok := mark.Props[propMarkReadAt].(string); ok {
				if read, err := time.Parse(time.RFC3339Nano, at); err == nil && !read.Before(c.SettledAt) {
					return substrate.PushSeen, nil
				}
			}
		}
		// A thread that ran again since is past its failure.
		row, err := t.loadRow(eref{Kind: typeThread, ID: c.Thread}, false)
		if err != nil {
			return "", err
		}
		if row == nil || row.DeletedAt != nil {
			return substrate.PushSeen, nil
		}
		if status, _ := row.Props["status"].(string); status != threadError && status != threadOverBudget {
			return substrate.PushSeen, nil
		}
	case substrate.PushEventRequest:
		pending, err := t.pushPending(c.Item)
		if err != nil || !pending {
			return substrate.PushSeen, err
		}
		if c.Request {
			return substrate.PushSend, nil
		}
		// Raised by a turn whose settle the person has since read through:
		// they saw the card where that settle left it.
		if seen, err := readsThrough(c.SettleMessage); err != nil || seen {
			return substrate.PushSeen, err
		}
	}
	return substrate.PushSend, nil
}

// PushPending reports whether item still waits on a person at the version
// it names: live, in its kind's waiting state, and not written since. A
// newer version of the record is a new item, so what a notification of the
// old one offers is refused as the decision's version precondition would
// be.
func (ds *dataset) PushPending(ctx context.Context, item substrate.PushItem) (bool, error) {
	var pending bool
	err := ds.pushRead(ctx, func(t *txn) error {
		var err error
		pending, err = t.pushPending(item)
		return err
	})
	return pending, err
}

func (t *txn) pushPending(item substrate.PushItem) (bool, error) {
	w, ok := waitingKinds[item.Kind]
	if !ok {
		return false, nil
	}
	row, err := t.loadRow(eref{Kind: item.Kind, ID: item.ID}, false)
	if err != nil || row == nil || row.DeletedAt != nil {
		return false, err
	}
	return row.States[w.machine] == w.state && row.Version == item.Version, nil
}

// PushMayDecide reports whether hand, with ctx's token, passes the check
// the write that decides item passes beside its roles: the continuation of
// the run the item is in (run.go continuation). A refusal is false, never
// an error.
func (ds *dataset) PushMayDecide(ctx context.Context, hand substrate.Actor, item substrate.PushItem) (bool, error) {
	err := ds.continuation(ctx, hand, item.Kind, item.ID)
	if errors.Is(err, substrate.ErrForbidden) {
		return false, nil
	}
	return err == nil, err
}

// pushBadgePage is how many unread candidates one page of the badge reads.
const pushBadgePage = 500

// PushBadgeThreads are the person's unread candidates for the badge, in one
// query: the live `chat` threads that act for the person
// (`heldTo.onBehalfOf`), not running, unread for them (`markedUnread` set,
// or a `lastMessage` that is not their `readThrough`; with no mark, any
// `lastMessage`), and not archived by them unless something waits on the
// thread. Another person's chats are theirs to read, never this person's
// to be told of: an admin who reads them has no mark of them, and would
// count them unread for good. Each carries its kind, id and `agent`, what
// the caller's view decides a thread by.
func (ds *dataset) PushBadgeThreads(ctx context.Context, person, after string) ([]*substrate.Record, error) {
	if person == "" {
		return nil, nil
	}
	b := &builder{}
	last := referencePathSQL("th.props", propThreadLastMessage)
	read := referencePathSQL("m.props", propMarkReadThrough)
	actsFor := referencePathSQL("(th.props->'"+propThreadHeldTo+"')", heldToOnBehalfOf)
	rows, err := ds.db.QueryContext(ctx, `
		SELECT th.id, th.props->'agent'
		FROM records th
		LEFT JOIN records m ON m.kind = `+b.arg(typeThreadMark)+` AND m.id = th.id || '.' || `+b.arg(person)+` AND m.deleted_at IS NULL
		WHERE th.kind = `+b.arg(typeThread)+` AND th.deleted_at IS NULL
		  AND th.props->>'mode' = `+b.arg(agentModeChat)+`
		  AND `+actsFor+` = `+b.arg(vocabulary.RecordPath(kindActor, person))+`
		  AND COALESCE(th.props->>'status', '') <> `+b.arg(threadRunning)+`
		  AND (COALESCE(m.props->>'markedUnread', '') = 'true'
		       OR (`+last+` IS NOT NULL AND `+last+` IS DISTINCT FROM `+read+`))
		  AND (COALESCE(m.props->>'archived', '') <> 'true'
		       OR COALESCE((th.props->>'waitingOnPerson')::bigint, 0) > 0)
		  AND th.id > `+b.arg(after)+`
		ORDER BY th.id LIMIT `+b.arg(pushBadgePage), b.args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*substrate.Record
	for rows.Next() {
		var id string
		var agent []byte
		if err := rows.Scan(&id, &agent); err != nil {
			return nil, err
		}
		rec := &substrate.Record{Kind: typeThread, ID: id, Properties: map[string]any{}}
		if len(agent) > 0 {
			var v any
			if err := json.Unmarshal(agent, &v); err == nil && v != nil {
				rec.Properties["agent"] = v
			}
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// PushAdmins are the actor records of the people who hold `admin`, live and
// not disabled.
func (ds *dataset) PushAdmins(ctx context.Context) ([]string, error) {
	rows, err := ds.db.QueryContext(ctx, `SELECT id, props->'roles' FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->>'type' = $2
		  AND COALESCE(props->>'disabled', '') <> 'true'
		ORDER BY id`, kindActor, substrate.ActorTypePerson)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		for _, role := range roleIDs(raw) {
			if role == scope.RoleAdmin {
				out = append(out, id)
				break
			}
		}
	}
	return out, rows.Err()
}

// PushPerson is the id of the actor record of the person ctx acts for, ""
// for none.
func (ds *dataset) PushPerson(ctx context.Context) (string, error) {
	person, ok := ds.actingPerson(ctx)
	if !ok {
		return "", nil
	}
	return actorID(person), nil
}

var _ substrate.PushStore = (*dataset)(nil)
