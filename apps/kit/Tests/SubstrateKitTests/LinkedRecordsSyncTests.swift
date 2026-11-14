import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

private let noteKind = "example.com/notes/note"

private func note(_ id: String, _ title: String, version: Int = 1) -> Record {
    Record(id: id, kind: noteKind, properties: ["title": .string(title)], version: version)
}

private let online = LinkSnapshot(
    phase: .signedIn, server: URL(string: "https://ada.example.com")!, repository: "ada.example.com",
    scopes: ["write:example.com/notes/*"], online: true)

/// A server as an app's token sees it: a list carries no head and no
/// generation (decision 0195) and pages by `pageSize`, the watch opens at
/// its bookmark (the head, or the cursor it resumes from under its
/// generation and within the history it keeps, else a 410) and gives every
/// row after it before following, and a record reads as it is now.
private final class HistoryServer: HTTPTransport, @unchecked Sendable {
    struct Sent: Sendable {
        var path: String
        var query: [String: String]
    }

    private let lock = NSLock()
    private var notes: [String: Record] = [:]
    private var rows: [WatchRow] = []
    private var head: Int
    private let generation: String
    /// The oldest seq a watch resumes from.
    private let horizon: Int
    private let pageSize: Int
    /// How many list continuations are answered 410 before one is resumed.
    private var compactions = 0
    private var following: [AsyncThrowingStream<String, any Error>.Continuation] = []
    private var kept: [Sent] = []
    private var afterList: [(id: String, title: String)] = []
    private var unreadable: Set<String> = []
    /// How many watches are answered 410 whatever they resume from.
    private var watchRefusals = 0
    /// A watch with no `from` waits, unanswered, while this holds.
    private var holdingFresh = false
    /// Every watch, once open at its bookmark, ends with a terminal
    /// `compacted` frame.
    private var endsCompacted = false

    init(head: Int, generation: String, horizon: Int = 0, pageSize: Int = .max, notes: [Record]) {
        self.head = head
        self.generation = generation
        self.horizon = horizon
        self.pageSize = pageSize
        for record in notes { self.notes[record.id] = record }
    }

    var sent: [Sent] { lock.withLock { kept } }

    var watches: [Sent] { sent.filter { $0.path == "/api/v1/changes" } }

    /// A note retitled now: its version moves on and the history gains a row.
    func write(_ id: String, title: String) {
        lock.withLock { land(id, title) }
    }

    /// A note retitled the moment the next list has been answered.
    func writeAfterNextList(_ id: String, title: String) {
        lock.withLock { afterList.append((id, title)) }
    }

    /// The next `count` list continuations are refused as the server
    /// refuses one it can no longer resume.
    func compactContinuations(_ count: Int) {
        lock.withLock { compactions = count }
    }

    /// The next `count` watches are refused as from a history replaced.
    func refuseWatches(_ count: Int) {
        lock.withLock { watchRefusals = count }
    }

    /// Whether a watch opened from the live head waits unanswered.
    func holdFreshWatches(_ holding: Bool) {
        lock.withLock { holdingFresh = holding }
    }

    /// Every watch from now on opens at its bookmark, then is refused as
    /// `compacted`.
    func endEveryWatchCompacted() {
        lock.withLock { endsCompacted = true }
    }

    /// Every watch open now is refused as `compacted`, with its terminal
    /// frame, and closes.
    func compactOpenWatches() {
        lock.withLock {
            for continuation in following {
                continuation.yield(Self.compacted)
                continuation.finish()
            }
            following = []
        }
    }

    /// Whether reading the note fails as a dropped connection does.
    func failReads(of id: String, _ failing: Bool) {
        lock.withLock {
            if failing { unreadable.insert(id) } else { unreadable.remove(id) }
        }
    }

    private func land(_ id: String, _ title: String) {
        head += 1
        var record = notes[id] ?? note(id, title, version: 0)
        record.properties["title"] = .string(title)
        record.version += 1
        notes[id] = record
        let row = WatchRow(seq: head, kind: noteKind, recordID: id, op: "patch")
        rows.append(row)
        for continuation in following { continuation.yield(Self.line(row)) }
    }

    private static let compacted = #"{"error":{"code":"compacted","message":"the history was replaced; re-list and resume from the head"}}"#

    private static func line(_ row: WatchRow) -> String {
        #"{"seq":\#(row.seq),"ts":"2026-11-04T09:00:00Z","kind":"\#(row.kind)","recordId":"\#(row.recordID)","op":"\#(row.op)"}"#
    }

    private static func json(_ record: Record) -> String {
        String(decoding: try! JSONEncoder().encode(record), as: UTF8.self)
    }

    private static func query(_ url: URL) -> [String: String] {
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        return Dictionary(items.map { ($0.name, $0.value ?? "") }) { first, _ in first }
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let url = request.url!
        let answer = lock.withLock { () -> (Int, String)? in
            let query = Self.query(url)
            kept.append(Sent(path: url.path, query: query))
            if url.path == "/api/v1/records" {
                if query["after"] != nil, compactions > 0 {
                    compactions -= 1
                    return (410, #"{"error":{"code":"compacted","message":"the cursor's history was replaced: list again from the first page"}}"#)
                }
                let sorted = notes.values.sorted { $0.id < $1.id }
                let start = query["after"].flatMap(Int.init) ?? 0
                let end = start + min(pageSize, sorted.count - start)
                let cursor = end < sorted.count ? #""\#(end)""# : "null"
                let page = #"{"records":[\#(sorted[start ..< end].map(Self.json).joined(separator: ","))],"cursor":\#(cursor)}"#
                for write in afterList { land(write.id, write.title) }
                afterList = []
                return (200, page)
            }
            let id = url.lastPathComponent
            guard url.path == "/api/v1/\(noteKind)/\(id)" else { return (404, #"{"error":{"code":"not_found","message":"no route"}}"#) }
            if unreadable.contains(id) { return nil }
            return notes[id].map { (200, Self.json($0)) } ?? (404, #"{"error":{"code":"not_found","message":"not found"}}"#)
        }
        guard let (status, body) = answer else { throw URLError(.networkConnectionLost) }
        let head = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
        return (Data(body.utf8), head)
    }

    func lines(for request: URLRequest) async throws -> LineStream {
        let url = request.url!
        let query = Self.query(url)
        lock.withLock { kept.append(Sent(path: url.path, query: query)) }
        while query["from"] == nil, lock.withLock({ holdingFresh }), !Task.isCancelled {
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
        let (lines, continuation) = AsyncThrowingStream<String, any Error>.makeStream()
        let status = lock.withLock { () -> Int in
            let from = query["from"].flatMap(Int.init)
            if watchRefusals > 0 {
                watchRefusals -= 1
                return 410
            }
            if let asked = query["generation"], asked != generation { return 410 }
            if let from, from > head || from < horizon { return 410 }
            let start = from ?? head
            continuation.yield(#"{"bookmark":\#(start),"generation":"\#(generation)"}"#)
            for row in rows where row.seq > start { continuation.yield(Self.line(row)) }
            if endsCompacted {
                continuation.yield(Self.compacted)
                continuation.finish()
            } else {
                following.append(continuation)
            }
            return 200
        }
        if status != 200 {
            continuation.yield(Self.compacted)
            continuation.finish()
        }
        let head = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/x-ndjson"])!
        return LineStream(head: head, lines: lines)
    }
}

/// An app whose records are notes, with nothing of its own.
private struct Notebook: LinkedRecordsApp {
    typealias Command = Never

    enum Event: Codable, Equatable, Sendable, LinkedRecordsEvent {
        case records(LinkedRecords.Event)

        var recordsEvent: LinkedRecords.Event? {
            switch self {
            case let .records(event): event
            }
        }
    }

    enum Effect: Equatable, Sendable, LinkedRecordsEffect {
        case records(LinkedRecords.Effect)

        var recordsEffect: LinkedRecords.Effect? {
            switch self {
            case let .records(effect): effect
            }
        }
    }

    struct State: Codable, Equatable, Sendable, LinkedRecordsHolding {
        var linked = LinkedRecordsState()
    }

    typealias Presentation = State

    let linked: LinkedRecords

    var initialState: State { State() }

    func events(for command: Never, in context: Enrichment<Notebook>) -> [Event] {}

    func present(_ input: PresenterInput<Notebook>) -> State { input.state }
}

private let words = StatusWords(app: "Notes", device: "this iPhone", kept: "notes")

/// The notebook over `cache`, speaking to `server`, on the harness's clock.
private func notebook(_ cache: RecordCache, _ server: HistoryServer) -> ControllerHarness<Notebook> {
    var client = SubstrateClient(server: URL(string: "https://ada.example.com")!, token: "substrate_tok_test", transport: server)
    client.pause = { _ in }
    let source = client
    let clock = TestClock()
    return ControllerHarness(
        Notebook(linked: LinkedRecords(kinds: [noteKind], cache: cache, outbox: nil, words: words)), clock: clock,
        environment: .testing(clock: clock, transport: server, client: ClientSource { source }))
}

private func stopped(_ phase: ReadPhase) -> Bool {
    if case .stopped = phase { return true }
    return false
}

private func title(_ harness: ControllerHarness<Notebook>, _ id: String) async -> JSONValue? {
    await harness.state.linked.records[RecordFold.key(noteKind, id)]?.properties["title"]
}

/// A notebook whose saved cursor fell below what the server keeps, refused
/// at the clock's start and read again; then, in order, the read again's
/// watch reached its bookmark, its list landed, the watch opened again at
/// that bookmark, and it delivered a change, read again. The watch then
/// waits for `compactOpenWatches`. Answers when the read again started.
private func readAgainAndFollowing(_ cache: RecordCache, _ server: HistoryServer) async throws -> (ControllerHarness<Notebook>, Date) {
    await cache.listed(RecordPage(records: [note("n1", "Milk")], head: 7, generation: "g1"), kinds: [noteKind])
    let harness = notebook(cache, server)
    let start = harness.clock.now
    await harness.runtime.linkChanged(online)
    try await harness.until(timeout: .seconds(20)) { harness in
        let linked = await harness.state.linked
        return stopped(linked.phase) || (linked.phase == .following && linked.records.count == 2 && server.watches.count == 3)
    }
    #expect(await harness.state.linked.phase == .following)
    server.write("n1", title: "Oat milk")
    try await harness.until(timeout: .seconds(20)) { await title($0, "n1") == "Oat milk" }
    #expect(await harness.state.linked.recoveryStarted == start)
    #expect(await cache.cursor == WatchCursor(seq: 41, generation: "g1"))
    // The saved cursor refused; the read again's watch, which lists only
    // after its bookmark; its list; the watch again from that bookmark; the
    // note its row named, read again.
    #expect(
        server.sent.map { "\($0.path) \($0.query["from"] ?? "-")" } == [
            "/api/v1/changes 7", "/api/v1/changes -", "/api/v1/records -", "/api/v1/changes 40", "/api/v1/\(noteKind)/n1 -",
        ])
    return (harness, start)
}

@Suite("Linked records against a server's history", .timeLimit(.minutes(10)))
struct LinkedRecordsSyncTests {
    /// An app's list carries no head, so the watch it follows from must be
    /// open before the list is read: a note retitled between the list's
    /// answer and the watch's opening is otherwise never read again.
    @Test func aWriteBetweenTheListAndTheWatchIsDelivered() async throws {
        let cache = RecordCache(folder: try RepositoryFolder(root: temporaryFolder()))
        let server = HistoryServer(head: 7, generation: "g1", notes: [note("n1", "Milk")])
        server.writeAfterNextList("n1", title: "Oat milk")
        let harness = notebook(cache, server)
        await harness.runtime.linkChanged(online)
        try await harness.until(timeout: .seconds(20)) { await title($0, "n1") == "Oat milk" }
        #expect(await harness.state.linked.phase == .following)
        #expect(await cache.record(noteKind, "n1")?.version == 2)
        #expect(await cache.cursor == WatchCursor(seq: 8, generation: "g1"))
        // The watch was opened before the list was read, at the head.
        let first = try #require(server.sent.first)
        #expect(first.path == "/api/v1/changes" && first.query["from"] == nil)
    }

    /// A cursor the server no longer holds (the history was replaced) is
    /// dropped before the read, so the next watch opens at the new head
    /// rather than at the cursor answered 410 again.
    @Test func aCursorFromAReplacedHistoryIsDroppedAndTheReadFollowsTheNewOne() async throws {
        let cache = RecordCache(folder: try RepositoryFolder(root: temporaryFolder()))
        await cache.listed(RecordPage(records: [note("n1", "Milk")], head: 7, generation: "g1"), kinds: [noteKind])
        let server = HistoryServer(head: 3, generation: "g2", notes: [note("n1", "Milk"), note("n2", "Bread")])
        let harness = notebook(cache, server)
        await harness.runtime.linkChanged(online)
        let current = WatchCursor(seq: 3, generation: "g2")
        try await harness.until(timeout: .seconds(20)) { harness in
            let linked = await harness.state.linked
            return linked.cursor == current && linked.phase == .following && linked.records.count == 2
        }
        #expect(await cache.cursor == current)
        // Answered 410 once, from the old history, and never asked again.
        #expect(server.watches.filter { $0.query["generation"] == "g1" }.count == 1)
        #expect(server.watches.contains { $0.query["generation"] == nil && $0.query["from"] == nil })
    }

    /// The same history, with the cursor below what it keeps: the read's
    /// bookmark is past the cursor, so only dropping it keeps the watch from
    /// resuming there again.
    @Test func aCursorPastTheHistoryKeptIsDroppedAndTheReadFollowsTheHead() async throws {
        let cache = RecordCache(folder: try RepositoryFolder(root: temporaryFolder()))
        await cache.listed(RecordPage(records: [note("n1", "Milk")], head: 7, generation: "g1"), kinds: [noteKind])
        let server = HistoryServer(head: 40, generation: "g1", horizon: 20, notes: [note("n1", "Milk"), note("n2", "Bread")])
        let harness = notebook(cache, server)
        await harness.runtime.linkChanged(online)
        let head = WatchCursor(seq: 40, generation: "g1")
        try await harness.until(timeout: .seconds(20)) { harness in
            let linked = await harness.state.linked
            return linked.cursor == head && linked.phase == .following && linked.records.count == 2
        }
        #expect(await cache.cursor == head)
        #expect(server.watches.filter { $0.query["from"] == "7" }.count == 1)
    }

    /// A list continuation the server can no longer resume reads the notes
    /// again from the first page, and they settle with nothing said: no
    /// note is missing, and the read is not stopped.
    @Test func aListToldCompactedPartWayReadsAgainAndSaysNothing() async throws {
        let cache = RecordCache(folder: try RepositoryFolder(root: temporaryFolder()))
        let notes = [note("n1", "Milk"), note("n2", "Bread"), note("n3", "Eggs")]
        let server = HistoryServer(head: 7, generation: "g1", pageSize: 2, notes: notes)
        server.compactContinuations(1)
        let harness = notebook(cache, server)
        await harness.runtime.linkChanged(online)
        try await harness.until(timeout: .seconds(20)) { harness in
            let phase = await harness.state.linked.phase
            if case .stopped = phase { return true }
            return phase == .following
        }
        let linked = await harness.state.linked
        #expect(linked.phase == .following)
        #expect(LinkedRecords(kinds: [noteKind], cache: cache, outbox: nil, words: words).notice(linked) == nil)
        #expect(linked.records.keys.sorted() == notes.map { RecordFold.key(noteKind, $0.id) })
        #expect(await cache.records(of: noteKind).map(\.id) == ["n1", "n2", "n3"])
        // Refused once part way, then read whole from the first page.
        #expect(server.sent.filter { $0.path == "/api/v1/records" }.map { $0.query["after"] } == [nil, "2", nil, "2"])
    }

    /// One recovery a minute: a watch that opens at its bookmark and is then
    /// refused as `compacted`, again and again, is read again once, and the
    /// second refusal, within the minute, stops it with the failure said.
    /// The bookmark between them makes nothing whole again.
    @Test func aWatchRefusedAfterEveryBookmarkIsReadAgainOnceAMinute() async throws {
        let cache = RecordCache(folder: try RepositoryFolder(root: temporaryFolder()))
        let server = HistoryServer(head: 7, generation: "g1", notes: [note("n1", "Milk")])
        server.endEveryWatchCompacted()
        let harness = notebook(cache, server)
        await harness.runtime.linkChanged(online)
        let lists: @Sendable () -> Int = { server.sent.filter { $0.path == "/api/v1/records" }.count }
        try await harness.until(timeout: .seconds(20)) { harness in
            await stopped(harness.state.linked.phase) || lists() > 2
        }
        let linked = await harness.state.linked
        // The first read, and the one read again; never a third.
        #expect(lists() == 2)
        #expect(
            LinkedRecords(kinds: [noteKind], cache: cache, outbox: nil, words: words).notice(linked)?.text
                == "Your notes are not being updated: the server could not resume their history even after reading them again")
        #expect(linked.cursor == nil)
        #expect(await cache.cursor == nil)
        #expect(linked.records.count == 1)
    }

    /// The refusal's instant is the app's clock: an hour after a read again,
    /// a refusal starts another, though the watch never opened between.
    @Test func aWatchRefusedAnHourLaterOnTheAppsClockIsReadAgain() async throws {
        let cache = RecordCache(folder: try RepositoryFolder(root: temporaryFolder()))
        await cache.listed(RecordPage(records: [note("n1", "Milk")], head: 7, generation: "g1"), kinds: [noteKind])
        let server = HistoryServer(head: 40, generation: "g1", horizon: 20, notes: [note("n1", "Milk"), note("n2", "Bread")])
        // The read again's watch waits while the hour passes.
        server.holdFreshWatches(true)
        let harness = notebook(cache, server)
        await harness.runtime.linkChanged(online)
        try await harness.until(timeout: .seconds(20)) { _ in server.watches.count == 2 }
        #expect(server.watches.first?.query["from"] == "7")
        await harness.advance(by: 3600)
        server.refuseWatches(1)
        server.holdFreshWatches(false)
        let head = WatchCursor(seq: 40, generation: "g1")
        try await harness.until(timeout: .seconds(20)) { harness in
            let linked = await harness.state.linked
            return stopped(linked.phase) || (linked.phase == .following && linked.cursor == head && server.watches.count == 4)
        }
        let linked = await harness.state.linked
        #expect(linked.phase == .following)
        #expect(linked.records.count == 2)
        // The saved cursor refused, the read again's watch refused an hour
        // later, then the read again that followed.
        #expect(server.watches.map { $0.query["from"] } == [Optional("7"), nil, nil, "40"])
    }

    /// The minute to the millisecond: a refusal 59.999 seconds after the
    /// read again started is the failure, and nothing is read again,
    /// though the read again's bookmark, its list and a change the watch
    /// delivered came between.
    @Test func aWatchRefusedAMillisecondShortOfTheMinuteStopsWhateverCameBetween() async throws {
        let cache = RecordCache(folder: try RepositoryFolder(root: temporaryFolder()))
        let server = HistoryServer(head: 40, generation: "g1", horizon: 20, notes: [note("n1", "Milk"), note("n2", "Bread")])
        let (harness, started) = try await readAgainAndFollowing(cache, server)
        let lists: @Sendable () -> Int = { server.sent.filter { $0.path == "/api/v1/records" }.count }
        harness.clock.advance(to: started.addingTimeInterval(59.999))
        server.compactOpenWatches()
        try await harness.until(timeout: .seconds(20)) { harness in
            await stopped(harness.state.linked.phase) || lists() > 1
        }
        let linked = await harness.state.linked
        #expect(linked.phase == .stopped(.refused, "the server could not resume their history even after reading them again"))
        #expect(lists() == 1)
        #expect(server.watches.count == 3)
        #expect(linked.recoveryStarted == started)
        #expect(linked.cursor == nil)
        #expect(await cache.cursor == nil)
    }

    /// And on the minute exactly, a refusal reads the notes again and
    /// follows the head that read gives.
    @Test func aWatchRefusedOnTheMinuteIsReadAgainWhateverCameBetween() async throws {
        let cache = RecordCache(folder: try RepositoryFolder(root: temporaryFolder()))
        let server = HistoryServer(head: 40, generation: "g1", horizon: 20, notes: [note("n1", "Milk"), note("n2", "Bread")])
        let (harness, started) = try await readAgainAndFollowing(cache, server)
        let lists: @Sendable () -> Int = { server.sent.filter { $0.path == "/api/v1/records" }.count }
        harness.clock.advance(to: started.addingTimeInterval(60))
        server.compactOpenWatches()
        let head = WatchCursor(seq: 41, generation: "g1")
        try await harness.until(timeout: .seconds(20)) { harness in
            let linked = await harness.state.linked
            return stopped(linked.phase) || (lists() == 2 && linked.phase == .following && linked.cursor == head && server.watches.count == 5)
        }
        let linked = await harness.state.linked
        #expect(linked.phase == .following)
        #expect(lists() == 2)
        #expect(linked.recoveryStarted == started.addingTimeInterval(60))
        #expect(linked.records.count == 2)
        #expect(server.watches.map { $0.query["from"] } == [Optional("7"), nil, "40", nil, "41"])
    }

    /// A note whose read again failed is kept as waiting for one, on the
    /// device too, and is read once the app opens again and connects.
    @Test func aReadAgainThatFailedIsTriedAfterTheAppOpensAgain() async throws {
        let folder = try RepositoryFolder(root: temporaryFolder())
        let first = RecordCache(folder: folder)
        await first.listed(RecordPage(records: [note("n1", "Milk")], head: 7, generation: "g1"), kinds: [noteKind])
        let server = HistoryServer(head: 7, generation: "g1", notes: [note("n1", "Milk")])
        server.failReads(of: "n1", true)
        let key = RecordFold.key(noteKind, "n1")

        let before = notebook(first, server)
        await before.runtime.linkChanged(online)
        try await before.until(timeout: .seconds(20)) { await $0.state.linked.phase == .following }
        server.write("n1", title: "Oat milk")
        try await before.until(timeout: .seconds(20)) { await $0.state.linked.stale == [key] }
        #expect(await first.stale == [key])
        await before.runtime.close()

        server.failReads(of: "n1", false)
        let second = RecordCache(folder: folder)
        #expect(await second.stale == [key])
        let after = notebook(second, server)
        await after.runtime.linkChanged(online)
        try await after.until(timeout: .seconds(20)) { await title($0, "n1") == "Oat milk" }
        #expect(await after.state.linked.stale.isEmpty)
        #expect(await second.stale.isEmpty)
        #expect(await second.record(noteKind, "n1")?.version == 2)
    }
}
