import ChatKit
import SubstrateKit

/// The device services Chat's runtime uses, made once, before the app
/// finishes launching: the notification center's delegate must be in place
/// for a tap that launched the app, and iOS takes background task
/// registrations only then.
@MainActor
enum ChatServices {
    static let notifications = UserNotificationsService()

    #if os(iOS)
    static let background: (any BackgroundWork)? = BackgroundTasksService(
        jobs: [ChatApp.refreshJob], continuedPrefix: "dev.reamde.substrate.chat.continued.")
    #else
    static let background: (any BackgroundWork)? = nil
    #endif

    static func start() {
        _ = notifications
        _ = background
    }
}
