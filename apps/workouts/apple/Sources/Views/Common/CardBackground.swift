import WorkoutsCore
import SwiftUI
import UIKit

/// A content card: the grouped background's raised surface, never glass (§8).
struct CardBackground: ViewModifier {
    var cornerRadius: CGFloat = 16

    func body(content: Content) -> some View {
        content.background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: cornerRadius))
    }
}

extension View {
    func cardBackground(cornerRadius: CGFloat = 16) -> some View {
        modifier(CardBackground(cornerRadius: cornerRadius))
    }
}
