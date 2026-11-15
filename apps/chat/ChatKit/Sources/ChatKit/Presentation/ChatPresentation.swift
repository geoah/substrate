import Foundation
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
    /// The conversations a stream is writing, for what offers Stop outside
    /// a conversation's own screen.
    public var streaming: [ChildKey]
    public var conversations: [ChildKey: ConversationScreen]
    public var offered: [AgentChoice]
    public var defaultAgent: AgentChoice?
    public var settings: ChatSettingsPresentation
    public var unread: Int
    /// The app's own question before the system's: what it would notify.
    /// Drawn in the chat on screen once a first reply settled; nil once it
    /// was answered, and where the system was asked before.
    public var notificationOffer: NotificationOfferPresentation?
    /// For each provider without a key.
    public var keys: [String: KeySheet]
    /// The provider whose key was just saved: its sheet closes.
    public var keySaved: Signal<String>
    /// A run settled: the announcement, and the haptic where its chat is
    /// on screen.
    public var answered: Signal<ChatAnswer>
    /// The diagnostics file as it would be shared, while Share Diagnostics
    /// shows it.
    public var diagnostics: DiagnosticsReport?
    /// The instant the tree must be presented again though nothing moved:
    /// a reply being written whose rows were left as they were, because its
    /// last parse was too recent. Nil while every reply is drawn as it
    /// stands. For the runtime's tick alone: no view reads it, and it is in
    /// no part the tree goes out in.
    public var redrawAt: Date?
}

/// What the app would notify, said before the system asks to allow it.
public struct NotificationOfferPresentation: Codable, Equatable, Sendable {
    public var title: String
    public var message: String
    /// Asks the system.
    public var allow: String
    public var decline: String
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
    /// The thread of the conversation it shows; nil for a new chat. Here so
    /// the list marks its row without reading the conversation.
    public var thread: String?
    public var query: String
    /// The list as searched; nil while nothing is.
    public var found: [DaySection]?
    /// Said when a search finds nothing.
    public var nothingFound: String?
    /// The messages the server found for the search, by chat, drawn under
    /// the chats found by their titles; nil while nothing is searched.
    public var messages: [SearchGroup]?
    /// The server is being asked.
    public var searching = false
    /// The window's list is the archived chats.
    public var archived = false
}

/// A conversation's run settled.
public struct ChatAnswer: Codable, Equatable, Sendable {
    /// Nil for a new chat the run never bound.
    public var thread: String?
    /// What is announced: "<Agent> answered".
    public var words: String
}
