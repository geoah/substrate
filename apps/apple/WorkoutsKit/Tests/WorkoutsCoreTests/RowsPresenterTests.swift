import Foundation
@testable import WorkoutsCore
import Testing

// The Rows layout (§7.6 Rows, the owner's chosen session screen) over the
// example sessions and routines: the moments the design's screens show, each
// driven through the engine as the app drives it (the Log button ticks with
// the row's `commitValues`, a stepper sends its patch), then read from
// `SessionPresentation.rowsLayout` as the views will read it.

private func d(_ text: String) -> Decimal { Decimal(string: text, locale: Locale(identifier: "en_US_POSIX"))! }

/// A session from an example routine, with the example sessions before it as
/// history and the Rows screen's settings (the RPE pre-fill on).
struct RowsSession {
    let start: Date
    var script: EngineScript
    let history: HistoryIndex
    var settings = PresenterSettings(calendar: Progression.calendar, prefillsRPE: true)

    /// Thu 29 Oct 12:00, Day 1 – Strength Foundation, after the 22 and 26 October Day 1 sessions.
    static func dayOne() throws -> RowsSession {
        let start = DateText.parse("2026-10-29T12:00:00Z")!
        let exercises = try EngineFixtures.exercises()
        let routine = try #require(try EngineFixtures.routines()["day-1-strength-foundation"])
        var script = EngineScript(at: start, seed: 31)
        try script.start(routine, exercises: routine.slots.compactMap { exercises[$0.exercise] }, bodyMass: .kg(82))
        return RowsSession(start: start, script: script, history: try Progression.history(before: start))
    }

    /// Fri 30 Oct 13:42, Fully Body Collection #1, after the 27 October one.
    static func fullyBody() throws -> RowsSession {
        let start = NextFullyBody.start
        return RowsSession(start: start, script: try Progression.start(at: start), history: try Progression.history(before: start))
    }

    func at(_ seconds: Int) -> Date { start.addingTimeInterval(TimeInterval(seconds)) }

    func present(at seconds: Int) throws -> SessionPresentation {
        SessionPresenter.present(try script.current, history: history, settings: settings, now: at(seconds))
    }

    func layout(at seconds: Int) throws -> RowsLayout {
        try present(at: seconds).rowsLayout
    }

    func row(_ slot: String, _ index: Int) throws -> RowID {
        try script.row(slot, index)
    }

    /// Log: what the button sends, the row's `commitValues` as presented then.
    mutating func log(_ slot: String, _ index: Int, at seconds: Int) throws {
        let id = try row(slot, index)
        let values = try #require(try present(at: seconds).row(id)?.commitValues, "nothing to log on \(slot) \(index)")
        try script.tick(id, values, at: at(seconds))
    }

    mutating func tick(_ slot: String, _ index: Int, _ values: SetValues, at seconds: Int) throws {
        try script.tick(try row(slot, index), values, at: at(seconds))
    }

    mutating func edit(_ row: RowID, _ patch: SetValuesPatch, at seconds: Int) throws {
        try script.send(.editSet(.init(row: row, values: patch)), at: at(seconds))
    }

    mutating func send(_ event: SessionEvent, at seconds: Int) throws {
        try script.send(event, at: at(seconds))
    }

    /// What a Rows action does, as the controller maps it to a command.
    mutating func perform(_ action: RowsAction, at seconds: Int) throws {
        switch action {
        case let .tick(row):
            let values = try #require(try present(at: seconds).row(row)?.commitValues)
            try script.tick(row, values, at: at(seconds))
        case let .touch(row): try send(.touchRow(row: row), at: seconds)
        case let .edit(row, patch): try edit(row, patch, at: seconds)
        case let .removeSet(row): try send(.removeSet(row: row), at: seconds)
        case let .startTimedSet(row):
            guard case let .ready(target) = try present(at: seconds).row(row)?.timer else { Issue.record("no timer"); return }
            try send(.startTimedSet(row: row, target: target), at: seconds)
        case .addSet, .stopTimedSet, .cancelTimedSet, .adjustRest, .skipRest:
            Issue.record("not used here: \(action)")
        }
    }
}

extension RowsBlock {
    /// The block's own set lines, in order (folded ones left out).
    var lines: [RowsSetItem] {
        items.compactMap { if case let .set(item) = $0 { item } else { nil } }
    }

    /// Each item as a short word, for order checks.
    var shape: [String] {
        items.map { item in
            switch item {
            case let .set(set): "\(set.badge.map { "\($0) " } ?? "")\(set.title): \(set.state.rawValue)"
            case let .warmups(fold): "\(fold.title): \(fold.values)"
            case let .round(header): header.title
            case let .connector(connector): "\(connector.lead), \(connector.text) (\(connector.phase.rawValue))"
            case let .chainHeader(header): header.text
            case let .miniSets(line): "\(line.title) \(line.chips.map(\.text).joined(separator: " "))"
            }
        }
    }
}

extension RowsLayout {
    func block(_ id: String) -> RowsBlock? { blocks.first { $0.id == id } }
}

@Suite("Rows layout: the open set")
struct RowsOpenSetTests {
    @Test("Leg press set 2 open: warm-ups folded, set 1 done with its change, set 2's editor with RPE 8 pre-filled")
    func setTwoOpen() throws {
        var session = try RowsSession.dayOne()
        try session.log("leg-press", 0, at: 60)
        try session.log("leg-press", 1, at: 160)
        try session.log("leg-press", 2, at: 400)
        let presentation = try session.present(at: 560)
        let layout = presentation.rowsLayout
        let block = try #require(layout.block("leg-press"))
        #expect(block.title == "Leg Press (Machine)")
        #expect(block.subtitle == "6–10 reps at RPE 8, rest 2:30")
        #expect(!block.isFinished && block.summary == nil)
        #expect(block.addSets.map(\.title) == ["Add set"])
        #expect(block.addSets.first?.action == .addSet(slot: "leg-press", role: .working))
        #expect(block.shape == ["Warm-ups: 40 kg × 12, 70 kg × 6", "Set 1: done", "Set 2: now", "Set 3: next"])
        guard case let .warmups(fold) = block.items[0] else { Issue.record("no warm-up fold"); return }
        #expect(fold.comparison == RowsChange(text: "as last time", isGain: false))
        #expect(fold.sets.map(\.state) == [.done, .done])
        #expect(fold.sentence == "Warm-ups done: 40 kilograms, 12 reps; 70 kilograms, 6 reps. The same as last time.")

        let (set1, set2, set3) = (block.lines[0], block.lines[1], block.lines[2])
        #expect(set1.values == "105 kg × 10" && set1.rpe == "@ 8" && set1.rpeIsPrefilled)
        #expect(set1.changes == [RowsChange(text: "+1 rep", isGain: true)] && set1.verdict == .better)
        #expect(set1.last == nil && set1.tap == .fix && set1.fix != nil && set1.editor == nil)
        #expect(set1.sentence
            == "Set 1, done: 105 kilograms, 10 reps, RPE 8 as planned. 1 rep more than last time, better than last time.")
        #expect(set2.state == .now && set2.tap == .none && set2.collapsedTag == RowsTag(text: "up next", style: .accent))
        #expect(layout.openRow == set2.id)
        #expect(set3.values == "105 kg × 10" && set3.last == "last 105 × 8 @ 9" && set3.tap == .doNow(.touch(set3.id)))
        #expect(set3.sentence == "Set 3, coming up: 105 kilograms, 10 reps. Last time 105 kilograms, 8 reps.")

        let editor = try #require(set2.editor)
        #expect(editor.mode == .log && editor.tint == .accent && editor.title == "Set 2" && editor.badge == nil)
        #expect(editor.note == RowsNote(text: "Set 1: 105 kg × 10, assuming RPE 8; try 105 kg × 10", kind: .reason))
        #expect(editor.fields.map(\.field) == [.load, .reps])
        let load = try #require(editor.field(.load))
        #expect(load.value == "105" && load.unit == "kg" && load.last == "last 105" && !load.isChanged && load.reset == nil)
        #expect(load.minus == RowsStep(title: "\u{2212}5", accessibilityLabel: "5 kilograms less",
                                       patch: SetValuesPatch(load: .set(.kg(100)))))
        #expect(load.plus?.title == "+5" && load.plus?.patch == SetValuesPatch(load: .set(.kg(110))))
        #expect(load.accessibilityLabel == "Leg Press (Machine), set 2, load in kilograms")
        #expect(load.accessibilityValue == "105, suggested")
        let reps = try #require(editor.field(.reps))
        #expect(reps.value == "10" && reps.unit == "reps" && reps.last == "last 9")
        #expect(reps.minus?.patch == SetValuesPatch(reps: .set(9)) && reps.plus?.patch == SetValuesPatch(reps: .set(11)))

        let strip = try #require(editor.rpe)
        #expect(strip.shown == 8 && !strip.isRated && !strip.isCollapsed)
        #expect(strip.cells.map(\.text) == ["6", "6.5", "7", "7.5", "8", "8.5", "9", "9.5", "10"])
        #expect(strip.cells.filter(\.isHalf).map(\.text) == ["6.5", "7.5", "8.5", "9.5"])
        #expect(strip.cells.filter { $0.style != .plain }.map(\.text) == ["8"])
        // The pre-filled cell is hollow; a tap on it rates it.
        #expect(strip.cells[4].style == .outlined && strip.cells[4].patch == .enteringRPE(8))
        #expect(strip.cells[5].meaning == "RPE 8.5: could do 1 more rep, maybe 2")
        #expect(strip.accessibilityValue == "8, planned, not rated yet")

        // The Log button names exactly what the tick commits.
        #expect(editor.primary == RowsButton(title: "Log 105 kg × 10 @ 8", action: .tick(set2.id)))
        #expect(presentation.row(set2.id)?.commitValues
            == SetValues(load: .kg(105), reps: 10, rpe: 8, rpeMethod: .prefilled))
        #expect(layout.capsule == .upNext(RowsNowCapsule(row: set2.id, tone: .restDone, title: "Set 2 is up",
                                                          detail: "Rest 2:30 done", since: session.at(400),
                                                          sinceLabel: "since set 1")))
    }

    @Test("One rep fewer: −1 turns the value to the tint, offers ↺ 10 back, and the Log label follows")
    func oneRepFewer() throws {
        var session = try RowsSession.dayOne()
        try session.log("leg-press", 0, at: 60)
        try session.log("leg-press", 1, at: 160)
        try session.log("leg-press", 2, at: 400)
        let set2 = try session.row("leg-press", 3)
        let before = try #require(try session.layout(at: 560).item(set2)?.editor)
        try session.edit(set2, try #require(before.field(.reps)?.minus?.patch), at: 565)

        let item = try #require(try session.layout(at: 566).item(set2))
        let editor = try #require(item.editor)
        let reps = try #require(editor.field(.reps))
        #expect(reps.value == "9" && reps.isChanged && reps.last == "last 9")
        #expect(reps.reset == RowsReset(text: "10", accessibilityLabel: "Back to 10", patch: SetValuesPatch(reps: .cleared)))
        #expect(reps.accessibilityValue == "9, changed from 10")
        #expect(editor.field(.load)?.isChanged == false)
        #expect(editor.primary.title == "Log 105 kg × 9 @ 8")
        #expect(item.values == "105 kg × 9" && item.valuesChanged)

        // ↺ goes back to the suggestion.
        try session.edit(set2, try #require(reps.reset?.patch), at: 570)
        let back = try #require(try session.layout(at: 571).item(set2))
        #expect(back.editor?.field(.reps)?.value == "10" && back.editor?.field(.reps)?.isChanged == false)
        #expect(!back.valuesChanged && back.editor?.primary.title == "Log 105 kg × 10 @ 8")
    }

    @Test("RPE in one tap: a cell is rated at once, the pre-filled one too, and a tap on the selected cell clears it")
    func rpeStrip() throws {
        var session = try RowsSession.dayOne()
        try session.log("leg-press", 0, at: 60)
        try session.log("leg-press", 1, at: 160)
        let set1 = try session.row("leg-press", 2)
        func strip(_ seconds: Int) throws -> (RowsRPEStrip, RowsEditor, SetValues?) {
            let presentation = try session.present(at: seconds)
            let editor = try #require(presentation.rowsLayout.item(set1)?.editor)
            return (try #require(editor.rpe), editor, presentation.row(set1)?.commitValues)
        }

        let (first, _, _) = try strip(300)
        try session.edit(set1, first.cells[5].patch, at: 301)
        let (rated, editor, commit) = try strip(302)
        #expect(rated.shown == d("8.5") && rated.isRated)
        #expect(rated.cells.map(\.style) == [.plain, .plain, .plain, .plain, .plain, .filled, .plain, .plain, .plain])
        #expect(rated.cells[5].patch == .clearingRPE && rated.cells[4].patch == .enteringRPE(8))
        #expect(editor.primary.title == "Log 105 kg × 10 @ 8.5")
        #expect(commit?.rpe == d("8.5") && commit?.rpeMethod == .entered)

        // The pre-filled value tapped is a rating, solid.
        try session.edit(set1, rated.cells[4].patch, at: 303)
        let (eight, _, commitEight) = try strip(304)
        #expect(eight.cells[4].style == .filled && eight.isRated && eight.accessibilityValue == "8")
        #expect(commitEight?.rpe == 8 && commitEight?.rpeMethod == .entered)

        // A tap on the selected cell clears it: no RPE, no pre-fill, the strip stays open.
        try session.edit(set1, eight.cells[4].patch, at: 305)
        let (cleared, clearedEditor, commitCleared) = try strip(306)
        #expect(cleared.shown == nil && !cleared.isCollapsed && cleared.accessibilityValue == "not rated")
        #expect(clearedEditor.primary.title == "Log 105 kg × 10")
        #expect(commitCleared?.rpe == nil)
    }

    @Test("Warm-ups: warm-up 1 open in orange with no strip shown, then one done, then both fold into one line")
    func warmupFolding() throws {
        var session = try RowsSession.dayOne()
        let layout = try session.layout(at: 0)
        let block = try #require(layout.block("leg-press"))
        #expect(block.shape == ["Warm-up 1: now", "Warm-up 2: next", "Set 1: next", "Set 2: next", "Set 3: next"])
        let warmup = try #require(block.lines.first)
        #expect(warmup.role == .warmup && warmup.values == "40 kg × 12" && warmup.last == "last 40 × 12")
        let editor = try #require(warmup.editor)
        #expect(editor.tint == .orange && editor.title == "Warm-up 1")
        #expect(editor.note == RowsNote(text: "of 2", kind: .status))
        #expect(editor.rpe?.isCollapsed == true && editor.rpe?.addTitle == "+ RPE")
        #expect(editor.primary.title == "Log warm-up 40 kg × 12")
        #expect(layout.capsule == .upNext(RowsNowCapsule(
            row: warmup.id, tone: .orange, title: "Leg Press (Machine), warm-up 1 is up",
            detail: "Rest 0:45 after, then warm-up 2, 70 kg × 6", since: session.start, sinceLabel: "session"
        )))

        try session.log("leg-press", 0, at: 60)
        let one = try #require(try session.layout(at: 61).block("leg-press"))
        #expect(one.shape.prefix(2) == ["Warm-up 1: done", "Warm-up 2: now"])
        // A done warm-up shows its change, never a verdict.
        #expect(one.lines[0].changes == [RowsChange(text: "as last time", isGain: false)] && one.lines[0].verdict == nil)

        try session.log("leg-press", 1, at: 160)
        let both = try #require(try session.layout(at: 161).block("leg-press"))
        #expect(both.shape.first == "Warm-ups: 40 kg × 12, 70 kg × 6")
        #expect(both.lines.map(\.title) == ["Set 1", "Set 2", "Set 3"])
        #expect(both.item(try session.row("leg-press", 0))?.state == .done)
    }

    @Test("Big jumps: the keypad's chips are six steps of the grid from one below, and typed text becomes a patch")
    func keypad() throws {
        var session = try RowsSession.dayOne()
        try session.log("leg-press", 0, at: 60)
        try session.log("leg-press", 1, at: 160)
        let editor = try #require(try session.layout(at: 300).item(try session.row("leg-press", 2))?.editor)
        let load = try #require(editor.field(.load)?.keypad)
        #expect(load.title == "Load in kg" && load.chipsTitle == "5 kg steps, tap one to set it" && load.allowsDecimal)
        #expect(load.chips.map(\.text) == ["100", "105", "110", "115", "120", "125"])
        #expect(load.chips.filter(\.isCurrent).map(\.text) == ["105"])
        #expect(load.chips[4].patch == SetValuesPatch(load: .set(.kg(120))))
        #expect(load.patch("120") == SetValuesPatch(load: .set(.kg(120))) && load.confirmTitle("120") == "Set 120 kg")
        #expect(load.patch("102,5") == SetValuesPatch(load: .set(.kg(d("102.5")))))
        #expect(load.patch("") == nil && load.patch("1.2.3") == nil && load.confirmTitle("x") == "Set")
        let reps = try #require(editor.field(.reps)?.keypad)
        #expect(reps.chips.isEmpty && reps.chipsTitle == nil && !reps.allowsDecimal)
        #expect(reps.patch("8") == SetValuesPatch(reps: .set(8)) && reps.patch("8.5") == nil && reps.confirmTitle("8") == "Set 8 reps")
        // A time is typed as digits, the last two the seconds.
        #expect(RowsKeypad.value("107", field: .duration) == 67 && RowsKeypad.value("45", field: .duration) == 45)
        #expect(RowsKeypad.value("190", field: .duration) == nil && RowsKeypad.value("0", field: .duration) == nil)

        // A chip applies at once: the load changes and the reset chip offers 105 back.
        let set1 = try session.row("leg-press", 2)
        try session.edit(set1, load.chips[4].patch, at: 301)
        let after = try #require(try session.layout(at: 302).item(set1)?.editor?.field(.load))
        #expect(after.value == "120" && after.isChanged && after.reset?.text == "105")
        #expect(after.keypad.chips.map(\.text) == ["115", "120", "125", "130", "135", "140"])
    }

    @Test("The Table layout's settings pre-fill nothing: the Rows editor shows no RPE and Log commits none")
    func noPrefillOnTable() throws {
        var session = try RowsSession.dayOne()
        session.settings = Progression.settings
        try session.log("leg-press", 0, at: 60)
        try session.log("leg-press", 1, at: 160)
        let set1 = try session.row("leg-press", 2)
        let presentation = try session.present(at: 300)
        let editor = try #require(presentation.rowsLayout.item(set1)?.editor)
        #expect(editor.rpe?.shown == nil && editor.rpe?.isRated == false)
        #expect(editor.primary.title == "Log 105 kg × 10")
        #expect(presentation.row(set1)?.commitValues == SetValues(load: .kg(105), reps: 10))
        #expect(presentation.row(set1)?.rpeEntry == .prefill(8))
    }
}

@Suite("Rows layout: rests, fixes and order")
struct RowsFlowTests {
    /// Day 1 after both warm-ups, set 1 at 400 s and set 2 at 600 s (each logged as shown).
    func afterSetTwo() throws -> RowsSession {
        var session = try RowsSession.dayOne()
        try session.log("leg-press", 0, at: 60)
        try session.log("leg-press", 1, at: 160)
        try session.log("leg-press", 2, at: 400)
        try session.log("leg-press", 3, at: 600)
        return session
    }

    @Test("Resting after set 2: the capsule counts down with −15, +15 and Skip, and says what comes after set 3")
    func resting() throws {
        let session = try afterSetTwo()
        let layout = try session.layout(at: 642)
        let block = try #require(layout.block("leg-press"))
        #expect(block.shape == ["Warm-ups: 40 kg × 12, 70 kg × 6", "Set 1: done", "Set 2: done", "Set 3: now"])
        #expect(block.lines[1].changes == [RowsChange(text: "+1 rep", isGain: true)] && block.lines[1].verdict == .better)
        #expect(block.lines[2].editor?.note == RowsNote(text: "after the rest", kind: .status))
        guard case let .resting(rest) = layout.capsule else { Issue.record("not resting: \(layout.capsule)"); return }
        #expect(rest.kind == .rest && rest.title == "Rest, then set 3" && rest.totalText == "of 2:30")
        #expect(rest.startedAt == session.at(600) && rest.endsAt == session.at(750))
        #expect(rest.afterLabel == "After set 3")
        #expect(rest.afterText == "Iso-Lateral Chest Press (Machine), set 1, 25 kg × 7")
        #expect(rest.minus.action == .adjustRest(-15) && rest.plus.action == .adjustRest(15) && rest.skip.action == .skipRest)
        #expect(rest.minus.title == "\u{2212}15" && rest.skip.title == "Skip")
    }

    @Test("Fixing set 1: a neutral editor on what was logged, ↺ was 10, Save sends one edit, the rest keeps running")
    func fixPastSet() throws {
        var session = try afterSetTwo()
        let set1 = try session.row("leg-press", 2)
        let item = try #require(try session.layout(at: 650).item(set1))
        let fix = try #require(item.fix)
        #expect(fix.logged == SetValues(load: .kg(105), reps: 10, rpe: 8, rpeMethod: .prefilled))
        let untouched = fix.editor(draft: fix.logged)
        #expect(untouched.mode == .fix && untouched.tint == .neutral && untouched.title == "Set 1")
        #expect(untouched.note == RowsNote(text: "logged at 12:06", kind: .status))
        #expect(untouched.cancelTitle == "Cancel")
        #expect(untouched.delete == RowsButton(title: "Delete set", accessibilityLabel: "Delete Leg Press (Machine), set 1",
                                               action: .removeSet(set1)))
        #expect(untouched.primary.title == "Save 105 kg × 10 @ 8" && !untouched.primary.isEnabled)
        // Logged as the plan's pre-fill, it is no rating: hollow, and a tap rates it.
        #expect(untouched.rpe?.cells[4].style == .outlined && untouched.rpe?.isRated == false)
        #expect(untouched.rpe?.cells[4].patch == .enteringRPE(8))
        #expect(untouched.rpe?.accessibilityValue == "8, as planned, not rated")
        #expect(fix.save(fix.logged) == nil)

        var draft = fix.logged.applying(try #require(untouched.field(.reps)?.minus?.patch))
        let changed = fix.editor(draft: draft)
        let reps = try #require(changed.field(.reps))
        #expect(reps.value == "9" && reps.isChanged)
        #expect(reps.reset == RowsReset(text: "was 10", accessibilityLabel: "Back to 10, as logged",
                                        patch: SetValuesPatch(reps: .set(10))))
        #expect(changed.primary.title == "Save 105 kg × 9 @ 8" && changed.primary.isEnabled)
        #expect(changed.primary.action == .edit(set1, SetValuesPatch(reps: .set(9))))
        #expect(fix.save(draft) == .edit(set1, SetValuesPatch(reps: .set(9))))
        // A tap on the logged RPE rates it: Save sends it as entered.
        draft = draft.applying(try #require(changed.rpe?.cells[4].patch))
        #expect(fix.save(draft) == .edit(set1, SetValuesPatch(reps: .set(9), rpe: .set(8), rpeMethod: .entered)))

        let countdown = try session.script.current.countdown
        try session.perform(try #require(fix.save(draft)), at: 660)
        let state = try session.script.current
        #expect(state.rows[set1]?.performed?.values == SetValues(load: .kg(105), reps: 9, rpe: 8, rpeMethod: .entered))
        #expect(state.countdown == countdown)
        let after = try #require(try session.layout(at: 661).item(set1))
        #expect(after.values == "105 kg × 9" && after.changes == [RowsChange(text: "as last time", isGain: false)])
    }

    @Test("Out of order: a tap on a later row opens it, the passed-over set waits with a later tag, Go back returns")
    func outOfOrder() throws {
        var session = try afterSetTwo()
        let chest = try session.row("iso-lateral-chest-press", 0)
        let set3 = try session.row("leg-press", 4)
        let before = try #require(try session.layout(at: 800).item(chest))
        #expect(before.state == .next && before.tap == .doNow(.touch(chest)))
        try session.perform(.touch(chest), at: 800)

        let layout = try session.layout(at: 801)
        let waiting = try #require(layout.item(set3))
        #expect(waiting.state == .later && waiting.tags == [RowsTag(text: "later", style: .grey)])
        #expect(waiting.tap == .doNow(.touch(set3)) && waiting.editor == nil)
        #expect(waiting.sentence == "Set 3, waiting: 105 kilograms, 10 reps. Last time 105 kilograms, 8 reps.")
        #expect(layout.item(chest)?.state == .now && layout.openRow == chest)
        #expect(layout.item(chest)?.editor?.primary.title == "Log 25 kg × 7 @ 8")
        let goBack = RowsButton(title: "Go back", accessibilityLabel: "Go back to Leg Press (Machine), set 3", action: .touch(set3))
        #expect(layout.capsule == .waiting(RowsWaitingCapsule(row: set3, title: "Leg Press (Machine), set 3 is waiting",
                                                               detail: "Go back to it when you are ready", goBack: goBack)))

        try session.perform(goBack.action, at: 802)
        let back = try session.layout(at: 803)
        #expect(back.openRow == set3 && back.item(set3)?.state == .now && back.item(chest)?.state == .next)
        if case .waiting = back.capsule { Issue.record("still waiting") }
    }

    @Test("The verdict reads effort when both sets were rated: the same numbers at a higher RPE are below")
    func verdictWithRatings() throws {
        var harder = try RowsSession.dayOne()
        try harder.log("leg-press", 0, at: 60)
        try harder.log("leg-press", 1, at: 160)
        try harder.log("leg-press", 2, at: 400)
        // Last time's set 2 was 105 kg × 9 at a rated 8.5.
        try harder.tick("leg-press", 3, kgReps(105, 9, rpe: d("9.5")), at: 600)
        let below = try #require(try harder.layout(at: 610).item(try harder.row("leg-press", 3)))
        #expect(below.changes == [RowsChange(text: "as last time", isGain: false)] && below.verdict == .below)
        #expect(below.sentence == "Set 2, done: 105 kilograms, 9 reps, RPE 9.5. The same as last time, below last time.")

        var level = try RowsSession.dayOne()
        try level.log("leg-press", 0, at: 60)
        try level.log("leg-press", 1, at: 160)
        try level.log("leg-press", 2, at: 400)
        try level.tick("leg-press", 3, kgReps(105, 9, rpe: d("8.5")), at: 600)
        let same = try #require(try level.layout(at: 610).item(try level.row("leg-press", 3)))
        #expect(same.verdict == .same)
        #expect(same.sentence == "Set 2, done: 105 kilograms, 9 reps, RPE 8.5. The same as last time.")
    }
}

@Suite("Rows layout: groups, chains and timed sets")
struct RowsStructureTests {
    @Test("The superset mid-round: rounds in the order done, the transition and round rest as connectors, go straight to A2")
    func supersetMidRound() throws {
        var session = try RowsSession.fullyBody()
        try session.log("glute-bridge", 0, at: 60)
        let layout = try session.layout(at: 69)
        let block = try #require(layout.block("group-a"))
        #expect(block.title == "Superset A" && block.subtitle == "Glute Bridge and Bird Dog, 2 rounds")
        #expect(block.group == "a" && block.slots == ["glute-bridge", "bird-dog"])
        #expect(block.addSets.map(\.title) == ["Add A1 set", "Add A2 set"])
        #expect(block.shape == [
            "Round 1 of 2",
            "A1 Glute Bridge: done",
            "0:00, straight to A2 (passed)",
            "A2 Bird Dog: now",
            "Rest 1:00, then round 2 (upcoming)",
            "Round 2 of 2",
            "A1 Glute Bridge: next",
            "0:00, straight to A2 (upcoming)",
            "A2 Bird Dog: next",
            "Rest 1:00, then Cable Core Pallof Press (upcoming)",
        ])
        guard case let .connector(transition) = block.items[2] else { Issue.record("no transition"); return }
        #expect(transition.kind == .transition && transition.seconds == 0 && transition.sentence == "No rest, straight to A2")
        let a1 = block.lines[0]
        #expect(a1.values == "16 reps" && a1.changes == [RowsChange(text: "+1 rep", isGain: true)] && a1.verdict == .better)
        let editor = try #require(block.lines[1].editor)
        #expect(editor.badge == "A2" && editor.title == "Bird Dog" && editor.fields.map(\.field) == [.reps])
        #expect(editor.field(.reps)?.value == "11" && editor.field(.reps)?.unit == "per side")
        #expect(editor.field(.reps)?.last == "last 10")
        // No plan effort and no rating last time: the strip waits behind + RPE.
        #expect(editor.rpe?.isCollapsed == true)
        #expect(editor.primary.title == "Log A2, 11 per side")
        let a2 = try session.row("bird-dog", 0)
        #expect(layout.capsule == .go(RowsNowCapsule(row: a2, tone: .accent, title: "A2 Bird Dog now, no rest",
                                                      detail: "Rest 1:00 after, then A1 Glute Bridge, round 2, 16 reps",
                                                      since: session.at(60), sinceLabel: "since A1")))

        // A2 done: the round rest runs.
        try session.log("bird-dog", 0, at: 100)
        let resting = try session.layout(at: 110)
        let rested = try #require(resting.block("group-a"))
        #expect(rested.shape[4] == "Rest 1:00, then round 2 (running)" && rested.items.count == 10)
        guard case let .resting(rest) = resting.capsule else { Issue.record("not resting"); return }
        #expect(rest.kind == .roundRest && rest.title == "Round rest, then A1 Glute Bridge, round 2")
    }

    @Test("The chest press drop: a + drop tag until set 2 is done, then the drop hangs off it and opens at once")
    func drop() throws {
        var session = try RowsSession.fullyBody()
        try session.tick("chest-press", 0, kgReps(25, 15), at: 60)
        try session.tick("chest-press", 1, kgReps(40, 11, rpe: 9), at: 200)
        let waiting = try #require(try session.layout(at: 250).block("chest-press"))
        #expect(waiting.subtitle == "10–15 reps at RPE 9.5, rest 1:30, set 2 ends in a drop")
        #expect(waiting.shape == ["Warm-up: 25 kg × 15", "Set 1: done", "Set 2: now"])
        #expect(waiting.lines[1].tags == [RowsTag(text: "+ drop", style: .orange)])

        try session.tick("chest-press", 2, kgReps(35, 14, rpe: 10), at: 400)
        let layout = try session.layout(at: 406)
        let block = try #require(layout.block("chest-press"))
        #expect(block.shape == ["Warm-up: 25 kg × 15", "Set 1: done", "Set 2: done", "Drop: now"])
        let set1 = block.lines[0]
        // Heavier for fewer reps reads below on the estimate (the design's documented risk).
        #expect(set1.changes == [RowsChange(text: "+5 kg", isGain: true), RowsChange(text: "\u{2212}4 reps", isGain: false)])
        #expect(set1.verdict == .below)
        let drop = block.lines[2]
        #expect(drop.role == .drop && drop.isChained && drop.values == "25 kg to failure" && drop.last == "last 35 × 8 @ 9.5")
        let editor = try #require(drop.editor)
        #expect(editor.tint == .orange && editor.title == "Drop" && editor.trailingNote == "no rest")
        #expect(editor.note == RowsNote(text: "to failure", kind: .status))
        let reps = try #require(editor.field(.reps))
        #expect(reps.value == nil && reps.placeholder == "to failure" && reps.isMissing && reps.last == "last 8")
        // The steppers start from last time's 8.
        #expect(reps.minus?.patch == SetValuesPatch(reps: .set(7)) && reps.plus?.patch == SetValuesPatch(reps: .set(9)))
        #expect(editor.rpe?.isCollapsed == false && editor.rpe?.shown == nil)
        #expect(editor.primary.title == "Log drop 25 kg" && !editor.primary.isEnabled)
        #expect(layout.capsule == .go(RowsNowCapsule(row: drop.id, tone: .orange, title: "Drop to 25 kg now",
                                                      detail: "Rest 1:30 after, then Seated Row (Machine), set 1, 50 kg × 11",
                                                      since: session.at(400), sinceLabel: "since set 2")))

        try session.edit(drop.id, SetValuesPatch(reps: .set(12)), at: 410)
        let typed = try #require(try session.layout(at: 411).item(drop.id)?.editor)
        #expect(typed.primary.title == "Log drop 25 kg × 12" && typed.primary.isEnabled)
        try session.perform(typed.primary.action, at: 420)
        let finished = try #require(try session.layout(at: 421).block("chest-press"))
        #expect(finished.isFinished)
        #expect(finished.summary?.text == "40 kg × 11, 35 kg × 14, drop 25 kg × 12")
        #expect(finished.summary?.change == RowsChange(text: "+5 kg", isGain: true))
        #expect(finished.summary?.sentence == "Chest Press (Machine), done: 40 kilograms, 11 reps; 35 kilograms, 14 reps; "
            + "drop, 25 kilograms, 12 reps. 5 kilograms more than last time.")
    }

    @Test("The plank: ready with its goal and last time on the track, then running with Stop and log")
    func plank() throws {
        var session = try RowsSession.fullyBody()
        let plank = try session.row("plank", 0)
        try session.perform(.touch(plank), at: 5)
        let ready = try #require(try session.layout(at: 6).item(plank)?.editor)
        #expect(ready.fields.map(\.field) == [.duration])
        let duration = try #require(ready.field(.duration))
        #expect(duration.value == "1:07" && duration.unit == nil && duration.last == "last 1:02")
        #expect(duration.minus?.title == "\u{2212}0:05" && duration.plus?.patch == SetValuesPatch(duration: .set(72)))
        let timer = try #require(ready.timer)
        #expect(timer.phase == .ready && timer.goal == 67 && timer.last == 62 && timer.trackEnd == 80)
        #expect(timer.goalText == "goal 1:07" && timer.lastText == "last 1:02")
        #expect(ready.primary.title == "Start" && ready.primary.action == .startTimedSet(plank))
        #expect(ready.primary.accessibilityLabel == "Start Plank, set 1, goal 1:07")
        #expect(ready.secondary == RowsButton(title: "Log 1:07 hold", action: .tick(plank)))
        #expect(ready.rpe?.isCollapsed == true)

        try session.perform(ready.primary.action, at: 100)
        let layout = try session.layout(at: 141)
        let running = try #require(layout.item(plank)?.editor)
        #expect(running.fields.isEmpty && running.note == RowsNote(text: "holding", kind: .running))
        #expect(running.timer?.phase == .running(startedAt: session.at(100), endsAt: session.at(167)))
        #expect(running.primary.title == "Stop and log" && running.primary.action == .stopTimedSet(plank))
        #expect(running.secondary?.action == .cancelTimedSet(plank))
        #expect(timer.caption(elapsed: 41) == "0:21 to beat last time")
        #expect(timer.caption(elapsed: 63) == "0:04 to the goal" && timer.caption(elapsed: 70) == "Goal reached")
        #expect(timer.fraction(40) == 0.5 && timer.fraction(200) == 1)
        #expect(layout.capsule == .timing(RowsTimingCapsule(row: plank, title: "Then rest 0:15",
                                                             detail: "Set 2, 0:58 hold, last 0:53",
                                                             startedAt: session.at(100), endsAt: session.at(167))))
    }

    @Test("Myo-reps: a header, the activation, mini-sets one at a time, done ones as chips, End myo-reps")
    func myoReps() throws {
        var session = try RowsSession.dayOne()
        try session.perform(.touch(try session.row("lateral-raise", 0)), at: 10)
        try session.tick("lateral-raise", 0, kgReps(6, 14, rpe: 7), at: 60)
        try session.tick("lateral-raise", 1, kgReps(6, 17, rpe: 9), at: 200)
        let pausing = try session.layout(at: 205)
        let block = try #require(pausing.block("lateral-raise"))
        #expect(block.subtitle == "12–15 reps at RPE 8, rest 1:30, set 2 as myo-reps")
        #expect(block.shape == ["Set 1: done", "Set 2 as myo-reps, 0:15 between mini-sets", "Activation: done", "Mini-set 1: now"])
        let mini = block.lines[2]
        #expect(mini.role == .miniSet && mini.isChained)
        let editor = try #require(mini.editor)
        #expect(editor.fields.map(\.field) == [.reps] && editor.note == RowsNote(text: "after the pause", kind: .status))
        #expect(editor.primary.title == "Log mini-set 1, 4 reps")
        #expect(editor.secondary == RowsButton(title: "End myo-reps here", action: .removeSet(mini.id)))
        guard case let .resting(pause) = pausing.capsule else { Issue.record("no pause"); return }
        #expect(pause.kind == .pause && pause.title == "Pause, then mini-set 1" && pause.totalText == "of 0:15")
        // The next mini-set appears only on a tick; the capsule names it all the same.
        #expect(pause.afterLabel == "After mini-set 1" && pause.afterText == "Mini-set 2")

        try session.tick("lateral-raise", 2, SetValues(load: .kg(6), reps: 4), at: 230)
        try session.tick("lateral-raise", 3, SetValues(load: .kg(6), reps: 4), at: 260)
        let layout = try session.layout(at: 290)
        let chain = try #require(layout.block("lateral-raise"))
        #expect(chain.shape == ["Set 1: done", "Set 2 as myo-reps, 0:15 between mini-sets", "Activation: done",
                                "Mini-sets 4 4", "Mini-set 3: now"])
        guard case let .miniSets(line) = chain.items[3] else { Issue.record("no chips"); return }
        #expect(line.comparison == RowsChange(text: "as last time", isGain: false) && line.stopText == nil)
        #expect(line.sets.map(\.id) == line.chips.map(\.row) && line.sets.allSatisfy { $0.tap == .fix })
        #expect(line.sentence == "Mini-sets done: 4 and 4 reps. The same as last time.")
        let third = try #require(chain.lines.last?.editor)
        #expect(third.note == RowsNote(text: "at 6 kg", kind: .status) && third.primary.title == "Log mini-set 3, 4 reps")
        #expect(layout.capsule == .upNext(RowsNowCapsule(row: third.row, tone: .restDone, title: "Mini-set 3 is up",
                                                          detail: "Pause 0:15 done", since: session.at(260),
                                                          sinceLabel: "since mini-set 2")))

        // End myo-reps: the pending mini-set goes, the chain's chips stay.
        try session.perform(try #require(third.secondary?.action), at: 300)
        let ended = try #require(try session.layout(at: 301).block("lateral-raise"))
        #expect(ended.shape.suffix(2) == ["Activation: done", "Mini-sets 4 4"] && ended.isFinished)
        #expect(ended.summary?.text == "6 kg × 14, 17 + 4 + 4")
    }

    @Test("A finished superset folds to one line with each member's sets and the headline change")
    func finishedSuperset() throws {
        var session = try RowsSession.fullyBody()
        try session.tick("glute-bridge", 0, SetValues(reps: 16), at: 60)
        try session.tick("bird-dog", 0, SetValues(reps: 11), at: 120)
        try session.tick("glute-bridge", 1, SetValues(reps: 16), at: 240)
        try session.tick("bird-dog", 1, SetValues(reps: 11), at: 300)
        let block = try #require(try session.layout(at: 310).block("group-a"))
        #expect(block.isFinished)
        #expect(block.summary == RowsBlockSummary(
            text: "Glute Bridge 16, 16; Bird Dog 11, 11 per side", change: RowsChange(text: "+1 rep", isGain: true),
            sentence: "Superset A, done: Glute Bridge: 16 reps; 16 reps; Bird Dog: 11 reps per side; 11 reps per side. "
                + "1 rep more than last time."
        ))
        #expect(block.items.compactMap { if case let .round(round) = $0 { round.isDone } else { nil } } == [true, true])
    }

    @Test("A session with no exercise asks for one; every set done offers Finish")
    func emptyAndDone() throws {
        var empty = EngineScript(at: engineEpoch)
        try empty.start(nil, exercises: [])
        let none = SessionPresenter.present(try empty.current, history: HistoryIndex([]),
                                            settings: PresenterSettings(prefillsRPE: true), now: engineEpoch)
        #expect(none.rowsLayout == RowsLayout(blocks: [], capsule: .empty, openRow: nil))

        let routine = EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(1))])
        var one = EngineScript(at: engineEpoch)
        try one.start(routine, exercises: [EngineCatalog.press])
        try one.tick(try one.row("press", 0), kgReps(50, 10, rpe: 8))
        let done = SessionPresenter.present(try one.current, history: HistoryIndex([]),
                                            settings: PresenterSettings(prefillsRPE: true), now: engineEpoch.addingTimeInterval(5))
        #expect(done.rowsLayout.capsule == .allDone(title: "All sets done", detail: "Finish saves the session"))
        #expect(done.rowsLayout.openRow == nil)
        let block = try #require(done.rowsLayout.blocks.first)
        #expect(block.isFinished && block.summary?.text == "50 kg × 10")
        #expect(block.summary?.change == RowsChange(text: "first time", isGain: false))
        #expect(block.lines.first?.changes == [RowsChange(text: "first time", isGain: false)] && block.lines.first?.verdict == nil)
    }
}

@Suite("Rows layout: words")
struct RowsWordsTests {
    @Test("Block subtitles: a range and its effort, a hold, reps alone, an effort alone, and the set rest")
    func subtitles() throws {
        let layout = try RowsSession.fullyBody().layout(at: 0)
        #expect(layout.block("plank")?.subtitle == "0:45–1:30 holds, rest 0:15")
        #expect(layout.block("crunch")?.subtitle == "12–20 reps, no rest")
        #expect(layout.block("push-up")?.subtitle == "Each set to RPE 9, rest 1:30")
        // The push-up's reps are a hint (rirstop): shown `≈15`, read "about", never logged.
        let push = try #require(layout.block("push-up")?.lines.first)
        #expect(push.values == "≈15 reps" && push.last == "last 14 @ 9")
        #expect(push.sentence == "Set 1, coming up: about 15 reps. Last time 14 reps.")
        // One side at a time: the second side waits for the first.
        let split = try #require(layout.block("bulgarian-split-squat"))
        #expect(split.lines.prefix(2).map(\.title) == ["Set 1 left", "Set 1 right"])
        #expect(split.lines[1].state == .locked && split.lines[1].tap == .none)
        #expect(split.lines[1].sentence == "Set 1 right, after Set 1 left: 10 kilograms, 10 reps.")
    }

    @Test("Changes against last time: units, a true minus, gains and losses, and the spoken form")
    func changes() {
        let kg = RowsText.changes(SetValues(load: .kg(110), reps: 8), last: SetValues(load: .kg(105), reps: 10),
                                  measurement: .weightreps, unit: .kg)
        #expect(kg == [RowsChange(text: "+5 kg", isGain: true), RowsChange(text: "\u{2212}2 reps", isGain: false)])
        #expect(RowsText.spokenChanges(kg, unit: .kg) == "5 kilograms more and 2 reps fewer than last time")
        let assisted = RowsText.changes(SetValues(assistance: .kg(20), reps: 8), last: SetValues(assistance: .kg(25), reps: 8),
                                        measurement: .bodyweightassistedreps, unit: .kg)
        #expect(assisted == [RowsChange(text: "\u{2212}5 kg", isGain: true)])
        let hold = RowsText.changes(SetValues(duration: 67), last: SetValues(duration: 62), measurement: .duration, unit: .kg)
        #expect(hold == [RowsChange(text: "+0:05", isGain: true)])
        #expect(RowsText.spokenChanges(hold, unit: .kg) == "5 seconds longer than last time")
        let row = RowsText.changes(SetValues(duration: 120, distance: Length(metres: 500)),
                                   last: SetValues(duration: 125, distance: Length(metres: 500)),
                                   measurement: .distanceduration, unit: .kg)
        #expect(row == [RowsChange(text: "\u{2212}0:05", isGain: true)])
        #expect(RowsText.changes(SetValues(reps: 15), last: SetValues(reps: 15), measurement: .repsonly, unit: .kg)
            == [RowsChange(text: "as last time", isGain: false)])
    }

    @Test("Values as rows say them: units on loads, none on last time, reps and holds in words")
    func values() {
        #expect(RowsText.values(SetValues(load: .kg(110), reps: 8), measurement: .weightreps, perSide: false) == "110 kg × 8")
        #expect(RowsText.values(SetValues(reps: 1), measurement: .repsonly, perSide: false) == "1 rep")
        #expect(RowsText.values(SetValues(reps: 11), measurement: .repsonly, perSide: true) == "11 per side")
        #expect(RowsText.values(SetValues(load: .kg(10), reps: 8), measurement: .weightedbodyweightreps, perSide: false)
            == "BW + 10 kg × 8")
        #expect(RowsText.values(SetValues(reps: 8), measurement: .weightedbodyweightreps, perSide: false) == "8 reps")
        #expect(RowsText.values(SetValues(duration: 58), measurement: .duration, perSide: false) == "0:58 hold")
        #expect(RowsText.values(SetValues(load: .kg(25)), measurement: .weightreps, perSide: false, placeholder: "to failure")
            == "25 kg to failure")
        #expect(RowsText.compact(SetValues(load: .kg(35), reps: 13, rpe: d("9.5"), rpeMethod: .entered),
                                 measurement: .weightreps, perSide: false) == "35 × 13 @ 9.5")
        #expect(RowsText.compact(SetValues(load: .kg(10), duration: 55), measurement: .weightduration, perSide: false)
            == "10 for 0:55")
        #expect(RowsText.logTitle(prefix: "Log A2,", values: "", rpe: nil) == "Log A2")
        #expect(RowsText.spokenDuration(62) == "1 minute 2 seconds" && RowsText.spokenDuration(120) == "2 minutes")
    }

    @Test("The track: a little past the goal and last time, on a 5 s step")
    func track() {
        #expect(RowsTimer.trackEnd(goal: 67, last: 62) == 80)
        #expect(RowsTimer.trackEnd(goal: nil, last: nil) == 35)
        let stopwatch = RowsTimer(phase: .ready, goal: nil, last: nil, trackEnd: 35, goalText: nil, lastText: nil)
        #expect(stopwatch.caption(elapsed: 10) == nil)
    }
}
