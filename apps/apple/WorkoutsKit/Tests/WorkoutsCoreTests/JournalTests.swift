import Foundation
@testable import WorkoutsCore
import Testing

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

private typealias Journal = SessionJournal<TallyEvent, TallyState>

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
    func loadMatching() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(3)
        try journal.write(events: log, state: fold(log))
        let before = try Data(contentsOf: journal.url)
        let loaded = try #require(try journal.load(fold: fold))
        #expect(loaded.cacheMatched)
        #expect(loaded.state == fold(log))
        #expect(loaded.events == log)
        #expect(try Data(contentsOf: journal.url) == before)
    }

    @Test("a stale cache loses to the fold, and the file is rewritten with it")
    func refoldMismatch() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(4)
        var stale = fold(log)
        stale.count = 99
        try journal.write(events: log, state: stale)
        let loaded = try #require(try journal.load(fold: fold))
        #expect(!loaded.cacheMatched)
        #expect(loaded.cachedState == stale)
        #expect(loaded.state == fold(log))
        #expect(try journal.read()?.cachedState == fold(log))
    }

    @Test("a cache that no longer decodes counts as a mismatch; the events still load")
    func undecodableCache() throws {
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
        let loaded = try #require(try journal.load(fold: fold))
        #expect(!loaded.cacheMatched)
        #expect(loaded.state == fold(log))
        #expect(try journal.read()?.cachedState == fold(log))
    }

    @Test("the file carries format 1; another format is refused")
    func formatNumber() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(1)
        try journal.write(events: log, state: fold(log))
        var file = try #require(try JSONValue(data: Data(contentsOf: journal.url)).objectValue)
        #expect(WorkoutsFormats.journalFormat == 1)
        #expect(file["format"] == .number(1))
        #expect(file.keys.sorted() == ["events", "format", "state"])
        file["format"] = .number(2)
        try JSONValue.object(file).data().write(to: journal.url)
        #expect(throws: JournalError.unsupportedFormat(2)) { try journal.read() }
    }

    @Test("no file means no session; delete removes it and may repeat")
    func absentAndDelete() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url.appendingPathComponent("not-yet"), writingOptions: .atomic)
        #expect(!journal.exists)
        #expect(try journal.read() == nil)
        #expect(try journal.load(fold: fold) == nil)
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
    func refoldWithoutWrite() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(3)
        try journal.write(events: log, state: fold(Array(log.prefix(2))))
        // A directory that takes no new file stands in for a full disk.
        try FileManager.default.setAttributes([.posixPermissions: 0o500], ofItemAtPath: directory.url.path)
        defer { try? FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.url.path) }
        let loaded = try #require(try journal.load(fold: fold))
        #expect(loaded.state == fold(log))
        #expect(!loaded.cacheMatched)
    }

    @Test("a fold that throws reaches the caller and leaves the file alone")
    func foldThrows() throws {
        struct Broken: Error {}
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let journal = Journal(directory: directory.url, writingOptions: .atomic)
        let log = events(2)
        try journal.write(events: log, state: fold(log))
        let before = try Data(contentsOf: journal.url)
        #expect(throws: Broken.self) { try journal.load { _ in throw Broken() } }
        #expect(try Data(contentsOf: journal.url) == before)
    }
}
