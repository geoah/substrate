import Foundation

/// The token endpoint's answer (RFC 6749 §5.1, with RFC 9396's
/// `authorization_details` listing what the approval set up).
public struct TokenResponse: Decodable, Sendable, Equatable {
    public let accessToken: String
    public let tokenType: String?
    public let scope: String
    /// Absent when the owner approved the app until they revoke it.
    public let expiresIn: Int?
    public let repository: String
    /// Each package with the version now held, each account's kind and id,
    /// each webhook's trigger and the URL to deliver to.
    public let authorizationDetails: [AuthorizationDetail]

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
        case tokenType = "token_type"
        case scope
        case expiresIn = "expires_in"
        case repository
        case authorizationDetails = "authorization_details"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        accessToken = try c.decode(String.self, forKey: .accessToken)
        tokenType = c.value(.tokenType, or: nil)
        scope = c.value(.scope, or: "")
        expiresIn = c.value(.expiresIn, or: nil)
        repository = try c.decode(String.self, forKey: .repository)
        authorizationDetails = c.value(.authorizationDetails, or: [])
    }

    public var scopes: [String] { scope.split(separator: " ").map(String.init) }
}

/// The authorization server's refusal: RFC 6749's error object, or the
/// redirect's `error`.
public struct OAuthRefusal: LocalizedError, Sendable, Equatable {
    public let code: String
    public let text: String?

    public init(code: String, text: String?) {
        self.code = code
        self.text = text
    }

    public var errorDescription: String? { text ?? "The server refused the sign-in (\(code))." }

    /// The owner chose Deny.
    public var denied: Bool { code == "access_denied" }
}

/// The substrate's app authorization (decision 0194) for one app and one
/// server: OAuth 2.1's code flow with PKCE for a public client whose
/// `client_id` is a URL, plus the details its approval sets up. The owner
/// signs in to their own substrate in the browser and approves what the app
/// may do; the app never sees a password.
public struct OAuthClient: Sendable {
    public var manifest: AppManifest
    public var metadata: AuthorizationServerMetadata
    public var transport: any HTTPTransport

    public init(manifest: AppManifest, metadata: AuthorizationServerMetadata,
                transport: any HTTPTransport = URLSessionTransport.shared)
    {
        self.manifest = manifest
        self.metadata = metadata
        self.transport = transport
    }

    /// The client for a server a probe found; nil when it has no app
    /// authorization.
    public init?(manifest: AppManifest, probe: ServerProbe.Result, transport: any HTTPTransport = URLSessionTransport.shared) {
        guard let metadata = probe.metadata else { return nil }
        self.init(manifest: manifest, metadata: metadata, transport: transport)
    }

    /// The server's metadata; nil when it predates app authorization.
    public static func metadata(server: URL, transport: any HTTPTransport = URLSessionTransport.shared) async throws
        -> AuthorizationServerMetadata?
    {
        try await SubstrateClient(server: server, transport: transport).authorizationServerMetadata()
    }

    /// The page the owner approves on. `authorization_details` is the
    /// manifest's details as compact JSON, sent only to a server that lists
    /// every type they use.
    public func authorizeURL(redirect: String, pkce: PKCE, state: String) throws -> URL {
        var items = [
            ("response_type", "code"),
            ("client_id", manifest.clientID),
            ("redirect_uri", redirect),
            ("scope", manifest.scope),
            ("code_challenge", pkce.challenge),
            ("code_challenge_method", "S256"),
            ("state", state),
        ]
        let details = manifest.details(for: metadata)
        if !details.isEmpty { items.append(("authorization_details", try AuthorizationDetail.json(details))) }
        guard var parts = URLComponents(url: metadata.authorizationEndpoint, resolvingAgainstBaseURL: false) else {
            throw URLError(.badURL)
        }
        parts.percentEncodedQueryItems = items.map { URLQueryItem(name: segment($0.0), value: segment($0.1)) }
        guard let url = parts.url else { throw URLError(.badURL) }
        return url
    }

    /// The redirect's query, checked: the state this flow sent, the issuer it
    /// began on (RFC 9207), required when the server says it sends one, and
    /// a code or the owner's refusal.
    public func callback(_ query: [String: String], state: String) throws -> String {
        guard query["state"] == state else {
            throw OAuthRefusal(code: "invalid_state", text: "The sign-in came back for another request. Try again.")
        }
        if let iss = query["iss"] {
            guard iss == metadata.issuer else {
                throw OAuthRefusal(code: "invalid_issuer", text: "The sign-in came back from \(iss), not \(metadata.issuer).")
            }
        } else if metadata.authorizationResponseISSParameterSupported {
            throw OAuthRefusal(code: "invalid_issuer", text: "The sign-in came back without naming its server. Try again.")
        }
        if let error = query["error"] {
            if error == "access_denied" { throw OAuthRefusal(code: error, text: "Access was not granted.") }
            throw OAuthRefusal(code: error, text: query["error_description"])
        }
        guard let code = query["code"], !code.isEmpty else {
            throw OAuthRefusal(code: "invalid_request", text: "The sign-in came back without a code.")
        }
        return code
    }

    /// Trades the code for the token. Never sent twice: a code is spent on
    /// first use, and a replay revokes the token the first use minted.
    public func exchange(code: String, redirect: String, pkce: PKCE) async throws -> TokenResponse {
        let request = Self.form(metadata.tokenEndpoint, [
            ("grant_type", "authorization_code"), ("code", code), ("client_id", manifest.clientID),
            ("redirect_uri", redirect), ("code_verifier", pkce.verifier),
        ], timeout: SubstrateClient.Timeout.quick)
        let (data, response) = try await transport.data(for: request)
        guard response.statusCode == 200 else { throw Self.refusal(status: response.statusCode, data: data) }
        return try JSONDecoder().decode(TokenResponse.self, from: data)
    }

    /// RFC 7009: answers 200 whether or not the token existed. Best effort
    /// and quick, so a sign-out never waits on a server that is gone.
    public func revoke(_ token: String) async throws {
        _ = try await transport.data(for: Self.form(metadata.revocation, [("token", token), ("client_id", manifest.clientID)],
                                                    timeout: 10))
    }

    /// Revokes an app session's token where and as it was approved: its own
    /// revocation endpoint under the client id it holds, whatever the build
    /// at hand calls itself now, since the server answers 200 to a token sent
    /// under another client id and leaves it live. A password session's token
    /// is revoked with `SubstrateClient.revokeToken(id:)` instead.
    public static func revoke(_ session: Session, transport: any HTTPTransport = URLSessionTransport.shared) async throws {
        guard let clientID = session.clientID else { return }
        let endpoint = session.revocation
            ?? URL(string: session.server.absoluteString.trimmingTrailingSlashes + "/oauth/revoke") ?? session.server
        _ = try await transport.data(for: form(endpoint, [("token", session.secret), ("client_id", clientID)], timeout: 10))
    }

    /// The whole flow: a loopback receiver for the redirect, the browser
    /// sheet on the server's own sign-in and approval page, the redirect
    /// checked and the code exchanged. The session it answers is not saved.
    @MainActor
    public func signIn(server: URL, now: Date = Date()) async throws -> Session {
        let receiver = try await LoopbackReceiver.start(appName: manifest.appName, path: manifest.redirectPath)
        defer { receiver.stop() }
        let pkce = try PKCE()
        let state = try PKCE.state()
        let redirect = receiver.redirect
        let url = try authorizeURL(redirect: redirect, pkce: pkce, state: state)
        let query = try await WebAuthenticator().run(url, receiver: receiver)
        let code = try callback(query, state: state)
        let token = try await exchange(code: code, redirect: redirect, pkce: pkce)
        return Session(server: server, token: token, clientID: manifest.clientID, revocation: metadata.revocation, now: now)
    }

    private static func form(_ url: URL, _ pairs: [(String, String)], timeout: TimeInterval) -> URLRequest {
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.timeoutInterval = timeout
        request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.httpBody = Data(pairs.map { "\(segment($0.0))=\(segment($0.1))" }.joined(separator: "&").utf8)
        return request
    }

    static func refusal(status: Int, data: Data) -> Error {
        if let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any], let code = object["error"] as? String {
            return OAuthRefusal(code: code, text: object["error_description"] as? String)
        }
        return ServerError(status: status, envelope: data) ?? TransportError(status: status, body: data)
    }
}
