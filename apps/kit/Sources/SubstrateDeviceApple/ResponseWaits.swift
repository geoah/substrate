#if canImport(UserNotifications)
import Foundation
import SubstrateDevice

/// Who waits for the app to say it has done what a notification's
/// background action asked for. The system keeps an app running for such an
/// action only until its delegate answers, so the delegate answers when the
/// app says so, and never later than `limit`: within what the system gives
/// one, so an action that hangs is cut by this and not by the system.
final class ResponseWaits: @unchecked Sendable {
    /// How long one action may hold the app awake.
    let limit: Duration

    private let lock = NSLock()
    private var waiting: [String: [UUID: CheckedContinuation<Void, Never>]] = [:]
    /// Finished before anything waited: the app was faster than the
    /// delegate's own next line.
    private var early: Set<String> = []

    init(limit: Duration = .seconds(25)) {
        self.limit = limit
    }

    /// What tells one response from another: its notification, its action
    /// and the text sent with it.
    static func key(_ response: NotificationResponse) -> String {
        "\(response.id)|\(response.action)|\(response.text ?? "")"
    }

    /// Returns once `finish` was called for the key, or the limit passed.
    func wait(for key: String) async {
        let id = UUID()
        let timer = Held()
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            let finished = lock.withLock { () -> Bool in
                if early.remove(key) != nil { return true }
                waiting[key, default: [:]][id] = continuation
                return false
            }
            if finished { return continuation.resume() }
            // Started once the wait is there to end.
            timer.task = Task { [limit] in
                try? await Task.sleep(for: limit)
                if !Task.isCancelled { self.release(key, id) }
            }
        }
        timer.task?.cancel()
    }

    private final class Held: @unchecked Sendable {
        var task: Task<Void, Never>?
    }

    /// The app is done with the response: everyone waiting on it goes on.
    func finish(_ key: String) {
        let waiters = lock.withLock { () -> [CheckedContinuation<Void, Never>] in
            guard let held = waiting.removeValue(forKey: key) else {
                early.insert(key)
                return []
            }
            return Array(held.values)
        }
        for waiter in waiters { waiter.resume() }
    }

    private func release(_ key: String, _ id: UUID) {
        let waiter = lock.withLock { () -> CheckedContinuation<Void, Never>? in
            let held = waiting[key]?.removeValue(forKey: id)
            if waiting[key]?.isEmpty == true { waiting[key] = nil }
            return held
        }
        waiter?.resume()
    }

    /// How many responses are waited on now.
    var count: Int { lock.withLock { waiting.values.reduce(0) { $0 + $1.count } } }
}
#endif
