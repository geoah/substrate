import SwiftUI

extension EnvironmentValues {
    /// How far past the transcript's measure a block that needs the room
    /// may reach, on each side: what the window has beyond the measure.
    /// Nothing on a phone, in a card, and inside a list or a quote, where
    /// a block is as wide as what holds it.
    @Entry var spill: CGFloat = 0
}

/// The transcript keeps a readable measure however wide its window, centred
/// in it (`ChatSize.measure`): a line of 150 characters is not read, it is
/// followed with a finger. What is not read as lines may use the room the
/// window has: a table and a block of code are told how far past the
/// measure they may reach (`spill`).
private struct ReadingMeasure: ViewModifier {
    @State private var spill: CGFloat = 0

    func body(content: Content) -> some View {
        content
            .environment(\.spill, spill)
            .frame(maxWidth: ChatSize.measure + 2 * ChatSpace.margin)
            .frame(maxWidth: .infinity)
            .onGeometryChange(for: CGFloat.self) { proxy in
                max(0, ((proxy.size.width - ChatSize.measure - 2 * ChatSpace.margin) / 2).rounded(.down))
            } action: { room in
                if room != spill { spill = room }
            }
    }
}

/// A block drawn wider than its place by `reach` on each side, about its
/// place's centre. Its own layout is told the place's width (`place`), which
/// the reach does not move, so what a block decides from its width (whether
/// a line of code overflows, how wide a table's columns are) never chases
/// the room it is then given.
private struct Reaching: ViewModifier {
    let reach: CGFloat
    let place: (CGFloat) -> Void

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, -reach)
            .onGeometryChange(for: CGFloat.self) {
                $0.size.width
            } action: {
                place($0)
            }
    }
}

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

    /// The rows under this view keep the reading measure, and its blocks
    /// are told the room past it.
    func readingMeasure() -> some View { modifier(ReadingMeasure()) }

    /// This block is drawn `reach` wider than its place on each side, and
    /// `place` is told its place's width.
    func reaching(_ reach: CGFloat, place: @escaping (CGFloat) -> Void) -> some View { modifier(Reaching(reach: reach, place: place)) }

    /// What is drawn under this view keeps to its own width: a card's
    /// content, a list's item, a quote.
    func keepsToItsWidth() -> some View { environment(\.spill, 0) }
}

/// How far a block reaches past its place on each side: half of what it
/// lacks, and no more than the window has.
func reach(wanting wanted: CGFloat, in place: CGFloat, spill: CGFloat) -> CGFloat {
    guard place > 0, spill > 0 else { return 0 }
    return min(spill, max(0, ((wanted - place) / 2).rounded(.up)))
}
