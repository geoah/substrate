import CryptoKit
import Foundation

/// The substrate's app authorization (decision 0194): OAuth 2.1's code flow
/// with PKCE for a public client whose `client_id` is a URL. The owner signs
/// in to their own substrate in the browser and approves what the app may
/// do; the app never sees a password, and its token reaches only what was
/// approved. Writes through it are recorded as the app's own actor.
enum OAuth {
    /// What this app asks for. `agents:*` talks to the agents (their chats,
    /// questions and turns), `decide:*` applies or dismisses what they
    /// suggest, and the provider rows say whether an agent can answer at all.
    /// The owner may leave any of them out on the approval page.
    static let scopes = ["agents:*", "decide:*", "read:\(Kinds.provider)"]

    /// The app's client id. A released build is stamped with the URL of its
    /// client ID metadata document, published beside the release, so the
    /// approval page shows the host that serves it. Any other build is a
    /// local app (a loopback id, which nothing fetches or verifies).
    static var clientID: String {
        let stamped = (Bundle.main.object(forInfoDictionaryKey: "SubstrateChatClientID") as? String)?
            .trimmingCharacters(in: .whitespaces) ?? ""
        return stamped.isEmpty ? "http://127.0.0.1/substrate-chat" : stamped
    }

    /// `GET /.well-known/oauth-authorization-server` (RFC 8414).
    struct Metadata: Decodable, Sendable {
        let issuer: String
        let authorizationEndpoint: URL
        let tokenEndpoint: URL
        let revocationEndpoint: URL?

        enum CodingKeys: String, CodingKey {
            case issuer
            case authorizationEndpoint = "authorization_endpoint"
            case tokenEndpoint = "token_endpoint"
            case revocationEndpoint = "revocation_endpoint"
        }
    }

    /// The token endpoint's answer.
    struct Grant: Decodable, Sendable {
        let accessToken: String
        let scope: String
        /// Absent when the owner approved the app until they revoke it.
        let expiresIn: Int?
        let repository: String

        enum CodingKeys: String, CodingKey {
            case accessToken = "access_token"
            case scope
            case expiresIn = "expires_in"
            case repository
        }

        var scopes: [String] { scope.split(separator: " ").map(String.init) }
    }

    /// The error object of RFC 6749 §5.2.
    struct Refusal: LocalizedError, Sendable {
        let code: String
        let text: String?
        var errorDescription: String? { text ?? "The server refused the sign-in (\(code))." }
    }

    /// PKCE (RFC 7636): a random verifier and its S256 challenge.
    struct PKCE: Sendable {
        let verifier: String
        let challenge: String

        init() {
            var bytes = [UInt8](repeating: 0, count: 32)
            _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
            self.init(verifier: Data(bytes).base64URL)
        }

        init(verifier: String) {
            self.verifier = verifier
            challenge = Data(SHA256.hash(data: Data(verifier.utf8))).base64URL
        }
    }

    static func state() -> String {
        var bytes = [UInt8](repeating: 0, count: 16)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        return Data(bytes).base64URL
    }

    /// Nil when the server predates app authorization (a 404): then it has
    /// only the password door.
    static func metadata(server: URL) async throws -> Metadata? {
        let api = API(server: server)
        let req = try api.request("GET", ".well-known/oauth-authorization-server", timeout: 20)
        let (data, response) = try await API.session.data(for: req)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        if status == 404 { return nil }
        guard status == 200 else {
            throw ServerError(status: status, envelope: data)
                ?? TransportError(text: "The server's sign-in metadata answered HTTP \(status).")
        }
        return try JSONDecoder().decode(Metadata.self, from: data)
    }

    static func authorizeURL(_ meta: Metadata, redirect: String, pkce: PKCE, state: String) -> URL? {
        guard var parts = URLComponents(url: meta.authorizationEndpoint, resolvingAgainstBaseURL: false) else { return nil }
        parts.percentEncodedQueryItems = [
            ("response_type", "code"),
            ("client_id", clientID),
            ("redirect_uri", redirect),
            ("scope", scopes.joined(separator: " ")),
            ("code_challenge", pkce.challenge),
            ("code_challenge_method", "S256"),
            ("state", state),
        ].map { URLQueryItem(name: $0.0, value: API.segment($0.1)) }
        return parts.url
    }

    static func exchange(_ meta: Metadata, code: String, redirect: String, pkce: PKCE) async throws -> Grant {
        var req = URLRequest(url: meta.tokenEndpoint)
        req.httpMethod = "POST"
        req.timeoutInterval = 20
        req.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        req.httpBody = form([
            ("grant_type", "authorization_code"), ("code", code), ("client_id", clientID),
            ("redirect_uri", redirect), ("code_verifier", pkce.verifier),
        ])
        let (data, response) = try await API.session.data(for: req)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 200 else { throw refusal(status: status, data: data) }
        return try JSONDecoder().decode(Grant.self, from: data)
    }

    /// RFC 7009: answers 200 whether or not the token existed.
    static func revoke(_ token: String, at endpoint: URL) async throws {
        var req = URLRequest(url: endpoint)
        req.httpMethod = "POST"
        req.timeoutInterval = 10
        req.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        req.httpBody = form([("token", token), ("client_id", clientID)])
        _ = try await API.session.data(for: req)
    }

    private static func form(_ pairs: [(String, String)]) -> Data {
        Data(pairs.map { "\(API.segment($0.0))=\(API.segment($0.1))" }.joined(separator: "&").utf8)
    }

    private static func refusal(status: Int, data: Data) -> Error {
        if let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any], let code = object["error"] as? String {
            return Refusal(code: code, text: object["error_description"] as? String)
        }
        return ServerError(status: status, envelope: data)
            ?? TransportError(text: "The token endpoint answered HTTP \(status).")
    }

    /// The redirect's query, checked: the state this flow sent, the issuer it
    /// began on (RFC 9207), and a code or the owner's refusal.
    static func callback(_ query: [String: String], state: String, issuer: String) throws -> String {
        guard query["state"] == state else { throw Refusal(code: "invalid_state", text: "The sign-in came back for another request. Try again.") }
        if let iss = query["iss"], iss != issuer {
            throw Refusal(code: "invalid_issuer", text: "The sign-in came back from \(iss), not \(issuer).")
        }
        if let error = query["error"] {
            if error == "access_denied" { throw Refusal(code: error, text: "Access was not granted.") }
            throw Refusal(code: error, text: query["error_description"])
        }
        guard let code = query["code"], !code.isEmpty else {
            throw Refusal(code: "invalid_request", text: "The sign-in came back without a code.")
        }
        return code
    }
}

/// What a granted token may do, from its scopes.
struct Grants: Codable, Equatable, Sendable {
    var scopes: [String]

    var chat: Bool { scopes.contains { $0.hasPrefix("agents:") } }
    var decide: Bool { scopes.contains { $0.hasPrefix("decide:") } }
    var providers: Bool { scopes.contains { $0 == "read:\(Kinds.provider)" || $0 == "write:\(Kinds.provider)" } }

    /// The approved access as a person reads it.
    var words: [String] {
        scopes.map { scope in
            let (verb, pattern) = scope.split(separator: ":", maxSplits: 1).map(String.init).paired
            let who = pattern == "*" ? "every agent" : pattern.hasSuffix("/*") ? "the agents of \(pattern.dropLast(2))" : agentName(pattern)
            switch verb {
            case "agents": return "Talk to \(who)"
            // "every agent" and one agent take "suggests"; a package's agents, "suggest".
            case "decide": return "Apply or dismiss what \(who) suggest\(pattern.hasSuffix("/*") ? "" : "s")"
            case "read": return "Read \(pattern.hasSuffix("/*") || pattern == "*" ? pattern : kindDisplayPlural(pattern).lowercasedFirst)"
            case "write": return "Change \(pattern.hasSuffix("/*") || pattern == "*" ? pattern : kindDisplayPlural(pattern).lowercasedFirst)"
            default: return scope
            }
        }
    }
}

private extension Array where Element == String {
    var paired: (String, String) { (first ?? "", count > 1 ? self[1] : "") }
}

extension Data {
    /// Base64url without padding (RFC 4648 §5), as PKCE and the state use.
    var base64URL: String {
        base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}
