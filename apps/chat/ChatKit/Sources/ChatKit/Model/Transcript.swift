import Foundation
import SubstrateCore

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
struct ChangeStamp: Codable, Hashable, Sendable {
    var seq: Int
    var op: String
    var kind: String
    var id: String
}

/// What a sub-agent call's child chain wrote, as the engine stamps it.
struct SubagentWrites: Codable, Hashable, Sendable {
    var thread: String?
    var records: Int
    var kinds: [String]
    var moreKinds: Int
}

/// One dispatched tool call, with whatever has settled about it so far.
struct ToolCall: Codable, Hashable, Sendable {
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
struct Compaction: Codable, Hashable, Sendable {
    var from: String?
    var through: String?
    var tokensBefore: Int?
    var covered: Int?
    var model: String?
}

/// One record a reply links, as the engine read it when the run settled
/// (`substrate.AgentMention`).
struct Mention: Codable, Hashable, Sendable {
    var kind: String
    var id: String
    /// The link's label as the model wrote it.
    var text: String
    /// No live record has this kind and id: the label stands alone.
    var missing = false
    /// Nothing the model saw in the run carried this record.
    var unread = false

    var path: String { "\(kind)/\(id)" }
}

struct Turn: Codable, Hashable, Sendable, Identifiable {
    enum Role: String, Codable, Hashable, Sendable { case user, assistant, system, summary }

    /// Stable across re-renders and across the live-to-stored handover.
    var id: String
    var role: Role
    var content: String
    var tools: [ToolCall] = []
    /// On a system turn: what the decision wrote.
    var changes: [ChangeStamp]?
    /// On a user turn: the records it is about, the row's `contextRecords`.
    var contextRecords: [RecordReference]?
    var compaction: Compaction?
    /// On an assistant turn: the model's thinking, as the row keeps it or as
    /// the live `reasoning` events carry it.
    var reasoning: String?
    /// How long the model thought, first piece to last.
    var reasoningMillis: Int?
    /// On a live turn: when the first reasoning piece arrived.
    var reasoningStartedAt: Date?
    /// On a run's final assistant turn: the records the reply links.
    var mentions: [Mention]?
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

private func mentions(of record: Record) -> [Mention]? {
    guard let raw = record["mentions"]?.array else { return nil }
    let out = raw.compactMap { item -> Mention? in
        guard let kind = item["kind"]?.text, let id = item["id"]?.text else { return nil }
        return Mention(kind: kind, id: id, text: item["text"]?.text ?? id,
                       missing: item["missing"]?.bool ?? false, unread: item["unread"]?.bool ?? false)
    }
    return out.isEmpty ? nil : out
}

/// A user row's `contextRecords`: the records the turn is about. The
/// engine's rendered block is the model's and is never shown.
func contextRecords(of record: Record) -> [RecordReference]? {
    guard let raw = record["contextRecords"]?.array else { return nil }
    let out = raw.compactMap { item -> RecordReference? in
        guard let kind = item["kind"]?.text, let id = item["id"]?.text else { return nil }
        return RecordReference(kind: kind, id: id)
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
                contextRecords: role == "user" ? contextRecords(of: record) : nil,
                reasoning: reasoning,
                reasoningMillis: reasoning == nil ? nil : record["reasoningMillis"]?.int,
                mentions: role == "assistant" ? mentions(of: record) : nil,
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
