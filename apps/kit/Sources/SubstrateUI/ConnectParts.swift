#if canImport(SwiftUI)
import SwiftUI

/// A field of the connect screen, and of a door an app draws under it (a
/// password, a code): a mark, then what is typed, on one filled shape, so
/// an app's fields are the screen's own.
public struct ConnectField<Content: View>: View {
    private let label: String
    private let systemImage: String
    private let content: Content

    public init(_ label: String, systemImage: String, @ViewBuilder content: () -> Content) {
        self.label = label
        self.systemImage = systemImage
        self.content = content()
    }

    public var body: some View {
        HStack(spacing: DesignTokens.Spacing.group.points) {
            Image(systemName: systemImage)
                .foregroundStyle(.secondary)
                .frame(width: mark)
            content
                .textFieldStyle(.plain)
        }
        .padding(.horizontal, DesignTokens.Spacing.content.points)
        .frame(minHeight: height)
        .background(.fill.tertiary, in: .rect(cornerRadius: DesignTokens.Radius.field.points))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(label)
    }

    /// The column the mark stands in, and the least a field is tall, at
    /// the text's usual size.
    @ScaledMetric(relativeTo: .body) private var mark = 22.0
    @ScaledMetric(relativeTo: .body) private var height = 50.0
}

/// The connect screen's one wide button, and a door's under it: its title,
/// a spinner while its work is out, filled with the accent as a fill.
public struct ConnectButton: View {
    private let title: String
    private let busy: Bool
    private let action: () -> Void

    public init(_ title: String, busy: Bool = false, action: @escaping () -> Void) {
        self.title = title
        self.busy = busy
        self.action = action
    }

    public var body: some View {
        Button(action: action) {
            HStack(spacing: DesignTokens.Spacing.medium.points) {
                if busy { ProgressView().controlSize(.small) }
                Text(title)
            }
            .frame(maxWidth: .infinity, minHeight: 30)
        }
        .buttonStyle(.borderedProminent)
        .prominentTint()
        .buttonBorderShape(.capsule)
        .controlSize(.large)
    }
}
#endif
