import Foundation

/// The Routines tab (§7.2).
public struct RoutineListPresentation: Equatable, Sendable {
    /// Folders by name, then "Other routines" for those with none.
    public var sections: [RoutineSection]
    /// `2 routines could not be read`, the footer that links to Settings; nil when all read.
    public var unreadable: String?

    public init(sections: [RoutineSection], unreadable: String?) {
        self.sections = sections
        self.unreadable = unreadable
    }

    public var isEmpty: Bool { sections.isEmpty }

    public static let emptyText =
        "No routines yet. Routines are written in the substrate console; this build bundles the owner's folder."
    public static let otherRoutines = "Other routines"
}

/// One folder of routines.
public struct RoutineSection: Equatable, Sendable, Identifiable {
    /// The folder's name.
    public var title: String
    /// `4 routines`, the header's separate secondary label.
    public var count: String
    public var routines: [RoutineCardPresentation]

    public init(title: String, count: String, routines: [RoutineCardPresentation]) {
        self.title = title
        self.count = count
        self.routines = routines
    }

    public var id: String { title }
}

/// A routine's card.
public struct RoutineCardPresentation: Equatable, Sendable, Identifiable {
    public var id: RoutineID
    public var name: String
    /// Its exercises' names joined by commas; the card shows two lines of it.
    public var exercises: String
    /// `Last done Tue 27 Oct`; nil when never done.
    public var lastDone: String?

    public init(id: RoutineID, name: String, exercises: String, lastDone: String?) {
        self.id = id
        self.name = name
        self.exercises = exercises
        self.lastDone = lastDone
    }
}

/// The routine preview (§7.3).
public struct RoutinePreviewPresentation: Equatable, Sendable, Identifiable {
    public var id: RoutineID
    public var name: String
    public var notes: String?
    /// `Last done Tue 27 Oct, 46 min`; nil when never done.
    public var lastDone: String?
    public var blocks: [RoutinePreviewBlock]
    /// Slots whose exercise could not be read: a session leaves them out.
    public var unreadable: [String]

    public init(id: RoutineID, name: String, notes: String?, lastDone: String?, blocks: [RoutinePreviewBlock],
                unreadable: [String])
    {
        self.id = id
        self.name = name
        self.notes = notes
        self.lastDone = lastDone
        self.blocks = blocks
        self.unreadable = unreadable
    }
}

/// A plain slot, or a group with its bracket and header.
public struct RoutinePreviewBlock: Equatable, Sendable, Identifiable {
    public var id: String
    /// `Superset A, rest 1:00 after each round`; nil for a plain slot.
    public var header: String?
    /// The group's colour, as in the session (`GroupHeader.paletteSize`).
    public var colorIndex: Int?
    public var slots: [RoutinePreviewSlot]

    public init(id: String, header: String?, colorIndex: Int?, slots: [RoutinePreviewSlot]) {
        self.id = id
        self.header = header
        self.colorIndex = colorIndex
        self.slots = slots
    }
}

public struct RoutinePreviewSlot: Equatable, Sendable, Identifiable {
    public var id: String
    public var name: String
    /// `A1`.
    public var groupLabel: String?
    /// `Warm-up: 40 kg × 12`, `2 sets of 10–15 reps at RPE 8, from 90 kg`,
    /// `Set 2 is a drop set: 1 drop`, `Rest 2:00`, `Tempo 3-1-1-0`.
    public var lines: [String]
    public var notes: String?

    public init(id: String, name: String, groupLabel: String?, lines: [String], notes: String?) {
        self.id = id
        self.name = name
        self.groupLabel = groupLabel
        self.lines = lines
        self.notes = notes
    }
}

public enum RoutinePresenter {
    /// The Routines list: one section per folder, by name; routines by
    /// `position`, then name; routines with no folder last.
    public static func list(_ catalog: Catalog, history: HistoryIndex, settings: PresenterSettings) -> RoutineListPresentation {
        let byFolder = Dictionary(grouping: catalog.routines.values) { $0.folder ?? "" }
        let folders = byFolder.keys.filter { !$0.isEmpty }.sorted { $0.localizedStandardCompare($1) == .orderedAscending }
            + (byFolder[""] == nil ? [] : [""])
        let sections = folders.map { folder in
            let routines = (byFolder[folder] ?? []).sorted { a, b in
                (a.position ?? .max, a.name, a.id.rawValue) < (b.position ?? .max, b.name, b.id.rawValue)
            }
            return RoutineSection(
                title: folder.isEmpty ? RoutineListPresentation.otherRoutines : folder,
                count: Format.count(routines.count, "routine"),
                routines: routines.map { routine in
                    RoutineCardPresentation(
                        id: routine.id,
                        name: routine.name,
                        exercises: routine.slots.compactMap { catalog.exercises[$0.exercise]?.name }.joined(separator: ", "),
                        lastDone: lastSession(of: routine.id, history: history).map { session in
                            "Last done \(Format.day(session.at, in: HistoryPresenter.zone(of: session, settings)))"
                        }
                    )
                }
            )
        }
        let unreadable = catalog.unreadable.count { $0.kind == KindNames.v1.routine }
        return RoutineListPresentation(
            sections: sections,
            unreadable: unreadable == 0 ? nil : "\(Format.count(unreadable, "routine")) could not be read"
        )
    }

    /// The newest finished session run from the routine.
    public static func lastSession(of routine: RoutineID, history: HistoryIndex) -> TrainingSession? {
        history.sessions.first { $0.session.routine == routine }?.session
    }

    /// The routine preview: each slot's planned sets in words, groups with
    /// their labels, the rests the routine names, tempo and notes.
    public static func preview(_ routine: Routine, catalog: Catalog, history: HistoryIndex,
                               settings: PresenterSettings) -> RoutinePreviewPresentation
    {
        let lastDone = lastSession(of: routine.id, history: history).map { session in
            var text = "Last done \(Format.day(session.at, in: HistoryPresenter.zone(of: session, settings)))"
            if let end = session.endsAt {
                text += ", \(Format.minutes(SessionPresenter.activeSeconds(from: session.at, to: end, pauses: session.pauses)))"
            }
            return text
        }
        let groupKeys = Set(routine.groups.map(\.key))
        var blocks: [RoutinePreviewBlock] = []
        var placed: Set<String> = []
        var colors = 0
        for slot in routine.slots {
            guard let key = slot.group, groupKeys.contains(key) else {
                blocks.append(RoutinePreviewBlock(id: slot.key, header: nil, colorIndex: nil,
                                                  slots: [previewSlot(slot, routine: routine, catalog: catalog, label: nil)]))
                continue
            }
            guard !placed.contains(key) else { continue }
            placed.insert(key)
            let members = routine.slots.filter { $0.group == key }
            let group = routine.groups.first { $0.key == key }!
            let rest = group.roundRest ?? RestDefaults.roundRest(
                kind: group.groupKind, memberRestClasses: members.map { catalog.exercises[$0.exercise]?.restClass })
            let restText = rest > 0 ? "rest \(Format.duration(rest)) after each round" : "no rest between rounds"
            blocks.append(RoutinePreviewBlock(
                id: "group-\(key)",
                header: "\(group.groupKind.label) \(key.uppercased()), \(restText)",
                colorIndex: colors % GroupHeader.paletteSize,
                slots: members.enumerated().map { index, member in
                    previewSlot(member, routine: routine, catalog: catalog,
                                label: groupMemberLabel(group: key, position: index + 1),
                                nextLabel: index + 1 < members.count ? groupMemberLabel(group: key, position: index + 2) : nil)
                }
            ))
            colors += 1
        }
        return RoutinePreviewPresentation(
            id: routine.id,
            name: routine.name,
            notes: routine.notes,
            lastDone: lastDone,
            blocks: blocks,
            unreadable: routine.slots.filter { catalog.exercises[$0.exercise] == nil }.map(\.exercise.rawValue)
        )
    }

    static func previewSlot(_ slot: RoutineSlot, routine: Routine, catalog: Catalog, label: String?,
                            nextLabel: String? = nil) -> RoutinePreviewSlot
    {
        let exercise = catalog.exercises[slot.exercise]
        let measurement = exercise?.measurement ?? .weightreps
        var lines: [String] = []
        let warmups = slot.sets.filter { $0.role == .warmup }
        let working = slot.sets.filter { $0.role == .working }
        for planned in warmups {
            let text = plannedText(planned, measurement: measurement)
            lines.append(text.isEmpty ? "Warm-up" : "Warm-up: \(text)")
        }
        // Working sets in runs of the same plan; structures are named after.
        var runs: [(PlannedSet, Int)] = []
        for planned in working {
            if let last = runs.last, sameShape(last.0, planned) {
                runs[runs.count - 1].1 += 1
            } else {
                runs.append((planned, 1))
            }
        }
        for (planned, count) in runs {
            lines.append(runText(planned, count: count, measurement: measurement))
        }
        for (index, planned) in working.enumerated() {
            if let special = specialText(planned, number: index + 1) { lines.append(special) }
        }
        if let rest = slot.setRest { lines.append("Rest \(Format.rest(rest))") }
        if let rest = slot.warmupRest { lines.append("Warm-up rest \(Format.rest(rest))") }
        if let rest = slot.sideSwitch { lines.append("Switch sides \(Format.rest(rest))") }
        if let nextLabel, let rest = slot.transition { lines.append("Then \(Format.duration(rest)) to \(nextLabel)") }
        for (index, planned) in warmups.enumerated() {
            guard let rest = planned.rest else { continue }
            lines.append("Rest after \(warmups.count > 1 ? "warm-up \(index + 1)" : "the warm-up"): \(Format.rest(rest))")
        }
        for (index, planned) in working.enumerated() {
            guard let rest = planned.rest else { continue }
            lines.append("Rest after set \(index + 1): \(Format.rest(rest))")
        }
        if let tempo = slot.sets.lazy.compactMap(\.tempo).first { lines.append("Tempo \(tempo)") }
        return RoutinePreviewSlot(id: slot.key, name: exercise?.name ?? slot.exercise.rawValue, groupLabel: label, lines: lines,
                                  notes: slot.notes)
    }

    static func sameShape(_ a: PlannedSet, _ b: PlannedSet) -> Bool {
        a.repsMin == b.repsMin && a.repsMax == b.repsMax && a.rpeMin == b.rpeMin && a.rpeMax == b.rpeMax
            && a.rirMin == b.rirMin && a.rirMax == b.rirMax && a.load == b.load
            && a.loadPercent == b.loadPercent && a.loadReference == b.loadReference
            && a.assistance == b.assistance && a.durationMin == b.durationMin && a.durationMax == b.durationMax
            && a.distance == b.distance && a.timerMode == b.timerMode
            && (a.termination == .rirstop) == (b.termination == .rirstop)
            && (a.termination == .amrap) == (b.termination == .amrap)
    }

    /// A warm-up's plan as a set: `40 kg × 12`, `× 8`, `0:30`, `500 m`.
    static func plannedText(_ planned: PlannedSet, measurement: MeasurementType) -> String {
        let reps = planned.repsMin == planned.repsMax ? planned.repsMin : nil
        var text = Format.set(SetValues(load: planned.load, assistance: planned.assistance, reps: reps,
                                        duration: planned.durationMin, distance: planned.distance), measurement: measurement)
        if reps == nil, let min = planned.repsMin, let max = planned.repsMax {
            text += text.isEmpty ? "\(Format.range(min, max)) reps" : " \u{00D7} \(Format.range(min, max))"
        }
        return text
    }

    /// A run of working sets: `2 sets of 10–15 reps at RPE 8, from 90 kg`.
    static func runText(_ planned: PlannedSet, count: Int, measurement: MeasurementType) -> String {
        let sets = Format.count(count, "set")
        let load = planned.load != nil || planned.assistance != nil
            ? Format.load(SetValues(load: planned.load, assistance: planned.assistance), mode: measurement.loadMode)
            : nil
        let effortTarget = Format.effort(rpeMin: planned.rpeMin, rpeMax: planned.rpeMax, rirMin: planned.rirMin,
                                         rirMax: planned.rirMax)
        let effort = effortTarget.map { " at \($0)" } ?? ""
        if measurement.isTimedHold {
            var text = sets
            if let min = planned.durationMin {
                text += ": hold \(Format.durationRange(min, planned.durationMax ?? min))"
                text += planned.timerMode == .stopwatch ? ", on the stopwatch" : ", counted down"
            }
            if let load { text += ", with \(load)" }
            return text
        }
        if let distance = planned.distance {
            return "\(sets) of \(Format.length(distance))" + (load.map { ", with \($0)" } ?? "")
        }
        var text = sets
        if planned.termination == .rirstop, planned.repsMin == nil {
            text += effortTarget.map { ", each until \($0)" } ?? ""
        } else {
            switch (planned.repsMin, planned.repsMax) {
            case let (min?, max?): text += " of \(Format.range(min, max)) \(min == max && min == 1 ? "rep" : "reps")"
            case let (min?, nil): text += " of at least \(Format.count(min, "rep"))"
            case let (nil, max?): text += " of up to \(Format.count(max, "rep"))"
            case (nil, nil): break
            }
            text += effort
        }
        if let load {
            text += ", from \(load)"
        } else if let percent = planned.loadPercent {
            text += ", from \(Format.number(percent))% of \(planned.loadReference.map(Format.loadReference) ?? "the reference")"
        }
        return text
    }

    /// A working set's method in words: `Set 2 is a drop set: 1 drop`.
    static func specialText(_ planned: PlannedSet, number: Int) -> String? {
        let set = "Set \(number)"
        switch planned.structure {
        case .drop:
            return "\(set) is a drop set: \(Format.count(planned.drops ?? 1, "drop"))"
        case .myo:
            return "\(set) is myo-reps"
        case .myomatch:
            return "\(set) is a myo-rep match"
        case .restpause:
            return "\(set) is rest-pause: \(Format.count(planned.miniSets ?? 2, "mini-set"))"
        case .cluster:
            let clusters = (planned.miniSets ?? SessionEngine.defaultClusters) + 1
            return "\(set) is a cluster set: \(clusters) clusters" + (planned.miniSetReps.map { " of \($0)" } ?? "")
        case .straight:
            break
        }
        switch planned.termination {
        case .failure: return "\(set) to failure"
        case .amrap: return "\(set): as many reps as possible"
        default: break
        }
        switch planned.roleLabel {
        case .top: return "\(set) is the top set"
        case .backoff:
            let by = planned.dropStep.map(Format.mass) ?? planned.dropPercent.map { "\(Format.number($0))%" }
            return by.map { "\(set) backs off \($0)" } ?? "\(set) is a back-off set"
        case nil: return nil
        }
    }
}
