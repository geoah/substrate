import Foundation
@testable import WorkoutsCore
import Testing

// Fixes from the V1 review: clusters, replacements in a group, failure sets
// switched in the gym, rest clocks around removals and same-second ticks, a
// drop with no load grid, and the rest bar's and dialogs' copy. Each test
// failed before its fix.

@Suite("Review fixes: suggestions")
struct ReviewSuggestionFixTests {
    /// Coverage, 28 Oct: the hip thrust is planned as clusters of 3 at 80 kg
    /// (`miniSetReps: 3`, no rep range). The head is the first cluster.
    @Test("a cluster set's head is suggested its miniSetReps, not the exercise's 8-12 default")
    func clusterHead() throws {
        let replay = try Progression.replay(EngineFixtures.coverage, file: "coverage")
        let head = try #require(replay.sets("hip-thrust").first)
        let suggestion = try #require(replay.before[head.id] ?? nil)
        #expect(suggestion.load == 80)
        #expect(suggestion.reps == 3)
    }

    @Test("next time, a cluster head reads last time's first cluster against 3 reps and its clusters take its load")
    func clusterHeadFromHistory() throws {
        let exercises = try EngineFixtures.exercises()
        let routine = try #require(try EngineFixtures.routines()["coverage-every-method"])
        let fixture = try EngineFixtures.session("coverage", EngineFixtures.coverage)
        var script = EngineScript(at: fixture.session.at.addingTimeInterval(7 * 86400), seed: 41)
        try script.start(routine, exercises: routine.slots.compactMap { exercises[$0.exercise] }, bodyMass: .kg(82))
        let presentation = SessionPresenter.present(try script.current, history: HistoryIndex([fixture]),
                                                    settings: Progression.settings, now: script.now)
        let head = try #require(presentation.row(try script.row("hip-thrust", 0))?.suggestion)
        let cluster = try #require(presentation.row(try script.row("hip-thrust", 1))?.suggestion)
        // 80 × 3 at RIR 2 met the 3-rep target: 03 §11.3 keeps it or loads up one step, never a new-range cut.
        #expect(head.reps == 3)
        #expect(head.load == 82.5)
        #expect(cluster.load == head.load && cluster.reps == 3)
    }

    /// 03 §13: a failure set with no `repsMax` has no top; the same set
    /// added in the gym has no target at all.
    @Test("an added set switched to Failure has an open top: the load stays, the reps rise")
    func addedFailureOpenTop() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(1))]),
                         exercises: EngineCatalog.all)
        try script.tick(try script.row("press", 0), kgReps(50, 12, rpe: 8))
        let added = script.rowIDs(1)[0]
        try script.send(.addSet(.init(slot: "press", row: added, role: .working)))
        let state = try script.send(.changeSetKind(.init(row: added, kind: .failure)))
        let suggestion = try #require(SuggestionInputs.suggestion(for: added, in: state, history: HistoryIndex(),
                                                                  now: script.now, calendar: Progression.calendar))
        #expect(suggestion.load == 50)
        #expect(suggestion.reason == .sessionKeep)
        // The planned equivalent, for comparison.
        var planned = EngineScript(at: engineEpoch)
        try planned.start(EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, [
            PlannedSet(key: "s1", repsMin: 8, repsMax: 12, load: .kg(50)),
            PlannedSet(key: "s2", termination: .failure, load: .kg(50)),
        ])]), exercises: EngineCatalog.all)
        let ticked = try planned.tick(try planned.row("press", 0), kgReps(50, 12, rpe: 8))
        let control = try #require(SuggestionInputs.suggestion(for: try planned.row("press", 1), in: ticked, history: HistoryIndex(),
                                                               now: planned.now, calendar: Progression.calendar))
        #expect(control.load == suggestion.load && control.reps == suggestion.reps)
    }

    /// §5.3: on equipment with no load grid the app cannot know the lighter
    /// setting, so the planned drops stay and the owner picks the load.
    @Test("a drop set on a no-grid exercise keeps its planned drops after the head's tick")
    func noGridDropKeepsRows() throws {
        let band = EngineCatalog.exercise("band-row", restClass: .isolation, equipment: .resistanceband)
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([EngineCatalog.slot("band", band, [
            PlannedSet(key: "s1", structure: .drop, repsMin: 12, repsMax: 15, load: .kg(20), drops: 2),
        ])]), exercises: EngineCatalog.all + [band])
        let head = try script.row("band", 0)
        let flag = SuggestionInputs.noLighterLoad(afterTicking: head, with: kgReps(20, 15), in: try script.current,
                                                  history: HistoryIndex(), now: script.now, calendar: Progression.calendar)
        #expect(flag == false)
        let after = try script.tick(head, kgReps(20, 15), noLighterLoad: flag)
        #expect(after.chain(of: head).count == 3)
        let drop = try script.row("band", 1)
        let presentation = SessionPresenter.present(after, history: HistoryIndex(), settings: Progression.settings, now: script.now)
        #expect(presentation.row(drop)?.suggestion?.text == "Drop from 20 kg: a lighter load, to failure")
    }
}

@Suite("Review fixes: engine")
struct ReviewEngineFixTests {
    private func pressSession(sets: Int = 3) throws -> EngineScript {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(sets), setRest: 120)]),
                         exercises: EngineCatalog.all)
        return script
    }

    private func dropSession(seed: UInt64 = 7) throws -> EngineScript {
        var script = EngineScript(at: engineEpoch, seed: seed)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, [
                PlannedSet(key: "s1", repsMin: 8, repsMax: 12, load: .kg(50)),
                PlannedSet(key: "s2", structure: .drop, repsMin: 8, repsMax: 12, load: .kg(50), drops: 1),
                PlannedSet(key: "s3", repsMin: 8, repsMax: 12, load: .kg(50)),
            ], setRest: 120),
        ]), exercises: EngineCatalog.all)
        return script
    }

    /// The controller floors instants to the second, so catching up on rows
    /// gives two ticks of one `completedAt`.
    @Test("un-ticking the older of two same-second ticks leaves the newer rest alone")
    func sameSecondUntick() throws {
        var script = try pressSession()
        let (s1, s2, s3) = try (script.row("press", 0), script.row("press", 1), script.row("press", 2))
        let at = script.now.addingTimeInterval(30)
        try script.tick(s1, kgReps(50, 10), at: at)
        let latest = try script.tick(s2, kgReps(50, 10), at: at)
        var state = try script.send(.uncompleteSet(row: s1), at: at.addingTimeInterval(5))
        #expect(state.countdown == latest.countdown)
        #expect(state.restClock == latest.restClock)
        state = try script.tick(s3, kgReps(50, 10), at: at.addingTimeInterval(120))
        #expect(state.rows[s3]?.performed?.restBefore == 120)
        // Un-ticking the newer one still takes its rest back.
        var other = try pressSession()
        let (a, b) = try (other.row("press", 0), other.row("press", 1))
        let first = try other.tick(a, kgReps(50, 10), at: at)
        try other.tick(b, kgReps(50, 10), at: at)
        state = try other.send(.uncompleteSet(row: b), at: at.addingTimeInterval(5))
        #expect(state.restClock == first.restClock)
    }

    @Test("a set switched to Failure in the gym reads as failure from its record")
    func failureKindSurvivesFinish() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(3))]),
                         exercises: EngineCatalog.all, session: SessionID("past"))
        let (s1, s2, s3) = try (script.row("press", 0), script.row("press", 1), script.row("press", 2))
        try script.tick(s1, kgReps(50, 10, rpe: 8))
        try script.send(.changeSetKind(.init(row: s2, kind: .failure)))
        let ticked = try script.tick(s2, kgReps(50, 10), at: script.now.addingTimeInterval(120))
        #expect(ticked.rows[s2]?.performed?.values.reachedFailure == true)
        // Marked done at Finish, the same.
        try script.send(.changeSetKind(.init(row: s3, kind: .failure)))
        let end = script.now.addingTimeInterval(60)
        let finished = try script.send(.finish(.init(start: engineEpoch, end: end,
                                                     unticked: .markDone([.init(row: s3, recordID: script.setID(), values: kgReps(50, 9))]),
                                                     utcOffset: 0)), at: end)
        let records = SessionEngine.records(finished, adapter: "substrate-workouts@test")
        let done = SuggestionInputs.doneSets(records.sets, exercise: EngineCatalog.press,
                                             config: SuggestionInputs.config(for: EngineCatalog.press, defaultUnit: .kg))
        #expect(done.map(\.kind) == [.normal, .failure, .failure])
        // A planned set left as it was keeps its record as planned.
        #expect(records.sets.first { $0.ordinal == 1 }?.reachedFailure == nil)
        // History shows the F badge.
        let labels = SetLabel.make(records: records.sets)
        #expect(records.sets.map { labels[$0.id]?.failureBadge } == [false, true, true])
    }

    @Test("a touch ends a 0 s rest while the standalone timer runs")
    func touchWithManualTimer() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(2), setRest: 0)]),
                         exercises: EngineCatalog.all)
        let (s1, s2) = try (script.row("press", 0), script.row("press", 1))
        let t = script.now.addingTimeInterval(10)
        let ticked = try script.tick(s1, kgReps(50, 10), at: t)
        #expect(ticked.countdown == nil && ticked.restClock?.planned == 0)
        try script.send(.startRest(seconds: 60), at: t.addingTimeInterval(5))
        let touched = try script.send(.touchRow(row: s2), at: t.addingTimeInterval(20))
        #expect(touched.restClock?.endedAt == t.addingTimeInterval(20))
        #expect(touched.countdown?.kind == .manual)
    }

    @Test("removing a completed drop chain restores the rest clock from before the chain")
    func removeCompletedChainClock() throws {
        var script = try dropSession()
        let (s1, s2, drop, s3) = try (script.row("press", 0), script.row("press", 1), script.row("press", 2), script.row("press", 3))
        let t0 = script.now.addingTimeInterval(10)
        let afterS1 = try script.tick(s1, kgReps(50, 10), at: t0)
        try script.tick(s2, kgReps(50, 10), at: t0.addingTimeInterval(120))
        try script.tick(drop, kgReps(40, 8), at: t0.addingTimeInterval(130))
        let removed = try script.send(.removeSet(row: s2), at: t0.addingTimeInterval(140))
        #expect(removed.restClock == afterS1.restClock)
        let next = try script.tick(s3, kgReps(50, 10), at: t0.addingTimeInterval(200))
        #expect(next.rows[s3]?.performed?.restBefore == 200)
        #expect(next.rows[s3]?.performed?.restPlanned == 120)
    }

    /// The same removal with the head and its drop ticked in one second.
    /// `EngineScript.send` also checks that the events fold to the same state.
    @Test("removing two ticks of one second restores the same clock whatever the row ids")
    func removalIsOrderIndependent() throws {
        var outcomes: Set<String> = []
        for seed in UInt64(1) ... 40 {
            var script = try dropSession(seed: seed)
            let (s1, s2, drop) = try (script.row("press", 0), script.row("press", 1), script.row("press", 2))
            let t0 = script.now.addingTimeInterval(10)
            try script.tick(s1, kgReps(50, 10), at: t0)
            try script.tick(s2, kgReps(50, 10), at: t0.addingTimeInterval(120))
            try script.tick(drop, kgReps(40, 8), at: t0.addingTimeInterval(120))
            let removed = try script.send(.removeSet(row: s2), at: t0.addingTimeInterval(140))
            outcomes.insert("\(removed.restClock?.slot.map(\.rawValue) ?? "nil")-\(removed.restClock?.planned ?? -1)")
        }
        #expect(outcomes == ["setRest-120"])
    }
}

@Suite("Review fixes: replacing in a group")
struct ReviewReplaceFixTests {
    /// A routine superset A (press) and B (row), three rounds. After A1, B1,
    /// A2, B2 the press is replaced by curl for its last set.
    @Test("a mid-superset replace keeps the replacement's set in its round")
    func replaceMidSuperset() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(3), group: "a"),
            EngineCatalog.slot("row", EngineCatalog.row, EngineCatalog.sets(3), group: "a"),
        ], groups: [RoutineGroup(key: "a", groupKind: .superset)]), exercises: EngineCatalog.all)
        let (a1, a2) = try (script.row("press", 0), script.row("press", 1))
        let (b1, b2, b3) = try (script.row("row", 0), script.row("row", 1), script.row("row", 2))
        for row in [a1, b1, a2, b2] {
            try script.tick(row, kgReps(50, 10), at: script.now.addingTimeInterval(60))
        }
        let state = try script.send(.replaceExercise(.init(slot: "press", newSlot: "curl", exercise: EngineCatalog.curl)))
        let curl3 = try #require(state.orderedRows(inSlot: "curl").first?.id)
        #expect(state.slot("curl")?.lane == "press")
        #expect(state.upNext == curl3)
        #expect(UpNext.round(of: curl3, in: state) == 3)
        #expect(UpNext.round(of: b3, in: state) == 3)
        #expect(UpNext.sequence(state) == [a1, b1, a2, b2, curl3, b3])
        // B3 follows in the same round: the transition (0 s), not the round rest.
        let after = try script.tick(curl3, kgReps(20, 12), at: script.now.addingTimeInterval(60))
        #expect(after.restClock?.slot == .transition)
        // The replacement is not written as a member of its own in the records.
        let end = script.now.addingTimeInterval(60)
        let finished = try script.send(.finish(.init(start: engineEpoch, end: end, utcOffset: 0)), at: end)
        #expect(SessionEngine.records(finished, adapter: "substrate-workouts@test").session.groups.first?.groupKind == .superset)
    }

    @Test("a mid-superset replace in a superset made in the gym keeps it a superset with its 0 s transition")
    func replaceMidGymSuperset() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(3)),
            EngineCatalog.slot("row", EngineCatalog.row, EngineCatalog.sets(3)),
        ]), exercises: EngineCatalog.all)
        try script.send(.groupExercises(slot: "row", partner: "press", group: nil))
        try script.tick(try script.row("press", 0), kgReps(50, 10))
        var state = try script.send(.replaceExercise(.init(slot: "press", newSlot: "curl", exercise: EngineCatalog.curl)))
        #expect(state.group("a")?.groupKind == .superset)
        #expect(RestRules.transition(slot: "curl", in: state).seconds == 0)
        // A real third member still makes it a tri-set.
        try script.send(.addExercise(.init(slot: "raise", exercise: EngineCatalog.raise, roles: [.working], rowIDs: script.rowIDs(1))))
        state = try script.send(.groupExercises(slot: "raise", partner: "row", group: nil))
        #expect(state.group("a")?.groupKind == .triset)
    }
}

@Suite("Review fixes: copy")
struct ReviewCopyFixTests {
    /// A superset whose second member has a warm-up the owner skipped: after
    /// the last working set only that warm-up (round 0) is left.
    @Test("a round rest towards a skipped warm-up is titled Rest over, not Round 0")
    func roundZeroTitle() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(1), group: "a"),
            EngineCatalog.slot("row", EngineCatalog.row, [PlannedSet(key: "w1", role: .warmup, repsMin: 10, repsMax: 10, load: .kg(20))]
                + EngineCatalog.sets(1), group: "a"),
        ], groups: [RoutineGroup(key: "a", groupKind: .superset)]), exercises: EngineCatalog.all)
        let (a1, b1) = try (script.row("press", 0), script.row("row", 1))
        try script.send(.touchRow(row: a1))
        try script.tick(a1, kgReps(50, 10))
        let state = try script.tick(b1, kgReps(50, 10), at: script.now.addingTimeInterval(30))
        #expect(state.restClock?.slot == .roundRest)
        let presentation = SessionPresenter.present(state, history: HistoryIndex(), settings: Progression.settings, now: script.now)
        #expect(presentation.notification?.title == "Rest over")
    }

    @Test("Rest over names the exercise when the next set is another exercise's")
    func restOverNamesTheExercise() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(2), setRest: 60),
            EngineCatalog.slot("row", EngineCatalog.row, EngineCatalog.sets(1)),
        ]), exercises: EngineCatalog.all)
        var state = try script.tick(try script.row("press", 0), kgReps(50, 10))
        var presentation = SessionPresenter.present(state, history: HistoryIndex(), settings: Progression.settings,
                                                    now: script.now.addingTimeInterval(62))
        #expect(presentation.restBar == .restOver("Rest over. Go: set 2, 50 kg × 10"))
        state = try script.tick(try script.row("press", 1), kgReps(50, 10), at: script.now.addingTimeInterval(70))
        let end = try #require(state.countdown?.endsAt)
        presentation = SessionPresenter.present(state, history: HistoryIndex(), settings: Progression.settings,
                                                now: end.addingTimeInterval(2))
        #expect(presentation.restBar == .restOver("Rest over. Go: Row, set 1, 50 kg × 8"))
    }

    @Test("an empty session asks for an exercise instead of offering Finish")
    func emptySession() throws {
        var script = EngineScript(at: engineEpoch)
        let state = try script.start(nil, exercises: [])
        let presentation = SessionPresenter.present(state, history: HistoryIndex(), settings: Progression.settings, now: script.now)
        #expect(presentation.restBar == .empty)
        #expect(!presentation.allDone)
        #expect(presentation.liveActivity.next == "No exercises yet")
    }

    /// A weighted plank added in the gym with no history: no ghost load, so
    /// the Lock Screen's Stop would be refused.
    @Test("the Lock Screen offers Stop only when the timed set lacks nothing but its time")
    func stopNeedsTheOtherValues() throws {
        let weighted = EngineCatalog.exercise("weighted-plank", .weightduration, restClass: .timed, equipment: .plate)
        var script = EngineScript(at: engineEpoch)
        try script.start(nil, exercises: [])
        try script.send(.addExercise(.init(slot: "plank", exercise: weighted, roles: [.working], rowIDs: script.rowIDs(1))))
        let row = try script.row("plank", 0)
        var state = try script.send(.startTimedSet(row: row, target: 60))
        var presentation = SessionPresenter.present(state, history: HistoryIndex(), settings: Progression.settings, now: script.now)
        #expect(presentation.liveActivity.nextRowID == row)
        #expect(!presentation.liveActivity.canComplete)
        state = try script.send(.editSet(.init(row: row, values: SetValuesPatch(load: .set(.kg(10))))))
        presentation = SessionPresenter.present(state, history: HistoryIndex(), settings: Progression.settings, now: script.now)
        #expect(presentation.liveActivity.canComplete)
    }

    @Test("the Start conflict's second question does not count zero sets")
    func discardNothing() throws {
        var script = EngineScript(at: engineEpoch)
        let state = try script.start(EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(1))]),
                                     exercises: EngineCatalog.all)
        let presentation = SessionPresenter.present(state, history: HistoryIndex(), settings: Progression.settings, now: script.now)
        #expect(presentation.discardBeforeStartQuestion == "Discard the open session?")
    }
}
