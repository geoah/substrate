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
        /// History holds a file for the replayed session that could not be
        /// read, in words: whether Finish already saved it is unknown, so
        /// the session is not changed until the file reads. Never journaled:
        /// every open checks again.
        public var historyProblem: String?

        public init(
            session: SessionState? = nil, lastAt: Date? = nil, revision: Int = 0, finishing: FinishedSession? = nil,
            historyProblem: String? = nil
        ) {
            self.session = session
            self.lastAt = lastAt
            self.revision = revision
            self.finishing = finishing
            self.historyProblem = historyProblem
        }
    }
}
