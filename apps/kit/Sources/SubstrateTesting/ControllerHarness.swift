import Foundation
import SubstrateAPI
import SubstrateDevice
import SubstrateRuntime

/// A controller on a runtime with a fake environment: a `TestClock`, ids a
/// test can name, a scripted server, English in UTC. It keeps the runtime's
/// trace, so a test reads what was journaled, presented, published and
/// asked for, in order, and waits on what it expects rather than on time.
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

    /// Waits until `condition` holds, failing after `timeout` of real time.
    public func until(timeout: Duration = .seconds(5), _ condition: @Sendable (ControllerHarness<C>) async -> Bool) async throws {
        let deadline = ContinuousClock.now + timeout
        while await !condition(self) {
            if ContinuousClock.now > deadline { throw HarnessTimedOut() }
            try await Task.sleep(nanoseconds: 1_000_000)
        }
    }
}

/// A harness waited longer than its timeout for what the test expected.
public struct HarnessTimedOut: Error, Sendable {}

private final class TraceRecorder: @unchecked Sendable {
    private let lock = NSLock()
    private var kept: [RuntimeTrace] = []

    func append(_ entry: RuntimeTrace) {
        lock.lock()
        kept.append(entry)
        lock.unlock()
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
