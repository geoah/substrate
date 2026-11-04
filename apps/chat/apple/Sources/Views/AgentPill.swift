import SwiftUI

/// An agent's name in a capsule tinted with a colour of the id's own, so one
/// agent reads the same in the list, the composer and Settings.
struct AgentPill: View {
    @Environment(\.colorScheme) private var scheme
    let id: String?
    let name: String
    /// The chat is running: a pulsing dot before the name.
    var running = false
    /// The composer's picker: the field's height and close to its type, with
    /// the chevron inside.
    var picker = false

    var body: some View {
        let tint = agentTint(id, dark: scheme == .dark)
        HStack(spacing: 5) {
            if running { PulsingDot(color: tint) }
            Text(name)
                .lineLimit(1)
                .truncationMode(.tail)
            if picker {
                Image(systemName: "chevron.up.chevron.down")
                    .font(.caption2.weight(.bold))
            }
        }
        .font(picker ? .subheadline.weight(.semibold) : .caption.weight(.semibold))
        .foregroundStyle(tint)
        .padding(.horizontal, picker ? 12 : 8)
        .padding(.vertical, picker ? 0 : 3)
        .frame(height: picker ? 32 : nil)
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
