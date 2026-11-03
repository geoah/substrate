import Foundation
import SubstrateLocal
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import Testing
import WorkoutsCore
@testable import WorkoutsSync
import SubstrateCore

/// The sample routine on a server (decision S1): written once, into a
/// repository with no routines, never again once the owner deletes it, on
/// any device; a failed write never stops the sync; a routine's delete goes
/// through the outbox and leaves the cache at once; a session logged signed
/// out on the sample finds its exercises on the server.
@Suite("Sample routine and routine deletes")
struct SampleInstallTests {
    let codec = RecordCodec()

    /// A repository the way the server keeps one: a PUT is kept (merged), a
    /// DELETE removes (404 when there is nothing), a list answers the kinds it
    /// names, and a session or set naming an exercise it lacks is refused, as
    /// `mustExist` refuses it. `refusing` refuses a PUT of those ids.
    final class Repository: @unchecked Sendable {
        let server = ScriptedServer()
        private let lock = NSLock()
        /// `kind/id` → (kind, id, properties).
        private var held: [String: (kind: String, id: String, properties: JSONValue)] = [:]
        private var refused: Set<String> = []

        init(_ records: [FlatRecord] = []) {
            for record in records { put(record.kind, record.id, .object(record.properties)) }
            server.on { [unowned self] sent in answer(sent) }
        }

        func refusing(_ ids: Set<String>) { lock.withLock { refused = ids } }

        func holds(_ kind: String, _ id: String) -> Bool {
            lock.withLock { held["\(kind)/\(id)"] != nil }
        }

        func ids(_ kind: String) -> [String] {
            lock.withLock { held.values.filter { $0.kind == kind }.map(\.id).sorted() }
        }

        func purge(_ kind: String, _ id: String) { _ = lock.withLock { held.removeValue(forKey: "\(kind)/\(id)") } }

        var puts: [String] { server.methodsAndPaths.filter { $0.hasPrefix("PUT ") } }

        private func put(_ kind: String, _ id: String, _ properties: JSONValue) {
            lock.withLock { held["\(kind)/\(id)"] = (kind, id, properties) }
        }

        private func answer(_ sent: ScriptedServer.Sent) -> (Int, String)? {
            if sent.method == "GET", sent.path == "/api/v1/records" {
                let query = sent.query ?? ""
                let records = lock.withLock { Array(held.values) }.filter { query.contains($0.kind) }.sorted { $0.id < $1.id }
                return (200, page(records))
            }
            let parts = sent.path.split(separator: "/").map(String.init)
            guard parts.count == 6, parts[0] == "api", parts[1] == "v1" else { return nil }
            let kind = parts[2 ... 4].joined(separator: "/")
            let id = parts[5]
            switch sent.method {
            case "PUT":
                if lock.withLock({ refused.contains(id) }) {
                    return (422, #"{"error":{"code":"validation","message":"refused by a rule"}}"#)
                }
                guard case let .object(body)? = sent.body, let properties = body["properties"] else { return (400, "{}") }
                if kind.hasSuffix("/session") || kind.hasSuffix("/set"), let missing = missingExercise(properties) {
                    return (422, #"{"error":{"code":"validation","message":"exercise \#(missing) does not exist"}}"#)
                }
                put(kind, id, properties)
                return (200, record(1))
            case "PATCH":
                // Finish: merged into the record the create left.
                guard lock.withLock({ held["\(kind)/\(id)"] }) != nil else { return (404, #"{"error":{"code":"not_found","message":"no such record"}}"#) }
                return (200, record(2))
            case "GET":
                return lock.withLock({ held["\(kind)/\(id)"] }) == nil ? (404, "{}") : (200, record(1))
            case "DELETE":
                let removed = lock.withLock { held.removeValue(forKey: "\(kind)/\(id)") }
                return removed == nil ? (404, #"{"error":{"code":"not_found","message":"no such record"}}"#) : (200, "{}")
            default:
                return nil
            }
        }

        /// The first `…/training/exercise/<id>` the properties name that the
        /// repository lacks.
        private func missingExercise(_ value: JSONValue) -> String? {
            switch value {
            case let .string(text):
                guard let range = text.range(of: "/training/exercise/") else { return nil }
                let remote = String(text[..<range.upperBound].dropLast())
                let id = String(text[range.upperBound...])
                return lock.withLock { held["\(remote)/\(id)"] } == nil ? id : nil
            case let .object(object):
                for (_, item) in object { if let missing = missingExercise(item) { return missing } }
                return nil
            case let .array(items):
                for item in items { if let missing = missingExercise(item) { return missing } }
                return nil
            default:
                return nil
            }
        }

        private func page(_ records: [(kind: String, id: String, properties: JSONValue)]) -> String {
            let items: [JSONValue] = records.map {
                .object(["kind": .string($0.kind), "id": .string($0.id), "properties": $0.properties, "version": .number(1)])
            }
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.withoutEscapingSlashes]
            let data = try! encoder.encode(JSONValue.object(["records": .array(items)]))
            return String(decoding: data, as: UTF8.self)
        }
    }

    /// One phone signed in to the repository: its own folder.
    struct Device {
        let directory = temporaryDirectory()
        var remote: RemoteCatalog { RemoteCatalog(directory: directory, writingOptions: .atomic) }
        var sample: SampleInstall { SampleInstall(directory: directory, writingOptions: .atomic) }
        var outbox: Outbox { (try! Outbox.workouts(directory: directory, writingOptions: .atomic)) }
    }

    func bundled() throws -> Catalog { try BundledCatalog().loadNow() }

    func sampleExercises() throws -> [FlatRecord] {
        try bundled().exercises.values.sorted { $0.id < $1.id }.map(codec.encode)
    }

    var sampleRoutine: String { SampleRoutine.id.rawValue }

    // MARK: The first run

    @Test("an empty repository gets the sample and its exercises once, exercises first, and a relaunch keeps it so")
    func firstRunOnce() async throws {
        let repository = Repository()
        let api = api(repository.server)
        let phone = Device()
        let sample = phone.sample
        #expect(sample.state == nil)
        #expect(try await sample.firstRun(api, remote: phone.remote, bundled: try bundled()))
        #expect(sample.state == .installed)
        let puts = repository.puts
        #expect(puts.count == 14)
        #expect(puts.dropLast().allSatisfy { $0.contains("/training/exercise/") })
        #expect(puts.last == "PUT /api/v1/substrate.reamde.dev/training/routine/\(sampleRoutine)")

        // The next sync, and the next launch, write nothing more.
        #expect(try await !sample.firstRun(api, remote: phone.remote, bundled: try bundled()))
        #expect(phone.sample.state == .installed)
        #expect(try await !phone.sample.firstRun(api, remote: phone.remote, bundled: try bundled()))
        #expect(repository.puts.count == 14)
    }

    @Test("a sample deleted on one phone stays deleted on a second phone and after a reinstall")
    func secondDeviceAndReinstall() async throws {
        let repository = Repository()
        let api = api(repository.server)

        // Phone A writes the sample, then the owner deletes it there.
        let phoneA = Device()
        #expect(try await phoneA.sample.firstRun(api, remote: phoneA.remote, bundled: try bundled()))
        try await phoneA.outbox.delete(routine: SampleRoutine.id, codec: codec)
        #expect(await phoneA.outbox.flush(api) == .sent)
        #expect(!repository.holds(codec.kinds.routine, sampleRoutine))
        #expect(repository.ids(codec.kinds.exercise).count == 13, "the exercises stay")
        let writes = repository.puts.count

        // Phone B signs in for the first time: no routines, but the sample's
        // exercises are there, so the sample was written once already.
        let phoneB = Device()
        #expect(try await !phoneB.sample.firstRun(api, remote: phoneB.remote, bundled: try bundled()))
        #expect(phoneB.sample.state == .installed)
        #expect(phoneB.sample.offersSample(try await phoneB.remote.refresh(api)), "B offers it back, as A does")

        // Phone A reinstalled: its folder is gone, and the server still says so.
        let reinstalled = Device()
        #expect(try await !reinstalled.sample.firstRun(api, remote: reinstalled.remote, bundled: try bundled()))
        #expect(reinstalled.sample.state == .installed)
        #expect(repository.puts.count == writes, "nobody wrote the sample again")
        #expect(!repository.holds(codec.kinds.routine, sampleRoutine))
    }

    @Test("a write cut short is finished by the phone that started it, and another phone does not take it for its own")
    func interruptedWrite() async throws {
        let repository = Repository()
        repository.refusing([sampleRoutine])
        let api = api(repository.server)
        let phoneA = Device()
        await #expect(throws: (any Error).self) { try await phoneA.sample.firstRun(api, remote: phoneA.remote, bundled: try bundled()) }
        #expect(phoneA.sample.state == .installing)

        // Phone B sees the exercises: written already, so it writes nothing.
        let phoneB = Device()
        #expect(try await !phoneB.sample.firstRun(api, remote: phoneB.remote, bundled: try bundled()))
        #expect(phoneB.sample.state == .installed)

        // Phone A's next sync finishes its own write.
        repository.refusing([])
        #expect(try await phoneA.sample.firstRun(api, remote: phoneA.remote, bundled: try bundled()))
        #expect(phoneA.sample.state == .installed)
        #expect(repository.holds(codec.kinds.routine, sampleRoutine))
    }

    @Test("a repository with routines of its own gets no sample, now or after they are gone")
    func routinesOfItsOwn() async throws {
        let own = FlatRecord(kind: codec.kinds.routine, id: "push-day", properties: ["name": .string("Push day")])
        let repository = Repository([own])
        let api = api(repository.server)
        let phone = Device()
        #expect(try await !phone.sample.firstRun(api, remote: phone.remote, bundled: try bundled()))
        #expect(phone.sample.state == .skipped)
        repository.purge(codec.kinds.routine, "push-day")
        #expect(try await !phone.sample.firstRun(api, remote: phone.remote, bundled: try bundled()))
        #expect(repository.puts.isEmpty)
        #expect(!phone.sample.offersSample(Catalog()), "nothing to offer back: the sample was never there")
    }

    @Test("a refused sample write never stops the sync: the catalogue is read, the outbox still flushes, and the next sync tries again")
    func failedWriteDoesNotBlock() async throws {
        let repository = Repository()
        repository.refusing(["push-up"])
        let api = api(repository.server)
        let phone = Device()
        let outbox = phone.outbox
        let sample = phone.sample
        let read = try await CatalogSync.read(api, remote: phone.remote, outbox: outbox, sample: sample, bundled: { try bundled() })
        #expect(read.sampleProblem?.contains("refused by a rule") == true, "said, softly: \(String(describing: read.sampleProblem))")
        #expect(read.catalog.routines.isEmpty)
        #expect(sample.state == .installing)

        // What follows the catalogue still runs: a write queued goes.
        let added = Exercise(id: "sled-push", name: "Sled Push", measurement: .shortdistanceweight)
        try await outbox.enqueue([OutboxPlan.exerciseAdded(added, codec: codec)])
        #expect(await outbox.flush(api) == .sent)
        #expect(repository.holds(codec.kinds.exercise, "sled-push"))

        // The next sync tries the sample again, and it lands.
        repository.refusing([])
        let again = try await CatalogSync.read(api, remote: phone.remote, outbox: outbox, sample: sample, bundled: { try bundled() })
        #expect(again.sampleProblem == nil)
        #expect(again.catalog.routines[SampleRoutine.id] != nil)
        #expect(sample.state == .installed)
    }

    // MARK: Deletes

    @Test("a routine deleted on the phone is purged through the outbox, leaves the cache at once, and never comes back")
    func deleteThroughTheOutbox() async throws {
        let repository = Repository()
        let api = api(repository.server)
        let phone = Device()
        let remote = phone.remote
        let outbox = phone.outbox
        let sample = phone.sample
        #expect(try await sample.firstRun(api, remote: remote, bundled: try bundled()))
        #expect(try await remote.refresh(api).routines[SampleRoutine.id] != nil)

        // Offline: the delete waits, the cache drops the routine and keeps its exercises.
        try await outbox.delete(routine: SampleRoutine.id, codec: codec)
        let cached = try remote.remove(routine: SampleRoutine.id)
        #expect(cached.routines.isEmpty)
        #expect(cached.exercises.count == 13, "the exercises stay: history names them")
        #expect(try remote.loadNow().routines.isEmpty, "the cache on disk lost it too")
        #expect(await outbox.waitingDeletes == ["\(codec.kinds.routine)/\(sampleRoutine)"])

        // A sync before the delete lands: the server still lists it, the app does not.
        let refreshed = try await remote.refresh(api) { await outbox.waitingDeletes }
        #expect(refreshed.routines.isEmpty)
        #expect(sample.offersSample(refreshed))

        // The delete lands as a purge of the routine, and only the routine.
        #expect(await outbox.flush(api) == .sent)
        let deletes = repository.server.sent.filter { $0.method == "DELETE" }
        #expect(deletes.map(\.path) == ["/api/v1/substrate.reamde.dev/training/routine/\(sampleRoutine)"])
        #expect(deletes.first?.query == "purge=true")
        #expect(await outbox.entries.isEmpty)
        let after = try await remote.refresh(api) { await outbox.waitingDeletes }
        #expect(after.routines.isEmpty && after.exercises.count == 13)
        let writes = repository.puts.count
        #expect(try await !sample.firstRun(api, remote: remote, bundled: try bundled()))
        #expect(repository.puts.count == writes)
    }

    @Test("a routine deleted while a catalogue read is on the wire stays out of what the read keeps")
    func deleteDuringRefresh() async throws {
        let repository = Repository()
        let api = api(repository.server)
        let phone = Device()
        let remote = phone.remote
        let outbox = phone.outbox
        try await phone.sample.firstRun(api, remote: remote, bundled: try bundled())
        try await remote.refresh(api)

        let gate = repository.server.hold("GET", "/api/v1/records")
        let read = Task { try await remote.refresh(api) { await outbox.waitingDeletes } }
        await gate.arrived()
        try await outbox.delete(routine: SampleRoutine.id, codec: codec)
        try remote.remove(routine: SampleRoutine.id)
        gate.release()
        #expect(try await read.value.routines.isEmpty, "the read answered with the routine, and left it out")
        #expect(try remote.loadNow().routines.isEmpty)
    }

    @Test("adding the sample back while its purge is on the wire lets the purge land, then writes the routine")
    func addBackDuringPurge() async throws {
        let repository = Repository()
        let api = api(repository.server)
        let phone = Device()
        let remote = phone.remote
        let outbox = phone.outbox
        let sample = phone.sample
        try await sample.firstRun(api, remote: remote, bundled: try bundled())
        try await outbox.delete(routine: SampleRoutine.id, codec: codec)

        let path = "/api/v1/substrate.reamde.dev/training/routine/\(sampleRoutine)"
        let gate = repository.server.hold("DELETE", path)
        let flush = Task { await outbox.flush(api) }
        await gate.arrived()
        let bundled = try bundled()
        let addBack = Task { try await sample.addBack(api, remote: remote, outbox: outbox, bundled: bundled) }
        try await Task.sleep(for: .milliseconds(200))
        gate.release()
        #expect(await flush.value == .sent)
        try await addBack.value

        let order = repository.server.methodsAndPaths.filter { $0.hasSuffix(path) }
        #expect(order.suffix(2) == ["DELETE \(path)", "PUT \(path)"], "the purge first, then the routine: \(order)")
        #expect(repository.holds(codec.kinds.routine, sampleRoutine))
        #expect(sample.state == .installed)
    }

    @Test("a purge of a routine already gone (404) counts as done")
    func purgeOfNothing() async throws {
        let repository = Repository()
        let phone = Device()
        let outbox = phone.outbox
        try await outbox.delete(routine: "gone-already", codec: codec)
        #expect(await outbox.flush(api(repository.server)) == .sent)
        #expect(await outbox.entries.isEmpty)
    }

    @Test("a delete replaces any waiting write of the routine, and the sample added back takes a waiting delete back")
    func withdraw() async throws {
        let directory = temporaryDirectory()
        let outbox = try Outbox.workouts(directory: directory, writingOptions: .atomic)
        let routine = FlatRecord(kind: codec.kinds.routine, id: "push-day", properties: [:])
        try await outbox.enqueue([OutboxEntry(method: .put, kind: routine.kind, recordID: routine.id, properties: [:])])
        try await outbox.delete(routine: "push-day", codec: codec)
        #expect(await outbox.entries.map(\.method) == [.purge])
        try await outbox.delete(routine: SampleRoutine.id, codec: codec)
        try await outbox.withdrawDelete(routine: SampleRoutine.id, codec: codec)
        #expect(await outbox.entries.map(\.recordID) == ["push-day"])
        #expect(await (try! Outbox.workouts(directory: directory, writingOptions: .atomic)).entries.map(\.recordID) == ["push-day"])
    }

    // MARK: Sessions logged signed out

    /// The recorded session, its exercises moved onto the sample's: a session
    /// logged on the sample signed out.
    func sampleSession() throws -> SessionRecords {
        func records(_ name: String) throws -> [FlatRecord] {
            try JSONDecoder().decode(WorkoutsAPI.Page.self, from: Fixtures.data(name)).records.map { try WorkoutsAPI.record($0) }
        }
        var session = try #require(codec.sessionRecords(from: records("list-sessions") + records("list-sets")).first)
        let onto: [ExerciseID: ExerciseID] = ["romanian-deadlift-dumbbell": "bulgarian-split-squat-dumbbell",
                                              "seated-row-machine": "triceps-pushdown"]
        for index in session.session.slots.indices {
            session.session.slots[index].exercise = onto[session.session.slots[index].exercise] ?? session.session.slots[index].exercise
        }
        for index in session.sets.indices {
            session.sets[index].exercise = onto[session.sets[index].exercise] ?? session.sets[index].exercise
        }
        return session
    }

    @Test("a session logged signed out on the sample uploads to a repository with routines of its own, its exercises first")
    func signedOutSessionUploads() async throws {
        let own = FlatRecord(kind: codec.kinds.routine, id: "push-day", properties: ["name": .string("Push day")])
        let repository = Repository([own])
        let api = api(repository.server)
        let phone = Device()
        #expect(try await !phone.sample.firstRun(api, remote: phone.remote, bundled: try bundled()))
        #expect(repository.ids(codec.kinds.exercise).isEmpty, "skipped: no sample, no exercises")

        let session = try sampleSession()
        let named = Set(session.sets.map(\.exercise.rawValue))
        #expect(named.isSubset(of: Set(try bundled().exercises.keys.map(\.rawValue))), "every exercise is the sample's")
        let exercises = OutboxPlan.exercisesFor([session], server: Set(repository.ids(codec.kinds.exercise)),
                                                known: try bundled().exercises, codec: codec)
        #expect(Set(exercises.map(\.recordID)) == named)
        let outbox = phone.outbox
        try await outbox.enqueue(exercises + OutboxPlan.finished(session, codec: codec))
        let sent = await outbox.flush(api)
        let parked = await outbox.parked.map(\.lastError)
        #expect(sent == .sent, "nothing refused: \(parked)")
        #expect(repository.holds(codec.kinds.session, session.session.id.rawValue))
        let puts = repository.puts
        let firstSession = try #require(puts.firstIndex { $0.contains("/training/session/") })
        #expect(puts[..<firstSession].allSatisfy { $0.contains("/training/exercise/") } && firstSession == named.count)

        // Without them the server refuses the session, as it did before.
        let bare = Repository([own])
        let refused = Device().outbox
        try await refused.enqueue(OutboxPlan.finished(session, codec: codec))
        if case .refused = await refused.flush(WorkoutsSyncTests.api(bare.server)) {} else { Issue.record("the session went without its exercises") }
    }

    @Test("an exercise the repository holds, or one no catalogue knows, is not written")
    func exercisesForSkips() throws {
        let session = try sampleSession()
        let all = Set(session.sets.map(\.exercise.rawValue))
        let entries = OutboxPlan.exercisesFor([session], server: ["leg-press-machine"], known: try bundled().exercises.filter { $0.key != "push-up" },
                                              codec: codec)
        #expect(Set(entries.map(\.recordID)) == all.subtracting(["leg-press-machine"]))
        #expect(entries.allSatisfy { $0.method == .put && $0.kind == codec.kinds.exercise })
    }

    // MARK: The empty screen

    @Test("the empty Routines screen offers the sample only once it was written and deleted")
    func emptyState() throws {
        let withSample = try bundled()
        let without = Catalog()
        #expect(!SampleInstall.offersSample(state: nil, catalog: without), "the first run is still to come")
        #expect(!SampleInstall.offersSample(state: .installing, catalog: without), "the write is still going")
        #expect(!SampleInstall.offersSample(state: .skipped, catalog: without), "the repository had routines of its own")
        #expect(!SampleInstall.offersSample(state: .installed, catalog: withSample), "the sample is there")
        #expect(SampleInstall.offersSample(state: .installed, catalog: without), "the owner deleted it")
        #expect(RoutineListPresentation.serverEmptyText.contains("your agent"))
        #expect(!RoutineListPresentation.serverEmptyText.localizedCaseInsensitiveContains("starter"))
    }

    @Test("an unreadable sample file is settled again from the server")
    func unreadable() throws {
        let directory = temporaryDirectory()
        try Data("not json".utf8).write(to: directory.appendingPathComponent(SampleInstall.fileName))
        #expect(SampleInstall(directory: directory, writingOptions: .atomic).state == nil)
    }
}
