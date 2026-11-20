#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateCore

/// Prose a record holds, as Markdown: inline Markdown in paragraphs, each
/// record it links, or names by a bare path, as the record's pill
/// (`RecordPillText`), named by its title once read. A tap on a pill opens
/// the record where a `recordViewer` is above; any other link opens where
/// the system opens it.
public struct RecordMarkdownText: View {
    @Environment(\.recordDirectory) private var directory
    @Environment(\.openRecord) private var openRecord
    let markdown: String

    public init(_ markdown: String) {
        self.markdown = markdown
    }

    public var body: some View {
        let named = RecordPaths.bare(in: markdown)
        let text = named.isEmpty ? markdown : RecordPaths.linking(markdown) { directory?.title(for: $0) ?? fallbackName($0) }
        let paragraphs = Self.paragraphs(text)
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(paragraphs.enumerated()), id: \.offset) { _, paragraph in
                Self.line(paragraph)
                    .recordPillText()
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .environment(
            \.openURL,
            OpenURLAction { url in
                guard let reference = RecordReference(link: url) else { return .systemAction }
                openRecord?(reference, title: directory?.title(for: reference))
                return .handled
            }
        )
        .task(id: named) {
            for reference in named where directory?.title(for: reference) == nil {
                await directory?.resolve(reference)
            }
        }
    }

    /// The paragraphs of a text: what blank lines part.
    static func paragraphs(_ text: String) -> [String] {
        text.replacingOccurrences(of: "\r\n", with: "\n")
            .components(separatedBy: "\n\n")
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
    }

    /// One paragraph as text: each record link its pill, any other link
    /// kept where it is safe to open (http, https, mailto).
    static func line(_ paragraph: String) -> Text {
        let parsed =
            (try? AttributedString(
                markdown: paragraph,
                options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace, failurePolicy: .returnPartiallyParsedIfPossible)))
            ?? AttributedString(paragraph)
        var out = Text(verbatim: "")
        var pending = AttributedString()
        var occurrence = 0
        var record: (reference: RecordReference, link: URL, name: AttributedString)?
        func flushRecord() {
            guard let current = record else { return }
            out = Text("\(out)\(RecordPillText.text(current.reference, name: current.name, link: current.link, occurrence: occurrence))")
            occurrence += 1
            record = nil
        }
        for run in parsed.runs {
            var piece = AttributedString(parsed[run.range])
            if let link = run.link, let reference = RecordReference(link: link) {
                if let current = record, current.link == link {
                    record?.name.append(piece)
                    continue
                }
                flushRecord()
                if !pending.characters.isEmpty {
                    out = Text("\(out)\(pending)")
                    pending = AttributedString()
                }
                piece.link = nil
                let target = URL(string: "substrate://\(reference.kind)/\(reference.recordID)") ?? link
                record = (reference, target, piece)
                continue
            }
            flushRecord()
            if let link = run.link, !["http", "https", "mailto"].contains(link.scheme?.lowercased() ?? "") {
                piece.link = nil
            }
            pending.append(piece)
        }
        flushRecord()
        if !pending.characters.isEmpty { out = Text("\(out)\(pending)") }
        return out
    }
}
#endif
