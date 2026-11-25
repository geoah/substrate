import Foundation
import Testing

@testable import SubstrateChatCore

/// How a table is laid out in the room it has, and when a code block
/// offers to wrap: decisions made of numbers, so they are held here and the
/// views only measure.
@Suite("A table's layout and a code block's wrap", .timeLimit(.minutes(10)))
struct TableLayoutTests {
    let metrics = TableLayout.Metrics()

    @Test func aTableThatFitsIsDrawnAtItsNaturalWidths() {
        let plan = TableLayout.plan(natural: [120, 60, 50], room: 353, accessibilitySize: false)
        #expect(plan == TableLayout.Plan(mode: .fits, widths: [120, 60, 50]))
        // To the point.
        #expect(TableLayout.plan(natural: [200, 153], room: 353, accessibilitySize: false).mode == .fits)
        // Before its room is measured it has all it asks for.
        #expect(TableLayout.plan(natural: [900, 900], room: 0, accessibilitySize: false) == TableLayout.Plan(mode: .fits, widths: [900, 900]))
        #expect(TableLayout.plan(natural: [], room: 353, accessibilitySize: false) == TableLayout.Plan(mode: .fits, widths: []))
    }

    @Test func aWideColumnIsWrappedWhereThatMakesTheTableFit() {
        // A name and a long description: the description wraps, and the
        // name keeps its width.
        let plan = TableLayout.plan(natural: [90, 700], room: 353, accessibilitySize: false)
        #expect(plan == TableLayout.Plan(mode: .fits, widths: [90, 263]))
        // Two wide ones share what the narrow one leaves.
        let two = TableLayout.plan(natural: [400, 50, 500], room: 350, accessibilitySize: false)
        #expect(two == TableLayout.Plan(mode: .fits, widths: [150, 50, 150]))
        // The room is used whole, and no column is wrapped below the least
        // or made wider than it asks.
        for natural in [[90.0, 700], [400, 50, 500], [120, 130, 140, 150], [88, 88, 88, 500]] {
            let room = 353.0
            let made = TableLayout.plan(natural: natural, room: room, accessibilitySize: false)
            guard made.mode == .fits else { continue }
            #expect(abs(made.widths.reduce(0, +) - min(room, natural.reduce(0, +))) < 0.001, "\(natural)")
            for (width, asked) in zip(made.widths, natural) { #expect(width <= asked && width >= min(asked, metrics.least), "\(natural)") }
        }
    }

    @Test func aTableWithMoreColumnsThanFitScrollsWithItsFirstColumnHeld() {
        // Six columns on a phone: none could be read at a sixth of it.
        let natural = [150.0, 120, 90, 400, 110, 95]
        let plan = TableLayout.plan(natural: natural, room: 353, accessibilitySize: false)
        #expect(plan.mode == .scrolls)
        // Each at its natural width, a long cell wrapped at the widest; the
        // held column takes no more than its share of the room.
        #expect(plan.widths == [353 * metrics.heldShare, 120, 90, metrics.widest, 110, 95])
        // A first column narrower than its share keeps its width, and one
        // is never held narrower than the least.
        #expect(TableLayout.plan(natural: [70, 200, 200, 200, 200], room: 353, accessibilitySize: false).widths.first == 70)
        #expect(TableLayout.plan(natural: [300, 200, 200, 200], room: 180, accessibilitySize: false).widths.first == metrics.least)
    }

    @Test func atTheAccessibilitySizesEveryRowIsACard() {
        #expect(TableLayout.plan(natural: [50, 50], room: 353, accessibilitySize: true) == TableLayout.Plan(mode: .cards, widths: []))
        #expect(TableLayout.plan(natural: [900, 900, 900], room: 353, accessibilitySize: true).mode == .cards)
    }

    @Test func aTableBeingWrittenMovesAColumnOnlyWhenItMust() {
        // Rows arrive: a column's natural width only ever grows. While the
        // table fits, no other column moves for it.
        var natural = [80.0, 60, 40]
        var before = TableLayout.plan(natural: natural, room: 353, accessibilitySize: false)
        for (column, width) in [(1, 90.0), (2, 70), (0, 120), (1, 100)] {
            natural[column] = width
            let now = TableLayout.plan(natural: natural, room: 353, accessibilitySize: false)
            #expect(now.mode == .fits)
            for index in natural.indices where index != column { #expect(now.widths[index] == before.widths[index]) }
            #expect(now.widths[column] >= before.widths[column])
            before = now
        }
        // Where it scrolls, the same: each column is its own.
        natural = [150, 120, 90, 200, 110, 95]
        before = TableLayout.plan(natural: natural, room: 353, accessibilitySize: false)
        natural[3] = 900
        let now = TableLayout.plan(natural: natural, room: 353, accessibilitySize: false)
        #expect(now.mode == .scrolls && now.widths[3] == metrics.widest)
        for index in natural.indices where index != 3 { #expect(now.widths[index] == before.widths[index]) }
    }

    // MARK: Code

    @Test func theWrapSwitchIsOfferedOnlyWhereALineOverflows() {
        // 40 columns of 9.6 points in 353: it fits, and nothing is offered.
        #expect(!CodeWrap.overflows(columns: 36, advance: 9.6, room: 353))
        #expect(CodeWrap.overflows(columns: 37, advance: 9.6, room: 353))
        // To the point, and half of one more.
        #expect(!CodeWrap.overflows(columns: 10, advance: 10, room: 100))
        #expect(!CodeWrap.overflows(columns: 10, advance: 10.04, room: 100))
        #expect(CodeWrap.overflows(columns: 10, advance: 10.1, room: 100))
        // Nothing overflows a room that has not been measured.
        #expect(!CodeWrap.overflows(columns: 400, advance: 9.6, room: 0))
        #expect(!CodeWrap.overflows(columns: 400, advance: 0, room: 353))
        #expect(!CodeWrap.overflows(columns: 0, advance: 9.6, room: 353))
    }

    @Test func aLongLineScrollsUnlessItsReaderChoseToWrap() {
        let long = CodeWrap.layout(columns: 120, advance: 9.6, room: 353, prefersWrap: false)
        #expect(long == CodeWrap.Layout(offers: true, wraps: false))
        #expect(CodeWrap.layout(columns: 120, advance: 9.6, room: 353, prefersWrap: true) == CodeWrap.Layout(offers: true, wraps: true))
        // A block with no long line shows no switch, whatever was chosen.
        #expect(CodeWrap.layout(columns: 20, advance: 9.6, room: 353, prefersWrap: true).offers == false)
        #expect(CodeWrap.layout(columns: 20, advance: 9.6, room: 353, prefersWrap: false) == CodeWrap.Layout(offers: false, wraps: false))
        // The longest line is the highlighter's count, wide characters in.
        let code = CodeHighlighter.highlight("let short = 1\nlet long = \"" + String(repeating: "x", count: 60) + "\"", language: "swift")
        #expect(code.columns == 73 && CodeWrap.overflows(columns: code.columns, advance: 9.6, room: 353))
    }
}
