import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SubstrateAPI
import SubstrateCore
import SubstrateDevice

/// The token endpoint's answer (RFC 6749 §5.1, with RFC 9396's
/// `authorization_details` listing what the approval set up).
public struct TokenResponse: Decodable, Sendable, Equatable {
    public let accessToken: String
    public let tokenType: String?
    public let scope: String
    /// Absent when the owner approved the app until they revoke it.
    public let expiresIn: Int?
    public let repository: String
    /// The token record's id, from a server that sends it.
    public let tokenID: String?
    /// Each package with the version now held, each account's kind and id,
    /// each webhook's trigger and the URL to deliver to.
    public let authorizationDetails: [AuthorizationDetail]

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
        case tokenType = "token_type"
        case scope
        case expiresIn = "expires_in"
        case repository
        case tokenID = "token_id"
        case authorizationDetails = "authorization_details"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        accessToken = try c.decode(String.self, forKey: .accessToken)
        tokenType = c.value(.tokenType, or: nil)
        scope = c.value(.scope, or: "")
        expiresIn = c.value(.expiresIn, or: nil)
        repository = try c.decode(String.self, forKey: .repository)
        tokenID = c.value(.tokenID, or: String?.none).flatMap { $0.isEmpty ? nil : $0 }
        authorizationDetails = c.value(.authorizationDetails, or: [])
    }

    public var scopes: [String] { scope.split(separator: " ").map(String.init) }
}

/// A token for another repository than the one the app found the server
/// from: the person signed in to a different repository on that server. The
/// app never keeps it, since its records are not the ones the name the
/// person typed points at.
public struct RepositoryMismatch: LocalizedError, Sendable, Equatable {
    public let resolved: String
    public let token: String
    /// The server that issued the token, which a failed revocation names.
    public let server: URL?
    /// What became of the token the app refused.
    public var revocation: Revocation = .revoked

    public enum Revocation: Sendable, Equatable {
        /// The server confirmed it.
        case revoked
        /// The server did not confirm it, and the app keeps the token only
        /// to revoke it again (`SessionStore.revokePending`).
        case pending
        /// The server did not confirm it, and the device could not keep the
        /// token to try again: it is live until the owner revokes it.
        case live
    }

    /// Nil when nothing was resolved or the token is for the resolved
    /// repository; a name compares without case or a trailing dot.
    public init?(resolved: String?, token: String, server: URL? = nil) {
        guard let resolved else { return nil }
        let normal = { (name: String) in
            name.lowercased().hasSuffix(".") ? String(name.lowercased().dropLast()) : name.lowercased()
        }
        guard normal(resolved) != normal(token) else { return nil }
        self.resolved = resolved
        self.token = token
        self.server = server
    }

    public var errorDescription: String? {
        let refused = "You signed in to \(token), not \(resolved). Sign in to \(resolved), or type \(token) to connect it instead."
        let host = server.map(ServerAddress.display) ?? "the server"
        switch revocation {
        case .revoked:
            return refused
        case .pending:
            return refused + " \(host) didn't confirm revoking the access it gave for \(token). "
                + "The app keeps that access only to revoke it, and tries again the next time it starts."
        case .live:
            return refused + " \(host) didn't confirm revoking the access it gave for \(token), and this device couldn't keep it "
                + "to try again. Revoke it in \(host)'s Settings, under Apps."
        }
    }
}

/// A released build with no sign-in callback of its own: its client id is
/// an https one, and the redirect it claims through its site is missing or
/// not an https URL, so it has nowhere the code can come back to it alone.
public struct UnclaimedRedirect: LocalizedError, Sendable, Equatable {
    public let appName: String
    public var errorDescription: String? {
        "This build of \(appName) can't sign in: it has no sign-in address of its own. Install it again from its release page."
    }
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

    /// The page the owner approves on. `authorization_details` is the
    /// manifest's details as compact JSON and `software_version` this
    /// build's version, which the token minted from the approval records
    /// with the details' floors. A server that does not list every
    /// type they use would approve the scopes alone, so it is refused here
    /// rather than sent the request without them, and so is metadata whose
    /// endpoints are not on its issuer's origin. `loginHint` is the
    /// repository the app found the server from, which the sign-in page
    /// fills in.
    public func authorizeURL(redirect: String, pkce: PKCE, state: String, loginHint: String? = nil) throws -> URL {
        if let problem = metadata.mismatch(server: nil) { throw ServerMismatch(problem) }
        guard manifest.takesDetails(metadata) else {
            throw ServerMismatch("\(ServerAddress.display(metadata.issuerOrigin ?? metadata.authorizationEndpoint)) can't set up what \(manifest.appName) asks for: it needs an update first.")
        }
        var items = [
            ("response_type", "code"),
            ("client_id", manifest.clientID),
            ("redirect_uri", redirect),
            ("scope", manifest.scope),
            ("code_challenge", pkce.challenge),
            ("code_challenge_method", "S256"),
            ("state", state),
        ]
        if let loginHint, !loginHint.isEmpty { items.append(("login_hint", loginHint)) }
        items.append(("software_version", manifest.softwareVersion))
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
    /// first use, and a replay revokes the token the first use minted. Never
    /// sent off the issuer's origin either, by its endpoint or by a redirect,
    /// which is a refusal here: the code and its verifier are the token to
    /// whoever receives them.
    public func exchange(code: String, redirect: String, pkce: PKCE) async throws -> TokenResponse {
        if let problem = metadata.mismatch(server: nil) { throw ServerMismatch(problem) }
        let request = Self.form(metadata.tokenEndpoint, [
            ("grant_type", "authorization_code"), ("code", code), ("client_id", manifest.clientID),
            ("redirect_uri", redirect), ("code_verifier", pkce.verifier),
        ], timeout: SubstrateClient.Timeout.quick)
        let (data, response) = try await transport.data(for: request)
        guard response.statusCode == 200 else { throw Self.refusal(response, data: data) }
        return try JSONDecoder().decode(TokenResponse.self, from: data)
    }

    /// The session a code is exchanged for. Its lifetime is read from `now`
    /// once the token came back, so a long approval does not shorten it.
    public func session(exchanging code: String, redirect: String, pkce: PKCE, server: URL,
                        now: @Sendable () -> Date = { Date() }) async throws -> Session
    {
        let token = try await exchange(code: code, redirect: redirect, pkce: pkce)
        return Session(server: server, token: token, clientID: manifest.clientID, revocation: metadata.revocation,
                       requested: manifest.scopes, now: now())
    }

    /// The whole flow: the browser on the server's own sign-in and approval
    /// page, the redirect taken through the route the manifest signs in by
    /// (`route(for:)`), the redirect checked and the code exchanged. `server` is
    /// the one the owner chose, and the metadata must be its own: an issuer
    /// or an endpoint elsewhere is refused before the browser opens.
    /// `repository` is the name the app found the server from: it is sent as
    /// `login_hint`, and a token for any other repository is refused and
    /// revoked, kept meanwhile in `rejected`'s pending revocations (never as
    /// the session) so a revocation that fails is tried again by
    /// `revokePending`. `browser` is the device's: it opens the page and
    /// hands the callback back. The session it answers is not saved.
    @MainActor
    public func signIn(server: URL, repository: String? = nil, keepingRejectedIn rejected: SessionStore? = nil,
                       browser: any BrowserSignIn, now: @escaping @Sendable () -> Date = { Date() }) async throws -> Session
    {
        if let problem = metadata.mismatch(server: server) { throw ServerMismatch(problem) }
        let pkce = try PKCE()
        let state = try PKCE.state()
        let route = try Self.route(for: manifest)
        let (redirect, query) = try await approve(route, browser: browser) { redirect in
            try authorizeURL(redirect: redirect, pkce: pkce, state: state, loginHint: repository)
        }
        let code = try callback(query, state: state)
        let session = try await session(exchanging: code, redirect: redirect, pkce: pkce, server: server, now: now)
        if let mismatch = RepositoryMismatch(resolved: repository, token: session.repository, server: server) {
            throw await reject(session, mismatch, keepingIn: rejected)
        }
        return session
    }

    /// How a build signs in: a release (an https client id) through the
    /// https callback it claims, which the system hands to this signed app
    /// alone; a local build (a loopback client id) through a loopback
    /// receiver (RFC 8252). A release whose claimed callback is missing or
    /// not one is refused rather than sent to a loopback port, where any
    /// program on the device could take the code it carries.
    static func route(for manifest: AppManifest) throws -> SignInRoute {
        if manifest.isLocal { return .loopback(appName: manifest.appName, path: manifest.loopbackPath) }
        guard let claimed = manifest.claimedRedirect, AppManifest.claimedRedirect(claimed.absoluteString) == claimed else {
            throw UnclaimedRedirect(appName: manifest.appName)
        }
        return .claimed(claimed)
    }

    /// The browser's part: the route made ready, the page opened on its
    /// redirect, and the route let go of once the callback is in, before the
    /// code is exchanged.
    @MainActor
    private func approve(_ route: SignInRoute, browser: any BrowserSignIn,
                         page: (String) throws -> URL) async throws -> (redirect: String, query: [String: String])
    {
        let redirect = try await browser.prepare(route)
        defer { browser.finish(redirect) }
        switch try await browser.open(try page(redirect.uri), for: redirect) {
        case let .callback(query): return (redirect.uri, query)
        case .cancelled: throw SignInCancelled()
        }
    }

    /// Retires a token the app refused: kept for revocation first, so no
    /// moment holds it nowhere, then revoked, and forgotten once the server
    /// confirms. The answer says which of the three it came to.
    func reject(_ session: Session, _ mismatch: RepositoryMismatch, keepingIn store: SessionStore?) async -> RepositoryMismatch {
        var mismatch = mismatch
        let kept = store.map { (try? $0.keepForRevocation(session)) != nil } ?? false
        do {
            try await session.revoke(transport: transport)
            if kept { store?.revoked(session) }
            mismatch.revocation = .revoked
        } catch {
            mismatch.revocation = kept ? .pending : .live
        }
        return mismatch
    }

    static func form(_ url: URL, _ pairs: [(String, String)], timeout: TimeInterval) -> URLRequest {
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.timeoutInterval = timeout
        request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.httpBody = Data(pairs.map { "\(segment($0.0))=\(segment($0.1))" }.joined(separator: "&").utf8)
        return request
    }

    /// Anything but the 200 the token and revocation endpoints answer: a
    /// redirect, which is never followed, RFC 6749's error object, or the
    /// envelope.
    static func refusal(_ response: HTTPURLResponse, data: Data) -> Error {
        let status = response.statusCode
        if (300 ..< 400).contains(status) {
            return Redirected(status: status, location: response.value(forHTTPHeaderField: "Location"))
        }
        if let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any], let code = object["error"] as? String {
            return OAuthRefusal(code: code, text: object["error_description"] as? String)
        }
        return ServerError(status: status, envelope: data) ?? TransportError(status: status, body: data)
    }
}
