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
    /// The transcript follows its end and the end is on screen, as its
    /// scroll view says: what the read mark goes by, with whether anybody
    /// is looking.
    @State private var atBottom = false
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
            transcript(screen)
        } else {
            NoChatView(scene: scene)
        }
    }

    private func transcript(_ screen: ConversationScreen) -> some View {
        let _ = Meter.shared.count(.body, "Transcript")
        return TranscriptScroll(opens: screen.opens, anchor: opening(screen), sends: screen.sends, looked: { atBottom = $0 }) {
            LazyVStack(alignment: .leading, spacing: 22) {
                TranscriptRows(screen: screen, key: key, onKey: { keySheet = host.chats?.keys[$0] }, act: act)
            }
            .scrollTargetLayout()
            .padding(.horizontal, 20)
            .padding(.top, 12)
            .padding(.bottom, 16)
            .environment(\.recordTitles, screen.titles)
            .environment(\.openThread, OpenThreadAction(host: host, scene: scene))
            .environment(\.conversationKey, key)
            .replyLinks()
            .frame(maxWidth: 780)
            .frame(maxWidth: .infinity)
        } bar: { atEnd, seen, jump in
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
                    key: key, screen: screen, dictation: dictation, atBottom: atEnd,
                    jump: JumpToLatest(below: atEnd ? 0 : screen.messages(below: TranscriptPiece.lastTurn(among: seen, of: screen.turns))),
                    onJump: jump,
                    onKey: { keySheet = host.chats?.keys[$0] }
                )
                .equatable()
            }
        }
        // The end of the chat is read once somebody looking at it has had
        // it on screen for a second (ChatKit counts): the list is told
        // whenever that starts or stops being so. The end is on screen
        // where the transcript follows it and the scroll view shows it;
        // somebody is looking where the window is in front and nothing
        // stands over the chat. Never from onDisappear: a split view takes
        // a detail away and puts the same one back, and this would not run
        // again; a chat no window shows is no longer the window's current
        // one, which the list knows by itself.
        .onChange(of: atBottom && front && !covered && phase == .active, initial: true) { _, looking in
            host.send(.end(scene: scene, key: key, shown: looking))
        }
        #if os(iOS)
        .background { CoverProbe(covered: $covered) }
        #endif
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

    /// The row a chat that opens at a turn is put at: the "New" line above
    /// the turn, or the turn's first row where it has none. Nil for a chat
    /// that opens at its end, and until the rows hold the turn.
    private func opening(_ screen: ConversationScreen) -> String? {
        guard case let .turn(id) = screen.opens else { return nil }
        return TranscriptPiece.pieces(of: screen.turns, newLine: screen.newLine).first { $0.turn.id == id }?.id
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
}

/// The scroll view a transcript lies in, which follows its end while its
/// reader lets it (`TranscriptFollow`, ChatKit's rule).
///
/// Following is kept as the reader's intent and never worked out from where
/// the scroll view stands after its content moved: a reply that grew by a
/// screen while the app was away would read as "not at the end", and the
/// chat would stay behind it. While it follows, every change of the
/// content's height and of the room for it (a piece of a reply, rows
/// landing, the keyboard, a turn of the device, a window resized, another
/// text size) takes it to its end. It reacts to that height, which the
/// scroll view reports, and not to a digest of the screen, so nothing that
/// grows is missed.
///
/// What moves at each of those is this view's own state, and the
/// transcript's rows are handed to it made: the conversation's view, which
/// makes them, is not drawn again for a move, so its body runs once for a
/// flush.
struct TranscriptScroll<Rows: View, Bar: View>: View {
    @Environment(\.scenePhase) private var phase
    #if os(macOS)
    @Environment(\.controlActiveState) private var keyState
    #endif
    let opens: TranscriptOpening
    /// The row a chat that opens at a turn is put at, once the rows hold
    /// it: they may be drawn before the turn is known, or after.
    let anchor: String?
    /// Moved by every send from the chat.
    let sends: Int
    /// Told whether the transcript follows its end with the end on screen:
    /// the reader's intent and what the scroll view shows, both. A chat
    /// opened further up, or scrolled up, is not at its end whatever
    /// arrives below.
    let looked: (Bool) -> Void
    let rows: Rows
    /// What floats over the transcript's end: handed whether the end is
    /// near, the ids of the rows on screen, and what Jump to Latest does.
    let bar: (_ atEnd: Bool, _ seen: [String], _ jump: @escaping () -> Void) -> Bar
    @State private var position = ScrollPosition(idType: String.self)
    @State private var follow: TranscriptFollow
    /// The chat was put at the row it opens at, which happens once, and
    /// only once the transcript has a size to scroll in; or its reader
    /// took it first, by a touch, a wheel, a send or Jump to Latest. Rows
    /// that bring a line after that leave the transcript where they put
    /// it.
    @State private var placed = false
    @State private var laidOut = false
    @State private var atEnd = true
    /// The rows on screen: what Jump to Latest counts from.
    @State private var seen: [String] = []
    @State private var reach = TranscriptReach()

    init(
        opens: TranscriptOpening, anchor: String? = nil, sends: Int, looked: @escaping (Bool) -> Void = { _ in },
        @ViewBuilder rows: () -> Rows,
        @ViewBuilder bar: @escaping (_ atEnd: Bool, _ seen: [String], _ jump: @escaping () -> Void) -> Bar
    ) {
        self.opens = opens
        self.anchor = anchor
        self.sends = sends
        self.looked = looked
        self.rows = rows()
        self.bar = bar
        _follow = State(initialValue: TranscriptFollow(opening: opens))
    }

    var body: some View {
        ScrollViewReader { reader in
            scroll
                // A chat with more than its reader had seen opens at the
                // first of it, and does not follow its end until they come
                // down to it. Where what is new fits on one screen the
                // scroll stops at the end, and the chat follows from there
                // as one with nothing new does.
                .onChange(of: opens) { _, opens in
                    // Known only now, with rows that arrived after the chat
                    // was shown: following the end would take the chat past
                    // where it is about to be put.
                    if !placed, opens != .end { follow = TranscriptFollow(opening: opens) }
                }
                .onChange(of: anchor, initial: true) { _, _ in place(reader) }
                .onChange(of: laidOut) { _, _ in place(reader) }
                .onChange(of: atEnd && follow.following, initial: true) { _, at in looked(at) }
        }
    }

    /// Puts a chat that opens at a row at it, once.
    private func place(_ reader: ScrollViewProxy) {
        guard laidOut, !placed, let anchor else { return }
        placed = true
        follow = TranscriptFollow(opening: opens)
        position = ScrollPosition(idType: String.self)
        // By the row's id, which the lazy stack knows however far up it
        // is. After this update, since the rows may have arrived in it and
        // the stack has not laid them out yet.
        DispatchQueue.main.async { reader.scrollTo(anchor, anchor: .top) }
    }

    private var scroll: some View {
        ScrollView { rows }
            .scrollPosition($position)
            // A chat opens at its end, and a short one lies from the top: it
            // has nothing to follow until it outgrows the screen.
            .defaultScrollAnchor(opens == .end ? .bottom : .top, for: .initialOffset)
            .defaultScrollAnchor(.top, for: .alignment)
            .onScrollPhaseChange { _, phase in
                switch phase {
                case .tracking, .interacting:
                    // A finger on it: the reader's, and where it opens is
                    // no longer the app's to say.
                    reach.touched = true
                    placed = true
                    if follow.following { leave() }
                case .idle:
                    // Come to rest: where a finger left it, that is the
                    // reader's word on following; where the app moved it,
                    // it is held to where it was sent.
                    let touched = reach.touched
                    (reach.touched, reach.moving) = (false, nil)
                    if touched { move(follow.rested(distance: reach.measure.distance)) } else { hold() }
                default:
                    break
                }
            }
            // Where the scroll view stands, at every move of it. Kept beside
            // the view, not in its state: it moves with every point
            // scrolled and draws nothing.
            .onScrollGeometryChange(for: TranscriptReach.Measure.self) { geo in
                TranscriptReach.Measure(geo)
            } action: { _, new in
                // Against what this view last saw, and never the value the
                // system hands over as the one before: that one may be a
                // place the scroll view passed through and never said, and
                // a lazy stack that guessed its rows' heights again, as it
                // does when rows are replaced, then reads as the reader
                // having moved the transcript up.
                let old = reach.measure
                reach.measure = new
                if !laidOut, new.content > 0 { laidOut = true }
                // Whether the end is near, for Jump to Latest: state, and
                // so moved only when it changes.
                let near = new.distance <= TranscriptFollow.near
                if near != atEnd { atEnd = near }
                if follow.following, !reach.touched, !reach.inMotion, new.content == old.content, new.room == old.room, new.top < old.top - 1,
                    new.distance > TranscriptFollow.near
                {
                    // A scroll bar or a key moved the transcript with no
                    // finger on it: up, away from the end, with the content
                    // and the room as they were. Content that grew is never
                    // read as the reader leaving.
                    leave()
                } else if near, !follow.following, !reach.touched {
                    // Brought back to the end with no finger on it, which
                    // never comes to rest as a touch does.
                    move(follow.rested(distance: new.distance))
                } else {
                    // The content's height or the room for it changed, or
                    // the scroll view moved for a reason of its own: a
                    // transcript that follows its end is held to it.
                    hold()
                }
            }
            .onScrollTargetVisibilityChange(idType: String.self, threshold: 0.01) { rows in
                // Kept at every change, a row coming or going and never a
                // piece of text: whether the end is near is said apart
                // from it, in either order.
                if rows != seen { seen = rows }
            }
            .onChange(of: sends) { _, _ in take(follow.sent()) }
            .onChange(of: front) { _, front in
                // Back in front: a chat that was at its end is at its end,
                // whatever arrived meanwhile.
                if front { move(follow.returned()) }
            }
            #if os(macOS)
        // A wheel, or two fingers on a trackpad, is a Mac reader's hand
        // on the transcript, and a Mac's scroll view says nothing of it
        // as a touch's phases do. Turned up, it is the reader leaving
        // the end, said before the scroll view has moved: by the time it
        // has, rows coming into view have changed the content's height
        // in the same breath, and a change of height alone takes a
        // transcript that follows back to its end. From then, while it
        // turns, the transcript is the reader's, however little it has
        // moved. It has come to rest a quarter of a second after its last
        // event, and never while fingers lie on a trackpad, however long
        // they pause; that is when the rule is asked. The glide after a
        // flick is no hand: it lets go of nothing, so a send or Jump to
        // Latest made while it lasts keeps the end. Turned down at an
        // end that is followed it changes nothing: a glide goes on for
        // seconds, and a reply that grew meanwhile would read as the
        // reader having left.
        .background {
            WheelWatcher { wheel in
                placed = true
                if wheel.up, !wheel.gliding, follow.following { leave() }
                guard !follow.following else { return }
                reach.touched = true
                reach.wheel += 1
                guard !wheel.held else { return }
                let turn = reach.wheel
                Task { @MainActor in
                    try? await Task.sleep(for: .milliseconds(250))
                    guard reach.wheel == turn else { return }
                    reach.touched = false
                    move(follow.rested(distance: reach.measure.distance))
                    // Left past the end, which nothing else puts right
                    // until the content moves.
                    hold()
                }
            }
        }
            #endif
            .scrollDismissesKeyboard(.interactively)
            .scrollEdgeEffectStyle(.soft, for: .bottom)
            .safeAreaBar(edge: .bottom) {
                bar(atEnd, seen) { take(follow.jumped()) }
            }
    }

    /// The app is in front, and on a Mac this window is the one typed in.
    private var front: Bool {
        #if os(macOS)
        phase == .active && keyState == .key
        #else
        phase == .active
        #endif
    }

    /// The reader took the transcript away from its end. The position is
    /// let go of with it: one still bound to a place would be put back
    /// there at the next change.
    private func leave() {
        placed = true
        follow.touched()
        position = ScrollPosition(idType: String.self)
    }

    /// A send and Jump to Latest take the transcript from whatever hand
    /// was on it: a rest that hand was still to come to is not asked
    /// after, or it would let go of the end while the move there is under
    /// way.
    private func take(_ move: TranscriptFollow.Move) {
        placed = true
        reach.touched = false
        reach.wheel += 1
        self.move(move)
    }

    /// Does what the rule answered.
    private func move(_ move: TranscriptFollow.Move) {
        switch move {
        case .stay:
            break
        case .end(animated: true):
            guard let end = reach.measure.away else { return }
            // Held to its end once the move has come to rest, and not
            // before: a correction would cut the move short. Not every
            // scroll view says when a move of the app's own ends, so it is
            // also given no longer than the motion takes.
            reach.moving = Date().addingTimeInterval(0.45)
            withAnimation(.snappy) { go(to: end) }
            Task { @MainActor in
                try? await Task.sleep(for: .milliseconds(500))
                hold()
            }
        case .end(animated: false):
            hold()
        }
    }

    /// Sends the scroll view to its content's end: `end` is the point of
    /// the content that then stands at the top of what shows.
    ///
    /// On an iPhone and an iPad it is asked for as that place. Asked for as
    /// an edge while what is inset over the end changes (the keyboard, the
    /// composer gaining or losing a line), the scroll view there goes past
    /// its end by the inset, and the transcript is a blank screen. A Mac's
    /// scroll view takes a place by another measure and its edge exactly.
    private func go(to end: Double) {
        #if os(macOS)
        position.scrollTo(edge: .bottom)
        #else
        position.scrollTo(y: end)
        #endif
    }

    /// Holds a transcript that follows its end to it, wherever the scroll
    /// view stands: short of it after the content grew, or past it after
    /// the room for it did or the content shrank. Whether it is there is
    /// worked out from what the scroll view last said of itself, and the
    /// end is asked for again a moment later where it did not get there,
    /// so a move lost to a change under way is made good: a few times for
    /// one reason, never in a loop.
    private func hold() {
        guard follow.following, !reach.touched, !reach.inMotion, let end = reach.measure.away else {
            reach.asked = 0
            return
        }
        // Counted while the content and the room are as they were when it
        // was last asked for: a change of either is a new reason to go.
        if reach.askedOf != reach.measure.extent { (reach.askedOf, reach.asked) = (reach.measure.extent, 0) }
        guard reach.asked < 4 else { return }
        reach.asked += 1
        go(to: end)
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(40))
            hold()
        }
    }
}

#if os(macOS)
/// Tells of a wheel, or two fingers on a trackpad, turned over the view it
/// stands behind: the scroll view over it takes the event itself, so it is
/// watched for on its way there and passed on as it came.
struct WheelWatcher: NSViewRepresentable {
    /// One event of a wheel.
    struct Wheel {
        /// Turned up, towards the transcript's start.
        var up: Bool
        /// Fingers lie on a trackpad: it is not at rest, moving or not.
        var held = false
        /// The glide after the fingers left.
        var gliding = false
    }

    let turned: (Wheel) -> Void

    func makeNSView(context: Context) -> Watching {
        let view = Watching()
        view.turned = turned
        return view
    }

    func updateNSView(_ view: Watching, context: Context) {
        view.turned = turned
    }

    final class Watching: NSView {
        var turned: (Wheel) -> Void = { _ in }
        private var monitor: Any?

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            if let monitor { NSEvent.removeMonitor(monitor) }
            monitor = nil
            guard window != nil else { return }
            monitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { [weak self] event in
                if let self, event.window === self.window {
                    self.turned(
                        by: event.scrollingDeltaY, at: event.locationInWindow,
                        held: !event.phase.isDisjoint(with: [.mayBegin, .began, .changed, .stationary]),
                        gliding: !event.momentumPhase.isEmpty)
                }
                return event
            }
        }

        /// A wheel turned by `delta` (up is more than none) at a point of
        /// the window. An event that moves nothing still says whether
        /// fingers lie on a trackpad, or have left it.
        func turned(by delta: CGFloat, at point: CGPoint, held: Bool = false, gliding: Bool = false) {
            guard convert(bounds, to: nil).contains(point) else { return }
            turned(Wheel(up: delta > 0, held: held, gliding: gliding))
        }

        isolated deinit {
            if let monitor { NSEvent.removeMonitor(monitor) }
        }
    }
}
#endif

/// Where a transcript's scroll view stands, as it last said: read when a
/// rule needs it. A reference the view holds, so a point scrolled writes it
/// and draws nothing.
@MainActor
final class TranscriptReach {
    /// What the scroll view says of itself at each move, in its content's
    /// own coordinates.
    struct Measure: Equatable {
        /// The content's height.
        var content = 0.0
        /// How much of the scroll view shows its content: what is left of
        /// it between what is inset over its top (a navigation bar) and
        /// over its end (the composer, the keyboard).
        var room = 0.0
        /// The point of the content at the top of that room.
        var top = 0.0

        init() {}

        init(_ geo: ScrollGeometry) {
            content = geo.contentSize.height
            // The visible rect reaches under what is inset over it.
            room = geo.visibleRect.height - geo.contentInsets.top - geo.contentInsets.bottom
            top = geo.contentOffset.y + geo.contentInsets.top
        }

        /// The point that stands at the top of the room when the content's
        /// end stands at the end of it; the content's start where it fits.
        var end: Double { max(0, content - room) }
        /// How much of the content lies below what shows of it.
        var distance: Double { max(0, end - top) }
        /// The end, where the scroll view is not at it.
        var away: Double? { abs(top - end) > 0.5 ? end : nil }
        /// The content's height and the room for it.
        var extent: [Double] { [content, room] }
    }

    var measure = Measure()
    /// A finger is on the transcript, or was when it last moved; on a Mac,
    /// a wheel is turning over it.
    var touched = false
    /// Counts a Mac's wheel events, so the last one is known for what it is.
    var wheel = 0
    /// The app is moving the transcript to its end, with motion, until
    /// then at the latest.
    var moving: Date?
    var inMotion: Bool { moving.map { $0 > Date() } ?? false }
    /// How many times in a row the end was asked for and not reached, with
    /// the content and the room as they are in `askedOf`.
    var asked = 0
    var askedOf: [Double] = []
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
        // A turn's failure is a card at that turn, a row under its last;
        // the "New" line is a row above the first turn that was not read.
        ForEach(TranscriptPiece.pieces(of: screen.turns, failures: screen.failures.turns, newLine: screen.newLine)) { piece in
            TranscriptPieceView(piece: piece, cards: screen.cards, expandThinking: screen.expandThinking, act: act)
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
