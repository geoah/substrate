import ChatKit
import SubstrateKit
import SwiftUI

/// Parsed blocks, memoized by text: a settled reply is parsed once, and only
/// the streaming one parses again on each flush.
@MainActor
enum MarkdownCache {
    private static var blocks: [String: [MarkdownBlock]] = [:]
    static var inlines: [String: Text] = [:]
    private static var order: [String] = []

    static func parse(_ text: String) -> [MarkdownBlock] {
        if let hit = blocks[text] { return hit }
        let parsed = Markdown.parse(text)
        blocks[text] = parsed
        order.append(text)
        if order.count > 400 { blocks[order.removeFirst()] = nil }
        return parsed
    }
}

/// A reply's Markdown, as SwiftUI views.
struct MarkdownView: View {
    @Environment(\.openRecord) private var openRecord
    @Environment(\.openURL) private var openURL
    let text: String
    /// The streaming reply is parsed again with every flush and never cached.
    var live = false
    /// The `kind/id` paths the engine found no record for: their links read
    /// as their text alone.
    var missing: Set<String> = []

    var body: some View {
        let blocks = live ? Markdown.parse(text) : MarkdownCache.parse(text)
        VStack(alignment: .leading, spacing: 12) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                BlockView(block: block)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .environment(\.missingRecords, missing)
        .environment(
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
func inline(_ text: String, missing: Set<String> = []) -> Text {
    let key = missing.isEmpty ? text : text + "\u{0}" + missing.sorted().joined(separator: "\u{0}")
    if let hit = MarkdownCache.inlines[key] { return hit }
    let out = marked(parseInline(text, missing: missing))
    MarkdownCache.inlines[key] = out
    if MarkdownCache.inlines.count > 2000 { MarkdownCache.inlines.removeAll(keepingCapacity: true) }
    return out
}

/// A line of inline text, cut where a link that leaves the app ends: its
/// mark goes there.
enum InlinePiece: Equatable {
    case text(AttributedString)
    case leaves
}

/// The pieces of a line: a link that is not a record's leaves the app, and
/// its mark follows the last run it covers.
func inlinePieces(_ text: AttributedString) -> [InlinePiece] {
    var pieces: [InlinePiece] = []
    var pending = AttributedString()
    var open: URL?
    for run in text.runs {
        let leaving = run.link.flatMap { linkedRecord($0) == nil ? $0 : nil }
        if let current = open, leaving != current {
            pieces += [.text(pending), .leaves]
            pending = AttributedString()
        }
        pending.append(text[run.range])
        open = leaving
    }
    if !pending.characters.isEmpty { pieces.append(.text(pending)) }
    if open != nil { pieces.append(.leaves) }
    return pieces
}

/// The line as one text, each mark kept beside its link by a no-break space.
func marked(_ text: AttributedString) -> Text {
    let pieces = inlinePieces(text)
    guard pieces.contains(.leaves) else { return Text(text) }
    return pieces.reduce(Text(verbatim: "")) { line, piece in
        switch piece {
        case let .text(part): Text("\(line)\(part)")
        case .leaves: Text("\(line)\u{00A0}\(ExternalMark.text)")
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
                    out[run.range].backgroundColor = Color.accentColor.opacity(0.12)
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

    var body: some View {
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
            CodeBlock(language: language, text: text)
        case let .quote(blocks):
            HStack(alignment: .top, spacing: 10) {
                RoundedRectangle(cornerRadius: 1.5)
                    .fill(.tertiary)
                    .frame(width: 3)
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in BlockView(block: block) }
                }
                .foregroundStyle(.secondary)
            }
        case let .list(ordered, start, items):
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
                        VStack(alignment: .leading, spacing: 6) {
                            ForEach(Array(item.blocks.enumerated()), id: \.offset) { _, block in BlockView(block: block) }
                        }
                    }
                }
            }
        case let .table(header, alignments, rows):
            TableBlock(header: header, alignments: alignments, rows: rows)
        case .rule:
            Divider().padding(.vertical, 4)
        }
    }
}

private struct CodeBlock: View {
    let language: String?
    let text: String
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(language ?? "code")
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.secondary)
                Spacer()
                Button {
                    copyToPasteboard(text)
                    copied = true
                    Task {
                        try? await Task.sleep(for: .seconds(1.5))
                        copied = false
                    }
                } label: {
                    Label(copied ? "Copied" : "Copy", systemImage: copied ? "checkmark" : "doc.on.doc")
                        .font(.caption)
                        .contentTransition(.symbolEffect(.replace))
                }
                .buttonStyle(.borderless)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 7)
            Divider().opacity(0.5)
            ScrollView(.horizontal, showsIndicators: false) {
                Text(text)
                    .font(.system(.callout, design: .monospaced))
                    .textSelection(.enabled)
                    .padding(12)
            }
        }
        .background(Color.secondary.opacity(0.09), in: .rect(cornerRadius: 12))
        .overlay { RoundedRectangle(cornerRadius: 12).strokeBorder(Color.secondary.opacity(0.15), lineWidth: 0.5) }
    }
}

private struct TableBlock: View {
    @Environment(\.missingRecords) private var missing
    let header: [String]
    let alignments: [MarkdownAlignment]
    let rows: [[String]]

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 8) {
                GridRow {
                    ForEach(Array(header.enumerated()), id: \.offset) { i, cell in
                        inline(cell, missing: missing).font(.subheadline.weight(.semibold))
                            .gridColumnAlignment(alignment(i))
                    }
                }
                Divider()
                ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                    GridRow {
                        ForEach(Array(row.enumerated()), id: \.offset) { _, cell in
                            inline(cell, missing: missing).font(.subheadline)
                        }
                    }
                }
            }
            .padding(12)
        }
        .background(Color.secondary.opacity(0.06), in: .rect(cornerRadius: 12))
    }

    private func alignment(_ i: Int) -> HorizontalAlignment {
        switch i < alignments.count ? alignments[i] : .leading {
        case .leading: .leading
        case .center: .center
        case .trailing: .trailing
        }
    }
}
