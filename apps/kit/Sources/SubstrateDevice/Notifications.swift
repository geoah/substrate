import Foundation

/// Whether the person lets the app notify them. `notAsked` is told apart
/// from `denied`: only the first may be asked, since a system shows its
/// question once.
public enum NotificationPermission: String, Codable, Hashable, Sendable {
    case notAsked
    case allowed
    case denied
}

/// One notification the app asks the device to show at an instant, by an
/// id of the app's: scheduling an id again replaces the one before.
public struct NotificationRequest: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var title: String
    public var body: String
    public var at: Date
    /// What the app is told when the person taps it, as `NotificationTap`.
    public var payload: [String: String]

    public init(id: String, title: String, body: String, at: Date, payload: [String: String] = [:]) {
        self.id = id
        self.title = title
        self.body = body
        self.at = at
        self.payload = payload
    }
}

/// A notification the person tapped: its id and the payload it was
/// scheduled with, which the app turns into a command.
public struct NotificationTap: Codable, Hashable, Sendable {
    public var id: String
    public var payload: [String: String]

    public init(id: String, payload: [String: String]) {
        self.id = id
        self.payload = payload
    }
}

/// Notifications as a device service (§5.4): UserNotifications on an Apple
/// system, `NotificationManager` on Android, a scripted one in a test. Each
/// call answers a value; a tap arrives on `taps()`, never as a callback
/// into the core.
public protocol Notifications: Sendable {
    func permission() async -> NotificationPermission
    /// Asks the person where it was never asked; answers what stands.
    func requestPermission() async -> NotificationPermission
    /// The ids scheduled and not yet shown, among those starting with
    /// `prefix`.
    func pending(prefix: String) async -> [String]
    func schedule(_ requests: [NotificationRequest]) async throws
    func cancel(_ ids: [String]) async
    /// Each tap, from now on, until the reader stops reading.
    func taps() -> AsyncStream<NotificationTap>
}
