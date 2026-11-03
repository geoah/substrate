#if canImport(UserNotifications)
import Foundation
import SubstrateDevice
@preconcurrency import UserNotifications

/// Notifications through UserNotifications. It is the notification
/// center's delegate, so make it once, before the app finishes launching:
/// a tap that launched the app is held until something reads `taps()`.
public final class UserNotificationsService: NSObject, Notifications, UNUserNotificationCenterDelegate, @unchecked Sendable {
    private let lock = NSLock()
    private var quietGroups: Set<String> = []
    private var listeners: [UUID: AsyncStream<[String: String]>.Continuation] = [:]
    private var held: [[String: String]] = []

    public override init() {
        super.init()
        UNUserNotificationCenter.current().delegate = self
    }

    public func permission() async -> NotificationPermission {
        switch await UNUserNotificationCenter.current().notificationSettings().authorizationStatus {
        case .notDetermined: .undetermined
        case .denied: .denied
        default: .allowed
        }
    }

    public func requestPermission() async -> Bool {
        (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])) ?? false
    }

    public func schedule(_ notification: LocalNotification) async {
        let content = UNMutableNotificationContent()
        content.title = notification.title
        if let subtitle = notification.subtitle { content.subtitle = subtitle }
        content.body = notification.body
        if notification.sound { content.sound = .default }
        if let group = notification.group { content.threadIdentifier = group }
        content.userInfo = notification.payload
        content.interruptionLevel = notification.urgency == .timeSensitive ? .timeSensitive : .active
        try? await UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: notification.id, content: content, trigger: nil))
    }

    public func cancel(ids: [String]) async {
        let center = UNUserNotificationCenter.current()
        center.removePendingNotificationRequests(withIdentifiers: ids)
        center.removeDeliveredNotifications(withIdentifiers: ids)
    }

    public func cancel(group: String) async {
        let center = UNUserNotificationCenter.current()
        let ids = await center.deliveredNotifications().filter { $0.request.content.threadIdentifier == group }.map(\.request.identifier)
        center.removeDeliveredNotifications(withIdentifiers: ids)
    }

    public func cancelAll() async {
        let center = UNUserNotificationCenter.current()
        center.removeAllDeliveredNotifications()
        center.removeAllPendingNotificationRequests()
        try? await center.setBadgeCount(0)
    }

    public func setBadge(_ count: Int) async {
        try? await UNUserNotificationCenter.current().setBadgeCount(count)
    }

    public func quiet(groups: Set<String>) async {
        lock.withLock { quietGroups = groups }
    }

    public func taps() -> AsyncStream<[String: String]> {
        let (stream, continuation) = AsyncStream<[String: String]>.makeStream()
        let id = UUID()
        let waiting = lock.withLock { () -> [[String: String]] in
            listeners[id] = continuation
            defer { held = [] }
            return held
        }
        for payload in waiting { continuation.yield(payload) }
        continuation.onTermination = { [weak self] _ in self?.lock.withLock { _ = self?.listeners.removeValue(forKey: id) } }
        return stream
    }

    public func userNotificationCenter(_ center: UNUserNotificationCenter,
                                       willPresent notification: UNNotification) async -> UNNotificationPresentationOptions
    {
        let group = notification.request.content.threadIdentifier
        return lock.withLock { quietGroups.contains(group) } ? [] : [.banner, .list, .sound]
    }

    public func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        var payload: [String: String] = [:]
        for (key, value) in response.notification.request.content.userInfo {
            if let key = key as? String, let value = value as? String { payload[key] = value }
        }
        let listening = lock.withLock { () -> [AsyncStream<[String: String]>.Continuation] in
            if listeners.isEmpty { held.append(payload) }
            return Array(listeners.values)
        }
        for continuation in listening { continuation.yield(payload) }
    }
}
#endif
