import Foundation
import Testing

@testable import WorkoutsCore

/// `DerivedMintedID`, held to the vectors substrate's
/// `vocabulary.DerivedMintedID` is held to, so an id the app derives is the
/// id a function or the server derives from the same parts.
@Suite("Derived minted ids", .timeLimit(.minutes(10)))
struct DerivedMintedIDTests {
    @Test(
        "the shared vectors",
        arguments: [
            (["fixture", "t-1"], "3c9ts84axnhyrvws6ntpphzves"),
            (["apple", "book-1", "contact-1"], "4dn5ng7cxvgdnrk2mbvy4c9mqk"),
            (["x"], "1de4b45ds6g1282rkwn7xtrcqn"),
        ])
    func vectors(parts: [String], id: String) {
        #expect(DerivedMintedID.make(parts) == id)
    }

    @Test("the minted shape, the same id for the same parts, another for others")
    func shape() {
        let shape = /^[0-7][0-9a-hjkmnp-tv-z]{25}$/
        for index in 0 ..< 200 {
            let id = DerivedMintedID.make("workouts-hevy", "session", "hevy-\(index)")
            #expect(id.wholeMatch(of: shape) != nil, "\(id)")
        }
        #expect(DerivedMintedID.make("a", "b") == DerivedMintedID.make(["a", "b"]))
        // The parts are joined by a NUL, so moving a boundary is another id.
        #expect(DerivedMintedID.make("ab", "c") != DerivedMintedID.make("a", "bc"))
    }

    @Test("the bundled library and sample are under the ids derived from their slugs")
    func bundled() throws {
        let library = try ExerciseLibrary.bundled()
        for record in library.records {
            let slug = try #require(ExerciseLibrary.code(ExerciseLibrary.system, in: record.properties), "\(record.id)")
            let name = String(record.kind.split(separator: "/").last ?? "")
            #expect(record.id == DerivedMintedID.make("workouts-library", name, slug), "\(slug)")
        }
        let catalog = try BundledCatalog().loadNow()
        let sample = try #require(catalog.routines[SampleRoutine.id])
        #expect(SampleRoutine.id.rawValue == DerivedMintedID.make("workouts-sample", "routine", "sample-every-feature"))
        for slot in sample.slots {
            let exercise = try #require(catalog.exercises[slot.exercise], "\(slot.exercise)")
            let slug = try #require(exercise.codes.first { $0.system == SampleRoutine.system }?.code)
            #expect(exercise.id == SampleRoutine.exerciseID(slug))
        }
    }
}
