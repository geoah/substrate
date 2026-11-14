#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple

/// What a record's pill is, wherever it stands: the kind's tile, the
/// record's name, a grey capsule behind both. `RecordPill` draws it as a
/// view, and `RecordPillText` inside a line of text, where a view cannot go;
/// both read every number and colour from here, so the two are one pill.
public enum RecordPillStyle {
    /// The capsule's fill.
    public static var fill: some ShapeStyle { .fill.tertiary }
    /// The name.
    public static var name: Color { .primary }
    /// The kind's tile, in points at the default text size, in a pill that
    /// stands alone. Inside a line the tile is as tall as the line lets it
    /// be (`tileInset`).
    public static let tile: CGFloat = 16
    /// Between the tile and the name.
    public static let spacing: CGFloat = 5
    /// The capsule past the tile, and past the name.
    public static let leading: CGFloat = 3
    public static let trailing: CGFloat = 8
    /// The capsule above and below the tile.
    public static let vertical: CGFloat = 3
    /// While it is pressed.
    public static let pressedOpacity = 0.55
    /// A record the server no longer has: its tile and its name, dimmed.
    public static let goneOpacity = 0.5

    /// Inside a line, the capsule stops this far short of the line's own
    /// top and bottom, so two on neighbouring lines do not touch.
    static let lineInset: CGFloat = 1
    /// And the tile this far inside the capsule.
    static let tileInset: CGFloat = 1.5

    /// What a screen reader is told: the kind, then the name.
    public static func label(_ reference: RecordReference, name: String) -> String {
        "\(kindDisplayName(reference.kind)): \(name)"
    }

    /// And what a tap does.
    public static let hint = "Opens it"
}

/// A linked record as a pill: its kind's glyph and its name. A tap opens it
/// read-only where a `recordViewer` is above; elsewhere it only names it.
public struct RecordPill: View {
    @Environment(\.recordDirectory) private var directory
    @Environment(\.recordTitles) private var titles
    @Environment(\.openRecord) private var open
    private let reference: RecordReference
    private let title: String?

    public init(_ reference: RecordReference, title: String? = nil) {
        self.reference = reference
        self.title = title
    }

    private var known: String? { title ?? titles[reference.path] ?? directory?.title(for: reference) }

    public var body: some View {
        let name = known ?? fallbackName(reference)
        // Read for its name and not there: deleted since the chat named it.
        let gone = directory?.isGone(reference) ?? false
        let label = HStack(spacing: RecordPillStyle.spacing) {
            KindGlyph(kind: reference.kind, size: RecordPillStyle.tile)
            Text(name)
                .foregroundStyle(RecordPillStyle.name)
                .strikethrough(gone)
                .lineLimit(1)
                // A name too long for its row keeps its start and its end.
                .truncationMode(.middle)
        }
        .opacity(gone ? RecordPillStyle.goneOpacity : 1)
        .padding(.leading, RecordPillStyle.leading)
        .padding(.trailing, RecordPillStyle.trailing)
        .padding(.vertical, RecordPillStyle.vertical)
        .background(RecordPillStyle.fill, in: .capsule)
        .contentShape(.capsule)
        Group {
            if let open {
                Button {
                    open(reference, title: known)
                } label: {
                    label
                }
                .buttonStyle(RecordPillButtonStyle())
                // A link, as the same pill is inside a line of text.
                .accessibilityRemoveTraits(.isButton)
                .accessibilityAddTraits(.isLink)
                .accessibilityHint(RecordPillStyle.hint)
            } else {
                label
            }
        }
        .accessibilityLabel(RecordPillStyle.label(reference, name: name) + (gone ? ", deleted" : ""))
        .task(id: reference) {
            if known == nil { await directory?.resolve(reference) }
        }
    }
}

/// The pill while it is pressed.
struct RecordPillButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.opacity(configuration.isPressed ? RecordPillStyle.pressedOpacity : 1)
    }
}
#endif
