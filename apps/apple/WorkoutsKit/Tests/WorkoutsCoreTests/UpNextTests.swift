import Foundation
@testable import WorkoutsCore
import Testing

// Up next (DESIGN §4.6): the plan sequence, the rule, and ticking in any
// order (§10.5).

/// A row's label for readable expectations: the slot key and its ordinal,
/// `w` for a warm-up, then the chain position past the head.
private func name(_ id: RowID, _ state: SessionState) -> String {
    guard let row = state.rows[id] else { return "?" }
    let ordinal = state.ordinal(of: id) ?? 0
    let base = "\(row.slot)\(row.role == .warmup ? "w" : "")\(ordinal)"
    return row.isHead ? base : "\(base).\(row.chainPosition)"
}

private func sequence(_ state: SessionState) -> [String] {
    UpNext.sequence(state).map { name($0, state) }
}

@Suite("Up next")
struct UpNextTests {
    @Test("plain slots contribute their rows in display order, each head followed by its segments")
    func plainSlots() throws {
        var script = EngineScript(at: engineEpoch)
        let state = try script.start(EngineCatalog.routine([
            EngineCatalog.slot("p", EngineCatalog.press, [PlannedSet(key: "w1", role: .warmup), PlannedSet(key: "s1"),
                                                           PlannedSet(key: "s2", structure: .drop, drops: 2)]),
            EngineCatalog.slot("l", EngineCatalog.lunge, EngineCatalog.sets(2)),
        ]), exercises: EngineCatalog.all)
        #expect(sequence(state) == ["pw1", "p1", "p2", "p2.2", "p2.3", "l1", "l1.2", "l2", "l2.2"])
        #expect(state.upNext == state.slot("p")?.rows.first)
    }

    @Test("a group is one block: members with fewer sets drop out of later rounds (A 4, B 3, C 2)")
    func unevenGroup() throws {
        var script = EngineScript(at: engineEpoch)
        let state = try script.start(EngineCatalog.routine([
            EngineCatalog.slot("x", EngineCatalog.curl, EngineCatalog.sets(1)),
            EngineCatalog.slot("a", EngineCatalog.press, EngineCatalog.sets(4), group: "g"),
            EngineCatalog.slot("b", EngineCatalog.row, EngineCatalog.sets(3), group: "g"),
            EngineCatalog.slot("c", EngineCatalog.raise, EngineCatalog.sets(2), group: "g"),
            EngineCatalog.slot("y", EngineCatalog.pushUp, EngineCatalog.sets(1)),
        ], groups: [RoutineGroup(key: "g", groupKind: .giantset)]), exercises: EngineCatalog.all)
        #expect(sequence(state) == ["x1", "a1", "b1", "c1", "a2", "b2", "c2", "a3", "b3", "a4", "y1"])
        #expect(UpNext.round(of: try script.row("b", 2), in: state) == 3)
    }

    @Test("every member's warm-ups come before round 1, and a round holds each head's segments")
    func warmupsAndChains() throws {
        var script = EngineScript(at: engineEpoch)
        let state = try script.start(EngineCatalog.routine([
            EngineCatalog.slot("a", EngineCatalog.press, [PlannedSet(key: "w1", role: .warmup), PlannedSet(key: "s1", structure: .drop),
                                                           PlannedSet(key: "s2")], group: "g"),
            EngineCatalog.slot("b", EngineCatalog.lunge, [PlannedSet(key: "w1", role: .warmup), PlannedSet(key: "s1")], group: "g"),
        ], groups: [RoutineGroup(key: "g", groupKind: .paired)]), exercises: EngineCatalog.all)
        #expect(sequence(state) == ["aw1", "bw1", "bw1.2", "a1", "a1.2", "b1", "b1.2", "a2"])
        #expect(UpNext.round(of: try script.row("a", 0), in: state) == 0)
        #expect(UpNext.round(of: try script.row("a", 2), in: state) == 1)
    }

    @Test("the next pending row after the last completed one, wrapping around; a locked segment is skipped")
    func wrapAround() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("a", EngineCatalog.press, [PlannedSet(key: "s1"), PlannedSet(key: "s2", structure: .drop)]),
            EngineCatalog.slot("b", EngineCatalog.row, EngineCatalog.sets(2)),
        ]), exercises: EngineCatalog.all)
        let (a1, a2, drop) = try (script.row("a", 0), script.row("a", 1), script.row("a", 2))
        let (b1, b2) = try (script.row("b", 0), script.row("b", 1))
        var state = try script.tick(b2, kgReps(40, 10))
        // After b2, wrapping to the start; the drop is locked behind its head.
        #expect(state.upNext == a1)
        state = try script.tick(a1, kgReps(50, 10))
        #expect(state.upNext == a2)
        #expect(UpNext.expectedNext(after: a2, in: state) == b1)
        state = try script.tick(a2, kgReps(50, 10))
        #expect(state.upNext == drop)
        state = try script.tick(drop, kgReps(40, 6))
        #expect(state.upNext == b1)
        state = try script.tick(b1, kgReps(40, 10))
        // All sets done.
        #expect(state.upNext == nil)
        #expect(UpNext.expectedNext(after: nil, in: state) == nil)
    }

    @Test("a touched pending row is Up next until the next tick; a locked one cannot be")
    func focus() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("a", EngineCatalog.press, [PlannedSet(key: "s1", structure: .drop), PlannedSet(key: "s2")]),
            EngineCatalog.slot("b", EngineCatalog.row, EngineCatalog.sets(2)),
        ]), exercises: EngineCatalog.all)
        let (a1, drop, a2) = try (script.row("a", 0), script.row("a", 1), script.row("a", 2))
        let b2 = try script.row("b", 1)
        var state = try script.send(.touchRow(row: drop))
        #expect(state.focus == nil && state.upNext == a1)
        state = try script.send(.touchRow(row: b2))
        #expect(state.upNext == b2)
        state = try script.tick(a1, kgReps(50, 10))
        #expect(state.focus == nil && state.upNext == drop)
        state = try script.tick(drop, kgReps(40, 6))
        #expect(state.upNext == a2)
    }

    @Test("ticking in any order: Up next and the rest follow the plan sequence from the set just done")
    func anyOrder() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("a", EngineCatalog.press, EngineCatalog.sets(2), setRest: 100),
            EngineCatalog.slot("b", EngineCatalog.row, EngineCatalog.sets(2), setRest: 80),
            EngineCatalog.slot("c", EngineCatalog.curl, EngineCatalog.sets(1), setRest: 60),
        ]), exercises: EngineCatalog.all)
        let (a1, a2, b1, b2, c1) = try (script.row("a", 0), script.row("a", 1), script.row("b", 0), script.row("b", 1), script.row("c", 0))
        // The second exercise first.
        var state = try script.tick(b1, kgReps(40, 10))
        #expect(state.upNext == b2 && state.restClock?.slot == .setRest && state.restClock?.planned == 80)
        state = try script.tick(b2, kgReps(40, 10))
        #expect(state.upNext == c1 && state.restClock?.slot == .blockRest && state.restClock?.planned == 80)
        // A later set of the first exercise before its first.
        state = try script.tick(a2, kgReps(50, 10))
        #expect(state.upNext == c1 && state.restClock?.slot == .blockRest && state.restClock?.planned == 100)
        state = try script.tick(c1, kgReps(20, 10))
        // Wrapping back to the one set left, a block away.
        #expect(state.upNext == a1 && state.restClock?.slot == .blockRest && state.restClock?.planned == 60)
        state = try script.tick(a1, kgReps(50, 10))
        #expect(state.upNext == nil && state.restClock?.slot == nil && state.countdown == nil)
    }

    @Test("ordinals count every row during the session, a chain once; Finish counts performed rows only")
    func ordinals() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("a", EngineCatalog.press, [PlannedSet(key: "w1", role: .warmup), PlannedSet(key: "w2", role: .warmup),
                                                           PlannedSet(key: "s1"), PlannedSet(key: "s2", structure: .drop, drops: 2),
                                                           PlannedSet(key: "s3")]),
        ]), exercises: EngineCatalog.all)
        let state = try script.current
        let rows = state.orderedRows(inSlot: "a")
        #expect(rows.map { state.ordinal(of: $0.id) } == [1, 2, 1, 2, 2, 2, 3])
        #expect(rows.map { Ordinals.segmentIndex(of: $0.id, in: state) } == [nil, nil, nil, nil, 1, 2, nil])
        #expect(Ordinals.warmupCount(inSlot: "a", of: state) == 2)
        try script.tick(rows[1].id, kgReps(30, 5))
        try script.tick(rows[6].id, kgReps(50, 8))
        let done = try script.current
        #expect(Ordinals.ordinals(inSlot: "a", of: done, performedOnly: true) == [rows[1].id: 1, rows[6].id: 1])
        #expect(Ordinals.orders(inSlot: "a", of: done) == [rows[1].id: 1, rows[6].id: 2])
    }

    @Test("labels: W, 2, 2a, M and 1L, short and spoken, with the failure badge and Top or Back-off")
    func labels() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("a", EngineCatalog.press, [
                PlannedSet(key: "w1", role: .warmup, repsMin: 10, repsMax: 10), PlannedSet(key: "w2", role: .warmup, repsMin: 5, repsMax: 5),
                PlannedSet(key: "s1", roleLabel: .top, repsMin: 5, repsMax: 5),
                PlannedSet(key: "s2", roleLabel: .backoff, structure: .drop, drops: 2),
                PlannedSet(key: "s3", structure: .restpause, load: .kg(30)),
                PlannedSet(key: "s4", structure: .cluster, miniSets: 1),
                PlannedSet(key: "s5", structure: .myo, load: .kg(30)),
                PlannedSet(key: "s6", termination: .failure),
            ]),
            EngineCatalog.slot("b", EngineCatalog.lunge, [PlannedSet(key: "w1", role: .warmup), PlannedSet(key: "s1", termination: .amrap, repsMin: 8)]),
        ]), exercises: EngineCatalog.all)
        try script.tick(script.row("a", 6), kgReps(30, 12))
        try script.tick(script.row("a", 10), kgReps(30, 16))
        let state = try script.current
        let labels = state.orderedRows(inSlot: "a").map { SetLabel.make($0.id, in: state) }
        #expect(labels.map { $0?.short } == ["W", "W", "1", "2", "2a", "2b", "3", "3a", "4", "4a", "5", "M", "6"])
        #expect(labels.map { $0?.spoken } == ["warm-up 1", "warm-up 2", "set 1", "set 2", "set 2, drop 1", "set 2, drop 2", "set 3",
                                              "set 3, mini-set 1", "set 4", "set 4, cluster 2", "set 5", "set 5, mini-set 1", "set 6"])
        #expect(labels.map { $0?.roleText } == [nil, nil, "Top", "Back-off", nil, nil, nil, nil, nil, nil, nil, nil, nil])
        #expect(labels.map { $0?.failureBadge } == [false, false, false, false, false, false, false, false, false, false, false, false, true])
        let sides = state.orderedRows(inSlot: "b").map { SetLabel.make($0.id, in: state) }
        #expect(sides.map { $0?.short } == ["WL", "WR", "1L", "1R"])
        #expect(sides.map { $0?.spoken } == ["warm-up, left side", "warm-up, right side", "set 1, left side", "set 1, right side"])
        #expect(sides[2]?.failureBadge == true && sides[3]?.failureBadge == false)
        #expect(SetLabel.make("nope", in: state) == nil)
    }
}
