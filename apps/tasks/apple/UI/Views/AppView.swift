import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// The App menu's sections (§5.6): a tab bar on a phone, a sidebar on an
/// iPad and a Mac, the same presentation under both. What is open over them
/// (an editor, a question, a problem) is drawn here once.
struct AppView: View {
    @Environment(TasksShell.self) private var shell
    let link: LinkPresentation<TasksPresentation>
    let app: TasksPresentation

    var body: some View {
        AppMenuRoot {
            TabRoot(link: link, app: app)
        } split: {
            SplitRoot(link: link, app: app)
        }
        .childSheet(app.editors, dismiss: { shell.send(.cancel, to: $0) }) { key, editor in
            TaskEditorView(key: key, editor: editor)
        }
        .confirmation(app.confirmation, confirm: { shell.send(.confirm) }, dismiss: { shell.send(.dismissConfirmation) })
        .problem(app.problem, dismiss: { shell.send(.dismissProblem) })
        .confirmationDialog(
            app.choice?.title ?? "", isPresented: Binding(get: { app.choice != nil }, set: { if !$0 { shell.send(.dismissChoice) } }),
            titleVisibility: .visible, presenting: app.choice
        ) { choice in
            ForEach(choice.options) { option in
                Button(option.title) { shell.send(.decide(choice.task, choice.decision, to: option.id)) }
            }
            Button(choice.cancel, role: .cancel) { shell.send(.dismissChoice) }
        }
        // Asked from Settings, an approval is said there; turned down after
        // a page's line, it is said over whatever is shown.
        .alert(
            app.approval?.refused ?? "",
            isPresented: Binding(
                get: { app.approval?.refused != nil && app.menu.selected != TasksMenu.settings }, set: { _ in }),
            presenting: app.approval
        ) { approval in
            Button(approval.cancel, role: .cancel) { shell.send(.dismissApproval) }
        }
    }
}

/// The link's banner and the app's notices above a column; a change the
/// owner must answer opens Settings, where its Retry and Discard are.
struct Notices: View {
    @Environment(TasksShell.self) private var shell
    let link: LinkPresentation<TasksPresentation>
    let app: TasksPresentation

    var body: some View {
        LinkNotices<TasksApp>(link: link, lines: app.notices) { shell.send(.select(section: TasksMenu.settings)) }
    }
}
