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
        return TranscriptScroll(
            opens: screen.opens, anchor: opening(screen), seeks: screen.seeks, sends: screen.sends, visit: screen.visit, above: screen.above,
            last: screen.approvals.last ?? TranscriptPiece.lastRow(of: screen.turns, failures: screen.failures.turns),
            // Rows before the ones held are asked for by a row that comes
            // on screen, which in a stack laid out whole every row has.
            count: screen.older == nil ? screen.turns.count : .max,
            first: { TranscriptPiece.firstRow(among: $0, of: screen.turns) },
            looked: { atBottom = $0 },
            released: { host.send(.followEnded(key: key, by: $0)) }
        ) {
            TranscriptStack {
                TranscriptRows(screen: screen, key: key, onKey: { keySheet = host.chats?.keys[$0] }, act: act)
            }
            .transcriptMargins()
            .environment(\.recordTitles, screen.titles)
            .environment(\.openThread, OpenThreadAction(host: host, scene: scene))
            .environment(\.conversationKey, key)
            .environment(\.showMessage) { message in
                // A card answered in a message points at it.
                if let thread = screen.thread { host.send(.open(thread: thread, message: message, scene: scene)) }
            }
            .replyLinks()
            .replyTools(wraps: screen.wrapsCode)
            .readingMeasure()
        } bar: { atEnd, seen, jump in
            VStack(spacing: ChatSpace.items) {
                // A failure of the chat as a whole: one strip, directly
                // above the composer.
                if let strip = screen.failures.strip {
                    FailureView(failure: strip, act: act)
                        .overComposer()
                }
                // Asked once a first reply settled, in the app's own words.
                if let offer = host.chats?.notificationOffer {
                    NotificationOfferView(offer: offer)
                        .overComposer()
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
    /// that opens at its end, and until the rows hold the turn. While the
    /// row it opens at is still read toward, the row above the ones held
    /// that says so: put at the first turn, that row stood just off the
    /// screen's top and nothing said a read was under way.
    private func opening(_ screen: ConversationScreen) -> String? {
        guard case let .turn(id) = screen.opens else { return nil }
        if screen.seeks, screen.older != nil { return OlderRowsRow.row }
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
    /// `anchor` is the top of the rows held, for now: the row the chat
    /// opens at is above them and is read toward (`ConversationScreen.seeks`).
    let seeks: Bool
    /// Moved by every send from the chat.
    let sends: Int
    /// Moved each time the chat is asked to be put at the row it opens at
    /// once more, while it is shown: a message opened from a search.
    var visit = 0
    /// Rows arrived above the ones drawn: the turn that stood first before
    /// them, which the transcript is kept at where it does not follow its
    /// end.
    var above: RowsAbove?
    /// The transcript's last row, by the id the scroll view knows it by:
    /// what the end is asked for by while that row is not on screen
    /// (`go(to:)`). Less than the room for the transcript stands under it
    /// (what a chat is doing, never a row of its own height). Nil where
    /// there is none, and the end is then a place.
    var last: String?
    /// How many turns the transcript holds: few are laid out whole
    /// (`whole`). More than any where its rows must be lazy whatever
    /// their number.
    var count = 0
    /// The first, in the transcript's order, of the rows named: the rows on
    /// screen come in no order this view knows.
    var first: ([String]) -> String? = { $0.first }
    /// Told whether the transcript follows its end with the end on screen:
    /// the reader's intent and what the scroll view shows, both. A chat
    /// opened further up, or scrolled up, is not at its end whatever
    /// arrives below.
    let looked: (Bool) -> Void
    /// The reader took the transcript from its end, and by what: said once
    /// each time, for the diagnostics file.
    let released: (TranscriptFollow.Release) -> Void
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
    /// The rows above that had arrived when the chat was put at its row.
    @State private var landed: RowsAbove?
    @State private var laidOut = false
    @State private var atEnd = true
    /// The rows on screen: what Jump to Latest counts from.
    @State private var seen: [String] = []
    @State private var reach = TranscriptReach()
    @State private var before = RowsBefore()
    @State private var places = RowPlaces()
    /// Counts the times the scroll view was made anew to stand at its end
    /// (`startOver`).
    @State private var starts = 0
    /// The rows are laid out whole, in a stack that is not lazy: said when
    /// the scroll view is made with its first rows and kept while it
    /// lives, since a stack that changed its kind would make every row
    /// anew under its reader. Nil until there are rows. One that has
    /// grown long is made lazy again where its reader is at its end or is
    /// taken there: at a send, and when the count moves under a transcript
    /// that follows (`unwhole`).
    ///
    /// A lazy stack guesses the height of the rows it has not laid out
    /// from the ones it has. With a few rows of very unlike heights (a
    /// line of the person's over a reply ten screens tall, which every
    /// long reply is at the largest text sizes) the guess is off by
    /// screens, the scroll view's own anchor stands it at the end of the
    /// guess, past the rows, and no row is laid out there to take the
    /// guess back: an empty screen. A transcript of a few turns costs
    /// little to lay out whole, and then nothing is guessed.
    @State private var whole: Bool?

    init(
        opens: TranscriptOpening, anchor: String? = nil, seeks: Bool = false, sends: Int, visit: Int = 0, above: RowsAbove? = nil,
        last: String? = nil, count: Int = 0,
        first: @escaping ([String]) -> String? = { $0.first },
        looked: @escaping (Bool) -> Void = { _ in },
        released: @escaping (TranscriptFollow.Release) -> Void = { _ in },
        @ViewBuilder rows: () -> Rows,
        @ViewBuilder bar: @escaping (_ atEnd: Bool, _ seen: [String], _ jump: @escaping () -> Void) -> Bar
    ) {
        self.opens = opens
        self.anchor = anchor
        self.seeks = seeks
        self.sends = sends
        self.visit = visit
        self.above = above
        self.last = last
        self.count = count
        self.first = first
        self.looked = looked
        self.released = released
        self.rows = rows()
        self.bar = bar
        _follow = State(initialValue: TranscriptFollow(opening: opens))
    }

    /// A transcript of this many turns or fewer is laid out whole.
    static var few: Int { 12 }
    /// And one of up to this many where it was found with no row on screen.
    static var fewAtMost: Int { 60 }

    /// Whether the rows are laid out whole: as was said when the scroll
    /// view was made, and until then by how many there are.
    private var laysOutWhole: Bool { whole ?? (count <= Self.few) }

    var body: some View {
        let _ = before.drawn(above: above, seen: seen, places: places)
        ScrollViewReader { reader in
            let _ = reach.reader = reader
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
                .onChange(of: visit) { _, _ in
                    // Asked for again: the chat is put at its row once more.
                    placed = false
                    place(reader)
                }
                .onChange(of: above) { _, above in keep(reader, at: above) }
                .onChange(of: last != nil, initial: true) { _, rows in
                    if rows, whole == nil { whole = count <= Self.few }
                }
                .onChange(of: count) { _, _ in
                    if follow.following { unwhole() }
                }
                .onChange(of: atEnd && follow.following, initial: true) { _, at in looked(at) }
        }
    }

    /// Puts a chat that opens at a row at it, once.
    private func place(_ reader: ScrollViewProxy) {
        guard laidOut, !placed, let anchor else { return }
        // Put at the top of what it holds while the rows before are read
        // toward where it opens: it is put again when that row is known,
        // unless its reader has taken it by then.
        placed = !seeks
        landed = above
        follow = TranscriptFollow(opening: opens)
        position = ScrollPosition(idType: String.self)
        // By the row's id, which the lazy stack knows however far up it
        // is. After this update, since the rows may have arrived in it and
        // the stack has not laid them out yet.
        DispatchQueue.main.async { reader.scrollTo(anchor, anchor: .top) }
    }

    /// Rows arrived above the ones drawn. A transcript that does not follow
    /// its end stays at the turn that was its first, which its reader had
    /// at the top when the rows were asked for: without this the content
    /// grows above them and what they read moves down off the screen. A
    /// transcript still to be put at the row it opens at is left for that,
    /// and so is one put there with these very rows: they were read to
    /// reach the row, and keeping the old first turn would take it away.
    private func keep(_ reader: ScrollViewProxy, at above: RowsAbove?) {
        guard let above, placed, above != landed, !follow.following else { return }
        // A reader who went on down the transcript while the rows were out
        // no longer has that turn on screen: they stay at the first row
        // they do have. Had, before these rows were laid out (`RowsBefore`).
        let seen = before.seen
        let own = seen.filter { $0 == above.turn || $0.hasPrefix(above.turn + "/") }
        let row = own.contains(above.turn) ? above.turn : first(own.isEmpty ? seen : own) ?? above.turn
        position = ScrollPosition(idType: String.self)
        // Where the row stood, which inside a row taller than the screen
        // is not its top.
        let place = before.anchor(of: row, room: reach.measure.room)
        DispatchQueue.main.async { reader.scrollTo(place.row, anchor: place.anchor) }
    }

    private var scroll: some View {
        ScrollView { rows.environment(\.rowPlaces, places).environment(\.transcriptWhole, laysOutWhole) }
            // Made anew when its first rows arrive, and when it is found
            // with none on screen (`startOver`): a scroll view that is
            // made with its rows stands at its own end by the anchor
            // below, where one that is sent there afterwards goes by a
            // height the lazy stack has guessed.
            .id(TranscriptStart(rows: last != nil, again: starts, whole: laysOutWhole))
            .scrollPosition($position)
            // A chat opens at its end, and a short one lies from the top: it
            // has nothing to follow until it outgrows the screen. One made
            // anew while it follows its end stands at it too.
            .defaultScrollAnchor(opens == .end || follow.following ? .bottom : .top, for: .initialOffset)
            .defaultScrollAnchor(.top, for: .alignment)
            .onScrollPhaseChange { _, phase in
                switch phase {
                case .tracking, .interacting:
                    // A finger on it: the reader's, and where it opens is
                    // no longer the app's to say.
                    reach.touched = true
                    reach.started = 0
                    placed = true
                    if follow.following { leave(by: .touch) }
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
                    leave(by: .bar)
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
                // No row on screen, it may be: a transcript that follows
                // its end is then not at it, whatever the scroll view says
                // of its place.
                if rows.isEmpty { hold() }
            }
            .onChange(of: sends) { _, _ in
                unwhole()
                take(follow.sent())
            }
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
                if wheel.up, !wheel.gliding, follow.following { leave(by: .wheel) }
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
    private func leave(by hand: TranscriptFollow.Release) {
        placed = true
        follow.touched()
        position = ScrollPosition(idType: String.self)
        released(hand)
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
            ChatMotion.animate(.place) { go(to: end) }
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
    ///
    /// And only for the last of the way. `end` is worked out from the
    /// content's height, which a lazy stack guesses for the rows it has
    /// not laid out: a guess too tall puts the place past the rows there
    /// are, the stack lays none out there and so takes nothing back, and
    /// the scroll view says it is at its end over a blank screen. So while
    /// the end of the transcript's last row is further than the room from
    /// where it would stand, or the row is not laid out, the end is asked
    /// for by that row. Nearer, the place finishes the move, and not while
    /// it is more than twice the room away: the row's end is within the
    /// room of where it would stand and less than the room from the
    /// content's, so a place further off is a guess the stack has yet to
    /// take back, and what the scroll view says next asks again.
    /// With no row on screen nothing is asked for here: the rows have yet
    /// to say where they stand, or none is laid out (`startOver`).
    ///
    /// Which rows are on screen is the rows' own word (`RowPlaces`). The
    /// scroll view's list of them (`seen`) is told once a frame, and where
    /// it was told twice in one it has stayed empty over rows that were
    /// drawn.
    private func go(to end: Double) {
        #if os(macOS)
        position.scrollTo(edge: .bottom)
        #else
        let room = reach.measure.room
        if let last, !places.ends(last, near: room) {
            guard places.shows(in: room), let reader = reach.reader else { return }
            // Let go of first: a position still bound to a place is put
            // back there at the next change.
            position = ScrollPosition(idType: String.self)
            reader.scrollTo(last, anchor: .bottom)
        } else if last == nil || abs(end - reach.measure.top) <= 2 * room {
            position.scrollTo(y: end)
        }
        #endif
    }

    /// There are rows, more than the room holds, and none is on screen:
    /// the scroll view stands past them. A row asked for by its id is put
    /// by the stack's guess too where the rows before it are not laid out,
    /// and a stack that stands past its rows lays none out and keeps its
    /// guess, so neither a place nor a row asked for again moves it. An
    /// iPhone's and an iPad's fault: a Mac's scroll view is sent to its
    /// edge, and its rows do not say where they stand.
    private var lost: Bool {
        #if os(iOS)
        laidOut && last != nil && reach.measure.content > reach.measure.room && !places.shows(in: reach.measure.room)
        #else
        false
        #endif
    }

    /// A transcript laid out whole that has grown past what is cheap to
    /// lay out so is made anew with a lazy stack. Only where it is at its
    /// end or about to be put there, which is where one made anew stands.
    private func unwhole() {
        guard whole == true, count > Self.fewAtMost else { return }
        whole = false
        position = ScrollPosition(idType: String.self)
        starts += 1
    }

    /// A transcript that follows its end and has no row on screen is made
    /// anew, which stands it at its end by the scroll view's own anchor;
    /// laid out whole where it is not too long for that, since the anchor
    /// goes by the stack's guess too and has been seen to stand a lazy
    /// one past its rows time after time. After a moment, since a row
    /// says where it stands a little after it has moved; and a few times
    /// at most, counted until it is found with a row on screen.
    private func startOver() {
        guard !reach.starting, reach.started < 3 else { return }
        reach.starting = true
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(120))
            reach.starting = false
            guard follow.following, !reach.touched else { return }
            guard lost else {
                reach.started = 0
                return
            }
            reach.started += 1
            // Let go of first: the scroll view made anew would be put at
            // the place this one was last sent to.
            position = ScrollPosition(idType: String.self)
            if count <= Self.fewAtMost { whole = true }
            starts += 1
        }
    }

    /// Holds a transcript that follows its end to it, wherever the scroll
    /// view stands: short of it after the content grew, or past it after
    /// the room for it did or the content shrank. Whether it is there is
    /// worked out from what the scroll view last said of itself, and the
    /// end is asked for again a moment later where it did not get there,
    /// so a move lost to a change under way is made good: a few times for
    /// one reason, never in a loop.
    private func hold() {
        guard follow.following, !reach.touched, !reach.inMotion else {
            reach.asked = 0
            return
        }
        if lost { startOver() }
        guard let end = reach.measure.away else {
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

/// What a transcript's scroll view is made anew for: its first rows, and
/// each time it was found past them.
struct TranscriptStart: Hashable {
    var rows: Bool
    var again: Int
    var whole: Bool
}

extension EnvironmentValues {
    /// The transcript's rows are laid out whole (`TranscriptScroll.whole`).
    @Entry var transcriptWhole = false
}

/// The stack a transcript's rows lie in: lazy, or whole where its scroll
/// view says so.
struct TranscriptStack<Rows: View>: View {
    @Environment(\.transcriptWhole) private var whole
    @ViewBuilder let rows: Rows

    var body: some View {
        if whole {
            VStack(alignment: .leading, spacing: ChatSpace.turns) { rows }.scrollTargetLayout()
        } else {
            LazyVStack(alignment: .leading, spacing: ChatSpace.turns) { rows }.scrollTargetLayout()
        }
    }
}

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
    /// The scroll view's own way to a row, handed over each time the body
    /// is made.
    var reader: ScrollViewProxy?
    /// The transcript is waiting to see whether it has a row on screen,
    /// and how many times it was made anew since one was.
    var starting = false
    var started = 0
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
        if let older = screen.older { OlderRowsRow(older: older, thread: screen.thread, arrived: screen.above?.count ?? 0).id(OlderRowsRow.row) }
        // A turn's failure is a card at that turn, a row under its last;
        // the "New" line is a row above the first turn that was not read.
        ForEach(TranscriptPiece.pieces(of: screen.turns, failures: screen.failures.turns, newLine: screen.newLine, actions: screen.replyActions)) { piece in
            TranscriptPieceView(piece: piece, cards: screen.cards, expandThinking: screen.expandThinking, act: act).placed(piece.id)
        }
        if screen.typing {
            TypingDots()
        }
        // A sub-agent's asks: its own tool rows are in its own
        // conversation, so they wait here.
        ForEach(screen.approvals, id: \.self) { id in
            if let card = screen.cards[id]?.approval { ApprovalCardView(id: id, card: card).placed(id) }
        }
        ConversationStatus(screen: screen, key: key)
        if let technical = screen.technical {
            Grid(alignment: .leadingFirstTextBaseline, horizontalSpacing: ChatSpace.beside, verticalSpacing: ChatSpace.pair) {
                ForEach(technical, id: \.self) { fact in
                    GridRow {
                        Text(fact.label).fontWeight(ChatType.strong)
                        Text(fact.value).monospaced(fact.verbatim).textSelection(.enabled)
                    }
                }
            }
            .font(ChatType.small)
            .ink(.quiet, on: .page)
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
        VStack(alignment: .leading, spacing: ChatSpace.inset) {
            if let empty = hero.empty {
                ContentUnavailableView {
                    Label(hero.title, systemImage: "person.crop.circle.badge.questionmark")
                } description: {
                    Text(empty)
                }
            } else {
                Text(hero.title)
                    .font(ChatType.title)
                    .accessibilityAddTraits(.isHeader)
                if let description = hero.description {
                    Text(description)
                        .ink(.quiet, on: .page)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if let keyless = hero.keyless {
                    KeylessCalloutView(callout: keyless) { addKey(keyless.provider) }
                        .padding(.top, ChatSpace.lines)
                }
                VStack(alignment: .leading, spacing: ChatSpace.items) {
                    ForEach(hero.prompts, id: \.self) { prompt in
                        Button {
                            pick(prompt)
                        } label: {
                            Text(prompt)
                                .font(ChatType.detail)
                                .multilineTextAlignment(.leading)
                                .padding(.horizontal, ChatSpace.inset)
                                .padding(.vertical, ChatSpace.inner)
                                .frame(minHeight: ChatSize.target)
                                .background(ChatColours.pillStyle, in: .rect(cornerRadius: ChatSize.target / 2))
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(.top, ChatSpace.lines)
            }
        }
        .padding(.top, ChatSpace.turns)
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
                    .font(ChatType.note)
                    .ink(.quiet, on: .page)
            case .background:
                HStack(spacing: ChatSpace.beside) {
                    ProgressView().controlSize(.small)
                    Text(note.message)
                        .font(ChatType.detail)
                        .ink(.quiet, on: .page)
                }
            case .deleted:
                ProblemBox(title: note.title, message: note.message, tint: ChatColours.neutral, systemImage: "trash")
                    .card()
            }
        }
    }
}
