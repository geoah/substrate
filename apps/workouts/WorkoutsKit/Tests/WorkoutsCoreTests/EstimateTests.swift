import Foundation
import Testing

@testable import WorkoutsCore

// The time left (DESIGN §4.15): working time and the plan's rests, a running
// rest's remainder, and today's measured sets in place of the default.

@Suite("The time left", .timeLimit(.minutes(10)))
struct EstimateTests {
    private func script() throws -> EngineScript {
        var script = EngineScript(at: engineEpoch)
        try script.start(
            EngineCatalog.routine([
                EngineCatalog.slot("a", EngineCatalog.press, EngineCatalog.sets(2)),
                EngineCatalog.slot("b", EngineCatalog.curl, EngineCatalog.sets(1)),
            ]), exercises: EngineCatalog.all)
        return script
    }

    @Test("before anything: each set's 45 s and the rests the plan gives between them, none after the last")
    func fresh() throws {
        let state = try script().current
        // a1, 2:00 set rest, a2, 2:00 before the next exercise, b1.
        #expect(SessionEstimate.timeLeft(state, now: engineEpoch) == 3 * 45 + 2 * 120)
    }

    @Test("a running rest counts what is left of it; nothing pending is no estimate")
    func runningRest() throws {
        var script = try script()
        let state = try script.tick(try script.row("a", 0), kgReps(50, 10), at: engineEpoch.addingTimeInterval(60))
        // The rest runs to 3:00; at 1:40, 80 s are left.
        #expect(SessionEstimate.timeLeft(state, now: engineEpoch.addingTimeInterval(100)) == 80 + 2 * 45 + 120)
        var done = state
        for id in done.rows.keys { done.rows[id]?.performed = Performed(recordID: SetID("x"), completedAt: engineEpoch, values: SetValues()) }
        #expect(SessionEstimate.timeLeft(done, now: engineEpoch) == nil)
    }

    @Test("today's sets measure the working time: the gap less the rest before it")
    func measured() throws {
        var script = try script()
        try script.tick(try script.row("a", 0), kgReps(50, 10), at: engineEpoch.addingTimeInterval(60))
        // The rest ran 2:00 to 3:00; a2 ticked 30 s after: a 30 s set.
        let state = try script.tick(try script.row("a", 1), kgReps(50, 10), at: engineEpoch.addingTimeInterval(210))
        #expect(SessionEstimate.workTimes(state)[EngineCatalog.press.id] == [30])
        // b1 has no sample of its own and takes today's median, 30 s, after
        // what is left of the 2:00 rest.
        #expect(SessionEstimate.timeLeft(state, now: engineEpoch.addingTimeInterval(210)) == 120 + 30)
    }
}
