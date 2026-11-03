import Foundation
import SubstrateAuth

/// What a screen may know of a session: whose, where, and what it may do.
/// Never its token, so it can sit in a controller's state, a presentation
/// or a bridge message.
public struct SessionFacts: Codable, Hashable, Sendable {
    public var server: URL
    public var repository: String
    /// The approved scopes; nil for a password session, which reaches
    /// everything.
    public var scopes: [String]?
    public var expiresAt: Date?

    public init(server: URL, repository: String, scopes: [String]? = nil, expiresAt: Date? = nil) {
        self.server = server
        self.repository = repository
        self.scopes = scopes
        self.expiresAt = expiresAt
    }

    public init(_ session: Session) {
        self.init(server: session.server, repository: session.repository, scopes: session.scopes, expiresAt: session.expiresAt)
    }

    public var isApp: Bool { scopes != nil }

    /// One server and repository, however spelled.
    public var identity: String { server.absoluteString.lowercased() + "|" + repository.lowercased() }
}
