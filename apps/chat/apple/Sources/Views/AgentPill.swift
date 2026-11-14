import SubstrateKit
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
        let colours = AgentColours(id)
        HStack(spacing: 5) {
            if running { PulsingDot(color: colours.tint) }
            Text(name)
                .lineLimit(1)
                .truncationMode(.tail)
            if picker {
                Image(systemName: "chevron.up.chevron.down")
                    .font(.caption2.weight(.bold))
            }
        }
        .font(picker ? .subheadline.weight(.semibold) : .caption.weight(.semibold))
        .foregroundStyle(colours.ink)
        .padding(.horizontal, picker ? 12 : 8)
        .padding(.vertical, picker ? 0 : 3)
        .frame(height: picker ? 32 : nil)
        .background(colours.tint.opacity(scheme == .dark ? ChatColours.agentPill.dark : ChatColours.agentPill.light), in: .capsule)
    }
}

/// An agent's colour from its id alone (`ChatColours.hue`): the tint its
/// pill is filled with, and the ink its name is written in, which reads on
/// the page, on the list and on that fill. No id, no colour: quiet.
struct AgentColours {
    let tint: Color
    let ink: Color

    init(_ id: String?) {
        guard let id else {
            tint = .secondary
            ink = .quietText
            return
        }
        let hue = ChatColours.hue(of: id)
        tint = ChatColours.agentTint(hue: hue).color
        ink = ChatColours.agentInk(hue: hue).color
    }
}
