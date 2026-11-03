/// Why a watch stopped for good, as the link hears it.
public enum WatchStop: String, Codable, Hashable, Sendable {
    /// The token was refused: sign in again.
    case signedOut
    /// The token does not reach the kinds.
    case needsApproval
    /// Anything else, in the server's words beside it.
    case refused
}
