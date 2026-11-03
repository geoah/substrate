import Foundation
import SubstrateKit

/// One settled agent invocation (`substrate.AgentResult`).
struct AgentResult: Codable, Hashable, Sendable {
    var reply: String = ""
    var thread: String = ""
    var status: String = ""
    var reason: String?
    var turns: Int = 0
    var toolCalls: Int = 0
    var promptTokens: Int = 0
    var completionTokens: Int = 0
    var totalTokens: Int = 0
    var costUSD: Double = 0

    init() {}

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        reply = (try? c.decode(String.self, forKey: .reply)) ?? ""
        thread = (try? c.decode(String.self, forKey: .thread)) ?? ""
        status = (try? c.decode(String.self, forKey: .status)) ?? ""
        reason = try? c.decode(String.self, forKey: .reason)
        turns = (try? c.decode(Int.self, forKey: .turns)) ?? 0
        toolCalls = (try? c.decode(Int.self, forKey: .toolCalls)) ?? 0
        promptTokens = (try? c.decode(Int.self, forKey: .promptTokens)) ?? 0
        completionTokens = (try? c.decode(Int.self, forKey: .completionTokens)) ?? 0
        totalTokens = (try? c.decode(Int.self, forKey: .totalTokens)) ?? 0
        costUSD = (try? c.decode(Double.self, forKey: .costUSD)) ?? 0
    }
}

/// One streamed loop event (`substrate.AgentEvent`). Every field is read
/// tolerantly: a field spelled some other way reads as absent.
struct AgentEvent: Sendable, Equatable {
    enum Kind: String, Sendable {
        case thread, delta, reasoning, toolStarted, toolFinished, compacted, truncated, idle, done, error
        /// The `{}` line the server writes every 30 s while a turn is quiet.
        /// Not the server's kind: the app's, so a waiting client can tell a
        /// live stream from a dead one.
        case heartbeat = "_heartbeat"
    }

    var kind: Kind
    /// Rides the first event: the thread id, minted or continued.
    var thread: String?
    /// A content delta, or on `reasoning` a piece of the model's thinking.
    var text: String?
    /// The tool CALL's id, on both tool events: one turn may dispatch the same
    /// tool twice, so pairing is by id, never by name.
    var id: String?
    var tool: String?
    var callable: String?
    var args: String?
    var ok: Bool?
    var output: String?
    var tokensBefore: Int?
    var covered: Int?
    var result: AgentResult?
    var error: String?
    /// The `turn` ordinal the matching `llm/message` row carries or will
    /// carry, on delta, reasoning and the tool events.
    var turn: Int?
    /// Rides `idle`: the thread row's status.
    var status: String?

    /// One ndjson line, or nil: a blank line, an unknown kind (a newer
    /// server's) or garbage. The `{}` heartbeat is `.heartbeat`.
    static func parse(_ line: String) -> AgentEvent? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty, let data = trimmed.data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return nil }
        if object.isEmpty { return AgentEvent(kind: .heartbeat) }
        guard let raw = object["kind"] as? String, let kind = Kind(rawValue: raw), kind != .heartbeat else { return nil }
        func string(_ key: String) -> String? { object[key] as? String }
        func int(_ key: String) -> Int? {
            guard let n = object[key] as? NSNumber, CFGetTypeID(n) != CFBooleanGetTypeID() else { return nil }
            return n.intValue
        }
        var event = AgentEvent(kind: kind)
        event.thread = string("thread")
        event.text = string("text")
        event.id = string("id")
        event.tool = string("tool")
        event.callable = string("callable")
        event.args = string("args")
        if let n = object["ok"] as? NSNumber, CFGetTypeID(n) == CFBooleanGetTypeID() { event.ok = n.boolValue }
        event.output = string("output")
        event.tokensBefore = int("tokensBefore")
        event.covered = int("covered")
        event.error = string("error")
        event.turn = int("turn")
        event.status = string("status")
        if let result = object["result"], let body = try? JSONSerialization.data(withJSONObject: result) {
            event.result = try? JSONDecoder().decode(AgentResult.self, from: body)
        }
        return event
    }

    init(kind: Kind) { self.kind = kind }

    /// The loop failed after the stream began: an `error` event, or from an
    /// older server a `done` that carries text and no result.
    var failure: String? {
        switch kind {
        case .error: return error?.nonEmpty ?? text?.nonEmpty ?? "The agent run failed."
        case .done where result == nil: return error?.nonEmpty ?? text?.nonEmpty
        default: return nil
        }
    }
}

extension String {
    var nonEmpty: String? { isEmpty ? nil : self }
}
