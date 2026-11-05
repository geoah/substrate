import Foundation

/// How long the session has left (DESIGN §4.15): each pending row's working
/// time plus the rest the plan gives after it, as the rest rule would pick it
/// with the rows done in plan order (§4.7, transitions, round rests, side
/// switches and warm-up rests included), plus what is left of a running rest.
public enum SessionEstimate {
    /// A rep set's working time with nothing measured yet.
    public static let defaultWork: Seconds = 45

    /// Seconds left, or nil with nothing pending.
    public static func timeLeft(_ state: SessionState, now: Date) -> Seconds? {
        let pending = UpNext.sequence(state).filter { state.rows[$0]?.isPending == true }
        guard !pending.isEmpty else { return nil }
        let measured = workTimes(state)
        var total: Seconds = 0
        if let countdown = state.countdown, countdown.isRestLike, let end = countdown.endsAt, end > now {
            total += Seconds(end.timeIntervalSince(now).rounded(.up))
        }
        // Each row done in turn on a copy, so the rule reads the plan as it will stand.
        var done = state
        for (index, id) in pending.enumerated() {
            total += work(id, in: state, measured: measured, now: now) + miniSets(after: id, in: state)
            done.rows[id]?.performed = Performed(recordID: SetID("estimate"), completedAt: now, values: SetValues())
            done.lastCompleted = id
            if index < pending.count - 1 {
                total += RestRules.after(id, in: done).seconds
            }
        }
        return total
    }

    /// A pending row's working time: a timed set's goal, or what is left of
    /// it while it runs; else the median of today's measured sets of its
    /// exercise, else of all of today's, else 45 s.
    static func work(_ id: RowID, in state: SessionState, measured: [ExerciseID: [Seconds]], now: Date) -> Seconds {
        guard let row = state.rows[id] else { return defaultWork }
        let head = state.rows[row.headID] ?? row
        if let countdown = state.countdown, countdown.timedRow == id {
            guard let end = countdown.endsAt else { return defaultWork }
            return max(0, Seconds(end.timeIntervalSince(now).rounded(.up)))
        }
        if state.exercise(ofRow: id)?.measurement.isTimedHold == true {
            let target = row.target ?? head.target
            if let goal = target?.durationMax ?? target?.durationMin { return goal }
        }
        let exercise = state.slot(row.slot)?.exercise
        let own = exercise.flatMap { measured[$0] } ?? []
        return median(own) ?? median(measured.values.flatMap { $0 }) ?? defaultWork
    }

    /// The mini-sets a myo-rep, myo-rep match or rest-pause head grows one at a
    /// time after it, which no row holds yet: the planned count (else 3),
    /// each a short bout and its pause.
    static func miniSets(after id: RowID, in state: SessionState) -> Seconds {
        guard let row = state.rows[id], row.isHead, row.side == nil, row.structure.growsOneAtATime,
            state.chain(of: id).count == 1
        else { return 0 }
        let count = state.plannedSet(ofRow: id)?.miniSets ?? row.target?.miniSets ?? 3
        let pause = row.intraSetPause ?? RestDefaults.intraSetPause(row.structure)
        return count * (15 + pause)
    }

    /// Today's working times by exercise: a timed set's start to its end;
    /// else the gap from the tick before less the rest the plan gave, so a
    /// rest run over counts as the set's (a set ticked with no touch first
    /// counts its working time into `restBefore`, §4.8). Kept within 5 s to
    /// 5 min, so a set marked done long after is no sample.
    static func workTimes(_ state: SessionState) -> [ExerciseID: [Seconds]] {
        let done = state.rows.values.compactMap { row -> (Row, Performed)? in
            guard let performed = row.performed, performed.completion == .individual else { return nil }
            return (row, performed)
        }
        .sorted { $0.1.completedAt < $1.1.completedAt }
        var times: [ExerciseID: [Seconds]] = [:]
        var previous: Date?
        for (row, performed) in done {
            defer { previous = performed.completedAt }
            let seconds: Double
            if let start = performed.startedAt {
                seconds = performed.completedAt.timeIntervalSince(start)
            } else if let previous, let rest = performed.restPlanned ?? performed.restBefore {
                seconds = performed.completedAt.timeIntervalSince(previous) - Double(rest)
            } else {
                continue
            }
            guard seconds >= 5, seconds <= 300, let exercise = state.slot(row.slot)?.exercise else { continue }
            times[exercise, default: []].append(Seconds(seconds.rounded()))
        }
        return times
    }

    static func median(_ values: [Seconds]) -> Seconds? {
        guard !values.isEmpty else { return nil }
        let sorted = values.sorted()
        return sorted[sorted.count / 2]
    }
}
