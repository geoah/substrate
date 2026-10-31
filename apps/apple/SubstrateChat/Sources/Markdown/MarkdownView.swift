import SwiftUI

/// Parsed blocks, memoized by text: a settled reply is parsed once, and only
/// the streaming one parses again on each flush.
@MainActor
enum MarkdownCache {
    private static var blocks: [String: [MarkdownBlock]] = [:]
    static var inlines: [String: AttributedString] = [:]
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
    let text: String
    /// The streaming reply is parsed again with every flush and never cached.
    var live = false

    var body: some View {
        let blocks = live ? Markdown.parse(text) : MarkdownCache.parse(text)
        VStack(alignment: .leading, spacing: 12) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                BlockView(block: block)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Inline Markdown, with links limited to http, https and mailto; memoized,
/// since every visible paragraph asks again on each redraw.
@MainActor
func inline(_ text: String) -> AttributedString {
    if let hit = MarkdownCache.inlines[text] { return hit }
    let out = parseInline(text)
    MarkdownCache.inlines[text] = out
    if MarkdownCache.inlines.count > 2000 { MarkdownCache.inlines.removeAll(keepingCapacity: true) }
    return out
}

private func parseInline(_ text: String) -> AttributedString {
    var out = (try? AttributedString(
        markdown: text,
        options: .init(allowsExtendedAttributes: false, interpretedSyntax: .inlineOnlyPreservingWhitespace,
                       failurePolicy: .returnPartiallyParsedIfPossible)))
        ?? AttributedString(text)
    for run in out.runs {
        if let link = run.link, !["http", "https", "mailto"].contains(link.scheme?.lowercased() ?? "") {
            out[run.range].link = nil
        }
        if run.inlinePresentationIntent?.contains(.code) == true {
            out[run.range].font = .system(.body, design: .monospaced)
            out[run.range].backgroundColor = Color.secondary.opacity(0.14)
        }
    }
    return out
}

private struct BlockView: View {
    let block: MarkdownBlock

    var body: some View {
        switch block {
        case let .paragraph(text):
            Text(inline(text))
                .fixedSize(horizontal: false, vertical: true)
        case let .heading(level, text):
            Text(inline(text))
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
    let header: [String]
    let alignments: [MarkdownAlignment]
    let rows: [[String]]

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 8) {
                GridRow {
                    ForEach(Array(header.enumerated()), id: \.offset) { i, cell in
                        Text(inline(cell)).font(.subheadline.weight(.semibold))
                            .gridColumnAlignment(alignment(i))
                    }
                }
                Divider()
                ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                    GridRow {
                        ForEach(Array(row.enumerated()), id: \.offset) { _, cell in
                            Text(inline(cell)).font(.subheadline)
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
