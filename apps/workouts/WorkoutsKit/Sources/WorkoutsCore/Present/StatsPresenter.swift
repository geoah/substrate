import Foundation

/// What a stats page covers: one routine's sessions, or every session.
public enum StatsScope: Hashable, Sendable {
    case all
    case routine(RoutineID)
}

/// The stats page's period, ending today, and the bars its volume chart
/// draws. Day bars for a week and a month, week bars for three and six
/// months, month bars for a year; the longer windows start on a bar's
/// boundary so no bar but today's is partial.
public enum StatsPeriod: String, CaseIterable, Sendable, Identifiable {
    case week = "1W"
    case month = "1M"
    case threeMonths = "3M"
    case sixMonths = "6M"
    case year = "1Y"

    public var id: String { rawValue }

    /// The picker's spoken label: `1 week`.
    public var spoken: String {
        switch self {
        case .week: "1 week"
        case .month: "1 month"
        case .threeMonths: "3 months"
        case .sixMonths: "6 months"
        case .year: "1 year"
        }
    }

    public var bar: StatsBar {
        switch self {
        case .week, .month: .day
        case .threeMonths, .sixMonths: .week
        case .year: .month
        }
    }

    /// Whole days up to the end of today: the last 7 days, the days since
    /// this date last month, the 13 or 26 weeks to this one, the 12 months
    /// to this one.
    public func window(now: Date, calendar: Calendar) -> DateInterval {
        let today = calendar.startOfDay(for: now)
        let end = calendar.date(byAdding: .day, value: 1, to: today) ?? now
        let start: Date? =
            switch self {
            case .week: calendar.date(byAdding: .day, value: -6, to: today)
            case .month: calendar.date(byAdding: .month, value: -1, to: end)
            case .threeMonths:
                calendar.dateInterval(of: .weekOfYear, for: today)
                    .flatMap { calendar.date(byAdding: .weekOfYear, value: -12, to: $0.start) }
            case .sixMonths:
                calendar.dateInterval(of: .weekOfYear, for: today)
                    .flatMap { calendar.date(byAdding: .weekOfYear, value: -25, to: $0.start) }
            case .year:
                calendar.dateInterval(of: .month, for: today)
                    .flatMap { calendar.date(byAdding: .month, value: -11, to: $0.start) }
            }
        return DateInterval(start: start ?? today, end: end)
    }

    /// The same number of whole days just before `window`, which the
    /// changes compare against.
    public static func previous(_ window: DateInterval, calendar: Calendar) -> DateInterval {
        let days = calendar.dateComponents([.day], from: window.start, to: window.end).day ?? 0
        let start = calendar.date(byAdding: .day, value: -days, to: window.start) ?? window.start
        return DateInterval(start: start, end: window.start)
    }
}

/// One bar of the volume chart.
public enum StatsBar: String, Sendable {
    case day, week, month

    var component: Calendar.Component {
        switch self {
        case .day: .day
        case .week: .weekOfYear
        case .month: .month
        }
    }
}

/// A routine or every session, over one period, against the period before.
public struct StatsPresentation: Equatable, Sendable {
    public var scope: StatsScope
    public var period: StatsPeriod
    /// `All routines`, or the routine's name.
    public var scopeTitle: String
    /// `Sat 24 Oct – Fri 30 Oct`.
    public var range: String
    /// `Compared with Sat 17 Oct – Fri 23 Oct`.
    public var comparison: String
    /// The scope has a finished session, in any period.
    public var hasSessions: Bool
    /// Sessions (with its rate over a longer period), Volume, Working sets, Time per session.
    public var totals: [StatsTotal]
    public var volumeUnit: MassUnit
    /// Every bar of the period, oldest first, empty ones included.
    public var volume: [StatsVolumeBar]
    /// The period's exercises with a working set, in the routine's order
    /// (else by working sets), the equipment versions of one movement together.
    public var exercises: [StatsExerciseGroup]
    /// Bests set in the period that beat every earlier session, newest first.
    public var records: [StatsRecord]
    /// Working sets per muscle group, weighted by share, largest first.
    public var muscles: [StatsMuscleGroup]
    /// Working sets per region of the body map (`BodyMap.sets`).
    public var regions: [BodyRegion: Double] = [:]
    /// Push against pull, then upper body against lower body and core, by
    /// the period's working sets; a pair with nothing on either side is left out.
    public var balance: [StatsBalance] = []
    /// Sessions in each calendar week the period touches, oldest first.
    public var weeks: [StatsWeek] = []
    /// How regularly the period was trained; nil for a week, which says nothing of it.
    public var consistency: StatsConsistency?
    /// The scope's newest session against the one before it of the same
    /// routine, in any period; nil until there are two.
    public var lastSession: StatsSessionComparison?
    /// The scope's sessions in any period, newest first.
    public var sessions: [HistoryRow]

    public init(
        scope: StatsScope, period: StatsPeriod, scopeTitle: String, range: String, comparison: String,
        hasSessions: Bool, totals: [StatsTotal], volumeUnit: MassUnit, volume: [StatsVolumeBar],
        exercises: [StatsExerciseGroup], records: [StatsRecord], muscles: [StatsMuscleGroup], sessions: [HistoryRow]
    ) {
        self.scope = scope
        self.period = period
        self.scopeTitle = scopeTitle
        self.range = range
        self.comparison = comparison
        self.hasSessions = hasSessions
        self.totals = totals
        self.volumeUnit = volumeUnit
        self.volume = volume
        self.exercises = exercises
        self.records = records
        self.muscles = muscles
        self.sessions = sessions
    }

    /// No session in the period.
    public var isEmpty: Bool { volume.allSatisfy { $0.sessions == 0 } }

    public static let allRoutines = "All routines"
    public static let noSessionsTitle = "No sessions yet"
    public static let noRoutineSessions = "Finish a session of this routine to see its progress here."
    public static let noSessions = "Finish a session to see your progress here."
    public static let emptyPeriod = "No sessions in this period."
    public static let musclesFooter =
        "Each working set counts toward the muscles it works by their share: 1 for a prime mover, 0.5 for a synergist, less for a helper."
}

/// A number of the period, and how it moved against the period before.
public struct StatsTotal: Equatable, Sendable, Identifiable {
    /// `Sessions`.
    public var label: String
    /// `3`, `12,400 kg`, `2 h 30 min`.
    public var value: String
    /// Nil when there is nothing to compare: no value in either period, or
    /// a percentage of nothing.
    public var change: StatsChange?
    /// What the value comes to: `1.5 a week`.
    public var detail: String?

    public init(label: String, value: String, change: StatsChange?, detail: String? = nil) {
        self.label = label
        self.value = value
        self.change = change
        self.detail = detail
    }

    public var id: String { label }
}

/// One side of a balance, `Push`, and its share of the pair.
public struct StatsBalancePart: Equatable, Sendable, Identifiable {
    public var label: String
    public var sets: Double
    /// 0 to 1, of the parts together.
    public var share: Double
    /// `42%`.
    public var percent: String
    /// `12.5 sets`.
    public var text: String

    public init(label: String, sets: Double, share: Double, percent: String, text: String) {
        self.label = label
        self.sets = sets
        self.share = share
        self.percent = percent
        self.text = text
    }

    public var id: String { label }
}

/// Two or three sides that should not drift far apart: push and pull,
/// upper body, lower body and core.
public struct StatsBalance: Equatable, Sendable, Identifiable {
    /// `push-pull`, `body`.
    public var id: String
    /// `Push and pull`.
    public var title: String
    /// What is counted: `Upper-body working sets`.
    public var note: String
    public var parts: [StatsBalancePart]
    /// `Push 42%, 10 sets. Pull 58%, 14 sets.`
    public var sentence: String

    public init(id: String, title: String, note: String, parts: [StatsBalancePart], sentence: String) {
        self.id = id
        self.title = title
        self.note = note
        self.parts = parts
        self.sentence = sentence
    }
}

/// A calendar week of the period and its sessions.
public struct StatsWeek: Equatable, Sendable, Identifiable {
    public var start: Date
    public var sessions: Int
    /// `Week of Mon 26 Oct`.
    public var label: String
    /// `2 sessions`, `No sessions`.
    public var text: String

    public init(start: Date, sessions: Int, label: String, text: String) {
        self.start = start
        self.sessions = sessions
        self.label = label
        self.text = text
    }

    public var id: Date { start }
}

/// How regularly the period was trained.
public struct StatsConsistency: Equatable, Sendable {
    public var weeksTrained: Int
    public var weeks: Int
    /// `9 of 13`.
    public var value: String
    /// `1.8`: sessions per week over the period's days.
    public var perWeek: String
    /// `6 weeks`: weeks in a row with a session up to this one (this week
    /// counts once it has one); nil under two.
    public var streak: String?
    /// `Trained in 9 of 13 weeks, 1.8 sessions a week. 6 weeks in a row.`
    public var sentence: String

    public init(weeksTrained: Int, weeks: Int, value: String, perWeek: String, streak: String?, sentence: String) {
        self.weeksTrained = weeksTrained
        self.weeks = weeks
        self.value = value
        self.perWeek = perWeek
        self.streak = streak
        self.sentence = sentence
    }
}

/// The newest session against the one before it.
public struct StatsSessionComparison: Equatable, Sendable {
    public var session: SessionID
    public var previous: SessionID
    /// `Mon 26 Oct`, and the routine's name on the Stats tab: `Mon 26 Oct, Day 1`.
    public var title: String
    /// `Against Thu 22 Oct`.
    public var against: String
    /// Time, Volume, Working sets, each against the session before.
    public var totals: [StatsTotal]
    /// `Better on 3 of 5 exercises`: each exercise both sessions hold, by its
    /// best estimated 1RM (or hold, or reps), 1% either way the same; nil
    /// when they share none.
    public var exercises: String?

    public init(session: SessionID, previous: SessionID, title: String, against: String, totals: [StatsTotal], exercises: String?) {
        self.session = session
        self.previous = previous
        self.title = title
        self.against = against
        self.totals = totals
        self.exercises = exercises
    }
}

public struct StatsChange: Equatable, Sendable {
    public enum Direction: String, Sendable {
        case up, down, same
    }

    public var direction: Direction
    /// `+1`, `−12%`, `+1.4 kg`, `Same`.
    public var text: String
    /// `Up 12% on the period before`.
    public var spoken: String

    public init(direction: Direction, text: String, spoken: String) {
        self.direction = direction
        self.text = text
        self.spoken = spoken
    }
}

public struct StatsVolumeBar: Equatable, Sendable, Identifiable {
    public var start: Date
    /// In the presentation's `volumeUnit`.
    public var value: Double
    public var sessions: Int
    /// `Mon 26 Oct`, `Week of Mon 26 Oct`, `October 2026`.
    public var label: String
    /// `3,120 kg, 1 session`; `No sessions`.
    public var text: String

    public init(start: Date, value: Double, sessions: Int, label: String, text: String) {
        self.start = start
        self.value = value
        self.sessions = sessions
        self.label = label
        self.text = text
    }

    public var id: Date { start }
}

/// One exercise, or the equipment versions of one movement done in the
/// period, each keeping its own trend since their loads never compare.
public struct StatsExerciseGroup: Equatable, Sendable, Identifiable {
    public var id: String
    /// The movement's name, `Chest Press`; nil for a lone exercise.
    public var title: String?
    /// `2 equipment versions`; nil for a lone exercise.
    public var detail: String?
    public var exercises: [StatsExercise]

    public init(id: String, title: String?, detail: String?, exercises: [StatsExercise]) {
        self.id = id
        self.title = title
        self.detail = detail
        self.exercises = exercises
    }
}

/// One exercise's progress over the period.
public struct StatsExercise: Equatable, Sendable, Identifiable {
    public var id: ExerciseID
    public var name: String
    /// `Estimated 1RM`, `Longest hold`, `Most reps`; nil for distance work.
    public var metric: String?
    public var trendMetric: TrendMetric
    public var unit: MassUnit
    /// The period's best by the metric: `148.5 kg`.
    public var best: String?
    /// The best against the period before's.
    public var change: StatsChange?
    /// One point per session in the period, oldest first.
    public var points: [TrendPoint]
    /// What the sparkline shows, for VoiceOver.
    public var trendSummary: String?
    /// Best set, Working sets.
    public var facts: [LabelledValue]

    public init(
        id: ExerciseID, name: String, metric: String?, trendMetric: TrendMetric, unit: MassUnit, best: String?,
        change: StatsChange?, points: [TrendPoint], trendSummary: String?, facts: [LabelledValue]
    ) {
        self.id = id
        self.name = name
        self.metric = metric
        self.trendMetric = trendMetric
        self.unit = unit
        self.best = best
        self.change = change
        self.points = points
        self.trendSummary = trendSummary
        self.facts = facts
    }
}

/// A personal record: a session's best that beat every earlier session of
/// the exercise, in any routine.
public struct StatsRecord: Equatable, Sendable, Identifiable {
    public var exercise: ExerciseID
    public var session: SessionID
    public var at: Date
    public var name: String
    /// `Estimated 1RM`, `Heaviest load`, `Longest hold`, `Most reps`.
    public var kind: String
    /// `148.5 kg`.
    public var value: String
    /// `147.1 kg`.
    public var previous: String
    /// `Mon 26 Oct`.
    public var date: String

    public init(
        exercise: ExerciseID, session: SessionID, at: Date, name: String, kind: String, value: String, previous: String,
        date: String
    ) {
        self.exercise = exercise
        self.session = session
        self.at = at
        self.name = name
        self.kind = kind
        self.value = value
        self.previous = previous
        self.date = date
    }

    public var id: String { "\(exercise.rawValue)|\(kind)" }
}

public struct StatsMuscleGroup: Equatable, Sendable, Identifiable {
    public var muscle: Muscle
    public var label: String
    public var sets: Double
    /// `12.5 sets`.
    public var text: String
    /// Its muscles, from the exercises that give each muscle's share;
    /// empty when none does.
    public var muscles: [StatsMuscle]

    public init(muscle: Muscle, label: String, sets: Double, text: String, muscles: [StatsMuscle]) {
        self.muscle = muscle
        self.label = label
        self.sets = sets
        self.text = text
        self.muscles = muscles
    }

    public var id: Muscle { muscle }
}

public struct StatsMuscle: Equatable, Sendable, Identifiable {
    public var muscle: MuscleDetail
    public var label: String
    public var sets: Double
    /// `4.5 sets`.
    public var text: String

    public init(muscle: MuscleDetail, label: String, sets: Double, text: String) {
        self.muscle = muscle
        self.label = label
        self.sets = sets
        self.text = text
    }

    public var id: MuscleDetail { muscle }
}

/// The routine page's header: its folder and what the plan holds.
public struct RoutineSummaryPresentation: Equatable, Sendable {
    public var folder: String?
    /// Exercises, Sets per session, Last done.
    public var facts: [LabelledValue]

    public init(folder: String?, facts: [LabelledValue]) {
        self.folder = folder
        self.facts = facts
    }
}

public enum StatsPresenter {
    /// The routine's sessions; every session for `.all`. Newest first, as
    /// the history keeps them.
    public static func entries(_ history: [HistoryEntry], scope: StatsScope) -> [HistoryEntry] {
        switch scope {
        case .all: history
        case let .routine(id): history.filter { $0.records.session.routine == id }
        }
    }

    /// The routine page's header facts.
    public static func summary(_ routine: Routine, history: [HistoryEntry], settings: PresenterSettings) -> RoutineSummaryPresentation {
        var facts = [
            LabelledValue("Exercises", "\(routine.slots.count)"),
            LabelledValue("Sets per session", "\(routine.slots.reduce(0) { $0 + $1.sets.count { $0.role == .working } })"),
        ]
        if let last = history.lazy.map(\.records.session).filter({ $0.routine == routine.id }).max(by: { $0.at < $1.at }) {
            facts.append(LabelledValue("Last done", Format.day(last.at, in: HistoryPresenter.zone(of: last, settings))))
        }
        let folder = routine.folder?.trimmingCharacters(in: .whitespaces)
        return RoutineSummaryPresentation(folder: folder?.isEmpty == false ? folder : nil, facts: facts)
    }

    /// The stats of `scope` over `period`, ending on `now`'s day in the
    /// settings' calendar.
    public static func present(
        _ history: [HistoryEntry], catalog: Catalog, scope: StatsScope, period: StatsPeriod, now: Date,
        settings: PresenterSettings
    ) -> StatsPresentation {
        let calendar = settings.calendar
        let window = period.window(now: now, calendar: calendar)
        let before = StatsPeriod.previous(window, calendar: calendar)
        let scoped = entries(history, scope: scope).sorted { a, b in
            a.records.session.at != b.records.session.at ? a.records.session.at > b.records.session.at : a.id > b.id
        }
        let current = scoped.filter { window.contains($0.records.session.at) && $0.records.session.at < window.end }
        let previous = scoped.filter { before.contains($0.records.session.at) && $0.records.session.at < before.end }
        let scopeTitle: String =
            switch scope {
            case .all: StatsPresentation.allRoutines
            case let .routine(id):
                catalog.routines[id]?.name
                    ?? scoped.first.map { HistoryPresenter.routineTitle(of: $0.records.session) } ?? id.rawValue
            }
        let unit = settings.defaultUnit
        var presentation = StatsPresentation(
            scope: scope,
            period: period,
            scopeTitle: scopeTitle,
            range: range(window, calendar: calendar),
            comparison: "Compared with \(range(before, calendar: calendar))",
            hasSessions: !scoped.isEmpty,
            totals: totals(
                current.map(\.records), previous.map(\.records), exercises: catalog.exercises, settings: settings,
                days: days(window, calendar: calendar)),
            volumeUnit: unit,
            volume: volume(current.map(\.records), window: window, bar: period.bar, exercises: catalog.exercises, settings: settings),
            exercises: exercises(current.map(\.records), previous.map(\.records), scope: scope, catalog: catalog, settings: settings),
            records: records(current.map(\.records), all: history.map(\.records), catalog: catalog, settings: settings),
            muscles: muscles(current.flatMap(\.records.sets), exercises: catalog.exercises),
            sessions: scoped.map { row($0, exercises: catalog.exercises, settings: settings) }
        )
        let sets = current.flatMap(\.records.sets)
        presentation.regions = BodyMap.sets(sets, exercises: catalog.exercises).mapValues(\.double)
        presentation.balance = balance(sets, catalog: catalog)
        presentation.weeks = weeks(current.map(\.records), window: window, calendar: calendar)
        if period != .week {
            presentation.consistency = consistency(
                presentation.weeks, sessions: current.count, all: scoped.map(\.records),
                window: window, now: now, calendar: calendar)
        }
        presentation.lastSession = lastSession(scoped.map(\.records), scope: scope, catalog: catalog, settings: settings)
        return presentation
    }

    // MARK: - Totals

    static func totals(
        _ current: [SessionRecords], _ previous: [SessionRecords], exercises: [ExerciseID: Exercise],
        settings: PresenterSettings, days: Int = 7
    ) -> [StatsTotal] {
        let now = Totals(current, exercises: exercises, settings: settings)
        let then = Totals(previous, exercises: exercises, settings: settings)
        let unit = settings.defaultUnit.rawValue
        var averageChange: StatsChange?
        if let average = now.average, let before = then.average {
            averageChange = change(Double(average - before) / 60, text: { "\(Format.number($0, decimals: 0)) min" })
        }
        // A week's count is its rate already.
        let rate = days > 7 && now.sessions > 0 ? "\(perWeek(now.sessions, days: days)) a week" : nil
        return [
            StatsTotal(label: "Sessions", value: "\(now.sessions)", change: count(now.sessions, then.sessions), detail: rate),
            StatsTotal(
                label: "Volume", value: "\(Format.grouped(Int(now.volume.rounded()))) \(unit)",
                change: percent(now.volume, then.volume)),
            StatsTotal(
                label: "Working sets", value: "\(now.sets)",
                change: count(now.sets, then.sets)),
            StatsTotal(
                label: "Time per session", value: now.average.map(Format.minutes) ?? "None",
                change: averageChange),
        ]
    }

    /// Whole days in `window`.
    static func days(_ window: DateInterval, calendar: Calendar) -> Int {
        max(1, calendar.dateComponents([.day], from: window.start, to: window.end).day ?? 7)
    }

    /// Sessions per seven days, to one decimal: `1.8`.
    static func perWeek(_ sessions: Int, days: Int) -> String {
        Format.number(Double(sessions) / (Double(max(1, days)) / 7), decimals: 1)
    }

    /// One period's sums. A session's length leaves its pauses out; one
    /// with no end adds to nothing but the count.
    struct Totals {
        var sessions = 0
        var volume = 0.0
        var sets = 0
        var seconds: Seconds = 0
        var timed = 0

        init(_ sessions: [SessionRecords], exercises: [ExerciseID: Exercise], settings: PresenterSettings) {
            for records in sessions {
                self.sessions += 1
                volume += sessionVolume(records, exercises: exercises, settings: settings)
                sets += Counting.workingSets(records.sets)
                if let end = records.session.endsAt {
                    seconds += SessionPresenter.activeSeconds(from: records.session.at, to: end, pauses: records.session.pauses)
                    timed += 1
                }
            }
        }

        var average: Seconds? { timed == 0 ? nil : seconds / timed }
    }

    static func sessionVolume(_ records: SessionRecords, exercises: [ExerciseID: Exercise], settings: PresenterSettings) -> Double {
        Counting.volume(
            records.sets, exercises: exercises, bodyMass: records.session.bodyMass, unit: settings.defaultUnit,
            includeWarmups: settings.countWarmupsInVolume
        ).value.double
    }

    // MARK: - Changes

    /// A difference of counts: `+1`; nil when both are 0.
    static func count(_ now: Int, _ then: Int, against: String = periodBefore) -> StatsChange? {
        guard now != 0 || then != 0 else { return nil }
        return change(Double(now - then), against: against, text: { Format.number($0, decimals: 0) })
    }

    /// A difference as a share of the period before's: `+12%`; nil when
    /// the period before had none, which no percentage measures.
    static func percent(_ now: Double, _ then: Double, against: String = periodBefore) -> StatsChange? {
        guard then > 0 else { return nil }
        return change(((now / then - 1) * 100).rounded(), against: against, text: { "\(Format.number($0, decimals: 0))%" })
    }

    static let periodBefore = "the period before"

    /// `delta` signed, its size written by `text`.
    static func change(_ delta: Double, against: String = periodBefore, text: (Double) -> String) -> StatsChange {
        let size = text(abs(delta))
        if delta == 0 || size == text(0) {
            return StatsChange(direction: .same, text: "Same", spoken: "The same as \(against)")
        }
        return delta > 0
            ? StatsChange(direction: .up, text: "+\(size)", spoken: "Up \(size) on \(against)")
            : StatsChange(direction: .down, text: "\u{2212}\(size)", spoken: "Down \(size) on \(against)")
    }

    // MARK: - Volume

    static func volume(
        _ sessions: [SessionRecords], window: DateInterval, bar: StatsBar, exercises: [ExerciseID: Exercise],
        settings: PresenterSettings
    ) -> [StatsVolumeBar] {
        let calendar = settings.calendar
        var starts: [Date] = []
        var cursor = window.start
        while cursor < window.end {
            starts.append(cursor)
            guard let next = calendar.date(byAdding: bar.component, value: 1, to: cursor), next > cursor else { break }
            cursor = next
        }
        var sums: [Date: (volume: Double, sessions: Int)] = [:]
        for records in sessions {
            let start = calendar.dateInterval(of: bar.component, for: records.session.at)?.start ?? records.session.at
            let key = max(start, window.start)
            sums[key, default: (0, 0)].volume += sessionVolume(records, exercises: exercises, settings: settings)
            sums[key, default: (0, 0)].sessions += 1
        }
        let unit = settings.defaultUnit
        return starts.map { start in
            let sum = sums[start] ?? (0, 0)
            let label =
                switch bar {
                case .day: Format.day(start, in: calendar.timeZone)
                case .week: "Week of \(Format.day(start, in: calendar.timeZone))"
                case .month: Format.month(start, in: calendar.timeZone)
                }
            let text =
                sum.sessions == 0
                ? "No sessions"
                : "\(Format.grouped(Int(sum.volume.rounded()))) \(unit.rawValue), \(Format.count(sum.sessions, "session"))"
            return StatsVolumeBar(start: start, value: sum.volume.rounded(), sessions: sum.sessions, label: label, text: text)
        }
    }

    // MARK: - Exercises

    /// Each exercise's sets per session, newest first, in display order.
    static func byExercise(_ sessions: [SessionRecords]) -> [ExerciseID: [ExerciseHistory]] {
        var out: [ExerciseID: [ExerciseHistory]] = [:]
        for records in sessions.sorted(by: { $0.session.at > $1.session.at }) {
            let slotOrder = Dictionary(records.session.slots.enumerated().map { ($1.key, $0) }, uniquingKeysWith: { a, _ in a })
            let ordered = records.sets.sorted { a, b in
                let (sa, sb) = (slotOrder[a.slot] ?? .max, slotOrder[b.slot] ?? .max)
                if sa != sb { return sa < sb }
                if a.slot != b.slot { return a.slot < b.slot }
                return a.order < b.order
            }
            var seen: [ExerciseID] = []
            var sets: [ExerciseID: [PerformedSet]] = [:]
            for set in ordered {
                if sets[set.exercise] == nil { seen.append(set.exercise) }
                sets[set.exercise, default: []].append(set)
            }
            for id in seen {
                out[id, default: []].append(ExerciseHistory(session: records.session, sets: sets[id] ?? []))
            }
        }
        return out
    }

    static func exercises(
        _ current: [SessionRecords], _ previous: [SessionRecords], scope: StatsScope, catalog: Catalog,
        settings: PresenterSettings
    ) -> [StatsExerciseGroup] {
        let now = byExercise(current)
        let then = byExercise(previous)
        let workingSets = now.mapValues { $0.reduce(0) { $0 + Counting.workingSets($1.sets) } }
        let planned: [ExerciseID] = if case let .routine(id) = scope { catalog.routines[id]?.slots.map(\.exercise) ?? [] } else { [] }
        // An exercise done only as a warm-up has no progress to show.
        let order = now.keys.filter { catalog.exercises[$0] != nil && (workingSets[$0] ?? 0) > 0 }.sorted { a, b in
            let (pa, pb) = (planned.firstIndex(of: a) ?? .max, planned.firstIndex(of: b) ?? .max)
            if pa != pb { return pa < pb }
            let (sa, sb) = (workingSets[a] ?? 0, workingSets[b] ?? 0)
            if sa != sb { return sa > sb }
            let (na, nb) = (catalog.exercises[a]?.name ?? "", catalog.exercises[b]?.name ?? "")
            return na != nb ? na.localizedStandardCompare(nb) == .orderedAscending : a < b
        }
        let rows = order.compactMap { id in
            catalog.exercises[id].map { exercise($0, now: now[id] ?? [], then: then[id] ?? [], catalog: catalog, settings: settings) }
        }
        // A movement done in two or more equipment versions is one group,
        // where its first version would stand.
        var versions: [MovementID: [StatsExercise]] = [:]
        for row in rows {
            guard let movement = catalog.exercises[row.id]?.movement, catalog.movements[movement] != nil else { continue }
            versions[movement, default: []].append(row)
        }
        var groups: [StatsExerciseGroup] = []
        var placed: Set<MovementID> = []
        for row in rows {
            if let movement = catalog.exercises[row.id]?.movement, let members = versions[movement], members.count > 1 {
                guard placed.insert(movement).inserted else { continue }
                groups.append(
                    StatsExerciseGroup(
                        id: "movement-\(movement.rawValue)", title: catalog.movements[movement]?.name,
                        detail: "\(members.count) equipment versions", exercises: members))
            } else {
                groups.append(StatsExerciseGroup(id: row.id.rawValue, title: nil, detail: nil, exercises: [row]))
            }
        }
        return groups
    }

    static func exercise(
        _ exercise: Exercise, now: [ExerciseHistory], then: [ExerciseHistory], catalog: Catalog,
        settings: PresenterSettings
    ) -> StatsExercise {
        let config = SuggestionInputs.config(for: exercise, defaultUnit: settings.defaultUnit)
        let trend = ExercisePresenter.trend(exercise, sessions: now, config: config)
        let previous = ExercisePresenter.trend(exercise, sessions: then, config: config)
        let best = trend.points.max { $0.value < $1.value }
        var change: StatsChange?
        if let best, let before = previous.points.map(\.value).max() {
            change = metricChange(best.value - before, metric: trend.metric, unit: config.unit)
        }
        var facts: [LabelledValue] = []
        if let set = bestSet(now, exercise: exercise, config: config, metric: trend.metric) {
            facts.append(LabelledValue("Best set", HistoryPresenter.text(of: set, measurement: exercise.measurement)))
        }
        facts.append(LabelledValue("Working sets", "\(now.reduce(0) { $0 + Counting.workingSets($1.sets) })"))
        return StatsExercise(
            id: exercise.id,
            name: exercise.name,
            metric: trend.metric == .none ? nil : trend.title,
            trendMetric: trend.metric,
            unit: config.unit,
            best: best?.text,
            change: change,
            points: trend.points,
            trendSummary: trendSummary(trend, zone: settings.timeZone),
            facts: facts
        )
    }

    /// A difference of the metric: `+1.4 kg`, `+0:05`, `+2 reps`.
    static func metricChange(_ delta: Double, metric: TrendMetric, unit: MassUnit) -> StatsChange? {
        switch metric {
        case .oneRepMax: change((delta * 10).rounded() / 10, text: { Format.mass($0, unit: unit) })
        case .longestHold: change(delta.rounded(), text: { Format.duration(Int($0)) })
        case .mostReps: change(delta.rounded(), text: { Format.count(Int($0), "rep") })
        case .none: nil
        }
    }

    /// `Estimated 1RM, 147.1 kg on Thu 22 Oct to 148.5 kg on Mon 26 Oct, 2 sessions`.
    static func trendSummary(_ trend: ExerciseTrend, zone: TimeZone) -> String? {
        guard let first = trend.points.first, let last = trend.points.last else { return nil }
        guard trend.points.count > 1 else { return "\(trend.title) \(first.text) on \(Format.day(first.date, in: zone)), 1 session" }
        return "\(trend.title), \(first.text) on \(Format.day(first.date, in: zone)) to \(last.text) on \(Format.day(last.date, in: zone)), "
            + Format.count(trend.points.count, "session")
    }

    /// The working set with the best e1RM, the longest hold or the most
    /// reps; ties keep the newest. An e1RM ranks the sets the trend charts
    /// (`n ≤ 12`) above the rest, so the best set is the one behind the
    /// best shown beside it whenever there is one.
    static func bestSet(
        _ sessions: [ExerciseHistory], exercise: Exercise, config: ExerciseConfig,
        metric: TrendMetric
    ) -> PerformedSet? {
        var best: (set: PerformedSet, charted: Bool, value: Double)?
        for entry in sessions {
            let bodyMass = entry.session.bodyMass.map(SuggestionInputs.kilograms)
            for set in entry.sets where set.role == .working {
                let rank: (charted: Bool, value: Double)? =
                    switch metric {
                    case .oneRepMax:
                        SuggestionInputs.doneSets([set], exercise: exercise, config: config).first
                            .flatMap { estimateOneRepMax(of: $0, exercise: config, bodyMass: bodyMass) }
                            .map { ($0.effort <= 12, $0.value) }
                    case .longestHold: set.duration.map { (true, Double($0)) }
                    case .mostReps: set.reps.map { (true, Double($0)) }
                    case .none: nil
                    }
                guard let rank else { continue }
                if let current = best, (current.charted ? 1 : 0, current.value) >= (rank.charted ? 1 : 0, rank.value) { continue }
                best = (set, rank.charted, rank.value)
            }
        }
        return best?.set
    }

    // MARK: - Records

    /// For each exercise of the period: the newest session in it whose best
    /// beat every earlier session's, in any routine, by its metric and, for
    /// a load, by the heaviest load. A first session beats nothing.
    static func records(
        _ current: [SessionRecords], all: [SessionRecords], catalog: Catalog,
        settings: PresenterSettings
    ) -> [StatsRecord] {
        let inPeriod = Set(current.map(\.session.id))
        let everything = byExercise(all)
        var out: [StatsRecord] = []
        for id in Set(current.flatMap { $0.sets.map(\.exercise) }) {
            guard let exercise = catalog.exercises[id], let sessions = everything[id] else { continue }
            let config = SuggestionInputs.config(for: exercise, defaultUnit: settings.defaultUnit)
            let trend = ExercisePresenter.trend(exercise, sessions: sessions, config: config)
            let points = trend.points.map { (session: $0.session, at: $0.date, value: $0.value, text: $0.text) }
            if let record = newestRecord(points, in: inPeriod) {
                out.append(statsRecord(exercise, kind: trend.title, record, settings: settings))
            }
            if config.mode == .external {
                let loads = sessions.reversed().compactMap { entry -> (session: SessionID, at: Date, value: Double, text: String)? in
                    let heaviest = entry.sets
                        .filter { $0.role == .working && ($0.reps ?? 0) > 0 }
                        .compactMap { $0.load.map { SuggestionInputs.convert($0, to: config.unit) } }
                        .max()
                    return heaviest.map { (entry.session.id, entry.session.at, $0, Format.mass($0, unit: config.unit)) }
                }
                if let record = newestRecord(loads, in: inPeriod) {
                    out.append(statsRecord(exercise, kind: "Heaviest load", record, settings: settings))
                }
            }
        }
        return out.sorted { a, b in
            a.at != b.at ? a.at > b.at : (a.name, a.kind, a.exercise.rawValue) < (b.name, b.kind, b.exercise.rawValue)
        }
    }

    private typealias Best = (session: SessionID, at: Date, value: Double, text: String)

    /// The newest point in `sessions` above every point before it, oldest
    /// first in, with the best it beat.
    private static func newestRecord(_ points: [Best], in sessions: Set<SessionID>) -> (record: Best, beat: Best)? {
        var best: Best?
        var found: (record: Best, beat: Best)?
        for point in points {
            if let before = best, point.value > before.value {
                if sessions.contains(point.session) { found = (point, before) }
                best = point
            } else if best == nil {
                best = point
            }
        }
        return found
    }

    private static func statsRecord(
        _ exercise: Exercise, kind: String, _ found: (record: Best, beat: Best),
        settings: PresenterSettings
    ) -> StatsRecord {
        StatsRecord(
            exercise: exercise.id, session: found.record.session, at: found.record.at, name: exercise.name, kind: kind,
            value: found.record.text, previous: found.beat.text, date: Format.day(found.record.at, in: settings.timeZone))
    }

    // MARK: - Muscles

    /// Working sets per group by `Counting.setsPerMuscle`, and under each
    /// its muscles from the exercises that name each muscle's share (their
    /// own, else their movement's).
    static func muscles(_ sets: [PerformedSet], exercises: [ExerciseID: Exercise]) -> [StatsMuscleGroup] {
        var detail: [MuscleDetail: Decimal] = [:]
        for set in sets where set.role == .working && set.continues == nil {
            guard let exercise = exercises[set.exercise] else { continue }
            let own = exercise.muscles.contains { $0.muscle != nil } ? exercise.muscles : exercise.inheritedMuscles
            for share in own where share.involvement > 0 {
                guard let muscle = share.muscle else { continue }
                detail[muscle, default: 0] += share.involvement
            }
        }
        let rows: [StatsMuscle] = detail.map { muscle, value in
            StatsMuscle(muscle: muscle, label: muscle.label, sets: value.double, text: setsText(value))
        }
        var groups: [StatsMuscleGroup] = []
        for (group, value) in Counting.setsPerMuscle(sets, exercises: exercises) where value > 0 {
            let muscles = rows.filter { $0.muscle.group == group }.sorted(by: largestFirst)
            groups.append(
                StatsMuscleGroup(
                    muscle: group, label: group.label, sets: value.double, text: setsText(value),
                    muscles: muscles))
        }
        return groups.sorted(by: largestFirst)
    }

    private static func largestFirst(_ a: StatsMuscle, _ b: StatsMuscle) -> Bool {
        a.sets != b.sets ? a.sets > b.sets : a.label < b.label
    }

    private static func largestFirst(_ a: StatsMuscleGroup, _ b: StatsMuscleGroup) -> Bool {
        a.sets != b.sets ? a.sets > b.sets : a.label < b.label
    }

    /// `1 set`, `12.5 sets`.
    static func setsText(_ value: Decimal) -> String {
        let number = Format.number(value, decimals: 1)
        return number == "1" ? "1 set" : "\(number) sets"
    }

    // MARK: - Balance

    static let upperBody: Set<Muscle> = [.chest, .shoulders, .lats, .upperback, .traps, .biceps, .triceps, .forearms, .neck]
    static let lowerBody: Set<Muscle> = [.quadriceps, .hamstrings, .glutes, .calves, .adductors, .abductors]
    static let core: Set<Muscle> = [.abdominals, .lowerback]
    static let pushGroups: Set<Muscle> = [.chest, .shoulders, .triceps]
    static let pullGroups: Set<Muscle> = [.lats, .upperback, .traps, .biceps, .forearms]

    /// Push against pull over the upper body's working sets (a leg press
    /// pushes too, which says nothing of the shoulders), whole sets; then
    /// upper body, lower body and core by the sets each group is credited
    /// (`Counting.setsPerMuscle`).
    static func balance(_ sets: [PerformedSet], catalog: Catalog) -> [StatsBalance] {
        var push = 0.0
        var pull = 0.0
        for set in sets where set.role == .working && set.continues == nil {
            guard let exercise = catalog.exercises[set.exercise], let top = topGroup(exercise), upperBody.contains(top) else { continue }
            switch force(of: exercise, catalog: catalog) {
            case .push: push += 1
            case .pull: pull += 1
            case .static, nil: break
            }
        }
        var out: [StatsBalance] = []
        if push + pull > 0 {
            out.append(balance("push-pull", "Push and pull", note: "Upper-body working sets", [("Push", push), ("Pull", pull)]))
        }
        let groups = Counting.setsPerMuscle(sets, exercises: catalog.exercises)
        func sum(_ muscles: Set<Muscle>) -> Double {
            groups.filter { muscles.contains($0.key) }.reduce(0) { $0 + $1.value.double }
        }
        let (upper, lower, trunk) = (sum(upperBody), sum(lowerBody), sum(core))
        if upper + lower > 0 {
            var parts = [("Upper body", upper), ("Lower body", lower)]
            if trunk > 0 { parts.append(("Core", trunk)) }
            out.append(balance("body", "Upper and lower body", note: "Sets each muscle group is credited", parts))
        }
        return out
    }

    /// The group an exercise works most; ties go to the first by name.
    static func topGroup(_ exercise: Exercise) -> Muscle? {
        exercise.groupShares.max { a, b in a.value != b.value ? a.value < b.value : a.key.rawValue > b.key.rawValue }?.key
    }

    /// The movement's force; without one, the push or pull the exercise's
    /// main group reads as; nil for neither.
    static func force(of exercise: Exercise, catalog: Catalog) -> Force? {
        if let force = exercise.movement.flatMap({ catalog.movements[$0] })?.force { return force }
        guard let top = topGroup(exercise) else { return nil }
        if pushGroups.contains(top) { return .push }
        if pullGroups.contains(top) { return .pull }
        return nil
    }

    private static func balance(_ id: String, _ title: String, note: String, _ parts: [(String, Double)]) -> StatsBalance {
        let total = parts.reduce(0) { $0 + $1.1 }
        let shares = parts.map { total > 0 ? $0.1 / total : 0 }
        let percents = roundedPercents(shares)
        let built = parts.indices.map { index in
            StatsBalancePart(
                label: parts[index].0, sets: parts[index].1, share: shares[index], percent: "\(percents[index])%",
                text: setsText(Format.decimal(parts[index].1)))
        }
        let sentence = built.map { "\($0.label) \($0.percent), \($0.text)" }.joined(separator: ". ") + "."
        return StatsBalance(id: id, title: title, note: note, parts: built, sentence: sentence)
    }

    /// Whole percentages that add up to 100, the largest remainders rounded up.
    static func roundedPercents(_ shares: [Double]) -> [Int] {
        let raw = shares.map { $0 * 100 }
        var out = raw.map { Int($0.rounded(.down)) }
        guard shares.reduce(0, +) > 0 else { return out }
        var left = 100 - out.reduce(0, +)
        let order = raw.indices.sorted { (raw[$0] - Double(out[$0]), -$0) > (raw[$1] - Double(out[$1]), -$1) }
        for index in order where left > 0 {
            out[index] += 1
            left -= 1
        }
        return out
    }

    // MARK: - Consistency

    /// Every calendar week the period touches, with its sessions.
    static func weeks(_ sessions: [SessionRecords], window: DateInterval, calendar: Calendar) -> [StatsWeek] {
        guard let first = calendar.dateInterval(of: .weekOfYear, for: window.start)?.start else { return [] }
        var starts: [Date] = []
        var cursor = first
        while cursor < window.end {
            starts.append(cursor)
            guard let next = calendar.date(byAdding: .weekOfYear, value: 1, to: cursor), next > cursor else { break }
            cursor = next
        }
        var counts: [Date: Int] = [:]
        for records in sessions {
            guard let start = calendar.dateInterval(of: .weekOfYear, for: records.session.at)?.start else { continue }
            counts[start, default: 0] += 1
        }
        return starts.map { start in
            let sessions = counts[start] ?? 0
            return StatsWeek(
                start: start, sessions: sessions, label: "Week of \(Format.day(start, in: calendar.timeZone))",
                text: sessions == 0 ? "No sessions" : Format.count(sessions, "session"))
        }
    }

    static func consistency(
        _ weeks: [StatsWeek], sessions: Int, all: [SessionRecords], window: DateInterval, now: Date,
        calendar: Calendar
    ) -> StatsConsistency? {
        guard !weeks.isEmpty else { return nil }
        let trained = weeks.count { $0.sessions > 0 }
        let rate = perWeek(sessions, days: days(window, calendar: calendar))
        let streak = streakWeeks(all, now: now, calendar: calendar)
        let streakText = streak >= 2 ? "\(streak) weeks" : nil
        var sentence = "Trained in \(trained) of \(weeks.count) weeks, \(rate) sessions a week."
        if let streakText { sentence += " \(streakText) in a row." }
        return StatsConsistency(
            weeksTrained: trained, weeks: weeks.count, value: "\(trained) of \(weeks.count)", perWeek: rate,
            streak: streakText, sentence: sentence)
    }

    /// Weeks in a row with a session, back from this week, or from the
    /// week before while this one has none yet: a new week breaks nothing.
    static func streakWeeks(_ sessions: [SessionRecords], now: Date, calendar: Calendar) -> Int {
        let trained = Set(sessions.compactMap { calendar.dateInterval(of: .weekOfYear, for: $0.session.at)?.start })
        guard var week = calendar.dateInterval(of: .weekOfYear, for: now)?.start else { return 0 }
        if !trained.contains(week) {
            guard let before = calendar.date(byAdding: .weekOfYear, value: -1, to: week) else { return 0 }
            week = before
        }
        var count = 0
        while trained.contains(week) {
            count += 1
            guard let before = calendar.date(byAdding: .weekOfYear, value: -1, to: week) else { break }
            week = before
        }
        return count
    }

    // MARK: - The last session

    /// The newest session against the newest one before it of the same
    /// routine. A session started empty has no routine to compare within.
    static func lastSession(
        _ sessions: [SessionRecords], scope: StatsScope, catalog: Catalog,
        settings: PresenterSettings
    ) -> StatsSessionComparison? {
        guard let latest = sessions.first, let routine = latest.session.routine,
            let before = sessions.dropFirst().first(where: { $0.session.routine == routine })
        else { return nil }
        func day(_ session: TrainingSession) -> String { Format.day(session.at, in: HistoryPresenter.zone(of: session, settings)) }
        let against = "the session before"
        let now = Totals([latest], exercises: catalog.exercises, settings: settings)
        let then = Totals([before], exercises: catalog.exercises, settings: settings)
        var totals: [StatsTotal] = []
        if let seconds = now.average {
            let change = then.average.map { previous in
                Self.change(Double(seconds - previous) / 60, against: against, text: { "\(Format.number($0, decimals: 0)) min" })
            }
            totals.append(StatsTotal(label: "Time", value: Format.minutes(seconds), change: change))
        }
        totals.append(
            StatsTotal(
                label: "Volume", value: "\(Format.grouped(Int(now.volume.rounded()))) \(settings.defaultUnit.rawValue)",
                change: percent(now.volume, then.volume, against: against)))
        totals.append(StatsTotal(label: "Working sets", value: "\(now.sets)", change: count(now.sets, then.sets, against: against)))
        let today = byExercise([latest])
        let earlier = byExercise([before])
        var shared = 0
        var better = 0
        for (id, entries) in today {
            guard let exercise = catalog.exercises[id], let previous = earlier[id] else { continue }
            let config = SuggestionInputs.config(for: exercise, defaultUnit: settings.defaultUnit)
            guard let value = ExercisePresenter.trend(exercise, sessions: entries, config: config).points.map(\.value).max(),
                let was = ExercisePresenter.trend(exercise, sessions: previous, config: config).points.map(\.value).max(),
                was > 0
            else { continue }
            shared += 1
            if value > was * 1.01 { better += 1 }
        }
        let title = scope == .all ? "\(day(latest.session)), \(HistoryPresenter.routineTitle(of: latest.session))" : day(latest.session)
        return StatsSessionComparison(
            session: latest.session.id, previous: before.session.id, title: title,
            against: "Against \(day(before.session))", totals: totals,
            exercises: shared == 0 ? nil : "Better on \(better) of \(Format.count(shared, "exercise"))")
    }

    // MARK: - Dates and rows

    /// `Sat 24 Oct – Fri 30 Oct`; with the years when they differ.
    static func range(_ window: DateInterval, calendar: Calendar) -> String {
        let zone = calendar.timeZone
        let last = calendar.date(byAdding: .day, value: -1, to: window.end) ?? window.end
        guard calendar.component(.year, from: window.start) != calendar.component(.year, from: last) else {
            return "\(Format.day(window.start, in: zone)) \u{2013} \(Format.day(last, in: zone))"
        }
        return "\(Format.dayWithYear(window.start, in: zone)) \u{2013} \(Format.dayWithYear(last, in: zone))"
    }

    /// The row History shows for the session.
    static func row(_ entry: HistoryEntry, exercises: [ExerciseID: Exercise], settings: PresenterSettings) -> HistoryRow {
        let session = entry.records.session
        return HistoryRow(
            id: session.id, start: session.at, title: HistoryPresenter.routineTitle(of: session),
            date: Format.dayAndTime(session.at, in: HistoryPresenter.zone(of: session, settings)),
            summary: HistoryPresenter.summary(entry.records, exercises: exercises, settings: settings),
            health: entry.local.health.status, isDeload: session.deload == true)
    }
}
