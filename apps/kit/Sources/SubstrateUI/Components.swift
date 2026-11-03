#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple
import SubstrateScreens

/// A problem in a sentence, where a status would be: an icon, a short title
/// and the server's or the system's own words. It sits in a card of its own
/// or inside one, never as a box inside a box.
public struct ProblemBox: View {
    public var title: String?
    public var message: String
    public var tint: Color
    public var systemImage: String

    public init(title: String? = nil, message: String, tint: Color = .red,
                systemImage: String = "exclamationmark.triangle.fill")
    {
        self.title = title
        self.message = message
        self.tint = tint
        self.systemImage = systemImage
    }

    /// A problem as its presentation words it.
    public init(_ problem: ProblemPresentation) {
        self.init(title: problem.title, message: problem.message, tint: problem.tone.color, systemImage: problem.symbol.systemName)
    }

    public var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: systemImage)
                .foregroundStyle(tint)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 3) {
                if let title { Text(title).font(.subheadline.weight(.semibold)) }
                Text(message)
                    .font(.subheadline)
                    .foregroundStyle(title == nil ? .primary : .secondary)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }
}

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

/// A card of the sign-in and status screens: a plain fill, never glass
/// (glass is for the control layer).
struct KitCard: ViewModifier {
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
#endif
