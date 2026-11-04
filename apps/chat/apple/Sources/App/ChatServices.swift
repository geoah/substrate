import ChatKit
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
    static let background: (any BackgroundWork)? = BackgroundTasksService(
        jobs: [ChatApp.refreshJob], continuedPrefix: "dev.reamde.substrate.chat.continued.")
    #else
    static let background: (any BackgroundWork)? = nil
    #endif

    static func start() {
        SystemNotifications.installDelegate()
        _ = background
    }
}
