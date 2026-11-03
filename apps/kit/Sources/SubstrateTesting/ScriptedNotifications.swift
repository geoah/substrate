import Foundation
import SubstrateDevice

/// Notifications a test reads back: what is scheduled now, every cancel in
/// order, and the permission it set; `tap` delivers a tap to every reader.
public final class ScriptedNotifications: Notifications, @unchecked Sendable {
    private let lock = NSLock()
    private var granted: NotificationPermission
    /// What the person answers when asked.
    private let answer: NotificationPermission
    private var kept: [String: NotificationRequest] = [:]
    private var cancels: [[String]] = []
    private var asks = 0
    private var readers: [Int: AsyncStream<NotificationTap>.Continuation] = [:]
    private var next = 0

    public init(permission: NotificationPermission = .allowed, answer: NotificationPermission = .allowed) {
        granted = permission
        self.answer = answer
    }

    /// What is scheduled, by id.
    public var scheduled: [String: NotificationRequest] { lock.withLock { kept } }
    /// Each cancel call's ids, in order.
    public var cancelled: [[String]] { lock.withLock { cancels } }
    /// How many times the person was asked.
    public var asked: Int { lock.withLock { asks } }

    public func permission() async -> NotificationPermission { lock.withLock { granted } }

    public func requestPermission() async -> NotificationPermission {
        lock.withLock {
            if granted == .notAsked {
                asks += 1
                granted = answer
            }
            return granted
        }
    }

    public func pending(prefix: String) async -> [String] {
        lock.withLock { kept.keys.filter { $0.hasPrefix(prefix) }.sorted() }
    }

    public func schedule(_ requests: [NotificationRequest]) async throws {
        lock.withLock {
            for request in requests { kept[request.id] = request }
        }
    }

    public func cancel(_ ids: [String]) async {
        lock.withLock {
            cancels.append(ids)
            for id in ids { kept[id] = nil }
        }
    }

    public func taps() -> AsyncStream<NotificationTap> {
        let (stream, continuation) = AsyncStream<NotificationTap>.makeStream()
        let id = lock.withLock { () -> Int in
            next += 1
            readers[next] = continuation
            return next
        }
        continuation.onTermination = { [weak self] _ in self?.lock.withLock { _ = self?.readers.removeValue(forKey: id) } }
        return stream
    }

    /// The person taps the notification `id`.
    public func tap(_ id: String, payload: [String: String]) {
        let readers = lock.withLock { Array(self.readers.values) }
        for reader in readers { reader.yield(NotificationTap(id: id, payload: payload)) }
    }
}
