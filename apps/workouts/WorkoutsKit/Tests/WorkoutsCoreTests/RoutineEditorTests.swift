import Foundation
import Testing

@testable import WorkoutsCore

/// The routine editor: rename, order (blocks, members, sets), groups made
/// and undone, exercises and sets added and removed, keys never reused, the
/// PATCH it asks for, the duplicate and the draft kept on disk.
@Suite("Routine editor", .timeLimit(.minutes(10)))
struct RoutineEditorTests {
    let codec = RecordCodec()

    /// warm (plain), a1 a2 a3 (tri-set a), solo (plain), b1 b2 (paired b).
    static func routine() -> Routine {
        func slot(_ key: String, group: String? = nil, sets: [PlannedSet]? = nil) -> RoutineSlot {
            RoutineSlot(
                key: key, exercise: ExerciseID(key), group: group,
                sets: sets ?? [PlannedSet(key: "s1", repsMin: 8, repsMax: 12), PlannedSet(key: "s2", repsMin: 8, repsMax: 12)],
                extra: ["coachTag": .string("slot \(key)")])
        }
        return Routine(
            id: "day-1", version: 4, name: "Day 1", folder: "Programme", notes: "Easy does it.",
            groups: [
                RoutineGroup(key: "a", groupKind: .triset, rounds: 2, roundRest: 60, extra: ["coachTag": .string("group a")]),
                RoutineGroup(key: "b", groupKind: .paired, pairing: .antagonist),
            ],
            slots: [
                slot("warm", sets: [PlannedSet(key: "w1", role: .warmup, durationMin: 180, durationMax: 180)]),
                slot("a1", group: "a"), slot("a2", group: "a"), slot("a3", group: "a"),
                slot("solo"),
                slot("b1", group: "b"), slot("b2", group: "b"),
            ],
            extra: ["coachPlan": .string("phase 1")]
        )
    }

    func keys(_ editor: RoutineEditor) -> [String] { editor.routine.slots.map(\.key) }

    @Test("blocks read a group as one block, in slot order")
    func blocks() {
        let editor = RoutineEditor(Self.routine())
        #expect(editor.blocks.map(\.id) == ["slot-warm", "group-a", "slot-solo", "group-b"])
        #expect(editor.blocks[1].slots == ["a1", "a2", "a3"])
    }

    @Test("rename, folder and notes; an emptied field is cleared with null")
    func rename() {
        var editor = RoutineEditor(Self.routine())
        editor.rename("Day 1 – Floor")
        editor.setFolder("")
        editor.setNotes("New notes")
        #expect(editor.routine.name == "Day 1 – Floor")
        #expect(editor.routine.folder == nil)
        let patch = editor.patch(codec: codec)
        #expect(patch["name"] == .string("Day 1 – Floor"))
        #expect(patch["folder"] == .null)
        #expect(patch["notes"] == .string("New notes"))
        #expect(patch["slots"] == nil && patch["groups"] == nil, "nothing else is sent")
        #expect(editor.finished.routine.name == "Day 1 – Floor", "Save trims the name")
        editor.rename("   ")
        #expect(editor.problem == "The routine needs a name.")
    }

    @Test("an untouched routine asks for no write")
    func noChange() {
        let editor = RoutineEditor(Self.routine())
        #expect(!editor.hasChanges)
        #expect(editor.patch(codec: codec).isEmpty)
    }

    @Test("blocks move whole: a group with its members")
    func moveBlocks() {
        var editor = RoutineEditor(Self.routine())
        editor.moveBlocks(from: [3], to: 1)
        #expect(keys(editor) == ["warm", "b1", "b2", "a1", "a2", "a3", "solo"])
        editor.moveBlocks(from: [0], to: 3)
        #expect(keys(editor) == ["b1", "b2", "a1", "a2", "a3", "warm", "solo"])
        #expect(Set(editor.patch(codec: codec).map(\.key)) == ["slots", "groups"], "slots and groups go together")
    }

    @Test("members reorder inside their group")
    func moveMembers() {
        var editor = RoutineEditor(Self.routine())
        editor.moveMembers(of: "a", from: [2], to: 0)
        #expect(keys(editor) == ["warm", "a3", "a1", "a2", "solo", "b1", "b2"])
        #expect(editor.routine.slots.allSatisfy { $0.extra["coachTag"] != nil }, "every slot keeps its unknown fields")
    }

    @Test("two plain slots make a superset; a plain slot joins a group; a group takes in the next")
    func groupWithNext() throws {
        var editor = RoutineEditor(Self.routine())
        // solo joins b, which is paired and keeps its kind.
        editor.groupWithNext(2)
        #expect(editor.members(of: "b") == ["solo", "b1", "b2"])
        #expect(editor.group("b")?.groupKind == .paired)
        // warm and group a: warm joins a, which becomes a giant set of four.
        editor.groupWithNext(0)
        #expect(editor.members(of: "a") == ["warm", "a1", "a2", "a3"])
        #expect(editor.group("a")?.groupKind == .giantset)
        #expect(editor.group("a")?.extra["coachTag"] == .string("group a"), "the group keeps its unknown fields")
        // a takes in b: one group of seven, b gone.
        editor.groupWithNext(0)
        #expect(editor.routine.groups.map(\.key) == ["a"])
        #expect(editor.members(of: "a").count == 7)

        var fresh = RoutineEditor(Self.routine())
        fresh.leaveGroup("a3")
        // a3 now sits after group a, before solo: group them.
        let index = try #require(fresh.blocks.firstIndex { $0.slots == ["a3"] })
        fresh.groupWithNext(index)
        let made = try #require(fresh.routine.slots.first { $0.key == "a3" }?.group)
        #expect(made == "c", "the first letter no group used")
        #expect(fresh.group(made)?.groupKind == .superset)
        #expect(fresh.members(of: made) == ["a3", "solo"])
        #expect(fresh.group("a")?.groupKind == .superset, "a tri-set of two is a superset")
    }

    @Test("leaving a group places the slot after it; a group of one is dissolved")
    func leaveGroup() {
        var editor = RoutineEditor(Self.routine())
        editor.leaveGroup("b1")
        #expect(editor.group("b") == nil)
        #expect(editor.routine.slots.allSatisfy { $0.group != "b" })
        #expect(keys(editor) == ["warm", "a1", "a2", "a3", "solo", "b2", "b1"])
        editor.leaveGroup("a1")
        #expect(keys(editor) == ["warm", "a2", "a3", "a1", "solo", "b2", "b1"])
        editor.ungroup("a")
        #expect(editor.routine.groups.isEmpty)
        #expect(editor.blocks.count == 7)
    }

    @Test("adding an exercise: a plain slot or a group member, with a key never used")
    func addExercise() {
        var editor = RoutineEditor(Self.routine())
        let solo = Exercise(id: "solo", name: "Solo", measurement: .weightreps)
        editor.removeSlot("solo")
        editor.addExercise(solo)
        #expect(editor.routine.slots.last?.key == "solo-2", "the removed slot's key is not given to a new one")
        #expect(editor.routine.slots.last?.sets.map(\.key) == ["s1", "s2", "s3"])
        #expect(editor.routine.slots.last?.sets.first?.repsMin == 8)
        let plank = Exercise(id: "plank", name: "Plank", measurement: .duration)
        editor.addExercise(plank, toGroup: "b")
        #expect(editor.members(of: "b") == ["b1", "b2", "plank"])
        #expect(editor.slot("plank")?.sets.first?.durationMin == 30)
    }

    @Test("removing blocks and members; a group left with one member is dissolved")
    func remove() {
        var editor = RoutineEditor(Self.routine())
        editor.removeBlocks(at: [1])
        #expect(keys(editor) == ["warm", "solo", "b1", "b2"])
        #expect(editor.group("a") == nil)
        editor.removeMembers(of: "b", at: [0])
        #expect(editor.group("b") == nil)
        #expect(editor.slot("b2")?.group == nil)
    }

    @Test("sets: added after their role with a fresh key, removed, moved, edited")
    func sets() {
        var editor = RoutineEditor(Self.routine())
        editor.removeSets(of: "a1", at: [1])
        editor.addSet(to: "a1")
        #expect(editor.slot("a1")?.sets.map(\.key) == ["s1", "s3"], "s2 was the routine's, so a new set never takes it")
        editor.addSet(to: "a1", role: .warmup)
        #expect(editor.slot("a1")?.sets.map(\.key) == ["w1", "s1", "s3"])
        #expect(editor.slot("a1")?.sets.first?.role == .warmup)
        editor.moveSets(of: "a1", from: [0], to: 3)
        #expect(editor.slot("a1")?.sets.map(\.key) == ["s1", "s3", "w1"])
        editor.updateSet("a1", "s1") {
            $0.repsMin = 6
            $0.repsMax = 8
            $0.rpeMin = 7
            $0.rpeMax = 8
            $0.roleLabel = .top
            $0.rest = 150
        }
        let set = editor.slot("a1")?.sets.first
        #expect(set?.repsMin == 6 && set?.repsMax == 8 && set?.rpeMax == 8 && set?.roleLabel == .top && set?.rest == 150)
        editor.updateSet("a1", "s1") { $0.repsMin = 10 }
        #expect(editor.problem == "A rep range runs backwards.")
    }

    @Test("group values: kind, rounds and round rest")
    func groupValues() {
        var editor = RoutineEditor(Self.routine())
        editor.updateGroup("a") {
            $0.rounds = 3
            $0.roundRest = 90
        }
        #expect(editor.group("a")?.rounds == 3 && editor.group("a")?.roundRest == 90)
        let patch = editor.patch(codec: codec)
        #expect(Set(patch.map(\.key)) == ["slots", "groups"])
    }

    @Test("a duplicate is a new routine at a free slug, whole, unknown fields kept, no version")
    func duplicate() {
        let routine = Self.routine()
        let editor = RoutineEditor.duplicate(routine, taken: ["day-1", "day-1-copy"])
        #expect(editor.isNew && editor.hasChanges)
        #expect(editor.routine.id == "day-1-copy-2")
        #expect(editor.routine.name == "Day 1 (copy)")
        #expect(editor.routine.version == nil)
        #expect(editor.baseVersion == nil)
        #expect(editor.routine.slots == routine.slots && editor.routine.groups == routine.groups)
        #expect(editor.routine.extra == routine.extra)
    }

    @Test("the draft is kept on disk and read back as it was")
    func draft() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("drafts-\(UUID().uuidString)")
        let drafts = RoutineDrafts(directory: directory, writingOptions: .atomic)
        #expect(try drafts.load() == nil)
        var editor = RoutineEditor(Self.routine())
        editor.rename("Renamed")
        editor.moveMembers(of: "a", from: [0], to: 3)
        try drafts.save(editor)
        let back = try #require(try drafts.load())
        #expect(back == editor)
        try drafts.clear()
        #expect(try drafts.load() == nil)
    }

    @Test("a set made a warm-up moves before the working sets; a replaced exercise gets a new key")
    func roleAndReplace() {
        var editor = RoutineEditor(Self.routine())
        editor.setRole("a1", "s2", .warmup)
        #expect(editor.slot("a1")?.sets.map(\.key) == ["s2", "s1"])
        #expect(editor.slot("a1")?.sets.first?.role == .warmup)
        editor.replaceExercise("a1", with: "other")
        #expect(editor.slot("a1") == nil)
        #expect(editor.slot("other")?.group == "a")
        #expect(editor.members(of: "a") == ["other", "a2", "a3"])
    }

    @Test("a routine whose slots share a key is not saved")
    func duplicateKeys() {
        var routine = Self.routine()
        routine.slots[2].key = "a1"
        #expect(RoutineEditor(routine).problem != nil)
    }

    @Test("SwiftUI's move, without SwiftUI")
    func moveOffsets() {
        var values = Array(0 ..< 6)
        values.moveOffsets([1, 3], to: 5)
        #expect(values == [0, 2, 4, 1, 3, 5])
        values = Array(0 ..< 6)
        values.moveOffsets([4], to: 0)
        #expect(values == [4, 0, 1, 2, 3, 5])
        values = Array(0 ..< 3)
        values.moveOffsets([0], to: 3)
        #expect(values == [1, 2, 0])
    }
}
