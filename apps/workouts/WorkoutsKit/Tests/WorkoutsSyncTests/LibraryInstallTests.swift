import Foundation
import Testing
import WorkoutsCore
@testable import WorkoutsSync
import SubstrateCore

/// The exercise library's seeding (`LibraryInstall`): the server's marker,
/// what a run writes and what it never touches.
@Suite("Library install", .timeLimit(.minutes(10)))
struct LibraryInstallTests {
    let movement = "substrate.reamde.dev/training/movement"
    let exercise = "substrate.reamde.dev/training/exercise"
    let marker = "substrate.reamde.dev/training/library"

    func code(_ id: String, since: Int) -> JSONValue {
        .object(["system": .string(ExerciseLibrary.system), "code": .string(id), "version": .string(String(since))])
    }

    /// Two movements, three exercises, as the generator writes them; `extra`
    /// adds records that first appear in `version`.
    func library(version: Int = 1, extra: Bool = false) -> ExerciseLibrary {
        var records = [
            FlatRecord(kind: "substrate.reamde.dev/training/movement", id: "chest-press", properties: [
                "name": .string("Chest Press"), "codes": .array([code("chest-press", since: 1)]),
            ]),
            FlatRecord(kind: "substrate.reamde.dev/training/movement", id: "row", properties: [
                "name": .string("Row"), "codes": .array([code("row", since: 1)]),
            ]),
            FlatRecord(kind: "substrate.reamde.dev/training/exercise", id: "chest-press-machine", properties: [
                "name": .string("Chest Press (Machine)"), "measurement": .string("weightreps"), "equipment": .string("machine"),
                "movement": .string("substrate.reamde.dev/training/movement/chest-press"),
                "codes": .array([code("chest-press-machine", since: 1), .object(["system": .string("hevy"), "code": .string("7EB3F7C3")])]),
            ]),
            FlatRecord(kind: "substrate.reamde.dev/training/exercise", id: "chest-press-dumbbell", properties: [
                "name": .string("Chest Press (Dumbbell)"), "measurement": .string("weightreps"), "loadBasis": .string("perimplement"),
                "implements": .number(2), "movement": .string("substrate.reamde.dev/training/movement/chest-press"),
                "codes": .array([code("chest-press-dumbbell", since: 1)]),
            ]),
            FlatRecord(kind: "substrate.reamde.dev/training/exercise", id: "row-cable", properties: [
                "name": .string("Row (Cable)"), "measurement": .string("weightreps"), "movement": .string("substrate.reamde.dev/training/movement/row"),
                "codes": .array([code("row-cable", since: 1)]),
            ]),
        ]
        if extra {
            records.append(FlatRecord(kind: "substrate.reamde.dev/training/exercise", id: "row-machine", properties: [
                "name": .string("Row (Machine)"), "measurement": .string("weightreps"), "movement": .string("substrate.reamde.dev/training/movement/row"),
                "codes": .array([code("row-machine", since: version)]),
            ]))
        }
        return ExerciseLibrary(version: version, records: records)
    }

    @Test("a fresh repository gets every record and the marker; a second run, on any device, writes nothing")
    func fresh() async throws {
        let memory = MemoryRecords()
        let outcome = try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(memory.api, library: library())
        #expect(outcome.written == 5 && outcome.deferred == 0)
        #expect(memory.get(marker, "exercises")?.properties["version"] == .number(1))
        #expect(memory.get(exercise, "chest-press-machine")?.properties["movement"] == .string("\(movement)/chest-press"))
        let again = try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(memory.api, library: library())
        #expect(again.current)
    }

    @Test("a record the owner deleted never comes back; a newer library writes only what appeared since")
    func deletedStaysDeleted() async throws {
        let memory = MemoryRecords()
        try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(memory.api, library: library())
        memory.delete(exercise, "row-cable")
        let next = try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic)
            .ensure(memory.api, library: library(version: 2, extra: true))
        #expect(next.written == 1)
        #expect(memory.get(exercise, "row-cable") == nil && memory.get(exercise, "row-machine") != nil)
        #expect(memory.get(marker, "exercises")?.properties["version"] == .number(2))
    }

    @Test("an import's library exercise on the server before any seeding holds nothing back")
    func importFirst() async throws {
        let memory = MemoryRecords()
        memory.put(exercise, "chest-press-machine", ["name": .string("Chest Press (Machine)"), "measurement": .string("weightreps"),
                                                     "codes": .array([code("chest-press-machine", since: 1)])])
        let outcome = try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(memory.api, library: library())
        #expect(outcome.written == 4)
        #expect(memory.all(exercise).count == 3 && memory.all(movement).count == 2)
    }

    @Test("a run cut short writes no marker, and the next run, on another device, finishes it")
    func interrupted() async throws {
        let memory = MemoryRecords()
        memory.refuse = { method, _, id in method == "PUT" && id == "row-cable" ? 503 : nil }
        await #expect(throws: (any Error).self) {
            try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(memory.api, library: library())
        }
        #expect(memory.get(marker, "exercises")?.properties["version"] == nil)
        #expect(memory.get(marker, "exercises")?.properties["seeding"] == .number(1))
        memory.refuse = nil
        let rest = try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(memory.api, library: library())
        #expect(rest.written == 1 && memory.get(exercise, "row-cable") != nil)
        #expect(memory.get(marker, "exercises")?.properties["version"] == .number(1))
        #expect(memory.get(marker, "exercises")?.properties["seeding"] == nil)
    }

    @Test("a repository an earlier build seeded without a marker is not seeded again")
    func legacy() async throws {
        let memory = MemoryRecords()
        try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(memory.api, library: library())
        memory.delete(marker, "exercises")
        memory.delete(exercise, "row-cable")
        let again = try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(memory.api, library: library())
        #expect(again.written == 0 && memory.get(exercise, "row-cable") == nil)
    }

    @Test("an owner's record at a library id: a different exercise is left alone; the same one gains only what it is")
    func ownersRecords() async throws {
        let memory = MemoryRecords()
        memory.put(exercise, "chest-press-dumbbell", ["name": .string("Chest Press (Dumbbell)"), "measurement": .string("weightreps")])
        memory.put(exercise, "row-cable", ["name": .string("My own cable row variant"), "measurement": .string("repsonly")])
        try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(memory.api, library: library())
        let same = try #require(memory.get(exercise, "chest-press-dumbbell")).properties
        #expect(same["movement"] == .string("\(movement)/chest-press"))
        #expect(same["loadBasis"] == nil && same["implements"] == nil, "a fill changed how its sets count")
        #expect(ExerciseLibrary.since(same) == 1)
        let other = try #require(memory.get(exercise, "row-cable")).properties
        #expect(other["movement"] == nil && other["codes"] == nil && other["measurement"] == .string("repsonly"))
    }

    @Test("a record created at a library id while a run is under way is never merged into, and the run is finished later")
    func createRace() async throws {
        let memory = MemoryRecords()
        let raced = Locked(false)
        memory.refuse = { [memory] method, kind, id in
            if method == "PUT", id == "row-cable", !raced.value {
                raced.value = true
                memory.put(kind, id, ["name": .string("Row (Cable)"), "measurement": .string("weightreps"), "notes": .string("the coach's")])
            }
            return nil
        }
        let outcome = try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(memory.api, library: library())
        #expect(outcome.deferred == 1 && memory.get(marker, "exercises")?.properties["version"] == nil)
        #expect(memory.get(exercise, "row-cable")?.properties["notes"] == .string("the coach's"))
        memory.refuse = nil
        let next = try await LibraryInstall(directory: temporaryDirectory(), writingOptions: .atomic).ensure(memory.api, library: library())
        #expect(next.filled == 1 && memory.get(marker, "exercises") != nil)
        #expect(memory.get(exercise, "row-cable")?.properties["notes"] == .string("the coach's"))
    }

    @Test("a held record gains only what it is, once: a value the owner clears stays cleared")
    func fill() {
        let held: JSONObject = ["name": .string("Chest Press (Machine)"), "equipment": .string("plateloaded")]
        let library: JSONObject = [
            "name": .string("Chest Press (Machine)"), "equipment": .string("machine"), "laterality": .string("unilateral"),
            "movement": .string("substrate.reamde.dev/training/movement/chest-press"), "codes": .array([code("chest-press-machine", since: 1)]),
        ]
        let patch = LibraryInstall.fill(held, from: library)
        #expect(Set(patch.keys) == ["movement", "codes"])
        var filled = held
        for (key, value) in patch { filled[key] = value }
        filled["movement"] = nil
        #expect(LibraryInstall.fill(filled, from: library).isEmpty)
    }

    @Test("the repository's library version is the newest code it holds")
    func since() {
        let record: JSONObject = ["codes": .array([code("x", since: 3)])]
        #expect(ExerciseLibrary.since(record) == 3)
        #expect(ExerciseLibrary.since(["name": .string("Owner's own")]) == nil)
    }
}
