import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// On a phone: a tab per section of the App menu. Lists pushes a list, then
/// a task; Today shows its list at once and pushes a task. What is pushed is
/// the presentation's, never the view's: the list shown and the task open,
/// so a reminder's tap or a command from anywhere moves the stack, and
/// going back is a command.
struct TabRoot: View {
    @Environment(TasksShell.self) private var shell
    let link: LinkPresentation<TasksPresentation>
    let app: TasksPresentation

    var body: some View {
        AppMenuView(app.menu, select: { shell.send(.select(section: $0)) }) { section in
            content(section.id)
        }
        .environment(\.addsFromTabBar, true)
        // The round + stands on its own to the right of the tab bar, level
        // with it, wherever there are tasks to add to.
        .overlay(alignment: .bottomTrailing) {
            if app.menu.selected != TasksMenu.settings {
                NewTaskButton(title: app.list?.newTask ?? "New Task")
                    .padding(.trailing, 7)
                    .padding(.bottom, -25)
            }
        }
    }

    @ViewBuilder
    private func content(_ section: String) -> some View {
        switch section {
        case TasksMenu.today:
            NavigationStack(path: path(listed: false)) {
                if let list = app.list, list.selection == .today {
                    TaskListView(list: list, app: app, link: link)
                        .navigationDestination(for: PhoneRoute.self) { _ in LivePushedDetail() }
                } else {
                    ProgressView()
                }
            }
        case TasksMenu.settings:
            NavigationStack { AppSettingsView<TasksApp>() }
        default:
            NavigationStack(path: path(listed: true)) {
                SidebarView(sidebar: app.sidebar, link: link, app: app, select: { shell.send(.show($0)) })
                    .navigationDestination(for: PhoneRoute.self) { route in
                        switch route {
                        case .list: LivePushedList()
                        case .task: LivePushedDetail()
                        }
                    }
            }
        }
    }

    /// The stack the presentation holds: the list shown (in Lists), then
    /// the task open. A pop the system makes (a back swipe) is the command
    /// that closes what it took off.
    private func path(listed: Bool) -> Binding<[PhoneRoute]> {
        let list = listed ? app.list.map { PhoneRoute.list($0.selection) } : nil
        let task = app.detail.map { PhoneRoute.task($0.id) }
        let current = [list, task].compactMap { $0 }
        return Binding(
            get: { current },
            set: { next in
                guard next.count < current.count else { return }
                if task != nil, !next.contains(where: { if case .task = $0 { true } else { false } }) { shell.send(.open(task: nil)) }
                if list != nil, next.isEmpty { shell.send(.show(nil)) }
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
        if let app = link.app, let list = app.list {
            TaskListView(list: list, app: app, link: link)
        }
    }
}

private struct LivePushedDetail: View {
    @Environment(TasksShell.self) private var shell

    var body: some View {
        if let detail = shell.presentation.app?.detail { TaskDetailView(detail: detail) }
    }
}
