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
/// generation (decision 0195), the watch opens at its bookmark (the head, or
/// the cursor it resumes from under its generation and within the history
/// it keeps, else a 410) and gives every row after it before following, and
/// a record reads as it is now.
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
    private var following: [AsyncThrowingStream<String, any Error>.Continuation] = []
    private var kept: [Sent] = []
    private var afterList: [(id: String, title: String)] = []
    private var unreadable: Set<String> = []

    init(head: Int, generation: String, horizon: Int = 0, notes: [Record]) {
        self.head = head
        self.generation = generation
        self.horizon = horizon
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
            kept.append(Sent(path: url.path, query: Self.query(url)))
            if url.path == "/api/v1/records" {
                let page = #"{"records":[\#(notes.values.sorted { $0.id < $1.id }.map(Self.json).joined(separator: ","))]}"#
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
        let (lines, continuation) = AsyncThrowingStream<String, any Error>.makeStream()
        let status = lock.withLock { () -> Int in
            kept.append(Sent(path: url.path, query: query))
            let from = query["from"].flatMap(Int.init)
            if let asked = query["generation"], asked != generation { return 410 }
            if let from, from > head || from < horizon { return 410 }
            let start = from ?? head
            continuation.yield(#"{"bookmark":\#(start),"generation":"\#(generation)"}"#)
            for row in rows where row.seq > start { continuation.yield(Self.line(row)) }
            following.append(continuation)
            return 200
        }
        if status != 200 {
            continuation.yield(#"{"error":{"code":"compacted","message":"the history was replaced; re-list and resume from the head"}}"#)
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

/// The notebook over `cache`, speaking to `server`.
private func notebook(_ cache: RecordCache, _ server: HistoryServer) -> ControllerHarness<Notebook> {
    var client = SubstrateClient(server: URL(string: "https://ada.example.com")!, token: "substrate_tok_test", transport: server)
    client.pause = { _ in }
    let source = client
    let words = StatusWords(app: "Notes", device: "this iPhone", kept: "notes")
    return ControllerHarness(
        Notebook(linked: LinkedRecords(kinds: [noteKind], cache: cache, outbox: nil, words: words)),
        environment: .testing(transport: server, client: ClientSource { source }))
}

private func title(_ harness: ControllerHarness<Notebook>, _ id: String) async -> JSONValue? {
    await harness.state.linked.records[RecordFold.key(noteKind, id)]?.properties["title"]
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
}
