import Foundation

/// The order the app's writes reach the outbox in: each takes a ticket as
/// its effect starts, on the runtime's executor, and waits for the writes
/// before it, since effects run as tasks of their own and two could reach
/// the outbox's actor the other way round.
public final class WriteLine: @unchecked Sendable {
    private let lock = NSLock()
    private var issued = 0
    private var served = 0
    private var waiting: [Int: CheckedContinuation<Void, Never>] = [:]

    public init() {}

    public func ticket() -> Int {
        lock.withLock {
            issued += 1
            return issued
        }
    }

    /// Returns once every ticket before this one is done.
    public func wait(_ ticket: Int) async {
        await withCheckedContinuation { continuation in
            let go = lock.withLock { () -> Bool in
                if served == ticket - 1 { return true }
                waiting[ticket] = continuation
                return false
            }
            if go { continuation.resume() }
        }
    }

    public func done(_ ticket: Int) {
        let next = lock.withLock { () -> CheckedContinuation<Void, Never>? in
            served = max(served, ticket)
            return waiting.removeValue(forKey: served + 1)
        }
        next?.resume()
    }
}
