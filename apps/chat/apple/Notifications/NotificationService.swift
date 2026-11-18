import ChatKit
import Foundation
import SubstrateAuth
import SubstrateDevice
import UserNotifications

/// What a push from the publisher's relay shows: the relay sends no words,
/// so this asks the person's own server what the push is about, with the
/// app's own token from the Keychain group the app and this extension
/// share, and shows it as the app's local notification of the same item
/// would (`ChatPush.content`). One monotonic deadline covers the whole
/// operation, from before the Keychain is read: the push is delivered then,
/// once, with what was made or as it came (`DeliverOnce`), and what is still
/// out is cancelled and never waited for. Should the system's time end
/// first, it delivers what the last check before showing confirmed, or
/// else the plain fallback with no badge, before it stops the work
/// (`PushRun`). The generation's check and the
/// client its requests go with come from one read of the session
/// (`PushRegistrar.acceptance(of:)`). On any failure the alert keeps the
/// app's own fallback words. It links the
/// chat's core and the kit's API, never a view: iOS holds an extension to a
/// small memory limit.
final class NotificationService: UNNotificationServiceExtension, @unchecked Sendable {
    private let lock = NSLock()
    private var run: PushRun?

    override func didReceive(_ request: UNNotificationRequest, withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void) {
        guard let best = request.content.mutableCopy() as? UNMutableNotificationContent else { return contentHandler(request.content) }
        let deadline = ContinuousClock.now + ChatPush.budget
        let run = PushRun(best, handler: contentHandler)
        lock.withLock { self.run = run }
        let registrar = PushRegistrar(sessions: .chat, device: nil, buildRelay: nil, clientID: "")
        run.start(PushPayload.flatten(request.content.userInfo), accepting: { await registrar.acceptance(of: $0) }, until: deadline)
    }

    override func serviceExtensionTimeWillExpire() {
        lock.withLock { run }?.expire()
    }
}
