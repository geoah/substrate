import Foundation
import SubstrateKit
import Testing
import WorkoutsCore
@testable import WorkoutsSync

/// The account folder the last release left on the phone, its outbox and
/// catalogue cache spelled `~/training/…` (decision T3): read once in the
/// published spelling and written back at the moved format, and nothing the
/// old build knew about `~/training` taken as known about
/// `substrate.reamde.dev/training`, which may be empty or hold the copies
/// the owner made. `Fixtures/legacy-outbox.json` is an outbox as that build
/// queued a finished session: its create, its 20 sets, Finish under
/// `ifVersion`, and the versions and chains it knew.
/// `Fixtures/legacy-outbox-sent.json` is the same outbox after that build's
/// flush landed and it queued a new exercise and a routine edit, and
/// `Fixtures/legacy-history.json` is the session's history file as that
/// build wrote it at Finish.
@Suite("Upgrading an account folder from the ~/training spelling")
struct UpgradeTests {
    let codec = RecordCodec()
    let session = "01a16363-cf80-73ed-85fd-704f6ce38d73"

    func legacyOutbox(_ name: String = "legacy-outbox", in directory: URL) throws -> URL {
        let url = directory.appendingPathComponent(Outbox.fileName)
        try Fixtures.data(name).write(to: url)
        return url
    }

    /// The history store with the old build's file in it, read in the
    /// published spelling.
    func legacyHistory(in directory: URL) throws -> (store: HistoryStore, records: [SessionRecords]) {
        let store = HistoryStore(directory: directory, codec: codec, writingOptions: .atomic)
        try FileManager.default.createDirectory(at: store.directory, withIntermediateDirectories: true)
        try Fixtures.data("legacy-history").write(to: store.url(for: SessionID(rawValue: session)))
        let load = store.loadAll()
        #expect(load.unreadable.isEmpty, "\(load.unreadable)")
        return (store, load.entries.map(\.records))
    }

    func records(_ name: String) throws -> [FlatRecord] {
        try JSONDecoder().decode(WorkoutsAPI.Page.self, from: Fixtures.data(name)).records.map { try WorkoutsAPI.record($0) }
    }

    /// A server already holding what the owner copied by hand: the session,
    /// its sets, its routine and the exercise, each at version 1.
    func copiedServer() throws -> MemoryRecords {
        let memory = MemoryRecords()
        for record in try records("list-sessions") + records("list-sets") + records("routine-sample") + [WorkoutsAPI.record(try JSONDecoder()
                .decode(WorkoutsCore.JSONValue.self, from: Fixtures.data("get-exercise")))]
        {
            memory.put(record.kind, record.id, record.properties)
        }
        memory.put(codec.kinds.routine, "day-1-strength-foundation", ["name": .string("Day 1 – Strength Foundation")])
        return memory
    }

    func exchange(_ memory: MemoryRecords, local: [SessionRecords], outbox: Outbox) async throws -> HistoryExchange.Plan {
        let snapshot = try await ServerHistory(api: memory.api, codec: codec).snapshot()
        return HistoryExchange.plan(server: snapshot, local: local, queued: Set(await outbox.entries.compactMap(\.session)),
                                    versions: await outbox.versions, tombstones: [], codec: codec)
    }

    @Test("a queued session's writes, and every version and chain of the old package, are dropped; the file is rewritten once at format 2")
    func outbox() async throws {
        let directory = temporaryDirectory()
        let url = try legacyOutbox(in: directory)
        #expect(String(decoding: try Data(contentsOf: url), as: UTF8.self).contains("\"~/training/set\""))

        let outbox = Outbox(directory: directory, writingOptions: .atomic)
        #expect(await outbox.unreadable == nil)
        #expect(await outbox.entries.isEmpty)
        #expect(await outbox.versions.isEmpty)
        #expect(await outbox.version(of: codec.kinds.routine, "day-1-strength-foundation") == nil)

        let rewritten = try Data(contentsOf: url)
        let file = try #require(try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: rewritten).objectValue)
        #expect(file["format"] == .number(Decimal(Outbox.fileFormat)) && Outbox.fileFormat == 2)
        #expect(!String(decoding: rewritten, as: UTF8.self).contains("~/"))
        #expect(file["entries"] == .array([]) && file["versions"] == .object([:]) && file["chains"] == .object([:]))

        let again = Outbox(directory: directory, writingOptions: .atomic)
        #expect(await again.entries.isEmpty)
        #expect(try Data(contentsOf: url) == rewritten)
    }

    @Test("a sent outbox keeps its other writes in the published spelling without their guards, and knows nothing of the published package")
    func sentOutbox() async throws {
        let directory = temporaryDirectory()
        let url = try legacyOutbox("legacy-outbox-sent", in: directory)
        #expect(String(decoding: try Data(contentsOf: url), as: UTF8.self).contains("\"~/training/session/\(session)\""))

        let outbox = Outbox(directory: directory, writingOptions: .atomic)
        #expect(await outbox.unreadable == nil)
        let entries = await outbox.entries
        #expect(entries.count == 2)
        let exercise = try #require(entries.first)
        #expect(exercise.method == .put && exercise.kind == codec.kinds.exercise && exercise.recordID == "chest-press-machine")
        #expect(exercise.properties?["movement"] == .object(["ref": .string("\(codec.kinds.movement)/chest-press")]))
        let edit = try #require(entries.last)
        #expect(edit.method == .patch && edit.kind == codec.kinds.routine && edit.recordID == "day-1-strength-foundation")
        #expect(edit.guarded == false && edit.baseVersion == nil && edit.parked == false)
        #expect(edit.properties == ["notes": .string("Edited on the phone before the update")])
        #expect(await outbox.versions.isEmpty)
        #expect(await outbox.version(of: codec.kinds.session, session) == nil)

        let rewritten = try Data(contentsOf: url)
        let file = try #require(try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: rewritten).objectValue)
        #expect(file["format"] == .number(Decimal(Outbox.fileFormat)))
        #expect(!String(decoding: rewritten, as: UTF8.self).contains("~/"))
        #expect(file["versions"] == .object([:]) && file["chains"] == .object([:]))

        let again = Outbox(directory: directory, writingOptions: .atomic)
        #expect(await again.entries == entries)
        #expect(try Data(contentsOf: url) == rewritten)
    }

    @Test("against an empty published package the exchange deletes nothing from the phone and uploads the session; the other writes land or park on their own")
    func emptyServer() async throws {
        let directory = temporaryDirectory()
        _ = try legacyOutbox("legacy-outbox-sent", in: directory)
        let (store, local) = try legacyHistory(in: directory)
        #expect(local.map(\.id.rawValue) == [session])
        let memory = MemoryRecords()
        // As the write path answers a patch of nothing.
        memory.refuse = { [unowned memory] method, kind, id in method == "PATCH" && memory.get(kind, id) == nil ? 404 : nil }
        let outbox = Outbox(directory: directory, writingOptions: .atomic)

        let plan = try await exchange(memory, local: local, outbox: outbox)
        #expect(plan.removals.isEmpty && plan.purges.isEmpty && plan.imports.isEmpty && plan.merges.isEmpty && plan.finishes.isEmpty)
        #expect(plan.uploads.map(\.id.rawValue) == [session])
        for records in plan.uploads { try await outbox.enqueue(OutboxPlan.finished(records, codec: codec)) }

        guard case let .refused(words) = await outbox.flush(memory.api) else { Issue.record("not refused"); return }
        #expect(words.contains("refused"))
        // The routine edit met nothing to patch and waits for the owner; it holds no session back.
        #expect(await outbox.parked.map(\.recordID) == ["day-1-strength-foundation"])
        #expect(await outbox.entries.count == 1)
        #expect(memory.all(codec.kinds.session).map(\.id) == [session])
        #expect(memory.all(codec.kinds.set).count == 20)
        #expect(memory.get(codec.kinds.session, session)?.properties["status"] == .string("finished"))
        #expect(memory.get(codec.kinds.exercise, "chest-press-machine")?.version == 1)
        #expect(!memory.writes.contains { $0.hasPrefix("DELETE") })
        #expect(memory.writes.allSatisfy { !$0.contains("~") })
        #expect(store.loadAll().entries.map(\.id.rawValue) == [session])
        #expect(await outbox.version(of: codec.kinds.session, session) == 2)
        // The next exchange has nothing to do.
        let again = try await exchange(memory, local: local, outbox: outbox)
        #expect(again.isEmpty, "\(again)")
    }

    @Test("against a package the owner copied by hand the exchange takes the server's copy and sends nothing of it again; nothing is deleted on either side")
    func copiedServerKeepsEverything() async throws {
        let directory = temporaryDirectory()
        _ = try legacyOutbox("legacy-outbox-sent", in: directory)
        let (store, local) = try legacyHistory(in: directory)
        let memory = try copiedServer()
        let outbox = Outbox(directory: directory, writingOptions: .atomic)

        let plan = try await exchange(memory, local: local, outbox: outbox)
        #expect(plan.removals.isEmpty && plan.purges.isEmpty && plan.imports.isEmpty && plan.uploads.isEmpty && plan.finishes.isEmpty)
        #expect(plan.merges.map(\.id.rawValue) == [session])
        #expect(plan.seen.count == 21 && plan.seen.values.allSatisfy { $0 == 1 })
        try await outbox.saw(plan.seen)

        #expect(await outbox.flush(memory.api) == .sent)
        #expect(await outbox.entries.isEmpty)
        #expect(memory.writes == ["PUT /api/v1/\(codec.kinds.exercise)/chest-press-machine",
                                  "PATCH /api/v1/\(codec.kinds.routine)/day-1-strength-foundation"])
        let patch = try #require(memory.server.sent.last { $0.method == "PATCH" })
        #expect(patch.body?.objectValue?["ifVersion"] == nil)
        #expect(memory.get(codec.kinds.routine, "day-1-strength-foundation")?.properties["notes"] == .string("Edited on the phone before the update"))
        #expect(memory.get(codec.kinds.exercise, "chest-press-machine")?.version == 2)
        #expect(memory.all(codec.kinds.session).map(\.id) == [session])
        #expect(memory.all(codec.kinds.set).count == 20)
        #expect(store.loadAll().entries.map(\.id.rawValue) == [session])
        let again = try await exchange(memory, local: local, outbox: outbox)
        #expect(again.isEmpty, "\(again)")
    }

    @Test("a catalogue cache from the last release draws in the published spelling and is rewritten at format 2")
    func catalogCache() throws {
        let bundled = try BundledCatalog().loadNow()
        let routine = try #require(bundled.routines[SampleRoutine.id])
        let exercises = bundled.exercises.values.filter { routine.slots.map(\.exercise).contains($0.id) }
        let records = exercises.map(codec.encode) + [codec.encode(routine)]
        let plain = JSONEncoder()
        plain.outputFormatting = [.withoutEscapingSlashes]
        let text = String(decoding: try plain.encode(RecordFile(format: 1, records: records)), as: UTF8.self)
            .replacingOccurrences(of: KindNames.packageID + "/", with: "~/training/")
        #expect(text.contains("\"~/training/routine\"") && !text.contains(KindNames.packageID))
        let remote = RemoteCatalog(directory: temporaryDirectory(), codec: codec, writingOptions: .atomic)
        try FileManager.default.createDirectory(at: remote.cacheURL.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data(text.utf8).write(to: remote.cacheURL)

        let catalog = try remote.loadNow()
        #expect(catalog.unreadable.isEmpty, "\(catalog.unreadable)")
        #expect(catalog.routines[SampleRoutine.id] == routine)
        #expect(Set(catalog.exercises.keys) == Set(exercises.map(\.id)))
        let rewritten = try Data(contentsOf: remote.cacheURL)
        let file = try JSONDecoder().decode(RecordFile.self, from: rewritten)
        #expect(file.format == WorkoutsFormats.recordFileFormat && Set(file.records) == Set(records))
        #expect(try remote.loadNow().routines == catalog.routines)
        #expect(try Data(contentsOf: remote.cacheURL) == rewritten)
    }
}
