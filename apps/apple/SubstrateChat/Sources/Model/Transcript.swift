import Foundation

// The transcript as a reader sees it, folded out of the `llm/message` rows the
// loop wrote: a `user` row, an `assistant` row carrying prose and/or its
// dispatched `toolCalls`, and one `tool` row per dispatch keyed back by
// `toolCallId`, folded INTO the call that asked for it. Ported from the
// console's web/console/src/lib/api/transcript.ts, which is the spec.
//
// Pairing is by ID, never by name: one turn may dispatch the same tool twice.
// The same id keys the live stream's tool events, so a streaming card and the
// same card replayed off the rows are the same card.

/// One changelog entry a dispatch wrote, as the engine stamps it.
struct ChangeStamp: Hashable, Sendable {
    var seq: Int
    var op: String
    var kind: String
    var id: String
}

/// What a sub-agent call's child chain wrote, as the engine stamps it.
struct SubagentWrites: Hashable, Sendable {
    var thread: String?
    var records: Int
    var kinds: [String]
    var moreKinds: Int
}

/// One dispatched tool call, with whatever has settled about it so far.
struct ToolCall: Hashable, Sendable {
    var id: String
    /// The name the model saw: the agent's alias for the callable.
    var name: String
    /// The callable behind the name, in the actor spelling
    /// (`function:<authority>:<package>:<name>`, `agent:…`).
    var callable: String?
    var arguments: String
    /// Absent while the call runs.
    var output: String?
    var ok: Bool?
    /// Persisted rows only: the live stream carries no stamps.
    var changes: [ChangeStamp]?
    var subagentWrites: SubagentWrites?

    var running: Bool { ok == nil }
}

/// What a compaction folded, as far as its row or its live event says.
struct Compaction: Hashable, Sendable {
    var from: String?
    var through: String?
    var tokensBefore: Int?
    var covered: Int?
    var model: String?
}

struct Turn: Hashable, Sendable, Identifiable {
    enum Role: String, Hashable, Sendable { case user, assistant, system, summary }

    /// Stable across re-renders and across the live-to-stored handover.
    var id: String
    var role: Role
    var content: String
    var tools: [ToolCall] = []
    /// On a system turn: what the decision wrote.
    var changes: [ChangeStamp]?
    var compaction: Compaction?
    /// On an assistant turn: the model's thinking, as the row keeps it or as
    /// the live `reasoning` events carry it.
    var reasoning: String?
    /// How long the model thought, first piece to last.
    var reasoningMillis: Int?
    /// On a live turn: when the first reasoning piece arrived.
    var reasoningStartedAt: Date?
    /// On a live turn: the latest event was reasoning.
    var thinking = false
    /// On a live user turn: not yet confirmed by the server.
    var pending = false
    /// When the row was written; nil on a live turn.
    var createdAt: Date?
}

// MARK: Reading rows

private func changes(of record: Record) -> [ChangeStamp]? {
    guard let raw = record["changes"]?.array else { return nil }
    let out = raw.compactMap { item -> ChangeStamp? in
        guard let seq = item["seq"]?.int, let kind = item["kind"]?.text, let id = item["id"]?.text else { return nil }
        return ChangeStamp(seq: seq, op: item["op"]?.string ?? "", kind: kind, id: id)
    }
    return out.isEmpty ? nil : out
}

private func subagentWrites(of record: Record) -> SubagentWrites? {
    guard let raw = record["subagentWrites"]?.object, let records = raw["records"]?.int else { return nil }
    return SubagentWrites(
        thread: raw["thread"]?.referenceID,
        records: records,
        kinds: raw["kinds"]?.array?.compactMap(\.text) ?? [],
        moreKinds: raw["moreKinds"]?.int ?? 0)
}

private func compaction(of record: Record) -> Compaction {
    let covers = record["covers"]?.object ?? [:]
    return Compaction(from: covers["from"]?.text, through: covers["through"]?.text,
                      tokensBefore: record["tokensBefore"]?.int, model: record["model"]?.text)
}

/// Whether a tool row reports a failure. The row's `ok` decides where it
/// carries one; an older row has only its payload, and `{"error": …}` with
/// exactly that one key is the evidence.
func toolOK(_ record: Record) -> Bool {
    if let ok = record["ok"]?.bool { return ok }
    let content = record["content"]?.string?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    guard content.hasPrefix("{"), let object = JSONValue.parse(content)?.object else { return true }
    return !(object.count == 1 && object["error"] != nil)
}

private func calls(of record: Record) -> [ToolCall] {
    guard let raw = record["toolCalls"]?.array else { return [] }
    return raw.compactMap { item in
        let id = item["id"]?.string ?? ""
        let name = item["name"]?.string ?? ""
        guard !id.isEmpty || !name.isEmpty else { return nil }
        return ToolCall(id: id, name: name, callable: item["callable"]?.text, arguments: item["arguments"]?.string ?? "")
    }
}

/// Folds rows in loop order into turns. The order is preserved and never
/// sorted: `turn` is the loop's counter and two rows of one turn are ordered
/// by creation.
func transcript(of messages: [Record]) -> [Turn] {
    var turns: [Turn] = []
    // Every unsettled call so far, by id → (turn index, call index).
    var pending: [String: (Int, Int)] = [:]

    for record in messages {
        let role = record["role"]?.string ?? ""
        switch role {
        case "user", "assistant":
            let tools = role == "assistant" ? calls(of: record) : []
            let reasoning = role == "assistant" ? record["reasoning"]?.text : nil
            turns.append(Turn(
                id: record.id, role: role == "user" ? .user : .assistant,
                content: record["content"]?.string ?? "", tools: tools,
                reasoning: reasoning,
                reasoningMillis: reasoning == nil ? nil : record["reasoningMillis"]?.int,
                createdAt: record.createdDate))
            for (i, call) in tools.enumerated() where !call.id.isEmpty {
                pending[call.id] = (turns.count - 1, i)
            }
        case "summary":
            // The engine's fold of older turns; no model said it to the reader.
            turns.append(Turn(id: record.id, role: .summary, content: record["content"]?.string ?? "",
                              compaction: compaction(of: record), createdAt: record.createdDate))
        case "system":
            // The substrate's own turn: a decision envelope. Nothing dispatched it.
            turns.append(Turn(id: record.id, role: .system, content: record["content"]?.string ?? "",
                              changes: changes(of: record), createdAt: record.createdDate))
        case "tool":
            let callID = record["toolCallId"]?.string ?? ""
            let output = record["content"]?.string ?? ""
            let callable = record["callable"]?.text
            if let (t, c) = pending[callID] {
                turns[t].tools[c].output = output
                turns[t].tools[c].ok = toolOK(record)
                turns[t].tools[c].changes = changes(of: record)
                if let wrote = subagentWrites(of: record) { turns[t].tools[c].subagentWrites = wrote }
                if turns[t].tools[c].callable == nil { turns[t].tools[c].callable = callable }
                pending[callID] = nil
            } else {
                // An orphan: its call is not in this window. Its own turn,
                // rather than attributed to whichever assistant turn is last.
                turns.append(Turn(
                    id: record.id, role: .assistant, content: "",
                    tools: [ToolCall(id: callID, name: record["name"]?.string ?? "", callable: callable,
                                     arguments: "", output: output, ok: toolOK(record),
                                     changes: changes(of: record), subagentWrites: subagentWrites(of: record))],
                    createdAt: record.createdDate))
            }
        default:
            continue
        }
    }
    return turns
}

/// The `turn` ordinals the stored rows carry: what an attach already holds.
func persistedTurns(_ messages: [Record]) -> Set<Int> {
    Set(messages.compactMap { $0["turn"]?.int })
}

/// The newest ordinal the rows hold, or -1.
func newestTurn(_ messages: [Record]) -> Int {
    messages.compactMap { $0["turn"]?.int }.max() ?? -1
}

/// Whether an attach's replayed event is already a stored row. Set
/// membership, never a highest-turn cutoff: a resolution written while a
/// reply streams takes a later ordinal than the reply, and would hide it.
func isReplayed(_ event: AgentEvent, held: Set<Int>) -> Bool {
    guard let turn = event.turn else { return false }
    return held.contains(turn)
}

// MARK: The live overlay

/// A run's turns while it streams, in the same shape the rows fold into.
/// `closed` marks the assistant turn finished with its tools: a finished
/// call does not end the turn, but the next text begins a new one.
struct LiveOverlay: Hashable, Sendable {
    var turns: [Turn] = []
    var closed = false
    /// An attach's replay overflowed: events between the replay and the live
    /// tail were dropped, so the overlay may miss text the rows will hold.
    var incomplete = false

    static let empty = LiveOverlay()

    mutating func pushReasoning(_ text: String, seq: Int, now: Date = Date()) {
        if let last = turns.last, last.role == .assistant, !closed {
            let started = last.reasoningStartedAt ?? now
            turns[turns.count - 1].reasoning = (last.reasoning ?? "") + text
            turns[turns.count - 1].reasoningStartedAt = started
            turns[turns.count - 1].reasoningMillis = max(0, Int(now.timeIntervalSince(started) * 1000))
            turns[turns.count - 1].thinking = true
        } else {
            turns.append(Turn(id: "live-a\(seq)", role: .assistant, content: "", reasoning: text,
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
    /// that refusal is the card a reader most needs.
    mutating func settleTool(_ call: ToolCall, output: String, ok: Bool, seq: Int) {
        // An empty id settles the oldest card still running, not all of them.
        let matches: (ToolCall) -> Bool = { call.id.isEmpty ? $0.running : $0.id == call.id }
        for t in turns.indices {
            if let c = turns[t].tools.firstIndex(where: matches) {
                turns[t].tools[c].output = output
                turns[t].tools[c].ok = ok
                closed = true
                return
            }
        }
        var settled = call
        settled.output = output
        settled.ok = ok
        turns.append(Turn(id: "live-a\(seq)", role: .assistant, content: "", tools: [settled]))
        closed = true
    }

    /// Where the loop compacted mid-run. The event carries no summary text
    /// (the row does), and what follows opens a new assistant turn.
    mutating func pushCompacted(tokensBefore: Int?, covered: Int?, seq: Int) {
        turns.append(Turn(id: "live-c\(seq)", role: .summary, content: "",
                          compaction: Compaction(tokensBefore: tokensBefore, covered: covered)))
        closed = true
    }

    /// Folds one stream event; answers whether it changed anything.
    @discardableResult
    mutating func apply(_ event: AgentEvent, seq: Int, now: Date = Date()) -> Bool {
        switch event.kind {
        case .reasoning:
            guard let text = event.text, !text.isEmpty else { return false }
            pushReasoning(text, seq: seq, now: now)
        case .delta:
            guard let text = event.text, !text.isEmpty else { return false }
            pushDelta(text, seq: seq)
        case .toolStarted:
            pushToolStart(ToolCall(id: event.id ?? "", name: event.tool ?? "tool", callable: event.callable,
                                   arguments: event.args ?? ""), seq: seq)
        case .toolFinished:
            settleTool(ToolCall(id: event.id ?? "", name: event.tool ?? "tool", callable: event.callable,
                                arguments: event.args ?? ""),
                       output: event.output ?? "", ok: event.ok ?? true, seq: seq)
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

// MARK: Envelopes the substrate writes into a thread

/// A system turn's proposal decision (internal/engine/agentdecision.go).
struct DecisionNotice: Hashable, Sendable {
    var requestID: String
    var accepted: Bool
    var op: String
    var target: String?
    var adjusted: Bool
}

/// A system turn's interaction resolution.
struct InteractionNotice: Hashable, Sendable {
    var interactionID: String
    var answered: Bool
    var answers: [(question: String, selected: [String])]

    static func == (a: Self, b: Self) -> Bool {
        a.interactionID == b.interactionID && a.answered == b.answered && a.answers.count == b.answers.count
    }

    func hash(into h: inout Hasher) {
        h.combine(interactionID)
        h.combine(answered)
    }
}

/// A triggered thread's opening message: the delivery envelope a trigger
/// wrote as its first user turn.
struct DeliveryNotice: Hashable, Sendable {
    var op: String
    var kind: String
    var id: String
    var title: String?
}

private func envelope(_ turn: Turn, role: Turn.Role) -> [String: JSONValue]? {
    guard turn.role == role, turn.content.trimmingCharacters(in: .whitespaces).hasPrefix("{") else { return nil }
    return JSONValue.parse(turn.content)?.object
}

func decisionNotice(_ turn: Turn) -> DecisionNotice? {
    guard let env = envelope(turn, role: .system), env["event"]?.string == "proposalDecision" else { return nil }
    let decision = env["decision"]?.string
    guard decision == "accepted" || decision == "rejected",
          let request = env["request"]?.string, let id = request.split(separator: "/").last.map(String.init)
    else { return nil }
    return DecisionNotice(requestID: id, accepted: decision == "accepted", op: env["op"]?.text ?? "patch",
                          target: env["target"]?.text, adjusted: env["adjustedDiff"]?.object != nil)
}

func interactionNotice(_ turn: Turn) -> InteractionNotice? {
    guard let env = envelope(turn, role: .system) else { return nil }
    let event = env["event"]?.string
    guard event == "interactionAnswered" || event == "interactionDismissed",
          let path = env["interaction"]?.string, let id = path.split(separator: "/").last.map(String.init)
    else { return nil }
    let answers = (env["answers"]?.array ?? []).compactMap { item -> (String, [String])? in
        guard let q = item["question"]?.text else { return nil }
        return (q, item["selected"]?.array?.compactMap(\.string) ?? [])
    }
    return InteractionNotice(interactionID: id, answered: event == "interactionAnswered", answers: answers)
}

func deliveryNotice(_ turn: Turn) -> DeliveryNotice? {
    guard let env = envelope(turn, role: .user), let change = env["change"]?.object,
          let record = env["record"]?.object,
          let op = change["op"]?.text, change["kind"]?.text != nil, change["id"]?.text != nil,
          let kind = record["kind"]?.text, let id = record["id"]?.text
    else { return nil }
    let props = record["properties"]?.object ?? [:]
    return DeliveryNotice(op: op, kind: kind, id: id, title: props["title"]?.text ?? props["name"]?.text)
}

// MARK: Cards a call landed

/// A minted record id: twelve lowercase base32 characters.
private func isMintedID(_ s: String) -> Bool {
    s.count == 12 && s.allSatisfy { ("a" ... "z").contains($0) || ("2" ... "7").contains($0) }
}

/// The change request a settled call landed: from the engine's stamp where
/// the row carries one, else (a live card, an older row) from the payload,
/// checked strictly, because the NAME is an alias and not provenance.
func requestID(of call: ToolCall) -> String? {
    if let stamped = call.changes?.first(where: { $0.kind == Kinds.request && $0.op == "put" }) { return stamped.id }
    guard call.ok == true else { return nil }
    if let callable = call.callable {
        guard callable == "function:substrate.reamde.dev:core:propose" else { return nil }
    } else if call.name != "propose" {
        return nil
    }
    guard let object = JSONValue.parse(call.output)?.object, object.count == 1,
          let id = object["id"]?.string, isMintedID(id)
    else { return nil }
    return id
}

/// The interaction a settled `ask` landed, from the engine's stamp.
func interactionID(of call: ToolCall) -> String? {
    call.changes?.first {
        ($0.kind == Kinds.interaction || $0.kind == Kinds.interactionBeforeMove) && $0.op == "put"
    }?.id
}
