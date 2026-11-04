import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateTesting
import Testing
#if canImport(SwiftUI)
import SubstrateUI
#endif

/// A controller whose effects turn on what an event changed: the level it
/// crossed, from the state before and after, with no flag in its state.
struct Gauge: Controller {
    enum Command: Codable, Equatable, Sendable {
        case add(Int)
        case ping
    }

    enum Event: Codable, Equatable, Sendable {
        case added(Int)
        case pinged
    }

    struct State: Codable, Equatable, Sendable {
        var level = 0
    }

    struct Transient: Equatable, Sendable {
        var ping = Signal<String>()
    }

    struct Presentation: Codable, Equatable, Sendable {
        var level: Int
        var ping: Signal<String>
    }

    enum Effect: Equatable, Sendable {
        case crossed(from: Int, to: Int)
    }

    var initialState: State { State() }
    var initialTransient: Transient { Transient() }

    func events(for command: Command, in context: Enrichment<Gauge>) -> [Event] {
        switch command {
        case let .add(amount): [.added(amount)]
        case .ping: [.pinged]
        }
    }

    func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        if case let .added(amount) = event { next.level += amount }
        return next
    }

    func transient(after event: Event, applied state: State, from transient: Transient) -> Transient {
        var next = transient
        if case .pinged = event { next.ping.raise("ping") }
        return next
    }

    func present(_ input: PresenterInput<Gauge>) -> Presentation {
        Presentation(level: input.state.level, ping: input.transient.ping)
    }

    func effects(of event: Event, on state: State, from previous: State, presented presentation: Presentation) -> [Effect] {
        previous.level / 10 == state.level / 10 ? [] : [.crossed(from: previous.level, to: state.level)]
    }
}

@Suite("Effects that see the state before, and signals", .timeLimit(.minutes(10)))
struct EffectAndSignalTests {
    @Test func anEffectSeesTheStateBeforeItsEvent() async {
        let harness = ControllerHarness(Gauge(), performsEffects: false)
        await harness.send(.add(4))
        await harness.send(.add(4))
        await harness.send(.add(4))
        await harness.send(.add(1))
        #expect(harness.effects == [.crossed(from: 8, to: 12)])
    }

    @Test func aControllerWithTheEffectsOfTheStateAfterAloneStillRunsThem() async {
        // `Training` implements `effects(of:on:presented:)` alone; the
        // runtime calls the four-argument one, whose default is that.
        let harness = ControllerHarness(Training(store: MemoryJournal<Training.Event, Training.State>(), shelf: HistoryShelf()),
                                        performsEffects: false)
        await harness.send(.start)
        await harness.send(.log("a"))
        #expect(harness.effects == [.announce("a, 1 so far")])
    }

    @Test func aSignalMovesItsSerialWithEveryRaiseAndKeepsItThroughAClear() {
        var signal = Signal<String>()
        #expect(signal.value == nil && signal.serial == 0)
        signal.raise("a")
        signal.raise("a")
        #expect(signal.value == "a" && signal.serial == 2)
        signal.clear()
        #expect(signal.value == nil && signal.serial == 2)
        signal.raise("b")
        #expect(signal.serial == 3)
    }

    #if canImport(SwiftUI)
    @MainActor
    @Test func aSignalActsOnceForEachRaiseAndNeverForARedraw() async throws {
        let model = PresentationModel(Gauge(), environment: AppEnvironment.testing())
        var heard: [String] = []
        model.onSignal(\.ping) { heard.append($0) }
        model.send(.ping)
        try await until { model.presentation.ping.serial == 1 }
        // Drawn again for another reason: the ping is not heard again.
        model.send(.add(1))
        try await until { model.presentation.level == 1 }
        model.send(.ping)
        try await until { model.presentation.ping.serial == 2 }
        #expect(heard == ["ping", "ping"])
    }

    @MainActor
    private func until(_ check: () -> Bool) async throws {
        for _ in 0 ..< 500 where !check() {
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(check())
    }
    #endif
}
