import Foundation

// Rows from a routine (§4.5), the queries every engine file and the
// presenter read, and the structural edits the reducer builds on.

// MARK: - Queries

public extension SessionState {
    func slotIndex(_ key: String) -> Int? {
        slots.firstIndex { $0.key == key }
    }

    func slot(_ key: String) -> SlotState? {
        slotIndex(key).map { slots[$0] }
    }

    func row(_ id: RowID) -> Row? {
        rows[id]
    }

    func group(_ key: String) -> GroupState? {
        groups.first { $0.key == key }
    }

    func exercise(ofSlot key: String) -> Exercise? {
        slot(key).flatMap { exercises[$0.exercise] }
    }

    func exercise(ofRow id: RowID) -> Exercise? {
        rows[id].flatMap { exercise(ofSlot: $0.slot) }
    }

    /// The routine slot a session slot runs (or replaces).
    func routineSlot(of slotKey: String) -> RoutineSlot? {
        guard let name = slot(slotKey)?.routineSlot else { return nil }
        return plan?.slots.first { $0.key == name }
    }

    /// The routine's group of the same key, unless the group was made in the gym.
    func routineGroup(of groupKey: String) -> RoutineGroup? {
        guard let state = group(groupKey), !state.madeInGym else { return nil }
        return plan?.groups.first { $0.key == groupKey }
    }

    /// The planned set a row copies, found by key in its slot's routine slot.
    func plannedSet(ofRow id: RowID) -> PlannedSet? {
        guard let row = rows[id], let key = row.plannedSet else { return nil }
        return routineSlot(of: row.slot)?.sets.first { $0.key == key }
    }

    /// The slot's rows in display order.
    func orderedRows(inSlot key: String) -> [Row] {
        slot(key)?.rows.compactMap { rows[$0] } ?? []
    }

    /// The members of a group, in display order.
    func members(ofGroup key: String) -> [SlotState] {
        slots.filter { $0.group == key }
    }

    /// A row's chain in display order: the head, then its segments. A row
    /// that is no chain is its own.
    func chain(of id: RowID) -> [Row] {
        guard let row = rows[id] else { return [] }
        let head = row.headID
        return slot(row.slot)?.rows.compactMap { rows[$0] }.filter { $0.headID == head } ?? []
    }

    /// True when every link of the row's chain before it is completed.
    func earlierLinksCompleted(_ id: RowID) -> Bool {
        guard let row = rows[id] else { return false }
        return chain(of: id).allSatisfy { $0.chainPosition >= row.chainPosition || $0.isCompleted }
    }

    /// A pending segment whose earlier link is pending: it cannot be ticked yet.
    func isLocked(_ id: RowID) -> Bool {
        guard let row = rows[id], row.isPending, !row.isHead else { return false }
        return !earlierLinksCompleted(id)
    }

    /// The row whose timed set runs now.
    var timedRow: RowID? {
        countdown?.timedRow
    }

    /// Whether the row's set runs a timer (§7.6: a TIME column, or a carry's stopwatch).
    func isTimed(_ id: RowID) -> Bool {
        exercise(ofRow: id)?.measurement.hasTimer ?? false
    }

    /// The row's chain heads and segments as units, in display order.
    func units(inSlot key: String) -> [[RowID]] {
        guard let slot = slot(key) else { return [] }
        var order: [RowID] = []
        var members: [RowID: [RowID]] = [:]
        for id in slot.rows {
            guard let row = rows[id] else { continue }
            if members[row.headID] == nil { order.append(row.headID) }
            members[row.headID, default: []].append(id)
        }
        return order.map { head in
            (members[head] ?? []).sorted { (rows[$0]?.chainPosition ?? 0) < (rows[$1]?.chainPosition ?? 0) }
        }
    }
}

public extension MeasurementType {
    /// The values a tick needs (§4.3's table).
    var requiredFields: [ValueField] {
        switch self {
        case .weightreps: [.load, .reps]
        case .repsonly, .bodyweightreps: [.reps]
        case .weightedbodyweightreps: [.load, .reps]
        case .bodyweightassistedreps: [.assistance, .reps]
        case .duration: [.duration]
        case .weightduration: [.load, .duration]
        case .distanceduration: [.distance, .duration]
        case .shortdistanceweight: [.load, .distance]
        }
    }

    /// The required values `values` lacks. A duration must be at least 1 s;
    /// a bodyweight-plus load may be 0.
    func missing(in values: SetValues) -> [ValueField] {
        requiredFields.filter { field in
            switch field {
            case .load: values.load == nil
            case .assistance: values.assistance == nil
            case .reps: values.reps == nil
            case .duration: (values.duration ?? 0) < 1
            case .distance: values.distance == nil
            }
        }
    }

    /// The set runs a timer: a TIME column, or a carry's stopwatch (§7.6).
    var hasTimer: Bool {
        switch self {
        case .duration, .weightduration, .distanceduration, .shortdistanceweight: true
        case .weightreps, .repsonly, .bodyweightreps, .weightedbodyweightreps, .bodyweightassistedreps: false
        }
    }

    /// The set's effort is measured in time rather than reps.
    var isTimedHold: Bool {
        self == .duration || self == .weightduration
    }
}

public extension Side {
    var other: Side { self == .left ? .right : .left }
}

// MARK: - Rows from a routine

public extension SessionEngine {
    /// How many row ids `start` consumes for `routine`: one per planned set
    /// and one per known segment (§4.5).
    static func rowIDCount(for routine: Routine?, exercises: [ExerciseID: Exercise]) -> Int {
        guard let routine else { return 0 }
        return routine.slots.reduce(0) { total, slot in
            guard let exercise = exercises[slot.exercise] else { return total }
            return total + slot.sets.reduce(0) { $0 + 1 + knownSegmentCount(of: $1, exercise: exercise) }
        }
    }

    /// The termination a planned set names, else the one its targets imply (§4.5).
    static func impliedTermination(of planned: PlannedSet) -> Termination? {
        if let termination = planned.termination { return termination }
        if planned.structure == .myomatch { return .totalreps }
        switch (planned.repsMin, planned.repsMax) {
        case let (min?, max?): return min == max ? .reps : .reprange
        // An AMRAP with a floor names repsMin alone (routine.yaml).
        case (_?, nil): return .amrap
        case (nil, _?): return .reprange
        case (nil, nil): break
        }
        if planned.totalReps != nil { return .totalreps }
        if planned.durationMin != nil || planned.durationMax != nil { return .time }
        if planned.distance != nil { return .distance }
        return nil
    }

    /// The target a planned set's head copies: every field but the key (as
    /// `plannedSet`), `role`, `roleLabel`, `structure` and `rest`, with the
    /// termination its targets imply and a percentage load resolved
    /// (`resolvedLoad`). The values the engine runs with where the plan is
    /// silent are added when the record is written (§3.5). The planned set's
    /// unknown fields stay with it (the session's plan snapshot keeps them):
    /// `set.target` declares other fields, and the server refuses an
    /// undeclared one.
    static func target(from planned: PlannedSet, bodyMass: Mass? = nil) -> SetTarget {
        let resolved = resolvedLoad(of: planned, bodyMass: bodyMass)
        return SetTarget(
            plannedSet: planned.key,
            termination: impliedTermination(of: planned),
            repsMin: planned.repsMin,
            repsMax: planned.repsMax,
            load: resolved.load,
            loadPercent: planned.loadPercent,
            loadReference: planned.loadReference,
            referenceLoad: resolved.reference,
            assistance: planned.assistance,
            rpeMin: planned.rpeMin,
            rpeMax: planned.rpeMax,
            rirMin: planned.rirMin,
            rirMax: planned.rirMax,
            durationMin: planned.durationMin,
            durationMax: planned.durationMax,
            timerMode: planned.timerMode,
            distance: planned.distance,
            tempo: planned.tempo,
            miniSetReps: planned.miniSetReps,
            miniSets: planned.miniSets,
            myoStopRule: planned.myoStopRule,
            totalReps: planned.totalReps,
            drops: planned.drops,
            dropPercent: planned.dropPercent,
            dropStep: planned.dropStep,
            dropReps: planned.dropReps,
            intraSetPause: planned.intraSetPause
        )
    }

    /// A planned set's absolute load (D5): its `load` when it names one,
    /// which wins over a percentage; else `loadPercent` of the session's body
    /// mass for `loadReference: bodymass`, in the body mass's unit. V1 keeps
    /// no one-rep max or training max, so a percentage of one stays
    /// unresolved: no load, no reference, and the suggestion picks a load.
    static func resolvedLoad(of planned: PlannedSet, bodyMass: Mass?) -> (load: Mass?, reference: Mass?) {
        if let load = planned.load { return (load, nil) }
        guard let percent = planned.loadPercent, planned.loadReference == .bodymass, let bodyMass else { return (nil, nil) }
        return (Mass(value: bodyMass.value * percent / 100, unit: bodyMass.unit), bodyMass)
    }

    /// The segments a planned set's head is created with: the second side on
    /// a one-side-at-a-time exercise (which then ignores the structure, §4.9),
    /// else its drops or clusters.
    internal static func knownSegmentCount(of planned: PlannedSet, exercise: Exercise) -> Int {
        if exercise.resolvedLaterality == .unilateral { return 1 }
        switch planned.structure {
        case .drop: return planned.drops ?? 1
        case .cluster: return planned.miniSets ?? defaultClusters
        case .straight, .myo, .myomatch, .restpause: return 0
        }
    }

    /// Clusters after the first when a cluster set names none: 02 §2.7's
    /// example of four clusters.
    internal static let defaultClusters = 3

    /// A planned set's head and its known segments.
    internal static func rows(for planned: PlannedSet, slot: String, exercise: Exercise, firstSide: Side, ids: inout RowIDPool,
                              bodyMass: Mass? = nil) -> [Row]
    {
        let unilateral = exercise.resolvedLaterality == .unilateral
        let head = Row(
            id: ids.next(),
            slot: slot,
            plannedSet: planned.key,
            role: planned.role,
            roleLabel: planned.roleLabel,
            structure: unilateral ? .straight : planned.structure,
            termination: impliedTermination(of: planned),
            side: unilateral ? firstSide : nil,
            target: target(from: planned, bodyMass: bodyMass),
            restOverride: planned.rest,
            intraSetPause: planned.intraSetPause,
            timerMode: planned.timerMode
        )
        if unilateral { return [head, secondSide(of: head, id: ids.next())] }
        let count = knownSegmentCount(of: planned, exercise: exercise)
        let reps: Int? = switch planned.structure {
        case .drop: planned.dropReps
        case .cluster: planned.miniSetReps
        default: nil
        }
        return [head] + (0 ..< count).map { index in
            segment(of: head, position: index + 2, id: ids.next(), reps: reps)
        }
    }

    /// A chain segment of `head`: a drop, cluster or mini-set. The examples'
    /// shape: it carries the planned set's key and only its own targets.
    internal static func segment(of head: Row, position: Int, id: RowID, reps: Int?) -> Row {
        Row(
            id: id,
            slot: head.slot,
            plannedSet: head.plannedSet,
            role: head.role,
            roleLabel: head.roleLabel,
            structure: head.structure,
            termination: reps == nil ? nil : .reps,
            chainHead: head.id,
            chainPosition: position,
            target: head.target.map { SetTarget(plannedSet: $0.plannedSet, repsMin: reps, repsMax: reps) },
            timerMode: head.timerMode,
            generated: true
        )
    }

    /// The second side of a one-side-at-a-time head. Its target is the planned
    /// set's key only, until `sideMatch` copies the first side's result.
    internal static func secondSide(of head: Row, id: RowID) -> Row {
        Row(
            id: id,
            slot: head.slot,
            plannedSet: head.plannedSet,
            role: head.role,
            roleLabel: head.roleLabel,
            structure: .straight,
            termination: head.termination,
            chainHead: head.id,
            chainPosition: 2,
            side: (head.side ?? .left).other,
            target: head.target.map { SetTarget(plannedSet: $0.plannedSet) },
            timerMode: head.timerMode,
            generated: true
        )
    }

    /// The side a one-side-at-a-time slot starts with (§4.9).
    internal static func firstSide(_ order: SideOrder?, resolved: Side?) -> Side {
        switch order {
        case .right: .right
        case .left, nil: .left
        case .weakerfirst: resolved ?? .left
        }
    }
}

/// Pre-minted row ids consumed in order. When they run out, ids are derived
/// from the event's id, which keeps a fold deterministic.
struct RowIDPool {
    private var ids: [RowID]
    private var index = 0
    private let prefix: String
    private var derived = 0

    init(_ ids: [RowID], prefix: String) {
        self.ids = ids
        self.prefix = prefix
    }

    mutating func next() -> RowID {
        if index < ids.count {
            defer { index += 1 }
            return ids[index]
        }
        derived += 1
        return RowID("\(prefix)-\(derived)")
    }
}

// MARK: - Structural edits

extension SessionState {
    func requireRow(_ id: RowID) throws(EngineError) -> Row {
        guard let row = rows[id] else { throw .unknownRow }
        return row
    }

    func requireSlotIndex(_ key: String) throws(EngineError) -> Int {
        guard let index = slotIndex(key) else { throw .unknownSlot }
        return index
    }

    /// Adds rows to the dictionary and to a slot at `position`, in order.
    mutating func insert(_ new: [Row], inSlot index: Int, at position: Int) throws(EngineError) {
        for row in new where rows[row.id] != nil {
            throw .duplicateID
        }
        for row in new {
            rows[row.id] = row
        }
        slots[index].rows.insert(contentsOf: new.map(\.id), at: min(position, slots[index].rows.count))
    }

    /// Where a new head unit goes: after the slot's last warm-up for a warm-up,
    /// else at the end.
    func insertionPosition(forRole role: Role, inSlot index: Int) -> Int {
        let slotRows = slots[index].rows
        guard role == .warmup else { return slotRows.count }
        let lastWarmup = slotRows.lastIndex { rows[$0].map { rows[$0.headID]?.role == .warmup } ?? false }
        return lastWarmup.map { $0 + 1 } ?? 0
    }

    /// The position just after a chain's last row in its slot.
    func positionAfterChain(_ head: RowID) -> Int? {
        guard let row = rows[head], let index = slotIndex(row.slot) else { return nil }
        let slotRows = slots[index].rows
        return slotRows.lastIndex { rows[$0]?.headID == head }.map { $0 + 1 }
    }

    /// Removes rows and repairs what pointed at them: the anchor, the focus,
    /// a countdown or rest clock that a removed tick started, a timed set.
    mutating func removeRows(_ ids: Set<RowID>) {
        guard !ids.isEmpty else { return }
        let removed = ids.compactMap { rows[$0] }
        for index in slots.indices {
            slots[index].rows.removeAll(where: ids.contains)
        }
        for id in ids {
            rows[id] = nil
        }
        let ticks = removed.compactMap(\.performed)
        let survivingTicks = Set(rows.values.compactMap { $0.performed?.completedAt })
        if let countdown, case .rest = countdown.kind, ticks.contains(where: { $0.completedAt == countdown.startedAt }),
           !survivingTicks.contains(countdown.startedAt)
        {
            self.countdown = nil
        }
        if let timed = countdown?.timedRow, ids.contains(timed) {
            countdown = nil
        }
        // The clock goes back past every clock a removed tick started, to the
        // one before the earliest of them. Among ticks of one second the one
        // with the earliest prior clock goes first, whatever the rows' hash
        // order, so the same events always fold to the same state.
        var unused = ticks.sorted { a, b in
            let (pa, pb) = (a.priorRestClock?.since ?? .distantPast, b.priorRestClock?.since ?? .distantPast)
            return pa != pb ? pa < pb : a.recordID.rawValue < b.recordID.rawValue
        }
        while let clock = restClock, !survivingTicks.contains(clock.since),
              let index = unused.firstIndex(where: { $0.completedAt == clock.since })
        {
            restClock = unused.remove(at: index).priorRestClock
        }
        if let focus, ids.contains(focus) {
            self.focus = nil
        }
        if let anchor = lastCompleted, ids.contains(anchor) {
            recomputeLastCompleted()
        }
    }

    /// The values a tick of `id` commits, saying it reached failure when its
    /// set was switched to Failure in the gym: the record writes the copied
    /// target, whose termination is still the planned one (or absent on an
    /// added row), so this is how history reads the set as failure (§5.2).
    func markedFailure(_ values: SetValues, row id: RowID) -> SetValues {
        guard values.reachedFailure == nil, let row = rows[id], let head = rows[row.headID],
              head.termination == .failure, head.target?.termination != .failure
        else { return values }
        var marked = values
        marked.reachedFailure = true
        return marked
    }

    /// The latest completed row, by tick then by plan order: the Up next anchor.
    mutating func recomputeLastCompleted() {
        let sequence = UpNext.sequence(self)
        let place = Dictionary(sequence.enumerated().map { ($1, $0) }, uniquingKeysWith: { first, _ in first })
        lastCompleted = rows.values.filter(\.isCompleted).max { a, b in
            let (ta, tb) = (a.performed!.completedAt, b.performed!.completedAt)
            if ta != tb { return ta < tb }
            return (place[a.id] ?? 0) < (place[b.id] ?? 0)
        }?.id
    }

    /// Renumbers a chain's positions 1, 2, 3 in display order.
    mutating func renumberChain(_ head: RowID) {
        for (offset, row) in chain(of: head).enumerated() {
            rows[row.id]?.chainPosition = offset + 1
        }
    }

    /// Copies the head's role, label and (off a side chain) structure to its segments.
    mutating func syncChain(_ head: RowID) {
        guard let headRow = rows[head] else { return }
        for row in chain(of: head) where row.id != head {
            rows[row.id]?.role = headRow.role
            rows[row.id]?.roleLabel = headRow.roleLabel
            if row.side == nil { rows[row.id]?.structure = headRow.structure }
        }
    }

    /// Keeps warm-up units first in a slot, otherwise in their order (invariant 3).
    mutating func sortWarmupsFirst(slot key: String) {
        guard let index = slotIndex(key) else { return }
        let units = units(inSlot: key)
        let warmups = units.filter { rows[$0[0]]?.role == .warmup }
        let working = units.filter { rows[$0[0]]?.role != .warmup }
        slots[index].rows = (warmups + working).flatMap(\.self)
    }

    /// Moves each group's members to its first member's position, keeping
    /// their order, so a group stays contiguous (invariant 4).
    mutating func gatherGroups() {
        var result: [SlotState] = []
        var placed: Set<String> = []
        for slot in slots {
            guard let key = slot.group else {
                result.append(slot)
                continue
            }
            guard !placed.contains(key) else { continue }
            placed.insert(key)
            result.append(contentsOf: slots.filter { $0.group == key })
        }
        slots = result
    }

    /// Removes groups left with fewer than two members, clears the remaining
    /// member's group, and lets a group made in the gym follow its member
    /// count. A replacement and the slot it replaced count as one member.
    mutating func pruneGroups() {
        for group in groups {
            let members = slots.filter { $0.group == group.key }
            let lanes = Set(members.map(\.lane)).count
            if members.count < 2 {
                for index in slots.indices where slots[index].group == group.key {
                    slots[index].group = nil
                }
                groups.removeAll { $0.key == group.key }
            } else if group.madeInGym, !group.kindSetByHand, let index = groups.firstIndex(where: { $0.key == group.key }) {
                groups[index].groupKind = lanes <= 2 ? .superset : lanes == 3 ? .triset : .giantset
            }
        }
    }

    /// Adds or removes second sides so a slot's rows match its exercise's
    /// laterality (a replaced exercise may differ). Completed chains stay.
    mutating func normalizeSides(slot key: String, firstSide: Side, ids: inout RowIDPool) throws(EngineError) {
        guard let index = slotIndex(key), let exercise = exercise(ofSlot: key) else { return }
        let unilateral = exercise.resolvedLaterality == .unilateral
        for unit in units(inSlot: key) {
            guard let head = rows[unit[0]], head.isPending else { continue }
            let sided = head.side != nil
            if unilateral, !sided {
                rows[head.id]?.side = firstSide
                rows[head.id]?.structure = .straight
                removeRows(Set(unit.dropFirst()))
                let second = SessionEngine.secondSide(of: rows[head.id]!, id: ids.next())
                try insert([second], inSlot: index, at: positionAfterChain(head.id) ?? slots[index].rows.count)
            } else if !unilateral, sided {
                rows[head.id]?.side = nil
                removeRows(Set(unit.dropFirst()))
            }
        }
    }
}
