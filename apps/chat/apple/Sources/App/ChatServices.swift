import ChatKit
import SubstrateKit
import UserNotifications

/// The device services Chat's runtime uses, made once, before the app
/// finishes launching: the notification center's delegate must be in place
/// for a tap that launched the app, and iOS takes background task
/// registrations only then.
@MainActor
enum ChatServices {
    static let notifications: any Notifications = SystemNotifications()

    #if os(iOS)
    static let background: (any BackgroundWork)? = BackgroundTasksService(
        jobs: [ChatApp.refreshJob], continuedPrefix: "dev.reamde.substrate.chat.continued.")
    #else
    static let background: (any BackgroundWork)? = nil
    #endif

    static func start() {
        UNUserNotificationCenter.current().delegate = NotificationDelegate.shared
        _ = background
    }
}

/// What a notification does while the app is in front (nothing, for a chat
/// a window shows) and what a tap opens: its chat, in the window that takes
/// the route.
final class NotificationDelegate: NSObject, UNUserNotificationCenterDelegate, @unchecked Sendable {
    static let shared = NotificationDelegate()

    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                willPresent notification: UNNotification) async -> UNNotificationPresentationOptions
    {
        let thread = notification.request.content.userInfo[ChatNotifications.threadKey] as? String
        let onScreen = await MainActor.run { thread.map { ChatHost.shared.chats?.onScreen.contains($0) ?? false } ?? false }
        return onScreen ? [] : [.banner, .list, .sound]
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        guard let thread = response.notification.request.content.userInfo[ChatNotifications.threadKey] as? String else { return }
        await MainActor.run { ChatHost.shared.route(.thread(thread)) }
    }
}
