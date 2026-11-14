package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A MESSAGE SENT WHILE A CHAT TURN RUNS (decision 0318).
//
// A chat message into a thread a detached chat turn runs on is admitted: its
// user row is written at once, under the id its client chose, and the loop
// reads it before its next model call, after the tool results of the last
// one. A message adds no call to the run it joins and moves none of its
// budgets. One the run ends without reading starts the next turn itself,
// where the run ended on its own (`ok`, `overbudget`); a run that was
// stopped or that failed leaves it in the thread for the person's next
// word.
//
// THE ORDINALS SAY WHAT EACH CALL READ. A message's ordinal and its place in
// the loop's queue are taken in one step, under the loop's turnMu, and so
// are a call's reading of the queue and the ordinal of that call's reply
// (takeQueued). So for every message m and every call c of the run:
//
//	c read m  ⇔  m.turn < c's reply ordinal
//
// A client orders the transcript by ordinal, so a message sits after the
// reply that was streaming when it was sent and before the first reply that
// answers it. Replay orders rows by creation, and a message's row is created
// before the reply it could not have been part of, so replay moves it back
// behind that reply (replayOrder).

// queuedMessage is one message a person sent while a chat turn ran.
type queuedMessage struct {
	id string
	// turn is the row's ordinal, drawn by enqueue.
	turn int
	// message is what the model is handed: the row as a replay would read
	// it.
	message llm.Message
	// persons and tokens are whose rules the sender adds to the rest of the
	// run: a hand that continues a run is one it is held to (run.go).
	persons []substrate.Actor
	tokens  []string
	// settled is closed once the transaction that writes the row has ended,
	// and committed says how. doubted is a write whose commit was sent and
	// whose answer never came, and whose row could not be read afterwards
	// either: whoever next needs to know reads the row (landed). The loop
	// reads none of the three before settled.
	settled   chan struct{}
	committed bool
	doubted   bool
	// taken is set, under turnMu, once a model call was handed the message.
	taken bool
}

// errQueueClosed is a message that reached a turn after its settle began.
var errQueueClosed = errors.New("the chat turn is settling")

// queueAttempts bounds how often one send goes round between a turn that is
// settling and the turn that follows it.
const queueAttempts = 3

// queuedTurnOpenWait bounds how long a settled run waits for the turn its
// unread messages start to hold the thread, so a client told `done` finds
// the next turn when it attaches. A turn that takes longer (its person's
// chat turns are all running) starts when it can.
const queuedTurnOpenWait = 2 * time.Second

// queueMessage admits a chat message into the detached chat turn running on
// the thread it names and answers that turn's hub, or nil when no chat turn
// of this process runs there: the caller then starts a turn, or is refused,
// as before. The user row is written under in.MessageID as a create that
// never replaces, in a transaction that holds the thread's row and finds
// this turn still running on it, so a row is written into a turn only while
// that turn can still read it or start the next one for it.
func (ds *dataset) queueMessage(ctx context.Context, actor substrate.Actor, ag *vocabulary.Agent, in substrate.ChatInput) (*turnHub, error) {
	v, ok := ds.runningThreads.Load(in.Thread)
	if !ok {
		return nil, nil
	}
	// A sub-agent's thread, a call's and a triggered run's have no hub: a
	// message into one is refused while it runs, as before.
	l, _ := v.(*agentLoop)
	if l == nil || l.hub == nil || l.in.mode != agentModeChat {
		return nil, nil
	}
	if l.ag.Identity() != ag.Identity() {
		return nil, fmt.Errorf("%w: thread %s belongs to agent %q — open a new thread", substrate.ErrValidation, in.Thread, l.ag.Identity())
	}
	if in.Message == "" {
		return nil, fmt.Errorf("%w: a chat turn needs a message", substrate.ErrValidation)
	}
	turnCtx, err := ds.chatTurnContext(ctx, ag, in, nowUTC())
	if err != nil {
		return nil, err
	}
	held := holdersOf(ctx, actor)
	if substrate.IsPerson(held.onBehalfOf) {
		held.onBehalfOf = ds.personActorFor(ctx, held.onBehalfOf)
	}
	m := &queuedMessage{
		id:      in.MessageID,
		message: llm.Message{Role: llm.RoleUser, Content: in.Message, Context: turnCtx.text},
		persons: append([]substrate.Actor{held.onBehalfOf}, held.continuedBy...),
		tokens:  held.tokens,
		settled: make(chan struct{}),
	}
	queued := false
	err = ds.runRecordTx(ctx, l.actor, exemptConversation, func(t *txn) error {
		queued = false
		// The lock order of claimThread and settle.
		if err := t.lockRegistryDepShared(); err != nil {
			return err
		}
		ref := eref{Kind: typeThread, ID: in.Thread}
		if err := t.lockRecord(ref); err != nil {
			return err
		}
		row, err := t.loadRow(ref, true)
		if err != nil {
			return err
		}
		if row == nil {
			return errQueueClosed
		}
		// The turn settled, or another run took the thread, since the read
		// above: the caller looks again.
		status, _ := row.Props["status"].(string)
		if cur, _ := ds.runningThreads.Load(in.Thread); row.DeletedAt != nil || status != threadRunning || cur != any(l) {
			return errQueueClosed
		}
		stored, err := t.storedNextTurn(in.Thread)
		if err != nil {
			return err
		}
		if !l.enqueue(int(stored), m) {
			return errQueueClosed
		}
		queued = true
		props := map[string]any{"role": "user", "content": in.Message}
		turnCtx.stamp(props)
		if hook := ds.agentRowWrite; hook != nil {
			if err := hook(t.ctx, props); err != nil {
				return err
			}
		}
		props["turn"] = m.turn
		props[msgRelThread] = in.Thread
		var unwritten int64
		return t.asActor(actor, func() error {
			_, err := t.put(substrate.PutInput{Kind: typeMessage, ID: m.id, Properties: props, IfVersion: &unwritten})
			return err
		})
	})
	committed, doubted := err == nil, false
	if queued && errors.Is(err, substrate.ErrWriteInDoubt) {
		// The commit was sent and its answer never came (decision 0271):
		// only the row says which way it went. A row the loop were not told
		// of would be read by nobody, and start no turn.
		rctx, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		row, rerr := ds.loadRowDB(rctx, eref{Kind: typeMessage, ID: m.id})
		done()
		committed, doubted = rerr == nil && m.landedAs(row, in.Thread), rerr != nil
	}
	if committed {
		// Before the loop may read it, so no client hears of a message taken
		// that it was never told of.
		l.hub.publish(substrate.AgentEvent{
			Kind: substrate.AgentEventMessageQueued, ID: m.id, Turn: m.turn, Text: in.Message,
		})
	}
	if queued {
		l.settleQueued(m, committed, doubted)
	}
	if errors.Is(err, errQueueClosed) {
		// The turn is past reading it. Its settle is short: wait it out, and
		// the caller starts the next turn with this message, or joins the one
		// an earlier message started. The wait is bounded, because a loop
		// that lost its thread to another run may never end.
		wctx, done := context.WithTimeout(ctx, agentSettleTimeout)
		defer done()
		if _, werr := l.hub.wait(wctx); werr != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errQueueClosed
	}
	if err != nil {
		return nil, err
	}
	return l.hub, nil
}

// enqueue puts m in the loop's queue and draws its row's ordinal, at least
// stored (what the thread's rows imply) and past every ordinal the loop has
// drawn, in one step: a call that does not find m in the queue has a reply
// ordinal below m's. It reports false once the turn's settle closed the
// queue.
func (l *agentLoop) enqueue(stored int, m *queuedMessage) bool {
	l.turnMu.Lock()
	defer l.turnMu.Unlock()
	if l.queueClosed {
		return false
	}
	m.turn = max(stored, l.turn)
	l.turn = m.turn + 1
	l.queued = append(l.queued, m)
	return true
}

// queuedIDs are the ids of the messages in the queue. A message that joins
// after this was read commits after it too, so no read of the thread's rows
// made before the call holds it.
func (l *agentLoop) queuedIDs() []string {
	l.turnMu.Lock()
	defer l.turnMu.Unlock()
	ids := make([]string, 0, len(l.queued))
	for _, m := range l.queued {
		ids = append(ids, m.id)
	}
	return ids
}

// landedAs reports whether row is the row m's transaction wrote: a live
// user row of this thread under m's id, at the ordinal m drew, saying what
// m says. Another message under the same id (a copy sent elsewhere that won
// the id) is not it.
func (m *queuedMessage) landedAs(row *erow, thread string) bool {
	if row == nil || row.DeletedAt != nil || referenceID(row.Props[msgRelThread]) != thread {
		return false
	}
	role, _ := row.Props["role"].(string)
	content, _ := row.Props["content"].(string)
	turn, ok := anyFloat(row.Props["turn"])
	return ok && int(turn) == m.turn && role == "user" && content == m.message.Content
}

// settleQueued records how the transaction writing m's row ended. A row that
// was not written leaves the queue: its ordinal names no row. One in doubt
// stays, for the next reader of the queue to settle.
func (l *agentLoop) settleQueued(m *queuedMessage, committed, doubted bool) {
	l.turnMu.Lock()
	m.committed, m.doubted = committed, doubted
	if !committed && !doubted {
		l.dropQueuedLocked(m)
	}
	l.turnMu.Unlock()
	close(m.settled)
}

func (l *agentLoop) dropQueuedLocked(m *queuedMessage) {
	for i, q := range l.queued {
		if q == m {
			l.queued = append(l.queued[:i:i], l.queued[i+1:]...)
			return
		}
	}
}

// resolveQueued waits for the transaction writing m's row to end and
// answers whether the row is stored. A write left in doubt is settled here
// by reading the row through load: stored, it is a message like any other;
// absent, it leaves the queue. A read that fails is an error, because a
// call that went on without knowing would write a reply past a message it
// may not have read.
func (l *agentLoop) resolveQueued(ctx context.Context, m *queuedMessage, load func() (*erow, error)) (bool, error) {
	select {
	case <-m.settled:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	l.turnMu.Lock()
	committed, doubted := m.committed, m.doubted
	l.turnMu.Unlock()
	if !doubted {
		return committed, nil
	}
	row, err := load()
	if err != nil {
		return false, fmt.Errorf("read a message whose write was left in doubt: %w", err)
	}
	landed := m.landedAs(row, l.threadID)
	l.turnMu.Lock()
	m.committed, m.doubted = landed, false
	if !landed {
		l.dropQueuedLocked(m)
	}
	l.turnMu.Unlock()
	if landed {
		l.hub.publish(substrate.AgentEvent{
			Kind: substrate.AgentEventMessageQueued, ID: m.id, Turn: m.turn, Text: m.message.Content,
		})
	}
	return landed, nil
}

// takeQueued answers the messages the next model call reads: every one
// queued and not yet read, oldest first. In the same step it draws that
// call's reply ordinal, past theirs and below every later message's. A
// reply ordinal reserved earlier and never streamed under (the call it was
// for was refused for its context) is given up when the call now reads more
// than that one did.
//
// A message whose row is still being written is waited for: its transaction
// holds the thread's row and is a moment from its end. A run that is stopped
// meanwhile reads none of what it waited on, and its call does not start. An
// error is a message in doubt that could not be read: the call must not be
// made.
func (l *agentLoop) takeQueued(ctx context.Context) ([]*queuedMessage, error) {
	if l.hub == nil {
		return nil, nil
	}
	l.turnMu.Lock()
	var waiting []*queuedMessage
	for _, m := range l.queued {
		if !m.taken {
			waiting = append(waiting, m)
		}
	}
	if l.replyTurn < 0 || l.turnStreamed || len(waiting) > 0 {
		l.replyTurn, l.turnStreamed = l.turn, false
		l.turn++
	}
	l.turnMu.Unlock()
	var taken []*queuedMessage
	for _, m := range waiting {
		landed, err := l.resolveQueued(ctx, m, func() (*erow, error) {
			return l.ds.loadRowDB(ctx, eref{Kind: typeMessage, ID: m.id})
		})
		if ctx.Err() != nil {
			// The stop is what ends the call: nothing more is read.
			return taken, nil
		}
		if err != nil {
			return nil, err
		}
		if !landed {
			continue
		}
		l.turnMu.Lock()
		m.taken = true
		l.turnMu.Unlock()
		taken = append(taken, m)
	}
	return taken, nil
}

// closeQueue ends admission into the turn and answers the ids of the
// messages it never handed a model call, oldest first. The settle calls it
// in its transaction, with the thread's row held, which every admission
// holds too: a message is either in the queue by now or finds the thread
// settled. A message whose write was left in doubt is read here, where no
// write is in flight.
func (l *agentLoop) closeQueue(t *txn) ([]string, error) {
	l.turnMu.Lock()
	l.queueClosed = true
	var waiting []*queuedMessage
	for _, m := range l.queued {
		if !m.taken {
			waiting = append(waiting, m)
		}
	}
	l.turnMu.Unlock()
	var unread []string
	for _, m := range waiting {
		landed, err := l.resolveQueued(t.ctx, m, func() (*erow, error) {
			return t.loadRow(eref{Kind: typeMessage, ID: m.id}, false)
		})
		if err != nil {
			return nil, err
		}
		if landed {
			unread = append(unread, m.id)
		}
	}
	return unread, nil
}

// startQueuedTurn starts the turn a settled run's unread messages ask for,
// as a chat thread's continuation runs (resumeChatTurn): no new message, the
// stored rows are the whole context, and it is held to whom the first of
// those messages names. It is the person's word, so the bounds of a resume
// the engine starts on its own (the agent's `resume: never`, the hourly
// budget) do not apply; a thread stopped since stays stopped. The channel it
// answers closes once the turn holds the thread, or will not for now.
func (ds *dataset) startQueuedTurn(threadID, message string) <-chan struct{} {
	opened := make(chan struct{})
	var once sync.Once
	signal := func() { once.Do(func() { close(opened) }) }
	started := ds.spawn("queued chat turn", func(ctx context.Context) {
		defer signal()
		// Shutdown reaches here as a canceled context: the message stays in
		// the thread, and the person's next word reads it.
		if ctx.Err() != nil {
			return
		}
		ds.continueThreadFrom(ctx, deferredResume{thread: threadID, resolution: message, sent: true}, signal)
	})
	if !started {
		signal()
	}
	return opened
}

// heldToUnanswered is ctx with the senders of the thread's unanswered
// messages added to whom the turn it starts is held to (run.go): the persons
// and the tokens each row's `heldTo` names. The thread is settled, so the
// set cannot grow before the turn claims it, and a claim that finds it
// answered runs nothing.
func (ds *dataset) heldToUnanswered(ctx context.Context, threadID string) (context.Context, error) {
	rows, err := ds.threadRows(ctx, threadID)
	if err != nil {
		return ctx, err
	}
	ctx, joiners := withRunJoiners(ctx)
	for _, r := range unansweredRows(rows) {
		persons, tokens, err := ds.storedHolders(ctx, r.props[propHeldTo])
		if err != nil {
			return ctx, err
		}
		joiners.add(persons, tokens...)
	}
	return ctx, nil
}

// unansweredRows are the user rows no reply came after: those whose ordinal
// is past every assistant row's, as unansweredMessage reads them.
func unansweredRows(rows []threadRow) []threadRow {
	replied := -1
	turnOf := func(r threadRow) int {
		t, ok := anyFloat(r.props["turn"])
		if !ok {
			return -1
		}
		return int(t)
	}
	for _, r := range rows {
		if r.role() == "assistant" {
			replied = max(replied, turnOf(r))
		}
	}
	var out []threadRow
	for _, r := range rows {
		if r.role() == "user" && turnOf(r) > replied {
			out = append(out, r)
		}
	}
	return out
}

// replayOrder puts a thread's rows, read in the order they were created, in
// the order the model read them. Only a message sent while a turn ran is out
// of place: its row is created at once, before the reply that was streaming
// and could not have read it, and before the results of calls already under
// way. Each of those rows drew its ordinal before the message did, so a user
// row moves behind the last row created after it under a smaller ordinal,
// whatever was written between them (a resolution's row, which draws a later
// ordinal than the message). A user row sent to a settled thread has the
// smallest ordinal of its turn and never moves. A row with no ordinal, from
// before rows carried one, ends the search: nothing is moved past it.
func replayOrder(rows []threadRow) {
	turnOf := func(r threadRow) (int, bool) {
		t, ok := anyFloat(r.props["turn"])
		return int(t), ok
	}
	for i := len(rows) - 2; i >= 0; i-- {
		if rows[i].role() != "user" {
			continue
		}
		at, ok := turnOf(rows[i])
		if !ok {
			continue
		}
		last := i
		for j := i + 1; j < len(rows); j++ {
			next, ok := turnOf(rows[j])
			if !ok {
				break
			}
			if next < at {
				last = j
			}
		}
		if last == i {
			continue
		}
		moved := rows[i]
		copy(rows[i:last], rows[i+1:last+1])
		rows[last] = moved
	}
}
