import ChatKit
import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import UserNotifications

/// What a push from the publisher's relay shows: the relay sends no words,
/// so this asks the person's own server what the push is about, with the
/// app's own token from the Keychain group the app and this extension
/// share, and shows it as the app's local notification of the same item
/// would (`ChatPush.content`). One monotonic deadline covers the whole
/// operation; what is still out then is cancelled, and the push is
/// delivered once, with what was made or as it came (`DeliverOnce`). On any
/// failure the alert keeps the app's own fallback words. It links the
/// chat's core and the kit's API, never a view: iOS holds an extension to a
/// small memory limit.
final class NotificationService: UNNotificationServiceExtension, @unchecked Sendable {
    private let lock = NSLock()
    private var once: DeliverOnce?
    private var work: Task<Void, Never>?

    override func didReceive(_ request: UNNotificationRequest, withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void) {
        guard let best = request.content.mutableCopy() as? UNMutableNotificationContent else { return contentHandler(request.content) }
        let once = DeliverOnce(best, handler: contentHandler)
        let userInfo = PushPayload.flatten(request.content.userInfo)
        let deadline = ContinuousClock.now + ChatPush.budget
        let task = Task {
            let registrar = PushRegistrar(sessions: .chat, device: nil, buildRelay: nil, clientID: "")
            let verdict = if let push = PushPayload(userInfo) { await registrar.verdict(push.generation) } else { PushVerdict.refused }
            let client = verdict == .accepted ? await SessionStore.chat.load().map { SubstrateClient($0) } : nil
            once.deliver(await ChatPush.content(userInfo, verdict: verdict, client: client, until: deadline))
        }
        lock.withLock {
            self.once = once
            work = task
        }
    }

    override func serviceExtensionTimeWillExpire() {
        let (once, work) = lock.withLock { (self.once, self.work) }
        work?.cancel()
        once?.deliver(nil)
    }
}
