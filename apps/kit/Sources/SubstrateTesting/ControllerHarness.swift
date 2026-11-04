import Foundation
import SubstrateAPI
import SubstrateDevice
import SubstrateRuntime

/// A controller on a runtime with a fake environment: a `TestClock`, ids a
/// test can name, a scripted server, English in UTC. It keeps the runtime's
/// trace, so a test reads what was journaled, presented, published and
/// asked for, in order, and waits on what it expects rather than on time:
/// `until` fails once the runtime has made no move at all for its bound, so
/// a host too loaded to run the test promptly slows it and never fails it,
/// or once a runtime that keeps moving has had ten times that bound.
public final class ControllerHarness<C: Controller>: @unchecked Sendable {
    public let clock: TestClock
    public let ids: SequentialIDs
    public let server: StubTransport
    public let environment: AppEnvironment
    public let runtime: Runtime<C>
    private let recorder: TraceRecorder

    /// - Parameter performsEffects: false holds every effect as a value in
    ///   the trace for the test to answer with `deliver(_:)`.
    public init(_ controller: C, clock: TestClock = TestClock(), environment: AppEnvironment? = nil,
                performsEffects: Bool = true)
    {
        let ids = SequentialIDs()
        let server = StubTransport()
        let environment = environment ?? AppEnvironment.testing(clock: clock, ids: ids, transport: server)
        let recorder = TraceRecorder()
        self.clock = clock
        self.ids = ids
        self.server = server
        self.environment = environment
        self.recorder = recorder
        runtime = Runtime(controller, environment: environment,
                          options: RuntimeOptions(performsEffects: performsEffects, trace: { recorder.append($0) }))
    }

    public var trace: [RuntimeTrace] { recorder.entries }

    /// The effects asked for, in order, as `C.Effect` values.
    public var effects: [C.Effect] {
        trace.compactMap { if case let .effect(_, effect) = $0 { effect as? C.Effect } else { nil } }
    }

    /// How many presentation trees went out to the UI.
    public var published: Int {
        trace.filter { if case .published = $0 { true } else { false } }.count
    }

    public var presentation: C.Presentation {
        get async { await runtime.presentation }
    }

    public var state: C.State {
        get async { await runtime.state }
    }

    public func send(_ command: C.Command) async {
        await runtime.send(command)
    }

    public func deliver(_ event: C.Event) async {
        await runtime.deliver(event)
    }

    /// Forgets the trace so far.
    public func clearTrace() {
        recorder.clear()
    }

    /// Moves the clock and waits until every task the move woke has reached
    /// the runtime again.
    public func advance(by seconds: TimeInterval) async {
        clock.advance(by: seconds)
        await settle()
    }

    /// Lets the tasks the runtime started run until they wait on the clock or
    /// on the test, as far as a bounded number of turns can tell.
    public func settle() async {
        for _ in 0 ..< 50 {
            await Task.yield()
            _ = await runtime.counters
        }
    }

    /// How many effects have started so far: the mark `untilIdle(since:)`
    /// waits from.
    public var effectsStarted: Int {
        get async { await runtime.activity.started }
    }

    /// Waits until every effect started after `mark` (and up to `through`,
    /// where given) has ended, and none waits for a presentation: what a
    /// test then asserts did not happen cannot still be on its way, while
    /// the long-lived work started before the mark (a watch, an answer the
    /// server holds) runs on. Work that never ends fails it after `idle`,
    /// as `until` does. `advancing` is `until(advancing:)`'s.
    public func untilIdle(since mark: Int = 0, through last: Int = .max, advancing window: TimeInterval = 0,
                          idle: Duration = ControllerHarness.idleBound) async throws
    {
        try await until(advancing: window, idle: idle) { harness in
            let activity = await harness.runtime.activity
            return activity.waiting == 0 && !activity.running.contains { $0 > mark && $0 <= last }
        }
    }

    /// `until`, for work that waits on the clock (a coalesced save, a
    /// presentation's window, a check a second later): while `condition`
    /// does not hold, the clock moves to the earliest sleep already
    /// waiting where that sleep ends within `window` seconds of the clock's
    /// now. A sleep that begins late is not stepped over, since the clock
    /// only ever moves to a sleep's end, and a sleep longer than the window
    /// is never the one stepped to.
    public func until(advancing window: TimeInterval, idle: Duration = ControllerHarness.idleBound,
                      _ condition: @Sendable (ControllerHarness<C>) async -> Bool) async throws
    {
        try await until(idle: idle) { harness in
            if await condition(harness) { return true }
            let clock = harness.clock
            if let due = clock.deadlines.first, due <= clock.now.addingTimeInterval(window) { clock.advance(to: due) }
            return false
        }
    }

    /// How long `until` waits with nothing happening in the runtime before
    /// it gives up: a hang guard, never a pace. `SUBSTRATE_HARNESS_IDLE`
    /// (seconds) moves it for a whole run.
    public static var idleBound: Duration {
        let seconds = ProcessInfo.processInfo.environment["SUBSTRATE_HARNESS_IDLE"].flatMap(Double.init) ?? 60
        return .milliseconds(Int(seconds * 1000))
    }

    /// Waits until `condition` holds, checking it each time the runtime
    /// moves (a step traced, a presentation published, an effect asked for)
    /// and between. It fails after `idle` with no move in the runtime at
    /// all, however long the whole wait takes, or once the whole wait passes
    /// ten times `idle`: a runtime that keeps moving without ever reaching
    /// the condition (a watch reopened on a real pause, again and again) is
    /// a livelock, not a slow host.
    public func until(idle: Duration = ControllerHarness.idleBound, _ condition: @Sendable (ControllerHarness<C>) async -> Bool) async throws {
        try await wait(idle: idle, cap: idle * 10, condition)
    }

    /// The same, bounded by `timeout` in all: it fails once `timeout` has
    /// passed, moving or not, and after `idleBound` with no move where that
    /// is shorter.
    public func until(timeout: Duration, _ condition: @Sendable (ControllerHarness<C>) async -> Bool) async throws {
        try await wait(idle: min(timeout, Self.idleBound), cap: timeout, condition)
    }

    private func wait(idle: Duration, cap: Duration, _ condition: @Sendable (ControllerHarness<C>) async -> Bool) async throws {
        var seen = recorder.count
        let started = ContinuousClock.now
        var quietSince = started
        while await !condition(self) {
            let now = ContinuousClock.now
            let count = recorder.count
            if now - started > cap { throw HarnessTimedOut() }
            if count != seen {
                seen = count
                quietSince = now
            } else if now - quietSince > idle {
                throw HarnessTimedOut()
            }
            await recorder.changed(since: count, orAfter: .milliseconds(20))
        }
    }
}

/// A harness waited longer than its timeout for what the test expected.
public struct HarnessTimedOut: Error, Sendable {}

private final class TraceRecorder: @unchecked Sendable {
    private let lock = NSLock()
    private var kept: [RuntimeTrace] = []
    /// Every entry ever appended, a clear included.
    private var appended = 0
    private var waiting: [UUID: CheckedContinuation<Void, Never>] = [:]
    /// Waits whose limit passed before they began waiting.
    private var expired: Set<UUID> = []

    func append(_ entry: RuntimeTrace) {
        lock.lock()
        kept.append(entry)
        appended += 1
        let woken = waiting
        waiting = [:]
        lock.unlock()
        for continuation in woken.values { continuation.resume() }
    }

    var count: Int { lock.withLock { appended } }

    /// Returns once an entry is appended after the `count`th, or after
    /// `limit`, whichever comes first.
    func changed(since count: Int, orAfter limit: Duration) async {
        let id = UUID()
        let timer = Task {
            try? await Task.sleep(for: limit)
            if !Task.isCancelled { self.wake(id) }
        }
        await withCheckedContinuation { continuation in
            let now = lock.withLock { () -> Bool in
                if appended != count || expired.remove(id) != nil { return true }
                waiting[id] = continuation
                return false
            }
            if now { continuation.resume() }
        }
        timer.cancel()
        lock.withLock { _ = expired.remove(id) }
    }

    private func wake(_ id: UUID) {
        let continuation = lock.withLock { () -> CheckedContinuation<Void, Never>? in
            guard let continuation = waiting.removeValue(forKey: id) else {
                expired.insert(id)
                return nil
            }
            return continuation
        }
        continuation?.resume()
    }

    func clear() {
        lock.lock()
        kept = []
        lock.unlock()
    }

    var entries: [RuntimeTrace] {
        lock.lock()
        defer { lock.unlock() }
        return kept
    }
}

public extension AppEnvironment {
    /// The environment a test runs a controller in: the clock and ids given,
    /// English (US) in UTC, the scripted server as the transport, no client.
    static func testing(clock: any RuntimeClock = TestClock(), ids: any IDSource = SequentialIDs(),
                        transport: any HTTPTransport = StubTransport(), client: ClientSource = .none) -> AppEnvironment
    {
        AppEnvironment(clock: clock, ids: ids, locale: Locale(identifier: "en_US"), timeZone: TimeZone(identifier: "UTC")!,
                       client: client, devices: DeviceServices(transport: transport))
    }
}
