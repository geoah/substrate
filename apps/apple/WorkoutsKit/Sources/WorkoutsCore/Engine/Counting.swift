import Foundation

/// What the header, the summaries and the session detail count (§4.11).
/// Each count reads either the session in progress or a finished session's records.
public enum Counting {
    // MARK: - Sets

    /// Completed working sets: a chain and a side pair count once; warm-ups never.
    public static func workingSetsDone(_ state: SessionState) -> Int {
        state.rows.values.count { $0.isHead && $0.role == .working && $0.isCompleted }
    }

    /// Working sets in the session, done or not: the "of 40" in "3 of 40".
    public static func workingSetsPlanned(_ state: SessionState) -> Int {
        state.rows.values.count { $0.isHead && $0.role == .working }
    }

    /// Working sets among a finished session's records.
    public static func workingSets(_ sets: [PerformedSet]) -> Int {
        sets.count { $0.role == .working && $0.continues == nil }
    }

    // MARK: - Volume

    /// The volume load of the session so far, in the default unit, rounded
    /// to a whole unit.
    public static func volume(_ state: SessionState) -> Mass {
        volume(SessionEngine.setRecords(state), exercises: state.exercises, bodyMass: state.session.bodyMass,
               unit: state.settings.defaultUnit, includeWarmups: state.settings.countWarmupsInVolume)
    }

    /// The volume load of `sets`: for each set with reps and an effective
    /// load (03 §10.4, with `bodyMass`), reps × load, times the implements
    /// for a per-implement load, times 2 for a per-side load on a two-sided
    /// exercise. No-load sets add nothing, nor do bodyweight sets without a
    /// body mass. Rounded to a whole `unit`. What a load counts is the set's
    /// own copy (`loadBasis`, `implements`, `laterality`, `bodyShare`), so an
    /// exercise edited later reinterprets nothing; a set written before those
    /// fields existed reads the exercise's.
    public static func volume(_ sets: [PerformedSet], exercises: [ExerciseID: Exercise], bodyMass: Mass?, unit: MassUnit,
                              includeWarmups: Bool) -> Mass
    {
        var total = Decimal(0)
        for set in sets where includeWarmups || set.role == .working {
            let exercise = exercises[set.exercise]
            guard let reps = set.reps, exercise != nil || set.loadMode != nil,
                  let load = effectiveLoad(set, exercise: exercise, bodyMass: bodyMass, unit: unit)
            else { continue }
            var multiplier = Decimal(1)
            switch set.resolvedLoadBasis(exercise) {
            case .total: break
            case .perimplement: multiplier = Decimal(set.resolvedImplements(exercise))
            case .perside: multiplier = set.resolvedLaterality(exercise) == .unilateral ? 1 : 2
            }
            total += Decimal(reps) * load * multiplier
        }
        var rounded = Decimal()
        NSDecimalRound(&rounded, &total, 0, .plain)
        return Mass(value: rounded, unit: unit)
    }

    /// A set's effective load in `unit` (03 §10.4), or nil when it has none.
    /// The share of body mass is the set's own copy, else the exercise's.
    public static func effectiveLoad(_ set: PerformedSet, exercise: Exercise?, bodyMass: Mass?, unit: MassUnit) -> Decimal? {
        func value(_ mass: Mass?) -> Decimal? { mass.map { $0.converted(to: unit).value } }
        let body = value(bodyMass).map { $0 * set.resolvedBodyShare(exercise) }
        switch set.loadMode ?? exercise?.measurement.loadMode {
        case .external: return value(set.load)
        case .bodyweight: return body
        case .bodyweightplus: return body.map { $0 + (value(set.load) ?? 0) }
        case .assisted: return body.map { max(0, $0 - (value(set.assistance) ?? 0)) }
        case nil: return nil
        }
    }

    // MARK: - Muscles

    /// Working sets per muscle: 1 per primary muscle and 0.5 per secondary,
    /// per counted set (02 §6.2). A derived view, never stored.
    public static func setsPerMuscle(_ sets: [PerformedSet], exercises: [ExerciseID: Exercise]) -> [Muscle: Decimal] {
        var result: [Muscle: Decimal] = [:]
        for set in sets where set.role == .working && set.continues == nil {
            guard let exercise = exercises[set.exercise] else { continue }
            for muscle in exercise.primaryMuscles {
                result[muscle, default: 0] += 1
            }
            for muscle in exercise.secondaryMuscles {
                result[muscle, default: 0] += Decimal(string: "0.5")!
            }
        }
        return result
    }

    public static func setsPerMuscle(_ state: SessionState) -> [Muscle: Decimal] {
        setsPerMuscle(SessionEngine.setRecords(state), exercises: state.exercises)
    }
}
