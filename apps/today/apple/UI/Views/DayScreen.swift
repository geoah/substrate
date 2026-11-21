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

    /// - Parameters:
    ///   - path: the pages open at first, for a gallery that draws one.
    ///   - opened: what is opened in place at first, likewise.
    init(
        app: TodayPresentation, path: [TodayRoute] = [], opened: Set<String> = [],
        send: @escaping @MainActor @Sendable (TodayCommand) -> Void = { _ in }, openSettings: (() -> Void)? = nil,
        @ViewBuilder notices: @escaping () -> Notices
    ) {
        self.app = app
        self.send = send
        self.openSettings = openSettings
        self.notices = notices
        _path = State(initialValue: path)
        let state = PaneViewState()
        state.opened = opened
        _state = State(initialValue: state)
    }

    var body: some View {
        GeometryReader { proxy in
            let wide = proxy.size.width >= 900
            ZStack {
                GroundView(ground: app.pane.ground)
                if wide { wideLayout } else { narrowLayout }
            }
        }
        .environment(state)
        .environment(\.pane, actions)
        .environment(\.words, app.words)
        .environment(\.ground, app.pane.ground)
        .environment(\.colorScheme, app.pane.ground.scheme(in: system))
        .tint(TodayLook.ink.color)
    }

    private var actions: PaneActions {
        let openURL = openURL
        return PaneActions(
            send: send,
            open: { route in
                guard path.last != route else { return }
                withAnimation(.snappy(duration: 0.25)) { path.append(route) }
            },
            openURL: { address in
                if let url = URL(string: address) { openURL(url) }
            })
    }

    private func back() {
        guard !path.isEmpty else { return }
        withAnimation(.snappy(duration: 0.25)) { _ = path.removeLast() }
    }

    // MARK: A phone

    @ViewBuilder private var narrowLayout: some View {
        if let route = path.last {
            PageView(page: app.page(route), back: back) { field }
                .id(route)
                .transition(.move(edge: .trailing).combined(with: .opacity))
        } else {
            day
        }
    }

    private var day: some View {
        PaneView(pane: app.pane, openSettings: openSettings, header: notices)
            .safeAreaInset(edge: .bottom, spacing: 0) { field }
            .safeAreaInset(edge: .top, spacing: 0) { WindowControlsRoom() }
            .edgeFade(.top)
    }

    private var field: some View {
        ComposerBar(composer: app.pane.composer, answer: app.pane.answer).edgeFade(.bottom)
    }

    // MARK: A wide screen

    private var wideLayout: some View {
        HStack(alignment: .top, spacing: 0) {
            day.frame(minWidth: 420, idealWidth: 640, maxWidth: 700)
            Group {
                if let route = path.last {
                    side(for: app.page(route), back: back).id(route)
                } else {
                    side(for: app.page(.projects), back: nil)
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

extension DayScreen where Notices == EmptyView {
    init(
        app: TodayPresentation, path: [TodayRoute] = [], opened: Set<String> = [],
        send: @escaping @MainActor @Sendable (TodayCommand) -> Void = { _ in }, openSettings: (() -> Void)? = nil
    ) {
        self.init(app: app, path: path, opened: opened, send: send, openSettings: openSettings) { EmptyView() }
    }
}
