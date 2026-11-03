import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateTesting
import Testing

@Suite("A repository's folder")
struct RepositoryFolderTests {
    let now = Date(timeIntervalSince1970: 1_793_696_400)

    @Test func aWriteIsWholeAndAReadOfNothingIsNil() throws {
        let folder = try RepositoryFolder(root: temporaryFolder())
        #expect(try folder.read("state.json", in: .cache) == nil)
        try folder.write(Data("one".utf8), to: "state.json", in: .cache)
        try folder.write(Data("two".utf8), to: "state.json", in: .cache)
        #expect(try folder.read("state.json", in: .cache) == Data("two".utf8))
        try folder.remove("state.json", in: .cache)
        try folder.remove("state.json", in: .cache)
        #expect(try folder.read("state.json", in: .cache) == nil)
    }

    @Test func aFileThatDoesNotDecodeIsSetAsideAndListed() throws {
        let folder = try RepositoryFolder(root: temporaryFolder())
        try folder.write(Data("not json".utf8), to: "drafts.json", in: .cache)
        let loaded = try folder.load([String: String].self, from: "drafts.json", in: .cache, at: now)
        #expect(loaded.value == nil)
        #expect(loaded.aside == SetAsideFile(area: .cache, name: "drafts.unreadable-1793696400.json", at: now))
        #expect(folder.setAside == [SetAsideFile(area: .cache, name: "drafts.unreadable-1793696400.json", at: now)])
        // A new file is written where it was; the old one is kept.
        try folder.write(Data("{}".utf8), to: "drafts.json", in: .cache)
        #expect(try folder.load([String: String].self, from: "drafts.json", in: .cache).value == [:])
        try folder.discard(folder.setAside[0])
        #expect(folder.setAside.isEmpty)
    }

    @Test func aFolderOfAnotherFormatIsSetAsideWholeAtOpen() throws {
        let root = temporaryFolder()
        try Data(#"{"format":0}"#.utf8).write(to: root.appendingPathComponent("folder.json"))
        try FileManager.default.createDirectory(at: root.appendingPathComponent("outbox"), withIntermediateDirectories: true)
        try Data("old".utf8).write(to: root.appendingPathComponent("outbox/outbox.json"))
        let folder = try RepositoryFolder(root: root, now: now)
        #expect(folder.setAsideAtOpen?.name == "folder.unreadable-1793696400")
        #expect(try folder.read("outbox.json", in: .outbox) == nil)
        #expect(folder.setAside.map(\.name) == ["folder.unreadable-1793696400"])
        // The next open finds its own format and sets nothing aside.
        #expect(try RepositoryFolder(root: root).setAsideAtOpen == nil)
    }

    @Test func eachAreaIsSizedAndACacheIsEvictedOldestFirst() throws {
        let folder = try RepositoryFolder(root: temporaryFolder())
        try folder.write(Data(count: 100), to: "outbox.json", in: .outbox)
        for (index, name) in ["a", "b", "c"].enumerated() {
            try folder.write(Data(count: 1000), to: name, in: .cache)
            folder.touch(name, in: .cache, at: now.addingTimeInterval(TimeInterval(index * 60)))
        }
        try folder.write(Data(count: 10), to: "stale", in: .cache)
        folder.touch("stale", in: .cache, at: now.addingTimeInterval(-90 * 86400))
        #expect(folder.usage() == [AreaUsage(area: .cache, bytes: 3010, files: 4), AreaUsage(area: .outbox, bytes: 100, files: 1)])
        let dropped = folder.evict(.cache, unusedSince: now.addingTimeInterval(-60 * 86400), budget: 2000)
        #expect(dropped == ["a", "stale"])
        #expect(folder.usage().first == AreaUsage(area: .cache, bytes: 2000, files: 2))
    }

    @Test func aClosedFolderRefusesEveryWriteAndCanGoWhole() throws {
        let root = temporaryFolder()
        let folder = try RepositoryFolder(root: root)
        folder.close(deleting: false)
        #expect(throws: FolderClosed.self) { try folder.write(Data(), to: "late.json", in: .cache) }
        folder.close(deleting: true)
        #expect(!FileManager.default.fileExists(atPath: root.path))
    }

    @Test func aRepositorysFolderIsNamedForItAndItsServer() {
        #expect(RepositoryFolder.name(server: URL(string: "https://substrate.example.com:8443")!, repository: "Ada.example.com")
            == "ada.example.com@substrate.example.com-8443")
    }

    @Test func theLinkGenerationIsKeptAndChecked() throws {
        let folder = try RepositoryFolder(root: temporaryFolder())
        let generation = LinkGeneration(folder: folder)
        #expect(generation.current == 0)
        let scheduled = generation.current
        try generation.check(scheduled)
        #expect(try generation.bump() == 1)
        // A job scheduled before the sign-out writes nothing, even in a new
        // process reading the folder afresh.
        #expect(throws: LinkGenerationMoved(scheduled: 0, current: 1)) {
            try LinkGeneration(folder: RepositoryFolder(root: folder.root)).check(scheduled)
        }
    }
}

@Suite("A journal in the folder")
struct JournalFileTests {
    @Test func theRuntimeReplaysWhatItWroteAcrossALaunch() async throws {
        let folder = try RepositoryFolder(root: temporaryFolder())
        let file = JournalFile<Training.Event, Training.State>(folder: folder, name: "training.json")
        let first = Runtime(Training(store: file, shelf: HistoryShelf()), environment: .testing())
        await first.send(.start)
        await first.send(.log("a"))
        await first.close()
        let second = Runtime(Training(store: file, shelf: HistoryShelf()), environment: .testing())
        #expect(await second.state.sets == ["a"])
        #expect(try file.read()?.events.count == 2)
    }

    @Test func aJournalOfAnotherFormatIsSetAsideByTheRuntime() async throws {
        let folder = try RepositoryFolder(root: temporaryFolder())
        try folder.write(Data(#"{"format":9,"events":[]}"#.utf8), to: "training.json", in: .journals)
        let file = JournalFile<Training.Event, Training.State>(folder: folder, name: "training.json")
        #expect(throws: JournalFormatMismatch(found: 9, expected: 1)) { try file.read() }
        let runtime = Runtime(Training(store: file, shelf: HistoryShelf()), environment: .testing())
        #expect(await runtime.state == Training.State())
        #expect(folder.setAside.map(\.area) == [.journals])
    }
}

@Suite("The record cache")
struct RecordCacheTests {
    func record(_ id: String, version: Int) -> Record {
        Record(id: id, kind: "example.com/tasks/task", properties: ["name": .string(id)], version: version)
    }

    @Test func aListFillsItAndTheWatchMarksWhatToReadAgain() async throws {
        let folder = try RepositoryFolder(root: temporaryFolder())
        let cache = RecordCache(folder: folder)
        await cache.listed(RecordPage(records: [record("a", version: 1), record("b", version: 1)], head: 40, generation: "g1"),
                           kinds: ["example.com/tasks/task"])
        #expect(await cache.cursor == WatchCursor(seq: 40, generation: "g1"))
        #expect(await cache.apply(WatchRow(seq: 41, kind: "example.com/tasks/task", recordID: "a", op: "patch")))
        #expect(!(await cache.apply(WatchRow(seq: 42, kind: "example.com/tasks/task", recordID: "b", op: "delete"))))
        #expect(await cache.stale == ["example.com/tasks/task/a"])
        #expect(await cache.records(of: "example.com/tasks/task").map(\.id) == ["a"])
        await cache.refreshed("example.com/tasks/task", "a", as: record("a", version: 2))
        #expect(await cache.stale.isEmpty)
        #expect(await cache.cursor == WatchCursor(seq: 42, generation: "g1"))

        // Kept across a launch.
        let again = RecordCache(folder: folder)
        #expect(await again.record("example.com/tasks/task", "a")?.version == 2)
        #expect(await again.cursor == WatchCursor(seq: 42, generation: "g1"))
    }

    @Test func aSecondReadUnderTheSameHistoryKeepsTheEarlierHead() async throws {
        let cache = RecordCache(folder: try RepositoryFolder(root: temporaryFolder()))
        await cache.listed(RecordPage(records: [], head: 40, generation: "g1"), kinds: ["example.com/tasks/list"])
        await cache.listed(RecordPage(records: [], head: 45, generation: "g1"), kinds: ["example.com/tasks/task"])
        #expect(await cache.cursor == WatchCursor(seq: 40, generation: "g1"))
        await cache.listed(RecordPage(records: [], head: 7, generation: "g2"), kinds: ["example.com/tasks/task"])
        #expect(await cache.cursor == WatchCursor(seq: 7, generation: "g2"))
        await cache.relist()
        #expect(await cache.cursor == nil)
    }
}

@Suite("A package the repository holds")
struct HeldPackageTests {
    func client(_ server: ScriptedServer) -> SubstrateClient {
        var client = SubstrateClient(server: URL(string: "http://localhost:8080")!, token: "t", transport: server)
        client.pause = { _ in }
        return client
    }

    let path = "/api/v1/substrate.reamde.dev/core/package/substrate.reamde.dev%2Ftasks"

    @Test func theVersionHeldIsThePublishersAndAMissingRowIsMissing() async throws {
        let server = ScriptedServer()
        server.answer("GET", path, 200,
                      #"{"id":"substrate.reamde.dev/tasks","kind":"substrate.reamde.dev/core/package","properties":{"shippedVersion":3,"version":5},"version":2}"#)
        #expect(try await HeldPackage.read("substrate.reamde.dev/tasks", needing: 3, from: client(server)) == .current(held: 3))
        #expect(try await HeldPackage.read("substrate.reamde.dev/tasks", needing: 4, from: client(server)) == .behind(held: 3))
        let empty = ScriptedServer()
        empty.answer("GET", path, 404, #"{"error":{"code":"not_found","message":"not found"}}"#)
        #expect(try await HeldPackage.read("substrate.reamde.dev/tasks", needing: 1, from: client(empty)) == .missing)
        #expect(!HeldPackage.behind(held: 1).writable && HeldPackage.current(held: 1).writable)
    }
}
