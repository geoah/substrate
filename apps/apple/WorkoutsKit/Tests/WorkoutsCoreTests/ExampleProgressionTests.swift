import Foundation
@testable import WorkoutsCore
import Testing

// DESIGN §10.3: the whole path (records, the history index, SuggestionInputs,
// suggest) over sessions.json alone, history newest first, as the examples
// README computed it. Each example session is replayed through the engine
// from its records (`ExampleReplay`); before each set's first event the
// suggestion for its row is read from the state at that moment, with only
// the sessions that started earlier as history. Every text below is quoted
// from the comments in research/workouts/substrate/examples/sessions.yaml.

/// The suggestions an example session's sets were offered, in the order done.
struct ReplayedSuggestions {
    let fixture: SessionRecords
    /// For each set, its suggestion before its first event.
    var before: [SetID: Suggestion?] = [:]
    /// For each set, the stop text its row shows right after its tick.
    var stopAfter: [SetID: String] = [:]
    /// For each set, the presentation's reason line when its row was Up next.
    var rows: [SetID: RowID] = [:]

    /// The sets of a slot in the order done.
    func sets(_ slot: String) -> [PerformedSet] {
        fixture.sets.filter { $0.slot == slot }.sorted { $0.at != $1.at ? $0.at < $1.at : $0.order < $1.order }
    }

    /// The suggestion texts of a slot's sets, in the order done; nil where none.
    func texts(_ slot: String) -> [String?] {
        sets(slot).map { before[$0.id].flatMap(\.self)?.text }
    }
}

enum Progression {
    static let london = TimeZone(identifier: "Europe/London")!

    static var calendar: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = london
        return calendar
    }

    static var settings: PresenterSettings { PresenterSettings(calendar: calendar) }

    /// The example sessions that started before `date`, as the history index holds them.
    static func history(before date: Date) throws -> HistoryIndex {
        HistoryIndex(try ExampleFixtures.sessions("sessions").filter { $0.session.at < date })
    }

    /// Replays a fixture session and reads every set's suggestion before it.
    static func replay(_ id: String, file: String = "sessions") throws -> ReplayedSuggestions {
        let fixture = try EngineFixtures.session(file, id)
        let replay = try ExampleReplay.run(fixture, finish: false)
        let events = replay.script.events
        let history = try history(before: fixture.session.at)
        var result = ReplayedSuggestions(fixture: fixture)
        for (index, envelope) in events.enumerated() {
            let tick: SessionEvent.CompleteSet
            switch envelope.event {
            case let .completeSet(value), let .stopTimedSet(value): tick = value
            default: continue
            }
            // The state before the row's first event: its timer's start or its tick.
            let first = events.firstIndex { envelope in
                switch envelope.event {
                case let .startTimedSet(row, _): row == tick.row
                case let .completeSet(value), let .stopTimedSet(value): value.row == tick.row
                default: false
                }
            } ?? index
            let before = try SessionEngine.fold(Array(events[..<first]))
            result.before[tick.recordID] = .some(SuggestionInputs.suggestion(
                for: tick.row, in: before, history: history, now: events[first].at, calendar: calendar))
            result.rows[tick.recordID] = tick.row
            let after = try SessionEngine.fold(Array(events[...index]))
            let presentation = SessionPresenter.present(after, history: history, settings: settings, now: envelope.at)
            if let stop = presentation.row(tick.row)?.stopText { result.stopAfter[tick.recordID] = stop }
        }
        return result
    }
}

@Suite("Example progression, DESIGN §10.3")
struct ExampleProgressionTests {
    @Test("Thu 22 Oct, no history: cold starts, then in-session changes and the myo stop")
    func firstDayOne() throws {
        let replay = try Progression.replay(EngineFixtures.day1First)
        #expect(replay.texts("leg-press") == [
            nil, nil,
            "First time; routine load, try 100 kg × 6",
            "Set 1: 100 kg × 10 at RPE 7.5; try 100 kg × 10",
            "Set 2: 100 kg × 10 at RPE 8; try 100 kg × 10",
        ])
        #expect(replay.texts("iso-lateral-chest-press") == [
            "First time; routine load, try 20 kg × 6",
            "Set 1: 20 kg × 10 at RPE 7, easier than RPE 8; try 22.5 kg × 8",
            "Set 2: 22.5 kg × 9 at RPE 8; try 22.5 kg × 9",
        ])
        #expect(replay.texts("seated-row") == [
            "First time; routine load, try 45 kg × 8",
            "Set 1: 45 kg × 12 at RPE 8; try 45 kg × 12",
            "Set 2: 45 kg × 11 at RPE 8.5; try 45 kg × 10",
        ])
        #expect(replay.texts("lateral-raise") == [
            "First time; routine load, try 6 kg × 12",
            "Set 1: 6 kg × 13 at RPE 7; try 6 kg × 15",
            "Mini-set 1: 6 kg × 4",
            "Mini-set 2: 6 kg × 4",
            "Mini-set 3: 6 kg × 4",
            "Mini-set 4: 6 kg × 4",
        ])
        let lastMini = try #require(replay.sets("lateral-raise").last)
        #expect(replay.stopAfter[lastMini.id] == "Stop: mini-set 4 got 3, first was 4")
        // No other set of the session shows a stop.
        #expect(replay.stopAfter.count == 1)
        #expect(replay.texts("romanian-deadlift") == [
            "First time; routine load, try 16 kg × 8",
            "Set 1: 16 kg × 7, harder than RPE 7; try 12 kg × 10",
            "Set 2: 12 kg × 10 at RPE 7; try 12 kg × 10",
        ])
    }

    @Test("Mon 26 Oct, from 22 Oct: warm-ups as last time, history up a step, add a rep, cut the load")
    func secondDayOne() throws {
        let replay = try Progression.replay(EngineFixtures.day1Second)
        #expect(replay.texts("leg-press") == [
            "Warm-up, as last time: 40 kg × 12",
            "Warm-up, as last time: 70 kg × 6",
            "Last time 100 kg × 10 at RPE 7.5, top of range; try 105 kg × 9",
            // Set 1's RPE 8 was the plan's, pre-filled and logged untouched:
            // no rating, so the set is taken at the target's top and the
            // reason says so (the package README, "Effort provenance").
            "Set 1: 105 kg × 9, assuming RPE 8; try 105 kg × 9",
            "Set 2: 105 kg × 9 at RPE 8.5; try 105 kg × 8",
        ])
        // Set 1 was ticked at the suggested 8 and corrected to 10 before set 2.
        #expect(replay.texts("iso-lateral-chest-press") == [
            "Last time 20 kg × 10 at RPE 7, top of range; try 22.5 kg × 8",
            "Set 1: 22.5 kg × 10 at RPE 8; try 22.5 kg × 10",
            "Set 2: 22.5 kg × 9 at RPE 8.5; try 22.5 kg × 8",
        ])
        #expect(replay.texts("seated-row") == [
            "Last time 45 kg × 12 at RPE 8, top of range; try 50 kg × 9",
            "Set 1: 50 kg × 10 at RPE 8; try 50 kg × 10",
            "Set 2: 50 kg × 10 at RPE 8.5; try 50 kg × 9",
        ])
        #expect(replay.texts("lateral-raise") == [
            "Last time 6 kg × 13 at RPE 7; try 6 kg × 15",
            "Set 1: 6 kg × 14 at RPE 7; try 6 kg × 16",
            "Mini-set 1: 6 kg × 4",
            "Mini-set 2: 6 kg × 4",
            "Mini-set 3: 6 kg × 4",
            "Mini-set 4: 6 kg × 4",
            "Mini-set 5: 6 kg × 4",
        ])
        let lastMini = try #require(replay.sets("lateral-raise").last)
        #expect(replay.stopAfter[lastMini.id] == "Stop: 5 mini-sets done")
        #expect(replay.texts("romanian-deadlift") == [
            "Last time 16 kg × 7, below target; try 12 kg × 11",
            "Set 1: 12 kg × 12 at RPE 7; try 12 kg × 12",
            "Set 2: 12 kg × 12 at RPE 7.5; try 12 kg × 11",
        ])
    }

    @Test("Tue 27 Oct, Fully Body: the plank's cold start, the chest press and its drop")
    func fullyBody() throws {
        let replay = try Progression.replay(EngineFixtures.fullyBody)
        #expect(replay.texts("plank") == ["First time: hold for 0:45", "First time: hold for 0:45"])
        #expect(replay.texts("chest-press") == [
            nil,
            "First time; routine load, try 35 kg × 10",
            "Set 1: 35 kg × 15; try 35 kg × 14",
            "Drop 20% from 35 kg: 25 kg to failure",
        ])
    }

    @Test("Fri 30 Oct, before anything is done: every row of §10.3's table")
    func nextFullyBody() throws {
        let start = DateText.parse("2026-10-30T13:42:00Z")!
        let history = try Progression.history(before: start)
        var script = try Progression.start(at: start)
        var state = try script.current
        func text(_ slot: String, _ index: Int) throws -> String? {
            let row = try script.row(slot, index)
            return SessionPresenter.present(state, history: history, settings: Progression.settings, now: start)
                .row(row)?.suggestion?.text
        }
        #expect(try text("chest-press", 0) == "Warm-up, as last time: 25 kg × 15")
        #expect(try text("chest-press", 1) == "Last time 35 kg × 15, top of range; try 40 kg × 11")
        #expect(try text("chest-press", 2) == "Last time 35 kg × 13 at RPE 9.5; try 35 kg × 14")
        // The drop reads the load set 2 shows while set 2 is pending.
        #expect(try text("chest-press", 3) == "Drop 20% from 35 kg: 25 kg to failure")
        #expect(try text("plank", 0) == "Last time 1:02; try 1:07")
        #expect(try text("plank", 1) == "Last time 0:53; try 0:58")
        // One side at a time: the second side shows the first side's suggestion.
        for index in 0 ... 1 {
            #expect(try text("bulgarian-split-squat", index) == "Last time 10 kg × 10 at RPE 8.5; try 10 kg × 10")
        }
        for index in 2 ... 3 {
            #expect(try text("bulgarian-split-squat", index) == "Last time 10 kg × 9 at RPE 9; try 10 kg × 9")
        }
        #expect(try text("push-up", 0) == "Last time BW × 14 at RPE 9; try BW × 15")
        #expect(try text("push-up", 1) == "Last time BW × 11 at RPE 9; try BW × 12")

        // After set 2 at 35 kg, the drop still reads 20 % off 35 kg.
        state = try script.tick(try script.row("chest-press", 2), kgReps(35, 14, rpe: 9.5), at: start.addingTimeInterval(300))
        #expect(try text("chest-press", 3) == "Drop 20% from 35 kg: 25 kg to failure")
    }

    @Test("Fri 30 Oct: the push-up's reps are a hint the tick does not commit (rirstop)")
    func pushUpHint() throws {
        let start = DateText.parse("2026-10-30T13:42:00Z")!
        let history = try Progression.history(before: start)
        let script = try Progression.start(at: start)
        let presentation = SessionPresenter.present(try script.current, history: history, settings: Progression.settings, now: start)
        let row = try #require(presentation.row(try script.row("push-up", 0)))
        let reps = try #require(row.cell(.reps))
        #expect(reps.text == "\u{2248}15")
        #expect(reps.isGhost && reps.isHint && reps.isMissing)
        #expect(row.commitValues == nil)
        #expect(row.missing == [.reps])
        #expect(!row.canTick)
    }

    @Test("Wed 28 Oct, coverage: the back-off set backs off 10 % from the 110 kg top set (§5.3)")
    func backoff() throws {
        let replay = try Progression.replay(EngineFixtures.coverage, file: "coverage")
        let backoff = try #require(replay.sets("leg-press").first { $0.roleLabel == .backoff })
        #expect(replay.before[backoff.id].flatMap(\.self)?.text == "Back-off 10% from 110 kg: try 95 kg × 10")
        #expect(replay.before[backoff.id].flatMap(\.self)?.reason == .backoff)
    }

    @Test("Wed 28 Oct, coverage: a myo-rep match aims at the set before it, rest-pause and clusters stop only at their count")
    func coverageChains() throws {
        let replay = try Progression.replay(EngineFixtures.coverage, file: "coverage")
        // Seated leg curl set 1 went 12; the match's head did 7, so 5 remain:
        // min(t1 = 3, 5), then min(3, 2), then the total is reached.
        let curl = replay.sets("seated-leg-curl")
        #expect(curl.count == 4)
        #expect(replay.before[curl[2].id].flatMap(\.self)?.text == "Mini-set 1: 40 kg × 3")
        #expect(replay.before[curl[3].id].flatMap(\.self)?.text == "Mini-set 2: 40 kg × 2")
        #expect(replay.stopAfter[curl[3].id] == "Stop: 5 reps reached")
        // Rest-pause: two mini-sets with no planned reps, the count is open.
        let extensions = replay.sets("leg-extension")
        #expect(extensions.count == 3)
        #expect(replay.before[extensions[1].id].flatMap(\.self)?.text == "Mini-set 1: 45 kg")
        #expect(replay.stopAfter[extensions[2].id] == "Stop: 2 mini-sets done")
        // Clusters of 3 at the head's load.
        let thrust = replay.sets("hip-thrust")
        #expect(thrust.dropFirst().map { replay.before[$0.id].flatMap(\.self)?.text }
            == ["Mini-set 1: 80 kg × 3", "Mini-set 2: 80 kg × 3", "Mini-set 3: 80 kg × 3"])
    }
}

extension Progression {
    /// A session started from Fully Body Collection #1 at `start`, with the
    /// 82.4 kg body mass of 27 October.
    static func start(at start: Date) throws -> EngineScript {
        let exercises = try EngineFixtures.exercises()
        let routine = try #require(try EngineFixtures.routines()["fully-body-collection-1"])
        var script = EngineScript(at: start, seed: 30)
        try script.start(routine, exercises: routine.slots.compactMap { exercises[$0.exercise] },
                         bodyMass: .kg(Decimal(string: "82.4")!))
        return script
    }
}
