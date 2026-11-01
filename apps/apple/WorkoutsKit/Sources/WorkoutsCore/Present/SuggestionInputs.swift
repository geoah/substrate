import Foundation

/// Builds the suggestion engine's arguments from the session state, the
/// exercise and the history index (§5.2), and runs it for a row. The engine
/// itself (`suggest`, Suggest/) knows nothing of records or rows.
public enum SuggestionInputs {
    /// History reaches back this many days (03 DI 7).
    public static let historyDays = 180
    /// And holds at most this many sessions (03 DI 7).
    public static let historySessions = 6

    // MARK: - The whole path

    /// The suggestion for a row of the session, as the presenter shows it:
    /// nil for a completed row, or when no rule applies (a warm-up with no
    /// warm-up last time).
    /// - Parameters:
    ///   - history: every finished session; the active one is left out.
    ///   - now: the instant the gap to each past session is counted to.
    ///   - calendar: the owner's calendar, which counts whole local days.
    public static func suggestion(for row: RowID, in state: SessionState, history: HistoryIndex, now: Date,
                                  calendar: Calendar) -> Suggestion?
    {
        SessionEvaluator(state: state, history: history, now: now, calendar: calendar).suggestion(row)
    }

    /// Whether ticking `row` with `values` leaves no lighter load for the drop
    /// after it (03 §11.7), which the controller passes as
    /// `CompleteSet.noLighterLoad` so the drops left are removed.
    public static func noLighterLoad(afterTicking row: RowID, with values: SetValues, in state: SessionState,
                                     history: HistoryIndex, now: Date, calendar: Calendar) -> Bool
    {
        guard let current = state.rows[row], current.isPending,
              let next = state.chain(of: row).first(where: { $0.chainPosition == current.chainPosition + 1 }),
              next.structure == .drop, next.side == nil
        else { return false }
        var ticked = state
        ticked.rows[row]?.performed = Performed(recordID: SetID("pending"), completedAt: now, values: values)
        let suggestion = SessionEvaluator(state: ticked, history: history, now: now, calendar: calendar).suggestion(next.id)
        return suggestion?.reason == .dropAtMinimum
    }

    // MARK: - Exercise configuration

    /// 03 §9.1's settings for an exercise (§5.2): its unit, how its load
    /// reads, its load grid (its own, else the defaults by equipment or
    /// measurement), and its suggestion settings with 03's defaults.
    public static func config(for exercise: Exercise, defaultUnit: MassUnit) -> ExerciseConfig {
        let unit = exercise.unit(default: defaultUnit)
        let mode: LoadMode03
        var canAddLoad = true
        switch exercise.measurement {
        case .weightreps, .weightduration, .shortdistanceweight: mode = .external
        case .weightedbodyweightreps: mode = .bodyweightAdded
        case .bodyweightreps:
            mode = .bodyweightAdded
            canAddLoad = false
        case .bodyweightassistedreps: mode = .bodyweightAssisted
        case .repsonly, .duration, .distanceduration: mode = .none
        }
        var base: Double?
        var step: Double?
        if mode != .none {
            let defaults = defaultGrid(for: exercise, unit: unit)
            step = exercise.loadStep.map { convert($0, to: unit) } ?? defaults?.step
            if step != nil { base = exercise.loadBase.map { convert($0, to: unit) } ?? defaults?.base ?? 0 }
        }
        return ExerciseConfig(
            unit: unit,
            mode: mode,
            bodyShare: exercise.resolvedBodyShare.double,
            base: base,
            step: step,
            maxLoad: mode == .none ? nil : exercise.loadMax.map { convert($0, to: unit) },
            canAddLoad: canAddLoad,
            defaultRepsMin: exercise.defaultRepsMin ?? 8,
            defaultRepsMax: exercise.defaultRepsMax ?? 12,
            defaultRPE: exercise.defaultRPE?.double ?? 8,
            repGain: exercise.repGain ?? 1,
            dropPercent: exercise.resolvedDropPercent.double / 100,
            timeStep: exercise.resolvedTimeStep,
            myoMaxMiniSets: exercise.resolvedMyoMaxMiniSets,
            myoStopRule: exercise.resolvedMyoStopRule
        )
    }

    /// §5.2's default load grid in `unit`: by measurement for added load and
    /// assistance, else by equipment; nil for equipment with no grid.
    public static func defaultGrid(for exercise: Exercise, unit: MassUnit) -> (base: Double, step: Double)? {
        let kg = unit == .kg
        switch exercise.measurement {
        case .weightedbodyweightreps: return (0, kg ? 2.5 : 5)
        case .bodyweightassistedreps: return (0, kg ? 5 : 10)
        default: break
        }
        switch exercise.equipment {
        case .barbell: return kg ? (20, 2.5) : (45, 5)
        case .dumbbell: return (0, kg ? 2 : 5)
        case .machine, .cable: return (0, kg ? 5 : 10)
        case .kettlebell: return (0, kg ? 4 : 5)
        case .plateloaded: return (0, kg ? 2.5 : 5)
        case Equipment.none?, .plate, .resistanceband, .suspension, .other, nil: return nil
        }
    }

    // MARK: - Kinds and targets

    /// 03's set kind (§5.2, the package README's "Reading"): `warmup` from
    /// the role; a drop chain's segment is a `drop`; a myo, myo-match,
    /// rest-pause or cluster segment a `myoMini`; a myo or myo-match head a
    /// `myoActivation`; a timed exercise's set `timed`; `failure` when it
    /// reached failure or its termination is failure or AMRAP; else `normal`.
    /// A side chain's records are one set of the head's kind.
    public static func kind(role: Role, isSegment: Bool, side: Side?, structure: Structure, measurement: MeasurementType,
                            reachedFailure: Bool, termination: Termination?) -> SuggestKind
    {
        if role == .warmup { return .warmup }
        if isSegment, side == nil {
            switch structure {
            case .drop: return .drop
            case .myo, .myomatch, .restpause, .cluster: return .myoMini
            case .straight: break
            }
        }
        if side == nil, structure == .myo || structure == .myomatch { return .myoActivation }
        if measurement.isTimedHold { return .timed }
        if reachedFailure || termination == .failure || termination == .amrap { return .failure }
        return .normal
    }

    /// 03's target from a copied target: the top of its effort range,
    /// `rpeMax`, else `10 − rirMin` (`SetTarget.effortTop`); the load (or
    /// the assistance) in the exercise's unit; a percentage as a fraction;
    /// an open top for AMRAP and failure sets with no `repsMax` (03 §13).
    /// A row with no target (added in the gym) still gets the open top when
    /// its kind was switched to failure or AMRAP.
    public static func target(_ target: SetTarget?, termination: Termination?, config: ExerciseConfig) -> Target03 {
        guard let target else { return Target03(openTop: termination == .amrap || termination == .failure) }
        let load: Mass? = switch config.mode {
        case .external, .bodyweightAdded: target.load
        case .bodyweightAssisted: target.assistance
        case .none: nil
        }
        let term = termination ?? target.termination
        return Target03(
            repsMin: target.repsMin,
            repsMax: target.repsMax,
            rpe: target.effortTop?.double,
            load: load.map { convert($0, to: config.unit) },
            durationMin: target.durationMin,
            durationMax: target.durationMax,
            dropPercent: target.dropPercent.map { $0.double / 100 },
            dropStep: target.dropStep.map { convert($0, to: config.unit) },
            myoMiniReps: target.miniSetReps,
            myoTotalReps: target.totalReps,
            openTop: (term == .amrap || term == .failure) && target.repsMax == nil
        )
    }

    /// A cluster set's head is its first cluster (§4.9): with no rep range
    /// of its own it aims at the clusters' `miniSetReps`, not the exercise's
    /// default range, so its load holds for the clusters after it.
    static func clusterHead(_ target: Target03, structure: Structure, isHead: Bool) -> Target03 {
        guard isHead, structure == .cluster, target.repsMin == nil, target.repsMax == nil,
              let reps = target.myoMiniReps
        else { return target }
        var target = target
        target.repsMin = reps
        target.repsMax = reps
        return target
    }

    // MARK: - History

    /// The suggestion's history for an exercise (§5.2): the history index's
    /// sessions with sets of it on no setup (V1), newest first, without the
    /// active session and deload sessions, the six newest within 180 days
    /// of `now`.
    public static func history(of exercise: Exercise, config: ExerciseConfig, index: HistoryIndex,
                               excluding active: SessionID?, now: Date, calendar: Calendar) -> [PastSession]
    {
        index.history(of: exercise.id, setup: nil, excluding: active)
            .lazy
            .filter { $0.session.deload != true }
            .filter { SuggestMath.days(from: $0.session.at, to: now, calendar: calendar) <= historyDays }
            .prefix(historySessions)
            .map { entry in
                PastSession(start: entry.session.at, bodyMass: entry.session.bodyMass.map(kilograms),
                            sets: doneSets(entry.sets, exercise: exercise, config: config))
            }
    }

    /// A finished session's sets of one exercise as 03's done sets, in order:
    /// a side pair folds into one set with `repsLeft` and `repsRight`, the
    /// higher rated RPE and the later side's time; drop and mini-set records
    /// stay separate sets of their kinds (§5.2). Only a rating counts as RPE
    /// (`PerformedSet.ratedRPE`): a pre-filled or copied value reads as none.
    public static func doneSets(_ sets: [PerformedSet], exercise: Exercise, config: ExerciseConfig) -> [DoneSet] {
        let byID = Dictionary(sets.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let ordinals = SetLabel.ordinals(records: sets)
        var result: [DoneSet] = []
        var pairs: [SetID: Int] = [:]
        for set in sets {
            let headID = set.continues ?? set.id
            let head = byID[headID] ?? set
            // A rating reads as one; a pre-filled or copied value keeps the
            // method of the field it came from, so it stays no rating.
            let rated = set.ratedRPE
            let unrated = set.unratedRPE
            let values = SetValues(load: set.load, assistance: set.assistance, reps: set.reps, duration: set.duration,
                                   distance: set.distance, rpe: rated ?? unrated?.value,
                                   rpeMethod: rated == nil ? unrated?.method : .entered, reachedFailure: set.reachedFailure)
            let kind = kind(role: head.role, isSegment: set.continues != nil, side: set.side, structure: head.structure,
                            measurement: exercise.measurement, reachedFailure: set.reachedFailure == true,
                            termination: head.target?.termination)
            let copied = target(set.target ?? head.target, termination: head.target?.termination, config: config)
            let done = doneSet(kind: kind, values: values, side: set.side, config: config,
                               target: clusterHead(copied, structure: head.structure, isHead: set.continues == nil),
                               completedAt: set.at, ordinal: ordinals[set.id])
            if set.side != nil, let index = pairs[headID] {
                result[index] = merged(result[index], with: done)
            } else {
                if set.side != nil { pairs[headID] = result.count }
                result.append(done)
            }
        }
        for index in result.indices {
            result[index].position = index + 1
        }
        return result
    }

    // MARK: - Done sets

    /// One done set from a set's values. A side's reps go to `repsLeft` or
    /// `repsRight`, so a lone side reads as 03 §13's "one side logged". Its
    /// RPE is the rated one; a pre-filled or copied value is kept apart
    /// (`unratedRPE`) and read as none.
    static func doneSet(kind: SuggestKind, values: SetValues, side: Side?, config: ExerciseConfig, target: Target03,
                        completedAt: Date, ordinal: Int?) -> DoneSet
    {
        let load: Mass? = switch config.mode {
        case .external, .bodyweightAdded: values.load
        case .bodyweightAssisted: values.assistance
        case .none: nil
        }
        return DoneSet(
            kind: kind,
            load: load?.value.double,
            unit: load?.unit ?? config.unit,
            reps: side == nil ? values.reps : nil,
            repsLeft: side == .left ? values.reps : nil,
            repsRight: side == .right ? values.reps : nil,
            duration: values.duration,
            rpe: values.ratedRPE?.double,
            unratedRPE: values.ratedRPE == nil ? values.rpe?.double : nil,
            reachedFailure: values.reachedFailure == true,
            target: target,
            completedAt: completedAt,
            position: 0,
            ordinal: ordinal
        )
    }

    /// A side pair as one set: both sides' reps, the higher rated RPE, the
    /// shorter hold (the weaker side, as with reps) and the later side's time.
    static func merged(_ first: DoneSet, with second: DoneSet) -> DoneSet {
        var pair = first
        pair.repsLeft = first.repsLeft ?? second.repsLeft
        pair.repsRight = first.repsRight ?? second.repsRight
        pair.rpe = [first.rpe, second.rpe].compactMap(\.self).max()
        pair.unratedRPE = pair.rpe == nil ? [first.unratedRPE, second.unratedRPE].compactMap(\.self).max() : nil
        pair.duration = [first.duration, second.duration].compactMap(\.self).min()
        pair.reachedFailure = first.reachedFailure || second.reachedFailure
        pair.completedAt = max(first.completedAt, second.completedAt)
        if pair.load == nil {
            pair.load = second.load
            pair.unit = second.unit
        }
        return pair
    }

    // MARK: - Units

    /// A mass in `unit` as the engine reads it: converted, then rounded to
    /// 0.01 (03 §13).
    static func convert(_ mass: Mass, to unit: MassUnit) -> Double {
        SuggestMath.convert(mass.value.double, from: mass.unit, to: unit)
    }

    /// A body mass in kilograms, which `suggest` takes.
    static func kilograms(_ mass: Mass) -> Double {
        mass.converted(to: .kg).value.double
    }
}

extension Decimal {
    /// The value as a Double, for the suggestion engine's arithmetic.
    var double: Double { NSDecimalNumber(decimal: self).doubleValue }
}
