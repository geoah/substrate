import Foundation
import SubstrateDevice

/// Notifications a test reads back: the permission it sets, and every
/// request scheduled and taken back, in order.
public final class RecordedNotifications: Notifications, @unchecked Sendable {
    public enum Call: Equatable, Sendable {
        case requestPermission
        case schedule(NotificationRequest)
        case cancel(String)
    }

    private let lock = NSLock()
    private var current: NotificationPermission
    private var answered: NotificationPermission
    private var kept: [Call] = []

    /// - Parameter granting: what the owner answers when asked.
    public init(_ permission: NotificationPermission = .on, granting: NotificationPermission = .on) {
        current = permission
        answered = granting
    }

    public var calls: [Call] { lock.withLock { kept } }

    /// The requests pending now, by id.
    public var pending: [String: NotificationRequest] {
        lock.withLock {
            var out: [String: NotificationRequest] = [:]
            for call in kept {
                switch call {
                case let .schedule(request): out[request.id] = request
                case let .cancel(id): out[id] = nil
                case .requestPermission: break
                }
            }
            return out
        }
    }

    public func set(_ permission: NotificationPermission) {
        lock.withLock { current = permission }
    }

    public func permission() async -> NotificationPermission { lock.withLock { current } }

    public func requestPermission() async -> NotificationPermission {
        lock.withLock {
            kept.append(.requestPermission)
            if current == .notAsked { current = answered }
            return current
        }
    }

    public func schedule(_ request: NotificationRequest) async {
        lock.withLock { kept.append(.schedule(request)) }
    }

    public func cancel(_ id: String) async {
        lock.withLock { kept.append(.cancel(id)) }
    }
}
