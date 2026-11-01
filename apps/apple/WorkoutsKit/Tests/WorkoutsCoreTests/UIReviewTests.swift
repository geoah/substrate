import Foundation
@testable import WorkoutsCore
import Testing

// Fixes from the simulator review of 31 October (the 03 to 19 screenshots):
// Finish's summary now projects the pending-set choice, a timed set's target
// is pinned, RPE reaches timed and distance rows, and the copy and VoiceOver
// strings the presenters build.

/// What Save wrote, read the way the projection reads it.
private struct Written: Equatable {
    var sets: Int
    var workingSets: Int
    var volume: Mass
    var exercises: Int

    init(_ records: SessionRecords, state: SessionState) {
        sets = records.sets.count
        workingSets = Counting.workingSets(records.sets)
        volume = Counting.volume(records.sets, exercises: state.exercises, bodyMass: state.session.bodyMass,
                                 unit: state.settings.defaultUnit, includeWarmups: state.settings.countWarmupsInVolume)
        exercises = Set(records.sets.map(\.slot)).count
    }

    init(_ projection: FinishProjection) {
        sets = projection.savedSets
        workingSets = projection.workingSets
        volume = projection.volume
        exercises = projection.exercises
    }
}

/// Finishes a copy of `script`'s session the way Save does: the projection
/// for `policy`, its event, the engine, the records.
private func save(_ script: EngineScript, presentation: SessionPresentation, policy: PendingSetPolicy,
                  at end: Date) throws -> (FinishProjection, SessionRecords, SessionState)
{
    var copy = script
    var minted = 0
    let projection = FinishPresenter.projection(try copy.current, presentation: presentation, policy: policy, end: end) { _ in
        minted += 1
        return SetID("saved-\(minted)")
    }
    let finished = try copy.send(.finish(projection.event(start: try copy.current.session.at, end: end, utcOffset: 0)), at: end)
    return (projection, SessionEngine.records(finished, adapter: "substrate-workouts@test"), finished)
}

@Suite("UI review: Finish projects the pending-set choice")
struct FinishProjectionTests {
    @Test("Fully Body, two sets ticked: each policy's summary is what Save writes")
    func bothPolicies() throws {
        var session = try NextFullyBody()
        try session.tick("chest-press", 0, kgReps(25, 15), at: 60)
        try session.tick("chest-press", 1, kgReps(40, 11, rpe: 9), at: 180)
        let presentation = try session.present(at: 200)
        let end = NextFullyBody.start.addingTimeInterval(47 * 60)
        let state = try session.script.current
        let incomplete = presentation.unticked.count { $0.values == nil }
        #expect(incomplete > 0, "the push-ups' hinted reps and the drop's 'to failure' commit nothing")

        let (dropped, droppedRecords, droppedState) = try save(session.script, presentation: presentation, policy: .drop, at: end)
        #expect(Written(dropped) == Written(droppedRecords, state: droppedState))
        #expect(dropped.pendingSets == 53 && dropped.markedSets == 0 && dropped.droppedSets == 53)
        #expect(dropped.savedSets == 2 && dropped.workingSets == 1 && dropped.exercises == 1 && dropped.exercisesInSession == 21)
        #expect(Format.volume(dropped.volume) == "440 kg")
        // The incomplete rows are the same under either policy.
        #expect(dropped.incompleteSets >= incomplete)

        let (marked, markedRecords, markedState) = try save(session.script, presentation: presentation, policy: .markDone, at: end)
        #expect(Written(marked) == Written(markedRecords, state: markedState))
        #expect(marked.pendingSets == 53)
        #expect(marked.markedSets + marked.incompleteSets == marked.pendingSets)
        #expect(marked.incompleteSets == dropped.incompleteSets)
        #expect(marked.savedSets == 2 + marked.markedSets)
        #expect(marked.workingSets > dropped.workingSets && marked.volume.value > dropped.volume.value)
        // History's line for the saved session reads the same numbers.
        let history = HistoryPresenter.summary(markedRecords, exercises: markedState.exercises, settings: Progression.settings)
        #expect(history.contains("\(marked.workingSets) sets") && history.contains(Format.volume(marked.volume)))

        // The sheet's rows are the projection's, for the policy chosen in the dialog.
        let sheet = FinishPresenter.summary(state, presentation: presentation, policy: .markDone, end: end, healthAllowed: true)
        #expect(sheet.rows == [LabelledValue("Working sets", "\(marked.workingSets)"),
                               LabelledValue("Volume", Format.volume(marked.volume)),
                               LabelledValue("Exercises", "\(marked.exercises) of 21")])
        #expect(sheet.untickedTitle == "53 sets are not ticked")
        #expect(sheet.pendingText == "\(marked.markedSets) unticked sets are saved as shown; \(marked.incompleteSets) are left out, a value is missing.")
        #expect(sheet.untickedMessage == "\(marked.incompleteSets) have a value missing and are left out either way.")
        let dropSheet = FinishPresenter.summary(state, presentation: presentation, policy: .drop, end: end, healthAllowed: true)
        #expect(dropSheet.rows.first == LabelledValue("Working sets", "1"))
        #expect(dropSheet.pendingText == "53 unticked sets are left out.")
    }

    @Test("A row with no load to show cannot be marked done, and neither can the side after it")
    func incompleteRows() throws {
        let cable = EngineCatalog.exercise("cable", restClass: .isolation, equipment: .cable)
        let lunge = EngineCatalog.lunge
        let routine = EngineCatalog.routine([
            EngineCatalog.slot("p", EngineCatalog.press, EngineCatalog.sets(2)),
            // No planned load, no history: the load cell is empty and outlined.
            EngineCatalog.slot("c", cable, EngineCatalog.sets(1, load: nil)),
            // One side at a time, no load: the right side waits on the left.
            EngineCatalog.slot("l", lunge, EngineCatalog.sets(1, load: nil)),
        ])
        var script = EngineScript(at: engineEpoch)
        try script.start(routine, exercises: [EngineCatalog.press, cable, lunge])
        try script.tick(try script.row("p", 0), kgReps(50, 10))
        let presentation = SessionPresenter.present(try script.current, history: HistoryIndex(), settings: Progression.settings,
                                                    now: script.now)
        #expect(presentation.row(try script.row("c", 0))?.missing == [.load])
        let end = engineEpoch.addingTimeInterval(1800)

        let (marked, records, state) = try save(script, presentation: presentation, policy: .markDone, at: end)
        #expect(Written(marked) == Written(records, state: state))
        // Pending: p set 2, the cable set, the lunge's left and right.
        #expect(marked.pendingSets == 4)
        #expect(marked.markedSets == 1 && marked.incompleteSets == 3)
        #expect(marked.savedSets == 2 && marked.workingSets == 2 && marked.exercises == 1 && marked.exercisesInSession == 3)
        #expect(records.sets.allSatisfy { $0.slot == "p" })

        let (dropped, droppedRecords, droppedState) = try save(script, presentation: presentation, policy: .drop, at: end)
        #expect(Written(dropped) == Written(droppedRecords, state: droppedState))
        #expect(dropped.markedSets == 0 && dropped.incompleteSets == 3 && dropped.savedSets == 1 && dropped.workingSets == 1)

        let sheet = FinishPresenter.summary(try script.current, presentation: presentation, policy: .markDone, end: end,
                                            healthAllowed: false)
        #expect(sheet.pendingText == "1 unticked set is saved as shown; 3 are left out, a value is missing.")
        #expect(sheet.untickedMessage == "3 have a value missing and are left out either way.")
        #expect(sheet.rows.last == LabelledValue("Exercises", "1 of 3"))
    }

    @Test("Every row ticked: nothing to ask, and both policies write the same")
    func nothingPending() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([EngineCatalog.slot("p", EngineCatalog.press, EngineCatalog.sets(1))]),
                         exercises: [EngineCatalog.press])
        try script.tick(try script.row("p", 0), kgReps(50, 10))
        let presentation = SessionPresenter.present(try script.current, history: HistoryIndex(), settings: Progression.settings,
                                                    now: script.now)
        let end = engineEpoch.addingTimeInterval(600)
        let summary = FinishPresenter.summary(try script.current, presentation: presentation, policy: .markDone, end: end,
                                              healthAllowed: true)
        #expect(summary.untickedTitle == nil && summary.untickedMessage == nil && summary.pendingText == nil)
        #expect(summary.rows == [LabelledValue("Working sets", "1"), LabelledValue("Volume", "500 kg"),
                                 LabelledValue("Exercises", "1 of 1")])
        let (drop, _, _) = try save(script, presentation: presentation, policy: .drop, at: end)
        let (mark, _, _) = try save(script, presentation: presentation, policy: .markDone, at: end)
        #expect(Written(drop) == Written(mark))
    }
}

@Suite("UI review: timed sets, RPE everywhere, copy and VoiceOver")
struct UIReviewPresenterTests {
    @Test("Play on a plank with only a ghost duration counts down to it")
    func timedSetTarget() throws {
        var session = try NextFullyBody()
        let plank = try session.row("plank", 0)
        let ready = try #require(try session.present().row(plank))
        // "Last time 1:02; try 1:07": nothing typed, the ghost is the target.
        #expect(ready.cell(.time)?.entered == nil && ready.cell(.time)?.isGhost == true)
        #expect(ready.timer == .ready(target: 67))
        try session.send(.startTimedSet(row: plank, target: 67), at: 10)
        let started = NextFullyBody.start.addingTimeInterval(10)
        let running = try session.present(at: 11)
        #expect(running.row(plank)?.timer == .running(startedAt: started, endsAt: started.addingTimeInterval(67)))
        guard case let .timedSet(bar) = running.restBar else {
            Issue.record("expected the timed-set bar, got \(running.restBar)")
            return
        }
        #expect(bar.endsAt == started.addingTimeInterval(67))
        #expect(running.liveActivity.countdown?.endsAt == started.addingTimeInterval(67))
        #expect(running.notification?.fireDate == started.addingTimeInterval(67))

        // A typed time wins over the ghost; with neither, the timer is a stopwatch.
        try session.send(.cancelTimedSet(row: plank), at: 20)
        try session.send(.editSet(.init(row: plank, values: SetValuesPatch(duration: .set(90)))), at: 21)
        #expect(try session.present(at: 22).row(plank)?.timer == .ready(target: 90))
        var bare = EngineScript(at: engineEpoch)
        try bare.start(EngineCatalog.routine([EngineCatalog.slot("k", EngineCatalog.plank, [PlannedSet(key: "s1")])]),
                       exercises: [EngineCatalog.plank])
        let row = try bare.row("k", 0)
        let stopwatch = SessionPresenter.present(try bare.current, history: HistoryIndex(), settings: Progression.settings,
                                                 now: engineEpoch)
        #expect(stopwatch.row(row)?.timer == .ready(target: nil))
    }

    @Test("RPE on a timed set: rated from the menu, carried by Stop, shown under the row, saved")
    func rpeOnTimedSets() throws {
        let sidePlank = EngineCatalog.exercise("side-plank", .duration, laterality: .unilateral, restClass: .timed,
                                               equipment: Equipment.none)
        let routine = EngineCatalog.routine([
            EngineCatalog.slot("k", EngineCatalog.plank, [PlannedSet(key: "s1", durationMin: 45), PlannedSet(key: "s2", durationMin: 45)]),
            EngineCatalog.slot("s", sidePlank, [PlannedSet(key: "s1", durationMin: 30)]),
            EngineCatalog.slot("p", EngineCatalog.press, EngineCatalog.sets(1)),
        ])
        var script = EngineScript(at: engineEpoch)
        try script.start(routine, exercises: [EngineCatalog.plank, sidePlank, EngineCatalog.press])
        func present() throws -> SessionPresentation {
            SessionPresenter.present(try script.current, history: HistoryIndex(), settings: Progression.settings, now: script.now)
        }
        let first = try script.row("k", 0)
        let second = try script.row("k", 1)
        var rows = try present()
        #expect(rows.card("k")?.columns.contains(.rpe) == false)
        #expect(rows.row(first)?.canRateEffort == true && rows.row(first)?.effortText == nil)
        // The second side waits for the first: no rating yet.
        #expect(rows.row(try script.row("s", 1))?.canRateEffort == false)
        // A grid with an RPE column rates there, not from the menu.
        #expect(rows.row(try script.row("p", 0))?.canRateEffort == false)

        // Rated before the set, then Play and Stop: the RPE goes with the time.
        try script.send(.editSet(.init(row: first, values: SetValuesPatch(rpe: .set(8)))))
        rows = try present()
        #expect(rows.row(first)?.effortText == "RPE 8" && rows.row(first)?.rpeText == "8")
        try script.send(.startTimedSet(row: first, target: 45))
        script.wait(50)
        let stop = try #require(try present().row(first)?.stopValues)
        #expect(stop.rpe == 8)
        try script.send(.stopTimedSet(.init(row: first, recordID: script.setID(), values: stop)))
        #expect(try present().row(first)?.effortText == "RPE 8")

        // Rated after the set: an edit of a done timed row.
        try script.tick(second, SetValues(duration: 40))
        let rated = try script.send(.editSet(.init(row: second, values: SetValuesPatch(rpe: .set(9.5)))))
        #expect(rated.rows[second]?.performed?.values.rpe == 9.5)
        #expect(try present().row(second)?.effortText == "RPE 9.5")
        let sets = SessionEngine.setRecords(rated)
        #expect(sets.compactMap(\.rpe) == [8, 9.5])
        #expect(sets.map(\.duration) == [50, 40])
        #expect(sets.last?.revision == 2)
        // The sheet's reps-in-reserve line is for rep sets only.
        #expect(RPEScaleText.reserve(8, measurement: .weightreps) == "Could do 2 more reps")
        #expect(RPEScaleText.reserve(8, measurement: .duration) == nil)
        #expect(RPEScaleText.headline(8) == "Hard")
    }

    @Test("RPE on a carry: the tick commits it and the record keeps it")
    func rpeOnDistanceSets() throws {
        let routine = EngineCatalog.routine([EngineCatalog.slot("c", EngineCatalog.carry, [PlannedSet(key: "s1", load: .kg(24))])])
        var script = EngineScript(at: engineEpoch)
        try script.start(routine, exercises: [EngineCatalog.carry])
        let row = try script.row("c", 0)
        try script.send(.editSet(.init(row: row, values: SetValuesPatch(distance: .set(Length(metres: 40)), rpe: .set(7)))))
        let presentation = SessionPresenter.present(try script.current, history: HistoryIndex(), settings: Progression.settings,
                                                    now: script.now)
        let shown = try #require(presentation.row(row))
        #expect(shown.canRateEffort && shown.effortText == "RPE 7")
        let values = try #require(shown.commitValues)
        #expect(values.rpe == 7)
        let state = try script.tick(row, values)
        #expect(SessionEngine.setRecords(state).first?.rpe == 7)
        #expect(SessionEngine.setRecords(state).first?.distance == Length(metres: 40))
    }

    @Test("Copy: Working sets in the header, no literal None, load steps in words")
    func copy() throws {
        #expect(SessionHeaderStats.setsTitle == "Working sets")
        #expect([SessionHeaderStats.durationTitle, SessionHeaderStats.volumeTitle] == ["Duration", "Volume"])
        let exercises = try EngineFixtures.exercises()
        let history = HistoryIndex(try ExampleFixtures.sessions("sessions"))
        let rows = ExercisePresenter.list(exercises, history: history, settings: Progression.settings).flatMap(\.rows)
        #expect(!rows.contains { $0.equipment == "None" })
        // Bird dog: reps only, no equipment, so no equipment line.
        #expect(rows.first { $0.id == "bird-dog" }?.equipment == nil)
        #expect(rows.first { $0.id == "push-up" }?.equipment == "Bodyweight")
        #expect(rows.first { $0.id == "chest-press-machine" }?.equipment == "Machine")
        func facts(_ id: ExerciseID) throws -> [LabelledValue] {
            ExercisePresenter.detail(try #require(exercises[id]), history: history, settings: Progression.settings).facts
        }
        #expect(try facts("bird-dog").allSatisfy { $0.label != "Equipment" })
        #expect(try facts("push-up").first == LabelledValue("Equipment", "Bodyweight"))
        // Load steps read naturally; the start only when it is not 0.
        #expect(try facts("chest-press-machine").last == LabelledValue("Load", "5 kg steps"))
        #expect(try facts("hip-thrust-barbell").last == LabelledValue("Load", "2.5 kg steps from 20 kg"))
        #expect(try facts("cable-core-pallof-press").last == LabelledValue("Load", "2.5 kg steps"))
        #expect(try facts("assisted-pull-up-machine").last == LabelledValue("Assistance", "10 lb steps"))
        #expect(try facts("push-up").allSatisfy { !["Load", "Added load", "Assistance"].contains($0.label) })
        let dip = Exercise(id: "dip", name: "Dip", measurement: .weightedbodyweightreps, equipment: .plate)
        #expect(ExercisePresenter.loadSteps(SuggestionInputs.config(for: dip, defaultUnit: .kg))
            == LabelledValue("Added load", "2.5 kg steps"))
        #expect(dip.equipmentText == "Plate")
        #expect(Exercise(id: "x", name: "X", measurement: .bodyweightassistedreps).equipmentText == "Bodyweight")
    }

    @Test("VoiceOver: each field names the exercise, the set and the unit, and says suggested or required")
    func accessibility() throws {
        var session = try NextFullyBody()
        let presentation = try session.present()
        let set1 = try #require(presentation.row(try session.row("chest-press", 1)))
        let load = try #require(set1.cell(.load))
        #expect(load.accessibilityLabel == "Chest Press (Machine), set 1, load in kilograms")
        #expect(load.accessibilityValue() == "suggested 40")
        #expect(load.accessibilityValue(draft: "42.5") == "42.5")
        #expect(set1.cell(.reps)?.accessibilityLabel == "Chest Press (Machine), set 1, reps")
        #expect(set1.rpeAccessibilityLabel == "Chest Press (Machine), set 1, RPE")
        #expect(set1.rpeAccessibilityValue == "not rated")
        let warmup = try #require(presentation.row(try session.row("chest-press", 0)))
        #expect(warmup.cell(.load)?.accessibilityLabel == "Chest Press (Machine), warm-up, load in kilograms")
        let plank = try #require(presentation.row(try session.row("plank", 0))?.cell(.time))
        #expect(plank.accessibilityLabel == "Plank, set 1, time in minutes and seconds")
        #expect(plank.accessibilityValue() == "suggested 1:07")
        let push = try #require(presentation.row(try session.row("push-up", 0))?.cell(.reps))
        #expect(push.accessibilityValue() == "\u{2248}15, a hint")
        // A drop's reps say "to failure"; nothing typed is a hint, not a value.
        let drop = try #require(presentation.row(try session.row("chest-press", 3))?.cell(.reps))
        #expect(drop.accessibilityValue() == "to failure, a hint")
        // Done: the value itself; typed: what was typed.
        try session.tick("chest-press", 1, kgReps(40, 11, rpe: 9), at: 60)
        try session.send(.editSet(.init(row: try session.row("chest-press", 2), values: SetValuesPatch(load: .set(.kg(45))))),
                         at: 70)
        let after = try session.present(at: 71)
        #expect(after.row(try session.row("chest-press", 1))?.cell(.load)?.accessibilityValue() == "40")
        #expect(after.row(try session.row("chest-press", 1))?.rpeAccessibilityValue == "9")
        let typed = try #require(after.row(try session.row("chest-press", 2))?.cell(.load))
        #expect(typed.accessibilityValue() == "45")
        // Cleared while typing: the field is empty, so not the 45 it replaced.
        #expect(typed.accessibilityValue(draft: "") == "empty")
        #expect(typed.accessibilityValue(draft: "47.5") == "47.5")
        // A ghost field cleared while typing still shows its ghost, a done one shows nothing.
        #expect(load.accessibilityValue(draft: "") == "suggested 40")
        #expect(push.accessibilityValue(draft: "") == "\u{2248}15, a hint")
        #expect(after.row(try session.row("chest-press", 1))?.cell(.load)?.accessibilityValue(draft: "") == "empty")
        // Required with nothing to show.
        let cable = EngineCatalog.exercise("cable", restClass: .isolation, equipment: .cable)
        var bare = EngineScript(at: engineEpoch)
        try bare.start(EngineCatalog.routine([EngineCatalog.slot("c", cable, EngineCatalog.sets(1, load: nil))]),
                       exercises: [cable])
        let empty = SessionPresenter.present(try bare.current, history: HistoryIndex(), settings: Progression.settings,
                                             now: engineEpoch).row(try bare.row("c", 0))?.cell(.load)
        #expect(empty?.accessibilityLabel == "Cable, set 1, load in kilograms")
        #expect(empty?.accessibilityValue() == "required, empty")
        #expect(GridColumn.addedLoad.spokenName(unit: .lb) == "added load in pounds")
        #expect(GridColumn.distance.spokenName(unit: .kg) == "distance in metres")
    }

    @Test("The Pallof press moves in 2.5 kg steps: 10 kg at the top of the range goes to 12.5, not 15")
    func pallofStep() throws {
        let session = try NextFullyBody()
        let presentation = try session.present()
        let left = try #require(presentation.row(try session.row("pallof-press", 0)))
        let suggestion = try #require(left.suggestion)
        #expect(suggestion.load == 12.5)
        #expect(left.cell(.load)?.text == "12.5")
        #expect(presentation.card("pallof-press")?.loadStep == .kg(2.5))
    }
}
