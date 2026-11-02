import Foundation

/// One journal entry: an event with its id and instant (§4.2). The controller
/// floors `at` to the second and clamps it to the previous event's, and
/// appends the envelope only when `SessionEngine.apply` succeeds.
public struct EventEnvelope: Codable, Equatable, Sendable, Identifiable {
    public var id: EventID
    public var at: Date
    public var event: SessionEvent

    public init(id: EventID, at: Date, event: SessionEvent) {
        self.id = id
        self.at = at
        self.event = event
    }
}

/// Everything that changes a session (§4.2). Ids the reducer needs (rows,
/// records, slots) are minted by the controller and carried here, so a fold
/// is deterministic. Where an event may create segment rows, a list of
/// pre-minted ids is consumed in order; if it runs short the engine derives
/// further ids from the envelope's id, which is just as deterministic.
public enum SessionEvent: Codable, Equatable, Sendable {
    case start(Start)
    /// A field focused, or a row's RPE or set-kind control opened.
    case touchRow(row: RowID)
    case editSet(EditSet)
    case completeSet(CompleteSet)
    case uncompleteSet(row: RowID)
    case addSet(AddSet)
    case removeSet(row: RowID)
    case changeSetKind(ChangeSetKind)
    /// The per-set rest override; nil clears it.
    case setRowRest(row: RowID, seconds: Seconds?)
    case addExercise(AddExercise)
    case removeExercise(slot: String)
    /// Every slot key, in the new order.
    case reorderExercises(slots: [String])
    case replaceExercise(ReplaceExercise)
    /// Joins `slot` to `partner`'s group, or makes a new one with `group`
    /// (nil: the first unused lowercase letter).
    case groupExercises(slot: String, partner: String, group: String?)
    case ungroupExercise(slot: String)
    case setGroup(SetGroup)
    /// Nil goes back to the default.
    case setSlotRest(slot: String, rest: SlotRestKind, seconds: Seconds?)
    /// Nil or empty text clears the note.
    case setNote(target: NoteTarget, text: String?)
    case pauseClock
    case resumeClock
    /// The toolbar's standalone timer.
    case startRest(seconds: Seconds)
    /// ±15 from every surface.
    case adjustRest(seconds: Seconds)
    case skipRest
    /// `target` nil runs a stopwatch.
    case startTimedSet(row: RowID, target: Seconds?)
    case stopTimedSet(CompleteSet)
    case cancelTimedSet(row: RowID)
    case finish(Finish)
    /// The controller deletes the journal after it applies.
    case discard

    /// Whether the event changes a property of the session record, so raises
    /// its revision (§4.2's right-hand column, §4.12).
    public var touchesSessionRecord: Bool {
        switch self {
        case .start, .addExercise, .removeExercise, .reorderExercises, .replaceExercise, .groupExercises,
             .ungroupExercise, .setGroup, .setSlotRest, .setNote, .pauseClock, .resumeClock, .finish:
            true
        case .touchRow, .editSet, .completeSet, .uncompleteSet, .addSet, .removeSet, .changeSetKind, .setRowRest,
             .startRest, .adjustRest, .skipRest, .startTimedSet, .stopTimedSet, .cancelTimedSet, .discard:
            false
        }
    }
}

public extension SessionEvent {
    /// Start a session from a routine, or an empty one.
    struct Start: Codable, Equatable, Sendable {
        public var session: SessionID
        /// The routine exactly as it is now; nil for an empty session.
        public var routine: Routine?
        /// A snapshot of every exercise the routine names. A routine slot whose
        /// exercise is missing (an unreadable record) is left out.
        public var exercises: [Exercise]
        public var settings: SessionSettings
        public var bodyMass: Mass?
        /// `TimeZone.current.identifier`.
        public var timeZone: String
        /// Minutes east of UTC at the start.
        public var utcOffset: Int
        /// One id per planned set and known segment, in routine order: each
        /// planned set's head, then its drops, clusters or second side.
        /// `SessionEngine.rowIDCount(for:exercises:)` says how many.
        public var rowIDs: [RowID]
        /// The first side of a `weakerfirst` slot, by slot key: the side with
        /// fewer reps last time, which only the controller can read. Left when absent.
        public var firstSides: [String: Side]

        public init(
            session: SessionID,
            routine: Routine?,
            exercises: [Exercise],
            settings: SessionSettings = SessionSettings(),
            bodyMass: Mass? = nil,
            timeZone: String,
            utcOffset: Int,
            rowIDs: [RowID] = [],
            firstSides: [String: Side] = [:]
        ) {
            self.session = session
            self.routine = routine
            self.exercises = exercises
            self.settings = settings
            self.bodyMass = bodyMass
            self.timeZone = timeZone
            self.utcOffset = utcOffset
            self.rowIDs = rowIDs
            self.firstSides = firstSides
        }
    }

    struct EditSet: Codable, Equatable, Sendable {
        public var row: RowID
        public var values: SetValuesPatch
        public var note: FieldPatch<String>
        /// Ids for a mini-set an edit may bring back (an edit that undoes a stop).
        public var segmentRowIDs: [RowID]

        public init(row: RowID, values: SetValuesPatch = SetValuesPatch(), note: FieldPatch<String> = .unchanged, segmentRowIDs: [RowID] = []) {
            self.row = row
            self.values = values
            self.note = note
            self.segmentRowIDs = segmentRowIDs
        }
    }

    /// A tick (`completeSet`) or a timed set's Stop (`stopTimedSet`).
    struct CompleteSet: Codable, Equatable, Sendable {
        public var row: RowID
        /// The set record's id; a row ticked before keeps its first one.
        public var recordID: SetID
        /// What the row shows, typed or ghost, except a `rirstop` hint (§5.5).
        /// For Stop, a nil duration is filled from the timer.
        public var values: SetValues
        /// Ids for the mini-set the tick may generate.
        public var segmentRowIDs: [RowID]
        /// 03 §11.7 found no lighter load for the next drop: the drops left in
        /// the chain are removed ("No lighter load for a drop"). The controller
        /// asks the suggestion engine, which owns the load grid.
        public var noLighterLoad: Bool

        public init(row: RowID, recordID: SetID, values: SetValues, segmentRowIDs: [RowID] = [], noLighterLoad: Bool = false) {
            self.row = row
            self.recordID = recordID
            self.values = values
            self.segmentRowIDs = segmentRowIDs
            self.noLighterLoad = noLighterLoad
        }
    }

    struct AddSet: Codable, Equatable, Sendable {
        public var slot: String
        public var row: RowID
        public var role: Role
        /// The second side's id on a one-side-at-a-time exercise.
        public var segmentRowIDs: [RowID]

        public init(slot: String, row: RowID, role: Role, segmentRowIDs: [RowID] = []) {
            self.slot = slot
            self.row = row
            self.role = role
            self.segmentRowIDs = segmentRowIDs
        }
    }

    struct ChangeSetKind: Codable, Equatable, Sendable {
        public var row: RowID
        public var kind: SetKind
        /// Ids for drop rows or the mini-set the change creates.
        public var segmentRowIDs: [RowID]

        public init(row: RowID, kind: SetKind, segmentRowIDs: [RowID] = []) {
            self.row = row
            self.kind = kind
            self.segmentRowIDs = segmentRowIDs
        }
    }

    struct AddExercise: Codable, Equatable, Sendable {
        public var slot: String
        public var exercise: Exercise
        /// One head row per role, warm-ups first: last time's, else one working row.
        public var roles: [Role]
        /// Head and second-side ids, consumed in order.
        public var rowIDs: [RowID]
        /// Last time's rests for the exercise.
        public var rests: SlotRests
        /// Insert after this slot's group block (the picker opened from an
        /// exercise menu); nil appends.
        public var after: String?
        /// The first side of a one-side-at-a-time exercise; left when nil.
        public var firstSide: Side?

        public init(
            slot: String,
            exercise: Exercise,
            roles: [Role] = [.working],
            rowIDs: [RowID] = [],
            rests: SlotRests = SlotRests(),
            after: String? = nil,
            firstSide: Side? = nil
        ) {
            self.slot = slot
            self.exercise = exercise
            self.roles = roles
            self.rowIDs = rowIDs
            self.rests = rests
            self.after = after
            self.firstSide = firstSide
        }
    }

    struct ReplaceExercise: Codable, Equatable, Sendable {
        public var slot: String
        /// The replacing slot's key.
        public var newSlot: String
        public var exercise: Exercise
        /// Ids for second sides when the new exercise is done one side at a time.
        public var segmentRowIDs: [RowID]

        public init(slot: String, newSlot: String, exercise: Exercise, segmentRowIDs: [RowID] = []) {
            self.slot = slot
            self.newSlot = newSlot
            self.exercise = exercise
            self.segmentRowIDs = segmentRowIDs
        }
    }

    struct SetGroup: Codable, Equatable, Sendable {
        public var group: String
        /// Nil leaves the kind as it is.
        public var kind: GroupKind?
        public var pairing: FieldPatch<Pairing>
        public var roundRest: FieldPatch<Seconds>

        public init(group: String, kind: GroupKind? = nil, pairing: FieldPatch<Pairing> = .unchanged, roundRest: FieldPatch<Seconds> = .unchanged) {
            self.group = group
            self.kind = kind
            self.pairing = pairing
            self.roundRest = roundRest
        }
    }

    struct Finish: Codable, Equatable, Sendable {
        /// The start and end confirmed in the Finish sheet; the engine clamps both.
        public var start: Date
        public var end: Date
        public var unticked: UntickedFate
        /// CR10, 1 to 10; nil when skipped.
        public var sessionRPE: Decimal?
        /// Nil keeps the session's notes.
        public var notes: String?
        /// Minutes east of UTC at the end.
        public var utcOffset: Int

        public init(start: Date, end: Date, unticked: UntickedFate = .drop, sessionRPE: Decimal? = nil, notes: String? = nil, utcOffset: Int) {
            self.start = start
            self.end = end
            self.unticked = unticked
            self.sessionRPE = sessionRPE
            self.notes = notes
            self.utcOffset = utcOffset
        }
    }
}

/// The set-kind menu's choices (§7.6).
public enum SetKind: String, Codable, Sendable, CaseIterable {
    case warmup, working, failure, drop, myo, myomatch
}

/// What a note is written on.
public enum NoteTarget: Codable, Equatable, Sendable {
    case session
    case slot(String)
}

/// What Finish does with rows nobody ticked (§7.7).
public enum UntickedFate: Codable, Equatable, Sendable {
    case drop
    /// Each row with its shown values and a record id; rows left out are dropped.
    case markDone([MarkDone])

    public struct MarkDone: Codable, Equatable, Sendable {
        public var row: RowID
        public var recordID: SetID
        public var values: SetValues

        public init(row: RowID, recordID: SetID, values: SetValues) {
            self.row = row
            self.recordID = recordID
            self.values = values
        }
    }
}

/// One field of a patch: left as it is, set, or cleared.
public enum FieldPatch<Value: Codable & Equatable & Sendable>: Codable, Equatable, Sendable {
    case unchanged
    case set(Value)
    case cleared

    public func applied(to value: Value?) -> Value? {
        switch self {
        case .unchanged: value
        case let .set(new): new
        case .cleared: nil
        }
    }
}

/// An edit of a row's values (§4.2 `editSet`).
public struct SetValuesPatch: Codable, Equatable, Sendable {
    public var load: FieldPatch<Mass>
    public var assistance: FieldPatch<Mass>
    public var reps: FieldPatch<Int>
    public var duration: FieldPatch<Seconds>
    public var distance: FieldPatch<Length>
    /// Setting it is a rating (`rpeMethod`); clearing it removes the rating
    /// and its method, and on a pending row stops the plan's pre-fill.
    public var rpe: FieldPatch<Decimal>
    /// How a set `rpe` was given: `entered` (nil reads so: a tap, even on the
    /// value already shown) or `copied` (PREVIOUS). `prefilled` and
    /// `imported` are refused (`EngineError.invalidEffort`); a pre-fill is
    /// accepted by the tick (`SessionState.acceptingRPEPrefill`).
    public var rpeMethod: EffortMethod?
    public var reachedFailure: FieldPatch<Bool>
    public var failedRepAttempted: FieldPatch<Bool>

    public init(
        load: FieldPatch<Mass> = .unchanged,
        assistance: FieldPatch<Mass> = .unchanged,
        reps: FieldPatch<Int> = .unchanged,
        duration: FieldPatch<Seconds> = .unchanged,
        distance: FieldPatch<Length> = .unchanged,
        rpe: FieldPatch<Decimal> = .unchanged,
        rpeMethod: EffortMethod? = nil,
        reachedFailure: FieldPatch<Bool> = .unchanged,
        failedRepAttempted: FieldPatch<Bool> = .unchanged
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

    /// Every field the values set; the rest unchanged. PREVIOUS copies this
    /// way (§5.4), its RPE with `rpeMethod` `copied`.
    public static func setting(_ values: SetValues) -> SetValuesPatch {
        func patch<V>(_ value: V?) -> FieldPatch<V> { value.map(FieldPatch.set) ?? .unchanged }
        return SetValuesPatch(
            load: patch(values.load), assistance: patch(values.assistance), reps: patch(values.reps),
            duration: patch(values.duration), distance: patch(values.distance), rpe: patch(values.rpe),
            rpeMethod: values.rpe == nil ? nil : values.rpeMethod,
            reachedFailure: patch(values.reachedFailure), failedRepAttempted: patch(values.failedRepAttempted)
        )
    }

    /// The lifter picks an RPE: `entered`, whatever the row showed.
    public static func enteringRPE(_ value: Decimal) -> SetValuesPatch {
        SetValuesPatch(rpe: .set(value), rpeMethod: .entered)
    }

    /// The lifter clears the RPE: no rating, and no pre-fill on a pending row.
    public static let clearingRPE = SetValuesPatch(rpe: .cleared)
}

public extension SetValues {
    /// The values with `patch` merged in. A set `rpe` takes the patch's
    /// method (`entered` when it names none); a cleared one goes with its
    /// method; an unchanged one keeps both.
    func applying(_ patch: SetValuesPatch) -> SetValues {
        let method: EffortMethod? = switch patch.rpe {
        case .unchanged: rpeMethod
        case .set: patch.rpeMethod ?? .entered
        case .cleared: nil
        }
        return SetValues(
            load: patch.load.applied(to: load),
            assistance: patch.assistance.applied(to: assistance),
            reps: patch.reps.applied(to: reps),
            duration: patch.duration.applied(to: duration),
            distance: patch.distance.applied(to: distance),
            rpe: patch.rpe.applied(to: rpe),
            rpeMethod: method,
            reachedFailure: patch.reachedFailure.applied(to: reachedFailure),
            failedRepAttempted: patch.failedRepAttempted.applied(to: failedRepAttempted)
        )
    }
}

/// Why `SessionEngine.apply` refused an event. The controller turns it into
/// a haptic and no change (§4.2).
public enum EngineError: Error, Equatable, Sendable {
    case noSession
    case sessionActive
    case unknownRow
    case unknownSlot
    case rowNotPending
    case rowNotCompleted
    /// A segment ticked before the link before it.
    case segmentBeforeItsLink
    case incompleteValues(missing: [ValueField])
    case timerRunning
    case noTimer
    case invalidOrder
    case groupTooSmall
    case finished
    // the app's own, beyond §4.2's list:
    case unknownGroup
    /// A slot key or row id that is already taken.
    case duplicateID
    /// A set kind §4.3 forbids for the row (a segment, a completed row's
    /// structure once a segment is done, a structure on a one-side exercise).
    case kindNotAllowed
    case clockPaused
    case clockNotPaused
    /// An RPE method the event may not claim: `prefilled` on a value that is
    /// not the row's pre-fill, `prefilled` in an edit, or `imported`.
    case invalidEffort
}

/// A value a measurement requires (§4.3's table).
public enum ValueField: String, Codable, Sendable, CaseIterable {
    case load, assistance, reps, duration, distance
}
