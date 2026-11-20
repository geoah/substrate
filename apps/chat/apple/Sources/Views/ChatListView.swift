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
    /// The microphone held in the list: the new chat opens once it is let go.
    @State private var dictation = Dictation()
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var sizeClass
    #endif

    private var chats: ChatSurface? { host.chats }

    private var selection: Binding<String?> {
        Binding {
            chats?.scenes[scene]?.thread
        } set: { id in
            if let id { host.send(.show(thread: id, scene: scene)) }
        }
    }

    var body: some View {
        let _ = Meter.shared.count(.body, "ChatList")
        let list = chats?.list
        let searched = chats?.scenes[scene]
        // The window lists the archived chats, a search's finds, or the
        // chats: one of the three.
        let shelf = searched?.archived == true ? list?.archived : nil
        let sections = shelf.map { [DaySection(title: "", rows: $0.rows)] } ?? searched?.found ?? list?.sections ?? []
        let flags = list?.flags == true
        List(selection: selection) {
            if let banner = host.link.banner {
                AccessBannerView(banner) { perform($0) }
                    // The banner's buttons are words in the accent, not
                    // words on it: the text tone, not the list's fill.
                    .tinted(.accent, on: .list)
                    .listRowSeparator(.hidden)
            }
            if let problem = host.authorizeProblem {
                ProblemBox(title: "Access didn't change", message: problem, tint: ScreenTone.caution.color)
                    .listRowSeparator(.hidden)
            }
            if let offline = list?.offline {
                Label(offline, systemImage: "wifi.slash")
                    .font(ChatType.detail)
                    .ink(.quiet, on: .list)
                    .listRowSeparator(.hidden)
            } else if let failure = list?.failure {
                // The list's one failure: a strip at its top.
                FailureView(failure: failure) { FailureActions(host: host, scene: scene, openURL: openURL)($0) }
                    .listRowSeparator(.hidden)
                    .listRowBackground(Color.clear)
            }
            // Rows stand in for the chats while the first read is out.
            if shelf == nil, query.isEmpty, let placeholders = list?.placeholders, placeholders > 0 {
                PlaceholderRows(count: placeholders)
            }
            ForEach(sections) { section in
                Section {
                    ForEach(section.rows) { row in
                        ThreadRowView(row: row, selected: selection.wrappedValue == row.id)
                            .tag(row.id)
                            .contextMenu { menu(row, flags: flags) }
                            .swipeActions(edge: .leading) {
                                if row.unread {
                                    Button("Read", systemImage: "envelope.open") { host.send(.markRead(row.id)) }
                                        .prominentTint()
                                } else {
                                    Button("Unread", systemImage: "envelope.badge") { host.send(.markUnread(row.id)) }
                                        .prominentTint()
                                }
                                if flags { PinButton(row: row, short: true) }
                            }
                            .swipeActions(edge: .trailing) {
                                if flags { ArchiveButton(row: row, short: true) }
                                Button("Rename", systemImage: "pencil") { beginRename(row) }
                                    .tint(ScreenTone.quiet.fillColor)
                            }
                    }
                } header: {
                    // The system's header grey is 3.2:1 on the list.
                    if !section.title.isEmpty { SectionTitle(title: section.title) }
                }
            }
            if let shelf {
                ShelfFooter(shelf: shelf)
            } else if searched?.found == nil, list?.more == true {
                // The list goes on: its next page is read when its end
                // comes on screen.
                MoreRow(loading: list?.loadingMore == true, title: "Earlier chats") { host.send(.more(archived: false)) }
            }
            if let groups = searched?.messages, !groups.isEmpty {
                SearchGroups(groups: groups, scene: scene)
            }
            if searched?.searching == true {
                SearchingRow()
            }
            if let nothing = searched?.nothingFound {
                Text(nothing)
                    .ink(.quiet, on: .list)
                    .listRowSeparator(.hidden)
            }
        }
        .listStyle(.sidebar)
        // A row takes its new place with motion when the order changes: a
        // chat that answered, one pinned, one archived. With none where
        // the device asks for less.
        .motion(.place, value: sections.map { $0.rows.map(\.id) })
        // An iPad fills the focused selection with the list's tint. The
        // accent in dark mode is a text tone, and the system's white words
        // are 2.8:1 on it, so the list's tint is the accent as a fill, the
        // same purple in both modes. A Mac's source list fills it with the
        // system's own tone of the accent, whatever the tint.
        .tint(ChatColours.selectionTint)
        .overlay {
            if let empty = shelf?.empty {
                ContentUnavailableView("No Archived Chats", systemImage: "archivebox", description: Text(empty))
            } else if shelf != nil {
                EmptyView()
            } else if query.isEmpty, let empty = list?.empty {
                ContentUnavailableView {
                    Label(empty.title, systemImage: "bubble.left.and.text.bubble.right")
                } description: {
                    Text(empty.message)
                } actions: {
                    if let action = empty.action {
                        Button(action) { host.send(.newChat(agent: nil, listen: false, scene: scene)) }
                            .buttonStyle(.glassProminent)
                            .prominentTint()
                    }
                }
            }
        }
        // The chat archived a moment ago, and Undo, for a few seconds.
        .safeAreaInset(edge: .bottom) {
            VStack(spacing: ChatSpace.items) {
                if dictation.active || dictation.problem != nil {
                    // A problem stands until it is tapped away, or the
                    // microphone is tried again.
                    ListeningBar(dictation: dictation)
                        .onTapGesture { if !dictation.active { dictation.problem = nil } }
                        .arrives()
                }
                if let undo = list?.undo { UndoBar(undo: undo) }
            }
        }
        .onDisappear { dictation.cancel() }
        .motion(.arrive, value: list?.undo)
        .searchable(text: $query, prompt: list?.searchPrompt ?? "Search chats")
        .onChange(of: query) { _, query in host.send(.search(scene: scene, query: query)) }
        .refreshable { host.send(.refresh) }
        .navigationTitle(shelf?.title ?? list?.title ?? "Chats")
        .toolbar {
            // Back from the archived chats to the chats, where Settings
            // stands on a phone and a tablet.
            #if os(iOS)
            ToolbarItem(placement: .topBarLeading) {
                if shelf != nil {
                    Button("Chats", systemImage: "chevron.backward") { host.send(.showArchived(scene: scene, shown: false)) }
                } else {
                    Button("Settings", systemImage: "gearshape") { showSettings = true }
                }
            }
            #else
            if shelf != nil {
                ToolbarItem(placement: .navigation) {
                    Button("Chats", systemImage: "chevron.backward") { host.send(.showArchived(scene: scene, shown: false)) }
                }
            }
            #endif
            ToolbarItem(placement: .primaryAction) {
                Menu("Show", systemImage: "line.3.horizontal.decrease") {
                    Toggle(
                        "Automated Runs", systemImage: "bolt",
                        isOn: Binding(
                            get: { list?.showAutomated ?? false },
                            set: { host.send(.settings(.toggle(row: .app("showAutomated"), on: $0))) }))
                    // Offered by what the server keeps, not by having read
                    // an archived chat: with every chat archived the list
                    // is empty.
                    if flags {
                        Toggle(
                            "Archived Chats", systemImage: "archivebox",
                            isOn: Binding(get: { shelf != nil }, set: { host.send(.showArchived(scene: scene, shown: $0)) }))
                    }
                }
            }
            #if os(iOS)
            if sizeClass == .compact {
                // A phone's search field sits in the bottom bar, and New
                // Chat beside it, under the thumb.
                DefaultToolbarItem(kind: .search, placement: .bottomBar)
                ToolbarSpacer(.fixed, placement: .bottomBar)
                ToolbarItem(placement: .bottomBar) { microphone }
                ToolbarItem(placement: .bottomBar) { newChat }
            } else {
                ToolbarSpacer(.fixed, placement: .primaryAction)
                ToolbarItem(placement: .primaryAction) { microphone }
                ToolbarItem(placement: .primaryAction) { newChat }
            }
            #else
            ToolbarSpacer(.fixed, placement: .primaryAction)
            ToolbarItem(placement: .primaryAction) { microphone }
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

    /// Talk to the assistant from the list: a tap opens a new chat
    /// listening, a hold listens here and opens it with what was heard
    /// once let go, sent where Settings says a hold talks.
    private var microphone: some View {
        let settings = chats?.settings
        let hold = settings?.micHold ?? .talk
        return Image(systemName: dictation.active ? "waveform" : "mic")
            .frame(width: ChatSize.target, height: ChatSize.target)
            .tapOrHold(
                holds: hold != .off,
                tap: {
                    host.voice.talks = settings?.micTap == .talk
                    host.send(.newChat(agent: nil, listen: true, scene: scene))
                },
                holdBegan: { Task { await dictation.start(localeID: settings?.voiceLocale) } },
                holdEnded: { Task { await released(sends: hold == .talk) } }
            )
            .accessibilityLabel("Talk")
            .accessibilityHint(hold == .off ? "Starts a new chat, listening" : "Hold to talk, and let go to send")
    }

    /// The held microphone let go: a new chat opens with what was heard.
    private func released(sends: Bool) async {
        let heard = await dictation.stop()
        guard !heard.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        host.voice.heard = heard
        host.voice.sends = sends
        host.send(.newChat(agent: nil, listen: false, scene: scene))
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
    private func menu(_ row: ThreadRow, flags: Bool) -> some View {
        if flags {
            PinButton(row: row)
            ArchiveButton(row: row)
            Divider()
        }
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
                ProgressView().padding(ChatSpace.screen)
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
    @Scaled(.dot) private var dot
    @Environment(\.dynamicTypeSize) private var size

    var body: some View {
        let _ = Meter.shared.count(.body, "ThreadRow")
        VStack(alignment: .leading, spacing: ChatSpace.mark) {
            if size.isAccessibilitySize {
                // Each on a line of its own: the title in full, when under
                // it, the preview, then what state the chat is in.
                title.lineLimit(2)
                stamp
                preview.font(ChatType.detail).lineLimit(3)
                HStack(spacing: ChatSpace.words) { marks }
            } else {
                HStack(alignment: .firstTextBaseline, spacing: ChatSpace.words) {
                    title.lineLimit(1)
                    Spacer(minLength: ChatSpace.mark)
                    stamp
                }
                HStack(alignment: .top, spacing: ChatSpace.words) {
                    preview
                        .font(ChatType.detail)
                        .lineLimit(2)
                    Spacer(minLength: ChatSpace.mark)
                    marks
                }
            }
        }
        .padding(.vertical, ChatSpace.pair)
        .accessibilityElement(children: .combine)
    }

    /// The glyph of an automated run, then the chat's title.
    private var title: some View {
        HStack(alignment: .firstTextBaseline, spacing: ChatSpace.words) {
            if let mode = row.mode {
                Image(systemName: Self.symbol(mode))
                    .font(ChatType.caption)
                    .ink(.quiet, on: .list, selected: selected)
                    .accessibilityLabel(mode)
            }
            Text(row.title)
                .font(ChatType.rowTitle(unread: row.unread))
        }
    }

    private var stamp: some View {
        Text(row.stamp)
            .font(ChatType.caption)
            .ink(row.unread ? .accent : .quiet, on: .list, selected: selected)
            .monospacedDigit()
    }

    /// What state the chat is in: a turn that did not finish, a card that
    /// waits, unread, at work.
    @ViewBuilder private var marks: some View {
        if let unfinished = row.unfinished {
            UnfinishedPill(mark: unfinished, selected: selected)
        }
        switch row.mark {
        case .attention?:
            Image(systemName: "questionmark.circle.fill")
                .ink(.mark(.caution), on: .list, selected: selected)
                .accessibilityLabel("Waiting on you")
        case .unread?:
            Circle().ink(.accent, on: .list, selected: selected).frame(width: dot, height: dot).padding(.top, ChatSpace.mark)
                .accessibilityLabel("Unread")
        case nil:
            // A chat at work says so where its mark would stand.
            if row.running { RunningMark(selected: selected) }
        }
    }

    /// The agent's name in its colour, then the start of the last reply:
    /// one run of text, so the name costs a word, not a column. On the
    /// selection both are the system's primary style, as every word of the
    /// row is.
    private var preview: Text {
        let name = Text(row.agentName).fontWeight(ChatType.strong).inAgentInk(row.agent, on: .list, selected: selected)
        guard !row.preview.isEmpty else { return name }
        return Text("\(name) \(Text(row.preview).ink(.quiet, on: .list, selected: selected))")
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
            .font(ChatType.smallLabel)
            .ink(mark.tone == .bad ? .tone(.critical) : mark.tone == .warn ? .tone(.caution) : .tone(.quiet), on: .badge, selected: selected)
            .padding(.horizontal, ChatSpace.pillSide)
            .padding(.vertical, ChatSpace.pillEdge)
            .background(ChatColours.badgeStyle(tone).opacity(selected ? 0 : 1), in: .capsule)
            .overlay {
                if selected { Capsule().strokeBorder(ChatColours.Ink.label.color.opacity(ChatColours.outline), lineWidth: ChatLine.rule) }
            }
            .fixedSize()
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

/// What the list's held microphone is hearing, over the bottom bar.
private struct ListeningBar: View {
    let dictation: Dictation
    @Scaled(.control) private var control

    var body: some View {
        HStack(alignment: .center, spacing: ChatSpace.apart) {
            LevelMeter(level: dictation.level)
            Group {
                if !dictation.active, let problem = dictation.problem {
                    Text(problem).ink(.tone(.critical), on: .field)
                } else if dictation.heard.isEmpty {
                    Text("Listening…").ink(.quiet, on: .field)
                } else {
                    Text("\(Text(dictation.finalized))\(Text(dictation.volatile).ink(.quiet, on: .field))")
                }
            }
            .lineLimit(1 ... 4)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, ChatSpace.edge)
        .frame(minHeight: control)
        .glassEffect(.regular, in: .rect(cornerRadius: ChatRadius.bubble))
        .padding(.horizontal, ChatSpace.screen)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.updatesFrequently)
    }
}
