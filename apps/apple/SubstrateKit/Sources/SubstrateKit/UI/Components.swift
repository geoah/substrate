import SwiftUI

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

/// One change an approval would make: its symbol, its words, and the fact
/// beside it. A change the server cannot make yet says so in orange.
public struct ChangeRow: View {
    public let change: Change

    public init(_ change: Change) {
        self.change = change
    }

    public var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Image(systemName: change.blocked ? "exclamationmark.triangle.fill" : symbol)
                .foregroundStyle(change.blocked ? .orange : tint)
                .frame(width: 22)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(change.words)
                    .fixedSize(horizontal: false, vertical: true)
                if let detail = change.detail {
                    Text(detail)
                        .font(.footnote)
                        .foregroundStyle(change.blocked ? .orange : .secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }

    private var symbol: String {
        switch change.kind {
        case .scope: "checkmark.circle.fill"
        case .install: "arrow.down.circle.fill"
        case .upgrade: "arrow.up.circle.fill"
        case .account: "plus.circle.fill"
        case .webhook: "bolt.circle.fill"
        }
    }

    private var tint: Color {
        switch change.kind {
        case .scope: .green
        case .install, .upgrade: .blue
        case .account: .teal
        case .webhook: .orange
        }
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

/// A day as a person reads it: "1 December", the year added once it is not
/// this one.
func dayWords(_ date: Date, now: Date = Date()) -> String {
    let calendar = Calendar.current
    let sameYear = calendar.component(.year, from: date) == calendar.component(.year, from: now)
    return sameYear ? date.formatted(.dateTime.day().month(.wide)) : date.formatted(.dateTime.day().month(.wide).year())
}
