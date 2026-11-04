#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple

/// One property's value, drawn by its shape: a linked record as a pill,
/// several as wrapping pills, a date in the reader's calendar, a link, prose.
public struct RecordValueView: View {
    let shape: ValueShape
    var struck = false

    public init(_ value: JSONValue, declared: PropertyDeclaration? = nil, struck: Bool = false) {
        shape = ValueShape(value, declared: declared)
        self.struck = struck
    }

    public init(shape: ValueShape, struck: Bool = false) {
        self.shape = shape
        self.struck = struck
    }

    public var body: some View {
        switch shape {
        case .empty:
            Text("Empty").foregroundStyle(.secondary)
        case let .reference(ref):
            RecordPill(ref).opacity(struck ? 0.5 : 1)
        case let .references(refs):
            WrapLayout(spacing: 6) {
                ForEach(refs, id: \.self) { RecordPill($0) }
            }
            .opacity(struck ? 0.5 : 1)
        case let .words(s):
            styled(Text(s))
        case let .text(s), let .longText(s):
            // Prose may carry inline Markdown; plain text reads the same.
            styled(Text(markdown(s)))
        case let .link(url):
            Link(url.absoluteString, destination: url).lineLimit(2)
        case let .date(date, time):
            styled(Text(time ? date.formatted(date: .abbreviated, time: .shortened) : date.formatted(date: .abbreviated, time: .omitted)))
        case let .raw(s):
            styled(Text(s).font(.caption.monospaced())).lineLimit(8)
        }
    }

    private func styled(_ text: Text) -> some View {
        text
            .strikethrough(struck)
            .foregroundStyle(struck ? .secondary : .primary)
            .textSelection(.enabled)
    }

    private func markdown(_ s: String) -> AttributedString {
        (try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(s)
    }
}
#endif
