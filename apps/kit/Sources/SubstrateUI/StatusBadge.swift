#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple
import SubstrateScreens

/// A short state word in a capsule. The word is the colour darkened
/// (lightened in dark mode), so it reads on its own pale tint.
public struct StatusBadge: View {
    public let text: String
    public let color: Color
    @Environment(\.colorScheme) private var scheme

    public init(text: String, color: Color) {
        self.text = text
        self.color = color
    }

    public var body: some View {
        Text(text)
            .font(.caption.weight(.semibold))
            .lineLimit(1)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .foregroundStyle(color.mix(with: scheme == .dark ? .white : .black, by: scheme == .dark ? 0.2 : 0.3))
            .background(color.opacity(0.16), in: Capsule())
            .fixedSize()
    }
}
#endif
