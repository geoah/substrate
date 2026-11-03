import Foundation

/// The session in progress, as `SessionEngine` folds it from its events
/// (§4.1). The journal stores it as a cache of the fold; the events are the truth.
public struct SessionState: Codable, Equatable, Sendable {
    /// The session record's own fields so far (§3.3 TrainingSession, minus slots and groups).
    public var session: SessionHeader
    public var phase: Phase
    /// The routine exactly as it was at Start; nil for an empty session.
    public var plan: Routine?
    /// A snapshot of every exercise any slot ever named.
    public var exercises: [ExerciseID: Exercise]
    /// Rest defaults and counting switches, snapshot at Start (§4.7).
    public var settings: SessionSettings
    /// Display order.
    public var slots: [SlotState]
    public var groups: [GroupState]
    public var rows: [RowID: Row]
    /// The one running countdown, a rest or a timed set.
    public var countdown: Countdown?
    /// Measures the rest before the next set (§4.8).
    public var restClock: RestClock?
    /// A pending row the owner touched since the last tick.
    public var focus: RowID?
    /// The anchor for Up next.
    public var lastCompleted: RowID?
    /// The session record's revision (§4.12).
    public var revision: Int

    public init(
        session: SessionHeader,
        phase: Phase = .active,
        plan: Routine? = nil,
        exercises: [ExerciseID: Exercise] = [:],
        settings: SessionSettings = SessionSettings(),
        slots: [SlotState] = [],
        groups: [GroupState] = [],
        rows: [RowID: Row] = [:],
        countdown: Countdown? = nil,
        restClock: RestClock? = nil,
        focus: RowID? = nil,
        lastCompleted: RowID? = nil,
        revision: Int = 1
    ) {
        self.session = session
        self.phase = phase
        self.plan = plan
        self.exercises = exercises
        self.settings = settings
        self.slots = slots
        self.groups = groups
        self.rows = rows
        self.countdown = countdown
        self.restClock = restClock
        self.focus = focus
        self.lastCompleted = lastCompleted
        self.revision = revision
    }

    public enum Phase: String, Codable, Sendable {
        case active, finished
    }
}

/// The session record's own fields, known at Start or changed since. `name`
/// is derived at Finish (§3.5) and `status` from the phase.
public struct SessionHeader: Codable, Equatable, Sendable {
    public var id: SessionID
    /// The start; Finish may move it.
    public var at: Date
    /// Set at Finish.
    public var endsAt: Date?
    public var routine: RoutineID?
    public var routineVersion: Int?
    public var deload: Bool?
    public var activityType: ActivityType?
    public var pauses: [Pause]
    public var utcOffsetStart: Int?
    public var utcOffsetEnd: Int?
    /// IANA name, `TimeZone.current.identifier` at Start.
    public var timezone: String?
    /// The body mass used at Start, if any.
    public var bodyMass: Mass?
    public var sessionRPE: Decimal?
    public var notes: String?

    public init(
        id: SessionID,
        at: Date,
        endsAt: Date? = nil,
        routine: RoutineID? = nil,
        routineVersion: Int? = nil,
        deload: Bool? = nil,
        activityType: ActivityType? = nil,
        pauses: [Pause] = [],
        utcOffsetStart: Int? = nil,
        utcOffsetEnd: Int? = nil,
        timezone: String? = nil,
        bodyMass: Mass? = nil,
        sessionRPE: Decimal? = nil,
        notes: String? = nil
    ) {
        self.id = id
        self.at = at
        self.endsAt = endsAt
        self.routine = routine
        self.routineVersion = routineVersion
        self.deload = deload
        self.activityType = activityType
        self.pauses = pauses
        self.utcOffsetStart = utcOffsetStart
        self.utcOffsetEnd = utcOffsetEnd
        self.timezone = timezone
        self.bodyMass = bodyMass
        self.sessionRPE = sessionRPE
        self.notes = notes
    }

    /// True while the session clock is paused.
    public var isPaused: Bool { pauses.last.map { $0.end == nil } ?? false }
}

/// One exercise of the session, in display order.
public struct SlotState: Codable, Equatable, Sendable {
    public var key: String
    public var exercise: ExerciseID
    public var group: String?
    /// The routine slot it runs (or replaces); nil for an added exercise.
    public var routineSlot: String?
    /// Notes written in the gym; the routine slot's own notes stay on the plan.
    public var notes: String?
    /// Values set in the gym, and an added slot's own rests (§3.5).
    public var rests: SlotRests
    /// Display order: warm-ups, then working sets, each head followed by its segments.
    public var rows: [RowID]
    /// Copied from the routine slot.
    public var sideOrder: SideOrder?
    public var sideMatch: Bool?
    /// The lane of the slot whose pending sets this one took over (a replace
    /// after a done set); nil for every other slot. Never written to records.
    public var replaces: String?

    public init(
        key: String,
        exercise: ExerciseID,
        group: String? = nil,
        routineSlot: String? = nil,
        notes: String? = nil,
        rests: SlotRests = SlotRests(),
        rows: [RowID] = [],
        sideOrder: SideOrder? = nil,
        sideMatch: Bool? = nil,
        replaces: String? = nil
    ) {
        self.key = key
        self.exercise = exercise
        self.group = group
        self.routineSlot = routineSlot
        self.notes = notes
        self.rests = rests
        self.rows = rows
        self.sideOrder = sideOrder
        self.sideMatch = sideMatch
        self.replaces = replaces
    }

    /// The member a slot counts as in a group's rounds (§4.6): its own key,
    /// or the key of the slot it replaced mid-exercise, so the replacement's
    /// sets keep their rounds and the group keeps its member count.
    public var lane: String { replaces ?? key }
}

/// A slot's rests as the session holds them; nil takes the routine's or the default.
public struct SlotRests: Codable, Equatable, Sendable {
    public var setRest: Seconds?
    public var warmupRest: Seconds?
    public var transition: Seconds?
    public var sideSwitch: Seconds?

    public init(setRest: Seconds? = nil, warmupRest: Seconds? = nil, transition: Seconds? = nil, sideSwitch: Seconds? = nil) {
        self.setRest = setRest
        self.warmupRest = warmupRest
        self.transition = transition
        self.sideSwitch = sideSwitch
    }

    public subscript(kind: SlotRestKind) -> Seconds? {
        get {
            switch kind {
            case .setRest: setRest
            case .warmupRest: warmupRest
            case .transition: transition
            case .sideSwitch: sideSwitch
            }
        }
        set {
            switch kind {
            case .setRest: setRest = newValue
            case .warmupRest: warmupRest = newValue
            case .transition: transition = newValue
            case .sideSwitch: sideSwitch = newValue
            }
        }
    }
}

/// Which of a slot's rests an event sets.
public enum SlotRestKind: String, Codable, Sendable, CaseIterable {
    case setRest, warmupRest, transition, sideSwitch
}

/// A group of the session. `roundRest` holds only a value set in the gym (or
/// on a group made there); the routine group's is read from the plan (§4.7).
public struct GroupState: Codable, Equatable, Sendable {
    public var key: String
    public var groupKind: GroupKind
    public var pairing: Pairing?
    public var roundRest: Seconds?
    public var rounds: Int?
    /// Decides what the session record stores (§3.5).
    public var madeInGym: Bool
    public var changedInGym: Bool
    /// The owner picked the kind; a group made in the gym otherwise follows its member count.
    public var kindSetByHand: Bool

    public init(
        key: String,
        groupKind: GroupKind,
        pairing: Pairing? = nil,
        roundRest: Seconds? = nil,
        rounds: Int? = nil,
        madeInGym: Bool = false,
        changedInGym: Bool = false,
        kindSetByHand: Bool = false
    ) {
        self.key = key
        self.groupKind = groupKind
        self.pairing = pairing
        self.roundRest = roundRest
        self.rounds = rounds
        self.madeInGym = madeInGym
        self.changedInGym = changedInGym
        self.kindSetByHand = kindSetByHand
    }
}

/// One row of a slot's set grid: a planned or added set, or a chain segment.
/// A performed row becomes one set record at Finish.
public struct Row: Codable, Equatable, Sendable {
    public var id: RowID
    /// The slot's key.
    public var slot: String
    /// The routine planned set's key; nil for an added row.
    public var plannedSet: String?
    /// A segment shares its head's role and structure.
    public var role: Role
    public var roleLabel: RoleLabel?
    public var structure: Structure
    public var termination: Termination?
    /// Nil on a head.
    public var chainHead: RowID?
    /// 1 on a head.
    public var chainPosition: Int
    public var side: Side?
    /// Copied from the planned set at row creation (§4.5).
    public var target: SetTarget?
    /// The planned set's `rest`, or one set in the gym. Read from the head.
    public var restOverride: Seconds?
    /// The planned set's, for a structured head.
    public var intraSetPause: Seconds?
    public var timerMode: TimerMode?
    /// Typed or copied values, not yet ticked.
    public var entered: SetValues
    /// A timed, distance or carry row whose timer runs or ran.
    public var timerStartedAt: Date?
    /// Nil while pending.
    public var performed: Performed?
    public var notes: String?
    /// Made by the engine as a chain segment.
    public var generated: Bool
    /// What an un-tick keeps for the next tick: the record id and the rest
    /// fields (§4.3 `uncompleteSet`).
    public var kept: KeptTick?
    /// The owner cleared the row's RPE: the plan's value is no longer
    /// pre-filled (`SessionState.rpePrefill`).
    public var rpePrefillDeclined: Bool

    public init(
        id: RowID,
        slot: String,
        plannedSet: String? = nil,
        role: Role = .working,
        roleLabel: RoleLabel? = nil,
        structure: Structure = .straight,
        termination: Termination? = nil,
        chainHead: RowID? = nil,
        chainPosition: Int = 1,
        side: Side? = nil,
        target: SetTarget? = nil,
        restOverride: Seconds? = nil,
        intraSetPause: Seconds? = nil,
        timerMode: TimerMode? = nil,
        entered: SetValues = SetValues(),
        timerStartedAt: Date? = nil,
        performed: Performed? = nil,
        notes: String? = nil,
        generated: Bool = false,
        kept: KeptTick? = nil,
        rpePrefillDeclined: Bool = false
    ) {
        self.id = id
        self.slot = slot
        self.plannedSet = plannedSet
        self.role = role
        self.roleLabel = roleLabel
        self.structure = structure
        self.termination = termination
        self.chainHead = chainHead
        self.chainPosition = chainPosition
        self.side = side
        self.target = target
        self.restOverride = restOverride
        self.intraSetPause = intraSetPause
        self.timerMode = timerMode
        self.entered = entered
        self.timerStartedAt = timerStartedAt
        self.performed = performed
        self.notes = notes
        self.generated = generated
        self.kept = kept
        self.rpePrefillDeclined = rpePrefillDeclined
    }

    public var isCompleted: Bool { performed != nil }
    public var isPending: Bool { performed == nil }
    public var isHead: Bool { chainHead == nil }
    /// The chain's head: itself for a head.
    public var headID: RowID { chainHead ?? id }
}

/// The values of a set as typed, copied or committed.
public struct SetValues: Codable, Equatable, Sendable {
    /// The load; the added load on a bodyweight-plus exercise.
    public var load: Mass?
    public var assistance: Mass?
    public var reps: Int?
    public var duration: Seconds?
    public var distance: Length?
    public var rpe: Decimal?
    /// How `rpe` got its value: `entered` (a tap), `copied` (PREVIOUS) or
    /// `prefilled` (the plan's value, accepted by a tick). Nil with no `rpe`;
    /// nil beside one reads as `entered`, which the engine writes on commit.
    public var rpeMethod: EffortMethod?
    public var reachedFailure: Bool?
    public var failedRepAttempted: Bool?

    public init(
        load: Mass? = nil,
        assistance: Mass? = nil,
        reps: Int? = nil,
        duration: Seconds? = nil,
        distance: Length? = nil,
        rpe: Decimal? = nil,
        rpeMethod: EffortMethod? = nil,
        reachedFailure: Bool? = nil,
        failedRepAttempted: Bool? = nil
    ) {
        self.load = load
        self.assistance = assistance
        self.reps = reps
        self.duration = duration
        self.distance = distance
        self.rpe = rpe
        self.rpeMethod = rpeMethod
        self.reachedFailure = reachedFailure
        self.failedRepAttempted = failedRepAttempted
    }

    /// The RPE a reader takes as a rating: `rpe` unless it was pre-filled or copied.
    public var ratedRPE: Decimal? {
        EffortMethod.isRating(rpeMethod) ? rpe : nil
    }
}

/// A row's tick.
public struct Performed: Codable, Equatable, Sendable {
    /// The set record's id, minted at the first tick.
    public var recordID: SetID
    public var completedAt: Date
    public var startedAt: Date?
    /// Complete for the measurement.
    public var values: SetValues
    public var restBefore: Seconds?
    public var restPlanned: Seconds?
    public var restSkipped: Bool
    /// Edits after the first tick: the set record's revision is `1 + edits`.
    public var edits: Int
    /// Restored if the tick is undone.
    public var priorRestClock: RestClock?
    /// `individual` for a tick or a Stop, `bulk` for a row Finish marked done.
    public var completion: CompletionMethod

    public init(
        recordID: SetID,
        completedAt: Date,
        startedAt: Date? = nil,
        values: SetValues,
        restBefore: Seconds? = nil,
        restPlanned: Seconds? = nil,
        restSkipped: Bool = false,
        edits: Int = 0,
        priorRestClock: RestClock? = nil,
        completion: CompletionMethod = .individual
    ) {
        self.recordID = recordID
        self.completedAt = completedAt
        self.startedAt = startedAt
        self.values = values
        self.restBefore = restBefore
        self.restPlanned = restPlanned
        self.restSkipped = restSkipped
        self.edits = edits
        self.priorRestClock = priorRestClock
        self.completion = completion
    }
}

/// What an un-ticked row keeps for its next tick.
public struct KeptTick: Codable, Equatable, Sendable {
    public var recordID: SetID
    public var restBefore: Seconds?
    public var restPlanned: Seconds?
    public var restSkipped: Bool
    public var edits: Int

    public init(recordID: SetID, restBefore: Seconds?, restPlanned: Seconds?, restSkipped: Bool, edits: Int) {
        self.recordID = recordID
        self.restBefore = restBefore
        self.restPlanned = restPlanned
        self.restSkipped = restSkipped
        self.edits = edits
    }
}

/// The seven rest slots of 02 §3 as §4.7 picks them, plus the rest before
/// the next block.
public enum RestSlot: String, Codable, Sendable, CaseIterable {
    case override, warmupRest, intraSetPause, sideSwitch, transition, roundRest, blockRest, setRest
}

/// The one running countdown.
public struct Countdown: Codable, Equatable, Sendable {
    public enum Kind: Codable, Equatable, Sendable {
        /// The rest a tick started.
        case rest(RestSlot)
        /// A timed set's timer.
        case timedSet(RowID)
        /// The toolbar's standalone timer.
        case manual
    }

    public var kind: Kind
    public var startedAt: Date
    /// Nil for a stopwatch.
    public var endsAt: Date?

    public init(kind: Kind, startedAt: Date, endsAt: Date?) {
        self.kind = kind
        self.startedAt = startedAt
        self.endsAt = endsAt
    }

    /// A rest or the standalone timer, which ±15 and Skip act on.
    public var isRestLike: Bool {
        if case .timedSet = kind { return false }
        return true
    }

    /// The timed row, for a timed set's countdown.
    public var timedRow: RowID? {
        if case let .timedSet(row) = kind { return row }
        return nil
    }

    /// Whether the countdown's end has come by `date`. A stopwatch never ends.
    public func hasEnded(by date: Date) -> Bool {
        endsAt.map { $0 <= date } ?? false
    }

    /// Whether its end alerts with a notification, not a haptic only (§4.7).
    public var notifies: Bool {
        switch kind {
        case let .rest(slot): RestRules.notifies(slot, seconds: endsAt.map { Seconds($0.timeIntervalSince(startedAt).rounded()) } ?? 0)
        case .manual: true
        case .timedSet: endsAt != nil
        }
    }
}

/// Measures the rest before the next set (§4.8).
public struct RestClock: Codable, Equatable, Sendable {
    /// The tick of the set before.
    public var since: Date
    /// 0 when no timer ran; after any ±15 s.
    public var planned: Seconds
    public var slot: RestSlot?
    public var endedAt: Date?
    public var skipped: Bool

    public init(since: Date, planned: Seconds, slot: RestSlot?, endedAt: Date? = nil, skipped: Bool = false) {
        self.since = since
        self.planned = planned
        self.slot = slot
        self.endedAt = endedAt
        self.skipped = skipped
    }
}
