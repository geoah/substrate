import Foundation

/// What a run's stream has said of the turns the stored rows do not hold
/// yet: provisional rows, in the shape the rows fold into. A turn is keyed
/// as its row will be, by the `turn` ordinal its `delta` and `reasoning`
/// events carry and by its calls' ids, so the stored row with the same key
/// replaces it (`TurnFold`), and a stream joined again (an attach, on every
/// return to the front) writes over what is held and empties nothing.
struct LiveRows: Codable, Hashable, Sendable {
    /// A message sent from here, until its row is read: what tells its row
    /// from an older one with the same words.
    struct Sent: Codable, Hashable, Sendable {
        /// The newest ordinal held of the thread before the send; nil
        /// while its rows were unread or stale.
        var baseline: Int?
        var at: Date
    }

    /// One pass over a turn's stream: a send's, or one attach's replay and
    /// what follows it live.
    struct Pass: Codable, Hashable, Sendable {
        /// Where the next piece of a text goes when the server sends no
        /// position (`agents` below 5): after the pieces of that text this
        /// pass has delivered, since every pass starts at the turn's start.
        var cursors: [String: Int] = [:]
        /// The largest ordinal an event of this pass carried.
        var seen = -1
        /// The assistant turn is finished with its tools, for a stream
        /// whose events name no turn: the next text begins another.
        var closed = false
        /// The replay lost events (`truncated`): a piece with no position
        /// can only go after what is held.
        var lost = false
    }

    var turns: [Turn] = []
    /// An attach's replay overflowed: events between the replay and the
    /// live tail were dropped, so a turn may miss text its row will hold.
    var incomplete = false
    /// Where each turn's reply and its reasoning stand, by the turn's id.
    var text: [String: TextRuns] = [:]
    var thought: [String: TextRuns] = [:]
    var sent: [String: Sent] = [:]
    /// By a compaction marker's id: the largest ordinal the stream had
    /// carried before it, which its summary row's ordinal is past.
    var markers: [String: Int] = [:]
    var pass = Pass()

    static let empty = LiveRows()

    var isEmpty: Bool { turns.isEmpty }

    /// A new pass begins: every position starts again, and nothing held is
    /// let go.
    ///
    /// - Parameter replaying: the pass is an attach's, which says the turn
    ///   again from its start. A turn the stream gave no ordinal has no key
    ///   to be found by when it is said again, so those are let go and the
    ///   replay makes them anew; written over, the first reply's text would
    ///   land in the last one held. Every server with an attach route names
    ///   the turn on each event, so this is no server's usual path.
    mutating func begin(replaying: Bool = false) {
        pass = Pass()
        if replaying, turns.contains(where: \.unkeyed) { keep(turns.filter { !$0.unkeyed }) }
    }

    /// The message just sent, shown before the server has it.
    ///
    /// - Parameter id: the id the message goes under (`messageId`), where
    ///   the server takes one: its row will have the same id, and that is
    ///   all the fold pairs the two by. With none, the turn is told from
    ///   an older row of the same words by `baseline` and `at`.
    mutating func send(_ message: String, id: String? = nil, seq: Int, baseline: Int?, at: Date) {
        if let id, let held = turns.firstIndex(where: { $0.id == id }) {
            // The same message once more: its turn is the one held.
            turns[held].pending = true
            return
        }
        let turn = id ?? "sent-\(seq)"
        turns.append(Turn(id: turn, role: .user, content: message, pending: true))
        if id == nil { sent[turn] = Sent(baseline: baseline, at: at) }
    }

    /// The server took the message.
    mutating func confirm() {
        if let i = turns.lastIndex(where: { $0.role == .user && $0.pending }) { turns[i].pending = false }
    }

    /// The assistant turn the stream is writing: the last one.
    var writing: Turn? { turns.last { $0.role == .assistant } }

    // MARK: Events

    /// Folds one stream event; answers whether it changed anything. An
    /// event that says again what is held changes nothing.
    @discardableResult
    mutating func apply(_ event: AgentEvent, seq: Int, now: Date) -> Bool {
        if let ordinal = event.turn { pass.seen = max(pass.seen, ordinal) }
        switch event.kind {
        case .reasoning:
            guard let piece = event.text, !piece.isEmpty else { return false }
            return write(piece, reasoning: true, event, seq: seq, now: now)
        case .delta:
            guard let piece = event.text, !piece.isEmpty else { return false }
            return write(piece, reasoning: false, event, seq: seq, now: now)
        case .toolStarted:
            return start(call(event), seq: seq)
        case .toolFinished:
            return settle(call(event), output: event.output ?? "", ok: event.ok ?? true, job: event.job, seq: seq)
        case .compacted:
            return compacted(tokensBefore: event.tokensBefore, covered: event.covered)
        case .truncated:
            pass.lost = true
            if incomplete { return false }
            incomplete = true
            return true
        case .thread, .idle, .done, .error, .lagged, .heartbeat:
            return false
        }
    }

    private func call(_ event: AgentEvent) -> ToolCall {
        ToolCall(id: event.id ?? "", name: event.tool ?? "tool", callable: event.callable, arguments: event.args ?? "", ordinal: event.turn)
    }

    /// A piece of a turn's reply or of its reasoning, written where it
    /// belongs: at the position the server gave it, else after the pieces
    /// this pass has delivered of the same text.
    private mutating func write(_ piece: String, reasoning: Bool, _ event: AgentEvent, seq: Int, now: Date) -> Bool {
        let index = assistant(event.turn, seq: seq)
        let id = turns[index].id
        var held = reasoning ? (turns[index].reasoning ?? "") : turns[index].content
        var runs = (reasoning ? thought[id] : text[id]) ?? TextRuns(holding: held)
        let cursor = id + (reasoning ? "/r" : "/d")
        let position = event.at ?? (pass.lost ? runs.end : pass.cursors[cursor, default: 0])
        let grew = runs.write(piece, at: position, into: &held)
        pass.cursors[cursor] = position + piece.unicodeScalars.count
        let before = turns[index]
        if reasoning {
            thought[id] = runs
            turns[index].reasoning = held
            // The clock and the mark follow what is new: thinking heard
            // again on a return to the front is not thinking going on.
            if grew {
                let started = turns[index].reasoningStartedAt ?? now
                turns[index].reasoningStartedAt = started
                turns[index].reasoningMillis = max(turns[index].reasoningMillis ?? 0, Int(now.timeIntervalSince(started) * 1000))
                turns[index].thinking = true
            }
        } else {
            text[id] = runs
            turns[index].content = held
            turns[index].thinking = false
        }
        if !runs.whole, !incomplete { incomplete = true }
        return turns[index] != before
    }

    /// The provisional turn of `ordinal`, made where the stream first
    /// names it. A stream that names no turn writes to the last assistant
    /// turn until that one is finished with its tools.
    private mutating func assistant(_ ordinal: Int?, seq: Int) -> Int {
        if let ordinal {
            if let index = turns.lastIndex(where: { $0.role == .assistant && $0.ordinal == ordinal }) { return index }
            turns.append(Turn(id: "turn-\(ordinal)", ordinal: ordinal, role: .assistant, content: ""))
        } else {
            if let last = turns.last, last.role == .assistant, !pass.closed { return turns.count - 1 }
            turns.append(Turn(id: "\(Turn.unkeyedPrefix)\(seq)", role: .assistant, content: ""))
        }
        pass.closed = false
        return turns.count - 1
    }

    private func locate(_ id: String) -> (turn: Int, call: Int)? {
        guard !id.isEmpty else { return nil }
        for t in turns.indices {
            if let c = turns[t].tools.firstIndex(where: { $0.id == id }) { return (t, c) }
        }
        return nil
    }

    /// The provisional turn a new call belongs to: the reply that asked
    /// for it. A reply's calls draw their ordinals one after another from
    /// the reply's own, and the next reply draws its own between, so a
    /// call whose ordinal does not follow the last turn's newest (its last
    /// call's, or its own where it has made none) is another reply's, one
    /// that streamed no text: a reply kept from the run before is not the
    /// one a continued run's first call belongs to. It is keyed by the
    /// call until its row is read.
    private mutating func home(of call: ToolCall, seq: Int) -> Int {
        if let last = turns.last, last.role == .assistant {
            let ordinals = last.tools.compactMap(\.ordinal)
            guard let ordinal = call.ordinal, ordinals.count == last.tools.count, let newest = ordinals.max() ?? last.ordinal else {
                return turns.count - 1
            }
            if ordinal <= newest + 1 { return turns.count - 1 }
        }
        turns.append(Turn(id: call.id.isEmpty ? "\(Turn.unkeyedPrefix)\(seq)" : "call-\(call.id)", role: .assistant, content: ""))
        pass.closed = false
        return turns.count - 1
    }

    private mutating func start(_ call: ToolCall, seq: Int) -> Bool {
        if let (t, c) = locate(call.id) {
            // Heard again: the same call, and what settled it stays.
            let before = turns[t].tools[c]
            turns[t].tools[c].name = call.name
            if let callable = call.callable { turns[t].tools[c].callable = callable }
            if !call.arguments.isEmpty { turns[t].tools[c].arguments = call.arguments }
            if let ordinal = call.ordinal { turns[t].tools[c].ordinal = ordinal }
            return turns[t].tools[c] != before
        }
        let index = home(of: call, seq: seq)
        turns[index].tools.append(call)
        turns[index].thinking = false
        return true
    }

    /// Settles a call BY ID. A finish with no card opens its own, settled:
    /// the loop refuses a dispatch past its budget without starting it, and
    /// that refusal is the card a reader most needs. A background job's end
    /// settles its card a second time, its output replacing the handle.
    private mutating func settle(_ call: ToolCall, output: String, ok: Bool, job: String?, seq: Int) -> Bool {
        pass.closed = true
        // An empty id settles the oldest card still running, not all of them.
        let matches: (ToolCall) -> Bool = { call.id.isEmpty ? $0.running : $0.id == call.id }
        for t in turns.indices {
            guard let c = turns[t].tools.firstIndex(where: matches) else { continue }
            let before = turns[t].tools[c]
            // A job that ended is not working again because its first
            // answer was heard again.
            if job == "working", let held = before.job, held != "working" { return false }
            turns[t].tools[c].output = output
            turns[t].tools[c].ok = ok
            if let job { turns[t].tools[c].job = job }
            return turns[t].tools[c] != before
        }
        var settled = call
        settled.output = output
        settled.ok = ok
        settled.job = job
        let index = home(of: settled, seq: seq)
        turns[index].tools.append(settled)
        pass.closed = true
        return true
    }

    /// Where the loop compacted mid-run. The event carries no summary text
    /// (the row does), and what follows opens a new assistant turn. It is
    /// keyed by where in the stream it stands, so the same marker heard
    /// again is the one held.
    private mutating func compacted(tokensBefore: Int?, covered: Int?) -> Bool {
        pass.closed = true
        let id = "summary-\(pass.seen + 1)"
        guard !turns.contains(where: { $0.id == id }) else { return false }
        turns.append(Turn(id: id, role: .summary, content: "", compaction: Compaction(tokensBefore: tokensBefore, covered: covered)))
        markers[id] = pass.seen
        return true
    }

    /// The records the run's reply links, as its `done` event carries them:
    /// they reach the reply before its row does, so from the run's end a
    /// link to no record reads as its text and the rest stand under it.
    mutating func mention(_ mentions: [Mention]?) {
        guard let mentions, !mentions.isEmpty,
            let reply = turns.lastIndex(where: { $0.role == .assistant && !$0.content.isEmpty })
        else { return }
        turns[reply].mentions = mentions
    }

    /// Keeps `kept` of the turns, and of what is held beside them what is
    /// theirs.
    mutating func keep(_ kept: [Turn]) {
        turns = kept
        let ids = Set(kept.map(\.id))
        text = text.filter { ids.contains($0.key) }
        thought = thought.filter { ids.contains($0.key) }
        sent = sent.filter { ids.contains($0.key) }
        markers = markers.filter { ids.contains($0.key) }
        if kept.isEmpty { incomplete = false }
    }
}

extension Turn {
    /// The id of a provisional turn whose stream named neither its ordinal
    /// nor a call: its place in the stream is all that tells it apart.
    static let unkeyedPrefix = "live-a"
    var unkeyed: Bool { id.hasPrefix(Self.unkeyedPrefix) }
}
