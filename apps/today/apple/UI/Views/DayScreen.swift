import SubstrateScreens
import SubstrateUI
import SwiftUI
import TodayCore

/// The app's whole screen from one presentation: the pane on its ground,
/// the page a route names in its place, and the field fixed at the bottom.
/// On a wide screen it is the same pane with the page beside it, so the
/// day stays in view. Navigation is the screen's own: a path of routes,
/// one level deep in what it shows, with back returning to where it was.
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
            let wide = proxy.size.width >= 900
            ZStack {
                GroundView(ground: app.pane.ground)
                if wide { wideLayout(page) } else { narrowLayout(page) }
            }
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
        .environment(\.colorScheme, app.pane.ground.scheme(in: system))
        .tint(TodayLook.ink.color)
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
            })
    }

    private func back() {
        guard !path.isEmpty else { return }
        withAnimation(state.motion) { _ = path.removeLast() }
    }

    // MARK: A phone

    /// A page comes in from the trailing edge over the day and leaves the
    /// way it came; with Reduce Motion it fades in its place. The day stays
    /// where it is under the page and fades as the page covers it, so the
    /// screen is never empty between the two, and going back is to where
    /// the day was scrolled.
    @ViewBuilder private func narrowLayout(_ page: PagePresentation?) -> some View {
        day
            .opacity(page == nil ? 1 : 0)
            .allowsHitTesting(page == nil)
            .accessibilityHidden(page != nil)
        if let route = path.last, let page {
            PageView(page: page, back: back) { field }
                .id(route)
                .transition(reduced ? .opacity : .move(edge: .trailing).combined(with: .opacity))
                .zIndex(1)
        }
    }

    private var day: some View {
        PaneView(pane: app.pane, openSettings: openSettings, header: notices)
            .safeAreaInset(edge: .bottom, spacing: 0) { field }
            .safeAreaInset(edge: .top, spacing: 0) { WindowControlsRoom() }
            .edgeFade(.top)
    }

    @ViewBuilder private var field: some View {
        // Above the keyboard it would lie over the field being typed in.
        if !state.writing { ComposerBar(composer: app.pane.composer, answer: app.pane.answer).edgeFade(.bottom).transition(.opacity) }
    }

    // MARK: A wide screen

    private func wideLayout(_ page: PagePresentation?) -> some View {
        HStack(alignment: .top, spacing: 0) {
            day.frame(minWidth: 420, idealWidth: 640, maxWidth: 700)
            // One page fades as the next comes, beside the day.
            ZStack(alignment: .top) {
                if let route = path.last, let page {
                    side(for: page, back: back).id(route).transition(.opacity)
                } else {
                    side(for: app.page(.projects), back: nil).transition(.opacity)
                }
            }
            .frame(minWidth: 340, idealWidth: 420, maxWidth: 520)
        }
        .frame(maxWidth: .infinity, alignment: .center)
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
