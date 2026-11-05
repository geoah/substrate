import Foundation

/// The progression offer for one exercise (DESIGN §5.3): the load the
/// suggestion holds, the one the owner may take, and why.
public struct SlotProgression: Sendable, Hashable {
    /// In the exercise's unit, as the lifter sets it.
    public var current: Double
    public var next: Double
    /// `105 kg`, `BW − 30 kg`.
    public var currentText: String
    /// `107.5 kg`, `BW − 25 kg`.
    public var nextText: String
    /// `All 3 sets reached 12 reps at 105 kg in both sessions, …`, or the
    /// engine's own: `Last time 105 kg × 12 with 4 left, top of range.`
    public var reason: String
}

/// Two sessions that show an exercise ready for the next load (DESIGN §5.3,
/// the progression offer's evidence).
public struct ProgressionCheck: Sendable, Hashable {
    /// The load both sessions were done at, as the lifter sets it, in the
    /// exercise's unit: the load, the added load or the assistance.
    public var load: Double
    /// One step of the grid up; for an assisted exercise, one step less assistance.
    public var next: Double
    /// `105 kg`, `BW + 10 kg`, `BW − 30 kg`.
    public var loadText: String
    /// `107.5 kg`, `BW + 12.5 kg`, `BW − 25 kg`.
    public var nextText: String
    /// The top of the range every set reached.
    public var topReps: Int
    /// How many sets reached it in each session.
    public var sets: Int
    /// The fewest reps left the plan asked for; nil when it names no effort.
    public var plannedLeft: Double?
    /// Some set's effort was not rated (the plan's value logged untouched, a copy).
    public var unrated: Bool

    /// `All 3 sets reached 12 reps at 105 kg in both sessions, with at
    /// least 2 reps left.`; unrated, `… Effort wasn't rated.`
    public var reason: String {
        let sets = sets == 1 ? "The set" : sets == 2 ? "Both sets" : "All \(sets) sets"
        let head = "\(sets) reached \(topReps) reps at \(loadText) in both sessions"
        guard let plannedLeft, plannedLeft > 0 else { return head + "." }
        if unrated { return head + ". Effort wasn't rated." }
        let left = Format.number(plannedLeft)
        return head + ", with at least \(left) \(left == "1" ? "rep" : "reps") left."
    }
}

/// The progression offer's evidence (DESIGN §5.3). In each of the exercise's
/// two newest sessions (no deload between: `history` holds none, and the
/// caller leaves out an exercise whose newest sessions include one; the newer
/// within 28 days, the older within 28 days of it), at least `planned` sets at
/// the heaviest working load reached the top of today's planned range, every
/// one of them on today's targets (the same range and effort), each with at
/// least the planned reps left where it was rated, at the same load both
/// times; none ended for technique, pain or another reason, and none reached
/// failure where the plan left reps. Lighter sets (a back-off) neither count
/// nor spoil it; drops and mini-sets never count.
public enum ProgressionOffer {
    public static let sessions = 2
    public static let maxGapDays = 28

    /// - Parameters:
    ///   - history: the exercise's sessions, newest first, deloads left out
    ///     (`SuggestionInputs.history`).
    ///   - plan: today's target for the exercise's first working set.
    ///   - planned: today's working sets on that target.
    public static func check(
        history: [PastSession], exercise: ExerciseConfig, plan: Target03, planned: Int, now: Date, calendar: Calendar
    ) -> ProgressionCheck? {
        let recent = Array(history.prefix(sessions))
        guard recent.count == sessions, let top = plan.repsMax, !plan.openTop, planned > 0,
            SuggestMath.days(from: recent[0].start, to: now, calendar: calendar) <= maxGapDays,
            SuggestMath.days(from: recent[1].start, to: recent[0].start, calendar: calendar) <= maxGapDays,
            let space = LoadSpace(exercise: exercise, bodyMass: nil), let grid = space.grid, !grid.isSinglePoint
        else { return nil }
        var loads: [Double] = []
        var unrated = false
        for session in recent {
            let working = session.sets.filter { $0.kind == .normal || $0.kind == .failure }
            let values = working.map { space.value(ofSetting: exercise.setting(of: $0)) }
            guard let heaviest = values.max() else { return nil }
            let atLoad = zip(working, values).filter { abs($0.1 - heaviest) < 1e-6 }.map(\.0)
            guard atLoad.count >= planned else { return nil }
            for set in atLoad {
                guard set.stopReason?.limitsEvidence != true, sameTarget(set.target, plan),
                    let reps = set.effectiveReps, reps >= top
                else { return nil }
                if let planned = plan.rpe {
                    if let rated = set.rpe {
                        guard rated <= planned + 1e-9 else { return nil }
                    } else if set.reachedFailure {
                        guard planned >= 10 - 1e-9 else { return nil }
                    } else {
                        unrated = true
                    }
                }
            }
            loads.append(heaviest)
        }
        guard abs(loads[0] - loads[1]) < 1e-6, let up = grid.next(loads[0]) else { return nil }
        let text = SuggestText(unit: exercise.unit, mode: exercise.mode)
        let load = space.setting(ofValue: loads[0])
        let next = space.setting(ofValue: up)
        guard let loadText = text.load(load), let nextText = text.load(next) else { return nil }
        return ProgressionCheck(
            load: load, next: next, loadText: loadText, nextText: nextText, topReps: top, sets: planned,
            plannedLeft: plan.rpe.map { 10 - $0 }, unrated: unrated)
    }

    /// The same prescription: the rep range and the effort aimed at.
    static func sameTarget(_ a: Target03, _ b: Target03) -> Bool {
        a.repsMin == b.repsMin && a.repsMax == b.repsMax && a.openTop == b.openTop
            && (a.rpe ?? -1).isClose(to: b.rpe ?? -1)
    }
}

private extension Double {
    func isClose(to other: Double) -> Bool { abs(self - other) < 1e-9 }
}
