import SwiftUI
import WorkoutsCore

/// An owner's note (a routine's, an exercise's, a set's, a session's) as
/// Markdown, collapsed to `lines` lines with Show more where it runs longer.
/// It is one `Text`, so the font, style and italic the caller sets reach it
/// as they reached the plain text it replaces.
struct NotesText: View {
    let text: String
    var lines = 5

    @State private var expanded = false
    @State private var shownHeight: CGFloat = 0
    @State private var fullHeight: CGFloat = 0

    init(_ text: String, lines: Int = 5) {
        self.text = text
        self.lines = lines
    }

    var body: some View {
        let rendered = Text(Self.attributed(text))
        VStack(alignment: .leading, spacing: 4) {
            rendered
                .lineLimit(expanded ? nil : lines)
                .frame(maxWidth: .infinity, alignment: .leading)
                .onGeometryChange(for: CGFloat.self) {
                    $0.size.height
                } action: {
                    shownHeight = $0
                }
                // The whole note at the same width, measured to know whether
                // the limit cut anything.
                .background(alignment: .topLeading) {
                    rendered
                        .fixedSize(horizontal: false, vertical: true)
                        .hidden()
                        .onGeometryChange(for: CGFloat.self) {
                            $0.size.height
                        } action: {
                            fullHeight = $0
                        }
                }
            if expanded || fullHeight > shownHeight + 1 {
                Button(expanded ? "Show less" : "Show more") {
                    withAnimation(.smooth(duration: 0.2)) { expanded.toggle() }
                }
                .buttonStyle(.borderless)
                .font(.footnote.weight(.semibold))
                .italic(false)
                .foregroundStyle(.tint)
            }
        }
    }

    static func attributed(_ text: String) -> AttributedString {
        let inline = NotesMarkdown.inline(text)
        let options = AttributedString.MarkdownParsingOptions(
            interpretedSyntax: .inlineOnlyPreservingWhitespace, failurePolicy: .returnPartiallyParsedIfPossible)
        return (try? AttributedString(markdown: inline, options: options)) ?? AttributedString(inline)
    }
}
