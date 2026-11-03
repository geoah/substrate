import Foundation
import SubstrateRuntime
import SubstrateTesting
import Testing

@Suite("A parent and its children, both ways")
struct RuntimeCompositionTests {
    let gates = Gates()

    @Test func aNewValueReachesTheChildAsAnEventWhoseEffectsRun() async throws {
        let harness = ControllerHarness(Desk(gates: gates))
        await harness.send(.open)
        let key = ChildKey(SequentialIDs.id(1).uuidString.lowercased())
        await harness.send(.post("lunch at one"))
        // The value changed: the clerk read it and waits on it.
        #expect(await harness.presentation.clerks[key]?.read == ["lunch at one"])
        try await harness.until { _ in await gates.isWaiting("lunch at one") }
        // The same value again is no event.
        await harness.send(.post("lunch at one"))
        #expect(await harness.presentation.clerks[key]?.read == ["lunch at one"])
    }

    @Test func theParentFoldsWhatTheChildHoldsAfterItsEvent() async throws {
        let harness = ControllerHarness(Desk(gates: gates))
        await harness.send(.open)
        let key = ChildKey(SequentialIDs.id(1).uuidString.lowercased())
        await harness.send(.post("fire drill"))
        #expect(await harness.presentation.busy == [key])
        await gates.release("fire drill")
        try await harness.until { await $0.presentation.busy.isEmpty }
    }

    @Test func aResettingEventDropsEveryResultOnItsWayAndClosesTheChildren() async throws {
        let harness = ControllerHarness(Desk(gates: gates))
        await harness.send(.open)
        await harness.send(.wait("bell"))
        try await harness.until { _ in await gates.isWaiting("bell") }
        await harness.send(.clear)
        #expect(await harness.presentation.clerks.isEmpty)
        await gates.release("bell")
        try await harness.until { await $0.runtime.counters.droppedByGeneration == 1 }
        #expect(await harness.presentation.answers.isEmpty)
        // What starts after the reset arrives.
        await harness.send(.wait("gong"))
        try await harness.until { _ in await gates.isWaiting("gong") }
        await gates.release("gong")
        try await harness.until { await $0.presentation.answers == ["gong"] }
    }
}
