import Foundation
import SubstrateRuntime

/// A clock a test moves: `now` stands still until `advance`, which wakes
/// every sleeper whose deadline it passed, earliest first.
public final class TestClock: RuntimeClock, @unchecked Sendable {
    private struct Sleeper {
        var id: Int
        var deadline: Date
        var continuation: CheckedContinuation<Void, any Error>
    }

    private let lock = NSLock()
    private var current: Date
    private var sleepers: [Sleeper] = []
    private var cancelled: Set<Int> = []
    private var nextID = 0

    /// 2026-11-03T09:00:00Z unless the test names another.
    public init(now: Date = Date(timeIntervalSince1970: 1_793_696_400)) {
        current = now
    }

    public var now: Date {
        lock.lock()
        defer { lock.unlock() }
        return current
    }

    /// How many tasks wait on this clock.
    public var sleeping: Int {
        lock.lock()
        defer { lock.unlock() }
        return sleepers.count
    }

    /// When each task waiting on this clock wakes, earliest first: a test
    /// that moves the clock past a sleep waits for the sleep to be here.
    public var deadlines: [Date] {
        lock.withLock { sleepers.map(\.deadline).sorted() }
    }

    public func sleep(until deadline: Date) async throws {
        let (id, due) = lock.withLock {
            nextID += 1
            return (nextID, deadline <= current)
        }
        if due { return try Task.checkCancellation() }
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
                lock.lock()
                if cancelled.remove(id) != nil {
                    lock.unlock()
                    continuation.resume(throwing: CancellationError())
                } else if deadline <= current {
                    lock.unlock()
                    continuation.resume()
                } else {
                    sleepers.append(Sleeper(id: id, deadline: deadline, continuation: continuation))
                    lock.unlock()
                }
            }
        } onCancel: {
            lock.lock()
            if let index = sleepers.firstIndex(where: { $0.id == id }) {
                let sleeper = sleepers.remove(at: index)
                lock.unlock()
                sleeper.continuation.resume(throwing: CancellationError())
            } else {
                cancelled.insert(id)
                lock.unlock()
            }
        }
    }

    public func advance(by seconds: TimeInterval) {
        advance(to: now.addingTimeInterval(seconds))
    }

    public func advance(by duration: Duration) {
        let parts = duration.components
        advance(by: TimeInterval(parts.seconds) + TimeInterval(parts.attoseconds) / 1e18)
    }

    public func advance(to instant: Date) {
        lock.lock()
        current = max(current, instant)
        let due = sleepers.filter { $0.deadline <= current }.sorted { $0.deadline < $1.deadline }
        sleepers.removeAll { $0.deadline <= current }
        lock.unlock()
        for sleeper in due { sleeper.continuation.resume() }
    }
}
