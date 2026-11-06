import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// The lists column: what needs the owner and the tasks in no list, then
/// the lists in the owner's order with their open counts, dragged to
/// reorder. With the App menu (iPad, Mac) it ends in Settings; on a phone,
/// Needs you and Settings are tabs of their own.
struct SidebarView: View {
    @Environment(TasksShell.self) private var shell
    let sidebar: SidebarPresentation
    let link: LinkPresentation<TasksPresentation>
    let app: TasksPresentation
    var menu: AppMenuPresentation?
    let select: (ListSelection) -> Void
    @State private var naming: ListNaming?

    enum Item: Hashable {
        case list(ListSelection)
        case settings
    }

    var body: some View {
        List(selection: menu == nil ? nil : selection) {
            Section {
                ForEach(sidebar.fixed.filter { menu != nil || $0.selection != .needsYou }) { row in
                    rowView(row)
                }
            }
            Section(sidebar.listsTitle) {
                ForEach(sidebar.lists.rows) { row in
                    rowView(row)
                        .contextMenu { actions(row.actions) }
                }
                .onMove { from, to in
                    guard let source = from.first else { return }
                    let id = sidebar.lists.rows[source].id
                    shell.send(.reorderList(id, to: to > source ? to - 1 : to))
                }
                if let noLists = sidebar.noLists {
                    Text(noLists).font(.footnote).foregroundStyle(.secondary)
                }
            }
            if let settings = menu?.sections.first(where: { $0.id == TasksMenu.settings }) {
                Section {
                    Label(settings.title, systemImage: TasksSymbols.symbol(settings.symbol))
                        .tag(Item.settings)
                }
            }
        }
        .navigationTitle(sidebar.title)
        .safeAreaInset(edge: .top) {
            Notices(link: link, app: app)
                .padding(.horizontal)
        }
        .scrollContentBackground(menu == nil ? .hidden : .automatic)
        .background { if menu == nil { TasksSymbols.screen } }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button {
                    naming = ListNaming(id: nil, name: "")
                } label: {
                    Label(sidebar.newList, systemImage: "folder.badge.plus")
                }
            }
        }
        .refreshable { shell.send(.refresh) }
        .listNaming($naming)
    }

    private var selection: Binding<Item?> {
        Binding(
            get: {
                if menu?.selected == TasksMenu.settings { return .settings }
                return sidebar.selected.map(Item.list)
            },
            set: { item in
                switch item {
                case .settings?: shell.send(.select(section: TasksMenu.settings))
                case let .list(selection)?:
                    shell.send(.select(section: selection == .needsYou ? TasksMenu.needsYou : TasksMenu.lists))
                    select(selection)
                case nil: break
                }
            })
    }

    @ViewBuilder
    private func rowView(_ row: SidebarRow) -> some View {
        let label = Label {
            Text(row.title).foregroundStyle(Color.primary)
        } icon: {
            Image(systemName: TasksSymbols.symbol(row.symbol)).foregroundStyle(Color.brandAccent)
        }
        if menu == nil {
            // A button tints what it holds, so the count is drawn here,
            // grey, rather than as the row's badge.
            Button {
                select(row.selection)
            } label: {
                HStack {
                    label
                    Spacer()
                    if row.open > 0 { Text(row.open, format: .number).foregroundStyle(Color.secondary) }
                    Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(Color.secondary.opacity(0.6))
                }
            }
            .accessibilityValue(row.openWords)
        } else {
            label
                .badge(row.open)
                .accessibilityValue(row.openWords)
                .tag(Item.list(row.selection))
        }
    }

    @ViewBuilder
    private func actions(_ actions: [ActionButton<ListAction>]) -> some View {
        ForEach(actions, id: \.self) { action in
            switch action.action {
            case let .rename(id):
                Button(action.title) {
                    naming = ListNaming(id: id, name: sidebar.lists[id]?.title ?? "")
                }
            case let .delete(id):
                Button(action.title, role: .destructive) { shell.send(.deleteList(id)) }
            }
        }
    }
}

/// A list being named: a new one, or one renamed. What is typed is the
/// field's until it is sent.
struct ListNaming: Identifiable, Equatable {
    var id: String?
    var name: String
}

extension View {
    func listNaming(_ naming: Binding<ListNaming?>) -> some View {
        modifier(ListNamingAlert(naming: naming))
    }
}

private struct ListNamingAlert: ViewModifier {
    @Environment(TasksShell.self) private var shell
    @Binding var naming: ListNaming?

    func body(content: Content) -> some View {
        content.alert(naming?.id == nil ? "New List" : "Rename List", isPresented: Binding(get: { naming != nil }, set: { if !$0 { naming = nil } })) {
            TextField("Name", text: Binding(get: { naming?.name ?? "" }, set: { naming?.name = $0 }))
            Button("Cancel", role: .cancel) { naming = nil }
            Button(naming?.id == nil ? "Add" : "Rename") {
                guard let naming else { return }
                if let id = naming.id {
                    shell.send(.renameList(id, to: naming.name))
                } else {
                    shell.send(.createList(naming.name))
                }
                self.naming = nil
            }
        } message: {
            Text("A task list's name, such as Groceries or Home.")
        }
    }
}
