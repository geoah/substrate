import Dispatch
import Foundation
import SubstrateDevice

/// What a measurement counts or times, by name. The runtime's own points are
/// here; an app adds its own beside them (a view's body, a parse).
public struct MeterPoint: Sendable {
    public let name: StaticString

    public init(_ name: StaticString) {
        self.name = name
    }

    /// One presenter run; its detail is the controller's type.
    public static let present = MeterPoint("present")
    /// The tree presented and sent to its readers, on the runtime's executor.
    public static let flush = MeterPoint("flush")
    /// The part of a flush that compares the tree with the one before and
    /// hands it to each reader.
    public static let publish = MeterPoint("publish")
    /// A published tree taken by `PresentationModel` on the main actor.
    public static let republish = MeterPoint("republish")
    /// A view's body evaluated; its detail is the view.
    public static let body = MeterPoint("body")
    /// One pass of the main run loop that did work, from waking to waiting
    /// again: what the main thread spent.
    public static let mainPass = MeterPoint("main")
}

/// What the loop costs, and the views that draw it: counts and durations by
/// point and detail, kept only while a measurement is on, and written as
/// signposts where it names a sink. Off, which it is unless something calls
/// `start`, a call takes one lock that nothing holds, reads no clock, builds
/// no detail and records nothing.
///
/// One for the process: a view's body has no environment to reach one
/// through without reading it. A test that asserts what one runtime did
/// reads that runtime's trace, which is its own, never this.
public final class Meter: @unchecked Sendable {
    public static let shared = Meter()

    /// How often a point was reached, and how long its intervals took.
    public struct Stat: Codable, Equatable, Sendable {
        public var count = 0
        public var nanoseconds: UInt64 = 0
        /// The longest single interval.
        public var longest: UInt64 = 0

        public init() {}

        public var milliseconds: Double { Double(nanoseconds) / 1_000_000 }
    }

    /// One point's figures under one detail.
    public struct Entry: Codable, Equatable, Sendable {
        public var point: String
        public var detail: String
        public var stat: Stat
    }

    fileprivate struct Key: Hashable {
        var point: String
        var detail: String
    }

    /// An interval begun while the meter was on.
    public struct Span {
        fileprivate var key: Key
        fileprivate var began: UInt64
        fileprivate var end: (@Sendable () -> Void)?
    }

    private let lock = NSLock()
    private var on = false
    private var signposts: (any Signposts)?
    private var stats: [Key: Stat] = [:]

    public init() {}

    public var isOn: Bool { lock.withLock { on } }

    /// The sink while the meter is on, which may be none; nil while it is off.
    private var sink: (any Signposts)?? { lock.withLock { on ? .some(signposts) : .none } }

    /// Starts counting from nothing, into `signposts` as well where a
    /// profiler should draw the intervals.
    public func start(signposts: (any Signposts)? = nil) {
        lock.withLock {
            on = true
            self.signposts = signposts
            stats = [:]
        }
    }

    /// Stops, and answers what was counted since the last `take`.
    @discardableResult
    public func stop() -> [Entry] {
        let taken = take()
        lock.withLock {
            on = false
            signposts = nil
        }
        return taken
    }

    /// What was counted since `start` or the last `take`, by point and then
    /// detail; counting goes on from nothing.
    public func take() -> [Entry] {
        let taken = lock.withLock {
            let taken = stats
            stats = [:]
            return taken
        }
        return taken.map { Entry(point: $0.key.point, detail: $0.key.detail, stat: $0.value) }
            .sorted { ($0.point, $0.detail) < ($1.point, $1.detail) }
    }

    public func count(_ point: MeterPoint, _ detail: @autoclosure () -> String = "") {
        guard let sink else { return }
        let detail = detail()
        sink?.event(point.name, detail)
        lock.withLock { stats[Key(point: "\(point.name)", detail: detail), default: Stat()].count += 1 }
    }

    /// Begins an interval; nil while the meter is off.
    public func begin(_ point: MeterPoint, _ detail: @autoclosure () -> String = "") -> Span? {
        guard let sink else { return nil }
        let detail = detail()
        return Span(key: Key(point: "\(point.name)", detail: detail), began: Self.now, end: sink?.begin(point.name, detail))
    }

    public func end(_ span: Span?) {
        guard let span else { return }
        let took = Self.now - span.began
        span.end?()
        add(span.key, nanoseconds: took)
    }

    /// Times `body` as one interval of `point`.
    public func time<T>(_ point: MeterPoint, _ detail: @autoclosure () -> String = "", _ body: () throws -> T) rethrows -> T {
        guard let span = begin(point, detail()) else { return try body() }
        defer { end(span) }
        return try body()
    }

    /// An interval measured elsewhere (a run loop's pass, a key's way to the
    /// screen).
    public func record(_ point: MeterPoint, _ detail: @autoclosure () -> String = "", nanoseconds: UInt64) {
        guard lock.withLock({ on }) else { return }
        add(Key(point: "\(point.name)", detail: detail()), nanoseconds: nanoseconds)
    }

    private func add(_ key: Key, nanoseconds: UInt64) {
        lock.withLock {
            guard on else { return }
            var stat = stats[key, default: Stat()]
            stat.count += 1
            stat.nanoseconds += nanoseconds
            stat.longest = max(stat.longest, nanoseconds)
            stats[key] = stat
        }
    }

    /// The monotonic clock, in nanoseconds.
    public static var now: UInt64 { DispatchTime.now().uptimeNanoseconds }
}
