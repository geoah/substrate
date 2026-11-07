/// The chats, newest activity first, under day headings.
public struct ChatListPresentation: Codable, Equatable, Sendable {
    public var title: String
    public var sections: [DaySection]
    /// "Offline. Showing saved chats."
    public var offline: String?
    /// "Couldn't refresh: …"
    public var problem: String?
    public var feedProblem: String?
    /// Nothing to list yet, and why; nil while there is.
    public var empty: EmptyList?
    /// Nothing held yet: the first read is on its way.
    public var loading: Bool
    public var showAutomated: Bool
    public var searchPrompt: String
}

public struct EmptyList: Codable, Equatable, Sendable {
    public var title: String
    public var message: String
    /// "New Chat", where there is an assistant to start one with.
    public var action: String?
}

public struct DaySection: Codable, Equatable, Sendable, Identifiable {
    public var id: String { title }
    public var title: String
    public var rows: [ThreadRow]
}

/// One chat in the list: the title and when, then the agent's name running
/// into the start of the last reply, and its state.
public struct ThreadRow: Codable, Hashable, Sendable, Identifiable {
    public enum Mark: String, Codable, Hashable, Sendable {
        /// A card waits on the owner.
        case attention
        case unread
    }

    /// A chat whose last turn didn't finish, so it is found from the list;
    /// the chat itself says why.
    public struct Unfinished: Codable, Hashable, Sendable {
        public var tone: UnfinishedTurn.Tone
        /// "Didn’t finish", "Stopped", "Failed".
        public var words: String
    }

    public var id: String
    public var title: String
    /// What a rename starts from.
    public var subject: String
    public var stamp: String
    public var agent: String?
    public var agentName: String
    /// The start of the last reply, or "Working…" while it runs.
    public var preview: String
    /// What started an automated run, by name; nil for a chat.
    public var mode: String?
    public var unread: Bool
    public var mark: Mark?
    public var unfinished: Unfinished?
}
