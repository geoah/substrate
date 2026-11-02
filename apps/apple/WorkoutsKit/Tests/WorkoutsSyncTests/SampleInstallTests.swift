import Foundation
import SubstrateKit
import Testing
import WorkoutsCore
@testable import WorkoutsSync

/// The sample routine on a server (decision S1): written once, into a
/// repository with no routines, and never again once the owner deletes it;
/// a routine's delete goes through the outbox and leaves the cache at once.
@Suite("Sample routine and routine deletes")
struct SampleInstallTests {
    let codec = RecordCodec()

    /// A repository holding `records` (in the phone's spelling): a list
    /// answers the ones of the kinds it names, and every write is taken.
    final class Repository: @unchecked Sendable {
        let server = ScriptedServer()
        private let lock = NSLock()
        private var held: [FlatRecord]
        let rehome = Rehome(authority: "ada.example.com")

        init(_ records: [FlatRecord] = []) {
            held = records
            server.on { [unowned self] sent in
                guard sent.method == "GET", sent.path == "/api/v1/records" else { return nil }
                let query = sent.query ?? ""
                let records = lock.withLock { held }.filter { query.contains(rehome.remoteKind($0.kind)) }
                return (200, page(records))
            }
            server.on { $0.method == "PUT" ? (200, record(1)) : nil }
            server.on { $0.method == "DELETE" ? (200, "{}") : nil }
        }

        func hold(_ records: [FlatRecord]) { lock.withLock { held = records } }

        private func page(_ records: [FlatRecord]) -> String {
            let items: [WorkoutsCore.JSONValue] = records.map { record in
                .object(["kind": .string(rehome.remoteKind(record.kind)), "id": .string(record.id),
                         "properties": rehome.outbound(.object(record.properties)), "version": .number(1)])
            }
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.withoutEscapingSlashes]
            let data = try! encoder.encode(WorkoutsCore.JSONValue.object(["records": .array(items)]))
            return String(decoding: data, as: UTF8.self)
        }

        var puts: [String] { server.methodsAndPaths.filter { $0.hasPrefix("PUT ") } }
    }

    func bundled() throws -> Catalog { try BundledCatalog().loadNow() }

    func sampleRecords() throws -> [FlatRecord] {
        let catalog = try bundled()
        return catalog.exercises.values.sorted { $0.id < $1.id }.map(codec.encode)
            + [codec.encode(try #require(catalog.routines[SampleRoutine.id]))]
    }

    @Test("an empty repository gets the sample and its exercises once, exercises first, and a relaunch keeps it so")
    func firstRunOnce() async throws {
        let directory = temporaryDirectory()
        let repository = Repository()
        let api = api(repository.server)
        let remote = RemoteCatalog(directory: directory, writingOptions: .atomic)
        let sample = SampleInstall(directory: directory, writingOptions: .atomic)
        #expect(sample.state == nil)

        #expect(try await sample.firstRun(api, remote: remote, bundled: try bundled()))
        #expect(sample.state == .installed)
        let puts = repository.puts
        #expect(puts.count == 10)
        #expect(puts.dropLast().allSatisfy { $0.contains("/training/exercise/") })
        #expect(puts.last == "PUT /api/v1/ada.example.com/training/routine/\(SampleRoutine.id.rawValue)")

        // The next sync, and the next launch, write nothing more.
        repository.hold([])
        #expect(try await !sample.firstRun(api, remote: remote, bundled: try bundled()))
        let relaunched = SampleInstall(directory: directory, writingOptions: .atomic)
        #expect(relaunched.state == .installed)
        #expect(try await !relaunched.firstRun(api, remote: remote, bundled: try bundled()))
        #expect(repository.puts.count == 10)
    }

    @Test("a repository with routines of its own gets no sample, now or after they are gone")
    func routinesOfItsOwn() async throws {
        let directory = temporaryDirectory()
        let own = FlatRecord(kind: codec.kinds.routine, id: "push-day", properties: ["name": .string("Push day")])
        let repository = Repository([own])
        let api = api(repository.server)
        let remote = RemoteCatalog(directory: directory, writingOptions: .atomic)
        let sample = SampleInstall(directory: directory, writingOptions: .atomic)
        #expect(try await !sample.firstRun(api, remote: remote, bundled: try bundled()))
        #expect(sample.state == .skipped)
        repository.hold([])
        #expect(try await !sample.firstRun(api, remote: remote, bundled: try bundled()))
        #expect(repository.puts.isEmpty)
        // Nothing to offer back: the sample was never there.
        #expect(!sample.offersSample(Catalog()))
    }

    @Test("a failed write keeps nothing, so the next sync tries again; ids already there are left alone")
    func failedWrite() async throws {
        let directory = temporaryDirectory()
        let server = ScriptedServer()
        server.answer("GET", "/api/v1/records", 200, #"{"records":[]}"#)
        server.on { $0.method == "PUT" ? (-1, "") : nil }
        let remote = RemoteCatalog(directory: directory, writingOptions: .atomic)
        let sample = SampleInstall(directory: directory, writingOptions: .atomic)
        await #expect(throws: (any Error).self) { try await sample.firstRun(api(server), remote: remote, bundled: try bundled()) }
        #expect(sample.state == nil)

        // The exercises landed before the routine did: only the routine is written.
        let repository = Repository(try sampleRecords().filter { $0.kind == codec.kinds.exercise })
        let ids = try await remote.copySample(api(repository.server), from: try bundled())
        #expect(ids == 1)
        #expect(repository.puts == ["PUT /api/v1/ada.example.com/training/routine/\(SampleRoutine.id.rawValue)"])
    }

    @Test("a routine deleted on the phone is purged through the outbox, leaves the cache at once, and never comes back")
    func deleteThroughTheOutbox() async throws {
        let directory = temporaryDirectory()
        let repository = Repository(try sampleRecords())
        let api = api(repository.server)
        let remote = RemoteCatalog(directory: directory, writingOptions: .atomic)
        let outbox = Outbox(directory: directory, writingOptions: .atomic)
        let sample = SampleInstall(directory: directory, writingOptions: .atomic)
        try sample.record(.installed)
        #expect(try await remote.refresh(api).routines[SampleRoutine.id] != nil)

        // Offline: the delete waits, the cache drops the routine and keeps its exercises.
        try await outbox.delete(routine: SampleRoutine.id, codec: codec)
        let cached = try remote.remove(routine: SampleRoutine.id)
        #expect(cached.routines.isEmpty)
        #expect(cached.exercises.count == 9, "the exercises stay: history names them")
        #expect(try remote.loadNow().routines.isEmpty, "the cache on disk lost it too")
        #expect(await outbox.waitingDeletes == ["\(codec.kinds.routine)/\(SampleRoutine.id.rawValue)"])

        // A sync before the delete lands: the server still lists it, the app does not.
        let refreshed = try await remote.refresh(api, hiding: await outbox.waitingDeletes)
        #expect(refreshed.routines.isEmpty)
        #expect(sample.offersSample(refreshed))

        // The delete lands as a purge of the routine, and only the routine.
        #expect(await outbox.flush(api) == .sent)
        let deletes = repository.server.sent.filter { $0.method == "DELETE" }
        #expect(deletes.map(\.path) == ["/api/v1/ada.example.com/training/routine/\(SampleRoutine.id.rawValue)"])
        #expect(deletes.first?.query == "purge=true")
        #expect(await outbox.entries.isEmpty)

        // The server no longer holds it, and the first run never writes it again.
        repository.hold(try sampleRecords().filter { $0.kind == codec.kinds.exercise })
        let after = try await remote.refresh(api, hiding: await outbox.waitingDeletes)
        #expect(after.routines.isEmpty && after.exercises.count == 9)
        #expect(try await !sample.firstRun(api, remote: remote, bundled: try bundled()))
        #expect(repository.puts.isEmpty)
    }

    @Test("a delete replaces any waiting write of the routine, and the sample added back takes a waiting delete back")
    func withdraw() async throws {
        let directory = temporaryDirectory()
        let outbox = Outbox(directory: directory, writingOptions: .atomic)
        let routine = FlatRecord(kind: codec.kinds.routine, id: "push-day", properties: [:])
        try await outbox.enqueue([OutboxEntry(method: .put, kind: routine.kind, recordID: routine.id, properties: [:])])
        try await outbox.delete(routine: "push-day", codec: codec)
        #expect(await outbox.entries.map(\.method) == [.purge])
        try await outbox.delete(routine: SampleRoutine.id, codec: codec)
        try await outbox.withdrawDelete(routine: SampleRoutine.id, codec: codec)
        #expect(await outbox.entries.map(\.recordID) == ["push-day"])
        // Kept on disk: a relaunch reads the same queue.
        #expect(await Outbox(directory: directory, writingOptions: .atomic).entries.map(\.recordID) == ["push-day"])
    }

    @Test("the empty Routines screen offers the sample only once it was written and deleted")
    func emptyState() throws {
        let withSample = try bundled()
        let without = Catalog()
        #expect(!SampleInstall.offersSample(state: nil, catalog: without), "the first run is still to come")
        #expect(!SampleInstall.offersSample(state: .skipped, catalog: without), "the repository had routines of its own")
        #expect(!SampleInstall.offersSample(state: .installed, catalog: withSample), "the sample is there")
        #expect(SampleInstall.offersSample(state: .installed, catalog: without), "the owner deleted it")
        // The words say where routines come from, and offer no starter set.
        #expect(RoutineListPresentation.serverEmptyText.contains("your agent"))
        #expect(!RoutineListPresentation.serverEmptyText.localizedCaseInsensitiveContains("starter"))
    }

    @Test("an unreadable sample file still says the first run happened")
    func unreadable() throws {
        let directory = temporaryDirectory()
        try Data("not json".utf8).write(to: directory.appendingPathComponent(SampleInstall.fileName))
        #expect(SampleInstall(directory: directory, writingOptions: .atomic).state == .installed)
    }
}
