import Foundation
import UIKit
import UserNotifications
import WorkoutsCore

/// What the countdown notification (DESIGN.md §9.2) does once the kit's
/// notification service has scheduled it: quiet while the session cover
/// shows, since its rest bar and haptic already say it, and a tap opens the
/// cover. Installed from `SubstrateWorkoutsApp.init`, before the app
/// finishes launching, so a tap that launched the app is delivered here
/// rather than dropped.
final class NotificationDelegate: NSObject, UNUserNotificationCenterDelegate {
    static let shared = NotificationDelegate()

    /// Sets the delegate and the category; calling it again is harmless.
    func install() {
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        // No actions: a rest-over alert is read, not answered, and a tap
        // opens the session where every control is.
        center.setNotificationCategories([
            UNNotificationCategory(identifier: SessionController.countdownCategory, actions: [], intentIdentifiers: [], options: []),
        ])
    }

    nonisolated func userNotificationCenter(_: UNUserNotificationCenter, willPresent notification: UNNotification) async
        -> UNNotificationPresentationOptions
    {
        guard notification.request.identifier == SessionController.countdownID else { return [.banner, .sound] }
        let showing = await MainActor.run { SessionScreen.shared.isShowing }
        return showing ? [] : [.banner, .sound]
    }

    nonisolated func userNotificationCenter(_: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        guard response.notification.request.content.categoryIdentifier == SessionController.countdownCategory,
              response.actionIdentifier == UNNotificationDefaultActionIdentifier
        else { return }
        await MainActor.run { DeepLinks.openSession() }
    }
}
