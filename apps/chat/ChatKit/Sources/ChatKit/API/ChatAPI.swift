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
    /// writes a `{}` heartbeat every 30 seconds, so a stream silent for
    /// longer than this is dead.
    static let streamTimeout: TimeInterval = 75

    /// The chat request's body: `substrate.ChatInput`. The device's zone and
    /// the records the message is about ride along, and the server renders
    /// them into the turn's context (decision 0196).
    static func chatBody(thread: String?, message: String, context: [RecordReference], timezone: String) -> JSONValue {
        var body: JSONObject = ["thread": .string(thread ?? ""), "message": .string(message), "timezone": .string(timezone)]
        if !context.isEmpty { body["context"] = .array(context.map { .string($0.path) }) }
        return .object(body)
    }

    /// Opens or continues a thread with one user message, and streams the run.
    static func chat(_ client: SubstrateClient, agent: String, thread: String?, message: String, timezone: String)
        -> AsyncThrowingStream<AgentEvent, any Error>
    {
        stream(client) {
            try client.request(
                "POST", "api/v1/\(Kinds.agent)/\(segment(agent))/chat",
                body: .jsonValue(chatBody(thread: thread, message: message, context: [], timezone: timezone)),
                accept: "application/x-ndjson", timeout: streamTimeout)
        }
    }

    /// Attaches to the chat turn running on a thread: its replay, then live
    /// events, then `done`; or one `idle` line when no chat turn runs.
    static func events(_ client: SubstrateClient, thread: String) -> AsyncThrowingStream<AgentEvent, any Error> {
        stream(client) {
            try client.request(
                "GET", "api/v1/\(Kinds.thread)/\(segment(thread))/events", accept: "application/x-ndjson",
                timeout: streamTimeout)
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
                        throw SubstrateClient.failure(SubstrateClient.Response(data: body, response: lines.head))
                    }
                    for try await line in lines.lines {
                        guard let event = AgentEvent.parse(line) else { continue }
                        if let failure = event.failure { throw TurnFailed(text: failure) }
                        continuation.yield(event)
                    }
                    continuation.finish()
                } catch {
                    if Task.isCancelled { return continuation.finish() }
                    switch error {
                    case is ServerError, is SessionExpired, is InsufficientScope, is TurnFailed, is TransportError, is Redirected:
                        continuation.finish(throwing: error)
                    default:
                        let detail = explain(error, app: ChatManifest.appName, server: client.server)
                        continuation.finish(
                            throwing: TransportError(
                                text: answered
                                    ? "The connection closed while the answer was read. \(detail)"
                                    : "The connection failed before the server answered. \(detail)"))
                    }
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}

/// A turn that failed after its stream began: the loop's own words.
struct TurnFailed: Error, Sendable {
    let text: String
}
