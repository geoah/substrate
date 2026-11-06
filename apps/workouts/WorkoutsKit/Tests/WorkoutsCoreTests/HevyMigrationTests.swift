import Foundation
import Testing

@testable import WorkoutsCore

/// A migration from Hevy keeps what the account holds: each workout,
/// routine and exercise by Hevy's own id, every value Hevy records, and the
/// owner's records untouched.
@Suite("Hevy migration", .timeLimit(.minutes(10)))
struct HevyMigrationTests {
    let london = TimeZone(identifier: "Europe/London")!

    func set(
        _ index: Int, _ type: String = "normal", kg: Double? = nil, reps: Int? = nil, seconds: Double? = nil,
        metric: Double? = nil, range: HevyAPI.RepRange? = nil
    ) -> HevyAPI.Set {
        HevyAPI.Set(
            index: index, type: type, weightKg: kg, reps: reps, distanceMeters: nil, durationSeconds: seconds, rpe: nil,
            customMetric: metric, repRange: range)
    }

    func exercise(
        _ index: Int, _ title: String, _ template: String, superset: Int? = nil, rest: Int? = nil, notes: String? = nil,
        _ sets: [HevyAPI.Set]
    ) -> HevyAPI.Exercise {
        HevyAPI.Exercise(
            index: index, title: title, notes: notes, exerciseTemplateId: template, supersetId: superset, restSeconds: rest,
            sets: sets)
    }

    func workout(
        _ id: String, title: String = "Push", start: String = "2026-10-30T13:43:00+00:00", end: String = "2026-10-30T14:30:00+00:00",
        routine: String? = nil, _ exercises: [HevyAPI.Exercise]
    ) -> HevyAPI.Workout {
        HevyAPI.Workout(id: id, title: title, routineId: routine, description: nil, startTime: start, endTime: end, exercises: exercises)
    }

    func plan(
        _ export: HevyAPI.Export, exercises: [ExerciseID: Exercise] = [:], routines: [RoutineID: Routine] = [:],
        history: [SessionRecords] = [], pairs: HevyImport.PairedWeights = .each
    ) throws -> HevyImport.Plan {
        HevyImport.plan(
            export, exercises: exercises, routines: routines, history: history, deleted: [], timeZone: london, adapter: "x@1",
            library: try Library.catalog.get().exercises, pairs: pairs)
    }

    let press = "7EB3F7C3"  // Chest Press (Machine) in Hevy

    /// The id an import derives for a record of `kind` from Hevy's key.
    func hevy(_ kind: String, _ key: String) -> String { HevyImport.derivedID(kind, key) }

    @Test("two workouts in one minute are two sessions, and an import in any order finds what it wrote")
    func workoutIdentity() throws {
        let a = workout("A-1", [exercise(0, "Chest Press (Machine)", press, [set(0, kg: 50, reps: 10)])])
        let b = workout("B-2", [exercise(0, "Chest Press (Machine)", press, [set(0, kg: 55, reps: 8)])])
        let both = try plan(HevyAPI.Export(workouts: [a, b]))
        #expect(Set(both.sessions.map(\.session.id.rawValue)) == [hevy("session", "hevy-a-1"), hevy("session", "hevy-b-2")])
        let first = try plan(HevyAPI.Export(workouts: [a]))
        let again = try plan(HevyAPI.Export(workouts: [b, a]), history: first.sessions)
        #expect(again.sessions.map(\.session.id.rawValue) == [hevy("session", "hevy-b-2")])
        #expect(again.skipped.map(\.reason) == [.imported])
    }

    @Test("an import run again lands on the same ids, and finds every session, routine and measurement it wrote")
    func reimport() throws {
        let routine = HevyAPI.Routine(id: "R1", title: "Push", folderId: nil, notes: nil, exercises: [exercise(0, "Chest Press (Machine)", press, [set(0)])])
        let w = workout("A-1", routine: "R1", [exercise(0, "Chest Press (Machine)", press, [set(0, kg: 50, reps: 10), set(1, kg: 55, reps: 8)])])
        let mass = HevyAPI.BodyMeasurement(
            id: 7, date: "2026-10-01", weightKg: 71, leanMassKg: nil, fatPercent: nil, neckCm: nil, shoulderCm: nil, chestCm: nil,
            leftBicepCm: nil, rightBicepCm: nil, leftForearmCm: nil, rightForearmCm: nil, abdomen: nil, waist: nil, hips: nil,
            leftThigh: nil, rightThigh: nil, leftCalf: nil, rightCalf: nil)
        let export = HevyAPI.Export(workouts: [w], routines: [routine], measurements: [mass])
        let first = try plan(export)
        let twice = try plan(export)
        #expect(twice.sessions.map(\.session.id) == first.sessions.map(\.session.id))
        #expect(twice.sessions.flatMap(\.sets).map(\.id) == first.sessions.flatMap(\.sets).map(\.id))
        #expect(twice.routines.map(\.id) == first.routines.map(\.id))
        #expect(twice.measurements.map(\.id) == first.measurements.map(\.id))
        let shape = /^[0-7][0-9a-hjkmnp-tv-z]{25}$/
        let ids =
            first.sessions.map(\.session.id.rawValue) + first.sessions.flatMap(\.sets).map(\.id.rawValue)
            + first.routines.map(\.id.rawValue) + first.measurements.map(\.id)
        #expect(ids.allSatisfy { $0.wholeMatch(of: shape) != nil }, "\(ids)")

        // Imported once: nothing new, the session skipped and the routine held.
        var routines: [RoutineID: Routine] = [:]
        for routine in first.routines { routines[routine.id] = routine }
        let again = try plan(export, routines: routines, history: first.sessions)
        #expect(again.sessions.isEmpty && again.skipped.map(\.reason) == [.imported])
        #expect(again.routines.isEmpty && again.routinesHeld == ["Push"])
        // A measurement is written again where it is: the same id.
        #expect(again.measurements.map(\.id) == first.measurements.map(\.id))
    }

    @Test("what an import wrote before the ids were derived is still its own: the session, a deleted one, a measurement")
    func legacyRows() throws {
        let a = workout("A-1", [exercise(0, "Chest Press (Machine)", press, [set(0, kg: 50, reps: 10)])])
        var old = try #require(try plan(HevyAPI.Export(workouts: [a])).sessions.first)
        old.session.id = "hevy-a-1"
        old.session.adapter = "substrate-workouts@0.9.0"
        let held = try plan(HevyAPI.Export(workouts: [a]), history: [old])
        #expect(held.sessions.isEmpty && held.skipped.map(\.reason) == [.imported])
        let deleted = HevyImport.plan(
            HevyAPI.Export(workouts: [a]), exercises: [:], routines: [:], history: [], deleted: ["hevy-a-1"], timeZone: london,
            adapter: "x@1", library: try Library.catalog.get().exercises)
        #expect(deleted.sessions.isEmpty && deleted.skipped.map(\.reason) == [.deleted])

        // A measurement held under its old id is written there again.
        let legacy = HevyImport.legacyIDs("bodymeasurement", held: ["hevy-7", "0190f3a2-7c1d-7e4b-9a61-2c7d5e8f4b10"])
        #expect(legacy == [hevy("bodymeasurement", "hevy-7"): "hevy-7"])
    }

    @Test("two API workouts at one time are two, whichever ids the first was written under; a CSV workout at that time is the same")
    func apiOverlap() throws {
        let a = workout("A-1", [exercise(0, "Chest Press (Machine)", press, [set(0, kg: 50, reps: 10)])])
        let b = workout("B-2", [exercise(0, "Chest Press (Machine)", press, [set(0, kg: 55, reps: 8)])])
        let first = try #require(try plan(HevyAPI.Export(workouts: [a])).sessions.first)
        #expect(try plan(HevyAPI.Export(workouts: [b]), history: [first]).sessions.count == 1)
        var old = first
        old.session.id = "hevy-a-1"
        old.session.adapter = "substrate-workouts@0.9.0"
        #expect(try plan(HevyAPI.Export(workouts: [b]), history: [old]).sessions.count == 1)

        // The CSV's workout at the same minute is the API's, imported already.
        let file = try HevyCSV.read(
            """
            title,start_time,end_time,description,exercise_title,superset_id,exercise_notes,set_index,set_type,weight_kg,reps,distance_km,duration_seconds,rpe
            "Push","30 Oct 2026, 13:43","30 Oct 2026, 14:30","","Chest Press (Machine)",,"",0,"normal",50,10,,,
            """)
        let csv = HevyImport.plan(file, exercises: [:], history: [first], deleted: [], timeZone: london, adapter: "x@1")
        #expect(csv.sessions.isEmpty && csv.skipped.map(\.reason) == [.imported])
    }

    @Test("how a session came from Hevy: its adapter, or the id an import wrote before the ids were derived")
    func importPath() {
        func session(_ id: SessionID, _ adapter: String?) -> TrainingSession {
            TrainingSession(id: id, at: Date(timeIntervalSince1970: 0), status: .finished, adapter: adapter)
        }
        #expect(HevyImport.importAdapter("substrate-workouts@1.2.0", .api) == "substrate-workouts-hevy-api@1.2.0")
        #expect(HevyImport.importAdapter("bare", .csv) == "bare-hevy-csv")
        #expect(HevyImport.importPath(session("3c9ts84axnhyrvws6ntpphzves", "substrate-workouts-hevy-api@1.2.0")) == .api)
        #expect(HevyImport.importPath(session("3c9ts84axnhyrvws6ntpphzves", "substrate-workouts-hevy-csv@1.2.0")) == .csv)
        #expect(HevyImport.importPath(session("hevy-3f1c9a2e-8b47-4d0e-9a61-2c7d5e8f4b10", "substrate-workouts@0.9.0")) == .api)
        #expect(HevyImport.importPath(session("hevy-20261030-1343", "substrate-workouts@0.9.0")) == .csv)
        #expect(HevyImport.importPath(session("hevy-20261030-1343-2", nil)) == .csv)
        #expect(HevyImport.importPath(session("0190f3a2-7c1d-7e4b-9a61-2c7d5e8f4b10", "substrate-workouts@1.2.0")) == nil)
    }

    @Test("the API's instant is kept through the autumn hour that repeats")
    func daylightSaving() throws {
        let w = workout(
            "D", start: "2026-10-25T01:30:00+00:00", end: "2026-10-25T02:10:00+00:00",
            [exercise(0, "Chest Press (Machine)", press, [set(0, kg: 50, reps: 10)])])
        let session = try #require(try plan(HevyAPI.Export(workouts: [w])).sessions.first).session
        #expect(session.at == ISO8601DateFormatter().date(from: "2026-10-25T01:30:00Z"))
        #expect(session.endsAt == ISO8601DateFormatter().date(from: "2026-10-25T02:10:00Z"))
    }

    @Test("two routines of one name stay two, each workout linked to its own; an owner's routine of that name is untouched")
    func routineIdentity() throws {
        let one = HevyAPI.Routine(id: "R1", title: "Push", folderId: nil, notes: nil, exercises: [exercise(0, "Chest Press (Machine)", press, [set(0)])])
        let two = HevyAPI.Routine(id: "R2", title: "Push", folderId: nil, notes: nil, exercises: [exercise(0, "Chest Press (Machine)", press, [set(0)])])
        let w = workout("W", routine: "R2", [exercise(0, "Chest Press (Machine)", press, [set(0, kg: 50, reps: 10)])])
        let owners: [RoutineID: Routine] = ["push": Routine(id: "push", name: "Push")]
        let first = try plan(HevyAPI.Export(workouts: [w], routines: [one, two]), routines: owners)
        #expect(first.routines.map(\.id.rawValue) == [hevy("routine", "hevy-r1"), hevy("routine", "hevy-r2")])
        #expect(first.sessions.first?.session.routine?.rawValue == hevy("routine", "hevy-r2"))
        var held = owners
        for routine in first.routines { held[routine.id] = routine }
        let again = try plan(HevyAPI.Export(workouts: [w], routines: [one, two]), routines: held)
        #expect(again.routines.isEmpty && again.routinesHeld == ["Push", "Push"])
    }

    @Test("two of the account's exercises with one name are two exercises, each with its own sets")
    func templateIdentity() throws {
        let reps = HevyAPI.Template(
            id: "c-1", title: "Press", type: "reps_only", primaryMuscleGroup: "chest", secondaryMuscleGroups: [],
            equipment: "none", isCustom: true)
        let loaded = HevyAPI.Template(
            id: "c-2", title: "Press", type: "weight_reps", primaryMuscleGroup: "shoulders",
            secondaryMuscleGroups: [], equipment: "dumbbell", isCustom: true)
        let w = workout("W", [exercise(0, "Press", "c-1", [set(0, reps: 12)]), exercise(1, "Press", "c-2", [set(0, kg: 50, reps: 8)])])
        let result = try plan(HevyAPI.Export(workouts: [w], templates: ["c-1": reps, "c-2": loaded]))
        #expect(Set(result.exercises.map(\.id.rawValue)) == [hevy("exercise", "template-c-1"), hevy("exercise", "template-c-2")])
        let loadedID = try #require(result.exercises.first { $0.codes.contains(Coding(system: "hevy", code: "c-2")) }?.id)
        #expect(result.sessions.first?.sets.first { $0.exercise == loadedID }?.load == Mass(value: 50, unit: .kg))
    }

    @Test("an account's own exercise comes in even when no workout or routine names it")
    func unusedTemplate() throws {
        let idle = HevyAPI.Template(
            id: "c-9", title: "Neck Curl", type: "reps_only", primaryMuscleGroup: "neck", secondaryMuscleGroups: [],
            equipment: "none", isCustom: true)
        let result = try plan(HevyAPI.Export(templates: ["c-9": idle]))
        #expect(result.exercises.map(\.name) == ["Neck Curl"])
    }

    @Test("a routine keeps its rep ranges, a drop's reps and step, and a rest of none")
    func routineTargets() throws {
        let routine = HevyAPI.Routine(
            id: "R", title: "Push", folderId: nil, notes: nil,
            exercises: [
                exercise(
                    0, "Chest Press (Machine)", press, rest: 0,
                    [
                        set(0, kg: 60, range: HevyAPI.RepRange(start: 8, end: 12)),
                        set(1, "dropset", kg: 45, reps: 10),
                        set(2, "dropset", kg: 30, reps: 12),
                    ])
            ])
        let slot = try #require(try plan(HevyAPI.Export(routines: [routine])).routines.first?.slots.first)
        #expect(slot.setRest == 0)
        let head = try #require(slot.sets.first)
        #expect(slot.sets.count == 1 && head.repsMin == 8 && head.repsMax == 12)
        #expect(head.structure == .drop && head.drops == 2 && head.dropReps == 10 && head.dropStep == Mass(value: 15, unit: .kg))
    }

    @Test("an empty workout and an exercise with no sets come through, notes and all; floors are kept in the set's notes")
    func emptiesAndMetrics() throws {
        let empty = workout("E", start: "2026-10-01T08:00:00+00:00", end: "2026-10-01T08:30:00+00:00", [])
        let some = workout(
            "S",
            [
                exercise(0, "Chest Press (Machine)", press, notes: "seat 4", []),
                exercise(1, "Stair Machine (Floors)", "STAIRS", [set(0, seconds: 600, metric: 42)]),
            ])
        let result = try plan(HevyAPI.Export(workouts: [empty, some]))
        #expect(result.sessions.count == 2)
        let blank = try #require(result.sessions.first { $0.session.id.rawValue == hevy("session", "hevy-e") })
        #expect(blank.session.slots.isEmpty && blank.sets.isEmpty)
        let full = try #require(result.sessions.first { $0.session.id.rawValue == hevy("session", "hevy-s") })
        #expect(full.session.slots.first?.notes == "seat 4" && full.sets.allSatisfy { $0.exercise != full.session.slots[0].exercise })
        #expect(full.sets.first?.notes == "42 floors (from Hevy)")
    }

    @Test("a pair of dumbbells logged together is halved onto one, as the library counts it")
    func pairedWeights() throws {
        let library = try Library.catalog.get().exercises
        let dumbbell = try #require(library.values.first { $0.loadBasis == .perimplement && $0.implements == 2 && $0.codes.contains { $0.system == "hevy" } })
        let template = try #require(dumbbell.codes.first { $0.system == "hevy" }?.code)
        let w = workout("P", [exercise(0, dumbbell.name, template, [set(0, kg: 40, reps: 10)])])
        let each = try plan(HevyAPI.Export(workouts: [w]))
        #expect(each.sessions.first?.sets.first?.load == Mass(value: 40, unit: .kg))
        let together = try plan(HevyAPI.Export(workouts: [w]), pairs: .together)
        #expect(together.sessions.first?.sets.first?.load == Mass(value: 20, unit: .kg))
    }

    @Test("body measurements come in whole, and a session takes the latest body mass on or before its day")
    func bodyMeasurements() throws {
        let entries = [
            HevyAPI.BodyMeasurement(
                id: 7, date: "2026-10-01", weightKg: 71.029, leanMassKg: nil, fatPercent: 13.8, neckCm: nil, shoulderCm: nil,
                chestCm: nil, leftBicepCm: nil, rightBicepCm: nil, leftForearmCm: nil, rightForearmCm: nil, abdomen: nil,
                waist: 85.1, hips: nil, leftThigh: nil, rightThigh: nil, leftCalf: nil, rightCalf: nil),
            HevyAPI.BodyMeasurement(
                id: 8, date: "2026-11-01", weightKg: 70, leanMassKg: nil, fatPercent: nil, neckCm: nil, shoulderCm: nil,
                chestCm: nil, leftBicepCm: nil, rightBicepCm: nil, leftForearmCm: nil, rightForearmCm: nil, abdomen: nil,
                waist: nil, hips: nil, leftThigh: nil, rightThigh: nil, leftCalf: nil, rightCalf: nil),
        ]
        let w = workout("M", [exercise(0, "Chest Press (Machine)", press, [set(0, kg: 50, reps: 10)])])
        let result = try plan(HevyAPI.Export(workouts: [w], measurements: entries))
        #expect(result.measurements.map(\.id) == [hevy("bodymeasurement", "hevy-7"), hevy("bodymeasurement", "hevy-8")])
        #expect(result.measurements.allSatisfy { $0.properties["adapter"] == .string("x-hevy-api@1") })
        let first = try #require(result.measurements.first).properties
        #expect(first["bodyMass"] == .object(["value": .string("71.029"), "unit": .string("kg")]))
        #expect(first["bodyFat"] == .string("13.8") && first["waist"] == .object(["value": .string("85.1"), "unit": .string("cm")]))
        #expect(result.sessions.first?.session.bodyMass == Mass(value: Decimal(string: "71.029")!, unit: .kg))
    }

    @Test("a CSV title finds the library exercise the owner renamed, at its id, instead of making a second")
    func renamedLibraryExercise() throws {
        let library = try Library.catalog.get().exercises
        var renamed = try #require(library[Library.exercise("lat-pulldown-cable")])
        renamed.name = "Pulldown, the blue cable"
        let file = try HevyCSV.read(
            """
            title,start_time,end_time,description,exercise_title,superset_id,exercise_notes,set_index,set_type,weight_kg,reps,distance_km,duration_seconds,rpe
            "Pull","1 Nov 2026, 09:00","1 Nov 2026, 10:00","","Lat Pulldown (Cable)",,"",0,"normal",40,12,,,
            """)
        let result = HevyImport.plan(
            file, exercises: [renamed.id: renamed], history: [], deleted: [], timeZone: london, adapter: "x@1",
            library: library)
        #expect(result.exercises.isEmpty)
        #expect(result.sessions.first?.sets.first?.exercise == Library.exercise("lat-pulldown-cable"))
    }

    @Test("an exercise counts from its movement's shares unless its own group lists say otherwise")
    func ownListsWin() throws {
        var catalog = try Library.catalog.get()
        var press = try #require(catalog.exercises[Library.exercise("chest-press-machine")])
        #expect(!press.inheritedMuscles.isEmpty)
        press.primaryMuscles = [.triceps]
        press.secondaryMuscles = []
        catalog.exercises[press.id] = press
        catalog.resolveMovements()
        let mine = try #require(catalog.exercises[Library.exercise("chest-press-machine")])
        #expect(mine.inheritedMuscles.isEmpty && mine.groupShares == [.triceps: 1])
    }
}
