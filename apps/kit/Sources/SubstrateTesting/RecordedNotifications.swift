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
        /// The app said it had done what this response's action asked.
        case finished(NotificationResponse)
        /// The threads the app said it shows in front, sorted.
        case showing([String])
    }

    private let lock = NSLock()
    private var current: NotificationPermission
    private var answered: NotificationPermission
    private var kept: [Call] = []
    private var held: [NotificationResponse] = []
    private var shown: [DeliveredNotification] = []
    private var readers: [UUID: AsyncStream<NotificationResponse>.Continuation] = [:]
    private var whenFinished: (@Sendable (NotificationResponse) -> Void)?
    private var beforeDelivered: (@Sendable () async -> Void)?

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
                case .requestPermission, .register, .cancelThread, .badge, .finished, .showing: break
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
        lock.withLock {
            kept.append(.cancel(id))
            shown.removeAll { $0.id == id }
        }
    }

    public func cancel(thread: String) async {
        lock.withLock {
            kept.append(.cancelThread(thread))
            shown.removeAll { $0.thread == thread }
        }
    }

    public func cancelAll() async {
        lock.withLock {
            kept.append(.cancelAll)
            shown = []
        }
    }

    /// A notification the system shows, as a push the app never scheduled
    /// is: what `delivered()` answers until it is taken back.
    public func deliver(_ notification: DeliveredNotification) {
        lock.withLock { shown.append(notification) }
    }

    /// The notifications shown and not taken back.
    public var standing: [DeliveredNotification] { lock.withLock { shown } }

    public func delivered() async -> [DeliveredNotification] {
        let reading = lock.withLock { beforeDelivered }
        await reading?()
        return standing
    }

    /// Run before `delivered()` answers, for a test of a system that is slow
    /// to: the answer is what is shown once it has run.
    public func onDelivered(_ reading: @escaping @Sendable () async -> Void) {
        lock.withLock { beforeDelivered = reading }
    }

    public func showing(threads: Set<String>) async {
        lock.withLock { kept.append(.showing(threads.sorted())) }
    }

    public func setBadge(_ count: Int) async {
        lock.withLock { kept.append(.badge(count)) }
    }

    public func finished(_ response: NotificationResponse) async {
        let told = lock.withLock { () -> (@Sendable (NotificationResponse) -> Void)? in
            kept.append(.finished(response))
            return whenFinished
        }
        told?(response)
    }

    /// Called as the app says it is done with a response, for a test of
    /// what the app holds at that instant: the system may suspend it there.
    public func onFinished(_ told: @escaping @Sendable (NotificationResponse) -> Void) {
        lock.withLock { whenFinished = told }
    }

    public func pending(prefix: String) async -> [String] {
        pending.keys.filter { $0.hasPrefix(prefix) }.sorted()
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
