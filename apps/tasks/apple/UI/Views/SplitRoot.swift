import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// On an iPad and a Mac: the App menu's sections, the suggestions, the
/// undated, the pinned groups and each group kind's busiest groups in the
/// sidebar; the list or the suggestions beside it; the open task or
/// suggestion in the detail. Under Settings, Collections and one
/// collection are the detail.
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
            } else if let queue = app.queue {
                SuggestionsView(queue: queue, app: app, link: link)
            } else if let list = app.list {
                TaskListView(list: list, app: app, link: link)
            } else {
                ContentUnavailableView("Choose a list", systemImage: "checklist")
            }
        } detail: {
            if app.menu.selected == TasksMenu.settings {
                if let collection = app.collection {
                    NavigationStack { CollectionView(collection: collection, approval: app.approval) }
                } else if app.settingsPage == .collections {
                    NavigationStack { CollectionsView(collections: app.collections, approval: app.approval) }
                }
            } else if let suggestion = app.suggestion {
                SuggestionView(suggestion: suggestion)
            } else if let detail = app.detail {
                TaskDetailView(detail: detail)
            } else {
                ContentUnavailableView("No task open", systemImage: "checkmark.circle", description: Text("Choose a task to see it here."))
            }
        }
    }
}
