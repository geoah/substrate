import Foundation
import SubstrateCore

/// The stored rows' turns under the ids they are drawn by, and where each
/// ordinal and each call stands in them.
struct StoredTurns: Equatable, Sendable {
    var turns: [Turn]
    /// A row's `turn` ordinal to its turn.
    var ordinals: [Int: Int] = [:]
    /// A call's id to the turn that holds it.
    var calls: [String: Int] = [:]

    /// - Parameter aliases: by a row's id, the id its turn was given while
    ///   it was streamed in this conversation, which it keeps.
    init(_ rows: [Turn], aliases: [String: String] = [:]) {
        turns = rows
        for index in turns.indices {
            if let alias = aliases[turns[index].id] { turns[index].id = alias }
            if let ordinal = turns[index].ordinal { ordinals[ordinal] = index }
            for call in turns[index].tools where !call.id.isEmpty { calls[call.id] = index }
        }
    }
}

/// One list of turns from the stored rows and what a run's stream has said
/// since: a stream's turn is a provisional row, and the stored row with its
/// key replaces it. The key is the row's own: its `turn` ordinal, which the
/// stream's text carries, and its calls' ids. So the two never show the
/// same text twice whichever arrives first, and a row read while its reply
/// is still being drawn takes the reply's place in the same list, under the
/// same id, with nothing emptied between.
enum TurnFold {
    struct Entry: Equatable, Sendable {
        var turn: Turn
        /// Where the turn stands among the stored ones, while it is that
        /// stored turn unchanged; nil for a provisional turn and for a
        /// stored one the stream changed (an id it took over, a call a
        /// stream event settled).
        var stored: Int?
    }

    struct Folded: Equatable, Sendable {
        var entries: [Entry]
        /// By a row's id, the provisional turn's id it took over.
        var claims: [String: String]
        /// What a conversation goes on holding of its stream once these
        /// rows are its own: the provisional turns no row replaced, as far
        /// as none did, and the calls the stream has said more of than
        /// the rows.
        var left: [Turn]
        /// The provisional messages paired with a row by their words alone,
        /// before the stream has named anything of the run they started: a
        /// guess, which a conversation's own read does not keep while the
        /// run goes on (`settle`).
        var unsure: Set<String> = []

        var turns: [Turn] { entries.map(\.turn) }
    }

    static func fold(_ stored: StoredTurns, _ live: LiveRows) -> Folded {
        var turns = stored.turns
        var changed: Set<Int> = []
        var claims: [String: String] = [:]
        var taken: Set<String> = []
        var claimed: Set<Int> = []
        // The row at `index` is the provisional turn `id`, and is drawn by
        // its id from here: once for a row, once for an id.
        func claim(_ index: Int, _ id: String) {
            guard !claimed.contains(index) else { return }
            claimed.insert(index)
            if turns[index].id == id { taken.insert(id) }
            // A row already drawn by the id of the turn it once replaced
            // keeps that one; what is left of a turn takes no row's place.
            guard turns[index].id == turns[index].row, !taken.contains(id), !id.hasSuffix("~") else { return }
            taken.insert(id)
            if let row = turns[index].row { claims[row] = id }
            turns[index].id = id
            changed.insert(index)
        }

        // The ordinals this run's stream has named, to place what names none.
        let named = live.turns.compactMap { turn in turn.ordinal ?? turn.tools.compactMap(\.ordinal).min() }
        var left: [(key: Double, turn: Turn)] = []
        // What the conversation goes on holding of its stream: the turns
        // no row replaced, and what the stream said past the rows.
        var held: [Turn] = []
        var unsure: Set<String> = []
        var floor = -Double.infinity
        func keep(_ turn: Turn, at key: Double?) {
            // Among provisional turns the order they arrived in holds.
            let key = max(key ?? floor, floor)
            floor = key.nextUp
            left.append((key, turn))
        }
        for turn in live.turns {
            switch turn.role {
            case .user:
                let sent = live.sent[turn.id]
                // The first row of the run the message started, as its own
                // stream named it: the reply's own, where the rows hold
                // the call that named it.
                let run = sent.flatMap { sent in sent.call.flatMap { stored.calls[$0] }.flatMap { stored.turns[$0].ordinal } ?? sent.first }
                if let index = row(of: turn, sent, before: run, in: turns, not: claimed) {
                    claim(index, turn.id)
                    // A new chat's thread held nothing before its first
                    // message (`baseline` -1), which is then the first row
                    // of its words there is, and no guess.
                    if let sent, run == nil, sent.baseline != -1 {
                        unsure.insert(turn.id)
                        held.append(turn)
                    }
                } else if let ordinal = turn.ordinal {
                    // A message sent while a turn ran, whose ordinal the
                    // server has said: where its row will stand.
                    keep(turn, at: Double(ordinal))
                    held.append(turn)
                } else if live.joined.contains(turn.id) || live.stands == turn.id {
                    // Sent into a running turn and not yet placed by the
                    // server: after everything held. So is one that
                    // stands unsent: a turn that runs beside it (resumed
                    // by an answer to a card) is not one it started.
                    keep(turn, at: .greatestFiniteMagnitude / 2)
                    held.append(turn)
                } else {
                    // Its row comes before every row of the run it started.
                    keep(turn, at: named.min().map { Double($0) - 0.75 } ?? .greatestFiniteMagnitude / 2)
                    held.append(turn)
                }
            case .summary:
                let after = live.markers[turn.id] ?? -1
                if let index = turns.indices.first(where: { turns[$0].role == .summary && (turns[$0].ordinal ?? -1) > after && !claimed.contains($0) }) {
                    claim(index, turn.id)
                } else {
                    keep(turn, at: Double(after) + 0.5)
                    held.append(turn)
                }
            case .assistant:
                var rest = turn
                var replaced = false
                if let ordinal = turn.ordinal, let index = stored.ordinals[ordinal], turns[index].role == .assistant {
                    // The row holds the whole reply: its text is the row's.
                    claim(index, turn.id)
                    replaced = true
                    rest.content = ""
                    rest.reasoning = nil
                    rest.mentions = nil
                    rest.thinking = false
                    rest.ordinal = nil
                }
                // The calls a row holds are that row's; one the stream has
                // said more of than the rows (it settled, its job ended) is
                // drawn in its row as the stream said it, and held until
                // the rows say as much.
                var newer: [ToolCall] = []
                rest.tools = turn.tools.filter { call in
                    guard let index = stored.calls[call.id], let at = turns[index].tools.firstIndex(where: { $0.id == call.id }) else { return true }
                    let held = turns[index].tools[at]
                    let settles = held.ok == nil && call.ok != nil
                    let ends = held.job == "working" && call.job != nil && call.job != "working"
                    if settles || ends {
                        turns[index].tools[at].output = call.output
                        turns[index].tools[at].ok = call.ok
                        turns[index].tools[at].job = call.job ?? held.job
                        changed.insert(index)
                        newer.append(call)
                    }
                    claim(index, turn.id)
                    replaced = true
                    return false
                }
                // What is left of a turn whose row took its id is drawn
                // beside it, under an id of its own.
                if replaced, taken.contains(turn.id) { rest.id = turn.id + "~" }
                let key = turn.ordinal.map(Double.init) ?? rest.tools.compactMap(\.ordinal).min().map { Double($0) - 0.5 }
                if !replaced || !rest.content.isEmpty || !(rest.reasoning ?? "").isEmpty || !rest.tools.isEmpty {
                    keep(rest, at: key)
                    rest.tools += newer
                    held.append(rest)
                } else if !newer.isEmpty {
                    rest.tools = newer
                    held.append(rest)
                }
            case .system:
                keep(turn, at: nil)
                held.append(turn)
            }
        }

        // The rows in their order, each provisional turn before the first
        // row whose ordinal is past its own.
        var entries: [Entry] = []
        entries.reserveCapacity(turns.count + left.count)
        var next = 0
        var last = -Double.infinity
        for index in turns.indices {
            let key = turns[index].ordinal.map(Double.init) ?? last
            last = key
            while next < left.count, left[next].key < key {
                entries.append(Entry(turn: left[next].turn, stored: nil))
                next += 1
            }
            entries.append(Entry(turn: turns[index], stored: changed.contains(index) ? nil : index))
        }
        entries += left[next...].map { Entry(turn: $0.turn, stored: nil) }
        return Folded(entries: entries, claims: claims, left: held, unsure: unsure)
    }

    /// The stored row of a message sent from here. Sent under an id of its
    /// own (`messageId`), it is the row of that id and no other, whatever
    /// its words: the server writes the message under the id it was sent
    /// with. Sent under none, to a server older than the id, it is found by
    /// its words, and by where it stands.
    ///
    /// Once the message's own stream has named a row of the run it started
    /// (`run`, from `LiveRows.Sent.first`), that is exact: a message with
    /// no id starts its run or is refused, so its row is the last of its
    /// words before that row, with no reply between the two. An older row
    /// of the same words has its own reply after it, and is never it,
    /// whatever the device held when the message was sent.
    ///
    /// Before that there is only what the device knew at the send, which
    /// is evidence: the first of its words past the newest ordinal held
    /// then (`baseline`, which a row written since on another device is
    /// also past); where the rows were unread, the last of its words
    /// written since the send, by two clocks that need not agree.
    private static func row(of turn: Turn, _ sent: LiveRows.Sent?, before run: Int?, in turns: [Turn], not claimed: Set<Int>) -> Int? {
        if let index = turns.firstIndex(where: { ($0.row ?? $0.id) == turn.id }) { return claimed.contains(index) ? nil : index }
        guard let sent else { return nil }
        let same = turns.indices.filter { turns[$0].role == .user && turns[$0].content == turn.content && !claimed.contains($0) }
        if let run {
            guard let index = same.last(where: { (turns[$0].ordinal ?? .max) < run }), let ordinal = turns[index].ordinal else { return nil }
            let answered = turns.contains { $0.role == .assistant && ($0.ordinal ?? .min) > ordinal && ($0.ordinal ?? .max) < run }
            return answered ? nil : index
        }
        if let baseline = sent.baseline { return same.first { (turns[$0].ordinal ?? -1) > baseline } }
        let since = sent.at.addingTimeInterval(-5)
        return same.last { (turns[$0].createdAt ?? .distantPast) >= since }
    }

    /// Rows a conversation read for itself landed: each provisional turn
    /// they hold is let go, its id kept for its row; the rest stay until
    /// their rows are read. With `ended`, the run is over and its rows are
    /// read, so they are the whole truth and nothing provisional is left.
    ///
    /// A message paired by its words alone (`Folded.unsure`) stays held
    /// while its run goes on, and its row keeps no id for it yet: the row
    /// may be an older one, and a name given for good would be the older
    /// row's for good. The stream names the run's first row, and the next
    /// read says which row it is.
    static func settle(_ rows: [Record], _ live: inout LiveRows, aliases: inout [String: String], ended: Bool) {
        guard !live.isEmpty else { return }
        let folded = fold(StoredTurns(transcript(of: rows), aliases: aliases), live)
        aliases.merge(folded.claims.filter { ended || !folded.unsure.contains($0.value) }) { held, _ in held }
        live.keep(ended ? [] : folded.left)
    }
}
