import Foundation
import TasksCore
@preconcurrency import UserNotifications

/// The app's own notification delegate (the kit's notifications leave it
/// to the app): a reminder due while the app is in front still shows, and a
/// tap on one opens its task, as a command. Set on the center before the
/// app finishes launching, so the tap that launched it is not lost.
final class ReminderDelegate: NSObject, UNUserNotificationCenterDelegate {
    private let open: @MainActor (String) -> Void

    init(open: @escaping @MainActor (String) -> Void) {
        self.open = open
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                willPresent notification: UNNotification) async -> UNNotificationPresentationOptions
    {
        [.banner, .list, .sound]
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let content = response.notification.request.content
        guard content.categoryIdentifier == Reminders.category, let task = content.userInfo["task"] as? String else { return }
        await open(task)
    }
}
