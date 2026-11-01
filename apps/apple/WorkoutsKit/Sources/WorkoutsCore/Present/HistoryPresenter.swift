import Foundation

/// A month of the History list (§7.8).
public struct HistorySection: Equatable, Sendable, Identifiable {
    /// `October 2026`.
    public var title: String
    public var rows: [HistoryRow]

    public init(title: String, rows: [HistoryRow]) {
        self.title = title
        self.rows = rows
    }

    public var id: String { title }
}

/// One finished session in the History list.
public struct HistoryRow: Equatable, Sendable, Identifiable {
    public var id: SessionID
    public var start: Date
    /// The name's routine part: `Fully Body Collection #1`.
    public var title: String
    /// `Tue 27 Oct, 13:30`, on the session's own clock.
    public var date: String
    /// `46 min, 24 sets, 3,120 kg`.
    public var summary: String
    /// The trailing glyph: saved, pending, not saved (none when Health is not available).
    public var health: HealthSaveStatus
    public var isDeload: Bool

    public init(id: SessionID, start: Date, title: String, date: String, summary: String, health: HealthSaveStatus,
                isDeload: Bool)
    {
        self.id = id
        self.start = start
        self.title = title
        self.date = date
        self.summary = summary
        self.health = health
        self.isDeload = isDeload
    }
}

/// The session detail (§7.8), read-only in V1.
public struct SessionDetailPresentation: Equatable, Sendable, Identifiable {
    public var id: SessionID
    public var title: String
    /// `Tue 27 Oct 2026, 13:30 to 14:16 (46 min)`.
    public var when: String
    /// `46 min, 24 sets, 3,120 kg`.
    public var summary: String
    /// `Session RPE 7`; nil when skipped.
    public var sessionRPE: String?
    public var health: HealthSaveStatus
    /// `Apple Health: saved`, `Saving to Apple Health`, `Apple Health: not saved`; nil when not available.
    public var healthText: String?
    /// "Not saved" offers Try again.
    public var canRetryHealth: Bool
    public var blocks: [SessionDetailBlock]
    /// `Chest 2, Triceps 1, Shoulders 1`, working sets per muscle; nil when none.
    public var muscles: String?
    public var notes: String?

    public init(id: SessionID, title: String, when: String, summary: String, sessionRPE: String?, health: HealthSaveStatus,
                healthText: String?, canRetryHealth: Bool, blocks: [SessionDetailBlock], muscles: String?, notes: String?)
    {
        self.id = id
        self.title = title
        self.when = when
        self.summary = summary
        self.sessionRPE = sessionRPE
        self.health = health
        self.healthText = healthText
        self.canRetryHealth = canRetryHealth
        self.blocks = blocks
        self.muscles = muscles
        self.notes = notes
    }
}

/// A plain exercise, or a group (`Superset A`) with its members.
public struct SessionDetailBlock: Equatable, Sendable, Identifiable {
    public var id: String
    /// `Superset A`; nil for a plain exercise.
    public var title: String?
    public var exercises: [SessionDetailExercise]

    public init(id: String, title: String?, exercises: [SessionDetailExercise]) {
        self.id = id
        self.title = title
        self.exercises = exercises
    }
}

public struct SessionDetailExercise: Equatable, Sendable, Identifiable {
    public var slot: String
    public var exercise: ExerciseID
    public var name: String
    /// `A1`.
    public var groupLabel: String?
    public var notes: String?
    public var sets: [SessionDetailSet]

    public init(slot: String, exercise: ExerciseID, name: String, groupLabel: String?, notes: String?, sets: [SessionDetailSet]) {
        self.slot = slot
        self.exercise = exercise
        self.name = name
        self.groupLabel = groupLabel
        self.notes = notes
        self.sets = sets
    }

    public var id: String { slot }
}

/// One set record in the session detail.
public struct SessionDetailSet: Equatable, Sendable, Identifiable {
    public var id: SetID
    public var label: SetLabel
    /// `35 kg × 13 @ 9.5`.
    public var text: String
    /// `rest 1:02`; nil on the first set and on sets with no measured rest.
    public var rest: String?
    public var notes: String?

    public init(id: SetID, label: SetLabel, text: String, rest: String?, notes: String?) {
        self.id = id
        self.label = label
        self.text = text
        self.rest = rest
        self.notes = notes
    }
}

public enum HistoryPresenter {
    public static let emptyTitle = "No sessions yet"
    public static let emptyMessage = "Finished sessions show up here, newest first."
    public static let deleteQuestion = "Delete this session? It stays in Apple Health."

    /// The History list: finished sessions newest first, in month sections.
    /// - Parameter exercises: the catalogue's, for volume (records do not copy them).
    public static func sections(_ entries: [HistoryEntry], exercises: [ExerciseID: Exercise],
                                settings: PresenterSettings) -> [HistorySection]
    {
        var sections: [HistorySection] = []
        let sorted = entries.sorted { $0.records.session.at > $1.records.session.at }
        for entry in sorted {
            let session = entry.records.session
            let zone = zone(of: session, settings)
            let month = Format.month(session.at, in: zone)
            let row = HistoryRow(id: session.id, start: session.at, title: routineTitle(of: session),
                                 date: Format.dayAndTime(session.at, in: zone),
                                 summary: summary(entry.records, exercises: exercises, settings: settings),
                                 health: entry.local.health.status, isDeload: session.deload == true)
            if sections.last?.title == month {
                sections[sections.count - 1].rows.append(row)
            } else {
                sections.append(HistorySection(title: month, rows: [row]))
            }
        }
        return sections
    }

    /// The session detail.
    public static func detail(_ entry: HistoryEntry, exercises: [ExerciseID: Exercise],
                              settings: PresenterSettings) -> SessionDetailPresentation
    {
        let records = entry.records
        let session = records.session
        let zone = zone(of: session, settings)
        var when = "\(Format.dayWithYear(session.at, in: zone)), \(Format.time(session.at, in: zone))"
        if let end = session.endsAt {
            let seconds = SessionPresenter.activeSeconds(from: session.at, to: end, pauses: session.pauses)
            when += " to \(Format.time(end, in: zone)) (\(Format.minutes(seconds)))"
        }
        let health = entry.local.health.status
        let healthText: String? = switch health {
        case .saved: "Apple Health: saved"
        case .pending: "Saving to Apple Health"
        case .notSaved: "Apple Health: not saved"
        case .notAvailable: nil
        }
        let muscles = Counting.setsPerMuscle(records.sets, exercises: exercises)
            .sorted { $0.value != $1.value ? $0.value > $1.value : $0.key.label < $1.key.label }
            .map { "\($0.key.label) \(Format.number($0.value, decimals: 1))" }
        return SessionDetailPresentation(
            id: session.id,
            title: routineTitle(of: session),
            when: when,
            summary: summary(records, exercises: exercises, settings: settings),
            sessionRPE: session.sessionRPE.map { "Session RPE \(Format.rpe($0))" },
            health: health,
            healthText: healthText,
            canRetryHealth: health == .notSaved,
            blocks: blocks(records, exercises: exercises),
            muscles: muscles.isEmpty ? nil : muscles.joined(separator: ", "),
            notes: session.notes
        )
    }

    /// `46 min, 24 sets, 3,120 kg`: the length with pauses left out, the
    /// working sets, and the volume when there is any.
    public static func summary(_ records: SessionRecords, exercises: [ExerciseID: Exercise],
                               settings: PresenterSettings) -> String
    {
        let session = records.session
        var parts: [String] = []
        if let end = session.endsAt {
            parts.append(Format.minutes(SessionPresenter.activeSeconds(from: session.at, to: end, pauses: session.pauses)))
        }
        parts.append(Format.count(Counting.workingSets(records.sets), "set"))
        let volume = Counting.volume(records.sets, exercises: exercises, bodyMass: session.bodyMass, unit: settings.defaultUnit,
                                     includeWarmups: settings.countWarmupsInVolume)
        if volume.value > 0 { parts.append(Format.volume(volume)) }
        return parts.joined(separator: ", ")
    }

    /// The name's routine part: the name without the date the app appends
    /// (`, Tue 27 Oct 13:30`), else the whole name, else "Training session".
    public static func routineTitle(of session: TrainingSession) -> String {
        guard let name = session.name else { return "Training session" }
        let zone = session.timezone.flatMap(TimeZone.init(identifier:))
            ?? TimeZone(secondsFromGMT: (session.utcOffsetStart ?? 0) * 60) ?? .gmt
        let suffix = String(TrainingSession.name(routine: "", start: session.at, timeZone: zone))
        if name.hasSuffix(suffix), name.count > suffix.count { return String(name.dropLast(suffix.count)) }
        return name
    }

    /// The session's slots as the detail lists them: groups as blocks, each
    /// exercise's sets in display order with their labels.
    static func blocks(_ records: SessionRecords, exercises: [ExerciseID: Exercise]) -> [SessionDetailBlock] {
        let session = records.session
        var blocks: [SessionDetailBlock] = []
        var placed: Set<String> = []
        var slots = session.slots
        // Sets whose slot the session record does not list (an importer's) still show.
        for set in records.sets where !slots.contains(where: { $0.key == set.slot }) {
            slots.append(SessionSlot(key: set.slot, exercise: set.exercise))
        }
        func exercise(_ slot: SessionSlot, label: String?) -> SessionDetailExercise {
            let sets = records.sets.filter { $0.slot == slot.key }.sorted { $0.order < $1.order }
            let labels = SetLabel.make(records: sets)
            let measurement = exercises[slot.exercise]?.measurement
            return SessionDetailExercise(
                slot: slot.key,
                exercise: slot.exercise,
                name: exercises[slot.exercise]?.name ?? slot.exercise.rawValue,
                groupLabel: label,
                notes: slot.notes,
                sets: sets.map { set in
                    SessionDetailSet(id: set.id, label: labels[set.id] ?? SetLabel(short: "?", spoken: "set"),
                                     text: text(of: set, measurement: measurement),
                                     rest: set.restBefore.map { "rest \(Format.duration($0))" }, notes: set.notes)
                }
            )
        }
        for slot in slots {
            guard let key = slot.group, session.groups.contains(where: { $0.key == key }) else {
                blocks.append(SessionDetailBlock(id: slot.key, title: nil, exercises: [exercise(slot, label: nil)]))
                continue
            }
            guard !placed.contains(key) else { continue }
            placed.insert(key)
            let kind = session.groups.first { $0.key == key }?.groupKind ?? .superset
            let members = slots.filter { $0.group == key }
            blocks.append(SessionDetailBlock(
                id: "group-\(key)",
                title: "\(kind.label) \(key.uppercased())",
                exercises: members.enumerated().map { exercise($1, label: groupMemberLabel(group: key, position: $0 + 1)) }
            ))
        }
        return blocks
    }

    /// One record as text, by its exercise's measurement, else by what it
    /// holds. Only a rated RPE is shown: a pre-filled or copied one is no rating.
    static func text(of set: PerformedSet, measurement: MeasurementType?) -> String {
        let values = SetValues(load: set.load, assistance: set.assistance, reps: set.reps, duration: set.duration,
                               distance: set.distance, rpe: set.ratedRPE)
        let inferred: MeasurementType = measurement ?? {
            switch (set.loadMode, set.reps != nil, set.distance != nil) {
            case (.bodyweight, _, _): .bodyweightreps
            case (.bodyweightplus, _, _): .weightedbodyweightreps
            case (.assisted, _, _): .bodyweightassistedreps
            case (.external, true, _): .weightreps
            case (.external, false, true): .shortdistanceweight
            case (.external, false, false): .weightduration
            case (nil, true, _): .repsonly
            case (nil, false, true): .distanceduration
            case (nil, false, false): .duration
            }
        }()
        return Format.set(values, measurement: inferred, rpe: true)
    }

    static func zone(of session: TrainingSession, _ settings: PresenterSettings) -> TimeZone {
        session.timezone.flatMap(TimeZone.init(identifier:)) ?? settings.timeZone
    }
}
