import Foundation
@testable import WorkoutsCore
import Testing

// Fixes from the data and UI review of the Rows layout (2026-10-31): the
// Table's RPE sheet rates what it confirms, a planned set's unknown fields
// stay out of `set.target`, ratings read only known methods on an RIR-based
// scale, a journal from before effort ranges resumes with its targets, a
// timed set's rest is measured to its start, superset connectors honour a
// per-set rest, and the capsule's session clock stops while paused. Each
// test but the volume one failed before its fix; that one pins the rule the
// package README and DESIGN §4.11 now state.

private let kinds = KindNames.v1

/// One press slot whose planned sets aim at RPE 8, with a set rest of 90 s.
private func pressSession() throws -> EngineScript {
    var script = EngineScript(at: engineEpoch)
    try script.start(EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(3), setRest: 90)]),
                     exercises: EngineCatalog.all)
    return script
}

private func presented(_ script: EngineScript, prefills: Bool = false) throws -> SessionPresentation {
    var settings = Progression.settings
    settings.prefillsRPE = prefills
    return SessionPresenter.present(try script.current, history: HistoryIndex([]), settings: settings, now: script.now)
}

@Suite("Rows review fixes: effort provenance")
struct RowsReviewEffortTests {
    @Test("the Table's RPE sheet rates what Done confirms: a copied or pre-filled value becomes entered, an entered one sends nothing")
    func sheetPatch() throws {
        var script = try pressSession()
        let (s1, s2) = try (script.row("press", 0), script.row("press", 1))

        // PREVIOUS copied 9.5; Done on the 9.5 the sheet opens on rates it.
        let previous = PreviousSet(session: "past", date: engineEpoch, record: "p1",
                                   values: SetValues(load: .kg(35), reps: 13, rpe: 9.5, rpeMethod: .entered),
                                   text: "35 kg × 13 @ 9.5", deload: false)
        try script.send(.editSet(.init(row: s1, values: previous.copy)))
        var row = try #require(try presented(script).row(s1))
        #expect(row.rpeEntry == .value(9.5, .copied))
        let patch = try #require(row.rpePatch(for: 9.5))
        #expect(patch == .enteringRPE(9.5))
        try script.send(.editSet(.init(row: s1, values: patch)))
        row = try #require(try presented(script).row(s1))
        #expect(row.rpeEntry == .value(9.5, .entered))
        // Done again on the same rating changes nothing; Clear clears it.
        #expect(row.rpePatch(for: 9.5) == nil)
        #expect(row.rpePatch(for: 9) == .enteringRPE(9))
        #expect(row.rpePatch(for: nil) == .clearingRPE)
        let state = try script.tick(s1, try #require(script.current.rows[s1]).entered)
        #expect(SessionEngine.setRecords(state).first?.rpeMethod == .entered)

        // A pending row with no value of its own: Clear has nothing to clear,
        // even with the plan's pre-fill in the state (the Table never shows it).
        let pending = try #require(try presented(script).row(s2))
        #expect(pending.rpeEntry == .prefill(8))
        #expect(pending.rpePatch(for: nil) == nil)
        #expect(pending.rpePatch(for: 8) == .enteringRPE(8))

        // Logged in Rows with the pre-fill, then the layout switched: Done on
        // the 8 shown makes it the owner's rating.
        let rows = try presented(script, prefills: true)
        try script.tick(s2, try #require(rows.row(s2)?.commitValues))
        let logged = try #require(try presented(script).row(s2))
        #expect(logged.rpeEntry == .value(8, .prefilled))
        try script.send(.editSet(.init(row: s2, values: try #require(logged.rpePatch(for: 8)))))
        #expect(SessionEngine.setRecords(try script.current).first { $0.target?.plannedSet == "s2" }?.rpeMethod == .entered)
    }

    @Test("a method or a scale this build does not know is no rating; a value from rir keeps rirMethod; cr10 is never read as reps in reserve")
    func ratingsReadKnownMethodsOnly() throws {
        let codec = RecordCodec()
        func set(_ extra: JSONObject) throws -> PerformedSet {
            var properties: JSONObject = [
                "at": .string("2026-10-27T13:42:10Z"), "session": .string("\(kinds.session)/s"),
                "exercise": .string("\(kinds.exercise)/press"), "slot": .string("press"), "order": .number(1),
                "loadMode": .string("external"), "load": .object(["value": .string("100"), "unit": .string("kg")]), "reps": .number(10),
            ]
            for (key, value) in extra {
                properties[key] = value
            }
            return try codec.decodeSet(FlatRecord(kind: kinds.set, id: "x", properties: properties))
        }
        // A future method is kept raw, and the value reads as no rating.
        var suggested = try set(["rpe": .string("8"), "rpeMethod": .string("suggested")])
        #expect(suggested.rpeMethod == nil && suggested.extra["rpeMethod"] == .string("suggested"))
        #expect(suggested.ratedRPE == nil && suggested.unratedRPE == nil)
        // Cleared later: the raw method goes with the value.
        suggested.rpe = nil
        #expect(codec.encode(suggested).properties["rpeMethod"] == nil)
        // A set written before the field existed is a rating, as before.
        #expect(try set(["rpe": .string("8")]).ratedRPE == 8)
        #expect(try set(["rir": .string("2"), "rirMethod": .string("guessed")]).ratedRPE == nil)
        // cr10, and a scale this build does not know: no rating.
        #expect(try set(["rpe": .string("7"), "rpeMethod": .string("imported"), "rpeScale": .string("cr10")]).ratedRPE == nil)
        #expect(try set(["rpe": .string("7"), "rpeMethod": .string("imported"), "rpeScale": .string("borg")]).ratedRPE == nil)
        #expect(try set(["rpe": .string("7"), "rpeMethod": .string("imported"), "rpeScale": .string("hevy")]).ratedRPE == 7)

        // The suggestion's done sets: a pre-filled rir stays unrated, with its method.
        let config = SuggestionInputs.config(for: EngineCatalog.press, defaultUnit: .kg)
        let prefilledRIR = try set(["rir": .string("2"), "rirMethod": .string("prefilled")])
        var done = SuggestionInputs.doneSets([prefilledRIR], exercise: EngineCatalog.press, config: config)
        #expect(done.first?.rpe == nil && done.first?.unratedRPE == 8)
        // An imported cr10 RPE enters neither as a rating nor as an assumption.
        let cr10 = try set(["rpe": .string("7"), "rpeMethod": .string("imported"), "rpeScale": .string("cr10")])
        done = SuggestionInputs.doneSets([cr10], exercise: EngineCatalog.press, config: config)
        #expect(done.first?.rpe == nil && done.first?.unratedRPE == nil)
        // An unknown method neither.
        done = SuggestionInputs.doneSets([try set(["rpe": .string("8"), "rpeMethod": .string("suggested")])],
                                         exercise: EngineCatalog.press, config: config)
        #expect(done.first?.rpe == nil && done.first?.unratedRPE == nil)
    }
}

@Suite("Rows review fixes: what the records carry")
struct RowsReviewRecordTests {
    @Test("a planned set's unknown fields reach the session's plan snapshot, never set.target")
    func targetKeepsDeclaredFieldsOnly() throws {
        let codec = RecordCodec()
        let routine = try codec.decodeRoutine(FlatRecord(kind: kinds.routine, id: "r", properties: [
            "name": .string("R"),
            "slots": .array([.object([
                "key": .string("press"), "exercise": .string("\(kinds.exercise)/press"),
                "sets": .array([.object(["key": .string("s1"), "roleLabel": .string("finisher"), "band": .string("red"),
                                         "repsMin": .number(8), "repsMax": .number(12), "rpeMin": .string("8"),
                                         "rpeMax": .string("8")])]),
            ])]),
        ]))
        var script = EngineScript(at: engineEpoch)
        try script.start(routine, exercises: EngineCatalog.all)
        let s1 = try script.row("press", 0)
        #expect(try script.current.rows[s1]?.target?.extra.isEmpty == true)
        let state = try script.tick(s1, kgReps(50, 10, rpe: 8))
        let records = SessionEngine.records(state, adapter: "substrate-workouts@test")
        #expect(records.session.slots.first?.sets.first?.extra == ["roleLabel": .string("finisher"), "band": .string("red")])
        let record = try #require(records.sets.first)
        let target = try #require(codec.encode(record).properties["target"]?.objectValue)
        #expect(target["band"] == nil && target["roleLabel"] == nil)
        #expect(target["plannedSet"] == .string("s1"))
    }

    @Test("a journal from before effort ranges resumes with its targets: a Start event's planned rpe and rir read as ranges of one")
    func legacyJournalStart() throws {
        let legacy = try JSONDecoder().decode(PlannedSet.self, from: Data(
            #"{"key": "s1", "role": "working", "structure": "straight", "rpe": 8, "rir": 2}"#.utf8
        ))
        #expect(legacy.rpeMin == 8 && legacy.rpeMax == 8 && legacy.rirMin == 2 && legacy.rirMax == 2)
        // A range written now wins over nothing; it round-trips as it is.
        let ranged = PlannedSet(key: "s2", rpeMin: 7, rpeMax: 8, extra: ["band": .string("red")])
        #expect(try JSONDecoder().decode(PlannedSet.self, from: JSONEncoder().encode(ranged)) == ranged)

        // The Start event as the older build wrote it: `rpe` in place of the range.
        let script = try pressSession()
        let start = try #require(script.events.first)
        var json = try #require(try JSONSerialization.jsonObject(with: JSONEncoder().encode(start)) as? [String: Any])
        func legacyEffort(_ value: Any) -> Any {
            if var object = value as? [String: Any] {
                if object["key"] != nil, let top = object["rpeMax"] {
                    object["rpeMin"] = nil
                    object["rpeMax"] = nil
                    object["rpe"] = top
                }
                return object.mapValues(legacyEffort)
            }
            if let array = value as? [Any] { return array.map(legacyEffort) }
            return value
        }
        json = try #require(legacyEffort(json) as? [String: Any])
        let old = try JSONDecoder().decode(EventEnvelope.self, from: JSONSerialization.data(withJSONObject: json))
        let resumed = try SessionEngine.fold([old])
        let s1 = try #require(resumed.orderedRows(inSlot: "press").first?.id)
        #expect(resumed.rows[s1]?.target?.rpeMax == 8 && resumed.rpePrefill(ofRow: s1) == 8)
        #expect(resumed == (try script.current))
    }

    @Test("a timed set's rest is measured to its start when its play ended it, else to the touch")
    func restMethodStart() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("plank", EngineCatalog.plank, (1 ... 3).map { PlannedSet(key: "s\($0)", durationMin: 45) }, setRest: 60),
        ]), exercises: EngineCatalog.all)
        let (p1, p2, p3) = try (script.row("plank", 0), script.row("plank", 1), script.row("plank", 2))
        func stop(_ row: RowID, after seconds: Int) throws {
            try script.send(.stopTimedSet(.init(row: row, recordID: script.setID(), values: SetValues(duration: seconds))),
                            at: script.now.addingTimeInterval(TimeInterval(seconds)))
        }
        try script.send(.startTimedSet(row: p1, target: 45), at: engineEpoch.addingTimeInterval(10))
        try stop(p1, after: 45)
        // Played while the 60 s rest still ran: the start ends the rest.
        try script.send(.startTimedSet(row: p2, target: 45), at: script.now.addingTimeInterval(20))
        try stop(p2, after: 50)
        // The rest ran out, a touch ended it, and the play came later.
        try script.send(.touchRow(row: p3), at: script.now.addingTimeInterval(80))
        try script.send(.startTimedSet(row: p3, target: 45), at: script.now.addingTimeInterval(15))
        try stop(p3, after: 40)
        let sets = SessionEngine.setRecords(try script.current)
        #expect(sets.map(\.restBefore) == [nil, 20, 80])
        #expect(sets.map(\.restMethod) == [nil, .start, .interaction])
    }

    @Test("volume counts reps as logged and a per-side load twice unless the set is one side at a time")
    func volumeRule() {
        func set(_ basis: LoadBasis, _ laterality: Laterality, implements: Int? = nil) -> PerformedSet {
            var set = PerformedSet(id: "x", at: engineEpoch, session: "s", exercise: "e", slot: "a", order: 1)
            set.loadMode = .external
            set.load = .kg(10)
            set.reps = 10
            set.loadBasis = basis
            set.implements = implements
            set.laterality = laterality
            set.repsBasis = laterality == .bilateral ? .total : .perside
            return set
        }
        func volume(_ set: PerformedSet) -> Decimal {
            Counting.volume([set], exercises: [:], bodyMass: nil, unit: .kg, includeWarmups: false).value
        }
        // An alternating curl with a pair of 10 kg dumbbells, 10 a side: 10 × 10 × 2.
        #expect(volume(set(.perimplement, .alternating, implements: 2)) == 200)
        #expect(volume(set(.perside, .alternating)) == 200)
        #expect(volume(set(.perside, .bilateral)) == 200)
        #expect(volume(set(.perside, .unilateral)) == 100)
        // A total load on alternating work: one side's reps, as logged.
        #expect(volume(set(.total, .alternating)) == 100)
    }
}

@Suite("Rows review fixes: the Rows layout")
struct RowsReviewLayoutTests {
    @Test("a per-set rest on A1 is the connector's rest, as the countdown runs it")
    func connectorHonoursOverride() throws {
        var session = try RowsSession.fullyBody()
        let a1 = try session.row("glute-bridge", 0)
        try session.send(.setRowRest(row: a1, seconds: 45), at: 30)
        try session.log("glute-bridge", 0, at: 60)
        let layout = try session.layout(at: 69)
        let block = try #require(layout.block("group-a"))
        #expect(block.shape[2] == "Rest 0:45, then A2 (running)")
        guard case let .connector(connector) = block.items[2] else { Issue.record("no connector"); return }
        #expect(connector.seconds == 45 && connector.sentence == "Rest 0:45, then A2")
        #expect(try session.script.current.countdown?.kind == .rest(.override))
        guard case let .resting(rest) = layout.capsule else { Issue.record("not resting"); return }
        #expect(rest.totalText == "of 0:45")
        // Round 2's A1 has no override: its connector is the plan's.
        #expect(block.shape[7] == "0:00, straight to A2 (upcoming)")
    }

    @Test("before the first set the capsule's session clock stands still while the session clock is paused")
    func capsuleClockPauses() throws {
        var session = try RowsSession.dayOne()
        guard case let .upNext(running) = try session.layout(at: 5).capsule else { Issue.record("no up next"); return }
        #expect(running.sinceLabel == "session" && running.pausedAt == nil)
        try session.send(.pauseClock, at: 10)
        guard case let .upNext(paused) = try session.layout(at: 40).capsule else { Issue.record("no up next"); return }
        #expect(paused.sinceLabel == "session" && paused.pausedAt == session.at(10))
        #expect(paused.pausedAt == (try session.present(at: 40)).header.clockPausedAt)
        try session.send(.resumeClock, at: 50)
        guard case let .upNext(resumed) = try session.layout(at: 60).capsule else { Issue.record("no up next"); return }
        #expect(resumed.pausedAt == nil)
        #expect(resumed.since == session.start.addingTimeInterval(40))
    }
}
