#if os(macOS)
import AppKit
import SwiftUI
import Testing

@testable import SubstrateAPI
@testable import SubstrateCore
@testable import SubstrateUI

/// A record's pill is one thing wherever it stands: what a screen reader is
/// told of it, how it keeps to its row, and that the form inside a line of
/// text leaves the line's height alone.
@MainActor @Suite("A record's pill", .timeLimit(.minutes(10)))
struct RecordPillTests {
    let task = RecordReference(kind: "ada.example.com/tasks/task", id: "q4")

    /// The widths a layout gave its children, by name.
    final class Widths {
        var seen: [String: CGFloat] = [:]
    }

    @Test func thePillSaysItsKindThenItsName() {
        #expect(RecordPillStyle.label(task, name: "Q4 budget") == "Task: Q4 budget")
        #expect(RecordPillStyle.label(RecordReference(kind: "substrate.reamde.dev/core/contact", id: "x"), name: "Ada") == "Contact: Ada")
    }

    /// A name longer than its row is cut inside the row: the wrap gives a
    /// child that asks for more than the row the row's width, and no more.
    @Test func aPillTooLongForItsRowKeepsToTheRow() {
        let long = String(repeating: "A very long name of a record ", count: 8)
        let widths = Widths()
        func measured(_ name: String, _ pill: RecordPill) -> some View {
            pill.background(
                GeometryReader { geometry in
                    let _ = widths.seen[name] = geometry.size.width
                    Color.clear
                })
        }
        _ = NSApplication.shared
        let hosting = NSHostingView(
            rootView: WrapLayout(spacing: 6) {
                measured("short", RecordPill(task, title: "Short"))
                measured("long", RecordPill(task, title: long))
            }
            .frame(width: 200, alignment: .leading)
            .fixedSize(horizontal: false, vertical: true)
            .frame(width: 320, height: 200, alignment: .topLeading))
        hosting.frame = CGRect(x: 0, y: 0, width: 320, height: 200)
        let window = NSWindow(contentRect: hosting.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = hosting
        hosting.layoutSubtreeIfNeeded()
        defer { window.contentView = nil }
        let (short, cut) = (widths.seen["short"] ?? 0, widths.seen["long"] ?? 0)
        // One that fits is as wide as it asks to be; one that does not is
        // the row's width.
        #expect(short > 0 && short < 120, "short \(short)")
        #expect(cut > 150 && cut <= 200.5, "long \(cut)")
    }

    /// Inside a line of text the pill is drawn behind the words, so a line
    /// with one is as tall as a line without, to within two points.
    @Test func aLineWithAPillIsAsTallAsALineWithout() {
        func height(_ text: Text) -> CGFloat {
            let hosting = NSHostingView(rootView: text.font(.body).fixedSize().recordPillText())
            return hosting.fittingSize.height
        }
        let pill = RecordPillText.text(task, name: AttributedString("Q4 budget"), link: URL(string: "substrate://\(task.path)"))
        let plain = height(Text(verbatim: "The Q4 budget is due."))
        let with = height(Text("The \(pill) is due."))
        #expect(plain > 0)
        #expect(abs(with - plain) <= 2, "plain \(plain), with a pill \(with)")
    }

    /// The pill's capsule and its tile are drawn: the pixels behind a pill
    /// in a line are not the page's.
    @Test func thePillInALineIsDrawnBehindItsWords() throws {
        let pill = RecordPillText.text(task, name: AttributedString("Q4 budget"), link: nil)
        func ink(_ text: Text) throws -> Int {
            let view = text.font(.body).padding(10).frame(width: 240, height: 44).background(Color.white).recordPillText().environment(\.colorScheme, .light)
            let hosting = NSHostingView(rootView: view)
            hosting.frame = CGRect(x: 0, y: 0, width: 240, height: 44)
            let window = NSWindow(contentRect: hosting.frame, styleMask: [.borderless], backing: .buffered, defer: false)
            window.contentView = hosting
            window.appearance = NSAppearance(named: .aqua)
            window.isReleasedWhenClosed = false
            hosting.layoutSubtreeIfNeeded()
            defer { window.contentView = nil }
            let rep = try #require(hosting.bitmapImageRepForCachingDisplay(in: hosting.bounds))
            hosting.cacheDisplay(in: hosting.bounds, to: rep)
            var dark = 0
            for y in 0 ..< rep.pixelsHigh {
                for x in 0 ..< rep.pixelsWide {
                    if let c = rep.colorAt(x: x, y: y)?.usingColorSpace(.sRGB), c.redComponent < 0.97 { dark += 1 }
                }
            }
            return dark
        }
        // The same words with a pill cover more of the page than without:
        // the capsule's fill and the tile are there.
        let without = try ink(Text(verbatim: "Q4 budget"))
        let with = try ink(Text("\(pill)"))
        #expect(with > without * 2, "without \(without), with \(with)")
    }
}
#endif
