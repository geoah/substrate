import Foundation
import SubstrateCore

/// How the outbox's last flush ended.
public enum OutboxState: Codable, Sendable, Hashable {
    case idle
    /// Every write that could go landed.
    case sent
    /// The server could not be reached; nothing is lost.
    case offline(String)
    /// The token was refused: sign in again.
    case signedOut
    /// The token lacks a scope the write needs.
    case needsApproval
    /// The server refused one or more writes, which wait, parked, with its
    /// words until the owner retries or discards them.
    case refused(String)
}

/// The outbox could not keep a change on disk: nothing was queued.
public struct OutboxNotSaved: LocalizedError, Sendable {
    public let reason: String

    public init(reason: String) {
        self.reason = reason
    }

    public var errorDescription: String? { "The change could not be kept on this device: \(reason)" }
}

/// A guarded edit met a newer version on the server: another device or
/// writer changed the record since the owner began editing.
public struct EditConflict: LocalizedError, Sendable {
    public let kind: String
    public let id: String
    public let server: String

    public var errorDescription: String? {
        "It changed on your substrate since you began editing it (another device or app), so your edit was not sent."
    }
}

/// What a write waiting for the server will set, merged in queue order:
/// what a read of the record lays over the server's answer until it lands.
public struct WaitingWrite: Sendable, Hashable {
    /// The write creates the record.
    public var creates: Bool
    /// The properties it sets, `null` clearing one.
    public var properties: JSONObject

    public init(creates: Bool, properties: JSONObject) {
        self.creates = creates
        self.properties = properties
    }
}
