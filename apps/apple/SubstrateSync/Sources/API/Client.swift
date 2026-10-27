import Foundation

/// A refusal from the server, carrying its own words: the `message` and
/// `problems` of the error envelope, shown to the owner verbatim.
struct ServerError: LocalizedError, Sendable {
    let status: Int
    let text: String

    var errorDescription: String? { text }

    init(status: Int, data: Data) {
        self.status = status
        if let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
           let error = object["error"] as? [String: Any] {
            var lines = [(error["message"] as? String) ?? "The server answered \(status)."]
            lines += (error["problems"] as? [String]) ?? []
            text = lines.joined(separator: "\n")
        } else if let body = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines),
                  !body.isEmpty, body.count < 500 {
            text = "The server answered \(status): \(body)"
        } else {
            text = "The server answered \(status)."
        }
    }
}

/// `GET /.well-known/substrate/server.json`, the part the app reads.
struct Discovery: Decodable, Sendable {
    struct Registration: Decodable, Sendable {
        let totpRequired: Bool?
    }

    struct Server: Decodable, Sendable {
        let version: String?
    }

    let registration: Registration?
    let server: Server?
}

struct LoginResult: Decodable, Sendable {
    struct Token: Decodable, Sendable {
        let id: String
    }

    let token: Token
    let secret: String
    let repository: String
}

struct CatalogItem: Decodable, Sendable {
    let id: String
    let version: Int?
    let installed: Bool
    let upgrade: Upgrade?

    struct Upgrade: Decodable, Sendable {}
}

/// The substrate API as this app speaks it. Every call but discovery and
/// login carries the session's bearer secret.
struct Client: Sendable {
    let server: URL
    var secret: String?

    static let session: URLSession = {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 60
        config.httpMaximumConnectionsPerHost = 8
        config.waitsForConnectivity = false
        return URLSession(configuration: config)
    }()

    /// A server address as typed: a bare host is HTTPS, localhost plain
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

    func discover() async throws -> Discovery {
        let (data, status) = try await send("GET", path: ".well-known/substrate/server.json", authorized: false)
        guard status == 200 else { throw ServerError(status: status, data: data) }
        guard (try? JSONSerialization.jsonObject(with: data)) is [String: Any] else {
            throw URLError(.cannotParseResponse)
        }
        return try JSONDecoder().decode(Discovery.self, from: data)
    }

    func login(repository: String, password: String, totp: String?, label: String) async throws -> LoginResult {
        var body: [String: Any] = ["repository": repository, "password": password, "label": label]
        if let totp, !totp.isEmpty { body["totpCode"] = totp }
        let (data, status) = try await send("POST", path: "login", json: body, authorized: false)
        guard status == 201 || status == 200 else { throw ServerError(status: status, data: data) }
        return try JSONDecoder().decode(LoginResult.self, from: data)
    }

    /// Best effort and quick: a sign-out never waits on a server that is
    /// gone or refuses the token anyway.
    func revoke(token id: String) async throws {
        let (data, status) = try await send("DELETE", path: "tokens/\(escape(id))", retries: 0, timeout: 10)
        guard (200 ..< 300).contains(status) || status == 404 else { throw ServerError(status: status, data: data) }
    }

    func catalog() async throws -> [CatalogItem] {
        struct Page: Decodable { let items: [CatalogItem] }
        let (data, status) = try await send("GET", path: "api/v1/catalog")
        guard status == 200 else { throw ServerError(status: status, data: data) }
        return try JSONDecoder().decode(Page.self, from: data).items
    }

    /// `install` for a provider (also its upgrade), `import` for a sample.
    func catalog(_ verb: String, _ id: String) async throws {
        let (data, status) = try await send("POST", path: "api/v1/catalog/\(escape(id))/\(verb)", json: [String: Any]())
        guard (200 ..< 300).contains(status) else { throw ServerError(status: status, data: data) }
    }

    /// One live record's properties, or nil when there is none: a deleted
    /// record answers 200 with `deletedAt`, and reads as absent here.
    func get(_ kind: String, _ id: String) async throws -> [String: Any]? {
        let (data, status) = try await send("GET", path: recordPath(kind, id))
        if status == 404 { return nil }
        guard status == 200 else { throw ServerError(status: status, data: data) }
        let object = try JSONSerialization.jsonObject(with: data) as? [String: Any]
        if let deleted = object?["deletedAt"], !(deleted is NSNull) { return nil }
        return object?["properties"] as? [String: Any] ?? [:]
    }

    /// PUT merges per property and never prunes.
    func put(_ kind: String, _ id: String, _ properties: Any) async throws {
        let (data, status) = try await send("PUT", path: recordPath(kind, id), json: ["properties": properties])
        guard (200 ..< 300).contains(status) else { throw ServerError(status: status, data: data) }
    }

    /// Clears keys: each one set to null.
    func clear(_ kind: String, _ id: String, keys: [String]) async throws {
        guard !keys.isEmpty else { return }
        let nulls = Dictionary(uniqueKeysWithValues: keys.map { ($0, NSNull() as Any) })
        let (data, status) = try await send("PATCH", path: recordPath(kind, id), json: ["properties": nulls])
        guard (200 ..< 300).contains(status) else { throw ServerError(status: status, data: data) }
    }

    func delete(_ kind: String, _ id: String) async throws {
        let (data, status) = try await send("DELETE", path: recordPath(kind, id))
        guard (200 ..< 300).contains(status) || status == 404 else { throw ServerError(status: status, data: data) }
    }

    /// One Health delivery to the webhook door, answering the fire id it was
    /// stored under. A 202 means stored, not filed: the provider's function
    /// runs after it, and writes its receipt at that id or parks the
    /// delivery (`parked`), so a caller confirms one or the other.
    @discardableResult
    func deliver(authority: String, trigger: String, body: Data, headers: [(String, String)]) async throws -> String {
        var request = URLRequest(url: server.appendingPathComponent("webhooks/\(authority)/\(trigger)"))
        request.httpMethod = "POST"
        request.timeoutInterval = 60
        for (name, value) in headers { request.setValue(value, forHTTPHeaderField: name) }
        request.httpBody = body
        let (data, status) = try await perform(request)
        guard status == 202 || status == 200 else { throw ServerError(status: status, data: data) }
        struct Accepted: Decodable { let fire: String }
        return (try? JSONDecoder().decode(Accepted.self, from: data))?.fire ?? ""
    }

    /// A delivery the trigger's function refused, kept by the server with
    /// its reason.
    struct Parked: Decodable, Sendable {
        let fireId: String?
        let lastError: String
    }

    func parked(trigger: String) async throws -> [Parked] {
        struct Page: Decodable { let items: [Parked] }
        let (data, status) = try await send("GET", path: "api/v1/substrate.reamde.dev/core/trigger/\(escape(trigger))/parked")
        guard status == 200 else { throw ServerError(status: status, data: data) }
        return try JSONDecoder().decode(Page.self, from: data).items
    }

    private func recordPath(_ kind: String, _ id: String) -> String {
        "api/v1/\(kind)/\(escape(id))"
    }

    private func escape(_ segment: String) -> String {
        var allowed = CharacterSet.urlPathAllowed
        allowed.remove(charactersIn: "/")
        return segment.addingPercentEncoding(withAllowedCharacters: allowed) ?? segment
    }

    private func send(
        _ method: String, path: String, json: Any? = nil, authorized: Bool = true, retries: Int = 3, timeout: TimeInterval = 60
    ) async throws -> (Data, Int) {
        // The path may carry a percent-encoded segment, so it is joined as
        // text rather than through appendingPathComponent, which re-encodes.
        guard let url = URL(string: server.absoluteString.trimmingSuffix("/") + "/" + path) else {
            throw URLError(.badURL)
        }
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.timeoutInterval = timeout
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if authorized, let secret { request.setValue("Bearer \(secret)", forHTTPHeaderField: "Authorization") }
        if let json {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: json, options: [.withoutEscapingSlashes])
        }
        return try await perform(request, retries: retries)
    }

    /// Sends a request, waiting out a busy server: the webhook door answers
    /// 503 with more than eight deliveries in flight across every sender.
    private func perform(_ request: URLRequest, retries: Int = 3) async throws -> (Data, Int) {
        var attempt = 0
        while true {
            let (data, response) = try await Self.session.data(for: request)
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            // A busy server says when to come back; three tries, then the
            // refusal stands.
            if status == 503 || status == 429, attempt < retries {
                attempt += 1
                let after = (response as? HTTPURLResponse)?.value(forHTTPHeaderField: "Retry-After").flatMap(Double.init) ?? 2
                try await Task.sleep(for: .seconds(min(after, 30)))
                continue
            }
            return (data, status)
        }
    }
}

extension String {
    func trimmingSuffix(_ suffix: String) -> String {
        var s = self
        while s.hasSuffix(suffix) { s.removeLast(suffix.count) }
        return s
    }
}
