import Foundation
import Testing
@testable import WorkoutsCore

/// Hevy's workout export read into sessions and sets: `Fixtures/hevy/`,
/// written by hand in the export's shape (a quoted description over two
/// lines, CRLF, warm-up, failure and drop rows, a superset, an exercise
/// done twice, a timed and a distance set, two rows that do not read).
@Suite("Hevy import")
struct HevyImportTests {
    let london = TimeZone(identifier: "Europe/London")!
    let adapter = "substrate-workouts@test"

    func text(_ name: String) throws -> String {
        let url = try #require(Bundle.module.url(forResource: name, withExtension: "csv", subdirectory: "Fixtures/hevy"))
        return try String(contentsOf: url, encoding: .utf8)
    }

    func importPlan(_ name: String = "workout_data", history: [SessionRecords] = [], deleted: Set<String> = []) throws -> HevyImport.Plan {
        let file = try HevyCSV.read(text(name))
        return HevyImport.plan(file, exercises: try ExampleFixtures.catalog().exercises, history: history, deleted: deleted,
                               timeZone: london, adapter: adapter)
    }

    @Test("quoted fields, line breaks inside them, CRLF and a byte-order mark")
    func csv() {
        let lines = CSVReader.lines("\u{FEFF}a,b,c\r\n\"x, y\",\"two\nlines\",\"say \"\"hi\"\"\"\r\n\r\nlast,,\n")
        #expect(lines.map(\.fields) == [["a", "b", "c"], ["x, y", "two\nlines", "say \"hi\""], ["last", "", ""]])
        #expect(lines.map(\.number) == [1, 2, 5])
    }

    @Test("a file that is not Hevy's workout export is refused")
    func notHevy() {
        #expect(throws: HevyImportError.empty) { try HevyCSV.read("") }
        #expect(throws: HevyImportError.strongExport) {
            try HevyCSV.read("Workout #,Date,Workout Name,Duration (sec),Exercise Name,Set Order,Weight (kg),Reps\n")
        }
        #expect(throws: HevyImportError.missingColumns(["exercise_title", "set_index"])) {
            try HevyCSV.read("title,start_time,set_type\n")
        }
    }

    @Test("two workouts, the rows that do not read listed with their lines")
    func workouts() throws {
        let plan = try importPlan()
        #expect(plan.sessions.map(\.session.id.rawValue) == ["hevy-20261030-1343", "hevy-20261102-0805"])
        #expect(plan.problems.map(\.line) == [27, 28])
        #expect(plan.problems[0].reason.contains("not a date"))
        #expect(plan.problems[1].reason.contains("weight_kg"))

        let push = try #require(plan.sessions.first).session
        #expect(push.at == ISO8601DateFormatter().date(from: "2026-10-30T13:43:00Z"))
        #expect(push.endsAt == ISO8601DateFormatter().date(from: "2026-10-30T14:30:00Z"))
        #expect(push.name == "Push, Fri 30 Oct 13:43")
        #expect(push.notes == "Felt strong,\nslept well")
        #expect(push.status == .finished && push.sport == .strength && push.revision == 1 && push.adapter == adapter)
        #expect(push.timezone == "Europe/London" && push.utcOffsetStart == 0 && push.utcOffsetEnd == 0)
        #expect(push.routine == nil && push.healthKitWorkout == nil)
    }

    @Test("slots by exercise run, a superset of neighbours, the same exercise again later")
    func slots() throws {
        let push = try #require(try importPlan().sessions.first).session
        #expect(push.slots.map(\.key) == ["bench-press-barbell", "chest-press-machine", "lat-pulldown-cable", "plank", "bench-press-barbell-2"])
        #expect(push.slots.map(\.exercise.rawValue) == ["bench-press-barbell", "chest-press-machine", "lat-pulldown-cable", "plank",
                                                         "bench-press-barbell"])
        #expect(push.slots[0].notes == "Pause at the chest")
        #expect(push.slots.map(\.group) == [nil, "superset-1", "superset-1", nil, nil])
        #expect(push.groups == [SessionGroup(key: "superset-1", groupKind: .superset)])
    }

    @Test("warm-up, working, failure and a drop chain")
    func sets() throws {
        let records = try #require(try importPlan().sessions.first)
        let bench = records.sets.filter { $0.slot == "bench-press-barbell" }
        #expect(bench.map(\.id.rawValue) == (0 ... 4).map { "hevy-20261030-1343-0-\($0)" })
        #expect(bench.map(\.role) == [.warmup, .working, .working, .working, .working])
        #expect(bench.map(\.ordinal) == [1, 1, 2, 2, 2])
        #expect(bench.map(\.order) == [1, 2, 3, 4, 5])
        #expect(bench.map(\.structure) == [.straight, .straight, .drop, .drop, .drop])
        #expect(bench.map(\.continues) == [nil, nil, nil, bench[2].id, bench[2].id])
        #expect(bench.map(\.chainPosition) == [nil, nil, 1, 2, 3])
        #expect(bench.map(\.reachedFailure) == [nil, nil, true, nil, nil])
        #expect(bench.map(\.sourceType) == ["warmup", "normal", "failure", "dropset", "dropset"])
        #expect(bench[1].load == Mass(value: 80, unit: .kg) && bench[1].reps == 8 && bench[1].repsBasis == .total)
        #expect(bench[1].rpe == 8 && bench[1].rpeScale == .hevy && bench[1].rpeMethod == .imported && bench[1].ratedRPE == 8)
        #expect(bench[0].rpe == nil && bench[0].rpeScale == nil && bench[0].rpeMethod == nil)
        for set in records.sets {
            #expect(set.at == records.session.at && set.timeBasis == .session && set.completionMethod == .imported && set.revision == 1)
            #expect(set.session == records.session.id && set.restBefore == nil && set.startedAt == nil)
        }
        let plank = try #require(records.sets.first { $0.slot == "plank" })
        #expect(plank.duration == 60 && plank.loadMode == nil && plank.load == nil && plank.reps == nil)
    }

    @Test("titles resolve against the catalogue by name; the rest are new exercises")
    func exercises() throws {
        let plan = try importPlan()
        #expect(plan.matched == ["Chest Press (Machine)": "chest-press-machine", "Plank": "plank", "Leg Press (Machine)": "leg-press-machine"])
        #expect(plan.exercises.map(\.id.rawValue) == ["bench-press-barbell", "lat-pulldown-cable", "pull-up-assisted", "treadmill"])
        let byID = Dictionary(uniqueKeysWithValues: plan.exercises.map { ($0.id.rawValue, $0) })
        #expect(byID["bench-press-barbell"]?.measurement == .weightreps && byID["bench-press-barbell"]?.equipment == .barbell)
        #expect(byID["lat-pulldown-cable"]?.equipment == .cable)
        #expect(byID["pull-up-assisted"]?.measurement == .bodyweightassistedreps)
        #expect(byID["treadmill"]?.measurement == .distanceduration)
        #expect(plan.exercises.allSatisfy { $0.isLocal })

        let legs = try #require(plan.sessions.last)
        let pullUp = try #require(legs.sets.first { $0.exercise == "pull-up-assisted" })
        #expect(pullUp.loadMode == .assisted && pullUp.assistance == Mass(value: 20, unit: .kg) && pullUp.load == nil)
        let treadmill = try #require(legs.sets.first { $0.exercise == "treadmill" })
        #expect(treadmill.distance == Length(metres: 1500) && treadmill.duration == 600)
        #expect(legs.sets.first { $0.exercise == "leg-press-machine" }?.load == Mass(value: Decimal(string: "120.5")!, unit: .kg))
    }

    @Test("pounds and miles from the header")
    func pounds() throws {
        let plan = try importPlan("workout_data_lbs")
        #expect(plan.sessions.count == 2)
        #expect(plan.sessions[0].sets[0].load == Mass(value: 135, unit: .lb))
        #expect(plan.sessions[1].sets[0].distance == Length(metres: Decimal(string: "1609.344")!))
        #expect(plan.sessions[0].session.utcOffsetStart == 0 && plan.sessions[1].session.utcOffsetStart == 60)
    }

    @Test("a workout imported, deleted, or logged in the app as well is skipped")
    func skips() throws {
        let first = try importPlan()
        let imported = first.sessions[0]
        var logged = first.sessions[1]
        logged.session.id = SessionID(UUIDv7.make(at: logged.session.at))
        logged.session.at = logged.session.at.addingTimeInterval(120)
        let again = try importPlan(history: [imported, logged])
        #expect(again.sessions.isEmpty)
        #expect(again.skipped.map(\.reason) == [.imported, .overlaps(logged.session.id)])

        let deleted = try importPlan(deleted: [imported.session.id.rawValue])
        #expect(deleted.sessions.map(\.session.id) == [first.sessions[1].session.id])
        #expect(deleted.skipped.map(\.reason) == [.deleted])
    }

    @Test("a weight the matched exercise does not record is counted, never silently lost")
    func weightsNotRecorded() throws {
        let file = try HevyCSV.read("""
        title,start_time,end_time,description,exercise_title,superset_id,exercise_notes,set_index,set_type,weight_kg,reps,distance_km,duration_seconds,rpe
        "Core","1 Nov 2026, 09:00","1 Nov 2026, 09:30","","Glute Bridge",,"",0,"normal",20,12,,,
        "Core","1 Nov 2026, 09:00","1 Nov 2026, 09:30","","Glute Bridge",,"",1,"normal",,12,,,
        "Core","1 Nov 2026, 09:00","1 Nov 2026, 09:30","","Chest Press (Machine)",,"",0,"normal",50,10,,,
        """)
        let plan = HevyImport.plan(file, exercises: try ExampleFixtures.catalog().exercises, history: [], deleted: [], timeZone: london,
                                   adapter: adapter)
        #expect(plan.matched["Glute Bridge"] == "glute-bridge")
        #expect(plan.weightsNotRecorded == ["Glute Bridge": 1])
    }

    @Test("every record encodes, decodes back and keeps to the package's kinds")
    func roundTrip() throws {
        let codec = RecordCodec()
        let plan = try importPlan()
        for records in plan.sessions {
            let flat = codec.encode(records)
            #expect(try codec.sessionRecords(from: flat) == [records])
            for record in flat {
                let schema = record.kind == codec.kinds.session ? codec.sessionSchema : codec.setSchema
                let declared = Set(schema.allProperties.map(\.name))
                #expect(Set(record.properties.keys).isSubset(of: declared), "\(record.id): \(Set(record.properties.keys).subtracting(declared))")
            }
        }
        for exercise in plan.exercises {
            var decoded = try codec.decodeExercise(codec.encode(exercise))
            decoded.isLocal = true
            #expect(decoded == exercise)
        }
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = HistoryStore(directory: directory, writingOptions: .atomic)
        for records in plan.sessions {
            try store.write(HistoryEntry(records: records, local: HistoryLocal(health: HealthSaveState(status: .notAvailable))))
        }
        let load = store.loadAll()
        #expect(load.unreadable.isEmpty)
        #expect(load.entries.map(\.records).sorted { $0.session.at < $1.session.at } == plan.sessions)
    }

    /// An owner's own export, when one is named: every row reads and every
    /// session survives the history file.
    @Test("an owner's export", .enabled(if: ProcessInfo.processInfo.environment["WORKOUTS_HEVY_CSV"] != nil))
    func ownersExport() throws {
        let path = try #require(ProcessInfo.processInfo.environment["WORKOUTS_HEVY_CSV"])
        let file = try HevyCSV.read(String(contentsOfFile: path, encoding: .utf8))
        let plan = HevyImport.plan(file, exercises: try ExampleFixtures.catalog().exercises, history: [], deleted: [], timeZone: london,
                                   adapter: adapter)
        #expect(plan.problems.isEmpty, "\(plan.problems)")
        #expect(plan.weightsNotRecorded.isEmpty, "\(plan.weightsNotRecorded)")
        #expect(plan.setCount == file.rows.count)
        let codec = RecordCodec()
        for records in plan.sessions {
            #expect(try codec.sessionRecords(from: codec.encode(records)) == [records])
        }
        print("owner's export: \(plan.sessions.count) sessions, \(plan.setCount) sets, \(plan.exercises.count) new exercises, "
            + "\(plan.matched.count) matched, \(plan.skipped.count) skipped")
    }
}
