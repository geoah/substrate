import SubstrateScreens
import SubstrateUI
import SwiftUI
import TemplateCore

/// The app's one screen: what stands in the way, the items, and the field
/// that adds one, whose text is the core's until the item it adds is kept.
/// Every change is a command.
struct ItemsView: View {
    @Environment(TemplateShell.self) private var shell
    let link: LinkPresentation<TemplatePresentation>
    let app: TemplatePresentation

    var body: some View {
        NavigationStack {
            List {
                ForEach(app.items.rows) { row in
                    Text(row.title)
                        .swipeActions { Button(row.delete, role: .destructive) { shell.send(.delete(row.id)) } }
                }
            }
            .overlay {
                if let empty = app.empty {
                    ContentUnavailableView(empty.title, systemImage: "tray", description: Text(empty.message))
                }
            }
            .safeAreaInset(edge: .top) { LinkNotices<TemplateApp>(link: link, lines: app.notices).padding(.horizontal) }
            .safeAreaInset(edge: .bottom) {
                VStack(alignment: .leading, spacing: 12) {
                    if let notKept = app.add.notKept {
                        ProblemView(notKept) { _ in shell.send(.add) }
                    }
                    TextField(app.add.prompt, text: Binding(get: { app.add.text }, set: { shell.send(.setDraft($0)) }))
                        .onSubmit { shell.send(.add) }
                        .textFieldStyle(.roundedBorder)
                }
                .padding()
                .background(.bar)
            }
            .refreshable { shell.send(.refresh) }
            .navigationTitle(app.title)
        }
        .problem(app.problem, dismiss: { shell.send(.dismissProblem) })
    }
}
