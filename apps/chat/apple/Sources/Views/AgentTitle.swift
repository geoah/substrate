import ChatKit
import SubstrateKit
import SwiftUI

/// A new chat's title as its assistant's picker: the title, and under it
/// the assistant's name with a chevron, which opens the assistants offered.
/// It stands in the bar, so the composer's field keeps its whole width
/// however many lines are typed.
struct AgentTitle: View {
    let key: ChildKey
    let title: String
    let picker: AgentPicker

    var body: some View {
        Menu {
            AgentChoices(key: key, current: picker.current)
        } label: {
            VStack(spacing: 0) {
                Text(title)
                    .font(ChatType.control)
                Text("\(picker.name) \(Text(Image(systemName: "chevron.down")).font(ChatType.smallLabel))")
                    .inAgentInk(picker.current, on: .page)
                    .font(ChatType.noteStrong)
            }
            .lineLimit(1)
            .contentShape(.rect)
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        // The bar holds two lines no larger than this; a long press at the
        // accessibility sizes shows them whole.
        .growing(to: .barTitle)
        .accessibilityShowsLargeContentViewer {
            Text("\(title), \(picker.spoken)")
        }
        .accessibilityLabel(picker.spoken)
        .accessibilityHint(picker.hint)
    }
}

/// The assistants a new chat may go to, the chosen one marked.
struct AgentChoices: View {
    @Environment(ChatHost.self) private var host
    let key: ChildKey
    let current: String?

    var body: some View {
        ForEach(host.chats?.offered ?? []) { agent in
            Button {
                host.send(.choose(agent: agent.id), to: key)
            } label: {
                if agent.id == current {
                    Label(agent.name, systemImage: "checkmark")
                } else {
                    Text(agent.name)
                }
            }
        }
    }
}

extension View {
    /// A new chat's title opens its assistants. An iPhone and an iPad put
    /// `AgentTitle` in the bar's middle; a Mac's window title is where its
    /// own title menu goes, the assistant named under it.
    @ViewBuilder func pickingAgent(_ screen: ConversationScreen, key: ChildKey) -> some View {
        #if os(macOS)
        if let picker = screen.picker {
            navigationSubtitle(screen.subtitle).toolbarTitleMenu { AgentChoices(key: key, current: picker.current) }
        } else {
            navigationSubtitle(screen.subtitle)
        }
        #else
        // The bar's middle is `AgentTitle`, which names the assistant: a
        // subtitle would stand under it as a third line.
        navigationSubtitle(screen.picker == nil ? screen.subtitle : "")
        #endif
    }
}
