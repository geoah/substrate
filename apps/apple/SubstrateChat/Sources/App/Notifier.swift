import Foundation
import UserNotifications

/// Local notifications: there is no push channel, so the app says what it
/// observed itself. Every notification carries its thread, and tapping it
/// opens that chat.
@MainActor
enum Notifier {
    nonisolated static let threadKey = "thread"

    static func requestAuthorization() async -> Bool {
        (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])) ?? false
    }

    static func status() async -> UNAuthorizationStatus {
        await UNUserNotificationCenter.current().notificationSettings().authorizationStatus
    }

    /// A reply settled in a chat that is not on screen.
    static func reply(thread: ChatThread, agent: String, title: String) {
        let content = UNMutableNotificationContent()
        content.title = agent
        content.subtitle = title
        content.body = thread.preview ?? "Answered."
        content.sound = .default
        content.threadIdentifier = thread.id
        content.userInfo = [threadKey: thread.id]
        content.interruptionLevel = .active
        post(content, id: "reply.\(thread.id)")
    }

    /// A question or a suggestion is waiting on the owner.
    static func attention(record: Record, thread: String?, agent: String, title: String) {
        let content = UNMutableNotificationContent()
        content.title = agent
        content.subtitle = title
        if record.kind == Kinds.request {
            content.body = "Suggested a change for you to apply or dismiss."
        } else {
            let n = record["questions"]?.array?.count ?? 1
            content.body = n == 1 ? "Has a question for you." : "Has \(n) questions for you."
        }
        content.sound = .default
        content.interruptionLevel = .timeSensitive
        if let thread {
            content.threadIdentifier = thread
            content.userInfo = [threadKey: thread]
        }
        post(content, id: "card.\(record.id)")
    }

    static func badge(_ count: Int) {
        UNUserNotificationCenter.current().setBadgeCount(count)
    }

    /// A chat was opened: what it notified is no longer news.
    static func clear(thread: String) {
        let center = UNUserNotificationCenter.current()
        center.getDeliveredNotifications { delivered in
            let ids = delivered.filter { $0.request.content.threadIdentifier == thread }.map(\.request.identifier)
            center.removeDeliveredNotifications(withIdentifiers: ids)
        }
    }

    static func removeAll() {
        UNUserNotificationCenter.current().removeAllDeliveredNotifications()
        UNUserNotificationCenter.current().removeAllPendingNotificationRequests()
        badge(0)
    }

    private static func post(_ content: UNMutableNotificationContent, id: String) {
        let request = UNNotificationRequest(identifier: id, content: content, trigger: nil)
        UNUserNotificationCenter.current().add(request)
    }
}

/// Routes a tapped notification to its chat, and decides what shows while
/// the app is open: nothing for a chat already on screen.
final class NotificationDelegate: NSObject, UNUserNotificationCenterDelegate, @unchecked Sendable {
    static let shared = NotificationDelegate()

    func userNotificationCenter(
        _ center: UNUserNotificationCenter, willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        let thread = notification.request.content.userInfo[Notifier.threadKey] as? String
        let onScreen = await MainActor.run { thread.map { AppModel.shared.store?.isVisible($0) ?? false } ?? false }
        return onScreen ? [] : [.banner, .list, .sound]
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        guard let thread = response.notification.request.content.userInfo[Notifier.threadKey] as? String else { return }
        await MainActor.run { AppModel.shared.route(.thread(thread)) }
    }
}
