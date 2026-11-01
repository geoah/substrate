import Foundation
import SubstrateKit

/// The substrate API as the chat speaks it: the kit's client, with the
/// session's bearer secret, for every request and its errors, and beside it
/// what only the chat reads, the agent streams and the change feed. None
/// names an actor, so the server records an app token's writes as
/// `app:<host>` and a password sign-in's as `api`: `console` would claim a
/// door this app is not.
struct API: Sendable {
    let server: URL
    var secret: String?

    init(server: URL, secret: String? = nil) {
        self.server = server
        self.secret = secret
    }

    init(_ session: Session) {
        server = session.server
        secret = session.secret
    }

    nonisolated(unsafe) static var session = URLSessionTransport.makeSession(timeout: 30)

    /// The streams never end on their own while a turn runs or the feed is
    /// idle; the server writes a `{}` heartbeat every 30 seconds, so a stream
    /// silent for longer than this is dead.
    nonisolated(unsafe) static var streaming = URLSessionTransport.makeSession(timeout: 75, resource: 7 * 24 * 3600)

    /// The kit's client over the app's session, read at each call: a test's
    /// scripted transport replaces the session in `protocolClasses`.
    var client: SubstrateClient {
        SubstrateClient(server: server, token: secret, transport: URLSessionTransport(session: Self.session))
    }

    /// A request for one of the streams, which the kit's transport does not
    /// read line by line.
    func request(_ method: String, _ path: String, query: [(String, String)] = [], json: Any? = nil,
                 accept: String, timeout: TimeInterval) throws -> URLRequest
    {
        let body: RequestBody = try json.map { .json(try JSONSerialization.data(withJSONObject: $0, options: [.withoutEscapingSlashes])) } ?? .none
        return try client.request(method, path, query: query, body: body, accept: accept, timeout: timeout)
    }

    /// A stream's refusal, as the kit says any other: its first bytes read
    /// for the server's envelope.
    static func refusal(_ bytes: URLSession.AsyncBytes, _ response: HTTPURLResponse, limit: Int) async throws -> Error {
        var data = Data()
        for try await byte in bytes {
            data.append(byte)
            if data.count > limit { break }
        }
        return SubstrateClient.failure(SubstrateClient.Response(data: data, response: response))
    }

    // MARK: Records

    func list(_ query: ListQuery, timeout: TimeInterval = SubstrateClient.Timeout.read) async throws -> RecordPage {
        try await client.list(query, timeout: timeout)
    }

    /// Every page of a read, up to `limit` rows: agents and providers are few,
    /// but a repository may hold more than one page of them.
    func listAll(_ query: ListQuery, limit: Int = 1000) async throws -> RecordPage {
        try await client.listAll(query, limit: limit)
    }

    /// One record, or nil when there is none or it was deleted.
    func get(_ kind: String, _ id: String) async throws -> Record? {
        try await client.record(kind, id)
    }

    /// A merge per property; `ifVersion` asserts the version the owner read,
    /// and `annotations` ride along with the write.
    @discardableResult
    func patch(_ kind: String, _ id: String, properties: [String: Any], ifVersion: Int? = nil,
               annotations: [String: String]? = nil) async throws -> Record
    {
        var body: [String: Any] = ["properties": properties]
        if let ifVersion { body["ifVersion"] = ifVersion }
        if let annotations { body["annotations"] = annotations }
        let json = try JSONSerialization.data(withJSONObject: body, options: [.withoutEscapingSlashes])
        return try await client.send("PATCH", SubstrateClient.recordPath(kind, id), body: .json(json)).decode(Record.self)
    }

    // MARK: Agents

    /// Stops the chat turn running on a thread; answers once it settled.
    func stop(thread: String) async throws -> AgentResult {
        try await client.send("POST", "api/v1/\(Kinds.thread)/\(segment(thread))/stop", timeout: 45).decode(AgentResult.self)
    }

    /// Opens or continues a thread with one user message, and streams the run.
    /// The device's zone and the records the message is about ride along, and
    /// the server renders them into the turn's context (decision 0196).
    func chat(agent: String, thread: String?, message: String, context: [RecordReference] = []) -> AsyncThrowingStream<AgentEvent, Error> {
        stream { try request("POST", "api/v1/\(Kinds.agent)/\(segment(agent))/chat",
                             json: Self.chatBody(thread: thread, message: message, context: context),
                             accept: "application/x-ndjson", timeout: 75) }
    }

    /// The chat request's body: `substrate.ChatInput`.
    static func chatBody(thread: String?, message: String, context: [RecordReference],
                         timezone: String = TimeZone.current.identifier) -> [String: Any]
    {
        var body: [String: Any] = ["thread": thread ?? "", "message": message, "timezone": timezone]
        if !context.isEmpty { body["context"] = context.map(\.path) }
        return body
    }

    /// Attaches to the chat turn running on a thread: its replay, then live
    /// events, then `done`; or one `idle` line when no chat turn runs.
    func events(thread: String) -> AsyncThrowingStream<AgentEvent, Error> {
        stream { try request("GET", "api/v1/\(Kinds.thread)/\(segment(thread))/events",
                             accept: "application/x-ndjson", timeout: 75) }
    }

    /// Reads an ndjson agent stream to its end. A post-200 loop failure (an
    /// `error` event, or from an older server a `done` with text and no
    /// result) finishes the stream with `TurnFailed`, never as a quiet end,
    /// so a caller never settles a blank reply over an error.
    private func stream(_ make: @escaping @Sendable () throws -> URLRequest) -> AsyncThrowingStream<AgentEvent, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                var answered = false
                do {
                    let (bytes, response) = try await Self.streaming.bytes(for: try make())
                    answered = true
                    guard let http = response as? HTTPURLResponse else { throw URLError(.badServerResponse) }
                    // Without the server's envelope the answer is not the
                    // server's: a gateway's 502 can come after the turn was
                    // admitted.
                    if http.statusCode != 200 { throw try await Self.refusal(bytes, http, limit: 256 * 1024) }
                    for try await line in bytes.lines {
                        guard let event = AgentEvent.parse(line) else { continue }
                        if let failure = event.failure { throw TurnFailed(text: failure) }
                        continuation.yield(event)
                    }
                    continuation.finish()
                } catch {
                    if Task.isCancelled { return continuation.finish() }
                    switch error {
                    case is ServerError, is SessionExpired, is InsufficientScope, is TurnFailed, is TransportError:
                        continuation.finish(throwing: error)
                    default:
                        let detail = explain(error, server: server)
                        continuation.finish(throwing: TransportError(text: answered
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
struct TurnFailed: LocalizedError, Sendable {
    let text: String
    var errorDescription: String? { text }
}
