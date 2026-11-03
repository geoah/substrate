import SubstrateScreens
import SubstrateUI
import SwiftUI
import TemplateCore

/// The app's one screen: what stands in the way, the items, and the field
/// that adds one. Every change is a command.
struct ItemsView: View {
    @Environment(TemplateShell.self) private var shell
    let link: LinkPresentation<TemplatePresentation>
    let app: TemplatePresentation
    @State private var draft = ""

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
                TextField(app.add, text: $draft)
                    .onSubmit {
                        shell.send(.add(draft))
                        draft = ""
                    }
                    .textFieldStyle(.roundedBorder)
                    .padding()
            }
            .refreshable { shell.send(.refresh) }
            .navigationTitle(app.title)
        }
        .problem(app.problem, dismiss: { shell.send(.dismissProblem) })
    }
}
