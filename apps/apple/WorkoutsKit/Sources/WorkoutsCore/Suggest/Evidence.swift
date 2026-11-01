import Foundation

/// A done set's effective RPE (03 §10.2). `logged` is true only when the set
/// carries a rating; the explanation shows "at RPE x" only then, and
/// "assuming RPE x" for a set whose pre-filled or copied value was set aside.
struct EffectiveRPE: Sendable, Hashable {
    var value: Double
    var logged: Bool
}

extension SuggestKind {
    /// Working sets are the evidence and the ordinals (03 §10.7): never warm-ups,
    /// drops or mini-sets.
    var isWorking: Bool {
        switch self {
        case .normal, .failure, .myoActivation, .timed: true
        case .warmup, .drop, .myoMini: false
        }
    }
}

extension DoneSet {
    /// 03 §10.3: the reps, or the weaker side's (the side present when only one is).
    var effectiveReps: Int? {
        switch (repsLeft, repsRight) {
        case let (left?, right?): min(left, right)
        case let (left?, nil): left
        case let (nil, right?): right
        case (nil, nil): reps
        }
    }

    /// 03 §10.2.
    func effectiveRPE(defaultRPE: Double) -> EffectiveRPE {
        if let rpe { return EffectiveRPE(value: min(max(rpe, 6), 10), logged: true) }
        if kind == .failure || reachedFailure { return EffectiveRPE(value: 10, logged: false) }
        if let repsMin = target.repsMin, let reps = effectiveReps, reps < repsMin {
            return EffectiveRPE(value: 10, logged: false) // it fell short; assume failure
        }
        return EffectiveRPE(value: target.rpe ?? defaultRPE, logged: false)
    }
}

extension ExerciseConfig {
    /// The set's load as logged, in this exercise's unit: the load, the added load
    /// (nil reads as 0) or the assistance (nil reads as 0); nil when the mode has no load.
    func setting(of set: DoneSet) -> Double? {
        switch mode {
        case .none: nil
        case .external: set.load.map { SuggestMath.convert($0, from: set.unit, to: unit) }
        case .bodyweightAdded, .bodyweightAssisted: SuggestMath.convert(set.load ?? 0, from: set.unit, to: unit)
        }
    }

    /// A body mass in kilograms, in this exercise's unit.
    func bodyMassInUnit(_ kilograms: Double?) -> Double? {
        kilograms.map { SuggestMath.convert($0, from: .kg, to: unit) }
    }

    /// 03 §10.3: reps (or a duration for a timed set) of at least 1, and in the
    /// external mode a load above 0.
    func isUsable(_ set: DoneSet) -> Bool {
        if set.kind == .timed {
            guard (set.duration ?? 0) >= 1 else { return false }
        } else {
            guard (set.effectiveReps ?? 0) >= 1 else { return false }
        }
        if mode == .external { return (setting(of: set) ?? 0) > 0 }
        return true
    }

    /// The e1RM of a usable, non-timed set with an effective load and `n ≤ 30`
    /// (03 §10.6). `bodyMass` is in the exercise unit.
    func oneRepMax(of set: DoneSet, bodyMass: Double?) -> OneRepMaxEstimate? {
        guard set.kind != .timed, isUsable(set), let reps = set.effectiveReps,
              let space = LoadSpace(exercise: self, bodyMass: bodyMass), space.isEffective
        else { return nil }
        let load = space.value(ofSetting: setting(of: set))
        let rpe = set.effectiveRPE(defaultRPE: defaultRPE)
        let n = Double(reps) + 10 - rpe.value
        guard load > 0, n <= 30 else { return nil }
        return OneRepMaxEstimate(value: load / RPECurve.p(n), effort: n, effectiveLoad: load,
                                 effectiveRPE: rpe.value, rpeLogged: rpe.logged)
    }
}

/// The evidence 03 §10.7 selects, over one exercise's history (newest first).
struct SuggestEvidence: Sendable {
    /// The reference set from history (03 §10.7) and its gap in days.
    struct Reference: Sendable {
        var set: DoneSet
        var session: PastSession
        var days: Int
    }

    var exercise: ExerciseConfig
    var history: [PastSession]
    /// Today's body mass in the exercise unit.
    var bodyMass: Double?
    var now: Date
    var calendar: Calendar

    /// The working ordinal of each of a session's sets of one kind class, from the
    /// sets' own `ordinal` where present, else counted by position.
    static func ordinals(of sets: [DoneSet], warmups: Bool) -> [(set: DoneSet, ordinal: Int)] {
        let chosen = sets.enumerated()
            .filter { warmups ? $0.element.kind == .warmup : $0.element.kind.isWorking }
            .sorted { ($0.element.position, $0.offset) < ($1.element.position, $1.offset) }
        return chosen.enumerated().map { index, item in (item.element, item.element.ordinal ?? index + 1) }
    }

    /// History newest first, by `start`; ties keep their given order.
    static func newestFirst(_ history: [PastSession]) -> [PastSession] {
        history.enumerated()
            .sorted { ($1.element.start, $0.offset) < ($0.element.start, $1.offset) }
            .map(\.element)
    }

    /// Today's evidence `W`: the most recently completed usable working set,
    /// timed for a timed pending set and the reverse, with its working ordinal.
    func latest(in today: [DoneSet], timed: Bool) -> (set: DoneSet, ordinal: Int)? {
        SuggestEvidence.ordinals(of: today, warmups: false)
            .filter { ($0.set.kind == .timed) == timed && exercise.isUsable($0.set) }
            .max { ($0.set.completedAt, $0.set.position) < ($1.set.completedAt, $1.set.position) }
    }

    /// `Pref` for working ordinal `k`: in the newest session with a usable,
    /// plausible working set, its `k`-th one, else the nearest earlier, else its last.
    func reference(ordinal k: Int, timed: Bool) -> Reference? {
        let plausibility = SuggestPlausibility(evidence: self)
        for (index, session) in history.enumerated() {
            let candidates = SuggestEvidence.ordinals(of: session.sets, warmups: false).filter {
                ($0.set.kind == .timed) == timed && exercise.isUsable($0.set)
                    && plausibility.accepts($0.set, inSessionAt: index)
            }
            guard !candidates.isEmpty else { continue }
            let chosen = candidates.first { $0.ordinal == k }
                ?? candidates.filter { $0.ordinal < k }.max { $0.ordinal < $1.ordinal }
                ?? candidates.max { $0.ordinal < $1.ordinal }!
            return Reference(set: chosen.set, session: session,
                             days: SuggestMath.days(from: session.start, to: now, calendar: calendar))
        }
        return nil
    }

    /// A past session's body mass in the exercise unit, else today's (03 §10.4).
    func bodyMass(of session: PastSession) -> Double? {
        exercise.bodyMassInUnit(session.bodyMass) ?? bodyMass
    }
}
