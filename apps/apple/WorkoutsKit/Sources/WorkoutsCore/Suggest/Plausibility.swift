import Foundation

/// An estimated one-rep maximum from one set (03 §10.6), without the in-session
/// fatigue allowance. The e1RM trend draws `value`; 03's design implication 12
/// keeps the chart to sets with `effort ≤ 12`.
public struct OneRepMaxEstimate: Sendable, Hashable {
    /// The e1RM, an effective load in the exercise unit.
    public var value: Double
    /// `n = reps + 10 − RPE`.
    public var effort: Double
    /// The load moved: the load, or body mass share plus added load, or minus assistance.
    public var effectiveLoad: Double
    public var effectiveRPE: Double
    public var rpeLogged: Bool
}

/// The e1RM of a usable working set with an effective load, or nil (a timed set,
/// no load, a bodyweight exercise with no body mass, or `n > 30`).
/// - Parameter bodyMass: kilograms, the body mass of the set's session.
public func estimateOneRepMax(of set: DoneSet, exercise: ExerciseConfig, bodyMass: Double?) -> OneRepMaxEstimate? {
    exercise.oneRepMax(of: set, bodyMass: exercise.bodyMassInUnit(bodyMass))
}

/// 03 §10.7's plausibility test of one set against recent sessions, for entry
/// validation (DESIGN §5.7).
public struct PlausibilityCheck: Sendable, Hashable {
    /// The set's e1RM in the exercise unit; nil when it has none.
    public var e1RM: Double?
    /// The median session-best e1RM of the other recent sessions; nil when
    /// fewer than three have one, and then every set is plausible.
    public var median: Double?

    /// True unless the e1RM lies outside 0.5 to 1.5 times the median.
    public var isPlausible: Bool {
        guard let e1RM else { return true }
        return accepts(e1RM)
    }

    /// Whether an e1RM (of an edited load, say) would pass.
    public func accepts(_ value: Double) -> Bool {
        guard let median else { return true }
        return value >= 0.5 * median && value <= 1.5 * median
    }
}

/// Checks a set about to be committed against the five newest sessions within
/// 180 days of `now` (DESIGN §5.7).
/// - Parameters:
///   - bodyMass: today's body mass in kilograms.
///   - history: earlier sessions of the exercise, excluding the active one.
public func checkPlausibility(
    of set: DoneSet,
    exercise: ExerciseConfig,
    bodyMass: Double?,
    history: [PastSession],
    now: Date,
    calendar: Calendar
) -> PlausibilityCheck {
    let evidence = SuggestEvidence(exercise: exercise, history: SuggestEvidence.newestFirst(history),
                            bodyMass: exercise.bodyMassInUnit(bodyMass), now: now, calendar: calendar)
    let plausibility = SuggestPlausibility(evidence: evidence)
    return PlausibilityCheck(e1RM: exercise.oneRepMax(of: set, bodyMass: evidence.bodyMass)?.value,
                             median: plausibility.median(excluding: nil))
}

/// 03 §10.7's plausibility over one history: a working set in session `S` is
/// implausible when its e1RM is above 1.5 or below 0.5 times the median
/// session-best e1RM of the other sessions among the five newest within 180
/// days, when at least three of them have one.
struct SuggestPlausibility: Sendable {
    let evidence: SuggestEvidence
    /// The session-best e1RM of each history session in the pool, by history index.
    private let bests: [(index: Int, best: Double)]

    init(evidence: SuggestEvidence) {
        self.evidence = evidence
        let pool = evidence.history.indices.filter {
            SuggestMath.days(from: evidence.history[$0].start, to: evidence.now, calendar: evidence.calendar) <= 180
        }.prefix(5)
        bests = pool.compactMap { index in
            let session = evidence.history[index]
            let bodyMass = evidence.bodyMass(of: session)
            let best = session.sets.filter(\.kind.isWorking)
                .compactMap { evidence.exercise.oneRepMax(of: $0, bodyMass: bodyMass)?.value }
                .max()
            return best.map { (index, $0) }
        }
    }

    /// The median of the pool's session bests other than session `excluded`'s.
    func median(excluding excluded: Int?) -> Double? {
        let values = bests.filter { $0.index != excluded }.map(\.best).sorted()
        guard values.count >= 3 else { return nil }
        let middle = values.count / 2
        return values.count.isMultiple(of: 2) ? (values[middle - 1] + values[middle]) / 2 : values[middle]
    }

    /// Whether a set of history session `index` may be a reference.
    func accepts(_ set: DoneSet, inSessionAt index: Int) -> Bool {
        let bodyMass = evidence.bodyMass(of: evidence.history[index])
        guard let e1RM = evidence.exercise.oneRepMax(of: set, bodyMass: bodyMass)?.value,
              let median = median(excluding: index)
        else { return true }
        return e1RM >= 0.5 * median && e1RM <= 1.5 * median
    }
}
