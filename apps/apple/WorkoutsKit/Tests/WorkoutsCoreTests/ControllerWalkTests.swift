import Foundation
@testable import WorkoutsCore
import Testing

// One session end to end, driven the way the app's SessionController drives
// it (SubstrateWorkouts/Sources/App/SessionController.swift), since the app target's own tests
// cannot run without a simulator: every value a tick commits comes from the
// presentation, every tick passes the entry check and `noLighterLoad`, the
// journal is written after every event, Finish writes the history file and
// the Health queue, and the next session reads what this one wrote.

/// The controller's command handling over WorkoutsKit alone: the same reads
/// from the presentation and the same events, in the same order.
struct ControllerWalk {
    var script: EngineScript
    let catalog: Catalog
    var history: HistoryIndex
    let directory: URL
    let journal: ActiveSessionJournal
    let settings = Progression.settings
    private(set) var presentation: SessionPresentation?

    init(at start: Date, history: HistoryIndex, directory: URL) throws {
        script = EngineScript(at: start, seed: 11)
        catalog = try BundledCatalog().loadNow()
        self.history = history
        self.directory = directory
        journal = ActiveSessionJournal(directory: directory, writingOptions: .atomic)
    }

    var state: SessionState { get throws { try script.current } }
    var now: Date { script.now }
    var shown: SessionPresentation { get throws { try #require(presentation) } }

    mutating func wait(_ seconds: Int) {
        script.wait(seconds)
        present()
    }

    /// `SessionController.present(now:)`.
    mutating func present() {
        presentation = script.state.map {
            SessionPresenter.present($0, catalog: catalog, history: history, settings: settings, now: script.now)
        }
    }

    /// `SessionController.apply`: the engine, the journal, the presentation.
    mutating func send(_ event: SessionEvent, sourceLocation: SourceLocation = #_sourceLocation) throws {
        try script.send(event, sourceLocation: sourceLocation)
        try journal.write(events: script.events, state: script.current)
        present()
    }

    /// `SessionController.start(_:)` for a routine.
    mutating func start(_ routine: RoutineID, bodyMass: Mass?) throws {
        let routine = try #require(catalog.routines[routine])
        var exercises: [ExerciseID: Exercise] = [:]
        for slot in routine.slots {
            if let exercise = catalog.exercises[slot.exercise] { exercises[exercise.id] = exercise }
        }
        var firstSides: [String: Side] = [:]
        for slot in routine.slots where slot.sideOrder == .weakerfirst {
            if let side = Previous.weakerSide(of: slot.exercise, history: history) { firstSides[slot.key] = side }
        }
        try script.start(routine, exercises: Array(exercises.values).sorted { $0.id < $1.id }, bodyMass: bodyMass,
                         firstSides: firstSides)
        try journal.write(events: script.events, state: script.current)
        present()
    }

    func row(_ slot: String, _ index: Int) throws -> RowID {
        try script.row(slot, index)
    }

    /// `.touchRow`: only a pending row, and only when it changes something.
    mutating func touch(_ row: RowID) throws {
        let state = try state
        let clockOpen = state.restClock.map { $0.endedAt == nil } ?? false
        guard state.rows[row]?.isPending == true, state.focus != row || clockOpen else { return }
        try send(.touchRow(row: row))
    }

    mutating func edit(_ row: RowID, _ patch: SetValuesPatch) throws {
        try send(.editSet(.init(row: row, values: patch, segmentRowIDs: script.rowIDs(1))))
    }

    /// The tick: what the row shows, through the entry check. Returns the
    /// entry check's warning instead of ticking when it asks.
    @discardableResult
    mutating func tick(_ row: RowID, sourceLocation: SourceLocation = #_sourceLocation) throws -> EntryWarning? {
        let presented = try #require(try shown.row(row), sourceLocation: sourceLocation)
        let values = try #require(presented.commitValues, "row \(presented.label.short) of \(presented.slot) cannot tick: missing \(presented.missing)",
                                  sourceLocation: sourceLocation)
        if let warning = EntryCheck.warning(for: row, values: values, in: try state, history: history, now: now,
                                            calendar: settings.calendar)
        {
            return warning
        }
        try commit(row, values: values, stop: false, sourceLocation: sourceLocation)
        return nil
    }

    /// `SessionController.commit`.
    mutating func commit(_ row: RowID, values: SetValues, stop: Bool, sourceLocation: SourceLocation = #_sourceLocation) throws {
        let noLighterLoad = SuggestionInputs.noLighterLoad(afterTicking: row, with: values, in: try state, history: history,
                                                           now: now, calendar: settings.calendar)
        let complete = SessionEvent.CompleteSet(row: row, recordID: script.setID(), values: values,
                                                segmentRowIDs: script.rowIDs(2), noLighterLoad: noLighterLoad)
        try send(stop ? .stopTimedSet(complete) : .completeSet(complete), sourceLocation: sourceLocation)
    }

    /// The Live Activity's Done: only for the row still Up next, as shown.
    mutating func liveActivityDone(_ rowID: String) throws -> Bool {
        let row = RowID(rowID)
        present()
        guard try shown.upNext?.row == row, let values = try shown.row(row)?.commitValues else { return false }
        try commit(row, values: values, stop: false)
        return true
    }

    /// Up next's row, which the Live Activity's Done would tick.
    var upNext: RowID? { presentation?.upNext?.row }
}

@Suite("A session end to end, as the controller drives it")
struct ControllerWalkTests {
    /// Fri 30 Oct 2026, 12:00 in London (GMT).
    static let friday = Date(timeIntervalSince1970: 1_793_361_600)

    static func scratchDirectory() throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("workouts-walk-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    @Test("Day 1 from Start to the next session's suggestion")
    func dayOne() throws {
        let directory = try Self.scratchDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        // The owner's history: the example sessions before Friday.
        let past = try ExampleFixtures.sessions("sessions").filter { $0.session.at < Self.friday }
        let store = HistoryStore(directory: directory, writingOptions: .atomic)
        for records in past {
            try store.write(HistoryEntry(records: records, local: HistoryLocal(health: HealthSaveState(status: .saved))))
        }
        var walk = try ControllerWalk(at: Self.friday, history: HistoryIndex(store.loadAll().entries.map(\.records)),
                                      directory: directory)

        // Start from Day 1: the cover opens at the first warm-up.
        try walk.start("day-1-strength-foundation", bodyMass: .kg(82.4))
        let warmup1 = try walk.row("leg-press", 0)
        #expect(walk.upNext == warmup1)
        #expect(try walk.shown.liveActivity.nextRowID == warmup1)
        #expect(try walk.shown.liveActivity.canComplete)
        #expect(try walk.shown.notification == nil)

        // A warm-up: its ghost values tick, then the warm-up rest runs and
        // the rest-over alert is scheduled for its end.
        try walk.tick(warmup1)
        let afterWarmup = try walk.state
        #expect(afterWarmup.rows[warmup1]?.performed?.values.load == .kg(40))
        let warmupRest = try #require(afterWarmup.countdown?.endsAt)
        #expect(try walk.shown.notification?.fireDate == warmupRest)
        #expect(try walk.shown.liveActivity.countdown?.endsAt == warmupRest)
        #expect(try walk.shown.nextChange != nil)

        // The second warm-up, then the first working set with its suggestion.
        let warmup2 = try walk.row("leg-press", 1)
        walk.wait(50)
        try walk.touch(warmup2)
        try walk.tick(warmup2)
        let working1 = try walk.row("leg-press", 2)
        walk.wait(95)
        #expect(walk.upNext == working1)
        let reason = try walk.shown.row(working1)?.reason
        #expect(reason != nil, "Up next shows its suggestion's reason")
        try walk.touch(working1)
        #expect(try walk.tick(working1) == nil)
        let workingRest = try #require(try walk.state.countdown?.endsAt)
        #expect(try walk.shown.notification?.fireDate == workingRest)

        // The phone is locked; the rest ends. The notification fired at its
        // end; on the next foreground the rest bar says it is over.
        walk.wait(Int(workingRest.timeIntervalSince(walk.now)) + 3)
        if case .restOver = try walk.shown.restBar {} else {
            Issue.record("rest bar after the end: \(String(describing: try walk.shown.restBar))")
        }
        // The presentation may still carry the past alert; the coordinator
        // schedules only one ending in the future.
        #expect(try walk.shown.notification.map { $0.fireDate <= walk.now } ?? true)

        // Skip from the Live Activity during the next rest.
        let working2 = try walk.row("leg-press", 3)
        try walk.touch(working2)
        try walk.tick(working2)
        #expect(try walk.state.countdown != nil)
        walk.wait(20)
        try walk.send(.skipRest)
        #expect(try walk.state.countdown == nil)
        #expect(try walk.shown.notification == nil)

        // A drop chain: the last leg-press set becomes a drop set in the gym.
        let working3 = try walk.row("leg-press", 4)
        try walk.send(.changeSetKind(.init(row: working3, kind: .drop, segmentRowIDs: walk.script.rowIDs(3))))
        let chain = try walk.state.chain(of: working3)
        #expect(chain.count >= 2, "a drop set shows its drop from the start")
        try walk.touch(working3)
        try walk.tick(working3)
        let drop = try #require(chain.first { !$0.isHead }?.id)
        #expect(try walk.state.countdown == nil, "no rest inside a drop chain")
        #expect(walk.upNext == drop)
        // To failure: the reps are typed (the outlined field) before the tick.
        #expect(try walk.shown.row(drop)?.missing == [.reps])
        #expect(try walk.shown.row(drop)?.canTick == false)
        // The Lock Screen offers no Done that would do nothing.
        #expect(try walk.shown.liveActivity.nextRowID == drop)
        #expect(try !walk.shown.liveActivity.canComplete)
        #expect(try !walk.liveActivityDone(drop.rawValue))
        var reps = SetValuesPatch()
        reps.reps = .set(7)
        try walk.edit(drop, reps)
        let dropValues = try #require(try walk.shown.row(drop)?.commitValues)
        #expect(try walk.shown.liveActivity.canComplete)
        #expect(dropValues.load.map { $0.value < 100 } == true, "the drop is lighter: \(String(describing: dropValues.load))")
        try walk.tick(drop)

        // A superset round: the chest press and the row become a superset.
        try walk.send(.groupExercises(slot: "seated-row", partner: "iso-lateral-chest-press", group: nil))
        let group = try #require(try walk.state.slots.first { $0.key == "seated-row" }?.group)
        #expect(try walk.shown.blocks.first { $0.group?.key == group }?.cards.map(\.slot) == ["iso-lateral-chest-press", "seated-row"])
        let press1 = try walk.row("iso-lateral-chest-press", 0)
        let row1 = try walk.row("seated-row", 0)
        walk.wait(30)
        try walk.touch(press1)
        try walk.tick(press1)
        #expect(walk.upNext == row1, "A2 follows A1")
        try walk.tick(row1)
        #expect(try walk.state.restClock?.slot == .roundRest, "round rest after the round")
        #expect(walk.upNext == (try walk.row("iso-lateral-chest-press", 1)))

        // A myo chain: the lateral raise's second set is a myo-rep set.
        walk.wait(150)
        let raise1 = try walk.row("lateral-raise", 0)
        try walk.touch(raise1)
        try walk.tick(raise1)
        walk.wait(95)
        let raise2 = try walk.row("lateral-raise", 1)
        try walk.touch(raise2)
        try walk.tick(raise2)
        var miniSets = 0
        while let next = walk.upNext, try walk.state.rows[next]?.headID == raise2, miniSets < 8 {
            walk.wait(15)
            try walk.touch(next)
            try walk.tick(next)
            miniSets += 1
        }
        // Ghost values never lose a rep, so the chain stops at its cap of five.
        #expect(miniSets == 5)
        #expect(try walk.state.chain(of: raise2).allSatisfy(\.isCompleted), "the chain stops itself")

        // A timed plank, added in the gym: Play, then Stop on the Lock Screen.
        let plank = try #require(walk.catalog.exercises["plank"])
        let shape = Previous.lastShape(of: plank.id, history: walk.history, excluding: try walk.state.session.id)
        try walk.send(.addExercise(.init(slot: "plank", exercise: plank, roles: shape.roles,
                                         rowIDs: walk.script.rowIDs(shape.roles.count), rests: shape.rests, after: nil,
                                         firstSide: nil)))
        let plankRow = try walk.row("plank", shape.roles.firstIndex(of: .working) ?? 0)
        walk.wait(90)
        guard case let .ready(target)? = try walk.shown.row(plankRow)?.timer else {
            Issue.record("the plank shows no Play: \(String(describing: try walk.shown.row(plankRow)?.timer))")
            return
        }
        try walk.send(.startTimedSet(row: plankRow, target: target))
        #expect(try walk.shown.liveActivity.nextRowID == plankRow, "Stop on the Lock Screen stops the plank")
        #expect(try walk.shown.liveActivity.canComplete, "Stop can stop the plank: only its time was missing")
        // Last time's two sets and its best hold as the target (27 Oct).
        #expect(shape.roles == [.working, .working])
        #expect(target == 67)
        #expect(try walk.shown.notification?.fireDate == walk.now.addingTimeInterval(67), "the plank alerts at its target")
        walk.wait(40)
        let stopValues = try #require(try walk.shown.row(plankRow)?.stopValues)
        try walk.commit(plankRow, values: stopValues, stop: true)
        #expect(try walk.state.rows[plankRow]?.performed?.values.duration == 40)

        // Done on the Live Activity: Up next is the plank's second set; then
        // the owner opens the Romanian deadlift and Done ticks its first set.
        walk.wait(60)
        let plank2 = try walk.row("plank", 1)
        #expect(try walk.shown.liveActivity.nextRowID == plank2)
        #expect(try walk.liveActivityDone(plank2.rawValue))
        #expect(try walk.state.rows[plank2]?.performed?.values.duration == 40)
        walk.wait(90)
        let rdl1 = try walk.row("romanian-deadlift", 0)
        try walk.touch(rdl1)
        #expect(try walk.shown.liveActivity.nextRowID == rdl1)
        #expect(try walk.liveActivityDone(rdl1.rawValue))
        #expect(try walk.state.rows[rdl1]?.isCompleted == true)
        // A second, late tap on the same Done does nothing.
        #expect(try !walk.liveActivityDone(rdl1.rawValue))

        // The journal folds back to what the controller holds (a relaunch).
        let loaded = try #require(try walk.journal.load(fold: { try SessionEngine.fold($0) }))
        #expect(loaded.state == (try walk.state))
        #expect(loaded.cacheMatched)

        // Finish: unticked rows dropped, a session RPE, notes.
        walk.wait(120)
        let state = try walk.state
        let summary = FinishPresenter.summary(state, presentation: try walk.shown, policy: .drop, end: walk.now,
                                              healthAllowed: true)
        #expect(!summary.rows.isEmpty)
        try walk.send(.finish(summary.projection.event(start: summary.start, end: walk.now.flooredToSecond, sessionRPE: 7,
                                                       notes: "Good day", utcOffset: 0)))
        let finished = try walk.state
        let records = SessionEngine.records(finished, adapter: "substrate-workouts@0.1.0")
        #expect(records.session.endsAt == walk.now.flooredToSecond)
        #expect(records.session.sessionRPE == 7)
        #expect(records.session.slots.contains { $0.key == "plank" })
        #expect(records.sets.count == state.rows.values.filter(\.isCompleted).count)
        #expect(SessionPresenter.finishedSummary(records).hasPrefix("Saved: "))

        // The history file, the journal gone, the Health queue.
        try store.write(HistoryEntry(records: records))
        try walk.journal.delete()
        #expect(try walk.journal.read() == nil)
        let queue = HealthQueueFile(directory: directory, writingOptions: .atomic)
        try queue.enqueue(records.session.id, at: walk.now)
        #expect(try queue.load().map(\.session) == [records.session.id])

        // The Health save: the workout's UUID lands in the session's file.
        let workout = UUID().uuidString
        let saved = try store.recordHealthSave(records.session.id, status: .saved, healthKitWorkout: workout, at: walk.now)
        try queue.remove(records.session.id)
        #expect(saved.records.session.healthKitWorkout == workout)
        #expect(saved.local.health.status == .saved)
        #expect(try queue.load().isEmpty)

        // History lists it first, with its sets.
        let load = store.loadAll()
        #expect(load.unreadable.isEmpty)
        let index = HistoryIndex(load.entries.map(\.records))
        let sections = HistoryPresenter.sections(load.entries, exercises: walk.catalog.exercises, settings: walk.settings)
        let first = try #require(sections.first?.rows.first)
        #expect(first.title == "Day 1 – Strength Foundation")
        #expect(first.date == "Fri 30 Oct, 12:00")
        #expect(first.health == .saved)
        let entry = try #require(load.entries.first { $0.id == records.session.id })
        let detail = HistoryPresenter.detail(entry, exercises: walk.catalog.exercises, settings: walk.settings)
        #expect(detail.blocks.flatMap(\.exercises).contains { $0.slot == "plank" })

        // The leg press's e1RM chart gains Friday's point.
        let legPress = try #require(walk.catalog.exercises["leg-press-machine"])
        let trend = ExercisePresenter.detail(legPress, history: index, settings: walk.settings).trend
        #expect(trend.metric == .oneRepMax)
        #expect(trend.points.contains { $0.session == records.session.id })
        #expect(trend.hasTrend(trend.points))

        // The next session's suggestion reads Friday's sets.
        let monday = Self.friday.addingTimeInterval(3 * 86400)
        var next = try ControllerWalk(at: monday, history: index, directory: try Self.scratchDirectory())
        defer { try? FileManager.default.removeItem(at: next.directory) }
        try next.start("day-1-strength-foundation", bodyMass: .kg(82.4))
        let nextWorking1 = try next.row("leg-press", 2)
        let previous = try #require(try next.shown.row(nextWorking1)?.previous)
        #expect(previous.session == records.session.id, "PREVIOUS is Friday's first working set")
        let suggestion = try #require(try next.shown.row(nextWorking1)?.suggestion)
        #expect(!suggestion.text.isEmpty)
        #expect(try next.shown.row(nextWorking1)?.commitValues != nil, "the ghost values tick")
    }
}
