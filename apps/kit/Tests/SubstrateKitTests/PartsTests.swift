import Foundation
import SubstrateAuth
import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing

#if canImport(SwiftUI)
import Observation
import SubstrateUI
#endif

/// A presentation published in parts, each under the path a view reads it
/// by, so only what moved is drawn again.
@Suite("A presentation in parts", .timeLimit(.minutes(10)))
struct PartsTests {
    let gates = Gates()

    private func paths(_ parts: [PresentationPart]) -> [[String]] { parts.map { $0.path.map(\.rawValue) } }

    /// A desk with two clerks, and their keys in the order they were made.
    private func desk() async -> (ControllerHarness<Desk>, [ChildKey]) {
        let harness = ControllerHarness(Desk(gates: gates))
        await harness.send(.open)
        await harness.send(.open)
        return (harness, [1, 2].map { ChildKey(SequentialIDs.id($0).uuidString.lowercased()) })
    }

    @Test func aControllerThatNamesNoPartsPublishesItsPresentationAsOne() async throws {
        let harness = ControllerHarness(Gauge())
        var parts = await harness.runtime.parts().makeAsyncIterator()
        let first = try #require(await parts.next())
        #expect(paths(first.changed) == [[]])
        #expect(first.changed.first?.value as? Gauge.Presentation == first.presentation)
        await harness.send(.add(3))
        let second = try #require(await parts.next())
        #expect((second.changed.first?.value as? Gauge.Presentation)?.level == 3)
        #expect(second.removed.isEmpty)
    }

    @Test func theFirstUpdateHoldsEveryPartAndEachAfterOnlyWhatMoved() async throws {
        let (harness, keys) = await desk()
        var parts = await harness.runtime.parts().makeAsyncIterator()
        let first = try #require(await parts.next())
        #expect(paths(first.changed) == [[]] + keys.sorted().map { [$0.rawValue] })
        #expect((first.changed.first?.value as? Desk.Presentation)?.clerks.isEmpty == true)
        #expect(first.presentation.clerks.count == 2)

        // An answer is the desk's own: no clerk's part goes out again.
        await harness.send(.wait("bell"))
        await gates.release("bell")
        try await harness.until { await $0.presentation.answers == ["bell"] }
        let second = try #require(await parts.next())
        #expect(paths(second.changed) == [[]])
        #expect((second.changed.first?.value as? Desk.Presentation)?.answers == ["bell"])
        #expect(second.presentation.clerks.count == 2)
    }

    @Test func whatMovedInOneChildGoesOutUnderItsPathAlone() async throws {
        let harness = ControllerHarness(Desk(gates: gates))
        await harness.send(.open)
        let key = ChildKey(SequentialIDs.id(1).uuidString.lowercased())
        await harness.send(.post("lunch"))
        try await harness.until { _ in await gates.isWaiting("lunch") }
        var parts = await harness.runtime.parts().makeAsyncIterator()
        _ = await parts.next()
        // The clerk is done waiting: its part moves, and so does the desk's
        // own, which lists who is busy. A second clerk made now is a part
        // of its own, and the first clerk's is not sent again for it.
        await gates.release("lunch")
        try await harness.until { await $0.presentation.busy.isEmpty }
        let done = try #require(await parts.next())
        #expect(Set(paths(done.changed)) == [[], [key.rawValue]])
        await harness.send(.open)
        let other = ChildKey(SequentialIDs.id(2).uuidString.lowercased())
        let made = try #require(await parts.next())
        #expect(paths(made.changed) == [[other.rawValue]])
    }

    @Test func aPathThatHoldsNoPartAnyMoreIsSaidRemoved() async throws {
        let (harness, keys) = await desk()
        var parts = await harness.runtime.parts().makeAsyncIterator()
        _ = await parts.next()
        await harness.send(.clear)
        let cleared = try #require(await parts.next())
        #expect(Set(cleared.removed) == Set(keys.map { [$0] }))
        #expect(cleared.changed.isEmpty)
        #expect(cleared.presentation.clerks.isEmpty)
    }

    @Test func aSlowReaderGetsEachPartsNewestValueAndEveryPathDropped() async throws {
        let (harness, keys) = await desk()
        var parts = await harness.runtime.parts().makeAsyncIterator()
        _ = await parts.next()
        // Three publishes before the reader looks again: an answer, the
        // clerks gone, and a clerk made after.
        await harness.send(.wait("bell"))
        await gates.release("bell")
        try await harness.until { await $0.presentation.answers == ["bell"] }
        await harness.send(.clear)
        await harness.send(.open)
        let third = ChildKey(SequentialIDs.id(3).uuidString.lowercased())
        let merged = try #require(await parts.next())
        #expect(merged.presentation == (await harness.presentation))
        #expect(Set(merged.removed) == Set(keys.map { [$0] }))
        #expect(Set(paths(merged.changed)) == [[], [third.rawValue]])
        // The reset left no answer: the desk's own part is the newest one.
        #expect((merged.changed.first { $0.path.isEmpty }?.value as? Desk.Presentation)?.answers == [])
    }

    @Test func aPublishThatMovesNoPartStillCarriesTheTree() async throws {
        // A controller whose parts leave something out of every one.
        struct Split: Controller {
            enum Command: Codable, Equatable, Sendable { case shown, hidden }
            typealias Event = Command
            struct State: Codable, Equatable, Sendable {
                var shown = 0
                var hidden = 0
            }

            var initialState: State { State() }
            func events(for command: Command, in context: Enrichment<Split>) -> [Event] { [command] }
            func apply(_ event: Event, to state: State) throws -> State {
                var next = state
                if event == .shown { next.shown += 1 } else { next.hidden += 1 }
                return next
            }
            func present(_ input: PresenterInput<Split>) -> State { input.state }
            func parts(of presentation: State) -> [PresentationPart] { [PresentationPart(presentation.shown)] }
        }
        let harness = ControllerHarness(Split())
        var parts = await harness.runtime.parts().makeAsyncIterator()
        _ = await parts.next()
        await harness.send(.hidden)
        let update = try #require(await parts.next())
        #expect(update.changed.isEmpty && update.removed.isEmpty)
        #expect(update.presentation.hidden == 1)
    }

    @Test func theLinkPublishesItsOwnScreensAndItsAppsPartsUnderTheAppsKey() async throws {
        let link = LinkController(
            manifest: ScreenFixtures.manifest, sessions: SessionStore(storage: MemoryStorage()),
            words: StatusWords(app: "Substrate Tasks", device: "this iPhone", kept: "tasks"))
        let harness = ControllerHarness(link)
        var parts = await harness.runtime.parts().makeAsyncIterator()
        let first = try #require(await parts.next())
        #expect(paths(first.changed) == [[], ["app"]])
        let own = try #require(first.changed.first?.value as? LinkPresentation<NoApp.State>)
        // The app is its own part, and the link's holds none of it.
        #expect(own.app == nil)
        #expect(first.presentation.app != nil)
        #expect(first.changed.last?.value is NoApp.State)
    }

    @Test func framesForAndroidAreTheSameWhileAReaderTakesParts() async throws {
        let (harness, _) = await desk()
        var parts = await harness.runtime.parts().makeAsyncIterator()
        _ = await parts.next()
        var frames = await harness.runtime.encodedFrames().makeAsyncIterator()
        let first = try JSONValue(data: #require(await frames.next()).presentation.get())
        #expect(first["clerks"]?.object?.count == 2)
        await harness.send(.wait("gong"))
        await gates.release("gong")
        try await harness.until { await $0.presentation.answers == ["gong"] }
        // The whole tree, clerks and all, whatever the parts left out.
        var tree = try JSONValue(data: #require(await frames.next()).presentation.get())
        if tree["answers"] != ["gong"] { tree = try JSONValue(data: #require(await frames.next()).presentation.get()) }
        #expect(tree["answers"] == ["gong"])
        #expect(tree["clerks"]?.object?.count == 2)
    }

    #if canImport(SwiftUI)
    /// Reads one part as a view's body does, and says whether what it read
    /// changed since.
    @MainActor
    private final class Reader {
        var fired = false

        func read<Root: Controller, Value>(_ model: PresentationModel<Root>, _ path: [ChildKey], as type: Value.Type) -> Value? {
            fired = false
            return withObservationTracking {
                model.part(at: path, as: type)
            } onChange: { [weak self] in
                Task { @MainActor in self?.fired = true }
            }
        }
    }

    @MainActor
    private func until(_ check: () -> Bool) async throws {
        for _ in 0 ..< 500 where !check() {
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(check())
    }

    @MainActor
    @Test func aViewOfOnePartIsNotDrawnAgainForAnother() async throws {
        let model = PresentationModel(Desk(gates: gates), environment: AppEnvironment.testing())
        model.send(.open)
        model.send(.open)
        try await until { model.presentation.clerks.count == 2 }
        let keys = model.presentation.clerks.keys.sorted()
        try await until { model.part(at: [keys[1]], as: Clerk.State.self) != nil }
        let own = Reader()
        let first = Reader()
        let second = Reader()
        #expect(own.read(model, [], as: Desk.Presentation.self)?.clerks.isEmpty == true)
        #expect(first.read(model, [keys[0]], as: Clerk.State.self) == Clerk.State())
        #expect(second.read(model, [keys[1]], as: Clerk.State.self) == Clerk.State())

        // An answer is the desk's own: neither clerk's reader hears of it.
        model.send(.wait("bell"))
        await gates.release("bell")
        try await until { own.fired }
        #expect(model.part(at: [], as: Desk.Presentation.self)?.answers == ["bell"])
        #expect(!first.fired && !second.fired)

        // The clerks gone: their readers hear, and find no part.
        model.send(.clear)
        try await until { first.fired && second.fired }
        #expect(model.part(at: [keys[0]], as: Clerk.State.self) == nil)
    }

    @MainActor
    @Test func aPartReadBeforeItIsPublishedDrawsItsReaderWhenItArrives() async throws {
        let model = PresentationModel(Desk(gates: gates), environment: AppEnvironment.testing())
        let key = ChildKey(SequentialIDs.id(1).uuidString.lowercased())
        let reader = Reader()
        #expect(reader.read(model, [key], as: Clerk.State.self) == nil)
        model.send(.open)
        try await until { reader.fired }
        #expect(model.part(at: [key], as: Clerk.State.self) == Clerk.State())
        // A part of another type than the one asked for reads as none.
        #expect(model.part(at: [key], as: Int.self) == nil)
    }
    #endif
}
