import Foundation
@testable import WorkoutsCore
import Testing

// DESIGN §10.1 PresenterTests: columns per measurement, labels (§4.10), ghost
// precedence (§5.5), PREVIOUS matching (§5.4), the reason line only on Up
// next (§5.6), `rirstop` hints (§5.3), and the presentation's other parts as
// value assertions: the header, groups, the rest bar's forms, the Live
// Activity's strings, the notification copy, Finish, the entry check, and
// the History, Routines and Exercises screens.

private func d(_ text: String) -> Decimal { Decimal(string: text, locale: Locale(identifier: "en_US_POSIX"))! }

/// The 30 October session (Fully Body Collection #1 at 13:42, 82.4 kg), with
/// the three example sessions as history.
struct NextFullyBody {
    static let start = DateText.parse("2026-10-30T13:42:00Z")!
    var script: EngineScript
    let history: HistoryIndex

    init() throws {
        script = try Progression.start(at: Self.start)
        history = try Progression.history(before: Self.start)
    }

    func present(at seconds: Int = 0) throws -> SessionPresentation {
        SessionPresenter.present(try script.current, history: history, settings: Progression.settings,
                                 now: Self.start.addingTimeInterval(TimeInterval(seconds)))
    }

    func row(_ slot: String, _ index: Int) throws -> RowID {
        try script.row(slot, index)
    }

    @discardableResult
    mutating func tick(_ slot: String, _ index: Int, _ values: SetValues, at seconds: Int) throws -> SessionState {
        try script.tick(try row(slot, index), values, at: Self.start.addingTimeInterval(TimeInterval(seconds)))
    }

    @discardableResult
    mutating func send(_ event: SessionEvent, at seconds: Int) throws -> SessionState {
        try script.send(event, at: Self.start.addingTimeInterval(TimeInterval(seconds)))
    }
}

/// A small hand-made session for the shapes the examples do not have.
private func handMade(_ routine: Routine, exercises: [Exercise], at start: Date = engineEpoch) throws -> EngineScript {
    var script = EngineScript(at: start)
    try script.start(routine, exercises: exercises)
    return script
}

/// A finished session of one exercise, by hand, for history.
private func pastSession(_ id: String, at date: Date, exercise: ExerciseID, sets: [SetValues]) -> SessionRecords {
    let session = SessionID(id)
    let records = sets.enumerated().map { index, values in
        PerformedSet(id: SetID("\(id)-\(index)"), at: date.addingTimeInterval(TimeInterval(60 * (index + 1))), session: session,
                     exercise: exercise, slot: "s", order: index + 1, ordinal: index + 1, loadMode: .external, load: values.load,
                     reps: values.reps, duration: values.duration, distance: values.distance, rpe: values.rpe,
                     target: SetTarget(repsMin: 8, repsMax: 12, rpeMin: 8, rpeMax: 8))
    }
    return SessionRecords(session: TrainingSession(id: session, at: date, endsAt: date.addingTimeInterval(3600), status: .finished,
                                                   slots: [SessionSlot(key: "s", exercise: exercise)], timezone: "UTC"),
                          sets: records)
}

@Suite("Session presenter: the grid")
struct PresenterGridTests {
    @Test("Before anything is done: the chest press card's columns, labels, PREVIOUS and ghost values")
    func chestPressCard() throws {
        let session = try NextFullyBody()
        let card = try #require(try session.present().card("chest-press"))
        #expect(card.name == "Chest Press (Machine)")
        #expect(card.columns == [.set, .previous, .load, .reps, .rpe, .tick])
        #expect(card.columnTitles == ["SET", "PREVIOUS", "KG", "REPS", "RPE", ""])
        #expect(card.restChip == "Rest 1:30")
        #expect(card.progress == "0 of 2 done")
        #expect(card.loadStep == .kg(5))
        #expect(card.planNotes == nil && card.notes == nil)
        #expect(card.rests.map(\.kind) == [.setRest, .warmupRest])
        #expect(card.rests.map(\.text) == ["1:30", "0:45"])
        #expect(card.rests.map(\.defaultText) == ["Default (1:30)", "Default (0:45)"])
        let rows = card.rows
        #expect(rows.map(\.label.short) == ["W", "1", "2", "2a"])
        #expect(rows.map(\.label.spoken) == ["warm-up", "set 1", "set 2", "set 2, drop 1"])
        #expect(rows.map(\.state) == [.pending, .pending, .pending, .locked])
        #expect(rows.map { $0.previous?.text } == ["25 kg × 15", "35 kg × 15", "35 kg × 13 @ 9.5", "35 kg × 8 @ 9.5"])
        #expect(rows.map { $0.cells.map(\.text) } == [["25", "15"], ["40", "11"], ["35", "14"], ["25", GhostValues.toFailure]])
        #expect(rows.allSatisfy { $0.cells.allSatisfy { $0.isGhost } })
        // The drop goes to failure: no number to commit, so its tick waits for typed reps.
        let drop = rows[3]
        #expect(drop.cell(.reps)?.isHint == true && drop.cell(.reps)?.isMissing == true)
        #expect(drop.missing == [.reps] && drop.commitValues == nil && !drop.canTick)
        #expect(drop.kind == nil && drop.allowedKinds.isEmpty && drop.isSegment)
        #expect(rows[1].commitValues == SetValues(load: .kg(40), reps: 11))
        #expect(rows[1].canTick && rows[1].missing.isEmpty)
        #expect(rows[2].kind == .drop && rows[1].kind == .working && rows[0].kind == .warmup)
        #expect(rows[1].allowedKinds == [.warmup, .working, .failure, .drop, .myo, .myomatch])
    }

    @Test("Columns follow the measurement: a timed hold's TIME cell, a bodyweight row's reps")
    func columnsPerMeasurement() throws {
        let presentation = try NextFullyBody().present()
        let plank = try #require(presentation.card("plank"))
        #expect(plank.columnTitles == ["SET", "PREVIOUS", "TIME", ""])
        #expect(plank.rows.map { $0.cell(.time)?.text } == ["1:07", "0:58"])
        #expect(plank.rows.map(\.timer) == [.ready(target: 67), .ready(target: 58)])
        #expect(plank.rows.map { $0.previous?.text } == ["1:02", "0:53"])
        #expect(plank.restChip == "Rest 0:15")
        let crunch = try #require(presentation.card("crunch"))
        #expect(crunch.columnTitles == ["SET", "PREVIOUS", "REPS", "RPE", ""])
        #expect(crunch.restChip == "Rest Off")
        #expect(crunch.rows.first?.previous?.text == "× 15")
        let push = try #require(presentation.card("push-up"))
        #expect(push.columns == [.set, .previous, .reps, .rpe, .tick])
        #expect(push.loadStep == nil)
    }

    @Test("One side at a time: 1L and 1R, PREVIOUS by side, the second side locked behind the first")
    func sides() throws {
        let card = try #require(try NextFullyBody().present().card("bulgarian-split-squat"))
        #expect(card.rows.map(\.label.short) == ["1L", "1R", "2L", "2R"])
        #expect(card.rows.map(\.label.spoken) == ["set 1, left side", "set 1, right side", "set 2, left side", "set 2, right side"])
        #expect(card.rows.map { $0.previous?.text }
            == ["10 kg × 10 @ 8", "10 kg × 10 @ 8.5", "10 kg × 10 @ 8.5", "10 kg × 9 @ 9"])
        #expect(card.rows.map(\.state) == [.pending, .locked, .pending, .locked])
        #expect(card.rows.map { $0.cells.map(\.text) } == [["10", "10"], ["10", "10"], ["10", "9"], ["10", "9"]])
        #expect(card.rows[0].allowedKinds == [.warmup, .working, .failure])
        #expect(card.rests.map(\.kind) == [.setRest, .sideSwitch])
    }

    @Test("The second side takes the first side's load once it is done")
    func secondSideLoad() throws {
        var session = try NextFullyBody()
        try session.tick("bulgarian-split-squat", 0, kgReps(12, 9, rpe: 9), at: 60)
        let card = try #require(try session.present(at: 61).card("bulgarian-split-squat"))
        #expect(card.rows[1].state == .upNext)
        #expect(card.rows[1].cells.map(\.text) == ["12", "10"])
        #expect(card.rows[1].reason == "Last time 10 kg × 10 at RPE 8.5; try 10 kg × 10")
    }

    @Test("Ghost precedence: typed beats the suggestion, the suggestion beats the target")
    func ghostPrecedence() throws {
        var session = try NextFullyBody()
        let row = try session.row("chest-press", 1)
        var presented = try #require(try session.present().row(row))
        // The target says 35 kg × 10; the suggestion's 40 kg × 11 wins.
        #expect(try session.script.current.rows[row]?.target?.repsMin == 10)
        #expect(presented.cells.map(\.text) == ["40", "11"])
        try session.send(.editSet(.init(row: row, values: SetValuesPatch(load: .set(.kg(d("37.5")))))), at: 10)
        presented = try #require(try session.present(at: 10).row(row))
        let load = try #require(presented.cell(.load))
        #expect(load.text == "37.5" && !load.isGhost && load.entered == "37.5")
        #expect(presented.cell(.reps)?.text == "11" && presented.cell(.reps)?.isGhost == true)
        #expect(presented.commitValues == SetValues(load: .kg(d("37.5")), reps: 11))
        // A load typed in another unit than the column's shows its unit.
        try session.send(.editSet(.init(row: row, values: SetValuesPatch(load: .set(.lb(90))))), at: 11)
        let pounds = try #require(try session.present(at: 11).row(row)?.cell(.load))
        #expect(pounds.text == "90 lb" && pounds.entered == "90")
    }

    @Test("Ghost precedence: the target when there is no suggestion")
    func ghostFromTarget() throws {
        // On 27 October the chest press had no history: its warm-up shows the plan.
        let start = DateText.parse("2026-10-27T13:30:00Z")!
        let history = try Progression.history(before: start)
        let script = try Progression.start(at: start)
        let presentation = SessionPresenter.present(try script.current, history: history, settings: Progression.settings, now: start)
        let warmup = try #require(presentation.row(try script.row("chest-press", 0)))
        #expect(warmup.suggestion == nil)
        #expect(warmup.previous == nil)
        #expect(warmup.cells.map(\.text) == ["25", "15"])
        #expect(warmup.cells.allSatisfy { $0.isGhost })
    }

    @Test("Ghost precedence: PREVIOUS when neither the suggestion nor the target has the value")
    func ghostFromPrevious() throws {
        let carry = EngineCatalog.carry
        let routine = EngineCatalog.routine([EngineCatalog.slot("s", carry, [PlannedSet(key: "s1"), PlannedSet(key: "s2", distance: Length(metres: 50))])])
        let past = pastSession("past", at: engineEpoch.addingTimeInterval(-3 * 86400), exercise: carry.id,
                               sets: [SetValues(load: .kg(24), duration: 30, distance: Length(metres: 30)),
                                      SetValues(load: .kg(24), duration: 32, distance: Length(metres: 30))])
        let script = try handMade(routine, exercises: [carry])
        let presentation = SessionPresenter.present(try script.current, history: HistoryIndex([past]),
                                                    settings: Progression.settings, now: engineEpoch)
        let rows = try #require(presentation.card("s")).rows
        #expect(rows[0].previous?.text == "24 kg for 30 m")
        #expect(rows[0].cells.map(\.text) == ["24", "30"])
        // A planned distance beats PREVIOUS.
        #expect(rows[1].cells.map(\.text) == ["24", "50"])
        #expect(rows[0].timer == .ready(target: nil))
    }

    @Test("PREVIOUS: a copy carries a rated RPE as copied; the second slot of an exercise reads last time's second slot")
    func previousMatching() throws {
        var session = try NextFullyBody()
        let row = try #require(try session.present().row(try session.row("chest-press", 2)))
        let previous = try #require(row.previous)
        #expect(previous.values.rpe == d("9.5"))
        #expect(previous.copy == SetValuesPatch(load: .set(.kg(35)), reps: .set(13), rpe: .set(d("9.5")), rpeMethod: .copied))
        #expect(previous.date == DateText.parse("2026-10-27T13:30:00Z"))
        #expect(!previous.deload)
        // Last time had one chest press slot, so a second one today has no PREVIOUS.
        let exercise = try #require(try EngineFixtures.exercises()["chest-press-machine"])
        try session.send(.addExercise(.init(slot: "chest-press-2", exercise: exercise, roles: [.working],
                                            rowIDs: session.script.rowIDs(1))), at: 5)
        let second = try #require(try session.present(at: 5).card("chest-press-2"))
        #expect(second.rows.map { $0.previous?.text } == [nil])
        #expect(second.rows.first?.label.short == "1")
    }

    @Test("The reason line shows under the Up next row only; other rows keep their text for the popover")
    func reasonOnlyOnUpNext() throws {
        let presentation = try NextFullyBody().present()
        let rows = presentation.cards.flatMap(\.rows)
        let reasons = rows.filter { $0.reason != nil }
        #expect(reasons.map(\.id) == [presentation.upNext?.row])
        #expect(reasons.first?.reason == "Warm-up, as last time: 40 kg × 12")
        #expect(reasons.first?.state == .upNext)
        #expect(rows.count { $0.suggestion != nil } > 30)
        #expect(presentation.row(try NextFullyBody().row("chest-press", 1))?.suggestion?.text
            == "Last time 35 kg × 15, top of range; try 40 kg × 11")
    }

    @Test("A rirstop set shows its reps as a hint, outlined, and the tick waits for the reps done")
    func rirstopHint() throws {
        var session = try NextFullyBody()
        let row = try session.row("push-up", 0)
        var presented = try #require(try session.present().row(row))
        #expect(presented.cell(.reps)?.text == "≈15")
        #expect(presented.commitValues == nil && !presented.canTick)
        try session.send(.editSet(.init(row: row, values: SetValuesPatch(reps: .set(13)))), at: 10)
        presented = try #require(try session.present(at: 10).row(row))
        #expect(presented.cell(.reps)?.text == "13" && presented.cell(.reps)?.isGhost == false)
        #expect(presented.commitValues == SetValues(reps: 13))
        #expect(presented.canTick)
    }
}

@Suite("Session presenter: header, groups, Up next")
struct PresenterSessionTests {
    @Test("The header, the superset block, Up next and the Live Activity before anything is done")
    func start() throws {
        let session = try NextFullyBody()
        let presentation = try session.present()
        #expect(presentation.title == "Fully Body Collection #1")
        #expect(presentation.header.durationText == "0:00")
        #expect(presentation.header.volumeText == "0 kg")
        #expect(presentation.header.setsText == "0 of 42")
        #expect(presentation.header.clockStart == NextFullyBody.start)
        let group = try #require(presentation.blocks.first { $0.group != nil })
        #expect(group.group?.title == "Superset A: 2 rounds, 1:00 after each round")
        #expect(group.group?.colorIndex == 0)
        #expect(group.cards.map(\.groupLabel) == ["A1", "A2"])
        #expect(group.cards.map(\.restChip) == ["Then 0:00 to A2", "Round rest 1:00"])
        #expect(group.cards.map(\.name) == ["Glute Bridge", "Bird Dog"])
        #expect(presentation.blocks.count == 20)
        let upNext = try #require(presentation.upNext)
        #expect(upNext.row == (try session.row("leg-press", 0)))
        #expect(upNext.line == "Next: Leg Press (Machine), warm-up: 40 kg × 12")
        #expect(upNext.values == "40 kg × 12")
        #expect(presentation.restBar == .upNext("Next: Leg Press (Machine), warm-up: 40 kg × 12"))
        #expect(presentation.liveActivity == LiveActivityContent(
            title: "Fully Body Collection #1", clockStart: NextFullyBody.start, clockPausedAt: nil,
            exercise: "Leg Press (Machine)", groupLabel: nil, next: "Warm-up: 40 kg × 12", nextRowID: upNext.row,
            countdown: nil, setsDone: 0, setsPlanned: 42
        ))
        #expect(presentation.notification == nil)
        #expect(presentation.accessory == AccessoryPresentation(title: "Fully Body Collection #1",
                                                                detail: "Leg Press (Machine), warm-up", isPaused: false,
                                                                restEndsAt: nil))
        #expect(presentation.summary == "Fully Body Collection #1, started 13:42, 0 sets done.")
        #expect(presentation.nextChange == nil)
        #expect(!presentation.allDone)
    }

    @Test("The header counts working sets and volume, and stops while paused")
    func headerAndPause() throws {
        var session = try NextFullyBody()
        try session.tick("chest-press", 0, kgReps(25, 15), at: 60)
        try session.tick("chest-press", 1, kgReps(40, 11, rpe: 9), at: 180)
        try session.send(.pauseClock, at: 200)
        var presentation = try session.present(at: 300)
        #expect(presentation.header.setsText == "1 of 42")
        #expect(presentation.header.volumeText == "440 kg")
        #expect(presentation.header.isPaused && presentation.header.durationText == "Paused")
        #expect(presentation.header.elapsed == 200)
        #expect(presentation.liveActivity.clockPausedAt == NextFullyBody.start.addingTimeInterval(200))
        #expect(presentation.accessory.isPaused)
        try session.send(.resumeClock, at: 260)
        presentation = try session.present(at: 300)
        #expect(presentation.header.clockStart == NextFullyBody.start.addingTimeInterval(60))
        #expect(presentation.header.durationText == "4:00")
        #expect(presentation.summary == "Fully Body Collection #1, started 13:42, 2 sets done.")
        #expect(try #require(presentation.card("chest-press")).progress == "1 of 2 done")
        #expect(presentation.card("chest-press")?.removeQuestion == "Remove Chest Press (Machine) and its 2 done sets?")
        #expect(presentation.card("leg-press")?.removeQuestion == nil)
        #expect(presentation.discardQuestion == "Discard this session? 2 done sets will be deleted.")
        #expect(presentation.discardBeforeStartQuestion == "Discard 2 sets?")
    }

    @Test("Group colours go round a palette of four, in display order")
    func groupColours() throws {
        let press = EngineCatalog.press, row = EngineCatalog.row, curl = EngineCatalog.curl, raise = EngineCatalog.raise
        let routine = EngineCatalog.routine(
            [EngineCatalog.slot("p", press, EngineCatalog.sets(1), group: "b"), EngineCatalog.slot("r", row, EngineCatalog.sets(1), group: "b"),
             EngineCatalog.slot("c", curl, EngineCatalog.sets(1), group: "a"), EngineCatalog.slot("l", raise, EngineCatalog.sets(1), group: "a")],
            groups: [RoutineGroup(key: "a", groupKind: .superset), RoutineGroup(key: "b", groupKind: .paired, pairing: .antagonist)]
        )
        let script = try handMade(routine, exercises: [press, row, curl, raise])
        let presentation = SessionPresenter.present(try script.current, history: HistoryIndex(), settings: Progression.settings,
                                                    now: engineEpoch)
        #expect(presentation.blocks.compactMap(\.group).map(\.colorIndex) == [0, 1])
        #expect(presentation.blocks.compactMap(\.group).map(\.title)
            == ["Paired sets B: 1 round, 2:30 after each round", "Superset A: 1 round, 1:30 after each round"])
        // Antagonists move on in 15 s (§4.7, the pairing before the kind).
        #expect(presentation.blocks.first?.cards.map(\.restChip) == ["Then 0:15 to B2", "Round rest 2:30"])
    }
}

@Suite("Session presenter: rests, timers and the Live Activity")
struct PresenterRestTests {
    @Test("A warm-up rest: counting, then Rest over for ten seconds, then the Up next chip")
    func restForms() throws {
        var session = try NextFullyBody()
        try session.tick("chest-press", 0, kgReps(25, 15), at: 60)
        let end = NextFullyBody.start.addingTimeInterval(105)
        let line = "Next: Chest Press (Machine), set 1: 40 kg × 11"
        var presentation = try session.present(at: 70)
        #expect(presentation.restBar == .resting(RestCountdownPresentation(
            label: "Warm-up rest", slot: .warmupRest, startedAt: NextFullyBody.start.addingTimeInterval(60), endsAt: end,
            next: line, notifies: true
        )))
        #expect(presentation.notification == CountdownNotification(title: "Rest over", body: line, fireDate: end))
        #expect(presentation.liveActivity.countdown == LiveActivityCountdown(
            kind: .rest, label: "Warm-up rest", startedAt: NextFullyBody.start.addingTimeInterval(60), endsAt: end))
        #expect(presentation.liveActivity.next == "Set 1: 40 kg × 11")
        #expect(presentation.liveActivity.exercise == "Chest Press (Machine)")
        #expect(presentation.accessory.restEndsAt == end)
        #expect(presentation.accessory.detail == "Chest Press (Machine), set 1")
        #expect(presentation.nextChange == end)
        #expect(presentation.upNext?.reason == "Last time 35 kg × 15, top of range; try 40 kg × 11")

        presentation = try session.present(at: 108)
        #expect(presentation.restBar == .restOver("Rest over. Go: set 1, 40 kg × 11"))
        #expect(presentation.nextChange == end.addingTimeInterval(10))
        #expect(presentation.accessory.restEndsAt == nil)

        presentation = try session.present(at: 120)
        #expect(presentation.restBar == .upNext(line))
        #expect(presentation.nextChange == nil)
    }

    @Test("A superset: Go with no timer inside the round, then the round rest names the round")
    func supersetRests() throws {
        var session = try NextFullyBody()
        try session.tick("glute-bridge", 0, SetValues(reps: 16), at: 60)
        var presentation = try session.present(at: 61)
        #expect(presentation.restBar == .go("Go: Bird Dog (A2), round 1"))
        #expect(presentation.liveActivity.countdown == nil)
        #expect(presentation.notification == nil)
        #expect(presentation.upNext?.line == "Next: Bird Dog (A2), set 1: × 11")
        #expect(presentation.liveActivity.groupLabel == "A2")

        try session.tick("bird-dog", 0, SetValues(reps: 10), at: 120)
        presentation = try session.present(at: 121)
        let end = NextFullyBody.start.addingTimeInterval(180)
        guard case let .resting(countdown) = presentation.restBar else {
            Issue.record("expected a countdown, got \(presentation.restBar)")
            return
        }
        #expect(countdown.label == "Round rest" && countdown.slot == .roundRest && countdown.endsAt == end)
        #expect(presentation.notification == CountdownNotification(title: "Round 2", body: "Next: Glute Bridge (A1), set 2: × 16",
                                                                   fireDate: end))
        #expect(presentation.liveActivity.countdown?.kind == .roundRest)
        #expect(presentation.liveActivity.countdown?.label == "Round rest")
    }

    @Test("A transition names where to go, and notifies only past 20 s")
    func transitions() throws {
        let press = EngineCatalog.press, row = EngineCatalog.row
        let routine = EngineCatalog.routine(
            [EngineCatalog.slot("p", press, EngineCatalog.sets(2), group: "a", transition: 30),
             EngineCatalog.slot("r", row, EngineCatalog.sets(2), group: "a")],
            groups: [RoutineGroup(key: "a", groupKind: .superset)]
        )
        var script = try handMade(routine, exercises: [press, row])
        try script.tick(try script.row("p", 0), kgReps(50, 10), at: engineEpoch.addingTimeInterval(60))
        let presentation = SessionPresenter.present(try script.current, history: HistoryIndex(), settings: Progression.settings,
                                                    now: engineEpoch.addingTimeInterval(65))
        guard case let .resting(countdown) = presentation.restBar else {
            Issue.record("expected a countdown, got \(presentation.restBar)")
            return
        }
        #expect(countdown.label == "Go: Row (A2) in")
        #expect(countdown.slot == .transition && countdown.notifies)
        #expect(presentation.liveActivity.countdown?.label == "Go: Row (A2)")
        #expect(presentation.liveActivity.countdown?.kind == .transition)
        #expect(presentation.notification?.title == "Go")
        #expect(presentation.notification?.body == "Next: Row (A2), round 1: 50 kg × 8")
    }

    @Test("A timed set: the bar's Stop, the activity's countdown, the target's alert, Stop's values")
    func timedSet() throws {
        var session = try NextFullyBody()
        let plank = try session.row("plank", 0)
        try session.send(.startTimedSet(row: plank, target: 67), at: 300)
        let presentation = try session.present(at: 310)
        let started = NextFullyBody.start.addingTimeInterval(300)
        let end = started.addingTimeInterval(67)
        #expect(presentation.restBar == .timedSet(TimedSetBar(row: plank, label: "Plank, set 1", startedAt: started, endsAt: end)))
        #expect(presentation.timedRow == plank)
        #expect(presentation.liveActivity.nextRowID == plank)
        #expect(presentation.liveActivity.countdown == LiveActivityCountdown(kind: .timedSet, label: "Plank, set 1",
                                                                              startedAt: started, endsAt: end))
        #expect(presentation.notification == CountdownNotification(title: "Time", body: "Plank, set 1: 1:07 reached", fireDate: end))
        let row = try #require(presentation.row(plank))
        #expect(row.state == .timing)
        #expect(row.timer == .running(startedAt: started, endsAt: end))
        #expect(row.stopValues == SetValues())
        #expect(row.canTick)
        // Another row cannot be ticked while the timer runs.
        #expect(presentation.row(try session.row("chest-press", 1))?.canTick == false)
    }

    @Test("The standalone timer: Timer in the bar and on the Lock Screen, Timer done with no body")
    func manualTimer() throws {
        var session = try NextFullyBody()
        try session.send(.startRest(seconds: 90), at: 10)
        let presentation = try session.present(at: 20)
        guard case let .resting(countdown) = presentation.restBar else {
            Issue.record("expected a countdown, got \(presentation.restBar)")
            return
        }
        #expect(countdown.label == "Timer" && countdown.isManual)
        #expect(presentation.liveActivity.countdown?.kind == .manual)
        #expect(presentation.notification == CountdownNotification(title: "Timer done", body: nil,
                                                                   fireDate: NextFullyBody.start.addingTimeInterval(100)))
    }

    @Test("All sets done: no Up next, Finish in the bar, and the activity says so")
    func allDone() throws {
        let routine = EngineCatalog.routine([EngineCatalog.slot("p", EngineCatalog.press, EngineCatalog.sets(1))])
        var script = try handMade(routine, exercises: [EngineCatalog.press])
        try script.tick(try script.row("p", 0), kgReps(50, 10), at: engineEpoch.addingTimeInterval(60))
        let presentation = SessionPresenter.present(try script.current, history: HistoryIndex(), settings: Progression.settings,
                                                    now: engineEpoch.addingTimeInterval(61))
        #expect(presentation.upNext == nil && presentation.allDone)
        #expect(presentation.restBar == .allDone)
        #expect(presentation.liveActivity.next == "All sets done")
        #expect(presentation.liveActivity.exercise == "")
        #expect(presentation.liveActivity.nextRowID == nil)
        #expect(presentation.accessory.detail == nil)
        #expect(presentation.unticked.isEmpty)
        #expect(presentation.title == "Test routine")
    }

    @Test("Saved: the activity's last words after Finish")
    func finishedSummary() throws {
        let records = try EngineFixtures.session("sessions", EngineFixtures.fullyBody)
        #expect(SessionPresenter.finishedSummary(records) == "Saved: 46 min, 18 sets")
    }
}

@Suite("Finish, the entry check, and what the controller reads")
struct PresenterControllerTests {
    @Test("Finish: the unticked rows' shown values, the summary rows, the dialog's title")
    func finish() throws {
        var session = try NextFullyBody()
        try session.tick("chest-press", 0, kgReps(25, 15), at: 60)
        try session.tick("chest-press", 1, kgReps(40, 11, rpe: 9), at: 180)
        let presentation = try session.present(at: 200)
        let unticked = Dictionary(uniqueKeysWithValues: presentation.unticked.map { ($0.row, $0.values) })
        #expect(unticked.count == 53)
        // Set 2 follows set 1's 40 kg × 11 at RPE 9 in the session.
        #expect(unticked[try session.row("chest-press", 2)] == .some(SetValues(load: .kg(40), reps: 11)))
        #expect(unticked[try session.row("push-up", 0)] == .some(nil))
        let end = NextFullyBody.start.addingTimeInterval(47 * 60 + 12)
        let summary = FinishPresenter.summary(try session.script.current, presentation: presentation, policy: .drop, end: end,
                                              healthAllowed: true)
        #expect(summary.durationText == "47:12")
        #expect(summary.rows == [LabelledValue("Working sets", "1"), LabelledValue("Volume", "440 kg"),
                                 LabelledValue("Exercises", "1 of 21")])
        #expect(summary.untickedTitle == "53 sets are not ticked")
        #expect(summary.savedLine == "Saved to this iPhone and to Apple Health.")
        #expect(summary.earliestEnd == NextFullyBody.start.addingTimeInterval(180))
        #expect(FinishPresenter.summary(try session.script.current, presentation: presentation, policy: .drop, end: end,
                                        healthAllowed: false).savedLine
            == "Apple Health access is off; saved to this iPhone.")
        // An end before the last tick is clamped to it.
        #expect(FinishPresenter.summary(try session.script.current, presentation: presentation, policy: .markDone,
                                        end: NextFullyBody.start, healthAllowed: true).end
            == NextFullyBody.start.addingTimeInterval(180))
        guard case let .markDone(marks) = FinishPresenter.markDone(presentation, recordID: { SetID("r-\($0.rawValue)") }) else {
            Issue.record("expected markDone")
            return
        }
        #expect(marks.count == presentation.unticked.count { $0.values != nil })
    }

    @Test("The entry check: 500 kg × 8 asks, offers 50 kg, keeps or edits; a near set passes")
    func entryCheck() throws {
        let press = EngineCatalog.press
        let past = (1 ... 3).map { day in
            pastSession("p\(day)", at: engineEpoch.addingTimeInterval(TimeInterval(-day * 7 * 86400)), exercise: press.id,
                        sets: [kgReps(35, 12, rpe: 8)])
        }
        let routine = EngineCatalog.routine([EngineCatalog.slot("p", press, EngineCatalog.sets(2) + [PlannedSet(key: "w", role: .warmup)])])
        let script = try handMade(routine, exercises: [press])
        let state = try script.current
        let row = try script.row("p", 1)
        let warning = try #require(EntryCheck.warning(for: row, values: kgReps(500, 8), in: state, history: HistoryIndex(past),
                                                      now: engineEpoch, calendar: Progression.calendar))
        #expect(warning.title == "500 kg × 8 is far from your recent sets")
        #expect(warning.message == "Recently about 35 kg × 12.")
        #expect(warning.useTitle == "Use 50 kg")
        #expect(warning.keepTitle == "Keep 500 kg")
        #expect(warning.editTitle == "Edit")
        #expect(warning.corrected(kgReps(500, 8), measurement: .weightreps) == kgReps(50, 8))
        #expect(EntryCheck.warning(for: row, values: kgReps(d("37.5"), 10), in: state, history: HistoryIndex(past),
                                   now: engineEpoch, calendar: Progression.calendar) == nil)
        // Too little history, or a warm-up: no question.
        #expect(EntryCheck.warning(for: row, values: kgReps(500, 8), in: state, history: HistoryIndex(Array(past.prefix(2))),
                                   now: engineEpoch, calendar: Progression.calendar) == nil)
        #expect(EntryCheck.warning(for: try script.row("p", 0), values: kgReps(500, 8), in: state, history: HistoryIndex(past),
                                   now: engineEpoch, calendar: Progression.calendar) == nil)
        // A load no tenth or tenfold fixes: no Use button.
        let odd = try #require(EntryCheck.warning(for: row, values: kgReps(200, 8), in: state, history: HistoryIndex(past),
                                                  now: engineEpoch, calendar: Progression.calendar))
        #expect(odd.useTitle == nil && odd.correctedLoad == nil && odd.keepTitle == "Keep 200 kg")
    }

    /// No dialog can ask from the Lock Screen: a typo the entry check would
    /// question must not be committed by the Live Activity's Done.
    @Test("The Live Activity offers Done only for values the entry check lets through")
    func doneNeedsTheEntryCheck() throws {
        let press = EngineCatalog.press
        let past = (1 ... 3).map { day in
            pastSession("p\(day)", at: engineEpoch.addingTimeInterval(TimeInterval(-day * 7 * 86400)), exercise: press.id,
                        sets: [kgReps(35, 12, rpe: 8)])
        }
        let routine = EngineCatalog.routine([EngineCatalog.slot("p", press, EngineCatalog.sets(2, load: 35))])
        var script = try handMade(routine, exercises: [press])
        let row = try script.row("p", 0)
        func activity() throws -> LiveActivityContent {
            SessionPresenter.present(try script.current, history: HistoryIndex(past), settings: Progression.settings,
                                     now: engineEpoch).liveActivity
        }
        try script.send(.touchRow(row: row))
        #expect(try activity().nextRowID == row)
        #expect(try activity().canComplete)
        try script.send(.editSet(.init(row: row, values: SetValuesPatch(load: .set(.kg(400))))))
        #expect(try activity().nextRowID == row)
        #expect(try !activity().canComplete)
    }

    @Test("No lighter load for a drop: the controller's flag for the tick")
    func noLighterLoad() throws {
        let session = try NextFullyBody()
        let state = try session.script.current
        let set2 = try session.row("chest-press", 2)
        #expect(SuggestionInputs.noLighterLoad(afterTicking: set2, with: kgReps(5, 12), in: state, history: session.history,
                                               now: NextFullyBody.start, calendar: Progression.calendar))
        #expect(!SuggestionInputs.noLighterLoad(afterTicking: set2, with: kgReps(35, 12), in: state, history: session.history,
                                                now: NextFullyBody.start, calendar: Progression.calendar))
        #expect(!SuggestionInputs.noLighterLoad(afterTicking: try session.row("chest-press", 1), with: kgReps(5, 12), in: state,
                                                history: session.history, now: NextFullyBody.start, calendar: Progression.calendar))
    }

    @Test("Last time's shape: the weaker side, an added exercise's sets and rests")
    func lastTime() throws {
        let history = try Progression.history(before: NextFullyBody.start)
        // 27 October's split squat: left 10 and 10, right 10 and 9.
        #expect(Previous.weakerSide(of: "bulgarian-split-squat-dumbbell", history: history) == .right)
        #expect(Previous.weakerSide(of: "chest-press-machine", history: history) == nil)
        let shape = Previous.lastShape(of: "chest-press-machine", history: history)
        #expect(shape.roles == [.warmup, .working, .working])
        #expect(shape.rests == SlotRests())
        #expect(Previous.lastShape(of: "never-done", history: history).roles == [.working])
    }

    @Test("The suggestion's history: newest first, six at most, within 180 days, no deload")
    func historySelection() throws {
        let press = EngineCatalog.press
        var sessions = (1 ... 8).map { week in
            pastSession("w\(week)", at: engineEpoch.addingTimeInterval(TimeInterval(-week * 7 * 86400)), exercise: press.id,
                        sets: [kgReps(35, 12, rpe: 8)])
        }
        sessions[0].session.deload = true
        sessions.append(pastSession("old", at: engineEpoch.addingTimeInterval(-200 * 86400), exercise: press.id,
                                    sets: [kgReps(30, 12)]))
        let config = SuggestionInputs.config(for: press, defaultUnit: .kg)
        let past = SuggestionInputs.history(of: press, config: config, index: HistoryIndex(sessions), excluding: nil,
                                            now: engineEpoch, calendar: Progression.calendar)
        #expect(past.map(\.start) == (2 ... 7).map { engineEpoch.addingTimeInterval(TimeInterval(-$0 * 7 * 86400)) })
    }

    @Test("Exercise settings: the unit and grid from the exercise, else the defaults by equipment")
    func exerciseConfig() throws {
        let exercises = try EngineFixtures.exercises()
        let chest = SuggestionInputs.config(for: try #require(exercises["chest-press-machine"]), defaultUnit: .kg)
        #expect(chest.unit == .kg && chest.mode == .external && chest.base == 0 && chest.step == 5)
        let thrust = SuggestionInputs.config(for: try #require(exercises["hip-thrust-barbell"]), defaultUnit: .lb)
        #expect(thrust.unit == .kg && thrust.base == 20 && thrust.step == 2.5)
        // The cable stacks name a 2.5 kg step (the 5 kg default jumped the Pallof press from 10 to 15 kg).
        let pushdown = SuggestionInputs.config(for: try #require(exercises["triceps-pushdown"]), defaultUnit: .lb)
        #expect(pushdown.unit == .kg && pushdown.base == 0 && pushdown.step == 2.5)
        let pallof = SuggestionInputs.config(for: try #require(exercises["cable-core-pallof-press"]), defaultUnit: .kg)
        #expect(pallof.unit == .kg && pallof.step == 2.5)
        // A cable exercise naming no step takes 03 §9.1's default in the owner's unit.
        let cable = SuggestionInputs.config(for: Exercise(id: "cable", name: "Cable", measurement: .weightreps,
                                                          equipment: .cable), defaultUnit: .lb)
        #expect(cable.unit == .lb && cable.base == 0 && cable.step == 10)
        let push = SuggestionInputs.config(for: try #require(exercises["push-up"]), defaultUnit: .kg)
        #expect(push.mode == .bodyweightAdded && !push.canAddLoad && push.bodyShare == 0.65)
        let plank = SuggestionInputs.config(for: try #require(exercises["plank"]), defaultUnit: .kg)
        #expect(plank.mode == .none && plank.step == nil)
        let raise = SuggestionInputs.config(for: try #require(exercises["lateral-raise-dumbbell"]), defaultUnit: .kg)
        #expect(raise.step == 2 && raise.dropPercent == 0.2 && raise.myoStopRule == .repdrop && raise.myoMaxMiniSets == 5)
        let assisted = SuggestionInputs.config(for: try #require(exercises["assisted-pull-up-machine"]), defaultUnit: .kg)
        #expect(assisted.mode == .bodyweightAssisted && assisted.unit == .lb && assisted.step == 10)
        let barbell = Exercise(id: "bar", name: "Bar", measurement: .weightreps, equipment: .barbell)
        #expect(SuggestionInputs.defaultGrid(for: barbell, unit: .lb)! == (45, 5))
        #expect(SuggestionInputs.defaultGrid(for: Exercise(id: "band", name: "Band", measurement: .weightreps,
                                                           equipment: .resistanceband), unit: .kg) == nil)
        #expect(SuggestionInputs.defaultGrid(for: Exercise(id: "dip", name: "Dip", measurement: .weightedbodyweightreps,
                                                           equipment: .plate), unit: .kg)! == (0, 2.5))
    }
}

@Suite("History, Routines and Exercises screens")
struct PresenterScreenTests {
    static func entries() throws -> [HistoryEntry] {
        try ExampleFixtures.sessions("sessions").map {
            HistoryEntry(records: $0, local: HistoryLocal(health: HealthSaveState(status: .saved)))
        }
    }

    static func catalog() throws -> Catalog {
        try BundledCatalog().loadNow()
    }

    @Test("History: month sections, the routine's name, the local date, a labelled summary")
    func historyList() throws {
        let catalog = try Self.catalog()
        let sections = HistoryPresenter.sections(try Self.entries(), exercises: catalog.exercises, settings: Progression.settings)
        #expect(sections.map(\.title) == ["October 2026"])
        let rows = try #require(sections.first).rows
        #expect(rows.map(\.title) == ["Fully Body Collection #1", "Day 1 – Strength Foundation", "Day 1 – Strength Foundation"])
        #expect(rows.map(\.date) == ["Tue 27 Oct, 13:30", "Mon 26 Oct, 12:31", "Thu 22 Oct, 12:34"])
        // 27 Oct: chest press 1,260, split squat 780 (two dumbbells), Pallof 470,
        // push-ups 25 reps of 0.65 × 82.4 kg; the core work moves no load.
        #expect(rows.first?.summary == "46 min, 18 sets, 3,849 kg")
        // 26 Oct: the four-minute pause is not training time.
        #expect(rows[1].summary.hasPrefix("47 min, 14 sets, "))
        #expect(rows.allSatisfy { $0.health == .saved })
        #expect(HistoryPresenter.sections([], exercises: [:], settings: Progression.settings).isEmpty)
    }

    @Test("Session detail: the span, the sets with labels and rests, groups, muscles")
    func sessionDetail() throws {
        let catalog = try Self.catalog()
        let entry = try #require(try Self.entries().first { $0.id.rawValue == EngineFixtures.fullyBody })
        let detail = HistoryPresenter.detail(entry, exercises: catalog.exercises, settings: Progression.settings)
        #expect(detail.title == "Fully Body Collection #1")
        #expect(detail.when == "Tue 27 Oct 2026, 13:30 to 14:16 (46 min)")
        #expect(detail.sessionRPE == nil)
        #expect(detail.healthText == "Apple Health: saved" && !detail.canRetryHealth)
        let chest = try #require(detail.blocks.flatMap(\.exercises).first { $0.slot == "chest-press" })
        #expect(chest.sets.map(\.label.short) == ["W", "1", "2", "2a"])
        #expect(chest.sets.map(\.text) == ["25 kg × 15", "35 kg × 15", "35 kg × 13 @ 9.5", "35 kg × 8 @ 9.5"])
        #expect(chest.sets.last?.rest == "rest 0:06")
        let superset = try #require(detail.blocks.first { $0.title != nil })
        #expect(superset.title == "Superset A")
        #expect(superset.exercises.map(\.groupLabel) == ["A1", "A2"])
        #expect(superset.exercises.map(\.name) == ["Glute Bridge", "Bird Dog"])
        #expect(detail.muscles
            == "Abdominals 7, Chest 4, Glutes 4, Lower back 4, Hamstrings 2, Quadriceps 2, Shoulders 2, Triceps 2")
        let day1 = try #require(try Self.entries().first { $0.id.rawValue == EngineFixtures.day1First })
        let first = HistoryPresenter.detail(day1, exercises: catalog.exercises, settings: Progression.settings)
        #expect(first.sessionRPE == "Session RPE 7")
        #expect(first.when == "Thu 22 Oct 2026, 12:34 to 13:21 (46 min)")
        #expect(first.blocks.flatMap(\.exercises).first { $0.slot == "lateral-raise" }?.sets.map(\.label.short)
            == ["1", "2", "M", "M", "M", "M"])
        var pending = entry
        pending.local.health.status = .notSaved
        #expect(HistoryPresenter.detail(pending, exercises: catalog.exercises, settings: Progression.settings).canRetryHealth)
    }

    @Test("Routines: the folder, routines by position, their exercises, when last done")
    func routineList() throws {
        let catalog = try Self.catalog()
        let history = HistoryIndex(try ExampleFixtures.sessions("sessions"))
        let list = RoutinePresenter.list(catalog, history: history, settings: Progression.settings)
        #expect(list.sections.map(\.title) == ["Full Body 3x Week (Back-Friendly)"])
        #expect(list.sections.first?.count == "4 routines")
        let cards = try #require(list.sections.first).routines
        #expect(cards.map(\.name) == ["Fully Body Collection #1", "Day 1 – Strength Foundation", "Day 2 – Volume & Stability", "Day 3"])
        #expect(cards.map(\.lastDone) == ["Last done Tue 27 Oct", "Last done Mon 26 Oct", nil, nil])
        #expect(cards.first?.exercises.hasPrefix("Leg Press (Machine), Calf Press (Machine), Seated Leg Curl (Machine)") == true)
        #expect(list.unreadable == nil)
        var broken = catalog
        broken.unreadable = [UnreadableRecord(source: "x", kind: KindNames.v1.routine, id: "a", reason: "r"),
                             UnreadableRecord(source: "x", kind: KindNames.v1.routine, id: "b", reason: "r")]
        broken.routines["loose"] = Routine(id: "loose", name: "Loose")
        let other = RoutinePresenter.list(broken, history: history, settings: Progression.settings)
        #expect(other.unreadable == "2 routines could not be read")
        #expect(other.sections.map(\.title) == ["Full Body 3x Week (Back-Friendly)", "Other routines"])
    }

    @Test("Routine preview: warm-ups, working sets in words, methods, rests, groups")
    func routinePreview() throws {
        let catalog = try Self.catalog()
        let routine = try #require(catalog.routines["fully-body-collection-1"])
        let history = HistoryIndex(try ExampleFixtures.sessions("sessions"))
        let preview = RoutinePresenter.preview(routine, catalog: catalog, history: history, settings: Progression.settings)
        #expect(preview.lastDone == "Last done Tue 27 Oct, 46 min")
        let slots = Dictionary(uniqueKeysWithValues: preview.blocks.flatMap(\.slots).map { ($0.id, $0) })
        #expect(slots["leg-press"]?.lines == ["Warm-up: 40 kg × 12", "2 sets of 10–15 reps at RPE 8, from 90 kg", "Rest 2:00"])
        #expect(slots["chest-press"]?.lines
            == ["Warm-up: 25 kg × 15", "2 sets of 10–15 reps at RPE 9.5, from 35 kg", "Set 2 is a drop set: 1 drop", "Rest 1:30"])
        #expect(slots["plank"]?.lines == ["2 sets: hold 0:45–1:30, counted down", "Rest 0:15"])
        #expect(slots["push-up"]?.lines == ["2 sets, each until RPE 9", "Rest 1:30"])
        #expect(slots["crunch"]?.lines == ["2 sets of 12–20 reps", "Rest Off"])
        #expect(slots["bulgarian-split-squat"]?.lines
            == ["2 sets of 8–12 reps at RPE 8, from 10 kg", "Rest 2:00", "Switch sides 0:15"])
        #expect(slots["hip-thrust"]?.notes?.hasPrefix("Ribs down") == true)
        let group = try #require(preview.blocks.first { $0.header != nil })
        #expect(group.header == "Superset A, rest 1:00 after each round")
        #expect(group.slots.map(\.groupLabel) == ["A1", "A2"])
        #expect(group.slots.map(\.lines) == [["2 sets of 12–20 reps", "Then 0:00 to A2"], ["2 sets of 8–10 reps"]])
        let day1 = RoutinePresenter.preview(try #require(catalog.routines["day-1-strength-foundation"]), catalog: catalog,
                                            history: history, settings: Progression.settings)
        let day1Slots = Dictionary(uniqueKeysWithValues: day1.blocks.flatMap(\.slots).map { ($0.id, $0) })
        #expect(day1Slots["leg-press"]?.lines == ["Warm-up: 40 kg × 12", "Warm-up: 70 kg × 6",
                                                  "3 sets of 6–10 reps at RPE 8, from 100 kg", "Rest 2:30",
                                                  "Rest after warm-up 2: 1:30"])
        #expect(day1Slots["lateral-raise"]?.lines == ["1 set of 12–15 reps at RPE 8, from 6 kg",
                                                      "1 set of 12–20 reps at RPE 9, from 6 kg", "Set 2 is myo-reps", "Rest 1:30"])
        #expect(day1Slots["romanian-deadlift"]?.lines.last == "Tempo 3-1-1-0")
    }

    @Test("Exercises: A to Z, the picker's search with synonyms and Recent")
    func exerciseList() throws {
        let catalog = try Self.catalog()
        let history = HistoryIndex(try ExampleFixtures.sessions("sessions"))
        let sections = ExercisePresenter.list(catalog.exercises, history: history, settings: Progression.settings)
        #expect(sections.first?.title == "B")
        #expect(sections.flatMap(\.rows).count == 30)
        let chest = try #require(sections.flatMap(\.rows).first { $0.id == "chest-press-machine" })
        #expect(chest.equipment == "Machine" && chest.lastDone == "Last done Tue 27 Oct" && !chest.isLocal)
        func names(_ query: String) -> [String] {
            ExercisePresenter.list(catalog.exercises, history: history, query: query, settings: Progression.settings)
                .flatMap(\.rows).map(\.name)
        }
        #expect(names("rdl") == ["Romanian Deadlift (Dumbbell)", "Single Leg Romanian Deadlift (Dumbbell)"])
        #expect(names("press chest") == ["Chest Press (Machine)", "Iso-Lateral Chest Press (Machine)"])
        #expect(names("CHÉST machine") == ["Chest Press (Machine)", "Iso-Lateral Chest Press (Machine)"])
        #expect(names("db incline") == ["Incline Bench Press (Dumbbell)"])
        // The stretch's alias is "Prone press-up".
        #expect(names("press-up") == ["Lumbar Extension Stretch", "Push Up"])
        #expect(names("zzz").isEmpty)
        let picker = ExercisePresenter.picker(catalog.exercises, history: history, now: NextFullyBody.start,
                                              settings: Progression.settings)
        #expect(picker.first?.title == "Recent")
        // 27 October's exercises first, by name, then 26 October's.
        #expect(picker.first?.rows.prefix(2).map(\.name) == ["Bird Dog", "Bulgarian Split Squat (Dumbbell)"])
        #expect(picker.first?.rows.last?.name == "Seated Row (Machine)")
        #expect(picker.first?.rows.count == 14)
    }

    @Test("Exercise detail: facts stacked, the e1RM trend, history with chains on one line")
    func exerciseDetail() throws {
        let catalog = try Self.catalog()
        let history = HistoryIndex(try ExampleFixtures.sessions("sessions"))
        let chest = ExercisePresenter.detail(try #require(catalog.exercises["chest-press-machine"]), history: history,
                                             settings: Progression.settings)
        #expect(chest.facts == [LabelledValue("Equipment", "Machine"), LabelledValue("Records", "Weight and reps"),
                                LabelledValue("Muscles", "Chest; also Shoulders, Triceps"), LabelledValue("Load", "5 kg steps")])
        #expect(chest.sessions.map(\.title) == ["Tue 27 Oct, Fully Body Collection #1"])
        #expect(chest.sessions.first?.lines == ["W 25 kg × 15", "1 35 kg × 15", "2 35 kg × 13 @ 9.5, 2a 35 kg × 8 @ 9.5"])
        // 35 kg × 15 and × 13 at RPE 9.5 are too far from failure (n > 12) for the chart.
        #expect(chest.trend.metric == .oneRepMax && chest.trend.points.isEmpty)
        let legPress = ExercisePresenter.detail(try #require(catalog.exercises["leg-press-machine"]), history: history,
                                                settings: Progression.settings)
        // 22 Oct: 100 kg × 10 at RPE 8 (n = 12) gives 100 / 0.680; 26 Oct: 105 kg × 9 at RPE 8 gives 105 / 0.707.
        #expect(legPress.trend.points.map(\.text) == ["147.1 kg", "148.5 kg"])
        #expect(legPress.trend.title == "Estimated 1RM")
        #expect(legPress.trend.hasTrend(legPress.trend.points))
        #expect(legPress.trend.points(lastMonths: 3, now: NextFullyBody.start, calendar: Progression.calendar).count == 2)
        #expect(legPress.trend.points(lastMonths: 3, now: NextFullyBody.start.addingTimeInterval(120 * 86400),
                                      calendar: Progression.calendar).isEmpty)
        let plank = ExercisePresenter.trend(try #require(catalog.exercises["plank"]), history: history, settings: Progression.settings)
        #expect(plank.metric == .longestHold && plank.points.map(\.text) == ["1:02"])
        #expect(!plank.hasTrend(plank.points))
        let crunch = ExercisePresenter.trend(try #require(catalog.exercises["crunch"]), history: history, settings: Progression.settings)
        #expect(crunch.metric == .mostReps && crunch.points.map(\.text) == ["15 reps"])
        let push = ExercisePresenter.trend(try #require(catalog.exercises["push-up"]), history: history, settings: Progression.settings)
        #expect(push.metric == .oneRepMax)
        let split = ExercisePresenter.detail(try #require(catalog.exercises["bulgarian-split-squat-dumbbell"]), history: history,
                                             settings: Progression.settings)
        #expect(split.facts.contains(LabelledValue("Sides", "One side at a time")))
        #expect(split.sessions.first?.lines == ["1L 10 kg × 10 @ 8, 1R 10 kg × 10 @ 8.5", "2L 10 kg × 10 @ 8.5, 2R 10 kg × 9 @ 9"])
    }
}
