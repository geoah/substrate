import Foundation
import SubstrateCore

/// A routine being edited on the phone: the routine as it stood when the
/// edit began (`base`, its server version included) and the routine as the
/// owner has it now. Every operation changes `routine` through its typed
/// fields only, so whatever the app does not show (the owner's notes, fields
/// the coach wrote, fields a later package adds, all kept in each level's
/// `extra`) goes back unchanged (decision A2).
///
/// The editor works in BLOCKS, as the preview reads a routine: a plain slot
/// is a block, and a group is one block holding its members in slot order.
/// An operation that changes the order writes `slots` back block by block,
/// so a group's members end up next to each other, as the kind asks.
public struct RoutineEditor: Codable, Hashable, Sendable {
    public var base: Routine
    public var routine: Routine
    /// A routine the server does not hold yet: a duplicate, written whole.
    public var isNew: Bool

    public init(_ routine: Routine) {
        base = routine
        self.routine = routine
        isNew = false
    }

    /// A copy of `source` under a freshly minted id, to save as a new
    /// routine. The copy keeps everything the source holds, its unknown
    /// fields included, and no version.
    public static func duplicate(_ source: Routine, id: RoutineID = .minted()) -> RoutineEditor {
        var copy = source
        copy.name = "\(source.name) (copy)"
        copy.id = id
        copy.version = nil
        var editor = RoutineEditor(copy)
        editor.isNew = true
        return editor
    }

    /// Whether anything differs from the routine the edit began from.
    public var hasChanges: Bool { isNew || routine != base }

    /// The version the edit asserts with `ifVersion`.
    public var baseVersion: Int? { isNew ? nil : base.version }

    // MARK: - Blocks

    /// A plain slot, or a group and its members, in the routine's order.
    public struct Block: Hashable, Sendable, Identifiable {
        /// `slot-<key>` or `group-<key>`.
        public var id: String
        /// The group's key; nil for a plain slot.
        public var group: String?
        public var slots: [String]
    }

    /// The routine as blocks. A slot naming a group the routine does not
    /// declare reads as a plain slot, as the preview reads it, and keeps its
    /// `group` untouched.
    public var blocks: [Block] {
        let declared = Set(routine.groups.map(\.key))
        var out: [Block] = []
        var placed: [String: Int] = [:]
        for slot in routine.slots {
            guard let key = slot.group, declared.contains(key) else {
                out.append(Block(id: "slot-\(slot.key)", group: nil, slots: [slot.key]))
                continue
            }
            if let index = placed[key] {
                out[index].slots.append(slot.key)
            } else {
                placed[key] = out.count
                out.append(Block(id: "group-\(key)", group: key, slots: [slot.key]))
            }
        }
        return out
    }

    /// The members of a group, in slot order.
    public func members(of group: String) -> [String] {
        routine.slots.filter { $0.group == group }.map(\.key)
    }

    public func slot(_ key: String) -> RoutineSlot? { routine.slots.first { $0.key == key } }

    public func group(_ key: String) -> RoutineGroup? { routine.groups.first { $0.key == key } }

    /// Writes `slots` in the order of `blocks`.
    private mutating func write(_ blocks: [Block]) {
        let byKey = Dictionary(routine.slots.map { ($0.key, $0) }, uniquingKeysWith: { first, _ in first })
        routine.slots = blocks.flatMap { $0.slots.compactMap { byKey[$0] } }
    }

    // MARK: - The routine

    /// The name as typed; a blank one is what `problem` refuses, and Save
    /// trims it (`finished`).
    public mutating func rename(_ name: String) {
        routine.name = name
    }

    /// The routine as Save writes it: the name trimmed.
    public var finished: RoutineEditor {
        var out = self
        out.routine.name = routine.name.trimmingCharacters(in: .whitespacesAndNewlines)
        return out
    }

    /// An empty folder is no folder.
    public mutating func setFolder(_ folder: String?) {
        routine.folder = Self.text(folder)
    }

    public mutating func setNotes(_ notes: String?) {
        routine.notes = Self.text(notes)
    }

    static func text(_ value: String?) -> String? {
        guard let value, !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
        return value
    }

    // MARK: - Order

    /// Moves blocks, as a List's `onMove` hands them.
    public mutating func moveBlocks(from source: IndexSet, to destination: Int) {
        var blocks = blocks
        blocks.moveOffsets(source, to: destination)
        write(blocks)
    }

    /// Moves members inside their group.
    public mutating func moveMembers(of group: String, from source: IndexSet, to destination: Int) {
        var blocks = blocks
        guard let index = blocks.firstIndex(where: { $0.group == group }) else { return }
        blocks[index].slots.moveOffsets(source, to: destination)
        write(blocks)
    }

    /// Moves planned sets inside a slot.
    public mutating func moveSets(of slot: String, from source: IndexSet, to destination: Int) {
        update(slot) { $0.sets.moveOffsets(source, to: destination) }
    }

    // MARK: - Groups

    /// Joins the block at `index` and the one after it into one group: two
    /// plain slots make a new superset, a plain slot joins the group beside
    /// it, and a group takes in the group after it. The group's kind follows
    /// its size when it is a superset, tri-set or giant set.
    public mutating func groupWithNext(_ index: Int) {
        var blocks = blocks
        guard index >= 0, index + 1 < blocks.count else { return }
        let first = blocks[index]
        let second = blocks[index + 1]
        let key: String
        if let existing = first.group {
            key = existing
            if let gone = second.group { routine.groups.removeAll { $0.key == gone } }
        } else if let existing = second.group {
            key = existing
        } else {
            key = nextGroupKey()
            routine.groups.append(RoutineGroup(key: key, groupKind: .superset))
        }
        let slots = first.slots + second.slots
        for slot in slots {
            update(slot) { $0.group = key }
        }
        blocks.replaceSubrange(index ... index + 1, with: [Block(id: "group-\(key)", group: key, slots: slots)])
        write(blocks)
        fitKind(key)
    }

    /// Takes a member out of its group, placed just after the group. A group
    /// left with one member is no group: the member is plain again.
    public mutating func leaveGroup(_ slot: String) {
        guard let key = self.slot(slot)?.group, group(key) != nil else { return }
        var blocks = blocks
        guard let index = blocks.firstIndex(where: { $0.group == key }) else { return }
        blocks[index].slots.removeAll { $0 == slot }
        blocks.insert(Block(id: "slot-\(slot)", group: nil, slots: [slot]), at: index + 1)
        update(slot) { $0.group = nil }
        write(blocks)
        settle(key)
    }

    /// Every member plain again, and the group gone.
    public mutating func ungroup(_ key: String) {
        for slot in members(of: key) {
            update(slot) { $0.group = nil }
        }
        routine.groups.removeAll { $0.key == key }
    }

    public mutating func updateGroup(_ key: String, _ change: (inout RoutineGroup) -> Void) {
        guard let index = routine.groups.firstIndex(where: { $0.key == key }) else { return }
        change(&routine.groups[index])
    }

    /// The first free letter, then `g2`, `g3`.
    private func nextGroupKey() -> String {
        let taken = Set(routine.groups.map(\.key)).union(routine.slots.compactMap(\.group)).union(base.groups.map(\.key))
        for letter in "abcdefghijklmnopqrstuvwxyz" where !taken.contains(String(letter)) {
            return String(letter)
        }
        var n = 2
        while taken.contains("g\(n)") { n += 1 }
        return "g\(n)"
    }

    /// A superset, tri-set or giant set named for its size; a circuit or
    /// paired sets keep their kind.
    private mutating func fitKind(_ key: String) {
        let count = members(of: key).count
        updateGroup(key) { group in
            guard [.superset, .triset, .giantset].contains(group.groupKind) else { return }
            group.groupKind = count <= 2 ? .superset : count == 3 ? .triset : .giantset
        }
    }

    /// After a member left: a group with one member or none is dissolved,
    /// any other has its kind fitted again.
    private mutating func settle(_ key: String) {
        let members = members(of: key)
        if members.count < 2 {
            ungroup(key)
        } else {
            fitKind(key)
        }
    }

    // MARK: - Slots

    /// Adds an exercise as a plain slot at the end, or as the last member of
    /// a group, with three working sets to start from.
    public mutating func addExercise(_ exercise: Exercise, toGroup group: String? = nil) {
        let key = nextSlotKey(for: exercise.id)
        let slot = RoutineSlot(key: key, exercise: exercise.id, group: group, sets: Self.startingSets(for: exercise))
        if let group, let last = routine.slots.lastIndex(where: { $0.group == group }) {
            routine.slots.insert(slot, at: last + 1)
            fitKind(group)
        } else {
            var plain = slot
            plain.group = nil
            routine.slots.append(plain)
        }
    }

    /// Three working sets, `s1` to `s3`: 8 to 12 reps for an exercise that
    /// counts reps, 30 seconds for a timed hold, nothing else for a distance.
    static func startingSets(for exercise: Exercise) -> [PlannedSet] {
        (1 ... 3).map { n in
            var set = PlannedSet(key: "s\(n)")
            switch exercise.measurement {
            case .duration, .weightduration:
                set.durationMin = 30
                set.durationMax = 30
            case .distanceduration, .shortdistanceweight:
                break
            default:
                set.repsMin = 8
                set.repsMax = 12
            }
            return set
        }
    }

    /// The exercise's id, then `-2`, `-3`, never a key the routine held
    /// when the edit began: a session slot names it as `routineSlot`.
    private func nextSlotKey(for exercise: ExerciseID) -> String {
        let taken = Set(routine.slots.map(\.key)).union(base.slots.map(\.key))
        var candidate = exercise.rawValue
        var n = 2
        while taken.contains(candidate) {
            candidate = "\(exercise.rawValue)-\(n)"
            n += 1
        }
        return candidate
    }

    /// Removes blocks, as a List's `onDelete` hands them: a group goes with
    /// its members.
    public mutating func removeBlocks(at offsets: IndexSet) {
        let blocks = blocks
        for index in offsets.sorted(by: >) where blocks.indices.contains(index) {
            let block = blocks[index]
            routine.slots.removeAll { block.slots.contains($0.key) }
            if let key = block.group { routine.groups.removeAll { $0.key == key } }
        }
    }

    /// Removes members of a group; a group left with one is dissolved.
    public mutating func removeMembers(of group: String, at offsets: IndexSet) {
        let members = members(of: group)
        let gone = Set(offsets.compactMap { members.indices.contains($0) ? members[$0] : nil })
        routine.slots.removeAll { gone.contains($0.key) }
        settle(group)
    }

    /// Puts another exercise in a slot, under a new key: sessions name the
    /// slot by its key, and the old one meant the old exercise.
    /// Answers the slot's key after the change.
    @discardableResult
    public mutating func replaceExercise(_ slot: String, with exercise: ExerciseID) -> String {
        guard let current = self.slot(slot), current.exercise != exercise else { return slot }
        let key = nextSlotKey(for: exercise)
        update(slot) {
            $0.exercise = exercise
            $0.key = key
        }
        return key
    }

    /// Changes a planned set's role, and moves it among its role's sets:
    /// warm-ups come first.
    public mutating func setRole(_ slot: String, _ set: String, _ role: Role) {
        update(slot) { slot in
            guard let index = slot.sets.firstIndex(where: { $0.key == set }), slot.sets[index].role != role else { return }
            var planned = slot.sets.remove(at: index)
            planned.role = role
            if role == .warmup { planned.roleLabel = nil }
            let lastWarmup = slot.sets.lastIndex { $0.role == .warmup }
            slot.sets.insert(planned, at: lastWarmup.map { $0 + 1 } ?? 0)
        }
    }

    public mutating func removeSlot(_ key: String) {
        let group = slot(key)?.group
        routine.slots.removeAll { $0.key == key }
        if let group, self.group(group) != nil { settle(group) }
    }

    /// Changes one slot's typed fields.
    public mutating func update(_ slot: String, _ change: (inout RoutineSlot) -> Void) {
        guard let index = routine.slots.firstIndex(where: { $0.key == slot }) else { return }
        change(&routine.slots[index])
    }

    // MARK: - Planned sets

    /// Adds a planned set after the last of its role, a copy of that set's
    /// targets under a key no set of the slot ever had. A warm-up is `w<n>`,
    /// a working set `s<n>`. Unknown fields are not copied: they describe the
    /// set they were written on.
    public mutating func addSet(to slot: String, role: Role = .working) {
        let baseKeys = Set(base.slots.first { $0.key == slot }?.sets.map(\.key) ?? [])
        update(slot) { slot in
            let taken = Set(slot.sets.map(\.key)).union(baseKeys)
            let prefix = role == .warmup ? "w" : "s"
            var n = slot.sets.count { $0.role == role } + 1
            while taken.contains("\(prefix)\(n)") { n += 1 }
            let key = "\(prefix)\(n)"
            var set: PlannedSet
            if let model = slot.sets.last(where: { $0.role == role }) {
                set = model
                set.key = key
                set.extra = [:]
            } else {
                set = PlannedSet(key: key, role: role)
                if role == .working {
                    set.repsMin = 8
                    set.repsMax = 12
                }
            }
            if let last = slot.sets.lastIndex(where: { $0.role == role }) {
                slot.sets.insert(set, at: last + 1)
            } else if role == .warmup {
                slot.sets.insert(set, at: 0)
            } else {
                slot.sets.append(set)
            }
        }
    }

    public mutating func removeSets(of slot: String, at offsets: IndexSet) {
        update(slot) { $0.sets.removeOffsets(offsets) }
    }

    /// Changes one planned set's typed fields.
    public mutating func updateSet(_ slot: String, _ set: String, _ change: (inout PlannedSet) -> Void) {
        update(slot) { slot in
            guard let index = slot.sets.firstIndex(where: { $0.key == set }) else { return }
            change(&slot.sets[index])
        }
    }

    // MARK: - Checks

    /// What stops the routine from being saved, in a sentence; nil when it
    /// can be.
    public var problem: String? {
        if routine.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return "The routine needs a name." }
        if routine.slots.isEmpty { return "The routine needs at least one exercise." }
        if Set(routine.slots.map(\.key)).count != routine.slots.count {
            return "Two exercises in this routine share a key, so it can\u{2019}t be edited here safely."
        }
        for slot in routine.slots {
            for set in slot.sets {
                if let min = set.repsMin, let max = set.repsMax, min > max { return "A rep range runs backwards." }
                if let min = set.rpeMin, let max = set.rpeMax, min > max { return "An RPE range runs backwards." }
                if let min = set.rirMin, let max = set.rirMax, min > max { return "An RIR range runs backwards." }
            }
        }
        return nil
    }
}

// MARK: - The write

public extension RoutineEditor {
    /// The properties a guarded PATCH sends: each top-level property whose
    /// encoding changed, whole (`slots` and `groups` are replaced whole by
    /// any write that names them), and `null` for one the edit cleared.
    /// Nothing the edit did not change is sent, so a property another writer
    /// changed meanwhile is never written back over.
    func patch(codec: RecordCodec) -> JSONObject {
        let before = codec.encode(base).properties
        let after = codec.encode(routine).properties
        var out = Self.patch(from: before, to: after)
        // Slots name groups by key: the two go together, so a write that
        // lands over another writer's never pairs one's slots with the
        // other's groups.
        if out["slots"] != nil || out["groups"] != nil {
            for key in ["slots", "groups"] {
                out[key] = after[key] ?? (before[key] == nil ? nil : .null)
            }
        }
        return out
    }

    static func patch(from before: JSONObject, to after: JSONObject) -> JSONObject {
        var out = JSONObject()
        for (key, value) in after where before[key] != value {
            out[key] = value
        }
        for (key, _) in before where after[key] == nil {
            out[key] = .null
        }
        return out
    }
}

extension Array {
    /// SwiftUI's `move(fromOffsets:toOffset:)`, without SwiftUI: the
    /// elements at `source`, in order, land before the element that was at
    /// `destination`.
    mutating func moveOffsets(_ source: IndexSet, to destination: Int) {
        let valid = source.filter { indices.contains($0) }
        guard !valid.isEmpty else { return }
        let moving = valid.map { self[$0] }
        let before = valid.count { $0 < destination }
        for index in valid.sorted(by: >) {
            remove(at: index)
        }
        let target = Swift.min(Swift.max(destination - before, 0), count)
        insert(contentsOf: moving, at: target)
    }

    mutating func removeOffsets(_ offsets: IndexSet) {
        for index in offsets.sorted(by: >) where indices.contains(index) {
            remove(at: index)
        }
    }
}

// MARK: - Words for the editor

public extension RoutinePresenter {
    /// A slot's planned sets in a line or two, for an editor row:
    /// `Warm-up: 3:00`, `2 sets of 12–15 reps at 4–5 RIR`.
    static func editorLines(_ slot: RoutineSlot, routine: Routine, catalog: Catalog) -> [String] {
        let lines = previewSlot(slot, routine: routine, catalog: catalog, label: nil).lines
        return Array(lines.prefix { !$0.hasPrefix("Rest") && !$0.hasPrefix("Warm-up rest") && !$0.hasPrefix("Then") }.prefix(3))
    }

    /// One planned set's targets: `8–12 reps, RPE 7–8`, `3:00`, `Top set`.
    static func plannedSetText(_ planned: PlannedSet, measurement: MeasurementType) -> String {
        var parts: [String] = []
        if measurement.isTimedHold || planned.durationMin != nil {
            if let min = planned.durationMin { parts.append(Format.durationRange(min, planned.durationMax ?? min)) }
        } else {
            switch (planned.repsMin, planned.repsMax) {
            case let (min?, max?): parts.append("\(Format.range(min, max)) \(min == max && min == 1 ? "rep" : "reps")")
            case let (min?, nil): parts.append("at least \(Format.count(min, "rep"))")
            case let (nil, max?): parts.append("up to \(Format.count(max, "rep"))")
            case (nil, nil): break
            }
        }
        if let load = planned.load { parts.append(Format.mass(load)) }
        if let effort = Format.effort(rpeMin: planned.rpeMin, rpeMax: planned.rpeMax, rirMin: planned.rirMin, rirMax: planned.rirMax) {
            parts.append(effort)
        }
        switch planned.termination {
        case .amrap: parts.append("as many as possible")
        case .failure: parts.append("to failure")
        default: break
        }
        if planned.structure != .straight, let kind = specialText(planned, number: 0) {
            parts.append(kind.replacingOccurrences(of: "Set 0 is ", with: "").replacingOccurrences(of: "Set 0 ", with: ""))
        }
        if let rest = planned.rest { parts.append("rest \(Format.rest(rest))") }
        return parts.isEmpty ? "No target" : parts.joined(separator: ", ")
    }
}
