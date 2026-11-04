import Foundation
import SubstrateCore
import SubstrateDevice

/// The device's notifications on Android, through the bridge: Kotlin's
/// `NotificationManager`, with each request kept under `noBackupFilesDir`
/// until it is shown and an alarm to show it at its `fireAt`. A call the
/// bridge could not make reads as the quiet answer: notifications off,
/// nothing pending.
public struct BridgeNotifications: Notifications {
    public let channel: DeviceChannel

    public init(channel: DeviceChannel) {
        self.channel = channel
    }

    public func permission() async -> NotificationPermission {
        (try? await channel.call(.notificationsPermission, as: NotificationPermission.self)) ?? .off
    }

    public func requestPermission() async -> NotificationPermission {
        (try? await channel.call(.notificationsRequestPermission, as: NotificationPermission.self)) ?? .off
    }

    public func register(_ categories: [NotificationCategory]) async {
        _ = try? await channel.call(.notificationsRegister(categories: categories))
    }

    public func schedule(_ request: NotificationRequest) async {
        _ = try? await channel.call(.notificationsSchedule(request: request))
    }

    public func cancel(_ id: String) async {
        _ = try? await channel.call(.notificationsCancel(id: id))
    }

    public func cancel(thread: String) async {
        _ = try? await channel.call(.notificationsCancelThread(thread: thread))
    }

    public func cancelAll() async {
        _ = try? await channel.call(.notificationsCancelAll)
    }

    public func setBadge(_ count: Int) async {
        _ = try? await channel.call(.notificationsBadge(count: count))
    }

    public func pending(prefix: String) async -> [String] {
        (try? await channel.call(.notificationsPending(prefix: prefix), as: [String].self)) ?? []
    }

    /// Each answer once, in order; a stream that fails or overflows ends,
    /// and the next reader asks Kotlin again, which kept what nobody read.
    public func responses() -> AsyncStream<NotificationResponse> {
        let values = channel.chunks(.notificationsResponses, capacity: 64)
        let (responses, continuation) = AsyncStream<NotificationResponse>.makeStream(bufferingPolicy: .bufferingOldest(64))
        let pump = Task {
            do {
                for try await value in values {
                    if case .dropped = continuation.yield(try BridgeCodec.decode(NotificationResponse.self, from: value)) { break }
                }
            } catch {}
            continuation.finish()
        }
        continuation.onTermination = { _ in pump.cancel() }
        return responses
    }
}
