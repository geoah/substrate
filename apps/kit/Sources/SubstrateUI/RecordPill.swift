#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple

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
        let label = HStack(spacing: 5) {
            KindGlyph(kind: reference.kind, size: 16)
            Text(name)
                .lineLimit(1)
                .truncationMode(.tail)
        }
        .padding(.leading, 3)
        .padding(.trailing, 8)
        .padding(.vertical, 3)
        .background(.fill.tertiary, in: .capsule)
        .contentShape(.capsule)
        Group {
            if let open {
                Button { open(reference, title: known) } label: { label }
                    .buttonStyle(.plain)
                    .accessibilityHint("Opens it")
            } else {
                label
            }
        }
        .accessibilityLabel("\(kindDisplayName(reference.kind)): \(name)")
        .task(id: reference) {
            if known == nil { await directory?.resolve(reference) }
        }
    }
}
#endif
