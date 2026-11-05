import Foundation
import Testing

@testable import WorkoutsCore

// The progression offer (DESIGN §5.3): when an exercise offers a higher load
// and why, and how the suggestion holds a load or takes the owner's new one.

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

private func check(
    _ history: [PastSession], _ exercise: ExerciseConfig = barbell, plan target: Target03 = plan, planned: Int = 3
) -> ProgressionCheck? {
    ProgressionOffer.check(history: history, exercise: exercise, plan: target, planned: planned, now: now, calendar: utc)
}

@Suite("The progression offer's evidence", .timeLimit(.minutes(10)))
struct ProgressionOfferTests {
    @Test("two sessions at the top of today's range on today's targets: one step up; untouched effort said so")
    func readyUnrated() throws {
        let offer = try #require(check(ready()))
        #expect(offer.load == 105 && offer.next == 107.5 && offer.loadText == "105 kg" && offer.nextText == "107.5 kg")
        #expect(offer.topReps == 12 && offer.sets == 3 && offer.plannedLeft == 2 && offer.unrated)
        #expect(offer.reason == "All 3 sets reached 12 reps at 105 kg in both sessions. Effort wasn't rated.")
    }

    @Test("rated at least the planned reps left: said so; fewer, or failure, rules it out")
    func ratings() {
        let allRated = [4, 9].map { days in session(daysAgo: days, (1 ... 3).map { working(105, 12, rpe: 8, ordinal: $0) }) }
        #expect(check(allRated)?.unrated == false)
        #expect(check(allRated)?.reason == "All 3 sets reached 12 reps at 105 kg in both sessions, with at least 2 reps left.")
        #expect(check(ready { $0[1].sets[1] = working(105, 12, rpe: 9, ordinal: 2) }) == nil)
        #expect(check(ready { $0[0].sets[2] = working(105, 12, failure: true, ordinal: 3) }) == nil)
        // No effort in the plan: nothing to hold the effort to, and nothing said of it.
        let open = Target03(repsMin: 8, repsMax: 12)
        let unplanned = [4, 9].map { days in session(daysAgo: days, (1 ... 3).map { working(105, 12, target: open, ordinal: $0) }) }
        #expect(check(unplanned, plan: open)?.reason == "All 3 sets reached 12 reps at 105 kg in both sessions.")
    }

    @Test("not ready: a set short of the top, a limited stop, two loads, one session, a long gap")
    func notReady() {
        #expect(check(ready { $0[0].sets[1] = working(105, 11, ordinal: 2) }) == nil)
        for reason in [StopReason.technique, .pain, .other] {
            #expect(check(ready { $0[1].sets[0] = working(105, 12, stop: reason, ordinal: 1) }) == nil)
        }
        #expect(check(ready { history in history[1].sets = (1 ... 3).map { working(100, 12, ordinal: $0) } }) == nil)
        #expect(check(Array(ready().prefix(1))) == nil)
        #expect(check(ready { $0[0].start = now.addingTimeInterval(-30 * 86400) }) == nil)
        #expect(check(ready { $0[1].start = $0[0].start.addingTimeInterval(-29 * 86400) }) == nil)
        // Fatigue is a reason, and still evidence; an open top never is.
        #expect(check(ready { $0[0].sets[0] = working(105, 12, stop: .fatigue, ordinal: 1) }) != nil)
        #expect(check(ready(), plan: Target03(repsMin: 8, openTop: true)) == nil)
    }

    @Test("comparable: today's range and effort, and as many sets as today plans")
    func comparable() {
        // Today aims at 10 to 15: last sessions' 8 to 12 is another prescription.
        #expect(check(ready(), plan: Target03(repsMin: 10, repsMax: 15, rpe: 8)) == nil)
        // A different effort target is another prescription too.
        #expect(check(ready(), plan: Target03(repsMin: 8, repsMax: 12, rpe: 9)) == nil)
        // Two sets done where today plans three: incomplete.
        #expect(check(ready { $0[0].sets.removeLast() }) == nil)
        #expect(check(ready { $0[0].sets.removeLast() }, planned: 2) != nil)
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
        let offer = try #require(check(history, assisted, planned: 2))
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

    @Test("a reference that ended for technique, pain or another reason gains no rep and raises no load")
    func limitedHistory() throws {
        for reason in [StopReason.technique, .pain, .other] {
            let limited = [PastSession(start: now.addingTimeInterval(-4 * 86400), sets: [working(100, 12, rpe: 6, stop: reason, ordinal: 1)])]
            let suggestion = try #require(
                suggest(pending: pending, exercise: barbell, today: [], history: limited, bodyMass: nil, now: now, calendar: utc))
            #expect(suggestion.load == 100 && suggestion.reason == .historyHold, "\(reason)")
        }
    }

    @Test("today: a set that ended for form never raises the next one; muscular fatigue may")
    func limitedToday() throws {
        let form = try #require(run(.automatic, today: [working(100, 12, rpe: 6, stop: .technique, ordinal: 1)]))
        #expect(form.load == 100 && form.reason == .sessionKeep)
        let fatigue = try #require(run(.automatic, today: [working(100, 12, rpe: 6, stop: .fatigue, ordinal: 1)]))
        #expect(fatigue.load ?? 0 > 100 && fatigue.reason == .sessionUp)
    }

    @Test("taken: the new load at the bottom of the range, then today's sets lead")
    func goingUp() throws {
        let suggestion = try #require(run(.ask(accepted: 102.5)))
        #expect(suggestion.reason == .progressionUp && suggestion.load == 102.5 && suggestion.reps == 8)
        #expect(suggestion.text == "Going up from 100 kg: try 102.5 kg × 8")
        let after = try #require(run(.ask(accepted: 102.5), today: [working(102.5, 10, rpe: 8, ordinal: 1)]))
        #expect(after.reason != .progressionUp)
    }

    @Test("taken: a back-off set never takes the top set's new load, before its parent or after it")
    func goingUpBackoff() throws {
        var backoffPlan = plan
        backoffPlan.dropPercent = 0.10
        let backoff = PendingSet(kind: .normal, ordinal: 2, target: backoffPlan)
        let before = try #require(
            suggest(
                pending: backoff, exercise: barbell, today: [], history: history, bodyMass: nil, now: now, calendar: utc,
                progression: .ask(accepted: 102.5)))
        #expect(before.reason != .progressionUp && before.load ?? 0 < 102.5)
        var after = backoff
        after.parent = working(102.5, 9, rpe: 8, ordinal: 1)
        let backedOff = try #require(
            suggest(
                pending: after, exercise: barbell, today: [working(102.5, 9, rpe: 8, ordinal: 1)], history: history, bodyMass: nil,
                now: now, calendar: utc, progression: .ask(accepted: 102.5)))
        #expect(backedOff.reason == .backoff && backedOff.load == 90)
    }
}

@Suite("The progression offer on the Rows layout", .timeLimit(.minutes(10)))
struct ProgressionOfferRowsTests {
    /// Two working sets of the press at 60 kg × 12, logged with the plan's 2
    /// left untouched, unless `rpe` rates them.
    private func past(
        _ id: String, daysAgo days: Int, reps: Int = 12, rpe: Decimal = 8, method: EffortMethod = .prefilled,
        deload: Bool = false
    ) -> SessionRecords {
        let session = SessionID(id)
        let date = engineEpoch.addingTimeInterval(-Double(days) * 86400)
        let sets = (0 ..< 2).map { index in
            PerformedSet(
                id: SetID("\(id)-\(index)"), at: date.addingTimeInterval(TimeInterval(60 * (index + 1))), session: session,
                exercise: EngineCatalog.press.id, slot: "s", order: index + 1, ordinal: index + 1, loadMode: .external,
                load: .kg(60), reps: reps, rpe: rpe, rpeMethod: method,
                target: SetTarget(repsMin: 8, repsMax: 12, rpeMin: 8, rpeMax: 8))
        }
        return SessionRecords(
            session: TrainingSession(
                id: session, at: date, endsAt: date.addingTimeInterval(3600), status: .finished, deload: deload ? true : nil,
                slots: [SessionSlot(key: "s", exercise: EngineCatalog.press.id)], timezone: "UTC"),
            sets: sets)
    }

    private func started() throws -> EngineScript {
        var script = EngineScript(at: engineEpoch)
        try script.start(
            EngineCatalog.routine([EngineCatalog.slot("s", EngineCatalog.press, EngineCatalog.sets(2))]),
            exercises: [EngineCatalog.press])
        return script
    }

    private let asks = PresenterSettings(calendar: Progression.calendar, prefillsRPE: true, asksBeforeGoingUp: true)

    @Test("one session the engine would go up from: the offer with the engine's reason, the load held")
    func engineIncrease() throws {
        let script = try started()
        let history = HistoryIndex([past("a", daysAgo: 3, rpe: 6, method: .entered)])
        let presentation = SessionPresenter.present(try script.current, history: history, settings: asks, now: engineEpoch)
        let offer = try #require(presentation.rowsLayout.blocks.first?.offers.first)
        #expect(offer.title == "Try a higher load?" && offer.detail == "Last time 60 kg × 12 with 4 left, top of range.")
        #expect(offer.accept?.title == "Use 65 kg" && offer.stay?.title == "Keep 60 kg")
        #expect(presentation.row(try script.row("s", 0))?.suggestion?.load == 60)
    }

    @Test("a deload among the two newest sessions starts the count again")
    func deloadResets() throws {
        let script = try started()
        let history = HistoryIndex([past("a", daysAgo: 3, deload: true), past("b", daysAgo: 7), past("c", daysAgo: 10)])
        let offer = SessionPresenter.present(try script.current, history: history, settings: asks, now: engineEpoch)
            .rowsLayout.blocks.first?.offers.first
        #expect(offer?.detail?.hasPrefix("Both sets") != true)
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
        #expect(offer.state == .offered && offer.title == "Try a higher load?" && offer.undo == nil)
        #expect(offer.detail == "Both sets reached 12 reps at 60 kg in both sessions. Effort wasn't rated.")
        #expect(offer.accept?.title == "Use 65 kg" && offer.accept?.action == .answerProgression(slot: "s", answer: .up(.kg(65))))
        #expect(offer.stay?.title == "Keep 60 kg" && offer.stay?.action == .answerProgression(slot: "s", answer: .stay))
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
        #expect(try offer()?.state == .staying && offer()?.title == "Keeping 60 kg")
        try script.send(.answerProgression(slot: "s", answer: nil))
        #expect(try offer()?.state == .offered)
    }
}

@Suite("The progression setting", .timeLimit(.minutes(10)))
struct ProgressionSettingTests {
    @Test("the session in progress asks before a load goes up unless the owner turned it off")
    func controllerPassesTheSetting() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        var settings = WorkoutsSettings()
        #expect(settings.asksBeforeGoingUp && settings.sessionPresenter.asksBeforeGoingUp)
        func controller(_ settings: WorkoutsSettings) -> SessionController {
            SessionController(catalog: Catalog(), history: HistoryIndex(), settings: settings, adapter: "test", directory: directory.url)
        }
        #expect(controller(settings).presenterSettings(calendar: Progression.calendar).asksBeforeGoingUp)
        settings.asksBeforeGoingUp = false
        #expect(!controller(settings).presenterSettings(calendar: Progression.calendar).asksBeforeGoingUp)
    }
}
