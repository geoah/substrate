import Foundation
import Testing
@testable import WorkoutsCore

/// A migration from Hevy keeps what the account holds: each workout,
/// routine and exercise by Hevy's own id, every value Hevy records, and the
/// owner's records untouched.
@Suite("Hevy migration")
struct HevyMigrationTests {
    let london = TimeZone(identifier: "Europe/London")!

    func set(_ index: Int, _ type: String = "normal", kg: Double? = nil, reps: Int? = nil, seconds: Double? = nil,
             metric: Double? = nil, range: HevyAPI.RepRange? = nil) -> HevyAPI.Set
    {
        HevyAPI.Set(index: index, type: type, weightKg: kg, reps: reps, distanceMeters: nil, durationSeconds: seconds, rpe: nil,
                    customMetric: metric, repRange: range)
    }

    func exercise(_ index: Int, _ title: String, _ template: String, superset: Int? = nil, rest: Int? = nil, notes: String? = nil,
                  _ sets: [HevyAPI.Set]) -> HevyAPI.Exercise
    {
        HevyAPI.Exercise(index: index, title: title, notes: notes, exerciseTemplateId: template, supersetId: superset, restSeconds: rest,
                         sets: sets)
    }

    func workout(_ id: String, title: String = "Push", start: String = "2026-10-30T13:43:00+00:00", end: String = "2026-10-30T14:30:00+00:00",
                 routine: String? = nil, _ exercises: [HevyAPI.Exercise]) -> HevyAPI.Workout
    {
        HevyAPI.Workout(id: id, title: title, routineId: routine, description: nil, startTime: start, endTime: end, exercises: exercises)
    }

    func plan(_ export: HevyAPI.Export, exercises: [ExerciseID: Exercise] = [:], routines: [RoutineID: Routine] = [:],
              history: [SessionRecords] = [], pairs: HevyImport.PairedWeights = .each) throws -> HevyImport.Plan
    {
        HevyImport.plan(export, exercises: exercises, routines: routines, history: history, deleted: [], timeZone: london, adapter: "x@1",
                        library: try ExerciseLibrary.bundled().catalog().exercises, pairs: pairs)
    }

    let press = "7EB3F7C3" // Chest Press (Machine) in Hevy

    @Test("two workouts in one minute are two sessions, and an import in any order finds what it wrote")
    func workoutIdentity() throws {
        let a = workout("A-1", [exercise(0, "Chest Press (Machine)", press, [set(0, kg: 50, reps: 10)])])
        let b = workout("B-2", [exercise(0, "Chest Press (Machine)", press, [set(0, kg: 55, reps: 8)])])
        let both = try plan(HevyAPI.Export(workouts: [a, b]))
        #expect(both.sessions.map(\.session.id.rawValue).sorted() == ["hevy-a-1", "hevy-b-2"])
        let first = try plan(HevyAPI.Export(workouts: [a]))
        let again = try plan(HevyAPI.Export(workouts: [b, a]), history: first.sessions)
        #expect(again.sessions.map(\.session.id.rawValue) == ["hevy-b-2"])
        #expect(again.skipped.map(\.reason) == [.imported])
    }

    @Test("the API's instant is kept through the autumn hour that repeats")
    func daylightSaving() throws {
        let w = workout("D", start: "2026-10-25T01:30:00+00:00", end: "2026-10-25T02:10:00+00:00",
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
        #expect(first.routines.map(\.id.rawValue) == ["hevy-r1", "hevy-r2"])
        #expect(first.sessions.first?.session.routine == "hevy-r2")
        var held = owners
        for routine in first.routines { held[routine.id] = routine }
        let again = try plan(HevyAPI.Export(workouts: [w], routines: [one, two]), routines: held)
        #expect(again.routines.isEmpty && again.routinesHeld == ["Push", "Push"])
    }

    @Test("two of the account's exercises with one name are two exercises, each with its own sets")
    func templateIdentity() throws {
        let reps = HevyAPI.Template(id: "c-1", title: "Press", type: "reps_only", primaryMuscleGroup: "chest", secondaryMuscleGroups: [],
                                    equipment: "none", isCustom: true)
        let loaded = HevyAPI.Template(id: "c-2", title: "Press", type: "weight_reps", primaryMuscleGroup: "shoulders",
                                      secondaryMuscleGroups: [], equipment: "dumbbell", isCustom: true)
        let w = workout("W", [exercise(0, "Press", "c-1", [set(0, reps: 12)]), exercise(1, "Press", "c-2", [set(0, kg: 50, reps: 8)])])
        let result = try plan(HevyAPI.Export(workouts: [w], templates: ["c-1": reps, "c-2": loaded]))
        #expect(Set(result.exercises.map(\.id.rawValue)) == ["press", "press-2"])
        let loadedID = try #require(result.exercises.first { $0.codes.contains(Coding(system: "hevy", code: "c-2")) }?.id)
        #expect(result.sessions.first?.sets.first { $0.exercise == loadedID }?.load == Mass(value: 50, unit: .kg))
    }

    @Test("an account's own exercise comes in even when no workout or routine names it")
    func unusedTemplate() throws {
        let idle = HevyAPI.Template(id: "c-9", title: "Neck Curl", type: "reps_only", primaryMuscleGroup: "neck", secondaryMuscleGroups: [],
                                    equipment: "none", isCustom: true)
        let result = try plan(HevyAPI.Export(templates: ["c-9": idle]))
        #expect(result.exercises.map(\.name) == ["Neck Curl"])
    }

    @Test("a routine keeps its rep ranges, a drop's reps and step, and a rest of none")
    func routineTargets() throws {
        let routine = HevyAPI.Routine(id: "R", title: "Push", folderId: nil, notes: nil, exercises: [
            exercise(0, "Chest Press (Machine)", press, rest: 0, [
                set(0, kg: 60, range: HevyAPI.RepRange(start: 8, end: 12)),
                set(1, "dropset", kg: 45, reps: 10),
                set(2, "dropset", kg: 30, reps: 12),
            ]),
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
        let some = workout("S", [
            exercise(0, "Chest Press (Machine)", press, notes: "seat 4", []),
            exercise(1, "Stair Machine (Floors)", "STAIRS", [set(0, seconds: 600, metric: 42)]),
        ])
        let result = try plan(HevyAPI.Export(workouts: [empty, some]))
        #expect(result.sessions.count == 2)
        let blank = try #require(result.sessions.first { $0.session.id == "hevy-e" })
        #expect(blank.session.slots.isEmpty && blank.sets.isEmpty)
        let full = try #require(result.sessions.first { $0.session.id == "hevy-s" })
        #expect(full.session.slots.first?.notes == "seat 4" && full.sets.allSatisfy { $0.exercise != full.session.slots[0].exercise })
        #expect(full.sets.first?.notes == "42 floors (from Hevy)")
    }

    @Test("a pair of dumbbells logged together is halved onto one, as the library counts it")
    func pairedWeights() throws {
        let library = try ExerciseLibrary.bundled().catalog().exercises
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
            HevyAPI.BodyMeasurement(id: 7, date: "2026-10-01", weightKg: 71.029, leanMassKg: nil, fatPercent: 13.8, neckCm: nil, shoulderCm: nil,
                                    chestCm: nil, leftBicepCm: nil, rightBicepCm: nil, leftForearmCm: nil, rightForearmCm: nil, abdomen: nil,
                                    waist: 85.1, hips: nil, leftThigh: nil, rightThigh: nil, leftCalf: nil, rightCalf: nil),
            HevyAPI.BodyMeasurement(id: 8, date: "2026-11-01", weightKg: 70, leanMassKg: nil, fatPercent: nil, neckCm: nil, shoulderCm: nil,
                                    chestCm: nil, leftBicepCm: nil, rightBicepCm: nil, leftForearmCm: nil, rightForearmCm: nil, abdomen: nil,
                                    waist: nil, hips: nil, leftThigh: nil, rightThigh: nil, leftCalf: nil, rightCalf: nil),
        ]
        let w = workout("M", [exercise(0, "Chest Press (Machine)", press, [set(0, kg: 50, reps: 10)])])
        let result = try plan(HevyAPI.Export(workouts: [w], measurements: entries))
        #expect(result.measurements.map(\.id) == ["hevy-7", "hevy-8"])
        let first = try #require(result.measurements.first).properties
        #expect(first["bodyMass"] == .object(["value": .string("71.029"), "unit": .string("kg")]))
        #expect(first["bodyFat"] == .string("13.8") && first["waist"] == .object(["value": .string("85.1"), "unit": .string("cm")]))
        #expect(result.sessions.first?.session.bodyMass == Mass(value: Decimal(string: "71.029")!, unit: .kg))
    }

    @Test("a CSV title finds the library exercise the owner renamed, at its id, instead of making a second")
    func renamedLibraryExercise() throws {
        let library = try ExerciseLibrary.bundled().catalog().exercises
        var renamed = try #require(library["lat-pulldown-cable"])
        renamed.name = "Pulldown, the blue cable"
        let file = try HevyCSV.read("""
        title,start_time,end_time,description,exercise_title,superset_id,exercise_notes,set_index,set_type,weight_kg,reps,distance_km,duration_seconds,rpe
        "Pull","1 Nov 2026, 09:00","1 Nov 2026, 10:00","","Lat Pulldown (Cable)",,"",0,"normal",40,12,,,
        """)
        let result = HevyImport.plan(file, exercises: [renamed.id: renamed], history: [], deleted: [], timeZone: london, adapter: "x@1",
                                     library: library)
        #expect(result.exercises.isEmpty)
        #expect(result.sessions.first?.sets.first?.exercise == "lat-pulldown-cable")
    }

    @Test("an exercise counts from its movement's shares unless its own group lists say otherwise")
    func ownListsWin() throws {
        var catalog = try ExerciseLibrary.bundled().catalog()
        var press = try #require(catalog.exercises["chest-press-machine"])
        #expect(!press.inheritedMuscles.isEmpty)
        press.primaryMuscles = [.triceps]
        press.secondaryMuscles = []
        catalog.exercises[press.id] = press
        catalog.resolveMovements()
        let mine = try #require(catalog.exercises["chest-press-machine"])
        #expect(mine.inheritedMuscles.isEmpty && mine.groupShares == [.triceps: 1])
    }
}
