import Foundation
import Observation
import os
import WorkoutsCore
import UIKit
import UserNotifications

/// The countdown notification (DESIGN.md §9.2): one pending request at a
/// time under a fixed identifier, time-sensitive, replaced on every change
/// by the countdown coordinator, which is the only caller of `schedule` and
/// `clear` (§6.2). Also the permission, the delegate that keeps the banner
/// quiet while the session cover shows, and the tap that opens it.
@MainActor
@Observable
final class Notifications {
    static let shared = Notifications()

    /// The one request's identifier: adding it again replaces it.
    nonisolated static let countdownID = "workouts.countdown"
    /// Its category. No actions: a rest-over alert is read, not answered,
    /// and a tap opens the session where every control is.
    nonisolated static let countdownCategory = "workouts.countdown"
    nonisolated static let sessionKey = "session"

    /// Notifications are not allowed, or allowed with no way to show them:
    /// the session screen says "Alerts are off" (§9.6). The Live Activity
    /// and the haptics still work.
    private(set) var alertsOff = false
    /// Alerts are on but not time sensitive (switched off for Workouts, or
    /// the entitlement missing): a Focus may hold them back.
    private(set) var timeSensitiveOff = false
    /// The owner has not been asked yet; the first Start asks.
    private(set) var notAsked = false

    /// What a tap on the notification does. Unset, it opens
    /// `substrate-workouts://session`, which the app's deep-link handler turns into
    /// the session cover scrolled to Up next.
    @ObservationIgnored var onOpenSession: (@MainActor () -> Void)?

    @ObservationIgnored private let delegate = Delegate()
    @ObservationIgnored private let logger = Logger(subsystem: "dev.reamde.substrate.workouts", category: "notifications")

    /// Installs the delegate on first use: at the latest when the launch
    /// restores the session, early enough for a tap to land somewhere.
    private init() {
        install()
    }

    /// Sets the delegate and the category; calling it again is harmless.
    /// `CountdownCoordinator.install()` calls it from `SubstrateWorkoutsApp.init`,
    /// before the app finishes launching, so a tap that launched the app is
    /// delivered to the delegate rather than dropped.
    func install() {
        let center = UNUserNotificationCenter.current()
        center.delegate = delegate
        center.setNotificationCategories([
            UNNotificationCategory(identifier: Self.countdownCategory, actions: [], intentIdentifiers: [], options: []),
        ])
    }

    /// At the first Start (§9.6): the system prompts once; afterwards this
    /// only re-reads the settings. Asks for alerts and sound only: the
    /// time-sensitive level comes from the entitlement, not an option.
    func requestAuthorizationIfNeeded() async {
        let center = UNUserNotificationCenter.current()
        if await center.notificationSettings().authorizationStatus == .notDetermined {
            do {
                let granted = try await center.requestAuthorization(options: [.alert, .sound])
                logger.info("Notification permission: \(granted ? "granted" : "refused", privacy: .public)")
            } catch {
                logger.error("Notification permission request failed: \(error.localizedDescription, privacy: .public)")
            }
        }
        await refresh()
    }

    /// Re-reads the settings: at launch, on every return to the foreground,
    /// and after the prompt.
    func refresh() async {
        let settings = await UNUserNotificationCenter.current().notificationSettings()
        notAsked = settings.authorizationStatus == .notDetermined
        switch settings.authorizationStatus {
        case .authorized, .provisional, .ephemeral:
            // Allowed but shown nowhere still means the owner never sees it.
            alertsOff = settings.alertSetting != .enabled
                && settings.lockScreenSetting != .enabled
                && settings.notificationCenterSetting != .enabled
        case .denied:
            alertsOff = true
        default:
            // Not asked yet: Start asks.
            alertsOff = false
        }
        timeSensitiveOff = !alertsOff && !notAsked && settings.timeSensitiveSetting != .enabled
    }

    // MARK: - The countdown coordinator's calls

    /// Removes the pending and the delivered countdown notification.
    func clear() {
        let center = UNUserNotificationCenter.current()
        center.removePendingNotificationRequests(withIdentifiers: [Self.countdownID])
        center.removeDeliveredNotifications(withIdentifiers: [Self.countdownID])
    }

    /// Adds the countdown's notification, replacing any pending one, when its
    /// end is still ahead. A refused permission makes this a quiet no-op.
    func schedule(_ notification: CountdownNotification, session: SessionID, now: Date = .now) async {
        let interval = notification.fireDate.timeIntervalSince(now)
        guard interval > 0 else { return }
        let content = UNMutableNotificationContent()
        content.title = notification.title
        if let body = notification.body { content.body = body }
        content.sound = .default
        // Breaks through a Focus; without the entitlement the system
        // delivers it at .active instead, with no code change (§9.2).
        content.interruptionLevel = .timeSensitive
        content.threadIdentifier = session.rawValue
        content.categoryIdentifier = Self.countdownCategory
        content.userInfo = [Self.sessionKey: session.rawValue]
        let trigger = UNTimeIntervalNotificationTrigger(timeInterval: max(1, interval), repeats: false)
        let request = UNNotificationRequest(identifier: Self.countdownID, content: content, trigger: trigger)
        do {
            try await UNUserNotificationCenter.current().add(request)
        } catch {
            logger.error("Countdown notification not scheduled: \(error.localizedDescription, privacy: .public)")
        }
    }

    // MARK: - Delegate

    fileprivate func openSession() {
        if let onOpenSession {
            onOpenSession()
        } else {
            UIApplication.shared.open(WorkoutsLinks.session)
        }
    }

    /// The system calls the delegate off the main actor, so the class is
    /// nonisolated; each method hops there for the one flag or action it needs.
    private nonisolated final class Delegate: NSObject, UNUserNotificationCenterDelegate {
        func userNotificationCenter(
            _: UNUserNotificationCenter,
            willPresent notification: UNNotification
        ) async -> UNNotificationPresentationOptions {
            guard notification.request.identifier == Notifications.countdownID else { return [.banner, .sound] }
            // The cover's rest bar and haptic already say it.
            let showing = await MainActor.run { SessionScreen.shared.isShowing }
            return showing ? [] : [.banner, .sound]
        }

        func userNotificationCenter(
            _: UNUserNotificationCenter,
            didReceive response: UNNotificationResponse
        ) async {
            guard response.notification.request.content.categoryIdentifier == Notifications.countdownCategory,
                  response.actionIdentifier == UNNotificationDefaultActionIdentifier
            else { return }
            await MainActor.run { Notifications.shared.openSession() }
        }
    }
}
