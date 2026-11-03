import Foundation

/// The device's local notifications (§5.4): whether the owner lets the app
/// alert, the categories its notifications answer with, one request
/// scheduled or replaced by its id, a request taken back, and what the owner
/// did with one. A tap or an action is a `NotificationResponse` on
/// `responses()`, which the app turns into a command; no app sets a delegate
/// of its own.
public protocol Notifications: Sendable {
    /// Where the owner's permission stands now.
    func permission() async -> NotificationPermission
    /// Asks the owner once, where they were never asked; answers where the
    /// permission stands after.
    func requestPermission() async -> NotificationPermission
    /// The categories the app's notifications name, each with the actions
    /// it offers, replacing the ones registered before. Registered before
    /// the first notification of a category is shown.
    func register(_ categories: [NotificationCategory]) async
    /// Schedules the request, replacing a pending one of the same id: shown
    /// at its `fireAt`, or at once where it has none. A request whose instant
    /// has passed is dropped, since what it would say is over. A permission
    /// refused makes it a quiet no-op.
    func schedule(_ request: NotificationRequest) async
    /// Removes the pending and the delivered notification of this id.
    func cancel(_ id: String) async
    /// Removes the delivered notifications of one thread (a chat opened:
    /// what it notified is no longer news).
    func cancel(thread: String) async
    /// Removes every pending and delivered notification, and the badge (a
    /// sign-out).
    func cancelAll() async
    /// The number on the app's icon.
    func setBadge(_ count: Int) async
    /// What the owner does with the app's notifications from now on: a tap,
    /// an action, a dismissal, each once. One that came before anything
    /// read the stream (the tap that launched the app) is kept for the first
    /// reader.
    func responses() -> AsyncStream<NotificationResponse>
}

public extension Notifications {
    func register(_ categories: [NotificationCategory]) async {}
    func cancel(thread: String) async {}
    func cancelAll() async {}
    func setBadge(_ count: Int) async {}
    func responses() -> AsyncStream<NotificationResponse> { AsyncStream { $0.finish() } }
}

/// What the owner allows.
public enum NotificationPermission: String, Codable, Equatable, Sendable {
    /// Never asked: the app asks when it first needs to.
    case notAsked
    /// Refused, or allowed with nowhere to show one.
    case off
    /// Allowed, but not time sensitive: a Focus may hold one back.
    case notTimeSensitive
    /// Allowed, time sensitive.
    case on
}

/// One notification, shown at an instant or at once.
public struct NotificationRequest: Codable, Equatable, Sendable {
    public var id: String
    public var title: String
    /// A line under the title: what the notification is about.
    public var subtitle: String?
    public var body: String?
    /// When it shows; nil shows it at once.
    public var fireAt: Date?
    /// Notifications of one thread group together.
    public var thread: String?
    /// One of the categories the app registered: the actions it offers, and
    /// what its responses carry.
    public var category: String?
    /// Breaks through a Focus where the app may.
    public var timeSensitive: Bool
    /// Shown while the app is in front. False keeps it quiet there, where
    /// the app already says it (a chat on screen, a countdown on the session
    /// screen); in the background it shows either way.
    public var showsWhileActive: Bool
    public var userInfo: [String: String]

    public init(id: String, title: String, body: String? = nil, fireAt: Date? = nil, thread: String? = nil, category: String? = nil,
                timeSensitive: Bool = false, showsWhileActive: Bool = true, userInfo: [String: String] = [:],
                subtitle: String? = nil)
    {
        self.id = id
        self.title = title
        self.subtitle = subtitle
        self.body = body
        self.fireAt = fireAt
        self.thread = thread
        self.category = category
        self.timeSensitive = timeSensitive
        self.showsWhileActive = showsWhileActive
        self.userInfo = userInfo
    }

    private enum CodingKeys: String, CodingKey {
        case id, title, subtitle, body, fireAt, thread, category, timeSensitive, showsWhileActive, userInfo
    }

    /// A request encoded before `showsWhileActive` reads as one that shows.
    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        title = try container.decode(String.self, forKey: .title)
        subtitle = try container.decodeIfPresent(String.self, forKey: .subtitle)
        body = try container.decodeIfPresent(String.self, forKey: .body)
        fireAt = try container.decodeIfPresent(Date.self, forKey: .fireAt)
        thread = try container.decodeIfPresent(String.self, forKey: .thread)
        category = try container.decodeIfPresent(String.self, forKey: .category)
        timeSensitive = try container.decodeIfPresent(Bool.self, forKey: .timeSensitive) ?? false
        showsWhileActive = try container.decodeIfPresent(Bool.self, forKey: .showsWhileActive) ?? true
        userInfo = try container.decodeIfPresent([String: String].self, forKey: .userInfo) ?? [:]
    }
}

/// A kind of notification the app shows, and the actions it answers with
/// beside a tap.
public struct NotificationCategory: Codable, Equatable, Sendable {
    public var id: String
    public var actions: [NotificationAction]
    /// A dismissal is a response too (`.dismiss`).
    public var reportsDismissal: Bool

    public init(id: String, actions: [NotificationAction] = [], reportsDismissal: Bool = false) {
        self.id = id
        self.actions = actions
        self.reportsDismissal = reportsDismissal
    }
}

/// A button on a notification.
public struct NotificationAction: Codable, Equatable, Sendable {
    public var id: String
    public var title: String
    /// Brings the app to the front, as a tap does; otherwise it answers in
    /// the background.
    public var opensApp: Bool
    /// Drawn as one that removes or gives something up.
    public var destructive: Bool
    /// Asks for a line of text first (a reply), which the response carries.
    public var input: NotificationTextInput?

    public init(id: String, title: String, opensApp: Bool = false, destructive: Bool = false, input: NotificationTextInput? = nil) {
        self.id = id
        self.title = title
        self.opensApp = opensApp
        self.destructive = destructive
        self.input = input
    }
}

/// The field an action asks for its text in.
public struct NotificationTextInput: Codable, Equatable, Sendable {
    /// The button that sends it.
    public var button: String
    public var placeholder: String

    public init(button: String, placeholder: String) {
        self.button = button
        self.placeholder = placeholder
    }
}

/// What the owner did with a notification: the request's id, category and
/// `userInfo`, and what was chosen, for the app to turn into a command.
public struct NotificationResponse: Codable, Equatable, Sendable {
    public enum Action: Codable, Equatable, Sendable {
        /// The notification itself was tapped.
        case open
        /// It was dismissed, where its category reports that.
        case dismiss
        /// One of its category's actions, by id.
        case named(String)
    }

    public var id: String
    public var category: String?
    public var action: Action
    public var userInfo: [String: String]
    /// The line an action with an input was sent with.
    public var text: String?

    public init(id: String, category: String?, action: Action, userInfo: [String: String] = [:], text: String? = nil) {
        self.id = id
        self.category = category
        self.action = action
        self.userInfo = userInfo
        self.text = text
    }
}
