import Foundation

/// The PREVIOUS cell (§5.4): the same row last time, wherever the exercise
/// was done.
public struct PreviousSet: Equatable, Sendable {
    public var session: SessionID
    /// When that session started.
    public var date: Date
    /// The record it shows.
    public var record: SetID
    /// Its values, with its RPE only when that was a rating (a pre-filled or
    /// copied RPE is no rating, so PREVIOUS neither shows nor copies it).
    public var values: SetValues
    /// `35 kg × 13 @ 9.5`.
    public var text: String
    /// That session was a deload: the cell marks it "deload" (§5.2).
    public var deload: Bool

    public init(session: SessionID, date: Date, record: SetID, values: SetValues, text: String, deload: Bool) {
        self.session = session
        self.date = date
        self.record = record
        self.values = values
        self.text = text
        self.deload = deload
    }

    /// What a tap copies into the row's entered values (`editSet`): the
    /// load, reps, time and distance, and a rated RPE as `copied` (§5.4),
    /// which a later tap on the RPE makes `entered`.
    public var copy: SetValuesPatch {
        var values = values
        values.rpeMethod = values.rpe == nil ? nil : .copied
        values.reachedFailure = nil
        values.failedRepAttempted = nil
        return .setting(values)
    }
}

/// Reading last time's sets: PREVIOUS (§5.4), and what the controller needs
/// from history to start a slot (the weaker side, an added exercise's rows
/// and rests).
public enum Previous {
    /// The row's PREVIOUS: in the newest finished session with a set of this
    /// exercise on no setup (V1), excluding the active one, the record of the
    /// same role and ordinal, then the same side (a side record) or chain
    /// position (a drop, a mini-set). Today's n-th slot of the exercise reads
    /// last time's n-th slot of it.
    public static func find(for id: RowID, in state: SessionState, history: HistoryIndex) -> PreviousSet? {
        guard let exercise = state.exercise(ofRow: id) else { return nil }
        return find(for: id, in: state, last: history.last(exercise.id, setup: nil, excluding: state.session.id))
    }

    /// The same, with last time's session of the row's exercise already found.
    static func find(for id: RowID, in state: SessionState, last: ExerciseHistory?) -> PreviousSet? {
        guard let row = state.rows[id], let head = state.rows[row.headID], let slot = state.slot(row.slot),
              let exercise = state.exercises[slot.exercise], let last, let ordinal = state.ordinal(of: id)
        else { return nil }
        let sameExercise = state.slots.filter { $0.exercise == slot.exercise }.map(\.key)
        guard let n = sameExercise.firstIndex(of: slot.key), n < last.slots.count else { return nil }
        let sets = last.sets(inSlot: last.slots[n])
        let ordinals = SetLabel.ordinals(records: sets)
        let byID = Dictionary(sets.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let match = sets.first { set in
            let setHead = set.continues.flatMap { byID[$0] } ?? set
            guard setHead.role == head.role, ordinals[set.id] == ordinal else { return false }
            if let side = row.side { return set.side == side }
            return set.side == nil && (set.chainPosition ?? 1) == row.chainPosition
        }
        guard let match else { return nil }
        let rated = match.ratedRPE
        let method: EffortMethod? = rated == nil ? nil : (match.rpe == rated ? match.rpeMethod : match.rirMethod) ?? .entered
        let values = SetValues(load: match.load, assistance: match.assistance, reps: match.reps, duration: match.duration,
                               distance: match.distance, rpe: rated, rpeMethod: method,
                               reachedFailure: match.reachedFailure, failedRepAttempted: match.failedRepAttempted)
        return PreviousSet(session: last.session.id, date: last.session.at, record: match.id, values: values,
                           text: Format.set(values, measurement: exercise.measurement, rpe: true),
                           deload: last.session.deload == true)
    }

    /// The weaker side of a one-side-at-a-time exercise last time, for
    /// `Start.firstSides` and `AddExercise.firstSide` (§4.9 `weakerfirst`):
    /// the side with fewer working reps (or less time) in the newest session
    /// with sided sets; nil when level or never done, which starts on the left.
    public static func weakerSide(of exercise: ExerciseID, history: HistoryIndex, excluding active: SessionID? = nil) -> Side? {
        guard let last = history.history(of: exercise, setup: nil, excluding: active)
            .first(where: { $0.sets.contains { $0.side != nil } })
        else { return nil }
        var totals: [Side: Int] = [:]
        for set in last.sets where set.role == .working {
            guard let side = set.side else { continue }
            totals[side, default: 0] += set.reps ?? set.duration ?? 0
        }
        let left = totals[.left] ?? 0
        let right = totals[.right] ?? 0
        return left == right ? nil : left < right ? .left : .right
    }

    /// An added exercise's shape from last time (§4.2 `addExercise`): one
    /// head per set, warm-ups first, and the slot's rests from the session
    /// record; one working set and no rests when it was never done.
    public static func lastShape(of exercise: ExerciseID, history: HistoryIndex,
                                 excluding active: SessionID? = nil) -> (roles: [Role], rests: SlotRests)
    {
        guard let last = history.last(exercise, setup: nil, excluding: active), let key = last.slots.first else {
            return ([.working], SlotRests())
        }
        let heads = last.sets(inSlot: key).filter { $0.continues == nil }
        let roles = heads.filter { $0.role == .warmup }.map(\.role) + heads.filter { $0.role == .working }.map(\.role)
        let slot = last.session.slots.first { $0.key == key }
        let rests = SlotRests(setRest: slot?.setRest, warmupRest: slot?.warmupRest, transition: slot?.transition,
                              sideSwitch: slot?.sideSwitch)
        return (roles.isEmpty ? [.working] : roles, rests)
    }
}
