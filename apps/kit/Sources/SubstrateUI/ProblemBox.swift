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

    public init(
        title: String? = nil, message: String, tint: Color = .red,
        systemImage: String = "exclamationmark.triangle.fill"
    ) {
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
#endif
