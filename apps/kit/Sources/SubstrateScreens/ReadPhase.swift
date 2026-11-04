/// Where reading an app's records stands: nothing yet, the first read, the
/// watch following, or the watch waiting to open again or stopped, with the
/// words for why. A stop keeps its reason, since only a `refused` one may
/// lift on its own: a signed-out or unapproved one waits for a new sign-in
/// or approval, and opening it again before then is refused the same way.
public enum ReadPhase: Codable, Hashable, Sendable {
    case idle
    case reading
    case following
    case interrupted(String)
    case stopped(WatchStop, String)

    /// A read or a watch is running, or waiting to open again.
    public var running: Bool {
        switch self {
        case .reading, .following, .interrupted: true
        case .idle, .stopped: false
        }
    }
}
