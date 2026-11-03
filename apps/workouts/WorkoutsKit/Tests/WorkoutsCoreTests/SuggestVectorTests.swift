import Foundation
@testable import WorkoutsCore
import Testing

// Research 03 §14's vectors V1 to V19 (DESIGN §10.2), verbatim, then the curve
// and grid checks and the cases of the app's own rules (DESIGN §5.3). Loads,
// reps, durations and strings compare exactly; intermediate numbers to 1e-4.

// MARK: Fixtures

/// The instant every vector is computed at, and a calendar that counts days in UTC.
private let now = try! Date("2026-10-30T12:00:00Z", strategy: .iso8601)
private let utc: Calendar = {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: "UTC")!
    return calendar
}()

private func daysAgo(_ days: Int) -> Date { now.addingTimeInterval(-Double(days) * 86400) }

// The grids 03 §14 names.
private let barbellKg = ExerciseConfig(unit: .kg, base: 20, step: 2.5)
private let stackKg = ExerciseConfig(unit: .kg, base: 0, step: 5)
private let dumbbellKg = ExerciseConfig(unit: .kg, base: 0, step: 2)
private let barbellLb = ExerciseConfig(unit: .lb, base: 45, step: 5)

private func range(_ lo: Int, _ hi: Int, rpe: Double) -> Target03 {
    Target03(repsMin: lo, repsMax: hi, rpe: rpe)
}

/// A performed set; `minutes` after `start` orders it.
private func set(
    _ kind: SuggestKind = .normal, load: Double?, reps: Int? = nil, duration: Int? = nil, rpe: Double? = nil,
    target: Target03 = Target03(), ordinal: Int, position: Int? = nil, unit: MassUnit = .kg,
    left: Int? = nil, right: Int? = nil, start: Date = now, minutes: Int = 0
) -> DoneSet {
    DoneSet(kind: kind, load: load, unit: unit, reps: reps, repsLeft: left, repsRight: right, duration: duration,
            rpe: rpe, target: target, completedAt: start.addingTimeInterval(Double(minutes) * 60),
            position: position ?? ordinal, ordinal: ordinal)
}

private func session(daysAgo gap: Int = 7, bodyMass: Double? = nil, _ sets: [DoneSet]) -> PastSession {
    PastSession(start: daysAgo(gap), bodyMass: bodyMass, sets: sets)
}

private func run(
    _ pending: PendingSet, _ exercise: ExerciseConfig, today: [DoneSet] = [], history: [PastSession] = [],
    bodyMass: Double? = nil
) -> Suggestion? {
    suggest(pending: pending, exercise: exercise, today: today, history: history,
            bodyMass: bodyMass, now: now, calendar: utc)
}

private func grid(_ exercise: ExerciseConfig, bodyMass: Double? = nil) -> LoadGrid {
    LoadSpace(exercise: exercise, bodyMass: bodyMass)!.grid!
}

private func close(_ a: Double, _ b: Double, _ tolerance: Double = 1e-4) -> Bool { abs(a - b) <= tolerance }

/// `E` as 03 §10.6 computes it from history: `L / p(n)` with the effective RPE.
private func e1RM(_ done: DoneSet, _ exercise: ExerciseConfig, bodyMass: Double? = nil) -> Double {
    exercise.oneRepMax(of: done, bodyMass: bodyMass)!.value
}

private func repsAt(_ load: Double, _ e1RM: Double, _ rpe: Double) -> Int {
    RPECurve.repsAt(load, e1RM: e1RM, rpe: rpe)
}

// MARK: V1 to V19

@Suite("Suggestion vectors, research 03 §14")
struct SuggestVectorTests {
    // Chest press (machine), stack kg; the owner's screenshot session.
    private let chestSet1 = set(load: 35, reps: 15, target: range(10, 15, rpe: 9.5), ordinal: 1, start: daysAgo(7))
    private let chestSet2 = set(load: 35, reps: 13, rpe: 9.5, target: range(10, 15, rpe: 9.5), ordinal: 2,
                                start: daysAgo(7), minutes: 3)

    @Test("V1. First set from history, top of range reached (load up)")
    func v1() throws {
        let rpe = chestSet1.effectiveRPE(defaultRPE: 8)
        #expect(rpe.value == 9.5 && !rpe.logged)
        let e = e1RM(chestSet1, stackKg)
        #expect(close(RPECurve.p(15.5), 0.586))
        #expect(close(e, 59.726962))
        #expect(repsAt(35, e, 9.5) == 15)
        #expect(grid(stackKg).next(35) == 40)
        #expect(close(40 / e, 0.669714))
        #expect(close(RPECurve.pInv(40 / e), 12.395604))
        #expect(repsAt(40, e, 9.5) == 11)

        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(10, 15, rpe: 9.5)), stackKg,
                                 history: [session([chestSet1])]))
        #expect(s.load == 40)
        #expect(s.reps == 11)
        #expect(!s.stop)
        #expect(s.reason == .historyLoadUp)
        #expect(s.text == "Last time 35 kg × 15, top of range; try 40 kg × 11")
    }

    @Test("V2. First set from history, add a rep (the owner's example)")
    func v2() throws {
        let e = e1RM(chestSet2, stackKg)
        #expect(close(RPECurve.p(13.5), 0.640))
        #expect(close(e, 54.6875))
        #expect(repsAt(35, e, 9.5) == 13)

        let s = try #require(run(PendingSet(kind: .normal, ordinal: 2, target: range(10, 15, rpe: 9.5)), stackKg,
                                 history: [session([chestSet1, chestSet2])]))
        #expect(s.load == 35)
        #expect(s.reps == 14)
        #expect(!s.stop)
        #expect(s.reason == .historyAddRep)
        #expect(s.text == "Last time 35 kg × 13 at RPE 9.5; try 35 kg × 14")
    }

    @Test("V3. In-session, one point easier than the target")
    func v3() throws {
        let e = 60 / RPECurve.p(13)
        #expect(close(e, 91.883614))
        #expect(repsAt(60, e, 8) == 11)

        let today = [set(load: 60, reps: 10, rpe: 7, target: range(8, 12, rpe: 8), ordinal: 1)]
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 2, target: range(8, 12, rpe: 8)), barbellKg,
                                 today: today))
        #expect(s.load == 60)
        #expect(s.reps == 11)
        #expect(!s.stop)
        #expect(s.reason == .sessionKeep)
        #expect(s.text == "Set 1: 60 kg × 10 at RPE 7; try 60 kg × 11")
    }

    @Test("V4. In-session, much easier than the target (load up)")
    func v4() throws {
        let e = 60 / RPECurve.p(16)
        let g = grid(barbellKg)
        #expect(close(e, 104.895105))
        #expect(repsAt(60, e, 8) == 14)
        #expect(close(e * RPECurve.p(15), 62.832168))
        #expect(g.floor(e * RPECurve.p(15)) == 62.5)
        #expect(g.next(62.5) == 65)
        #expect(g.next(60) == 62.5)
        #expect(g.floor(66) == 65)
        #expect(close(65 / e, 0.619667))
        #expect(close(RPECurve.pInv(65 / e), 14.243590))

        let today = [set(load: 60, reps: 12, rpe: 6, target: range(8, 12, rpe: 8), ordinal: 1)]
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 2, target: range(8, 12, rpe: 8)), barbellKg,
                                 today: today))
        #expect(s.load == 65)
        #expect(s.reps == 12)
        #expect(!s.stop)
        #expect(s.reason == .sessionUp)
        #expect(s.text == "Set 1: 60 kg × 12 at RPE 6, easier than RPE 8; try 65 kg × 12")
    }

    @Test("V5. In-session, harder than the target (load down)")
    func v5() throws {
        let e = 100 / RPECurve.p(5.5 - 0.5)
        #expect(close(e, 115.874855))
        #expect(repsAt(100, e, 8) == 3)
        #expect(close(e * RPECurve.p(7), 93.974508))
        #expect(grid(barbellKg).floor(e * RPECurve.p(7)) == 92.5)
        #expect(close(92.5 / e, 0.798275))
        #expect(close(RPECurve.pInv(92.5 / e), 7.527885))

        let today = [set(load: 100, reps: 5, rpe: 9.5, target: range(5, 5, rpe: 8), ordinal: 1)]
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 2, target: range(5, 5, rpe: 8)), barbellKg,
                                 today: today))
        #expect(s.load == 92.5)
        #expect(s.reps == 5)
        #expect(!s.stop)
        #expect(s.reason == .sessionDown)
        #expect(s.text == "Set 1: 100 kg × 5 at RPE 9.5, harder than RPE 8; try 92.5 kg × 5")
    }

    @Test("V6. In-session fatigue allowance")
    func v6() throws {
        let e = 60 / RPECurve.p(11 - 0.5)
        #expect(close(e, 82.987552))
        #expect(repsAt(60, e, 8) == 8)

        let today = [set(load: 60, reps: 10, rpe: 9, target: range(8, 12, rpe: 8), ordinal: 1)]
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 2, target: range(8, 12, rpe: 8)), barbellKg,
                                 today: today))
        #expect(s.load == 60)
        #expect(s.reps == 8)
        #expect(!s.stop)
        #expect(s.reason == .sessionKeep)
        #expect(s.text == "Set 1: 60 kg × 10 at RPE 9; try 60 kg × 8")
    }

    @Test("V7. Missed reps last time, no RPE")
    func v7() throws {
        let last = set(load: 40, reps: 6, target: range(8, 12, rpe: 8), ordinal: 1, start: daysAgo(7))
        let rpe = last.effectiveRPE(defaultRPE: 8)
        #expect(rpe.value == 10 && !rpe.logged)
        let e = e1RM(last, barbellKg)
        #expect(close(e, 47.789725))
        #expect(repsAt(40, e, 8) == 4)
        #expect(close(e * RPECurve.p(10), 35.316607))
        #expect(grid(barbellKg).floor(e * RPECurve.p(10)) == 35)
        #expect(close(35 / e, 0.732375))
        #expect(close(RPECurve.pInv(35 / e), 10.207031))

        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(8, 12, rpe: 8)), barbellKg,
                                 history: [session([last])]))
        #expect(s.load == 35)
        #expect(s.reps == 8)
        #expect(!s.stop)
        #expect(s.reason == .historyLoadDown)
        #expect(s.text == "Last time 40 kg × 6, below target; try 35 kg × 8")
    }

    @Test("V8. Warm-up sets are ignored")
    func v8() throws {
        let start = daysAgo(7)
        let warmup = set(.warmup, load: 25, reps: 15, rpe: 6, ordinal: 1, position: 0, start: start)
        let working = set(load: 35, reps: 15, target: range(10, 15, rpe: 9.5), ordinal: 1, position: 1,
                          start: start, minutes: 3)
        let history = [session([warmup, working])]

        // 8a. The warm-ups are not W and not ordinal 1.
        let todayWarmup = set(.warmup, load: 25, reps: 15, rpe: 6, ordinal: 1, position: 0)
        let a = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(10, 15, rpe: 9.5)), stackKg,
                                 today: [todayWarmup], history: history))
        #expect(a.load == 40)
        #expect(a.reps == 11)
        #expect(!a.stop)
        #expect(a.reason == .historyLoadUp)
        #expect(a.text == "Last time 35 kg × 15, top of range; try 40 kg × 11")

        // 8b. Pending warm-up 1, before it is done.
        let b = try #require(run(PendingSet(kind: .warmup, ordinal: 1), stackKg, history: history))
        #expect(b.load == 25)
        #expect(b.reps == 15)
        #expect(!b.stop)
        #expect(b.reason == .warmupAsLastTime)
        #expect(b.text == "Warm-up, as last time: 25 kg × 15")
    }

    // Machine lateral raise, stack kg: activation 25 × 17 at RPE 9 done today.
    private let activation = set(.myoActivation, load: 25, reps: 17, rpe: 9, ordinal: 1)

    private func mini(_ done: [Int], head: DoneSet? = nil) -> Suggestion? {
        run(PendingSet(kind: .myoMini, ordinal: 1, parent: head ?? activation, miniSetsDone: done), stackKg,
            today: [head ?? activation])
    }

    @Test("V9a. Myo-rep mini-sets: the first mini-set")
    func v9a() throws {
        let s = try #require(mini([]))
        #expect(s.load == 25)
        #expect(s.reps == 4)
        #expect(!s.stop)
        #expect(s.reason == .myoMini)
        #expect(s.text == "Mini-set 1: 25 kg × 4")
    }

    @Test("V9b. Myo-rep mini-sets: the target after the first is m1")
    func v9b() throws {
        let s = try #require(mini([4, 4, 4]))
        #expect(s.load == 25)
        #expect(s.reps == 4)
        #expect(!s.stop)
        #expect(s.reason == .myoMini)
        #expect(s.text == "Mini-set 4: 25 kg × 4")
    }

    @Test("V9c. Myo-rep mini-sets: a lost rep stops the chain")
    func v9c() throws {
        let s = try #require(mini([4, 4, 4, 3]))
        #expect(s.stop)
        #expect(s.reason == .myoStop)
        #expect(s.text == "Stop: mini-set 4 got 3, first was 4")
    }

    @Test("V9d. Myo-rep mini-sets: the cap stops the chain")
    func v9d() throws {
        let s = try #require(mini([5, 5, 5, 5, 5]))
        #expect(s.stop)
        #expect(s.reason == .myoStop)
        #expect(s.text == "Stop: 5 mini-sets done")
    }

    @Test("V9. Rounding check: t1 rounds halves up", arguments: [(18, 5), (13, 3), (21, 5), (17, 4)])
    func v9Rounding(activationReps: Int, t1: Int) throws {
        let head = set(.myoActivation, load: 25, reps: activationReps, rpe: 9, ordinal: 1)
        let s = try #require(mini([], head: head))
        #expect(s.reps == t1)
    }

    @Test("V10. Drop set")
    func v10() throws {
        let parent = set(load: 35, reps: 8, rpe: 10, ordinal: 1)
        #expect(grid(stackKg).floor(35 * 0.8) == 25)
        let a = try #require(run(PendingSet(kind: .drop, ordinal: 1, parent: parent), stackKg, today: [parent]))
        #expect(a.load == 25)
        #expect(a.reps == nil)
        #expect(!a.stop)
        #expect(a.reason == .drop)
        #expect(a.text == "Drop 20% from 35 kg: 25 kg to failure")

        // 10b. A second drop after 25.
        let first = set(.drop, load: 25, reps: 6, ordinal: 1, position: 2, minutes: 1)
        #expect(grid(stackKg).floor(25 * 0.8) == 20)
        let b = try #require(run(PendingSet(kind: .drop, ordinal: 1, parent: first), stackKg,
                                 today: [parent, first]))
        #expect(b.load == 20)
        #expect(b.reps == nil)
        #expect(!b.stop)
        #expect(b.reason == .drop)
        #expect(b.text == "Drop 20% from 25 kg: 20 kg to failure")
    }

    // Plank: a `duration` exercise, so no load (DESIGN §5.2); no duration range, no RPE.
    private let plank = ExerciseConfig(unit: .kg, mode: .none)
    private var plankHistory: [PastSession] {
        [session([
            set(.timed, load: nil, duration: 62, ordinal: 1, start: daysAgo(7)),
            set(.timed, load: nil, duration: 53, ordinal: 2, start: daysAgo(7), minutes: 2),
        ])]
    }

    @Test("V11. Timed set")
    func v11() throws {
        let a = try #require(run(PendingSet(kind: .timed, ordinal: 1), plank, history: plankHistory))
        #expect(a.duration == 67)
        #expect(a.load == nil)
        #expect(!a.stop)
        #expect(a.reason == .timedHistory)
        #expect(a.text == "Last time 1:02; try 1:07")

        // 11b. Set 2, set 1 not yet done.
        let b = try #require(run(PendingSet(kind: .timed, ordinal: 2), plank, history: plankHistory))
        #expect(b.duration == 58)
        #expect(!b.stop)
        #expect(b.reason == .timedHistory)
        #expect(b.text == "Last time 0:53; try 0:58")

        // 11c. Set 2, today set 1 held 55 s.
        let today = [set(.timed, load: nil, duration: 55, ordinal: 1)]
        let c = try #require(run(PendingSet(kind: .timed, ordinal: 2), plank, today: today, history: plankHistory))
        #expect(c.duration == 55)
        #expect(!c.stop)
        #expect(c.reason == .timedSession)
        #expect(c.text == "Set 1 held 0:55; try 0:55")
    }

    @Test("V12. Bodyweight with added load")
    func v12() throws {
        let pullUp = ExerciseConfig(unit: .kg, mode: .bodyweightAdded, bodyShare: 1.0, base: 0, step: 2.5,
                                    canAddLoad: true)
        let last = set(load: 0, reps: 10, rpe: 9, target: range(6, 10, rpe: 9), ordinal: 1, start: daysAgo(7))
        let e = e1RM(last, pullUp, bodyMass: 80)
        #expect(exercise(pullUp, effectiveLoadOf: last, bodyMass: 80) == 80)
        #expect(close(e, 113.154173))
        #expect(repsAt(80, e, 9) == 10)
        #expect(grid(pullUp, bodyMass: 80).next(80) == 82.5)
        #expect(close(82.5 / e, 0.729094))
        #expect(close(RPECurve.pInv(82.5 / e), 10.309570))

        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(6, 10, rpe: 9)), pullUp,
                                 history: [session(bodyMass: 80, [last])], bodyMass: 80))
        #expect(s.load == 2.5)
        #expect(s.reps == 9)
        #expect(!s.stop)
        #expect(s.reason == .historyLoadUp)
        #expect(s.text == "Last time BW × 10 at RPE 9, top of range; try BW + 2.5 kg × 9")
    }

    @Test("V13. Assisted")
    func v13() throws {
        let assisted = ExerciseConfig(unit: .kg, mode: .bodyweightAssisted, base: 0, step: 5)
        let last = set(load: 30, reps: 12, rpe: 8, target: range(8, 12, rpe: 8), ordinal: 1, start: daysAgo(7))
        let e = e1RM(last, assisted, bodyMass: 80)
        #expect(exercise(assisted, effectiveLoadOf: last, bodyMass: 80) == 50)
        #expect(close(e, 79.872204))
        #expect(repsAt(50, e, 8) == 12)
        #expect(grid(assisted, bodyMass: 80).next(50) == 55)
        #expect(close(55 / e, 0.688600))
        #expect(close(RPECurve.pInv(55 / e), 11.692857))

        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(8, 12, rpe: 8)), assisted,
                                 history: [session(bodyMass: 80, [last])], bodyMass: 80))
        #expect(s.load == 25)
        #expect(s.reps == 9)
        #expect(!s.stop)
        #expect(s.reason == .historyLoadUp)
        #expect(s.text == "Last time BW − 30 kg × 12 at RPE 8, top of range; try BW − 25 kg × 9")
    }

    @Test("V14a. No history, the routine gives a load")
    func v14a() throws {
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: Target03(repsMin: 8, repsMax: 12,
                                                                                         rpe: 8, load: 50)),
                                 barbellKg))
        #expect(s.load == 50)
        #expect(s.reps == 8)
        #expect(!s.stop)
        #expect(s.reason == .coldStartRoutineLoad)
        #expect(s.text == "First time; routine load, try 50 kg × 8")
    }

    @Test("V14b. No history, no routine load")
    func v14b() throws {
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(8, 12, rpe: 8)), barbellKg))
        #expect(s.load == nil)
        #expect(s.reps == 8)
        #expect(!s.stop)
        #expect(s.reason == .coldStartPickLoad)
        #expect(s.text == "First time: pick a load for about 10 reps; do 8")
    }

    @Test("V15. Pounds, with history stored in kg")
    func v15() throws {
        #expect(close(83.9146 / 0.45359237, 185.0000255, 1e-6))
        #expect(SuggestMath.convert(83.9146, from: .kg, to: .lb) == 185)
        let last = set(load: 83.9146, reps: 8, rpe: 8, target: range(6, 8, rpe: 8), ordinal: 1, unit: .kg,
                       start: daysAgo(7))
        let e = e1RM(last, barbellLb)
        #expect(close(e, 250.338295))
        #expect(repsAt(185, e, 8) == 8)
        #expect(grid(barbellLb).next(185) == 190)
        #expect(close(190 / e, 0.758973))
        #expect(close(RPECurve.pInv(190 / e), 9.137592))

        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(6, 8, rpe: 8)), barbellLb,
                                 history: [session([last])]))
        #expect(s.load == 190)
        #expect(s.reps == 7)
        #expect(!s.stop)
        #expect(s.reason == .historyLoadUp)
        #expect(s.text == "Last time 185 lb × 8 at RPE 8, top of range; try 190 lb × 7")
    }

    @Test("V16. Long break")
    func v16() throws {
        let last = set(load: 60, reps: 10, rpe: 8, target: range(8, 12, rpe: 8), ordinal: 1, start: daysAgo(45))
        let e = e1RM(last, barbellKg) * 0.95
        #expect(close(e, 83.823529))
        #expect(close(60 / e, 0.715789))
        #expect(close(RPECurve.pInv(60 / e), 10.725329))
        #expect(repsAt(60, e, 8) == 8)

        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(8, 12, rpe: 8)), barbellKg,
                                 history: [session(daysAgo: 45, [last])]))
        #expect(s.load == 60)
        #expect(s.reps == 8)
        #expect(!s.stop)
        #expect(s.reason == .historyAddRep)
        #expect(s.text == "Last time 60 kg × 10 at RPE 8 (45 days ago); try 60 kg × 8")
    }

    @Test("V17. New rep range")
    func v17() throws {
        let last = set(load: 100, reps: 5, rpe: 8, target: range(5, 5, rpe: 8), ordinal: 1, start: daysAgo(7))
        let e = e1RM(last, barbellKg)
        #expect(close(e, 123.304562))
        #expect(close(e * RPECurve.p(13), 80.517879))
        #expect(grid(barbellKg).floor(e * RPECurve.p(13)) == 80)
        #expect(close(80 / e, 0.648800))
        #expect(close(RPECurve.pInv(80 / e), 13.161538))

        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(10, 12, rpe: 8)), barbellKg,
                                 history: [session([last])]))
        #expect(s.load == 80)
        #expect(s.reps == 11)
        #expect(!s.stop)
        #expect(s.reason == .historyNewRange)
        #expect(s.text == "Last time 100 kg × 5 at RPE 8; for 10–12 reps try 80 kg × 11")
    }

    @Test("V18. Unilateral")
    func v18() throws {
        let last = set(load: 20, rpe: 8, target: range(8, 12, rpe: 8), ordinal: 1, left: 10, right: 9,
                       start: daysAgo(7))
        #expect(last.effectiveReps == 9)
        let e = e1RM(last, dumbbellKg)
        #expect(close(e, 28.288543))
        #expect(repsAt(20, e, 8) == 9)

        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(8, 12, rpe: 8)), dumbbellKg,
                                 history: [session([last])]))
        #expect(s.load == 20)
        #expect(s.reps == 10)
        #expect(!s.stop)
        #expect(s.reason == .historyAddRep)
        #expect(s.text == "Last time 20 kg × 9 at RPE 8; try 20 kg × 10")
    }

    @Test("V19. Outlier skipped")
    func v19() throws {
        let target = range(10, 15, rpe: 8)
        let history = [
            session(daysAgo: 3, [set(load: 350, reps: 12, rpe: 8, target: target, ordinal: 1, start: daysAgo(3))]),
            session(daysAgo: 10, [set(load: 35, reps: 12, rpe: 8, target: target, ordinal: 1, start: daysAgo(10))]),
            session(daysAgo: 17, [set(load: 35, reps: 11, rpe: 8, target: target, ordinal: 1, start: daysAgo(17))]),
            session(daysAgo: 24, [set(load: 30, reps: 14, rpe: 8, target: target, ordinal: 1, start: daysAgo(24))]),
        ]
        let e1RMs = history.map { e1RM($0.sets[0], stackKg) }
        #expect(close(e1RMs[0], 559.105431))
        #expect(close(e1RMs[1], 55.910543))
        #expect(close(e1RMs[2], 53.598775))
        #expect(close(e1RMs[3], 52.447552))

        let evidence = SuggestEvidence(exercise: stackKg, history: history, bodyMass: nil, now: now, calendar: utc)
        let plausibility = SuggestPlausibility(evidence: evidence)
        let forS1 = try #require(plausibility.median(excluding: 0))
        #expect(close(forS1, 53.598775))
        #expect(close(1.5 * forS1, 80.398162))
        #expect(!plausibility.accepts(history[0].sets[0], inSessionAt: 0))
        let forS2 = try #require(plausibility.median(excluding: 1))
        #expect(close(forS2, 53.598775))
        #expect(close(0.5 * forS2, 26.799387))
        #expect(plausibility.accepts(history[1].sets[0], inSessionAt: 1))
        #expect(repsAt(35, e1RMs[1], 8) == 12)

        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: target), stackKg, history: history))
        #expect(s.load == 35)
        #expect(s.reps == 13)
        #expect(!s.stop)
        #expect(s.reason == .historyAddRep)
        #expect(s.text == "Last time 35 kg × 12 at RPE 8; try 35 kg × 13")
        // History given oldest first is read newest first all the same (03 §13).
        #expect(run(PendingSet(kind: .normal, ordinal: 1, target: target), stackKg, history: history.reversed()) == s)
    }

    private func exercise(_ config: ExerciseConfig, effectiveLoadOf done: DoneSet, bodyMass: Double) -> Double? {
        config.oneRepMax(of: done, bodyMass: bodyMass)?.effectiveLoad
    }
}

// MARK: The curve, the grid and rounding

@Suite("Suggestion building blocks, research 03 §10")
struct SuggestBlockTests {
    @Test("p and pInv at every published point")
    func publishedPoints() {
        #expect(RPECurve.points.count == 31)
        for (i, value) in RPECurve.points.enumerated() {
            let n = 1 + 0.5 * Double(i)
            #expect(RPECurve.p(n) == value, "p(\(n))")
            #expect(close(RPECurve.pInv(value), n, 1e-9), "pInv(\(value))")
        }
    }

    @Test("p interpolates between half steps and follows the tail past 16")
    func curveShape() {
        #expect(close(RPECurve.p(0.5), 1))
        #expect(close(RPECurve.p(7.25), (0.811 + 0.799) / 2, 1e-12))
        #expect(close(RPECurve.p(20), 0.526240, 1e-6))
        #expect(close(RPECurve.p(30), 0.438533, 1e-6))
        #expect(close(RPECurve.pInv(0.526240), 20, 1e-4))
        #expect(close(RPECurve.pInv(1.2), 1))
        // A load of nothing cannot overflow the reps.
        #expect(RPECurve.repsAt(0, e1RM: 100, rpe: 8) > 1000)
    }

    @Test("repsAt gives back floor(r + R − e) at the set's own load")
    func repsAtOwnLoad() {
        let e = 100 / RPECurve.p(5 + 10 - 8)
        #expect(RPECurve.repsAt(100, e1RM: e, rpe: 8) == 5)
        #expect(RPECurve.repsAt(100, e1RM: e, rpe: 9) == 6)
        #expect(RPECurve.repsAt(100, e1RM: e, rpe: 7.5) == 4)
    }

    @Test("The external grid: floorG, next and prev, rounded to 6 decimals")
    func externalGrid() {
        let g = grid(barbellKg)
        #expect(g.floor(62.832168) == 62.5)
        #expect(g.floor(62.5 - 1e-10) == 62.5) // the 1e-9 snapping tolerance
        #expect(g.floor(5) == 20)               // below the bar: the lightest point
        #expect(g.next(62.5) == 65)
        #expect(g.next(61) == 62.5)
        #expect(g.prev(62.5) == 60)
        #expect(g.prev(20) == nil)
        let fine = grid(ExerciseConfig(unit: .kg, base: 0, step: 0.1))
        #expect(fine.floor(0.3) == 0.3)
        #expect(fine.next(0.3) == 0.4)
        // A grid based at 0 starts one step up: 0 kg is not a set.
        #expect(grid(stackKg).floor(3) == 5)
        #expect(grid(stackKg).prev(5) == nil)
    }

    @Test("A step too small for any equipment reads as no grid")
    func tinyStep() {
        #expect(LoadSpace(exercise: ExerciseConfig(unit: .kg, base: 0, step: 1e-9), bodyMass: nil)?.grid == nil)
        #expect(LoadSpace(exercise: ExerciseConfig(unit: .kg, base: 0, step: 0), bodyMass: nil)?.grid == nil)
    }

    @Test("The grid stops at the heaviest load")
    func maxLoad() {
        let g = grid(ExerciseConfig(unit: .kg, base: 0, step: 5, maxLoad: 100))
        #expect(g.floor(140) == 100)
        #expect(g.next(100) == nil)
        #expect(g.next(97) == 100)
    }

    @Test("The assisted grid counts assistance down and stays above 0")
    func assistedGrid() {
        let g = grid(ExerciseConfig(unit: .kg, mode: .bodyweightAssisted, step: 5), bodyMass: 80)
        #expect(g.floor(52) == 50)  // assistance 30
        #expect(g.floor(90) == 80)  // no assistance is the heaviest point
        #expect(g.floor(1) == 5)    // the most assistance that leaves a load
        #expect(g.next(80) == nil)
        #expect(g.prev(5) == nil)
    }

    @Test("Plain bodyweight is a grid of one point")
    func plainBodyweight() {
        let g = grid(ExerciseConfig(unit: .kg, mode: .bodyweightAdded, step: 2.5, canAddLoad: false), bodyMass: 80)
        #expect(g.isSinglePoint)
        #expect(g.floor(100) == 80)
        #expect(g.next(80) == nil)
    }

    @Test("Units convert with 1 lb = 0.45359237 kg, rounded to 0.01")
    func units() {
        #expect(SuggestMath.convert(83.9146, from: .kg, to: .lb) == 185)
        #expect(SuggestMath.convert(190, from: .lb, to: .kg) == 86.18)
        #expect(SuggestMath.convert(35, from: .kg, to: .kg) == 35)
    }

    @Test("Explanation formats")
    func formats() {
        let kg = SuggestText(unit: .kg, mode: .external)
        #expect(kg.mass(35) == "35 kg")
        #expect(kg.mass(92.5) == "92.5 kg")
        #expect(kg.mass(1.25) == "1.25 kg")
        #expect(kg.mass(100) == "100 kg")
        #expect(SuggestText(unit: .lb, mode: .external).mass(190) == "190 lb")
        #expect(SuggestText.rpe(9.5) == "9.5")
        #expect(SuggestText.rpe(8) == "8")
        #expect(SuggestText.duration(62) == "1:02")
        #expect(SuggestText.duration(53) == "0:53")
        #expect(SuggestText.duration(3725) == "1:02:05")
        #expect(SuggestText.percent(0.2) == "20")
        #expect(SuggestText.percent(0.125) == "12.5")
        #expect(SuggestText(unit: .kg, mode: .external).timed(load: 10, duration: 62) == "10 kg for 1:02")
        #expect(SuggestText(unit: .kg, mode: .none).set(load: nil, reps: 15) == "× 15")
    }
}

// MARK: the app's rules around 03 (DESIGN §5.3) and the edges 03 §13 names

@Suite("the app's suggestion rules, DESIGN §5.3")
struct SuggestWorkoutsRuleTests {
    // Leg press (machine), stack kg, as in the coverage session.
    private let legPress = ExerciseConfig(unit: .kg, base: 0, step: 5)
    private let topSet = set(load: 110, reps: 6, rpe: 8, target: Target03(repsMin: 6, repsMax: 6, rpe: 8, load: 110),
                             ordinal: 1)

    @Test("Back-off by a percentage: the coverage session's 110 kg top set gives 95 kg")
    func backoffPercent() throws {
        let e = 110 / RPECurve.p(8)
        #expect(close(e, 139.949109))
        #expect(close(RPECurve.pInv(95 / e), 12.045462))
        let pending = PendingSet(kind: .normal, ordinal: 2,
                                 target: Target03(repsMin: 8, repsMax: 10, rpe: 8, dropPercent: 0.10), parent: topSet)
        let s = try #require(run(pending, legPress, today: [topSet]))
        #expect(s.load == 95)
        #expect(s.reps == 10)
        #expect(!s.stop)
        #expect(s.reason == .backoff)
        #expect(s.text == "Back-off 10% from 110 kg: try 95 kg × 10")
    }

    @Test("Back-off by a step")
    func backoffStep() throws {
        let pending = PendingSet(kind: .normal, ordinal: 2,
                                 target: Target03(repsMin: 8, repsMax: 10, rpe: 8, dropStep: 10), parent: topSet)
        let s = try #require(run(pending, legPress, today: [topSet]))
        #expect(s.load == 100)
        #expect(s.reps == 8) // floor(10.764187 − 2) = 8
        #expect(s.reason == .backoff)
        #expect(s.text == "Back-off 10 kg from 110 kg: try 100 kg × 8")
    }

    @Test("Back-off reps use 11.2's fatigue allowance and stay in the range")
    func backoffFatigue() throws {
        let top = set(load: 100, reps: 5, rpe: 9.5, ordinal: 1)
        let pending = PendingSet(kind: .normal, ordinal: 2,
                                 target: Target03(repsMin: 5, repsMax: 5, rpe: 8, dropPercent: 0.10), parent: top)
        let s = try #require(run(pending, barbellKg, today: [top]))
        #expect(s.load == 90)
        #expect(s.reps == 5) // repsAt(90) = 6 with E = 100 / p(5), capped to 5
        #expect(s.text == "Back-off 10% from 100 kg: try 90 kg × 5")
    }

    @Test("Back-off from the lightest load keeps it")
    func backoffAtMinimum() throws {
        let top = set(load: 5, reps: 10, rpe: 8, ordinal: 1)
        let pending = PendingSet(kind: .normal, ordinal: 2,
                                 target: Target03(repsMin: 8, repsMax: 12, rpe: 8, dropPercent: 0.20), parent: top)
        let s = try #require(run(pending, legPress, today: [top]))
        #expect(s.load == 5)
        #expect(s.reason == .backoff)
    }

    @Test("A back-off row whose top set is not done follows 03's rules")
    func backoffWithoutParent() throws {
        let pending = PendingSet(kind: .normal, ordinal: 2,
                                 target: Target03(repsMin: 8, repsMax: 10, rpe: 8, dropPercent: 0.10))
        let s = try #require(run(pending, legPress))
        #expect(s.reason == .coldStartPickLoad)
    }

    // Leg extension, stack kg: a rest-pause head 45 kg × 11 at RPE 9.5.
    private let restPause = ExerciseConfig(unit: .kg, base: 0, step: 5, myoMaxMiniSets: 2, myoStopRule: nil)
    private let restPauseHead = set(load: 45, reps: 11, rpe: 9.5, ordinal: 1)

    private func restPauseMini(_ done: [Int], target: Target03 = Target03(), exercise: ExerciseConfig? = nil)
        -> Suggestion?
    {
        run(PendingSet(kind: .myoMini, ordinal: 1, target: target, parent: restPauseHead, miniSetsDone: done),
            exercise ?? restPause, today: [restPauseHead])
    }

    @Test("Rest-pause mini-sets: open reps, the head's load")
    func restPauseOpen() throws {
        let s = try #require(restPauseMini([]))
        #expect(s.load == 45)
        #expect(s.reps == nil)
        #expect(!s.stop)
        #expect(s.reason == .myoMini)
        #expect(s.text == "Mini-set 1: 45 kg")
    }

    @Test("Rest-pause mini-sets stop only at the cap, never on a lost rep")
    func restPauseCap() throws {
        let second = try #require(restPauseMini([4]))
        #expect(!second.stop)
        #expect(second.text == "Mini-set 2: 45 kg")
        let lostRep = try #require(restPauseMini([4, 2], exercise: ExerciseConfig(
            unit: .kg, base: 0, step: 5, myoMaxMiniSets: 3, myoStopRule: nil)))
        #expect(!lostRep.stop)
        let capped = try #require(restPauseMini([4, 2]))
        #expect(capped.stop)
        #expect(capped.reason == .myoStop)
        #expect(capped.text == "Stop: 2 mini-sets done")
    }

    @Test("Rest-pause mini-sets stop at the total and aim at what is left of it")
    func restPauseTotal() throws {
        let exercise = ExerciseConfig(unit: .kg, base: 0, step: 5, myoMaxMiniSets: 5, myoStopRule: nil)
        let rest = try #require(restPauseMini([5], target: Target03(myoTotalReps: 8), exercise: exercise))
        #expect(rest.reps == 3)
        #expect(rest.text == "Mini-set 2: 45 kg × 3")
        let done = try #require(restPauseMini([5, 3], target: Target03(myoTotalReps: 8), exercise: exercise))
        #expect(done.stop)
        #expect(done.text == "Stop: 8 reps reached")
    }

    @Test("Cluster mini-sets take miniSetReps and stop after the last cluster")
    func cluster() throws {
        let hipThrust = ExerciseConfig(unit: .kg, base: 20, step: 2.5, myoMaxMiniSets: 3, myoStopRule: nil)
        let head = set(load: 80, reps: 3, rpe: 8, ordinal: 1)
        func cluster(_ done: [Int]) -> Suggestion? {
            run(PendingSet(kind: .myoMini, ordinal: 1, target: Target03(repsMin: 3, repsMax: 3, myoMiniReps: 3),
                           parent: head, miniSetsDone: done), hipThrust, today: [head])
        }
        let first = try #require(cluster([]))
        #expect(first.load == 80)
        #expect(first.reps == 3)
        #expect(first.text == "Mini-set 1: 80 kg × 3")
        let afterShort = try #require(cluster([3, 2]))
        #expect(!afterShort.stop)
        #expect(afterShort.reps == 3)
        let last = try #require(cluster([3, 3, 3]))
        #expect(last.stop)
        #expect(last.text == "Stop: 3 mini-sets done")
    }

    @Test("Myo-rep match: the total is T minus the head's reps")
    func myoMatch() throws {
        let curl = ExerciseConfig(unit: .kg, base: 0, step: 5)
        let head = set(.myoActivation, load: 40, reps: 12, rpe: 9, ordinal: 2)
        // The slot's previous working set did 20, so T = 20 and the mini-sets owe 8.
        func match(_ done: [Int]) -> Suggestion? {
            run(PendingSet(kind: .myoMini, ordinal: 2, target: Target03(myoTotalReps: 20 - 12), parent: head,
                           miniSetsDone: done), curl, today: [head])
        }
        let first = try #require(match([]))
        #expect(first.reps == 3) // t1 = round(12 / 4)
        #expect(first.text == "Mini-set 1: 40 kg × 3")
        let third = try #require(match([3, 3]))
        #expect(third.reps == 2)
        let reached = try #require(match([3, 3, 2]))
        #expect(reached.stop)
        #expect(reached.text == "Stop: 8 reps reached")
    }

    @Test("The belowTarget stop rule keeps t1 and stops under it")
    func belowTarget() throws {
        let raise = ExerciseConfig(unit: .kg, base: 0, step: 5, myoStopRule: .belowtarget)
        let head = set(.myoActivation, load: 25, reps: 17, rpe: 9, ordinal: 1)
        func mini(_ done: [Int]) -> Suggestion? {
            run(PendingSet(kind: .myoMini, ordinal: 1, parent: head, miniSetsDone: done), raise, today: [head])
        }
        #expect(try #require(mini([5, 4])).reps == 4)
        let stop = try #require(mini([4, 4, 3]))
        #expect(stop.stop)
        #expect(stop.text == "Stop: mini-set 3 got 3, under 4")
    }

    // A resistance band: equipment with no grid (DESIGN §5.2).
    private let band = ExerciseConfig(unit: .kg, mode: .external)

    @Test("No grid: in session, the load stays and the reps move, uncapped")
    func noGridInSession() throws {
        let today = [set(load: 10, reps: 15, rpe: 6, ordinal: 1)]
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 2, target: range(8, 12, rpe: 8)), band,
                                 today: today))
        #expect(s.load == 10)
        #expect(s.reps == 17)
        #expect(s.reason == .sessionKeep)
        #expect(s.text == "Set 1: 10 kg × 15 at RPE 6; try 10 kg × 17")
    }

    @Test("No grid: from history, the load stays and the reps follow the goal")
    func noGridHistory() throws {
        let last = set(load: 10, reps: 7, rpe: 10, target: range(8, 12, rpe: 8), ordinal: 1, start: daysAgo(7))
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(8, 12, rpe: 8)), band,
                                 history: [session([last])]))
        #expect(s.load == 10)
        #expect(s.reps == 6)
        #expect(s.reason == .historyAddRep)
        #expect(s.text == "Last time 10 kg × 7 at RPE 10; try 10 kg × 6")
    }

    @Test("No grid: the routine load is kept as written, and a drop leaves the lighter load to the owner")
    func noGridColdStartAndDrop() throws {
        let cold = try #require(run(PendingSet(kind: .normal, ordinal: 1,
                                               target: Target03(repsMin: 8, repsMax: 12, rpe: 8, load: 12.3)), band))
        #expect(cold.load == 12.3)
        #expect(cold.text == "First time; routine load, try 12.3 kg × 8")
        // the app cannot know a band's lighter setting, so the drop is not
        // stopped (which would remove the planned drops): the owner picks it.
        let parent = set(load: 10, reps: 12, rpe: 10, ordinal: 1)
        let drop = try #require(run(PendingSet(kind: .drop, ordinal: 1, parent: parent), band, today: [parent]))
        #expect(!drop.stop)
        #expect(drop.reason == .drop)
        #expect(drop.load == nil && drop.reps == nil)
        #expect(drop.text == "Drop from 10 kg: a lighter load, to failure")
        let withReps = try #require(run(PendingSet(kind: .drop, ordinal: 1, target: Target03(repsMin: 8, repsMax: 8),
                                                   parent: parent), band, today: [parent]))
        #expect(withReps.reps == 8)
        #expect(withReps.text == "Drop from 10 kg: a lighter load, 8 reps")
    }

    @Test("A drop by a planned step, and a drop with planned reps")
    func dropStepAndReps() throws {
        let parent = set(load: 35, reps: 8, rpe: 10, ordinal: 1)
        let byStep = try #require(run(PendingSet(kind: .drop, ordinal: 1, target: Target03(dropStep: 10),
                                                 parent: parent), stackKg, today: [parent]))
        #expect(byStep.load == 25)
        #expect(byStep.text == "Drop 10 kg from 35 kg: 25 kg to failure")
        let withReps = try #require(run(PendingSet(kind: .drop, ordinal: 1, target: Target03(repsMin: 8, repsMax: 8),
                                                   parent: parent), stackKg, today: [parent]))
        #expect(withReps.reps == 8)
        #expect(withReps.text == "Drop 20% from 35 kg: 25 kg × 8")
    }

    @Test("A drop from the lightest point stops the chain")
    func dropAtMinimum() throws {
        let parent = set(load: 5, reps: 12, rpe: 10, ordinal: 1)
        let s = try #require(run(PendingSet(kind: .drop, ordinal: 1, parent: parent), stackKg, today: [parent]))
        #expect(s.stop)
        #expect(s.reason == .dropAtMinimum)
    }

    @Test("An AMRAP set has no top to its range")
    func openTop() throws {
        let pushdown = ExerciseConfig(unit: .kg, base: 0, step: 5)
        let today = [set(load: 20, reps: 15, rpe: 8, target: range(12, 15, rpe: 9), ordinal: 1)]
        let target = Target03(repsMin: 12, rpe: nil, openTop: true)
        let s = try #require(run(PendingSet(kind: .failure, ordinal: 2, target: target), pushdown, today: today))
        // R = 10 for a failure set; n = 17, so 17 reps, not capped at a top.
        #expect(s.load == 20)
        #expect(s.reps == 17)
        #expect(s.reason == .sessionKeep)
    }

    @Test("A bodyweight exercise with no body mass uses the reps-only rule")
    func repsOnly() throws {
        let dip = ExerciseConfig(unit: .kg, mode: .bodyweightAdded, step: 2.5)
        let last = set(load: 0, reps: 12, rpe: 8, target: range(6, 10, rpe: 8), ordinal: 1, start: daysAgo(7))
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(6, 10, rpe: 8)), dip,
                                 history: [session([last])]))
        // goal = floor(12 + 8 − 8) + 1 = 13 > 10: one step more added load, at the bottom of the range.
        #expect(s.load == 2.5)
        #expect(s.reps == 6)
        #expect(s.reason == .historyLoadUp)
        #expect(s.text == "Last time BW × 12 at RPE 8, top of range; try BW + 2.5 kg × 6")
    }

    @Test("Plain bodyweight from history adds a rep with no top (the push-up)")
    func pushUp() throws {
        let pushUp = ExerciseConfig(unit: .kg, mode: .bodyweightAdded, bodyShare: 0.64, canAddLoad: false)
        let last = set(load: nil, reps: 14, rpe: 9, target: range(8, 12, rpe: 9), ordinal: 1, start: daysAgo(3))
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(8, 12, rpe: 9)), pushUp,
                                 history: [session(daysAgo: 3, [last])], bodyMass: 82.4))
        #expect(s.load == 0)
        #expect(s.reps == 15)
        #expect(s.reason == .historyAddRep)
        #expect(s.text == "Last time BW × 14 at RPE 9; try BW × 15")
    }

    @Test("Plain bodyweight with no history: nothing to pick")
    func plainBodyweightColdStart() throws {
        let pushUp = ExerciseConfig(unit: .kg, mode: .bodyweightAdded, canAddLoad: false)
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: range(8, 12, rpe: 9)), pushUp,
                                 bodyMass: 80))
        #expect(s.load == 0)
        #expect(s.reps == 8)
        #expect(s.text == "First time; try BW × 8")
    }

    @Test("A timed set past its range takes the next load step")
    func timedLoadUp() throws {
        let weightedPlank = ExerciseConfig(unit: .kg, base: 0, step: 5)
        let last = set(.timed, load: 10, duration: 90, ordinal: 1, start: daysAgo(7))
        let s = try #require(run(PendingSet(kind: .timed, ordinal: 1, target: Target03(durationMin: 45,
                                                                                      durationMax: 90)),
                                 weightedPlank, history: [session([last])]))
        #expect(s.load == 15)
        #expect(s.duration == 45)
        #expect(s.reason == .timedLoadUp)
        #expect(s.text == "Last time 10 kg for 1:30, top of range; try 15 kg for 0:45")
    }

    @Test("A timed set with no history holds for the bottom of its range")
    func timedColdStart() throws {
        let plank = ExerciseConfig(unit: .kg, mode: .none)
        let s = try #require(run(PendingSet(kind: .timed, ordinal: 1, target: Target03(durationMin: 45)), plank))
        #expect(s.duration == 45)
        #expect(s.reason == .timedColdStart)
        #expect(s.text == "First time: hold for 0:45")
        // Set 1 held longer, so the cap changes nothing.
        let today = [set(.timed, load: nil, duration: 62, ordinal: 1)]
        let second = try #require(run(PendingSet(kind: .timed, ordinal: 2, target: Target03(durationMin: 45)), plank,
                                      today: today))
        #expect(second.reason == .timedColdStart)
        let open = try #require(run(PendingSet(kind: .timed, ordinal: 1), plank))
        #expect(open.duration == nil)
        #expect(open.text == "First time: hold as long as form allows")
    }

    @Test("A warm-up with no warm-up last time has no suggestion")
    func warmupWithoutHistory() {
        #expect(run(PendingSet(kind: .warmup, ordinal: 1), stackKg) == nil)
        let history = [session([set(load: 35, reps: 12, ordinal: 1, start: daysAgo(7))])]
        #expect(run(PendingSet(kind: .warmup, ordinal: 1), stackKg, history: history) == nil)
    }

    @Test("Entry validation: a typo far from recent sets is implausible, a tenth of it is not")
    func entryValidation() {
        let target = range(10, 15, rpe: 8)
        let history = [10, 17, 24].map { gap in
            session(daysAgo: gap, [set(load: 35, reps: 12, rpe: 8, target: target, ordinal: 1, start: daysAgo(gap))])
        }
        let typo = set(load: 350, reps: 12, rpe: 8, target: target, ordinal: 1)
        let check = checkPlausibility(of: typo, exercise: stackKg, bodyMass: nil, history: history, now: now,
                                      calendar: utc)
        #expect(!check.isPlausible)
        #expect(close(check.median ?? 0, 55.910543))
        #expect(check.accepts(35 / RPECurve.p(14)))
        let fine = checkPlausibility(of: set(load: 40, reps: 10, rpe: 8, ordinal: 1), exercise: stackKg,
                                     bodyMass: nil, history: history, now: now, calendar: utc)
        #expect(fine.isPlausible)
        // Fewer than three sessions: everything is plausible.
        let early = checkPlausibility(of: typo, exercise: stackKg, bodyMass: nil, history: Array(history.prefix(2)),
                                      now: now, calendar: utc)
        #expect(early.median == nil)
        #expect(early.isPlausible)
    }
}

// MARK: 03 §11 branches the vectors leave out

@Suite("Suggestion rules the vectors do not reach, research 03 §11")
struct SuggestBranchTests {
    private let target = range(8, 12, rpe: 8)

    @Test("In session, one raise is capped at one step or 10 %")
    func raiseCap() throws {
        let e = 60 / RPECurve.p(19)
        let g = grid(barbellKg)
        #expect(close(e, 111.736060))
        #expect(g.next(g.floor(e * RPECurve.p(15))) == 67.5) // the uncapped load
        #expect(max(62.5, g.floor(1.10 * 60)) == 65)        // the cap
        let today = [set(load: 60, reps: 15, rpe: 6, target: target, ordinal: 1)]
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 2, target: target), barbellKg, today: today))
        #expect(s.load == 65)
        #expect(s.reps == 12)
        #expect(s.reason == .sessionUp)
        #expect(s.text == "Set 1: 60 kg × 15 at RPE 6, easier than RPE 8; try 65 kg × 12")
    }

    @Test("In session, a step too coarse for the range keeps the load at the top")
    func coarseStep() throws {
        let today = [set(load: 6, reps: 14, rpe: 8, target: target, ordinal: 1)]
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 2, target: target), dumbbellKg, today: today))
        // 8 kg would give 6 reps, under the range.
        #expect(s.load == 6)
        #expect(s.reps == 12)
        #expect(s.reason == .sessionCoarse)
        #expect(s.text == "Set 1: 6 kg × 14 at RPE 8; next step too big, try 6 kg × 12")
    }

    @Test("At the heaviest load, in session and from history")
    func atMax() throws {
        let capped = ExerciseConfig(unit: .kg, base: 0, step: 5, maxLoad: 100)
        let today = [set(load: 100, reps: 15, rpe: 7, target: target, ordinal: 1)]
        let session1 = try #require(run(PendingSet(kind: .normal, ordinal: 2, target: target), capped, today: today))
        #expect(session1.load == 100)
        #expect(session1.reps == 12)
        #expect(session1.reason == .sessionAtMax)
        #expect(session1.text == "Set 1: 100 kg × 15 at RPE 7, at the heaviest load; try 100 kg × 12")

        let last = set(load: 100, reps: 12, rpe: 8, target: target, ordinal: 1, start: daysAgo(7))
        let fromHistory = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: target), capped,
                                           history: [session([last])]))
        #expect(fromHistory.load == 100)
        #expect(fromHistory.reps == 12)
        #expect(fromHistory.reason == .historyAtMax)
        #expect(fromHistory.text == "Last time 100 kg × 12 at RPE 8, at the heaviest load; try 100 kg × 12")
    }

    @Test("A break of more than 90 days cuts the e1RM by 10 % and adds no rep")
    func longerBreak() throws {
        let last = set(load: 60, reps: 10, rpe: 8, target: target, ordinal: 1, start: daysAgo(100))
        let e = e1RM(last, barbellKg) * 0.90
        #expect(close(e, 79.411765))
        #expect(repsAt(60, e, 8) == 7)
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: target), barbellKg,
                                 history: [session(daysAgo: 100, [last])]))
        #expect(s.load == 57.5)
        #expect(s.reps == 8)
        #expect(s.reason == .historyLoadDown)
        #expect(s.text == "Last time 60 kg × 10 at RPE 8 (100 days ago), below target; try 57.5 kg × 8")
    }

    @Test("Plausibility only counts sessions within 180 days")
    func plausibilityWindow() throws {
        let rows = [(3, 350.0), (10, 35.0), (200, 35.0), (210, 35.0)]
        let history = rows.map { gap, load in
            session(daysAgo: gap, [set(load: load, reps: 12, rpe: 8, target: target, ordinal: 1, start: daysAgo(gap))])
        }
        // One other session in the window: too few to judge, so the newest set stands.
        let s = try #require(run(PendingSet(kind: .normal, ordinal: 1, target: target), stackKg, history: history))
        #expect(s.text == "Last time 350 kg × 12 at RPE 8, top of range; try 355 kg × 11")
    }
}
