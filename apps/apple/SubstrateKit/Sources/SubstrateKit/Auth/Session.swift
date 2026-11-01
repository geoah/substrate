import Foundation

/// A signed-in session: the server, the repository's authority and the
/// bearer secret. An app session (the authorization flow) carries what the
/// owner approved, when it ends, the webhooks it delivers to and what its
/// approval set up; a password session (a server without app authorization,
/// or one signed in before it) has full access and its token's id, so Sign
/// Out revokes the very token it forgets.
///
/// It reads every shape an app has kept in the Keychain: Substrate Sync's
/// `{server, repository, secret, tokenID}`, Substrate Chat's with `grants`,
/// `expiresAt` and `revocation`, and this one, versioned. It writes the
/// fields those earlier builds read too, so a rollback keeps the sign-in.
public struct Session: Codable, Equatable, Sendable {
    public static let currentVersion = 2

    public var version: Int
    public var server: URL
    public var repository: String
    public var secret: String
    /// The token record's id where the session knows it: a password login's.
    public var tokenID: String?
    /// The client id the token was approved for; nil for a password session.
    public var clientID: String?
    /// The approved scopes; nil is a full-access password session.
    public var scopes: [String]?
    /// The scopes the request that minted the token asked for, where the
    /// session knows: one asked for and not approved was left out by the
    /// owner, and the app does not ask about it again.
    public var requestedScopes: [String]?
    /// Absent when the access lasts until the owner revokes it.
    public var expiresAt: Date?
    /// When the token was issued, where the session knows: with `expiresAt`,
    /// the lifetime the owner approved.
    public var issuedAt: Date?
    /// Where an app session's token is revoked (RFC 7009).
    public var revocation: URL?
    /// Each approved webhook's delivery URL, by trigger id.
    public var webhooks: [String: URL]
    /// What the approval set up, as the token response listed it.
    public var details: [AuthorizationDetail]

    public init(server: URL, repository: String, secret: String, tokenID: String? = nil, clientID: String? = nil,
                scopes: [String]? = nil, requestedScopes: [String]? = nil, expiresAt: Date? = nil, issuedAt: Date? = nil,
                revocation: URL? = nil, webhooks: [String: URL] = [:], details: [AuthorizationDetail] = [])
    {
        version = Self.currentVersion
        self.server = server
        self.repository = repository
        self.secret = secret
        self.tokenID = tokenID
        self.clientID = clientID
        self.scopes = scopes
        self.requestedScopes = requestedScopes
        self.expiresAt = expiresAt
        self.issuedAt = issuedAt
        self.revocation = revocation
        self.webhooks = webhooks
        self.details = details
    }

    /// An app session from the token endpoint's answer to a request that
    /// asked for `requested`. `now` is when the answer came: the lifetime the
    /// owner approved starts at the exchange, however long the approval took.
    public init(server: URL, token: TokenResponse, clientID: String, revocation: URL?, requested: [String]? = nil,
                now: Date = Date())
    {
        var webhooks: [String: URL] = [:]
        for detail in token.authorizationDetails where detail.type == AuthorizationDetail.Types.webhook {
            if let trigger = detail.trigger, let url = detail.url { webhooks[trigger] = url }
        }
        self.init(server: server, repository: token.repository, secret: token.accessToken, clientID: clientID,
                  scopes: token.scopes, requestedScopes: requested,
                  expiresAt: token.expiresIn.map { now.addingTimeInterval(TimeInterval($0)) },
                  issuedAt: now, revocation: revocation, webhooks: webhooks, details: token.authorizationDetails)
    }

    /// Whether the token is an app's, holding only what was approved.
    public var isApp: Bool { scopes != nil }

    /// What the token may do; nil for a full-access password session.
    public var grants: Grants? { scopes.map(Grants.init) }

    /// Which repository on which server: what every byte an app keeps about
    /// it is bound to.
    public var identity: String { server.absoluteString.lowercased() + "|" + repository.lowercased() }

    public func hasEnded(at now: Date) -> Bool { expiresAt.map { $0 <= now } ?? false }

    enum CodingKeys: String, CodingKey {
        case version, server, repository, secret, tokenID, clientID, scopes, requestedScopes, grants, expiresAt, issuedAt, revocation
        case webhooks, details
    }

    enum GrantsKeys: String, CodingKey { case scopes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = c.value(.version, or: 1)
        server = try c.decode(URL.self, forKey: .server)
        repository = try c.decode(String.self, forKey: .repository)
        secret = try c.decode(String.self, forKey: .secret)
        tokenID = c.value(.tokenID, or: String?.none).flatMap { $0.isEmpty ? nil : $0 }
        clientID = c.value(.clientID, or: nil)
        if let scopes = c.value(.scopes, or: [String]?.none) {
            self.scopes = scopes
        } else {
            self.scopes = (try? c.nestedContainer(keyedBy: GrantsKeys.self, forKey: .grants))?.value(.scopes, or: nil)
        }
        requestedScopes = c.value(.requestedScopes, or: nil)
        expiresAt = Self.date(c, .expiresAt)
        issuedAt = Self.date(c, .issuedAt)
        revocation = c.value(.revocation, or: nil)
        webhooks = c.value(.webhooks, or: [:])
        details = c.value(.details, or: [])
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(Self.currentVersion, forKey: .version)
        try c.encode(server, forKey: .server)
        try c.encode(repository, forKey: .repository)
        try c.encode(secret, forKey: .secret)
        // Earlier builds require the key, and read "" as an app token's.
        try c.encode(tokenID ?? "", forKey: .tokenID)
        try c.encodeIfPresent(clientID, forKey: .clientID)
        try c.encodeIfPresent(scopes, forKey: .scopes)
        try c.encodeIfPresent(requestedScopes, forKey: .requestedScopes)
        if let scopes {
            var grants = c.nestedContainer(keyedBy: GrantsKeys.self, forKey: .grants)
            try grants.encode(scopes, forKey: .scopes)
        }
        // Seconds since 2001, as a default JSONEncoder wrote Chat's, whatever
        // strategy the encoder at hand was given.
        try c.encodeIfPresent(expiresAt?.timeIntervalSinceReferenceDate, forKey: .expiresAt)
        try c.encodeIfPresent(issuedAt?.timeIntervalSinceReferenceDate, forKey: .issuedAt)
        try c.encodeIfPresent(revocation, forKey: .revocation)
        if !webhooks.isEmpty { try c.encode(webhooks, forKey: .webhooks) }
        if !details.isEmpty { try c.encode(details, forKey: .details) }
    }

    /// A stamp as seconds since 2001 or as RFC 3339 text.
    private static func date(_ c: KeyedDecodingContainer<CodingKeys>, _ key: CodingKeys) -> Date? {
        if let seconds = try? c.decodeIfPresent(Double.self, forKey: key) { return Date(timeIntervalSinceReferenceDate: seconds) }
        return c.value(key, or: String?.none).flatMap(Timestamp.parse)
    }
}

public extension Session {
    /// Revokes the token through its own door, and returns only once the
    /// server says it is revoked: an app token at its revocation endpoint
    /// (RFC 7009), which must answer 200, and a password one with its own
    /// `DELETE /tokens/{id}`. An app token goes under the client id it was
    /// approved for, since the server answers 200 to a token sent under
    /// another and leaves it live; a session that never kept its client id
    /// sends none, and the token alone is the credential. The endpoint is the
    /// session's own only on its server's origin, and a redirect is a
    /// refusal: a token is never sent anywhere else.
    func revoke(transport: any HTTPTransport = URLSessionTransport.shared) async throws {
        if isApp {
            let fixed = URL(string: server.absoluteString.trimmingTrailingSlashes + "/oauth/revoke") ?? server
            let endpoint = revocation.flatMap { ServerAddress.sameOrigin($0, server) ? $0 : nil } ?? fixed
            var pairs = [("token", secret)]
            if let clientID { pairs.append(("client_id", clientID)) }
            let (data, response) = try await transport.data(for: OAuthClient.form(endpoint, pairs, timeout: 10))
            guard response.statusCode == 200 else { throw OAuthClient.refusal(response, data: data) }
        } else if let tokenID {
            do {
                try await SubstrateClient(self, transport: transport).revokeToken(id: tokenID)
            } catch is SessionExpired {
                // The server no longer accepts the token: there is nothing
                // left to revoke.
            }
        }
    }
}
