import SwiftUI

/// An agent's mark: its initials on a disc whose colour is the id's own, so
/// one agent looks the same in the list, the header and the picker.
struct AgentAvatar: View {
    let id: String?
    var size: CGFloat = 32

    private var name: String { id.map(agentName) ?? "?" }

    private var initials: String {
        let words = name.split(separator: " ")
        let letters = words.prefix(2).compactMap(\.first)
        return letters.isEmpty ? "?" : String(letters).uppercased()
    }

    private var hue: Double {
        guard let id else { return 0.72 }
        var h: UInt64 = 1_469_598_103_934_665_603
        for byte in id.utf8 { h = (h ^ UInt64(byte)) &* 1_099_511_628_211 }
        return Double(h % 360) / 360
    }

    var body: some View {
        Circle()
            .fill(LinearGradient(
                colors: [Color(hue: hue, saturation: 0.55, brightness: 0.92),
                         Color(hue: (hue + 0.08).truncatingRemainder(dividingBy: 1), saturation: 0.75, brightness: 0.7)],
                startPoint: .topLeading, endPoint: .bottomTrailing))
            .overlay {
                Text(initials)
                    .font(.system(size: size * 0.4, weight: .semibold, design: .rounded))
                    .foregroundStyle(.white)
                    .minimumScaleFactor(0.5)
            }
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
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

/// A pulsing ring: a chat is running.
struct RunningRing: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var on = false

    var body: some View {
        Circle()
            .strokeBorder(Color.accentColor, lineWidth: 2)
            .scaleEffect(on ? 1.18 : 1)
            .opacity(on ? 0.2 : 0.9)
            .onAppear {
                guard !reduceMotion else { return }
                withAnimation(.easeInOut(duration: 1.1).repeatForever()) { on = true }
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
