import SwiftUI

extension View {
    /// The transcript's rows from the edges of what they lie in: the
    /// app's scroll view and a picture's frame alike.
    func transcriptMargins() -> some View {
        padding(.horizontal, ChatSpace.margin)
            .padding(.top, ChatSpace.gutter)
            .padding(.bottom, ChatSpace.edge)
    }

    /// A strip over the composer: as wide as the composer's bar, and it
    /// appears.
    func overComposer() -> some View {
        padding(.horizontal, ChatSpace.gutter)
            .frame(maxWidth: ChatSize.measure + 2 * ChatSpace.gutter)
            .arrives()
    }

    /// The transcript keeps a readable measure however wide its window,
    /// centred in it (`ChatSize.measure`).
    func readingMeasure() -> some View {
        frame(maxWidth: ChatSize.measure + 2 * ChatSpace.margin)
            .frame(maxWidth: .infinity)
    }
}
