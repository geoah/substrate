import Foundation
import SubstrateCore

/// What the list holds of a thread's row, as a conversation reads it.
struct ThreadFacts: Codable, Hashable, Sendable {
    var version: Int
    var status: String
    /// The agent the thread is with.
    var agent: String?
    /// When the rate limit its last run ended on lifts, where the server
    /// said how long.
    var retryAt: Date?

    init(version: Int, status: String, agent: String?, retryAt: Date? = nil) {
        self.version = version
        self.status = status
        self.agent = agent
        self.retryAt = retryAt
    }

    init(_ thread: ChatThread) {
        self.init(version: thread.record.version, status: thread.status, agent: thread.agentID, retryAt: thread.runFailure?.retryAt)
    }

    var running: Bool { status == "running" }
}
