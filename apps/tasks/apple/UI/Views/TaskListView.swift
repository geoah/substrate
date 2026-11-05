import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// One list's tasks: the open ones in the order the presentation gives,
/// dragged to reorder where it allows, then the completed ones when shown;
/// New Task at the bottom, and the list's options one tap away. Every
/// change is a command.
struct TaskListView: View {
    @Environment(TasksShell.self) private var shell
    @Environment(\.horizontalSizeClass) private var sizeClass
    let list: ListPresentation
    let app: TasksPresentation
    let link: LinkPresentation<TasksPresentation>
    @State private var naming: ListNaming?

    var body: some View {
        List(selection: selection) { rows }
            .overlay {
                if let empty = list.empty, list.closed.rows.isEmpty {
                    ContentUnavailableView(empty.title, systemImage: "checkmark.circle", description: Text(empty.message))
                }
            }
            .navigationTitle(list.title)
            .refreshable { shell.send(.refresh) }
            .safeAreaInset(edge: .bottom) { newTaskButton }
            .toolbar { toolbar }
            .listNaming($naming)
    }

    /// On a phone a row is a button that pushes the task; beside a detail
    /// column, the open task is the list's selection.
    private var selection: Binding<String?> {
        Binding(
            get: { sizeClass == .compact ? nil : app.detail?.id },
            set: { task in
                // On a phone a row is a button and the list keeps no selection:
                // what it writes back is no choice of the owner's.
                guard sizeClass != .compact else { return }
                shell.send(.open(task: task))
            })
    }

    @ViewBuilder
    private var rows: some View {
        if let note = list.orderNote {
            Text(note).font(.footnote).foregroundStyle(.secondary)
        }
        Section {
            ForEach(list.open.rows) { row in rowView(row) }
                .onMove(perform: reorder)
        }
        if list.showsClosed, !list.closed.rows.isEmpty {
            Section(list.closedTitle) {
                ForEach(list.closed.rows) { row in rowView(row) }
            }
        }
    }

    /// On a phone the row's content opens the task, beside its round mark;
    /// beside a detail column the row is the list's selection.
    @ViewBuilder
    private func rowView(_ row: TaskRow) -> some View {
        Group {
            if sizeClass == .compact {
                TaskRowView(row: row, open: { shell.send(.open(task: row.id)) })
            } else {
                TaskRowView(row: row).tag(row.id)
            }
        }
        // The open task's row is marked whichever path drew it: an iPad's
        // middle column is compact too, and there no selection draws one.
        // On a phone the task covers its list while it is open.
        .listRowBackground(Color.secondary.opacity(app.detail?.id == row.id ? 0.15 : 0))
        .swipeActions(edge: .leading) {
            Button(row.toggle) { shell.send(row.status == .open ? .complete(row.id) : .reopen(row.id)) }
                .tint(.green)
        }
        // Deleting is never a full swipe: it takes the revealed button.
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            Button(list.rowMenu.swipeDelete, role: .destructive) { shell.send(.deleteTask(row.id)) }
            if row.status == .open {
                Button(list.rowMenu.abandon) { shell.send(.abandon(row.id)) }
            }
        }
        .contextMenu { TaskMenu(task: row.id, status: row.status, priority: row.priority?.priority ?? .none, sidebar: app.sidebar, words: list.rowMenu) }
    }

    /// Dragging, only where the order is the owner's.
    private var reorder: ((IndexSet, Int) -> Void)? {
        guard list.reorders else { return nil }
        return { from, to in move(from, to) }
    }

    private func move(_ from: IndexSet, _ to: Int) {
        guard let source = from.first else { return }
        shell.send(.reorderTask(list.open.rows[source].id, to: to > source ? to - 1 : to))
    }

    /// The one way to add a task: the editor, seeded with this list (and
    /// today, in Today).
    private var newTaskButton: some View {
        Button {
            shell.send(.newTask)
        } label: {
            Label(list.newTask, systemImage: "plus")
                .font(.body.weight(.semibold))
                .padding(.horizontal, 6)
        }
        .buttonStyle(.glassProminent)
        .controlSize(.large)
        .tint(.brandAccent)
        .frame(maxWidth: .infinity, alignment: .trailing)
        .padding(.horizontal)
        .padding(.bottom, 8)
    }

    /// The order, as Settings offers it: one choice for every list.
    private var sort: (label: String, selected: String, options: [SettingsOption])? {
        for section in app.settings {
            for row in section.rows where row.id == .app(TasksSettings.order) {
                if case let .choice(selected, options) = row.control { return (row.label, selected, options) }
            }
        }
        return nil
    }

    /// Today is what is due, in due order and open: nothing to sort or show.
    private var offersViewOptions: Bool { list.selection != .today }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        if offersViewOptions || !list.actions.isEmpty {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    if offersViewOptions {
                        if let sort {
                            Picker(
                                sort.label,
                                selection: Binding(
                                    get: { sort.selected },
                                    set: { shell.send(.settings(.choose(row: .app(TasksSettings.order), option: $0))) })
                            ) {
                                ForEach(sort.options, id: \.id) { Text($0.label).tag($0.id) }
                            }
                            .pickerStyle(.menu)
                        }
                        Button(list.toggleClosed, systemImage: list.showsClosed ? "eye.slash" : "eye") {
                            shell.send(.settings(.toggle(row: .app(TasksSettings.showCompleted), on: !list.showsClosed)))
                        }
                    }
                    if !list.actions.isEmpty {
                        Divider()
                        ForEach(list.actions, id: \.self) { action in
                            switch action.action {
                            case let .rename(id):
                                Button(action.title, systemImage: "pencil") { naming = ListNaming(id: id, name: list.title) }
                            case let .delete(id):
                                Button(action.title, systemImage: "trash", role: .destructive) { shell.send(.deleteList(id)) }
                            }
                        }
                    }
                } label: {
                    Label("List Options", systemImage: "ellipsis")
                }
            }
        }
    }
}

/// What a task offers from its row's context menu: its state, its priority,
/// its list, edit and delete, in the presentation's words.
struct TaskMenu: View {
    @Environment(TasksShell.self) private var shell
    let task: String
    let status: TaskStatus
    let priority: TaskPriority
    let sidebar: SidebarPresentation
    let words: TaskMenuWords

    var body: some View {
        if status == .open {
            Button(words.complete, systemImage: "checkmark.circle") { shell.send(.complete(task)) }
            Button(words.abandon, systemImage: "xmark.circle") { shell.send(.abandon(task)) }
        } else {
            Button(words.reopen, systemImage: "circle") { shell.send(.reopen(task)) }
        }
        Button(words.edit, systemImage: "pencil") { shell.send(.edit(task)) }
        Menu(words.priority) {
            ForEach(words.priorities) { level in
                Toggle(
                    level.title,
                    isOn: Binding(
                        get: { priority.rawValue == level.id },
                        set: { _ in shell.send(.setPriority(task, TaskPriority(rawValue: level.id) ?? .none)) }))
            }
        }
        Menu(words.move) {
            ForEach(sidebar.fixed.filter { $0.selection != .today } + sidebar.lists.rows) { row in
                Button(row.title) { shell.send(.move(task, toList: row.selection.listID)) }
            }
        }
        Divider()
        Button(words.delete, systemImage: "trash", role: .destructive) { shell.send(.deleteTask(task)) }
    }
}
