import Foundation

/// A training session (`session.yaml`; traits `temporal(range)` and
/// `revised`): one visit to the gym as the app ran it.
public struct TrainingSession: Codable, Hashable, Sendable, Identifiable {
    public var id: SessionID
    /// The trait's start.
    public var at: Date
    /// The trait's end; absent while active.
    public var endsAt: Date?
    public var name: String?
    /// Absent on decode reads as active; encoded only when finished (§3.4 rule 5).
    public var status: SessionStatus
    public var routine: RoutineID?
    public var routineVersion: Int?
    public var deload: Bool?
    /// `health/workout`'s sport; the app writes `strength`.
    public var sport: Sport?
    /// The sport as an imported source named it; absent for the app's own.
    public var sportSource: String?
    /// The Apple Health save's workout type only; `sport` is what the session was.
    public var activityType: ActivityType?
    public var slots: [SessionSlot]
    public var groups: [SessionGroup]
    public var pauses: [Pause]
    /// Minutes east of UTC, -1080...1080.
    public var utcOffsetStart: Int?
    public var utcOffsetEnd: Int?
    /// An IANA zone name (`timezone`, as core's `recurring` trait spells it).
    public var timezone: String?
    public var bodyMass: Mass?
    /// CR10, 0...10.
    public var sessionRPE: Decimal?
    public var notes: String?
    /// HealthKit's uppercase UUID of the saved workout.
    public var healthKitWorkout: String?
    /// V2 only.
    public var workout: RecordRef?
    public var source: RecordRef?
    public var about: RecordRef?
    public var origin: RecordRef?
    public var revision: Int?
    /// What wrote it, such as "substrate-workouts@0.1.0".
    public var adapter: String?
    /// Unknown properties, kept for round trips (§3.4 rule 7).
    public var extra: [String: JSONValue]

    public init(
        id: SessionID,
        at: Date,
        endsAt: Date? = nil,
        name: String? = nil,
        status: SessionStatus = .active,
        routine: RoutineID? = nil,
        routineVersion: Int? = nil,
        deload: Bool? = nil,
        sport: Sport? = nil,
        sportSource: String? = nil,
        activityType: ActivityType? = nil,
        slots: [SessionSlot] = [],
        groups: [SessionGroup] = [],
        pauses: [Pause] = [],
        utcOffsetStart: Int? = nil,
        utcOffsetEnd: Int? = nil,
        timezone: String? = nil,
        bodyMass: Mass? = nil,
        sessionRPE: Decimal? = nil,
        notes: String? = nil,
        healthKitWorkout: String? = nil,
        workout: RecordRef? = nil,
        source: RecordRef? = nil,
        about: RecordRef? = nil,
        origin: RecordRef? = nil,
        revision: Int? = nil,
        adapter: String? = nil,
        extra: [String: JSONValue] = [:]
    ) {
        self.id = id
        self.at = at
        self.endsAt = endsAt
        self.name = name
        self.status = status
        self.routine = routine
        self.routineVersion = routineVersion
        self.deload = deload
        self.sport = sport
        self.sportSource = sportSource
        self.activityType = activityType
        self.slots = slots
        self.groups = groups
        self.pauses = pauses
        self.utcOffsetStart = utcOffsetStart
        self.utcOffsetEnd = utcOffsetEnd
        self.timezone = timezone
        self.bodyMass = bodyMass
        self.sessionRPE = sessionRPE
        self.notes = notes
        self.healthKitWorkout = healthKitWorkout
        self.workout = workout
        self.source = source
        self.about = about
        self.origin = origin
        self.revision = revision
        self.adapter = adapter
        self.extra = extra
    }
}

public enum SessionStatus: String, Codable, Sendable, CaseIterable {
    case active, finished
}

/// What sport a session was, in `health/workout`'s code set (`session.sport`).
public enum Sport: String, Codable, Sendable, CaseIterable {
    case running, walking, hiking, cycling, swimming, strength, yoga, pilates, rowing, elliptical, stairs, hiit, dance,
         boxing, climbing, tennis, golf, skiing, other
}

/// One exercise of a session, in display order; its sets name it by `key`.
public struct SessionSlot: Codable, Hashable, Sendable {
    public var key: String
    public var exercise: ExerciseID
    public var group: String?
    public var routineSlot: String?
    public var notes: String?
    /// Only when set in the gym, or for an exercise with no routine slot (§3.5).
    public var setRest: Seconds?
    public var warmupRest: Seconds?
    public var transition: Seconds?
    public var sideSwitch: Seconds?
    /// The routine slot's planned sets as the session started with them,
    /// done or not; empty on a slot with no routine slot (§3.5).
    public var sets: [PlannedSet]
    /// Unknown fields, kept for round trips (§3.4 rule 7).
    @UnknownFields public var extra: [String: JSONValue]

    public init(
        key: String,
        exercise: ExerciseID,
        group: String? = nil,
        routineSlot: String? = nil,
        notes: String? = nil,
        setRest: Seconds? = nil,
        warmupRest: Seconds? = nil,
        transition: Seconds? = nil,
        sideSwitch: Seconds? = nil,
        sets: [PlannedSet] = [],
        extra: [String: JSONValue] = [:]
    ) {
        self.key = key
        self.exercise = exercise
        self.group = group
        self.routineSlot = routineSlot
        self.notes = notes
        self.setRest = setRest
        self.warmupRest = warmupRest
        self.transition = transition
        self.sideSwitch = sideSwitch
        self.sets = sets
        self.extra = extra
    }
}

/// A superset, circuit or paired sets of a session, stored once per group.
public struct SessionGroup: Codable, Hashable, Sendable {
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

/// A span the session's clock was paused; `end` is absent while paused.
public struct Pause: Codable, Hashable, Sendable {
    public var start: Date
    public var end: Date?
    /// Unknown fields, kept for round trips (§3.4 rule 7).
    @UnknownFields public var extra: [String: JSONValue]

    public init(start: Date, end: Date? = nil, extra: [String: JSONValue] = [:]) {
        self.start = start
        self.end = end
        self.extra = extra
    }
}

/// One finished session as records: what the history file holds and what V2 uploads.
public struct SessionRecords: Codable, Hashable, Sendable, Identifiable {
    public var session: TrainingSession
    public var sets: [PerformedSet]

    public init(session: TrainingSession, sets: [PerformedSet]) {
        self.session = session
        self.sets = sets
    }

    public var id: SessionID { session.id }
}

/// The exercises and routines the app can start from.
public struct Catalog: Sendable {
    public var exercises: [ExerciseID: Exercise]
    public var routines: [RoutineID: Routine]
    /// The stations exercises are done on. V1 bundles and writes none.
    public var setups: [SetupID: Setup]
    /// Records the source could not decode; skipped, and listed in Settings.
    public var unreadable: [UnreadableRecord]

    public init(exercises: [ExerciseID: Exercise] = [:], routines: [RoutineID: Routine] = [:], setups: [SetupID: Setup] = [:],
                unreadable: [UnreadableRecord] = [])
    {
        self.exercises = exercises
        self.routines = routines
        self.setups = setups
        self.unreadable = unreadable
    }
}

public extension TrainingSession {
    /// The heading a session records as `name` (§3.5), on its own clock:
    /// `"<routine name>, <EEE d MMM HH:mm>"`, such as `Fully Body Collection #1,
    /// Fri 30 Oct 13:42`, and `Training session, …` for an empty session.
    /// Formatted with `en_GB` and the session's zone, so it is deterministic.
    static func name(routine routineName: String?, start: Date, timeZone: TimeZone) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_GB")
        formatter.timeZone = timeZone
        formatter.dateFormat = "EEE d MMM HH:mm"
        return "\(routineName ?? "Training session"), \(formatter.string(from: start))"
    }

    /// Minutes east of UTC in `timeZone` at `date`, as `utcOffsetStart` and
    /// `utcOffsetEnd` store them.
    static func utcOffset(in timeZone: TimeZone, at date: Date) -> Int {
        timeZone.secondsFromGMT(for: date) / 60
    }

    /// The session RPE as Apple Health's workout effort score, which runs
    /// from 1 to 10 (§9.5): nil when the owner skipped it, and for a CR10 0
    /// ("nothing at all"), which has no effort score, rather than sent as 1.
    var healthEffortScore: Double? {
        guard let rpe = sessionRPE, rpe >= 1 else { return nil }
        return min(10, NSDecimalNumber(decimal: rpe).doubleValue)
    }
}
