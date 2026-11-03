import Foundation

/// The device's local notifications (§5.4): whether the owner lets the app
/// alert, one request scheduled or replaced by its id, and a request taken
/// back. A tap reaches the app through its own delegate, as a command.
public protocol Notifications: Sendable {
    /// Where the owner's permission stands now.
    func permission() async -> NotificationPermission
    /// Asks the owner once, where they were never asked; answers where the
    /// permission stands after.
    func requestPermission() async -> NotificationPermission
    /// Schedules the request, replacing a pending one of the same id. A
    /// permission refused makes it a quiet no-op.
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
}

public extension Notifications {
    func cancel(thread: String) async {}
    func cancelAll() async {}
    func setBadge(_ count: Int) async {}
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

/// One notification to show at an instant.
public struct NotificationRequest: Codable, Equatable, Sendable {
    public var id: String
    public var title: String
    /// A line under the title: what the notification is about.
    public var subtitle: String?
    public var body: String?
    public var fireAt: Date
    /// Notifications of one thread group together.
    public var thread: String?
    /// The app's category, which its delegate reads on a tap.
    public var category: String?
    /// Breaks through a Focus where the app may.
    public var timeSensitive: Bool
    public var userInfo: [String: String]

    public init(id: String, title: String, body: String? = nil, fireAt: Date, thread: String? = nil, category: String? = nil,
                timeSensitive: Bool = false, userInfo: [String: String] = [:], subtitle: String? = nil)
    {
        self.id = id
        self.title = title
        self.subtitle = subtitle
        self.body = body
        self.fireAt = fireAt
        self.thread = thread
        self.category = category
        self.timeSensitive = timeSensitive
        self.userInfo = userInfo
    }
}
