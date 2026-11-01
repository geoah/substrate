import SwiftUI

/// One chat: the transcript, what became of the last send, and the composer
/// floating over it.
struct ConversationView: View {
    @Environment(ChatStore.self) private var store
    @Environment(SceneModel.self) private var scene
    @Environment(\.scenePhase) private var phase
    @Bindable var conversation: Conversation
    @State private var dictation = Dictation()
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var atBottom = true
    @State private var keySheet: Provider?
    @State private var renaming = false
    @State private var newTitle = ""
    @State private var titleIndex = TitleIndex()

    private var thread: ChatThread? { store.thread(conversation.threadID) }

    private var title: String {
        if let thread { return store.title(thread) }
        return conversation.threadID == nil ? "New Chat" : "Chat"
    }

    private var subtitle: String {
        if conversation.streaming || conversation.phase == .background { return "\(conversation.name) is working…" }
        return "with \(conversation.name)"
    }

    var body: some View {
        let turns = conversation.turns
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 22) {
                if conversation.threadID == nil, turns.isEmpty {
                    NewChatHero(conversation: conversation)
                }
                if let id = conversation.threadID, (store.rows[id]?.count ?? 0) >= 500 {
                    Label("Older messages aren't shown here.", systemImage: "clock.arrow.circlepath")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity)
                }
                let decided = Set(turns.compactMap { decisionNotice($0)?.requestID })
                let proposals = proposalCalls(turns)
                ForEach(turns) { turn in
                    TurnView(turn: turn, agent: conversation.agent, live: turn.id == conversation.liveKey, decided: decided,
                             proposal: decisionNotice(turn).flatMap { proposals[$0.requestID] })
                        .equatable()
                }
                if conversation.awaitingFirstEvent {
                    TypingDots()
                }
                ConversationStatus(conversation: conversation)
                if store.prefs.technicalDetails, let thread {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("\(Kinds.thread)/\(thread.id)").textSelection(.enabled)
                        Text(thread.tally.joined(separator: " · "))
                    }
                    .font(.caption2.monospaced())
                    .foregroundStyle(.tertiary)
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 12)
            .padding(.bottom, 16)
            .environment(\.recordTitles, titleIndex.titles(for: turns))
            .frame(maxWidth: 780)
            .frame(maxWidth: .infinity)
        }
        .scrollPosition($position)
        .defaultScrollAnchor(.bottom, for: .initialOffset)
        // The visible rect is in the content's coordinates, so it reaches
        // past the content's end exactly when the reader is at the bottom,
        // whatever the composer's and the keyboard's insets are.
        .onScrollGeometryChange(for: Bool.self) { geo in
            geo.visibleRect.maxY >= geo.contentSize.height - 60
        } action: { _, bottom in
            atBottom = bottom
        }
        .onChange(of: follow(turns)) { _, _ in
            if atBottom { position.scrollTo(edge: .bottom) }
        }
        .scrollDismissesKeyboard(.interactively)
        .scrollEdgeEffectStyle(.soft, for: .bottom)
        .safeAreaBar(edge: .bottom) {
            ComposerBar(conversation: conversation, dictation: dictation, atBottom: atBottom,
                        onJump: { withAnimation(.snappy) { position.scrollTo(edge: .bottom) } },
                        onKey: { keySheet = $0 })
        }
        .navigationTitle(title)
        .navigationSubtitle(subtitle)
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .toolbar { toolbar }
        .focusedSceneValue(\.conversation, conversation)
        .onAppear {
            store.retain(conversation, scene: scene.id)
            conversation.open()
            showing()
        }
        .onDisappear {
            store.setVisible(scene: scene.id, thread: nil)
            if dictation.active { dictation.cancel() }
            store.release(conversation, scene: scene.id)
        }
        .onChange(of: conversation.threadID) { _, _ in showing() }
        .onChange(of: phase) { _, phase in
            showing()
            if phase != .active, dictation.active { Task { await stopListening() } }
        }
        .onChange(of: conversation.settles) { _, _ in
            showing()
            if phase == .active {
                AccessibilityNotification.Announcement("\(conversation.name) answered").post()
            }
        }
        .sensoryFeedback(.success, trigger: conversation.settles)
        .sensoryFeedback(.error, trigger: conversation.error)
        .sheet(item: $keySheet) { ProviderKeySheet(provider: $0) }
        .alert("Rename Chat", isPresented: $renaming) {
            TextField("Title", text: $newTitle)
            Button("Cancel", role: .cancel) {}
            Button("Rename") {
                if let id = conversation.threadID {
                    let title = newTitle
                    Task { try? await store.rename(id, to: title) }
                }
            }
        }
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        if conversation.streaming {
            ToolbarItem(placement: .primaryAction) {
                // ⌘. is the Chat menu's.
                Button("Stop", systemImage: "stop.fill") { conversation.stop() }
                    .disabled(conversation.stopping)
            }
        }
        if let id = conversation.threadID {
            ToolbarItem(placement: .primaryAction) {
                Menu("More", systemImage: "ellipsis") {
                    Button("Rename…", systemImage: "pencil") {
                        newTitle = thread?.subject ?? title
                        renaming = true
                    }
                    Button("Mark as Unread", systemImage: "envelope.badge") { store.markUnread(id) }
                    Button("Copy Thread ID", systemImage: "doc.on.doc") { copyToPasteboard(id) }
                    Divider()
                    Button("New Chat with \(conversation.name)", systemImage: "square.and.pencil") {
                        scene.open(store.newConversation(agent: conversation.agentID))
                    }
                }
            }
        }
    }

    /// The chat is on screen in an active window: its notifications are not
    /// news, and it is read.
    private func showing() {
        let on = phase == .active ? conversation.threadID : nil
        store.setVisible(scene: scene.id, thread: on)
        if let on { Notifier.clear(thread: on) }
    }

    private func stopListening() async {
        let heard = await dictation.stop()
        conversation.draft = joined(conversation.draft, heard)
    }

    /// What moving means for following the bottom: a new turn, more text, a
    /// tool settling.
    private func follow(_ turns: [Turn]) -> Int {
        var h = Hasher()
        h.combine(turns.count)
        if let last = turns.last {
            h.combine(last.id)
            h.combine(last.content.count)
            h.combine(last.reasoning?.count ?? 0)
            h.combine(last.tools.count)
            h.combine(last.tools.last?.ok)
        }
        h.combine(conversation.outcomes.count)
        h.combine(conversation.error)
        return h.finalize()
    }
}

/// Each suggestion's `propose` call, by the request it landed.
func proposalCalls(_ turns: [Turn]) -> [String: ToolCall] {
    var out: [String: ToolCall] = [:]
    for turn in turns where turn.role == .assistant {
        for call in turn.tools {
            if let id = requestID(of: call) { out[id] = call }
        }
    }
    return out
}

/// What dictation adds to a draft: after it, with a space.
func joined(_ draft: String, _ heard: String) -> String {
    let words = heard.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !words.isEmpty else { return draft }
    let head = draft.trimmingCharacters(in: .whitespaces)
    return head.isEmpty ? words : (head.hasSuffix("\n") ? head + words : head + " " + words)
}

/// A new chat before its first message: who it is with, and a few openers.
private struct NewChatHero: View {
    @Environment(ChatStore.self) private var store
    @Bindable var conversation: Conversation

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if let agent = conversation.agent {
                Text(agent.name)
                    .font(.title.weight(.bold))
                if let description = agent.description {
                    Text(description)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(agent.examplePrompts, id: \.self) { prompt in
                        Button {
                            conversation.draft = prompt
                        } label: {
                            Text(prompt)
                                .font(.subheadline)
                                .padding(.horizontal, 14)
                                .padding(.vertical, 9)
                                .background(.fill.tertiary, in: .capsule)
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(.top, 6)
            } else {
                ContentUnavailableView {
                    Label("No assistant", systemImage: "person.crop.circle.badge.questionmark")
                } description: {
                    Text(store.offered.isEmpty
                        ? "There is no assistant to chat with yet. Choose which agents are offered in Settings → Agents."
                        : "Choose who to chat with below.")
                }
            }
        }
        .padding(.top, 24)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// What became of the run: working elsewhere, an error, sends that started
/// no turn, a handover that did not land, a chat that is gone.
private struct ConversationStatus: View {
    @Environment(ChatStore.self) private var store
    @Bindable var conversation: Conversation

    var body: some View {
        if conversation.overlay.incomplete, conversation.streaming {
            Label("Part of this answer arrived before you opened the chat; it shows in full when it finishes.",
                  systemImage: "text.badge.minus")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        if conversation.phase == .background {
            HStack(spacing: 10) {
                ProgressView().controlSize(.small)
                Text("\(conversation.name) is working on this chat in the background. Its answer shows here when it finishes.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
        if let error = conversation.error {
            ProblemLine(title: "\(conversation.name) couldn't answer", message: error).card(tint: .red)
        }
        ForEach(conversation.outcomes) { outcome in
            OutcomeCard(outcome: outcome) { conversation.dismiss(outcome) }
        }
        if let failed = conversation.reloadFailed {
            VStack(alignment: .leading, spacing: 10) {
                ProblemLine(message: failed, tint: .orange)
                Button("Try Again") { conversation.retryReload() }
                    .buttonStyle(.bordered)
            }
            .card(tint: .orange)
        }
        if conversation.deleted {
            ProblemLine(title: "This chat was deleted", message: "It is no longer on the server. What you typed stays below, to copy.",
                        tint: .secondary, systemImage: "trash")
                .card()
        }
    }
}

private struct OutcomeCard: View {
    let outcome: Outcome
    let dismiss: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            switch outcome.kind {
            case .refused:
                ProblemLine(title: "Your message wasn't sent",
                            message: "The server said: \(outcome.reason)" + (outcome.restored ? "\nIt's back in the box below." : ""))
            case .checking:
                HStack(spacing: 10) {
                    ProgressView().controlSize(.small)
                    Text("The connection dropped before the server confirmed your message. Checking whether it arrived…")
                        .font(.subheadline)
                }
            case .unknown:
                ProblemLine(title: "It may not have arrived",
                            message: "The connection dropped before the server confirmed your message (\(outcome.reason)). Look for it in this chat, or for a new chat in the list, before sending it again.",
                            tint: .orange)
            }
            if outcome.kind != .checking {
                Text(outcome.message)
                    .font(.subheadline)
                    .textSelection(.enabled)
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.background, in: .rect(cornerRadius: 10))
                HStack {
                    Button("Copy", systemImage: "doc.on.doc") { copyToPasteboard(outcome.message) }
                    Button("Dismiss", action: dismiss)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            }
        }
        .card(tint: outcome.kind == .refused ? .red : .orange)
    }
}
