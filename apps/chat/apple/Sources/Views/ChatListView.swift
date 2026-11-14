import ChatKit
import SubstrateKit
import SwiftUI

/// The chats, newest activity first, under day headings.
struct ChatListView: View {
    @Environment(ChatHost.self) private var host
    let scene: String
    @Binding var showSettings: Bool
    @State private var query = ""
    @State private var reviewing = false
    @State private var renaming: ThreadRow?
    @State private var newTitle = ""
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var sizeClass
    #endif

    private var chats: ChatPresentation? { host.chats }

    private var selection: Binding<String?> {
        Binding {
            chats?.scenes[scene]?.current.flatMap { chats?.conversations[$0]?.thread }
        } set: { id in
            if let id { host.send(.show(thread: id, scene: scene)) }
        }
    }

    var body: some View {
        let list = chats?.list
        let searched = chats?.scenes[scene]
        let sections = searched?.found ?? list?.sections ?? []
        List(selection: selection) {
            if let banner = host.link.banner {
                AccessBannerView(banner) { perform($0) }
                    // The banner's buttons are words in the accent, not
                    // words on it: the text tone, not the list's fill.
                    .tint(ChatColours.accent.color)
                    .listRowSeparator(.hidden)
            }
            if let problem = host.authorizeProblem {
                ProblemBox(title: "Access didn't change", message: problem, tint: .orange)
                    .listRowSeparator(.hidden)
            }
            if let offline = list?.offline {
                Label(offline, systemImage: "wifi.slash")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .listRowSeparator(.hidden)
            } else if let problem = list?.problem {
                ProblemBox(message: problem, tint: .orange)
                    .listRowSeparator(.hidden)
            }
            if let problem = chats?.renameProblem {
                ProblemBox(title: "Couldn't rename", message: problem, tint: .orange)
                    .listRowSeparator(.hidden)
            }
            ForEach(sections) { section in
                Section {
                    ForEach(section.rows) { row in
                        ThreadRowView(row: row, selected: selection.wrappedValue == row.id)
                            .tag(row.id)
                            .contextMenu { menu(row) }
                            .swipeActions(edge: .leading) {
                                if row.unread {
                                    Button("Read", systemImage: "envelope.open") { host.send(.markRead(row.id)) }
                                        .tint(.accentFill)
                                } else {
                                    Button("Unread", systemImage: "envelope.badge") { host.send(.markUnread(row.id)) }
                                        .tint(.accentFill)
                                }
                            }
                            .swipeActions(edge: .trailing) {
                                Button("Rename", systemImage: "pencil") { beginRename(row) }
                                    .tint(.gray)
                            }
                    }
                } header: {
                    // The system's header grey is 3.2:1 on the list.
                    Text(section.title).foregroundStyle(.quietText)
                }
            }
            if let nothing = searched?.nothingFound {
                Text(nothing)
                    .foregroundStyle(.secondary)
                    .listRowSeparator(.hidden)
            }
        }
        .listStyle(.sidebar)
        // An iPad fills the focused selection with the list's tint. The
        // accent in dark mode is a text tone, and the system's white words
        // are 2.8:1 on it, so the list's tint is the accent as a fill, the
        // same purple in both modes. A Mac's source list fills it with the
        // system's own tone of the accent, whatever the tint.
        .tint(ChatColours.selectionTint)
        .overlay {
            if query.isEmpty, let empty = list?.empty {
                ContentUnavailableView {
                    Label(empty.title, systemImage: "bubble.left.and.text.bubble.right")
                } description: {
                    Text(empty.message)
                } actions: {
                    if let action = empty.action {
                        Button(action) { host.send(.newChat(agent: nil, listen: false, scene: scene)) }
                            .buttonStyle(.glassProminent)
                            .tint(.accentFill)
                    }
                }
            } else if query.isEmpty, list?.loading != false {
                ProgressView()
            }
        }
        .searchable(text: $query, prompt: list?.searchPrompt ?? "Search chats")
        .onChange(of: query) { _, query in host.send(.search(scene: scene, query: query)) }
        .refreshable { host.send(.refresh) }
        .navigationTitle(list?.title ?? "Chats")
        .toolbar {
            #if os(iOS)
            ToolbarItem(placement: .topBarLeading) {
                Button("Settings", systemImage: "gearshape") { showSettings = true }
            }
            #endif
            ToolbarItem(placement: .primaryAction) {
                Menu("Show", systemImage: "line.3.horizontal.decrease") {
                    Toggle(
                        "Automated Runs", systemImage: "bolt",
                        isOn: Binding(
                            get: { list?.showAutomated ?? false },
                            set: { host.send(.settings(.toggle(row: .app("showAutomated"), on: $0))) }))
                }
            }
            #if os(iOS)
            if sizeClass == .compact {
                // A phone's search field sits in the bottom bar, and New
                // Chat beside it, under the thumb.
                DefaultToolbarItem(kind: .search, placement: .bottomBar)
                ToolbarSpacer(.fixed, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) { newChat }
            } else {
                ToolbarSpacer(.fixed, placement: .primaryAction)
                ToolbarItem(placement: .primaryAction) { newChat }
            }
            #else
            ToolbarSpacer(.fixed, placement: .primaryAction)
            ToolbarItem(placement: .primaryAction) { newChat }
            #endif
        }
        .sheet(isPresented: $reviewing) { ReviewSheet(isPresented: $reviewing) }
        .alert("Rename Chat", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
            TextField("Title", text: $newTitle)
            Button("Cancel", role: .cancel) { renaming = nil }
            Button("Rename") { commitRename() }
        }
    }

    private var newChat: some View {
        // ⌘N is the File menu's.
        Button("New Chat", systemImage: "square.and.pencil") { host.send(.newChat(agent: nil, listen: false, scene: scene)) }
    }

    private func perform(_ action: AccessAction) {
        switch action {
        case .review: reviewing = true
        case .signInAgain: host.signInAgain()
        case .retry: host.model.send(.refresh(force: true))
        case let .openFix(path):
            if let server = SessionStore.chat.load()?.server, let url = ServerAddress.page(server, path: path) { openURL(url) }
        case .moveToAppAccess: break
        }
    }

    @Environment(\.openURL) private var openURL

    @ViewBuilder
    private func menu(_ row: ThreadRow) -> some View {
        Button("Rename…", systemImage: "pencil") { beginRename(row) }
        if row.unread {
            Button("Mark as Read", systemImage: "envelope.open") { host.send(.markRead(row.id)) }
        } else {
            Button("Mark as Unread", systemImage: "envelope.badge") { host.send(.markUnread(row.id)) }
        }
        Button("Copy Thread ID", systemImage: "doc.on.doc") { copyToPasteboard(row.id) }
    }

    private func beginRename(_ row: ThreadRow) {
        newTitle = row.subject
        renaming = row
    }

    private func commitRename() {
        guard let row = renaming else { return }
        renaming = nil
        host.send(.rename(thread: row.id, subject: newTitle))
    }
}

/// The review of what changed, then the approval page again: what the
/// link's last check found, and what the owner left out last time, offered
/// back.
struct ReviewSheet: View {
    @Environment(ChatHost.self) private var host
    @Binding var isPresented: Bool
    var changingAccess = false
    @State private var changes: [Change]?

    var body: some View {
        Group {
            if let session = SessionStore.chat.load(), let changes {
                if changingAccess {
                    ReviewChangesSheet(
                        changingAccessFor: ChatManifest.manifest, session: session, changes: changes,
                        onContinue: continueReview, onCancel: { isPresented = false })
                } else {
                    ReviewChangesSheet(
                        appName: ChatManifest.appName, host: ServerAddress.display(session.server), changes: changes,
                        firstSignIn: false, onContinue: continueReview, onCancel: { isPresented = false })
                }
            } else {
                ProgressView().padding(40)
            }
        }
        .task {
            guard let session = await SessionStore.chat.load() else { return }
            let status = await host.model.runtime.state.status
            changes = ChatManifest.manifest.review(status, session: session)
        }
    }

    private func continueReview() {
        isPresented = false
        host.reauthorize()
    }
}

/// One chat in the list. Its words are chosen for the fill behind them. On
/// the list's own background each has its colour: the agent's, the accent
/// for an unread date, quiet for the rest. On the selection nothing keeps a
/// colour of its own: every word is the system's primary style, which the
/// system draws white on the selection's fill while the list has the focus
/// and in the label colour on the grey it is while the focus is elsewhere.
/// So the row never has to know which of the two is behind it, and a
/// colour chosen for the list's background is never left on either.
struct ThreadRowView: View {
    let row: ThreadRow
    /// The row is the list's selection, focused or not.
    var selected = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                if let mode = row.mode {
                    Image(systemName: Self.symbol(mode))
                        .font(.caption)
                        .foregroundStyle(quiet)
                        .accessibilityLabel(mode)
                }
                Text(row.title)
                    .font(.body.weight(row.unread ? .semibold : .medium))
                    .lineLimit(1)
                Spacer(minLength: 4)
                Text(row.stamp)
                    .font(.caption)
                    .foregroundStyle(row.unread ? own(ChatColours.accent.color) : quiet)
                    .monospacedDigit()
            }
            HStack(alignment: .top, spacing: 6) {
                preview
                    .font(.subheadline)
                    .lineLimit(2)
                Spacer(minLength: 4)
                if let unfinished = row.unfinished {
                    UnfinishedPill(mark: unfinished, selected: selected)
                }
                switch row.mark {
                case .attention?:
                    Image(systemName: "questionmark.circle.fill")
                        .foregroundStyle(own(.orange))
                        .accessibilityLabel("Waiting on you")
                case .unread?:
                    Circle().fill(own(ChatColours.accent.color)).frame(width: 9, height: 9).padding(.top, 5)
                        .accessibilityLabel("Unread")
                case nil:
                    EmptyView()
                }
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    /// A colour of the row's own on the list, and the system's primary
    /// style on the selection.
    private func own(_ color: Color) -> AnyShapeStyle {
        selected ? AnyShapeStyle(.primary) : AnyShapeStyle(color)
    }

    /// The preview, the date and the glyph of an automated run.
    private var quiet: AnyShapeStyle { own(.quietText) }

    /// The agent's name in its colour, then the start of the last reply:
    /// one run of text, so the name costs a word, not a column.
    private var preview: Text {
        let name = Text(row.agentName).fontWeight(.semibold).foregroundStyle(own(AgentColours(row.agent).ink))
        guard !row.preview.isEmpty else { return name }
        return Text("\(name) \(Text(row.preview).foregroundStyle(quiet))")
    }

    private static func symbol(_ mode: String) -> String {
        switch mode {
        case "schedule": "clock"
        case "webhook": "antenna.radiowaves.left.and.right"
        case "record": "bolt"
        default: "gearshape.2"
        }
    }
}

/// The mark of a chat whose last turn didn't finish: "Didn’t finish",
/// "Stopped" or "Failed". On the list it is its tone's words on its tone's
/// pale fill. On the selection it is an outline in the row's own style with
/// no fill, since a fill would take from what contrast the words have
/// there.
private struct UnfinishedPill: View {
    let mark: ThreadRow.Unfinished
    let selected: Bool

    var body: some View {
        Text(mark.words)
            .font(.caption2.weight(.semibold))
            .foregroundStyle(selected ? AnyShapeStyle(.primary) : AnyShapeStyle(tone.textColor))
            .padding(.horizontal, 7)
            .padding(.vertical, 2)
            .background(tone.color.opacity(selected ? 0 : ChatColours.badge), in: .capsule)
            .overlay {
                if selected { Capsule().strokeBorder(.primary.opacity(0.6), lineWidth: 1) }
            }
            .fixedSize()
            .padding(.top, 2)
    }

    private var tone: ScreenTone {
        switch mark.tone {
        case .bad: .critical
        case .warn: .caution
        case .quiet: .quiet
        }
    }
}

func copyToPasteboard(_ text: String) {
    #if os(macOS)
    NSPasteboard.general.clearContents()
    NSPasteboard.general.setString(text, forType: .string)
    #else
    UIPasteboard.general.string = text
    #endif
}
