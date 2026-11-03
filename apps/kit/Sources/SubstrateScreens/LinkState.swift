import Foundation
import SubstrateAuth
import SubstrateDevice
import SubstrateLocal

/// What the link knows about the connection to the repository. No token:
/// the session's secret stays in the session store, read by the effects
/// that need it.
public struct LinkState: Codable, Equatable, Sendable {
    public var session: SessionFacts?
    /// Tokens this device stopped using that their servers have not revoked
    /// yet.
    public var unrevoked: [SessionFacts] = []
    public var status: AppStatus?
    public var checking = false
    /// What the repository holds of each of the app's packages.
    public var packages: [String: HeldPackage] = [:]
    public var network: NetworkPath?
    public var outbox: OutboxState = .idle
    public var waiting = 0
    public var parked: [ParkedWrite] = []
    public var sending = false
    /// Moved by each sign-out.
    public var generation = 0
    public var signingOut = false
    public var signOutProblem: String?

    public init() {}

    /// The access is over until the owner signs in again.
    public var accessEnded: Bool {
        status?.issues.contains(.accessEnded) == true || outbox == .signedOut
    }
}
