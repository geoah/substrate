import SwiftUI

/// An agent's name in a capsule tinted with a colour of the id's own, so one
/// agent reads the same in the list, the composer and Settings.
struct AgentPill: View {
    @Environment(\.colorScheme) private var scheme
    let id: String?
    /// The chat is running: a pulsing dot before the name.
    var running = false

    var body: some View {
        let tint = agentTint(id, dark: scheme == .dark)
        HStack(spacing: 5) {
            if running { PulsingDot(color: tint) }
            Text(id.map(agentName) ?? "No assistant")
                .lineLimit(1)
                .truncationMode(.tail)
        }
        .font(.caption.weight(.semibold))
        .foregroundStyle(tint)
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(tint.opacity(scheme == .dark ? 0.22 : 0.13), in: .capsule)
    }
}

/// A colour from the id alone: the same agent is the same colour on every
/// device, with nothing stored.
func agentTint(_ id: String?, dark: Bool) -> Color {
    guard let id else { return .secondary }
    var h: UInt64 = 1_469_598_103_934_665_603
    for byte in id.utf8 { h = (h ^ UInt64(byte)) &* 1_099_511_628_211 }
    return Color(hue: Double(h % 360) / 360, saturation: dark ? 0.5 : 0.7, brightness: dark ? 0.95 : 0.62)
}

/// A problem in a sentence, with an icon, where a status would be.
struct ProblemLine: View {
    var title: String?
    let message: String
    var tint: Color = .red
    var systemImage = "exclamationmark.triangle.fill"

    var body: some View {
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

/// A card in the transcript: a plain fill, never glass (glass is for the
/// control layer).
struct CardBackground: ViewModifier {
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

extension View {
    func card(tint: Color? = nil) -> some View { modifier(CardBackground(tint: tint)) }
}

/// A gentle shimmer over text, for "Thinking…". Still under Reduce Motion.
struct Shimmer: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var phase: CGFloat = -1

    func body(content: Content) -> some View {
        if reduceMotion {
            content
        } else {
            content
                .overlay {
                    GeometryReader { geo in
                        LinearGradient(colors: [.clear, .white.opacity(0.55), .clear],
                                       startPoint: .leading, endPoint: .trailing)
                            .frame(width: geo.size.width * 0.6)
                            .offset(x: phase * geo.size.width * 1.4)
                            .blendMode(.plusLighter)
                    }
                    .mask(content)
                    .allowsHitTesting(false)
                }
                .onAppear {
                    withAnimation(.linear(duration: 1.4).repeatForever(autoreverses: false)) { phase = 1 }
                }
        }
    }
}

extension View {
    func shimmer() -> some View { modifier(Shimmer()) }
}

/// A pulsing dot: a chat is running.
struct PulsingDot: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var color: Color = .accentColor
    @State private var on = false

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: 6, height: 6)
            .opacity(on ? 0.3 : 1)
            .onAppear {
                guard !reduceMotion else { return }
                withAnimation(.easeInOut(duration: 0.9).repeatForever()) { on = true }
            }
            .accessibilityLabel("Working")
    }
}

extension Date {
    /// "now", "3 min. ago", "Yesterday", "Mon", "12 Oct".
    var listStamp: String {
        let cal = Calendar.current
        if Date().timeIntervalSince(self) < 60 { return "now" }
        if cal.isDateInToday(self) { return formatted(date: .omitted, time: .shortened) }
        if cal.isDateInYesterday(self) { return "Yesterday" }
        if Date().timeIntervalSince(self) < 6 * 86400 { return formatted(.dateTime.weekday(.abbreviated)) }
        return formatted(.dateTime.day().month(.abbreviated))
    }
}

/// The bytes of a cache as a person reads them.
func byteWords(_ n: Int) -> String {
    ByteCountFormatter.string(fromByteCount: Int64(n), countStyle: .file)
}
