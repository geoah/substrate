import Foundation
import SubstrateKit

/// The signed-in session: the server, the repository's authority and the
/// bearer secret. An app token (the OAuth door) carries what the owner
/// approved, when it ends and where it is revoked; a password login carries
/// the token's id, so Sign Out revokes the very token it forgets.
struct Session: Codable, Equatable, Sendable {
    var server: URL
    var repository: String
    var secret: String
    var tokenID: String
    /// The approved scopes; nil is a full-access token (a password login).
    var grants: Grants?
    /// Absent when the owner approved the app until they revoke it.
    var expiresAt: Date?
    var revocation: URL?

    /// Which repository on which server: what every cached byte is bound to.
    var identity: String { server.absoluteString.lowercased() + "|" + repository.lowercased() }
}

/// `GET /.well-known/substrate/server.json`, the part the app reads.
struct Discovery: Decodable, Sendable {
    struct Registration: Decodable, Sendable { let totpRequired: Bool? }
    struct Server: Decodable, Sendable { let version: String? }
    let registration: Registration?
    let server: Server?
}

/// A list read of `GET /api/v1/records`: the kinds in `filter.kinds`, the
/// other arms beside them, and the wire's compact `orderBy`.
struct ListQuery: Sendable, Hashable {
    var kinds: [String]
    var properties: [String: JSONValue] = [:]
    var first = 100
    var after: String?
    var orderBy: String?

    var filter: JSONValue {
        var f: [String: JSONValue] = ["kinds": .array(kinds.map(JSONValue.string))]
        if !properties.isEmpty { f["properties"] = .object(properties) }
        return .object(f)
    }

    var queryItems: [(String, String)] {
        var items = [("first", String(first)), ("filter", filter.jsonText)]
        if let after { items.append(("after", after)) }
        if let orderBy { items.append(("orderBy", orderBy)) }
        return items
    }
}

/// The substrate API as this app speaks it. Every request but discovery and
/// login carries the bearer secret. None names an actor, so the server
/// attributes them to `api`: `console` would claim a door this app is not,
/// and a name of its own would lose the owner's annotation namespace.
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

    nonisolated(unsafe) static var session = makeSession(timeout: 30)

    /// The streams never end on their own while a turn runs or the feed is
    /// idle; the server writes a `{}` heartbeat every 30 seconds, so a stream
    /// silent for longer than this is dead.
    nonisolated(unsafe) static var streaming = makeSession(timeout: 75, resource: 7 * 24 * 3600)

    /// The sessions are replaceable for the tests' scripted transport, which
    /// a `URLProtocol` in `protocolClasses` stands in for the server with.
    static func makeSession(timeout: TimeInterval, resource: TimeInterval? = nil,
                            protocolClasses: [AnyClass]? = nil) -> URLSession
    {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = timeout
        if let resource { config.timeoutIntervalForResource = resource }
        config.httpMaximumConnectionsPerHost = 6
        config.waitsForConnectivity = false
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        config.urlCache = nil
        if let protocolClasses { config.protocolClasses = protocolClasses }
        return URLSession(configuration: config)
    }

    /// A server address as typed: a bare host is HTTPS, a local one plain
    /// HTTP, and only the origin is kept.
    static func origin(of typed: String) -> URL? {
        var text = typed.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        if !text.contains("://") {
            let local = text.hasPrefix("localhost") || text.hasPrefix("127.0.0.1") || text.hasPrefix("[::1]")
            text = (local ? "http://" : "https://") + text
        }
        guard var parts = URLComponents(string: text), let scheme = parts.scheme?.lowercased(),
              scheme == "http" || scheme == "https", let host = parts.host, !host.isEmpty
        else { return nil }
        parts.scheme = scheme
        parts.path = ""
        parts.query = nil
        parts.fragment = nil
        parts.user = nil
        parts.password = nil
        return parts.url
    }

    // MARK: Requests

    /// One path segment, `/` included, encoded exactly once: a record id may
    /// carry a slash (an agent's id is `<authority>/<package>/<name>`).
    static func segment(_ text: String) -> String {
        text.addingPercentEncoding(withAllowedCharacters: unreserved) ?? text
    }

    private static let unreserved = CharacterSet(charactersIn:
        "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~")

    func url(_ path: String, query: [(String, String)] = []) throws -> URL {
        var text = server.absoluteString
        while text.hasSuffix("/") { text.removeLast() }
        text += "/" + path
        if !query.isEmpty {
            text += "?" + query.map { "\(Self.segment($0.0))=\(Self.segment($0.1))" }.joined(separator: "&")
        }
        guard let url = URL(string: text) else { throw URLError(.badURL) }
        return url
    }

    func request(_ method: String, _ path: String, query: [(String, String)] = [], json: Any? = nil,
                 accept: String = "application/json", timeout: TimeInterval = 30) throws -> URLRequest
    {
        var request = URLRequest(url: try url(path, query: query))
        request.httpMethod = method
        request.timeoutInterval = timeout
        request.setValue(accept, forHTTPHeaderField: "Accept")
        if let secret { request.setValue("Bearer \(secret)", forHTTPHeaderField: "Authorization") }
        if let json {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: json, options: [.withoutEscapingSlashes])
        }
        return request
    }

    /// Sends a request and answers its body on a 2xx. A 401 is the session
    /// ending, and anything else the server's envelope, or a transport error
    /// when the answer is not the server's.
    @discardableResult
    func send(_ request: URLRequest, accept statuses: Set<Int> = []) async throws -> (Data, Int) {
        let (data, response) = try await Self.session.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        if (200 ..< 300).contains(status) || statuses.contains(status) { return (data, status) }
        if status == 401 { throw SessionExpired() }
        if let error = ServerError(status: status, envelope: data) { throw error }
        throw TransportError(text: "The request ended with HTTP \(status) and no answer from the server.")
    }

    // MARK: The door

    func discover() async throws -> Discovery {
        let req = try request("GET", ".well-known/substrate/server.json", timeout: 20)
        let (data, _) = try await send(req)
        return try JSONDecoder().decode(Discovery.self, from: data)
    }

    struct LoginResult: Decodable, Sendable {
        struct Token: Decodable, Sendable { let id: String }
        let token: Token
        let secret: String
        let repository: String
    }

    func login(repository: String, password: String, totp: String?, label: String) async throws -> LoginResult {
        var body: [String: Any] = ["repository": repository, "password": password, "label": label]
        if let totp, !totp.isEmpty { body["totpCode"] = totp }
        var req = try request("POST", "login", json: body, timeout: 20)
        req.setValue(nil, forHTTPHeaderField: "Authorization")
        let (data, response) = try await Self.session.data(for: req)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        // At the door a 401 is the name, password or code, not a session ending.
        if status == 401 { throw LoginRefused(server: ServerError(status: status, envelope: data)) }
        guard (200 ..< 300).contains(status) else {
            throw ServerError(status: status, envelope: data)
                ?? TransportError(text: "The sign-in ended with HTTP \(status) and no answer from the server.")
        }
        return try JSONDecoder().decode(LoginResult.self, from: data)
    }

    /// Best effort and quick: a sign-out never waits on a server that is gone
    /// or already refuses the token.
    func revoke(token id: String) async throws {
        let req = try request("DELETE", "tokens/\(Self.segment(id))", timeout: 10)
        try await send(req, accept: [404])
    }

    // MARK: Records

    func list(_ query: ListQuery, timeout: TimeInterval = 30) async throws -> Page {
        let req = try request("GET", "api/v1/records", query: query.queryItems, timeout: timeout)
        let (data, _) = try await send(req)
        return try JSONDecoder().decode(Page.self, from: data)
    }

    /// Every page of a read, up to `limit` rows: agents and providers are few,
    /// but a repository may hold more than one page of them.
    func listAll(_ query: ListQuery, limit: Int = 1000) async throws -> Page {
        var first = try await list(query)
        var cursor = first.cursor
        while let next = cursor, first.records.count < limit {
            var q = query
            q.after = next
            let page = try await list(q)
            first.records += page.records
            cursor = page.cursor
        }
        return first
    }

    static func recordPath(_ kind: String, _ id: String) -> String { "api/v1/\(kind)/\(segment(id))" }

    /// One record, or nil when there is none or it was deleted.
    func get(_ kind: String, _ id: String) async throws -> Record? {
        let req = try request("GET", Self.recordPath(kind, id))
        let (data, status) = try await send(req, accept: [404])
        if status == 404 { return nil }
        let record = try JSONDecoder().decode(Record.self, from: data)
        return record.deletedAt == nil ? record : nil
    }

    /// A merge per property; `ifVersion` asserts the version the owner read.
    @discardableResult
    func patch(_ kind: String, _ id: String, properties: [String: Any], ifVersion: Int? = nil,
               annotations: [String: String]? = nil) async throws -> Record
    {
        var body: [String: Any] = ["properties": properties]
        if let ifVersion { body["ifVersion"] = ifVersion }
        if let annotations { body["annotations"] = annotations }
        let req = try request("PATCH", Self.recordPath(kind, id), json: body)
        let (data, _) = try await send(req)
        return try JSONDecoder().decode(Record.self, from: data)
    }

    // MARK: Agents

    /// Stops the chat turn running on a thread; answers once it settled.
    func stop(thread: String) async throws -> AgentResult {
        let req = try request("POST", "api/v1/\(Kinds.thread)/\(Self.segment(thread))/stop", timeout: 45)
        let (data, _) = try await send(req)
        return try JSONDecoder().decode(AgentResult.self, from: data)
    }

    /// Opens or continues a thread with one user message, and streams the run.
    /// The device's zone and the records the message is about ride along, and
    /// the server renders them into the turn's context (decision 0196).
    func chat(agent: String, thread: String?, message: String, context: [RecordReference] = []) -> AsyncThrowingStream<AgentEvent, Error> {
        stream { try request("POST", "api/v1/\(Kinds.agent)/\(Self.segment(agent))/chat",
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
        stream { try request("GET", "api/v1/\(Kinds.thread)/\(Self.segment(thread))/events",
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
                    let status = (response as? HTTPURLResponse)?.statusCode ?? 0
                    if status == 401 { throw SessionExpired() }
                    if status != 200 {
                        var data = Data()
                        for try await byte in bytes {
                            data.append(byte)
                            if data.count > 256 * 1024 { break }
                        }
                        // Without the server's envelope the answer is not the
                        // server's: a gateway's 502 can come after the turn
                        // was admitted.
                        throw ServerError(status: status, envelope: data)
                            ?? TransportError(text: "The request ended with HTTP \(status) and no answer from the server.")
                    }
                    for try await line in bytes.lines {
                        guard let event = AgentEvent.parse(line) else { continue }
                        if let failure = event.failure { throw TurnFailed(text: failure) }
                        continuation.yield(event)
                    }
                    continuation.finish()
                } catch {
                    if Task.isCancelled { return continuation.finish() }
                    switch error {
                    case is ServerError, is SessionExpired, is TurnFailed, is TransportError:
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
