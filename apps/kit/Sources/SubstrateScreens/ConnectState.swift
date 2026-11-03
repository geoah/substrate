import SubstrateAPI

/// The connect screen's state: what was typed, where finding and signing in
/// stand, and the last problem.
public struct ConnectState: Codable, Equatable, Sendable {
    public enum Phase: Codable, Equatable, Sendable {
        case idle
        case checking
        case found(ServerProbe.Result)
        case signingIn(ServerProbe.Result)
    }

    public var address: String
    public var phase: Phase
    public var problem: String?
    /// The last check failed in a way the same check may not: the name's DNS
    /// lookup did not answer, so the button offers Try Again.
    public var retryable: Bool

    public init(address: String = "", phase: Phase = .idle, problem: String? = nil, retryable: Bool = false) {
        self.address = address
        self.phase = phase
        self.problem = problem
        self.retryable = retryable
    }

    public var found: ServerProbe.Result? {
        switch phase {
        case let .found(result), let .signingIn(result): result
        default: nil
        }
    }

    public var busy: Bool {
        switch phase {
        case .checking, .signingIn: true
        default: false
        }
    }

    /// What the field shows once a server is found: the repository's name it
    /// was found from, else the server's origin.
    public static func shown(_ found: ServerProbe.Result) -> String {
        found.repository ?? found.origin.absoluteString
    }
}
