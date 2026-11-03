import Foundation

/// The entry check's dialog (§5.7, req 30): a working set far from the recent
/// ones asks before it is committed.
public struct EntryWarning: Equatable, Sendable {
    /// `500 kg × 8 is far from your recent sets`.
    public var title: String
    /// `Recently about 35 kg × 12.`
    public var message: String
    /// A slipped decimal point, when dividing or multiplying the load by 10
    /// lands inside the range: the first button offers it.
    public var correctedLoad: Mass?
    /// `Use 50 kg`.
    public var useTitle: String?
    /// `Keep 500 kg`.
    public var keepTitle: String
    /// `Edit`.
    public var editTitle: String

    public init(title: String, message: String, correctedLoad: Mass?, useTitle: String?, keepTitle: String,
                editTitle: String = "Edit")
    {
        self.title = title
        self.message = message
        self.correctedLoad = correctedLoad
        self.useTitle = useTitle
        self.keepTitle = keepTitle
        self.editTitle = editTitle
    }

    /// The values with the corrected load in place of the typed one.
    public func corrected(_ values: SetValues, measurement: MeasurementType) -> SetValues {
        guard let correctedLoad else { return values }
        var fixed = values
        if measurement.loadMode == .assisted { fixed.assistance = correctedLoad } else { fixed.load = correctedLoad }
        return fixed
    }
}

/// 03 §10.7's plausibility test on a set about to be committed (§5.7): its
/// e1RM against the median session-best e1RM of the five newest other
/// sessions within 180 days, when there are at least three.
public enum EntryCheck {
    /// The dialog for committing `values` on `row`, or nil when the set may
    /// go: it is not a working set with an effective load, there is too
    /// little history, or it lies within 0.5 to 1.5 times the median.
    public static func warning(for row: RowID, values: SetValues, in state: SessionState, history: HistoryIndex, now: Date,
                               calendar: Calendar) -> EntryWarning?
    {
        guard let current = state.rows[row], let head = state.rows[current.headID], current.isHead || current.side != nil,
              head.role == .working, let exercise = state.exercise(ofRow: row)
        else { return nil }
        let config = SuggestionInputs.config(for: exercise, defaultUnit: state.settings.defaultUnit)
        let kind = SuggestionInputs.kind(role: head.role, isSegment: false, side: current.side, structure: head.structure,
                                         measurement: exercise.measurement, reachedFailure: values.reachedFailure == true,
                                         termination: head.termination)
        guard kind.isWorking, kind != .timed else { return nil }
        let bodyMass = state.session.bodyMass.map(SuggestionInputs.kilograms)
        let past = SuggestionInputs.history(of: exercise, config: config, index: history, excluding: state.session.id,
                                            now: now, calendar: calendar)
        func done(_ values: SetValues) -> DoneSet {
            SuggestionInputs.doneSet(kind: kind, values: values, side: current.side, config: config,
                                     target: SuggestionInputs.target(head.target, termination: head.termination, config: config),
                                     completedAt: now, ordinal: nil)
        }
        let check = checkPlausibility(of: done(values), exercise: config, bodyMass: bodyMass, history: past, now: now,
                                      calendar: calendar)
        guard !check.isPlausible, let median = check.median else { return nil }

        let measurement = exercise.measurement
        let typed = measurement.loadMode == .assisted ? values.assistance : values.load
        var corrected: Mass?
        if let typed, typed.value > 0 {
            for factor in [Decimal(string: "0.1")!, Decimal(10)] {
                var candidate = values
                let mass = Mass(value: typed.value * factor, unit: typed.unit)
                if measurement.loadMode == .assisted { candidate.assistance = mass } else { candidate.load = mass }
                if let e1RM = estimateOneRepMax(of: done(candidate), exercise: config, bodyMass: bodyMass)?.value,
                   check.accepts(e1RM)
                {
                    corrected = mass
                    break
                }
            }
        }
        let load = Format.load(values, mode: measurement.loadMode) ?? Format.set(values, measurement: measurement)
        return EntryWarning(
            title: "\(Format.set(values, measurement: measurement)) is far from your recent sets",
            message: recent(past, median: median, config: config, measurement: measurement, bodyMass: bodyMass, now: now,
                            calendar: calendar).map { "Recently about \($0)." } ?? "It is far from your recent sets.",
            correctedLoad: corrected,
            useTitle: corrected.map { "Use \(Format.mass($0))" },
            keepTitle: "Keep \(load)"
        )
    }

    /// The recent session-best set nearest the median, as text: `35 kg × 12`.
    static func recent(_ past: [PastSession], median: Double, config: ExerciseConfig, measurement: MeasurementType,
                       bodyMass: Double?, now: Date, calendar: Calendar) -> String?
    {
        var best: (set: DoneSet, distance: Double)?
        for session in past.prefix(5) {
            let mass = session.bodyMass ?? bodyMass
            let top = session.sets.filter(\.kind.isWorking)
                .compactMap { set in estimateOneRepMax(of: set, exercise: config, bodyMass: mass).map { (set, $0.value) } }
                .max { $0.1 < $1.1 }
            guard let top else { continue }
            let distance = abs(top.1 - median)
            if best.map({ distance < $0.distance }) ?? true { best = (top.0, distance) }
        }
        guard let set = best?.set else { return nil }
        let mass = set.load.map { Mass(suggested: SuggestMath.convert($0, from: set.unit, to: config.unit), unit: config.unit) }
        let values = SetValues(load: measurement.loadMode == .assisted ? nil : mass,
                               assistance: measurement.loadMode == .assisted ? mass : nil, reps: set.effectiveReps)
        return Format.set(values, measurement: measurement)
    }
}
