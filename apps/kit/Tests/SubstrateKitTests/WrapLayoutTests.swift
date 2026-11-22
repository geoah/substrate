#if os(macOS)
import AppKit
import SwiftUI
import Testing

@testable import SubstrateUI

/// The wrap layout says how tall it is from the rows it measured, so the
/// rows it then places must be those rows: a child that moves to a row the
/// height did not count is drawn over whatever stands below.
@MainActor @Suite("The wrap layout", .timeLimit(.minutes(10)))
struct WrapLayoutTests {
    /// Where a layout put its children, by name, in the sheet's space.
    final class Frames {
        var seen: [String: CGRect] = [:]
    }

    /// Two children and what stands below them, laid out in a window: the
    /// children as wide as asked, the whole pushed in from the leading edge.
    func frames(widths: [CGFloat], within: CGFloat = 320, inset: CGFloat = 0) -> Frames {
        let frames = Frames()
        func measured(_ name: String, width: CGFloat) -> some View {
            Color.clear.frame(width: width, height: 20).background(
                GeometryReader { geometry in
                    let _ = frames.seen[name] = geometry.frame(in: .named("sheet"))
                    Color.clear
                })
        }
        _ = NSApplication.shared
        let hosting = NSHostingView(
            rootView: VStack(alignment: .leading, spacing: 8) {
                WrapLayout(spacing: 6) {
                    ForEach(Array(widths.enumerated()), id: \.offset) { index, width in measured("child\(index)", width: width) }
                }
                measured("below", width: 50)
            }
            .padding(.leading, inset)
            .frame(width: within, height: 200, alignment: .topLeading)
            .coordinateSpace(name: "sheet"))
        hosting.frame = CGRect(x: 0, y: 0, width: within, height: 200)
        let window = NSWindow(contentRect: hosting.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = hosting
        hosting.layoutSubtreeIfNeeded()
        window.contentView = nil
        return frames
    }

    /// The layout hugs its widest row, and the parent rounds the bounds it
    /// hands back to the screen's pixels: a row that was measured to fit
    /// can then be a hair wider than the bounds it is placed in. It stays
    /// one row, whatever fraction of a point its children end on.
    @Test func aRowThatFitsStaysOneRowWhereItIsPlaced() {
        for tenth in 0 ..< 10 {
            for inset in [0, 0.25, 0.3, 0.5, 0.7] as [CGFloat] {
                let width = 100 + CGFloat(tenth) / 10
                let seen = frames(widths: [width, width], inset: inset).seen
                guard let first = seen["child0"], let second = seen["child1"], let below = seen["below"] else {
                    Issue.record("nothing was laid out for \(width) at \(inset)")
                    continue
                }
                #expect(abs(first.minY - second.minY) < 0.5, "children \(width) wide, pushed in \(inset): the second is on a row of its own")
                #expect(below.minY >= max(first.maxY, second.maxY) - 0.5, "children \(width) wide, pushed in \(inset): drawn over what is below")
            }
        }
    }

    /// Children that do not fit one row take two, and what is below stands
    /// under the second.
    @Test func childrenThatDoNotFitWrapAndPushWhatIsBelowDown() throws {
        let seen = frames(widths: [150.3, 150.3, 90.6], within: 320).seen
        let first = try #require(seen["child0"])
        let second = try #require(seen["child1"])
        let third = try #require(seen["child2"])
        let below = try #require(seen["below"])
        #expect(abs(first.minY - second.minY) < 0.5 && third.minY >= first.maxY)
        #expect(below.minY >= third.maxY - 0.5)
    }
}
#endif
