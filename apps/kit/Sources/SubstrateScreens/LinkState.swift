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
    /// The outbox as it last reported.
    public var outbox = OutboxFacts()
    /// The revision of the last report, nil before the first.
    public var seenRevision: Int?
    /// The last report moved the revision with writes to send: a flush is
    /// due where the link can send.
    public var flushDue = false
    public var sending = false
    /// The app reported a request answered 401 since the session loaded.
    public var tokenRefused = false
    /// The app reported a request answered 403 `insufficient_scope` since
    /// the session loaded.
    public var scopeRefused = false
    /// Moved by each sign-out.
    public var generation = 0
    public var signingOut = false
    public var signOutProblem: String?
    /// Why the session could not be read, until it is read again: not
    /// signed out, so the facts last read stay and nothing that needs the
    /// token runs meanwhile.
    public var sessionProblem: String?

    public init() {}

    /// The access is over until the owner signs in again.
    public var accessEnded: Bool {
        tokenRefused || status?.issues.contains(.accessEnded) == true || outbox.state == .signedOut
    }

    /// Writes may go now: signed in with access, not known to be offline,
    /// no flush running, and the last one not stopped by the token.
    public var canSend: Bool {
        guard session != nil, sessionProblem == nil, !accessEnded, !sending, network?.online != false else { return false }
        return outbox.state != .needsApproval
    }
}

/// The outbox as the link keeps it: where its last flush ended, what waits,
/// the writes the server refused, and why the device could not keep it.
public struct OutboxFacts: Codable, Hashable, Sendable {
    public var state: OutboxState
    public var waiting: Int
    /// The writes a flush would try now.
    public var sendable: Int
    public var parked: [ParkedWrite]
    public var flushing: Bool
    public var revision: Int
    /// Why the outbox could not be read or saved, or the file it could not
    /// decode and set aside.
    public var problem: String?

    public init(
        state: OutboxState = .idle, waiting: Int = 0, sendable: Int = 0, parked: [ParkedWrite] = [], flushing: Bool = false,
        revision: Int = 0, problem: String? = nil
    ) {
        self.state = state
        self.waiting = waiting
        self.sendable = sendable
        self.parked = parked
        self.flushing = flushing
        self.revision = revision
        self.problem = problem
    }

    public init(_ summary: OutboxSummary) {
        self.init(
            state: summary.state, waiting: summary.waiting, sendable: summary.sendable, parked: summary.parked.map(ParkedWrite.init),
            flushing: summary.flushing, revision: summary.revision,
            problem: summary.unavailable ?? summary.saveProblem
                ?? summary.unreadable.map { "The queue could not be read and was kept aside as \($0.name)." })
    }
}
