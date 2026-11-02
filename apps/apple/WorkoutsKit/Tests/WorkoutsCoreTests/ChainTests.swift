import Foundation
@testable import WorkoutsCore
import Testing

// Chains (DESIGN §4.9): drops, myo-reps and their stop rules, myo-rep match,
// rest-pause, clusters and one side at a time, and the edits that stop or
// restart a chain.

/// The reps of a chain's rows, pending ones as nil.
private func chainReps(_ state: SessionState, _ head: RowID) -> [Int?] {
    state.chain(of: head).map { $0.performed?.values.reps }
}

/// Ticks the chain's next pending links, one per entry of `reps`, 15 s apart.
private func tickMiniSets(_ script: inout EngineScript, _ head: RowID, load: Decimal, _ reps: [Int],
                          sourceLocation: SourceLocation = #_sourceLocation) throws
{
    for count in reps {
        let next = try #require(try script.current.chain(of: head).first { $0.isPending }, "no pending link", sourceLocation: sourceLocation)
        try script.tick(next.id, kgReps(load, count), at: script.now.addingTimeInterval(15), sourceLocation: sourceLocation)
    }
}

private func singleSlot(_ exercise: Exercise, _ sets: [PlannedSet], sideOrder: SideOrder? = nil, sideMatch: Bool? = nil,
                        firstSides: [String: Side] = [:]) throws -> EngineScript
{
    var script = EngineScript(at: engineEpoch)
    try script.start(EngineCatalog.routine([EngineCatalog.slot("x", exercise, sets, sideOrder: sideOrder, sideMatch: sideMatch)]),
                     exercises: EngineCatalog.all + [exercise], firstSides: firstSides)
    return script
}

@Suite("Chains: drops")
struct DropChainTests {
    @Test("drop rows come with the head, locked until it is ticked, with dropReps as their target")
    func created() throws {
        var script = try singleSlot(EngineCatalog.press, [PlannedSet(key: "s1", structure: .drop, repsMin: 8, repsMax: 12, load: .kg(50),
                                                                     drops: 2, dropReps: 6)])
        let state = try script.current
        let chain = state.chain(of: try script.row("x", 0))
        #expect(chain.map(\.structure) == [.drop, .drop, .drop])
        #expect(chain.map(\.chainPosition) == [1, 2, 3])
        #expect(chain.dropFirst().allSatisfy { $0.generated && $0.target == SetTarget(plannedSet: "s1", repsMin: 6, repsMax: 6) })
        #expect(chain[0].target?.drops == 2)
        #expect(state.isLocked(chain[1].id) && state.isLocked(chain[2].id))
        script.refuse(.completeSet(.init(row: chain[1].id, recordID: "r", values: kgReps(40, 6))), .segmentBeforeItsLink)
        script.refuse(.startTimedSet(row: chain[1].id, target: nil), .segmentBeforeItsLink)
        try script.tick(chain[0].id, kgReps(50, 10))
        #expect(try !script.current.isLocked(chain[1].id) && script.current.isLocked(chain[2].id))
        try script.tick(chain[1].id, kgReps(40, 6))
        try script.tick(chain[2].id, kgReps(30, 6))
        let records = SessionEngine.setRecords(try script.current)
        #expect(records.map(\.chainPosition) == [1, 2, 3])
        #expect(records.map(\.ordinal) == [1, 1, 1])
        #expect(records.dropFirst().allSatisfy { $0.continues == records[0].id && $0.structure == .drop })
    }

    @Test("no lighter load at a tick removes the drops left")
    func noLighterLoad() throws {
        var script = try singleSlot(EngineCatalog.press, [PlannedSet(key: "s1", structure: .drop, load: .kg(10), drops: 2)])
        let (head, drop1) = try (script.row("x", 0), script.row("x", 1))
        try script.tick(head, kgReps(10, 10))
        var state = try script.tick(drop1, kgReps(5, 8), noLighterLoad: true)
        #expect(state.chain(of: head).count == 2)
        #expect(state.upNext == nil)
        // At the head: every drop goes, and the set is complete at once.
        script = try singleSlot(EngineCatalog.press, [PlannedSet(key: "s1", structure: .drop, load: .kg(5), drops: 2, rest: 90),
                                                     PlannedSet(key: "s2")])
        state = try script.tick(script.row("x", 0), kgReps(5, 10), noLighterLoad: true)
        #expect(state.chain(of: try script.row("x", 0)).count == 1)
        #expect(state.restClock?.slot == .override)
    }
}

@Suite("Chains: myo-reps")
struct MyoChainTests {
    private let raise = EngineCatalog.raise

    private func myo(_ exercise: Exercise? = nil, miniSets: Int? = nil, totalReps: Int? = nil, rule: MyoStopRule? = nil,
                     miniSetReps: Int? = nil) throws -> (EngineScript, RowID)
    {
        let script = try singleSlot(exercise ?? raise, [PlannedSet(key: "s1", structure: .myo, repsMin: 12, repsMax: 20, load: .kg(6),
                                                                   miniSetReps: miniSetReps, miniSets: miniSets, myoStopRule: rule,
                                                                   totalReps: totalReps)])
        return (script, try script.row("x", 0))
    }

    @Test("mini-sets appear one at a time and stop when one loses a rep from the first (22 October)")
    func repDrop() throws {
        var (script, head) = try myo()
        #expect(try script.current.chain(of: head).count == 1)
        try script.tick(head, kgReps(6, 16, rpe: 9))
        let first = try #require(try script.current.chain(of: head).last)
        #expect(first.isPending && first.generated && first.chainPosition == 2 && first.target == SetTarget(plannedSet: "s1"))
        try tickMiniSets(&script, head, load: 6, [4, 4, 4, 3])
        #expect(chainReps(try script.current, head) == [16, 4, 4, 4, 3])
        #expect(try script.current.chainStops(head))
        #expect(try script.current.upNext == nil)
    }

    @Test("the cap stops a chain: five by default (26 October), else the plan's or the exercise's")
    func cap() throws {
        var (script, head) = try myo()
        try script.tick(head, kgReps(6, 17, rpe: 9))
        try tickMiniSets(&script, head, load: 6, [4, 4, 4, 4, 4])
        #expect(chainReps(try script.current, head) == [17, 4, 4, 4, 4, 4])
        (script, head) = try myo(miniSets: 2)
        try script.tick(head, kgReps(6, 17))
        try tickMiniSets(&script, head, load: 6, [5, 5])
        #expect(try script.current.chain(of: head).count == 3)
        (script, head) = try myo(EngineCatalog.exercise("cap3", restClass: .isolation, myoMaxMiniSets: 3))
        try script.tick(head, kgReps(6, 17))
        try tickMiniSets(&script, head, load: 6, [5, 5, 5])
        #expect(try script.current.chain(of: head).count == 4)
        // A cap of 0: the head's tick grows nothing.
        (script, head) = try myo(miniSets: 0)
        let state = try script.tick(head, kgReps(6, 17))
        #expect(state.chain(of: head).count == 1 && state.upNext == nil)
    }

    @Test("belowtarget stops when a mini-set falls under its target: a quarter of the activation, 3 to 5")
    func belowTarget() throws {
        var (script, head) = try myo(rule: .belowtarget)
        try script.tick(head, kgReps(6, 18))
        // t1 = round(18 / 4) = 5, halves up: 5, 5, then 4 stops it; a rep drop alone would not.
        try tickMiniSets(&script, head, load: 6, [5, 5])
        #expect(try !script.current.chainStops(head))
        try tickMiniSets(&script, head, load: 6, [4])
        #expect(try script.current.chainStops(head))
        // The exercise's rule applies when the plan names none; the plan's miniSetReps sets the target.
        (script, head) = try myo(EngineCatalog.exercise("bt", restClass: .isolation, myoStopRule: .belowtarget), miniSetReps: 3)
        try script.tick(head, kgReps(6, 20))
        try tickMiniSets(&script, head, load: 6, [3, 3, 3])
        #expect(try !script.current.chainStops(head))
        #expect(try script.current.chain(of: head).last?.target == SetTarget(plannedSet: "s1", repsMin: 3, repsMax: 3))
    }

    @Test("a planned total stops the chain once the mini-sets reach it")
    func total() throws {
        var (script, head) = try myo(totalReps: 10)
        try script.tick(head, kgReps(6, 16))
        try tickMiniSets(&script, head, load: 6, [4, 4])
        #expect(try !script.current.chainStops(head))
        // 12 of 10, with no rep lost and under the cap.
        try tickMiniSets(&script, head, load: 6, [4])
        #expect(try script.current.chainStops(head))
    }

    @Test("an edit that now meets the stop rule removes the pending mini-set; one that undoes it brings it back")
    func edits() throws {
        var (script, head) = try myo()
        try script.tick(head, kgReps(6, 16))
        try tickMiniSets(&script, head, load: 6, [4, 4])
        let second = try script.current.chain(of: head)[2]
        // Mini-set 2 corrected to 3: a rep lost, so the pending third goes.
        var state = try script.send(.editSet(.init(row: second.id, values: SetValuesPatch(reps: .set(3)))))
        #expect(chainReps(state, head) == [16, 4, 3])
        // Back to 4: the chain runs on, with a pre-minted id.
        let id = script.rowIDs(1)[0]
        state = try script.send(.editSet(.init(row: second.id, values: SetValuesPatch(reps: .set(4)), segmentRowIDs: [id])))
        #expect(chainReps(state, head) == [16, 4, 4, nil])
        #expect(state.chain(of: head).last?.id == id)
        // Un-ticking the last done mini-set removes the pending one after it; an earlier link cannot be un-ticked.
        script.refuse(.uncompleteSet(row: head), .invalidOrder)
        state = try script.send(.uncompleteSet(row: second.id))
        #expect(chainReps(state, head) == [16, 4, nil])
        state = try script.send(.uncompleteSet(row: state.chain(of: head)[1].id))
        state = try script.send(.uncompleteSet(row: head))
        // The pending mini-set after the head goes too.
        #expect(state.chain(of: head).count == 1)
    }
}

@Suite("Chains: myo-rep match, rest-pause and clusters")
struct MatchPauseClusterTests {
    @Test("a myo-rep match resolves its total from the slot's previous working set at the head's tick (28 October)")
    func matchFromPrevious() throws {
        var script = try singleSlot(EngineCatalog.curl, [PlannedSet(key: "s1", repsMin: 10, repsMax: 15, load: .kg(40)),
                                                         PlannedSet(key: "s2", structure: .myomatch, load: .kg(40), intraSetPause: 10)])
        let (s1, s2) = try (script.row("x", 0), script.row("x", 1))
        #expect(try script.current.rows[s2]?.target?.termination == .totalreps)
        #expect(try script.current.rows[s2]?.target?.totalReps == nil)
        try script.tick(s1, kgReps(40, 12, rpe: 9))
        try script.tick(s2, kgReps(40, 7, rpe: 9))
        #expect(try script.current.rows[s2]?.target?.totalReps == 12)
        // 7 + 3 + 2 = 12: the short last mini-set is by design, not a stop by rep drop.
        try tickMiniSets(&script, s2, load: 40, [3, 2])
        let state = try script.current
        #expect(chainReps(state, s2) == [7, 3, 2])
        #expect(state.chainStops(s2) && state.upNext == nil)
    }

    @Test("a myo-rep match's planned total wins; with nothing to match it stops at the cap")
    func matchPlannedAndCap() throws {
        var script = try singleSlot(EngineCatalog.curl, [PlannedSet(key: "s1", structure: .myomatch, load: .kg(40), miniSets: 2, totalReps: 20)])
        let head = try script.row("x", 0)
        try script.tick(head, kgReps(40, 10))
        #expect(try script.current.rows[head]?.target?.totalReps == 20)
        try tickMiniSets(&script, head, load: 40, [4, 3])
        #expect(try script.current.chainStops(head))
        #expect(chainReps(try script.current, head) == [10, 4, 3])
    }

    @Test("rest-pause: two mini-sets by default, or until the total with the first bout")
    func restPause() throws {
        var script = try singleSlot(EngineCatalog.curl, [PlannedSet(key: "s1", structure: .restpause, repsMin: 10, repsMax: 12, load: .kg(45),
                                                                    miniSets: 2, intraSetPause: 20)])
        var head = try script.row("x", 0)
        try script.tick(head, kgReps(45, 12))
        try tickMiniSets(&script, head, load: 45, [4, 3])
        #expect(chainReps(try script.current, head) == [12, 4, 3])
        #expect(try script.current.upNext == nil)
        script = try singleSlot(EngineCatalog.curl, [PlannedSet(key: "s1", structure: .restpause, load: .kg(45), totalReps: 20)])
        head = try script.row("x", 0)
        try script.tick(head, kgReps(45, 15))
        try tickMiniSets(&script, head, load: 45, [5])
        #expect(try script.current.chainStops(head))
        #expect(chainReps(try script.current, head) == [15, 5])
    }

    @Test("a cluster set has all its clusters from the start, with miniSetReps as their target, and grows none")
    func cluster() throws {
        var script = try singleSlot(EngineCatalog.press, [PlannedSet(key: "s1", structure: .cluster, load: .kg(80), rirMin: 2, rirMax: 2, miniSetReps: 3,
                                                                     miniSets: 3, intraSetPause: 20)])
        let head = try script.row("x", 0)
        let chain = try script.current.chain(of: head)
        #expect(chain.count == 4)
        #expect(chain[0].target == SetTarget(plannedSet: "s1", load: .kg(80), rirMin: 2, rirMax: 2, miniSetReps: 3, miniSets: 3,
                                             intraSetPause: 20))
        #expect(chain.dropFirst().allSatisfy { $0.target == SetTarget(plannedSet: "s1", repsMin: 3, repsMax: 3) })
        try script.tick(head, kgReps(80, 3))
        try tickMiniSets(&script, head, load: 80, [3, 3, 3])
        #expect(chainReps(try script.current, head) == [3, 3, 3, 3])
        #expect(try script.current.upNext == nil)
        // No number of clusters named: three after the first.
        script = try singleSlot(EngineCatalog.press, [PlannedSet(key: "s1", structure: .cluster, miniSetReps: 2)])
        #expect(try script.current.chain(of: script.row("x", 0)).count == 4)
    }
}

@Suite("Chains: one side at a time")
struct SideChainTests {
    @Test("the first side by sideOrder: left by default, right, or the weaker side the controller found")
    func sideOrder() throws {
        let unilateral = EngineCatalog.lunge
        for (order, resolved, expected) in [(SideOrder?.none, Side?.none, Side.left), (.left, nil, .left), (.right, nil, .right),
                                            (.weakerfirst, .right, .right), (.weakerfirst, nil, .left)]
        {
            let script = try singleSlot(unilateral, EngineCatalog.sets(1), sideOrder: order,
                                        firstSides: resolved.map { ["x": $0] } ?? [:])
            let rows = try script.current.orderedRows(inSlot: "x")
            #expect(rows.map(\.side) == [expected, expected.other])
            #expect(rows[1].chainHead == rows[0].id && rows[1].structure == .straight)
        }
    }

    @Test("sideMatch copies the first side's reps into the second side's target; an un-tick undoes it")
    func sideMatchReps() throws {
        var script = try singleSlot(EngineCatalog.lunge, EngineCatalog.sets(1), sideMatch: true)
        let (left, right) = try (script.row("x", 0), script.row("x", 1))
        var state = try script.tick(left, kgReps(10, 9))
        #expect(state.rows[right]?.target == SetTarget(plannedSet: "s1", termination: .reps, repsMin: 9, repsMax: 9))
        // An edit of the first side re-matches the pending second.
        state = try script.send(.editSet(.init(row: left, values: SetValuesPatch(reps: .set(10)))))
        #expect(state.rows[right]?.target?.repsMin == 10)
        state = try script.send(.uncompleteSet(row: left))
        #expect(state.rows[right]?.target == SetTarget(plannedSet: "s1"))
        // Without sideMatch the second side keeps the planned set's key only.
        script = try singleSlot(EngineCatalog.lunge, EngineCatalog.sets(1))
        state = try script.tick(script.row("x", 0), kgReps(10, 9))
        #expect(state.rows[try script.row("x", 1)]?.target == SetTarget(plannedSet: "s1"))
    }

    @Test("a matched timed hold aims for the first side's time (28 October side plank)")
    func sideMatchTime() throws {
        let sidePlank = EngineCatalog.exercise("side-plank", .duration, laterality: .unilateral, restClass: .timed, equipment: Equipment.none)
        var script = try singleSlot(sidePlank, [PlannedSet(key: "s1", durationMin: 30, durationMax: 45, timerMode: .stopwatch)], sideMatch: true)
        let (left, right) = try (script.row("x", 0), script.row("x", 1))
        try script.send(.startTimedSet(row: left, target: nil))
        let state = try script.send(.stopTimedSet(.init(row: left, recordID: script.setID(), values: SetValues())),
                                    at: script.now.addingTimeInterval(41))
        #expect(state.rows[right]?.target == SetTarget(plannedSet: "s1", termination: .time, durationMin: 41, durationMax: 41))
        #expect(state.restClock?.slot == .sideSwitch)
    }

    @Test("a structured set on a one-side exercise runs as a side chain; adding a set adds both sides")
    func structureIgnored() throws {
        var script = try singleSlot(EngineCatalog.lunge, [PlannedSet(key: "s1", structure: .drop, drops: 2)])
        var state = try script.current
        #expect(state.orderedRows(inSlot: "x").map(\.side) == [.left, .right])
        #expect(state.orderedRows(inSlot: "x").map(\.structure) == [.straight, .straight])
        let ids = script.rowIDs(2)
        state = try script.send(.addSet(.init(slot: "x", row: ids[0], role: .working, segmentRowIDs: [ids[1]])))
        #expect(state.chain(of: ids[0]).map(\.id) == ids)
        #expect(state.chain(of: ids[0]).map(\.side) == [.left, .right])
        let records = SessionEngine.setRecords(try script.tick(ids[0], kgReps(10, 8)))
        #expect(records.first?.chainPosition == 1 && records.first?.side == .left && records.first?.structure == .straight)
    }
}
