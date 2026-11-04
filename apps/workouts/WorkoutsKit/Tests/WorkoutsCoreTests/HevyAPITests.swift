import Foundation
import Testing
@testable import WorkoutsCore

/// An account read through Hevy's API (`Fixtures/hevy/api.json`, written by
/// hand in the API's shape): exercises named by template id, a routine with
/// a superset, rests and set types, a session linked to it, and an exercise
/// of the account's own.
@Suite("Hevy API import", .timeLimit(.minutes(10)))
struct HevyAPITests {
    let london = TimeZone(identifier: "Europe/London")!

    struct Fixture: Decodable {
        var workouts: [HevyAPI.Workout]
        var routines: [HevyAPI.Routine]
        var folders: [HevyAPI.Folder]
        var templates: [String: HevyAPI.Template]
    }

    func plan(routines held: [RoutineID: Routine] = [:]) throws -> HevyImport.Plan {
        let url = try #require(Bundle.module.url(forResource: "api", withExtension: "json", subdirectory: "Fixtures/hevy"))
        let fixture = try HevyAPI.decoder.decode(Fixture.self, from: Data(contentsOf: url))
        let export = HevyAPI.Export(workouts: fixture.workouts, routines: fixture.routines, folders: fixture.folders, templates: fixture.templates)
        return HevyImport.plan(export, exercises: [:], routines: held, history: [], deleted: [], timeZone: london, adapter: "x@1",
                               library: try ExerciseLibrary.bundled().catalog().exercises)
    }

    @Test("a workout lands at Hevy's id, linked to its routine, its exercises by template id")
    func workout() throws {
        let plan = try plan()
        let records = try #require(plan.sessions.first)
        #expect(records.session.id == "hevy-w-1")
        #expect(records.session.routine == "hevy-r-1")
        #expect(records.session.slots.map(\.exercise.rawValue) == ["chest-press-machine", "lat-pulldown-cable", "my-stretch"])
        #expect(records.session.groups.count == 1 && records.session.slots[0].group == records.session.slots[1].group)
        let set = try #require(records.sets.first { $0.exercise == "chest-press-machine" && $0.role == .working })
        #expect(set.load == Mass(value: Decimal(string: "3.757")!, unit: .kg) && set.rpe == Decimal(string: "8.5"))
    }

    @Test("a routine keeps its folder, superset, rests and set types")
    func routine() throws {
        let routine = try #require(try plan().routines.first)
        #expect(routine.id == "hevy-r-1" && routine.name == "Push Day" && routine.folder == "Strength" && routine.position == 1)
        #expect(routine.slots.map(\.exercise.rawValue) == ["chest-press-machine", "lat-pulldown-cable", "plank"])
        #expect(routine.groups == [RoutineGroup(key: "superset-1", groupKind: .superset)])
        #expect(routine.slots.map(\.group) == ["superset-1", "superset-1", nil])
        #expect(routine.slots.map(\.setRest) == [90, 0, 60])
        #expect(routine.slots[0].notes == "Pause at the chest")
        let press = routine.slots[0].sets
        #expect(press.map(\.key) == ["w1", "s1", "s2"])
        #expect(press.map(\.role) == [.warmup, .working, .working])
        #expect(press[1].load == Mass(value: 50, unit: .kg) && press[1].repsMin == 8 && press[1].repsMax == 8)
        #expect(press[2].termination == .failure && press[2].structure == .drop && press[2].drops == 1)
        let plank = routine.slots[2].sets
        #expect(plank.first?.durationMin == 45 && plank.first?.load == nil)
    }

    @Test("an exercise of the account's own comes from its template; a routine held already is left and linked")
    func ownAndHeld() throws {
        let plan = try plan()
        let own = try #require(plan.exercises.first { $0.name == "My Stretch" })
        #expect(own.measurement == .repsonly && own.equipment == Equipment.none)
        #expect(own.primaryMuscles == [.lowerback] && own.secondaryMuscles == [.glutes])
        #expect(own.codes == [Coding(system: "hevy", code: "11111111-2222-3333-4444-555555555555")])
        #expect(plan.exercises.first { $0.id == "chest-press-machine" }?.movement == "chest-press")

        let held = Routine(id: "hevy-r-1", name: "Push Day, edited")
        let again = try self.plan(routines: ["hevy-r-1": held])
        #expect(again.routines.isEmpty && again.routinesHeld == ["Push Day"])
        #expect(again.sessions.first?.session.routine == "hevy-r-1")
        // An owner's own routine of the same name is another routine.
        let owners = try self.plan(routines: ["push-day": Routine(id: "push-day", name: "Push Day")])
        #expect(owners.routines.map(\.id) == ["hevy-r-1"] && owners.routinesHeld.isEmpty)
    }

    @Test("every routine and session encodes, decodes back and keeps to the package's kinds")
    func roundTrip() throws {
        let codec = RecordCodec()
        let plan = try plan()
        for routine in plan.routines {
            #expect(try codec.decodeRoutine(codec.encode(routine)) == routine)
        }
        for records in plan.sessions {
            #expect(try codec.sessionRecords(from: codec.encode(records)) == [records])
        }
    }
}
