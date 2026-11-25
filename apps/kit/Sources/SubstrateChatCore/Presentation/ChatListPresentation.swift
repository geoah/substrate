/// The chats, newest activity first, under day headings; the pinned ones
/// in a section of their own above them.
public struct ChatListPresentation: Codable, Equatable, Sendable {
    public var title: String
    public var sections: [DaySection]
    /// "Offline. Showing saved chats."
    public var offline: String?
    /// The list's one failure, drawn at its top: a refresh that failed,
    /// the change feed ended, a rename of a chat no window shows.
    public var failure: FailurePresentation?
    /// The change feed dropped and is being opened again: a state, not a
    /// failure.
    public var reconnecting: Bool
    /// Nothing to list yet, and why; nil while there is.
    public var empty: EmptyList?
    /// Nothing held yet: the first read is on its way.
    public var loading: Bool
    public var showAutomated: Bool
    public var searchPrompt: String
    /// The server keeps a person's pins and archives: Pin and Archive are
    /// offered on a row, and the archived chats as a list. False on a
    /// server older than them, where neither is.
    public var flags = false
    /// The server's list goes on past the chats held: reaching the end
    /// reads the next page (`ChatApp.Command.more`).
    public var more = false
    /// That read is out.
    public var loadingMore = false
    /// The chat archived a moment ago, which Undo puts back.
    public var undo: UndoPresentation?
    /// The archived chats, for the window that lists them; nil where the
    /// server keeps no archive.
    public var archived: ArchivedList?
    /// How many rows stand in for the chats while the first read is out.
    public var placeholders = 0
}

/// What the list says of the chat that just left it.
public struct UndoPresentation: Codable, Equatable, Sendable {
    public var thread: String
    /// "Archived “Balcony plants”"
    public var words: String
    /// "Undo"
    public var action: String
}

/// The chats the person archived, newest activity first.
public struct ArchivedList: Codable, Equatable, Sendable {
    public var title: String
    public var rows: [ThreadRow]
    /// The first read is out and nothing is held to draw.
    public var loading: Bool
    public var more: Bool
    public var loadingMore: Bool
    /// Said when there is none.
    public var empty: String?
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

    public init(title: String, rows: [ThreadRow]) {
        self.title = title
        self.rows = rows
    }
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
    /// A turn is running on it: the row shows it at work.
    public var running = false
    /// The person pinned it: Unpin is offered, and it stands in the pinned
    /// section.
    public var pinned = false
    /// The person archived it: a row of the archived list, or one listed
    /// among the chats all the same because it waits on them.
    public var archived = false
}

/// The messages a search found in one chat.
public struct SearchGroup: Codable, Hashable, Sendable, Identifiable {
    /// The thread.
    public var id: String
    public var title: String
    public var agent: String?
    public var agentName: String
    /// "Archived", on a chat the person archived.
    public var archived: String?
    public var hits: [SearchHit]
    /// "and 4 more", where the chat holds more of them than are drawn.
    public var more: String?
}

/// One message a search found: who said it, and the words around the ones
/// that matched.
public struct SearchHit: Codable, Hashable, Sendable, Identifiable {
    /// The message: what the chat is opened at.
    public var id: String
    /// "You", or the agent's name.
    public var who: String
    public var runs: [SearchRun]
}

/// A piece of a hit's text: `match` where it is one of the words searched.
public struct SearchRun: Codable, Hashable, Sendable {
    public var text: String
    public var match: Bool
}
