import Foundation
import SubstrateCore

/// A message sent while the assistant works (`agents` 6, decision 0318).
///
/// The server takes a message sent under its id into the chat turn running
/// on its thread: it writes the row at once, answers with that turn's
/// stream, and says two things on every stream of the turn, `messageQueued`
/// when the row is written and `messageTaken` when a model call reads it.
/// Between the two the message is queued, and is drawn so. What is queued
/// is kept here apart from the rows, since a message's row may be read
/// before any call has read the message.
///
/// A message is queued only while the run it joined goes on. When that run
/// says it ended, the message is an ordinary one in the thread from then.
/// A run that ended `ok` or `overbudget` with one unread starts the next
/// turn itself, and the conversation attaches to it once its rows are
/// read; a run that was stopped or that failed starts nothing, and the
/// card at the end of the chat offers Continue.
extension Conversation {
    /// The stream being read is let go for a message sent into its turn:
    /// the send's own stream is that turn's, from its start. What the
    /// stream knew of the rows goes with the send, so its replay drops what
    /// that one dropped.
    func join(_ state: inout State) -> (held: Set<Int>, replied: Int) {
        state.asks.append(.cancel("run"))
        switch state.run {
        case let .attach(run)?: return (run.held, run.replied)
        case let .send(run)?: return (run.held, run.replied)
        case nil: return ([], -1)
        }
    }

    /// Whether a send's stream says again what a stored row already holds,
    /// of a turn this conversation never drew from a stream. A message
    /// sent into a thread may meet a turn already running there (one
    /// another device started, or one this conversation was not reading),
    /// and is then answered with that turn's stream from its start: the
    /// rows of its earlier calls are drawn from the rows, under their own
    /// ids, and a turn made of their replay would take those ids over. A
    /// turn the stream is drawing here goes on being written. Only where
    /// the server takes a message into a running turn: elsewhere a send's
    /// stream is its own turn's from the first event.
    func stale(_ event: AgentEvent, _ state: State) -> Bool {
        guard queues, let ordinal = event.turn, rowTurns.contains(ordinal) else { return false }
        return !state.provisional.turns.contains { turn in
            turn.ordinal == ordinal || turn.tools.contains { $0.ordinal == ordinal || (!$0.id.isEmpty && $0.id == event.id) }
        }
    }

    /// What a stream's event says of the messages sent while its turn
    /// runs. A piece of a reply whose ordinal is past a message's was
    /// written by a call that read it, which is the one thing a tool's
    /// event does not say: the result of a call under way when the message
    /// was sent may carry an ordinal past it.
    ///
    /// A stream may be saying its turn again from the start (an attach, a
    /// send into a running turn, a send's repeat), and nothing on the wire
    /// says where that ends and the present begins. So a message it says is
    /// queued is drawn so only once the stream is known to have caught up
    /// (`catchUp`); until then it is kept apart (`recalled`), and drawn as
    /// the ordinary message its row is.
    func hear(_ state: inout State, _ event: AgentEvent, replied: Int) {
        switch event.kind {
        case .messageQueued:
            guard let id = event.id, !id.isEmpty, !state.taken.contains(id) else { return }
            // Read by a reply the rows already hold: said queued again
            // only because the stream is said again from its start.
            if let ordinal = event.turn, ordinal < replied { return take(&state, [id]) }
            // The message this send carried was written a moment ago, and
            // the stream says what its turn has done in the order it did
            // it: what it said was queued before this, and has not said
            // taken, was queued when this message was written.
            if case let .send(run)? = state.run, run.id == id { catchUp(&state) }
            if let held = state.queued.firstIndex(where: { $0.id == id }) {
                if let ordinal = event.turn { state.queued[held].ordinal = ordinal }
            } else if state.caught {
                state.queued.append(State.Queued(id: id, ordinal: event.turn))
            } else if let held = state.recalled.firstIndex(where: { $0.id == id }) {
                if let ordinal = event.turn { state.recalled[held].ordinal = ordinal }
            } else {
                state.recalled.append(State.Queued(id: id, ordinal: event.turn))
            }
        case .messageTaken:
            if let id = event.id { take(&state, [id]) }
        case .delta, .reasoning:
            guard let turn = event.turn, !state.queued.isEmpty || !state.recalled.isEmpty else { return }
            take(&state, (state.queued + state.recalled).filter { ($0.ordinal ?? .max) < turn }.map(\.id))
        case .heartbeat:
            // Sent only into a stream that has gone quiet, which one still
            // saying what its turn did before never is.
            catchUp(&state)
        default:
            break
        }
    }

    /// The stream has caught up with its turn: what it said was queued and
    /// has not said taken since is queued, and what it says from here is
    /// said as it happens. Known at three moments: a send that started its
    /// own turn is answered with that turn from its first event (`named`);
    /// the stream says the message this send carried was written; and a
    /// heartbeat.
    func catchUp(_ state: inout State) {
        state.caught = true
        guard !state.recalled.isEmpty else { return }
        state.queued += state.recalled.filter { said in !state.queued.contains { $0.id == said.id } }
        state.recalled = []
    }

    /// A model call read these messages: none is queued any more, and none
    /// is said to be again.
    func take(_ state: inout State, _ ids: [String]) {
        guard !ids.isEmpty else { return }
        state.queued.removeAll { ids.contains($0.id) }
        state.recalled.removeAll { ids.contains($0.id) }
        state.taken.formUnion(ids)
        // What a replay could say again is this run's: a few are all.
        if state.taken.count > 64 { state.taken = Set(ids) }
    }

    /// Rows this conversation read: a reply's row past a queued message's
    /// ordinal is one that read it.
    func read(_ state: inout State, queuedIn rows: [Record]) {
        guard !state.queued.isEmpty || !state.recalled.isEmpty else { return }
        let replied = Self.replied(rows)
        take(&state, (state.queued + state.recalled).filter { ($0.ordinal ?? .max) < replied }.map(\.id))
    }

    /// The newest ordinal a reply's row carries, or -1.
    static func replied(_ rows: [Record]) -> Int {
        rows.filter { $0["role"]?.string == "assistant" }.compactMap { $0["turn"]?.int }.max() ?? -1
    }

    /// The run said how it ended: nothing is queued from here. One that
    /// ended on its own starts the turn of what it left unread, which is
    /// looked for once its rows are read (`follows`); a stop or a failure
    /// starts nothing, and the card at the chat's end says how many
    /// messages it left.
    ///
    /// A stream that says how its run ended has said all the run did: what
    /// it said was queued and never said taken was not read.
    func ended(_ state: inout State, queueAfter status: String?) {
        catchUp(&state)
        guard !state.queued.isEmpty else { return }
        if status == "ok" || status == "overbudget" {
            state.follows = true
        } else {
            state.unread = state.queued.count
        }
        state.queued = []
    }

    /// The run is over and its rows are read, with the thread's row as it
    /// stood then. Where it left messages unread and the thread runs
    /// again, that is the turn they start: the server gives it a moment to
    /// take the thread before it says the run before is done, and its
    /// stream opens with a `messageTaken` for each. Where the thread does
    /// not run, the turn has not started: the messages are unanswered in
    /// the thread, which its rows show.
    ///
    /// - Returns: whether the conversation attaches to that turn.
    func follows(_ state: State, thread: Record?) -> Bool {
        state.follows && thread?["status"]?.string == "running"
    }

    /// The conversation is left with no stream: one dropped and is asked
    /// for again, now or after a wait, one is let go for the next run's
    /// (a send into the running turn too), or the run has none. What it
    /// held as queued it no longer knows: a call may read a message while
    /// nothing can say so, and the turn may end and the next stream be
    /// another turn's, which says nothing of it. It calls nothing queued,
    /// and the next stream says what is, from its turn's start (`hear`).
    func deafen(_ state: inout State) {
        state.queued = []
        state.recalled = []
        state.caught = false
    }

    /// Nothing is queued where no run goes on: whatever a conversation
    /// held is an ordinary message of the thread, which its rows show.
    func quieten(_ state: inout State) {
        guard state.phase == .idle || state.phase == .handingOver, !state.queued.isEmpty || !state.recalled.isEmpty else { return }
        state.queued = []
        state.recalled = []
    }
}
