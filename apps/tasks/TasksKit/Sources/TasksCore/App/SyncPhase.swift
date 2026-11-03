/// Where reading the repository stands: nothing yet, the first read, the
/// watch following, or the watch waiting to open again or stopped, with
/// the words for why.
public enum SyncPhase: Codable, Hashable, Sendable {
    case idle
    case reading
    case following
    case interrupted(String)
    case stopped(String)

    /// Neither reading nor following: a start is due where the link can.
    var stopped: Bool {
        switch self {
        case .idle, .stopped: true
        default: false
        }
    }
}

/// What the engine decided the read should do next, for the effects to
/// start: set by the one event that decided it, cleared by every other.
public enum SyncDue: Codable, Hashable, Sendable {
    case read
    case follow
    case stop
}

/// Why the watch stopped for good, as the link hears it.
public enum WatchStop: String, Codable, Hashable, Sendable {
    /// The token was refused: sign in again.
    case signedOut
    /// The token does not reach the kinds.
    case needsApproval
    /// Anything else, in the server's words beside it.
    case refused
}
