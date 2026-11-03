import SubstrateRuntime
import SubstrateScreens

/// Everything Substrate Chat draws, as one value: the list, every window's
/// chat, each conversation with its rows and its run, and Settings.
public struct ChatPresentation: Codable, Equatable, Sendable {
    public var signedIn: Bool
    /// The owner left out talking to the agents: the one thing the app is for.
    public var canChat: Bool
    /// The server, as a person reads its address.
    public var host: String
    public var list: ChatListPresentation
    public var scenes: [String: ScenePresentation]
    /// A notification, a shortcut or a menu asked for a chat a window takes.
    public var routePending: Bool
    /// The threads a window in front shows: their notifications stay quiet.
    public var onScreen: [String]
    public var conversations: [ChildKey: ConversationScreen]
    public var offered: [AgentChoice]
    public var defaultAgent: AgentChoice?
    public var settings: ChatSettingsPresentation
    public var unread: Int
    public var renameProblem: String?
    /// For each provider without a key.
    public var keys: [String: KeySheet]
    /// Moved when a key was saved: the sheet closes.
    public var keySaved: Int
}

/// An agent as a chat offers it.
public struct AgentChoice: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var description: String?
}

/// One window: the conversation it shows and the list it searched.
public struct ScenePresentation: Codable, Equatable, Sendable {
    public var current: ChildKey?
    public var query: String
    /// The list as searched; nil while nothing is.
    public var found: [DaySection]?
    /// Said when a search finds nothing.
    public var nothingFound: String?
}
