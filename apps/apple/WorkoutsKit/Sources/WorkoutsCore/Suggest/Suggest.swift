import Foundation

// The next-set suggestion engine: research 03 Part B (§8 to §13) as one pure
// function, plus the app's own rules around it (DESIGN §5.3). Its input types
// are 03 §9's, kept apart from the record model so the engine is testable from
// 03's vectors alone; `SuggestionInputs` (Present/) builds them.

/// The kind of a set as the suggestion engine reads it (03 §9.2).
public enum SuggestKind: Sendable, Hashable {
    case warmup, normal, failure, drop, myoActivation, myoMini, timed
}

/// How an exercise's load field is read (03 §9.1).
public enum LoadMode03: Sendable, Hashable {
    /// The load is the weight moved.
    case external
    /// The load is added to `bodyShare` of body mass; 0 or nil is plain bodyweight.
    case bodyweightAdded
    /// The load is assistance taken off `bodyShare` of body mass.
    case bodyweightAssisted
    /// No load at all (reps only, duration, distance).
    case none
}

/// Per-exercise settings (03 §9.1). Loads are in `unit`.
public struct ExerciseConfig: Sendable, Hashable {
    public var unit: MassUnit
    public var mode: LoadMode03
    /// Share of body mass moved, 0 to 1 (bodyweight modes).
    public var bodyShare: Double
    /// Lightest loadable value (the empty bar). Bodyweight modes ignore it.
    /// Nil reads as 0 when `step` is set.
    public var base: Double?
    /// The load step. Nil (or under 0.01) means no load grid: every rule that
    /// moves a load keeps it and works on reps instead (DESIGN §5.3).
    public var step: Double?
    /// Heaviest loadable value; in bodyweight modes the most added load or assistance.
    public var maxLoad: Double?
    /// bodyweightAdded only: false for plain bodyweight (a grid of one point).
    public var canAddLoad: Bool
    public var defaultRepsMin: Int
    public var defaultRepsMax: Int
    public var defaultRPE: Double
    public var repGain: Int
    /// Load cut per drop, a fraction (0.20 is 20 %).
    public var dropPercent: Double
    /// Seconds added per session to a timed set.
    public var timeStep: Int
    /// The mini-set cap (DESIGN §4.9: the planned `miniSets` when the routine sets one).
    public var myoMaxMiniSets: Int
    /// The myo-rep stop rule. Nil switches it off, for rest-pause and cluster
    /// chains, which stop only at the cap or the total (DESIGN §5.3).
    public var myoStopRule: MyoStopRule?

    public init(
        unit: MassUnit,
        mode: LoadMode03 = .external,
        bodyShare: Double = 1.0,
        base: Double? = nil,
        step: Double? = nil,
        maxLoad: Double? = nil,
        canAddLoad: Bool = true,
        defaultRepsMin: Int = 8,
        defaultRepsMax: Int = 12,
        defaultRPE: Double = 8.0,
        repGain: Int = 1,
        dropPercent: Double = 0.20,
        timeStep: Int = 5,
        myoMaxMiniSets: Int = 5,
        myoStopRule: MyoStopRule? = .repdrop
    ) {
        self.unit = unit
        self.mode = mode
        self.bodyShare = bodyShare
        self.base = base
        self.step = step
        self.maxLoad = maxLoad
        self.canAddLoad = canAddLoad
        self.defaultRepsMin = defaultRepsMin
        self.defaultRepsMax = defaultRepsMax
        self.defaultRPE = defaultRPE
        self.repGain = repGain
        self.dropPercent = dropPercent
        self.timeStep = timeStep
        self.myoMaxMiniSets = myoMaxMiniSets
        self.myoStopRule = myoStopRule
    }
}

/// The plan values in force for a set (03 §9.2 and §9.4).
public struct Target03: Sendable, Hashable {
    public var repsMin: Int?
    public var repsMax: Int?
    /// The top of the plan's effort range: `rpeMax`, or `10 − rirMin` when
    /// the plan gives reps in reserve.
    public var rpe: Double?
    /// In the exercise unit; the added load or the assistance in bodyweight modes.
    public var load: Double?
    public var durationMin: Int?
    public var durationMax: Int?
    /// A fraction, as `ExerciseConfig.dropPercent` (0.10 is 10 %).
    public var dropPercent: Double?
    /// In the exercise unit. Wins over `dropPercent` when both are set.
    public var dropStep: Double?
    public var myoMiniReps: Int?
    public var myoTotalReps: Int?
    /// AMRAP or failure with no `repsMax`: the top of the range is unbounded (03 §13).
    public var openTop: Bool

    public init(
        repsMin: Int? = nil,
        repsMax: Int? = nil,
        rpe: Double? = nil,
        load: Double? = nil,
        durationMin: Int? = nil,
        durationMax: Int? = nil,
        dropPercent: Double? = nil,
        dropStep: Double? = nil,
        myoMiniReps: Int? = nil,
        myoTotalReps: Int? = nil,
        openTop: Bool = false
    ) {
        self.repsMin = repsMin
        self.repsMax = repsMax
        self.rpe = rpe
        self.load = load
        self.durationMin = durationMin
        self.durationMax = durationMax
        self.dropPercent = dropPercent
        self.dropStep = dropStep
        self.myoMiniReps = myoMiniReps
        self.myoTotalReps = myoTotalReps
        self.openTop = openTop
    }
}

/// A performed set (03 §9.2). Side pairs arrive folded into one set.
public struct DoneSet: Sendable, Hashable {
    public var kind: SuggestKind
    public var setup: String?
    /// In `unit`; the added load (nil or 0 for none) or the assistance in bodyweight modes.
    public var load: Double?
    /// The unit `load` was entered in; the engine converts it to the exercise's.
    public var unit: MassUnit
    public var reps: Int?
    public var repsLeft: Int?
    public var repsRight: Int?
    /// Seconds, timed sets.
    public var duration: Int?
    /// As rated; pass `10 − rir` when only reps in reserve were rated. Never
    /// a pre-filled or copied value, which goes in `unratedRPE`.
    public var rpe: Double?
    /// An RPE the set was logged with that is no rating (the plan's pre-fill,
    /// or a copy of last time's): read as no RPE (03 §10.2), and the
    /// explanation says "assuming RPE x" of the set (DESIGN §5.3).
    public var unratedRPE: Double?
    public var reachedFailure: Bool
    /// The plan in force when the set was done.
    public var target: Target03
    public var completedAt: Date
    /// Its order within the exercise in that session.
    public var position: Int
    /// The working ordinal; the warm-up ordinal for warm-ups.
    public var ordinal: Int?

    public init(
        kind: SuggestKind,
        setup: String? = nil,
        load: Double? = nil,
        unit: MassUnit,
        reps: Int? = nil,
        repsLeft: Int? = nil,
        repsRight: Int? = nil,
        duration: Int? = nil,
        rpe: Double? = nil,
        unratedRPE: Double? = nil,
        reachedFailure: Bool = false,
        target: Target03 = Target03(),
        completedAt: Date,
        position: Int,
        ordinal: Int? = nil
    ) {
        self.kind = kind
        self.setup = setup
        self.load = load
        self.unit = unit
        self.reps = reps
        self.repsLeft = repsLeft
        self.repsRight = repsRight
        self.duration = duration
        self.rpe = rpe
        self.unratedRPE = unratedRPE
        self.reachedFailure = reachedFailure
        self.target = target
        self.completedAt = completedAt
        self.position = position
        self.ordinal = ordinal
    }
}

/// One earlier session's sets of this exercise and setup (03 §9.3).
public struct PastSession: Sendable, Hashable {
    public var start: Date
    /// Kilograms, the body mass in force that day.
    public var bodyMass: Double?
    public var sets: [DoneSet]

    public init(start: Date, bodyMass: Double? = nil, sets: [DoneSet]) {
        self.start = start
        self.bodyMass = bodyMass
        self.sets = sets
    }
}

/// The set to suggest (03 §9.4).
public struct PendingSet: Sendable, Hashable {
    public var kind: SuggestKind
    /// The working ordinal `k`, or the warm-up ordinal for a warm-up.
    public var ordinal: Int
    public var target: Target03
    /// Drops: the set before. Mini-sets: the chain's head. A back-off set
    /// (DESIGN §5.3): the completed working set it backs off from.
    public var parent: DoneSet?
    /// Reps of the earlier mini-sets in this chain, in order.
    public var miniSetsDone: [Int]

    public init(
        kind: SuggestKind,
        ordinal: Int,
        target: Target03 = Target03(),
        parent: DoneSet? = nil,
        miniSetsDone: [Int] = []
    ) {
        self.kind = kind
        self.ordinal = ordinal
        self.target = target
        self.parent = parent
        self.miniSetsDone = miniSetsDone
    }
}

/// What the engine suggests for the pending set (03 §9.5).
public struct Suggestion: Sendable, Equatable, Hashable {
    /// Exercise unit; the added load or the assistance in bodyweight modes; nil: the lifter picks.
    public var load: Double?
    /// Nil for timed sets, drops to failure and open mini-sets.
    public var reps: Int?
    /// Seconds, timed sets.
    public var duration: Int?
    public var targetRPE: Double?
    /// True: no further mini-set (or no lighter load for a drop).
    public var stop: Bool
    public var reason: Reason
    /// The one-line explanation (03 §12).
    public var text: String

    public init(
        load: Double? = nil,
        reps: Int? = nil,
        duration: Int? = nil,
        targetRPE: Double? = nil,
        stop: Bool = false,
        reason: Reason,
        text: String
    ) {
        self.load = load
        self.reps = reps
        self.duration = duration
        self.targetRPE = targetRPE
        self.stop = stop
        self.reason = reason
        self.text = text
    }
}

/// Which rule produced a suggestion (03 §9.5; `backoff` is the app's, DESIGN §5.3).
public enum Reason: Sendable, Hashable {
    case warmupAsLastTime, historyAddRep, historyLoadUp, historyLoadDown, historyNewRange,
         historyAtMax, sessionKeep, sessionUp, sessionDown, sessionCoarse, sessionAtMax, coldStartRoutineLoad,
         coldStartPickLoad, myoMini, myoStop, drop, dropAtMinimum, timedHistory, timedSession, timedLoadUp,
         timedColdStart, backoff
}

/// Suggests the pending set's load and reps (or duration) from today's sets of
/// the exercise and its history, by research 03 Part B and DESIGN §5.3.
///
/// - Parameters:
///   - today: this session's completed sets of the exercise.
///   - history: earlier sessions of the exercise and setup, newest first,
///     excluding this one.
///   - bodyMass: today's body mass in kilograms.
///   - now: the instant the gap to each past session is counted to.
///   - calendar: the owner's calendar; it only counts whole local days.
/// - Returns: nil when no rule applies (a warm-up with no warm-up last time).
public func suggest(
    pending: PendingSet,
    exercise: ExerciseConfig,
    today: [DoneSet],
    history: [PastSession],
    bodyMass: Double?,
    now: Date,
    calendar: Calendar
) -> Suggestion? {
    SuggestRules(pending: pending, exercise: exercise, today: today, history: history,
          bodyMass: bodyMass, now: now, calendar: calendar).run()
}
