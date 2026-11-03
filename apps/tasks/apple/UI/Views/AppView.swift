import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// The App menu's sections (§5.6): a tab bar on a phone, a sidebar on an
/// iPad and a Mac, the same presentation under both. What is open over
/// them (an editor, a question, a problem) is drawn here once.
struct AppView: View {
    @Environment(TasksShell.self) private var shell
    @Environment(\.horizontalSizeClass) private var sizeClass
    let link: LinkPresentation<TasksPresentation>
    let app: TasksPresentation

    var body: some View {
        Group {
            if sizeClass == .compact {
                TabRoot(link: link, app: app)
            } else {
                SplitRoot(link: link, app: app)
            }
        }
        .sheet(item: Binding(get: { app.editors.keys.sorted().first.map(EditorKey.init) },
                             set: { key in if key == nil, let open = app.editors.keys.sorted().first { shell.send(.cancel, to: open) } })) { key in
            if let editor = app.editors[key.key] {
                TaskEditorView(key: key.key, editor: editor)
            }
        }
        .confirmationDialog(app.confirmation?.title ?? "", isPresented: Binding(get: { app.confirmation != nil },
                                                                                set: { if !$0 { shell.send(.dismissConfirmation) } }),
                            titleVisibility: .visible, presenting: app.confirmation) { confirmation in
            Button(confirmation.confirm, role: .destructive) { shell.send(.confirm) }
            Button(confirmation.cancel, role: .cancel) { shell.send(.dismissConfirmation) }
        } message: { confirmation in
            Text(confirmation.message)
        }
        .alert(app.problem?.title ?? "", isPresented: Binding(get: { app.problem != nil }, set: { if !$0 { shell.send(.dismissProblem) } }),
               presenting: app.problem) { _ in
            Button("OK") { shell.send(.dismissProblem) }
        } message: { problem in
            Text(problem.message)
        }
    }
}

/// An editor's key, as a sheet's item.
struct EditorKey: Identifiable, Hashable {
    let key: ChildKey
    var id: ChildKey { key }
}

/// The link's banner, then the line about reading the repository, above a
/// column.
struct LinkNotices: View {
    @Environment(TasksShell.self) private var shell
    @Environment(\.openURL) private var openURL
    let link: LinkPresentation<TasksPresentation>
    let app: TasksPresentation

    var body: some View {
        VStack(spacing: 8) {
            if let banner = link.banner {
                AccessBannerView(banner) { action in
                    switch action {
                    case .review, .signInAgain, .moveToAppAccess: Task { try? await shell.changeAccess() }
                    case .retry: shell.send(.refresh(force: true))
                    case let .openFix(path):
                        if let server = link.snapshot.server, let url = ServerAddress.page(server, path: path) { openURL(url) }
                    }
                }
            }
            if let sync = app.sync {
                Label(sync.text, systemImage: sync.tone == .quiet ? "arrow.triangle.2.circlepath" : "exclamationmark.triangle")
                    .font(.footnote)
                    .foregroundStyle(TasksSymbols.color(sync.tone))
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}
