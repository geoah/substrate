import ChatKit
import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import SubstrateDeviceApple
import UserNotifications

/// What a push from the publisher's relay shows: the relay sends no words,
/// so this asks the person's own server what the push is about, with the
/// app's own token from the Keychain group the app and this extension
/// share, and shows it as the app's local notification of the same item
/// would (`ChatPush.content`). One deadline covers every read; what was
/// read by the time iOS calls time, or nothing, is delivered, and on any
/// failure the alert keeps the app's own fallback words. It links the
/// chat's core and the kit's API, never a view: iOS holds an extension to
/// a small memory limit.
final class NotificationService: UNNotificationServiceExtension, @unchecked Sendable {
    private let lock = NSLock()
    private var handler: ((UNNotificationContent) -> Void)?
    private var best: UNMutableNotificationContent?
    private var work: Task<Void, Never>?

    /// The session where the app keeps it: the same service, account and
    /// accessibility, readable after the first unlock.
    static let sessions = SessionStore(account: "session", storage: KeychainStorage(service: "dev.reamde.substrate.chat"))

    override func didReceive(_ request: UNNotificationRequest, withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void) {
        guard let best = request.content.mutableCopy() as? UNMutableNotificationContent else { return contentHandler(request.content) }
        lock.withLock {
            handler = contentHandler
            self.best = best
        }
        let userInfo = PushPayload.flatten(request.content.userInfo)
        let deadline = Date().addingTimeInterval(ChatPush.budget)
        let task = Task { [weak self] in
            let registrar = PushRegistrar(sessions: Self.sessions, device: nil, buildRelay: nil, clientID: "")
            var accepted = false
            if let push = PushPayload(userInfo) { accepted = await registrar.accepts(push.generation) }
            let client = await Self.sessions.load().map { SubstrateClient($0) }
            let content = await ChatPush.content(userInfo, accepted: accepted, client: client, until: deadline)
            self?.deliver(content)
        }
        lock.withLock { work = task }
    }

    override func serviceExtensionTimeWillExpire() {
        lock.withLock { work }?.cancel()
        deliver(nil)
    }

    /// Delivers once: what was made, or the push as it came.
    private func deliver(_ content: PushContent?) {
        let (handler, best) = lock.withLock {
            defer { self.handler = nil }
            return (self.handler, self.best)
        }
        guard let handler, let best else { return }
        if let content { Self.apply(content, to: best) }
        handler(best)
    }

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
            best.categoryIdentifier = request.category ?? ""
            if request.timeSensitive { best.interruptionLevel = .timeSensitive }
            var userInfo = best.userInfo
            for (key, value) in request.userInfo { userInfo[key] = value }
            best.userInfo = userInfo
        }
    }
}
