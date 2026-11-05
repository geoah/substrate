import Foundation
import Testing

@testable import WorkoutsCore

// The progression offer (DESIGN §5.3): when an exercise is ready to go up,
// and how the suggestion holds a load or takes the owner's new one.

private let now = try! Date("2026-11-05T12:00:00Z", strategy: .iso8601)
private let utc: Calendar = {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: "UTC")!
    return calendar
}()

private let barbell = ExerciseConfig(unit: .kg, base: 20, step: 2.5)
private let plan = Target03(repsMin: 8, repsMax: 12, rpe: 8)

private func working(
    _ load: Double, _ reps: Int, rpe: Double? = nil, unrated: Double? = 8, failure: Bool = false, stop: StopReason? = nil,
    target: Target03 = plan, ordinal: Int
) -> DoneSet {
    DoneSet(
        kind: failure ? .failure : .normal, load: load, unit: .kg, reps: reps, rpe: rpe,
        unratedRPE: rpe == nil ? unrated : nil, reachedFailure: failure, stopReason: stop, target: target,
        completedAt: now, position: ordinal, ordinal: ordinal)
}

private func session(daysAgo days: Int, _ sets: [DoneSet]) -> PastSession {
    PastSession(start: now.addingTimeInterval(-Double(days) * 86400), sets: sets)
}

/// Two sessions of three working sets of 105 kg × 12, logged as planned.
private func ready(_ change: (inout [PastSession]) -> Void = { _ in }) -> [PastSession] {
    var history = [4, 9].map { days in session(daysAgo: days, (1 ... 3).map { working(105, 12, ordinal: $0) }) }
    change(&history)
    return history
}

private func check(_ history: [PastSession], _ exercise: ExerciseConfig = barbell) -> ProgressionCheck? {
    ProgressionOffer.check(history: history, exercise: exercise, now: now, calendar: utc)
}

@Suite("The progression offer", .timeLimit(.minutes(10)))
struct ProgressionOfferTests {
    @Test("two sessions at the top of the range at the planned reps left: one step up, said as planned")
    func readyAsPlanned() throws {
        let offer = try #require(check(ready()))
        #expect(offer.load == 105 && offer.next == 107.5 && offer.loadText == "105 kg" && offer.nextText == "107.5 kg")
        #expect(offer.topReps == 12 && offer.plannedLeft == 2 && offer.assumedEffort)
    }

    @Test("a rating counts when it left at least the planned reps, and fails when it left fewer")
    func ratings() {
        let rated = ready { history in history[0].sets[2] = working(105, 12, rpe: 7.5, ordinal: 3) }
        #expect(check(rated)?.assumedEffort == true)
        let allRated = [4, 9].map { days in session(daysAgo: days, (1 ... 3).map { working(105, 12, rpe: 8, ordinal: $0) }) }
        #expect(check(allRated)?.assumedEffort == false)
        let tooHard = ready { history in history[1].sets[1] = working(105, 12, rpe: 9, ordinal: 2) }
        #expect(check(tooHard) == nil)
        let failed = ready { history in history[0].sets[2] = working(105, 12, failure: true, ordinal: 3) }
        #expect(check(failed) == nil)
    }

    @Test("not ready: a set short of the top, a form or pain stop, two loads, one session, a long gap")
    func notReady() {
        #expect(check(ready { $0[0].sets[1] = working(105, 11, ordinal: 2) }) == nil)
        #expect(check(ready { $0[1].sets[0] = working(105, 12, stop: .technique, ordinal: 1) }) == nil)
        #expect(check(ready { $0[0].sets[0] = working(105, 12, stop: .pain, ordinal: 1) }) == nil)
        #expect(check(ready { history in history[1].sets = (1 ... 3).map { working(100, 12, ordinal: $0) } }) == nil)
        #expect(check(Array(ready().prefix(1))) == nil)
        #expect(check(ready { $0[0].start = now.addingTimeInterval(-30 * 86400) }) == nil)
        #expect(check(ready { $0[1].start = $0[0].start.addingTimeInterval(-29 * 86400) }) == nil)
        // A muscular stop is evidence, and an open top never is.
        #expect(check(ready { $0[0].sets[0] = working(105, 12, stop: .fatigue, ordinal: 1) }) != nil)
        #expect(check(ready { $0[0].sets[0] = working(105, 12, target: Target03(repsMin: 8, openTop: true), ordinal: 1) }) == nil)
    }

    @Test("a lighter back-off set neither qualifies nor spoils the working load")
    func backOff() throws {
        let history = ready { history in
            for index in history.indices { history[index].sets.append(working(90, 9, ordinal: 4)) }
        }
        #expect(try #require(check(history)).load == 105)
    }

    @Test("no grid, no offer; an assisted exercise goes up by one step less assistance")
    func grids() throws {
        #expect(check(ready(), ExerciseConfig(unit: .kg)) == nil)
        let assisted = ExerciseConfig(unit: .kg, mode: .bodyweightAssisted, base: 0, step: 5)
        let history = [4, 9].map { days in session(daysAgo: days, (1 ... 2).map { working(30, 12, ordinal: $0) }) }
        let offer = try #require(check(history, assisted))
        #expect(offer.load == 30 && offer.next == 25 && offer.nextText == "BW \u{2212} 25 kg")
    }
}

@Suite("The progression gate", .timeLimit(.minutes(10)))
struct ProgressionGateTests {
    /// Last time 100 kg × 12 with 4 left: 03 §11.3 goes up a step.
    private let history = [PastSession(start: now.addingTimeInterval(-4 * 86400), sets: [working(100, 12, rpe: 6, ordinal: 1)])]
    private let pending = PendingSet(kind: .normal, ordinal: 1, target: plan)

    private func run(_ gate: ProgressionGate, today: [DoneSet] = []) -> Suggestion? {
        suggest(
            pending: pending, exercise: barbell, today: today, history: history, bodyMass: nil, now: now, calendar: utc,
            progression: gate)
    }

    @Test("automatic: 03's load-up from history")
    func automatic() throws {
        let suggestion = try #require(run(.automatic))
        #expect(suggestion.reason == .historyLoadUp && suggestion.load == 102.5)
    }

    @Test("asked and not answered: the load holds, at the top of the range")
    func holds() throws {
        let suggestion = try #require(run(.ask(accepted: nil)))
        #expect(suggestion.reason == .historyHold && suggestion.load == 100 && suggestion.reps == 12)
        #expect(suggestion.text == "Last time 100 kg × 12 with 4 left, top of range; try 100 kg × 12")
    }

    @Test("taken: the new load at the bottom of the range, then today's sets lead")
    func goingUp() throws {
        let suggestion = try #require(run(.ask(accepted: 102.5)))
        #expect(suggestion.reason == .progressionUp && suggestion.load == 102.5 && suggestion.reps == 8)
        #expect(suggestion.text == "Going up from 100 kg: try 102.5 kg × 8")
        let after = try #require(run(.ask(accepted: 102.5), today: [working(102.5, 10, rpe: 8, ordinal: 1)]))
        #expect(after.reason != .progressionUp)
    }
}

@Suite("The progression offer on the Rows layout", .timeLimit(.minutes(10)))
struct ProgressionOfferRowsTests {
    /// Two working sets of the press at 60 kg × 12, logged with the plan's 2 left untouched.
    private func past(_ id: String, daysAgo days: Int) -> SessionRecords {
        let session = SessionID(id)
        let date = engineEpoch.addingTimeInterval(-Double(days) * 86400)
        let sets = (0 ..< 2).map { index in
            PerformedSet(
                id: SetID("\(id)-\(index)"), at: date.addingTimeInterval(TimeInterval(60 * (index + 1))), session: session,
                exercise: EngineCatalog.press.id, slot: "s", order: index + 1, ordinal: index + 1, loadMode: .external,
                load: .kg(60), reps: 12, rpe: 8, rpeMethod: .prefilled,
                target: SetTarget(repsMin: 8, repsMax: 12, rpeMin: 8, rpeMax: 8))
        }
        return SessionRecords(
            session: TrainingSession(
                id: session, at: date, endsAt: date.addingTimeInterval(3600), status: .finished,
                slots: [SessionSlot(key: "s", exercise: EngineCatalog.press.id)], timezone: "UTC"),
            sets: sets)
    }

    @Test("ready: the card with its reasons, the load held meanwhile; Use takes the new load until a set is done")
    func offerOnTheBlock() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(
            EngineCatalog.routine([EngineCatalog.slot("s", EngineCatalog.press, EngineCatalog.sets(2))]),
            exercises: [EngineCatalog.press])
        let history = HistoryIndex([past("a", daysAgo: 3), past("b", daysAgo: 7)])
        var settings = PresenterSettings(calendar: Progression.calendar, prefillsRPE: true, asksBeforeGoingUp: true)
        func present() throws -> SessionPresentation {
            SessionPresenter.present(try script.current, history: history, settings: settings, now: engineEpoch)
        }
        let first = try script.row("s", 0)

        let offer = try #require(try present().rowsLayout.blocks.first?.offers.first)
        #expect(offer.state == .offered && offer.title == "Ready to go up" && offer.undo == nil)
        #expect(offer.detail == "Last 2 sessions: every working set reached 12 reps at 60 kg, with 2 or more left (as planned).")
        #expect(offer.accept?.title == "Use 65 kg" && offer.accept?.action == .answerProgression(slot: "s", answer: .up(.kg(65))))
        #expect(offer.stay?.title == "Stay at 60 kg" && offer.stay?.action == .answerProgression(slot: "s", answer: .stay))
        // Until the owner answers, the load holds at the top of the range.
        let held = try #require(try present().row(first)?.suggestion)
        #expect(held.reason == .historyHold && held.load == 60 && held.reps == 12)

        try script.send(.answerProgression(slot: "s", answer: .up(.kg(65))))
        let taken = try #require(try present().rowsLayout.blocks.first?.offers.first)
        #expect(taken.state == .goingUp && taken.title == "Going up to 65 kg")
        #expect(taken.undo?.action == .answerProgression(slot: "s", answer: nil))
        let up = try #require(try present().row(first)?.suggestion)
        #expect(up.reason == .progressionUp && up.load == 65 && up.reps == 8)

        // Once a working set is done the card goes: the sets speak for it.
        try script.tick(first, kgReps(65, 9))
        #expect(try present().rowsLayout.blocks.first?.offers.isEmpty == true)

        // Asking off: no card, and 03's own load-up.
        settings.asksBeforeGoingUp = false
        try script.send(.uncompleteSet(row: first))
        #expect(try present().rowsLayout.blocks.first?.offers.isEmpty == true)
        #expect(try present().row(first)?.suggestion?.reason == .historyLoadUp)
    }

    @Test("Stay holds the load and says so; Undo offers it again")
    func stay() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(
            EngineCatalog.routine([EngineCatalog.slot("s", EngineCatalog.press, EngineCatalog.sets(2))]),
            exercises: [EngineCatalog.press])
        let history = HistoryIndex([past("a", daysAgo: 3), past("b", daysAgo: 7)])
        let settings = PresenterSettings(calendar: Progression.calendar, prefillsRPE: true, asksBeforeGoingUp: true)
        func offer() throws -> RowsProgressionOffer? {
            try SessionPresenter.present(try script.current, history: history, settings: settings, now: engineEpoch)
                .rowsLayout.blocks.first?.offers.first
        }
        try script.send(.answerProgression(slot: "s", answer: .stay))
        #expect(try offer()?.state == .staying && offer()?.title == "Staying at 60 kg")
        try script.send(.answerProgression(slot: "s", answer: nil))
        #expect(try offer()?.state == .offered)
    }
}
