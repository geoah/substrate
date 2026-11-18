import Foundation
import SubstrateAPI
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import SubstrateRuntime

/// Asks a server once per command: the client is taken as the effect
/// starts, and the request sent once the test opens the effect's gate,
/// whatever session is held by then. It never resets on a link: what holds
/// a request back is the runtime's session epoch alone, and what ends the
/// wait on one the session changed under is `ended(bySessionChange:)`.
struct Asker: Controller {
    enum Command: Codable, Equatable, Sendable { case ask(String) }
    enum Event: Codable, Equatable, Sendable {
        case ask(String)
        case answered(String, sent: Bool)
        case ended(String)
    }

    struct State: Codable, Equatable, Sendable {
        var answered: [String: Bool] = [:]
        var ended: Set<String> = []
    }

    typealias Presentation = State
    enum Effect: Equatable, Sendable { case ask(String) }

    let gates: Gates
    /// Every ask under one key: a second replaces the first.
    var keyed = false

    var initialState: State { State() }

    func key(of effect: Effect) -> String? { keyed ? "ask" : nil }

    func events(for command: Command, in context: Enrichment<Asker>) -> [Event] {
        guard case let .ask(name) = command else { return [] }
        return [.ask(name)]
    }

    func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        switch event {
        case let .answered(name, sent): next.answered[name] = sent
        case let .ended(name): next.ended.insert(name)
        case .ask: break
        }
        return next
    }

    func present(_ input: PresenterInput<Asker>) -> State { input.state }

    func effects(of event: Event, on state: State, presented presentation: State) -> [Effect] {
        guard case let .ask(name) = event else { return [] }
        return [.ask(name)]
    }

    func ended(bySessionChange effect: Effect) -> Event? {
        guard case let .ask(name) = effect else { return nil }
        return .ended(name)
    }

    func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        guard case let .ask(name) = effect else { return .none }
        let source = environment.client
        let gates = gates
        return .once {
            guard let client = await source.current() else { return .answered(name, sent: false) }
            await gates.wait(name)
            let sent = (try? await client.send("GET", "api/v1/\(name)", retries: 0)) != nil
            return .answered(name, sent: sent)
        }
    }
}

/// The token a secure store holds now, as a test switches it.
final class HeldToken: @unchecked Sendable {
    private let lock = NSLock()
    private var token: String

    init(_ token: String) { self.token = token }

    var value: String { lock.withLock { token } }
    func set(_ token: String) { lock.withLock { self.token = token } }
}

@Suite("The session an effect starts under", .timeLimit(.minutes(10)))
struct SessionEpochTests {
    static let server = URL(string: "https://s.example.com")!

    static func link(_ tokenID: String?, repository: String = "r.example.com") -> LinkSnapshot {
        LinkSnapshot(phase: tokenID == nil ? .signedOut : .signedIn, server: server, repository: repository, tokenID: tokenID, online: true)
    }

    /// An effect that took its client under one session sends nothing once
    /// another is held: the request is held back as it would leave, what the
    /// effect answers is dropped, and its controller's word for that end is
    /// applied in its place. One started under the session held now is sent,
    /// and ended by its own answer alone.
    @Test func anEffectStartedUnderOneSessionSendsNothingUnderTheNext() async throws {
        let gates = Gates()
        let stub = StubTransport().on("GET", "/api/v1/first", json: "{}").on("GET", "/api/v1/second", json: "{}")
        let environment = AppEnvironment.testing(transport: stub, client: .fixed(SubstrateClient(server: Self.server, token: "a", transport: stub)))
        let harness = ControllerHarness(Asker(gates: gates), environment: environment)
        await harness.runtime.linkChanged(Self.link("tokenA"))
        await harness.send(.ask("first"))
        try await harness.until { _ in await gates.isWaiting("first") }
        // Another token, the same repository: another session.
        await harness.runtime.linkChanged(Self.link("tokenB"))
        await gates.release("first")
        try await harness.until { await $0.state.ended == ["first"] }
        #expect(stub.requests.isEmpty)
        #expect(await harness.state.answered.isEmpty)
        #expect(await harness.runtime.counters.droppedBySession == 1)
        #expect(await harness.runtime.counters.endedBySession == 1)

        await harness.send(.ask("second"))
        await gates.release("second")
        try await harness.until { await $0.state.answered["second"] == true }
        #expect(stub.sent("GET", "/api/v1/second").count == 1)
        #expect(await harness.state.ended == ["first"])
    }

    /// A source that names its token vends only the client of the session
    /// an effect started under: a switch the link has not told yet gives the
    /// effect no client at all.
    @Test func aSwitchTheLinkHasNotToldYetGivesNoClient() async throws {
        let gates = Gates()
        await gates.release("first")
        let stub = StubTransport().on("GET", "/api/v1/first", json: "{}")
        let token = HeldToken("tokenA")
        let source = ClientSource.sessions {
            ClientSource.Vended(client: SubstrateClient(server: Self.server, token: token.value, transport: stub), tokenID: token.value)
        }
        let harness = ControllerHarness(Asker(gates: gates), environment: AppEnvironment.testing(transport: stub, client: source))
        await harness.runtime.linkChanged(Self.link("tokenA"))
        token.set("tokenB")
        await harness.send(.ask("first"))
        try await harness.until { await $0.state.answered["first"] != nil }
        #expect(await harness.state.answered["first"] == false)
        #expect(stub.requests.isEmpty)
        // The epoch held: the effect's own answer ended it.
        #expect(await harness.state.ended.isEmpty)
        #expect(await harness.runtime.counters.endedBySession == 0)
    }

    /// An effect replaced under its key, or closed with its controller, ends
    /// unheard though the session changed under it: nothing waits on it.
    @Test func anEffectReplacedUnderItsKeyIsNotEndedByTheSessionChange() async throws {
        let gates = Gates()
        let stub = StubTransport().on("GET", "/api/v1/first", json: "{}")
        let environment = AppEnvironment.testing(transport: stub, client: .fixed(SubstrateClient(server: Self.server, token: "a", transport: stub)))
        let harness = ControllerHarness(Asker(gates: gates, keyed: true), environment: environment)
        await harness.runtime.linkChanged(Self.link("tokenA"))
        await harness.send(.ask("first"))
        try await harness.until { _ in await gates.isWaiting("first") }
        await harness.runtime.linkChanged(Self.link("tokenB"))
        // Another under the same key replaces it before it ends.
        await harness.send(.ask("first"))
        await gates.release("first")
        try await harness.until { await $0.state.answered["first"] != nil }
        try await harness.untilIdle()
        #expect(await harness.state.ended.isEmpty)
        #expect(await harness.runtime.counters.endedBySession == 0)
        #expect(await harness.runtime.counters.droppedByEpoch == 1)
    }

    /// The epoch moves on a sign-in, a sign-out and a switch, and on nothing
    /// else a link says.
    @Test func theEpochMovesOnASessionChangeAlone() {
        let epochs = SessionEpoch()
        epochs.observe(Self.link("tokenA"))
        let signedIn = epochs.stamp.number
        var online = Self.link("tokenA")
        online.online = false
        online.waiting = 3
        epochs.observe(online)
        #expect(epochs.holds(signedIn))
        epochs.observe(Self.link("tokenB"))
        #expect(!epochs.holds(signedIn))
        let switched = epochs.stamp.number
        epochs.observe(Self.link("tokenB", repository: "o.example.com"))
        #expect(!epochs.holds(switched))
        let other = epochs.stamp.number
        epochs.observe(Self.link(nil))
        #expect(!epochs.holds(other))
    }
}
