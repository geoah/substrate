import Foundation
import SubstrateAuth
import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing

/// The trace as words a test compares: what happened, in order.
func steps(_ trace: [RuntimeTrace]) -> [String] {
    trace.map { entry in
        switch entry {
        case let .replayed(path, events): "replayed \(path.map(\.rawValue)) \(events)"
        case let .setAside(path, _): "set aside \(path.map(\.rawValue))"
        case let .journaled(_, journaling, event): "journaled \(journaling) \(event)"
        case let .refused(_, event, _): "refused \(event)"
        case let .presented(path): path.isEmpty ? "presented" : "presented child"
        case let .replaced(path): "replaced \(path.map(\.rawValue))"
        case .published: "published"
        case let .effect(_, effect): "effect \(effect)"
        case let .dropped(_, drop): "dropped \(drop.rawValue)"
        }
    }
}

@Suite("The runtime's loop", .timeLimit(.minutes(10)))
struct RuntimeLoopTests {
    let journal = MemoryJournal<Training.Event, Training.State>()
    let shelf = HistoryShelf()

    var training: Training { Training(store: journal, shelf: shelf) }

    @Test func untilIdleWaitsForEveryEffectsWorkToEnd() async throws {
        let harness = ControllerHarness(training)
        await harness.send(.start)
        await harness.send(.log("100 kg × 5"))
        await harness.send(.finish)
        try await harness.untilIdle()
        #expect(await harness.runtime.activity == RuntimeActivity(started: 1, running: [], waiting: 0))
        #expect(await shelf.written == [["100 kg × 5"]])
    }

    @Test func aStepJournalsThenPresentsThenAsksForItsEffects() async throws {
        let harness = ControllerHarness(training, performsEffects: false)
        await harness.send(.start)
        harness.clearTrace()
        await harness.send(.log("100 kg × 5"))
        #expect(
            steps(harness.trace) == [
                "journaled append logged(\"100 kg × 5\")",
                "presented",
                "published",
                "effect announce(\"100 kg × 5, 1 so far\")",
            ])
        // The effect was decided from the presentation the event made.
        #expect(harness.effects == [.announce("100 kg × 5, 1 so far")])
    }

    @Test func theJournalHoldsTheEventWhenSendReturnsWhateverTheEffectsDo() async throws {
        let harness = ControllerHarness(training)
        await harness.send(.start)
        await harness.send(.log("a"))
        // No suspension between the engine and the write: the journal has
        // the event the moment the step is over, its effect not yet run.
        #expect(journal.events?.count == 2)
        #expect(journal.writes.map(\.count) == [1, 2])
        #expect(journal.cachedState?.sets == ["a"])
    }

    @Test func theCommandIsEnrichedWithTheEnvironmentsIdsAndInstant() async throws {
        let clock = TestClock()
        let harness = ControllerHarness(training, clock: clock)
        await harness.send(.start)
        #expect(journal.events == [.started(id: SequentialIDs.id(1), at: clock.now)])
    }

    @Test func aRefusalChangesTheTransientStateAndJournalsNothing() async throws {
        let harness = ControllerHarness(training)
        await harness.send(.log("a"))
        #expect(await harness.presentation.refusal == "No session is open")
        #expect(journal.events == nil)
        #expect(harness.effects.isEmpty)
        await harness.send(.start)
        #expect(await harness.presentation.refusal == nil)
    }

    @Test func aJournalThatCannotBeWrittenIsSaidAndTheStateMovesOn() async throws {
        let harness = ControllerHarness(training)
        await harness.send(.start)
        journal.failWrites(with: Refused(description: "No space left"))
        await harness.send(.log("a"))
        #expect(await harness.state.sets == ["a"])
        journal.failWrites(with: nil)
        await harness.send(.log("b"))
        #expect(journal.events?.count == 3)
    }

    @Test func aReplayFoldsTheJournalAndRunsNoEffect() async throws {
        let id = UUID()
        let stored = MemoryJournal<Training.Event, Training.State>(
            events: [.started(id: id, at: Date(timeIntervalSince1970: 0)), .logged("a"), .logged("b")])
        let harness = ControllerHarness(Training(store: stored, shelf: shelf))
        #expect(await harness.state == Training.State(id: id, sets: ["a", "b"]))
        #expect(steps(harness.trace) == ["replayed [] 3", "presented"])
        #expect(harness.effects.isEmpty)
        // The cache that was missing is written back from the fold.
        #expect(stored.cachedState?.sets == ["a", "b"])
    }

    @Test func aJournalTheEngineRefusesIsSetAside() async throws {
        let stored = MemoryJournal<Training.Event, Training.State>(events: [.logged("a")])
        let harness = ControllerHarness(Training(store: stored, shelf: shelf))
        #expect(await harness.state == Training.State())
        #expect(stored.setAsideCount == 1)
        #expect(steps(harness.trace) == ["set aside []", "presented"])
    }

    @Test func aFinishThatCrashesBeforeItsHistoryIsWrittenReplaysToAnOpenSession() async throws {
        // The first launch: started, two sets, then Finish, whose history
        // write never comes back: the process dies.
        let first = ControllerHarness(training, performsEffects: false)
        await first.send(.start)
        await first.send(.log("a"))
        await first.send(.log("b"))
        await first.send(.finish)
        #expect(first.effects.last == .writeHistory(["a", "b"]))
        #expect(await first.presentation.finishing)
        await first.runtime.close()

        // The relaunch folds the journal: the session is open, not
        // finishing, and nothing was lost.
        let second = ControllerHarness(training)
        #expect(await second.state.sets == ["a", "b"])
        #expect(await second.state.finishing == false)
        #expect(second.effects.isEmpty)

        // A failed write keeps the session open and the journal in place.
        await shelf.setFailing(true)
        await second.send(.finish)
        try await second.until { await $0.runtime.counters.delivered == 1 }
        #expect(await second.state.finishing == false)
        #expect(await second.state.sets == ["a", "b"])
        #expect(journal.events?.count == 3)

        // Then it lands: the history is written, then the journal goes.
        await shelf.setFailing(false)
        await second.send(.finish)
        try await second.until { await !$0.presentation.open }
        #expect(await shelf.written == [["a", "b"]])
        #expect(journal.events == nil)

        let third = ControllerHarness(training)
        #expect(await third.presentation.open == false)
    }

    @Test func thePresentationsNextChangeDeliversATick() async throws {
        let clock = TestClock()
        let harness = ControllerHarness(training, clock: clock)
        await harness.send(.start)
        await harness.send(.rest(seconds: 90))
        #expect(await harness.presentation.rest == "Rest 90 s")
        await harness.advance(by: 30)
        // Nothing changes before the instant the presentation named.
        #expect(await harness.presentation.rest == "Rest 90 s")
        await harness.advance(by: 60)
        try await harness.until { await $0.presentation.rest == nil }
        #expect(journal.events?.last == .restEnded)
    }

    @Test func theEventAtOpenRunsItsEffectsLiveAfterTheReplay() async throws {
        let id = UUID()
        let stored = MemoryJournal<Training.Event, Training.State>(events: [.started(id: id, at: Date(timeIntervalSince1970: 0)), .logged("a")])
        let harness = ControllerHarness(Training(store: stored, shelf: shelf, resumes: true))
        #expect(await harness.state.sets == ["a"])
        // Replayed with no effect, then the event at open on the live path,
        // presented first, its effect after.
        #expect(steps(harness.trace) == ["replayed [] 2", "presented", "effect restoreActivity(sets: 1)"])
        // Nothing was replayed: the event at open says so, and asks for none.
        let fresh = ControllerHarness(Training(store: MemoryJournal(), shelf: shelf, resumes: true))
        _ = await fresh.state
        #expect(fresh.effects.isEmpty)
    }

    @Test func makingARuntimeReadsNothingUntilItIsFirstUsed() async throws {
        let stored = MemoryJournal<Training.Event, Training.State>(events: [.started(id: UUID(), at: Date(timeIntervalSince1970: 0))])
        let runtime = Runtime(Training(store: stored, shelf: shelf), environment: .testing())
        // The journal is folded on the runtime's own executor, at the first
        // call, never in the initialiser a view's main actor runs.
        #expect(stored.reads == 0)
        #expect(await runtime.presentation.open)
        #expect(stored.reads == 1)
        _ = await runtime.state
        #expect(stored.reads == 1)
    }

    @Test func theEnvironmentsLocaleAndTimeZoneReachThePresenter() async throws {
        let input = PresenterInput<Training>(
            state: Training.State(), transient: Training.Transient(), now: Date(),
            locale: Locale(identifier: "de_DE"), timeZone: TimeZone(identifier: "Europe/Berlin")!)
        #expect(input.calendar.timeZone.identifier == "Europe/Berlin")
        #expect(input.calendar.locale?.identifier == "de_DE")
    }
}

@Suite("Effects, keys and generations", .timeLimit(.minutes(10)))
struct RuntimeEffectTests {
    let gates = Gates()
    let source = Source()

    var feed: Feed { Feed(gates: gates, source: source) }

    @Test func startingUnderAKeyCancelsTheRunningOneAndDropsItsLateResult() async throws {
        let harness = ControllerHarness(feed)
        await harness.send(.fetch("first"))
        try await harness.until { _ in await gates.isWaiting("first") }
        await harness.send(.fetch("second"))
        await gates.release("second")
        try await harness.until { await $0.state.fetched == ["second"] }
        // The first one's work ignores its cancellation and answers anyway.
        await gates.release("first")
        try await harness.until { await $0.runtime.counters.droppedByEpoch == 1 }
        #expect(await harness.state.fetched == ["second"])
    }

    @Test func aResetDropsEveryResultStillOnItsWay() async throws {
        let harness = ControllerHarness(feed)
        await harness.send(.fetch("slow"))
        try await harness.until { _ in await gates.isWaiting("slow") }
        await harness.runtime.reset()
        await gates.release("slow")
        try await harness.until { await $0.runtime.counters.droppedByGeneration == 1 }
        #expect(await harness.state.fetched.isEmpty)
    }

    @Test func aLongLivedEffectDeliversUntilItIsCancelled() async throws {
        let harness = ControllerHarness(feed)
        await harness.send(.follow)
        try await harness.until { _ in source.isOpen }
        source.send(1)
        source.send(2)
        try await harness.until { await $0.state.heard == [1, 2] }
        await harness.send(.unfollow)
        source.send(3)
        await harness.settle()
        #expect(await harness.state.heard == [1, 2])
        // Following again opens a new stream; the old one's values stay out.
        await harness.send(.follow)
        try await harness.until { _ in source.opened == 2 }
        source.send(4)
        try await harness.until { await $0.state.heard == [1, 2, 4] }
    }

    @Test func eventsInAWindowPresentOnceAtItsEnd() async throws {
        let clock = TestClock()
        let harness = ControllerHarness(feed, clock: clock)
        harness.clearTrace()
        await harness.send(.delta("Hel"))
        await harness.send(.delta("lo"))
        await harness.send(.delta(","))
        // Applied at once, drawn not yet.
        #expect(await harness.state.text == "Hello,")
        #expect(await harness.presentation.text == "")
        #expect(harness.published == 0)
        await harness.advance(by: 0.05)
        try await harness.until { await $0.presentation.text == "Hello," }
        #expect(harness.published == 1)
    }

    @Test func aShorterWindowAskedLaterMovesTheDeadlineEarlier() async throws {
        let clock = TestClock()
        let harness = ControllerHarness(feed, clock: clock)
        await harness.send(.follow)
        try await harness.until { _ in source.isOpen }
        harness.clearTrace()
        await harness.send(.delta("a"))
        await harness.advance(by: 0.005)
        #expect(harness.published == 0)
        // A value heard is drawn within 10 ms of it, not at the end of the
        // delta's 50.
        source.send(7)
        try await harness.until { await $0.state.heard == [7] }
        await harness.advance(by: 0.010)
        try await harness.until { await $0.presentation.heard == [7] }
        #expect(await harness.presentation.text == "a")
        #expect(harness.published == 1)
        #expect(clock.now == TestClock().now.addingTimeInterval(0.015))
    }

    @Test func anEventThatPresentsNowEndsTheWindowEarly() async throws {
        let harness = ControllerHarness(feed)
        harness.clearTrace()
        await harness.send(.delta("a"))
        await harness.send(.mark)
        #expect(await harness.presentation == Feed.State(text: "a", marks: 1))
        #expect(harness.published == 1)
    }
}

@Suite("Children in one tree", .timeLimit(.minutes(10)))
struct RuntimeChildTests {
    let gates = Gates()

    @Test func aChildsEventIsFoldedByItsParentAndPublishedAsOneTree() async throws {
        let harness = ControllerHarness(Lobby(gates: gates))
        await harness.send(.open("Kitchen"))
        let key = ChildKey(SequentialIDs.id(1).uuidString.lowercased())
        #expect(await harness.presentation.rooms[key]?.name == "Kitchen")
        harness.clearTrace()
        try await harness.runtime.send(Room.Command.say("hello"), to: [key])
        // The child's step and the parent's fold go out together.
        #expect(steps(harness.trace) == ["presented child", "presented", "published"])
        let presented = await harness.presentation
        #expect(presented.transcript == ["Kitchen: hello"])
        #expect(presented.rooms[key]?.spoken == ["hello"])
    }

    @Test func theLinkReachesEveryChildAndEachOneMadeLater() async throws {
        let harness = ControllerHarness(Lobby(gates: gates))
        await harness.send(.open("Kitchen"))
        await harness.runtime.linkChanged(LinkSnapshot(phase: .signedIn, generation: 3))
        await harness.send(.open("Hall"))
        let rooms = await harness.presentation.rooms.values.map(\.link)
        #expect(rooms == [3, 3])
    }

    @Test func aDroppedKeyClosesItsChildAndDropsItsResults() async throws {
        let harness = ControllerHarness(Lobby(gates: gates))
        await harness.send(.open("Kitchen"))
        let key = ChildKey(SequentialIDs.id(1).uuidString.lowercased())
        try await harness.runtime.send(Room.Command.wait("bell"), to: [key])
        try await harness.until { _ in await gates.isWaiting("bell") }
        await harness.send(.close(key))
        #expect(await harness.runtime.state(of: key) == nil)
        await gates.release("bell")
        try await harness.until { await $0.runtime.counters.droppedByGeneration == 1 }
        #expect(await harness.presentation.rooms.isEmpty)
    }

    @Test func aParentHandsItsChildrenANewValueAndTheirStateStays() async throws {
        let harness = ControllerHarness(Lobby(gates: gates))
        await harness.send(.open("Kitchen"))
        let key = ChildKey(SequentialIDs.id(1).uuidString.lowercased())
        try await harness.runtime.send(Room.Command.say("hello"), to: [key])
        harness.clearTrace()
        await harness.send(.topic("Dinner"))
        let room = try #require(await harness.presentation.rooms[key])
        #expect(room.topic == "Dinner")
        #expect(room.spoken == ["hello"])
        #expect(steps(harness.trace).contains("replaced [\"\(key.rawValue)\"]"))
        #expect(harness.published == 1)
        // The same value again replaces nothing.
        harness.clearTrace()
        await harness.send(.topic("Dinner"))
        #expect(!steps(harness.trace).contains { $0.hasPrefix("replaced") })
    }

    @Test func aChildClosedByItsParentsFoldMidStepIsGoneInTheSameTree() async throws {
        let harness = ControllerHarness(Lobby(gates: gates))
        await harness.send(.open("Kitchen"))
        await harness.send(.open("Hall"))
        let kitchen = ChildKey(SequentialIDs.id(1).uuidString.lowercased())
        let hall = ChildKey(SequentialIDs.id(2).uuidString.lowercased())
        try await harness.runtime.send(Room.Command.bind("thread-1"), to: [hall])
        try await harness.runtime.send(Room.Command.wait("bell"), to: [hall])
        try await harness.until { _ in await gates.isWaiting("bell") }
        harness.clearTrace()
        // The kitchen binds the hall's thread: the parent's fold of the
        // kitchen's event closes the hall inside the kitchen's step.
        try await harness.runtime.send(Room.Command.bind("thread-1"), to: [kitchen])
        #expect(await harness.runtime.state(of: hall) == nil)
        #expect(await harness.presentation.rooms.keys.sorted() == [kitchen])
        #expect(harness.published == 1)
        await gates.release("bell")
        try await harness.until { await $0.runtime.counters.droppedByGeneration == 1 }
        // The kitchen itself goes on.
        try await harness.runtime.send(Room.Command.say("still here"), to: [kitchen])
        #expect(await harness.presentation.transcript == ["Kitchen: still here"])
    }

    @Test func aCommandReachesAControllerTwoKeysDown() async throws {
        let link = LinkController(
            app: Lobby(gates: gates), manifest: ScreenFixtures.manifest,
            sessions: SessionStore(storage: MemoryStorage()),
            words: StatusWords(app: "Lobby", device: "this iPhone", kept: "rooms"))
        let harness = ControllerHarness(link, performsEffects: false)
        try await harness.runtime.send(Lobby.Command.open("Kitchen"), to: LinkController<Lobby>.appPath)
        let key = ChildKey(SequentialIDs.id(1).uuidString.lowercased())
        try await harness.runtime.send(Room.Command.say("hi"), to: LinkController<Lobby>.appPath + [key])
        let app = try #require(await harness.presentation.app)
        #expect(app.transcript == ["Kitchen: hi"])
        // A room made under the link is told the link's snapshot as it opens.
        #expect(app.rooms[key]?.link == 0)
    }

    @Test func aRoutedCommandReachesTheChildItNames() async throws {
        let harness = ControllerHarness(Lobby(gates: gates))
        await harness.send(.open("Kitchen"))
        let key = ChildKey(SequentialIDs.id(1).uuidString.lowercased())
        let say = try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(Room.Command.say("hi")))
        try await harness.runtime.send(encoded: JSONEncoder().encode(RoutedCommand(path: [key], command: say)))
        #expect(await harness.presentation.transcript == ["Kitchen: hi"])
        let nowhere = RoutedCommand(path: [ChildKey("nobody")], command: say)
        await #expect(throws: UnroutableCommand.self) {
            try await harness.runtime.send(encoded: JSONEncoder().encode(nowhere))
        }
    }

    @Test func thePresentationsGoOutEncodedForTheBridge() async throws {
        let harness = ControllerHarness(Lobby(gates: gates))
        let stream = await harness.runtime.encodedPresentations()
        var iterator = stream.makeAsyncIterator()
        let first = try #require(await iterator.next()).get()
        #expect(String(decoding: first, as: UTF8.self) == #"{"rooms":{},"transcript":[]}"#)
        await harness.send(.open("Kitchen"))
        let second = try #require(await iterator.next()).get()
        let decoded = try JSONDecoder().decode(Lobby.Presentation.self, from: second)
        #expect(decoded.rooms.values.map(\.name) == ["Kitchen"])
    }
}

@Suite("The bridge's presentations", .timeLimit(.minutes(10)))
struct EncodedPresentationTests {
    @Test func aPresentationThatDoesNotEncodeArrivesAsItsFailureAndTheNextStillComes() async throws {
        let harness = ControllerHarness(Fragile())
        let stream = await harness.runtime.encodedPresentations()
        var iterator = stream.makeAsyncIterator()
        #expect(try #require(await iterator.next()).get() == Data("false".utf8))
        await harness.send(.set(true))
        let failed = try #require(await iterator.next())
        guard case let .failure(failure) = failed else { throw Refused(description: "it encoded") }
        #expect(failure.reason.contains("broken on purpose"))
        await harness.send(.set(false))
        #expect(try #require(await iterator.next()).get() == Data("false".utf8))
    }
}

@Suite("Keyed lists", .timeLimit(.minutes(10)))
struct KeyedListTests {
    struct Line: Codable, Equatable, Sendable, Identifiable {
        var id: String
        var text: String
    }

    @Test func aDeltaCarriesOnlyWhatMovedAndTheOrder() throws {
        let base = KeyedList([Line(id: "a", text: "A"), Line(id: "b", text: "B"), Line(id: "c", text: "C")])
        var next = base
        next.update([Line(id: "c", text: "C"), Line(id: "a", text: "A, edited"), Line(id: "d", text: "D")])
        #expect(next.revision == 1)
        let delta = next.delta(from: base)
        #expect(
            delta
                == KeyedListDelta(
                    base: 0, revision: 1, inserted: [Line(id: "d", text: "D")],
                    changed: [Line(id: "a", text: "A, edited")], removed: ["b"], order: ["c", "a", "d"]))
        var reader = base
        try reader.apply(delta)
        #expect(reader == next)
    }

    @Test func theRevisionMovesOnlyOnAChange() {
        var list = KeyedList([Line(id: "a", text: "A")])
        list.update([Line(id: "a", text: "A")])
        #expect(list.revision == 0)
        list.update([Line(id: "a", text: "A"), Line(id: "a", text: "again")])
        #expect(list.revision == 0)
        #expect(list.rows.count == 1)
    }

    @Test func aDeltaFromAnotherRevisionIsRefused() {
        var reader = KeyedList([Line(id: "a", text: "A")])
        var writer = reader
        writer.update([Line(id: "b", text: "B")])
        let first = writer.delta(from: reader)
        var later = writer
        later.update([Line(id: "c", text: "C")])
        let second = later.delta(from: writer)
        #expect(throws: KeyedListMismatch(revision: 0, base: 1)) { try reader.apply(second) }
        #expect(throws: Never.self) { try reader.apply(first) }
        #expect(throws: Never.self) { try reader.apply(second) }
        #expect(reader == later)
    }
}
