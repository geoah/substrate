import SubstrateKit
import SwiftUI

/// An agent's name in a capsule tinted with a colour of the id's own, so one
/// agent reads the same in the list, the composer and Settings. The
/// capsule is the kit's record pill's (`RecordPillStyle`: its shape, its
/// word from its ends and from its top and bottom, a mark from its word),
/// so every pill in the app is one shape; what it does not share is the
/// pill's grey and its kind's tile, since the colour here is the agent.
struct AgentPill: View {
    @Environment(\.colorScheme) private var scheme
    let id: String?
    let name: String
    /// The chat is running: a pulsing dot before the name.
    var running = false
    /// The composer's picker: close to the field's type, with the chevron
    /// inside.
    var picker = false

    var body: some View {
        let colours = AgentColours(id)
        HStack(spacing: RecordPillStyle.spacing) {
            if running { PulsingDot(color: colours.fill) }
            Text(name)
                .lineLimit(1)
                .truncationMode(.tail)
            if picker {
                Image(systemName: "chevron.up.chevron.down")
                    .font(ChatType.smallLabel)
            }
        }
        .font(picker ? ChatType.heading : ChatType.noteStrong)
        .foregroundStyle(colours.ink)
        .padding(.horizontal, ChatSpace.pillSide)
        .padding(.vertical, picker ? ChatSpace.lines : ChatSpace.pillEdge)
        .background(colours.fill.opacity(scheme == .dark ? ChatColours.agentPill.dark : ChatColours.agentPill.light), in: .capsule)
    }
}

extension Text {
    /// An agent's name in the ink of its own colour, which reads on each
    /// of `ChatColours.agentBackgrounds`: `surface` is one of them.
    func inAgentInk(_ id: String?, on surface: ChatColours.Surface, selected: Bool = false) -> Text {
        foregroundStyle(selected ? ChatColours.Ink.label.style : AnyShapeStyle(AgentColours(id).ink))
    }
}

/// An agent's colour from its id alone (`ChatColours.hue`): what its pill
/// is filled with a little of, and the ink its name is written in, which
/// reads on the page, on the list and on that fill. No id, no colour:
/// quiet.
struct AgentColours {
    let fill: Color
    let ink: Color

    init(_ id: String?) {
        guard let id else {
            fill = ChatColours.neutral
            ink = ChatColours.Ink.quiet.color
            return
        }
        let hue = ChatColours.hue(of: id)
        fill = ChatColours.agentTint(hue: hue).color
        ink = ChatColours.agentInk(hue: hue).color
    }
}
