import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// One list's tasks: the open ones in the order the presentation gives,
/// dragged to reorder where it allows, then the completed ones when shown;
/// the quick add field at the bottom. Every change is a command.
struct TaskListView: View {
    @Environment(TasksShell.self) private var shell
    @Environment(\.horizontalSizeClass) private var sizeClass
    let list: ListPresentation
    let app: TasksPresentation
    let link: LinkPresentation<TasksPresentation>
    @State private var draft = ""
    @State private var naming: ListNaming?
    @FocusState private var adding: Bool

    var body: some View {
        List(selection: selection) { rows }
        .overlay {
            if let empty = list.empty, list.closed.rows.isEmpty {
                ContentUnavailableView(empty.title, systemImage: "checkmark.circle", description: Text(empty.message))
            }
        }
        .navigationTitle(list.title)
        .refreshable { shell.send(.refresh) }
        .scrollDismissesKeyboard(.immediately)
        .safeAreaInset(edge: .bottom) { quickAdd }
        .toolbar { toolbar }
        .listNaming($naming)
    }

    /// On a phone a row is a button that pushes the task; beside a detail
    /// column, the open task is the list's selection.
    private var selection: Binding<String?> {
        Binding(get: { sizeClass == .compact ? nil : app.detail?.id }, set: { task in
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

    @ViewBuilder
    private func rowView(_ row: TaskRow) -> some View {
        Group {
            if sizeClass == .compact {
                Button { shell.send(.open(task: row.id)) } label: { TaskRowView(row: row) }
                    .buttonStyle(.plain)
            } else {
                TaskRowView(row: row).tag(row.id)
            }
        }
        .swipeActions(edge: .leading) {
            Button(row.toggle) { shell.send(row.status == .open ? .complete(row.id) : .reopen(row.id)) }
                .tint(.green)
        }
        .swipeActions(edge: .trailing) {
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

    private var quickAdd: some View {
        HStack(spacing: 10) {
            Image(systemName: "plus.circle.fill").foregroundStyle(.tint).font(.title3)
            TextField(list.quickAdd, text: $draft)
                .focused($adding)
                .submitLabel(.done)
                .onSubmit {
                    // Return on what was typed adds it and stays for the
                    // next; return on an empty field ends adding.
                    adding = !draft.trimmingCharacters(in: .whitespaces).isEmpty
                    shell.send(.quickAdd(draft))
                    draft = ""
                }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .glassEffect(.regular.interactive(), in: .capsule)
        .padding(.horizontal)
        .padding(.bottom, 8)
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Button { shell.send(.newTask) } label: { Label(list.newTask, systemImage: "square.and.pencil") }
        }
        ToolbarItem(placement: .secondaryAction) {
            Menu {
                if list.selection != .today {
                    Button(list.toggleClosed) {
                        shell.send(.settings(.toggle(row: .app(TasksSettings.showCompleted), on: !list.showsClosed)))
                    }
                }
                ForEach(list.actions, id: \.self) { action in
                    switch action.action {
                    case let .rename(id): Button(action.title) { naming = ListNaming(id: id, name: list.title) }
                    case let .delete(id): Button(action.title, role: .destructive) { shell.send(.deleteList(id)) }
                    }
                }
            } label: {
                Label("List Options", systemImage: "ellipsis.circle")
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
                Toggle(level.title, isOn: Binding(get: { priority.rawValue == level.id },
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
