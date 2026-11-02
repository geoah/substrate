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

    @Test("a fix: the open panel leaving for the fix's never docks it, and Cancel brings it back in view")
    func cancelFix() {
        var dock = dock(open: set2)
        dock.show(open: set2, isFixing: true)
        // The open panel is replaced by a line while set 1 is fixed: its report a beat later.
        dock.logButton(of: set2, isVisible: false)
        #expect(!dock.isDocked)
        dock.show(open: set2, isFixing: false)
        #expect(!dock.isDocked && dock.logVisible == nil)
        // A real report after the fix still counts.
        dock.logButton(of: set2, isVisible: false)
        #expect(dock.isDocked)
        // Docked, then a fix starts and ends: back in view either way.
        dock.show(open: set2, isFixing: true)
        #expect(!dock.isDocked)
        dock.show(open: set2, isFixing: false)
        #expect(!dock.isDocked)
    }

    @Test("Undo: the set taken back opens in view, and the panel the Log removed reports nothing that counts")
    func undo() {
        var dock = dock(open: set1)
        // Log set 1: set 2 opens; set 1's panel goes, and its report comes a beat later.
        dock.show(open: set2, isFixing: false)
        dock.logButton(of: set1, isVisible: false)
        #expect(!dock.isDocked)
        // Undo within five seconds: set 1 is open again, in view.
        dock.undo()
        dock.show(open: set1, isFixing: false)
        #expect(!dock.isDocked)
        // Undo while docked with the open row unchanged also puts the editor back.
        dock.logButton(of: set1, isVisible: false)
        #expect(dock.isDocked)
        dock.undo()
        #expect(!dock.isDocked)
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

@Suite("Rows island: what \"Then\" may open")
struct RowsAfterActionTests {
    /// Every "Then" action the island offers opens that set: performed, the open row is that row.
    private func expectEveryActionOpens(_ start: RowsSession, logs: Int) throws {
        var session = start
        var second = 10
        for _ in 0 ..< logs {
            let layout = try session.layout(at: second)
            guard let open = layout.openRow else { return }
            if let after = layout.island.after, let action = after.action {
                var opened = session
                try opened.perform(action, at: second + 1)
                #expect(try opened.layout(at: second + 2).openRow == after.row, "\(after.text)")
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
    }

    @Test("walking two whole sessions, every \"Then\" with an action opens its set when performed")
    func everyActionOpens() throws {
        try expectEveryActionOpens(try RowsSession.dayOne(), logs: 40)
        try expectEveryActionOpens(try RowsSession.fullyBody(), logs: 40)
    }

    @Test("a second side: the line reads, with no action, and a touch on it would not open it")
    func secondSide() throws {
        var session = try RowsSession.fullyBody()
        let left = try session.row("bulgarian-split-squat", 0)
        let right = try session.row("bulgarian-split-squat", 1)
        try session.perform(.touch(left), at: 5)
        let layout = try session.layout(at: 6)
        #expect(layout.openRow == left)
        let after = try #require(layout.island.after)
        #expect(after.row == right && after.action == nil)
        #expect(after.text.lowercased().contains("right"))
        // Why: the engine leaves a locked link closed.
        try session.perform(.touch(right), at: 7)
        #expect(try session.layout(at: 8).openRow == left)
    }

    @Test("a drop: while its set is open, the line names the drop and offers nothing to tap")
    func drop() throws {
        var session = try RowsSession.fullyBody()
        try session.tick("chest-press", 0, kgReps(25, 15), at: 60)
        try session.tick("chest-press", 1, kgReps(40, 11, rpe: 9), at: 200)
        let set2 = try session.row("chest-press", 2)
        let layout = try session.layout(at: 250)
        #expect(layout.openRow == set2)
        let after = try #require(layout.island.after)
        #expect(after.text.hasPrefix("Drop") && after.action == nil)
        if let drop = after.row {
            try session.perform(.touch(drop), at: 251)
            #expect(try session.layout(at: 252).openRow == set2)
        }
    }

    @Test("a cluster: the next cluster waits on this one, so the line only reads")
    func cluster() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("x", EngineCatalog.press, [PlannedSet(key: "s1", structure: .cluster, load: .kg(80), rirMin: 2, rirMax: 2,
                                                                     miniSetReps: 3, miniSets: 3, intraSetPause: 20)]),
        ]), exercises: EngineCatalog.all)
        var session = RowsSession(start: engineEpoch, script: script, history: HistoryIndex([]))
        let head = try session.row("x", 0)
        let layout = try session.layout(at: 1)
        #expect(layout.openRow == head)
        let after = try #require(layout.island.after)
        let second = try #require(after.row)
        #expect(after.text == "Cluster 2, 80 kg × 3" && after.action == nil)
        try session.perform(.touch(second), at: 2)
        #expect(try session.layout(at: 3).openRow == head)
    }

    @Test("a running plank keeps the editor: the line after it only reads until it stops")
    func runningTimedSet() throws {
        var session = try RowsSession.fullyBody()
        let plank = try session.row("plank", 0)
        try session.perform(.touch(plank), at: 5)
        let ready = try session.layout(at: 6)
        // Ready, not running: set 2 can open.
        let set2 = try session.row("plank", 1)
        #expect(ready.island.after?.action == .touch(set2))
        try session.perform(.startTimedSet(plank), at: 100)
        let running = try session.layout(at: 120)
        let after = try #require(running.island.after)
        #expect(after.text == "Set 2, 0:58 hold" && after.row == set2 && after.action == nil)
        try session.perform(.touch(set2), at: 121)
        #expect(try session.layout(at: 122).openRow == plank)
    }
}

@Suite("Rows island: words in a group and the minimised bar")
struct RowsIslandWordsTests {
    @Test("a group's warm-up reads Warm-up in the island, never Round 1")
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
        #expect(next.badge == "A1" && next.exercise == "Row" && next.label == "Warm-up" && next.tint == .orange)
        #expect(next.loggedText == "A1 Warm-up logged")

        try session.tick("row", 0, kgReps(30, 10), at: 30)
        let working = try #require(try session.layout(at: 31).island.next)
        #expect(working.label == "Round 1" && working.badge == "A1")
    }

    @Test("the minimised bar splits the set from its values, so the values are never what is cut")
    func barValues() throws {
        var session = try RowsSession.dayOne()
        try session.log("leg-press", 0, at: 60)
        let accessory = try session.present(at: 70).accessory
        #expect(accessory.set == "Leg Press (Machine), warm-up 2" && accessory.values == "70 kg × 6")
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

    @Test("a compact stepper's glyph is its sign")
    func stepSymbol() {
        #expect(RowsStep(title: "\u{2212}5", accessibilityLabel: "", patch: SetValuesPatch()).symbol == "\u{2212}")
        #expect(RowsStep(title: "+0:05", accessibilityLabel: "", patch: SetValuesPatch()).symbol == "+")
    }
}
