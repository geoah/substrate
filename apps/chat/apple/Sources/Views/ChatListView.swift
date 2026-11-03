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
                Section(section.title) {
                    ForEach(section.rows) { row in
                        ThreadRowView(row: row)
                            .tag(row.id)
                            .contextMenu { menu(row) }
                            .swipeActions(edge: .leading) {
                                if row.unread {
                                    Button("Read", systemImage: "envelope.open") { host.send(.markRead(row.id)) }
                                        .tint(.accentColor)
                                } else {
                                    Button("Unread", systemImage: "envelope.badge") { host.send(.markUnread(row.id)) }
                                        .tint(.accentColor)
                                }
                            }
                            .swipeActions(edge: .trailing) {
                                Button("Rename", systemImage: "pencil") { beginRename(row) }
                                    .tint(.gray)
                            }
                    }
                }
            }
            if let nothing = searched?.nothingFound {
                Text(nothing)
                    .foregroundStyle(.secondary)
                    .listRowSeparator(.hidden)
            }
        }
        .listStyle(.sidebar)
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
                    Toggle("Automated Runs", systemImage: "bolt", isOn: Binding(
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
                    ReviewChangesSheet(changingAccessFor: ChatManifest.manifest, session: session, changes: changes,
                                       onContinue: continueReview, onCancel: { isPresented = false })
                } else {
                    ReviewChangesSheet(appName: ChatManifest.appName, host: ServerAddress.display(session.server), changes: changes,
                                       firstSignIn: false, onContinue: continueReview, onCancel: { isPresented = false })
                }
            } else {
                ProgressView().padding(40)
            }
        }
        .task {
            guard let session = SessionStore.chat.load() else { return }
            let status = await host.model.runtime.state.status
            changes = ChatManifest.manifest.review(status, session: session)
        }
    }

    private func continueReview() {
        isPresented = false
        host.reauthorize()
    }
}

struct ThreadRowView: View {
    @Environment(\.colorScheme) private var scheme
    let row: ThreadRow

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                if let mode = row.mode {
                    Image(systemName: Self.symbol(mode))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .accessibilityLabel(mode)
                }
                Text(row.title)
                    .font(.body.weight(row.unread ? .semibold : .medium))
                    .lineLimit(1)
                Spacer(minLength: 4)
                Text(row.stamp)
                    .font(.caption)
                    .foregroundStyle(row.unread ? Color.accentColor : .secondary)
                    .monospacedDigit()
            }
            HStack(alignment: .top, spacing: 6) {
                preview
                    .font(.subheadline)
                    .lineLimit(2)
                Spacer(minLength: 4)
                switch row.mark {
                case .attention?:
                    Image(systemName: "questionmark.circle.fill")
                        .foregroundStyle(.orange)
                        .accessibilityLabel("Waiting on you")
                case .failed?:
                    Image(systemName: "exclamationmark.circle.fill")
                        .foregroundStyle(.red)
                        .accessibilityLabel("Ended in an error")
                case .unread?:
                    Circle().fill(Color.accentColor).frame(width: 9, height: 9).padding(.top, 5)
                        .accessibilityLabel("Unread")
                case nil:
                    EmptyView()
                }
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    /// The agent's name in its colour, then the start of the last reply:
    /// one run of text, so the name costs a word, not a column.
    private var preview: Text {
        let name = Text(row.agentName).fontWeight(.semibold).foregroundStyle(agentTint(row.agent, dark: scheme == .dark))
        guard !row.preview.isEmpty else { return name }
        return Text("\(name) \(Text(row.preview).foregroundStyle(.secondary))")
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

func copyToPasteboard(_ text: String) {
    #if os(macOS)
    NSPasteboard.general.clearContents()
    NSPasteboard.general.setString(text, forType: .string)
    #else
    UIPasteboard.general.string = text
    #endif
}
