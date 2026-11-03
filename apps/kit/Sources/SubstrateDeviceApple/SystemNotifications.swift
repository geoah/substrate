#if canImport(UserNotifications)
import Foundation
import SubstrateDevice
@preconcurrency import UserNotifications

/// Notifications through UserNotifications. Made once, at launch, before
/// the app finishes launching: it becomes the notification center's
/// delegate, so a tap that launched the app is kept until a reader comes
/// for it, and a notification due while the app is in front still shows.
public final class SystemNotifications: NSObject, Notifications, UNUserNotificationCenterDelegate, @unchecked Sendable {
    private let center: UNUserNotificationCenter
    private let lock = NSLock()
    private var readers: [Int: AsyncStream<NotificationTap>.Continuation] = [:]
    private var next = 0
    /// Taps that came before any reader.
    private var early: [NotificationTap] = []

    public init(center: UNUserNotificationCenter = .current()) {
        self.center = center
        super.init()
        center.delegate = self
    }

    public func permission() async -> NotificationPermission {
        Self.permission(await center.notificationSettings().authorizationStatus)
    }

    public func requestPermission() async -> NotificationPermission {
        let now = await permission()
        guard now == .notAsked else { return now }
        _ = try? await center.requestAuthorization(options: [.alert, .sound, .badge])
        return await permission()
    }

    public func pending(prefix: String) async -> [String] {
        await center.pendingNotificationRequests().map(\.identifier).filter { $0.hasPrefix(prefix) }.sorted()
    }

    public func schedule(_ requests: [NotificationRequest]) async throws {
        for request in requests {
            let content = UNMutableNotificationContent()
            content.title = request.title
            content.body = request.body
            content.sound = .default
            content.userInfo = request.payload
            // An instant, not a wall-clock time: a time zone change leaves it
            // where it is, and the app plans a day's hour again itself. The
            // system refuses an interval that is not positive.
            let trigger = UNTimeIntervalNotificationTrigger(timeInterval: max(1, request.at.timeIntervalSinceNow), repeats: false)
            try await center.add(UNNotificationRequest(identifier: request.id, content: content, trigger: trigger))
        }
    }

    public func cancel(_ ids: [String]) async {
        center.removePendingNotificationRequests(withIdentifiers: ids)
    }

    public func taps() -> AsyncStream<NotificationTap> {
        let (stream, continuation) = AsyncStream<NotificationTap>.makeStream()
        let (id, waiting) = lock.withLock { () -> (Int, [NotificationTap]) in
            next += 1
            readers[next] = continuation
            defer { early = [] }
            return (next, early)
        }
        for tap in waiting { continuation.yield(tap) }
        continuation.onTermination = { [weak self] _ in self?.lock.withLock { _ = self?.readers.removeValue(forKey: id) } }
        return stream
    }

    public func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let info = response.notification.request.content.userInfo
        var payload: [String: String] = [:]
        for (key, value) in info {
            if let key = key as? String, let value = value as? String { payload[key] = value }
        }
        let tap = NotificationTap(id: response.notification.request.identifier, payload: payload)
        let readers = lock.withLock { () -> [AsyncStream<NotificationTap>.Continuation] in
            if self.readers.isEmpty { early.append(tap) }
            return Array(self.readers.values)
        }
        for reader in readers { reader.yield(tap) }
    }

    public func userNotificationCenter(_ center: UNUserNotificationCenter,
                                       willPresent notification: UNNotification) async -> UNNotificationPresentationOptions
    {
        [.banner, .list, .sound]
    }

    static func permission(_ status: UNAuthorizationStatus) -> NotificationPermission {
        switch status {
        case .notDetermined: .notAsked
        case .denied: .denied
        default: .allowed
        }
    }
}
#endif
