package engine

// THREAD MARKS (decision record 0313). How far one person has read one thread
// is a record of `llm/threadmark`, one per thread and person, and the thread
// carries the two facts a list compares it with: `lastMessage`, the newest
// message it held when it last settled, and `waitingOnPerson`, how many of
// its questions, suggested changes and calls needing approval still wait.
//
// The mark is an ordinary record a client puts. What the engine holds, on
// every write of one:
//
//   - its id is `<thread id>.<actor id>`, built here for a create that names
//     none, so a client reads a mark without a search;
//   - a person, or an app acting for them, writes only their own: whom a
//     hand acts for is read as the one check reads it (roles.go), and
//     installed code writes none;
//   - `thread` and `readBy` are fixed at the create;
//   - `readThrough` names a message of that thread, and a write naming one
//     before the stored one keeps the stored position, so a late write lands
//     nothing and is answered with what is stored;
//   - `readAt` is stamped when a write's position is taken, and a value a
//     write sends is dropped.
//
// `pinned` and `archived` (decision record 0322) are the person's own flags
// on the same record, held by the same rules and by nothing more: no path
// here reads or writes either, so no run, reply or settle moves them, and a
// write of one never touches the position. A person may set one on a thread
// they never opened, so a mark may hold no `readThrough`; everything here
// already reads such a mark as a position not yet taken.
//
// `lastMessage` and `waitingOnPerson` are the engine's (`writer: system`):
// the first is stamped on every write that settles a thread, whichever path
// makes it, and the second is counted again in each write that creates or
// resolves one of the three kinds.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	typeThreadMark = vocabulary.KindLLMThreadMark

	propMarkThread      = "thread"
	propMarkReadBy      = "readBy"
	propMarkReadThrough = "readThrough"
	propMarkReadAt      = "readAt"
	propMarkUnread      = "markedUnread"
	propMarkPinned      = "pinned"
	propMarkArchived    = "archived"

	propThreadLastMessage = "lastMessage"
	propThreadWaiting     = "waitingOnPerson"

	// filterMe is the value a filter condition on a reference to an actor may
	// name for the person the request acts for.
	filterMe = substrate.FilterMe
)

// threadMarkID is the id of the mark of one thread and one person's actor.
func threadMarkID(thread, actor string) string { return thread + "." + actor }

// markWriter is the person a write of a mark is made as: a person's own
// hand, or the person an app's token acts for. No other hand keeps a
// person's place.
func (t *txn) markWriter() (substrate.Actor, error) {
	switch substrate.ActorTypeOf(t.actor) {
	case substrate.ActorTypePerson:
		return t.ds.personActorFor(t.ctx, t.actor), nil
	case substrate.ActorTypeApp:
		if p := t.onBehalfOfKey(t.actor); substrate.ActorTypeOf(p) == substrate.ActorTypePerson {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w: %w: %s may not write %s: only a person, or an app acting for them, keeps their place in a thread",
		substrate.ErrForbidden, substrate.ErrNotPermitted, handWords(t.actor), typeThreadMark)
}

// writtenReferenceID is the id a reference value names as a writer sent it,
// before coercion: the stored object, a record path or a bare id. "" when it
// names none, or a record of another kind.
func writtenReferenceID(v any, kind string) string {
	switch x := v.(type) {
	case string:
		if k, id, ok := vocabulary.SplitRecordPath(x); ok {
			if k != kind {
				return ""
			}
			return id
		}
		return x
	case map[string]any:
		path, _ := x[vocabulary.ReferenceValueKey].(string)
		return writtenReferenceID(path, kind)
	}
	return ""
}

// buildThreadMarkID names a mark created without an id: the thread the write
// names and the person it is made as.
func (t *txn) buildThreadMarkID(props map[string]any) (string, error) {
	thread := writtenReferenceID(props[propMarkThread], typeThread)
	if thread == "" {
		return "", fmt.Errorf("%w: a %s names its thread", substrate.ErrValidation, typeThreadMark)
	}
	writer, err := t.markWriter()
	if err != nil {
		return "", err
	}
	return threadMarkID(thread, actorID(writer)), nil
}

// messagePlace is where a message stands in its thread: by `turn`, then by
// when its row was written, then by id.
type messagePlace struct {
	turn int64
	at   time.Time
	id   string
}

func (p messagePlace) before(q messagePlace) bool {
	if p.turn != q.turn {
		return p.turn < q.turn
	}
	if !p.at.Equal(q.at) {
		return p.at.Before(q.at)
	}
	return p.id < q.id
}

// messagePlaceIn is the place of a live message of thread, false for a
// message that is not one.
func (t *txn) messagePlaceIn(id, thread string) (messagePlace, bool, error) {
	row, err := t.loadRow(eref{Kind: typeMessage, ID: id}, false)
	if err != nil {
		return messagePlace{}, false, err
	}
	if row == nil || row.DeletedAt != nil || referenceTargetOf(row, msgRelThread) != (eref{Kind: typeThread, ID: thread}) {
		return messagePlace{}, false, nil
	}
	turn, _ := anyFloat(row.Props["turn"])
	return messagePlace{turn: int64(turn), at: row.CreatedAt, id: row.ID}, true, nil
}

// prepareThreadMark holds a write of a mark to the rules above and settles
// what the engine decides about it, before the managed check reads the
// write. It reports whether the write's position is taken, which stamps
// `readAt`.
func (t *txn) prepareThreadMark(sp *applySpec) (bool, error) {
	if sp.ty.Identity != typeThreadMark || t.internal || t.movingRecords {
		return false, nil
	}
	// The engine's own, whatever the write says: a client that echoes a
	// stored value another device has since moved is not refused for it.
	delete(sp.props, propMarkReadAt)
	writer, err := t.markWriter()
	if err != nil {
		return false, err
	}
	me := actorID(writer)
	target := func(v any) eref {
		if refs := referenceTargets(v); len(refs) > 0 {
			return refs[0]
		}
		return eref{}
	}
	stored := sp.existing
	fixed := func(name string) (eref, error) {
		held := referenceTargetOf(stored, name)
		if !named(sp.props, name) {
			return held, nil
		}
		got := target(sp.props[name])
		if held.ID != "" && got != held {
			return eref{}, fmt.Errorf("%w: %s.%s is fixed when the record is created", substrate.ErrValidation, typeThreadMark, name)
		}
		return got, nil
	}
	thread, err := fixed(propMarkThread)
	if err != nil {
		return false, err
	}
	if thread.ID == "" {
		return false, fmt.Errorf("%w: a %s names its thread", substrate.ErrValidation, typeThreadMark)
	}
	by, err := fixed(propMarkReadBy)
	if err != nil {
		return false, err
	}
	if by.ID == "" {
		by = eref{Kind: kindActor, ID: me}
		sp.props[propMarkReadBy] = referenceValueOf(vocabulary.RecordPath(kindActor, me))
	}
	if by.ID != me {
		return false, fmt.Errorf("%w: %w: %s may not write this %s: it is another person's, and a person moves only their own",
			substrate.ErrForbidden, substrate.ErrNotPermitted, handWords(t.actor), typeThreadMark)
	}
	if want := threadMarkID(thread.ID, by.ID); sp.id != want {
		return false, fmt.Errorf("%w: the id of a %s is <thread id>.<actor id>, here %s: leave the id out and the substrate builds it",
			substrate.ErrValidation, typeThreadMark, want)
	}
	if stored == nil {
		row, err := t.loadRow(thread, false)
		if err != nil {
			return false, err
		}
		if row == nil || row.DeletedAt != nil {
			return false, fmt.Errorf("%w: thread %s", substrate.ErrNotFound, thread.ID)
		}
	}
	if !named(sp.props, propMarkReadThrough) {
		return false, nil
	}
	// A position is never taken back to nothing: the write keeps what is
	// stored, as a write naming an earlier message does.
	if sp.props[propMarkReadThrough] == nil {
		delete(sp.props, propMarkReadThrough)
		return false, nil
	}
	next := target(sp.props[propMarkReadThrough])
	place, ok, err := t.messagePlaceIn(next.ID, thread.ID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("%w: %s.%s names no message of thread %s", substrate.ErrValidation, typeThreadMark, propMarkReadThrough, thread.ID)
	}
	held := referenceTargetOf(stored, propMarkReadThrough)
	if held.ID != "" && held.ID != next.ID {
		was, ok, err := t.messagePlaceIn(held.ID, thread.ID)
		if err != nil {
			return false, err
		}
		// Forward only: two devices racing end on the later position
		// whichever commits first. A stored message that is gone holds
		// nothing back.
		if ok && place.before(was) {
			delete(sp.props, propMarkReadThrough)
			return false, nil
		}
	}
	unread := stored != nil && stored.Props[propMarkUnread] == true
	if unread && !named(sp.props, propMarkUnread) {
		sp.props[propMarkUnread] = false
	}
	cleared := unread && sp.props[propMarkUnread] != true
	return held.ID != next.ID || cleared, nil
}

// guardThreadMarkOwner holds what a hand does to a stored mark outside a
// put or a patch (its delete, a pin or a release of one of its properties)
// to the mark's own person, as a write of it is held. what says it in the
// refusal.
func (t *txn) guardThreadMarkOwner(kind string, row *erow, what string) error {
	if kind != typeThreadMark || t.internal {
		return nil
	}
	writer, err := t.markWriter()
	if err != nil {
		return err
	}
	if referenceTargetOf(row, propMarkReadBy).ID != actorID(writer) {
		return fmt.Errorf("%w: %w: %s may not %s this %s: it is another person's",
			substrate.ErrForbidden, substrate.ErrNotPermitted, handWords(t.actor), what, typeThreadMark)
	}
	return nil
}

// threadRecordsWhere renders, into b, the predicate selecting the records of
// one of kinds that name a thread in their `thread` reference, from the
// records table aliased `alias`: through refs, and by the row's own
// properties while a re-derivation still owes those rows
// (threadMessagesWhere, which this is for the kinds beside the message).
func threadRecordsWhere(ctx context.Context, x dbx, b *builder, alias string, kinds []string, threadID string) (string, error) {
	pending, err := refsPending(ctx, x, kinds, msgRelThread)
	if err != nil {
		return "", err
	}
	if pending {
		probe, err := json.Marshal(map[string]any{
			msgRelThread: referenceValueOf(vocabulary.RecordPath(typeThread, threadID)),
		})
		if err != nil {
			return "", err
		}
		return alias + `.props @> ` + b.arg(string(probe)) + `::jsonb`, nil
	}
	return refsAtSiteSQL(b, alias, nil, msgRelThread, true,
		[]refTarget{{kind: typeThread, ids: []string{threadID}}}), nil
}

// newestMessage is the id of the newest live message of a thread, by `turn`
// and then by when its row was written, "" for a thread that holds none. A
// `summary` row is the engine's note on older rows and is never the one.
//
// A person's message that no reply came after (a `user` row whose ordinal is
// past every `assistant` row's) is passed over while the thread holds
// anything else: one sent while a turn ran, which that turn never read
// (decision 0318), or one whose own call failed. It is its sender's own
// word and nothing has answered it, so it is not what a chat is unread for.
// The rows alone decide, so the loop's settle, a stop's and the recovery's
// after a crash name the same message. A thread that holds nothing but such
// messages (a new chat whose first call failed) names the newest of them. A
// row with no ordinal, from before rows carried one, is no such message: it
// keeps the last place, as it had.
func (t *txn) newestMessage(threadID string) (string, error) {
	b := &builder{}
	kind := b.arg(typeMessage)
	thread, err := threadMessagesWhere(t.ctx, t.tx, b, "e", threadID)
	if err != nil {
		return "", err
	}
	replies, err := threadMessagesWhere(t.ctx, t.tx, b, "a", threadID)
	if err != nil {
		return "", err
	}
	rows, err := t.query(`
		SELECT e.id FROM records e
		WHERE e.kind = `+kind+` AND e.deleted_at IS NULL AND `+thread+`
		  AND coalesce(e.props->>'role', '') <> `+b.arg(msgRoleSummary)+`
		ORDER BY coalesce(e.props->>'role' = 'user' AND (e.props->>'turn')::bigint > (
		           SELECT coalesce(max((a.props->>'turn')::bigint), -1) FROM records a
		           WHERE a.kind = `+kind+` AND a.deleted_at IS NULL AND `+replies+`
		             AND a.props->>'role' = 'assistant'), false),
		         (e.props->>'turn')::bigint DESC NULLS LAST, e.created_at DESC, e.id DESC LIMIT 1`,
		b.args...)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	var id string
	if rows.Next() {
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
	}
	return id, rows.Err()
}

// stampLastMessage writes the engine's `lastMessage` into a thread's row on
// a write that settles it, after the write's own checks: a hand never names
// it (its `writer:` is the system). One place serves every path that
// settles a thread: the loop's, a stop's and the recovery's.
func (t *txn) stampLastMessage(sp *applySpec, row *erow, accepted []string) ([]string, error) {
	if sp.ty.Identity != typeThread || t.movingRecords || sp.ty.Props[propThreadLastMessage] == nil {
		return accepted, nil
	}
	status, _ := sp.props["status"].(string)
	if status == "" || status == threadRunning {
		return accepted, nil
	}
	last, err := t.newestMessage(row.ID)
	if err != nil || last == "" {
		return accepted, err
	}
	want := referenceValueOf(vocabulary.RecordPath(typeMessage, last))
	if jsonEqual(row.Props[propThreadLastMessage], want) {
		return accepted, nil
	}
	// It never moves back, as a mark does not (prepareThreadMark): a mark
	// that reads through the message it names could not follow it. A thread
	// may name a message newestMessage now passes over (one the upgrade
	// seeded, a person's message with no reply after it); it keeps naming it
	// until a later message takes its place.
	if stored := referenceTargetOf(row, propThreadLastMessage); stored.Kind == typeMessage && stored.ID != "" {
		was, live, err := t.messagePlaceIn(stored.ID, row.ID)
		if err != nil {
			return accepted, err
		}
		if now, ok, err := t.messagePlaceIn(last, row.ID); err != nil {
			return accepted, err
		} else if live && ok && now.before(was) {
			return accepted, nil
		}
	}
	row.Props[propThreadLastMessage] = want
	return append(accepted, propThreadLastMessage), nil
}

// waitingKinds are the kinds whose open records wait on a person in a
// thread, each with the state machine and the state that says it still
// waits.
var waitingKinds = map[string]struct{ machine, state string }{
	vocabulary.KindLLMInteraction:     {propInteractionState, interactionPending},
	vocabulary.KindLLMApproval:        {propApprovalState, approvalPending},
	vocabulary.KindRecordPatchRequest: {propDecision, decisionProposed},
}

// decisionProposed is a change request nobody has decided.
const decisionProposed = "proposed"

// waitingOn counts the live records of the three kinds that name thread and
// still wait.
func (t *txn) waitingOn(threadID string) (int, error) {
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
	thread, err := threadRecordsWhere(t.ctx, t.tx, b, "e", kinds, threadID)
	if err != nil {
		return 0, err
	}
	var n int
	err = t.row(`SELECT count(*) FROM records e WHERE e.deleted_at IS NULL AND (`+open+`) AND `+thread, b.args...).Scan(&n)
	return n, err
}

// syncWaitingOnPerson counts again what waits on a person in the threads
// rows name, after a write or a delete of one of the three kinds, and writes
// the count where it moved. The write is the engine's own, in the
// transaction that changed the count.
func (t *txn) syncWaitingOnPerson(kind string, rows ...*erow) error {
	if _, ok := waitingKinds[kind]; !ok || t.movingRecords || t.bulk != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, row := range rows {
		id := referenceTargetOf(row, msgRelThread)
		if id.Kind != typeThread || id.ID == "" || seen[id.ID] {
			continue
		}
		seen[id.ID] = true
		if err := t.countWaitingOnPerson(id, rows); err != nil {
			return err
		}
	}
	return nil
}

// countWaitingOnPerson counts again what waits on a person in thread and
// writes the count where it moved; rows are the records whose write moved
// it, which a push names (pushsignals.go signalWaiting).
func (t *txn) countWaitingOnPerson(thread eref, rows []*erow) error {
	ty, err := t.resolveType(typeThread)
	if err != nil || ty.Props[propThreadWaiting] == nil {
		// A repository whose upgrade has not declared the property keeps
		// none; the migration's count is what its threads start from.
		return nil
	}
	if err := t.lockRecord(thread); err != nil {
		return err
	}
	row, err := t.loadRow(thread, true)
	if err != nil || row == nil || row.DeletedAt != nil {
		return err
	}
	n, err := t.waitingOn(thread.ID)
	if err != nil {
		return err
	}
	held, _ := anyFloat(row.Props[propThreadWaiting])
	if int(held) == n {
		return nil
	}
	t.signalWaiting(row, int(held), n, rows)
	wasScoped, wasInternal, wasExempt := t.scoped, t.internal, t.exempt
	t.scoped, t.internal, t.exempt = false, true, ""
	defer func() { t.scoped, t.internal, t.exempt = wasScoped, wasInternal, wasExempt }()
	return t.asActor(substrate.ActorSystem, func() error {
		_, err := t.patch(thread, substrate.PatchInput{Properties: map[string]any{propThreadWaiting: n}})
		return err
	})
}

// actingPerson is the actor of the person a read acts for: the one a
// request's token names, the repository's first for a token from before
// identities, or the person a run's work acts for once its request is over
// (a resumed turn, a job), which its context carries with no token. Work
// that acts for nobody, a trigger's, has none.
func (ds *dataset) actingPerson(ctx context.Context) (substrate.Actor, bool) {
	if substrate.IdentityFrom(ctx) != "" || substrate.PrincipalFrom(ctx) != "" {
		return ds.personActorFor(ctx, substrate.ActorPerson), true
	}
	if behalf := substrate.OnBehalfOfFrom(ctx); substrate.ActorTypeOf(behalf) == substrate.ActorTypePerson {
		return ds.personActorFor(ctx, behalf), true
	}
	return "", false
}

// resolveMe rewrites, in a filter's property conditions, the value `me` on a
// reference pinned to the actor kind into the actor of the person the
// request acts for (decision record 0313). A person reads their own marks
// without knowing their actor's id, and one who may not read actor records
// still filters by their own. The filter is copied where it changes.
func (ds *dataset) resolveMe(ctx context.Context, types []*vocabulary.Kind, f substrate.Filter) (substrate.Filter, error) {
	var out map[string]substrate.Cond
	for name, c := range f.Properties {
		if !condNames(c, filterMe) || !actorReference(types, name) {
			continue
		}
		person, ok := ds.actingPerson(ctx)
		if !ok {
			return f, fmt.Errorf("%w: %s: `me` is the person a read acts for, and this one acts for nobody", substrate.ErrValidation, name)
		}
		path := vocabulary.RecordPath(kindActor, actorID(person))
		if out == nil {
			out = make(map[string]substrate.Cond, len(f.Properties))
			for k, v := range f.Properties {
				out[k] = v
			}
		}
		if c.Eq == filterMe {
			c.Eq = path
		}
		if len(c.In) > 0 {
			in := make([]any, len(c.In))
			for i, v := range c.In {
				if v == filterMe {
					v = path
				}
				in[i] = v
			}
			c.In = in
		}
		out[name] = c
	}
	if out != nil {
		f.Properties = out
	}
	return f, nil
}

// condNames reports whether a condition's eq or in names value.
func condNames(c substrate.Cond, value string) bool {
	if c.Eq == value {
		return true
	}
	for _, v := range c.In {
		if v == value {
			return true
		}
	}
	return false
}

// actorReference reports whether every one of types that declares name
// declares it a reference pinned to the actor kind, and at least one does.
func actorReference(types []*vocabulary.Kind, name string) bool {
	any := false
	for _, ty := range types {
		p, ok := ty.Prop(name)
		if !ok {
			continue
		}
		if p.Datatype != vocabulary.DatatypeReference || p.To != kindActor {
			return false
		}
		any = true
	}
	return any
}
