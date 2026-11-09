import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// On a phone: a tab per section of the App menu, and apart from them at
/// the bar's trailing end the system's prominent tab, the round + that adds
/// a task (seeded with the list shown, and today on Needs you). Needs you
/// shows its rows at once and pushes the suggestions, the undated or a
/// task; Lists pushes a group, then a task; Settings pushes Collections,
/// then one collection. What is pushed is the presentation's, never the
/// view's: the list shown, the task open and the settings page, so a
/// reminder's tap or a command from anywhere moves the stack, and going
/// back is a command.
struct TabRoot: View {
    @Environment(TasksShell.self) private var shell
    let link: LinkPresentation<TasksPresentation>
    let app: TasksPresentation

    var body: some View {
        AppMenuView(
            app.menu, select: { shell.send(.select(section: $0)) },
            action: AppMenuAction(app.list?.newTask ?? "New Task", symbol: "plus") { shell.send(.newTask) }
        ) { section in
            content(section.id)
        }
        .environment(\.addsFromTabBar, true)
    }

    @ViewBuilder
    private func content(_ section: String) -> some View {
        switch section {
        case TasksMenu.needsYou:
            NavigationStack(path: path(home: .needsYou)) {
                if let list = app.list, list.selection == .needsYou {
                    TaskListView(list: list, app: app, link: link)
                        .navigationDestination(for: PhoneRoute.self) { route in pushed(route) }
                } else if app.list != nil || app.queue != nil {
                    // A list pushed over Needs you: Needs you is under it.
                    Color.clear.navigationDestination(for: PhoneRoute.self) { route in pushed(route) }
                } else {
                    ProgressView()
                }
            }
        case TasksMenu.settings:
            NavigationStack(path: settingsPath) {
                AppSettingsView<TasksApp>()
                    .navigationDestination(for: SettingsPage.self) { page in
                        switch page {
                        case .collections: LiveCollections()
                        case .collection: LiveCollection()
                        }
                    }
            }
        default:
            NavigationStack(path: path(home: nil)) {
                SidebarView(sidebar: app.sidebar, link: link, app: app, select: { shell.send(.show($0)) })
                    .navigationDestination(for: PhoneRoute.self) { route in pushed(route) }
            }
        }
    }

    @ViewBuilder
    private func pushed(_ route: PhoneRoute) -> some View {
        switch route {
        case .list: LivePushedList()
        case .task: LivePushedDetail()
        }
    }

    /// The stack the presentation holds: the list shown over the tab's home
    /// (Needs you's, or none for Lists), then the task open. A pop the
    /// system makes (a back swipe) is the command that closes what it took
    /// off.
    private func path(home: ListSelection?) -> Binding<[PhoneRoute]> {
        let shown = app.queue != nil ? ListSelection.suggestions : app.list?.selection
        let list = shown.flatMap { $0 == home ? nil : PhoneRoute.list($0) }
        let task = (app.detail?.id ?? app.suggestion?.id).map(PhoneRoute.task)
        let current = [list, task].compactMap { $0 }
        return Binding(
            get: { current },
            set: { next in
                guard next.count < current.count else { return }
                if task != nil, !next.contains(where: { if case .task = $0 { true } else { false } }) { shell.send(.open(task: nil)) }
                if list != nil, next.isEmpty { shell.send(.show(home)) }
            })
    }

    /// Settings' stack: Collections, then one collection.
    private var settingsPath: Binding<[SettingsPage]> {
        let current: [SettingsPage] =
            switch app.settingsPage {
            case nil: []
            case .collections?: [.collections]
            case let .collection(kind)?: [.collections, .collection(kind)]
            }
        return Binding(
            get: { current },
            set: { next in
                guard next.count < current.count else { return }
                shell.send(.showSettings(next.last))
            })
    }
}

/// What a phone's stack holds, by what it shows.
enum PhoneRoute: Hashable {
    case list(ListSelection)
    case task(String)
}

/// A pushed screen reads the presentation where it is drawn: a navigation
/// destination keeps the closure it was made with, so a value handed down
/// to it would stay the one it was pushed with.
private struct LivePushedList: View {
    @Environment(TasksShell.self) private var shell

    var body: some View {
        let link = shell.presentation
        if let app = link.app {
            if let queue = app.queue {
                SuggestionsView(queue: queue, app: app, link: link)
            } else if let list = app.list {
                TaskListView(list: list, app: app, link: link)
            }
        }
    }
}

private struct LivePushedDetail: View {
    @Environment(TasksShell.self) private var shell

    var body: some View {
        if let suggestion = shell.presentation.app?.suggestion {
            SuggestionView(suggestion: suggestion)
        } else if let detail = shell.presentation.app?.detail {
            TaskDetailView(detail: detail)
        }
    }
}

private struct LiveCollections: View {
    @Environment(TasksShell.self) private var shell

    var body: some View {
        if let collections = shell.presentation.app?.collections { CollectionsView(collections: collections) }
    }
}

private struct LiveCollection: View {
    @Environment(TasksShell.self) private var shell

    var body: some View {
        if let collection = shell.presentation.app?.collection { CollectionView(collection: collection) }
    }
}
