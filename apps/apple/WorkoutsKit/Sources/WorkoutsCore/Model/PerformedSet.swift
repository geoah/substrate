import Foundation

/// One performed set (`set.yaml`; traits `temporal(point)` and `revised`). A
/// drop, a mini-set or one side is a record of its own, chained to its head.
public struct PerformedSet: Codable, Hashable, Sendable, Identifiable {
    public var id: SetID
    /// When the set ended (the tick); `timeBasis` says what kind of instant it is.
    public var at: Date
    public var session: SessionID
    public var exercise: ExerciseID
    public var slot: String
    /// From 1.
    public var order: Int
    public var ordinal: Int?
    /// The station it was done on; V1 never writes one.
    public var setup: SetupID?
    public var role: Role = .working
    public var roleLabel: RoleLabel?
    public var structure: Structure = .straight
    /// The chain's head; nil on the head.
    public var continues: SetID?
    public var chainPosition: Int?
    public var side: Side?
    /// Nil when the exercise moves no load.
    public var loadMode: LoadMode?
    public var load: Mass?
    public var assistance: Mass?
    /// A resistance that is not a load, as the equipment labels it (a red band, level 7).
    public var resistanceLabel: String?
    public var reps: Int?
    public var duration: Seconds?
    public var distance: Length?
    /// What the load counted, copied from the exercise when logged (with a load).
    public var loadBasis: LoadBasis?
    /// How many implements, copied from the exercise when logged (with a load).
    public var implements: Int?
    /// The share of body mass moved, 0...1, copied when logged (bodyweight modes).
    public var bodyShare: Decimal?
    /// Copied from the exercise when logged, on every set the app writes.
    public var laterality: Laterality?
    /// Whether `reps` counts both sides or one (with reps).
    public var repsBasis: RepsBasis?
    public var rpe: Decimal?
    /// How `rpe` got its value; written exactly when `rpe` is.
    public var rpeMethod: EffortMethod?
    public var rir: Decimal?
    /// How `rir` got its value; written exactly when `rir` is.
    public var rirMethod: EffortMethod?
    /// the app writes `rirbased`; absent reads as `rirbased`.
    public var rpeScale: RPEScale?
    public var reachedFailure: Bool?
    public var failedRepAttempted: Bool?
    /// Ticked on its own, marked done at Finish, or imported.
    public var completionMethod: CompletionMethod?
    public var notes: String?
    public var sourceType: String?
    /// What `at` is: the tick, a time of its session's, or an estimate.
    public var timeBasis: TimeBasis?
    public var startedAt: Date?
    public var restBefore: Seconds?
    /// How `restBefore` was measured; written exactly when it is.
    public var restMethod: RestMethod?
    public var restPlanned: Seconds?
    public var restSkipped: Bool?
    public var target: SetTarget?
    public var origin: RecordRef?
    public var revision: Int?
    /// Unknown properties, kept for round trips (§3.4 rule 7).
    public var extra: [String: JSONValue]

    public init(
        id: SetID,
        at: Date,
        session: SessionID,
        exercise: ExerciseID,
        slot: String,
        order: Int,
        ordinal: Int? = nil,
        setup: SetupID? = nil,
        role: Role = .working,
        roleLabel: RoleLabel? = nil,
        structure: Structure = .straight,
        continues: SetID? = nil,
        chainPosition: Int? = nil,
        side: Side? = nil,
        loadMode: LoadMode? = nil,
        load: Mass? = nil,
        assistance: Mass? = nil,
        resistanceLabel: String? = nil,
        reps: Int? = nil,
        duration: Seconds? = nil,
        distance: Length? = nil,
        loadBasis: LoadBasis? = nil,
        implements: Int? = nil,
        bodyShare: Decimal? = nil,
        laterality: Laterality? = nil,
        repsBasis: RepsBasis? = nil,
        rpe: Decimal? = nil,
        rpeMethod: EffortMethod? = nil,
        rir: Decimal? = nil,
        rirMethod: EffortMethod? = nil,
        rpeScale: RPEScale? = nil,
        reachedFailure: Bool? = nil,
        failedRepAttempted: Bool? = nil,
        completionMethod: CompletionMethod? = nil,
        notes: String? = nil,
        sourceType: String? = nil,
        timeBasis: TimeBasis? = nil,
        startedAt: Date? = nil,
        restBefore: Seconds? = nil,
        restMethod: RestMethod? = nil,
        restPlanned: Seconds? = nil,
        restSkipped: Bool? = nil,
        target: SetTarget? = nil,
        origin: RecordRef? = nil,
        revision: Int? = nil,
        extra: [String: JSONValue] = [:]
    ) {
        self.id = id
        self.at = at
        self.session = session
        self.exercise = exercise
        self.slot = slot
        self.order = order
        self.ordinal = ordinal
        self.setup = setup
        self.role = role
        self.roleLabel = roleLabel
        self.structure = structure
        self.continues = continues
        self.chainPosition = chainPosition
        self.side = side
        self.loadMode = loadMode
        self.load = load
        self.assistance = assistance
        self.resistanceLabel = resistanceLabel
        self.reps = reps
        self.duration = duration
        self.distance = distance
        self.loadBasis = loadBasis
        self.implements = implements
        self.bodyShare = bodyShare
        self.laterality = laterality
        self.repsBasis = repsBasis
        self.rpe = rpe
        self.rpeMethod = rpeMethod
        self.rir = rir
        self.rirMethod = rirMethod
        self.rpeScale = rpeScale
        self.reachedFailure = reachedFailure
        self.failedRepAttempted = failedRepAttempted
        self.completionMethod = completionMethod
        self.notes = notes
        self.sourceType = sourceType
        self.timeBasis = timeBasis
        self.startedAt = startedAt
        self.restBefore = restBefore
        self.restMethod = restMethod
        self.restPlanned = restPlanned
        self.restSkipped = restSkipped
        self.target = target
        self.origin = origin
        self.revision = revision
        self.extra = extra
    }
}

public enum Side: String, Codable, Sendable, CaseIterable {
    case left, right
}

public enum LoadMode: String, Codable, Sendable, CaseIterable {
    case external, bodyweight, bodyweightplus, assisted
}

/// The scale `rpe` and the target's effort are on. `rirbased` is the app's;
/// `hevy` and `strong` word a half step differently; `cr10` is Health
/// Connect's and FIT's, on which RIR = 10 − RPE does not hold.
public enum RPEScale: String, Codable, Sendable, CaseIterable {
    case rirbased, hevy, strong, cr10

    /// Whether RIR = 10 − RPE holds on the scale. An absent scale reads as `rirbased`.
    public static func isRIRBased(_ scale: RPEScale?) -> Bool { scale != .cr10 }
}

/// Whether a set's `reps` counts both sides together or one side's.
public enum RepsBasis: String, Codable, Sendable, CaseIterable {
    case total, perside
}

/// How a set was completed (`set.completionMethod`).
public enum CompletionMethod: String, Codable, Sendable, CaseIterable {
    /// Ticked, or stopped, on its own.
    case individual
    /// Finish's "Mark them done as shown".
    case bulk
    case imported
}

/// What a set's `at` is (`set.timeBasis`).
public enum TimeBasis: String, Codable, Sendable, CaseIterable {
    /// The instant it was completed.
    case completion
    /// A time of its session's standing in: an import's start, a bulk row's end.
    case session
    case estimated
}

/// How `restBefore` was measured (`set.restMethod`).
public enum RestMethod: String, Codable, Sendable, CaseIterable {
    /// To the set's recorded start: a timed set's play ended the rest.
    case start
    /// To a touch on the device: Skip, a touch once the countdown is over, the tick.
    case interaction
    case imported
    case estimated
}

/// What a percentage load is a percentage of (`loadReference`).
public enum LoadReference: String, Codable, Sendable, CaseIterable {
    case onerm, e1rm, trainingmax, bodymass
}

/// The planned set a performed set fulfils and its targets as they stood
/// (`set.target`): every field of the planned set, with the values the
/// engine ran with where the plan left them out (§3.5). A chain's later
/// records carry the planned set's key and only their own.
public struct SetTarget: Codable, Hashable, Sendable {
    public var plannedSet: String?
    public var termination: Termination?
    public var repsMin: Int?
    public var repsMax: Int?
    /// Always the resolved absolute load.
    public var load: Mass?
    public var loadPercent: Decimal?
    public var loadReference: LoadReference?
    /// The reference the percentage was taken of, as it stood.
    public var referenceLoad: Mass?
    public var assistance: Mass?
    public var rpeMin: Decimal?
    public var rpeMax: Decimal?
    public var rirMin: Decimal?
    public var rirMax: Decimal?
    public var durationMin: Seconds?
    public var durationMax: Seconds?
    public var timerMode: TimerMode?
    public var distance: Length?
    public var tempo: String?
    public var miniSetReps: Int?
    public var miniSets: Int?
    public var myoStopRule: MyoStopRule?
    public var totalReps: Int?
    public var drops: Int?
    public var dropPercent: Decimal?
    public var dropStep: Mass?
    /// Absent means each drop to failure.
    public var dropReps: Int?
    public var intraSetPause: Seconds?
    /// Unknown fields, kept for round trips (§3.4 rule 7).
    @UnknownFields public var extra: [String: JSONValue]

    public init(
        plannedSet: String? = nil,
        termination: Termination? = nil,
        repsMin: Int? = nil,
        repsMax: Int? = nil,
        load: Mass? = nil,
        loadPercent: Decimal? = nil,
        loadReference: LoadReference? = nil,
        referenceLoad: Mass? = nil,
        assistance: Mass? = nil,
        rpeMin: Decimal? = nil,
        rpeMax: Decimal? = nil,
        rirMin: Decimal? = nil,
        rirMax: Decimal? = nil,
        durationMin: Seconds? = nil,
        durationMax: Seconds? = nil,
        timerMode: TimerMode? = nil,
        distance: Length? = nil,
        tempo: String? = nil,
        miniSetReps: Int? = nil,
        miniSets: Int? = nil,
        myoStopRule: MyoStopRule? = nil,
        totalReps: Int? = nil,
        drops: Int? = nil,
        dropPercent: Decimal? = nil,
        dropStep: Mass? = nil,
        dropReps: Int? = nil,
        intraSetPause: Seconds? = nil,
        extra: [String: JSONValue] = [:]
    ) {
        self.plannedSet = plannedSet
        self.termination = termination
        self.repsMin = repsMin
        self.repsMax = repsMax
        self.load = load
        self.loadPercent = loadPercent
        self.loadReference = loadReference
        self.referenceLoad = referenceLoad
        self.assistance = assistance
        self.rpeMin = rpeMin
        self.rpeMax = rpeMax
        self.rirMin = rirMin
        self.rirMax = rirMax
        self.durationMin = durationMin
        self.durationMax = durationMax
        self.timerMode = timerMode
        self.distance = distance
        self.tempo = tempo
        self.miniSetReps = miniSetReps
        self.miniSets = miniSets
        self.myoStopRule = myoStopRule
        self.totalReps = totalReps
        self.drops = drops
        self.dropPercent = dropPercent
        self.dropStep = dropStep
        self.dropReps = dropReps
        self.intraSetPause = intraSetPause
        self.extra = extra
    }

    /// The effort the target aims at, as an RPE on the RIR-based scale: the
    /// top of its RPE range, else 10 − the fewest reps in reserve. The
    /// suggestion's load selector (02 §5.4) and the Rows screen's pre-fill.
    public var effortTop: Decimal? {
        effortRangeTop(rpeMin: rpeMin, rpeMax: rpeMax, rirMin: rirMin, rirMax: rirMax)
    }
}

/// The top of an effort range as an RPE: `rpeMax` (or a lone `rpeMin`), else
/// `10 − rirMin` (or a lone `rirMax`); nil outside 0...10.
func effortRangeTop(rpeMin: Decimal?, rpeMax: Decimal?, rirMin: Decimal?, rirMax: Decimal?) -> Decimal? {
    let value = rpeMax ?? rpeMin ?? (rirMin ?? rirMax).map { 10 - $0 }
    guard let value, value >= 0, value <= 10 else { return nil }
    return value
}

public extension PerformedSet {
    /// The RPE a reader takes as a rating on the RIR-based scale (the package
    /// README, "Effort provenance"): `rpe` when its method is `entered`,
    /// `imported` or absent; else `10 − rir` when that is a rating. Nil for a
    /// pre-filled or copied value, for a method this build does not know
    /// (kept in `extra`), on a scale that is not RIR-based (`cr10`, or one it
    /// does not know), and for no value. What the suggestion, e1RM, PREVIOUS
    /// and the verdict read.
    var ratedRPE: Decimal? {
        guard isRIRBasedScale else { return nil }
        if let rpe, isRating(rpeMethod, field: "rpeMethod") { return rpe }
        if let rir, isRating(rirMethod, field: "rirMethod") { return 10 - rir }
        return nil
    }

    /// The RPE the set was logged with whatever its method: `rpe`, else
    /// `10 − rir` on an RIR-based scale. What a pre-filled set shows.
    var loggedRPE: Decimal? {
        rpe ?? (RPEScale.isRIRBased(rpeScale) ? rir.map { 10 - $0 } : nil)
    }

    /// A pre-filled or copied effort, as an RPE on the RIR-based scale, with
    /// the method of the field it came from: what the suggestion reads as an
    /// assumption. Nil when the set holds a rating (`ratedRPE`), a method or
    /// a scale this build does not know, or no value.
    var unratedRPE: (value: Decimal, method: EffortMethod)? {
        guard ratedRPE == nil, isRIRBasedScale else { return nil }
        if let rpe, let rpeMethod, !rpeMethod.isRating { return (rpe, rpeMethod) }
        if rpe == nil, let rir, let rirMethod, !rirMethod.isRating { return (10 - rir, rirMethod) }
        return nil
    }

    /// The scale reads RPE as reps in reserve: `rirbased`, `hevy`, `strong`,
    /// or none written; not `cr10`, nor a value this build does not know.
    private var isRIRBasedScale: Bool {
        rpeScale.map { RPEScale.isRIRBased($0) } ?? (extra["rpeScale"] == nil)
    }

    /// Whether an effort field's method makes its value a rating. Absent is
    /// a set written before the field existed, a rating; a value this build
    /// does not know (kept raw in `extra`) is not.
    private func isRating(_ method: EffortMethod?, field: String) -> Bool {
        if let method { return method.isRating }
        return extra[field] == nil
    }

    /// What the load counted when the set was logged: the set's own copy,
    /// else the exercise's (a set written before the field existed).
    func resolvedLoadBasis(_ exercise: Exercise?) -> LoadBasis { loadBasis ?? exercise?.resolvedLoadBasis ?? .total }
    func resolvedImplements(_ exercise: Exercise?) -> Int { implements ?? exercise?.resolvedImplements ?? 1 }
    func resolvedBodyShare(_ exercise: Exercise?) -> Decimal { bodyShare ?? exercise?.resolvedBodyShare ?? 1 }
    func resolvedLaterality(_ exercise: Exercise?) -> Laterality { laterality ?? exercise?.resolvedLaterality ?? .bilateral }
}
