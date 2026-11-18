import Foundation

/// Work bounded by a deadline that is kept: the answer comes at the deadline
/// whatever the work does.
public enum Deadline {
    /// What a raced piece of work is told: whether what it does still
    /// counts. Once the race is answered, by the work, the deadline or the
    /// caller's cancellation, it does not, and the work acts on nothing.
    public struct Race: Sendable {
        fileprivate let answered: @Sendable () -> Bool
        public let deadline: ContinuousClock.Instant

        /// Whether the work may still act: the race is unanswered, the
        /// deadline has not passed, and the work is not cancelled.
        public var live: Bool { !answered() && ContinuousClock.now < deadline && !Task.isCancelled }
    }

    /// `work`'s answer, or `fallback` once `deadline` passes or the caller
    /// is cancelled, whichever is first.
    ///
    /// Nothing starts where the deadline has passed or the caller is
    /// cancelled already. Otherwise the fallback is answered first and the
    /// work cancelled after, from another task: a cancellation handler runs
    /// where `cancel()` is called and may take its time, and the caller does
    /// not wait for it, nor for work that ignores its cancellation. What the
    /// work answers late is dropped, and what it would do late it checks
    /// against `Race.live`.
    public static func race<T: Sendable>(
        until deadline: ContinuousClock.Instant, or fallback: T, _ work: @escaping @Sendable (Race) async -> T
    ) async -> T {
        guard !Task.isCancelled, ContinuousClock.now < deadline else { return fallback }
        let answer = AnswerOnce<T>()
        let race = Race(answered: { answer.answered }, deadline: deadline)
        let task = Task { answer.resume(await work(race)) }
        let timer = Task {
            try? await Task.sleep(until: deadline, clock: .continuous)
            if answer.resume(fallback) { task.cancel() }
        }
        let value = await withTaskCancellationHandler {
            await withCheckedContinuation { answer.set($0) }
        } onCancel: {
            // The timer answers and cancels the work, on its own task.
            timer.cancel()
        }
        // Answered by the work: the timer has nothing left to do.
        timer.cancel()
        return value
    }

    /// `race` for work that has nothing to check as it goes.
    public static func race<T: Sendable>(
        until deadline: ContinuousClock.Instant, or fallback: T, _ work: @escaping @Sendable () async -> T
    ) async -> T {
        await race(until: deadline, or: fallback) { (_: Race) in await work() }
    }
}

/// A continuation resumed once, by whichever racer comes first; an answer
/// that comes before the continuation is kept for it.
final class AnswerOnce<T: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<T, Never>?
    private var early: T?

    var answered: Bool { lock.withLock { early != nil } }

    func set(_ continuation: CheckedContinuation<T, Never>) {
        let ready = lock.withLock { () -> T? in
            if let early { return early }
            self.continuation = continuation
            return nil
        }
        if let ready { continuation.resume(returning: ready) }
    }

    /// Answers with `value`; false where it was answered already.
    @discardableResult
    func resume(_ value: T) -> Bool {
        let (first, waiting) = lock.withLock { () -> (Bool, CheckedContinuation<T, Never>?) in
            guard early == nil else { return (false, nil) }
            early = value
            defer { continuation = nil }
            return (true, continuation)
        }
        waiting?.resume(returning: value)
        return first
    }
}
