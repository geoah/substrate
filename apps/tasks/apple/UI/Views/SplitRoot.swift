import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// On an iPad and a Mac: the App menu's sections and the lists in the
/// sidebar, a list's tasks beside it, the open task in the detail.
struct SplitRoot: View {
    @Environment(TasksShell.self) private var shell
    let link: LinkPresentation<TasksPresentation>
    let app: TasksPresentation

    var body: some View {
        AppMenuSplit {
            SidebarView(sidebar: app.sidebar, link: link, app: app, menu: app.menu, select: { shell.send(.show($0)) })
        } content: {
            if app.menu.selected == TasksMenu.settings {
                AppSettingsView<TasksApp>()
            } else if let list = app.list {
                TaskListView(list: list, app: app, link: link)
            } else {
                ContentUnavailableView("Choose a list", systemImage: "checklist")
            }
        } detail: {
            if app.menu.selected != TasksMenu.settings, let detail = app.detail {
                TaskDetailView(detail: detail)
            } else if app.menu.selected != TasksMenu.settings {
                ContentUnavailableView("No task open", systemImage: "checkmark.circle", description: Text("Choose a task to see it here."))
            }
        }
    }
}
