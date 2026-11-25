#if canImport(SwiftUI)
import SubstrateChatCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI

extension MeterPoint {
    /// A code block's text made into coloured text, on the main thread.
    static let paint = MeterPoint("paint")
}

/// A fenced block: its language, the switch that wraps its lines where one
/// overflows, Copy, and the code in the colours its row carries. A long
/// line scrolls sideways unless its reader chose to wrap, a choice kept on
/// the device.
struct CodeBlockView: View {
    @Environment(\.codeAdvance) private var advance
    @Environment(\.codeWrap) private var wrap
    @Environment(\.spill) private var spill
    let language: String?
    let text: String
    let code: CodeHighlight?
    /// The width of the block's place among the reply's blocks.
    @State private var place: CGFloat = 0
    @State private var source = CodeSource()

    var body: some View {
        // Its longest line's width, with the block's insets: what it
        // would be drawn at with all the room there is.
        let wanted = CGFloat(code?.columns ?? 0) * advance + 2 * ChatSpace.blockInset
        let reach = reach(wanting: wanted, in: place, spill: spill)
        let room = max(0, place + 2 * reach - 2 * ChatSpace.blockInset)
        let layout = CodeWrap.layout(columns: code?.columns ?? 0, advance: advance, room: room, prefersWrap: wrap.on)
        // Copy reads the code when it is pressed.
        let _ = source.text = text
        VStack(alignment: .leading, spacing: 0) {
            CodeBar(language: language, offers: layout.offers, wrap: wrap, source: source).equatable()
            Rectangle().fill(ChatColours.codeEdge).frame(height: ChatLine.hairline)
            if layout.wraps {
                lines.frame(maxWidth: .infinity, alignment: .leading)
            } else {
                ScrollView(.horizontal, showsIndicators: false) { lines }
            }
        }
        .background(ChatColours.codeStyle, in: .rect(cornerRadius: ChatRadius.block))
        .overlay { RoundedRectangle(cornerRadius: ChatRadius.block).strokeBorder(ChatColours.codeEdge, lineWidth: ChatLine.hairline) }
        // A long line has the room the window has past the measure
        // before it scrolls or wraps.
        .reaching(reach) { width in
            if abs(width - place) > 0.5 { place = width }
        }
    }

    private var lines: some View {
        CodeText(text: text, code: code)
            .equatable()
            .textSelection(.enabled)
            .padding(ChatSpace.blockInset)
    }
}

/// A block's code as it stands, for Copy to read when it is pressed.
@MainActor
final class CodeSource {
    var text = ""
}

/// The bar above a block's code: its language, the switch that wraps its
/// lines where one overflows, and Copy. Drawn again only when one of those
/// changes: a block being written has other code at every flush, which
/// nothing here shows.
private struct CodeBar: View, Equatable {
    let language: String?
    /// A line overflows: the switch is offered.
    let offers: Bool
    let wrap: CodeWrapChoice
    let source: CodeSource
    /// The bar keeps its height at the text's size: its words, and room
    /// above and below them.
    @Scaled(.codeBar) private var height

    nonisolated static func == (lhs: CodeBar, rhs: CodeBar) -> Bool {
        lhs.language == rhs.language && lhs.offers == rhs.offers && lhs.wrap == rhs.wrap && lhs.source === rhs.source
    }

    var body: some View {
        HStack(spacing: 0) {
            Text(language ?? "code")
                .font(ChatType.label)
                .ink(.quiet, on: .code)
            Spacer(minLength: ChatSpace.beside)
            if offers {
                Button {
                    if let set = wrap.set { set(!wrap.on) }
                } label: {
                    Image(systemName: "text.word.spacing")
                        .symbolVariant(wrap.on ? .fill : .none)
                        .ink(wrap.on ? .accent : .quiet, on: .code)
                        .barTarget()
                }
                .buttonStyle(.borderless)
                .accessibilityLabel("Wrap Lines")
                .accessibilityValue(wrap.on ? "On" : "Off")
                .accessibilityAddTraits(wrap.on ? .isSelected : [])
                .help(wrap.on ? "Scroll long lines" : "Wrap long lines")
            }
            CopyButton(label: "Copy Code") { source.text }
        }
        .font(ChatType.caption)
        .padding(.leading, ChatSpace.blockInset)
        // The bar keeps its height; a control's room to touch reaches
        // past it. Its words and marks grow as far as the bar does.
        .growing(to: .codeBar)
        .frame(height: height)
    }
}

extension View {
    /// A small control's label: its room to touch is a whole target,
    /// whatever its mark's size and however low the bar it sits in.
    func barTarget() -> some View {
        frame(minWidth: ChatSize.target, minHeight: ChatSize.target)
            .contentShape(.rect)
    }
}

/// The code as one text, each run in its token's colour. Drawn again only
/// when its text or its runs change, and then made from where it was last
/// made to: a block being written has one more line coloured, not all of
/// them again.
struct CodeText: View, Equatable {
    let text: String
    let code: CodeHighlight?
    @State private var made = CodePaint()

    nonisolated static func == (lhs: CodeText, rhs: CodeText) -> Bool { lhs.text == rhs.text && lhs.code == rhs.code }

    var body: some View {
        let _ = Meter.shared.count(.body, "Code")
        // Code with no colours (a language the highlighter does not read,
        // or none) is its text and nothing more to make.
        if let runs = code?.runs, !runs.isEmpty {
            Text(Meter.shared.time(.paint) { made.text(text, runs: runs) })
                .font(ChatType.code)
        } else {
            Text(verbatim: text)
                .font(ChatType.code)
        }
    }
}

/// Code as coloured text, kept as far as its whole lines go: the next text
/// that starts with those lines, coloured alike, has only what follows
/// them made. Making a long block's text costs about 15 microseconds a
/// line, which a flush cannot pay for every line of an open block.
@MainActor
final class CodePaint {
    /// The lines made so far, as text; the bytes of the code they are,
    /// and how many of its runs lie in them.
    private var head = AttributedString()
    private var bytes: [UInt8] = []
    private var runs: [CodeRun] = []

    func text(_ text: String, runs: [CodeRun]) -> AttributedString {
        let utf8 = Array(text.utf8)
        if !(utf8.count >= bytes.count && utf8.prefix(bytes.count).elementsEqual(bytes) && runs.prefix(self.runs.count).elementsEqual(self.runs)) {
            (head, bytes, self.runs) = (AttributedString(), [], [])
        }
        // Its whole lines join what is kept; the line still open is made
        // each time.
        let whole = utf8.lastIndex(of: 0x0A).map { $0 + 1 } ?? 0
        if whole > bytes.count {
            let settled = runs.prefix { $0.start + $0.length <= whole }
            head.append(Self.paint(utf8[bytes.count ..< whole], runs: settled.dropFirst(self.runs.count), from: bytes.count))
            bytes = Array(utf8[..<whole])
            self.runs = Array(settled)
        }
        guard whole < utf8.count else { return head }
        var out = head
        out.append(Self.paint(utf8[whole...], runs: runs.dropFirst(self.runs.count), from: whole))
        return out
    }

    /// Each token's colour and the two fonts, made once: a colour made at
    /// each run would be another colour to everything that compares them.
    static let colours = Dictionary(uniqueKeysWithValues: CodeToken.allCases.map { ($0, ChatColours.code($0).color) })

    /// Some of a block's bytes as text, with the runs that lie in them:
    /// `from` is where the bytes start in the block.
    static func paint(_ bytes: ArraySlice<UInt8>, runs: ArraySlice<CodeRun>, from: Int) -> AttributedString {
        var out = AttributedString(String(decoding: bytes, as: UTF8.self))
        var at = out.utf8.startIndex
        var offset = from
        for run in runs where run.start >= offset && run.start + run.length <= from + bytes.count {
            let start = out.utf8.index(at, offsetBy: run.start - offset)
            let end = out.utf8.index(start, offsetBy: run.length)
            out[start ..< end].foregroundColor = colours[run.token]
            // Not by its colour alone: a keyword is heavier, a comment
            // slanted.
            if run.token == .keyword { out[start ..< end].font = ChatType.codeKeyword }
            if run.token == .comment { out[start ..< end].font = ChatType.codeComment }
            at = end
            offset = run.start + run.length
        }
        return out
    }
}

/// Copy, which says it has copied: its mark and its label change for a
/// moment, VoiceOver is told, and the device taps as it does for a send.
struct CopyButton: View {
    /// What VoiceOver reads for it: "Copy", "Copy Code".
    let label: String
    var copied = "Copied"
    /// The text, read when it is pressed: a reply being written has more
    /// by then than when this was drawn.
    let text: () -> String
    @State private var done = false
    @State private var presses = 0

    var body: some View {
        Button {
            copyToPasteboard(text())
            presses += 1
            done = true
            AccessibilityNotification.Announcement(copied).post()
            let press = presses
            Task {
                try? await Task.sleep(for: ChatMotion.confirms)
                if press == presses { done = false }
            }
        } label: {
            Image(systemName: done ? "checkmark" : "doc.on.doc")
                .changes(.mark)
                .ink(.quiet, on: .page, .code)
                .growing(to: .actionMark)
                .barTarget()
        }
        .buttonStyle(.borderless)
        .sensoryFeedback(.impact(weight: .light), trigger: presses)
        .accessibilityLabel(done ? copied : label)
        .help(done ? copied : label)
    }
}

/// The choice to wrap code, and how it is changed: the person's, kept on
/// the device with their other choices. A value that compares, never a
/// closure, so setting it above a transcript draws nothing under it again.
struct CodeWrapChoice: Equatable {
    var on = false
    var set: CodeWrapAction?
}

struct CodeWrapAction: Equatable {
    let host: ChatHost

    static func == (lhs: CodeWrapAction, rhs: CodeWrapAction) -> Bool { lhs.host === rhs.host }

    @MainActor func callAsFunction(_ on: Bool) { host.send(.settings(.toggle(row: .app("wrapCode"), on: on))) }
}

extension EnvironmentValues {
    /// One column's width in the code's font, measured once above the
    /// transcript (`replyTools`); zero where it was not.
    @Entry var codeAdvance: CGFloat = 0
    @Entry var codeWrap = CodeWrapChoice()
}
#endif
