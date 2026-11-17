import Foundation
import SubstrateCore

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
    /// The records the reply links, as the engine read them when the run
    /// settled: what its row will carry.
    var mentions: [Mention]?
    /// Why a run that did not finish ended, from the server's closed set,
    /// and the seconds a provider asked to wait; nil from an older server.
    var cause: String?
    var retryAfter: Int?

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
        mentions = ChatKit.mentions(try? c.decode([JSONValue].self, forKey: .mentions))
        cause = try? c.decode(String.self, forKey: .cause)
        retryAfter = try? c.decode(Int.self, forKey: .retryAfter)
    }
}

/// One streamed loop event (`substrate.AgentEvent`). Every field is read
/// tolerantly: a field spelled some other way reads as absent.
struct AgentEvent: Codable, Sendable, Equatable {
    /// A kind this build does not name is no event: its line is skipped, and
    /// the stream goes on.
    enum Kind: String, Codable, Sendable {
        case thread, delta, reasoning, toolStarted, toolFinished, compacted, truncated, idle, done, error
        /// A message sent while the turn runs (`agents` 6, decision 0318).
        /// `messageQueued` says its row is written: `id` is the row's,
        /// `turn` its ordinal, `text` what it says, on every stream of the
        /// turn, so a window that did not send it draws it too.
        /// `messageTaken` says a model call now reads it: until then it is
        /// queued.
        case messageQueued, messageTaken
        /// Ends the stream of a reader that fell behind the turn, with no
        /// `done`: the turn runs on, and the reader attaches again
        /// (`agents` 5; before it the same was an `error`).
        case lagged
        /// The `{}` line the server writes while a turn is quiet: every 10 s
        /// from `agents` 5, every 30 s before it.
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
    /// tool twice, so pairing is by id, never by name. On `messageQueued`
    /// and `messageTaken`, the message's own record id.
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
    /// Rides `delta` and `reasoning` from a server whose `agents` feature
    /// is at revision 5: how many Unicode code points of this `turn`'s text
    /// of that kind came before this piece, so a replay is written over
    /// what is held and never beside it. Absent from an older server.
    var at: Int?
    /// Rides `idle`: the thread row's status.
    var status: String?
    /// Rides a background call's `toolFinished`: `working` on the one its
    /// handle answers, then where the job ended on a second one under the
    /// same `id`.
    var job: String?
    /// Rides `error`: why the run failed, from the server's closed set, and
    /// the seconds a provider asked to wait. Absent from an older server.
    var cause: String?
    var retryAfter: Int?
    /// Rides an `error` that is not a failed turn and so has no `cause`:
    /// `auth`, `internal` or `unavailable` (`StreamErrorCode`).
    var code: String?
    /// Ride the tool events from a server whose `agents` feature is at
    /// revision 8, by the server's clock: when the call's own dispatch
    /// started and, on `toolFinished`, ended. On a background job's end
    /// (`job` past `working`) they are the job's. Absent from an older
    /// server.
    var startedAt: Date?
    var finishedAt: Date?

    /// One ndjson line, or nil: a blank line, an unknown kind (a newer
    /// server's) or garbage. The `{}` heartbeat is `.heartbeat`.
    static func parse(_ line: String) -> AgentEvent? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty, case let .object(object)? = JSONValue.parse(trimmed) else { return nil }
        if object.isEmpty { return AgentEvent(kind: .heartbeat) }
        guard case let .string(raw)? = object["kind"], let kind = Kind(rawValue: raw), kind != .heartbeat else { return nil }
        func string(_ key: String) -> String? { if case let .string(s)? = object[key] { s } else { nil } }
        func int(_ key: String) -> Int? { if case .number? = object[key] { object[key]?.int } else { nil } }
        var event = AgentEvent(kind: kind)
        event.thread = string("thread")
        event.text = string("text")
        event.id = string("id")
        event.tool = string("tool")
        event.callable = string("callable")
        event.args = string("args")
        if case let .bool(ok)? = object["ok"] { event.ok = ok }
        event.output = string("output")
        event.tokensBefore = int("tokensBefore")
        event.covered = int("covered")
        event.error = string("error")
        event.turn = int("turn")
        event.at = int("at")
        event.status = string("status")
        event.job = string("job")
        event.cause = string("cause")
        event.retryAfter = int("retryAfter")
        event.code = string("code")
        event.startedAt = string("startedAt").flatMap(Timestamp.parse)
        event.finishedAt = string("finishedAt").flatMap(Timestamp.parse)
        if let result = object["result"] { event.result = try? ValueCoding.decode(AgentResult.self, from: result) }
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
