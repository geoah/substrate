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

    @Test("it holds every measurement, side, load basis, structure, ending, timer and stop rule the app runs")
    func everyFeature() throws {
        let (routine, catalog) = try Self.sample()
        let sets = routine.slots.flatMap(\.sets)
        let exercises = try routine.slots.map { try #require(catalog.exercises[$0.exercise], "no exercise for \($0.key)") }
        #expect(routine.slots.count == 13)
        #expect(sets.count == 25)
        #expect(routine.notes != nil)
        #expect(routine.slots.allSatisfy { $0.notes != nil }, "every exercise says what it shows")

        // Every case of each of these, so a feature dropped from the sample,
        // or added to the app without a place in it, fails here. What the
        // sample leaves out on purpose is named, as sample.yaml's header says.
        #expect(Set(exercises.map(\.measurement)) == Set(MeasurementType.allCases))
        #expect(Set(exercises.map(\.resolvedLaterality)) == Set(Laterality.allCases))
        #expect(Set(exercises.compactMap(\.loadBasis)) == Set(LoadBasis.allCases))
        #expect(Set(sets.map(\.structure)) == Set(Structure.allCases))
        #expect(Set(sets.compactMap(SessionEngine.impliedTermination(of:))) == Set(Termination.allCases))
        #expect(Set(sets.compactMap(\.roleLabel)) == Set(RoleLabel.allCases))
        #expect(Set(sets.map(\.role)) == Set(Role.allCases))
        #expect(Set(exercises.compactMap(\.myoStopRule) + sets.compactMap(\.myoStopRule)) == Set(MyoStopRule.allCases))
        let timed = sets.filter { $0.durationMin != nil }
        #expect(Set(timed.map { $0.timerMode ?? .countdown }) == Set(TimerMode.allCases))
        #expect(Set(routine.groups.map(\.groupKind)).union([.triset, .giantset, .paired]) == Set(GroupKind.allCases))
        #expect(Set(sets.compactMap(\.loadReference)) == [.bodymass], "the one reference V1 resolves")
        #expect(routine.deload == nil && routine.activityType == nil)

        // The finer options, each once.
        #expect(sets.contains { $0.loadPercent != nil })
        #expect(sets.contains { $0.dropPercent != nil } && sets.contains { $0.dropStep != nil } && sets.contains { $0.dropReps != nil })
        #expect(sets.contains { $0.totalReps != nil && $0.structure == .restpause })
        #expect(sets.contains { $0.rirMin != nil } && sets.contains { $0.rpeMin != $0.rpeMax })
        #expect(sets.contains { $0.repsMin != $0.repsMax && $0.repsMax != nil })
        #expect(sets.contains { $0.assistance != nil } && sets.contains { $0.tempo != nil } && sets.contains { $0.rest != nil })
        #expect(sets.contains { $0.miniSetReps != nil } && sets.contains { $0.intraSetPause != nil })

        // The rests and the sides.
        let legPress = try Self.slot(routine, "leg-press")
        #expect(legPress.sets[0].loadPercent == 50 && legPress.sets[0].loadReference == .bodymass)
        #expect(legPress.warmupRest == 60 && legPress.setRest == 150)
        let superset = try #require(routine.groups.first { $0.key == "a" })
        #expect(superset.groupKind == .superset && superset.roundRest == 90 && superset.rounds == nil)
        #expect(try Self.slot(routine, "pull-up").transition == 0)
        #expect(routine.slots.filter { $0.group == "a" }.map(\.key) == ["pull-up", "push-up"])
        let circuit = try #require(routine.groups.first { $0.key == "b" })
        #expect(circuit.groupKind == .circuit && circuit.rounds == 2 && circuit.roundRest == 60)
        #expect(try Self.slot(routine, "plank").transition == 15)
        let split = try Self.slot(routine, "split-squat")
        #expect(split.sideOrder == .weakerfirst && split.sideSwitch == 15 && split.sideMatch == true)

        // Signed out it cannot be deleted, so the note says so.
        #expect(routine.notes?.contains("Once you are signed in") == true)
    }

    @Test("the preview says each method in words")
    func preview() throws {
        let (routine, catalog) = try Self.sample()
        let preview = RoutinePresenter.preview(routine, catalog: catalog, history: HistoryIndex(), settings: Progression.settings)
        #expect(preview.name == "Sample: every feature")
        #expect(preview.unreadable.isEmpty)
        let slots = preview.blocks.flatMap(\.slots)
        #expect(slots.count == 13)
        #expect(slots.allSatisfy { $0.notes != nil })
        let text = slots.flatMap(\.lines).joined(separator: "\n")
        for phrase in ["Warm-up: 500 m", "Warm-up: 8 reps", "Warm-up: 12 reps at 50% of body mass", "Set 1 is the top set",
                       "Set 2 backs off 10%", "Warm-up rest 1:00", "RPE 7–8", "Tempo 3-0-1-0", "Rest after set 2: 3:00",
                       "Set 3 is a drop set: 2 drops, 5 kg lighter each, 6 reps each", "BW − 90 lb", "Then straight to A2",
                       "each until 2–3 reps in reserve", "as many reps as possible", "Set 1 is myo-reps, until a mini-set comes in short",
                       "Set 2 is a myo-rep match", "Weaker side first", "Switch sides 0:15", "The second side matches",
                       "Set 2 is rest-pause: 3 mini-sets, to 20 total reps", "Set 1 is a cluster set", "Set 2 to failure",
                       "counted down", "on the stopwatch", "Then 0:15 to B2", "2 sets of 40 m, with 16 kg", "hold 0:30–0:45, counted down, with 10 kg"]
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
        // One between the superset's two rounds, one between the circuit's two.
        #expect(roundRests == 2, "the superset's and the circuit's round rests ran: \(roundRests)")

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
