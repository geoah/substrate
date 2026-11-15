#if canImport(SwiftUI)
import SwiftUI

/// The room a control has to be touched in: its own bounds, widened and
/// heightened about their centre to a whole target (`DesignTokens.touchTarget`)
/// where they are less. A shape for touches alone, so a control keeps the
/// size it is drawn at and the place it is laid out in: a small mark in a
/// low bar, a pill in a line, a link under a card.
public struct TouchTarget: Shape {
    public init() {}

    public func path(in rect: CGRect) -> Path {
        let least = DesignTokens.touchTarget
        return Path(rect.insetBy(dx: min(0, (rect.width - least) / 2), dy: min(0, (rect.height - least) / 2)))
    }
}

public extension View {
    /// This control is touched anywhere in a whole target around it,
    /// whatever it looks.
    func touchTarget() -> some View { contentShape(TouchTarget()) }
}
#endif
