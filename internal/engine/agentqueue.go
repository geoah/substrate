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
	// and committed says how. The loop reads neither field before settled.
	settled   chan struct{}
	committed bool
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
	if err == nil {
		// Before the loop may read it, so no client hears of a message taken
		// that it was never told of.
		l.hub.publish(substrate.AgentEvent{
			Kind: substrate.AgentEventMessageQueued, ID: m.id, Turn: m.turn, Text: in.Message,
		})
	}
	if queued {
		l.settleQueued(m, err == nil)
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

// settleQueued records how the transaction writing m's row ended. A row that
// was not written leaves the queue: its ordinal names no row.
func (l *agentLoop) settleQueued(m *queuedMessage, committed bool) {
	l.turnMu.Lock()
	m.committed = committed
	if !committed {
		for i, q := range l.queued {
			if q == m {
				l.queued = append(l.queued[:i:i], l.queued[i+1:]...)
				break
			}
		}
	}
	l.turnMu.Unlock()
	close(m.settled)
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
// meanwhile reads none of what it waited on.
func (l *agentLoop) takeQueued(ctx context.Context) []*queuedMessage {
	if l.hub == nil {
		return nil
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
		select {
		case <-m.settled:
		case <-ctx.Done():
			return taken
		}
		if !m.committed {
			continue
		}
		l.turnMu.Lock()
		m.taken = true
		l.turnMu.Unlock()
		taken = append(taken, m)
	}
	return taken
}

// closeQueue ends admission into the turn and answers the ids of the
// messages it never handed a model call, oldest first. The settle calls it
// with the thread's row held, which every admission holds too: a message is
// either in the queue by now or finds the thread settled.
func (l *agentLoop) closeQueue(ctx context.Context) []string {
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
		select {
		case <-m.settled:
		case <-ctx.Done():
			return unread
		}
		if m.committed {
			unread = append(unread, m.id)
		}
	}
	return unread
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

// replayOrder puts a thread's rows, read in the order they were created, in
// the order the model read them. Only a message sent while a turn ran is out
// of place: its row is created at once, before the reply that was streaming
// and could not have read it, and before the results of calls already under
// way. Each of those rows drew its ordinal before the message did, so a user
// row moves behind every row created after it under a smaller ordinal. A
// user row sent to a settled thread has the smallest ordinal of its turn and
// never moves.
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
		for j := i; j+1 < len(rows); j++ {
			next, ok := turnOf(rows[j+1])
			if !ok || next >= at {
				break
			}
			rows[j], rows[j+1] = rows[j+1], rows[j]
		}
	}
}
