import Foundation

// A routine's plan as a started session draws it (§7.3, §7.6a), read only:
// the same blocks, set lines, labels, round headers, chain headers and tags
// as the Rows layout, each line one planned set's targets with last time
// beside it. The routine page draws it, and edits it in place: each line
// names its planned set (`RoutinePlanSetRef`), and `RoutinePlanSetEditor`
// is the open set's fields, each step a change to one planned field.

/// A planned set: its slot's key and its own.
public struct RoutinePlanSetRef: Hashable, Sendable {
    public var slot: String
    public var set: String

    public init(slot: String, set: String) {
        self.slot = slot
        self.set = set
    }

    /// The line's id. The separator is no character a key holds.
    public var row: RowID { RowID("plan\u{1F}\(slot)\u{1F}\(set)") }
}

/// The routine page's plan.
public struct RoutinePlanPresentation: Equatable, Sendable {
    public var id: RoutineID
    /// A plain slot, or a group, in the routine's order (`RoutineEditor.blocks`).
    public var blocks: [RoutinePlanBlock]
    /// Slots whose exercise could not be read: a session leaves them out.
    public var unreadable: [String]
    /// Each line's planned set, by the line's id.
    public var refs: [RowID: RoutinePlanSetRef]

    public init(id: RoutineID, blocks: [RoutinePlanBlock], unreadable: [String], refs: [RowID: RoutinePlanSetRef]) {
        self.id = id
        self.blocks = blocks
        self.unreadable = unreadable
        self.refs = refs
    }

    public var isEmpty: Bool { blocks.isEmpty }

    public func ref(_ row: RowID) -> RoutinePlanSetRef? { refs[row] }

    public static let empty = "No exercises yet."
}

/// One card of the plan.
public struct RoutinePlanBlock: Equatable, Sendable, Identifiable {
    /// `RoutineEditor.Block.id`: `slot-<key>` or `group-<key>`.
    public var id: String
    /// The group's key; nil for a plain slot.
    public var group: String?
    /// The slots in the block, in member order.
    public var members: [RoutinePlanMember]
    /// `Leg Press (Machine)`, `Superset`.
    public var title: String
    /// `10–15 reps at RPE 8, rest 2:00`, `Glute Bridge and Bird Dog, 2 rounds, rest 1:00 after each round`.
    public var subtitle: String?
    /// The slots' notes, each after its exercise's name in a group.
    public var notes: String?
    /// The lines in the order a session does them: warm-ups, then the
    /// working sets (by round in a group), a chain's header above its set.
    public var items: [RowsItem]
    /// `Superset, Glute Bridge and Bird Dog`.
    public var spokenTitle: String

    public init(
        id: String, group: String?, members: [RoutinePlanMember], title: String, subtitle: String?, notes: String?,
        items: [RowsItem], spokenTitle: String
    ) {
        self.id = id
        self.group = group
        self.members = members
        self.title = title
        self.subtitle = subtitle
        self.notes = notes
        self.items = items
        self.spokenTitle = spokenTitle
    }

    /// The set lines, in order.
    public var lines: [RowsSetItem] {
        items.compactMap { item in
            if case let .set(set) = item { set } else { nil }
        }
    }
}

/// One exercise of a block.
public struct RoutinePlanMember: Equatable, Sendable, Identifiable {
    public var slot: String
    public var exercise: ExerciseID
    /// `Glute Bridge`; the exercise's id when the catalogue lacks it.
    public var name: String
    /// The catalogue holds the exercise: its history opens.
    public var isKnown: Bool

    public init(slot: String, exercise: ExerciseID, name: String, isKnown: Bool) {
        self.slot = slot
        self.exercise = exercise
        self.name = name
        self.isKnown = isKnown
    }

    public var id: String { slot }
}

public enum RoutinePlanPresenter {
    /// The plan of `routine`: each planned set as a pending session line,
    /// its targets as values, the plan's effort beside them and last time's
    /// set at the end. `rests` are the defaults a slot without its own rest
    /// takes, as a session would.
    public static func present(
        _ routine: Routine, exercises: [ExerciseID: Exercise], history: HistoryIndex,
        rests: SessionSettings = SessionSettings()
    ) -> RoutinePlanPresentation {
        var builder = PlanBuilder(routine: routine, exercises: exercises, history: history, rests: rests)
        let blocks = builder.blocks()
        return RoutinePlanPresentation(
            id: routine.id,
            blocks: blocks,
            unreadable: routine.slots.filter { exercises[$0.exercise] == nil }.map(\.exercise.rawValue),
            refs: builder.refs
        )
    }

    /// A planned set's targets as its line shows them: `90 kg × 10–15`,
    /// `10–15 reps`, `12 per side`, `BW + 10 kg × 8`, `0:30–0:45 hold`,
    /// `20 kg for 40 m`. A set that names only how hard reads `to RIR 2–3`
    /// (`aimsAtEffort`), one to go as far as it can `max reps`, one that
    /// names nothing `any reps`.
    public static func values(_ planned: PlannedSet, measurement: MeasurementType, perSide: Bool) -> String {
        let reps: String? =
            switch (planned.repsMin, planned.repsMax) {
            case let (min?, max?): Format.range(min, max)
            case let (min?, nil): "\(min)+"
            case let (nil, max?): "\u{2264}\(max)"
            case (nil, nil): nil
            }
        let load = loadText(planned, measurement: measurement)
        let time = planned.durationMin.map { Format.durationRange($0, planned.durationMax ?? $0) }
        switch measurement {
        case .duration, .weightduration:
            switch (load, time) {
            case let (load?, time?): return "\(load) for \(time)"
            case let (nil, time?): return "\(time) hold"
            case let (load?, nil): return load
            case (nil, nil): return "hold"
            }
        case .distanceduration, .shortdistanceweight:
            var text = planned.distance.map(Format.length) ?? ""
            if let time { text += text.isEmpty ? time : " in \(time)" }
            if let load { text = text.isEmpty ? load : "\(load) for \(text)" }
            return text.isEmpty ? "any distance" : text
        case .weightreps, .repsonly, .bodyweightreps, .weightedbodyweightreps, .bodyweightassistedreps:
            switch (load, reps) {
            case let (load?, reps?): return "\(load) \(RowsText.times) \(reps)"
            case let (nil, reps?): return perSide ? "\(reps) per side" : reps == "1" ? "1 rep" : "\(reps) reps"
            case let (load?, nil): return load
            case (nil, nil):
                if planned.termination == .amrap { return "max reps" }
                if aimsAtEffort(planned, measurement: measurement), let effort = effort(planned) {
                    return "to \(effort.replacingOccurrences(of: "@ ", with: "RPE "))"
                }
                return "any reps"
            }
        }
    }

    /// The load as the lifter sets it, or a percentage of a reference; nil
    /// for a bare `BW`, which says nothing a bodyweight exercise does not.
    static func loadText(_ planned: PlannedSet, measurement: MeasurementType) -> String? {
        var load = Format.load(SetValues(load: planned.load, assistance: planned.assistance), mode: measurement.loadMode)
        if load == "BW" { load = nil }
        if load == nil, measurement.loadMode == .external { load = percentText(planned) }
        return load
    }

    /// The set counts reps but names neither them nor a load, only an
    /// effort: its values say the effort, which then stands nowhere else.
    static func aimsAtEffort(_ planned: PlannedSet, measurement: MeasurementType) -> Bool {
        measurement.recordsReps && planned.repsMin == nil && planned.repsMax == nil && planned.termination != .amrap
            && loadText(planned, measurement: measurement) == nil && effort(planned) != nil
    }

    /// `50% 1RM`: a percentage load, short, for a line.
    static func percentText(_ planned: PlannedSet) -> String? {
        planned.loadPercent.map { percent in
            let of: String =
                switch planned.loadReference {
                case .onerm, nil: "1RM"
                case .e1rm: "e1RM"
                case .trainingmax: "TM"
                case .bodymass: "BW"
                }
            return "\(Format.number(percent))% \(of)"
        }
    }

    /// The plan's effort beside the values, as a session writes a rating:
    /// `@ 8`, `@ 7–8`; reps in reserve as `RIR 2`, `RIR 2–3`.
    public static func effort(_ planned: PlannedSet) -> String? {
        if let range = Format.rpeRange(planned.rpeMin, planned.rpeMax) { return "@ \(range)" }
        return Format.rpeRange(planned.rirMin, planned.rirMax).map { "RIR \($0)" }
    }
}

/// One plan's work.
struct PlanBuilder {
    let routine: Routine
    let exercises: [ExerciseID: Exercise]
    let history: HistoryIndex
    let rests: SessionSettings
    var refs: [RowID: RoutinePlanSetRef] = [:]

    init(routine: Routine, exercises: [ExerciseID: Exercise], history: HistoryIndex, rests: SessionSettings) {
        self.routine = routine
        self.exercises = exercises
        self.history = history
        self.rests = rests
    }

    mutating func blocks() -> [RoutinePlanBlock] {
        let declared = Set(routine.groups.map(\.key))
        var out: [RoutinePlanBlock] = []
        var placed: Set<String> = []
        for slot in routine.slots {
            guard let key = slot.group, declared.contains(key) else {
                out.append(plain(slot))
                continue
            }
            guard placed.insert(key).inserted, let group = routine.groups.first(where: { $0.key == key }) else { continue }
            out.append(self.group(group, members: routine.slots.filter { $0.group == key }))
        }
        return out
    }

    func member(_ slot: RoutineSlot) -> RoutinePlanMember {
        let exercise = exercises[slot.exercise]
        return RoutinePlanMember(
            slot: slot.key, exercise: slot.exercise, name: exercise?.name ?? slot.exercise.rawValue,
            isKnown: exercise != nil)
    }

    // MARK: - Blocks

    mutating func plain(_ slot: RoutineSlot) -> RoutinePlanBlock {
        let member = member(slot)
        let last = lastSets(slot)
        var items: [RowsItem] = []
        for planned in slot.sets where planned.role == .warmup {
            items.append(.set(line(planned, in: slot, group: nil, last: last)))
        }
        for planned in slot.sets where planned.role == .working {
            if let header = chainHeader(planned, in: slot) { items.append(.chainHeader(header)) }
            items.append(.set(line(planned, in: slot, group: nil, last: last)))
        }
        return RoutinePlanBlock(
            id: "slot-\(slot.key)", group: nil, members: [member], title: member.name,
            subtitle: subtitle(slot), notes: slot.notes, items: items, spokenTitle: member.name)
    }

    mutating func group(_ group: RoutineGroup, members slots: [RoutineSlot]) -> RoutinePlanBlock {
        let members = slots.map(member)
        let lasts = Dictionary(slots.map { ($0.key, lastSets($0)) }, uniquingKeysWith: { first, _ in first })
        var items: [RowsItem] = []
        for slot in slots {
            for planned in slot.sets where planned.role == .warmup {
                items.append(.set(line(planned, in: slot, group: group, last: lasts[slot.key] ?? [:])))
            }
        }
        let working = slots.map { slot in slot.sets.filter { $0.role == .working } }
        let rounds = working.map(\.count).max() ?? 0
        for round in 0 ..< rounds {
            items.append(
                .round(
                    RowsRoundHeader(
                        id: "group-\(group.key)-round-\(round + 1)", round: round + 1, rounds: rounds,
                        title: "Round \(round + 1) of \(rounds)", isDone: false)))
            for (slot, sets) in zip(slots, working) where round < sets.count {
                if let header = chainHeader(sets[round], in: slot) { items.append(.chainHeader(header)) }
                items.append(.set(line(sets[round], in: slot, group: group, last: lasts[slot.key] ?? [:])))
            }
        }
        let names = members.map(\.name)
        let list = names.count == 1 ? names[0] : names.dropLast().joined(separator: ", ") + " and " + (names.last ?? "")
        let rest =
            group.roundRest
            ?? RestDefaults.roundRest(
                kind: group.groupKind, memberRestClasses: slots.map { exercises[$0.exercise]?.restClass })
        var parts = [list]
        if let count = group.rounds { parts.append(Format.count(count, "round")) }
        parts.append(rest > 0 ? "rest \(Format.duration(rest)) after each round" : "no rest between rounds")
        let notes = zip(slots, members).compactMap { slot, member in slot.notes.map { "\(member.name): \($0)" } }
        let title = group.groupKind.label
        return RoutinePlanBlock(
            id: "group-\(group.key)", group: group.key, members: members, title: title,
            subtitle: parts.joined(separator: ", "), notes: notes.isEmpty ? nil : notes.joined(separator: "\n"),
            items: items, spokenTitle: "\(title), \(list)")
    }

    /// `10–15 reps at RPE 8, rest 2:00, tempo 3-1-1-0`: the first working
    /// set's aim, the slot's rest and the tempo, as a session's card says it.
    func subtitle(_ slot: RoutineSlot) -> String? {
        var parts: [String] = []
        if let first = slot.sets.first(where: { $0.role == .working }) {
            var aim: String?
            switch (first.repsMin, first.repsMax) {
            case let (min?, max?): aim = "\(Format.range(min, max)) \(min == max && min == 1 ? "rep" : "reps")"
            case let (min?, nil): aim = "\(min) or more reps"
            case let (nil, max?): aim = "up to \(max) reps"
            case (nil, nil):
                if let min = first.durationMin { aim = "\(Format.durationRange(min, first.durationMax ?? min)) holds" }
            }
            let effort = Format.effort(rpeMin: first.rpeMin, rpeMax: first.rpeMax, rirMin: first.rirMin, rirMax: first.rirMax)
            switch (aim, effort) {
            case let (aim?, effort?): parts.append("\(aim) at \(effort)")
            case let (aim?, nil): parts.append(aim)
            case let (nil, effort?): parts.append("each set to \(effort)")
            case (nil, nil): break
            }
        }
        let rest = slot.setRest ?? rests.setRest[exercises[slot.exercise]?.restClass]
        parts.append(rest > 0 ? "rest \(Format.duration(rest))" : "no rest")
        if let tempo = slot.sets.lazy.compactMap(\.tempo).first { parts.append("tempo \(tempo)") }
        return parts.joined(separator: ", ").capitalizedFirst
    }

    /// `Set 2 as myo-reps, 0:15 between mini-sets`, above a chain's set; a
    /// drop says so in its tag instead, as a session's pending drop does.
    func chainHeader(_ planned: PlannedSet, in slot: RoutineSlot) -> RowsChainHeader? {
        let (what, noun): (String, String)
        switch planned.structure {
        case .myo: (what, noun) = ("myo-reps" + RoutinePresenter.totalText(planned), "mini-sets")
        case .myomatch: (what, noun) = ("a myo-rep match", "mini-sets")
        case .restpause: (what, noun) = ("rest-pause, \(Format.count(planned.miniSets ?? 2, "mini-set"))", "mini-sets")
        case .cluster:
            let clusters = (planned.miniSets ?? SessionEngine.defaultClusters) + 1
            (what, noun) = ("\(clusters) clusters" + (planned.miniSetReps.map { " of \($0)" } ?? ""), "clusters")
        case .straight, .drop: return nil
        }
        let pause = planned.intraSetPause ?? RestDefaults.intraSetPause(planned.structure)
        let gap = pause > 0 ? "\(Format.duration(pause)) between \(noun)" : "no rest between \(noun)"
        let ref = RoutinePlanSetRef(slot: slot.key, set: planned.key)
        return RowsChainHeader(
            id: "\(ref.row.rawValue)-chain", head: ref.row,
            text: "\(setTitle(planned, in: slot)) as \(what), \(gap)")
    }

    // MARK: - Lines

    /// `Set 2`, `Warm-up`, `Warm-up 2`.
    func setTitle(_ planned: PlannedSet, in slot: RoutineSlot) -> String {
        let peers = slot.sets.filter { $0.role == planned.role }
        let ordinal = (peers.firstIndex { $0.key == planned.key } ?? 0) + 1
        guard planned.role == .warmup else { return "Set \(ordinal)" }
        return peers.count > 1 ? "Warm-up \(ordinal)" : "Warm-up"
    }

    mutating func line(
        _ planned: PlannedSet, in slot: RoutineSlot, group: RoutineGroup?,
        last: [String: SetValues]
    ) -> RowsSetItem {
        let ref = RoutinePlanSetRef(slot: slot.key, set: planned.key)
        refs[ref.row] = ref
        let exercise = exercises[slot.exercise]
        let measurement = exercise?.measurement ?? .weightreps
        let perSide = exercise?.resolvedLaterality == .alternating
        let name = exercise?.name ?? slot.exercise.rawValue
        let setTitle = setTitle(planned, in: slot)
        let title = group == nil ? setTitle : "\(name), \(setTitle.lowercased())"
        let working = slot.sets.filter { $0.role == .working }
        let label = planned.role == .warmup ? "W" : "\((working.firstIndex { $0.key == planned.key } ?? 0) + 1)"
        var tags: [RowsTag] = []
        if planned.structure == .drop {
            let drops = planned.drops ?? 1
            tags.append(RowsTag(text: drops == 1 ? "+ drop" : "+ \(drops) drops", style: .orange))
        }
        let values = RoutinePlanPresenter.values(planned, measurement: measurement, perSide: perSide)
        switch planned.termination {
        case .failure: tags.append(RowsTag(text: "to failure", style: .red))
        case .amrap where values != "max reps": tags.append(RowsTag(text: "max reps", style: .grey))
        default: break
        }
        switch planned.roleLabel {
        case .top: tags.append(RowsTag(text: "top set", style: .grey))
        case .backoff: tags.append(RowsTag(text: "back-off", style: .grey))
        case nil: break
        }
        if let rest = planned.rest { tags.append(RowsTag(text: "rest \(Format.rest(rest))", style: .grey)) }
        let previous = last[planned.key]
        let lastText = previous.map { RowsText.compact($0, measurement: measurement, perSide: perSide) }
        var spoken = "\(title): \(spokenTarget(planned, measurement: measurement, perSide: perSide))"
        if !tags.isEmpty { spoken += ", " + tags.map(\.text).joined(separator: ", ").replacingOccurrences(of: "+ ", with: "with ") }
        spoken += "."
        if let previous {
            spoken += " Last time \(RowsText.spoken(previous, measurement: measurement, perSide: perSide))."
        }
        return RowsSetItem(
            id: ref.row, slot: slot.key, state: .next, role: planned.role == .warmup ? .warmup : .working, title: title,
            setLabel: label, caption: group == nil ? nil : name, isChained: false, values: values, valuesChanged: false,
            rpe: RoutinePlanPresenter.aimsAtEffort(planned, measurement: measurement) ? nil : RoutinePlanPresenter.effort(planned),
            rpeIsPrefilled: false, tags: tags, collapsedTag: nil,
            last: lastText.map { "last \($0)" }, changes: [], verdict: nil, sentence: spoken, tap: .none, editor: nil, fix: nil
        )
    }

    /// `90 kilograms, 10 to 15 reps, RPE 8`.
    func spokenTarget(_ planned: PlannedSet, measurement: MeasurementType, perSide: Bool) -> String {
        var parts: [String] = []
        if let load = planned.load { parts.append(RowsText.spokenMass(load)) }
        if let assistance = planned.assistance { parts.append("\(RowsText.spokenMass(assistance)) of assistance") }
        if planned.load == nil, let percent = planned.loadPercent {
            parts.append("\(Format.number(percent)) percent of \(planned.loadReference.map(Format.loadReference) ?? "one-rep max")")
        }
        let each = perSide ? " per side" : ""
        switch (planned.repsMin, planned.repsMax) {
        case let (min?, max?): parts.append(min == max ? "\(Format.count(min, "rep"))\(each)" : "\(min) to \(max) reps\(each)")
        case let (min?, nil): parts.append("at least \(Format.count(min, "rep"))\(each)")
        case let (nil, max?): parts.append("up to \(Format.count(max, "rep"))\(each)")
        case (nil, nil): break
        }
        if let min = planned.durationMin {
            let max = planned.durationMax ?? min
            parts.append(
                min == max
                    ? "hold \(RowsText.spokenDuration(min))"
                    : "hold \(RowsText.spokenDuration(min)) to \(RowsText.spokenDuration(max))")
        }
        if let distance = planned.distance { parts.append("\(Format.number(distance.metres)) metres") }
        if let effort = Format.effort(rpeMin: planned.rpeMin, rpeMax: planned.rpeMax, rirMin: planned.rirMin, rirMax: planned.rirMax) {
            parts.append(effort.replacingOccurrences(of: "\u{2013}", with: " to "))
        }
        return parts.isEmpty ? "no target" : parts.joined(separator: ", ")
    }

    /// Last time's set for each planned set of the slot, as PREVIOUS reads
    /// it (§5.4): the newest session with the exercise, its n-th slot of it
    /// for the routine's n-th, the set of the same role and ordinal; a
    /// sided set's first side; the RPE only when it was a rating.
    func lastSets(_ slot: RoutineSlot) -> [String: SetValues] {
        let same = routine.slots.filter { $0.exercise == slot.exercise }.map(\.key)
        guard let n = same.firstIndex(of: slot.key), let last = history.last(slot.exercise), n < last.slots.count else { return [:] }
        let sets = last.sets(inSlot: last.slots[n]).filter { $0.continues == nil }
        let ordinals = SetLabel.ordinals(records: sets)
        var out: [String: SetValues] = [:]
        for role in [Role.warmup, .working] {
            let planned = slot.sets.filter { $0.role == role }
            for (index, set) in planned.enumerated() {
                let match = sets.first { $0.role == role && ordinals[$0.id] == index + 1 }
                guard let match else { continue }
                out[set.key] = SetValues(
                    load: match.load, assistance: match.assistance, reps: match.reps, duration: match.duration,
                    distance: match.distance, rpe: match.ratedRPE)
            }
        }
        return out
    }
}
