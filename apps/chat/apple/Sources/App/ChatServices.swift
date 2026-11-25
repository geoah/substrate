import Foundation
import SubstrateChatCore
import SubstrateKit

/// The device services Chat's runtime uses, made once, before the app
/// finishes launching: the kit's notification delegate must be in place for
/// a tap that launched the app, and iOS takes background task registrations
/// only then.
@MainActor
enum ChatServices {
    /// Chat sets the badge to its unread chats, so it asks for that too.
    static let notifications: any Notifications = SystemNotifications(badges: true)

    #if os(iOS)
    static let background: (any BackgroundWork)? = BackgroundTasksService(jobs: [ChatApp.refreshJob])
    #else
    static let background: (any BackgroundWork)? = nil
    #endif

    /// Pushes from the publisher's relay, on iPhone and iPad; the Mac
    /// follows once a development push has reached a development-signed Mac.
    #if os(iOS)
    static let remote: (any PushRegistration)? = RemoteNotifications(topic: Bundle.main.bundleIdentifier ?? "dev.reamde.substrate.chat")
    #else
    static let remote: (any PushRegistration)? = nil
    #endif

    /// The registration with the relay this build names, for the session in
    /// the Keychain.
    static let push: PushRegistrar? = remote.map {
        PushRegistrar(
            sessions: .chat, device: $0, buildRelay: Bundle.main.object(forInfoDictionaryKey: "SubstratePushRelay") as? String,
            clientID: ChatManifest.manifest.clientID)
    }

    /// A `read` push: what it covers taken back, for this install's
    /// registration alone, with the client of the session that accepted it.
    static func takeBack(_ userInfo: [String: String]) async -> Bool {
        // iOS gives the app about 30 seconds: one deadline for all of it,
        // the Keychain's reads included, the extension's own.
        let deadline = ContinuousClock.now + ChatPush.budget
        guard let registrar = push else { return false }
        return await ChatPush.takeBack(
            userInfo, accepting: { await registrar.acceptance(of: $0) }, notifications: notifications, until: deadline)
    }

    static func start() {
        SystemNotifications.installDelegate()
        _ = background
        _ = remote
    }
}
