#if os(macOS)
import Testing
import TodayCore

@testable import SubstrateTodayUI

/// Where each thing of the day is drawn. Every item has one place and
/// keeps it, closed, opened or as the current item, so a change is the same
/// thing changing and can be seen to happen; a list of closed rows is only
/// how rows that follow each other look.
@MainActor
@Suite struct PaneSlotTests {
    static let h = HandScenes.self

    static func row(_ id: String, _ state: Row.State = .next) -> PaneItem {
        .row(h.taskRow(id, "A task", "", flag: nil, state: state))
    }

    static func meeting(_ id: String) -> PaneItem {
        .meeting(h.meeting(id, "14:20", "14:40", "Tomas", people: ["TV"], line: nil))
    }

    @Test func everyItemHasOnePlaceWhateverItIsDrawnAs() {
        let items = [Self.row("a", .now), Self.row("b"), Self.row("c"), Self.row("d"), Self.meeting("m1"), Self.meeting("m2")]
        let closed = PaneSlot.slots(of: items, open: [])
        let opened = PaneSlot.slots(of: items, open: ["row:" + Self.id("c"), "meeting:m1"])
        // The same places in the same order, whatever is open.
        #expect(closed.map(\.id) == items.map(\.id) && opened.map(\.id) == items.map(\.id))
        // The current item stands alone; the closed rows after it read as one list, and the meetings as another.
        #expect(
            closed.map(\.joins) == [
                Joins(), Joins(below: true), Joins(above: true, below: true), Joins(above: true), Joins(below: true), Joins(above: true),
            ])
        // A row opened in the middle of a list stands alone, and the list is two around it; a meeting likewise.
        #expect(opened.map(\.joins) == [Joins(), Joins(), Joins(), Joins(), Joins(), Joins()])
    }

    /// The row's id as the day names it.
    static func id(_ task: String) -> String {
        guard case let .row(row) = row(task) else { return task }
        return row.id
    }

    @Test func whenTheCurrentItemIsFinishedTheNextKeepsItsPlaceAndStandsAlone() {
        let before = PaneSlot.slots(of: [Self.row("a", .now), Self.row("b"), Self.row("c")], open: [])
        let after = PaneSlot.slots(of: [Self.row("b", .now), Self.row("c")], open: [])
        // The row that becomes the current item is the same place: it is seen to grow into it.
        #expect(before[1].id == after[0].id)
        #expect(before[1].joins == Joins(below: true) && after[0].joins == Joins() && after[1].joins == Joins())
    }

    @Test func whatIsDoneKeepsOnePlaceForEachRowMeetingAndLine() {
        let line = { (id: String) in EarlierPresentation.Entry.line(EarlierPresentation.Line(id: id, time: "09:10", text: "Read")) }
        guard case let .row(a) = Self.row("a", .done), case let .row(b) = Self.row("b", .done), case let .meeting(m) = Self.meeting("m") else { return }
        let entries: [EarlierPresentation.Entry] = [.row(a), .row(b), line("l1"), line("l2"), .meeting(m)]
        let slots = EarlierSlot.slots(of: entries, open: [])
        #expect(slots.map(\.id) == ["done:row:" + a.id, "done:row:" + b.id, "line:l1", "line:l2", "done:meeting:m"])
        #expect(slots.map(\.joins) == [Joins(below: true), Joins(above: true), Joins(below: true), Joins(above: true), Joins()])
        // A done row opened in place stands alone, and keeps its place.
        let opened = EarlierSlot.slots(of: entries, open: ["row:" + a.id])
        #expect(opened.map(\.id) == slots.map(\.id) && opened[0].joins == Joins() && opened[1].joins == Joins())
    }
}
#endif
