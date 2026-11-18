import Foundation

/// Work bounded by a deadline that is kept: the answer comes at the deadline
/// whatever the work does.
public enum Deadline {
    /// What a raced piece of work is told: whether it may still act.
    public struct Race: Sendable {
        fileprivate let claim: Claim

        public var deadline: ContinuousClock.Instant { claim.deadline }

        /// Whether the work may still act: the race is unclaimed, the
        /// deadline has not passed and the caller is not cancelled. The work
        /// checks it immediately before each thing it does: a request, a
        /// call to the device.
        public var live: Bool { claim.open }
    }

    /// `work`'s answer, or `fallback` once `deadline` passes or the caller
    /// is cancelled, whichever is first.
    ///
    /// One claim decides it: whoever answers claims first, and the work
    /// loses its claim once the deadline has passed or the caller was
    /// cancelled, however late the timer runs. Nothing starts where the
    /// race is decided already, the fallback is answered before the work is
    /// cancelled, from another task, so a cancellation handler that takes
    /// its time holds nobody, and work that ignores its cancellation runs on
    /// unwaited, its answer dropped.
    public static func race<T: Sendable>(
        until deadline: ContinuousClock.Instant, or fallback: T, _ work: @escaping @Sendable (Race) async -> T
    ) async -> T {
        let claim = Claim(deadline: deadline)
        guard !Task.isCancelled, claim.open else { return fallback }
        let answer = AnswerOnce<T>()
        let task = Task {
            // Started after the race was decided: the work does not run.
            guard claim.open else {
                if claim.fall() { answer.resume(fallback) }
                return
            }
            let value = await work(Race(claim: claim))
            if claim.take() {
                answer.resume(value)
            } else if claim.fall() {
                answer.resume(fallback)
            }
        }
        let timer = Task {
            try? await Task.sleep(until: deadline, clock: .continuous)
            guard claim.fall() else { return }
            answer.resume(fallback)
            task.cancel()
        }
        let value = await withTaskCancellationHandler {
            await withCheckedContinuation { answer.set($0) }
        } onCancel: {
            // The timer answers and cancels the work, on its own task.
            claim.cancel()
            timer.cancel()
        }
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

/// Who answers a race, decided once under one lock.
final class Claim: @unchecked Sendable {
    let deadline: ContinuousClock.Instant
    private let lock = NSLock()
    private var taken = false
    private var cancelled = false

    init(deadline: ContinuousClock.Instant) {
        self.deadline = deadline
    }

    var open: Bool { lock.withLock { !taken && !cancelled && ContinuousClock.now < deadline } }

    /// The work's claim, lost once the deadline has passed or the caller
    /// was cancelled, whatever the timer has done yet.
    func take() -> Bool {
        lock.withLock {
            guard !taken, !cancelled, ContinuousClock.now < deadline else { return false }
            taken = true
            return true
        }
    }

    /// The fallback's claim: anyone's, where the work has not claimed.
    func fall() -> Bool {
        lock.withLock {
            guard !taken else { return false }
            taken = true
            return true
        }
    }

    /// The caller was cancelled: the work can no longer claim.
    func cancel() { lock.withLock { cancelled = true } }
}

/// A continuation resumed once; an answer that comes before the
/// continuation is kept for it.
final class AnswerOnce<T: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<T, Never>?
    private var early: T?

    func set(_ continuation: CheckedContinuation<T, Never>) {
        let ready = lock.withLock { () -> T? in
            if let early { return early }
            self.continuation = continuation
            return nil
        }
        if let ready { continuation.resume(returning: ready) }
    }

    func resume(_ value: T) {
        let waiting = lock.withLock { () -> CheckedContinuation<T, Never>? in
            guard early == nil else { return nil }
            early = value
            defer { continuation = nil }
            return continuation
        }
        waiting?.resume(returning: value)
    }
}
