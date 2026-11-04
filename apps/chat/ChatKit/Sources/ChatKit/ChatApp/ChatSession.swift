import Foundation
import SubstrateCore
import SubstrateRuntime

/// The session the link signed in, as the chats know it: whose, where, and
/// what its token may do. No token.
struct ChatSession: Codable, Equatable, Sendable {
    var server: URL
    var repository: String
    /// An app token's approved scopes; nil for a password sign-in's, which
    /// reaches everything.
    var scopes: [String]?

    init?(_ link: LinkSnapshot) {
        guard link.phase != .signedOut, let server = link.server, let repository = link.repository else { return nil }
        self.server = server
        self.repository = repository
        scopes = link.scopes
    }

    /// One server and repository, however spelled: what the device keeps
    /// their chats under.
    var identity: String { server.absoluteString.lowercased() + "|" + repository.lowercased() }

    var grants: Grants? { scopes.map(Grants.init) }
    var canChat: Bool { grants?.talks ?? true }
    var canDecide: Bool { grants?.decidesAny ?? true }
    /// Only an exact scope reaches the provider rows: `llm` is internal.
    var canReadProviders: Bool { grants?.reads(Kinds.provider) ?? true }
    /// An app never writes a provider's key: the console does.
    var canWriteKeys: Bool { grants == nil }

    /// A page of the console on this server: where the app sends the owner
    /// for what it may not do itself.
    func console(_ path: String) -> String {
        var base = server.absoluteString
        while base.hasSuffix("/") { base.removeLast() }
        return base + "/" + path
    }
}
