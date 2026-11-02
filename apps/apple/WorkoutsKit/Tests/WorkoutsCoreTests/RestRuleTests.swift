import Foundation
@testable import WorkoutsCore
import Testing

// Rest selection (DESIGN §4.7) and rest measurement (§4.8): every line of
// the table, every default, the notify rule, and the example sessions
// replayed set by set (§10.5).

/// Ticks `row` and returns the rest decision the tick made.
@discardableResult
private func tickAndDecide(_ script: inout EngineScript, _ row: RowID, _ values: SetValues, after seconds: Int = 0,
                           sourceLocation: SourceLocation = #_sourceLocation) throws -> RestDecision
{
    let state = try script.tick(row, values, at: script.now.addingTimeInterval(TimeInterval(seconds)), sourceLocation: sourceLocation)
    let decision = RestRules.after(row, in: state)
    // The tick started exactly what the rule decides.
    #expect(state.restClock?.slot == decision.slot && state.restClock?.planned == decision.seconds, sourceLocation: sourceLocation)
    if decision.seconds > 0 {
        #expect(state.countdown == Countdown(kind: .rest(decision.slot!), startedAt: script.now,
                                             endsAt: script.now.addingTimeInterval(TimeInterval(decision.seconds))),
                sourceLocation: sourceLocation)
    } else {
        #expect(state.countdown == nil, sourceLocation: sourceLocation)
    }
    return decision
}

private func decision(_ slot: RestSlot?, _ seconds: Seconds, _ source: RestSource) -> RestDecision {
    RestDecision(slot: slot, seconds: seconds, source: source)
}

@Suite("Rest rules: the lines of §4.7")
struct RestLineTests {
    @Test("line 0: after the last set no timer runs")
    func lineZero() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(1))]),
                         exercises: EngineCatalog.all)
        #expect(try tickAndDecide(&script, script.row("press", 0), kgReps(50, 10)) == decision(nil, 0, .none))
        #expect(try script.current.upNext == nil)
    }

    @Test("line 1: a per-set override applies once its chain is complete, and wins over the warm-up rest")
    func lineOne() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, [
            PlannedSet(key: "w1", role: .warmup, repsMin: 10, repsMax: 10, rest: 90),
            PlannedSet(key: "s1", structure: .drop, load: .kg(50), rest: 200),
            PlannedSet(key: "s2", load: .kg(50)),
        ], setRest: 100)]), exercises: EngineCatalog.all)
        #expect(try tickAndDecide(&script, script.row("press", 0), kgReps(30, 10)) == decision(.override, 90, .plannedSet))
        // The drop's head: the chain is not complete, so the intra-set pause.
        #expect(try tickAndDecide(&script, script.row("press", 1), kgReps(50, 10)) == decision(.intraSetPause, 0, .rule))
        #expect(try tickAndDecide(&script, script.row("press", 2), kgReps(40, 6)) == decision(.override, 200, .plannedSet))
    }

    @Test("line 2: the side switch, from the session slot, the routine slot, else 0")
    func lineTwo() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("lunge", EngineCatalog.lunge, EngineCatalog.sets(2), setRest: 90, sideSwitch: 12),
            EngineCatalog.slot("step", EngineCatalog.exercise("step", laterality: .unilateral), EngineCatalog.sets(1)),
        ]), exercises: EngineCatalog.all + [EngineCatalog.exercise("step", laterality: .unilateral)])
        #expect(try tickAndDecide(&script, script.row("lunge", 0), kgReps(10, 10)) == decision(.sideSwitch, 12, .routineSlot))
        #expect(try tickAndDecide(&script, script.row("lunge", 1), kgReps(10, 10)) == decision(.setRest, 90, .routineSlot))
        try script.send(.setSlotRest(slot: "lunge", rest: .sideSwitch, seconds: 25))
        let longSwitch = try tickAndDecide(&script, script.row("lunge", 2), kgReps(10, 10))
        #expect(longSwitch == decision(.sideSwitch, 25, .sessionSlot))
        #expect(longSwitch.notifies)
        try tickAndDecide(&script, script.row("lunge", 3), kgReps(10, 10))
        #expect(try tickAndDecide(&script, script.row("step", 0), kgReps(0, 10)) == decision(.sideSwitch, 0, .rule))
    }

    @Test("line 3: the intra-set pause, the head's planned one, else by structure")
    func lineThree() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, [
            PlannedSet(key: "s1", structure: .myo, load: .kg(30)),
            PlannedSet(key: "s2", structure: .restpause, load: .kg(30)),
            PlannedSet(key: "s3", structure: .cluster, load: .kg(30), miniSetReps: 3, miniSets: 1),
            PlannedSet(key: "s4", structure: .myomatch, load: .kg(30), totalReps: 20, intraSetPause: 10),
        ])]), exercises: EngineCatalog.all)
        #expect(try tickAndDecide(&script, script.row("press", 0), kgReps(30, 16)) == decision(.intraSetPause, 15, .rule))
        // Stop the myo chain by removing its pending mini-set.
        try script.send(.removeSet(row: script.row("press", 1)))
        #expect(try tickAndDecide(&script, script.row("press", 1), kgReps(30, 12)) == decision(.intraSetPause, 20, .rule))
        try script.send(.removeSet(row: script.row("press", 2)))
        #expect(try tickAndDecide(&script, script.row("press", 2), kgReps(30, 3)) == decision(.intraSetPause, 20, .rule))
        try tickAndDecide(&script, script.row("press", 3), kgReps(30, 3))
        #expect(try tickAndDecide(&script, script.row("press", 4), kgReps(30, 8)) == decision(.intraSetPause, 10, .plannedSet))
        for (structure, seconds) in [(Structure.drop, 0), (.myo, 15), (.myomatch, 15), (.restpause, 20), (.cluster, 20), (.straight, 0)] {
            #expect(RestDefaults.intraSetPause(structure) == seconds)
        }
    }

    @Test("line 4: after a warm-up, the session slot's warm-up rest, the routine slot's, else the setting")
    func lineFour() throws {
        var script = EngineScript(at: engineEpoch)
        let warmups = [PlannedSet(key: "w1", role: .warmup, repsMin: 10, repsMax: 10), PlannedSet(key: "w2", role: .warmup, repsMin: 5, repsMax: 5),
                       PlannedSet(key: "s1", load: .kg(50))]
        var settings = SessionSettings()
        settings.warmupRest = 60
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, warmups, warmupRest: 50),
            EngineCatalog.slot("row", EngineCatalog.row, warmups),
        ]), exercises: EngineCatalog.all, settings: settings)
        #expect(try tickAndDecide(&script, script.row("press", 0), kgReps(30, 10)) == decision(.warmupRest, 50, .routineSlot))
        try script.send(.setSlotRest(slot: "press", rest: .warmupRest, seconds: 30))
        #expect(try tickAndDecide(&script, script.row("press", 1), kgReps(40, 5)) == decision(.warmupRest, 30, .sessionSlot))
        // A warm-up is a warm-up even when the next set is elsewhere.
        #expect(try tickAndDecide(&script, script.row("row", 1), kgReps(40, 5)) == decision(.warmupRest, 60, .settings))
    }

    @Test("line 5: the next member of the round, after the session slot's transition, the routine slot's, else the default")
    func lineFive() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(3), group: "a", transition: 20),
            EngineCatalog.slot("row", EngineCatalog.row, EngineCatalog.sets(3), group: "a"),
            EngineCatalog.slot("curl", EngineCatalog.curl, EngineCatalog.sets(3), group: "a"),
        ], groups: [RoutineGroup(key: "a", groupKind: .triset, roundRest: 75)]), exercises: EngineCatalog.all)
        #expect(try tickAndDecide(&script, script.row("press", 0), kgReps(50, 10)) == decision(.transition, 20, .routineSlot))
        // No transition named: a tri-set's default.
        #expect(try tickAndDecide(&script, script.row("row", 0), kgReps(50, 10)) == decision(.transition, 15, .rule))
        try tickAndDecide(&script, script.row("curl", 0), kgReps(20, 10))
        try script.send(.setSlotRest(slot: "press", rest: .transition, seconds: 5))
        #expect(try tickAndDecide(&script, script.row("press", 1), kgReps(50, 10)) == decision(.transition, 5, .sessionSlot))
        // The pairing decides when there is one.
        try script.send(.setGroup(.init(group: "a", pairing: .set(.samemuscle))))
        #expect(try tickAndDecide(&script, script.row("row", 1), kgReps(50, 10)) == decision(.transition, 0, .rule))
    }

    @Test("the default transition by pairing, else by kind")
    func transitionDefaults() {
        for pairing in [Pairing.samemuscle, .preexhaust, .mechanicaldrop] {
            #expect(RestDefaults.transition(kind: .paired, pairing: pairing) == 0)
        }
        for pairing in [Pairing.antagonist, .upperlower, .unrelated] {
            #expect(RestDefaults.transition(kind: .superset, pairing: pairing) == 15)
        }
        #expect(RestDefaults.transition(kind: .paired, pairing: nil) == 60)
        #expect(RestDefaults.transition(kind: .triset, pairing: nil) == 15)
        #expect(RestDefaults.transition(kind: .giantset, pairing: nil) == 15)
        #expect(RestDefaults.transition(kind: .circuit, pairing: nil) == 15)
        #expect(RestDefaults.transition(kind: .superset, pairing: nil) == 0)
    }

    @Test("line 6: the next round, after the session group's round rest, the routine group's, else the default")
    func lineSix() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(3), group: "a"),
            EngineCatalog.slot("curl", EngineCatalog.curl, EngineCatalog.sets(3), group: "a"),
        ], groups: [RoutineGroup(key: "a", groupKind: .superset, roundRest: 75)]), exercises: EngineCatalog.all)
        #expect(try tickAndDecide(&script, script.row("press", 0), kgReps(50, 10)) == decision(.transition, 0, .rule))
        #expect(try tickAndDecide(&script, script.row("curl", 0), kgReps(20, 10)) == decision(.roundRest, 75, .routineGroup))
        try tickAndDecide(&script, script.row("press", 1), kgReps(50, 10))
        try script.send(.setGroup(.init(group: "a", roundRest: .set(40))))
        #expect(try tickAndDecide(&script, script.row("curl", 1), kgReps(20, 10)) == decision(.roundRest, 40, .sessionGroup))
        try script.send(.setGroup(.init(group: "a", roundRest: .cleared)))
        try tickAndDecide(&script, script.row("press", 2), kgReps(50, 10))
        // The last round's last member: the next block, after the group's round rest.
        try script.send(.addExercise(.init(slot: "raise", exercise: EngineCatalog.raise)))
        #expect(try tickAndDecide(&script, script.row("curl", 2), kgReps(20, 10)) == decision(.blockRest, 75, .routineGroup))
    }

    @Test("the default round rest: a circuit 2:00, a group with a compound 2:30, else 1:30")
    func roundRestDefaults() throws {
        #expect(RestDefaults.roundRest(kind: .circuit, memberRestClasses: [.heavycompound]) == 120)
        for heavy in [RestClass.heavycompound, .compound, .power, .carry] {
            #expect(RestDefaults.roundRest(kind: .superset, memberRestClasses: [.isolation, heavy]) == 150)
        }
        #expect(RestDefaults.roundRest(kind: .giantset, memberRestClasses: [.isolation, .highrep, .timed, nil]) == 90)
        // A group made in the gym has no routine group, even when its key matches one.
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("curl", EngineCatalog.curl, EngineCatalog.sets(1)),
            EngineCatalog.slot("raise", EngineCatalog.raise, EngineCatalog.sets(2)),
        ]), exercises: EngineCatalog.all)
        try script.send(.groupExercises(slot: "raise", partner: "curl", group: nil))
        try tickAndDecide(&script, script.row("curl", 0), kgReps(20, 10))
        #expect(try tickAndDecide(&script, script.row("raise", 0), kgReps(6, 12)) == decision(.roundRest, 90, .rule))
    }

    @Test("line 7: the next block, after the group's round rest or the slot's set rest, or nothing when switched off")
    func lineSeven() throws {
        for restAfterLastSet in [true, false] {
            var script = EngineScript(at: engineEpoch)
            var settings = SessionSettings()
            settings.restAfterLastSet = restAfterLastSet
            try script.start(EngineCatalog.routine([
                EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(1), setRest: 100),
                EngineCatalog.slot("row", EngineCatalog.row, EngineCatalog.sets(1), group: "a"),
                EngineCatalog.slot("curl", EngineCatalog.curl, EngineCatalog.sets(1), group: "a"),
                EngineCatalog.slot("raise", EngineCatalog.raise, EngineCatalog.sets(1)),
            ], groups: [RoutineGroup(key: "a", groupKind: .superset)]), exercises: EngineCatalog.all, settings: settings)
            let press = try tickAndDecide(&script, script.row("press", 0), kgReps(50, 10))
            try tickAndDecide(&script, script.row("row", 0), kgReps(50, 10))
            let group = try tickAndDecide(&script, script.row("curl", 0), kgReps(20, 10))
            if restAfterLastSet {
                #expect(press == decision(.blockRest, 100, .routineSlot))
                // Row and curl are compounds and an isolation: 2:30.
                #expect(group == decision(.blockRest, 150, .rule))
            } else {
                #expect(press == decision(.blockRest, 0, .none))
                #expect(group == decision(.blockRest, 0, .none))
            }
        }
    }

    @Test("line 8: the set rest, from the session slot, the routine slot, the rest class's setting, else the fallback")
    func lineEight() throws {
        var script = EngineScript(at: engineEpoch)
        var settings = SessionSettings()
        settings.setRest.isolation = 70
        let plain = EngineCatalog.exercise("plain", restClass: nil)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(3), setRest: 100),
            EngineCatalog.slot("curl", EngineCatalog.curl, EngineCatalog.sets(2)),
            EngineCatalog.slot("plain", plain, EngineCatalog.sets(2)),
        ]), exercises: EngineCatalog.all + [plain], settings: settings)
        #expect(try tickAndDecide(&script, script.row("press", 0), kgReps(50, 10)) == decision(.setRest, 100, .routineSlot))
        try script.send(.setSlotRest(slot: "press", rest: .setRest, seconds: 0))
        // "Off": no countdown; Up next simply moves on.
        #expect(try tickAndDecide(&script, script.row("press", 1), kgReps(50, 10)) == decision(.setRest, 0, .sessionSlot))
        try tickAndDecide(&script, script.row("press", 2), kgReps(50, 10))
        #expect(try tickAndDecide(&script, script.row("curl", 0), kgReps(20, 10)) == decision(.setRest, 70, .settings))
        try tickAndDecide(&script, script.row("curl", 1), kgReps(20, 10))
        #expect(try tickAndDecide(&script, script.row("plain", 0), kgReps(20, 10)) == decision(.setRest, 120, .settings))
    }

    @Test("the set rest defaults by rest class and the other defaults")
    func setRestDefaults() {
        let defaults = SetRestDefaults()
        let expected: [RestClass?: Seconds] = [.heavycompound: 180, .compound: 120, .isolation: 90, .highrep: 60, .timed: 60,
                                               .carry: 120, .power: 180, nil: 120]
        for (restClass, seconds) in expected {
            #expect(defaults[restClass] == seconds)
        }
        #expect(RestDefaults.warmupRest == 45 && SessionSettings().warmupRest == 45)
        #expect(RestDefaults.sideSwitch == 0)
        #expect(SessionSettings().restAfterLastSet && !SessionSettings().countWarmupsInVolume)
    }

    @Test("rests and the standalone timer notify; transitions, pauses and side switches only past 20 s; 0 never")
    func notifyRule() {
        for slot in [RestSlot.override, .warmupRest, .roundRest, .blockRest, .setRest] {
            #expect(RestRules.notifies(slot, seconds: 1))
            #expect(!RestRules.notifies(slot, seconds: 0))
        }
        for slot in [RestSlot.transition, .intraSetPause, .sideSwitch] {
            #expect(!RestRules.notifies(slot, seconds: 20))
            #expect(RestRules.notifies(slot, seconds: 21))
        }
        #expect(!RestRules.notifies(nil, seconds: 60))
        let start = engineEpoch
        #expect(Countdown(kind: .manual, startedAt: start, endsAt: start.addingTimeInterval(10)).notifies)
        #expect(!Countdown(kind: .rest(.transition), startedAt: start, endsAt: start.addingTimeInterval(15)).notifies)
        #expect(Countdown(kind: .rest(.transition), startedAt: start, endsAt: start.addingTimeInterval(30)).notifies)
        #expect(Countdown(kind: .timedSet("r"), startedAt: start, endsAt: start.addingTimeInterval(30)).notifies)
        #expect(!Countdown(kind: .timedSet("r"), startedAt: start, endsAt: nil).notifies)
    }
}

// MARK: - The examples, replayed

@Suite("Rest rules: the example sessions")
struct RestReplayTests {
    static let sessions = [
        ("sessions", EngineFixtures.day1First),
        ("sessions", EngineFixtures.day1Second),
        ("sessions", EngineFixtures.fullyBody),
        ("coverage", EngineFixtures.coverage),
    ]

    @Test("every example set's planned rest, measured rest and skip come out of the rules", arguments: RestReplayTests.sessions)
    func replay(file: String, id: String) throws {
        let replay = try ExampleReplay.run(EngineFixtures.session(file, id))
        let ours = Dictionary(uniqueKeysWithValues: try replay.records().sets.map { ($0.id, $0) })
        for set in replay.fixture.sets {
            let mine = try #require(ours[set.id])
            #expect(mine.restPlanned == set.restPlanned, "restPlanned of \(set.id)")
            #expect(mine.restBefore == set.restBefore, "restBefore of \(set.id)")
            #expect(mine.restSkipped == set.restSkipped, "restSkipped of \(set.id)")
        }
    }

    @Test("27 October: each kind of rest §4.7 names, from its own slot")
    func fullyBodySlots() throws {
        let replay = try ExampleReplay.run(EngineFixtures.session("sessions", EngineFixtures.fullyBody))
        let expected: [(String, RestSlot)] = [
            ("01a17d94-f708-797f-9387-d4893d457dfa", .setRest), // plank 2 after plank 1, 15 s
            ("01a17d96-0c60-7bb3-a536-16069b31abb8", .blockRest), // crunch 1 after plank 2, 15 s
            ("01a17d9a-a428-764e-ac70-c6e1d40de631", .transition), // bird dog 1 after glute bridge 1, 0
            ("01a17d9c-3a68-7d92-b30f-89c5c2b0437d", .roundRest), // glute bridge 2 after bird dog 1, 1:00
            ("01a17d9f-e3e8-7ba0-bfae-d46d6a4d1451", .sideSwitch), // Pallof 1 right after 1 left, 10 s
            ("01a17da8-2918-7803-beb8-3cf24a5598ee", .warmupRest), // chest press 1 after its warm-up, 45 s
            ("01a17daa-f3f0-7c4d-9e6f-708192031425", .intraSetPause), // chest press 2a after 2, 0
            ("01a17d9f-2c50-74dd-89aa-1c8eef1b8492", .blockRest), // Pallof 1 left after the superset: its round rest
        ]
        for (id, slot) in expected {
            #expect(replay.restSlots[SetID(id)] == slot, "rest slot before \(id)")
        }
    }

    @Test("the coverage session and the hotel gym session: warm-up rests per slot, paired sets, a circuit, mini-sets")
    func coverageSlots() throws {
        let replay = try ExampleReplay.run(EngineFixtures.session("coverage", EngineFixtures.coverage))
        let expected: [(String, RestSlot)] = [
            ("01a181f8-7068-73f4-8e27-bbcb79d0ed1d", .warmupRest), // leg press W1 after the rowing warm-up, 45 s
            ("01a181f9-f708-7a19-86e7-d0bc28959ad6", .warmupRest), // the top set after the slot's 1:00 warm-up rest
            ("01a18205-39d8-73a9-9e67-6d959031fb3e", .intraSetPause), // myo-rep match 2a, 10 s
            ("01a1820b-aa48-76a3-be4a-291a7d40b3da", .intraSetPause), // cluster 2, 20 s
            ("01a18212-7490-7407-82ca-12ec2f86a375", .blockRest), // the dip warm-up after the pushdown
            ("01a18213-ad10-76f0-9265-7db113416dfb", .warmupRest), // pull-up 1 after the dip warm-up, 45 s
            ("01a18215-0ca0-7609-8768-ce74caf3e770", .transition), // dip 1 after pull-up 1, 1:00
            ("01a18216-f4e8-79c7-ba7e-253c3d207ce3", .roundRest), // pull-up 2, 1:30
            ("01a1821d-f1f8-7f52-8abb-ae1b158e2771", .blockRest), // the carry after the paired sets' last round
            ("01a1821f-e210-73ef-85b9-7a80dcb33bba", .sideSwitch), // side plank right, 10 s
            ("01a18221-5528-7bda-8fea-bff804c6b52b", .transition), // weighted plank after the side plank's second side
            ("01a18222-dbc8-7d81-97be-a9ada36d102c", .roundRest), // carry 2: the circuit's round rest, 1:00
        ]
        for (id, slot) in expected {
            #expect(replay.restSlots[SetID(id)] == slot, "rest slot before \(id)")
        }
    }

    @Test("29 October: rests set in the gym on an empty session's slots and group")
    func hotelGymRests() throws {
        let fixture = try EngineFixtures.session("coverage", EngineFixtures.hotelGym)
        let exercises = try EngineFixtures.exercises()
        let replay = try ExampleReplay.run(fixture, prepare: { script, fixture in
            for slot in fixture.session.slots {
                let roles = fixture.sets.filter { $0.slot == slot.key }.map(\.role)
                try script.send(.addExercise(.init(slot: slot.key, exercise: #require(exercises[slot.exercise]), roles: roles)))
            }
            try script.send(.setSlotRest(slot: "leg-extension", rest: .warmupRest, seconds: 30))
            try script.send(.groupExercises(slot: "seated-row", partner: "chest-press", group: nil))
            try script.send(.setSlotRest(slot: "chest-press", rest: .transition, seconds: 15))
            try script.send(.setGroup(.init(group: "a", pairing: .set(.antagonist), roundRest: .set(90))))
        })
        let ours = Dictionary(uniqueKeysWithValues: try replay.records().sets.map { ($0.id, $0) })
        for set in fixture.sets {
            #expect(ours[set.id]?.restPlanned == set.restPlanned, "restPlanned of \(set.id)")
            #expect(ours[set.id]?.restBefore == set.restBefore, "restBefore of \(set.id)")
        }
        #expect(replay.restSlots[SetID("01a188d9-16f8-7752-a873-f3b7709309ce")] == .warmupRest)
        #expect(replay.restSlots[SetID("01a188dd-6870-7808-a4ea-ce14cf769c54")] == .blockRest)
        #expect(replay.restSlots[SetID("01a188de-3f48-795d-b7b7-2bed0de9bd11")] == .transition)
        #expect(replay.restSlots[SetID("01a188e0-2b78-76b9-89b1-d266b1d3b50e")] == .roundRest)
    }

    @Test("22 October: a warm-up's own rest overrides the warm-up rest before the first working set")
    func overrideAfterWarmup() throws {
        let replay = try ExampleReplay.run(EngineFixtures.session("sessions", EngineFixtures.day1First))
        #expect(replay.restSlots[SetID("01a16369-0770-7d84-b8fb-2bdec3590e78")] == .override)
        #expect(replay.restSlots[SetID("01a16366-f048-7d25-8ece-ab06c4283a1b")] == .warmupRest)
    }
}

// MARK: - Rest measurement

@Suite("Rest measurement (§4.8)")
struct RestMeasurementTests {
    @Test("22 October iso-lateral set 3: Skip at 1:38 of 2:00 is a skipped rest of 1:38")
    func skipped() throws {
        let replay = try ExampleReplay.run(EngineFixtures.session("sessions", EngineFixtures.day1First))
        let set = try #require(try replay.records().sets.first { $0.id == "01a16376-78d8-741d-8b7f-3e01a0258499" })
        #expect(set.restBefore == 98 && set.restPlanned == 120 && set.restSkipped == true)
    }

    @Test("26 October: a pause inside a rest stops the session clock, not the rest")
    func pauseInsideRest() throws {
        let replay = try ExampleReplay.run(EngineFixtures.session("sessions", EngineFixtures.day1Second))
        let records = try replay.records()
        #expect(records.session.pauses == replay.fixture.session.pauses)
        let raise = try #require(records.sets.first { $0.id == "01a17852-54c0-7fe2-9bcf-fd968468291c" })
        #expect(raise.restBefore == 390 && raise.restPlanned == 120)
    }

    @Test("27 October plank 2: the rest ends at the play tap, not at the tick")
    func playTap() throws {
        let replay = try ExampleReplay.run(EngineFixtures.session("sessions", EngineFixtures.fullyBody))
        let plank = try #require(try replay.records().sets.first { $0.id == "01a17d94-f708-797f-9387-d4893d457dfa" })
        #expect(plank.restBefore == 32 && plank.startedAt != nil)
        #expect(plank.at.timeIntervalSince(plank.startedAt!) == 53)
    }

    @Test("a rep set ticked with no touch first counts its own working time in the rest; the first set has no rest fields")
    func noTouch() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(3), setRest: 60)]),
                         exercises: EngineCatalog.all)
        var state = try script.tick(script.row("press", 0), kgReps(50, 10))
        #expect(state.rows[try script.row("press", 0)]?.performed?.restBefore == nil)
        state = try script.tick(script.row("press", 1), kgReps(50, 10), at: script.now.addingTimeInterval(95))
        #expect(state.rows[try script.row("press", 1)]?.performed?.restBefore == 95)
        // A touch during the countdown leaves it running; one after its end ends the rest.
        try script.send(.touchRow(row: script.row("press", 2)), at: script.now.addingTimeInterval(30))
        try script.send(.touchRow(row: script.row("press", 2)), at: script.now.addingTimeInterval(40))
        try script.send(.touchRow(row: script.row("press", 2)), at: script.now.addingTimeInterval(10))
        state = try script.tick(script.row("press", 2), kgReps(50, 10), at: script.now.addingTimeInterval(30))
        #expect(state.rows[try script.row("press", 2)]?.performed?.restBefore == 70)
    }
}
