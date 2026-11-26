import SubstrateScreens
import SubstrateUI
import SwiftUI
import TodayCore

/// The app's whole screen from one presentation: the pane on its ground,
/// the page a route names in its place, and the field fixed at the bottom.
/// A window with the room draws more beside the day (`DayColumns`): the
/// conversation with the assistant in a column of its own, and with more
/// room still the projects, or the open page, between the two, so the day
/// stays in view. Navigation is the screen's own: a path of routes, one
/// level deep in what it shows, with back returning to where it was.
struct DayScreen<Notices: View>: View {
    var app: TodayPresentation
    /// What a view may do. A drawing's go nowhere.
    var send: @MainActor @Sendable (TodayCommand) -> Void
    var openSettings: (() -> Void)?
    @ViewBuilder var notices: () -> Notices

    @State private var path: [TodayRoute]
    @State private var state: PaneViewState
    @Environment(\.colorScheme) private var system
    @Environment(\.openURL) private var openURL
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.accessibilityReduceMotion) private var reduced
    /// The links the app was opened with; none where a gallery draws.
    @Environment(OpenedLinks.self) private var links: OpenedLinks?
    /// The chat's own screen; none where a gallery draws.
    @Environment(\.chatScreen) private var chatScreen
    @Environment(\.chatColumn) private var chatColumn
    /// The conversation that was asked for: over the day as a sheet where
    /// the day has the window to itself, and in the conversation's column
    /// where there is one, in place of the day's own until it is closed.
    @State private var asking: Asking?
    /// The conversation has a column of its own, so no sheet stands over the day.
    @State private var chatBeside = false

    /// A conversation opened from the field or from a thing's box: which,
    /// and the words to send as it opens, until they are sent.
    private struct Asking: Identifiable {
        let id = UUID()
        var door: ChatDoor
        var message: String?
        /// It was opened with no words to send: to write in.
        let writes: Bool

        init(door: ChatDoor, message: String?) {
            self.door = door
            self.message = message
            writes = message == nil
        }
    }

    /// - Parameters:
    ///   - path: the pages open at first, for a gallery that draws one.
    ///   - opened: what is opened in place at first, likewise.
    ///   - showsDone: whether what is done is shown, for a gallery; nil is
    ///     the choice the device kept.
    ///   - finishing: finishes that began at an instant, drawn at another
    ///     with no clock running, for a gallery.
    init(
        app: TodayPresentation, path: [TodayRoute] = [], opened: Set<String> = [], showsDone: Bool? = nil,
        finishing: (commands: [TodayCommand], began: Date, at: Date)? = nil,
        send: @escaping @MainActor @Sendable (TodayCommand) -> Void = { _ in }, openSettings: (() -> Void)? = nil,
        @ViewBuilder notices: @escaping () -> Notices
    ) {
        self.app = app
        self.send = send
        self.openSettings = openSettings
        self.notices = notices
        _path = State(initialValue: path)
        let state = PaneViewState(showsDone: showsDone)
        state.opened = opened
        if let finishing {
            for command in finishing.commands { state.draw(command, began: finishing.began, at: finishing.at) }
        }
        _state = State(initialValue: state)
    }

    var body: some View {
        let page = path.last.map { app.page($0) }
        GeometryReader { proxy in
            // The window's whole height: under a keyboard the room left is lower, and the columns must stay as they are.
            let height = proxy.size.height + proxy.safeAreaInsets.top + proxy.safeAreaInsets.bottom
            let columns = DayColumns.fit(width: proxy.size.width, height: height, chat: dayDoor != nil)
            let chatWidth = columns.chatWidth(in: proxy.size.width)
            ZStack {
                // One ground under every column: the conversation stands on it as a card, as a page does, and
                // the window is one place from edge to edge.
                GroundView(ground: app.pane.ground)
                HStack(spacing: 0) {
                    layout(
                        page, beside: columns == .beside || columns == .all,
                        side: columns == .all ? columns.sideWidth(in: proxy.size.width - chatWidth) : nil,
                        // The conversation's column has the chat's own field: the day needs none under it.
                        field: !columns.showsChat
                    )
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    // The day's ink is its tint; the conversation beside it keeps the chat's own.
                    .tint(TodayLook.ink.color)
                    if columns.showsChat, let dayDoor {
                        chatColumn(day: dayDoor).frame(width: chatWidth)
                    }
                }
            }
            // Light or dark is the ground's to say, for every column: a card is dark on the closing ground
            // whatever the system's mode, the conversation's card with the rest.
            .environment(\.colorScheme, app.pane.ground.scheme(in: system))
            .onChange(of: columns.showsChat, initial: true) { _, shows in chatBeside = shows }
        }
        // What the day changes by itself moves as what a tap changes does (`Motion`): the current item is
        // finished and the next takes its place, an answer arrives over the field, a line of the night is
        // read. While an answer is being written the day changes with every word, and is drawn at once.
        .animation(app.writing ? nil : Motion.panel(still: reduced), value: Drawn(pane: app.pane, page: page, notices: app.notices))
        .onChange(of: reduced, initial: true) { _, reduced in state.reduced = reduced }
        // What was just sent is in the day now: it is drawn from its records again.
        .onChange(of: app.pane) { _, pane in state.followed(open: pane.unfinished) }
        .environment(state)
        .environment(\.pane, actions)
        .environment(\.words, app.words)
        .environment(\.ground, app.pane.ground)
        // A finish waits a few seconds so it can be taken back; leaving
        // the app or the screen sends what waits, so none is lost.
        .onChange(of: scenePhase) { _, phase in
            if phase != .active { state.flush() }
        }
        .onDisappear { state.flush() }
        // A page is on the screen: the core is told, since a project's page has what its timeline was written from read.
        .onChange(of: path.last, initial: true) { _, route in
            if let route { send(.opened(route)) }
        }
        // A link the app was opened with: its page takes the place of whatever was open, and the day's own closes it.
        .onChange(of: links?.waiting, initial: true) { _, waiting in
            guard waiting != nil, let link = links?.take() else { return }
            withAnimation(state.motion) { path = link.route.map { [$0] } ?? [] }
        }
        .sensoryFeedback(.impact(weight: .light), trigger: state.begun)
        .sensoryFeedback(.success, trigger: state.sent)
        // The chat's screen is its own: it stands over the day in the app's
        // colours, not the ground's, so it is attached outside them. Where
        // the conversation has a column there is no sheet: the column shows
        // what was asked for.
        .sheet(item: sheetAsking) { asking in
            NavigationStack {
                conversation(asking.door, request: asking.id, closes: true, writes: true)
            }
            #if os(macOS)
            .frame(minWidth: 520, idealWidth: 640, minHeight: 560, idealHeight: 760)
            #endif
        }
        #if DEBUG
        // A debug build asked to open on the day's conversation, so its
        // screen can be looked at with nothing pressed (`TodayDebug`).
        .task {
            guard TodayDebug.value("SUBSTRATE_TODAY_CHAT") != nil, asking == nil, let door = app.pane.composer.chat else { return }
            chat?(door, nil)
        }
        #endif
    }

    private var actions: PaneActions {
        let openURL = openURL
        let state = state
        return PaneActions(
            send: { command in
                // What finishes a thing waits where it can be taken back, and is sent when its time is up.
                if command.finishing != nil { return state.finish(command, deliver: send) }
                // What was typed under "Other" is the view's until its Save: it goes with that Save, and is the view's no more.
                guard case let .saveSuggested(ids, _) = command else { return send(command) }
                send(.saveSuggested(ids, words: state.words.filter { ids.contains($0.key) }))
                for id in ids { state.words[id] = nil }
            },
            open: { route in
                guard path.last != route else { return }
                withAnimation(state.motion) { path.append(route) }
            },
            openURL: { address in
                if let url = URL(string: address) { openURL(url) }
            },
            chat: chat)
    }

    /// Opens a conversation over the day, where the app has the chat's
    /// screen. What waits to be finished is sent first: the assistant then
    /// reads the day as it stands.
    private var chat: (@MainActor @Sendable (ChatDoor, String?) -> Void)? {
        guard chatScreen != nil else { return nil }
        let state = state
        return { door, message in
            state.flush()
            asking = Asking(door: door, message: message)
        }
    }

    private func back() {
        guard !path.isEmpty else { return }
        withAnimation(state.motion) { _ = path.removeLast() }
    }

    // MARK: The conversation

    /// The day's own conversation, which the conversation's column shows
    /// while nothing else was asked for. Nil where the screen has no chat
    /// (a drawing, a fixture), and on a Mac, whose conversations stand over
    /// the day as a sheet: the columns then are the day and a page alone.
    private var dayDoor: ChatDoor? {
        chatColumn && chatScreen != nil ? app.pane.composer.chat : nil
    }

    /// What the sheet shows: what was asked for, while the conversation has
    /// no column. A swipe down asks for it no more. Where the sheet closes
    /// because the conversation got a column (the window grew, an iPhone
    /// Duo was opened), what was asked for stays, and the column shows it.
    private var sheetAsking: Binding<Asking?> {
        Binding(
            get: { chatBeside ? nil : asking },
            set: { shown in
                guard shown == nil, !chatBeside else { return }
                asking = nil
            })
    }

    /// The chat's own screen for a conversation. The words it was asked
    /// with are the request's: the screen takes them when it has the
    /// conversation to send to, and they are then held here no longer, so a
    /// screen made for the same request again (a sheet that became a
    /// column) sends nothing. Closing it closes that request alone.
    ///
    /// - Parameters:
    ///   - request: what was asked for (`Asking.id`); nil for the day's own
    ///     conversation in its column, which nothing asked for.
    ///   - closes: whether the screen has a Done.
    ///   - writes: whether a conversation with nothing in it yet takes the
    ///     keyboard (`ChatScreen.make`).
    @ViewBuilder private func conversation(_ door: ChatDoor, request: UUID?, closes: Bool, writes: Bool) -> some View {
        if let chatScreen {
            chatScreen.make(
                door, { take(request) },
                closes
                    ? {
                        if asking?.id == request { asking = nil }
                    } : nil,
                writes
            ) { thread in
                send(.chatThread(scope: door.scope, thread: thread))
            }
        }
    }

    /// The words a request was made with, once: they are the conversation's from here.
    private func take(_ request: UUID?) -> String? {
        guard let request, var asked = asking, asked.id == request, let message = asked.message else { return nil }
        asked.message = nil
        asking = asked
        return message
    }

    /// The conversation in a column of its own, beside the day: the day's,
    /// or the one a thing's box opened, which Done closes back to the
    /// day's. A hairline parts it from the day's ground.
    private func chatColumn(day: ChatDoor) -> some View {
        // What was asked for of the day's own conversation is the day's conversation: nothing to close back to.
        let thing = asking.flatMap { $0.door.scope == day.scope ? nil : $0 }
        let card = RoundedRectangle(cornerRadius: TodayLook.cardRadius, style: .continuous)
        // The column's one navigation stack, for as long as the column is there: a conversation takes another's
        // place inside it. A stack made with each conversation, while the keyboard a question was typed on was
        // leaving, kept the keyboard's room, and the chat's field stood half way up the column.
        return NavigationStack {
            conversation(thing?.door ?? day, request: asking?.id, closes: thing != nil, writes: asking?.writes ?? false)
                // A conversation that was asked for is a screen of its own, so its words are sent as it opens; the
                // day's is the day's own, and tomorrow's is another.
                .id(asking?.id.uuidString ?? day.scope)
                #if os(iOS)
            // The stack's own fill is the card's, not the system's page.
            .containerBackground(TodayLook.card.color, for: .navigation)
                #endif
        }
        // A card on the day's ground, as a page beside the day is: the same fill, corners and shadow, its top
        // where the day's first card and a page's are, and the same room from the window's edges. A panel of
        // the system's page colour from edge to edge, parted from the day by a line, read as another app's.
        .background(.card)
        .clipShape(card)
        .cardShadow()
        .padding(.top, TodayLook.columnTop)
        .padding([.horizontal, .bottom], TodayLook.side)
        // To the window's own bottom edge, as the day's ground goes: the room a keyboard takes is still kept.
        .ignoresSafeArea(.container, edges: .bottom)
    }

    // MARK: The day and its page

    /// The day, and the page a route names: over the day where the window
    /// has one column for the two, beside it where it has a column each,
    /// the list of projects standing there while no page is open.
    ///
    /// The day is one view in every arrangement, so it keeps its place (how
    /// far it is scrolled, what is open in it) when the window changes its
    /// columns: a fold opened or closed, a device turned, a window resized.
    ///
    /// Over the day, a page comes in from the trailing edge and leaves the
    /// way it came; with Reduce Motion it fades in its place. The day stays
    /// where it is under the page and fades as the page covers it, so the
    /// screen is never empty between the two, and going back is to where
    /// the day was scrolled.
    ///
    /// - Parameters:
    ///   - beside: the page has a column of its own beside the day.
    ///   - side: that column's width where the conversation has the third
    ///     column and the day has the rest; nil where the two have the
    ///     window to themselves and stand in its middle.
    ///   - field: whether the field stands under the day and under a page
    ///     over it. Where the conversation has a column, its own field is
    ///     the one field.
    private func layout(_ page: PagePresentation?, beside: Bool, side width: CGFloat?, field shown: Bool) -> some View {
        let covered = !beside && page != nil
        let centred = beside && width == nil
        return ZStack {
            HStack(alignment: .top, spacing: 0) {
                day(field: shown)
                    .frame(minWidth: centred ? 420 : nil, idealWidth: centred ? 640 : nil, maxWidth: centred ? 700 : .infinity)
                    .opacity(covered ? 0 : 1)
                    .allowsHitTesting(!covered)
                    .accessibilityHidden(covered)
                if beside {
                    // One page fades as the next comes, beside the day.
                    ZStack(alignment: .top) {
                        if let route = path.last, let page {
                            side(for: page, back: back).id(route).transition(.opacity)
                        } else {
                            side(for: app.page(.projects), back: nil).transition(.opacity)
                        }
                    }
                    .frame(minWidth: width ?? 340, idealWidth: width ?? 420, maxWidth: width ?? 520)
                }
            }
            .frame(maxWidth: .infinity, alignment: .center)
            if !beside, let route = path.last, let page {
                Group {
                    // A page with nothing fixed at its bottom is told so by its type: an empty one would still be given a fade.
                    if shown {
                        PageView(page: page, back: back) { field }
                    } else {
                        PageView(page: page, back: back) { EmptyView() }
                    }
                }
                .id(route)
                .transition(reduced ? .opacity : .move(edge: .trailing).combined(with: .opacity))
                .zIndex(1)
            }
        }
    }

    private func day(field shown: Bool) -> some View {
        PaneView(pane: app.pane, openSettings: openSettings, header: notices)
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if shown { field }
            }
            .safeAreaInset(edge: .top, spacing: 0) { WindowControlsRoom() }
            .edgeFade(.top)
    }

    @ViewBuilder private var field: some View {
        // Above the keyboard it would lie over the field being typed in.
        if !state.writing { ComposerBar(composer: app.pane.composer, answer: app.pane.answer).edgeFade(.bottom).transition(.opacity) }
    }

    /// A page beside the day: the same page, without a field of its own.
    /// With no page open it is the list of projects, with no bar: the strip
    /// beside it already holds the way to the priorities.
    @ViewBuilder private func side(for page: PagePresentation, back: (() -> Void)?) -> some View {
        if back == nil, case let .projects(projects) = page {
            PageScaffold(back: nil) {
                ProjectsPageView(page: projects)
            } bottom: {
                EmptyView()
            }
        } else {
            PageView(page: page, back: back) { EmptyView() }
        }
    }
}

extension PanePresentation {
    /// What in the day can still be finished, as a finish names it
    /// (`TodayCommand.finishing`): every row that is not done, and each
    /// item of a routine that is not logged.
    var unfinished: Set<String> {
        var out: Set<String> = []
        for case let .row(row) in items where row.state != .done {
            out.insert(row.id)
            if case let .routine(card) = row.body {
                for item in card.items where item.logged == nil { out.insert(item.id) }
            }
        }
        return out
    }
}

/// What is on the screen from the day's records: when it changes, what
/// changed is seen to move (`Motion`).
private struct Drawn: Equatable {
    var pane: PanePresentation
    var page: PagePresentation?
    var notices: [NoticeLine]
}

extension DayScreen where Notices == EmptyView {
    init(
        app: TodayPresentation, path: [TodayRoute] = [], opened: Set<String> = [], showsDone: Bool? = nil,
        finishing: (commands: [TodayCommand], began: Date, at: Date)? = nil,
        send: @escaping @MainActor @Sendable (TodayCommand) -> Void = { _ in }, openSettings: (() -> Void)? = nil
    ) {
        self.init(app: app, path: path, opened: opened, showsDone: showsDone, finishing: finishing, send: send, openSettings: openSettings) {
            EmptyView()
        }
    }
}
