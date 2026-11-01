import Foundation

/// A password sign-in's answer: the token record minted, its secret, and
/// the repository's authority.
public struct LoginResult: Decodable, Sendable, Equatable {
    public struct Token: Decodable, Sendable, Equatable {
        public let id: String
    }

    public let token: Token
    public let secret: String
    public let repository: String
}

/// A blob as stored: its digest, the value a `blobref` property takes, and
/// its size.
public struct StoredBlob: Decodable, Sendable, Equatable {
    public let digest: String
    public let size: Int

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        digest = try c.decode(String.self, forKey: .digest)
        size = c.value(.size, or: 0)
    }

    enum CodingKeys: String, CodingKey { case digest, size }
}

/// One row of a webhook trigger's parked list: a delivery its function
/// refused, kept with its reason, or one accepted and not settled yet,
/// which the list carries too and which is no failure.
public struct ParkedDelivery: Decodable, Sendable, Equatable {
    public let fireID: String?
    public let lastError: String
    public let running: Bool

    enum CodingKeys: String, CodingKey {
        case fireID = "fireId"
        case lastError, running
    }

    public init(fireID: String?, lastError: String, running: Bool = false) {
        self.fireID = fireID
        self.lastError = lastError
        self.running = running
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        fireID = c.value(.fireID, or: nil)
        lastError = c.value(.lastError, or: "")
        running = c.value(.running, or: false)
    }

    /// The server's words for a delivery it accepted and has not settled
    /// (internal/engine's webhooks.go pendingWebhookError).
    public static let pendingPrefix = "delivery accepted:"

    /// Refused for good: neither waiting to run nor running.
    public var refused: Bool { !running && !lastError.hasPrefix(Self.pendingPrefix) }
}

public extension SubstrateClient {
    // MARK: Discovery

    /// `GET /.well-known/substrate/server.json`. An answer that is not a JSON
    /// object is not a substrate's, whatever its status.
    func discovery() async throws -> Discovery {
        let response = try await send(request("GET", ".well-known/substrate/server.json", timeout: Timeout.quick, authorized: false))
        guard (try? JSONSerialization.jsonObject(with: response.data)) is [String: Any] else {
            throw URLError(.cannotParseResponse)
        }
        return try response.decode(Discovery.self)
    }

    /// `GET /.well-known/oauth-authorization-server`; nil when the server
    /// predates app authorization (a 404) and has only the password door.
    func authorizationServerMetadata() async throws -> AuthorizationServerMetadata? {
        let response = try await send(request("GET", ".well-known/oauth-authorization-server", timeout: Timeout.quick,
                                              authorized: false), accept: [404])
        if response.status == 404 { return nil }
        return try response.decode(AuthorizationServerMetadata.self)
    }

    // MARK: The password door

    /// `POST /login`, for a server without app authorization. At the door a
    /// 401 is the name, the password or the code, not a sign-in ending.
    func login(repository: String, password: String, totp: String?, label: String) async throws -> LoginResult {
        var body: [String: JSONValue] = ["repository": .string(repository), "password": .string(password), "label": .string(label)]
        if let totp, !totp.isEmpty { body["totpCode"] = .string(totp) }
        let req = try request("POST", "login", body: .jsonValue(.object(body)), timeout: Timeout.quick, authorized: false)
        do {
            return try await send(req).decode(LoginResult.self)
        } catch let expired as SessionExpired {
            throw LoginRefused(server: expired.message.map { ServerError(status: 401, message: $0) })
        }
    }

    /// `DELETE /tokens/{id}`: a password session's own token. Best effort and
    /// quick: a sign-out never waits on a server that is gone or already
    /// refuses the token, and one already revoked is not an error.
    func revokeToken(id: String) async throws {
        try await send(request("DELETE", "tokens/\(segment(id))", timeout: 10), accept: [404], retries: 0)
    }

    // MARK: Blobs

    /// Stores bytes as a blob and answers its digest. The record naming it
    /// must follow within a minute: the server collects a blob nothing
    /// references after that. An app's token stores one when its approval
    /// writes a kind with a `blobref` property.
    func putBlob(_ data: Data, mediaType: String, name: String? = nil) async throws -> StoredBlob {
        let req = try request("PUT", "api/v1/blobs", query: name.map { [("name", $0)] } ?? [],
                              body: .bytes(data, contentType: mediaType), timeout: Timeout.upload)
        return try await send(req).decode(StoredBlob.self)
    }

    // MARK: Webhooks

    /// One delivery to `POST /webhooks/{authority}/{trigger}`, answering the
    /// fire id it was stored under. A 202 means stored, not filed: the
    /// trigger's function runs after it and files the delivery or parks it.
    /// The client's token is the credential (it holds `webhook:<trigger>`);
    /// `credential` names another, such as a trigger's own key under a
    /// password session. The door answers 503 while eight deliveries are in
    /// flight across every sender, before it reads one, so a busy door is
    /// waited out and the delivery sent again.
    @discardableResult
    func deliver(authority: String, trigger: String, body: Data, contentType: String = "application/json",
                 headers: [String: String] = [:], credential: String? = nil, retries: Int = 8) async throws -> String
    {
        var req = try request("POST", "webhooks/\(segment(authority))/\(segment(trigger))",
                              body: .bytes(body, contentType: contentType), timeout: Timeout.upload)
        for (name, value) in headers { req.setValue(value, forHTTPHeaderField: name) }
        if let credential { req.setValue("Bearer \(credential)", forHTTPHeaderField: "Authorization") }
        let response = try await send(req, retries: retries, longestWait: 30)
        struct Accepted: Decodable { let fire: String }
        guard let fire = try? response.decode(Accepted.self).fire, !fire.isEmpty else { throw URLError(.cannotParseResponse) }
        return fire
    }

    /// A webhook trigger's parked deliveries, every sender's: readable with
    /// `webhook:<trigger>`.
    func parked(trigger: String, timeout: TimeInterval = Timeout.read) async throws -> [ParkedDelivery] {
        struct Page: Decodable { let items: [ParkedDelivery] }
        return try await send("GET", "api/v1/substrate.reamde.dev/core/trigger/\(segment(trigger))/parked", timeout: timeout,
                              retries: 0).decode(Page.self).items
    }

    // MARK: Authorizations

    /// `POST /api/v1/authorizations/check`: what the token lacks of the
    /// request the app would send now.
    func checkAuthorization(_ check: AuthorizationCheck) async throws -> AuthorizationCheckResult {
        try await send("POST", "api/v1/authorizations/check", body: .encoding(check), timeout: Timeout.quick)
            .decode(AuthorizationCheckResult.self)
    }
}
