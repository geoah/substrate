import SubstrateKit
import SwiftUI

// What a layout does where the text size leaves it no room: it takes a
// taller form, and never clips, overlaps or cuts its words to nothing.

/// Buttons side by side, or one above the other where they do not fit.
struct ButtonRow<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: ChatSpace.apart) { content }
            VStack(alignment: .leading, spacing: ChatSpace.items) { content }
        }
    }
}

/// Two things on one line, the second at its far end, the first wrapping
/// in the room that leaves; at the accessibility text sizes the second
/// stands under the first, which then has the whole line: a note and its
/// button, a heading and its badge.
struct SplitRow<Leading: View, Trailing: View>: View {
    @Environment(\.dynamicTypeSize) private var size
    var alignment: VerticalAlignment = .center
    @ViewBuilder var leading: Leading
    @ViewBuilder var trailing: Trailing

    var body: some View {
        if size.isAccessibilitySize {
            VStack(alignment: .leading, spacing: ChatSpace.items) {
                leading
                trailing
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        } else {
            HStack(alignment: alignment, spacing: ChatSpace.apart) {
                leading
                Spacer(minLength: 0)
                trailing
            }
        }
    }
}

/// A fact as its label and its value in one run of words, the label
/// heavier: it wraps between words where a line cannot hold it, and never
/// inside one, which two texts side by side do.
struct FactText: View {
    let label: String
    let value: String
    /// The value is an identifier, drawn in fixed-width letters.
    var verbatim = false

    var body: some View {
        Text("\(Text(label).fontWeight(ChatType.strong)) \(Text(value).monospaced(verbatim))")
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityLabel("\(label): \(value)")
    }
}
