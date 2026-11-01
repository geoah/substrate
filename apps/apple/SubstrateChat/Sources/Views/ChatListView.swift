import SwiftUI
import SubstrateKit

/// The chats, newest activity first, under day headings.
struct ChatListView: View {
    @Environment(AppModel.self) private var model
    @Environment(ChatStore.self) private var store
    @Environment(SceneModel.self) private var scene
    @State private var query = ""
    @State private var reviewing = false
    @State private var renaming: ChatThread?
    @State private var newTitle = ""
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var sizeClass
    #endif

    private var selection: Binding<String?> {
        Binding {
            scene.current?.threadID
        } set: { id in
            if let id { scene.open(store.conversation(thread: id)) }
        }
    }

    private var shown: [ChatThread] {
        let all = store.threads
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { return all }
        return all.filter { thread in
            store.title(thread).localizedCaseInsensitiveContains(q)
                || (thread.preview?.localizedCaseInsensitiveContains(q) ?? false)
                || (store.rows[thread.id]?.contains { $0["content"]?.string?.localizedCaseInsensitiveContains(q) ?? false } ?? false)
        }
    }

    var body: some View {
        let threads = shown
        let groups = DayGroup.allCases.compactMap { day -> (DayGroup, [ChatThread])? in
            let items = threads.filter { DayGroup.of($0.activeAt) == day }
            return items.isEmpty ? nil : (day, items)
        }
        List(selection: selection) {
            if !model.issues.isEmpty {
                AppStatusBanner(issues: model.issues, appName: ChatManifest.appName, host: host,
                                actions: AppStatusActions(review: { reviewing = true }, signInAgain: { model.signInAgain() }))
                    .listRowSeparator(.hidden)
            }
            if let problem = model.authorizeProblem {
                ProblemLine(title: "Access didn't change", message: problem, tint: .orange)
                    .listRowSeparator(.hidden)
            }
            if !store.online {
                Label("Offline. Showing saved chats.", systemImage: "wifi.slash")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .listRowSeparator(.hidden)
            } else if let problem = store.refreshError {
                ProblemLine(message: "Couldn't refresh: \(problem)", tint: .orange)
                    .listRowSeparator(.hidden)
            }
            ForEach(groups, id: \.0) { day, items in
                Section(day.rawValue) {
                    ForEach(items) { thread in
                        ThreadRow(thread: thread)
                            .tag(thread.id)
                            .contextMenu { menu(thread) }
                            .swipeActions(edge: .leading) {
                                if store.unread(thread) {
                                    Button("Read", systemImage: "envelope.open") { store.markSeen(thread.id) }
                                        .tint(.accentColor)
                                } else {
                                    Button("Unread", systemImage: "envelope.badge") { store.markUnread(thread.id) }
                                        .tint(.accentColor)
                                }
                            }
                            .swipeActions(edge: .trailing) {
                                Button("Rename", systemImage: "pencil") { beginRename(thread) }
                                    .tint(.gray)
                            }
                    }
                }
            }
            if !query.isEmpty, threads.isEmpty {
                Text("No chat on this device matches “\(query)”.")
                    .foregroundStyle(.secondary)
                    .listRowSeparator(.hidden)
            }
        }
        .listStyle(.sidebar)
        .overlay {
            if store.threads.isEmpty, query.isEmpty {
                if store.loaded {
                    ContentUnavailableView {
                        Label("No chats yet", systemImage: "bubble.left.and.text.bubble.right")
                    } description: {
                        Text(store.offered.isEmpty
                            ? "There is no assistant to chat with yet. Install a package that ships one from the console's Providers page."
                            : "Start one with \(store.defaultAgent?.name ?? "an assistant").")
                    } actions: {
                        if !store.offered.isEmpty {
                            Button("New Chat") { scene.open(store.newConversation()) }
                                .buttonStyle(.glassProminent)
                        }
                    }
                } else {
                    ProgressView()
                }
            }
        }
        .searchable(text: $query, prompt: "Search chats")
        .refreshable { await store.refreshAll() }
        .navigationTitle("Chats")
        .toolbar {
            #if os(iOS)
            ToolbarItem(placement: .topBarLeading) {
                Button("Settings", systemImage: "gearshape") { scene.showSettings = true }
            }
            #endif
            ToolbarItem(placement: .primaryAction) {
                Menu("Show", systemImage: "line.3.horizontal.decrease") {
                    Toggle("Automated Runs", systemImage: "bolt", isOn: Binding(
                        get: { store.prefs.showAutomated },
                        set: { store.prefs.showAutomated = $0
                            store.refresh()
                        }))
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
        .sheet(isPresented: $reviewing) {
            ReviewChangesSheet(appName: ChatManifest.appName, host: host, changes: model.status.status?.changes ?? [],
                               firstSignIn: false,
                               onContinue: {
                                   reviewing = false
                                   Task { await model.reauthorize() }
                               },
                               onCancel: { reviewing = false })
        }
        .alert("Rename Chat", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
            TextField("Title", text: $newTitle)
            Button("Cancel", role: .cancel) { renaming = nil }
            Button("Rename") { commitRename() }
        }
    }

    private var host: String { ServerAddress.display(store.session.server) }

    private var newChat: some View {
        // ⌘N is the File menu's.
        Button("New Chat", systemImage: "square.and.pencil") { scene.open(store.newConversation()) }
    }

    @ViewBuilder
    private func menu(_ thread: ChatThread) -> some View {
        Button("Rename…", systemImage: "pencil") { beginRename(thread) }
        if store.unread(thread) {
            Button("Mark as Read", systemImage: "envelope.open") { store.markSeen(thread.id) }
        } else {
            Button("Mark as Unread", systemImage: "envelope.badge") { store.markUnread(thread.id) }
        }
        Button("Copy Thread ID", systemImage: "doc.on.doc") { copyToPasteboard(thread.id) }
    }

    private func beginRename(_ thread: ChatThread) {
        newTitle = thread.subject ?? store.title(thread)
        renaming = thread
    }

    private func commitRename() {
        guard let thread = renaming else { return }
        renaming = nil
        let title = newTitle
        Task { try? await store.rename(thread.id, to: title) }
    }
}

struct ThreadRow: View {
    @Environment(ChatStore.self) private var store
    @Environment(\.colorScheme) private var scheme
    let thread: ChatThread

    var body: some View {
        let unread = store.unread(thread)
        let attention = store.attention(thread.id)
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                if thread.automated {
                    Image(systemName: modeSymbol)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .accessibilityLabel(thread.mode)
                }
                Text(store.title(thread))
                    .font(.body.weight(unread ? .semibold : .medium))
                    .lineLimit(1)
                Spacer(minLength: 4)
                Text(thread.activeAt.listStamp)
                    .font(.caption)
                    .foregroundStyle(unread ? Color.accentColor : .secondary)
                    .monospacedDigit()
            }
            HStack(alignment: .top, spacing: 6) {
                preview
                    .font(.subheadline)
                    .lineLimit(2)
                Spacer(minLength: 4)
                if attention > 0 {
                    Image(systemName: "questionmark.circle.fill")
                        .foregroundStyle(.orange)
                        .accessibilityLabel("Waiting on you")
                } else if thread.status == "error" {
                    Image(systemName: "exclamationmark.circle.fill")
                        .foregroundStyle(.red)
                        .accessibilityLabel("Ended in an error")
                } else if unread {
                    Circle().fill(Color.accentColor).frame(width: 9, height: 9).padding(.top, 5)
                        .accessibilityLabel("Unread")
                }
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    /// The agent's name in its colour, then the start of the last reply:
    /// one run of text, so the name costs a word, not a column.
    private var preview: Text {
        let name = Text(agentName(thread.agentID ?? "")).fontWeight(.semibold)
            .foregroundStyle(agentTint(thread.agentID, dark: scheme == .dark))
        let rest = thread.running ? "Working…" : (thread.preview ?? "")
        guard !rest.isEmpty else { return name }
        return Text("\(name) \(Text(rest).foregroundStyle(.secondary))")
    }

    private var modeSymbol: String {
        switch thread.mode {
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
