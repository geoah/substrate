import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing

private let shelfKind = "example.com/shelf/book"

private struct Unexpected: Error {}

private let reachable = LinkSnapshot(
    phase: .signedIn, server: URL(string: "https://ada.example.com")!, repository: "ada.example.com",
    scopes: ["write:example.com/shelf/*"])

/// An app whose records are the kit's, writing none of their stages: its
/// own event, its own effect, and an editor whose save is two events.
private struct Shelf: LinkedRecordsApp {
    enum Command: Codable, Equatable, Sendable {
        case add(String)
        case edit
        case dismissProblem
    }

    enum Event: Codable, Equatable, Sendable, LinkedRecordsEvent {
        case records(LinkedRecords.Event)
        case editing(ChildKey)
        case closed(ChildKey)
        case problemDismissed

        var recordsEvent: LinkedRecords.Event? {
            if case let .records(event) = self { event } else { nil }
        }
    }

    enum Effect: Equatable, Sendable, LinkedRecordsEffect {
        case records(LinkedRecords.Effect)
        case count(Int)

        var recordsEffect: LinkedRecords.Effect? {
            if case let .records(effect) = self { effect } else { nil }
        }
    }

    struct State: Codable, Equatable, Sendable, LinkedRecordsHolding {
        var linked = LinkedRecordsState()
        var editors: Set<ChildKey> = []
        var problem: String?
        var counted = 0
    }

    typealias Presentation = State
    typealias Child = ShelfEditor

    let linked: LinkedRecords

    static func == (lhs: Shelf, rhs: Shelf) -> Bool { lhs.linked == rhs.linked }

    var initialState: State { State() }

    func events(for command: Command, in context: Enrichment<Shelf>) -> [Event] {
        let writes = RecordWrites(mint: { context.mint() })
        switch command {
        case let .add(title): return [.records(.wrote(writes.create(shelfKind, writes.newID(), properties: ["title": .string(title)])))]
        case .edit: return [.editing(ChildKey("editor"))]
        case .dismissProblem: return [.problemDismissed]
        }
    }

    func apply(_ event: Event, to state: State, from previous: State) throws -> State {
        var next = state
        switch event {
        case let .records(.notQueued(_, words)): next.problem = words
        case .records: break
        case let .editing(key): next.editors.insert(key)
        case let .closed(key): next.editors.remove(key)
        case .problemDismissed: next.problem = nil
        }
        next.counted = next.linked.records.count
        return next
    }

    func ownEffects(of event: Event, on state: State, from previous: State, presented presentation: State) -> [Effect] {
        state.counted == previous.counted ? [] : [.count(state.counted)]
    }

    func ownKey(of effect: Effect) -> String? { "count" }

    func children(in state: State) -> [ChildKey: ShelfEditor] {
        Dictionary(uniqueKeysWithValues: state.editors.map { ($0, ShelfEditor()) })
    }

    func events(fromChild key: ChildKey, _ event: ShelfEditor.Event) -> [Event] {
        switch event {
        case let .saved(write): [.closed(key), .records(.wrote(write))]
        }
    }

    func present(_ input: PresenterInput<Shelf>) -> State { input.state }
}

private struct ShelfEditor: Controller, Equatable {
    enum Command: Codable, Equatable, Sendable { case save(String) }
    enum Event: Codable, Equatable, Sendable { case saved(LocalWrite) }
    struct State: Codable, Equatable, Sendable {}
    typealias Presentation = State

    var initialState: State { State() }

    func events(for command: Command, in context: Enrichment<ShelfEditor>) -> [Event] {
        let writes = RecordWrites(mint: { context.mint() })
        switch command {
        case let .save(title): return [.saved(writes.create(shelfKind, writes.newID(), properties: ["title": .string(title)]))]
        }
    }

    func apply(_ event: Event, to state: State) throws -> State { state }
    func present(_ input: PresenterInput<ShelfEditor>) -> State { input.state }
}

private func shelf() -> Shelf {
    Shelf(linked: LinkedRecords(kinds: [shelfKind], cache: nil, outbox: nil, words: StatusWords(app: "Shelf", device: "this iPhone", kept: "books")))
}

@Suite("An app over linked records", .timeLimit(.minutes(10)))
struct LinkedRecordsAppTests {
    @Test func theRecordsStagesAreTheKitsAndTheAppWritesItsOwnAlone() async throws {
        let harness = ControllerHarness(shelf(), performsEffects: false)
        // Opened with the app: the device's copy read and the outbox followed.
        _ = await harness.state
        #expect(harness.effects.contains(.records(.load)))
        #expect(harness.effects.contains(.records(.followOutbox)))
        await harness.deliver(.records(.loaded([], cursor: nil, queue: [])))
        await harness.runtime.linkChanged(reachable)
        #expect(harness.effects.contains(.records(.read(over: [shelfKind]))))
        harness.clearTrace()
        await harness.send(.add("Dune"))
        // The records' effect, then the app's own, from one event.
        let effects = harness.effects
        #expect(effects.count == 2)
        guard case .records(.enqueue) = effects.first else { throw Unexpected() }
        #expect(effects.last == .count(1))
        #expect(await harness.state.linked.records.count == 1)
        // A write the device could not keep is the app's problem to say.
        await harness.deliver(.records(.notQueued([], "Not kept.")))
        #expect(await harness.state.problem == "Not kept.")
        let refused = Date(timeIntervalSince1970: 1_793_696_400)
        #expect(Shelf.Event.records(.relistNeeded(at: refused)).recordsEvent == .relistNeeded(at: refused))
        #expect(Shelf.Event.problemDismissed.recordsEvent == nil)
        #expect(shelf().key(of: .records(.read(over: [shelfKind]))) == "records.watch")
        #expect(shelf().key(of: .count(1)) == "count")
        #expect(shelf().report(of: .records(.watchEnded(.needsApproval, ""))) == .needsApproval)
        #expect(shelf().coalescing(of: .records(.changed("k", nil))) == .within(.milliseconds(100)))
    }

    @Test func aChildsEventMayBeSeveralOfTheParentsEachSeenAsItsOwn() async throws {
        let harness = ControllerHarness(shelf(), performsEffects: false)
        await harness.deliver(.records(.loaded([], cursor: nil, queue: [])))
        await harness.send(.edit)
        let key = ChildKey("editor")
        #expect(await harness.state.editors == [key])
        harness.clearTrace()
        try await harness.runtime.send(ShelfEditor.Command.save("Emma"), to: [key])
        // Closed, and the write laid over and queued, as one of the app's own.
        #expect(await harness.state.editors.isEmpty)
        #expect(await harness.state.linked.records.values.map { $0.properties["title"] } == ["Emma"])
        #expect(harness.effects.contains { if case .records(.enqueue) = $0 { true } else { false } })
    }
}

@Suite("A stopped read", .timeLimit(.minutes(10)))
struct StoppedReadTests {
    private struct Stepper {
        let records = LinkedRecords(
            kinds: [shelfKind], cache: nil, outbox: nil, words: StatusWords(app: "Shelf", device: "this iPhone", kept: "books"),
            limit: 2)
        var state = LinkedRecordsState()

        mutating func step(_ event: LinkedRecords.Event) -> [LinkedRecords.Effect] {
            let previous = state
            state = records.apply(event, to: state)
            return records.effects(of: event, on: state, from: previous)
        }
    }

    @Test func anUnapprovedReadWaitsForANewApprovalNotTheNetwork() {
        var stepper = Stepper()
        _ = stepper.step(.loaded([], cursor: nil, queue: []))
        // A cold start: signed in, the network not reported yet.
        #expect(stepper.step(.linked(reachable)) == [.read(over: [shelfKind])])
        _ = stepper.step(.readFailed("Not in scope.", .needsApproval))
        #expect(stepper.state.phase == .stopped(.needsApproval, "Not in scope."))
        // The network's first report, and the app coming forward, ask nothing again.
        var online = reachable
        online.online = true
        #expect(stepper.step(.linked(online)).isEmpty)
        #expect(stepper.step(.foregrounded).isEmpty)
        // A new approval is a new token: it reads again.
        var approved = online
        approved.scopes = ["write:example.com/shelf/*", "read:example.com/people/*"]
        #expect(stepper.step(.linked(approved)) == [.read(over: [shelfKind])])
    }

    @Test func aSignedOutWatchWaitsForASignInAndARefusedOneForTheNetwork() {
        var stepper = Stepper()
        let cursor = WatchCursor(seq: 3, generation: "g")
        _ = stepper.step(.loaded([], cursor: cursor, queue: []))
        var online = reachable
        online.online = true
        #expect(stepper.step(.linked(online)) == [.follow(cursor, over: [shelfKind])])
        _ = stepper.step(.watchEnded(.signedOut, "Your access ended."))
        var offline = online
        offline.online = false
        _ = stepper.step(.linked(offline))
        #expect(stepper.step(.linked(online)).isEmpty)
        var ended = online
        ended.phase = .accessEnded
        _ = stepper.step(.linked(ended))
        #expect(stepper.step(.linked(online)) == [.follow(cursor, over: [shelfKind])])
        // Refused for the server's own reasons: the network back lifts it.
        _ = stepper.step(.watchEnded(.refused, "The server is restarting."))
        _ = stepper.step(.linked(offline))
        #expect(stepper.step(.linked(online)) == [.follow(cursor, over: [shelfKind])])
    }

    @Test func aReadThatStoppedAtItsLimitSaysSo() {
        var stepper = Stepper()
        let cursor = WatchCursor(seq: 3, generation: "g")
        _ = stepper.step(.loaded([], cursor: nil, queue: []))
        _ = stepper.step(.linked(reachable))
        let books = ["a", "b"].map { Record(id: $0, kind: shelfKind, properties: [:]) }
        _ = stepper.step(.listed(books, cursor: cursor, more: true))
        #expect(stepper.records.notice(stepper.state)?.text == "Only the first 2 of your books are shown: Shelf reads no more.")
        _ = stepper.step(.listed(books, cursor: cursor))
        #expect(stepper.records.notice(stepper.state) == nil)
    }

    @Test func theReadAsksForNoMoreThanItsLimit() async throws {
        let server = ScriptedServer()
        let page: @Sendable ([String], String?) -> String = { ids, cursor in
            let rows = ids.map { #"{"id":"\#($0)","kind":"\#(shelfKind)","properties":{},"version":1}"# }.joined(separator: ",")
            return #"{"records":[\#(rows)],"cursor":\#(cursor.map { "\"\($0)\"" } ?? "null"),"head":1,"generation":"g"}"#
        }
        server.answer("GET", "/api/v1/changes", 200, #"{"bookmark":1,"generation":"g"}"#)
        server.on { ($0.query ?? "").contains("after=") ? (200, page(["c"], nil)) : (200, page(["a", "b"], "next")) }
        let client = SubstrateClient(server: URL(string: "https://ada.example.com")!, token: "substrate_tok_test", transport: server)
        var environment = AppEnvironment.testing()
        environment.client = ClientSource { client }
        let records = Stepper().records
        guard case let .sequence(make) = records.perform(.read(over: [shelfKind]), in: environment) else { throw Unexpected() }
        var events: [LinkedRecords.Event] = []
        for await event in make() { events.append(event) }
        guard case let .listed(listed, cursor, more) = events.last else { throw Unexpected() }
        #expect(listed.map(\.id) == ["a", "b"] && more)
        #expect(cursor == WatchCursor(seq: 1, generation: "g"))
        // The page after the limit is never asked for.
        let lists = server.sent.filter { $0.path == "/api/v1/records" }
        #expect(lists.count == 1)
        #expect(!lists.contains { ($0.query ?? "").contains("after=") })
    }

    /// A watch that cannot open holds back neither the list nor why it was
    /// refused: the list's 403 is the approval the token needs.
    @Test func aReadWhoseWatchCannotOpenStillSaysWhyTheListWasRefused() async throws {
        let server = ScriptedServer()
        server.answer("GET", "/api/v1/changes", 404, #"{"error":{"code":"not_found","message":"unknown kind example.com/shelf/book"}}"#)
        server.answer(
            "GET", "/api/v1/records", 403, #"{"error":{"code":"forbidden","message":"not in scope"}}"#,
            headers: ["WWW-Authenticate": #"Bearer error="insufficient_scope""#])
        let client = SubstrateClient(server: URL(string: "https://ada.example.com")!, token: "substrate_tok_test", transport: server)
        var environment = AppEnvironment.testing()
        environment.client = ClientSource { client }
        guard case let .sequence(make) = Stepper().records.perform(.read(over: [shelfKind]), in: environment) else { throw Unexpected() }
        var events: [LinkedRecords.Event] = []
        for await event in make() { events.append(event) }
        guard events.count == 1, case .readFailed(_, .needsApproval) = events[0] else { throw Unexpected() }
    }
}

@Suite("The harness's bounds", .timeLimit(.minutes(10)))
struct HarnessBoundsTests {
    @Test func aWaitIsBoundedInAllEvenWhileTheRuntimeKeepsMoving() async throws {
        let harness = ControllerHarness(shelf(), performsEffects: false)
        let ticking = Task {
            while !Task.isCancelled {
                await harness.deliver(.problemDismissed)
                try? await Task.sleep(for: .milliseconds(5))
            }
        }
        defer { ticking.cancel() }
        let started = ContinuousClock.now
        await #expect(throws: HarnessTimedOut.self) { try await harness.until(timeout: .milliseconds(300)) { _ in false } }
        #expect(ContinuousClock.now - started < .seconds(5))
        await #expect(throws: HarnessTimedOut.self) { try await harness.until(idle: .milliseconds(30)) { _ in false } }
    }
}
