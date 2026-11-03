import Foundation
import Testing
import WorkoutsCore
@testable import WorkoutsSync

/// The exercise library's seeding rules (`LibraryInstall`): a held record
/// gains what it lacks and keeps what it has.
@Suite("Library install")
struct LibraryInstallTests {
    @Test("a held record gains the properties and codes it lacks, and keeps every value it has")
    func fill() {
        let held: JSONObject = [
            "name": .string("Chest Press (Machine)"),
            "equipment": .string("plateloaded"),
            "codes": .array([.object(["system": .string("hevy"), "code": .string("7EB3F7C3")])]),
        ]
        let library: JSONObject = [
            "name": .string("Chest Press (Machine)"),
            "equipment": .string("machine"),
            "movement": .string("substrate.reamde.dev/training/movement/chest-press"),
            "codes": .array([
                .object(["system": .string(ExerciseLibrary.system), "code": .string("chest-press-machine"), "version": .string("1")]),
                .object(["system": .string("hevy"), "code": .string("7EB3F7C3")]),
            ]),
        ]
        let patch = LibraryInstall.fill(held, from: library)
        #expect(patch["equipment"] == nil && patch["name"] == nil)
        #expect(patch["movement"] == .string("substrate.reamde.dev/training/movement/chest-press"))
        guard case let .array(codes)? = patch["codes"] else { Issue.record("no codes"); return }
        #expect(codes.count == 2)
        var filled = held
        for (key, value) in patch { filled[key] = value }
        #expect(LibraryInstall.fill(filled, from: library).isEmpty)
    }

    @Test("the repository's library version is the newest code it holds")
    func since() {
        let record: JSONObject = ["codes": .array([.object(["system": .string(ExerciseLibrary.system), "code": .string("x"), "version": .string("3")])])]
        #expect(ExerciseLibrary.since(record) == 3)
        #expect(ExerciseLibrary.since(["name": .string("Owner's own")]) == nil)
    }
}
