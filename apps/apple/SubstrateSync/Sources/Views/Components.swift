import SwiftUI

/// A problem, in the same place a status would be: a red icon, a short title
/// and the server's or the system's own words. It sits in a card of its own or
/// inside one, never as a box inside a box.
struct ErrorBox: View {
    var title = "Something went wrong"
    let message: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.red)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 3) {
                Text(title).font(.subheadline.weight(.semibold))
                Text(message)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }
}

/// A short state word in a capsule: what a step or a source is, at a glance.
/// The word is the colour darkened (lightened in dark mode), so it reads on
/// its own pale tint.
struct StatusBadge: View {
    let text: String
    let color: Color
    @Environment(\.colorScheme) private var scheme

    var body: some View {
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

/// The app's buttons, one height whatever their label holds. iOS's own glass
/// buttons swell while pressed, and a spinner is taller than a symbol; these
/// dim instead, so nothing around them moves.
struct ActionButtonStyle: ButtonStyle {
    enum Kind { case primary, secondary, destructive }

    let kind: Kind
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .padding(.horizontal, 16)
            .frame(maxWidth: .infinity, minHeight: 50)
            .foregroundStyle(foreground)
            .tint(foreground)
            .background(background, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .opacity(configuration.isPressed ? 0.6 : (isEnabled ? 1 : 0.4))
            .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }

    private var foreground: Color {
        switch kind {
        case .primary: .white
        case .secondary: .accentColor
        case .destructive: .red
        }
    }

    private var background: Color {
        switch kind {
        case .primary: .accentColor
        case .secondary: .accentColor.opacity(0.12)
        case .destructive: .red.opacity(0.12)
        }
    }
}

extension View {
    /// The one main action on a screen, filled.
    func primaryAction() -> some View { buttonStyle(ActionButtonStyle(kind: .primary)) }

    /// A second action beside the main one, tinted.
    func secondaryAction() -> some View { buttonStyle(ActionButtonStyle(kind: .secondary)) }

    /// An action that ends something: Stop, Sign Out.
    func destructiveAction() -> some View { buttonStyle(ActionButtonStyle(kind: .destructive)) }

    /// A page as wide as reads well on an iPad, on the grouped background
    /// edge to edge.
    func readableWidth() -> some View {
        frame(maxWidth: 640)
            .frame(maxWidth: .infinity)
            .background(Color(.systemGroupedBackground).ignoresSafeArea())
    }

    /// A card row that holds only a button: no row background, no separator,
    /// the button's own width.
    func buttonRow() -> some View {
        listRowBackground(Color.clear)
            .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 4, trailing: 0))
            .listRowSeparator(.hidden)
    }
}

/// A button's label: its symbol, or a spinner in the same place while it
/// works, and its title. The symbol's slot is fixed, so swapping in the
/// spinner changes neither the width nor the height.
struct WideLabel: View {
    let title: String
    var systemImage: String?
    var busy = false

    var body: some View {
        HStack(spacing: 8) {
            if busy || systemImage != nil {
                ZStack {
                    if busy {
                        ProgressView().controlSize(.small)
                    } else if let systemImage {
                        Image(systemName: systemImage).imageScale(.medium)
                    }
                }
                .frame(width: 22, height: 22)
                .accessibilityHidden(true)
            }
            Text(title)
        }
        .accessibilityLabel(busy ? "\(title), working" : title)
    }
}

extension Date {
    /// "now", "3 min. ago", "2 hr. ago": short enough for one line.
    var shortAgo: String { formatted(.relative(presentation: .named, unitsStyle: .abbreviated)) }
}
