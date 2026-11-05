import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// On a phone: a tab per section of the App menu. Lists pushes a list, then
/// a task; Today shows its list at once and pushes a task. What is pushed is
/// the presentation's, never the view's: the list shown and the task open,
/// so a reminder's tap or a command from anywhere moves the stack, and
/// going back is a command.
///
/// The bar is Tasks' own: the sections' island and, beside it on the same
/// row, the round + that adds a task, two shapes of one Liquid Glass. The
/// system's tab bar puts no button of an app's beside itself.
struct TabRoot: View {
    @Environment(TasksShell.self) private var shell
    let link: LinkPresentation<TasksPresentation>
    let app: TasksPresentation

    var body: some View {
        content(app.menu.selected)
            .id(app.menu.selected)
            .safeAreaInset(edge: .bottom, spacing: 0) {
                TasksTabBar(menu: app.menu, adds: app.menu.selected != TasksMenu.settings, newTask: app.list?.newTask ?? "New Task")
            }
            .environment(\.addsFromTabBar, true)
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

/// The phone's bar: an island of the sections, each its symbol over its
/// name, the chosen one lifted; and, apart from it on the same row, the
/// round + where there are tasks to add to.
private struct TasksTabBar: View {
    @Environment(TasksShell.self) private var shell
    let menu: AppMenuPresentation
    let adds: Bool
    let newTask: String

    var body: some View {
        GlassEffectContainer(spacing: 12) {
            HStack(spacing: 12) {
                HStack(spacing: 0) {
                    ForEach(menu.sections) { section in item(section) }
                }
                .padding(4)
                .glassEffect(.regular.interactive(), in: .capsule)
                if adds {
                    Button {
                        shell.send(.newTask)
                    } label: {
                        Image(systemName: "plus")
                            .font(.system(size: 22, weight: .semibold))
                            .foregroundStyle(TasksSymbols.card)
                            .frame(width: 62, height: 62)
                            .contentShape(.circle)
                    }
                    .buttonStyle(.plain)
                    .glassEffect(.regular.tint(Color.primary).interactive(), in: .circle)
                    .accessibilityLabel(newTask)
                }
            }
        }
        .padding(.horizontal, 16)
        // Into the home indicator's margin, where the system's bar sits.
        .padding(.bottom, -10)
    }

    private func item(_ section: AppMenuSection) -> some View {
        let chosen = section.id == menu.selected
        return Button {
            shell.send(.select(section: section.id))
        } label: {
            VStack(spacing: 2) {
                Image(systemName: DesignTokens.menuSymbol(section.symbol).sfSymbol)
                    .font(.system(size: 19, weight: .medium))
                    .frame(height: 24)
                    .overlay(alignment: .topTrailing) {
                        if let badge = section.badge, badge > 0 {
                            Text(badge, format: .number)
                                .font(.caption2.weight(.bold))
                                .foregroundStyle(.white)
                                .padding(.horizontal, 5)
                                .frame(minWidth: 17, minHeight: 17)
                                .background(Color.red, in: .capsule)
                                .offset(x: 12, y: -6)
                        }
                    }
                Text(section.title).font(.caption2.weight(.semibold))
            }
            .foregroundStyle(chosen ? Color.brandAccent : Color.primary)
            .frame(width: 74, height: 54)
            .background {
                if chosen { Capsule().fill(Color.primary.opacity(0.08)) }
            }
            .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(section.title)
        .accessibilityAddTraits(chosen ? .isSelected : [])
    }
}
