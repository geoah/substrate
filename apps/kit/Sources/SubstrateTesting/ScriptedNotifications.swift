import Foundation
import SubstrateDevice

/// Notifications a test reads back: what was scheduled, cancelled and
/// badged, in order, a permission it sets, and taps it makes.
public final class ScriptedNotifications: Notifications, @unchecked Sendable {
    private let lock = NSLock()
    private var answer: NotificationPermission
    private var kept: [LocalNotification] = []
    private var cancelled: [String] = []
    private var badge: Int?
    private var quieted: Set<String> = []
    private var listeners: [AsyncStream<[String: String]>.Continuation] = []

    public init(permission: NotificationPermission = .allowed) {
        answer = permission
    }

    /// Every notification scheduled, in order.
    public var scheduled: [LocalNotification] { lock.withLock { kept } }
    /// Every id and group cancelled, a group as `group:<name>`, `all` for every one.
    public var cancellations: [String] { lock.withLock { cancelled } }
    public var badgeCount: Int? { lock.withLock { badge } }
    public var quietGroups: Set<String> { lock.withLock { quieted } }

    public func permission() async -> NotificationPermission { lock.withLock { answer } }

    public func requestPermission() async -> Bool {
        lock.withLock {
            if answer == .undetermined { answer = .allowed }
            return answer == .allowed
        }
    }

    public func schedule(_ notification: LocalNotification) async { lock.withLock { kept.append(notification) } }
    public func cancel(ids: [String]) async { lock.withLock { cancelled += ids } }
    public func cancel(group: String) async { lock.withLock { cancelled.append("group:\(group)") } }
    public func cancelAll() async { lock.withLock { cancelled.append("all") } }
    public func setBadge(_ count: Int) async { lock.withLock { badge = count } }
    public func quiet(groups: Set<String>) async { lock.withLock { quieted = groups } }

    public func taps() -> AsyncStream<[String: String]> {
        let (stream, continuation) = AsyncStream<[String: String]>.makeStream()
        lock.withLock { listeners.append(continuation) }
        return stream
    }

    /// The person tapped a notification with this payload.
    public func tap(_ payload: [String: String]) {
        for continuation in lock.withLock({ listeners }) { continuation.yield(payload) }
    }
}
