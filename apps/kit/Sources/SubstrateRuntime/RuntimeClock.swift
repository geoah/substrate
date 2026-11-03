import Foundation

/// The instant, and waiting for one: the environment's, so a test fixes the
/// time and moves it (SubstrateTesting's `TestClock`).
public protocol RuntimeClock: Sendable {
    var now: Date { get }
    /// Returns at `deadline`, or throws `CancellationError` when the task is
    /// cancelled first.
    func sleep(until deadline: Date) async throws
}

/// The device's wall clock.
public struct WallClock: RuntimeClock {
    public init() {}

    public var now: Date { Date() }

    public func sleep(until deadline: Date) async throws {
        let seconds = deadline.timeIntervalSinceNow
        if seconds > 0 { try await Task.sleep(nanoseconds: UInt64(seconds * 1_000_000_000)) }
        try Task.checkCancellation()
    }
}

extension Duration {
    /// The duration in seconds, as a date's arithmetic takes it.
    var seconds: TimeInterval {
        let parts = components
        return TimeInterval(parts.seconds) + TimeInterval(parts.attoseconds) / 1e18
    }
}
