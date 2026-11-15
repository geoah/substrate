import SubstrateKit
import SwiftUI

/// A card in the transcript: a plain fill of its tone, or of none, never
/// glass (glass is for the control layer).
struct CardBackground: ViewModifier {
    var tone: ScreenTone?

    func body(content: Content) -> some View {
        content
            .padding(ChatSpace.inset)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(ChatColours.cardStyle(tone), in: .rect(cornerRadius: ChatRadius.card))
            .overlay {
                RoundedRectangle(cornerRadius: ChatRadius.card)
                    .strokeBorder(ChatColours.cardEdge(tone), lineWidth: ChatLine.hairline)
            }
    }
}

extension View {
    func card(_ tone: ScreenTone? = nil) -> some View { modifier(CardBackground(tone: tone)) }
}
