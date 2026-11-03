import Foundation
import SubstrateKit
import Testing
import WorkoutsCore
@testable import WorkoutsSync

/// A Hevy export imported and sent as the app sends it, against a real
/// substrate: skipped unless `WORKOUTS_E2E_SERVER` names one,
/// `WORKOUTS_E2E_OWNER_TOKEN` holds a full-access token of a repository
/// (`WORKOUTS_E2E_REPOSITORY`) holding the training package (installed
/// through the app's approval or `substratectl install`), and
/// `WORKOUTS_HEVY_CSV` names the export. The import's sessions go
/// through the history exchange and the outbox, every write lands, and a
/// second phone reading the repository back finds exactly them.
///
///     WORKOUTS_E2E_SERVER=http://localhost:8080 WORKOUTS_E2E_OWNER_TOKEN=… WORKOUTS_E2E_REPOSITORY=… \
///       WORKOUTS_HEVY_CSV=workout_data.csv swift test --filter LiveHevyImport
@Suite("Live Hevy import", .enabled(if: ProcessInfo.processInfo.environment["WORKOUTS_E2E_SERVER"] != nil))
struct LiveHevyImportTests {
    let environment = ProcessInfo.processInfo.environment

    @Test("an export's sessions and exercises land on the server",
          .enabled(if: ProcessInfo.processInfo.environment["WORKOUTS_HEVY_CSV"] != nil))
    func importAndSend() async throws {
        let server = try #require(environment["WORKOUTS_E2E_SERVER"].flatMap(URL.init(string:)))
        let repository = try #require(environment["WORKOUTS_E2E_REPOSITORY"])
        let client = SubstrateClient(server: server, token: environment["WORKOUTS_E2E_OWNER_TOKEN"] ?? "")
        let api = WorkoutsAPI(client: client)
        let codec = RecordCodec()
        #expect(try await PackageCheck(api: api, manifest: try PackageManifest.bundled()).state().writable, "install \(KindNames.packageID) in \(repository) first")

        // The exercise library, seeded as the sync seeds it, and read back as
        // the catalogue; a second run writes nothing.
        let library = try ExerciseLibrary.bundled()
        let seeding = LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic)
        let seeded = try await seeding.ensure(api, library: library)
        print("live Hevy import: library \(library.version), \(seeded.written) written, \(seeded.filled) filled")
        #expect(try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(api, library: library).current)
        let remote = RemoteCatalog(directory: temporaryDirectory(), writingOptions: .atomic)
        let catalog = try await remote.refresh(api)
        #expect(catalog.unreadable.isEmpty, "\(catalog.unreadable.prefix(3))")
        #expect(catalog.movements.count >= library.records.count { $0.kind == codec.kinds.movement })

        let path = try #require(environment["WORKOUTS_HEVY_CSV"])
        let file = try HevyCSV.read(String(contentsOfFile: path, encoding: .utf8))
        let bundled = try BundledCatalog().loadNow()
        let plan = HevyImport.plan(file, exercises: catalog.exercises, history: [], deleted: [], timeZone: TimeZone(identifier: "Europe/London")!,
                                   adapter: "substrate-workouts@test", library: try library.catalog().exercises)
        print("live Hevy import: \(plan.sessions.count) sessions, \(plan.matched.count) titles matched, new: \(plan.exercises.map(\.name))")
        #expect(plan.problems.isEmpty)
        #expect(!plan.sessions.isEmpty)

        // What `SubstrateLink.exchangeHistory` queues for a phone holding the
        // imported sessions and their new exercises.
        let directory = temporaryDirectory()
        let outbox = Outbox(directory: directory, writingOptions: .atomic)
        let before = try await ServerHistory(api: api, codec: codec).snapshot()
        let exchange = HistoryExchange.plan(server: before, local: plan.sessions, queued: [], versions: [:], tombstones: [], codec: codec)
        #expect(exchange.uploads.count == plan.sessions.count)
        let held = Set(try await api.list([codec.kinds.exercise]).map(\.id))
        var entries = plan.exercises.filter { !held.contains($0.id.rawValue) }.map { OutboxPlan.exerciseAdded($0, codec: codec) }
        let known = bundled.exercises.merging(catalog.exercises) { _, server in server }.merging(plan.exercises.map { ($0.id, $0) }) { _, new in new }
        entries += OutboxPlan.exercisesFor(exchange.uploads, server: held.union(entries.map(\.recordID)), known: known, codec: codec)
        for records in exchange.uploads.sorted(by: { $0.session.at < $1.session.at }) {
            entries += OutboxPlan.finished(records, codec: codec)
        }
        try await outbox.enqueue(entries)

        let clock = ContinuousClock()
        let started = clock.now
        let state = await outbox.flush(api)
        print("live Hevy import: \(entries.count) writes in \(clock.now - started)")
        let parked = await outbox.parked.prefix(3).map { "\($0.kind)/\($0.recordID): \($0.lastError ?? "")" }
        #expect(state == .sent, "\(parked)")
        #expect(await outbox.entries.isEmpty)

        // A second phone reads them back as the import wrote them.
        let after = try await ServerHistory(api: api, codec: codec).snapshot()
        let fresh = HistoryExchange.plan(server: after, local: [], queued: [], versions: [:], tombstones: [], codec: codec)
        #expect(fresh.unreadable.isEmpty)
        let read = Dictionary(uniqueKeysWithValues: fresh.imports.map { ($0.id, $0) })
        for records in plan.sessions {
            let back = try #require(read[records.id], "\(records.id) is not on the server")
            #expect(back.session.status == .finished && back.session.revision == 2)
            // A reference reads back as the server's object, kept in `extra`
            // for the round trip; the rest is the import's.
            var session = back.session
            session.revision = records.session.revision
            session.extra = [:]
            for index in session.slots.indices { session.slots[index].extra = [:] }
            #expect(session == records.session, "\(records.id): the session differs")
            #expect(Set(back.sets.map { set in
                var set = set
                set.extra = [:]
                return set
            }) == Set(records.sets), "\(records.id): the sets differ")
        }
        // This phone, after the flush, has nothing left to send.
        let quiet = HistoryExchange.plan(server: after, local: plan.sessions, queued: [], versions: await outbox.versions, tombstones: [],
                                         codec: codec)
        #expect(quiet.uploads.isEmpty && quiet.removals.isEmpty && quiet.finishes.isEmpty && quiet.merges.isEmpty)
    }

    /// The same through Hevy's API (`WORKOUTS_HEVY_API_KEY`): the account's
    /// workouts, routines and own exercises, every exercise named by its
    /// template id, the routines written as workout routines and each
    /// session linked to the routine it was started from.
    @Test("an account read through Hevy's API lands with its routines",
          .enabled(if: ProcessInfo.processInfo.environment["WORKOUTS_HEVY_API_KEY"] != nil))
    func importFromAPI() async throws {
        let server = try #require(environment["WORKOUTS_E2E_SERVER"].flatMap(URL.init(string:)))
        let repository = try #require(environment["WORKOUTS_E2E_REPOSITORY"])
        let api = WorkoutsAPI(client: SubstrateClient(server: server, token: environment["WORKOUTS_E2E_OWNER_TOKEN"] ?? ""))
        let codec = RecordCodec()
        #expect(try await PackageCheck(api: api, manifest: try PackageManifest.bundled()).state().writable, "install \(KindNames.packageID) in \(repository) first")
        let library = try ExerciseLibrary.bundled()
        try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(api, library: library)
        let catalog = try await RemoteCatalog(directory: temporaryDirectory(), writingOptions: .atomic).refresh(api)
        let libraryExercises = try library.catalog().exercises
        let known = Set(libraryExercises.values.flatMap { $0.codes.filter { $0.system == "hevy" }.map(\.code) })

        let export = try await HevyClient(apiKey: environment["WORKOUTS_HEVY_API_KEY"] ?? "").export(known: known)
        let plan = HevyImport.plan(export, exercises: catalog.exercises, routines: catalog.routines, history: [], deleted: [],
                                   timeZone: TimeZone(identifier: "Europe/London")!, adapter: "substrate-workouts@test", library: libraryExercises)
        let linked = plan.sessions.count { $0.session.routine != nil }
        print("live Hevy API: \(export.workouts.count) workouts, \(export.routines.count) routines, \(export.templates.count) own exercises; "
            + "\(plan.sessions.count) sessions (\(linked) linked to a routine), \(plan.routines.count) routines, new: \(plan.exercises.map(\.name))")
        #expect(plan.problems.isEmpty)
        #expect(plan.routines.count == export.routines.count)

        // Exercises first (routine slots and sets must find them), then the
        // routines, then the sessions, as the app queues them.
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        let held = Set(try await api.list([codec.kinds.exercise]).map(\.id))
        var entries = plan.exercises.filter { !held.contains($0.id.rawValue) }.map { OutboxPlan.exerciseAdded($0, codec: codec) }
        entries += plan.routines.map { OutboxPlan.routineCreated($0, codec: codec) }
        let exchange = HistoryExchange.plan(server: try await ServerHistory(api: api, codec: codec).snapshot(), local: plan.sessions, queued: [],
                                            versions: [:], tombstones: [], codec: codec)
        for records in exchange.uploads.sorted(by: { $0.session.at < $1.session.at }) {
            entries += OutboxPlan.finished(records, codec: codec)
        }
        try await outbox.enqueue(entries)
        let state = await outbox.flush(api)
        let parked = await outbox.parked.prefix(3).map { "\($0.kind)/\($0.recordID): \($0.lastError ?? "")" }
        #expect(state == .sent, "\(parked)")

        let after = try await RemoteCatalog(directory: temporaryDirectory(), writingOptions: .atomic).refresh(api)
        for routine in plan.routines {
            let back = try #require(after.routines[routine.id], "\(routine.id) is not on the server")
            #expect(back.slots.map(\.exercise) == routine.slots.map(\.exercise) && back.slots.map(\.sets) == routine.slots.map(\.sets))
        }
        let sessions = HistoryExchange.plan(server: try await ServerHistory(api: api, codec: codec).snapshot(), local: [], queued: [],
                                            versions: [:], tombstones: [], codec: codec).imports
        #expect(sessions.count == plan.sessions.count)
        #expect(sessions.count { $0.session.routine != nil } == linked)
    }
}

