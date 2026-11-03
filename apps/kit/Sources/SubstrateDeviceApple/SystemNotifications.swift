#if canImport(UserNotifications)
import Foundation
import SubstrateDevice
import UserNotifications

/// Notifications through `UNUserNotificationCenter`. The delegate, which
/// decides what a notification does while the app is in front and what a
/// tap opens, is the app's: it sets it on the same center before the app
/// finishes launching.
public struct SystemNotifications: Notifications {
    public init() {}

    public func permission() async -> NotificationPermission {
        Self.permission(of: await UNUserNotificationCenter.current().notificationSettings())
    }

    /// Asks for alerts and sound only: being time sensitive comes from the
    /// app's entitlement, not an option.
    public func requestPermission() async -> NotificationPermission {
        let center = UNUserNotificationCenter.current()
        if await center.notificationSettings().authorizationStatus == .notDetermined {
            _ = try? await center.requestAuthorization(options: [.alert, .sound])
        }
        return await permission()
    }

    public func schedule(_ request: NotificationRequest) async {
        let interval = request.fireAt.timeIntervalSinceNow
        guard interval > 0 else { return }
        let content = UNMutableNotificationContent()
        content.title = request.title
        if let subtitle = request.subtitle { content.subtitle = subtitle }
        if let body = request.body { content.body = body }
        content.sound = .default
        #if os(iOS)
        // Without the entitlement the system delivers it as active, with no
        // code change.
        if request.timeSensitive { content.interruptionLevel = .timeSensitive }
        #endif
        if let thread = request.thread { content.threadIdentifier = thread }
        if let category = request.category { content.categoryIdentifier = category }
        content.userInfo = request.userInfo
        let trigger = UNTimeIntervalNotificationTrigger(timeInterval: max(1, interval), repeats: false)
        try? await UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: request.id, content: content, trigger: trigger))
    }

    public func cancel(_ id: String) async {
        let center = UNUserNotificationCenter.current()
        center.removePendingNotificationRequests(withIdentifiers: [id])
        center.removeDeliveredNotifications(withIdentifiers: [id])
    }

    public func cancel(thread: String) async {
        let center = UNUserNotificationCenter.current()
        let ids = await center.deliveredNotifications().filter { $0.request.content.threadIdentifier == thread }.map(\.request.identifier)
        center.removeDeliveredNotifications(withIdentifiers: ids)
    }

    public func cancelAll() async {
        let center = UNUserNotificationCenter.current()
        center.removeAllPendingNotificationRequests()
        center.removeAllDeliveredNotifications()
        try? await center.setBadgeCount(0)
    }

    public func setBadge(_ count: Int) async {
        try? await UNUserNotificationCenter.current().setBadgeCount(count)
    }

    public func pending(prefix: String) async -> [String] {
        await UNUserNotificationCenter.current().pendingNotificationRequests().map(\.identifier).filter { $0.hasPrefix(prefix) }.sorted()
    }

    static func permission(of settings: UNNotificationSettings) -> NotificationPermission {
        switch settings.authorizationStatus {
        case .notDetermined:
            return .notAsked
        case .denied:
            return .off
        default:
            // Allowed but shown nowhere still means the owner never sees one.
            if settings.alertSetting != .enabled, settings.lockScreenSetting != .enabled,
               settings.notificationCenterSetting != .enabled { return .off }
            #if os(iOS)
            return settings.timeSensitiveSetting == .enabled ? .on : .notTimeSensitive
            #else
            return .on
            #endif
        }
    }
}
#endif
