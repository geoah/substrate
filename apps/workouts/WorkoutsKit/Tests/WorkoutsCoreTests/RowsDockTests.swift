import Foundation
@testable import WorkoutsCore
import Testing

// The review of the three-part island (PR #1110): when the island docks the
// open set, what "Then …" may open, a group's warm-up in the island, and the
// minimised bar while a timed set runs.

private let set1 = RowID("set-1")
private let set2 = RowID("set-2")
private let set3 = RowID("set-3")

/// A dock with `open` in view, as a panel that laid out and reported.
private func dock(open: RowID) -> RowsDock {
    var dock = RowsDock()
    dock.show(open: open, isFixing: false)
    dock.logButton(of: open, isVisible: true)
    return dock
}

@Suite("Rows dock: when the island docks the open set")
struct RowsDockTests {
    @Test("the open set docks only once its Log button is reported out of view")
    func docksOnReport() {
        var dock = dock(open: set2)
        #expect(!dock.isDocked)
        dock.logButton(of: set2, isVisible: false)
        #expect(dock.isDocked)
        dock.logButton(of: set2, isVisible: true)
        #expect(!dock.isDocked)
        // Another row's panel says nothing about the open one.
        dock.logButton(of: set1, isVisible: false)
        #expect(!dock.isDocked)
    }

    @Test("a fix: the open panel leaving for the fix's never docks it, and the last report stands through it")
    func cancelFix() {
        var dock = dock(open: set2)
        dock.show(open: set2, isFixing: true)
        // The open panel is replaced by a line while set 1 is fixed: its report a beat later.
        dock.logButton(of: set2, isVisible: false)
        #expect(!dock.isDocked)
        dock.show(open: set2, isFixing: false)
        // In view before the fix, in view after it until the panel reports again.
        #expect(!dock.isDocked && dock.logVisible == true && dock.awaitsReport)
        dock.logButton(of: set2, isVisible: true)
        #expect(!dock.isDocked && !dock.awaitsReport)
        // A real report after the fix still counts.
        dock.logButton(of: set2, isVisible: false)
        #expect(dock.isDocked)
        // Docked, then a fix starts: the fix holds the screen.
        dock.show(open: set2, isFixing: true)
        #expect(!dock.isDocked)
    }

    @Test("an open editor off screen, a fix elsewhere, Cancel, and no geometry report: the set docks again")
    func fixOffScreenNoReport() {
        var dock = dock(open: set2)
        dock.logButton(of: set2, isVisible: false)
        #expect(dock.isDocked)
        // Fix set 1 far up the list; the lazy list has dropped set 2's panel.
        dock.show(open: set2, isFixing: true)
        #expect(!dock.isDocked)
        // Cancel: the panel is not laid out again, so nothing reports. The last report stands at once…
        dock.show(open: set2, isFixing: false)
        #expect(dock.isDocked && dock.awaitsReport)
        // …and the bounded wait settles it: still out of view.
        dock.noReport()
        #expect(dock.isDocked && !dock.awaitsReport)

        // In view before the fix, gone after it (the owner scrolled during the fix), and no report: it docks.
        var scrolled = RowsDock()
        scrolled.show(open: set2, isFixing: false)
        scrolled.logButton(of: set2, isVisible: true)
        scrolled.show(open: set2, isFixing: true)
        scrolled.show(open: set2, isFixing: false)
        #expect(!scrolled.isDocked)
        scrolled.noReport()
        #expect(scrolled.isDocked)

        // A report always wins over the wait, and a wait ending during a fix settles nothing.
        var reported = dock
        reported.show(open: set2, isFixing: true)
        reported.show(open: set2, isFixing: false)
        reported.logButton(of: set2, isVisible: true)
        reported.noReport()
        #expect(!reported.isDocked)
        var during = RowsDock()
        during.show(open: set2, isFixing: false)
        during.show(open: set2, isFixing: true)
        during.noReport()
        #expect(!during.isDocked && during.awaitsReport)
    }

    @Test("an Undo that leaves the open row where it was changes nothing: an off-screen editor stays docked")
    func undoOpenRowUnchanged() {
        var dock = dock(open: set2)
        dock.logButton(of: set2, isVisible: false)
        #expect(dock.isDocked)
        // A running timed set keeps the editor: the untick and touch leave set 2 open.
        dock.show(open: set2, isFixing: false)
        #expect(dock.isDocked && !dock.awaitsReport)
    }

    @Test("a new open row awaits its report; one never laid out docks once the wait ends")
    func newRowAwaitsReport() {
        var dock = dock(open: set1)
        dock.show(open: set2, isFixing: false)
        #expect(!dock.isDocked && dock.awaitsReport && dock.logVisible == nil)
        // The panel the Log removed says nothing that counts.
        dock.logButton(of: set1, isVisible: false)
        #expect(!dock.isDocked && dock.awaitsReport)
        dock.noReport()
        #expect(dock.isDocked)
    }

    @Test("a panel that reports before the dock hears its row opened: the report is kept, and the wait never docks it")
    func earlyReport() {
        // A timed set stopped: the next set's panel is laid out in view in the same pass, and reports first.
        var dock = dock(open: set1)
        dock.logButton(of: set2, isVisible: true)
        #expect(!dock.isDocked && dock.open == set1)
        dock.show(open: set2, isFixing: false)
        #expect(!dock.isDocked && dock.logVisible == true && !dock.awaitsReport)
        // Its Bool does not change again, so no report follows; the bounded wait settles nothing.
        dock.noReport()
        #expect(!dock.isDocked)

        // Out of view first: it docks once the row is open.
        var below = RowsDock()
        below.show(open: set1, isFixing: false)
        below.logButton(of: set2, isVisible: false)
        below.show(open: set2, isFixing: false)
        #expect(below.isDocked)

        // A report kept for one row says nothing once another opens: it goes at every change of the open row.
        var stale = RowsDock()
        stale.show(open: set1, isFixing: false)
        stale.logButton(of: set2, isVisible: false)
        stale.show(open: set3, isFixing: false)
        stale.show(open: set2, isFixing: false)
        #expect(!stale.isDocked && stale.logVisible == nil && stale.awaitsReport)
    }

    @Test("while the list scrolls itself to the open set nothing docks; an undock is never held")
    func seekHoldsDock() {
        // A Log near the bottom: the next panel opens lower, reports out of view, and the list scrolls to it.
        var dock = dock(open: set1)
        dock.show(open: set2, isFixing: false)
        dock.seek(true)
        dock.logButton(of: set2, isVisible: false)
        #expect(!dock.isDocked)
        dock.logButton(of: set2, isVisible: true)
        dock.seek(false)
        // It landed in view: the island never showed it.
        #expect(!dock.isDocked)

        // A seek that lands short leaves it docked once the seek ends.
        var short = dock
        short.seek(true)
        short.logButton(of: set2, isVisible: false)
        #expect(!short.isDocked)
        short.seek(false)
        #expect(short.isDocked)

        // Docked, the island's title scrolls to the editor: it stays docked until the panel shows, then undocks.
        short.seek(true)
        #expect(short.isDocked)
        short.logButton(of: set2, isVisible: true)
        #expect(!short.isDocked)
    }

    @Test("a new open row starts in view: no stale dock from the row before")
    func rowChange() {
        var dock = dock(open: set2)
        dock.logButton(of: set2, isVisible: false)
        #expect(dock.isDocked)
        dock.show(open: set3, isFixing: false)
        #expect(!dock.isDocked)
        // Coming back to set 2 does not bring the old report back.
        dock.show(open: set2, isFixing: false)
        #expect(!dock.isDocked)
        // No open row, nothing to dock.
        dock.show(open: nil, isFixing: false)
        dock.jumped()
        #expect(!dock.isDocked)
    }

    @Test("a jump past an editor the list never laid out docks it; one that reported waits for its own report")
    func jumpPastUnlaidEditor() {
        var never = RowsDock()
        never.show(open: set2, isFixing: false)
        #expect(!never.isDocked)
        never.jumped()
        #expect(never.isDocked)
        // The jump landed near it after all: it lays out and reports.
        never.logButton(of: set2, isVisible: true)
        #expect(!never.isDocked)

        var seen = dock(open: set2)
        seen.jumped()
        #expect(!seen.isDocked)
        seen.logButton(of: set2, isVisible: false)
        #expect(seen.isDocked)

        var fixing = RowsDock()
        fixing.show(open: set2, isFixing: true)
        fixing.jumped()
        #expect(!fixing.isDocked)
    }

    @Test("undocking while the list moves keeps the bar's height until it is idle; docking takes it at once")
    func holdsHeightWhileScrolling() {
        var dock = dock(open: set2)
        #expect(!dock.holdsHeight)
        dock.scroll(isIdle: false)
        dock.logButton(of: set2, isVisible: false)
        #expect(dock.isDocked && dock.holdsHeight)
        // Dragged back: the set undocks, the inset stays under the finger.
        dock.logButton(of: set2, isVisible: true)
        #expect(!dock.isDocked && dock.holdsHeight)
        // Back and forth across the line while the finger is down: the height never moves.
        dock.logButton(of: set2, isVisible: false)
        #expect(dock.holdsHeight)
        dock.logButton(of: set2, isVisible: true)
        #expect(dock.holdsHeight)
        dock.scroll(isIdle: true)
        #expect(!dock.holdsHeight)

        // Idle: an undock (a tap on the title, a fix) gives the height back at once.
        dock.logButton(of: set2, isVisible: false)
        #expect(dock.holdsHeight)
        dock.show(open: set2, isFixing: true)
        #expect(!dock.holdsHeight)

        // A row change while the list moves holds too, until idle.
        var moving = RowsDock()
        moving.show(open: set1, isFixing: false)
        moving.scroll(isIdle: false)
        moving.logButton(of: set1, isVisible: false)
        moving.show(open: set2, isFixing: false)
        #expect(!moving.isDocked && moving.holdsHeight)
        moving.scroll(isIdle: true)
        #expect(!moving.holdsHeight)
    }

    @Test("the line is the Log button's, half of it in the viewport; how much of a tall panel shows does not count")
    func threshold() {
        // The list's viewport, between the bars: 100 to 700.
        let viewport = 100.0 ... 700.0
        // A 350 pt panel from 340 to 690 with its Log button at its foot, 640 to 690: all of it shows.
        #expect(RowsDock.isInView(640 ... 690, viewport: viewport))
        // Scrolled up 500 pt: the panel's top is off screen, but the Log button (140 to 190) still shows.
        // At 40 % of the panel the old rule docked here, and two Log buttons showed.
        #expect(RowsDock.isInView(140 ... 190, viewport: viewport))
        // Exactly half of it.
        #expect(RowsDock.isInView(75 ... 125, viewport: viewport))
        #expect(RowsDock.isInView(675 ... 725, viewport: viewport))
        // Less than half, under the top bar or the island.
        #expect(!RowsDock.isInView(60 ... 110, viewport: viewport))
        #expect(!RowsDock.isInView(690 ... 740, viewport: viewport))
        // Wholly out, either way; and a button with no height never counts.
        #expect(!RowsDock.isInView(-200 ... -150, viewport: viewport))
        #expect(!RowsDock.isInView(900 ... 950, viewport: viewport))
        #expect(!RowsDock.isInView(300 ... 300, viewport: viewport))
    }
}

@Suite("Rows island: \"Then\" takes the list to the set, never opens it")
struct RowsAfterActionTests {
    /// Every "Then" line that names a row names one the list shows, so a tap lands on it.
    private func expectEveryLineLands(_ start: RowsSession, logs: Int) throws {
        var session = start
        var second = 10
        var lines = 0
        for _ in 0 ..< logs {
            let layout = try session.layout(at: second)
            guard let open = layout.openRow else { break }
            if let after = layout.island.after, let row = after.row {
                lines += 1
                #expect(layout.item(row) != nil, "\(after.text)")
            }
            // Log the open set and go on.
            let editor = try #require(layout.item(open)?.editor)
            if editor.primary.action == .startTimedSet(open) {
                try session.perform(editor.primary.action, at: second)
                second += 30
                let values = try #require(try session.present(at: second).row(open)?.stopValues)
                try session.send(.stopTimedSet(.init(row: open, recordID: session.script.setID(), values: values)), at: second)
            } else if let values = try session.present(at: second).row(open)?.commitValues, editor.primary.isEnabled {
                try session.script.tick(open, values, at: session.at(second))
            } else {
                try session.script.tick(open, kgReps(20, 8), at: session.at(second))
            }
            second += 200
        }
        #expect(lines > 10, "\(lines) lines")
    }

    @Test("walking two whole sessions, every \"Then\" that names a set names a line in the list")
    func everyLineLands() throws {
        try expectEveryLineLands(try RowsSession.dayOne(), logs: 40)
        try expectEveryLineLands(try RowsSession.fullyBody(), logs: 40)
    }

    @Test("a second side: the line takes the list to it, though it cannot open yet")
    func secondSide() throws {
        var session = try RowsSession.fullyBody()
        let left = try session.row("bulgarian-split-squat", 0)
        let right = try session.row("bulgarian-split-squat", 1)
        try session.perform(.touch(left), at: 5)
        let layout = try session.layout(at: 6)
        #expect(layout.openRow == left)
        let after = try #require(layout.island.after)
        #expect(after.row == right && layout.item(right) != nil)
        #expect(after.text.lowercased().contains("right"))
    }

    @Test("a drop: while its set is open, the line names the drop and takes the list to it")
    func drop() throws {
        var session = try RowsSession.fullyBody()
        try session.tick("chest-press", 0, kgReps(25, 15), at: 60)
        try session.tick("chest-press", 1, kgReps(40, 11, rpe: 9), at: 200)
        let set2 = try session.row("chest-press", 2)
        let layout = try session.layout(at: 250)
        #expect(layout.openRow == set2)
        let after = try #require(layout.island.after)
        #expect(after.text.hasPrefix("Drop"))
        // The drop waits on set 2, drawn as `+ drop` on its line: the tap takes the list to that line.
        #expect(after.row == set2 && layout.item(set2)?.tags.contains { $0.text.contains("drop") } == true)
    }

    @Test("a cluster's next part exists in the list: the line takes the list to it")
    func cluster() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("x", EngineCatalog.press, [PlannedSet(key: "s1", structure: .cluster, load: .kg(80), rirMin: 2, rirMax: 2,
                                                                     miniSetReps: 3, miniSets: 3, intraSetPause: 20)]),
        ]), exercises: EngineCatalog.all)
        let session = RowsSession(start: engineEpoch, script: script, history: HistoryIndex([]))
        let head = try session.row("x", 0)
        let layout = try session.layout(at: 1)
        #expect(layout.openRow == head)
        let after = try #require(layout.island.after)
        let second = try #require(after.row)
        #expect(after.text == "Cluster 2, 80 kg × 3" && layout.item(second) != nil)
    }

    @Test("a running plank: the line after it still takes the list to set 2")
    func runningTimedSet() throws {
        var session = try RowsSession.fullyBody()
        let plank = try session.row("plank", 0)
        try session.perform(.touch(plank), at: 5)
        try session.perform(.startTimedSet(plank), at: 100)
        let running = try session.layout(at: 120)
        let set2 = try session.row("plank", 1)
        let after = try #require(running.island.after)
        #expect(after.text == "Set 2, 0:58 hold" && after.row == set2)
        #expect(running.openRow == plank)
    }
}

@Suite("Rows: taking a logged set back")
struct RowsTakeBackTests {
    @Test("Undo after an out-of-order Log reopens the set taken back, with its values, not the plan's next set")
    func undoOutOfOrder() throws {
        var session = try RowsSession.fullyBody()
        let calf = try session.row("calf-press", 0)
        let planned = try session.layout(at: 1).openRow
        #expect(planned != nil && planned != calf)
        // The owner opens Calf Press set 1 out of order and logs it.
        try session.perform(.touch(calf), at: 10)
        let values = try #require(try session.present(at: 20).row(calf)?.commitValues)
        try session.perform(.tick(calf), at: 30)
        let logged = try session.layout(at: 31)
        #expect(logged.item(calf)?.state == .done && logged.openRow != calf)
        // Undo: the engine's untick alone would open the plan's next set.
        var untickOnly = session
        try untickOnly.perform(.untick(calf), at: 33)
        #expect(try untickOnly.layout(at: 34).openRow == planned)
        // The take-back opens Calf Press set 1 again, in place, with what was logged.
        for action in RowsAction.takeBack(calf) { try session.perform(action, at: 33) }
        let back = try session.layout(at: 34)
        #expect(back.openRow == calf && back.item(calf)?.state == .now)
        let commit = try #require(try session.present(at: 34).row(calf)?.commitValues)
        #expect(commit.load == values.load && commit.reps == values.reps)
    }

    @Test("Untick set on an older done set opens it again, and a Log of it brings back what it was")
    func untickOlderSet() throws {
        var session = try RowsSession.dayOne()
        try session.log("leg-press", 0, at: 60)
        try session.log("leg-press", 1, at: 200)
        let warmup1 = try session.row("leg-press", 0)
        for action in RowsAction.takeBack(warmup1) { try session.perform(action, at: 300) }
        let layout = try session.layout(at: 301)
        let warmup2 = try session.row("leg-press", 1)
        #expect(layout.openRow == warmup1 && layout.item(warmup2)?.state == .done)
        // Its undo is a Log of it.
        try session.perform(.tick(warmup1), at: 310)
        #expect(try session.layout(at: 311).item(warmup1)?.state == .done)
    }
}

@Suite("Rows island: words in a group and the minimised bar")
struct RowsIslandWordsTests {
    @Test("a group's warm-up shows its orange W in the island, never Round 1 or a group letter")
    func groupWarmup() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("row", EngineCatalog.row,
                               [PlannedSet(key: "w1", role: .warmup, repsMin: 10, repsMax: 10, load: .kg(30))] + EngineCatalog.sets(2),
                               group: "a"),
            EngineCatalog.slot("curl", EngineCatalog.curl, EngineCatalog.sets(2), group: "a"),
        ], groups: [RoutineGroup(key: "a", groupKind: .superset)]), exercises: EngineCatalog.all)
        var session = RowsSession(start: engineEpoch, script: script, history: HistoryIndex([]))
        let warmup = try session.row("row", 0)
        let layout = try session.layout(at: 1)
        #expect(layout.openRow == warmup)
        let next = try #require(layout.island.next)
        #expect(next.setLabel == "W" && next.exercise == "Row" && next.label.isEmpty && next.tint == .orange)
        #expect(next.loggedText == "Row, warm-up logged")

        try session.tick("row", 0, kgReps(30, 10), at: 30)
        let working = try #require(try session.layout(at: 31).island.next)
        #expect(working.label == "Round 1" && working.setLabel == "1" && working.exercise == "Row")
    }

    @Test("the minimised bar splits the set from its values, so the values are never what is cut")
    func barValues() throws {
        var session = try RowsSession.dayOne()
        try session.log("leg-press", 0, at: 60)
        let accessory = try session.present(at: 70).accessory
        #expect(accessory.set == "Leg Press (Machine), warm-up 2" && accessory.values == "70 kg × 6")
        #expect(accessory.exercise == "Leg Press (Machine)" && accessory.setLabel == "warm-up 2")
        #expect(accessory.detail == "Leg Press (Machine), warm-up 2, 70 kg × 6")
        #expect(accessory.isResting && accessory.timerStartedAt == nil)
    }

    @Test("the minimised bar says a plank is running, with its start for the clock")
    func barTimedSet() throws {
        var session = try RowsSession.fullyBody()
        let plank = try session.row("plank", 0)
        try session.perform(.touch(plank), at: 5)
        let ready = try session.present(at: 6).accessory
        #expect(ready.values != "holding" && ready.timerStartedAt == nil)
        try session.perform(.startTimedSet(plank), at: 100)
        let running = try session.present(at: 130).accessory
        #expect(running.row == plank && running.set == "Plank, set 1" && running.values == "holding")
        #expect(running.timerStartedAt == session.at(100) && !running.isResting)
        #expect(running.detail == "Plank, set 1, holding")
    }

    @Test("the docked set carries each value's − and + beside its RPE: the island never trades the steppers for the RPE")
    func dockedStepsAndRPE() throws {
        let session = try RowsSession.dayOne()
        let next = try #require(try session.layout(at: 0).island.next)
        #expect(!next.fields.isEmpty)
        for field in next.fields {
            #expect(field.minus != nil && field.plus != nil, "\(field.field)")
        }
        let working = try #require(try RowsSession.dayOne().layout(at: 0).blocks.first?.setItems.first { $0.role != .warmup })
        var opened = session
        try opened.perform(.touch(working.id), at: 5)
        let set = try #require(try opened.layout(at: 6).island.next)
        #expect(set.rpe == "@ 8" && set.rpeStrip?.isCollapsed == false)
        #expect(set.fields.allSatisfy { $0.minus != nil && $0.plus != nil })
    }

    @Test("a compact stepper's glyph is its sign")
    func stepSymbol() {
        #expect(RowsStep(title: "\u{2212}5", accessibilityLabel: "", patch: SetValuesPatch()).symbol == "\u{2212}")
        #expect(RowsStep(title: "+0:05", accessibilityLabel: "", patch: SetValuesPatch()).symbol == "+")
    }
}
