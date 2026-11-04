import Foundation
@testable import WorkoutsCore
import Testing
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateTesting

/// A scratch directory under the system's temporary directory, removed by `remove()`.
struct TemporaryDirectory {
    let url: URL

    init() throws {
        url = FileManager.default.temporaryDirectory.appendingPathComponent("workouts-tests-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
    }

    func remove() {
        try? FileManager.default.removeItem(at: url)
    }

    func files(in subdirectory: String? = nil) -> [String] {
        let directory = subdirectory.map { url.appendingPathComponent($0) } ?? url
        return ((try? FileManager.default.contentsOfDirectory(atPath: directory.path)) ?? []).sorted()
    }
}

/// Stand-ins for the engine's `EventEnvelope` and `SessionState`: the journal
/// only needs them to be Codable, and the state Equatable.
private struct TallyEvent: Codable, Sendable, Equatable {
    var id: EventID
    var at: Date
    var load: Mass
}

private struct TallyState: Codable, Sendable, Equatable {
    var total: Decimal = 0
    var count = 0
    var last: Date?
    var target: SetTarget?
}

private func fold(_ events: [TallyEvent]) -> TallyState {
    var state = TallyState()
    for event in events {
        state.total += event.load.value
        state.count += 1
        state.last = event.at
        state.target = SetTarget(plannedSet: "s\(state.count)", load: event.load, rpeMin: Decimal(string: "9.5"), rpeMax: Decimal(string: "9.5"))
    }
    return state
}

/// The session's journal is the kit's `JournalFile`, in the app's folder
/// under the name and format every build has kept.
private struct Journal {
    let file: JournalFile<TallyEvent, TallyState>
    let url: URL

    init(directory: URL, writingOptions: Data.WritingOptions) {
        let folder = try! RepositoryFolder(adopting: directory, writingOptions: writingOptions)
        file = JournalFile(folder: folder, name: SessionJournalFile.fileName, area: .root, format: SessionJournalFile.format)
        url = directory.appendingPathComponent(SessionJournalFile.fileName)
    }

    var exists: Bool { FileManager.default.fileExists(atPath: url.path) }
    func write(events: [TallyEvent], state: TallyState) throws { try file.write(events: events, state: state) }
    func read() throws -> StoredJournal<TallyEvent, TallyState>? { try file.read() }
    func delete() throws { try file.delete() }
    func setAside(at instant: Date) throws -> URL { url.deletingLastPathComponent().appendingPathComponent(try file.setAside(at: instant)) }
}

/// A controller over the tally, so the fold at open is the runtime's, as
/// the session's is.
private struct TallyController: Controller {
    typealias Command = TallyEvent
    typealias Event = TallyEvent
    typealias State = TallyState
    typealias Presentation = TallyState

    let journal: Journal
    var broken = false

    struct Broken: Error {}

    var initialState: TallyState { TallyState() }
    func events(for command: TallyEvent, in context: Enrichment<TallyController>) -> [TallyEvent] { [command] }
    func apply(_ event: TallyEvent, to state: TallyState) throws -> TallyState {
        if broken { throw Broken() }
        var next = state
        next.total += event.load.value
        next.count += 1
        next.last = event.at
        next.target = SetTarget(plannedSet: "s\(next.count)", load: event.load, rpeMin: Decimal(string: "9.5"), rpeMax: Decimal(string: "9.5"))
        return next
    }
    func present(_ input: PresenterInput<TallyController>) -> TallyState { input.state }
    func journaling(of event: TallyEvent) -> Journaling { .append }
    func journal(in environment: AppEnvironment) -> (any JournalStore<TallyEvent, TallyState>)? { journal.file }
}

/// The state a runtime opens to over the journal, as the session's does at launch.
private func opened(_ journal: Journal, broken: Bool = false) async -> (state: TallyState, presentation: String?) {
    let harness = ControllerHarness(TallyController(journal: journal, broken: broken))
    let state = await harness.state
    let problem = harness.trace.compactMap { entry -> String? in if case let .setAside(_, place) = entry { place } else { nil } }.first
    return (state, problem)
}

private func events(_ count: Int) -> [TallyEvent] {
    var generator = SeededGenerator(seed: 3)
    return (0 ..< count).map { index in
        let at = Date(timeIntervalSince1970: 1_793_107_800 + Double(index * 97))
        return TallyEvent(id: EventID(UUIDv7.make(at: at, using: &generator)), at: at, load: .kg(Decimal(string: "83.9146")! + Decimal(index)))
    }
}

@Suite("Session journal")
struct JournalTests {
    @Test("a write reloads exactly: events, decimals and instants")
    func writeAndReload() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(5)
        // A sub-second instant too: the journal must not round what it stores.
        var odd = log
        odd[4].at = Date(timeIntervalSince1970: 1_793_108_200.123_456)
        try journal.write(events: odd, state: fold(odd))
        let contents = try #require(try journal.read())
        #expect(contents.events == odd)
        #expect(contents.cachedState == fold(odd))
        #expect(contents.cachedState?.total == Decimal(string: "429.573"))
    }

    @Test("each write replaces the file whole and leaves nothing beside it")
    func atomicReplace() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(20)
        for count in 1 ... log.count {
            let prefix = Array(log.prefix(count))
            try journal.write(events: prefix, state: fold(prefix))
            #expect(try journal.read()?.events.count == count)
        }
        #expect(directory.files() == ["active-session.json"])
    }

    @Test("a matching cache loads as it is")
    func loadMatching() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(3)
        try journal.write(events: log, state: fold(log))
        let before = try Data(contentsOf: journal.url)
        #expect(await opened(journal).state == fold(log))
        #expect(try journal.read()?.events == log)
        #expect(try Data(contentsOf: journal.url) == before)
    }

    @Test("a stale cache loses to the fold, and the file is rewritten with it")
    func refoldMismatch() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(4)
        var stale = fold(log)
        stale.count = 99
        try journal.write(events: log, state: stale)
        #expect(try journal.read()?.cachedState == stale)
        #expect(await opened(journal).state == fold(log))
        #expect(try journal.read()?.cachedState == fold(log))
    }

    @Test("a cache that no longer decodes counts as a mismatch; the events still load")
    func undecodableCache() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(2)
        try journal.write(events: log, state: fold(log))
        var file = try #require(try JSONValue(data: Data(contentsOf: journal.url)).objectValue)
        file["state"] = .object(["shape": .string("from another build")])
        try JSONValue.object(file).data().write(to: journal.url)
        let contents = try #require(try journal.read())
        #expect(contents.cachedState == nil)
        #expect(await opened(journal).state == fold(log))
        #expect(try journal.read()?.cachedState == fold(log))
    }

    /// `Fixtures/journal/active-session.json` is a session's journal as this
    /// build writes it (a start and one logged set, its instants as seconds
    /// since 2001), standing for the file on phones today.
    @Test("a journal a phone holds today reads, folds to its cached state and is written back as it was")
    func todaysFile() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let fixture = try #require(Bundle.module.url(forResource: "active-session", withExtension: "json", subdirectory: "Fixtures/journal"))
        let bytes = try Data(contentsOf: fixture)
        let url = directory.url.appendingPathComponent(SessionJournalFile.fileName)
        try bytes.write(to: url)
        let file = try SessionJournalFile(folder: RepositoryFolder(adopting: directory.url, writingOptions: .atomic))
        let stored = try #require(try file.read())
        #expect(stored.events.count == 2)
        let cached = try #require(stored.cachedState)
        let first = try #require(try JSONValue(data: bytes).objectValue?["events"]?.arrayValue?.first?.objectValue?["at"])
        #expect(first == .number(815_389_200))
        #expect(stored.events.first.flatMap { if case let .engine(envelope) = $0 { envelope.at } else { nil } }
            == Date(timeIntervalSinceReferenceDate: 815_389_200))
        // Written back, the events are the same text; the state's
        // dictionaries are keyed by values and written in hash order, so it
        // is held as what it decodes to.
        try file.write(events: stored.events, state: cached)
        let rewritten = try Data(contentsOf: url)
        #expect(try JSONValue(data: rewritten).objectValue?["events"] == JSONValue(data: bytes).objectValue?["events"])
        #expect(try file.read()?.cachedState == cached)
        // The session opens on it: the events fold to the state it caches.
        let rig = try SessionControllerTests.Rig(directory: directory.url, performsEffects: false)
        #expect(await rig.harness.state.session == cached.session)
    }

    @Test("the file carries format 1; another format is refused")
    func formatNumber() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(1)
        try journal.write(events: log, state: fold(log))
        var file = try #require(try JSONValue(data: Data(contentsOf: journal.url)).objectValue)
        #expect(SessionJournalFile.format == 1)
        #expect(file["format"] == .number(1))
        #expect(file.keys.sorted() == ["events", "format", "state"])
        file["format"] = .number(2)
        try JSONValue.object(file).data().write(to: journal.url)
        #expect(throws: JournalFormatMismatch(found: 2, expected: 1)) { try journal.read() }
    }

    @Test("no file means no session; delete removes it and may repeat")
    func absentAndDelete() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url.appendingPathComponent("not-yet"), writingOptions: .atomic)
        #expect(!journal.exists)
        #expect(try journal.read() == nil)
        #expect(await opened(journal).state == TallyState())
        let log = events(1)
        try journal.write(events: log, state: fold(log))
        #expect(journal.exists)
        try journal.delete()
        #expect(!journal.exists)
        try journal.delete()
    }

    @Test("a journal that does not load is set aside, so a new session cannot overwrite it")
    func setAside() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(2)
        try journal.write(events: log, state: fold(log))
        let before = try Data(contentsOf: journal.url)
        let aside = try journal.setAside(at: Date(timeIntervalSince1970: 1_793_110_588))
        #expect(aside.lastPathComponent == "active-session.unreadable-1793110588.json")
        #expect(!journal.exists)
        #expect(try Data(contentsOf: aside) == before)
        try journal.write(events: events(1), state: fold(events(1)))
        #expect(directory.files() == ["active-session.json", "active-session.unreadable-1793110588.json"])
    }

    @Test("a stale cache whose rewrite fails still loads the fold")
    func refoldWithoutWrite() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(3)
        try journal.write(events: log, state: fold(Array(log.prefix(2))))
        // A directory that takes no new file stands in for a full disk.
        try FileManager.default.setAttributes([.posixPermissions: 0o500], ofItemAtPath: directory.url.path)
        defer { try? FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.url.path) }
        #expect(await opened(journal).state == fold(log))
    }

    @Test("a fold that throws sets the file aside, so a new session cannot write over it")
    func foldThrows() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(2)
        try journal.write(events: log, state: fold(log))
        let before = try Data(contentsOf: journal.url)
        let open = await opened(journal, broken: true)
        #expect(open.state == TallyState())
        let place = try #require(open.presentation)
        #expect(place.hasPrefix("active-session.unreadable-"))
        #expect(!journal.exists)
        #expect(try Data(contentsOf: directory.url.appendingPathComponent(place)) == before)
    }
}
