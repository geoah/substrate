import ChatKit
import SubstrateKit
import SwiftUI

/// Parsed rows, memoized by text, for the texts a view parses itself.
@MainActor
enum MarkdownCache {
    private static var rows: [String: [MarkdownRow]] = [:]
    static var inlines: [String: InlineLine] = [:]
    private static var order: [String] = []

    static func parse(_ text: String) -> [MarkdownRow] {
        if let hit = rows[text] { return hit }
        let parsed = Markdown.rows(text, open: false)
        rows[text] = parsed
        order.append(text)
        if order.count > 400 { rows[order.removeFirst()] = nil }
        return parsed
    }
}

/// Markdown as SwiftUI views: a reply's rows, parsed where its presentation
/// was made, or a text parsed here once and kept (a summary, a sub-agent's
/// answer under its call), which is never one being written. What a link in
/// it opens is the transcript's to say (`replyLinks()`), set once above it.
struct MarkdownView: View {
    private let text: String
    private let rows: ArraySlice<MarkdownRow>?
    /// The `kind/id` paths the engine found no record for: their links read
    /// as their text alone.
    private let missing: Set<String>

    init(text: String, missing: Set<String> = []) {
        self.text = text
        rows = nil
        self.missing = missing
    }

    init(rows: ArraySlice<MarkdownRow>, missing: Set<String> = []) {
        text = ""
        self.rows = rows
        self.missing = missing
    }

    var body: some View {
        let _ = Meter.shared.count(.body, "Markdown")
        let rows = rows.map(Array.init) ?? MarkdownCache.parse(text)
        // By position: the block being written keeps its view, and with it
        // where its code or its table was scrolled to. A finished block is
        // an equal value there, which is not drawn again.
        VStack(alignment: .leading, spacing: 12) {
            ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                BlockView(block: row.block, code: row.code[...])
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .environment(\.missingRecords, missing)
    }
}

/// What a link in a reply opens: a record in the app, a console page on the
/// signed-in server, anything else where the system opens it. Set once for
/// a transcript and never where a reply is drawn: an action is a closure,
/// which nothing can compare, so a view that set one at each of its draws
/// had every text under it read its environment and resolve again.
private struct ReplyLinks: ViewModifier {
    @Environment(\.openRecord) private var openRecord
    @Environment(\.openURL) private var openURL

    func body(content: Content) -> some View {
        content.environment(
            \.openURL,
            OpenURLAction { url in
                guard let reference = linkedRecord(url) else {
                    if let path = consolePath(url) {
                        if let server = SessionStore.chat.load()?.server, let page = ServerAddress.page(server, path: path) {
                            openURL(page)
                        }
                    } else {
                        openURL(url)
                    }
                    return .handled
                }
                openRecord?(reference)
                return .handled
            })
    }
}

extension View {
    /// The links of every reply drawn under this view open as a reply's do.
    func replyLinks() -> some View { modifier(ReplyLinks()) }
}

extension EnvironmentValues {
    /// The paths a reply links that name no record (`MarkdownView.missing`).
    @Entry var missingRecords: Set<String> = []
}

/// A console page a reply links by its path alone (`[Books](/applets/books)`,
/// as the console's own chat draws it): it opens on the signed-in server.
func consolePath(_ url: URL) -> String? {
    guard url.scheme == nil, url.host == nil else { return nil }
    let path = url.relativeString
    guard path.hasPrefix("/"), !path.hasPrefix("//"), !path.contains("\\") else { return nil }
    return path
}

/// Inline Markdown as text, with links limited to http, https, mailto,
/// console paths and record links, each that leaves the app marked; memoized, since every
/// visible paragraph asks again on each redraw.
@MainActor
func inline(_ text: String, missing: Set<String> = []) -> InlineLine {
    let key = missing.isEmpty ? text : text + "\u{0}" + missing.sorted().joined(separator: "\u{0}")
    if let hit = MarkdownCache.inlines[key] { return hit }
    let out = Meter.shared.time(.inline) {
        let pieces = inlinePieces(parseInline(text, missing: missing))
        return InlineLine(text: marked(pieces), pills: pieces.contains { if case .record = $0 { true } else { false } })
    }
    MarkdownCache.inlines[key] = out
    if MarkdownCache.inlines.count > 2000 { MarkdownCache.inlines.removeAll(keepingCapacity: true) }
    return out
}

/// A line of inline Markdown as it is drawn. A record a line links is the
/// kit's pill, drawn behind its words by a renderer of the text's own
/// (`recordPillText()`), and only a line that holds one is drawn through
/// it: a text with a renderer is laid out and drawn by the slower way, and
/// most lines of most replies link no record.
struct InlineLine: View {
    let text: Text
    let pills: Bool

    var body: some View {
        if pills {
            text.recordPillText()
        } else {
            text
        }
    }
}

/// A line of inline text, cut where a link that leaves the app ends (its
/// mark goes there) and around each record a link names (its pill).
enum InlinePiece: Equatable {
    case text(AttributedString)
    case leaves
    /// A link to a record, and the link's own words: the pill's name.
    case record(RecordReference, AttributedString)
}

/// The pieces of a line: a record's link is its pill, and any other link
/// leaves the app, its mark following the last run it covers.
func inlinePieces(_ text: AttributedString) -> [InlinePiece] {
    var pieces: [InlinePiece] = []
    var pending = AttributedString()
    var leaving: URL?
    var record: (reference: RecordReference, link: URL)?
    func flush() {
        if !pending.characters.isEmpty { pieces.append(.text(pending)) }
        pending = AttributedString()
    }
    for run in text.runs {
        let named = run.link.flatMap(linkedRecord)
        let leaves = named == nil ? run.link : nil
        if leaving != nil, leaves != leaving {
            flush()
            pieces.append(.leaves)
        }
        if let current = record, run.link != current.link {
            pending.link = nil
            pieces.append(.record(current.reference, pending))
            pending = AttributedString()
            record = nil
        }
        if let named, let link = run.link, record == nil {
            flush()
            record = (named, link)
        }
        pending.append(text[run.range])
        leaving = leaves
    }
    if let current = record {
        pending.link = nil
        pieces.append(.record(current.reference, pending))
    } else {
        flush()
    }
    if leaving != nil { pieces.append(.leaves) }
    return pieces
}

/// The line as one text: each mark kept beside its link by a no-break
/// space, each record as its pill.
func marked(_ text: AttributedString) -> Text { marked(inlinePieces(text)) }

func marked(_ pieces: [InlinePiece]) -> Text {
    if pieces.count == 1, case let .text(only) = pieces[0] { return Text(only) }
    guard !pieces.isEmpty else { return Text(verbatim: "") }
    return pieces.enumerated().reduce(Text(verbatim: "")) { line, piece in
        switch piece.element {
        case let .text(part): Text("\(line)\(part)")
        case .leaves: Text("\(line)\u{00A0}\(ExternalMark.text)")
        case let .record(reference, name):
            // Its place in the line tells it from the same record's pill
            // beside it.
            Text("\(line)\(RecordPillText.text(reference, name: name, link: URL(string: "substrate://" + reference.path), occurrence: piece.offset))")
        }
    }
}

func parseInline(_ text: String, missing: Set<String> = []) -> AttributedString {
    var out =
        (try? AttributedString(
            markdown: text,
            options: .init(
                allowsExtendedAttributes: false, interpretedSyntax: .inlineOnlyPreservingWhitespace,
                failurePolicy: .returnPartiallyParsedIfPossible)))
        ?? AttributedString(text)
    for run in out.runs {
        if let link = run.link {
            if let record = linkedRecord(link) {
                // A record link opens the record in the app; one the engine
                // found no record for is the model's invention, shown as text.
                if missing.contains(record.path) {
                    out[run.range].link = nil
                } else {
                    out[run.range].link = URL(string: "substrate://" + record.path)
                }
            } else if consolePath(link) == nil, !["http", "https", "mailto"].contains(link.scheme?.lowercased() ?? "") {
                out[run.range].link = nil
            }
        }
        if run.inlinePresentationIntent?.contains(.code) == true {
            out[run.range].font = .system(.body, design: .monospaced)
            out[run.range].backgroundColor = Color.secondary.opacity(0.14)
        }
    }
    return out
}

private struct BlockView: View {
    @Environment(\.missingRecords) private var missing
    let block: MarkdownBlock
    /// The colours of the code blocks in this block, in the order they
    /// stand (`MarkdownRow.code`).
    let code: ArraySlice<CodeHighlight>

    /// The colours of the code in each of `blocks`, which stand one after
    /// another in this block from `start` of its own.
    private func colours(_ blocks: [MarkdownBlock], from start: Int) -> [ArraySlice<CodeHighlight>] {
        var at = start
        return blocks.map { block in
            let end = min(at + block.codeCount, code.endIndex)
            defer { at = end }
            return code[min(at, end) ..< end]
        }
    }

    var body: some View {
        let _ = Meter.shared.count(.body, "MarkdownBlock")
        switch block {
        case let .paragraph(text):
            inline(text, missing: missing)
                .fixedSize(horizontal: false, vertical: true)
        case let .heading(level, text):
            inline(text, missing: missing)
                .font(level == 1 ? .title2.bold() : level == 2 ? .title3.bold() : .headline)
                .padding(.top, level <= 2 ? 6 : 2)
                .accessibilityAddTraits(.isHeader)
        case let .code(language, text, _):
            CodeBlockView(language: language, text: text, code: code.first)
        case let .quote(blocks):
            let colours = colours(blocks, from: code.startIndex)
            HStack(alignment: .top, spacing: 10) {
                RoundedRectangle(cornerRadius: 1.5)
                    .fill(.tertiary)
                    .frame(width: 3)
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(Array(blocks.enumerated()), id: \.offset) { index, block in BlockView(block: block, code: colours[index]) }
                }
                .foregroundStyle(.secondary)
            }
        case let .list(ordered, start, items):
            // Each item's code follows the code of the items before it.
            let starts = items.reduce(into: [code.startIndex]) { starts, item in
                starts.append((starts.last ?? 0) + item.blocks.reduce(0) { $0 + $1.codeCount })
            }
            VStack(alignment: .leading, spacing: 6) {
                ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Group {
                            if let checked = item.checked {
                                Image(systemName: checked ? "checkmark.square.fill" : "square")
                                    .foregroundStyle(checked ? Color.accentColor : .secondary)
                            } else if ordered {
                                Text("\(start + index).").monospacedDigit()
                            } else {
                                Text("•")
                            }
                        }
                        .foregroundStyle(.secondary)
                        .frame(minWidth: ordered ? 22 : 12, alignment: .trailing)
                        let colours = colours(item.blocks, from: starts[index])
                        VStack(alignment: .leading, spacing: 6) {
                            ForEach(Array(item.blocks.enumerated()), id: \.offset) { at, block in BlockView(block: block, code: colours[at]) }
                        }
                    }
                }
            }
        case let .table(header, alignments, rows):
            TableBlockView(header: header, alignments: alignments, rows: rows)
        case .rule:
            Divider().padding(.vertical, 4)
        }
    }
}
