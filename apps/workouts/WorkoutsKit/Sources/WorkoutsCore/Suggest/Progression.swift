import Foundation

/// An exercise ready to go up a load (DESIGN §5.3, the progression offer):
/// what its two newest sessions show, and the load one step up.
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
    /// The top of the range every working set reached; nil when their tops differ.
    public var topReps: Int?
    /// The fewest reps left the plan asked for.
    public var plannedLeft: Double
    /// Some set was logged with the plan's reps left untouched, read as the plan.
    public var assumedEffort: Bool
}

/// The progression offer's rule (DESIGN §5.3). An exercise is ready to go up
/// when in each of its two newest sessions (no deload, the newer within 28
/// days, the older within 28 days of it) every working set at the working
/// load reached the top of its planned rep range with at least the planned
/// reps left, at one load, the same both times, and none ended for technique
/// or pain. A set logged with the plan's reps left untouched counts as at the
/// plan, and says so; one that reached failure has none left. The working
/// load is the heaviest of the session's working sets: a lighter back-off
/// set neither qualifies nor spoils it.
public enum ProgressionOffer {
    public static let sessions = 2
    public static let maxGapDays = 28

    /// - Parameter history: the exercise's sessions, newest first, deloads
    ///   left out (`SuggestionInputs.history`).
    public static func check(
        history: [PastSession], exercise: ExerciseConfig, now: Date, calendar: Calendar
    ) -> ProgressionCheck? {
        let recent = Array(history.prefix(sessions))
        guard recent.count == sessions,
            SuggestMath.days(from: recent[0].start, to: now, calendar: calendar) <= maxGapDays,
            SuggestMath.days(from: recent[1].start, to: recent[0].start, calendar: calendar) <= maxGapDays,
            let space = LoadSpace(exercise: exercise, bodyMass: nil), let grid = space.grid, !grid.isSinglePoint
        else { return nil }
        var loads: [Double] = []
        var tops: Set<Int> = []
        var lefts: [Double] = []
        var assumed = false
        for session in recent {
            let working = session.sets.filter { $0.kind == .normal || $0.kind == .failure }
            let values = working.map { space.value(ofSetting: exercise.setting(of: $0)) }
            guard let heaviest = values.max() else { return nil }
            for (set, value) in zip(working, values) where abs(value - heaviest) < 1e-6 {
                guard set.stopReason != .technique, set.stopReason != .pain,
                    let top = set.target.repsMax, !set.target.openTop,
                    let reps = set.effectiveReps, reps >= top
                else { return nil }
                let planned = set.target.rpe ?? exercise.defaultRPE
                if let rated = set.rpe {
                    guard rated <= planned + 1e-9 else { return nil }
                } else if set.reachedFailure {
                    guard planned >= 10 - 1e-9 else { return nil }
                } else {
                    assumed = true
                }
                tops.insert(top)
                lefts.append(10 - planned)
            }
            loads.append(heaviest)
        }
        guard abs(loads[0] - loads[1]) < 1e-6, let up = grid.next(loads[0]) else { return nil }
        let text = SuggestText(unit: exercise.unit, mode: exercise.mode)
        let load = space.setting(ofValue: loads[0])
        let next = space.setting(ofValue: up)
        guard let loadText = text.load(load), let nextText = text.load(next) else { return nil }
        return ProgressionCheck(
            load: load, next: next, loadText: loadText, nextText: nextText,
            topReps: tops.count == 1 ? tops.first : nil, plannedLeft: lefts.min() ?? 0, assumedEffort: assumed)
    }
}
