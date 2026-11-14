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
        Self.symbol(spec, side: side, dark: scheme == .dark)
            .frame(width: side, height: side)
            .background(Self.fill(spec, dark: scheme == .dark), in: .rect(cornerRadius: side * Self.corner))
            .accessibilityHidden(true)
    }

    /// The tile's corner, and its symbol's size, as parts of its side.
    nonisolated static let corner: CGFloat = 0.26
    nonisolated static let symbolScale: CGFloat = 0.58

    nonisolated static func ink(_ spec: KindGlyphSpec, dark: Bool) -> Color {
        dark ? spec.hue.color : spec.hue.color.mix(with: .black, by: 0.3)
    }

    nonisolated static func fill(_ spec: KindGlyphSpec, dark: Bool) -> Color {
        spec.hue.color.opacity(dark ? 0.28 : 0.18)
    }

    nonisolated static func symbol(_ spec: KindGlyphSpec, side: CGFloat, dark: Bool) -> Text {
        Text(Image(systemName: spec.symbol))
            .font(.system(size: side * symbolScale, weight: .semibold))
            .foregroundStyle(ink(spec, dark: dark))
    }

    /// The same tile drawn into a context, where a view cannot go: behind a
    /// line of text (`RecordPillText`).
    nonisolated static func draw(kind: String, in rect: CGRect, context: inout GraphicsContext) {
        let spec = KindGlyphSpec(kind: kind)
        let dark = context.environment.colorScheme == .dark
        let side = min(rect.width, rect.height)
        context.fill(RoundedRectangle(cornerRadius: side * corner).path(in: rect), with: .color(fill(spec, dark: dark)))
        context.draw(context.resolve(symbol(spec, side: side, dark: dark)), at: CGPoint(x: rect.midX, y: rect.midY), anchor: .center)
    }
}
#endif
