#if canImport(UserNotifications)
import Foundation
import SubstrateDevice
import UserNotifications

/// Notifications through `UNUserNotificationCenter`, whose delegate is the
/// kit's: it keeps a request quiet in front where the request asks
/// (`showsWhileActive`), and hands every tap and action to `responses()`.
/// An app calls `installDelegate()` before it finishes launching, so the tap
/// that launched it is kept for the first reader rather than dropped.
public struct SystemNotifications: Notifications {
    /// Whether the permission asked for covers the number on the icon, for
    /// an app that sets one.
    public let badges: Bool

    public init(badges: Bool = false) {
        self.badges = badges
    }

    /// Makes the kit's delegate the notification center's. Idempotent; an
    /// app that sets a delegate of its own after this takes the responses
    /// and the in-front rule back.
    @MainActor
    public static func installDelegate() {
        UNUserNotificationCenter.current().delegate = SystemNotificationDelegate.shared
    }

    public func permission() async -> NotificationPermission {
        Self.permission(of: await UNUserNotificationCenter.current().notificationSettings())
    }

    /// Asks for alerts and sound, and the badge where the app sets one:
    /// being time sensitive comes from the app's entitlement, not an
    /// option.
    public func requestPermission() async -> NotificationPermission {
        let center = UNUserNotificationCenter.current()
        if await center.notificationSettings().authorizationStatus == .notDetermined {
            _ = try? await center.requestAuthorization(options: badges ? [.alert, .sound, .badge] : [.alert, .sound])
        }
        return await permission()
    }

    public func register(_ categories: [NotificationCategory]) async {
        UNUserNotificationCenter.current().setNotificationCategories(Set(categories.map(Self.category)))
    }

    public func schedule(_ request: NotificationRequest) async {
        var trigger: UNNotificationTrigger?
        if let fireAt = request.fireAt {
            let interval = fireAt.timeIntervalSinceNow
            guard interval > 0 else { return }
            trigger = UNTimeIntervalNotificationTrigger(timeInterval: max(1, interval), repeats: false)
        }
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
        var userInfo: [String: String] = request.userInfo
        if !request.showsWhileActive { userInfo[SystemNotificationDelegate.quietInFront] = "true" }
        content.userInfo = userInfo
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

    public func delivered() async -> [DeliveredNotification] {
        await UNUserNotificationCenter.current().deliveredNotifications().map { notification in
            let content = notification.request.content
            var userInfo = PushPayload.flatten(content.userInfo)
            userInfo[SystemNotificationDelegate.quietInFront] = nil
            return DeliveredNotification(
                id: notification.request.identifier, thread: content.threadIdentifier.isEmpty ? nil : content.threadIdentifier,
                userInfo: userInfo)
        }
    }

    public func showing(threads: Set<String>) async {
        SystemNotificationDelegate.shared.show(threads)
    }

    public func responses() -> AsyncStream<NotificationResponse> {
        SystemNotificationDelegate.shared.responses()
    }

    public func finished(_ response: NotificationResponse) async {
        guard case .named = response.action else { return }
        SystemNotificationDelegate.shared.waits.finish(ResponseWaits.key(response))
    }

    static func category(_ category: NotificationCategory) -> UNNotificationCategory {
        let actions = category.actions.map { action -> UNNotificationAction in
            var options: UNNotificationActionOptions = []
            if action.opensApp { options.insert(.foreground) }
            if action.destructive { options.insert(.destructive) }
            guard let input = action.input else {
                return UNNotificationAction(identifier: action.id, title: action.title, options: options)
            }
            return UNTextInputNotificationAction(
                identifier: action.id, title: action.title, options: options,
                textInputButtonTitle: input.button, textInputPlaceholder: input.placeholder)
        }
        return UNNotificationCategory(
            identifier: category.id, actions: actions, intentIdentifiers: [],
            options: category.reportsDismissal ? [.customDismissAction] : [])
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
                settings.notificationCenterSetting != .enabled
            {
                return .off
            }
            #if os(iOS)
            return settings.timeSensitiveSetting == .enabled ? .on : .notTimeSensitive
            #else
            return .on
            #endif
        }
    }
}
/// The one delegate every app's notification center has: what a
/// notification does in front, and every response kept for
/// `SystemNotifications.responses()`. Responses that come before anything
/// reads them wait for the first reader.
final class SystemNotificationDelegate: NSObject, UNUserNotificationCenterDelegate, @unchecked Sendable {
    static let shared = SystemNotificationDelegate()
    /// The `userInfo` key a request quiet in front carries; never in a
    /// response.
    static let quietInFront = "substrate.quietInFront"

    private let lock = NSLock()
    private var held: [NotificationResponse] = []
    /// The threads the app shows in front (`Notifications.showing`).
    private var onScreen: Set<String> = []
    private var readers: [UUID: AsyncStream<NotificationResponse>.Continuation] = [:]
    /// The background actions the app has not said it finished.
    let waits = ResponseWaits()

    func responses() -> AsyncStream<NotificationResponse> {
        let (stream, continuation) = AsyncStream<NotificationResponse>.makeStream()
        let id = UUID()
        lock.withLock {
            for response in held { continuation.yield(response) }
            held = []
            readers[id] = continuation
        }
        continuation.onTermination = { [weak self] _ in
            self?.lock.withLock { self?.readers[id] = nil }
        }
        return stream
    }

    func deliver(_ response: NotificationResponse) {
        lock.withLock {
            if readers.isEmpty {
                held.append(response)
            } else {
                for reader in readers.values { reader.yield(response) }
            }
        }
    }

    func show(_ threads: Set<String>) {
        lock.withLock { onScreen = threads }
    }

    /// In front, a request that asked to be quiet there shows nothing, and
    /// neither does one of a thread the app shows, a push's included: the
    /// screen already says it.
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        let content = notification.request.content
        if content.userInfo[Self.quietInFront] != nil { return [] }
        let thread = content.threadIdentifier
        if !thread.isEmpty, lock.withLock({ onScreen.contains(thread) }) { return [] }
        return [.banner, .list, .sound]
    }

    /// A background action holds the app awake until the app says it is
    /// done with it (`SystemNotifications.finished`): the system may
    /// suspend the app the moment it is answered. A tap brings the app to
    /// the front, and a dismissal asks for nothing: both are answered at
    /// once. The system's handler is called on the main thread, where it
    /// takes the app's snapshot: the `async` form of this method answers
    /// from whichever thread the wait ended on, and UIKit stops the app
    /// there.
    func userNotificationCenter(
        _ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let made = Self.response(response)
        deliver(made)
        guard case .named = made.action else { return completionHandler() }
        let answer = Answer(completionHandler)
        Task { [waits] in
            await waits.wait(for: ResponseWaits.key(made))
            await MainActor.run { answer.call() }
        }
    }

    /// The system's handler, carried to the main thread to be called there
    /// once.
    private final class Answer: @unchecked Sendable {
        private let handler: () -> Void

        init(_ handler: @escaping () -> Void) {
            self.handler = handler
        }

        func call() { handler() }
    }

    static func response(_ response: UNNotificationResponse) -> NotificationResponse {
        let request = response.notification.request
        // A push's own values arrive under `substrate.`.
        var userInfo = PushPayload.flatten(request.content.userInfo)
        userInfo[quietInFront] = nil
        let action: NotificationResponse.Action =
            switch response.actionIdentifier {
            case UNNotificationDefaultActionIdentifier: .open
            case UNNotificationDismissActionIdentifier: .dismiss
            default: .named(response.actionIdentifier)
            }
        let category = request.content.categoryIdentifier
        return NotificationResponse(
            id: request.identifier, category: category.isEmpty ? nil : category, action: action,
            userInfo: userInfo, text: (response as? UNTextInputNotificationResponse)?.userText)
    }
}
#endif
