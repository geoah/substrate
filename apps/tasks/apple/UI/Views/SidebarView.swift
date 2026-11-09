import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// The lists column. On a phone (Lists): search, the pinned groups, then a
/// section per group kind, headed by its label with its full reference
/// under it, listing the groups with open tasks, the tasks in no group,
/// those whose group is gone, and how many more hold nothing open. With the
/// App menu (iPad, Mac) it opens with Needs you, Suggestions and No date,
/// shows each kind's busiest groups with "All N…", and ends in Settings.
/// The app's own task lists are dragged to reorder.
struct SidebarView: View {
    @Environment(TasksShell.self) private var shell
    let sidebar: SidebarPresentation
    let link: LinkPresentation<TasksPresentation>
    let app: TasksPresentation
    var menu: AppMenuPresentation?
    let select: (ListSelection) -> Void
    @State private var naming: ListNaming?
    @State private var search = ""
    @State private var opened: Set<String> = []

    enum Item: Hashable {
        case list(ListSelection)
        case settings
    }

    var body: some View {
        List(selection: menu == nil ? nil : selection) {
            if menu != nil {
                Section {
                    ForEach(sidebar.fixed) { row in rowView(row) }
                }
            }
            if !sidebar.pinned.isEmpty, matching(sidebar.pinned).count > 0 {
                Section(sidebar.pinnedTitle) {
                    ForEach(matching(sidebar.pinned)) { row in rowView(row) }
                }
            }
            ForEach(sidebar.sections) { section in
                Section {
                    let shown = rows(of: section)
                    ForEach(shown) { row in
                        rowView(row).contextMenu { actions(row.actions) }
                    }
                    .onMove(perform: section.reorders ? { from, to in reorder(section, shown, from, to) } : nil)
                    if menu != nil, let all = section.all, !opened.contains(section.id), search.isEmpty {
                        Button(all) { opened.insert(section.id) }
                            .foregroundStyle(.secondary)
                    } else if menu == nil, let more = section.more, !opened.contains(section.id), search.isEmpty {
                        Button {
                            opened.insert(section.id)
                        } label: {
                            HStack {
                                Text(more).foregroundStyle(.secondary)
                                Spacer()
                                Image(systemName: "chevron.down").font(.footnote.weight(.semibold)).foregroundStyle(Color.secondary.opacity(0.6))
                            }
                        }
                    }
                    if section.reorders, let noLists = sidebar.noLists {
                        Text(noLists).font(.footnote).foregroundStyle(.secondary)
                    }
                } header: {
                    VStack(alignment: .leading, spacing: 1) {
                        Text(section.title)
                        Text(section.reference).font(.caption2.monospaced()).foregroundStyle(.tertiary).textCase(nil)
                    }
                }
            }
            if !matching(sidebar.ungrouped).isEmpty {
                Section(sidebar.ungroupedTitle) {
                    ForEach(matching(sidebar.ungrouped)) { row in rowView(row) }
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
        .searching($search, prompt: sidebar.search, if: menu == nil)
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

    /// A section's rows as this column shows them: what search matches;
    /// else, wide, the busiest until "All" is chosen; on a phone, those with
    /// open tasks, and the rest once "more" is.
    private func rows(of section: GroupSection) -> [SidebarRow] {
        if !search.isEmpty { return matching(section.rows + section.quiet) }
        let open = opened.contains(section.id)
        if menu != nil { return open ? section.rows + section.quiet : Array(section.rows.prefix(section.busiest)) }
        return open ? section.rows + section.quiet : section.rows
    }

    private func matching(_ rows: [SidebarRow]) -> [SidebarRow] {
        let query = search.trimmingCharacters(in: .whitespaces)
        guard !query.isEmpty else { return rows }
        return rows.filter { $0.title.localizedCaseInsensitiveContains(query) }
    }

    private func reorder(_ section: GroupSection, _ shown: [SidebarRow], _ from: IndexSet, _ to: Int) {
        guard let source = from.first, let id = shown[source].selection.listID else { return }
        let lists = shown.filter { $0.selection.listID != nil }
        let target = min(to > source ? to - 1 : to, max(lists.count - 1, 0))
        shell.send(.reorderList(id, to: target))
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
            VStack(alignment: .leading, spacing: 1) {
                Text(row.title).foregroundStyle(Color.primary)
                if let detail = row.detail, !detail.isEmpty {
                    Text(detail).font(.caption).foregroundStyle(.secondary)
                }
            }
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
                    if row.pinned { Image(systemName: "pin").font(.caption).foregroundStyle(.secondary) }
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
                    let title = sidebar.sections.flatMap { $0.rows + $0.quiet }.first { $0.selection.listID == id }?.title ?? ""
                    naming = ListNaming(id: id, name: title)
                }
            case let .delete(id):
                Button(action.title, role: .destructive) { shell.send(.deleteList(id)) }
            }
        }
    }
}

extension View {
    /// Search on a phone's Lists; the wide sidebar shows each kind's
    /// busiest groups and "All N…" instead.
    @ViewBuilder
    func searching(_ text: Binding<String>, prompt: String, if on: Bool) -> some View {
        if on { searchable(text: text, prompt: prompt) } else { self }
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
