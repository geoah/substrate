import Foundation

/// A letter of the Exercises list and the picker (§7.9, §7.6).
public struct ExerciseSection: Equatable, Sendable, Identifiable {
    /// `A`, or `Recent` in the picker.
    public var title: String
    public var rows: [ExerciseRow]

    public init(title: String, rows: [ExerciseRow]) {
        self.title = title
        self.rows = rows
    }

    public var id: String { title }
}

/// An exercise in a list: the name, and under it secondary lines.
public struct ExerciseRow: Equatable, Sendable, Identifiable {
    public var id: ExerciseID
    public var name: String
    /// `Machine`; `Bodyweight` for a bodyweight exercise with no equipment;
    /// nil with no equipment otherwise (never "None", `Exercise.equipmentText`).
    public var equipment: String?
    /// `Last done Tue 27 Oct`; nil when never done.
    public var lastDone: String?
    /// Added on this phone: `On this iPhone`.
    public var isLocal: Bool

    public init(id: ExerciseID, name: String, equipment: String?, lastDone: String?, isLocal: Bool) {
        self.id = id
        self.name = name
        self.equipment = equipment
        self.lastDone = lastDone
        self.isLocal = isLocal
    }

    public static let localText = "On this iPhone"
}

/// The exercise detail (§7.9).
public struct ExerciseDetailPresentation: Equatable, Sendable, Identifiable {
    public var id: ExerciseID
    public var name: String
    /// Stacked label over value, never side by side: Equipment, Records,
    /// Sides, Muscles, and the load's steps (`Load`: `5 kg steps`,
    /// `2.5 kg steps from 20 kg`; `Added load` or `Assistance` on bodyweight exercises).
    public var facts: [LabelledValue]
    public var isLocal: Bool
    public var trend: ExerciseTrend
    /// Every session with the exercise, newest first.
    public var sessions: [ExerciseHistorySession]

    public init(id: ExerciseID, name: String, facts: [LabelledValue], isLocal: Bool, trend: ExerciseTrend,
                sessions: [ExerciseHistorySession])
    {
        self.id = id
        self.name = name
        self.facts = facts
        self.isLocal = isLocal
        self.trend = trend
        self.sessions = sessions
    }
}

/// What the trend charts, one point per session (§7.9).
public enum TrendMetric: String, Equatable, Sendable {
    /// The best e1RM of the working sets with `n ≤ 12` (03 DI 12), in the exercise's unit.
    case oneRepMax
    /// Timed exercises: the longest hold, in seconds.
    case longestHold
    /// Reps only, and bodyweight with no body mass: the most reps.
    case mostReps
    /// Distance work: nothing to chart in V1.
    case none
}

public struct ExerciseTrend: Equatable, Sendable {
    public var metric: TrendMetric
    public var unit: MassUnit
    /// Oldest first.
    public var points: [TrendPoint]
    /// `Estimated 1RM`, `Longest hold`, `Most reps`.
    public var title: String

    public init(metric: TrendMetric, unit: MassUnit, points: [TrendPoint], title: String) {
        self.metric = metric
        self.unit = unit
        self.points = points
        self.title = title
    }

    public static let notEnough = "Not enough sessions for a trend yet."
    /// The range picker's choices, in months: 3M, 6M, 1Y.
    public static let ranges = [3, 6, 12]

    /// The points from `months` before `now`.
    public func points(lastMonths months: Int, now: Date, calendar: Calendar) -> [TrendPoint] {
        guard let from = calendar.date(byAdding: .month, value: -months, to: now) else { return points }
        return points.filter { $0.date >= from }
    }

    /// Under two points there is no trend to draw.
    public func hasTrend(_ points: [TrendPoint]) -> Bool { points.count >= 2 }
}

public struct TrendPoint: Equatable, Sendable, Identifiable {
    public var session: SessionID
    public var date: Date
    public var value: Double
    /// `62.3 kg`, `1:02`, `15 reps`.
    public var text: String
    /// A deload session's point is drawn hollow.
    public var hollow: Bool

    public init(session: SessionID, date: Date, value: Double, text: String, hollow: Bool) {
        self.session = session
        self.date = date
        self.value = value
        self.text = text
        self.hollow = hollow
    }

    public var id: SessionID { session }
}

/// One session in the exercise's history.
public struct ExerciseHistorySession: Equatable, Sendable, Identifiable {
    public var id: SessionID
    /// `Tue 27 Oct, Fully Body Collection #1`.
    public var title: String
    /// One line per set, a chain's records on one line: `2 35 kg × 13 @ 9.5, 2a 35 kg × 8 @ 9.5`.
    public var lines: [String]
    public var isDeload: Bool

    public init(id: SessionID, title: String, lines: [String], isDeload: Bool) {
        self.id = id
        self.title = title
        self.lines = lines
        self.isDeload = isDeload
    }
}

/// The picker's and the Exercises list's search (§7.6): every token of the
/// query matches the name or an alias, in any order, case and diacritics
/// ignored, with a few gym abbreviations understood.
public enum ExerciseSearch {
    public static let synonyms: [String: String] = [
        "db": "dumbbell", "bb": "barbell", "kb": "kettlebell", "rdl": "romanian deadlift", "ohp": "overhead press",
    ]

    public static func matches(_ exercise: Exercise, query: String) -> Bool {
        let tokens = words(query)
        guard !tokens.isEmpty else { return true }
        let haystack = ([exercise.name] + exercise.aliases).map(fold).joined(separator: " ")
        return tokens.allSatisfy { token in
            if haystack.contains(token) { return true }
            guard let expansion = synonyms[token] else { return false }
            return words(expansion).allSatisfy(haystack.contains)
        }
    }

    static func words(_ text: String) -> [String] {
        fold(text).split(whereSeparator: { !$0.isLetter && !$0.isNumber }).map(String.init)
    }

    static func fold(_ text: String) -> String {
        text.folding(options: [.caseInsensitive, .diacriticInsensitive], locale: Locale(identifier: "en_US_POSIX"))
    }
}

public enum ExercisePresenter {
    /// "Recent" in the picker: done in the last 30 days.
    public static let recentDays = 30

    /// The Exercises list: sections A to Z of the exercises matching `query`.
    public static func list(_ exercises: [ExerciseID: Exercise], history: HistoryIndex, query: String = "",
                            settings: PresenterSettings) -> [ExerciseSection]
    {
        let rows = exercises.values.filter { ExerciseSearch.matches($0, query: query) }
            .sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
            .map { row($0, history: history, settings: settings) }
        var sections: [ExerciseSection] = []
        for row in rows {
            let first = ExerciseSearch.fold(String(row.name.prefix(1))).uppercased()
            let letter = first.first?.isLetter == true ? first : "#"
            if sections.last?.title == letter {
                sections[sections.count - 1].rows.append(row)
            } else {
                sections.append(ExerciseSection(title: letter, rows: [row]))
            }
        }
        return sections
    }

    /// The picker: "Recent" (done in the last 30 days, newest first), then A to Z.
    public static func picker(_ exercises: [ExerciseID: Exercise], history: HistoryIndex, query: String = "", now: Date,
                              settings: PresenterSettings) -> [ExerciseSection]
    {
        let all = list(exercises, history: history, query: query, settings: settings)
        let recent = exercises.values.filter { ExerciseSearch.matches($0, query: query) }
            .compactMap { exercise -> (Exercise, Date)? in
                guard let date = history.lastDone(exercise.id),
                      SuggestMath.days(from: date, to: now, calendar: settings.calendar) <= recentDays
                else { return nil }
                return (exercise, date)
            }
            .sorted { $0.1 != $1.1 ? $0.1 > $1.1 : $0.0.name.localizedStandardCompare($1.0.name) == .orderedAscending }
            .map { row($0.0, history: history, settings: settings) }
        return (recent.isEmpty ? [] : [ExerciseSection(title: "Recent", rows: recent)]) + all
    }

    static func row(_ exercise: Exercise, history: HistoryIndex, settings: PresenterSettings) -> ExerciseRow {
        ExerciseRow(id: exercise.id, name: exercise.name, equipment: exercise.equipmentText,
                    lastDone: history.lastDone(exercise.id).map { "Last done \(Format.day($0, in: settings.timeZone))" },
                    isLocal: exercise.isLocal)
    }

    /// The exercise detail: its facts, the trend and its history.
    public static func detail(_ exercise: Exercise, history: HistoryIndex,
                              settings: PresenterSettings) -> ExerciseDetailPresentation
    {
        let config = SuggestionInputs.config(for: exercise, defaultUnit: settings.defaultUnit)
        var facts: [LabelledValue] = []
        if let equipment = exercise.equipmentText { facts.append(LabelledValue("Equipment", equipment)) }
        facts.append(LabelledValue("Records", exercise.measurement.label))
        if exercise.resolvedLaterality != .bilateral { facts.append(LabelledValue("Sides", exercise.resolvedLaterality.label)) }
        let primary = exercise.primaryMuscles.map(\.label).joined(separator: ", ")
        let secondary = exercise.secondaryMuscles.map(\.label).joined(separator: ", ")
        switch (primary.isEmpty, secondary.isEmpty) {
        case (false, false): facts.append(LabelledValue("Muscles", "\(primary); also \(secondary)"))
        case (false, true): facts.append(LabelledValue("Muscles", primary))
        case (true, false): facts.append(LabelledValue("Muscles", "Also \(secondary)"))
        case (true, true): break
        }
        if let fact = loadSteps(config) { facts.append(fact) }
        let sessions = sessionsWith(exercise.id, history: history)
        return ExerciseDetailPresentation(
            id: exercise.id,
            name: exercise.name,
            facts: facts,
            isLocal: exercise.isLocal,
            trend: trend(exercise, sessions: sessions, config: config),
            sessions: sessions.map { session(exercise, $0, settings: settings) }
        )
    }

    /// The load's grid in words: `Load: 5 kg steps`, `Load: 2.5 kg steps
    /// from 20 kg` (the start only when it is not 0), `Added load: 2.5 kg
    /// steps`, `Assistance: 5 kg steps`; nil with no grid.
    static func loadSteps(_ config: ExerciseConfig) -> LabelledValue? {
        guard let step = config.step, config.canAddLoad else { return nil }
        let steps = "\(Format.mass(step, unit: config.unit)) steps"
        switch config.mode {
        case .external:
            let base = config.base ?? 0
            return LabelledValue("Load", base == 0 ? steps : "\(steps) from \(Format.mass(base, unit: config.unit))")
        case .bodyweightAdded: return LabelledValue("Added load", steps)
        case .bodyweightAssisted: return LabelledValue("Assistance", steps)
        case .none: return nil
        }
    }

    /// Every session with a set of the exercise, any setup, newest first.
    static func sessionsWith(_ id: ExerciseID, history: HistoryIndex) -> [ExerciseHistory] {
        history.sessions.compactMap { records in
            let slotOrder = Dictionary(records.session.slots.enumerated().map { ($1.key, $0) }, uniquingKeysWith: { a, _ in a })
            let sets = records.sets.filter { $0.exercise == id }.sorted { a, b in
                let (sa, sb) = (slotOrder[a.slot] ?? .max, slotOrder[b.slot] ?? .max)
                return sa != sb ? sa < sb : a.order < b.order
            }
            return sets.isEmpty ? nil : ExerciseHistory(session: records.session, sets: sets)
        }
    }

    /// The trend (§7.9): one point per session, by the exercise's metric.
    public static func trend(_ exercise: Exercise, history: HistoryIndex, settings: PresenterSettings) -> ExerciseTrend {
        let config = SuggestionInputs.config(for: exercise, defaultUnit: settings.defaultUnit)
        return trend(exercise, sessions: sessionsWith(exercise.id, history: history), config: config)
    }

    static func trend(_ exercise: Exercise, sessions: [ExerciseHistory], config: ExerciseConfig) -> ExerciseTrend {
        let metric: TrendMetric
        if exercise.measurement.isTimedHold {
            metric = .longestHold
        } else if !exercise.measurement.recordsReps {
            metric = .none
        } else if config.mode == .none || (config.mode != .external && !sessions.contains { $0.session.bodyMass != nil }) {
            metric = .mostReps
        } else {
            metric = .oneRepMax
        }
        var points: [TrendPoint] = []
        for entry in sessions.reversed() {
            let sets = SuggestionInputs.doneSets(entry.sets, exercise: exercise, config: config).filter(\.kind.isWorking)
            let value: Double?
            let text: (Double) -> String
            switch metric {
            case .oneRepMax:
                let bodyMass = entry.session.bodyMass.map(SuggestionInputs.kilograms)
                value = sets.compactMap { estimateOneRepMax(of: $0, exercise: config, bodyMass: bodyMass) }
                    .filter { $0.effort <= 12 }
                    .map(\.value).max()
                text = { Format.mass($0, unit: config.unit) }
            case .longestHold:
                value = sets.compactMap(\.duration).max().map(Double.init)
                text = { Format.duration(Int($0)) }
            case .mostReps:
                value = sets.compactMap(\.effectiveReps).max().map(Double.init)
                text = { Format.count(Int($0), "rep") }
            case .none:
                value = nil
                text = { _ in "" }
            }
            guard let value else { continue }
            let rounded = metric == .oneRepMax ? (value * 10).rounded() / 10 : value
            points.append(TrendPoint(session: entry.session.id, date: entry.session.at, value: rounded, text: text(rounded),
                                     hollow: entry.session.deload == true))
        }
        let title = switch metric {
        case .oneRepMax: "Estimated 1RM"
        case .longestHold: "Longest hold"
        case .mostReps: "Most reps"
        case .none: ""
        }
        return ExerciseTrend(metric: metric, unit: config.unit, points: points, title: title)
    }

    /// A session's sets of the exercise as lines, a chain on one line.
    static func session(_ exercise: Exercise, _ entry: ExerciseHistory, settings: PresenterSettings) -> ExerciseHistorySession {
        let zone = HistoryPresenter.zone(of: entry.session, settings)
        var lines: [String] = []
        for slot in entry.slots {
            let sets = entry.sets(inSlot: slot)
            let labels = SetLabel.make(records: sets)
            var current: (head: SetID, parts: [String])?
            for set in sets {
                let part = "\(labels[set.id]?.short ?? "?") \(HistoryPresenter.text(of: set, measurement: exercise.measurement))"
                let head = set.continues ?? set.id
                if let open = current, open.head == head {
                    current?.parts.append(part)
                } else {
                    if let open = current { lines.append(open.parts.joined(separator: ", ")) }
                    current = (head, [part])
                }
            }
            if let open = current { lines.append(open.parts.joined(separator: ", ")) }
        }
        return ExerciseHistorySession(
            id: entry.session.id,
            title: "\(Format.day(entry.session.at, in: zone)), \(HistoryPresenter.routineTitle(of: entry.session))",
            lines: lines,
            isDeload: entry.session.deload == true
        )
    }
}
