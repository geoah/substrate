import Foundation

/// Local notifications as a device service (§5.4): the permission, a
/// notification scheduled or cancelled by id, and each one the person taps,
/// as its payload, which the app turns into a command. An app with no push
/// channel says what it observed itself.
public protocol Notifications: Sendable {
    func permission() async -> NotificationPermission
    /// Asks the person, where the system still lets the app ask; answers
    /// whether notifications are allowed.
    func requestPermission() async -> Bool
    /// Shows the notification now, replacing one with the same id.
    func schedule(_ notification: LocalNotification) async
    /// Removes the notifications with these ids, waiting or shown.
    func cancel(ids: [String]) async
    /// Removes the shown notifications of one group (a chat's).
    func cancel(group: String) async
    func cancelAll() async
    /// The number on the app's icon.
    func setBadge(_ count: Int) async
    /// The groups whose notifications are not shown while the app is in
    /// front: what is on screen already says it.
    func quiet(groups: Set<String>) async
    /// The payload of each notification the person taps from now on, the
    /// one that opened the app first.
    func taps() -> AsyncStream<[String: String]>
}

public enum NotificationPermission: String, Codable, Hashable, Sendable {
    /// Not asked yet.
    case undetermined
    case denied
    case allowed
}

/// One local notification, as a value.
public struct LocalNotification: Codable, Hashable, Sendable {
    public enum Urgency: String, Codable, Hashable, Sendable {
        case active
        /// Breaks through a focus: something waits on the person.
        case timeSensitive
    }

    public var id: String
    public var title: String
    public var subtitle: String?
    public var body: String
    /// Notifications of one group show together, and leave together.
    public var group: String?
    /// What a tap hands back.
    public var payload: [String: String]
    public var urgency: Urgency
    public var sound: Bool

    public init(id: String, title: String, subtitle: String? = nil, body: String, group: String? = nil, payload: [String: String] = [:],
                urgency: Urgency = .active, sound: Bool = true)
    {
        self.id = id
        self.title = title
        self.subtitle = subtitle
        self.body = body
        self.group = group
        self.payload = payload
        self.urgency = urgency
        self.sound = sound
    }
}
