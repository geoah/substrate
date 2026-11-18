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
	"encoding/json"
	"time"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
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
// the thread's patch: wasLast is the `lastMessage` the row held before it.
// Only a `chat` thread with a person pushes a reply or a failure, and a
// turn that raised a request pushes that request alone.
func (t *txn) signalSettle(thread string, status string, wasLast eref, started time.Time) error {
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
	if mode != agentModeChat || person == "" {
		return nil
	}
	raised, err := t.raisedSince(thread, started)
	if err != nil || raised {
		return err
	}
	message := ""
	now := referenceTargetOf(row, propThreadLastMessage)
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
		Status: status, ThreadVersion: row.Version, TurnStarted: started, Message: message,
	})
	return nil
}

// raisedSince reports whether a record of thread that still waits on a
// person was created at or after since: what the turn that began then
// raised.
func (t *txn) raisedSince(threadID string, since time.Time) (bool, error) {
	b := &builder{}
	kinds := make([]string, 0, len(waitingKinds))
	open := ""
	for _, kind := range sortedKeys(waitingKinds) {
		w := waitingKinds[kind]
		kinds = append(kinds, kind)
		if open != "" {
			open += ` OR `
		}
		open += `(e.kind = ` + b.arg(kind) + ` AND e.states->>` + b.arg(w.machine) + ` = ` + b.arg(w.state) + `)`
	}
	where, err := threadRecordsWhere(t.ctx, t.tx, b, "e", kinds, threadID)
	if err != nil {
		return false, err
	}
	var raised bool
	err = t.row(`SELECT EXISTS (SELECT 1 FROM records e WHERE e.deleted_at IS NULL AND (`+open+`) AND `+where+
		` AND e.created_at >= `+b.arg(since)+`)`, b.args...).Scan(&raised)
	return raised, err
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
		t.pushSignal(substrate.PushSignal{
			Kind: kind, Thread: thread.ID, Mode: mode, Person: person,
			Item: substrate.PushItem{Kind: r.Kind, ID: r.ID, Version: r.Version}, Token: t.principal,
		})
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

// pushRead runs fn in a read-only transaction as the system.
func (ds *dataset) pushRead(ctx context.Context, fn func(t *txn) error) error {
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		return fn(t)
	})
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
		if mark != nil {
			if at, ok := mark.Props[propMarkReadAt].(string); ok {
				if read, err := time.Parse(time.RFC3339Nano, at); err == nil && !read.Before(c.TurnStarted) {
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
		// Raised by a settle the person has since read through: they saw
		// the card where the settle left it.
		row, err := t.loadRow(eref{Kind: typeThread, ID: c.Thread}, false)
		if err != nil || row == nil || row.DeletedAt != nil {
			return substrate.PushSend, err
		}
		if status, _ := row.Props["status"].(string); status == threadRunning {
			return substrate.PushSend, nil
		}
		rec, err := t.loadRow(eref{Kind: c.Item.Kind, ID: c.Item.ID}, false)
		if err != nil || rec == nil {
			return substrate.PushSend, err
		}
		finished, _ := row.Props[propThreadFinished].(string)
		if at, err := time.Parse(time.RFC3339Nano, finished); err != nil || rec.CreatedAt.After(at) {
			return substrate.PushSend, nil
		}
		if seen, err := readsThrough(referenceTargetOf(row, propThreadLastMessage).ID); err != nil || seen {
			return substrate.PushSeen, err
		}
	}
	return substrate.PushSend, nil
}

// PushPending reports whether item still waits on a person: live, and in
// its kind's waiting state.
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
	return row.States[w.machine] == w.state, nil
}

// pushBadgeCandidates bounds the unread candidates one badge reads: the
// count stops at substrate.PushBadgeMax, and the view may keep fewer.
const pushBadgeCandidates = 4 * substrate.PushBadgeMax

// PushBadgeThreads are the person's unread candidates for the badge, in one
// query: the live `chat` threads not running, unread for the person
// (`markedUnread` set, or a `lastMessage` that is not their `readThrough`;
// with no mark, any `lastMessage`), and not archived by them unless
// something waits on the thread. Each carries its kind, id and `agent`,
// what the caller's view decides a thread by.
func (ds *dataset) PushBadgeThreads(ctx context.Context, person string) ([]*substrate.Record, error) {
	if person == "" {
		return nil, nil
	}
	b := &builder{}
	last := referencePathSQL("th.props", propThreadLastMessage)
	read := referencePathSQL("m.props", propMarkReadThrough)
	rows, err := ds.db.QueryContext(ctx, `
		SELECT th.id, th.props->'agent'
		FROM records th
		LEFT JOIN records m ON m.kind = `+b.arg(typeThreadMark)+` AND m.id = th.id || '.' || `+b.arg(person)+` AND m.deleted_at IS NULL
		WHERE th.kind = `+b.arg(typeThread)+` AND th.deleted_at IS NULL
		  AND th.props->>'mode' = `+b.arg(agentModeChat)+`
		  AND COALESCE(th.props->>'status', '') <> `+b.arg(threadRunning)+`
		  AND (COALESCE(m.props->>'markedUnread', '') = 'true'
		       OR (`+last+` IS NOT NULL AND `+last+` IS DISTINCT FROM `+read+`))
		  AND (COALESCE(m.props->>'archived', '') <> 'true'
		       OR COALESCE((th.props->>'waitingOnPerson')::bigint, 0) > 0)
		ORDER BY th.id LIMIT `+b.arg(pushBadgeCandidates), b.args...)
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
