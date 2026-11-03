import SubstrateCore

/// What the list holds of a thread's row, as a conversation reads it.
struct ThreadFacts: Codable, Hashable, Sendable {
    var version: Int
    var status: String
    /// The agent the thread is with.
    var agent: String?

    init(version: Int, status: String, agent: String?) {
        self.version = version
        self.status = status
        self.agent = agent
    }

    init(_ thread: ChatThread) {
        self.init(version: thread.record.version, status: thread.status, agent: thread.agentID)
    }

    var running: Bool { status == "running" }
}
