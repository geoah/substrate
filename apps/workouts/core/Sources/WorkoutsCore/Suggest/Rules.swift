import Foundation

/// Today's rep range for the pending set (03 §10.8). `hi` is nil when the top
/// is open (an AMRAP or failure set with no `repsMax`, 03 §13).
struct RepRange: Sendable, Hashable {
    var lo: Int
    var hi: Int?

    init(target: Target03, exercise: ExerciseConfig) {
        let lo = target.repsMin ?? exercise.defaultRepsMin
        let hi = target.openTop ? nil : target.repsMax ?? exercise.defaultRepsMax
        if let hi, lo > hi { (self.lo, self.hi) = (hi, lo) } else { (self.lo, self.hi) = (lo, hi) }
    }

    func contains(_ reps: Int) -> Bool { reps >= lo && !isAboveTop(reps) }
    func isAboveTop(_ reps: Int) -> Bool { hi.map { reps > $0 } ?? false }
    func capped(_ reps: Int) -> Int { hi.map { min(reps, $0) } ?? reps }
    /// `max(lo, min(reps, hi))`.
    func clamped(_ reps: Int) -> Int { capped(max(reps, lo)) }
}

/// One call of `suggest`: the inputs brought into the exercise unit, and the
/// rules of 03 §11 and DESIGN §5.3 over them.
struct SuggestRules: Sendable {
    let pending: PendingSet
    let exercise: ExerciseConfig
    let today: [DoneSet]
    let evidence: SuggestEvidence
    let text: SuggestText
    let range: RepRange
    /// The target RPE `R`: 10 for a failure set (03 §10.8).
    let rpe: Double

    init(pending: PendingSet, exercise: ExerciseConfig, today: [DoneSet], history: [PastSession],
         bodyMass: Double?, now: Date, calendar: Calendar)
    {
        self.pending = pending
        self.exercise = exercise
        self.today = today
        evidence = SuggestEvidence(exercise: exercise, history: SuggestEvidence.newestFirst(history),
                            bodyMass: exercise.bodyMassInUnit(bodyMass), now: now, calendar: calendar)
        text = SuggestText(unit: exercise.unit, mode: exercise.mode)
        range = RepRange(target: pending.target, exercise: exercise)
        rpe = pending.kind == .failure ? 10 : pending.target.rpe ?? exercise.defaultRPE
    }

    var target: Target03 { pending.target }

    func run() -> Suggestion? {
        switch pending.kind {
        case .warmup: warmup()
        case .drop: drop()
        case .myoMini: miniSet()
        case .timed: timed()
        case .normal, .failure, .myoActivation: working()
        }
    }

    // MARK: Working sets

    /// A working set: the app's back-off rule, else 11.2, 11.3 or 11.4 (03 §11).
    func working() -> Suggestion {
        if let backoff = backoff() { return backoff }
        if let latest = evidence.latest(in: today, timed: false) {
            return inSession(latest.set, ordinal: latest.ordinal)
        }
        if let reference = evidence.reference(ordinal: pending.ordinal, timed: false) {
            return fromHistory(reference)
        }
        return coldStart()
    }

    /// 03 §11.2: from the set just done today.
    func inSession(_ done: DoneSet, ordinal k: Int) -> Suggestion {
        let effort = done.effectiveRPE(defaultRPE: exercise.defaultRPE)
        let reps = done.effectiveReps ?? 0
        let n = Double(reps) + 10 - effort.value
        let fatigue = effort.value >= 10 ? 1.0 : effort.value >= 9 ? 0.5 : 0
        let setting = exercise.setting(of: done)
        let prev = text.previous(done, setting: setting, rpe: effort)
        guard let space = LoadSpace(exercise: exercise, bodyMass: evidence.bodyMass), space.isEffective,
              case let lw = space.value(ofSetting: setting), lw > 0, n <= 30
        else {
            // 03 §11.5: no effective load, or too far from failure to estimate.
            let r0 = Int((Double(reps) + rpe - effort.value - fatigue + 1e-6).rounded(.down))
            return repsOnly(setting: setting, predicted: r0, inSession: true) { reason, new in
                sessionText(reason, k: k, prev: prev, new: new)
            }
        }
        let grid = space.grid(around: lw)
        let e1RM = lw / RPECurve.p(max(1, n - fatigue))
        let r0 = RPECurve.repsAt(lw, e1RM: e1RM, rpe: rpe)
        let (load, newReps, reason): (Double, Int, Reason)
        if grid.isSinglePoint {
            (load, newReps, reason) = (lw, max(1, r0), .sessionKeep)
        } else if range.contains(r0) {
            (load, newReps, reason) = (lw, r0, .sessionKeep)
        } else if range.isAboveTop(r0), let hi = range.hi {
            if let stepUp = grid.next(lw) {
                var up = grid.next(grid.floor(e1RM * RPECurve.p(Double(hi + 1) + 10 - rpe))) ?? stepUp
                up = max(up, stepUp)
                // One suggestion never raises the load by more than one step or 10 %.
                up = min(up, max(stepUp, grid.floor(1.10 * lw)))
                let r = min(RPECurve.repsAt(up, e1RM: e1RM, rpe: rpe), hi)
                (load, newReps, reason) = r < range.lo ? (lw, hi, .sessionCoarse) : (up, r, .sessionUp)
            } else {
                (load, newReps, reason) = (lw, hi, .sessionAtMax)
            }
        } else {
            var down = grid.floor(e1RM * RPECurve.p(Double(range.lo) + 10 - rpe))
            if down >= lw - 1e-9 { down = grid.prev(lw) ?? lw }
            let r = max(1, range.capped(RPECurve.repsAt(down, e1RM: e1RM, rpe: rpe)))
            (load, newReps, reason) = (down, r, .sessionDown)
        }
        let newSetting = space.setting(ofValue: load)
        return Suggestion(load: newSetting, reps: newReps, targetRPE: rpe, reason: reason,
                          text: sessionText(reason, k: k, prev: prev, new: text.set(load: newSetting, reps: newReps)))
    }

    /// 03 §11.3: from the same set last time.
    func fromHistory(_ reference: SuggestEvidence.Reference) -> Suggestion {
        let done = reference.set
        let effort = done.effectiveRPE(defaultRPE: exercise.defaultRPE)
        let reps = done.effectiveReps ?? 0
        let n = Double(reps) + 10 - effort.value
        var gain = exercise.repGain
        var cut = 1.0
        if reference.days > 90 {
            (cut, gain) = (0.90, 0)
        } else if reference.days > 28 {
            (cut, gain) = (0.95, 0)
        }
        let setting = exercise.setting(of: done)
        let prev = text.previous(done, setting: setting, rpe: effort, days: reference.days)
        let thenMass = evidence.bodyMass(of: reference.session)
        // With no body mass today, last time's stands in, so the setting keeps its load.
        let nowMass = evidence.bodyMass ?? thenMass
        guard let then = LoadSpace(exercise: exercise, bodyMass: thenMass), then.isEffective,
              let now = LoadSpace(exercise: exercise, bodyMass: nowMass),
              case let lp = then.value(ofSetting: setting), case let lpNow = now.value(ofSetting: setting),
              lp > 0, lpNow > 0, n <= 30
        else {
            // 03 §11.5.
            let goal = Int((Double(reps) + rpe - effort.value + 1e-6).rounded(.down)) + gain
            return repsOnly(setting: setting, predicted: goal, inSession: false) { reason, new in
                historyText(reason, prev: prev, new: new)
            }
        }
        let grid = now.grid(around: lpNow)
        let e1RM = lp / RPECurve.p(n) * cut
        let (load, newReps, reason): (Double, Int, Reason)
        if reps >= range.lo - 2, range.hi.map({ reps <= $0 + 2 }) ?? true {
            // The same rep scheme.
            let goal = RPECurve.repsAt(lpNow, e1RM: e1RM, rpe: rpe) + gain
            if grid.isSinglePoint {
                (load, newReps, reason) = (lpNow, max(1, goal), .historyAddRep)
            } else if range.contains(goal) {
                (load, newReps, reason) = (lpNow, goal, .historyAddRep)
            } else if range.isAboveTop(goal), let hi = range.hi {
                if let up = grid.next(lpNow) {
                    // Exactly one step; the reps never go below the range, as in double progression.
                    let r = range.clamped(RPECurve.repsAt(up, e1RM: e1RM, rpe: rpe))
                    (load, newReps, reason) = (up, r, .historyLoadUp)
                } else {
                    (load, newReps, reason) = (lpNow, hi, .historyAtMax)
                }
            } else {
                var down = grid.floor(e1RM * RPECurve.p(Double(range.lo) + 10 - rpe))
                if down >= lpNow - 1e-9 { down = grid.prev(lpNow) ?? lpNow }
                let r = max(1, range.capped(RPECurve.repsAt(down, e1RM: e1RM, rpe: rpe)))
                (load, newReps, reason) = (down, r, .historyLoadDown)
            }
        } else {
            // The rep scheme changed: aim at the middle of the new range (its bottom when the top is open).
            let mid = range.hi.map { (range.lo + $0) / 2 } ?? range.lo
            let fresh = grid.floor(e1RM * RPECurve.p(Double(mid) + 10 - rpe))
            let r = max(1, range.capped(RPECurve.repsAt(fresh, e1RM: e1RM, rpe: rpe)))
            (load, newReps, reason) = (fresh, r, .historyNewRange)
        }
        let newSetting = now.setting(ofValue: load)
        return Suggestion(load: newSetting, reps: newReps, targetRPE: rpe, reason: reason,
                          text: historyText(reason, prev: prev, new: text.set(load: newSetting, reps: newReps)))
    }

    /// 03 §11.5: work on the setting as logged, one step at a time. Its reasons
    /// and texts are 11.2's in session and 11.3's from history.
    func repsOnly(setting: Double?, predicted: Int, inSession session: Bool,
                  text explain: (Reason, String) -> String) -> Suggestion
    {
        let space = LoadSpace(exercise: exercise, bodyMass: nil)
        let value = space?.value(ofSetting: setting)
        var (load, reps, reason) = (setting, predicted, session ? Reason.sessionKeep : .historyAddRep)
        if range.isAboveTop(predicted), let space, let value, let heavier = space.grid?.next(value) {
            load = space.setting(ofValue: heavier)
            (reps, reason) = (range.lo, session ? .sessionUp : .historyLoadUp)
        } else if predicted < range.lo, let space, let value, let lighter = space.grid?.prev(value) {
            load = space.setting(ofValue: lighter)
            (reps, reason) = (range.lo, session ? .sessionDown : .historyLoadDown)
        } else {
            reps = max(1, predicted)
        }
        return Suggestion(load: load, reps: reps, targetRPE: rpe, reason: reason,
                          text: explain(reason, text.set(load: load, reps: reps)))
    }

    /// 03 §11.4: no evidence today and no history.
    func coldStart() -> Suggestion {
        let lo = range.lo
        guard exercise.mode != .none else {
            return Suggestion(reps: lo, targetRPE: rpe, reason: .coldStartRoutineLoad,
                              text: "First time; try \(text.set(load: nil, reps: lo))")
        }
        if let planned = target.load, let space = LoadSpace(exercise: exercise, bodyMass: evidence.bodyMass) {
            let value = space.value(ofSetting: planned)
            let load = space.setting(ofValue: space.grid(around: value).floor(value))
            return Suggestion(load: load, reps: lo, targetRPE: rpe, reason: .coldStartRoutineLoad,
                              text: "First time; routine load, try \(text.set(load: load, reps: lo))")
        }
        if exercise.mode == .bodyweightAdded, !exercise.canAddLoad {
            // Plain bodyweight: nothing to pick.
            return Suggestion(load: 0, reps: lo, targetRPE: rpe, reason: .coldStartRoutineLoad,
                              text: "First time; try \(text.set(load: 0, reps: lo))")
        }
        let about = Int((Double(lo) + 10 - rpe + 1e-6).rounded(.down))
        return Suggestion(load: nil, reps: lo, targetRPE: rpe, reason: .coldStartPickLoad,
                          text: "First time: pick a load for about \(about) reps; do \(lo)")
    }

    /// DESIGN §5.3: a back-off set after the working set it backs off from.
    func backoff() -> Suggestion? {
        guard pending.kind == .normal || pending.kind == .failure, let parent = pending.parent,
              target.dropPercent != nil || target.dropStep != nil,
              let space = LoadSpace(exercise: exercise, bodyMass: evidence.bodyMass)
        else { return nil }
        let parentSetting = exercise.setting(of: parent)
        let parentValue = space.value(ofSetting: parentSetting)
        let grid = space.grid(around: parentValue)
        var load = grid.floor(lighter(than: parentValue, in: space, step: target.dropStep,
                                      fraction: target.dropPercent ?? 0))
        if load >= parentValue - 1e-9 { load = grid.prev(parentValue) ?? parentValue }
        // Reps from 11.2's in-session estimate of the parent, kept inside the range.
        var reps = range.lo
        let effort = parent.effectiveRPE(defaultRPE: exercise.defaultRPE)
        let n = Double(parent.effectiveReps ?? 0) + 10 - effort.value
        if space.isEffective, exercise.isUsable(parent), parentValue > 0, n <= 30 {
            let fatigue = effort.value >= 10 ? 1.0 : effort.value >= 9 ? 0.5 : 0
            let e1RM = parentValue / RPECurve.p(max(1, n - fatigue))
            reps = range.clamped(RPECurve.repsAt(load, e1RM: e1RM, rpe: rpe))
        }
        let newSetting = space.setting(ofValue: load)
        let by = target.dropStep.map(text.mass) ?? "\(SuggestText.percent(target.dropPercent ?? 0))%"
        let from = text.load(parentSetting) ?? ""
        return Suggestion(load: newSetting, reps: reps, targetRPE: rpe, reason: .backoff,
                          text: "Back-off \(by) from \(from): try \(text.set(load: newSetting, reps: reps))")
    }

    /// The raw lighter value a drop or back-off aims at, before rounding to the grid.
    private func lighter(than value: Double, in space: LoadSpace, step: Double?, fraction: Double) -> Double {
        if let step { return value - step }
        // An assistance with no body mass has no percentage to take: one step more assistance.
        if !space.isEffective, exercise.mode == .bodyweightAssisted { return value }
        return value * (1 - fraction)
    }

    // MARK: Chains, warm-ups, timed sets

    /// 03 §11.1: last time's warm-up with the same warm-up ordinal, unchanged.
    func warmup() -> Suggestion? {
        guard let newest = evidence.history.first,
              let match = SuggestEvidence.ordinals(of: newest.sets, warmups: true)
              .first(where: { $0.ordinal == pending.ordinal })
        else { return nil }
        let done = match.set
        let setting = exercise.setting(of: done)
        let new = done.duration != nil && done.effectiveReps == nil
            ? text.timed(load: setting, duration: done.duration)
            : text.set(load: setting, reps: done.effectiveReps)
        return Suggestion(load: setting, reps: done.effectiveReps, duration: done.duration, targetRPE: target.rpe,
                          reason: .warmupAsLastTime, text: "Warm-up, as last time: \(new)")
    }

    /// 03 §11.7: a drop after its parent (the working set or the previous drop).
    func drop() -> Suggestion? {
        guard let parent = pending.parent,
              let space = LoadSpace(exercise: exercise, bodyMass: evidence.bodyMass),
              let parentSetting = exercise.setting(of: parent)
        else { return nil }
        // A planned `dropReps` arrives as the drop row's fixed reps; else to failure.
        let reps = target.repsMin != nil && target.repsMin == target.repsMax ? target.repsMin : nil
        guard space.grid != nil else {
            // No grid (DESIGN §5.3): the app cannot know the lighter setting (a
            // band, a suspension trainer), so the owner picks it and the
            // planned drops stay; "no lighter load" is only for a known grid.
            let rest = reps.map { "\($0) reps" } ?? "to failure"
            return Suggestion(reps: reps, targetRPE: target.rpe, reason: .drop,
                              text: "Drop from \(text.load(parentSetting) ?? ""): a lighter load, \(rest)")
        }
        let parentValue = space.value(ofSetting: parentSetting)
        let grid = space.grid(around: parentValue)
        // DESIGN §4.9: the planned step wins over the planned percentage, then the exercise's.
        let fraction = target.dropPercent ?? exercise.dropPercent
        var load = grid.floor(lighter(than: parentValue, in: space, step: target.dropStep, fraction: fraction))
        if load >= parentValue - 1e-9 {
            guard let lower = grid.prev(parentValue) else {
                return Suggestion(targetRPE: target.rpe, stop: true, reason: .dropAtMinimum,
                                  text: "No lighter load for a drop")
            }
            load = lower
        }
        let newSetting = space.setting(ofValue: load)
        let by = target.dropStep.map(text.mass) ?? "\(SuggestText.percent(fraction))%"
        let to = reps == nil ? "\(text.load(newSetting) ?? "") to failure" : text.set(load: newSetting, reps: reps)
        return Suggestion(load: newSetting, reps: reps, targetRPE: target.rpe, reason: .drop,
                          text: "Drop \(by) from \(text.load(parentSetting) ?? ""): \(to)")
    }

    /// 03 §11.6: a myo-rep mini-set; with the stop rule off (`myoStopRule` nil),
    /// a rest-pause or cluster mini-set (DESIGN §5.3).
    func miniSet() -> Suggestion? {
        guard let head = pending.parent else { return nil }
        let done = pending.miniSetsDone
        let j = done.count + 1
        let rule = exercise.myoStopRule
        let headReps = head.effectiveReps ?? 0
        // Halves round up: (rA + 2) / 4 is round(rA / 4) for whole reps.
        let t1 = target.myoMiniReps ?? (rule == nil ? nil : min(max((headReps + 2) / 4, 3), 5))
        let m1 = done.first
        func stop(_ text: String) -> Suggestion {
            Suggestion(targetRPE: target.rpe, stop: true, reason: .myoStop, text: text)
        }
        let sum = done.reduce(0, +)
        if done.count >= exercise.myoMaxMiniSets {
            return stop("Stop: \(done.count) mini-sets done")
        }
        // The total before the rep drop: a last mini-set cut to what was left of
        // the total (a myo-rep match) is short by design, not a lost rep.
        if let total = target.myoTotalReps, sum >= total {
            return stop("Stop: \(total) reps reached")
        }
        if rule == .repdrop, let m1, let i = done.indices.dropFirst().first(where: { done[$0] < m1 }) {
            return stop("Stop: mini-set \(i + 1) got \(done[i]), first was \(m1)")
        }
        if rule == .belowtarget, let t1, let i = done.indices.first(where: { done[$0] < t1 }) {
            return stop("Stop: mini-set \(i + 1) got \(done[i]), under \(t1)")
        }
        var reps = j >= 2 && rule == .repdrop ? m1 : t1
        if let total = target.myoTotalReps { reps = min(reps ?? .max, total - sum) }
        let load = exercise.setting(of: head)
        return Suggestion(load: load, reps: reps, targetRPE: target.rpe, reason: .myoMini,
                          text: "Mini-set \(j): \(text.set(load: load, reps: reps))")
    }

    /// 03 §11.8: a timed set.
    func timed() -> Suggestion {
        let lo = target.durationMin
        let hi = target.durationMax
        var load: Double?
        var duration: Int?
        var reason: Reason
        var explanation: String
        if let reference = evidence.reference(ordinal: pending.ordinal, timed: true),
           let held = reference.set.duration
        {
            let done = reference.set
            let effort = done.effectiveRPE(defaultRPE: exercise.defaultRPE)
            let gain = effort.logged && effort.value > rpe || reference.days > 28 ? 0 : exercise.timeStep
            var goal = held + gain
            if let lo { goal = max(goal, lo) }
            load = exercise.setting(of: done)
            let prev = text.previous(done, setting: load, rpe: effort, days: reference.days)
            let takesLoad = exercise.mode == .external || exercise.mode == .bodyweightAdded && exercise.canAddLoad
            if let hi, goal > hi, takesLoad,
               let space = LoadSpace(exercise: exercise, bodyMass: evidence.bodyMass),
               case let value = space.value(ofSetting: load),
               let heavier = space.grid(around: value).next(value)
            {
                load = space.setting(ofValue: heavier)
                duration = lo ?? held
                reason = .timedLoadUp
                explanation = "Last time \(prev), top of range; try \(text.timed(load: load, duration: duration))"
            } else {
                if let hi, goal > hi { goal = hi }
                duration = goal
                reason = .timedHistory
                explanation = "Last time \(prev); try \(text.timed(load: load, duration: duration))"
            }
        } else {
            load = exercise.mode == .none ? nil : target.load
            duration = lo
            reason = .timedColdStart
            explanation = lo.map { "First time: hold for \(SuggestText.duration($0))" }
                ?? "First time: hold as long as form allows"
        }
        // The lifter is not asked to hold longer than they just did.
        if let latest = evidence.latest(in: today, timed: true), let heldToday = latest.set.duration,
           heldToday < duration ?? .max
        {
            duration = heldToday
            reason = .timedSession
            explanation = "Set \(latest.ordinal) held \(SuggestText.duration(heldToday)); "
                + "try \(text.timed(load: load, duration: duration))"
        }
        return Suggestion(load: load, duration: duration, targetRPE: target.rpe, reason: reason, text: explanation)
    }

    // MARK: Texts (03 §12)

    /// `10–12`, or `5` when the range is one count (or open at the top).
    var rangeText: String {
        guard let hi = range.hi, hi != range.lo else { return "\(range.lo)" }
        return "\(range.lo)\u{2013}\(hi)"
    }

    func sessionText(_ reason: Reason, k: Int, prev: String, new: String) -> String {
        switch reason {
        case .sessionUp: "Set \(k): \(prev), easier than RPE \(SuggestText.rpe(rpe)); try \(new)"
        case .sessionDown: "Set \(k): \(prev), harder than RPE \(SuggestText.rpe(rpe)); try \(new)"
        case .sessionCoarse: "Set \(k): \(prev); next step too big, try \(new)"
        case .sessionAtMax: "Set \(k): \(prev), at the heaviest load; try \(new)"
        default: "Set \(k): \(prev); try \(new)"
        }
    }

    func historyText(_ reason: Reason, prev: String, new: String) -> String {
        switch reason {
        case .historyLoadUp: "Last time \(prev), top of range; try \(new)"
        case .historyLoadDown: "Last time \(prev), below target; try \(new)"
        case .historyAtMax: "Last time \(prev), at the heaviest load; try \(new)"
        case .historyNewRange:
            "Last time \(prev); for \(rangeText) reps try \(new)"
        default: "Last time \(prev); try \(new)"
        }
    }
}
