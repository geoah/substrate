import SwiftUI

/// A card in the transcript: a plain fill, never glass (glass is for the
/// control layer).
struct CardBackground: ViewModifier {
    var tint: Color?

    func body(content: Content) -> some View {
        content
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background((tint ?? Color.secondary).opacity(tint == nil ? 0.08 : 0.1), in: .rect(cornerRadius: 16))
            .overlay {
                RoundedRectangle(cornerRadius: 16)
                    .strokeBorder((tint ?? Color.secondary).opacity(0.18), lineWidth: 0.5)
            }
    }
}

extension View {
    func card(tint: Color? = nil) -> some View { modifier(CardBackground(tint: tint)) }
}
