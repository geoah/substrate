import Foundation

/// A workout routine (`routine.yaml`; no traits): the plan the console's
/// agent writes. The record's server `version` is kept for `routineVersion`.
public struct Routine: Codable, Hashable, Sendable, Identifiable {
    public var id: RoutineID
    /// The flat record's top-level `version`; bundled routines have none, read as 1.
    public var version: Int?
    public var name: String
    public var folder: String?
    public var position: Int?
    public var notes: String?
    public var deload: Bool?
    public var activityType: ActivityType?
    public var groups: [RoutineGroup]
    public var slots: [RoutineSlot]
    /// Unknown properties, kept for round trips (§3.4 rule 7).
    public var extra: [String: JSONValue]

    public init(
        id: RoutineID,
        version: Int? = nil,
        name: String,
        folder: String? = nil,
        position: Int? = nil,
        notes: String? = nil,
        deload: Bool? = nil,
        activityType: ActivityType? = nil,
        groups: [RoutineGroup] = [],
        slots: [RoutineSlot] = [],
        extra: [String: JSONValue] = [:]
    ) {
        self.id = id
        self.version = version
        self.name = name
        self.folder = folder
        self.position = position
        self.notes = notes
        self.deload = deload
        self.activityType = activityType
        self.groups = groups
        self.slots = slots
        self.extra = extra
    }

    /// The version a session records as `routineVersion`.
    public var resolvedVersion: Int { version ?? 1 }
}

/// The Apple Health workout type a routine's sessions are saved as; absent
/// means traditional strength training.
public enum ActivityType: String, Codable, Sendable, CaseIterable {
    case traditionalstrengthtraining, functionalstrengthtraining,
         coretraining, highintensityintervaltraining
}

/// A superset, circuit or paired sets of a routine; its slots name its key.
public struct RoutineGroup: Codable, Hashable, Sendable {
    public var key: String
    public var groupKind: GroupKind
    public var pairing: Pairing?
    public var rounds: Int?
    public var roundRest: Seconds?
    /// Unknown fields, kept for round trips (§3.4 rule 7).
    @UnknownFields public var extra: [String: JSONValue]

    public init(key: String, groupKind: GroupKind, pairing: Pairing? = nil, rounds: Int? = nil, roundRest: Seconds? = nil,
                extra: [String: JSONValue] = [:])
    {
        self.key = key
        self.groupKind = groupKind
        self.pairing = pairing
        self.rounds = rounds
        self.roundRest = roundRest
        self.extra = extra
    }
}

public enum GroupKind: String, Codable, Sendable, CaseIterable {
    case superset, triset, giantset, circuit, paired
}

public enum Pairing: String, Codable, Sendable, CaseIterable {
    case antagonist, samemuscle, preexhaust, upperlower, unrelated, mechanicaldrop
}

/// One exercise of a routine, with its rests and planned sets. Absent rests
/// take the engine's defaults; zero means no rest.
public struct RoutineSlot: Codable, Hashable, Sendable {
    public var key: String
    public var exercise: ExerciseID
    public var group: String?
    public var setRest: Seconds?
    public var warmupRest: Seconds?
    public var transition: Seconds?
    public var sideSwitch: Seconds?
    public var sideOrder: SideOrder?
    public var sideMatch: Bool?
    public var notes: String?
    public var sets: [PlannedSet]
    /// Unknown fields, kept for round trips (§3.4 rule 7).
    @UnknownFields public var extra: [String: JSONValue]

    public init(
        key: String,
        exercise: ExerciseID,
        group: String? = nil,
        setRest: Seconds? = nil,
        warmupRest: Seconds? = nil,
        transition: Seconds? = nil,
        sideSwitch: Seconds? = nil,
        sideOrder: SideOrder? = nil,
        sideMatch: Bool? = nil,
        notes: String? = nil,
        sets: [PlannedSet] = [],
        extra: [String: JSONValue] = [:]
    ) {
        self.key = key
        self.exercise = exercise
        self.group = group
        self.setRest = setRest
        self.warmupRest = warmupRest
        self.transition = transition
        self.sideSwitch = sideSwitch
        self.sideOrder = sideOrder
        self.sideMatch = sideMatch
        self.notes = notes
        self.sets = sets
        self.extra = extra
    }
}

public enum SideOrder: String, Codable, Sendable, CaseIterable {
    case left, right, weakerfirst
}

/// One planned set of a routine slot, and of a session's plan snapshot
/// (`session.slots[].sets`, the same fields). `role` and `structure` carry
/// the kind's declared defaults, which the codec never writes out (§3.4
/// rule 4). An effort target is a range: one value is min equal to max.
public struct PlannedSet: Codable, Hashable, Sendable {
    public var key: String
    public var role: Role = .working
    public var roleLabel: RoleLabel?
    public var structure: Structure = .straight
    public var termination: Termination?
    public var repsMin: Int?
    public var repsMax: Int?
    /// Wins over `loadPercent` when both are given.
    public var load: Mass?
    /// A percentage of `loadReference`, 0...200.
    public var loadPercent: Decimal?
    public var loadReference: LoadReference?
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
    public var dropReps: Int?
    public var intraSetPause: Seconds?
    public var rest: Seconds?
    /// Unknown fields, kept for round trips (§3.4 rule 7).
    @UnknownFields public var extra: [String: JSONValue]

    public init(
        key: String,
        role: Role = .working,
        roleLabel: RoleLabel? = nil,
        structure: Structure = .straight,
        termination: Termination? = nil,
        repsMin: Int? = nil,
        repsMax: Int? = nil,
        load: Mass? = nil,
        loadPercent: Decimal? = nil,
        loadReference: LoadReference? = nil,
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
        rest: Seconds? = nil,
        extra: [String: JSONValue] = [:]
    ) {
        self.key = key
        self.role = role
        self.roleLabel = roleLabel
        self.structure = structure
        self.termination = termination
        self.repsMin = repsMin
        self.repsMax = repsMax
        self.load = load
        self.loadPercent = loadPercent
        self.loadReference = loadReference
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
        self.rest = rest
        self.extra = extra
    }

    /// The effort the planned set aims at, as an RPE: `rpeMax`, else
    /// `10 − rirMin` (`SetTarget.effortTop`).
    public var effortTop: Decimal? {
        effortRangeTop(rpeMin: rpeMin, rpeMax: rpeMax, rirMin: rirMin, rirMax: rirMax)
    }
}

extension PlannedSet {
    private enum CodingKeys: String, CodingKey {
        case key, role, roleLabel, structure, termination, repsMin, repsMax, load, loadPercent, loadReference, assistance
        case rpeMin, rpeMax, rirMin, rirMax, durationMin, durationMax, timerMode, distance, tempo, miniSetReps, miniSets
        case myoStopRule, totalReps, drops, dropPercent, dropStep, dropReps, intraSetPause, rest, extra
    }

    /// The single `rpe` and `rir` a planned set had before effort targets
    /// became ranges (D3).
    private enum LegacyKeys: String, CodingKey {
        case rpe, rir
    }

    /// The JSON form the journal stores (a Start event's routine). A journal
    /// written before effort targets became ranges holds `rpe` or `rir`: each
    /// reads as a range of one value, as `HistoryMigration` does for history,
    /// so a session started on the older build resumes with its effort targets.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.decode(String.self, forKey: .key)
        role = try c.decode(Role.self, forKey: .role)
        roleLabel = try c.decodeIfPresent(RoleLabel.self, forKey: .roleLabel)
        structure = try c.decode(Structure.self, forKey: .structure)
        termination = try c.decodeIfPresent(Termination.self, forKey: .termination)
        repsMin = try c.decodeIfPresent(Int.self, forKey: .repsMin)
        repsMax = try c.decodeIfPresent(Int.self, forKey: .repsMax)
        load = try c.decodeIfPresent(Mass.self, forKey: .load)
        loadPercent = try c.decodeIfPresent(Decimal.self, forKey: .loadPercent)
        loadReference = try c.decodeIfPresent(LoadReference.self, forKey: .loadReference)
        assistance = try c.decodeIfPresent(Mass.self, forKey: .assistance)
        rpeMin = try c.decodeIfPresent(Decimal.self, forKey: .rpeMin)
        rpeMax = try c.decodeIfPresent(Decimal.self, forKey: .rpeMax)
        rirMin = try c.decodeIfPresent(Decimal.self, forKey: .rirMin)
        rirMax = try c.decodeIfPresent(Decimal.self, forKey: .rirMax)
        durationMin = try c.decodeIfPresent(Seconds.self, forKey: .durationMin)
        durationMax = try c.decodeIfPresent(Seconds.self, forKey: .durationMax)
        timerMode = try c.decodeIfPresent(TimerMode.self, forKey: .timerMode)
        distance = try c.decodeIfPresent(Length.self, forKey: .distance)
        tempo = try c.decodeIfPresent(String.self, forKey: .tempo)
        miniSetReps = try c.decodeIfPresent(Int.self, forKey: .miniSetReps)
        miniSets = try c.decodeIfPresent(Int.self, forKey: .miniSets)
        myoStopRule = try c.decodeIfPresent(MyoStopRule.self, forKey: .myoStopRule)
        totalReps = try c.decodeIfPresent(Int.self, forKey: .totalReps)
        drops = try c.decodeIfPresent(Int.self, forKey: .drops)
        dropPercent = try c.decodeIfPresent(Decimal.self, forKey: .dropPercent)
        dropStep = try c.decodeIfPresent(Mass.self, forKey: .dropStep)
        dropReps = try c.decodeIfPresent(Int.self, forKey: .dropReps)
        intraSetPause = try c.decodeIfPresent(Seconds.self, forKey: .intraSetPause)
        rest = try c.decodeIfPresent(Seconds.self, forKey: .rest)
        _extra = try c.decode(UnknownFields.self, forKey: .extra)
        let legacy = try decoder.container(keyedBy: LegacyKeys.self)
        if rpeMin == nil, rpeMax == nil, let rpe = try legacy.decodeIfPresent(Decimal.self, forKey: .rpe) {
            rpeMin = rpe
            rpeMax = rpe
        }
        if rirMin == nil, rirMax == nil, let rir = try legacy.decodeIfPresent(Decimal.self, forKey: .rir) {
            rirMin = rir
            rirMax = rir
        }
    }
}

public enum Role: String, Codable, Sendable, CaseIterable {
    case warmup, working
}

public enum RoleLabel: String, Codable, Sendable, CaseIterable {
    case top, backoff
}

public enum Structure: String, Codable, Sendable, CaseIterable {
    case straight, drop, myo, myomatch, restpause, cluster
}

public enum Termination: String, Codable, Sendable, CaseIterable {
    case reps, reprange, amrap, failure, rirstop, time, distance, totalreps
}

public enum TimerMode: String, Codable, Sendable, CaseIterable {
    case countdown, stopwatch
}
