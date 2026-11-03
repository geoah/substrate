import Foundation

public extension SessionController {
    /// The engine's side of the session: what a replay of the journal
    /// rebuilds, and a finish waiting on its history file, which no replay
    /// does.
    struct State: Codable, Equatable, Sendable {
        /// The session in progress; nil when none is open.
        public var session: SessionState?
        /// The last journaled event's instant: the next one is never before
        /// it (§4.4 invariant 9).
        public var lastAt: Date?
        /// How many events the session has journaled: the Live Activity's
        /// content carries it, so a late, older update loses.
        public var revision: Int
        /// Save asked; the history file is being written. Never journaled:
        /// a crash before `finished` replays to the session still open.
        public var finishing: FinishedSession?

        public init(session: SessionState? = nil, lastAt: Date? = nil, revision: Int = 0, finishing: FinishedSession? = nil) {
            self.session = session
            self.lastAt = lastAt
            self.revision = revision
            self.finishing = finishing
        }
    }
}
