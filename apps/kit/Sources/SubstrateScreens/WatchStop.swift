/// Why a watch, or a read, stopped for good, as the link hears it.
public enum WatchStop: String, Codable, Hashable, Sendable {
    /// The token was refused: sign in again.
    case signedOut
    /// The token does not reach the kinds.
    case needsApproval
    /// Anything else, in the server's words beside it: a read the network
    /// or the server failed included.
    case refused
}
