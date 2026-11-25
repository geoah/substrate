import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// What only the chat asks of a substrate, over the kit's client: the agent
/// streams, read line by line through the transport, a turn's stop, and the
/// reads a conversation makes for itself. Nothing names an actor, so the
/// server records an app token's writes as `app:<host>` and a password
/// sign-in's as `api`: `console` would claim a door this app is not.
enum ChatAPI {
    /// The streams never end on their own while a turn runs; the server
    /// writes a `{}` heartbeat while one is quiet, so a stream silent for
    /// longer than its caller's `quiet` is dead (`Conversation.quiet`: past
    /// two heartbeats of the server it talks to).

    /// The chat request's body: `substrate.ChatInput`. The device's zone and
    /// the records the message is about ride along, and the server renders
    /// them into the turn's context (decision 0196). `messageID` is the id
    /// the app chose for the message's record: a request that repeats it
    /// writes nothing and runs nothing. It is left out where the server's
    /// document does not say it takes one, since an older server refuses a
    /// body field it does not know.
    ///
    /// `repliesTo` is the id of questions of this thread that wait, which
    /// the message answers in the person's own words (`agents` 7, decision
    /// 0327): the server marks them replied in the write of the message,
    /// and the turn that reads the message is the only one. It is left out
    /// below that revision for the same reason.
    static func chatBody(
        thread: String?, message: String, messageID: String? = nil, repliesTo: String? = nil, context: [RecordReference], timezone: String
    ) -> JSONValue {
        var body: JSONObject = ["thread": .string(thread ?? ""), "message": .string(message), "timezone": .string(timezone)]
        if let messageID { body["messageId"] = .string(messageID) }
        if let repliesTo { body["repliesTo"] = .string(repliesTo) }
        if !context.isEmpty { body["context"] = .array(context.map { .string($0.path) }) }
        return .object(body)
    }

    /// Opens or continues a thread with one user message, and streams the
    /// run. Under a `messageID`, the same request again streams the turn
    /// the first one started, or says how its thread stands.
    static func chat(
        _ client: SubstrateClient, agent: String, thread: String?, message: String, messageID: String? = nil, repliesTo: String? = nil,
        context: [RecordReference] = [], timezone: String, quiet: TimeInterval
    )
        -> AsyncThrowingStream<AgentEvent, any Error>
    {
        stream(client) {
            try client.request(
                "POST", "api/v1/\(Kinds.agent)/\(segment(agent))/chat",
                body: .jsonValue(
                    chatBody(thread: thread, message: message, messageID: messageID, repliesTo: repliesTo, context: context, timezone: timezone)),
                accept: "application/x-ndjson", timeout: quiet)
        }
    }

    /// The message a send wrote under the id it chose, as the server holds
    /// it: nil where no record has the id, or the record is not a message
    /// somebody sent.
    static func sent(_ client: SubstrateClient, messageID: String) async throws -> (thread: String, record: Record)? {
        guard let record = try await client.record(Kinds.message, messageID), record.deletedAt == nil,
            record["role"]?.string == "user", let thread = record["thread"]?.referenceID
        else { return nil }
        return (thread, record)
    }

    /// Runs a thread again with no new message (`…/continue`): what Try
    /// Again and Continue ask for. A server older than the route answers
    /// 404, which the conversation falls back on.
    static func resume(_ client: SubstrateClient, thread: String, quiet: TimeInterval) -> AsyncThrowingStream<AgentEvent, any Error> {
        stream(client) {
            try client.request(
                "POST", "api/v1/\(Kinds.thread)/\(segment(thread))/continue", body: .jsonValue(.object([:])),
                accept: "application/x-ndjson", timeout: quiet)
        }
    }

    /// Attaches to the chat turn running on a thread: its replay, then live
    /// events, then `done`; or one `idle` line when no chat turn runs.
    static func events(_ client: SubstrateClient, thread: String, quiet: TimeInterval) -> AsyncThrowingStream<AgentEvent, any Error> {
        stream(client) {
            try client.request(
                "GET", "api/v1/\(Kinds.thread)/\(segment(thread))/events", accept: "application/x-ndjson",
                timeout: quiet)
        }
    }

    /// Stops the chat turn running on a thread; answers once it settled.
    static func stop(_ client: SubstrateClient, thread: String) async throws {
        try await client.send("POST", "api/v1/\(Kinds.thread)/\(segment(thread))/stop", timeout: 45)
    }

    static func transcriptQuery(_ thread: String, first: Int = 500) -> ListQuery {
        ListQuery(kinds: [Kinds.message], properties: ["thread": ["eq": .string(thread)]], first: first, orderBy: "turn:desc,createdAt:desc")
    }

    /// A thread and its newest rows as they stand now, read straight from
    /// the server: a conversation's own read (handover, attach, a delivery
    /// check). The thread first: a turn claims its thread before it writes
    /// its user row, so a thread that was not running holds every row it
    /// admitted.
    static func readNow(_ client: SubstrateClient, thread: String) async throws -> (thread: Record?, rows: [Record]) {
        let record = try await client.record(Kinds.thread, thread)
        let page = try await client.list(transcriptQuery(thread))
        return (record, page.records.reversed())
    }

    /// Reads an ndjson agent stream to its end. A failure after the 200 (an
    /// `error` event, or from an older server a `done` with text and no
    /// result) finishes the stream with `TurnFailed`, never as a quiet end,
    /// so a caller never settles a blank reply over an error.
    private static func stream(_ client: SubstrateClient, _ make: @escaping @Sendable () throws -> URLRequest)
        -> AsyncThrowingStream<AgentEvent, any Error>
    {
        AsyncThrowingStream { continuation in
            let task = Task {
                var answered = false
                do {
                    let lines = try await client.transport.lines(for: make())
                    answered = true
                    // Without the server's envelope the answer is not the
                    // server's: a gateway's 502 can come after the turn was
                    // admitted.
                    if lines.head.statusCode != 200 {
                        let body = try await lines.body(limit: 256 * 1024)
                        throw ChatRefusal.reading(
                            SubstrateClient.failure(SubstrateClient.Response(data: body, response: lines.head)), body: body,
                            retryAfter: lines.head.value(forHTTPHeaderField: "Retry-After"))
                    }
                    for try await line in lines.lines {
                        guard let event = AgentEvent.parse(line) else { continue }
                        if let failure = event.failure {
                            throw TurnFailed(text: failure, cause: event.cause, retryAfter: event.retryAfter, code: event.code)
                        }
                        continuation.yield(event)
                    }
                    continuation.finish()
                } catch {
                    if Task.isCancelled { return continuation.finish() }
                    switch error {
                    case is ServerError, is ChatRefusal, is SessionExpired, is InsufficientScope, is TurnFailed, is TransportError, is Redirected:
                        continuation.finish(throwing: error)
                    default:
                        continuation.finish(throwing: StreamBroke(answered: answered, underlying: error))
                    }
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}

/// A stream that ended in an `error` event after it began: the server's own
/// words, the `cause` and `retryAfter` a server that names causes sends
/// with a failed turn, and the `code` of an error that is not one.
struct TurnFailed: Error, Sendable {
    let text: String
    var cause: String?
    var retryAfter: Int?
    var code: String?
}

/// The `code` of an `error` event that is not a failed turn and carries no
/// `cause`.
enum StreamErrorCode: String, CaseIterable, Sendable {
    /// The sign-in ended while the stream was open.
    case auth
    /// The server panicked.
    case `internal`
    /// This client read too slowly and was cut off; the turn runs on.
    case unavailable
}
