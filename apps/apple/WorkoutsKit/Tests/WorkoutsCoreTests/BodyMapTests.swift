import Foundation
import Testing
@testable import WorkoutsCore

/// The body map's regions (`BodyMap.sets`): each set counted by the largest
/// share among a region's muscles, its movement's when the exercise names
/// none, else by its groups.
@Suite("Body map")
struct BodyMapTests {
    func set(_ exercise: String, role: Role = .working, continues: SetID? = nil) -> PerformedSet {
        PerformedSet(id: SetID(UUID().uuidString), at: Date(timeIntervalSince1970: 0), session: "s", exercise: ExerciseID(exercise), slot: exercise,
                     order: 1, role: role, continues: continues)
    }

    @Test("a library exercise shades its movement's regions by share; warm-ups and drops add nothing")
    func library() throws {
        let exercises = try ExerciseLibrary.bundled().catalog().exercises
        let sets = [set("chest-press-machine"), set("chest-press-machine"), set("chest-press-machine", role: .warmup),
                    set("chest-press-machine", continues: "head")]
        let regions = BodyMap.sets(sets, exercises: exercises)
        #expect(regions[.chest] == 2)
        #expect(regions[.frontDelts] == 1 && regions[.triceps] == 1)
        #expect(regions[.lats] == nil && regions[.quadriceps] == nil)
    }

    @Test("two muscles of one region never count one set twice")
    func largestShare() {
        let exercise = Exercise(id: "skull", name: "Skull", measurement: .weightreps,
                                muscles: [MuscleShare(.tricepslong, 1), MuscleShare(.tricepslateral, 1), MuscleShare(.forearmflexors, Decimal(string: "0.25")!)])
        let regions = BodyMap.sets([set("skull")], exercises: ["skull": exercise])
        #expect(regions[.triceps] == 1 && regions[.forearms] == Decimal(string: "0.25"))
    }

    @Test("an exercise that names only groups shades their regions, primary 1 and secondary 0.5")
    func groupsOnly() {
        let exercise = Exercise(id: "row", name: "Row", measurement: .weightreps, primaryMuscles: [.upperback], secondaryMuscles: [.biceps])
        let regions = BodyMap.sets([set("row")], exercises: ["row": exercise])
        #expect(regions[.lats] == 1 && regions[.biceps] == Decimal(string: "0.5"))
        #expect(regions[.tibialis] == nil)
    }

    @Test("every region is drawn on a figure, and every muscle shades a region")
    func coverage() {
        #expect(BodyRegion.allCases.allSatisfy { !$0.shapes.isEmpty })
        let shaded = Set(BodyRegion.allCases.flatMap(\.muscles))
        #expect(Set(MuscleDetail.allCases).subtracting(shaded).isEmpty, "\(Set(MuscleDetail.allCases).subtracting(shaded))")
    }
}
