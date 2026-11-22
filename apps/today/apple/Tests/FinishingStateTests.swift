#if os(macOS)
import Foundation
import Testing
import TodayCore

@testable import SubstrateTodayUI

/// What the screen keeps of a finish until it is sent, and of whether what
/// is done is shown. The waits themselves are `Finishing`'s, tested in the
/// core; here it is what the screen does around them.
@MainActor
@Suite struct FinishingStateTests {
    final class Sent {
        var commands: [TodayCommand] = []
    }

    @Test func aFinishWaitsIsTakenBackOrIsSentWhenTheScreenLeaves() {
        let state = PaneViewState(showsDone: true)
        let sent = Sent()
        // A command that finishes nothing goes at once.
        state.finish(.reopen("a")) { sent.commands.append($0) }
        #expect(sent.commands == [.reopen("a")] && state.begun == 0)
        // One that finishes something waits, as a countdown, and nothing is sent.
        state.finish(.done("a")) { sent.commands.append($0) }
        #expect(sent.commands.count == 1 && state.begun == 1 && state.waiting("task:a") == .done("a") && state.phase("task:a") == .counting)
        // Taken back, it is never sent, even when the screen leaves.
        state.takeBack("task:a")
        state.flush()
        #expect(sent.commands.count == 1 && state.waiting("task:a") == nil && state.sent == 0)
        // An item ticked, then its whole row: leaving sends both at once, the item first.
        state.finish(.logOccurrence("o1", done: true)) { sent.commands.append($0) }
        state.finish(.log(row: "r", done: true)) { sent.commands.append($0) }
        state.flush()
        #expect(sent.commands.dropFirst() == [.logOccurrence("o1", done: true), .log(row: "r", done: true)] && state.sent == 2)
        #expect(state.waiting("r") == nil && state.phase("o1") == nil)
    }

    @Test func aDrawingStandsWhereItsInstantPutsIt() {
        let began = Date(timeIntervalSince1970: 1_800_000_000)
        let state = PaneViewState(showsDone: false)
        state.draw(.done("a"), began: began, at: began.addingTimeInterval(1.5))
        #expect(state.phase("task:a") == .counting && state.left("task:a") == 0.5)
        state.draw(.done("a"), began: began, at: began.addingTimeInterval(4))
        #expect(state.phase("task:a") == .settled && state.left("task:a") == 0)
        // A drawing's choice of what is shown is its own.
        #expect(!state.showsDone)
        state.show(done: true)
        #expect(state.showsDone)
    }
}
#endif
