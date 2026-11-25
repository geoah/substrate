import Foundation
import SubstrateChatCore
import UserNotifications

/// What the extension does with what `ChatPush.content` answered: the
/// delivered content, made once. Its own file so the app's tests compile it
/// too: an extension has no test target of its own.
enum PushDelivery {
    static func apply(_ content: PushContent, to best: UNMutableNotificationContent) {
        switch content {
        case let .fallback(dropsBadge):
            // Nil leaves the icon as it is: another sign-in's count is not
            // this one's.
            if dropsBadge { best.badge = nil }
        case let .shown(request):
            best.title = request.title
            if let subtitle = request.subtitle { best.subtitle = subtitle }
            if let body = request.body { best.body = body }
            if let thread = request.thread { best.threadIdentifier = thread }
            // An empty category offers no action.
            best.categoryIdentifier = request.category ?? ""
            #if os(iOS)
            if request.timeSensitive { best.interruptionLevel = .timeSensitive }
            #endif
            // The relay's `substrate` object stays: a tap, an action and a
            // take-back read the generation and the event id from it.
            var userInfo = best.userInfo
            for (key, value) in request.userInfo { userInfo[key] = value }
            best.userInfo = userInfo
        }
    }
}

/// The system's handler, called once: by the work when it ends, or by
/// `serviceExtensionTimeWillExpire` (`expire(_:)`), whichever is first.
final class DeliverOnce: @unchecked Sendable {
    private let lock = NSLock()
    private var handler: ((UNNotificationContent) -> Void)?
    private let best: UNMutableNotificationContent

    init(_ best: UNMutableNotificationContent, handler: @escaping (UNNotificationContent) -> Void) {
        self.best = best
        self.handler = handler
    }

    /// Delivers `content` applied, or the push as it came where it is nil;
    /// false where it was delivered already.
    @discardableResult
    func deliver(_ content: PushContent?) -> Bool {
        let handler = lock.withLock {
            defer { self.handler = nil }
            return self.handler
        }
        guard let handler else { return false }
        if let content { PushDelivery.apply(content, to: best) }
        handler(best)
        return true
    }

    /// The system ends the extension before the work answered: what the last
    /// check before showing confirmed, or else the plain fallback with no
    /// chat text and no badge.
    @discardableResult
    func expire(_ expiry: PushExpiry?) -> Bool { deliver(expiry?.atExpiry ?? .fallback(dropsBadge: true)) }
}

/// One push's delivery in the extension: the work, and the system's expiry,
/// which claims the delivery before it stops the work. A work stopped first
/// could answer from its cancellation and win; claimed first, whatever the
/// work answers after is delivered nowhere.
final class PushRun: @unchecked Sendable {
    private let lock = NSLock()
    private let once: DeliverOnce
    private let expiry = PushExpiry()
    private var work: Task<Void, Never>?

    init(_ best: UNMutableNotificationContent, handler: @escaping (UNNotificationContent) -> Void) {
        once = DeliverOnce(best, handler: handler)
    }

    func start(_ userInfo: [String: String], accepting: @escaping ChatPush.Accepting, until deadline: ContinuousClock.Instant) {
        let (once, expiry) = (once, expiry)
        let task = Task {
            let content = await ChatPush.content(userInfo, accepting: accepting, until: deadline, expiry: expiry)
            once.deliver(content)
        }
        lock.withLock { work = task }
    }

    /// The system's time is up: what the last check confirmed, or the plain
    /// fallback with no chat text and no badge, is delivered first; then the
    /// work is stopped.
    @discardableResult
    func expire() -> Bool {
        let delivered = once.expire(expiry)
        lock.withLock { work }?.cancel()
        return delivered
    }
}
