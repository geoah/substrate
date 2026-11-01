import Foundation
@testable import WorkoutsCore
import Testing

/// Every example session (the three of sessions.json and the three of
/// coverage.json, one of them in progress) with its sets.
private func exampleSessions() throws -> [SessionRecords] {
    try ExampleFixtures.sessions("sessions") + ExampleFixtures.sessions("coverage")
}

private let fullyBody = SessionID("01a17d8d-afc0-7a2b-bc4d-5e6f70819203")
private let day1First = SessionID("01a16363-cf80-73ed-85fd-704f6ce38d73")
private let day1Second = SessionID("01a17831-8a38-7502-a232-81e9c175d65e")
private let coverage = SessionID("01a181f3-c900-74e4-b0e2-345e3defd3cf")
private let hotelGym = SessionID("01a188d6-2ce0-7bbd-84a8-a52f602ab29c")
private let hevy = SessionID("hevy-3f1c9a2e-8b47-4d0e-9a61-2c7d5e8f4b10")

@Suite("History store")
struct HistoryStoreTests {
    @Test("written sessions load back equal, newest first, with a pending Health status")
    func writeAndLoad() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let store = HistoryStore(directory: directory.url, writingOptions: .atomic)
        let sessions = try exampleSessions()
        for records in sessions {
            try store.write(HistoryEntry(records: records))
        }
        #expect(directory.files(in: "history").count == 6)
        let load = store.loadAll()
        #expect(load.unreadable.isEmpty)
        #expect(load.entries.map(\.id) == [hotelGym, coverage, fullyBody, day1Second, day1First, hevy])
        for entry in load.entries {
            #expect(entry.records == sessions.first { $0.session.id == entry.id })
            #expect(entry.local.health.status == .pending)
        }
        #expect(try store.read(fullyBody).records.sets.count == 24)
    }

    @Test("the file is format 2 with the session record first, then its sets")
    func fileShape() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let store = HistoryStore(directory: directory.url, writingOptions: .atomic)
        let records = try #require(try exampleSessions().first { $0.session.id == fullyBody })
        try store.write(HistoryEntry(records: records))
        let file = try #require(try JSONValue(data: Data(contentsOf: store.url(for: fullyBody))).objectValue)
        #expect(WorkoutsFormats.historyFormat == 2)
        #expect(file["format"] == .number(2))
        let flat = try #require(file["records"]?.arrayValue).map { try FlatRecord(json: $0) }
        #expect(flat.first?.kind == RecordCodec().kinds.session)
        #expect(flat.dropFirst().allSatisfy { $0.kind == RecordCodec().kinds.set })
        #expect(flat == RecordCodec().encode(records))
        #expect(file["local"]?.objectValue?["health"]?.objectValue?["status"] == .string("pending"))
    }

    @Test("a file that does not decode is skipped, reported and kept")
    func skipUndecodable() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let store = HistoryStore(directory: directory.url, writingOptions: .atomic)
        let sessions = try exampleSessions()
        for records in sessions.prefix(3) {
            try store.write(HistoryEntry(records: records))
        }
        let history = directory.url.appendingPathComponent("history")
        try Data("not json".utf8).write(to: history.appendingPathComponent("garbage.json"))
        // A set whose rpe is a JSON number, which the codec refuses.
        var bad = try #require(try JSONValue(data: Data(contentsOf: store.url(for: fullyBody))).objectValue)
        var flat = try #require(bad["records"]?.arrayValue)
        var set = try #require(flat[1].objectValue)
        var properties = try #require(set["properties"]?.objectValue)
        properties["rpe"] = .number(9)
        set["properties"] = .object(properties)
        flat[1] = .object(set)
        bad["records"] = .array(flat)
        try JSONValue.object(bad).data().write(to: history.appendingPathComponent("refused.json"))
        // A later format.
        var future = try #require(try JSONValue(data: Data(contentsOf: store.url(for: day1First))).objectValue)
        future["format"] = .number(3)
        try JSONValue.object(future).data().write(to: history.appendingPathComponent("future.json"))
        try Data("ignored".utf8).write(to: history.appendingPathComponent("notes.txt"))

        let load = store.loadAll()
        #expect(load.entries.map(\.id) == [fullyBody, day1Second, day1First])
        #expect(load.unreadable.map(\.source) == ["history/future.json", "history/garbage.json", "history/refused.json"])
        #expect(load.unreadable.allSatisfy { !$0.reason.isEmpty })
        #expect(directory.files(in: "history").contains("garbage.json"))
        #expect(directory.files(in: "history").contains("refused.json"))
    }

    @Test("delete removes the file; deleting again is fine")
    func delete() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let store = HistoryStore(directory: directory.url, writingOptions: .atomic)
        for records in try exampleSessions().prefix(3) {
            try store.write(HistoryEntry(records: records))
        }
        try store.delete(day1Second)
        #expect(store.loadAll().entries.map(\.id) == [fullyBody, day1First])
        try store.delete(day1Second)
        #expect(throws: (any Error).self) { try store.read(day1Second) }
    }

    @Test("a missing history directory is an empty history")
    func empty() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let load = HistoryStore(directory: directory.url, writingOptions: .atomic).loadAll()
        #expect(load.entries.isEmpty)
        #expect(load.unreadable.isEmpty)
    }

    @Test("a Health save links the workout, raises the revision once and records the status")
    func healthSave() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let store = HistoryStore(directory: directory.url, writingOptions: .atomic)
        var records = try #require(try exampleSessions().first { $0.session.id == fullyBody })
        records.session.healthKitWorkout = nil
        records.session.revision = 2
        try store.write(HistoryEntry(records: records))
        let when = Date(timeIntervalSince1970: 1_793_110_600)
        try store.recordHealthSave(fullyBody, status: .saved, healthKitWorkout: "F1DB07FB-4FAF-4334-922C-B0B3F5A970EA", at: when)
        // A repeat of the same link changes nothing more.
        let entry = try store.recordHealthSave(fullyBody, status: .saved, healthKitWorkout: "F1DB07FB-4FAF-4334-922C-B0B3F5A970EA", at: when)
        #expect(entry.records.session.healthKitWorkout == "F1DB07FB-4FAF-4334-922C-B0B3F5A970EA")
        #expect(entry.records.session.revision == 3)
        #expect(entry.local.health == HealthSaveState(status: .saved, updatedAt: when))
        #expect(try store.read(fullyBody) == entry)
    }

    @Test("the store is a HistorySource")
    func historySource() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let store = HistoryStore(directory: directory.url, writingOptions: .atomic)
        for records in try exampleSessions() {
            try store.write(HistoryEntry(records: records))
        }
        let source: any HistorySource = store
        #expect(try await source.sessions().map(\.id) == [hotelGym, coverage, fullyBody, day1Second, day1First, hevy])
    }
}

@Suite("History index")
struct HistoryIndexTests {
    @Test("an exercise's history is newest first, its sets in display order")
    func byExercise() throws {
        let index = try HistoryIndex(exampleSessions())
        let legPress = index.history(of: "leg-press-machine")
        #expect(legPress.map(\.session.id) == [coverage, day1Second, day1First])
        for entry in legPress {
            #expect(entry.sets.allSatisfy { $0.exercise == "leg-press-machine" && $0.setup == nil })
            #expect(entry.sets.map(\.order) == entry.sets.map(\.order).sorted())
        }
        #expect(legPress[2].sets.map(\.role) == [.warmup, .warmup, .working, .working, .working])
        #expect(index.lastDone("leg-press-machine") == index.session(coverage)?.session.at)
    }

    @Test("sets keep to their setup; V1 reads sets with none")
    func setups() throws {
        let index = try HistoryIndex(exampleSessions())
        #expect(index.history(of: "chest-press-machine").map(\.session.id) == [fullyBody])
        #expect(index.history(of: "chest-press-machine", setup: "old-gym-chest-press").map(\.session.id) == [hevy])
        #expect(index.history(of: "chest-press-machine", setup: "hotel-gym-chest-press").map(\.session.id) == [hotelGym])
        #expect(index.lastDone("chest-press-machine") == index.session(hotelGym)?.session.at)
    }

    @Test("the active session is left out when asked")
    func excluding() throws {
        let index = try HistoryIndex(exampleSessions())
        #expect(index.last("leg-press-machine")?.session.id == coverage)
        #expect(index.last("leg-press-machine", excluding: coverage)?.session.id == day1Second)
        #expect(index.history(of: "no-such-exercise").isEmpty)
    }

    @Test("sets come in the session's slot order, whatever order they were done in")
    func displayOrder() throws {
        let index = try HistoryIndex(exampleSessions())
        let session = try #require(index.session(fullyBody))
        let chest = try #require(index.last("chest-press-machine"))
        #expect(chest.slots == ["chest-press"])
        #expect(chest.sets.map(\.order) == [1, 2, 3, 4])
        #expect(chest.sets(inSlot: "chest-press").count == 4)
        // The owner did core first; the slots still list the chest press first.
        #expect(session.session.slots.first?.key == "chest-press")
    }

    @Test("insert and remove keep the index current")
    func insertAndRemove() throws {
        let sessions = try exampleSessions()
        var index = HistoryIndex(sessions.filter { $0.session.id != coverage })
        #expect(index.last("leg-press-machine")?.session.id == day1Second)
        try index.insert(#require(sessions.first { $0.session.id == coverage }))
        #expect(index.last("leg-press-machine")?.session.id == coverage)
        #expect(index.sessions.count == 6)
        index.remove(coverage)
        #expect(index.last("leg-press-machine")?.session.id == day1Second)
        #expect(!index.exercises.contains("rowing-machine"))
    }
}

@Suite("Health queue file")
struct HealthQueueFileTests {
    @Test("enqueue once, record failures, remove")
    func lifecycle() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let queue = HealthQueueFile(directory: directory.url, writingOptions: .atomic)
        #expect(try queue.load().isEmpty)
        let start = Date(timeIntervalSince1970: 1_793_110_588)
        try queue.enqueue(fullyBody, at: start)
        try queue.enqueue(day1First, at: start.addingTimeInterval(60))
        try queue.enqueue(fullyBody, at: start.addingTimeInterval(120))
        #expect(try queue.load().map(\.session) == [fullyBody, day1First])
        try queue.recordFailure(fullyBody, error: "The operation couldn't be completed.", at: start.addingTimeInterval(180))
        try queue.recordFailure(fullyBody, error: "Still failing", at: start.addingTimeInterval(240))
        let entry = try #require(try queue.load().first)
        #expect(entry.attempts == 2)
        #expect(entry.lastError == "Still failing")
        #expect(entry.lastAttemptAt == start.addingTimeInterval(240))
        #expect(entry.enqueuedAt == start)
        try queue.remove(fullyBody)
        #expect(try queue.load().map(\.session) == [day1First])
        #expect(directory.files() == ["health-queue.json"])
    }

    /// Finish died between the history file and the queue write, or the
    /// queue would not decode: History still says pending with no workout.
    @Test("sessions History holds as pending with no workout are queued again; an unreadable queue is set aside")
    func lostSessionsAndUnreadableQueue() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let queue = HealthQueueFile(directory: directory.url, writingOptions: .atomic)
        let sessions = try exampleSessions()
        var history = sessions.map { HistoryEntry(records: $0) }
        history[0].local.health.status = .saved
        history[1].records.session.healthKitWorkout = "5E1C0A8B-0000-4000-8000-000000000001"
        history[2].local.health.status = .notSaved
        let lost = HealthQueueFile.lost(in: history)
        let waiting = history[3...].filter { $0.records.session.healthKitWorkout == nil }
        #expect(lost.count >= 2)
        #expect(lost == waiting.sorted { $0.records.session.at < $1.records.session.at }.map(\.id))
        let start = Date(timeIntervalSince1970: 1_793_110_588)
        try queue.enqueue(lost[0], at: start)
        #expect(try queue.enqueue(missing: lost + [lost[1]], at: start) == Array(lost.dropFirst()))
        #expect(try queue.load().map(\.session) == lost)
        #expect(try queue.enqueue(missing: lost, at: start).isEmpty)
        #expect(queue.setAsideIfUnreadable(at: start) == nil)

        try Data("{\"format\": 1, \"entries\": [{\"session\": 3}]}".utf8).write(to: queue.url)
        #expect(throws: DecodingError.self) { try queue.load() }
        let aside = try #require(queue.setAsideIfUnreadable(at: start))
        #expect(aside.lastPathComponent == "health-queue.unreadable-1793110588.json")
        #expect(try queue.load().isEmpty)
        #expect(try queue.enqueue(missing: lost, at: start) == lost)
        #expect(directory.files() == ["health-queue.json", "health-queue.unreadable-1793110588.json"])
    }
}

@Suite("Local exercises")
struct LocalExercisesTests {
    @Test("an added exercise loads back marked as local; adding its id again replaces it")
    func addAndLoad() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let store = LocalExercises(directory: directory.url, writingOptions: .atomic)
        #expect(try store.load().exercises.isEmpty)
        var exercise = Exercise(id: ExerciseID.slug(for: "Hack Squat (Machine)"), name: "Hack Squat (Machine)", measurement: .weightreps, equipment: .plateloaded, restClass: .heavycompound)
        try store.add(exercise)
        try store.add(Exercise(id: "sled-push", name: "Sled Push", measurement: .shortdistanceweight, equipment: .other))
        exercise.laterality = .bilateral
        try store.add(exercise)
        let load = try store.load()
        #expect(load.unreadable.isEmpty)
        #expect(load.exercises.map(\.id) == ["hack-squat-machine", "sled-push"])
        #expect(load.exercises.allSatisfy { $0.isLocal })
        #expect(load.exercises.first?.laterality == .bilateral)
        // Stored as flat exercise records, which never carry isLocal.
        let file = try JSONDecoder().decode(RecordFile.self, from: Data(contentsOf: store.url))
        #expect(file.format == 1)
        #expect(file.records.allSatisfy { $0.kind == RecordCodec().kinds.exercise && $0.properties["isLocal"] == nil })
    }

    @Test("a record this build cannot read is listed and kept through later adds")
    func keepsUnreadable() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let store = LocalExercises(directory: directory.url, writingOptions: .atomic)
        let future = FlatRecord(kind: RecordCodec().kinds.exercise, id: "ski-erg", properties: ["name": .string("SkiErg"), "measurement": .string("calories")])
        try RecordFile(format: 1, records: [future]).json.data().write(to: store.url)
        try store.add(Exercise(id: "sled-push", name: "Sled Push", measurement: .shortdistanceweight))
        let load = try store.load()
        #expect(load.exercises.map(\.id) == ["sled-push"])
        #expect(load.unreadable.map(\.id) == ["ski-erg"])
        #expect(try JSONDecoder().decode(RecordFile.self, from: Data(contentsOf: store.url)).records.first == future)
    }
}

@Suite("Bundled catalogue")
struct BundledCatalogTests {
    @Test("the bundle holds the owner's 30 exercises and 4 routines, all readable")
    func bundled() throws {
        let catalog = try BundledCatalog().loadNow()
        #expect(catalog.exercises.count == 30)
        #expect(catalog.routines.count == 4)
        #expect(catalog.unreadable.isEmpty)
        #expect(catalog.exercises.values.allSatisfy { !$0.isLocal })
        #expect(catalog.routines["fully-body-collection-1"]?.name == "Fully Body Collection #1")
        // Every routine slot names a bundled exercise.
        for routine in catalog.routines.values {
            for slot in routine.slots {
                #expect(catalog.exercises[slot.exercise] != nil, "\(routine.id) \(slot.key)")
            }
        }
        // The test-only coverage exercises never ship.
        #expect(catalog.exercises["rowing-machine"] == nil)
    }

    @Test("local exercises join the catalogue and every update stream hears of a new one")
    func localExercises() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let catalogSource = BundledCatalog(localExercises: LocalExercises(directory: directory.url, writingOptions: .atomic))
        let updates = catalogSource.updates()
        let added = Exercise(id: "sled-push", name: "Sled Push", measurement: .shortdistanceweight)
        try catalogSource.addLocalExercise(added)
        var iterator = updates.makeAsyncIterator()
        let update = try #require(await iterator.next())
        #expect(update.exercises.count == 31)
        #expect(update.exercises["sled-push"]?.isLocal == true)
        let loaded = try await catalogSource.load()
        #expect(loaded.exercises["sled-push"]?.name == "Sled Push")
    }

    @Test("a local exercise may not take a bundled id")
    func collision() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let local = LocalExercises(directory: directory.url, writingOptions: .atomic)
        try local.add(Exercise(id: "chest-press-machine", name: "My Chest Press", measurement: .weightreps))
        let catalog = try BundledCatalog(localExercises: local).loadNow()
        #expect(catalog.exercises["chest-press-machine"]?.name == "Chest Press (Machine)")
        #expect(catalog.unreadable.map(\.id) == ["chest-press-machine"])
    }

    @Test("without a local store, adding is refused")
    func noLocalStore() {
        #expect(throws: CatalogError.noLocalStore) {
            try BundledCatalog().addLocalExercise(Exercise(id: "x", name: "X", measurement: .repsonly))
        }
    }
}
