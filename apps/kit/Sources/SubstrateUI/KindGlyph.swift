#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple

public extension KindHue {
    var color: Color {
        switch self {
        case .gray: .gray
        case .brown: .brown
        case .orange: .orange
        case .yellow: .yellow
        case .green: .green
        case .teal: .teal
        case .blue: .blue
        case .purple: .purple
        case .pink: .pink
        case .red: .red
        }
    }
}

/// The kind's tile: its symbol on its hue. Every surface that marks a kind,
/// or a record of one, draws this.
public struct KindGlyph: View {
    @Environment(\.colorScheme) private var scheme
    @ScaledMetric private var side: CGFloat
    private let spec: KindGlyphSpec

    public init(kind: String, size: CGFloat = 18) {
        spec = KindGlyphSpec(kind: kind)
        _side = ScaledMetric(wrappedValue: size)
    }

    public var body: some View {
        let hue = spec.hue.color
        Image(systemName: spec.symbol)
            .font(.system(size: side * 0.58, weight: .semibold))
            .foregroundStyle(scheme == .dark ? hue : hue.mix(with: .black, by: 0.3))
            .frame(width: side, height: side)
            .background(hue.opacity(scheme == .dark ? 0.28 : 0.18), in: .rect(cornerRadius: side * 0.26))
            .accessibilityHidden(true)
    }
}
#endif
