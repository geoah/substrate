import Foundation
@testable import WorkoutsCore
import Testing

// The owner's feedback on the Rows session screen (2026-11-02): a set is
// never "started" and nothing counts up for it, sets are logged in any order,
// only a timed set runs a clock, there is no "… is up" page after a Log, and
// the island stacks up to three parts (the rest, the next set, the one after
// it), the next set docking only while its editor is out of view.

extension RowsSession {
    func layoutItem(_ row: RowID, at seconds: Int) -> RowsSetItem? {
        (try? layout(at: seconds))?.item(row)
    }
}

@Suite("Rows island: parts and order")
struct RowsIslandTests {
    @Test("the parts: rest only while resting, next only while its editor is out of view, after whenever there is one")
    func partSelection() {
        let button = RowsButton(title: "Log", action: .tick(RowID("r1")))
        let next = RowsNextPart(row: RowID("r1"), exercise: "Leg Press (Machine)", badge: nil, label: "Set 1", tint: .accent,
                                fields: [], rpe: nil, primary: button, timerStartedAt: nil, sentence: "")
        let after = RowsAfterPart(row: RowID("r2"), label: "Then", text: "Set 2, 90 kg × 10", action: .touch(RowID("r2")),
                                  sentence: "")
        let rest = RowsRestPart(kind: .rest, title: "Rest", startedAt: engineEpoch, endsAt: engineEpoch.addingTimeInterval(90),
                                totalText: "of 1:30", minus: button, plus: button, skip: button)

        let resting = RowsIsland(rest: rest, next: next, after: after, end: nil)
        #expect(resting.parts(editorInView: true) == [.rest, .after])
        #expect(resting.parts(editorInView: false) == [.rest, .next, .after])
        #expect(resting.isCounting)

        let calm = RowsIsland(rest: nil, next: next, after: after, end: nil)
        #expect(calm.parts(editorInView: true) == [.after])
        #expect(calm.parts(editorInView: false) == [.next, .after])
        #expect(!calm.isCounting)

        let lastSet = RowsIsland(rest: nil, next: next, after: nil, end: nil)
        #expect(lastSet.parts(editorInView: true).isEmpty)
        #expect(lastSet.parts(editorInView: false) == [.next])

        let done = RowsIsland(rest: rest, next: nil, after: nil, end: .allDone(title: "All sets done", detail: ""))
        #expect(done.parts(editorInView: false) == [.rest, .allDone])
        #expect(RowsIsland.empty.parts(editorInView: true) == [.empty])

        var timing = calm
        timing.next?.timerStartedAt = engineEpoch
        #expect(timing.isCounting)
    }

    @Test("a new session starts nothing: no rest, no clock, the first set open and nothing marked started")
    func newSessionStartsNothing() throws {
        let session = try RowsSession.fullyBody()
        let layout = try session.layout(at: 3)
        let warmup = try session.row("leg-press", 0)
        #expect(layout.openRow == warmup)
        #expect(layout.island.rest == nil && !layout.island.isCounting && layout.island.next?.timerStartedAt == nil)
        // The open set has no timer and no Start: only a Log.
        let editor = try #require(layout.item(warmup)?.editor)
        #expect(editor.timer == nil && editor.primary.action == .tick(warmup))
        #expect(editor.note?.kind != .running)
    }

    @Test("only timed sets have Start, a running clock and Stop; every other set logs with one tap")
    func timedOnlyTimers() throws {
        let session = try RowsSession.fullyBody()
        let layout = try session.layout(at: 0)
        let state = try session.script.current
        var opened = 0
        for block in layout.blocks {
            for item in block.setItems {
                let exercise = try #require(state.exercise(ofRow: item.id), "\(item.title)")
                let timed = exercise.measurement.hasTimer
                // Open each set as the owner would, and read its editor.
                var touched = session
                try touched.perform(.touch(item.id), at: 1)
                let opening = touched.layoutItem(item.id, at: 2)?.editor
                // A link that waits on an earlier one (a second side, a drop, a
                // cluster's next part) stays closed by design; every other set opens.
                if state.isLocked(item.id) {
                    #expect(opening == nil && item.state == .locked, "\(item.title)")
                    continue
                }
                let editor = try #require(opening, "\(item.title) did not open")
                opened += 1
                if timed {
                    #expect(editor.timer != nil && editor.primary.action == .startTimedSet(item.id), "\(item.title)")
                } else {
                    #expect(editor.timer == nil && editor.primary.action == .tick(item.id), "\(item.title)")
                    #expect(try touched.layout(at: 2).island.next?.primary.title == "Log")
                }
            }
        }
        // Every pending set the layout shows, less the locked links.
        #expect(opened == layout.blocks.flatMap(\.setItems).count { !state.isLocked($0.id) } && opened > 30)
    }

    @Test("free order across exercises: open and log sets of three exercises out of plan order, each logs and none waits")
    func freeOrderAcrossExercises() throws {
        var session = try RowsSession.fullyBody()
        let calf = try session.row("calf-press", 0)
        let legPressSet = try session.row("leg-press", 2)
        let chest = try session.row("chest-press", 1)

        // A set far down the plan first, while the leg press warm-up is the suggestion.
        try session.perform(.touch(calf), at: 10)
        #expect(try session.layout(at: 11).openRow == calf)
        try session.perform(.tick(calf), at: 30)
        // The leg press's first working set next, skipping its warm-up.
        try session.perform(.touch(legPressSet), at: 40)
        try session.perform(.tick(legPressSet), at: 60)
        // Then the chest press's first working set.
        try session.perform(.touch(chest), at: 70)
        try session.perform(.tick(chest), at: 90)

        let layout = try session.layout(at: 95)
        #expect([calf, legPressSet, chest].allSatisfy { layout.item($0)?.state == .done })
        // The skipped warm-ups are plain pending sets: no "later" tag, no waiting state, a tap opens them.
        let warmup = try #require(layout.item(try session.row("leg-press", 0)))
        #expect(warmup.state == .next && warmup.tags.isEmpty && warmup.tap == .doNow(.touch(warmup.id)))
        #expect(layout.blocks.flatMap(\.setItems).allSatisfy { !$0.tags.contains { $0.text == "later" } })
    }

    @Test("after a Log with no rest the next set shows at once: no \"is up\" page, no clock")
    func noIsUpAfterLog() throws {
        var session = try RowsSession.fullyBody()
        try session.log("glute-bridge", 0, at: 60)
        let layout = try session.layout(at: 61)
        let a2 = try session.row("bird-dog", 0)
        #expect(layout.island.rest == nil && layout.island.next?.row == a2)
        let note = layout.item(a2)?.editor?.note?.text
        #expect(!["after the rest", "after the pause", "after the switch"].contains(note))
    }

    @Test("after Skip the next set shows at once: no \"Rest skipped\" page")
    func noIsUpAfterSkip() throws {
        var session = try RowsSession.dayOne()
        try session.log("leg-press", 0, at: 60)
        let resting = try session.layout(at: 70)
        let rest = try #require(resting.island.rest)
        #expect(rest.skip.action == .skipRest)
        try session.send(.skipRest, at: 75)
        let skipped = try session.layout(at: 76)
        let warmup2 = try session.row("leg-press", 1)
        #expect(skipped.island.rest == nil && skipped.island.next?.row == warmup2 && skipped.island.end == nil)
        #expect(skipped.island.after?.text == "Set 1, 105 kg × 10")
        #expect(skipped.island.parts(editorInView: true) == [.after])
    }

    @Test("the line after the next set opens that set, and the island follows")
    func afterOpensThatSet() throws {
        var session = try RowsSession.dayOne()
        let layout = try session.layout(at: 0)
        let after = try #require(layout.island.after)
        let warmup2 = try session.row("leg-press", 1)
        #expect(after.row == warmup2 && after.action == .touch(warmup2))
        try session.perform(try #require(after.action), at: 5)
        let opened = try session.layout(at: 6)
        #expect(opened.openRow == warmup2 && opened.island.next?.row == warmup2)
        #expect(opened.island.after?.text == "Set 1, 105 kg × 10")
    }

    @Test("the minimised bar names the next set with its values and points at it")
    func minimisedBar() throws {
        var session = try RowsSession.dayOne()
        try session.log("leg-press", 0, at: 60)
        let presentation = try session.present(at: 70)
        let warmup2 = try session.row("leg-press", 1)
        #expect(presentation.accessory.detail == "Leg Press (Machine), warm-up 2, 70 kg × 6")
        #expect(presentation.accessory.row == warmup2)
        #expect(presentation.accessory.restEndsAt != nil)
    }
}
