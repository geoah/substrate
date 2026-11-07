import Foundation

/// A run's turns while it streams, in the same shape the rows fold into.
/// `closed` marks the assistant turn finished with its tools: a finished
/// call does not end the turn, but the next text begins a new one.
struct LiveOverlay: Codable, Hashable, Sendable {
    var turns: [Turn] = []
    var closed = false
    /// An attach's replay overflowed: events between the replay and the live
    /// tail were dropped, so the overlay may miss text the rows will hold.
    var incomplete = false

    static let empty = LiveOverlay()

    mutating func pushReasoning(_ text: String, seq: Int, now: Date) {
        if let last = turns.last, last.role == .assistant, !closed {
            let started = last.reasoningStartedAt ?? now
            turns[turns.count - 1].reasoning = (last.reasoning ?? "") + text
            turns[turns.count - 1].reasoningStartedAt = started
            turns[turns.count - 1].reasoningMillis = max(0, Int(now.timeIntervalSince(started) * 1000))
            turns[turns.count - 1].thinking = true
        } else {
            turns.append(
                Turn(
                    id: "live-a\(seq)", role: .assistant, content: "", reasoning: text,
                    reasoningMillis: 0, reasoningStartedAt: now, thinking: true))
        }
        closed = false
    }

    mutating func pushDelta(_ text: String, seq: Int) {
        if let last = turns.last, last.role == .assistant, !closed {
            turns[turns.count - 1].content += text
            turns[turns.count - 1].thinking = false
        } else {
            turns.append(Turn(id: "live-a\(seq)", role: .assistant, content: text))
        }
        closed = false
    }

    mutating func pushToolStart(_ call: ToolCall, seq: Int) {
        if let last = turns.last, last.role == .assistant {
            turns[turns.count - 1].tools.append(call)
            turns[turns.count - 1].thinking = false
        } else {
            turns.append(Turn(id: "live-a\(seq)", role: .assistant, content: "", tools: [call]))
        }
    }

    /// Settles a call BY ID. A finish with no card opens its own, settled:
    /// the loop refuses a dispatch past its budget without starting it, and
    /// that refusal is the card a reader most needs. A background job's end
    /// settles its card a second time, its output replacing the handle.
    mutating func settleTool(_ call: ToolCall, output: String, ok: Bool, job: String? = nil, seq: Int) {
        // An empty id settles the oldest card still running, not all of them.
        let matches: (ToolCall) -> Bool = { call.id.isEmpty ? $0.running : $0.id == call.id }
        for t in turns.indices {
            if let c = turns[t].tools.firstIndex(where: matches) {
                turns[t].tools[c].output = output
                turns[t].tools[c].ok = ok
                if let job { turns[t].tools[c].job = job }
                closed = true
                return
            }
        }
        var settled = call
        settled.output = output
        settled.ok = ok
        settled.job = job
        turns.append(Turn(id: "live-a\(seq)", role: .assistant, content: "", tools: [settled]))
        closed = true
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

    /// Where the loop compacted mid-run. The event carries no summary text
    /// (the row does), and what follows opens a new assistant turn.
    mutating func pushCompacted(tokensBefore: Int?, covered: Int?, seq: Int) {
        turns.append(
            Turn(
                id: "live-c\(seq)", role: .summary, content: "",
                compaction: Compaction(tokensBefore: tokensBefore, covered: covered)))
        closed = true
    }

    /// Folds one stream event; answers whether it changed anything.
    @discardableResult
    mutating func apply(_ event: AgentEvent, seq: Int, now: Date) -> Bool {
        switch event.kind {
        case .reasoning:
            guard let text = event.text, !text.isEmpty else { return false }
            pushReasoning(text, seq: seq, now: now)
        case .delta:
            guard let text = event.text, !text.isEmpty else { return false }
            pushDelta(text, seq: seq)
        case .toolStarted:
            pushToolStart(
                ToolCall(
                    id: event.id ?? "", name: event.tool ?? "tool", callable: event.callable,
                    arguments: event.args ?? ""), seq: seq)
        case .toolFinished:
            settleTool(
                ToolCall(
                    id: event.id ?? "", name: event.tool ?? "tool", callable: event.callable,
                    arguments: event.args ?? ""),
                output: event.output ?? "", ok: event.ok ?? true, job: event.job, seq: seq)
        case .compacted:
            pushCompacted(tokensBefore: event.tokensBefore, covered: event.covered, seq: seq)
        case .truncated:
            incomplete = true
        case .thread, .idle, .done, .error, .heartbeat:
            return false
        }
        return true
    }
}
