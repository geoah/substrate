import Foundation
import SubstrateCore

/// What every controller may know about the connection to the repository,
/// delivered down the tree as `linkChanged`. It carries no secret: the token
/// stays inside the session store and the transport, so a snapshot can be
/// presented, logged, or sent over the Android bridge.
public struct LinkSnapshot: Bridged, Equatable {
    public enum Phase: String, Bridged, Equatable {
        case signedOut
        case signedIn
        /// The token was refused or its access ended: only signing in again
        /// gets it back.
        case accessEnded
    }

    public var phase: Phase
    public var server: URL?
    public var repository: String?
    /// An app session's approved scopes; nil for a password session, which
    /// reaches everything.
    public var scopes: [String]?
    public var accessEnds: Date?
    /// Moved by every sign-out: background work scheduled under another
    /// writes nothing.
    public var generation: Int
    /// Nil until connectivity has answered.
    public var online: Bool?
    /// The version of each of the app's packages the repository holds.
    public var packages: [String: Int]
    /// Writes waiting in the outbox, and how many of them the server refused.
    public var waiting: Int
    public var refused: Int

    public init(phase: Phase = .signedOut, server: URL? = nil, repository: String? = nil, scopes: [String]? = nil,
                accessEnds: Date? = nil, generation: Int = 0, online: Bool? = nil, packages: [String: Int] = [:],
                waiting: Int = 0, refused: Int = 0)
    {
        self.phase = phase
        self.server = server
        self.repository = repository
        self.scopes = scopes
        self.accessEnds = accessEnds
        self.generation = generation
        self.online = online
        self.packages = packages
        self.waiting = waiting
        self.refused = refused
    }

    public var signedIn: Bool { phase == .signedIn }
}
