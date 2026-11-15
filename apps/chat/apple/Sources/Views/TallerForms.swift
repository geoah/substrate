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

/// Two things on one line, the second at its far end; or the second under
/// the first where they do not fit: a note and its button, a heading and
/// its badge.
struct SplitRow<Leading: View, Trailing: View>: View {
    var alignment: VerticalAlignment = .center
    @ViewBuilder var leading: Leading
    @ViewBuilder var trailing: Trailing

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: alignment, spacing: ChatSpace.apart) {
                leading
                Spacer(minLength: 0)
                trailing
            }
            VStack(alignment: .leading, spacing: ChatSpace.items) {
                leading
                trailing
            }
        }
    }
}
