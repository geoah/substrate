import Foundation
import Testing

@testable import WorkoutsCore

// A warm-up circuit (proposal 2026-11-05, 1d): a group of warm-up slots alone
// runs its warm-ups as rounds, with the group's transitions between members
// and, absent a round rest of its own, the warm-up rest between rounds. A
// group that mixes keeps every member's warm-ups before round 1.

@Suite("Warm-up circuits", .timeLimit(.minutes(10)))
struct WarmupCircuitTests {
    private func circuit() throws -> EngineScript {
        var script = EngineScript(at: engineEpoch)
        try script.start(
            EngineCatalog.routine(
                [
                    EngineCatalog.slot(
                        "bike", EngineCatalog.curl, [PlannedSet(key: "w1", role: .warmup), PlannedSet(key: "w2", role: .warmup)],
                        group: "warm"),
                    EngineCatalog.slot(
                        "band", EngineCatalog.row, [PlannedSet(key: "w1", role: .warmup), PlannedSet(key: "w2", role: .warmup)],
                        group: "warm"),
                    EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(1)),
                ], groups: [RoutineGroup(key: "warm", groupKind: .circuit)]), exercises: EngineCatalog.all)
        return script
    }

    @Test("the warm-ups run as rounds: each member's first, then each one's second")
    func rounds() throws {
        let script = try circuit()
        let state = try script.current
        let order = try [("bike", 0), ("band", 0), ("bike", 1), ("band", 1), ("press", 0)].map { try script.row($0.0, $0.1) }
        #expect(UpNext.sequence(state) == order)
        #expect(UpNext.isWarmupCircuit(group: "warm", in: state))
        #expect(try UpNext.round(of: script.row("band", 0), in: state) == 1)
        #expect(try UpNext.round(of: script.row("bike", 1), in: state) == 2)
    }

    @Test("the group's transition between members, the warm-up rest between rounds, the warm-up rest out of it")
    func rests() throws {
        var script = try circuit()
        let values = kgReps(20, 10)
        var state = try script.tick(try script.row("bike", 0), values)
        // A circuit's transition by default: 15 s.
        #expect(RestRules.after(try script.row("bike", 0), in: state).slot == .transition)
        state = try script.tick(try script.row("band", 0), values)
        let round = RestRules.after(try script.row("band", 0), in: state)
        #expect(round.slot == .roundRest && round.seconds == RestDefaults.warmupRest && round.source == .settings)
        state = try script.tick(try script.row("bike", 1), values)
        state = try script.tick(try script.row("band", 1), values)
        // Out of the circuit, a warm-up's own rest, as after any warm-up.
        #expect(RestRules.after(try script.row("band", 1), in: state).slot == .warmupRest)
    }

    @Test("a group that mixes keeps every member's warm-ups before round 1")
    func mixed() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(
            EngineCatalog.routine(
                [
                    EngineCatalog.slot("bike", EngineCatalog.curl, [PlannedSet(key: "w1", role: .warmup)], group: "g"),
                    EngineCatalog.slot(
                        "press", EngineCatalog.press, [PlannedSet(key: "w1", role: .warmup), PlannedSet(key: "s1")], group: "g"),
                ], groups: [RoutineGroup(key: "g", groupKind: .superset)]), exercises: EngineCatalog.all)
        let state = try script.current
        #expect(!UpNext.isWarmupCircuit(group: "g", in: state))
        #expect(try UpNext.sequence(state) == [script.row("bike", 0), script.row("press", 0), script.row("press", 1)])
        #expect(try UpNext.round(of: script.row("bike", 0), in: state) == 0)
    }

    @Test("the open warm-up's note: none in a circuit, whose round says it; the warm-up named in a group")
    func editorNote() throws {
        func note(_ script: EngineScript, _ slot: String) throws -> RowsNote? {
            let state = try script.current
            let layout = SessionPresenter.present(
                state, history: HistoryIndex([]), settings: Progression.settings, now: engineEpoch
            ).rowsLayout
            let id = try script.row(slot, 0)
            let line = layout.blocks.lazy.compactMap { $0.item(id) }.first
            return try #require(line?.editor, "\(slot) is not open").note
        }
        #expect(try note(circuit(), "bike") == nil)

        var mixed = EngineScript(at: engineEpoch)
        try mixed.start(
            EngineCatalog.routine(
                [
                    EngineCatalog.slot(
                        "press", EngineCatalog.press,
                        [PlannedSet(key: "w1", role: .warmup), PlannedSet(key: "w2", role: .warmup), PlannedSet(key: "s1")],
                        group: "g"),
                    EngineCatalog.slot("row", EngineCatalog.row, [PlannedSet(key: "s1")], group: "g"),
                ], groups: [RoutineGroup(key: "g", groupKind: .superset)]), exercises: EngineCatalog.all)
        #expect(try note(mixed, "press") == RowsNote(text: "warm-up 1 of 2", kind: .status))
    }
}
