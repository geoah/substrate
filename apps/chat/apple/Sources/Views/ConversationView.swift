import ChatKit
import SubstrateKit
import SwiftUI

/// One chat: the transcript, what became of the last send, and the composer
/// floating over it.
struct ConversationView: View {
    @Environment(ChatHost.self) private var host
    @Environment(\.scenePhase) private var phase
    @Environment(\.openURL) private var openURL
    let key: ChildKey
    let scene: String
    @State private var dictation = Dictation()
    @State private var draftField = DraftField()
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var atBottom = true
    /// The chat was put at its "New" line, which happens once, on opening,
    /// and only once the transcript has a size to scroll in.
    @State private var placedAtNew = false
    @State private var laidOut = false
    #if os(macOS)
    /// The window is the key one of the app in front.
    @Environment(\.appearsActive) private var front
    private let covered = false
    #else
    private let front = true
    /// A sheet or an alert stands over the chat.
    @State private var covered = false
    #endif
    @State private var keySheet: KeySheet?
    @State private var renaming = false
    @State private var newTitle = ""

    var body: some View {
        if let screen = host.conversation(key) {
            ScrollViewReader { reader in transcript(screen, reader) }
        } else {
            NoChatView(scene: scene)
        }
    }

    private func transcript(_ screen: ConversationScreen, _ reader: ScrollViewProxy) -> some View {
        let _ = Meter.shared.count(.body, "Transcript")
        return ScrollView {
            LazyVStack(alignment: .leading, spacing: 22) {
                TranscriptRows(screen: screen, key: key, onKey: { keySheet = host.chats?.keys[$0] }, act: act)
            }
            .padding(.horizontal, 20)
            .padding(.top, 12)
            .padding(.bottom, 16)
            .environment(\.recordTitles, screen.titles)
            .environment(\.openThread, OpenThreadAction(host: host, scene: scene))
            .environment(\.conversationKey, key)
            .replyLinks()
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
        // A chat with more than the person had read opens at the first of
        // it: where what is new fits on one screen the scroll stops at the
        // end, as a chat with nothing new opens. The rows may be drawn
        // before the line is known (they come from the disk) or after.
        .onScrollGeometryChange(for: Bool.self) { geo in
            geo.contentSize.height > 0
        } action: { _, laid in
            laidOut = laid
            placeAtNew(screen, reader)
        }
        .onChange(of: screen.newLine?.turn, initial: true) { _, _ in placeAtNew(screen, reader) }
        // The end of the chat is read once somebody looking at it has had
        // it on screen for a second (ChatKit counts): the list is told
        // whenever that starts or stops being so. Never from onDisappear:
        // a split view takes a detail away and puts the same one back, and
        // this would not run again; a chat no window shows is no longer
        // the window's current one, which the list knows by itself.
        .onChange(of: atBottom && front && !covered && phase == .active, initial: true) { _, looking in
            host.send(.end(scene: scene, key: key, shown: looking))
        }
        #if os(iOS)
        .background { CoverProbe(covered: $covered) }
        #endif
        .scrollDismissesKeyboard(.interactively)
        .scrollEdgeEffectStyle(.soft, for: .bottom)
        .safeAreaBar(edge: .bottom) {
            VStack(spacing: 8) {
                // A failure of the chat as a whole: one strip, directly
                // above the composer.
                if let strip = screen.failures.strip {
                    FailureView(failure: strip, act: act)
                        .padding(.horizontal, 12)
                        .frame(maxWidth: 820)
                        .transition(.opacity)
                }
                // Asked once a first reply settled, in the app's own words.
                if let offer = host.chats?.notificationOffer {
                    NotificationOfferView(offer: offer)
                        .padding(.horizontal, 12)
                        .frame(maxWidth: 820)
                        .transition(.opacity)
                }
                ComposerBar(
                    key: key, screen: screen, dictation: dictation, atBottom: atBottom,
                    onJump: { withAnimation(.snappy) { position.scrollTo(edge: .bottom) } },
                    onKey: { keySheet = host.chats?.keys[$0] }
                )
                .equatable()
            }
        }
        .environment(\.draftField, draftField)
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
        .sensoryFeedback(trigger: screen.failures.spoken) { _, spoken in spoken == nil ? nil : .error }
        // VoiceOver hears a failure when it appears in the chat on screen.
        .onChange(of: screen.failures.spoken) { _, spoken in
            if let spoken { AccessibilityNotification.Announcement(spoken).post() }
        }
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

    private func placeAtNew(_ screen: ConversationScreen, _ reader: ScrollViewProxy) {
        guard laidOut, !placedAtNew, let line = screen.newLine,
            let row = TranscriptPiece.pieces(of: screen.turns).first(where: { $0.turn.id == line.turn })
        else { return }
        placedAtNew = true
        // By the id of the turn's first row, which the lazy stack knows
        // however far up it is; the line is drawn above that row.
        reader.scrollTo(row.id, anchor: .top)
    }

    private var act: (FailureAction) -> Void {
        let actions = FailureActions(host: host, scene: scene, key: key, openURL: openURL, draftField: draftField) {
            keySheet = host.chats?.keys[$0]
        }
        return { actions($0) }
    }

    private func stopListening(_ screen: ConversationScreen) async {
        // The field said what it holds when the app left the front: what
        // was heard lands after it, in the conversation.
        host.send(.heard(await dictation.stop()), to: key)
    }

    /// What moving means for following the bottom: a new turn, more text, a
    /// tool settling.
    private func follow(_ screen: ConversationScreen) -> Int {
        var h = Hasher()
        h.combine(screen.turns.count)
        h.combine(screen.turns.last)
        h.combine(screen.approvals)
        h.combine(screen.failures.turns)
        h.combine(screen.failures.strip?.id)
        h.combine(screen.typing)
        return h.finalize()
    }
}

/// The transcript's rows, in order: a new chat's opening, the turns, what a
/// sub-agent asks, what became of the run, and the technical details. The
/// conversation lays them out lazily in its scroll view; a gallery lays
/// the same rows out whole.
struct TranscriptRows: View {
    @Environment(ChatHost.self) private var host
    let screen: ConversationScreen
    let key: ChildKey
    /// Add a key for the provider named.
    let onKey: (String) -> Void
    /// What a failure's button does.
    let act: (FailureAction) -> Void

    var body: some View {
        if let hero = screen.hero {
            NewChatHeroView(
                hero: hero, pick: { host.send(.fill($0), to: key) },
                addKey: onKey)
        }
        if let older = screen.olderHidden {
            Label(older, systemImage: "clock.arrow.circlepath")
                .font(.caption)
                .foregroundStyle(.quietText)
                .frame(maxWidth: .infinity)
        }
        let pieces = TranscriptPiece.pieces(of: screen.turns)
        // The last row of each turn, by the turn's id.
        let ends = Dictionary(pieces.map { ($0.turn.id, $0.id) }, uniquingKeysWith: { _, last in last })
        // The first row of the turn the person had not read, where the
        // chat was opened with more than they had.
        let new = screen.newLine.flatMap { line in pieces.first { $0.turn.id == line.turn }?.id }
        ForEach(pieces) { piece in
            if piece.id == new, let line = screen.newLine { NewLineView(line: line).padding(.top, 8) }
            TranscriptPieceView(piece: piece, cards: screen.cards, expandThinking: screen.expandThinking)
            // A turn's failure is a card at that turn, under its last row.
            if ends[piece.turn.id] == piece.id, let failure = screen.failures.turns[piece.turn.id] {
                FailureView(failure: failure, act: act)
            }
        }
        if screen.typing {
            TypingDots()
        }
        // A sub-agent's asks: its own tool rows are in its own
        // conversation, so they wait here.
        ForEach(screen.approvals, id: \.self) { id in
            if let card = screen.cards[id]?.approval { ApprovalCardView(id: id, card: card) }
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
            .foregroundStyle(.quietText)
        }
    }
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
                        .foregroundStyle(.quietText)
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

/// What the run is doing that is no failure: working elsewhere, an answer
/// arriving in part, a chat that is gone.
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
                    .foregroundStyle(.quietText)
            case .background:
                HStack(spacing: 10) {
                    ProgressView().controlSize(.small)
                    Text(note.message)
                        .font(.subheadline)
                        .foregroundStyle(.quietText)
                }
            case .deleted:
                ProblemBox(title: note.title, message: note.message, tint: .secondary, systemImage: "trash")
                    .card()
            }
        }
    }
}
