import ChatKit
import SubstrateKit
import SwiftUI

/// One chat: the transcript, what became of the last send, and the composer
/// floating over it.
struct ConversationView: View {
    @Environment(ChatHost.self) private var host
    @Environment(\.scenePhase) private var phase
    let key: ChildKey
    let scene: String
    @State private var dictation = Dictation()
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var atBottom = true
    @State private var keySheet: KeySheet?
    @State private var renaming = false
    @State private var newTitle = ""

    var body: some View {
        if let screen = host.chats?.conversations[key] {
            transcript(screen)
        }
    }

    private func transcript(_ screen: ConversationScreen) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 22) {
                if let hero = screen.hero {
                    NewChatHeroView(
                        hero: hero, pick: { host.send(.draft($0), to: key) },
                        addKey: { keySheet = host.chats?.keys[$0] })
                }
                if let older = screen.olderHidden {
                    Label(older, systemImage: "clock.arrow.circlepath")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity)
                }
                ForEach(screen.turns) { turn in
                    TurnView(turn: turn, cards: screen.cards, expandThinking: screen.expandThinking)
                        .equatable()
                }
                if screen.typing {
                    TypingDots()
                }
                ConversationStatus(screen: screen, key: key)
                if let technical = screen.technical {
                    Grid(alignment: .leadingFirstTextBaseline, horizontalSpacing: 8, verticalSpacing: 2) {
                        ForEach(technical, id: \.self) { fact in
                            GridRow {
                                Text(fact.label).fontWeight(.semibold)
                                Text(fact.value).monospaced(fact.verbatim).textSelection(.enabled)
                            }
                        }
                    }
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 12)
            .padding(.bottom, 16)
            .environment(\.recordTitles, screen.titles)
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
        .onChange(of: follow(screen)) { _, _ in
            if atBottom { position.scrollTo(edge: .bottom) }
        }
        .scrollDismissesKeyboard(.interactively)
        .scrollEdgeEffectStyle(.soft, for: .bottom)
        .safeAreaBar(edge: .bottom) {
            ComposerBar(
                key: key, screen: screen, dictation: dictation, atBottom: atBottom,
                onJump: { withAnimation(.snappy) { position.scrollTo(edge: .bottom) } },
                onKey: { keySheet = host.chats?.keys[$0] })
        }
        .navigationTitle(screen.title)
        .navigationSubtitle(screen.subtitle)
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .toolbar { toolbar(screen) }
        .focusedSceneValue(\.conversation, key)
        .onAppear { host.send(.open, to: key) }
        .onDisappear {
            if dictation.active { dictation.cancel() }
        }
        .onChange(of: phase) { _, phase in
            if phase != .active, dictation.active { Task { await stopListening(screen) } }
        }
        .sensoryFeedback(.error, trigger: screen.error)
        .sheet(item: $keySheet) { ProviderKeySheet(sheet: $0) }
        .alert("Rename Chat", isPresented: $renaming) {
            TextField("Title", text: $newTitle)
            Button("Cancel", role: .cancel) {}
            Button("Rename") {
                if let thread = screen.thread { host.send(.rename(thread: thread, subject: newTitle)) }
            }
        }
    }

    @ToolbarContentBuilder private func toolbar(_ screen: ConversationScreen) -> some ToolbarContent {
        if screen.streaming {
            ToolbarItem(placement: .primaryAction) {
                // ⌘. is the Chat menu's.
                Button("Stop", systemImage: "stop.fill") { host.send(.stop, to: key) }
                    .disabled(screen.stopping)
            }
        }
        if let thread = screen.thread {
            ToolbarItem(placement: .primaryAction) {
                Menu("More", systemImage: "ellipsis") {
                    Button("Rename…", systemImage: "pencil") {
                        newTitle = screen.subject
                        renaming = true
                    }
                    Button("Mark as Unread", systemImage: "envelope.badge") { host.send(.markUnread(thread)) }
                    Button("Copy Thread ID", systemImage: "doc.on.doc") { copyToPasteboard(thread) }
                    Divider()
                    Button("New Chat with \(screen.agentName)", systemImage: "square.and.pencil") {
                        host.send(.newChat(agent: screen.agent, listen: false, scene: scene))
                    }
                }
            }
        }
    }

    private func stopListening(_ screen: ConversationScreen) async {
        let heard = await dictation.stop()
        host.send(.draft(joined(screen.composer.draft, heard)), to: key)
    }

    /// What moving means for following the bottom: a new turn, more text, a
    /// tool settling.
    private func follow(_ screen: ConversationScreen) -> Int {
        var h = Hasher()
        h.combine(screen.turns.count)
        h.combine(screen.turns.last)
        h.combine(screen.outcomes.count)
        h.combine(screen.error)
        h.combine(screen.typing)
        return h.finalize()
    }
}

/// What dictation adds to a draft: after it, with a space.
func joined(_ draft: String, _ heard: String) -> String {
    let words = heard.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !words.isEmpty else { return draft }
    let head = draft.trimmingCharacters(in: .whitespaces)
    return head.isEmpty ? words : (head.hasSuffix("\n") ? head + words : head + " " + words)
}

/// A new chat before its first message: who it is with, a provider's
/// missing key, and a few openers below it.
private struct NewChatHeroView: View {
    let hero: NewChatHero
    let pick: (String) -> Void
    let addKey: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if let empty = hero.empty {
                ContentUnavailableView {
                    Label(hero.title, systemImage: "person.crop.circle.badge.questionmark")
                } description: {
                    Text(empty)
                }
            } else {
                Text(hero.title)
                    .font(.title.weight(.bold))
                if let description = hero.description {
                    Text(description)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if let keyless = hero.keyless {
                    KeylessCalloutView(callout: keyless) { addKey(keyless.provider) }
                        .padding(.top, 6)
                }
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(hero.prompts, id: \.self) { prompt in
                        Button {
                            pick(prompt)
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
            }
        }
        .padding(.top, 24)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// What became of the run: working elsewhere, an error, sends that started
/// no turn, a handover that did not land, a chat that is gone.
private struct ConversationStatus: View {
    @Environment(ChatHost.self) private var host
    let screen: ConversationScreen
    let key: ChildKey

    var body: some View {
        ForEach(screen.status, id: \.self) { note in
            switch note.kind {
            case .partial:
                Label(note.message, systemImage: "text.badge.minus")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            case .background:
                HStack(spacing: 10) {
                    ProgressView().controlSize(.small)
                    Text(note.message)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            case .failed:
                VStack(alignment: .leading, spacing: 10) {
                    ProblemBox(title: note.title, message: note.message)
                    if let detail = note.detail {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("The server said")
                                .font(.caption.weight(.semibold))
                                .foregroundStyle(.secondary)
                            Text(detail)
                                .font(.caption.monospaced())
                                .foregroundStyle(.secondary)
                                .textSelection(.enabled)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        // Under the message, past the box's symbol.
                        .padding(.leading, 26)
                    }
                }
                .card(tint: .red)
            case .reloadFailed:
                VStack(alignment: .leading, spacing: 10) {
                    ProblemBox(message: note.message, tint: .orange)
                    if let action = note.action {
                        Button(action) { host.send(.retryReload, to: key) }
                            .buttonStyle(.bordered)
                    }
                }
                .card(tint: .orange)
            case .deleted:
                ProblemBox(title: note.title, message: note.message, tint: .secondary, systemImage: "trash")
                    .card()
            }
        }
        ForEach(screen.outcomes) { outcome in
            OutcomeCard(outcome: outcome) {
                if let id = UUID(uuidString: outcome.id) { host.send(.dismiss(id), to: key) }
            }
        }
    }
}

private struct OutcomeCard: View {
    let outcome: OutcomePresentation
    let dismiss: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            switch outcome.kind {
            case .refused:
                ProblemBox(title: outcome.title, message: outcome.message)
            case .checking:
                HStack(spacing: 10) {
                    ProgressView().controlSize(.small)
                    Text(outcome.message)
                        .font(.subheadline)
                }
            case .unknown:
                ProblemBox(title: outcome.title, message: outcome.message, tint: .orange)
            }
            if let text = outcome.text {
                Text(text)
                    .font(.subheadline)
                    .textSelection(.enabled)
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.background, in: .rect(cornerRadius: 10))
                HStack {
                    Button("Copy", systemImage: "doc.on.doc") { copyToPasteboard(text) }
                    Button("Dismiss", action: dismiss)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            }
        }
        .card(tint: outcome.kind == .refused ? .red : .orange)
    }
}
