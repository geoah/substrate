import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// On a phone: a tab per section of the App menu. Lists pushes a list,
/// then a task; Today shows its list at once.
struct TabRoot: View {
    @Environment(TasksShell.self) private var shell
    let link: LinkPresentation<TasksPresentation>
    let app: TasksPresentation
    @State private var pushed: ListSelection?

    var body: some View {
        TabView(selection: Binding(get: { app.menu.selected }, set: { shell.send(.select(section: $0)) })) {
            ForEach(app.menu.sections) { section in
                Tab(section.title, systemImage: TasksSymbols.symbol(section.symbol), value: section.id) {
                    content(section.id)
                }
                .badge(section.badge ?? 0)
            }
        }
        .onChange(of: app.menu.selected) { _, _ in pushed = nil }
    }

    @ViewBuilder
    private func content(_ section: String) -> some View {
        switch section {
        case TasksMenu.today:
            NavigationStack {
                if let list = app.list, list.selection == .today {
                    TaskListView(list: list, app: app, link: link)
                        .navigationDestination(item: openTask) { _ in LivePushedDetail() }
                } else {
                    ProgressView()
                }
            }
        case TasksMenu.settings:
            NavigationStack { SettingsView() }
        default:
            NavigationStack {
                SidebarView(sidebar: app.sidebar, link: link, app: app, select: { selection in
                    shell.send(.show(selection))
                    pushed = selection
                })
                .navigationDestination(item: $pushed) { _ in
                    LivePushedList()
                }
            }
        }
    }

    private var openTask: Binding<String?> {
        Binding(get: { app.detail?.id }, set: { shell.send(.open(task: $0)) })
    }
}

/// A pushed screen reads the presentation where it is drawn: a navigation
/// destination keeps the closure it was made with, so a value handed down
/// to it would stay the one it was pushed with.
private struct LivePushedList: View {
    @Environment(TasksShell.self) private var shell

    var body: some View {
        let link = shell.model.presentation
        if let app = link.app, let list = app.list {
            TaskListView(list: list, app: app, link: link)
                .navigationDestination(item: Binding(get: { app.detail?.id }, set: { shell.send(.open(task: $0)) })) { _ in
                    LivePushedDetail()
                }
        }
    }
}

private struct LivePushedDetail: View {
    @Environment(TasksShell.self) private var shell

    var body: some View {
        if let detail = shell.model.presentation.app?.detail { TaskDetailView(detail: detail) }
    }
}
