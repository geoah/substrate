#if DEBUG
import Foundation
import WorkoutsCore

/// What the SwiftUI previews show, built the way the app builds it: the
/// bundled catalogue (the sample routine, decision S1), two earlier sessions
/// of it made by the engine as History, and a session in progress made by
/// the engine and drawn by the presenter. Made from the bundle at preview
/// time, so it never drifts from what the app carries. Debug only.
@MainActor
enum PreviewData {
    static let catalog: Catalog = (try? BundledCatalog().loadNow()) ?? Catalog()

    /// The sample, done in full nine and four days ago, newest first, saved
    /// to Apple Health.
    static let history: [HistoryEntry] = {
        let app = AppModel(catalog: catalog, history: [])
        let entries: [HistoryEntry] = [9, 4].compactMap { daysAgo in
            var script = Script(app: app, now: Date.now.addingTimeInterval(TimeInterval(-daysAgo * 86400)), startedSecondsAgo: 0)
            script.start()
            let end = script.logAll(before: nil, from: 60, every: 75)
            guard let records = script.finish(at: end) else { return nil }
            return HistoryEntry(records: records, local: HistoryLocal(health: HealthSaveState(status: .saved)))
        }
        return entries.sorted { $0.records.session.at > $1.records.session.at }
    }()

    /// The app with the session cover in `layout` (Rows unless a preview asks for the Table).
    static func app(layout: SessionLayout = .rows) -> AppModel {
        var settings = WorkoutsSettings()
        settings.sessionLayout = layout
        return AppModel(catalog: catalog, history: history, settings: settings)
    }

    /// A session model drawing `state` as the app would.
    static func preview(_ app: AppModel, _ state: SessionState?, now: Date) -> SessionModel {
        SessionModel(preview: state, catalog: app.catalog ?? catalog, history: app.historyIndex, settings: app.settings, now: now)
    }

    static func noSession(_ app: AppModel) -> SessionModel {
        preview(app, nil, now: .now)
    }

    /// The sample, started 16 minutes ago: the leg press's warm-up and top
    /// set done and its rest counting down.
    static func session(_ app: AppModel, now: Date = .now) -> SessionModel {
        preview(app, sessionState(app, now: now), now: now)
    }

    /// The same session with the plank's timed set running.
    static func timedSession(_ app: AppModel, now: Date = .now) -> SessionModel {
        var script = Script(app: app, now: now)
        script.start()
        if let plank = script.row(slot: "plank", index: 0) {
            script.apply(.startTimedSet(row: plank, target: 60), secondsAgo: 20)
        }
        return preview(app, script.state, now: now)
    }

    static func sessionState(_ app: AppModel, now: Date) -> SessionState? {
        var script = Script(app: app, now: now)
        script.start()
        script.tick(slot: "leg-press", index: 0, secondsAgo: 11 * 60)
        script.tick(slot: "leg-press", index: 1, secondsAgo: 40)
        return script.state
    }

    // MARK: - The Rows screens

    /// The moments of the Rows design (DESIGN.md §7.6a), each a session
    /// driven through the engine with the Rows settings (the RPE pre-fill on),
    /// after the example sessions in History.
    enum RowsMoment: String, CaseIterable {
        /// The sample, the leg press's warm-up open in orange.
        case warmupOpen
        /// Leg press set 2 (the back-off) open: the warm-up folded, the top set done with its verdict, the RPE pre-filled.
        case setTwoOpen
        /// The same after −1 rep: the value tinted, `↺ 10` offered.
        case oneRepFewer
        /// The same after a tap on 8.5: the cell filled, the Log label with it.
        case rpeTapped
        /// Set 2 logged, the rest counting down, set 3 open with "after the rest".
        case resting
        /// Resting, with set 1 open to fix (see `rowsScreen`).
        case fixing
        /// Chest press set 1 open out of order: the leg press's back-off waits.
        case outOfOrder
        /// Set 2 open with the keypad typing 120 (see `rowsScreen`).
        case keypad
        /// The lateral raise as myo-reps: two mini-sets as chips, the third open.
        case myoReps
        /// The superset in round 1: A1 done, go straight to A2.
        case superset
        /// The chest press drop hanging off set 3 and open at once.
        case drop
        /// The plank ready with its goal and last time.
        case plankReady
        /// The plank held for 41 s.
        case plankRunning
        /// Every set done: the blocks folded, Finish in the capsule.
        case allDone
        /// An empty session: the capsule asks for an exercise.
        case empty
    }

    static func rowsSession(_ app: AppModel, _ moment: RowsMoment, now: Date = .now) -> SessionModel {
        preview(app, rowsState(app, moment, now: now), now: now)
    }

    /// The Rows view state a moment opens with: the fix of set 1 with one rep
    /// more, or the keypad on set 2's load with 120 typed.
    static func rowsScreen(_ session: SessionModel, _ moment: RowsMoment) -> RowsScreenModel {
        guard let state = session.state, let layout = session.presentation?.rowsLayout else { return RowsScreenModel() }
        switch moment {
        case .fixing:
            guard let set1 = state.slot("leg-press")?.rows[1], let item = layout.item(set1), let fix = item.fix,
                  let plus = fix.editor(draft: fix.logged).field(.reps)?.plus
            else { return RowsScreenModel() }
            return RowsScreenModel(fixing: .init(row: set1, draft: fix.logged.applying(plus.patch)))
        case .keypad:
            guard let open = layout.openRow, let editor = layout.item(open)?.editor, let load = editor.field(.load) else {
                return RowsScreenModel()
            }
            return RowsScreenModel(typing: .init(row: open, mode: .log, keypad: load.keypad, text: "120"))
        default:
            return RowsScreenModel()
        }
    }

    static func rowsState(_ app: AppModel, _ moment: RowsMoment, now: Date) -> SessionState? {
        switch moment {
        case .warmupOpen:
            var script = Script(app: app, now: now, startedSecondsAgo: 52)
            script.start()
            return script.state
        case .setTwoOpen, .oneRepFewer, .rpeTapped, .keypad:
            var script = Script(app: app, now: now, startedSecondsAgo: 560)
            script.start()
            script.log("leg-press", 0, at: 60)
            script.log("leg-press", 1, at: 200)
            if moment == .oneRepFewer || moment == .rpeTapped, let set2 = script.row(slot: "leg-press", index: 2) {
                script.apply(.editSet(.init(row: set2, values: SetValuesPatch(reps: .set(9)))), at: 500)
                if moment == .rpeTapped {
                    script.apply(.editSet(.init(row: set2, values: .enteringRPE(Decimal(string: "8.5")!))), at: 510)
                }
            }
            return script.state
        case .resting, .fixing:
            var script = Script(app: app, now: now, startedSecondsAgo: 820)
            script.start()
            script.log("leg-press", 0, at: 60)
            script.log("leg-press", 1, at: 730)
            return script.state
        case .outOfOrder:
            var script = Script(app: app, now: now, startedSecondsAgo: 965)
            script.start()
            script.log("leg-press", 0, at: 60)
            script.log("leg-press", 1, at: 400)
            script.touch("chest-press", 0, at: 960)
            return script.state
        case .myoReps:
            var script = Script(app: app, now: now, startedSecondsAgo: 2200)
            script.start()
            let at = script.logAll(before: "lateral-raise", from: 60, every: 60)
            script.log("lateral-raise", 0, at: at)
            script.log("lateral-raise", 1, at: at + 30)
            script.log("lateral-raise", 2, at: at + 60)
            return script.state
        case .superset:
            var script = Script(app: app, now: now, startedSecondsAgo: 1409)
            script.start()
            let at = script.logAll(before: "pull-up", from: 60, every: 50)
            script.log("pull-up", 0, at: max(at, 1400))
            return script.state
        case .drop:
            var script = Script(app: app, now: now, startedSecondsAgo: 606)
            script.start()
            let at = script.logAll(before: "chest-press", from: 60, every: 50)
            script.log("chest-press", 0, at: at)
            script.log("chest-press", 1, at: at + 100)
            script.log("chest-press", 2, at: max(at + 200, 600))
            return script.state
        case .plankReady, .plankRunning:
            var script = Script(app: app, now: now, startedSecondsAgo: 2241)
            script.start()
            let at = script.logAll(before: "plank", from: 60, every: 50)
            script.touch("plank", 0, at: at)
            if moment == .plankRunning {
                script.startTimed("plank", 0, at: 2200)
            }
            return script.state
        case .allDone:
            var script = Script(app: app, now: now, startedSecondsAgo: 3000)
            script.start()
            _ = script.logAll(before: nil, from: 60, every: 70)
            return script.state
        case .empty:
            var script = Script(app: app, now: now, startedSecondsAgo: 30)
            script.startEmpty()
            return script.state
        }
    }

    /// Drives the engine the way the controller does, with the presenter's
    /// shown values for each tick (the session's settings: the RPE pre-fill
    /// when the cover shows Rows).
    private struct Script {
        let app: AppModel
        let now: Date
        /// When the session started.
        let started: Date
        var state: SessionState?

        init(app: AppModel, now: Date, startedSecondsAgo: Int = 16 * 60) {
            self.app = app
            self.now = now.flooredToSecond
            started = self.now.addingTimeInterval(TimeInterval(-startedSecondsAgo))
        }

        /// The sample routine, the one the app carries.
        mutating func start() {
            start(routine: SampleRoutine.id.rawValue)
        }

        mutating func start(routine id: String) {
            guard let routine = catalog.routines[RoutineID(id)] else { return }
            var exercises: [ExerciseID: Exercise] = [:]
            for slot in routine.slots {
                if let exercise = catalog.exercises[slot.exercise] { exercises[exercise.id] = exercise }
            }
            let count = SessionEngine.rowIDCount(for: routine, exercises: exercises)
            let start = SessionEvent.Start(
                session: SessionID(UUIDv7.make(at: started)),
                routine: routine,
                exercises: exercises.values.sorted { $0.id < $1.id },
                settings: app.settings.session,
                bodyMass: .kg(Decimal(string: "82.4")!),
                timeZone: TimeZone.current.identifier,
                utcOffset: TimeZone.current.secondsFromGMT(for: started) / 60,
                rowIDs: (0 ..< count).map { _ in RowID(UUIDv7.make(at: started)) }
            )
            apply(.start(start), at: 0)
        }

        mutating func startEmpty() {
            let start = SessionEvent.Start(
                session: SessionID(UUIDv7.make(at: started)),
                routine: nil,
                exercises: [],
                settings: app.settings.session,
                timeZone: TimeZone.current.identifier,
                utcOffset: TimeZone.current.secondsFromGMT(for: started) / 60,
                rowIDs: []
            )
            apply(.start(start), at: 0)
        }

        func row(slot: String, index: Int) -> RowID? {
            guard let rows = state?.slot(slot)?.rows, index < rows.count else { return nil }
            return rows[index]
        }

        /// The Table's previews: a tick `secondsAgo` before now.
        mutating func tick(slot: String, index: Int, secondsAgo: Int) {
            log(slot, index, at: Int(now.timeIntervalSince(started)) - secondsAgo)
        }

        /// Log: what the row shows, as the button commits it. A timed set is
        /// played to its target and stopped; a set with nothing to commit (a
        /// drop to failure) gets last time's 8 reps first.
        mutating func log(_ slot: String, _ index: Int, at seconds: Int) {
            guard let row = row(slot: slot, index: index) else { return }
            if case let .ready(target)? = present(at: seconds)?.row(row)?.timer {
                // Held up to 40 s, so the stop lands before the next tick a step later.
                let held = min(target.map { Int($0) } ?? 40, 40)
                apply(.startTimedSet(row: row, target: target), at: seconds - held)
                guard let values = present(at: seconds)?.row(row)?.stopValues else { return }
                let at = date(seconds)
                apply(.stopTimedSet(SessionEvent.CompleteSet(row: row, recordID: SetID(UUIDv7.make(at: at)), values: values,
                                                             segmentRowIDs: [RowID(UUIDv7.make(at: at))])),
                      at: seconds)
                return
            }
            if present(at: seconds)?.row(row)?.commitValues == nil {
                apply(.editSet(.init(row: row, values: SetValuesPatch(reps: .set(8)))), at: seconds)
            }
            guard let values = present(at: seconds)?.row(row)?.commitValues else { return }
            let at = date(seconds)
            apply(.completeSet(SessionEvent.CompleteSet(row: row, recordID: SetID(UUIDv7.make(at: at)), values: values,
                                                        segmentRowIDs: [RowID(UUIDv7.make(at: at))])),
                  at: seconds)
        }

        /// Logs every pending set of a slot in order, `step` seconds apart;
        /// returns when the next one would be.
        mutating func logAll(_ slot: String, from seconds: Int, every step: Int) -> Int {
            var at = seconds
            var guardCount = 0
            while guardCount < 20, let rows = state?.slot(slot)?.rows,
                  let index = rows.firstIndex(where: { state?.rows[$0]?.isPending == true })
            {
                let before = state?.rows[rows[index]]?.isPending
                log(slot, index, at: at)
                at += step
                guardCount += 1
                if state?.rows[rows[index]]?.isPending == before { break }
            }
            return at
        }

        /// Logs every slot before `slot` in display order; nil logs them all.
        mutating func logAll(before slot: String?, from seconds: Int, every step: Int) -> Int {
            var at = seconds
            for key in state?.slots.map(\.key) ?? [] {
                if key == slot { break }
                at = logAll(key, from: at, every: step)
            }
            return at
        }

        /// Finish at `seconds`, unticked sets dropped: the records History keeps.
        mutating func finish(at seconds: Int) -> SessionRecords? {
            apply(.finish(.init(start: started, end: date(seconds), sessionRPE: 7,
                                utcOffset: TimeZone.current.secondsFromGMT(for: started) / 60)), at: seconds)
            guard let state, state.phase == .finished else { return nil }
            return SessionEngine.records(state, adapter: "substrate-workouts@0.1.0")
        }

        mutating func touch(_ slot: String, _ index: Int, at seconds: Int) {
            guard let row = row(slot: slot, index: index) else { return }
            apply(.touchRow(row: row), at: seconds)
        }

        mutating func startTimed(_ slot: String, _ index: Int, at seconds: Int) {
            guard let row = row(slot: slot, index: index),
                  case let .ready(target)? = present(at: seconds)?.row(row)?.timer
            else { return }
            apply(.startTimedSet(row: row, target: target), at: seconds)
        }

        func date(_ seconds: Int) -> Date {
            started.addingTimeInterval(TimeInterval(seconds))
        }

        func present(at seconds: Int) -> SessionPresentation? {
            state.map {
                SessionPresenter.present($0, catalog: catalog, history: app.historyIndex,
                                         settings: app.sessionPresenterSettings, now: date(seconds))
            }
        }

        mutating func apply(_ event: SessionEvent, secondsAgo: Int) {
            apply(event, at: Int(now.timeIntervalSince(started)) - secondsAgo)
        }

        mutating func apply(_ event: SessionEvent, at seconds: Int) {
            let at = date(seconds)
            let envelope = EventEnvelope(id: EventID(UUIDv7.make(at: at)), at: at, event: event)
            if let next = try? SessionEngine.apply(envelope, to: state) { state = next }
        }
    }
}
#endif
