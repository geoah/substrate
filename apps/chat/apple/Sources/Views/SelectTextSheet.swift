import ChatKit
import SubstrateKit
import SwiftUI

#if os(macOS)
import AppKit
#else
import UIKit
#endif

/// A reply opened for selecting: its rows as the reply drew them.
struct SelectableReply: Identifiable, Equatable {
    let id: String
    let rows: [MarkdownRow]
    let missing: [String]
}

/// Opens a reply for selecting, in the sheet above the transcript. A value
/// that compares, as `OpenThreadAction` is, so setting it draws nothing
/// under it again.
struct SelectReplyAction: Equatable {
    fileprivate let shown: Shown

    static func == (lhs: SelectReplyAction, rhs: SelectReplyAction) -> Bool { lhs.shown === rhs.shown }

    @MainActor func callAsFunction(_ reply: SelectableReply) { shown.reply = reply }

    /// The reply the sheet shows.
    @MainActor @Observable fileprivate final class Shown {
        var reply: SelectableReply?
    }
}

extension EnvironmentValues {
    @Entry var selectReply: SelectReplyAction?
}

/// What a reply's rows need set once above them, and never where one is
/// drawn: the width of one column of code, which says whether a line
/// overflows; the choice to wrap code and how it is changed; and the sheet
/// a reply's text is selected in. A reply's rows are separate views, so a
/// selection across the whole of it needs a place of its own.
private struct ReplyTools: ViewModifier {
    /// Nil where the rows are drawn with no app around them: the choice to
    /// wrap is then read and not changed.
    @Environment(ChatHost.self) private var host: ChatHost?
    let wraps: Bool
    @State private var advance: CGFloat = 0
    @State private var shown = SelectReplyAction.Shown()

    func body(content: Content) -> some View {
        @Bindable var shown = shown
        content
            .environment(\.codeAdvance, advance)
            .environment(\.codeWrap, CodeWrapChoice(on: wraps, set: host.map(CodeWrapAction.init)))
            .environment(\.selectReply, SelectReplyAction(shown: shown))
            .background {
                // One character of the code's font, never seen: its width
                // is a column's, at whatever size the text is.
                Text(verbatim: "0")
                    .font(ChatType.code)
                    .fixedSize()
                    .hidden()
                    .accessibilityHidden(true)
                    .onGeometryChange(for: CGFloat.self) {
                        $0.size.width
                    } action: { width in
                        if width != advance { advance = width }
                    }
            }
            .sheet(item: $shown.reply) { SelectTextSheet(reply: $0) }
    }
}

extension View {
    /// The rows of every reply drawn under this view measure their code
    /// and open for selecting as a transcript's do.
    func replyTools(wraps: Bool) -> some View { modifier(ReplyTools(wraps: wraps)) }
}

/// A reply's whole text, where words can be selected across its blocks.
struct SelectTextSheet: View {
    @Environment(\.dismiss) private var dismiss
    let reply: SelectableReply

    var body: some View {
        NavigationStack {
            SelectableText(text: ReplyText.attributed(reply.rows, missing: Set(reply.missing)))
                .navigationTitle("Select Text")
                #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
                #endif
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
        }
        #if os(macOS)
        .frame(
            minWidth: ChatSize.readingSheet.least.width, idealWidth: ChatSize.readingSheet.ideal.width,
            minHeight: ChatSize.readingSheet.least.height, idealHeight: ChatSize.readingSheet.ideal.height)
        #endif
    }
}

/// A reply's rows as one text for the system's text view: what the reply
/// reads as, block after block, with the weight of a heading, a list's
/// marks, a table's cells a row to a line, and code in its fixed-width
/// font. It is for selecting and copying, so it holds every word the reply
/// draws and no control.
@MainActor
enum ReplyText {
    #if os(macOS)
    typealias PlatformFont = NSFont
    typealias PlatformColor = NSColor
    private static var body: NSFont { .preferredFont(forTextStyle: .body) }
    private static var label: NSColor { .labelColor }
    private static var quiet: NSColor { NSColor(ChatColours.Ink.quiet.color) }
    private static func fixed(_ size: CGFloat) -> NSFont { .monospacedSystemFont(ofSize: size, weight: .regular) }
    #else
    typealias PlatformFont = UIFont
    typealias PlatformColor = UIColor
    private static var body: UIFont { .preferredFont(forTextStyle: .body) }
    private static var label: UIColor { .label }
    private static var quiet: UIColor { UIColor(ChatColours.Ink.quiet.color) }
    private static func fixed(_ size: CGFloat) -> UIFont { .monospacedSystemFont(ofSize: size, weight: .regular) }
    #endif

    /// Between two blocks, and between two of one list item.
    private nonisolated static let gap = "\n\n"
    private nonisolated static let itemGap = "\n"
    /// Between two columns of a table.
    private static let columnGap = ChatSpace.edge

    static func attributed(_ rows: [MarkdownRow], missing: Set<String> = []) -> NSAttributedString {
        let out = NSMutableAttributedString()
        append(rows.map(\.block), to: out, indent: "", missing: missing)
        return out
    }

    /// The same, as its words alone.
    static func plain(_ rows: [MarkdownRow]) -> String { attributed(rows).string }

    private static func append(
        _ blocks: [MarkdownBlock], to out: NSMutableAttributedString, indent: String, missing: Set<String>, gap: String = gap
    ) {
        for (index, block) in blocks.enumerated() {
            if index > 0 { out.append(NSAttributedString(string: gap)) }
            switch block {
            case let .paragraph(text):
                out.append(line(text, font: body, indent: indent, missing: missing))
            case let .heading(level, text):
                out.append(line(text, font: bold(body, larger: level == 1 ? 1.3 : level == 2 ? 1.15 : 1), indent: indent, missing: missing))
            case let .code(_, text, _):
                let code = text.split(separator: "\n", omittingEmptySubsequences: false).map { indent + $0 }.joined(separator: "\n")
                out.append(NSAttributedString(string: code, attributes: [.font: fixed(body.pointSize * 0.9), .foregroundColor: label]))
            case let .quote(blocks):
                append(blocks, to: out, indent: indent + "  ", missing: missing)
            case let .list(ordered, start, items):
                for (number, item) in items.enumerated() {
                    if number > 0 { out.append(NSAttributedString(string: "\n")) }
                    let mark = item.checked.map { $0 ? "☑ " : "☐ " } ?? (ordered ? "\(start + number). " : "• ")
                    out.append(NSAttributedString(string: indent + mark, attributes: [.font: body, .foregroundColor: quiet]))
                    let inner = NSMutableAttributedString()
                    append(item.blocks, to: inner, indent: indent + String(repeating: " ", count: mark.count), missing: missing, gap: itemGap)
                    // The item's first line stands beside its mark.
                    if inner.string.hasPrefix(indent + String(repeating: " ", count: mark.count)) {
                        inner.deleteCharacters(in: NSRange(location: 0, length: (indent as NSString).length + mark.count))
                    }
                    out.append(inner)
                }
            case let .table(header, _, rows):
                // A row to a line, its cells apart by tabs that stand where
                // the widest cell of each column ends: columns to the eye,
                // and to whatever the text is pasted into.
                let lines = ([header] + rows).enumerated().map { number, cells in
                    cells.map { line($0, font: number == 0 ? bold(body, larger: 1) : body, indent: "", missing: missing) }
                }
                var stops: [NSTextTab] = []
                var edge: CGFloat = 0
                for column in 0 ..< max(0, header.count - 1) {
                    edge += (lines.map { column < $0.count ? $0[column].size().width : 0 }.max() ?? 0) + columnGap
                    stops.append(NSTextTab(textAlignment: .natural, location: edge))
                }
                let style = NSMutableParagraphStyle()
                style.tabStops = stops
                let table = NSMutableAttributedString()
                for (number, cells) in lines.enumerated() {
                    if number > 0 { table.append(NSAttributedString(string: "\n")) }
                    table.append(NSAttributedString(string: indent, attributes: [.font: body]))
                    for (column, cell) in cells.enumerated() {
                        if column > 0 { table.append(NSAttributedString(string: "\t", attributes: [.font: body])) }
                        table.append(cell)
                    }
                }
                table.addAttribute(.paragraphStyle, value: style, range: NSRange(location: 0, length: table.length))
                out.append(table)
            case .rule:
                out.append(NSAttributedString(string: indent + "———", attributes: [.font: body, .foregroundColor: quiet]))
            }
        }
    }

    /// A line of inline Markdown in the system's own attributes: its
    /// emphasis as the font's traits, its code in the fixed-width font, a
    /// link as a link.
    private static func line(_ text: String, font: PlatformFont, indent: String, missing: Set<String>) -> NSAttributedString {
        let out = NSMutableAttributedString(string: indent, attributes: [.font: font, .foregroundColor: label])
        let parsed = parseInline(text, missing: missing)
        for run in parsed.runs {
            var attributes: [NSAttributedString.Key: Any] = [.foregroundColor: label]
            let intent = run.inlinePresentationIntent ?? []
            var face = font
            if intent.contains(.code) {
                face = fixed(font.pointSize * 0.9)
            } else {
                if intent.contains(.stronglyEmphasized) { face = with(face, bold: true) }
                if intent.contains(.emphasized) { face = with(face, italic: true) }
            }
            attributes[.font] = face
            if intent.contains(.strikethrough) { attributes[.strikethroughStyle] = NSUnderlineStyle.single.rawValue }
            if let link = run.link { attributes[.link] = link }
            out.append(NSAttributedString(string: String(parsed[run.range].characters), attributes: attributes))
        }
        return out
    }

    private static func bold(_ font: PlatformFont, larger scale: CGFloat) -> PlatformFont {
        with(font.withSize(font.pointSize * scale), bold: true)
    }

    private static func with(_ font: PlatformFont, bold: Bool = false, italic: Bool = false) -> PlatformFont {
        #if os(macOS)
        var traits = font.fontDescriptor.symbolicTraits
        if bold { traits.insert(.bold) }
        if italic { traits.insert(.italic) }
        return NSFont(descriptor: font.fontDescriptor.withSymbolicTraits(traits), size: font.pointSize) ?? font
        #else
        var traits = font.fontDescriptor.symbolicTraits
        if bold { traits.insert(.traitBold) }
        if italic { traits.insert(.traitItalic) }
        return font.fontDescriptor.withSymbolicTraits(traits).map { UIFont(descriptor: $0, size: font.pointSize) } ?? font
        #endif
    }
}

/// The system's text view, read-only: where a selection can run across
/// lines and blocks, which a SwiftUI text on an iPhone does not allow.
struct SelectableText {
    let text: NSAttributedString
    /// The space around the text.
    static let inset = ChatSpace.edge
}

#if os(macOS)
extension SelectableText: NSViewRepresentable {
    func makeNSView(context: Context) -> NSScrollView {
        let scroll = NSTextView.scrollableTextView()
        if let view = scroll.documentView as? NSTextView {
            view.isEditable = false
            view.isSelectable = true
            view.drawsBackground = false
            view.textContainerInset = NSSize(width: Self.inset, height: Self.inset)
            view.textStorage?.setAttributedString(text)
        }
        scroll.drawsBackground = false
        return scroll
    }

    func updateNSView(_ scroll: NSScrollView, context: Context) {
        guard let view = scroll.documentView as? NSTextView, view.attributedString() != text else { return }
        view.textStorage?.setAttributedString(text)
    }
}
#else
extension SelectableText: UIViewRepresentable {
    func makeUIView(context: Context) -> UITextView {
        let view = UITextView()
        view.isEditable = false
        view.isSelectable = true
        view.backgroundColor = .clear
        view.adjustsFontForContentSizeCategory = true
        view.textContainerInset = UIEdgeInsets(top: Self.inset, left: Self.inset, bottom: Self.inset, right: Self.inset)
        view.attributedText = text
        return view
    }

    func updateUIView(_ view: UITextView, context: Context) {
        if view.attributedText != text { view.attributedText = text }
    }
}
#endif
