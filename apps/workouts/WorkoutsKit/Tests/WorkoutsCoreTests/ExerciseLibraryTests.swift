import Foundation
import Testing

@testable import WorkoutsCore

/// The bundled library, decoded once for the whole run. It is a megabyte of
/// records: decoded again in every test that reads it, it kept every thread
/// of a four-core Linux runner busy for over a minute, long enough for the
/// controller tests waiting beside them to time out.
enum Library {
    static let bundled = Result { try ExerciseLibrary.bundled() }
    static let catalog = Result { try bundled.get().catalog() }

    /// The id of the library's exercise of `slug`, derived from it.
    static func exercise(_ slug: String) -> ExerciseID { ExerciseID(DerivedMintedID.make("workouts-library", "exercise", slug)) }

    /// The id of the library's movement of `slug`.
    static func movement(_ slug: String) -> MovementID { MovementID(DerivedMintedID.make("workouts-library", "movement", slug)) }
}

/// The exercise library this build carries (`Resources/Bundled/library.json`,
/// from `apps/workouts/scripts/exercise-library.py`).
@Suite("Exercise library", .timeLimit(.minutes(10)))
struct ExerciseLibraryTests {
    let codec = RecordCodec()

    @Test("every record decodes, movements before the exercises that name them")
    func decodes() throws {
        let library = try Library.bundled.get()
        #expect(library.version >= 1)
        var movements = Set<String>()
        for record in library.records {
            switch try codec.decode(record) {
            case let .movement(movement):
                movements.insert(movement.id.rawValue)
            case let .exercise(exercise):
                let movement = try #require(exercise.movement, "\(exercise.name) names no movement")
                #expect(movements.contains(movement.rawValue), "\(exercise.name) comes before its movement \(movement)")
            default:
                Issue.record("\(record.kind)/\(record.id) is neither a movement nor an exercise")
            }
            #expect(ExerciseLibrary.since(record.properties) != nil, "\(record.id) carries no library code")
            #expect(codec.encode(try codec.decode(record)).properties == record.properties, "\(record.id) does not round-trip")
        }
    }

    @Test("a movement's group lists are the ones its muscles imply, as the app reckons them")
    func groupLists() throws {
        let catalog = try Library.catalog.get()
        let shared = catalog.movements.values.filter { !$0.muscles.isEmpty }
        #expect(shared.count >= 300)
        for movement in shared {
            let lists = Exercise.groupLists(movement.muscles)
            #expect(lists.primary == movement.primaryMuscles && lists.secondary == movement.secondaryMuscles, "\(movement.name)")
            #expect(movement.muscles.allSatisfy { $0.muscle != nil && $0.involvement > 0 && $0.involvement <= 1 }, "\(movement.name)")
        }
    }

    @Test("an exercise counts from its movement's muscles unless it names its own")
    func inherited() throws {
        let catalog = try Library.catalog.get()
        let press = try #require(catalog.exercises[Library.exercise("chest-press-machine")])
        #expect(press.movement == Library.movement("chest-press") && press.muscles.isEmpty)
        #expect(press.groupShares[.chest] == 1 && press.groupShares[.triceps] == Decimal(string: "0.5"))
        let cable = try #require(catalog.exercises[Library.exercise("chest-press-cable")])
        #expect(cable.movement == press.movement)
        // Without its movement it falls back to the two group lists.
        var alone = press
        alone.inheritedMuscles = []
        #expect(alone.groupShares[.chest] == 1 && alone.groupShares[.shoulders] == Decimal(string: "0.5"))
    }

    @Test("an owner's Hevy exercises resolve to library exercises by name")
    func hevyNames() throws {
        let library = try Library.catalog.get().exercises
        let file = try HevyCSV.read(
            """
            title,start_time,end_time,description,exercise_title,superset_id,exercise_notes,set_index,set_type,weight_kg,reps,distance_km,duration_seconds,rpe
            "Push","1 Nov 2026, 09:00","1 Nov 2026, 10:00","","Lateral Raise (Dumbbell)",,"",0,"normal",8,15,,,
            "Push","1 Nov 2026, 09:00","1 Nov 2026, 10:00","","Bulgarian Split Squat",,"",0,"normal",12,10,,,
            "Push","1 Nov 2026, 09:00","1 Nov 2026, 10:00","","Cat Camel",,"",0,"normal",,10,,,
            """)
        let plan = HevyImport.plan(
            file, exercises: [:], history: [], deleted: [], timeZone: TimeZone(identifier: "Europe/London")!,
            adapter: "x@1", library: library)
        let byName = Dictionary(uniqueKeysWithValues: plan.exercises.map { ($0.name, $0) })
        let raise = try #require(byName["Lateral Raise (Dumbbell)"])
        #expect(raise.movement == Library.movement("lateral-raise") && raise.loadBasis == .perimplement && raise.implements == 2)
        #expect(byName["Bulgarian Split Squat (Dumbbell)"]?.id == Library.exercise("bulgarian-split-squat-dumbbell"))
        #expect(byName["Cat Camel"]?.movement == nil)
        let set = try #require(plan.sessions.first?.sets.first { $0.exercise == raise.id })
        #expect(set.loadBasis == .perimplement && set.implements == 2)
    }
}
