import Foundation
import Testing

@testable import WorkoutsCore

/// The body map's regions (`BodyMap.sets`): each set counted by the largest
/// share among a region's muscles, its movement's when the exercise names
/// none, else by its groups.
@Suite("Body map", .timeLimit(.minutes(10)))
struct BodyMapTests {
    func set(_ exercise: String, role: Role = .working, continues: SetID? = nil) -> PerformedSet {
        PerformedSet(
            id: SetID(UUID().uuidString), at: Date(timeIntervalSince1970: 0), session: "s", exercise: ExerciseID(exercise), slot: exercise,
            order: 1, role: role, continues: continues)
    }

    @Test("a library exercise shades its movement's regions by share; warm-ups and drops add nothing")
    func library() throws {
        let exercises = try Library.catalog.get().exercises
        let press = Library.exercise("chest-press-machine").rawValue
        let sets = [set(press), set(press), set(press, role: .warmup), set(press, continues: "head")]
        let regions = BodyMap.sets(sets, exercises: exercises)
        #expect(regions[.chest] == 2)
        #expect(regions[.frontDelts] == 1 && regions[.triceps] == 1)
        #expect(regions[.lats] == nil && regions[.quadriceps] == nil)
    }

    @Test("two muscles of one region never count one set twice")
    func largestShare() {
        let exercise = Exercise(
            id: "skull", name: "Skull", measurement: .weightreps,
            muscles: [MuscleShare(.tricepslong, 1), MuscleShare(.tricepslateral, 1), MuscleShare(.forearmflexors, Decimal(string: "0.25")!)])
        let regions = BodyMap.sets([set("skull")], exercises: ["skull": exercise])
        #expect(regions[.triceps] == 1 && regions[.forearms] == Decimal(string: "0.25"))
    }

    @Test("an exercise that names only groups shades their regions, primary 1 and secondary 0.5")
    func groupsOnly() {
        let exercise = Exercise(id: "row", name: "Row", measurement: .weightreps, primaryMuscles: [.upperback], secondaryMuscles: [.biceps])
        let regions = BodyMap.sets([set("row")], exercises: ["row": exercise])
        // The upper back is the rhomboids and the middle traps, not the lats.
        #expect(regions[.traps] == 1 && regions[.biceps] == Decimal(string: "0.5"))
        #expect(regions[.lats] == nil && regions[.tibialis] == nil)
    }

    @Test("an exercise shades the same regions whether it names its muscles or only their groups")
    func pathsAgree() {
        let named = Exercise(
            id: "named", name: "Named", measurement: .weightreps,
            muscles: [
                MuscleShare(.rhomboids, 1), MuscleShare(.trapsmiddle, 1), MuscleShare(.iliopsoas, 1),
                MuscleShare(.serratus, 1),
            ])
        let grouped = Exercise(id: "grouped", name: "Grouped", measurement: .weightreps, primaryMuscles: [.upperback, .other, .chest])
        let a = BodyMap.sets([set("named")], exercises: ["named": named])
        let b = BodyMap.sets([set("grouped")], exercises: ["grouped": grouped])
        #expect(a == [.traps: 1, .chest: 1] && a == b)
        // For every group but the shin's, its muscles' regions are the regions it shades alone.
        for group in Muscle.allCases {
            let muscles = MuscleDetail.allCases.filter { $0.group == group && $0 != .tibialis }
            let byMuscle = Set(BodyRegion.allCases.filter { !Set($0.muscles).isDisjoint(with: muscles) })
            let byGroup = Set(BodyRegion.allCases.filter { $0.groups.contains(group) })
            #expect(byMuscle == byGroup, "\(group): \(byMuscle) against \(byGroup)")
        }
    }

    @Test("every region is drawn on a figure, and every muscle a figure draws shades a region")
    func coverage() {
        #expect(BodyRegion.allCases.allSatisfy { !$0.shapes.isEmpty })
        let shaded = Set(BodyRegion.allCases.flatMap(\.muscles))
        // The hip flexors lie under the quadriceps, where neither figure draws them.
        #expect(Set(MuscleDetail.allCases).subtracting(shaded) == [.iliopsoas])
    }
}
