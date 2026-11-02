import Foundation
@testable import WorkoutsCore
import Testing

// The sample routine the app carries (decision S1, docs/examples/sample.yaml):
// it must hold every method the app runs, and a session through it must
// reach Finish the way the controller drives one.

@Suite("The sample routine")
struct SampleRoutineTests {
    static func sample() throws -> (Routine, Catalog) {
        let catalog = try BundledCatalog().loadNow()
        return (try #require(catalog.routines[SampleRoutine.id]), catalog)
    }

    static func slot(_ routine: Routine, _ key: String) throws -> RoutineSlot {
        try #require(routine.slots.first { $0.key == key }, "no slot \(key)")
    }

    @Test("it holds every method the app runs, and says on each exercise what it shows")
    func everyFeature() throws {
        let (routine, catalog) = try Self.sample()
        let sets = routine.slots.flatMap(\.sets)
        #expect(routine.slots.count == 9)
        #expect(sets.count == 23)
        #expect(routine.notes != nil)
        #expect(routine.slots.allSatisfy { $0.notes != nil }, "every exercise says what it shows")

        // Warm-up at a percentage of body weight, top and back-off sets, the slot's own rests.
        let legPress = try Self.slot(routine, "leg-press")
        #expect(legPress.sets.map(\.role) == [.warmup, .working, .working])
        #expect(legPress.sets[0].loadPercent == 50 && legPress.sets[0].loadReference == .bodymass)
        #expect(legPress.sets.compactMap(\.roleLabel) == [.top, .backoff])
        #expect(legPress.sets[2].dropPercent == 10)
        #expect(legPress.warmupRest == 60 && legPress.setRest == 150)

        // Straight sets with rep and RPE ranges at a tempo, a set's own rest, a drop set.
        let chest = try Self.slot(routine, "chest-press")
        #expect(chest.sets[0].repsMin == 8 && chest.sets[0].repsMax == 12)
        #expect(chest.sets[0].rpeMin == 7 && chest.sets[0].rpeMax == 8)
        #expect(chest.sets[0].tempo == "3-0-1-0")
        #expect(chest.sets[1].rest == 180)
        #expect(chest.sets[2].structure == .drop && chest.sets[2].drops == 2)

        // A superset with no rest inside it and a rest after each round.
        let superset = try #require(routine.groups.first { $0.key == "a" })
        #expect(superset.groupKind == .superset && superset.roundRest == 90)
        #expect(try Self.slot(routine, "pull-up").transition == 0)
        #expect(routine.slots.filter { $0.group == "a" }.map(\.key) == ["pull-up", "push-up"])
        // Assisted and bodyweight, an RIR stop and an AMRAP.
        #expect(try Self.slot(routine, "pull-up").sets.allSatisfy { $0.assistance == .lb(60) })
        #expect(catalog.exercises["assisted-pull-up-machine"]?.measurement == .bodyweightassistedreps)
        #expect(catalog.exercises["push-up"]?.measurement == .bodyweightreps)
        let push = try Self.slot(routine, "push-up")
        #expect(push.sets.map(\.termination) == [.rirstop, .rirstop, .amrap])
        #expect(push.sets[0].rirMin == 2 && push.sets[0].rirMax == 3)

        // Myo-reps and a myo-rep match.
        #expect(try Self.slot(routine, "lateral-raise").sets.map(\.structure) == [.myo, .myomatch])

        // One side at a time.
        let split = try Self.slot(routine, "split-squat")
        #expect(catalog.exercises[split.exercise]?.resolvedLaterality == .unilateral)
        #expect(split.sideOrder == .left && split.sideSwitch == 15 && split.sideMatch == true)

        // A cluster, a rest-pause and a set to failure.
        let pushdown = try Self.slot(routine, "pushdown")
        #expect(pushdown.sets.map(\.structure) == [.cluster, .restpause, .straight])
        #expect(pushdown.sets[2].termination == .failure)

        // A circuit of two rounds: a timed hold on a countdown then a stopwatch, a carried distance.
        let circuit = try #require(routine.groups.first { $0.key == "b" })
        #expect(circuit.groupKind == .circuit && circuit.rounds == 2 && circuit.roundRest == 60)
        let plank = try Self.slot(routine, "plank")
        #expect(catalog.exercises[plank.exercise]?.measurement == .duration)
        #expect(plank.sets[0].durationMin == 45 && plank.sets[0].durationMax == 60 && plank.sets[0].timerMode == nil)
        #expect(plank.sets[1].timerMode == .stopwatch)
        let carry = try Self.slot(routine, "carry")
        #expect(carry.sets.allSatisfy { $0.termination == .distance && $0.distance != nil && $0.load != nil })
    }

    @Test("the preview says each method in words")
    func preview() throws {
        let (routine, catalog) = try Self.sample()
        let preview = RoutinePresenter.preview(routine, catalog: catalog, history: HistoryIndex(), settings: Progression.settings)
        #expect(preview.name == "Sample: every feature")
        #expect(preview.unreadable.isEmpty)
        let slots = preview.blocks.flatMap(\.slots)
        #expect(slots.count == 9)
        #expect(slots.allSatisfy { $0.notes != nil })
        let text = slots.flatMap(\.lines).joined(separator: "\n")
        for phrase in ["Warm-up: 50% of body mass \u{00D7} 12", "Set 1 is the top set", "Set 2 backs off 10%", "Warm-up rest 1:00",
                       "RPE 7–8", "Tempo 3-0-1-0", "Rest after set 2: 3:00", "Set 3 is a drop set: 2 drops", "BW − 60 lb",
                       "Then straight to A2", "each until 2–3 reps in reserve", "as many reps as possible", "Set 1 is myo-reps",
                       "Set 2 is a myo-rep match", "Left side first", "Switch sides 0:15", "The second side matches",
                       "Set 1 is a cluster set", "Set 2 is rest-pause", "Set 3 to failure", "counted down", "on the stopwatch",
                       "Then 0:15 to B2", "2 sets of 40 m, with 16 kg"]
        {
            #expect(text.localizedCaseInsensitiveContains(phrase), "the preview names \(phrase):\n\(text)")
        }
        // The two groups show as their own blocks with labels.
        #expect(preview.blocks.compactMap(\.header) == ["Superset A, rest 1:30 after each round", "Circuit B, 2 rounds, rest 1:00 after each round"])
        #expect(slots.compactMap(\.groupLabel) == ["A1", "A2", "B1", "B2"])
    }

    @Test("a session through it reaches Finish with every method in its records")
    func walk() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("workouts-sample-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let (_, catalog) = try Self.sample()
        var walk = try ControllerWalk(at: ControllerWalkTests.friday, history: HistoryIndex(), directory: directory, catalog: catalog)
        try walk.start(SampleRoutine.id, bodyMass: .kg(80))

        // The warm-up's load is half the body weight.
        let warmup = try walk.row("leg-press", 0)
        #expect(try walk.state.rows[warmup]?.target?.load == .kg(40))
        // The split squat is a side chain: left first.
        #expect(try walk.state.orderedRows(inSlot: "split-squat").map(\.side) == [.left, .right, .left, .right])

        // Every set, in the order Up next offers them: timed sets by Play and
        // Stop, a set with nothing to commit given its reps first.
        var ticks = 0
        var roundRests = 0
        while let row = walk.upNext, ticks < 200 {
            walk.wait(20)
            try walk.touch(row)
            let shown = try #require(try walk.shown.row(row))
            if case let .ready(target)? = shown.timer {
                try walk.send(.startTimedSet(row: row, target: target))
                walk.wait(target.map { Int($0) } ?? 50)
                let stop = try #require(try walk.shown.row(row)?.stopValues, "the timed set \(shown.slot) cannot stop")
                try walk.commit(row, values: stop, stop: true)
            } else {
                if shown.commitValues == nil {
                    var reps = SetValuesPatch()
                    reps.reps = .set(8)
                    try walk.edit(row, reps)
                }
                let values = try #require(try walk.shown.row(row)?.commitValues,
                                          "\(shown.slot) \(shown.label.short) cannot tick: missing \(shown.missing)")
                try walk.commit(row, values: values, stop: false)
            }
            if try walk.state.restClock?.slot == .roundRest { roundRests += 1 }
            ticks += 1
        }
        #expect(walk.upNext == nil, "every set was offered and done")
        // Two between the superset's three rounds, one between the circuit's two.
        #expect(roundRests == 3, "the superset's and the circuit's round rests ran: \(roundRests)")

        let state = try walk.state
        let summary = FinishPresenter.summary(state, presentation: try walk.shown, policy: .drop, end: walk.now, healthAllowed: true)
        try walk.send(.finish(summary.projection.event(start: summary.start, end: walk.now.flooredToSecond, sessionRPE: 7,
                                                       notes: nil, utcOffset: 0)))
        let records = SessionEngine.records(try walk.state, adapter: "substrate-workouts@0.1.0")
        #expect(records.session.routine == SampleRoutine.id)
        #expect(records.session.slots.map(\.key) == state.slots.map(\.key))
        let structures = Set(records.sets.map(\.structure))
        for structure in [Structure.straight, .drop, .myo, .myomatch, .cluster, .restpause] {
            #expect(structures.contains(structure), "a \(structure) set was written")
        }
        #expect(records.sets.contains { $0.role == .warmup })
        #expect(Set(records.sets.compactMap(\.side)) == [.left, .right])
        #expect(records.sets.contains { $0.duration != nil })
        #expect(records.sets.contains { $0.distance != nil })
        #expect(records.sets.contains { $0.assistance != nil })
    }
}
