import Foundation
import SubstrateDevice

/// Notifications a test reads back: the permission it sets, the categories
/// registered, and every request scheduled and taken back, in order; and the
/// owner's responses, which a test sends with `respond(_:)`.
public final class RecordedNotifications: Notifications, @unchecked Sendable {
    public enum Call: Equatable, Sendable {
        case requestPermission
        case register([NotificationCategory])
        case schedule(NotificationRequest)
        case cancel(String)
        case cancelThread(String)
        case cancelAll
        case badge(Int)
    }

    private let lock = NSLock()
    private var current: NotificationPermission
    private var answered: NotificationPermission
    private var kept: [Call] = []
    private var held: [NotificationResponse] = []
    private var readers: [UUID: AsyncStream<NotificationResponse>.Continuation] = [:]

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
                case .cancelAll: out = [:]
                case .requestPermission, .register, .cancelThread, .badge: break
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

    /// The categories registered last.
    public var categories: [NotificationCategory] {
        lock.withLock {
            for call in kept.reversed() {
                if case let .register(categories) = call { return categories }
            }
            return []
        }
    }

    public func register(_ categories: [NotificationCategory]) async {
        lock.withLock { kept.append(.register(categories)) }
    }

    public func schedule(_ request: NotificationRequest) async {
        lock.withLock { kept.append(.schedule(request)) }
    }

    public func cancel(_ id: String) async {
        lock.withLock { kept.append(.cancel(id)) }
    }

    public func cancel(thread: String) async {
        lock.withLock { kept.append(.cancelThread(thread)) }
    }

    public func cancelAll() async {
        lock.withLock { kept.append(.cancelAll) }
    }

    public func setBadge(_ count: Int) async {
        lock.withLock { kept.append(.badge(count)) }
    }

    /// The owner answers a notification: every reader gets it, or the first
    /// one to come, as the device's delegate keeps a launch's tap.
    public func respond(_ response: NotificationResponse) {
        lock.withLock {
            if readers.isEmpty {
                held.append(response)
            } else {
                for reader in readers.values { reader.yield(response) }
            }
        }
    }

    public func responses() -> AsyncStream<NotificationResponse> {
        let (stream, continuation) = AsyncStream<NotificationResponse>.makeStream()
        let id = UUID()
        lock.withLock {
            for response in held { continuation.yield(response) }
            held = []
            readers[id] = continuation
        }
        continuation.onTermination = { [weak self] _ in
            self?.lock.withLock { self?.readers[id] = nil }
        }
        return stream
    }
}
