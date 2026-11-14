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
    /// A chat's thread, whose every run a server at `agents` 5 streams; an
    /// automated run's (a trigger, a schedule, a call) is never streamed.
    var chat = true

    init(version: Int, status: String, agent: String?, retryAt: Date? = nil, chat: Bool = true) {
        self.version = version
        self.status = status
        self.agent = agent
        self.retryAt = retryAt
        self.chat = chat
    }

    init(_ thread: ChatThread) {
        self.init(
            version: thread.record.version, status: thread.status, agent: thread.agentID, retryAt: thread.runFailure?.retryAt,
            chat: !thread.automated)
    }

    var running: Bool { status == "running" }
}
